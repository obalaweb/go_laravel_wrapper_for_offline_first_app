# Laravel Offline Wrapper

A Go-based wrapper that serves Laravel applications locally and synchronizes database changes with a remote database when internet connectivity is restored. Perfect for offline-first Laravel applications on client machines.

## Features

- **Offline-First**: Serve Laravel applications locally without internet
- **Auto-Sync**: Automatically synchronizes database changes when online
- **System Service**: Runs as Windows Service or Linux systemd service
- **Auto-Start**: Starts automatically when the computer boots
- **Low Resource Usage**: Optimized for low-budget client machines
- **Real-time Updates**: WebSocket support for real-time connectivity status
- **Queue Management**: Intelligent sync queue with operation batching
- **Laravel Integration**: Easy-to-use traits and commands for Laravel

## Quick Start

### For Windows

1. **Prerequisites**:
   - Install Go from https://golang.org/dl/
   - Install XAMPP or standalone MySQL
   - Install PHP (usually comes with XAMPP)

2. **Build and Install**:
   ```batch
   # Build the application
   build.bat
   
   # Edit config.json with your database settings
   notepad config.json
   
   # Install as Windows Service (Run as Administrator)
   install-service.bat
   ```

3. **Your Laravel app is now running at**: http://localhost:8080

### For Linux (Ubuntu/Debian)

1. **Prerequisites**:
   ```bash
   # Install Go
   sudo apt update
   sudo apt install golang-go mysql-server php php-mysql php-cli composer
   
   # Start MySQL
   sudo systemctl start mysql
   sudo systemctl enable mysql
   ```

2. **Build and Install**:
   ```bash
   # Make build script executable
   chmod +x build.sh
   
   # Build the application
   ./build.sh
   
   # Edit config.json with your database settings
   nano config.json
   
   # Install as systemd service
   sudo ./install-service.sh
   ```

3. **Your Laravel app is now running at**: http://localhost:8080

## Configuration

Edit `config.json` before installing the service:

```json
{
  "laravel_path": "./laravel-app",
  "local_port": 8080,
  "php_port": 8000,
  "local_db": {
    "host": "localhost",
    "port": 3306,
    "database": "laravel_local",
    "username": "root",
    "password": "your_password"
  },
  "remote_db": {
    "host": "your-remote-server.com",
    "port": 3306,
    "database": "laravel_remote",
    "username": "remote_user",
    "password": "remote_password"
  },
  "sync_interval_seconds": 30,
  "connectivity_check_interval_seconds": 10
}
```

## Laravel Integration

### 1. Add the sync migration:

```bash
cd laravel-app
php artisan make:migration create_sync_queue_table
```

Copy the migration content from `create_sync_queue_table.php`.

### 2. Add the SyncableTrait to your models:

```php
<?php

namespace App\Models;

use App\Traits\SyncableTrait;
use Illuminate\Database\Eloquent\Model;

class User extends Model
{
    use SyncableTrait;
    
    // Optional: Exclude sensitive fields from sync
    protected $syncExclude = ['password', 'remember_token'];
}
```

### 3. Run migrations:

```bash
php artisan migrate
```

## Service Management

### Windows

```batch
# Check service status
sc query LaravelWrapper

# Start service
sc start LaravelWrapper

# Stop service
sc stop LaravelWrapper

# Restart service
sc stop LaravelWrapper && timeout /t 3 && sc start LaravelWrapper

# Uninstall service
uninstall-service.bat
```

### Linux

```bash
# Check service status
sudo systemctl status laravel-wrapper

# Start service
sudo systemctl start laravel-wrapper

# Stop service
sudo systemctl stop laravel-wrapper

# Restart service
sudo systemctl restart laravel-wrapper

# View logs
sudo journalctl -u laravel-wrapper -f

# Uninstall service
sudo systemctl stop laravel-wrapper
sudo systemctl disable laravel-wrapper
sudo rm /etc/systemd/system/laravel-wrapper.service
sudo systemctl daemon-reload
```

## Client Machine Setup

### Minimum Requirements
- **RAM**: 2GB (4GB recommended)
- **Storage**: 1GB free space
- **OS**: Windows 7+ or Linux (Ubuntu 18.04+)
- **Network**: Intermittent internet connection

### Typical Setup Process

1. **Download and extract** the application files to a permanent location (e.g., `C:\laravel-wrapper` or `/opt/laravel-wrapper`)

