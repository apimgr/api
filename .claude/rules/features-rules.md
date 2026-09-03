# Features Rules (PART 17, 18, 19, 20, 21, 22)

⚠️ **These rules are NON-NEGOTIABLE. Violations are bugs.** ⚠️

## CRITICAL - NEVER DO
- Never attempt to send email without a valid, working SMTP connection; never queue emails hoping SMTP appears later; never log "would have sent email" messages
- Never show email-dependent UI/options when SMTP is not configured
- Never store the backup encryption password anywhere — no recovery if lost
- Never use external schedulers (cron, systemd timers, Task Scheduler, launchd, Kubernetes CronJob, cloud schedulers) for any project task — built-in scheduler only, no exceptions
- Never treat GeoIP/country data as the sole access-control gate or as authentication — it is advisory only (VPN/proxy/Tor bypass it trivially)
- Never block a request solely because GeoIP lookup failed/missing/stale — fail-open for GeoIP, fail-closed only for real auth
- Never use a raw client IP as a Prometheus metric label value (unbounded cardinality / memory-DoS); never label with user_id/request_id/timestamps
- Never expose `/metrics` publicly — it is internal-only; do not proxy it externally
- Never delete existing backups unless the new backup passes ALL verification checks; on verification failure, delete only the failed backup and retry next run
- Never restore without full verification passing (checksum, decrypt, manifest, format)
- Never allow restore by "random user" (no root, no service-user token) — authorization required
- Never pass backup/restore passwords via CLI flags (shell history/process list leakage) — interactive prompt only
- Never inject update availability/version info into public API responses, headers, or any public endpoint — update status is operator-only information
- Never skip SHA256 checksum verification on downloaded update binaries
- Operator events (backups, SSL, scheduler failures, updates, abuse detection) must never render in the public WebUI — no admin UI exists

## CRITICAL - ALWAYS DO
- Always check SMTP status once at startup and on config change; completely disable email features when SMTP unavailable
- Always give every operator event a structured, leveled log line, regardless of whether email is sent
- Always run the built-in scheduler continuously from app start until shutdown, with state persisted in `server.db`
- Always run missed/catch-up tasks on startup if within the catch-up window
- Always verify every backup immediately after creation (file exists, size>0, checksum, decrypt test, manifest, content extraction, DB integrity) before applying retention/pruning
- Always check free disk space before creating a scheduled backup (abort with `backup.skipped_disk_full` if insufficient)
- Always require SSL/backup password via interactive prompt, never a flag, when encryption is enabled
- Always require `server.compliance.enabled: true` to force backup encryption (block backups until password set)
- Always require a valid checksum match (against release `checksums.txt`) before installing a self-update
- Always treat manual `--update check`/`--update yes` as overriding `defer_days` (explicit operator action wins)
- Always use platform-specific binary replacement logic (Unix atomic rename vs Windows rename-to-.old + delayed delete)

## Key Rules Summary

