# Email, Scheduler, GeoIP, Metrics, Backup & Update Rules (PART 17-22)

⚠️ **These rules are NON-NEGOTIABLE. Violations are bugs.** ⚠️

**IDEA.md override:** No user accounts exist, so there are NO account-related
emails (no password reset, no verification flow, no welcome email) — every
email template in PART 17 is an operator/system notification, never a
user-facing account email. There is no admin web UI, so operator events
(backup, SSL, update, scheduler) NEVER render in the public WebUI — only
logs, email, and CLI output reach the operator, per PART 17 "Operator
Notifications".

## CRITICAL - NEVER DO
- Never send an email without a valid, tested SMTP connection — "No SMTP =
  No emails. Don't even try." No queuing, no "would have sent" logs
- Never invent account-related email templates (password reset, welcome,
  verification) — none exist in this project
- Never show email-dependent UI/config options when SMTP is not configured
- Never use an external cron/Task Scheduler/launchd/CronJob for ANY
  scheduled work — the built-in scheduler (Go `time`/ticker based) handles
  every scheduled task; there are no exceptions
- Never let GeoIP be the sole access-control gate — it is always one risk
  signal among many; blocked-country requests still pass through rate
  limiting, logging, and all other checks
- Never block a request because a GeoIP lookup failed or the database is
  missing/stale — GeoIP fails open, real auth/rate-limiting fails closed
- Never use `geoip2-golang` — ip-location-db's custom `database_type`
  strings make `geoip2.Open()` reject the files; use
  `github.com/oschwald/maxminddb-golang`
- Never embed GeoIP MMDB files in the binary — always downloaded on first
  run and refreshed via the scheduler
- Never expose `/metrics` publicly — it is internal-only (firewall/proxy/
  NetworkPolicy/security-group restricted), never proxied to the public
- Never let backup encryption passwords be stored anywhere — operator must
  remember them; no recovery mechanism
- Never delete existing valid backups before a new backup passes ALL
  verification checks
- Never let compliance mode run backups unencrypted — if
  `server.compliance.enabled: true`, backups are blocked until an
  encryption password is set
- Never let the update flow install without a SHA-256 checksum match
  against the release's `checksums.txt`
- Never inject update-availability notices into any public API response,
  header, or endpoint — update status is Tier 3/operator-only information

## CRITICAL - ALWAYS DO
- Auto-detect local SMTP on first run (loopback → Docker bridge → gateway
  → FQDN → global IPv4 → `mail.{fqdn}` → `smtp.{fqdn}`, ports 25/465/587);
  test the configured connection on every startup
- Store default email templates embedded in the binary
  (`src/server/template/email/`); custom overrides live in
  `{config_dir}/template/email/`; changes apply immediately (live reload)
- Use the three-channel notification model exactly: Public WebUI
  (toast/banner, visitors only, ephemeral/cookie-based) · Logs (always,
  every operator event) · Email (operators, SMTP-gated)
- Apply the `backup_failed`/`ssl_renewal_failed` → `scheduler_error`
  suppression rule — one notification per failed execution, not two
- Register every PART 18 built-in task: `ssl_renewal`, `geoip_update`,
  `blocklist_update`, `cve_update`, `update_check`, `token_cleanup`,
  `log_rotation`, `backup_daily`, `backup_hourly` (disabled by default),
  `healthcheck_self`, `tor_health`
- Persist scheduler task state (`last_run`, `last_status`, `next_run`,
  `run_count`, `fail_count`, `enabled`) in `server.db`; recover missed tasks
  within `catch_up_window` on startup
- Treat country/ASN/city GeoIP signals as risk inputs only — never login
  replacements
- Prefix every Prometheus metric with `{project_name}_`, snake_case, unit
  suffix (`_seconds`, `_bytes`, `_total`); normalize high-cardinality path
  labels (`:id`) and never emit a raw client IP as a label value
- Verify every backup immediately after creation (file exists, size > 0,
  checksum, decrypt test, manifest, content extraction, DB integrity) —
  ALL checks must pass before applying retention/deleting old backups
- Apply retention priority order: yearly > monthly > weekly > daily,
  oldest deleted first; `max_total_size` (if set) overrides count limits
- Require backup-restore authorization per PART 21 (allowed on empty DB or
  as root with confirmation; requires `server.token` as service user;
  denied for random users)
- Support all three update branches (`stable`/`beta`/`daily`), cumulative
  channel semantics, and `defer_days` gating for the scheduled check only
  (manual `--update check`/`yes` always sees the true latest)

