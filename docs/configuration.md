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
| `server.debug` | Explicitly enabled diagnostics, profiling, and verbose request/query/cache logging |
| `server.ssl` | TLS and certificate automation |
| `server.trusted_proxies` | Peers allowed to supply trusted reverse-proxy headers |
| `server.rate_limit`, `server.healthz` | Request limits and health aliases |
| `server.security`, `server.token` | Security controls and operator credential |
| `server.logs` | Access, application, audit, and security logging |
| `server.scheduler`, `server.backup`, `server.restore`, `server.update` | Scheduled tasks, encrypted backups, restore size limits, retention, and release channel |
| `server.notifications`, `server.contact`, `server.privacy`, `server.csrf` | Email delivery and contact/privacy protections |
| `server.i18n` | Default interface language and the enabled locale set |
| `server.metrics`, `server.geoip`, `server.tor`, `server.i2p` | Metrics, local GeoIP data, and Tor/I2P overlay services |
| `server.compliance`, `server.tracking` | Compliance-driven defaults and privacy choices |
| `web.robots`, `web.security`, `web.cors`, `web.hsts`, `web.csp`, `web.headers`, `web.reports` | Public web policy, security metadata, browser headers, and reporting endpoints |
| `web.ui` | Public interface theme and branding assets |
| `output.color`, `output.emoji` | CLI output preferences |
| `data.security_dir`, `data.cve`, `data.trivy` | External security-database storage, NVD CVE API updates, and optional Trivy OCI data |

Configuration is validated per setting. Consult the matching sections of `AI.md` for field-specific accepted values, defaults, and interactions; this table names the complete configuration areas, not a substitute for those detailed constraints.

## TLS and Let's Encrypt

The server supports HTTP-01, TLS-ALPN-01, and DNS-01 ACME challenges. HTTP-01 is answered by the server's HTTP listener and TLS-ALPN-01 by its HTTPS listener; the server does not open separate challenge listeners. Use DNS-01 when those public ports are unavailable or a wildcard certificate is needed.

DNS-01 uses the provider name and environment-variable credential names defined by lego. All lego DNS providers are available; no provider allowlist is configured. Store credentials as one flat map under `server.ssl.letsencrypt.dns_credentials` in `secrets.yml`. Values may be entered as plaintext and are rewritten atomically as AES-256-GCM `enc:v1:` values using `server.security.encryption_key`; credentials are decrypted only in memory, never logged, and shown masked in configuration views. Provider construction is checked at startup and before each certificate request. A provider or credential error fails that DNS-01 request and is logged without trying another ACME challenge. See [lego's DNS provider documentation](https://go-acme.github.io/lego/dns/) for the provider's exact environment-variable names.

```yaml
server:
  ssl:
    letsencrypt:
      enabled: true
      email: operator@example.com
      challenge: dns-01
      dns_provider: cloudflare
      dns_credentials:
        CF_DNS_API_TOKEN: "replace-with-provider-token"
```

For RFC 2136, select `rfc2136` (also `dnsupdate`) and provide `DNSUPDATE_NAMESERVER`; the older `RFC2136_*` variable spelling is accepted. TSIG and GSS-TSIG credential values receive the same encryption and redaction handling.

## External security data

Security databases are downloaded outside the binary under `data.security_dir` (default: the data directory's `security/` subdirectory). The `cve_update` task uses the NVD CVE 2.0 API, stores its CVE objects in `cve/nvd.json`, and requests only modifications since the last successful update after the initial full download. `data.cve.api_key` is optional and is stored in `secrets.yml`; without a key, updates follow NVD's lower request rate. Trivy data is disabled by default and can be enabled with `data.trivy.enabled`; `data.trivy.source` selects its OCI image reference. The OCI database layer is extracted into `trivy/` only when its metadata `NextUpdate` has passed.

The built-in `firehol_level1` IPv4 list is always included when blocklists are enabled. Operators may add HTTPS-only IP or domain sources; there is no built-in domain feed. Validated lists are stored under `security/blocklists/`, reloaded after updates, and stale checks are visible through health endpoints.

```yaml
data:
  cve:
    source: https://services.nvd.nist.gov/rest/json/cves/2.0
    filter_by_cpe: false
    api_key: ""
  trivy:
    enabled: false
    source: ghcr.io/aquasecurity/trivy-db:2
server:
  security:
    blocklists:
      enabled: true
      extra_sources: []
```

## Debug diagnostics

Debug endpoints and detailed diagnostics require the explicit `--debug` flag or `DEBUG=true`. The selected `debug` mode enables the debug flag by default, while an explicit `--debug=false` or `DEBUG=false` keeps the endpoints off. Every `/debug/*` route also requires `Authorization: Bearer <server.token>`; the debug flag never bypasses authentication.

```yaml
server:
  debug:
    pprof: true
    log_queries: true
    log_cache: true
    log_bodies: false
    max_body_log_size: 10KB
    block_profile_rate: 1
    mutex_profile_fraction: 1
    runtime_endpoints: true
```

`pprof` controls `/debug/pprof/*`. `runtime_endpoints` controls `/debug/vars`, `/debug/config`, `/debug/routes`, `/debug/cache`, `/debug/db`, `/debug/scheduler`, `/debug/memory`, `/debug/goroutines`, and `/debug/error500`. Request-body logging is off by default; enabled body previews are bounded and credential fields are redacted. Non-JSON body content is omitted from previews.

## Branding and page content

Branding and public-page extensions belong in `{config_dir}/yaml.d/branding.yml`. The server sanitizes every configured fragment before rendering it. `header` and `footer` accept HTML only; `about`, `privacy`, and `terms` accept Markdown, HTML, or plain text. Language maps use the request language, then `default`. Oversized or invalid entries are ignored and the built-in content is used.

```yaml
server:
  branding:
    content:
      header:
        format: html
        body: '<p>Example organization</p>'
      about:
        format: md
        body:
          default: 'Maintained by the Example team.'
          it: 'Gestito dal team Example.'
      footer:
        format: html
        body: '<p>Powered by <strong>Example</strong></p>'
```

`header` is appended inside the public header, `about` and `privacy` extend their built-in pages, `terms` replaces the built-in terms, and `footer` replaces the optional branding block above the standard footer. Scripts, forms, frames, unsafe URLs, and links to `/server/contact` are removed. The terms API returns the configured source text and its format.

## Restore limits

`server.restore` bounds backup restore and import work. Archive size, total expanded size, and single-file size accept byte counts with `K`, `M`, or `G` suffixes (or `KB`/`KiB`, `MB`/`MiB`, `GB`/`GiB`). Invalid settings fall back to the documented defaults with a startup warning.

```yaml
server:
  restore:
    max_archive_size: 1G
    max_expanded_size: 4G
    max_files: 10000
    max_file_size: 512M
    max_ratio: 100
```

The defaults cap compressed archives at 1 GiB, expanded content at 4 GiB, files at 10,000 total and 512 MiB each, and the expanded-to-compressed ratio at 100:1. Restored files use safe permissions; symlinks, special files, unsafe paths, and unsafe mode bits are rejected.

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