### Email & Notifications
- Two-tier templates: embedded defaults in binary (`src/server/template/email/`) vs custom overrides in `{config_dir}/template/email/`; custom wins if present; delete custom file to reset to default; changes apply live, no restart
- No account-related email flows exist (no users, no password reset/verification) — all templates are system/operator notifications
- SMTP auto-detection order on first run: `127.0.0.1` → `172.17.0.1` (docker bridge) → gateway IP → detected FQDN → global IPv4 → `mail.{fqdn}` → `smtp.{fqdn}`, each tried on ports 25/465/587 via EHLO handshake; first success saved to `server.yml`; all fail = email disabled (not an error)
- If host is explicitly configured, connection is tested every startup; success enables email, failure disables it (with warning) and retries next startup
- `SMTP_*` env vars (`SMTP_HOST`, `SMTP_PORT`, `SMTP_USERNAME`, `SMTP_PASSWORD`, `SMTP_TLS`, `SMTP_FROM_NAME`, `SMTP_FROM_EMAIL`) override config file, useful for containers
- Default sender: From Name = app title, From Address = `no-reply@{fqdn}` (or `no-reply@localhost`)
- 10 built-in templates: `security_alert`, `backup_complete`, `backup_failed`, `ssl_expiring`, `ssl_renewed`, `ssl_renewal_failed`, `scheduler_error`, `update_available`, `update_installed`, `test` — each has a sane default subject/behavior working with zero config
- Template format: first line `Subject: ...`, then `---` separator, then plain-text body using `{variable}` syntax
- Global variables available in all templates: `{app_name}`, `{app_url}`, `{fqdn}`, `{onion_url}`, `{onion_address}`, `{i2p_url}`, `{i2p_address}`, `{notification_reply_to}`, `{timestamp}`, `{year}`
- All server notification emails must state why sent, app identity (name+FQDN), and a visible plaintext link
- Suppression rules: `backup_failed` suppresses `scheduler_error` for the same execution; `ssl_renewal_failed` suppresses `scheduler_error` for the same execution — one notification, not two; tasks without dedicated failure events (`token_cleanup`, `log_rotation`, `update_check`) still fire `scheduler_error` normally
- Template validation before save: unknown variables, empty subject/body, invalid syntax rejected; warnings (non-blocking) for deprecated vars, subject >78 chars
- `api email test` sends a real test email using sample data, subject prefixed `[TEST]`, logged to audit log
- Three notification channels: Public WebUI (toast/banner, visitors only), Logs (always, operators), Email (operators, requires SMTP)
- Public WebUI has exactly two mechanisms — toast (ephemeral, DOM-only, auto-dismiss) and server-rendered site banner (persists until dismissed via `dismissed_announcements` cookie); no notification center/bell/history exists
- Toast auto-dismiss: success/info 3s, warning 5s, error manual; default position `top-right`
- Operator events NEVER appear in WebUI toast/banner — only via logs/email/CLI (`--status`, `--update check`)
- Decision logic for log vs email: always log; only email if SMTP configured AND (critical/security/urgent OR operator needs record while away); routine success = log only
- No server-side notification storage exists — no `notifications` table, no per-operator state, no WebSocket sync
- Per-event email toggles live under `server.notifications.email.events` (e.g. `backup_failed: true`, `ssl_expiring: true`, `security_alert: true`, `scheduler_error: true`, `update_installed: true`; several default false: `startup`, `shutdown`, `backup_complete`, `ssl_renewed`, `update_available`)

### Scheduler
- Built-in scheduler is mandatory, always running, no enable/disable toggle for the scheduler itself (individual tasks can be enabled/disabled)
- 11 required built-in tasks with defaults: `ssl_renewal` (daily 03:00, not skippable), `geoip_update` (weekly Sun 03:00, skippable), `blocklist_update` (daily 04:00, skippable), `cve_update` (daily 05:00, skippable), `update_check` (daily 06:00, skippable), `token_cleanup` (every 15m, not skippable), `log_rotation` (daily 00:00, not skippable), `backup_daily` (daily 02:00, skippable), `backup_hourly` (hourly, disabled by default), `healthcheck_self` (every 5m, not skippable), `tor_health` (every 10m, required when Tor installed)
- Task state persisted in `server.db`: task_id, task_name, schedule, last_run, last_status, last_error, next_run, run_count, fail_count, enabled
- Startup flow: load state → check for missed tasks within `catch_up_window` (default 1h) → queue missed tasks in original scheduled order → start loop
- Retry policy defaults: `max_retries: 3`, `retry_delay: 5m`, exponential backoff (5m, 10m, 20m)
- Shutdown: stop accepting new executions, wait up to 30s for running tasks, force-release locks and mark interrupted tasks for retry on timeout, persist state before exit
- Schedule formats supported: standard 5-field cron, `@hourly`, `@daily`, `@weekly`, `@monthly`, `@every Xm/Xh`
- Timezone default `America/New_York`, configurable via `server.scheduler.timezone`
- CLI: `scheduler list`, `scheduler show <id>`, `scheduler run <id>`, `scheduler enable/disable <id>`, `scheduler history <id>`
- Implementation: Go's time/ticker (no external cron libs), database-backed state, graceful shutdown, audit logging of every execution, notifications on failure
- Backup retention settings live under `scheduler.tasks.backup_daily.retention` (max_backups, keep_weekly/monthly/yearly, max_total_size) — see Backup & Restore

