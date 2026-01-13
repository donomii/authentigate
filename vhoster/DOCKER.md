# Docker Deployment Guide for vhoster

## Quick Start

### Build and Run with Docker Compose

```bash
docker-compose up -d
```

This will:
- Build the vhoster image
- Start the container in development mode on port 3000
- Mount config.json and accessLog

### Build Docker Image Manually

```bash
docker build -t vhoster:latest .
```

### Run Container Manually

Development mode (HTTP on port 3000):
```bash
docker run -d \
  --name vhoster \
  -p 3000:3000 \
  -v $(pwd)/config.json:/app/config.json:ro \
  -v $(pwd)/accessLog:/app/accessLog \
  vhoster:latest ./vhoster -develop
```

Production mode (HTTPS with Let's Encrypt on ports 80/443):
```bash
docker run -d \
  --name vhoster \
  -p 80:80 \
  -p 443:443 \
  -v $(pwd)/config.json:/app/config.json:ro \
  -v $(pwd)/accessLog:/app/accessLog \
  vhoster:latest
```

## Configuration

Edit `config.json` before building/running. The config file defines:
- `Redirects`: Array of host-to-backend mappings
- `Port`: Port to listen on (80 for production)
- `HostNames`: List of domains for Let's Encrypt certificates
- `LogFile`: Access log file path

Example redirect:
```json
{
  "Name": "example",
  "Host": "example.com",
  "To": "http://backend-service:8080",
  "CopyHeaders": ["Content-Type", "Authorization"]
}
```

## Testing

Test that vhoster is working:
```bash
curl -H "Host: donomii.com" http://localhost:3000/
```

## Logs

View logs:
```bash
docker logs -f vhoster
```

View access log:
```bash
tail -f accessLog
```

## Stopping

```bash
docker-compose down
```

Or:
```bash
docker stop vhoster
docker rm vhoster
```
