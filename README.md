# Laravel Database Sync Wrapper

This Go application serves as a sync wrapper for Laravel applications using the `obalaweb/laravel-database-sync` package. It provides offline-first database synchronization capabilities.

## Features

- **Offline-First**: Queues database changes locally when remote database is unavailable
- **Real-time Sync**: Automatically syncs changes when connectivity is restored
- **WebSocket Support**: Real-time status updates via WebSocket connections
- **Laravel Integration**: Seamlessly integrates with Laravel applications
- **Retry Logic**: Automatically retries failed sync operations
- **Monitoring**: Built-in endpoints for monitoring sync status

## Setup

### 1. Laravel Application Setup

Install the Laravel package:

```bash
composer require obalaweb/laravel-database-sync
```

Publish the configuration:

```bash
php artisan vendor:publish --provider="LaravelDatabaseSync\DatabaseSyncServiceProvider" --tag="config"
```

Configure your `.env` file:

```env
DATABASE_SYNC_ENABLED=true
DATABASE_SYNC_ENDPOINT=http://localhost:8080/sync-record
DATABASE_SYNC_TIMEOUT=5
```

### 2. Go Application Setup

1. **Install Dependencies**:
   ```bash
   go mod init laravel-sync-wrapper
   go get github.com/go-sql-driver/mysql
   go get github.com/gorilla/mux
   go get github.com/gorilla/websocket
   ```

2. **Create Configuration**:
   Create a `config.json` file in your project root (see config.json artifact).

3. **Build and Run**:
   ```bash
   go build -o laravel-sync-wrapper
   ./laravel-sync-wrapper
   ```

## Configuration

### Database Setup

Create the local database and ensure both local and remote databases exist:

```sql
-- Local database
CREATE DATABASE laravel_local;

-- Remote database (on remote server)
CREATE DATABASE laravel_remote;
```

### Laravel Models

Your Laravel models will automatically sync when using the package. For custom control, implement the `SyncableModelInterface`:

```php
<?php

namespace App\Models;

use Illuminate\Database\Eloquent\Model;
use LaravelDatabaseSync\Contracts\SyncableModelInterface;
use LaravelDatabaseSync\Traits\SyncableModel;

class User extends Model implements SyncableModelInterface
{
    use SyncableModel;

    protected $fillable = [
        'name',
        'email',
        'password',
    ];

    public function getSyncableData(): array
    {
        $data = $this->toArray();
        unset($data['password']);
        return $data;
    }

    public function shouldSync(): bool
    {
        return $this->status === 'active';
    }
}
```

## API Endpoints

### Status and Monitoring

- **GET /status**: Get current sync status
- **GET /health**: Health check for all services
- **GET /queue/status**: Get sync queue status
- **GET /ws**: WebSocket connection for real-time updates

### Sync Operations

- **POST /sync-record**: Laravel sync endpoint (used by Laravel package)
- **POST /sync**: Trigger manual sync
- **POST /queue/clear**: Clear completed sync records
- **POST /queue/retry**: Retry failed sync operations

### WebSocket Events

The WebSocket connection provides real-time updates:

```javascript
const ws = new WebSocket('ws://localhost:8080/ws');

ws.onmessage = (event) => {
    const data = JSON.parse(event.data);
    console.log('Sync event:', data);
};
```

Event types:
- `status`: Connection status updates
- `queue_status`: Queue status updates
- `sync_immediate`: Immediate sync success
- `sync_queued`: Record queued for sync
- `sync_complete`: Batch sync completion
- `connectivity`: Connectivity status change

## Operation Modes

### Online Mode
- Changes are synced immediately to remote database
- Laravel receives immediate success response
- Real-time synchronization

### Offline Mode
- Changes are queued locally
- Laravel receives success response (queued)
- Automatic sync when connectivity restored

## Monitoring

### Status Endpoint Response
```json
{
  "online": true,
  "pending_sync": 0,
  "failed_sync": 0,
  "local_port": 8080,
  "php_port": 8000,
  "timestamp": "2025-01-15T10:30:00Z"
}
```

### Queue Status Response
```json
{
  "pending_count": 5,
  "failed_count": 2,
  "oldest_pending": "2025-01-15T10:25:00Z",
  "timestamp": "2025-01-15T10:30:00Z"
}
```

## Laravel Package Integration

The wrapper expects HTTP POST requests from Laravel with this structure:

```json
{
  "table_name": "users",
  "operation": "INSERT|UPDATE|DELETE",
  "data": {
    "id": 1,
    "name": "John Doe",
    "email": "john@example.com",
    "created_at": "2025-01-15T10:30:00.000000Z",
    "updated_at": "2025-01-15T10:30:00.000000Z"
  }
}
```

## Troubleshooting

### Common Issues

1. **Laravel PHP Server Not Starting**
   - Ensure Laravel directory exists
   - Check PHP is installed and accessible
   - Verify Laravel dependencies are installed

2. **Database Connection Issues**
   - Check database credentials in config.json
   - Ensure databases exist
   - Verify network connectivity

3. **Sync Operations Failing**
   - Check remote database schema matches local
   - Verify table structures are identical
   - Review logs for specific errors

### Logging

All operations are logged to:
- Console output
- `laravel-wrapper.log` file

View logs:
```bash
tail -f laravel-wrapper.log
```

### Laravel Integration Testing

Test the integration:

```bash
# Check if sync service is running
curl -X GET http://localhost:8080/status

# Test sync endpoint
curl -X POST http://localhost:8080/sync-record \
  -H "Content-Type: application/json" \
  -d '{
    "table_name": "users",
    "operation": "INSERT",
    "data": {
      "id": 1,
      "name": "Test User",
      "email": "test@example.com"
    }
  }'
```

## Production Deployment

### Systemd Service

Create `/etc/systemd/system/laravel-sync.service`:

```ini
[Unit]
Description=Laravel Database Sync Wrapper
After=network.target

[Service]
Type=simple
User=www-data
WorkingDirectory=/path/to/your/app
ExecStart=/path/to/your/app/laravel-sync-wrapper
Restart=always
RestartSec=10

[Install]
WantedBy=multi-user.target
```

Enable and start:
```bash
sudo systemctl enable laravel-sync.service
sudo systemctl start laravel-sync.service
```

### Docker Support

```dockerfile
FROM golang:1.21-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go build -o laravel-sync-wrapper

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/

COPY --from=builder /app/laravel-sync-wrapper .
COPY --from=builder /app/config.json .

CMD ["./laravel-sync-wrapper"]
```

## Security Considerations

- Use secure database credentials
- Implement authentication for API endpoints in production
- Use HTTPS for remote database connections
- Restrict network access to sync endpoints
- Monitor sync operations for suspicious activity

## Contributing

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Add tests
5. Submit a pull request