### GeoIP
- Built-in support required, using `sapics/ip-location-db` MMDB databases — never embedded in binary, downloaded on first run, updated via scheduler (weekly default)
- Go library must be `github.com/oschwald/maxminddb-golang`, NOT `geoip2-golang` (ip-location-db's custom `database_type` strings break `geoip2.Open()`)
- Four database types: ASN (`asn.mmdb`), Country (`country.mmdb`), City IPv4/IPv6 (`dbip-city-*.mmdb`); WHOIS is not a separate file — it's a combined ASN+Country lookup at query time
- Country blocking config: `deny_countries` (block listed, allow rest) vs `allow_countries` (allow only listed, block rest) — mutually exclusive in intent; if both set, `allow_countries` wins
- ISO 3166-1 alpha-2 codes, uppercase, 2 letters
- Allowlisted IPs (`server.security.allowlist`) always bypass country blocking
- If `country.mmdb` missing, country blocking is skipped with a warning (fail-open)
- Tor exit nodes blocked/allowed by exit-node country, not user origin; private/internal (RFC 1918) IPs are never country-blocked
- GeoIP is one risk signal among many — must run alongside/after rate limiting and authentication, never replace them; a blocked-country request still consumes rate-limit budget and is logged

### Metrics
- Built-in Prometheus-compatible `/metrics` endpoint mandatory, using `github.com/prometheus/client_golang`
- Access: internal only — firewall/proxy/NetworkPolicy/security-group restricted; optional bearer token auth via `Authorization: Bearer <token>` when `token` config set (empty = no auth, relies on network isolation)
- Naming: all metrics prefixed `api_`, snake_case, unit suffixes (`_seconds`, `_bytes`), counters end `_total`, base units only (seconds not ms, bytes not KB)
- Metric types: Counter (cumulative), Gauge (up/down), Histogram (bucketed), Summary (quantiles)
- Label rules: snake_case, lowercase, no units in label names, low cardinality only (`method`, `status` — never `user_id`/`request_id`)
- Path normalization required: replace UUIDs/numeric IDs with `:id` before using as a label to control cardinality
- Required minimum metrics: `app_info`, `app_uptime_seconds`, `app_start_timestamp`; HTTP (`http_requests_total`, `http_request_duration_seconds`, `http_request_size_bytes`, `http_response_size_bytes`, `http_active_requests`); DB metrics required if using a database (`db_queries_total`, `db_query_duration_seconds`, `db_connections_open/in_use`, `db_errors_total`); auth metrics (`auth_attempts_total`, `auth_sessions_active`)
- Optional/conditional metrics: cache (if cache used), scheduler (if scheduler used — always true), system (if `include_system: true`), Go runtime (if `include_runtime: true`), Tor (if Tor enabled), rate limiting
- Config keys: `server.metrics.enabled`, `.endpoint` (default `/metrics`), `.include_system`, `.include_runtime`, `.token`, `.duration_buckets`, `.size_buckets`
- Rate-limit metric `per_ip` must be a `limit` label *value* ("per_ip" as a category), never a per-address label — log per-IP detail to structured logs instead

### Backup & Restore
- Backup command: `api --maintenance backup [filename]`
- Always included: `server.yml`, `server.db` (config KV, rate limit counters, audit log, scheduler task state, backup metadata), `{config_dir}/template/`, `{config_dir}/theme/` (if exist); optional via flags: `--include-ssl`, `--include-data`
- Format: single `.tar.gz` (or `.tar.gz.enc` if encrypted) with a `manifest.json` (version, created_at/by, app_version, contents, encrypted flag, encryption method, SHA256 checksum)
- Naming: manual/timestamped `api_backup_YYYY-MM-DD_HHMMSS.tar.gz[.enc]`; scheduled daily full `api_backup_YYYY-MM-DD.tar.gz[.enc]`; daily incremental `api-daily.tar.gz[.enc]`; hourly incremental `api-hourly.tar.gz[.enc]`
- Encryption optional unless compliance mode enabled (then mandatory, blocks backups until password set); AES-256-GCM with Argon2id key derivation; password never stored, no recovery; unencrypted archive never touches disk during encryption
- Retention config (`server.backup.retention`): `max_backups` (default 1, daily fulls), `keep_weekly` (default 0), `keep_monthly` (default 0), `keep_yearly` (default 0), `max_total_size` (default `"10%"`, hard cap overriding count limits, deletes oldest first); falsey/disabled values: `0`, `false`, `no`, `none`, `disable`, `disabled`, `off`
- Invalid retention values (0 for max_backups, negative, non-numeric) warn and fall back to default rather than failing startup; excessive values (e.g. max_backups>7) warn but are accepted
- Retention priority order: yearly > monthly > weekly > daily; a single backup can satisfy multiple categories simultaneously
- Backup creation flow: retention sweep → disk-space check (abort+log if insufficient) → create full → verify → create daily incremental → verify → apply retention only if ALL verifications pass; on any failure, delete failed file, keep existing backups, alert operator, retry next run
- Verification checks (all must pass): file exists, size>0, checksum valid, decrypt test (if encrypted), manifest readable, content extraction test, database integrity check
- Cleanup runs at startup and after every backup, against the backup dir resolved/cached at startup — never re-resolved; matches all app-created filename patterns; unclassified matching files treated as daily for pruning purposes (nothing is exempt)
- Restore command: `api --maintenance restore <backup-file>`; destructive operation requiring authorization (allowed if DB empty/first-run, allowed as root with confirmation, requires operator token as service user, denied for random user)
- Restore requires backup password (separate from `server.token`) if encrypted — interactive prompt (CLI), dialog (WebUI), or 400 `password_required` error (API); no password CLI flag
- Restore verification (all must pass before proceeding): file exists/readable, valid tar.gz(.enc) format, decrypt test, checksum, manifest valid, version compatibility (warning only, not blocking)
- Audit events logged: `backup.created`, `backup.restored`, `backup.deleted`, `backup.failed`, `backup.retention_cleanup`, `backup.verification_failed`, `backup.daily_updated`, `backup.skipped_disk_full`

### Update Command
- Command: `--update [check|yes|branch {stable|beta|daily}]`, default `yes`; alias: `--maintenance update [cmd]`
- Exit codes: 0 = success or already current, 1 = error; GitHub API 404 means no updates available
- Three cumulative channels: `stable` (full releases only), `beta` (beta pre-releases + all stable, newest of both), `daily` (daily pre-releases + beta + stable, newest overall) — a less-stable channel is never older than a more-stable one
- `--update branch {name}` writes directly to `update.branch` config — config file is the single source of truth, no separate CLI state
- `defer_days` (0-365, default 0) gates only the scheduled `update_check` task (both notify and auto-install); manual `--update check`/`--update yes` always sees/installs true latest, bypassing the defer window; eligibility is per-release: `now - published_at >= defer_days`; newest eligible release adopted each run (rolling delay, no deadlock)
- `update_check` scheduled task (daily 06:00, skippable): runs equivalent of `--update check` filtered by defer_days; `auto_install: false` (default) = notify only via `update_available` event; `auto_install: true` = runs full `--update yes` flow
- Update visibility restricted to operators only: WARN log + optional `update_available` email + CLI (`--update check`, `--status`) — fires once per newly-seen eligible version, not re-sent every run; never surfaced via any public API/endpoint/header
- Self-update flow: check GitHub Releases API → download binary to temp → verify SHA256 against release's `checksums.txt` (mandatory) → replace running binary (platform-specific) → restart service or re-exec
- Unix: atomic `os.Rename` over running binary works; restart via `syscall.Exec`; Windows: cannot replace running exe — rename current to `.old`, move new binary into place, schedule `.old` deletion via `MOVEFILE_DELAY_UNTIL_REBOOT`; restart by spawning new process and `os.Exit(0)` (no exec-replace support)
- Service-aware restart per platform: systemd (`systemctl restart`), other Linux init (service-specific), launchd (`launchctl kickstart -k`), BSD rc.d (`service restart`), Windows SCM (`sc stop && sc start`)

For complete details, see AI.md PART 17, 18, 19, 20, 21, 22
