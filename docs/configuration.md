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
      - "/api/v1"

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


# SSL settings
export API_SSL_ENABLED=true
export API_SSL_LETSENCRYPT_EMAIL=ssl@example.com

# Database
export API_DATABASE_DRIVER=postgres
export API_DATABASE_HOST=localhost
export API_DATABASE_PORT=5432

# Scheduled backup encryption password (overrides
# server.backup.encryption_password for unattended scheduled backup runs
# only - the scheduler cannot prompt interactively the way the CLI/WebUI
# restore flow can)
export API_BACKUP_PASSWORD=changeme
```

!!! note
    SMTP/email settings are not yet exposed via `server.yml` or
    environment variables in this build - there is no `SMTP_HOST`,
    `SMTP_PORT`, `SMTP_USERNAME`, `SMTP_PASSWORD`, `SMTP_TLS`,
    `SMTP_FROM_NAME`, or `SMTP_FROM_EMAIL` override wired up yet, so
    email-dependent notification features stay disabled.

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

There is no web-based setup wizard or admin panel. On first run, the binary
itself:

1. Generates the project secrets (`installation_secret`, `cookie_signing_key`,
   `csrf_token_secret`, `server.security.encryption_key`)
2. Writes a default `server.yml` with sane defaults
3. Initializes the database (`server.db`)
4. Sets up SSL (if configured)

Run `api --maintenance setup` to (re-)run this first-run initialization from
the CLI. See [CLI Reference](cli.md#administration) for the full
administration surface.

## Backup Configuration

```bash
# Backup current config
api --maintenance backup /path/to/backup.json

# Restore from backup
api --maintenance restore /path/to/backup.json
```

## Next Steps

- [Explore the API](api.md)
- [Security](security.md)
- [CLI reference](cli.md)
