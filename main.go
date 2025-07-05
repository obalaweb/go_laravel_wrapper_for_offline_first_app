package main

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
)

type Config struct {
	LaravelPath    string   `json:"laravel_path"`
	LocalPort      int      `json:"local_port"`
	PHPPort        int      `json:"php_port"`
	LocalDB        DBConfig `json:"local_db"`
	RemoteDB       DBConfig `json:"remote_db"`
	SyncInterval   int      `json:"sync_interval_seconds"`
	CheckInterval  int      `json:"connectivity_check_interval_seconds"`
}

type DBConfig struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Database string `json:"database"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type SyncRecord struct {
	ID        int       `json:"id"`
	TableName string    `json:"table_name"`
	Operation string    `json:"operation"` // INSERT, UPDATE, DELETE
	Data      string    `json:"data"`      // JSON data
	CreatedAt time.Time `json:"created_at"`
	Synced    bool      `json:"synced"`
}

// Laravel sync request structure
type LaravelSyncRequest struct {
	TableName string                 `json:"table_name"`
	Operation string                 `json:"operation"`
	Data      map[string]interface{} `json:"data"`
}

// Response structure for Laravel sync requests
type SyncResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
	Error   string `json:"error,omitempty"`
}

type LaravelWrapper struct {
	config          Config
	localDB         *sql.DB
	remoteDB        *sql.DB
	phpServer       *exec.Cmd
	isOnline        bool
	onlineMutex     sync.RWMutex
	syncMutex       sync.Mutex
	clients         map[*websocket.Conn]bool
	clientsMutex    sync.RWMutex
	upgrader        websocket.Upgrader
}

func main() {
	// Setup logging to file
	logFile, err := os.OpenFile("laravel-wrapper.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		log.Fatal("Failed to open log file:", err)
	}
	defer logFile.Close()
	
	// Log to both file and console
	log.SetOutput(io.MultiWriter(os.Stdout, logFile))
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	wrapper := &LaravelWrapper{
		clients: make(map[*websocket.Conn]bool),
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true
			},
		},
	}

	if err := wrapper.loadConfig(); err != nil {
		log.Fatal("Failed to load config:", err)
	}

	if err := wrapper.setupDatabases(); err != nil {
		log.Fatal("Failed to setup databases:", err)
	}

	if err := wrapper.startPHPServer(); err != nil {
		log.Fatal("Failed to start PHP server:", err)
	}

	// Start background services
	go wrapper.connectivityChecker()
	go wrapper.syncScheduler()

	// Start HTTP server
	wrapper.startHTTPServer()
}

func (w *LaravelWrapper) loadConfig() error {
	configFile := "config.json"
	if len(os.Args) > 1 {
		configFile = os.Args[1]
	}

	data, err := os.ReadFile(configFile)
	if err != nil {
		// Create default config if not exists
		w.config = Config{
			LaravelPath:   "./laravel-app",
			LocalPort:     8080,
			PHPPort:       8000,
			SyncInterval:  30,
			CheckInterval: 10,
			LocalDB: DBConfig{
				Host:     "localhost",
				Port:     3306,
				Database: "laravel_local",
				Username: "root",
				Password: "",
			},
			RemoteDB: DBConfig{
				Host:     "remote-db.example.com",
				Port:     3306,
				Database: "laravel_remote",
				Username: "user",
				Password: "password",
			},
		}
		return w.saveConfig(configFile)
	}

	return json.Unmarshal(data, &w.config)
}

func (w *LaravelWrapper) saveConfig(filename string) error {
	data, err := json.MarshalIndent(w.config, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filename, data, 0644)
}

func (w *LaravelWrapper) setupDatabases() error {
	// Setup local database
	localDSN := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?parseTime=true",
		w.config.LocalDB.Username,
		w.config.LocalDB.Password,
		w.config.LocalDB.Host,
		w.config.LocalDB.Port,
		w.config.LocalDB.Database,
	)

	var err error
	w.localDB, err = sql.Open("mysql", localDSN)
	if err != nil {
		return fmt.Errorf("failed to connect to local database: %v", err)
	}

	// Test local database connection
	if err := w.localDB.Ping(); err != nil {
		return fmt.Errorf("failed to ping local database: %v", err)
	}

	// Create sync table if not exists
	if err := w.createSyncTable(); err != nil {
		return fmt.Errorf("failed to create sync table: %v", err)
	}

	// Setup remote database connection (will be tested during connectivity check)
	remoteDSN := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?parseTime=true",
		w.config.RemoteDB.Username,
		w.config.RemoteDB.Password,
		w.config.RemoteDB.Host,
		w.config.RemoteDB.Port,
		w.config.RemoteDB.Database,
	)

	w.remoteDB, err = sql.Open("mysql", remoteDSN)
	if err != nil {
		log.Printf("Warning: Failed to setup remote database connection: %v", err)
	}

	return nil
}

func (w *LaravelWrapper) createSyncTable() error {
	query := `
	CREATE TABLE IF NOT EXISTS sync_queue (
		id INT AUTO_INCREMENT PRIMARY KEY,
		table_name VARCHAR(255) NOT NULL,
		operation VARCHAR(10) NOT NULL,
		data TEXT NOT NULL,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		synced BOOLEAN DEFAULT FALSE,
		retry_count INT DEFAULT 0,
		last_error TEXT,
		INDEX idx_synced (synced),
		INDEX idx_created_at (created_at),
		INDEX idx_table_operation (table_name, operation)
	)`

	_, err := w.localDB.Exec(query)
	return err
}

func (w *LaravelWrapper) startPHPServer() error {
	// Check if Laravel directory exists
	if _, err := os.Stat(w.config.LaravelPath); os.IsNotExist(err) {
		return fmt.Errorf("Laravel directory not found: %s", w.config.LaravelPath)
	}

	// Start PHP built-in server
	w.phpServer = exec.Command("php", "artisan", "serve", 
		"--host=127.0.0.1", 
		fmt.Sprintf("--port=%d", w.config.PHPPort))
	w.phpServer.Dir = w.config.LaravelPath

	// Capture output
	stdout, err := w.phpServer.StdoutPipe()
	if err != nil {
		return err
	}

	stderr, err := w.phpServer.StderrPipe()
	if err != nil {
		return err
	}

	if err := w.phpServer.Start(); err != nil {
		return fmt.Errorf("failed to start PHP server: %v", err)
	}

	// Log PHP server output
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			log.Printf("PHP Server: %s", scanner.Text())
		}
	}()

	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			log.Printf("PHP Server Error: %s", scanner.Text())
		}
	}()

	// Wait for PHP server to start
	time.Sleep(2 * time.Second)

	// Check if server is running
	if !w.isPortOpen("127.0.0.1", w.config.PHPPort) {
		return fmt.Errorf("PHP server failed to start on port %d", w.config.PHPPort)
	}

	log.Printf("PHP server started on port %d", w.config.PHPPort)
	return nil
}

func (w *LaravelWrapper) isPortOpen(host string, port int) bool {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", host, port), 3*time.Second)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

func (w *LaravelWrapper) startHTTPServer() {
	r := mux.NewRouter()

	// WebSocket endpoint for real-time updates
	r.HandleFunc("/ws", w.handleWebSocket)

	// Status endpoint
	r.HandleFunc("/status", w.handleStatus).Methods("GET")

	// Laravel sync endpoint - this is where Laravel sends sync requests
	r.HandleFunc("/sync-record", w.handleLaravelSync).Methods("POST")

	// Manual sync endpoint
	r.HandleFunc("/sync", w.handleManualSync).Methods("POST")

	// Health check endpoint
	r.HandleFunc("/health", w.handleHealth).Methods("GET")

	// Sync queue status endpoint
	r.HandleFunc("/queue/status", w.handleQueueStatus).Methods("GET")

	// Clear sync queue endpoint
	r.HandleFunc("/queue/clear", w.handleClearQueue).Methods("POST")

	// Retry failed sync records
	r.HandleFunc("/queue/retry", w.handleRetryFailed).Methods("POST")

	// Proxy all other requests to Laravel
	r.PathPrefix("/").HandlerFunc(w.proxyToLaravel)

	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", w.config.LocalPort),
		Handler:      r,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	// Handle graceful shutdown
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		<-sigChan

		log.Println("Shutting down...")
		w.shutdown()
		server.Shutdown(context.Background())
		os.Exit(0)
	}()

	log.Printf("Server started on port %d", w.config.LocalPort)
	log.Printf("Laravel sync endpoint: http://localhost:%d/sync-record", w.config.LocalPort)
	log.Fatal(server.ListenAndServe())
}

// Handle Laravel sync requests
func (w *LaravelWrapper) handleLaravelSync(wr http.ResponseWriter, r *http.Request) {
	var syncReq LaravelSyncRequest
	
	// Parse JSON body
	if err := json.NewDecoder(r.Body).Decode(&syncReq); err != nil {
		log.Printf("Failed to parse Laravel sync request: %v", err)
		w.sendSyncResponse(wr, http.StatusBadRequest, false, "", "Invalid JSON format")
		return
	}

	// Validate request
	if syncReq.TableName == "" || syncReq.Operation == "" {
		log.Printf("Invalid sync request: missing table_name or operation")
		w.sendSyncResponse(wr, http.StatusBadRequest, false, "", "Missing table_name or operation")
		return
	}

	// Validate operation
	validOps := map[string]bool{"INSERT": true, "UPDATE": true, "DELETE": true}
	if !validOps[syncReq.Operation] {
		log.Printf("Invalid operation: %s", syncReq.Operation)
		w.sendSyncResponse(wr, http.StatusBadRequest, false, "", "Invalid operation")
		return
	}

	log.Printf("Received Laravel sync request: %s %s", syncReq.Operation, syncReq.TableName)

	// Convert data to JSON string
	dataJSON, err := json.Marshal(syncReq.Data)
	if err != nil {
		log.Printf("Failed to marshal sync data: %v", err)
		w.sendSyncResponse(wr, http.StatusInternalServerError, false, "", "Failed to process data")
		return
	}

	// Check if we're online and can sync immediately
	w.onlineMutex.RLock()
	online := w.isOnline
	w.onlineMutex.RUnlock()

	if online {
		// Try to sync immediately
		if w.syncRecordToRemote(syncReq.TableName, syncReq.Operation, syncReq.Data) {
			log.Printf("Successfully synced %s %s immediately", syncReq.Operation, syncReq.TableName)
			w.sendSyncResponse(wr, http.StatusOK, true, "Synced immediately", "")
			
			// Broadcast to WebSocket clients
			w.broadcast(map[string]interface{}{
				"type":      "sync_immediate",
				"table":     syncReq.TableName,
				"operation": syncReq.Operation,
			})
			return
		}
	}

	// Add to sync queue if immediate sync failed or we're offline
	if err := w.addToSyncQueue(syncReq.TableName, syncReq.Operation, string(dataJSON)); err != nil {
		log.Printf("Failed to add to sync queue: %v", err)
		w.sendSyncResponse(wr, http.StatusInternalServerError, false, "", "Failed to queue sync")
		return
	}

	log.Printf("Added to sync queue: %s %s", syncReq.Operation, syncReq.TableName)
	w.sendSyncResponse(wr, http.StatusOK, true, "Queued for sync", "")

	// Broadcast to WebSocket clients
	w.broadcast(map[string]interface{}{
		"type":      "sync_queued",
		"table":     syncReq.TableName,
		"operation": syncReq.Operation,
	})
}

func (w *LaravelWrapper) sendSyncResponse(wr http.ResponseWriter, statusCode int, success bool, message, error string) {
	wr.Header().Set("Content-Type", "application/json")
	wr.WriteHeader(statusCode)
	
	resp := SyncResponse{
		Success: success,
		Message: message,
		Error:   error,
	}
	
	json.NewEncoder(wr).Encode(resp)
}

func (w *LaravelWrapper) addToSyncQueue(tableName, operation, data string) error {
	query := "INSERT INTO sync_queue (table_name, operation, data) VALUES (?, ?, ?)"
	_, err := w.localDB.Exec(query, tableName, operation, data)
	return err
}

func (w *LaravelWrapper) syncRecordToRemote(tableName, operation string, data map[string]interface{}) bool {
	if w.remoteDB == nil {
		return false
	}

	switch operation {
	case "INSERT":
		return w.performInsert(tableName, data)
	case "UPDATE":
		return w.performUpdate(tableName, data)
	case "DELETE":
		return w.performDelete(tableName, data)
	default:
		log.Printf("Unknown operation: %s", operation)
		return false
	}
}

func (w *LaravelWrapper) handleWebSocket(wr http.ResponseWriter, r *http.Request) {
	conn, err := w.upgrader.Upgrade(wr, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}
	defer conn.Close()

	w.clientsMutex.Lock()
	w.clients[conn] = true
	w.clientsMutex.Unlock()

	// Send initial status
	w.sendToClient(conn, map[string]interface{}{
		"type":   "status",
		"online": w.isOnline,
	})

	// Send queue status
	pendingCount := w.getPendingSyncCount()
	w.sendToClient(conn, map[string]interface{}{
		"type":    "queue_status",
		"pending": pendingCount,
	})

	// Handle client messages
	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			break
		}
	}

	w.clientsMutex.Lock()
	delete(w.clients, conn)
	w.clientsMutex.Unlock()
}

func (w *LaravelWrapper) sendToClient(conn *websocket.Conn, data map[string]interface{}) {
	if err := conn.WriteJSON(data); err != nil {
		log.Printf("WebSocket write error: %v", err)
	}
}

func (w *LaravelWrapper) broadcast(data map[string]interface{}) {
	w.clientsMutex.RLock()
	defer w.clientsMutex.RUnlock()

	for conn := range w.clients {
		go w.sendToClient(conn, data)
	}
}

func (w *LaravelWrapper) handleStatus(wr http.ResponseWriter, r *http.Request) {
	w.onlineMutex.RLock()
	online := w.isOnline
	w.onlineMutex.RUnlock()

	// Get pending sync count
	pendingCount := w.getPendingSyncCount()

	// Get failed sync count
	failedCount := w.getFailedSyncCount()

	status := map[string]interface{}{
		"online":       online,
		"pending_sync": pendingCount,
		"failed_sync":  failedCount,
		"local_port":   w.config.LocalPort,
		"php_port":     w.config.PHPPort,
		"timestamp":    time.Now().Format(time.RFC3339),
	}

	wr.Header().Set("Content-Type", "application/json")
	json.NewEncoder(wr).Encode(status)
}

func (w *LaravelWrapper) handleManualSync(wr http.ResponseWriter, r *http.Request) {
	go w.performSync()
	wr.Header().Set("Content-Type", "application/json")
	json.NewEncoder(wr).Encode(map[string]string{"status": "sync_started"})
}

func (w *LaravelWrapper) handleHealth(wr http.ResponseWriter, r *http.Request) {
	health := map[string]interface{}{
		"status":    "healthy",
		"timestamp": time.Now().Format(time.RFC3339),
		"services": map[string]bool{
			"local_db":   w.isDBHealthy(w.localDB),
			"remote_db":  w.isDBHealthy(w.remoteDB),
			"php_server": w.isPortOpen("127.0.0.1", w.config.PHPPort),
		},
	}

	wr.Header().Set("Content-Type", "application/json")
	json.NewEncoder(wr).Encode(health)
}

func (w *LaravelWrapper) handleQueueStatus(wr http.ResponseWriter, r *http.Request) {
	pendingCount := w.getPendingSyncCount()
	failedCount := w.getFailedSyncCount()
	
	// Get oldest pending record
	var oldestPending *time.Time
	row := w.localDB.QueryRow("SELECT created_at FROM sync_queue WHERE synced = FALSE ORDER BY created_at ASC LIMIT 1")
	var timestamp time.Time
	if err := row.Scan(&timestamp); err == nil {
		oldestPending = &timestamp
	}

	status := map[string]interface{}{
		"pending_count": pendingCount,
		"failed_count":  failedCount,
		"oldest_pending": oldestPending,
		"timestamp":     time.Now().Format(time.RFC3339),
	}

	wr.Header().Set("Content-Type", "application/json")
	json.NewEncoder(wr).Encode(status)
}

func (w *LaravelWrapper) handleClearQueue(wr http.ResponseWriter, r *http.Request) {
	result, err := w.localDB.Exec("DELETE FROM sync_queue WHERE synced = TRUE")
	if err != nil {
		log.Printf("Failed to clear sync queue: %v", err)
		http.Error(wr, "Failed to clear queue", http.StatusInternalServerError)
		return
	}

	rowsAffected, _ := result.RowsAffected()
	response := map[string]interface{}{
		"status":        "cleared",
		"rows_affected": rowsAffected,
	}

	wr.Header().Set("Content-Type", "application/json")
	json.NewEncoder(wr).Encode(response)
}

func (w *LaravelWrapper) handleRetryFailed(wr http.ResponseWriter, r *http.Request) {
	// Reset failed records for retry
	result, err := w.localDB.Exec("UPDATE sync_queue SET retry_count = 0, last_error = NULL WHERE retry_count > 0")
	if err != nil {
		log.Printf("Failed to reset failed records: %v", err)
		http.Error(wr, "Failed to reset failed records", http.StatusInternalServerError)
		return
	}

	rowsAffected, _ := result.RowsAffected()
	
	// Trigger sync
	go w.performSync()

	response := map[string]interface{}{
		"status":        "retry_started",
		"rows_affected": rowsAffected,
	}

	wr.Header().Set("Content-Type", "application/json")
	json.NewEncoder(wr).Encode(response)
}

func (w *LaravelWrapper) getPendingSyncCount() int {
	var count int
	w.localDB.QueryRow("SELECT COUNT(*) FROM sync_queue WHERE synced = FALSE").Scan(&count)
	return count
}

func (w *LaravelWrapper) getFailedSyncCount() int {
	var count int
	w.localDB.QueryRow("SELECT COUNT(*) FROM sync_queue WHERE synced = FALSE AND retry_count > 0").Scan(&count)
	return count
}

func (w *LaravelWrapper) isDBHealthy(db *sql.DB) bool {
	if db == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return db.PingContext(ctx) == nil
}

func (w *LaravelWrapper) proxyToLaravel(wr http.ResponseWriter, r *http.Request) {
	// Create request to PHP server
	url := fmt.Sprintf("http://127.0.0.1:%d%s", w.config.PHPPort, r.URL.Path)
	if r.URL.RawQuery != "" {
		url += "?" + r.URL.RawQuery
	}

	req, err := http.NewRequest(r.Method, url, r.Body)
	if err != nil {
		http.Error(wr, "Failed to create request", http.StatusInternalServerError)
		return
	}

	// Copy headers
	for key, values := range r.Header {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}

	// Make request
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		http.Error(wr, "Failed to proxy request", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// Copy response headers
	for key, values := range resp.Header {
		for _, value := range values {
			wr.Header().Add(key, value)
		}
	}

	wr.WriteHeader(resp.StatusCode)
	io.Copy(wr, resp.Body)
}

func (w *LaravelWrapper) connectivityChecker() {
	ticker := time.NewTicker(time.Duration(w.config.CheckInterval) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			w.checkConnectivity()
		}
	}
}

func (w *LaravelWrapper) checkConnectivity() {
	online := false

	if w.remoteDB != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := w.remoteDB.PingContext(ctx); err == nil {
			online = true
		}
	}

	w.onlineMutex.Lock()
	previousState := w.isOnline
	w.isOnline = online
	w.onlineMutex.Unlock()

	if online != previousState {
		log.Printf("Connectivity changed: online=%v", online)
		w.broadcast(map[string]interface{}{
			"type":   "connectivity",
			"online": online,
		})

		if online {
			go w.performSync()
		}
	}
}

func (w *LaravelWrapper) syncScheduler() {
	ticker := time.NewTicker(time.Duration(w.config.SyncInterval) * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		w.onlineMutex.RLock()
		online := w.isOnline
		w.onlineMutex.RUnlock()

		if online {
			w.performSync()
		}
	}
}

func (w *LaravelWrapper) performSync() {
	w.syncMutex.Lock()
	defer w.syncMutex.Unlock()

	w.onlineMutex.RLock()
	online := w.isOnline
	w.onlineMutex.RUnlock()

	if !online {
		return
	}

	log.Println("Starting database sync...")

	// Get pending sync records
	rows, err := w.localDB.Query(`
		SELECT id, table_name, operation, data, created_at, retry_count
		FROM sync_queue 
		WHERE synced = FALSE 
		ORDER BY created_at ASC
		LIMIT 100
	`)
	if err != nil {
		log.Printf("Failed to get sync records: %v", err)
		return
	}
	defer rows.Close()

	var syncedIDs []int
	var failedIDs []int
	
	for rows.Next() {
		var record SyncRecord
		var retryCount int
		if err := rows.Scan(&record.ID, &record.TableName, &record.Operation, &record.Data, &record.CreatedAt, &retryCount); err != nil {
			log.Printf("Failed to scan sync record: %v", err)
			continue
		}

		// Skip if too many retries
		if retryCount >= 3 {
			continue
		}

		var data map[string]interface{}
		if err := json.Unmarshal([]byte(record.Data), &data); err != nil {
			log.Printf("Failed to parse sync data for record %d: %v", record.ID, err)
			failedIDs = append(failedIDs, record.ID)
			continue
		}

		if w.syncRecordToRemote(record.TableName, record.Operation, data) {
			syncedIDs = append(syncedIDs, record.ID)
		} else {
			failedIDs = append(failedIDs, record.ID)
		}
	}

	// Mark records as synced
	if len(syncedIDs) > 0 {
		w.markAsSynced(syncedIDs)
		log.Printf("Synced %d records", len(syncedIDs))
	}

	// Update retry count for failed records
	if len(failedIDs) > 0 {
		w.updateRetryCount(failedIDs)
		log.Printf("Failed to sync %d records", len(failedIDs))
	}

	if len(syncedIDs) > 0 || len(failedIDs) > 0 {
		w.broadcast(map[string]interface{}{
			"type":    "sync_complete",
			"synced":  len(syncedIDs),
			"failed":  len(failedIDs),
			"pending": w.getPendingSyncCount(),
		})
	}
}

func (w *LaravelWrapper) updateRetryCount(ids []int) {
	if len(ids) == 0 {
		return
	}

	placeholders := make([]string, len(ids))
	values := make([]interface{}, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		values[i] = id
	}

	query := fmt.Sprintf("UPDATE sync_queue SET retry_count = retry_count + 1, last_error = 'Sync failed' WHERE id IN (%s)", strings.Join(placeholders, ","))
	_, err := w.localDB.Exec(query, values...)
	if err != nil {
		log.Printf("Failed to update retry count: %v", err)
	}
}

func (w *LaravelWrapper) markAsSynced(ids []int) {
	if len(ids) == 0 {
		return
	}

	placeholders := make([]string, len(ids))
	values := make([]interface{}, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		values[i] = id
	}

	query := fmt.Sprintf("UPDATE sync_queue SET synced = TRUE WHERE id IN (%s)", strings.Join(placeholders, ","))
	_, err := w.localDB.Exec(query, values...)
	if err != nil {
		log.Printf("Failed to mark records as synced: %v", err)
	}
}

func (w *LaravelWrapper) performInsert(tableName string, data map[string]interface{}) bool {
	if len(data) == 0 {
		log.Printf("No data to insert for table %s", tableName)
		return false
	}

	// Build column names and placeholders
	columns := make([]string, 0, len(data))
	placeholders := make([]string, 0, len(data))
	values := make([]interface{}, 0, len(data))

	for column, value := range data {
		columns = append(columns, fmt.Sprintf("`%s`", column))
		placeholders = append(placeholders, "?")
		values = append(values, value)
	}

	query := fmt.Sprintf("INSERT INTO `%s` (%s) VALUES (%s)", 
		tableName, 
		strings.Join(columns, ", "), 
		strings.Join(placeholders, ", "))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := w.remoteDB.ExecContext(ctx, query, values...)
	if err != nil {
		log.Printf("Failed to insert into %s: %v", tableName, err)
		return false
	}

	log.Printf("Successfully inserted into %s", tableName)
	return true
}

func (w *LaravelWrapper) performUpdate(tableName string, data map[string]interface{}) bool {
	if len(data) == 0 {
		log.Printf("No data to update for table %s", tableName)
		return false
	}

	// Extract ID for WHERE clause
	id, exists := data["id"]
	if !exists {
		log.Printf("No ID found for update in table %s", tableName)
		return false
	}

	// Build SET clause
	setPairs := make([]string, 0, len(data)-1)
	values := make([]interface{}, 0, len(data)-1)

	for column, value := range data {
		if column != "id" {
			setPairs = append(setPairs, fmt.Sprintf("`%s` = ?", column))
			values = append(values, value)
		}
	}

	if len(setPairs) == 0 {
		log.Printf("No columns to update for table %s", tableName)
		return false
	}

	// Add ID to values for WHERE clause
	values = append(values, id)

	query := fmt.Sprintf("UPDATE `%s` SET %s WHERE `id` = ?", 
		tableName, 
		strings.Join(setPairs, ", "))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := w.remoteDB.ExecContext(ctx, query, values...)
	if err != nil {
		log.Printf("Failed to update %s: %v", tableName, err)
		return false
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		log.Printf("No rows updated in %s for ID %v", tableName, id)
		return false
	}

	log.Printf("Successfully updated %s (ID: %v)", tableName, id)
	return true
}

func (w *LaravelWrapper) performDelete(tableName string, data map[string]interface{}) bool {
	// Extract ID for WHERE clause
	id, exists := data["id"]
	if !exists {
		log.Printf("No ID found for delete in table %s", tableName)
		return false
	}

	query := fmt.Sprintf("DELETE FROM `%s` WHERE `id` = ?", tableName)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := w.remoteDB.ExecContext(ctx, query, id)
	if err != nil {
		log.Printf("Failed to delete from %s: %v", tableName, err)
		return false
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		log.Printf("No rows deleted in %s for ID %v", tableName, id)
		return false
	}

	log.Printf("Successfully deleted from %s (ID: %v)", tableName, id)
	return true
}

func (w *LaravelWrapper) shutdown() {
	log.Println("Shutting down services...")

	// Close database connections
	if w.localDB != nil {
		w.localDB.Close()
	}
	if w.remoteDB != nil {
		w.remoteDB.Close()
	}

	// Close WebSocket connections
	w.clientsMutex.Lock()
	for conn := range w.clients {
		conn.Close()
	}
	w.clientsMutex.Unlock()

	// Stop PHP server
	if w.phpServer != nil && w.phpServer.Process != nil {
		log.Println("Stopping PHP server...")
		w.phpServer.Process.Kill()
		w.phpServer.Wait()
	}

	log.Println("Shutdown complete")
}