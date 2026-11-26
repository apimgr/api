# Configuration

The server reads `server.yml`, optional drop-in files from `yaml.d/`, and protected `secrets.yml` from its configuration directory. Defaults are built in; a fresh installation does not need a hand-written 1000-line configuration. Use the server's `--help` output for directory flags and the generated configuration for the values initialized on that host.

## Sources and precedence

Configuration is merged in this order: built-in defaults, `server.yml`, `yaml.d/` files in byte-wise filename order, and `secrets.yml`. Command-line flags and runtime environment overrides have higher priority. Maps merge recursively; later scalar values and lists replace earlier values. Invalid values fall back to their documented defaults with a warning rather than preventing startup. Configuration and secret files are private files; do not commit them or include their contents in logs or support messages.

Use `yaml.d/` for optional area files. Project help and report-form settings belong in `yaml.d/pages.yml`; only regular `.yml` and `.yaml` files are loaded. Secrets belong in `secrets.yml`, not in README examples or source control.

## Configuration areas

| YAML area | Purpose |
|------------|---------|
| `server.address`, `server.port`, `server.fqdn`, `server.api_version`, `server.baseurl` | Listener, public host, API version, and base URL resolution |
| `server.mode`, `server.branding` | Runtime mode and application identity |
| `server.limits` | HTTP request body and connection timeouts |
| `server.database`, `server.cache` | Database and optional cache connections |
| `server.debug` | Explicitly enabled diagnostics, profiling, and verbose request/query/cache logging |
| `server.ssl` | TLS and certificate automation |
| `server.trusted_proxies` | Peers allowed to supply trusted reverse-proxy headers |
| `server.rate_limit`, `server.healthz` | Request limits and health aliases |
| `server.security`, `server.token` | Security controls and operator credential |
| `server.tokens` | Anonymous owner-token limits and lifecycle settings |
| `server.logs` | Access, application, audit, and security logging |
| `server.scheduler`, `server.backup`, `server.restore`, `server.update` | Scheduled tasks, encrypted backups, restore size limits, retention, and release channel |
| `server.notifications`, `server.contact`, `server.privacy`, `server.csrf` | Email delivery and contact/privacy protections |
| `server.pages` | Project-defined page/help content settings |
| `server.i18n` | Default interface language and the enabled locale set |
| `server.metrics`, `server.geoip`, `server.tor`, `server.i2p` | Metrics, local GeoIP data, and Tor/I2P overlay services |
| `server.compliance`, `server.tracking` | Compliance-driven defaults and privacy choices |
| `web.announcements` | Scheduled public site banners and visitor dismissal controls |
| `web.robots`, `web.llms`, `web.security`, `web.cors`, `web.hsts`, `web.csp`, `web.headers`, `web.reports` | Public discovery documents, security metadata, browser headers, and reporting endpoints |
| `web.ui` | Public interface theme and branding assets |
| `output.color`, `output.emoji` | CLI output preferences |
| `cli.binary_download` | Operator-provisioned CLI download directory and authentication policy |
| `data.security_dir`, `data.cve`, `data.trivy` | External security-database storage, NVD CVE API updates, and optional Trivy OCI data |

Configuration is validated per setting. Consult the matching sections of `AI.md` for field-specific accepted values, defaults, and interactions; this table names the complete configuration areas, not a substitute for those detailed constraints.

## Consent and announcements

The first-visit consent banner is rendered by the server and its Accept and Decline forms work without JavaScript. Choices are stored in the browser's `cookie_consent` cookie; the `/server/preferences` form provides the granular preference controls. The banner is omitted from that page so its single-form contract remains intact. The configured policy link defaults to `/server/privacy`.

Announcements are rendered before the page header from `web.announcements`. Only enabled messages with a supported type and an active RFC 3339 start/end window are shown. Dismissible messages store their IDs in the `dismissed_announcements` cookie, and Reset on the preferences page clears it.

