# Configuration

The server reads `server.yml`, optional drop-in files from `yaml.d/`, and protected `secrets.yml` from its configuration directory. Defaults are built in; a fresh installation does not need a hand-written 1000-line configuration. Use the server's `--help` output for directory flags and the generated configuration for the values initialized on that host.

## Sources and precedence

Configuration is merged in this order: built-in defaults, `server.yml`, `yaml.d/` files in byte-wise filename order, and `secrets.yml`. Command-line flags and runtime environment overrides have higher priority. Maps merge recursively; later scalar values and lists replace earlier values. Invalid values fall back to their documented defaults with a warning rather than preventing startup. Configuration and secret files are private files; do not commit them or include their contents in logs or support messages.

Use `yaml.d/` for optional area files such as networking, email, or logging when the corresponding workflow creates them. Only regular `.yml` and `.yaml` files are loaded. Secrets belong in `secrets.yml`, not in README examples or source control.

## Configuration areas

| YAML area | Purpose |
|------------|---------|
| `server.address`, `server.port`, `server.fqdn`, `server.api_version`, `server.baseurl` | Listener, public host, API version, and base URL resolution |
| `server.mode`, `server.branding` | Runtime mode and application identity |
| `server.database`, `server.cache` | Database and optional cache connections |
| `server.ssl` | TLS and certificate automation |
| `server.trusted_proxies` | Peers allowed to supply trusted reverse-proxy headers |
| `server.rate_limit`, `server.healthz` | Request limits and health aliases |
| `server.security`, `server.token` | Security controls and operator credential |
| `server.logs` | Access, application, audit, and security logging |
| `server.scheduler`, `server.backup`, `server.update` | Scheduled tasks, encrypted backups, retention, and release channel |
| `server.notifications`, `server.contact`, `server.privacy`, `server.csrf` | Email delivery and contact/privacy protections |
| `server.i18n` | Default interface language and the enabled locale set |
| `server.metrics`, `server.geoip`, `server.tor`, `server.i2p` | Metrics, local GeoIP data, and Tor/I2P overlay services |
| `server.compliance`, `server.tracking` | Compliance-driven defaults and privacy choices |
| `web.robots`, `web.security`, `web.cors`, `web.hsts`, `web.csp`, `web.headers`, `web.reports` | Public web policy, security metadata, browser headers, and reporting endpoints |
| `web.ui` | Public interface theme and branding assets |
| `output.color`, `output.emoji` | CLI output preferences |

Configuration is validated per setting. Consult the matching sections of `AI.md` for field-specific accepted values, defaults, and interactions; this table names the complete configuration areas, not a substitute for those detailed constraints.

## Languages

`server.i18n.default_language` sets the language used when a workflow has no request language, such as operator notifications. `server.i18n.available_languages` controls which embedded locales visitors can select and receive through language negotiation.

By default, every embedded locale is enabled. Operators may disable optional locales, but the required English, Spanish, French, German, Italian, and Arabic locales are always enabled. Unsupported codes are ignored with a startup warning, and missing required codes are restored with a warning.

```yaml
server:
  i18n:
    default_language: en
    available_languages: [en, es, fr, de, it, ar, zh, ja]
```

## I2P eepsite

I2P is disabled by default outside containers and enabled by default inside the application container. `server.i2p` controls the private eepsite lifecycle; an installed `i2pd` is preferred, while an operator-provided SAM bridge is used when no local binary is available. If neither provider is available, the server continues without opening the I2P backend listener. Public autodiscovery includes the `.b32.i2p` address only when the service is active and `expose` is enabled.

```yaml
server:
  i2p:
    enabled: false
    expose: false
    binary: ""
    sam_address: 127.0.0.1:7656
    virtual_port: 80
    inbound_length: 3
    outbound_length: 3
    inbound_quantity: 5
    outbound_quantity: 5
    signature_type: 7
    bootstrap_timeout: 300
```

The server creates and protects the I2P destination key under `{data_dir}/i2p/site/site-keys.dat`. Do not copy, publish, or edit that key. `--i2p` enables the service for the current run; `I2P_ENABLED` provides the environment override. The status command and `/server/i2p/status` report lifecycle state without exposing private key material.

## Secrets and credentials

`server.token`, database/cache credentials, SMTP credentials, private keys, installation secrets, encryption keys, and backup passwords are sensitive. Store them in the protected config or secrets files generated for the server. Do not pass passwords on command lines. Configuration and backup passwords use Argon2id; API tokens are stored as SHA-256 hashes. The server redacts secret values from logs and operator-facing config output.

Generated secrets are stored in `secrets.yml` or in the database secret store according to the key's lifecycle. Secret rotation is performed through the documented maintenance commands, not by copying secret values into ordinary configuration examples.

## Environment variables

The following runtime variables are read by the server:

| Variable | Purpose |
|----------|---------|
| `NO_COLOR` | Disable ANSI color output when set to a non-empty value |
| `TERM` | Detect terminal capabilities; `dumb` disables ANSI output |
| `DOMAIN` | Highest-priority public hostname override |
| `MODE` | Select `production` (default) or `development`; aliases are documented in the CLI help |
| `DEBUG` | Enable debug behavior when set to a truthy value; `--debug` takes precedence |
| `SMTP_HOST`, `SMTP_PORT`, `SMTP_USERNAME`, `SMTP_PASSWORD` | SMTP connection overrides |
| `SMTP_FROM_NAME`, `SMTP_FROM_EMAIL`, `SMTP_TLS` | SMTP sender and TLS mode overrides |
| `CONFIG_DIR`, `DATA_DIR`, `LOG_DIR`, `DATABASE_DIR`, `BACKUP_DIR` | Directory overrides where supported |
| `PORT`, `LISTEN`, `APPLICATION_NAME`, `APPLICATION_TAGLINE` | First-run initialization values |
| `DATABASE_DRIVER`, `DATABASE_URL`, `CACHE_URL` | First-run database and cache connection values |
| `I2P_ENABLED` | Override `server.i2p.enabled` for this process |

First-run-only values are written to configuration and ignored on later starts. Credential-bearing connection strings are stored in the protected secrets file. Do not use an environment variable as a substitute for rotating an already-persisted secret.

## Security and public endpoints

`web.security` configures the RFC 9116 repository report URL, expiry, disclosure policy, and whether the project PGP public key is advertised. The secondary security contact is configured under `server.contact.security`; security-report storage/encryption settings are under `server.security`. `web.well_known` controls optional standards endpoints; unknown well-known paths return `404`. Reverse-proxy-derived URLs and origins are trusted only when the immediate peer is allowed by `server.trusted_proxies`.

See [Security](security.md) for the public reporting endpoints and [Integrations](integrations.md) for discovery settings.

`server.notifications.disk_low` controls the scheduled disk-space warning. It defaults to enabled, an 85% used threshold, a five-minute check interval, and a 24-hour repeat interval. The check deduplicates the data, log, and backup paths by filesystem and only emails on a threshold crossing or after the repeat interval.

## Configuration management

The configuration is file-based; the watcher monitors `server.yml`, `yaml.d/`, `secrets.yml`, and the configured assets directory and re-merges changes. Restart the service only when an affected feature requires it. Sensitive operations such as secret rotation and backup restoration use the CLI maintenance flow and audit logging. The CLI uses its own configuration file and options; see [CLI reference](cli.md).