2. **Install dependencies**:
   - **Windows**: XAMPP (includes PHP, MySQL, Apache)
   - **Linux**: `sudo apt install mysql-server php php-mysql composer`

3. **Configure databases**:
   - Create local MySQL database
   - Configure remote database connection
   - Update `config.json` with credentials

4. **Install your Laravel application**:
   ```bash
   # Place your Laravel app in the configured directory
   cp -r your-laravel-app ./laravel-app
   
   # Or create new Laravel app
   composer create-project laravel/laravel laravel-app
   ```

5. **Build and install service**:
   - Run build script
   - Edit configuration
   - Install as system service

6. **Test the setup**:
   - Access http://localhost:8080
   - Check service status
   - Verify database connectivity

## Troubleshooting

### Common Issues

1. **Service won't start**:
   - Check if MySQL is running
   - Verify Laravel app path in config
   - Check logs: `laravel-wrapper.log`

2. **Database connection failed**:
   - Verify database credentials
   - Check if MySQL service is running
   - Test connection manually

3. **PHP errors**:
   - Check if PHP is installed and in PATH
   - Verify Laravel app is properly configured
   - Check Laravel logs in `laravel-app/storage/logs/`

4. **Port conflicts**:
   - Change ports in config.json
   - Check if other services use ports 8080/8000

### Log Files

- **Windows**: `laravel-wrapper.log` in installation directory
- **Linux**: `sudo journalctl -u laravel-wrapper -f`
- **Laravel**: `laravel-app/storage/logs/laravel.log`

### Performance Tips for Low-Budget Machines

1. **Memory optimization**:
   - Set `memory_limit` in PHP to 256M
   - Use SQLite for local database if needed
   - Reduce sync frequency for very slow connections

2. **CPU optimization**:
   - Service runs with CPU limits (50% max)
   - PHP processes are automatically managed
   - Sync operations are throttled

3. **Storage optimization**:
   - Regular cleanup of old sync records
   - Log rotation configured
   - Temporary files cleaned automatically

## File Structure

```
laravel-wrapper/
├── main.go                     # Main application
├── go.mod                      # Go dependencies
├── config.json                 # Configuration
├── build.sh / build.bat        # Build scripts# Laravel Offline Wrapper

A Go-based wrapper that serves Laravel applications locally and synchronizes database changes with a remote database when internet connectivity is restored. Perfect for offline-first Laravel applications.

## Features

- **Offline-First**: Serve Laravel applications locally without internet
- **Auto-Sync**: Automatically synchronizes database changes when online
- **Real-time Updates**: WebSocket support for real-time connectivity status
- **Queue Management**: Intelligent sync queue with operation batching
- **Laravel Integration**: Easy-to-use traits and commands for Laravel
- **Docker Support**: Full Docker and Docker Compose configuration
- **Health Monitoring**: Built-in status endpoints and health checks

## Quick Start

### 1. Setup

```bash
# Clone or create the project
git clone <repository-url>
cd laravel-wrapper

# Install Go dependencies
go mod download

# Setup Laravel application
make setup-laravel

# Configure database settings
cp config.json.example config.json
# Edit config.json with your database settings
```

### 2. Configuration

Edit `config.json`:

```json
{
  "laravel_path": "./laravel-app",
  "local_port": 8080,
  "php_port": 8000,
  "local_db": {
    "host": "localhost",
    "port": 3306,
    "database": "laravel_local",
    "username": "root",
    "password": ""
  },
  "remote_db": {
    "host": "your-remote-db.com",
    "port": 3306,
    "database": "laravel_remote",
    "username": "remote_user",
    "password": "remote_password"
  },
  "sync_interval_seconds": 30,
  "connectivity_check_interval_seconds": 10
}
```

### 3. Laravel Integration

#### Add the sync migration:

```bash
cd laravel-app
php artisan make:migration create_sync_queue_table
```

Copy the migration content from the provided migration file.

#### Add the SyncableTrait to your models:

```php
<?php

namespace App\Models;

use App\Traits\SyncableTrait;
use Illuminate\Database\Eloquent\Model;

class User extends Model
{
    use SyncableTrait;
    
    // Optional: Exclude fields from sync
    protected $syncExclude = ['password', 'remember_token'];
}
```

#### Register the sync command:

Add to `app/Console/Kernel.php`:

```php
protected $commands = [
    \App\Console\Commands\SyncCommand::class,
];
```

### 4. Run the Application

```bash
# Build and run
make run