```yaml
web:
  announcements:
    enabled: true
    messages:
      - id: scheduled-maintenance-2026
        type: warning
        title: Scheduled maintenance
        message: Service may be unavailable during the maintenance window.
        start: "2026-12-01T02:00:00Z"
        end: "2026-12-01T04:00:00Z"
        dismissible: true
```

## TLS and Let's Encrypt

The server supports HTTP-01, TLS-ALPN-01, and DNS-01 ACME challenges. HTTP-01 is answered by the server's HTTP listener and TLS-ALPN-01 by its HTTPS listener; the server does not open separate challenge listeners. Managed Let's Encrypt certificates renew when 30 days or fewer remain; this window is fixed. Use DNS-01 when those public ports are unavailable or a wildcard certificate is needed.

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

## Local cache for remote libSQL

`server.database.cache` configures an optional local SQLite cache for a remote libSQL/Turso database. It is disabled by default. When enabled, reads check the local cache first, writes go to the remote database and then update the cache, and background synchronization keeps cached data fresh. The local cache is a separate SQLite file; SQLite itself does not use this cache option.

```yaml
server:
  database:
    cache:
      enabled: true
      path: "${DATA_DIR}/db/cache.db"
      sync_interval: 30s
      offline_mode: true
      max_age: 1h
```

The example follows AI.md PART 10; only `enabled: false` is explicitly identified there as the default, so the cache remains opt-in.

## Debug diagnostics

Debug endpoints and detailed diagnostics require the explicit `--debug` flag or `DEBUG=true`. The selected `debug` mode enables the debug flag by default, while an explicit `--debug=false` or `DEBUG=false` keeps the endpoints off. They exist only under root `/debug/*`; there are no `/api/debug/*` or versioned debug aliases. Every `/debug/*` route also requires `Authorization: Bearer <server.token>`; the debug flag never bypasses authentication.

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

Branding and public-page extensions belong in `{config_dir}/yaml.d/branding.yml`. The server sanitizes every configured fragment before rendering it. `header` and `footer` accept HTML only; `about`, `privacy`, and `terms` accept Markdown, HTML, or plain text. Language maps use the request language, then `default`. Oversized or invalid entries are ignored and the built-in content is used. `server.privacy.retention.period` accepts one string for all languages or a language map with an optional `default` value; an empty or unset value uses the translated built-in statement.

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

The same `branding.yml` file accepts `server.seo` metadata. Robots directives default to `index,follow` only for registered public pages; internal and management routes default to `noindex,nofollow`. Verification codes are validated before the server renders their tags. The sitemap and feeds are always available.

```yaml
server:
  seo:
    keywords: [api, developer tools]
    author: Example Organization
    og_image: https://example.org/social-card.png
    twitter_handle: '@example'
    title_separator: ' - '
    canonical: ''
    noindex: false
    opengraph:
      enabled: true
      type: website
      site_name: ''
    twitter:
      enabled: true
      card: summary_large_image
      creator: ''
    sitemap:
      max_urls: 50000
      include_images: false
    feed:
      max_items: 50
      title: ''
```

## Contact destinations and webhooks

Contact destinations include `admin`, `security`, `abuse`, `general`, and `dpo`. The `admin` destination is the internal notification recipient; the other roles serve their documented public or submission workflows. Supported recipient roles accept named webhook URLs; DPO additionally accepts a `name`. Empty role destinations follow the fallback rules in AI.md PART 12; the admin address is never exposed publicly.

Built-in webhook transport names are `telegram`, `discord`, `slack`, `mattermost`, `pushover`, `gotify`, and `generic`. The open map also accepts project-added adapters. URLs and generated per-webhook signing secrets are sensitive and belong in `secrets.yml`; never publish them in ordinary config examples.

```yaml
server:
  contact:
    admin:
      email: operator@example.com
      webhooks:
        generic: "https://hooks.example.net/replace-me"
    security:
      email: security@example.com
```

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

The server reads the runtime and directory variables below. Database/cache connection variables and initialization variables are described separately because they seed configuration only on first run.

