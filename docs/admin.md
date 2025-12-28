# Admin Panel

The admin panel provides a web-based interface for managing the API server.

## Access

**URL:** `http://localhost:64580/admin`

**Default credentials:** Set during first-run setup wizard

## First-Run Setup

On first launch, access the setup wizard:

1. Navigate to `http://localhost:64580/admin/setup`
2. Create your admin account:
   - Email address
   - Username
   - Password (strong password required)
3. Configure basic settings:
   - Server FQDN
   - Default timezone
   - Email settings (optional)
4. Complete setup

After setup, you'll be redirected to the admin dashboard.

## Dashboard

The admin dashboard provides an overview of:

- **Server Status** - Uptime, mode, version
- **System Metrics** - CPU, memory, disk usage
- **Recent Activity** - Latest requests and errors
- **Quick Actions** - Common admin tasks

## Configuration Management

### Server Settings

Configure server-wide settings:

- **General**
  - Server FQDN
  - Listen address and port
  - Application mode (production/development)
  - Timezone

- **Branding**
  - Site title and tagline
  - Custom logo and favicon
  - Color scheme

### SSL/TLS

Manage SSL certificates:

- **Let's Encrypt**
  - Auto-renewal enabled by default
  - Choose challenge type (HTTP-01, TLS-ALPN-01, DNS-01)
  - Email for expiration notifications

- **Manual Certificates**
  - Upload custom certificates
  - View expiration dates
  - Renewal reminders

### Security

Configure security settings:

- **Rate Limiting**
  - Requests per minute limits
  - IP-based restrictions
  - Whitelist/blacklist management

- **Access Control**
  - GeoIP country blocking
  - IP address filtering
  - API key management

- **Authentication**
  - Session timeout
  - 2FA enforcement
  - Password policy

### Logging

Configure logging behavior:

- **Log Levels** - Debug, Info, Warn, Error
- **Log Formats** - JSON or text
- **Log Rotation** - Daily, weekly, size-based
- **Retention** - How long to keep logs

### API Services

Enable/disable API service categories:

- Text utilities
- Crypto utilities
- DateTime utilities
- Network utilities

Configure service limits:
- Maximum input size
- Maximum batch operations
- Request timeouts

## User Management

!!! note
    User management is only available if multi-user mode is enabled in configuration.

### Admin Accounts

Manage server administrators:

- View all admin accounts
- Create new admin accounts
- Reset admin passwords
- Enable/disable 2FA
- Revoke admin access

### Admin Roles

Admin accounts have full access to:

- Server configuration
- User management (if enabled)
- Backup/restore operations
- System logs and audit trail
- API keys and tokens

## Backup & Restore

### Creating Backups

**Automatic Backups:**

- Scheduled daily at 02:00 (configurable)
- Retained for 7 days by default
- Stored in data directory: `/var/lib/apimgr/api/backups/`

**Manual Backup:**

1. Go to **Admin > Backup & Restore**
2. Click "Create Backup Now"
3. Download the backup file

**Via CLI:**

```bash
api --maintenance backup /path/to/backup.json
```

### Restoring Backups

1. Stop the service: `api --service stop`
2. Restore via CLI: `api --maintenance restore /path/to/backup.json`
3. Start the service: `api --service start`

!!! warning
    Restore operations replace all configuration and data. Ensure you have a current backup before restoring.

## Monitoring

### Health Checks

Monitor service health:

- **Health Endpoint:** `/healthz`
- **Status Check:** `api --status`
- **Dashboard:** Real-time metrics in admin panel

### Logs

View logs in the admin panel:

- **Access Log** - All HTTP requests
- **Server Log** - Application events
- **Error Log** - Error messages and stack traces
- **Audit Log** - Administrative actions
- **Security Log** - Authentication and authorization events

### Metrics

Track performance metrics:

- Request count and latency
- Error rates
- Resource usage (CPU, memory)
- API endpoint usage statistics

## Scheduled Tasks

View and manage scheduled tasks:

| Task | Schedule | Description |
|------|----------|-------------|
| **Backup** | 02:00 daily | Automatic database backup |
| **SSL Renewal** | 03:00 daily | Check and renew SSL certificates |
| **GeoIP Update** | 03:00 Sunday | Update GeoIP database |
| **Session Cleanup** | Every hour | Remove expired sessions |
| **Log Rotation** | Daily | Rotate log files |

## API Keys

Generate and manage API keys for programmatic access:

1. Go to **Admin > API Keys**
2. Click "Generate New Key"
3. Set permissions and expiration
4. Save the key securely (shown only once)

**Using API Keys:**

```bash
curl -H "Authorization: Bearer key_abc123..." \
  http://localhost:64580/api/v1/admin/server/status
```

## Theme Customization

Change the admin panel theme:

- **Dark Mode** - Default, optimized for low-light use
- **Light Mode** - High-contrast for bright environments
- **Auto Mode** - Follows system preference

Theme applies to:
- Admin panel
- Swagger UI
- GraphQL interface
- Documentation site

## Maintenance Mode

Enable maintenance mode to prevent API access:

```bash
# Enable
api --maintenance mode on

# Disable
api --maintenance mode off

# Check status
api --maintenance mode status
```

When enabled, all requests return `503 Service Unavailable` except:
- Health checks (`/healthz`)
- Admin panel (for admins to disable maintenance mode)

## Security Best Practices

### Admin Account Security

1. **Use Strong Passwords** - Minimum 12 characters, mixed case, numbers, symbols
2. **Enable 2FA** - TOTP or WebAuthn for all admin accounts
3. **Limit Admin Accounts** - Only create admins who need access
4. **Regular Audits** - Review admin account access in audit logs
5. **Rotate API Keys** - Regenerate keys periodically

### Server Hardening

1. **Enable SSL** - Always use HTTPS in production
2. **Configure Rate Limiting** - Prevent abuse
3. **Restrict Access** - Use GeoIP blocking if needed
4. **Enable Logging** - Track all administrative actions
5. **Regular Updates** - Check for updates weekly

## Troubleshooting

### Can't Access Admin Panel

**Check service status:**
```bash
api --status
```

**Check logs:**
```bash
tail -f /var/log/apimgr/api/server.log
```

**Reset admin password:**
```bash
api --maintenance setup --reset-admin
```

### SSL Certificate Issues

**Check certificate status:**
```bash
api --maintenance ssl status
```

**Force renewal:**
```bash
api --maintenance ssl renew
```

### Configuration Errors

**Validate configuration:**
```bash
api --validate-config
```

**Reload configuration:**
```bash
kill -HUP $(cat /var/run/apimgr/api.pid)
# or
api --service reload
```

## Next Steps

- [API Reference](api.md)
- [Configuration Guide](configuration.md)
- [Development Guide](development.md)
