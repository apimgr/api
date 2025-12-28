# Configuration

The API server can be configured via configuration file, environment variables, or command-line flags.

## Configuration Priority

Settings are applied in this order (highest priority first):

1. **Command-line flags** - `--port 8080`
2. **Environment variables** - `API_PORT=8080`
3. **Configuration file** - `server.yml`
4. **Default values** - Built-in defaults

## Configuration File

### Location

Default configuration file locations:

| Context | Path |
|---------|------|
| **Root user** | `/etc/apimgr/api/server.yml` |
| **Regular user** | `~/.config/apimgr/api/server.yml` |
| **Custom** | Specified with `--config` flag |

### Example Configuration

Create `server.yml`:

```yaml
server:
  # Listen address (0.0.0.0 for all interfaces)
  address: "0.0.0.0"

  # Port number
  port: "64580"

  # Fully qualified domain name
  fqdn: "api.example.com"

  # Application mode: production or development
  mode: production

  # Branding
  branding:
    title: "API Toolkit"
    tagline: "Universal API Services"

  # Admin account
  admin:
    email: "admin@example.com"
    username: "admin"
    password: "" # Set on first run via setup wizard
    token: "" # Generated automatically

  # SSL/TLS configuration
  ssl:
    enabled: false
    cert_path: ""
    letsencrypt:
      enabled: false
      email: "ssl@example.com"
      challenge: "http-01" # http-01, tls-alpn-01, or dns-01

  # Database configuration
  database:
    driver: "sqlite" # sqlite, postgres, mysql

  # Rate limiting
  rate_limit:
    enabled: true
    requests: 100 # requests per window
    window: 60 # window in seconds

  # Logging
  logs:
    level: "info" # debug, info, warn, error
    access:
      filename: "access.log"
      format: "combined"
      rotate: "daily"
      keep: "7"
    server:
      filename: "server.log"
      format: "json"
      rotate: "daily"
      keep: "30"
    error:
      filename: "error.log"
      format: "json"
      rotate: "daily"
      keep: "30"
    audit:
      filename: "audit.log"
      format: "json"
      rotate: "daily"
      keep: "90"
    security:
      enabled: true
      filename: "security.log"
      format: "json"
      rotate: "daily"
      keep: "90"

  # Scheduler
  schedule:
    enabled: true

# Web interface configuration
web:
  # CORS configuration
  cors: "*"

  # UI settings
  ui:
    theme: "dark" # dark, light, auto

  # Robots.txt
  robots:
    allow:
      - "/"
    deny:
      - "/admin"
      - "/api/v1/admin"

  # Security settings
  security:
    email: "security@example.com"

# API-specific settings
api:
  # Enable/disable service categories
  services:
    text:
      enabled: true
    crypto:
      enabled: true
    datetime:
      enabled: true
    network:
      enabled: true

  # Service limits
  limits:
    max_input_size: 1048576  # 1MB
    max_batch_operations: 100
```

## Environment Variables

All configuration options can be set via environment variables using the `API_` prefix:

```bash
# Server settings
export API_PORT=8080
export API_ADDRESS=0.0.0.0
export API_MODE=production
export API_FQDN=api.example.com

# Admin settings
export API_ADMIN_EMAIL=admin@example.com
export API_ADMIN_USERNAME=admin

# SSL settings
export API_SSL_ENABLED=true
export API_SSL_LETSENCRYPT_EMAIL=ssl@example.com

# Database
export API_DATABASE_DRIVER=postgres
export API_DATABASE_HOST=localhost
export API_DATABASE_PORT=5432
```

## Command-Line Flags

Override any setting with command-line flags:

```bash
# Server configuration
api --address 0.0.0.0 --port 8080

# Application mode
api --mode development

# Custom paths
api --config /path/to/config \
    --data /path/to/data \
    --log /path/to/logs

# Enable debug mode
api --debug
```

## Boolean Values

Boolean settings accept multiple formats (case-insensitive):

**Truthy values:**
`1`, `yes`, `true`, `on`, `enable`, `enabled`, `y`, `t`, `yep`, `yup`, `yeah`, `aye`, `si`, `oui`

**Falsy values:**
`0`, `no`, `false`, `off`, `disable`, `disabled`, `n`, `f`, `nope`, `nah`, `nay`, `nein`, `non`

## Application Modes

### Production Mode

Optimized for production use:

- Strict security headers
- Compressed responses
- Minimal logging
- No debug endpoints
- HTTPS enforcement (if SSL enabled)

```bash
api --mode production
```

### Development Mode

Enhanced debugging and development features:

- Relaxed CSP headers
- Verbose logging
- Debug endpoints enabled (`/debug/pprof`, `/debug/vars`)
- Hot reload support
- Detailed error messages

```bash
api --mode development
```

## SSL/TLS Configuration

### Manual Certificates

```yaml
server:
  ssl:
    enabled: true
    cert_path: "/path/to/certs"
```

Place your certificates:
- `/path/to/certs/cert.pem`
- `/path/to/certs/key.pem`

### Let's Encrypt

```yaml
server:
  ssl:
    enabled: true
    letsencrypt:
      enabled: true
      email: "ssl@example.com"
      challenge: "http-01"
```

Supported challenges:
- `http-01` - HTTP challenge (port 80 required)
- `tls-alpn-01` - TLS-ALPN challenge (port 443 required)
- `dns-01` - DNS challenge (requires DNS provider API access)

## Logging Configuration

### Log Levels

- `debug` - Detailed debugging information
- `info` - General informational messages
- `warn` - Warning messages
- `error` - Error messages only

### Log Formats

- `combined` - Apache combined format (access logs)
- `json` - Structured JSON (server/error/audit logs)

### Log Rotation

- `daily` - Rotate logs daily
- `weekly` - Rotate logs weekly
- `monthly` - Rotate logs monthly
- `size:10M` - Rotate when file reaches 10MB

## First-Run Setup

On first run, the setup wizard will:

1. Generate admin credentials
2. Create default configuration
3. Initialize database
4. Set up SSL (if configured)

Access the setup wizard at: `http://localhost:64580/admin/setup`

## Backup Configuration

```bash
# Backup current config
api --maintenance backup /path/to/backup.json

# Restore from backup
api --maintenance restore /path/to/backup.json
```

## Next Steps

- [Explore the API](api.md)
- [Set up the admin panel](admin.md)
- [CLI reference](cli.md)