# Or with Docker
make docker-run

# Check status
make status

# Force sync
make sync
```

## Architecture

### Components

1. **Go Wrapper**: Main application that manages:
   - Laravel PHP server proxy
   - Database connectivity monitoring
   - Sync queue management
   - WebSocket connections

2. **Laravel Application**: Your standard Laravel app with:
   - SyncableTrait for automatic sync queue population
   - Sync commands for manual operations
   - Migration for sync queue table

3. **Database Layer**:
   - Local MySQL database for offline operations
   - Remote MySQL database for synchronized data
   - Sync queue table for tracking changes

### Sync Process

1. **Offline Mode**: All database changes are captured by the SyncableTrait
2. **Queue Storage**: Changes are stored in the local `sync_queue` table
3. **Connectivity Check**: Regular checks for remote database availability
4. **Sync Execution**: When online, queued changes are applied to remote database
5. **Queue Cleanup**: Successfully synced records are marked as completed

## API Endpoints

### Status Endpoint
```
GET /status
```

Returns current status including connectivity and pending sync count.

### Sync Endpoint
```
POST /sync
```

Manually triggers a sync operation.

### WebSocket Endpoint
```
WS /ws
```

Real-time updates for connectivity changes and sync status.

## Laravel Commands

### Check Sync Status
```bash
php artisan sync:status
```

### Force Sync
```bash
php artisan sync:status --force
```

## Docker Usage

### Build and Run
```bash
# Build the Docker image
make docker-build

# Run with Docker Compose
make docker-run

# View logs
make docker-logs

# Stop services
make docker-stop
```

### Services Included

- **laravel-wrapper**: Main Go application
- **mysql**: Local MySQL database
- **phpmyadmin**: Web interface for database management (http://localhost:8081)

## Development

### Project Structure
```
laravel-wrapper/
├── main.go                 # Main Go application
├── config.json            # Configuration file
├── go.mod                  # Go module dependencies
├── Dockerfile             # Docker configuration
├── docker-compose.yml     # Docker Compose setup
├── Makefile              # Build and run commands
├── laravel-app/          # Laravel application directory
│   ├── app/
│   │   ├── Traits/
│   │   │   └── SyncableTrait.php
│   │   └── Console/
│   │       └── Commands/
│   │           └── SyncCommand.php
│   └── database/
│       └── migrations/
└── README.md
```

### Adding New Features

1. **Extend the SyncableTrait**: Add custom sync logic for specific models
2. **Modify Sync Operations**: Customize how different operations are handled
3. **Add Webhooks**: Implement webhook support for real-time notifications
4. **Enhance Monitoring**: Add metrics and logging for better observability

## Configuration Options

| Option | Description | Default |
|--------|-------------|---------|
| `laravel_path` | Path to Laravel application | `./laravel-app` |
| `local_port` | Port for the wrapper server | `8080` |
| `php_port` | Port for PHP built-in server | `8000` |
| `local_db` | Local database configuration | - |
| `remote_db` | Remote database configuration | - |
| `sync_interval_seconds` | How often to sync when online | `30` |
| `connectivity_check_interval_seconds` | How often to check connectivity | `10` |

## Troubleshooting

### Common Issues

1. **PHP Server Won't Start**
   - Check if Laravel application exists at configured path
   - Verify PHP is installed and accessible
   - Check port availability

2. **Database Connection Failed**
   - Verify database credentials
   - Check if MySQL service is running
   - Test connectivity manually

3. **Sync Not Working**
   - Check remote database connectivity
   - Verify sync queue table exists
   - Check Laravel application logs

### Debug Mode

Run with verbose logging:
```bash
./laravel-wrapper -v
```

### Health Checks

```bash
# Check wrapper status
curl http://localhost:8080/status

# Check PHP server
curl http://localhost:8000

# Check database connectivity
mysql -h localhost -u root -p laravel_local
```

## Contributing

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Add tests if applicable
5. Submit a pull request

## License

This project is licensed under the MIT License. See LICENSE file for details.

## Support

For issues and questions:
- Create an issue in the GitHub repository
- Check the troubleshooting section
- Review the logs for error messages

---

**Note**: This wrapper is designed for development and testing environments. For production use, consider additional security measures and monitoring solutions.