## KEY DECISIONS (pre-answered)
| Question | Answer | Spec Reference |
|----------|--------|----------------|
| Account emails? | None exist — no accounts, no password reset/verification/welcome email | IDEA.md non-goals; PART 17 |
| SMTP implementation | `src/email/email.go` — `net/smtp`, `AutoDetectSMTP()`, `TestConnection()`, `SendNotification()` implement auto-detect + startup test per spec | PART 17 |
| Scheduler tasks currently registered | `src/scheduler/tasks.go` `RegisterDefaultTasks()` registers `backup_daily`, `ssl_renewal`, `geoip_update`, `token_cleanup`, `log_rotation`, `healthcheck_self`, `tor_health` — **`blocklist_update`, `cve_update`, and `update_check` are NOT registered**, an open gap against PART 18's required task list | PART 18 |
| Self-update implementation | **No `src/update`/`updater` package exists in the codebase** — PART 22's self-update flow (GitHub release check, checksum verify, platform-specific binary replace) is unimplemented; `--update` CLI behavior should be verified against this gap before assuming it works | PART 22 |
| GeoIP library | `github.com/oschwald/maxminddb-golang` (never `geoip2-golang`) — confirmed in `src/geoip/geoip.go` | PART 19 |
| GeoIP database source | `sapics/ip-location-db` via jsdelivr CDN, MMDB format, downloaded at runtime, refreshed weekly (Sunday 03:00) by `geoip_update` | PART 19 |
| GeoIP failure mode | Fail open — request still processed by the rest of the pipeline if DB missing/stale/lookup error | PART 19; IDEA.md "Failure mode for GeoIP" |
| Metrics namespace | `{project_name}_` prefix; implemented in `src/metrics/metrics.go` via `prometheus.NewRegistry()` + `promhttp`, registered at `/metrics` in `src/server/server.go` | PART 20 |
| Metrics auth | Spec allows optional bearer token; **current `metricsPrometheusHandler` in `src/server/server.go` has no token check and no config struct for `server.metrics.token` was found under `src/config/`** — open gap; operators must rely on firewall/proxy restriction only until token auth is added | PART 20 |
| Backup encryption | AES-256-GCM with Argon2id key derivation — implemented in `src/backup/backup.go` (`encrypt`/`decrypt`, Argon2id-labeled comment) | PART 21 |
| Backup retention tiers | **`src/backup/backup.go`'s `CleanupOldBackups(backupDir, keepCount)` only supports a flat count — no `keep_weekly`/`keep_monthly`/`keep_yearly`/`max_total_size` tiered retention found**; open gap against PART 21's full retention model | PART 21 |
| Compliance-mode backup enforcement | **No `server.compliance.enabled` gating found in `src/backup/backup.go`** — the "blocked until password set" behavior from PART 21 is not yet implemented | PART 21 |
| Update branch storage | `--update branch {name}` must write `update.branch` to `server.yml` — config is sole source of truth, no separate CLI-side state | PART 22 |

## TERMINOLOGY
| Term | Meaning |
|------|---------|
| Three notification channels | Public WebUI (toast/banner) · Logs · Email — each with a distinct audience and availability |
| Suppression rule | A more specific failure event (`backup_failed`, `ssl_renewal_failed`) suppresses the generic `scheduler_error` for the same execution |
| Catch-up window | Duration after restart within which missed scheduled tasks are still run |
| GeoIP risk signal | Country/ASN/city data used to adjust risk scoring, never as sole gate |
| Retention priority order | yearly > monthly > weekly > daily when pruning backups |
| Compliance mode | `server.compliance.enabled` — when true, forces encrypted backups, blocking unencrypted ones |
| Update channel | `stable`/`beta`/`daily` — cumulative, each includes all more-stable releases |
| `defer_days` | Minimum age (days) a release must reach before the scheduled `update_check` task treats it as eligible |

## QUICK REFERENCE
- Email: `src/email/email.go`; templates: `src/server/template/email/`
  (embedded defaults) + `{config_dir}/template/email/` (custom overrides)
- Scheduler: `src/scheduler/{scheduler,cron,tasks,tasks_unix,tasks_windows}.go`;
  task state persisted via `src/database/scheduler.go`
- GeoIP: `src/geoip/geoip.go`; MMDB files under
  `{data_dir}/security/geoip` per `server.geoip.dir`
- Metrics: `src/metrics/metrics.go`; endpoint wired in `src/server/server.go`
  at `/metrics` via `metricsPrometheusHandler`
- Backup: `src/backup/backup.go` (AES-256-GCM + Argon2id, tar.gz/tar.gz.enc)
- Update: **no implementation package found** — verify before relying on
  `--update` working end-to-end
- Missing scheduler tasks (`blocklist_update`, `cve_update`, `update_check`)
  and missing backup retention tiers/compliance gating/metrics token auth
  are open gaps, not intentional scope reductions — do not silently
  "correct" this rules file to match the gaps; the gaps belong in
  TODO.AI.md as follow-up work

---
For complete details, see AI.md PART 17, PART 18, PART 19, PART 20, PART 21, PART 22
