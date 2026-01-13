# Docker Deployment Guide for vhoster

## What Was Fixed

### 1. Empty Response Bug
The main issue causing empty responses was fixed in `main.go`:
- Added `return` statements after each relay handler call
- Added 404 fallback response when no route matches
- Properly handles the case where no matching host is found

### 2. Code Modernization
- Updated deprecated `ioutil` calls to use `io` and `os` packages
- Compatible with modern Go versions

## Quick Start

### Build and Run with Docker Compose

```bash
docker-compose up -d
```

This will:
- Build the vhoster image
- Start the container in development mode on port 3000
- Mount config.json as read-only
- Create a volume for logs

### Stop and Remove

```bash
docker-compose down
```

## Manual Docker Commands

### Build Docker Image

```bash
docker build -t vhoster:latest .
```

### Run in Development Mode (HTTP on port 3000)

```bash
docker run -d \
  --name vhoster \
  -p 3000:3000 \
  -v $(pwd)/config.json:/app/config.json:ro \
  -e LOG_FILE=/app/logs/accessLog \
  -v vhoster-logs:/app/logs \
  vhoster:latest ./vhoster -develop
```

### Run in Production Mode (HTTPS with Let's Encrypt)

```bash
docker run -d \
  --name vhoster \
  -p 80:80 \
  -p 443:443 \
  -v $(pwd)/config.json:/app/config.json:ro \
  -e LOG_FILE=/app/logs/accessLog \
  -v vhoster-logs:/app/logs \
  vhoster:latest
```

## Configuration

Edit `config.json` before building/running. The config file defines:
- `Redirects`: Array of host-to-backend mappings
- `Port`: Port to listen on (80 for production, 3000 for dev mode)
- `HostNames`: List of domains for Let's Encrypt certificates
- `LogFile`: Access log file path (overridden by LOG_FILE env var in Docker)

Example redirect configuration:
```json
{
  "Name": "example",
  "Host": "example.com",
  "To": "http://backend-service:8080",
  "CopyHeaders": ["Content-Type", "Authorization"]
}
```

## Testing

Test that vhoster is routing correctly:
```bash
curl -H "Host: donomii.com" http://localhost:3000/
```

If your backend service is not running, you'll see a connection error - this is expected and means vhoster is working correctly.

## Logs

View container logs:
```bash
docker logs -f vhoster
```

View access logs (using docker-compose):
```bash
docker exec vhoster cat /app/logs/accessLog
```

Or monitor live:
```bash
docker exec vhoster tail -f /app/logs/accessLog
```

## Environment Variables

- `LOG_FILE`: Override the log file path (default: `accessLog`)
- `TZ`: Set timezone (default: `UTC`)

## Production Deployment

For production with HTTPS:

1. Ensure your domains in `config.json` HostNames are pointing to your server
2. Remove the `-develop` flag from the command in `docker-compose.yml`
3. Run `docker-compose up -d`

Let's Encrypt will automatically provision SSL certificates for the domains listed in `HostNames`.