| Variable | Purpose |
|----------|---------|
| `NO_COLOR` | Disable ANSI color output when set to a non-empty value |
| `TERM` | Detect terminal capabilities; `dumb` disables ANSI output |
| `DOMAIN` | Highest-priority public hostname override |
| `HOSTNAME` | Hostname candidate after `os.Hostname()` during public URL resolution |
| `MODE` | Select `production` (default) or `development`; aliases are documented in the CLI help |
| `DEBUG` | Enable debug behavior when set to a truthy value; `--debug` takes precedence |
| `SMTP_HOST`, `SMTP_PORT`, `SMTP_USERNAME`, `SMTP_PASSWORD` | SMTP connection overrides |
| `SMTP_FROM_NAME`, `SMTP_FROM_EMAIL`, `SMTP_TLS` | SMTP sender and TLS mode overrides |
| `CONFIG_DIR`, `DATA_DIR`, `LOG_DIR`, `DATABASE_DIR`, `BACKUP_DIR` | Directory overrides applied at every startup where supported |
| `I2P_ENABLED` | Override `server.i2p.enabled` for this process |
| `LC_ALL`, `LANG` | Select the CLI language when no explicit language flag or CLI config value is set |
| `API_TOKEN` | Client token fallback, after `--token` and before `cli.yml` |

Directory variables are applied on every startup; command-line directory flags take precedence over them. The following values initialize configuration only on the first run.

| Variable | Purpose |
|----------|---------|
| `PORT` | Initial server port |
| `LISTEN` | Initial listen address |
| `APPLICATION_NAME` | Initial application title |
| `APPLICATION_TAGLINE` | Initial application description |
| `DATABASE_DRIVER` | Initial database driver when `DATABASE_URL` is not set (`sqlite` or `libsql`, including documented aliases) |
| `DATABASE_URL` | First-run database connection string; saved to `secrets.yml` when it contains credentials, otherwise `server.yml` |
| `CACHE_URL` | First-run Valkey/Redis connection string; saved to `secrets.yml` when it contains credentials, otherwise `server.yml` |

First-run-only values are written to configuration and ignored on later starts. Credential-bearing connection strings are stored in the protected secrets file. Do not use an environment variable as a substitute for rotating an already-persisted secret.

## Security and public endpoints

`web.security` configures the RFC 9116 repository report URL, expiry, disclosure policy, and whether the project PGP public key is advertised. The secondary security contact is configured under `server.contact.security`; security-report storage/encryption settings are under `server.security`. The root-owned well-known namespace serves the implemented security, LLM-discovery, PGP-key, and active ACME challenge entries; unknown paths return `404`. Reverse-proxy-derived URLs and origins are trusted only when the immediate peer is allowed by `server.trusted_proxies`.

See [Security](security.md) for the public reporting endpoints and [Integrations](integrations.md) for discovery settings.

`server.notifications.disk_low` controls the scheduled disk-space warning. It defaults to enabled, an 85% used threshold, a five-minute check interval, and a 24-hour repeat interval. The check deduplicates the data, log, and backup paths by filesystem and emails on a threshold crossing or after the repeat interval when SMTP is configured. Its `server.notifications.email.events.disk_low` toggle defaults to enabled.

`server.notifications.database_error` controls the database-connection warning. It probes both read and write handles every minute by default and triggers after three consecutive failures; repeated alerts are limited to once per 24 hours while failures continue. The first successful probe logs recovery and resets the failure count without sending another email. Error text has credentials removed. Its `server.notifications.email.events.database_error` toggle defaults to enabled. The example settings are:

```yaml
server:
  notifications:
    database_error:
      enabled: true
      failures: 3
      check_interval: 1m
      repeat: 24h
```

## Configuration management

The configuration is file-based; the watcher monitors `server.yml`, `yaml.d/`, `secrets.yml`, and the configured assets directory and re-merges changes. Restart the service only when an affected feature requires it. Sensitive operations such as secret rotation and backup restoration use the CLI maintenance flow and audit logging. The CLI uses its own configuration file and options; see [CLI reference](cli.md).
## Complete setting key index

