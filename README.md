# API - Universal API Toolkit

[![Build Status](https://github.com/apimgr/api/workflows/Release/badge.svg)](https://github.com/apimgr/api/actions)
[![Docker](https://github.com/apimgr/api/workflows/Docker%20Build/badge.svg)](https://github.com/apimgr/api/actions)
[![Documentation](https://readthedocs.org/projects/apimgr-api/badge/?version=latest)](https://apimgr-api.readthedocs.io)
[![License](https://img.shields.io/github/license/apimgr/api)](LICENSE.md)

A versatile REST API toolkit providing multiple utility services through a unified interface.

## Features

- **Multiple Utility Services**
  - Text manipulation (encoding, hashing, formatting)
  - Cryptographic operations (hashing, password generation, JWT)
  - Date/time utilities (timezone conversion, calculations)
  - Network utilities (IP info, headers, GeoIP)

- **Multiple API Interfaces**
  - REST API at `/api/v1/`
  - Swagger/OpenAPI documentation at `/openapi`
  - GraphQL interface at `/graphql`

- **Production Ready**
  - SSL/TLS with Let's Encrypt integration
  - Dark/light/auto theme support
  - Comprehensive security headers
  - Rate limiting and request tracking
  - Multi-format logging (access, server, error, audit, security)
  - Automated backup and restore
  - Background task scheduler
  - Systemd service integration
  - Docker containerization

## Quick Start

### Docker (Recommended)

```bash
docker run -d \
  --name api \
  -p 64580:80 \
  -v ./data:/data \
  -v ./config:/config \
  ghcr.io/apimgr/api:latest
```

Visit `http://localhost:64580` to access the API.

### Binary Installation

Download the latest release for your platform:

```bash
# Linux AMD64
wget https://github.com/apimgr/api/releases/latest/download/api-linux-amd64
chmod +x api-linux-amd64
sudo mv api-linux-amd64 /usr/local/bin/api

# Install as system service
sudo api --service --install
sudo api --service start
```

### Build from Source

```bash
git clone https://github.com/apimgr/api.git
cd api
make build
```

Binaries will be in `binaries/` directory.

## Usage

### API Examples

**Text Utilities:**
```bash
# Convert to uppercase
curl -X POST http://localhost:64580/api/v1/text/uppercase \
  -H "Content-Type: application/json" \
  -d '{"text":"hello world"}'

# Base64 encode
curl -X POST http://localhost:64580/api/v1/text/base64/encode \
  -H "Content-Type: application/json" \
  -d '{"text":"hello"}'
```

**Crypto Utilities:**
```bash
# Generate SHA-256 hash
curl -X POST http://localhost:64580/api/v1/crypto/hash/sha256 \
  -H "Content-Type: application/json" \
  -d '{"input":"hello world"}'

# Generate random UUID
curl http://localhost:64580/api/v1/crypto/random/uuid
```

**DateTime Utilities:**
```bash
# Get current time
curl http://localhost:64580/api/v1/datetime/now

# Convert timezone
curl -X POST http://localhost:64580/api/v1/datetime/convert \
  -H "Content-Type: application/json" \
  -d '{"timestamp":"2025-01-15T10:30:45Z","from_timezone":"UTC","to_timezone":"America/New_York"}'
```

**Network Utilities:**
```bash
# Get client IP
curl http://localhost:64580/api/v1/network/ip

# Parse user agent
curl http://localhost:64580/api/v1/network/useragent
```

### CLI Commands

```bash
# Show version
api --version
api -v

# Show help
api --help
api -h

# Check service status
api --status

# Start in development mode
api --mode development --debug

# Service management
api --service --install
api --service start
api --service stop
api --service restart

# Backup and restore
api --maintenance backup /path/to/backup.json
api --maintenance restore /path/to/backup.json

# Check for updates
api --update check
```

## Documentation

**Complete documentation available at:** [apimgr-api.readthedocs.io](https://apimgr-api.readthedocs.io)

- [Installation Guide](https://apimgr-api.readthedocs.io/en/latest/installation/)
- [Configuration Reference](https://apimgr-api.readthedocs.io/en/latest/configuration/)
- [API Reference](https://apimgr-api.readthedocs.io/en/latest/api/)
- [CLI Reference](https://apimgr-api.readthedocs.io/en/latest/cli/)
- [Admin Panel Guide](https://apimgr-api.readthedocs.io/en/latest/admin/)
- [Development Guide](https://apimgr-api.readthedocs.io/en/latest/development/)

## Interactive Documentation

- **Swagger UI:** `http://localhost:64580/openapi`
- **GraphQL:** `http://localhost:64580/graphql`

## Configuration

Create `server.yml`:

```yaml
server:
  address: "0.0.0.0"
  port: "64580"
  mode: production

  branding:
    title: "API Toolkit"
    tagline: "Universal API Services"

  rate_limit:
    enabled: true
    requests: 100
    window: 60

  schedule:
    enabled: true

api:
  services:
    text:
      enabled: true
    crypto:
      enabled: true
    datetime:
      enabled: true
    network:
      enabled: true

  limits:
    max_input_size: 1048576  # 1MB
    max_batch_operations: 100
```

## Development

```bash
# Quick development build
make dev

# Full build (all 8 platforms)
make build

# Run tests
make test

# Build Docker image
make docker

# Show all targets
make help
```

## Supported Platforms

- Linux (amd64, arm64)
- macOS (amd64, arm64)
- Windows (amd64, arm64)
- FreeBSD (amd64, arm64)

All binaries are statically compiled with no external dependencies.

## Architecture

- **Language:** Go 1.24+
- **Web Framework:** Chi router
- **Template Engine:** Go html/template
- **Database:** SQLite (default), PostgreSQL/MySQL (cluster mode)
- **Caching:** In-memory (default), Valkey/Redis (cluster mode)
- **Deployment:** Docker, systemd, standalone binary

## License

MIT License - See [LICENSE.md](LICENSE.md) for details.

## Contributing

Contributions are welcome! Please see the [Development Guide](https://apimgr-api.readthedocs.io/en/latest/development/) for details.

## Support

- **Documentation:** [apimgr-api.readthedocs.io](https://apimgr-api.readthedocs.io)
- **Issues:** [GitHub Issues](https://github.com/apimgr/api/issues)
- **Source Code:** [GitHub](https://github.com/apimgr/api)

---

**Author:** [casjay](https://github.com/casjay)
