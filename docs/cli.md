# CLI Reference

Complete command-line interface reference for the API server binary.

## Usage

```bash
api [options]
```

## Global Options

### --help, -h

Show help message and exit.

```bash
api --help
```

### --version, -v

Show version information.

```bash
api --version
```

**Output:**
```
API v1.0.0 (built: 2025-01-15T10:00:00Z)
Go version: go1.24.0
OS/Arch: linux/amd64
```

### --status

Check if the service is running and display status.

```bash
api --status
```

**Output:**
```
✅ Service is running
   Port: 64580
   Config: /etc/apimgr/api/server.yml
```

## Server Configuration

### --mode

Set application mode.

```bash
api --mode production
api --mode development
```

**Values:**
- `production` - Production mode (strict security, minimal logging)
- `development` - Development mode (debug endpoints, verbose logging)

### --config

Specify configuration directory.

```bash
api --config /path/to/config
```

**Default:**
- Root: `/etc/apimgr/api/`
- User: `~/.config/apimgr/api/`

### --data

Specify data directory.

```bash
api --data /path/to/data
```

**Default:**
- Root: `/var/lib/apimgr/api/`
- User: `~/.local/share/apimgr/api/`

### --log

Specify log directory.

```bash
api --log /path/to/logs
```

**Default:**
- Root: `/var/log/apimgr/api/`
- User: `~/.local/share/apimgr/api/logs/`

### --pid

Specify PID file path.

```bash
api --pid /var/run/api.pid
```

**Default:**
- Root: `/var/run/apimgr/api.pid`
- User: `~/.local/share/apimgr/api/api.pid`

### --address

Set listen address.

```bash
api --address 0.0.0.0
api --address 192.168.1.100
```

**Default:** `0.0.0.0` (all interfaces)

### --port

Set listen port.

```bash
api --port 8080
```

**Default:** `64580`

### --debug

Enable debug mode (verbose logging, debug endpoints).

```bash
api --debug
```

**Debug endpoints:**
- `/debug/pprof` - Go profiling
- `/debug/vars` - Exported variables
- `/debug/config` - Current configuration
- `/debug/routes` - Registered routes

### --daemon

Run in background (daemonize).

```bash
api --daemon
```

!!! note
    Not needed when using `--service start`. Service managers handle daemonization automatically.

## Service Management

### --service install

Install as system service.

```bash
sudo api --service install
```

Creates systemd service file and enables auto-start.

### --service start

Start the service.

```bash
sudo api --service start
```

### --service stop

Stop the service.

```bash
sudo api --service stop
```

### --service restart

Restart the service.

```bash
sudo api --service restart
```

### --service reload

Reload configuration without restarting.

```bash
sudo api --service reload
```

Sends `SIGHUP` signal to reload config.

### --service uninstall

Uninstall the system service.

```bash
sudo api --service uninstall
```

### --service --help

Show service command help.

```bash
api --service --help
```

## Maintenance Commands

### --maintenance backup

Create a backup.

```bash
api --maintenance backup /path/to/backup.json
```

**Backs up:**
- Configuration file
- Database
- SSL certificates
- Scheduler state

### --maintenance restore

Restore from backup.

```bash
api --maintenance restore /path/to/backup.json
```

!!! warning
    Service must be stopped before restoring.

### --maintenance update

Update server configuration.

```bash
api --maintenance update setting_name value
```

### --maintenance mode

Change application mode.

```bash
api --maintenance mode production
api --maintenance mode development
```

### --maintenance setup

Run first-time setup wizard.

```bash
api --maintenance setup
```

Creates admin account and initializes configuration.

## Update Commands

### --update check

Check for available updates.

```bash
api --update check
```

**Output:**
```
Current version: 1.0.0
Latest version: 1.1.0
Update available: https://github.com/apimgr/api/releases/tag/v1.1.0
```

### --update yes

Download and install updates.

```bash
sudo api --update yes
```

!!! warning
    Requires root/admin privileges.

### --update branch

Switch update channel.

```bash
api --update branch stable
api --update branch beta
api --update branch daily
```

**Channels:**
- `stable` - Stable releases only (recommended)
- `beta` - Beta releases
- `daily` - Daily builds (development only)

## Environment Variables

All flags can be set via environment variables:

```bash
export API_MODE=production
export API_CONFIG=/path/to/config
export API_DATA=/path/to/data
export API_LOG=/path/to/logs
export API_ADDRESS=0.0.0.0
export API_PORT=8080
export API_DEBUG=true
```

## Signals

The server responds to Unix signals:

| Signal | Action |
|--------|--------|
| `SIGTERM` | Graceful shutdown |
| `SIGINT` | Graceful shutdown (Ctrl+C) |
| `SIGHUP` | Reload configuration |

```bash
# Reload config
kill -HUP $(cat /var/run/apimgr/api.pid)

# Graceful shutdown
kill -TERM $(cat /var/run/apimgr/api.pid)
```

## Examples

### Development Server

```bash
api --mode development --debug --port 8080
```

### Production Server

```bash
api --mode production \
  --config /etc/api \
  --data /var/lib/api \
  --log /var/log/api
```

### Docker Container

```bash
docker run -d \
  -p 64580:80 \
  -e API_MODE=production \
  -e API_DEBUG=false \
  -v $(pwd)/data:/var/lib/apimgr/api \
  ghcr.io/apimgr/api:latest
```

## Next Steps

- [API Reference](api.md)
- [Admin Panel](admin.md)
- [Configuration Guide](configuration.md)
