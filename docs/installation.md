# Installation

This guide covers different installation methods for the API Toolkit.

## Docker (Recommended)

The fastest way to get started is using Docker.

### Docker Run

```bash
docker run -d \
  --name api \
  -p 64580:80 \
  -v ./data:/var/lib/apimgr/api \
  -v ./config:/etc/apimgr/api \
  ghcr.io/apimgr/api:latest
```

### Docker Compose

Create `docker-compose.yml`:

```yaml
version: '3.8'

services:
  api:
    image: ghcr.io/apimgr/api:latest
    container_name: api
    ports:
      - "64580:80"
    volumes:
      - ./data:/var/lib/apimgr/api
      - ./config:/etc/apimgr/api
      - ./logs:/var/log/apimgr/api
    environment:
      - TZ=America/New_York
    restart: unless-stopped
```

Start the service:

```bash
docker-compose up -d
```

## Binary Installation

### Download Pre-built Binary

Download the latest release for your platform:

```bash
# Linux AMD64
wget https://github.com/apimgr/api/releases/latest/download/api-linux-amd64

# Linux ARM64
wget https://github.com/apimgr/api/releases/latest/download/api-linux-arm64

# macOS AMD64
wget https://github.com/apimgr/api/releases/latest/download/api-darwin-amd64

# macOS ARM64 (Apple Silicon)
wget https://github.com/apimgr/api/releases/latest/download/api-darwin-arm64

# Windows AMD64
wget https://github.com/apimgr/api/releases/latest/download/api-windows-amd64.exe
```

Make it executable (Linux/macOS):

```bash
chmod +x api-*
sudo mv api-* /usr/local/bin/api
```

### Build from Source

Requirements:
- Go 1.24 or later (for building only, not required for running)
- Git

```bash
# Clone the repository
git clone https://github.com/apimgr/api.git
cd api

# Build
make build

# The binary will be in binaries/api
sudo mv binaries/api /usr/local/bin/
```

## System Service Installation

### Linux (systemd)

Install as a system service:

```bash
# Install the service
sudo api --service install

# Enable start on boot
sudo systemctl enable api

# Start the service
sudo api --service start

# Check status
sudo api --status
```

### macOS (launchd)

Create `/Library/LaunchDaemons/com.apimgr.api.plist`:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.apimgr.api</string>
    <key>ProgramArguments</key>
    <array>
        <string>/usr/local/bin/api</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
</dict>
</plist>
```

Load the service:

```bash
sudo launchctl load /Library/LaunchDaemons/com.apimgr.api.plist
```

## Verification

After installation, verify the service is running:

```bash
# Check status
api --status

# Check version
api --version

# Access web interface
curl http://localhost:64580/healthz
```

Expected response:

```json
{
  "status": "ok",
  "uptime": 3600,
  "version": "1.0.0"
}
```

## Next Steps

- [Configure the server](configuration.md)
- [Explore the API](api.md)
- [Set up the admin panel](admin.md)

## Ports

Default ports:

| Service | Port | Description |
|---------|------|-------------|
| HTTP Server | 64580 | Main API and web interface |
| HTTPS Server | 64543 | Secure API (if SSL enabled) |

!!! note
    The default external port is `64580` (Docker maps this to internal port `80`). You can change this in your Docker configuration or via the `--port` flag.