This index lists the stable YAML keys exposed by the server configuration model. It excludes user-account settings because this product has no accounts. Map keys use `{key}` and list items use `[*]`; replace those markers with the application-defined map key or list item. The `data` and `server.pages` maps also accept the project-specific entries described above. Webhook transport names are open maps; built-in names are listed in [Contact destinations and webhooks](#contact-destinations-and-webhooks).

The first-run configuration is generated with the effective built-in defaults for the current platform and environment. That generated value is the default for each setting; runtime-derived values (such as the hostname, random port, or container-dependent service defaults) vary by installation. Invalid or unsupported values warn and fall back to the generated default. The preceding sections document the accepted values and behavior for constrained settings. Secret-valued keys are kept in `secrets.yml`, not ordinary `server.yml`.

| YAML key | Value type |
|---|---|
| `cli.binary_download.dir` | `string` |
| `cli.binary_download.require_auth` | `bool` |
| `data.cve.api_key` | `secret string` |
| `data.cve.filter_by_cpe` | `bool` |
| `data.cve.source` | `string` |
| `data.security_dir` | `string` |
| `data.trivy.enabled` | `bool` |
| `data.trivy.source` | `string` |
| `output.color` | `bool` |
| `output.emoji` | `bool` |
| `server.address` | `string` |
| `server.api_version` | `string` |
| `server.backup.encryption.enabled` | `bool` |
| `server.backup.encryption_password` | `string` |
| `server.backup.retention.keep_monthly` | `int` |
| `server.backup.retention.keep_weekly` | `int` |
| `server.backup.retention.keep_yearly` | `int` |
| `server.backup.retention.max_backups` | `int` |
| `server.backup.retention.max_total_size` | `string` |
| `server.baseurl` | `string` |
| `server.branding.content.{key}.body` | `interface{}` |
| `server.branding.content.{key}.format` | `string` |
| `server.branding.description` | `string` |
| `server.branding.favicon` | `string` |
| `server.branding.logo` | `string` |
| `server.branding.tagline` | `string` |
| `server.branding.title` | `string` |
| `server.cache.db` | `int` |
| `server.cache.host` | `string` |
| `server.cache.min_idle` | `int` |
| `server.cache.password` | `string` |
| `server.cache.pool_size` | `int` |
| `server.cache.port` | `int` |
| `server.cache.prefix` | `string` |
| `server.cache.static` | `bool` |
| `server.cache.templates` | `bool` |
| `server.cache.timeout` | `string` |
| `server.cache.tls` | `bool` |
| `server.cache.tls_ca` | `string` |
| `server.cache.tls_cert` | `string` |
| `server.cache.tls_key` | `string` |
| `server.cache.tls_skip_verify` | `bool` |
| `server.cache.ttl` | `string` |
| `server.cache.type` | `string` |
| `server.cache.url` | `string` |
| `server.cache.username` | `string` |
| `server.compliance.enabled` | `bool` |
| `server.contact.abuse.email` | `string` |
| `server.contact.abuse.webhooks.{transport}` | `string` |
| `server.contact.admin.email` | `string` |
| `server.contact.admin.webhooks.{transport}` | `string` |
| `server.contact.dpo.name` | `string` |
| `server.contact.dpo.email` | `string` |
| `server.contact.dpo.webhooks.{transport}` | `string` |
| `server.contact.general.email` | `string` |
| `server.contact.general.webhooks.{transport}` | `string` |
| `server.contact.security.email` | `string` |
| `server.contact.security.webhooks.{transport}` | `string` |
| `server.csrf.cookie_name` | `string` |
| `server.csrf.enabled` | `bool` |
| `server.csrf.exempt_paths[*]` | `[]string` |
| `server.csrf.header_name` | `string` |
| `server.csrf.secure` | `string` |
| `server.csrf.token_length` | `int` |
| `server.database.cache.enabled` | `bool` |
| `server.database.cache.max_age` | `string` |
| `server.database.cache.offline_mode` | `bool` |
| `server.database.cache.path` | `string` |
| `server.database.cache.sync_interval` | `string` |
| `server.database.driver` | `string` |
| `server.database.pool.max_idle` | `int` |
| `server.database.pool.max_idle_time` | `string` |
| `server.database.pool.max_lifetime` | `string` |
| `server.database.pool.max_open` | `int` |
| `server.database.token` | `string` |
| `server.database.url` | `string` |
| `server.debug.block_profile_rate` | `int` |
| `server.debug.log_bodies` | `bool` |
| `server.debug.log_cache` | `bool` |
| `server.debug.log_queries` | `bool` |
| `server.debug.max_body_log_size` | `string` |
| `server.debug.mutex_profile_fraction` | `int` |
| `server.debug.pprof` | `bool` |
| `server.debug.runtime_endpoints` | `bool` |
| `server.fqdn` | `string` |
| `server.geoip.allow_countries[*]` | `[]string` |
| `server.geoip.deny_countries[*]` | `[]string` |
| `server.geoip.enabled` | `bool` |
| `server.geoip.presets.{key}` | `map value` |
| `server.healthz.root.enabled` | `bool` |
| `server.i18n.available_languages[*]` | `[]string` |
| `server.i18n.default_language` | `string` |
| `server.i2p.binary` | `string` |
| `server.i2p.bootstrap_timeout` | `int` |
| `server.i2p.enabled` | `bool` |
| `server.i2p.expose` | `bool` |
| `server.i2p.inbound_length` | `int` |
| `server.i2p.inbound_quantity` | `int` |
| `server.i2p.outbound_length` | `int` |
| `server.i2p.outbound_quantity` | `int` |
| `server.i2p.sam_address` | `string` |
| `server.i2p.signature_type` | `int` |
| `server.i2p.virtual_port` | `int` |
| `server.limits.idle_timeout` | `string` |
| `server.limits.max_body_size` | `string` |
| `server.limits.read_timeout` | `string` |
| `server.limits.write_timeout` | `string` |
| `server.logs.access.custom` | `string` |
| `server.logs.access.filename` | `string` |
| `server.logs.access.format` | `string` |
| `server.logs.access.keep` | `string` |
| `server.logs.access.rotate` | `string` |
| `server.logs.app.custom` | `string` |
| `server.logs.app.filename` | `string` |
| `server.logs.app.format` | `string` |
| `server.logs.app.keep` | `string` |
| `server.logs.app.rotate` | `string` |
| `server.logs.audit.compress` | `bool` |
| `server.logs.audit.enabled` | `bool` |
| `server.logs.audit.filename` | `string` |
| `server.logs.audit.format` | `string` |
| `server.logs.audit.keep` | `string` |
| `server.logs.audit.rotate` | `string` |
| `server.logs.auth.custom` | `string` |
| `server.logs.auth.filename` | `string` |
| `server.logs.auth.format` | `string` |
| `server.logs.auth.keep` | `string` |
| `server.logs.auth.rotate` | `string` |
| `server.logs.debug.custom` | `string` |
| `server.logs.debug.enabled` | `bool` |
| `server.logs.debug.filename` | `string` |
| `server.logs.debug.format` | `string` |
| `server.logs.debug.keep` | `string` |
| `server.logs.debug.rotate` | `string` |
| `server.logs.error.custom` | `string` |
| `server.logs.error.filename` | `string` |
| `server.logs.error.format` | `string` |
| `server.logs.error.keep` | `string` |
| `server.logs.error.rotate` | `string` |
| `server.logs.level` | `string` |
| `server.logs.security.custom` | `string` |
| `server.logs.security.filename` | `string` |
| `server.logs.security.format` | `string` |
| `server.logs.security.keep` | `string` |
| `server.logs.security.rotate` | `string` |
| `server.logs.server.custom` | `string` |
| `server.logs.server.filename` | `string` |
| `server.logs.server.format` | `string` |
| `server.logs.server.keep` | `string` |
| `server.logs.server.rotate` | `string` |
| `server.metrics.auth.allow_unauthenticated` | `bool` |
| `server.metrics.auth.tokens.grafana` | `string` |
| `server.metrics.auth.tokens.loki` | `string` |
| `server.metrics.auth.tokens.prometheus` | `string` |
| `server.metrics.duration_buckets[*]` | `[]float64` |
| `server.metrics.enabled` | `bool` |
| `server.metrics.include_runtime` | `bool` |
| `server.metrics.include_system` | `bool` |
| `server.metrics.loki.max_age` | `string` |
| `server.metrics.loki.max_entries` | `int` |
| `server.metrics.root.enabled` | `bool` |
| `server.metrics.size_buckets[*]` | `[]float64` |
| `server.mode` | `string` |
| `server.notifications.database_error.check_interval` | `string` |
| `server.notifications.database_error.enabled` | `bool` |
| `server.notifications.database_error.failures` | `int` |
| `server.notifications.database_error.repeat` | `string` |
| `server.notifications.disk_low.check_interval` | `string` |
| `server.notifications.disk_low.enabled` | `bool` |
| `server.notifications.disk_low.repeat` | `string` |
| `server.notifications.disk_low.threshold_percent` | `int` |
| `server.notifications.email.events.backup_complete` | `bool` |
| `server.notifications.email.events.backup_failed` | `bool` |
| `server.notifications.email.events.database_error` | `bool` |
| `server.notifications.email.events.disk_low` | `bool` |
| `server.notifications.email.events.scheduler_error` | `bool` |
| `server.notifications.email.events.security_alert` | `bool` |
| `server.notifications.email.events.shutdown` | `bool` |
| `server.notifications.email.events.ssl_expiring` | `bool` |
| `server.notifications.email.events.ssl_renewal_failed` | `bool` |
| `server.notifications.email.events.ssl_renewed` | `bool` |
| `server.notifications.email.events.startup` | `bool` |
| `server.notifications.email.events.update_available` | `bool` |
| `server.notifications.email.events.update_failed` | `bool` |
| `server.notifications.email.events.update_installed` | `bool` |
| `server.notifications.email.events.update_staged` | `bool` |
| `server.notifications.email.from.email` | `string` |
| `server.notifications.email.from.name` | `string` |
| `server.notifications.email.reply_to` | `string` |
| `server.notifications.email.smtp.host` | `string` |
| `server.notifications.email.smtp.password` | `string` |
| `server.notifications.email.smtp.port` | `int` |
| `server.notifications.email.smtp.tls` | `string` |
| `server.notifications.email.smtp.username` | `string` |
| `server.notifications.webui.duration` | `int` |
| `server.notifications.webui.position` | `string` |
| `server.pages.{key}` | `dynamic page setting` |
| `server.port` | `string` |
| `server.privacy.consent.buttons.accept` | `string` |
| `server.privacy.consent.buttons.decline` | `string` |
| `server.privacy.consent.default_enabled` | `bool` |
| `server.privacy.consent.message` | `string` |
| `server.privacy.consent.message_if_sold` | `string` |
| `server.privacy.consent.policy.text` | `string` |
| `server.privacy.consent.policy.url` | `string` |
| `server.privacy.consent.position` | `string` |
| `server.privacy.consent.preferences_text` | `string` |
| `server.privacy.consent.show_preferences` | `bool` |
| `server.privacy.consent.show_until_acknowledged` | `bool` |
| `web.announcements.enabled` | `bool` |
| `web.announcements.messages[].id` | `string` (1–64 letters, digits, underscores, or hyphens) |
| `web.announcements.messages[].type` | `info`, `warning`, `error`, or `success` |
| `web.announcements.messages[].title` | `string` |
| `web.announcements.messages[].message` | `string` |
| `web.announcements.messages[].start`, `web.announcements.messages[].end` | RFC 3339 timestamp, optional |
| `web.announcements.messages[].dismissible` | `bool` |
| `server.privacy.cookies.analytics.description` | `string` |
| `server.privacy.cookies.analytics.description_suffix_not_sold` | `string` |
| `server.privacy.cookies.analytics.description_suffix_sold` | `string` |
| `server.privacy.cookies.analytics.enabled` | `bool` |
| `server.privacy.cookies.essential.description` | `string` |
| `server.privacy.cookies.essential.enabled` | `bool` |
| `server.privacy.cookies.preferences.description` | `string` |
| `server.privacy.cookies.preferences.enabled` | `bool` |
| `server.privacy.data.sharing.condition` | `string` |
| `server.privacy.data.sharing.data` | `string` |
| `server.privacy.data.sharing.when` | `string` |
| `server.privacy.data.sold` | `bool` |
| `server.privacy.data.stored_on_server` | `bool` |
| `server.privacy.retention.deletion_available` | `bool` |
| `server.privacy.retention.export_available` | `bool` |
| `server.privacy.retention.period` | `string` or language map (`en`, `es`, `fr`, `de`, `it`, `ar`, `zh`, `ja`, optional `default`) |
| `server.privacy.third_party.services.data_sent` | `string` |
| `server.privacy.third_party.services.name` | `string` |
| `server.privacy.third_party.services.policy_url` | `string` |
| `server.privacy.third_party.services.purpose` | `string` |
| `server.rate_limit.auth_failures.requests` | `int` |
| `server.rate_limit.auth_failures.window` | `int` |
| `server.rate_limit.global_burst` | `int` |
| `server.rate_limit.health.requests` | `int` |
| `server.rate_limit.health.window` | `int` |
| `server.rate_limit.read.requests` | `int` |
| `server.rate_limit.read.window` | `int` |
| `server.rate_limit.strict.requests` | `int` |
| `server.rate_limit.strict.window` | `int` |
| `server.rate_limit.write.requests` | `int` |
| `server.rate_limit.write.window` | `int` |
| `server.restore.max_archive_size` | `string` |
| `server.restore.max_expanded_size` | `string` |
| `server.restore.max_file_size` | `string` |
| `server.restore.max_files` | `int` |
| `server.restore.max_ratio` | `int64` |
| `server.scheduler.backoff` | `string` |
| `server.scheduler.catch_up_window` | `string` |
| `server.scheduler.max_retries` | `int` |
| `server.scheduler.retry_delay` | `string` |
| `server.scheduler.tasks.{key}.retry_delay` | `string` |
| `server.scheduler.tasks.{key}.schedule` | `string` |
| `server.scheduler.timezone` | `string` |
| `server.security.abuse_detection.auto_alert` | `bool` |
| `server.security.abuse_detection.auto_block_ip` | `bool` |
| `server.security.abuse_detection.request_flood.block_duration` | `time.Duration` |
| `server.security.abuse_detection.request_flood.multiplier` | `int` |
| `server.security.blocklists.enabled` | `bool` |
| `server.security.blocklists.extra_sources.enabled` | `bool` |
| `server.security.blocklists.extra_sources.id` | `string` |
| `server.security.blocklists.extra_sources.type` | `string` |
| `server.security.blocklists.extra_sources.url` | `string` |
| `server.security.encryption_key` | `string` |
| `server.security.encryption_key_version` | `int` |
| `server.security.reports.retention_days` | `int` |
| `server.ssl.cert` | `string` |
| `server.ssl.cert_path` | `string` |
| `server.ssl.enabled` | `bool` |
| `server.ssl.key` | `string` |
| `server.ssl.letsencrypt.challenge` | `string` |
| `server.ssl.letsencrypt.dns_credentials.{key}` | `map value` |
| `server.ssl.letsencrypt.dns_provider` | `string` |
| `server.ssl.letsencrypt.email` | `string` |
| `server.ssl.letsencrypt.enabled` | `bool` |
| `server.token` | `string` |
| `server.tokens.cookie_max_age` | `string` |
| `server.tokens.idle_purge_days` | `int` |
| `server.tokens.max_linked` | `int` |
| `server.tor.bandwidth_burst` | `string` |
| `server.tor.bandwidth_rate` | `string` |
| `server.tor.binary` | `string` |
| `server.tor.bootstrap_timeout` | `int` |
| `server.tor.circuit_timeout` | `int` |
| `server.tor.close_circuit_on_stream_limit` | `bool` |
| `server.tor.expose` | `bool` |
| `server.tor.max_circuits` | `int` |
| `server.tor.max_monthly_bandwidth` | `string` |
| `server.tor.max_streams_per_circuit` | `int` |
| `server.tor.num_intro_points` | `int` |
| `server.tor.safe_logging` | `bool` |
| `server.tor.use_network` | `bool` |
| `server.tor.virtual_port` | `int` |
| `server.tracking.id` | `string` |
| `server.tracking.type` | `string` |
| `server.tracking.url` | `string` |
| `server.trusted_proxies.additional[*]` | `[]string` |
| `server.update.auto_install` | `bool` |
| `server.update.branch` | `string` |
| `server.update.cli_min_version` | `string` |
| `server.update.defer_days` | `int` |
| `web.cors.allowed_origins[*]` | `[]string` |
| `web.csp.connect_src_extra` | `string` |
| `web.csp.connect_src_override` | `string` |
| `web.csp.default_src_override` | `string` |
| `web.csp.enabled` | `bool` |
| `web.csp.font_src_extra` | `string` |
| `web.csp.font_src_override` | `string` |
| `web.csp.form_action_extra` | `string` |
| `web.csp.form_action_override` | `string` |
| `web.csp.frame_src_extra` | `string` |
| `web.csp.frame_src_override` | `string` |
| `web.csp.img_src_extra` | `string` |
| `web.csp.img_src_override` | `string` |
| `web.csp.mode` | `string` |
| `web.csp.reports_enabled` | `bool` |
| `web.csp.reports_sample_rate` | `float64` |
| `web.csp.script_src_extra` | `string` |
| `web.csp.script_src_override` | `string` |
| `web.csp.style_src_extra` | `string` |
| `web.csp.style_src_override` | `string` |
| `web.headers.clear_site_data.execution_contexts` | `bool` |
| `web.headers.clear_site_data.on_consent_withdrawal` | `bool` |
| `web.headers.clear_site_data.on_token_revocation` | `bool` |
| `web.headers.coep` | `string` |
| `web.headers.coop` | `string` |
| `web.headers.corp` | `string` |
| `web.headers.cross_domain_policies` | `string` |
| `web.headers.dns_prefetch_control` | `string` |
| `web.headers.honor_dnt` | `bool` |
| `web.headers.honor_sec_gpc` | `bool` |
| `web.headers.nel.enabled` | `bool` |
| `web.headers.nel.include_subdomains` | `bool` |
| `web.headers.nel.max_age_seconds` | `int` |
| `web.headers.nel.sample_rate` | `float64` |
| `web.headers.origin_agent_cluster` | `bool` |
| `web.headers.referrer_policy` | `string` |
| `web.headers.sec_fetch_validation` | `bool` |
| `web.headers.server_timing_in_debug_only` | `bool` |
| `web.hsts.enabled` | `bool` |
| `web.hsts.include_subdomains` | `bool` |
| `web.hsts.max_age_seconds` | `int` |
| `web.hsts.preload` | `bool` |
| `web.llms.custom_sections[*]` | `[]any` |
| `web.llms.include_endpoints` | `bool` |
| `web.llms.include_schemas` | `bool` |
| `web.reports.rate_limit_per_ip_burst` | `int` |
| `web.reports.rate_limit_per_minute` | `int` |
| `web.robots.allow[*]` | `[]string` |
| `web.robots.deny[*]` | `[]string` |
| `web.security.contact` | `string` |
| `web.security.expires` | `time.Time` |
| `web.security.keyservers[*]` | `[]string` |
| `web.security.policy` | `string` |
| `web.security.publish_pgp_key` | `bool` |
| `web.security.report_url` | `string` |
| `web.ui.favicon` | `string` |
| `web.ui.logo` | `string` |
| `web.ui.theme` | `string` |
