# Backend Rules (PART 9, 10, 11, 31)

⚠️ **These rules are NON-NEGOTIABLE. Violations are bugs.** ⚠️

## CRITICAL - NEVER DO
- Never expose stack traces, internal error chains, or filesystem/log/config paths in production responses (Tier 3 — debug-only, stripped by the `_debug` sanitizer).
- Never expose DB credentials, internal IPs/hostnames, any user's tokens/PGP keys, other users' PII, account-existence signals, or exact rate-limit thresholds (Tier 1 — never, not even in debug mode).
- Never compare tokens, password hashes, HMACs, signatures, or TOTP codes with `==`, `bytes.Equal`, or `strings.EqualFold` — always `crypto/subtle.ConstantTimeCompare`.
- Never return a different message/HTTP status/timing for "wrong password" vs "no such user" vs "locked" vs "expired" — always the same generic auth failure, logged server-side only.
- Never expose sequential integer primary keys in URLs/JSON/logs (`/users/1`) — external IDs must be opaque UUIDv4/v7.
- Never echo submitted passwords/tokens back in errors, logs, or audit events, even hashed — log only a stable hash/prefix.
- Never cast user-controlled content to `template.HTML`; never inline untrusted SVG/XML/HTML in templates or `<img src="data:...">`.
- Never build SQL with `fmt.Sprintf`/string concat — parameterized queries only; DB app role has no DROP/DDL.
- Never `DROP COLUMN`, `DROP TABLE`, `DELETE` in schema updates, and never rename columns — add new, migrate in app code, deprecate old.
- Never run without connection pooling or without a `context` timeout on every query/transaction.
- Never shell out with raw user content, filenames, refs, or repo metadata; never execute user-supplied content server-side.
- Never let `/.well-known/**` be claimed by user slugs/vanity routes; unsupported entries return 404, never a directory listing.
- Never emit `Server-Timing`, `Expect-CT`, `Feature-Policy`, or `Public-Key-Pins` in production.
- Never log private keys, TLS secrets, SMTP credentials, full API credentials, or financial data to any log file.
- Never use system Tor or default Tor ports (9050/9051) — the binary owns a dedicated, fully isolated Tor process with `ControlPort 127.0.0.1:auto`.
- Never fail server startup because Tor is missing/broken — Tor absence/errors log at INFO/WARN only, never block startup.

## CRITICAL - ALWAYS DO
- Always use `CREATE TABLE IF NOT EXISTS` + idempotent `ALTER TABLE ADD COLUMN` on every startup; no migration files, no version table.
- Always wrap error responses in the canonical `{"ok":false,"error":"CODE","message":"..."}` shape; success is `{"ok":true,"data":{}}`.
- Always log errors with request ID, error code, HTTP status, and internal detail (internal detail never leaves the process).
- Always set connection pool limits (`max_open`/`max_idle`/`max_lifetime`/`max_idle_time`) sized to deployment tier.
- Always give every DB query and transaction a `context.WithTimeout` (5s simple SELECT, 15s JOIN, 10s write, 60s bulk, 30s transaction).
- Always use `crypto/subtle.ConstantTimeCompare` for token/password/HMAC/signature checks and pad failed-auth timing to a fixed floor (≥100ms).
- Always run every public response through the Output Sanitization Pipeline: allow-list fields → redact sensitive query params → strip internal IPs/paths → truncate long fields → strip `dev_only` fields in production → pad timing.
- Always set the full security header set on every response (nosniff, X-Frame-Options, Referrer-Policy, COOP/COEP/CORP, CSP, Permissions-Policy, Reporting-Endpoints/Report-To/NEL, X-Request-ID) plus HSTS when SSL is on.
- Always serve `robots.txt` and RFC 9116 `security.txt`/`llms.txt`, auto-generated if no operator file exists.
- Always route sensitive operations (secret rotation, PGP export, IP block changes) through the existing `--maintenance` dispatcher with operator-token re-prompt + `audit.log` entry — never a new web/API route.
- Always store the four project secrets (`installation_secret`, `cookie_signing_key`, `csrf_token_secret`, `server.security.encryption_key`) generated on first start, never logged, always included in backups.
- Always render user-controlled content (pastes, markdown, uploads) as escaped text or sanitized markdown; force `Content-Disposition: attachment` for active MIME types (`text/html`, `image/svg+xml`, `text/xml`).
- Always keep the audit log append-only, JSON-only, one event per line, with `id`/`time`/`event`/`category`/`severity`/`actor`/`result`.

## Key Rules Summary

**PART 9 — Error Handling & Caching**
- Response shape: success `{"ok":true,"data":{}}`; error `{"ok":false,"error":"CODE","message":"..."}` with an optional `details` object for structured validation context.
- Error codes map to HTTP status:
  - `BAD_REQUEST`, `VALIDATION_FAILED` → 400
  - `UNAUTHORIZED`, `TOKEN_EXPIRED`, `TOKEN_INVALID` → 401
  - `FORBIDDEN`, `ACCOUNT_LOCKED`, `CSRF_FAILED` → 403
  - `NOT_FOUND` → 404, `METHOD_NOT_ALLOWED` → 405, `CONFLICT` → 409
  - `RATE_LIMITED` → 429, `MAINTENANCE` → 503, default → 500
- Log level follows HTTP status: ≥500 = Error, 400-499 = Warn; always include `error_code`, `request_id`, `http_status`, and (server-side only) the internal error string.
- Retryable errors (network errors, `context.DeadlineExceeded`, 503) use exponential backoff: 0s, 1s, 2s, 4s, 8s, capped at 30s; 4xx errors are never retried.
- Cache keys are hierarchical/colon-separated/lowercase: `{type}:{id}`, `{type}:{id}:{field}`, `{type}:list:{filter}`, `{scope}:{type}:{id}`, `rate:{type}:{key}`, `lock:{resource}`; include a version prefix (`v1:user:123`) for cache-busting.
- Default TTLs: API tokens no expiry (explicit revocation only), rate-limit counters 1 minute, user profile 5 minutes, config 1 minute, static content hash 24 hours, GeoIP data 7 days, blocklist 1 hour, page cache 5 minutes, API response cache 30 seconds.
- Invalidation strategies: time-based (TTL on write), event-based (delete related keys on update/delete), version-based (key embeds version, old keys expire naturally), tag-based (invalidate a group by tag).
- HTTP `Cache-Control`: static assets (fingerprinted) `public, max-age=31536000, immutable`; HTML pages and authenticated/error responses `no-store` / `private, no-store`; public API responses `public, max-age=60`.
- Cache drivers: `memory` (dev default, in-process, lost on restart), `valkey` (preferred production), `redis` (full compatibility).

**PART 10 — Database**
- Idempotent schema only, applied on every startup: `CREATE TABLE IF NOT EXISTS` for tables, idempotent `ALTER TABLE ADD COLUMN` for changes; no migration files, no schema-version tracking table.
- New columns must have a `DEFAULT` or be nullable; "duplicate column" errors from `ALTER ADD COLUMN` are swallowed (`isColumnExistsError`), not treated as failures.
- Column renames use a 3-step process across releases: (1) add the new column, (2) app code reads new/falls back to old and writes both, (3) after upgrade, old column stays in the DB unused (never dropped) — comment each step with the version it shipped in.
- Same idempotent approach applies to both local SQLite and remote libsql/Turso.
- Connection pooling is mandatory; pool sizing by deployment tier (max_open/max_idle): development 5/2, small 25/5 (default), medium 50/10, large 100/20; also configure `max_lifetime` and `max_idle_time`.
- Every query and transaction gets a `context.WithTimeout`: simple SELECT 5s, complex JOIN 15s, INSERT/UPDATE/DELETE 10s, bulk operations 60s, transactions 30s, reports 2m, schema changes 5m.
- Transaction patterns: basic `WithTransaction(ctx, db, fn)` wrapper that rolls back on any error and commits otherwise; optimistic locking via a `version` column with `WHERE id = $n AND version = $v`, returning `CONFLICT` on zero rows affected; `sql.LevelSerializable` isolation for high-contention flows (e.g. reservations), with retry-on-`SQLITE_BUSY`/"database is locked" using incremental backoff and a max-retry cap.

**PART 11 — Security & Logging**
- Public-endpoint safety is a 3-tier model, not a feel-based judgment call:
  - Tier 1 (never, not even in debug): DB credentials, internal IPs/hostnames, any user's tokens, other users' PII, filesystem paths, account-existence signals, exact rate-limit thresholds, fields tagged `private`/`internal`/`sensitive`.
  - Tier 2 (always public, even unauthenticated): `app_name`, `version`, `commit_hash`, `build_date`, `go_version`, `uptime`, `mode`, `db_type`/`db_locality` (no host/creds), aggregate metrics, OpenAPI/GraphQL spec.
  - Tier 3 (debug-only, `--debug`/`DEBUG=true`): stack traces, full error chains, CSP/CSRF/CORS failure detail, validation constraint detail, rate-limit counters, SQL text (params redacted), goroutine/pprof dumps.
  - When unsure between Tier 2/3 default to Tier 3 (operator promotes later); between Tier 1/3 default to Tier 1 (debug is not a leak license).
- Defense-in-depth: every threat class (SQLi, XSS, enumeration, timing oracles, credential stuffing, path traversal, credential leakage, CSRF) must be mitigated at all four layers — input validation, data-access (parameterized queries, constant-time compare), output (escaping, identical response shape/timing), transport (least-privilege DB role, TLS, `SameSite=Strict`) — never assume another layer catches it.
- Authentication/identity rules: constant-time comparison for every token/password-hash/HMAC/signature/TOTP check; identical generic message + status for every auth failure mode with the real reason logged server-side only; failed-auth timing padded to a fixed floor (≥100ms); opaque UUIDv4/v7 for all externally exposed resource IDs (internal `BIGSERIAL` PKs may remain for performance); never echo submitted credentials, even hashed.
- Output Sanitization Pipeline (mandatory chokepoint for every public response): allow-list fields → redact sensitive query params (`token`, `session`, `code`, `key`, `password`, `secret`, `auth`, `api_key`, `access_token`, `refresh_token`) → strip private-IP/filesystem-path patterns → truncate long strings (256/200/2KB) → strip `dev_only` fields in production → pad response time for sensitive ops.
- Untrusted content handling: plain text/code renders escaped in `<pre><code>`; markdown sanitized after disabling raw HTML passthrough; user-supplied HTML/SVG/XML never rendered inline — force `Content-Disposition: attachment` for active MIME types (`text/html`, `image/svg+xml`, `text/xml`, `application/xhtml+xml`); archive extraction (if implemented) enforces path confinement, rejects symlinks/special files/absolute paths/`..`, and enforces size/file-count/compression-ratio limits; never shell out with raw user content.
- Security headers required on every response: `X-Content-Type-Options: nosniff`, `X-Frame-Options: SAMEORIGIN`, `X-XSS-Protection`, `Referrer-Policy: strict-origin-when-cross-origin`, `X-Permitted-Cross-Domain-Policies: none`, `Origin-Agent-Cluster: ?1`, COOP/COEP/CORP (default `unsafe-none`/`unsafe-none`/`cross-origin`, tightened automatically per `IDEA.md` compliance declarations — COPPA/HIPAA/PCI-DSS tighten to `same-origin`/`require-corp`/`same-site`), CSP, Permissions-Policy (all sensor/camera/mic/geo features locked to `()` unless declared in use), `Reporting-Endpoints`/`Report-To`/`NEL`, `X-Request-ID`; add HSTS (`max-age=63072000`, `includeSubDomains`, `preload`) when SSL is on; add `Clear-Site-Data` on token revocation and consent withdrawal.
- CSP default (per-directive, operator-extendable via `*_extra` config, never redefine the whole policy): `default-src 'self'`, `script-src 'self'` (zero inline scripts allowed), `style-src 'self' 'unsafe-inline'`, `img-src`/`font-src` allow `https:`, `connect-src 'self' {learned_origins}`, `object-src 'none'`, `frame-ancestors 'self'`, `base-uri 'self'`, `form-action 'self'`, `upgrade-insecure-requests`; violations POST to `/api/{api_version}/server/reports/csp`, logged as `security.csp_violation`, never echo user-controlled fields back.
- `Sec-GPC: 1` is a binding opt-out — skip personalization/tracking/non-essential cookies and log `compliance.gpc_honored`; `DNT` is not honored by default (dead standard) unless operator opts in.
- `Sec-Fetch-*` headers are validated as a CSRF/clickjacking defense layer on state-changing requests; absence (older browsers) falls through to the CSRF token check rather than being rejected outright.
- Well-known namespace (`/.well-known/**`) is root-owned and reserved: GET/HEAD only (405 otherwise), no auth/CSRF/session required, no directory listing, never serves user-uploaded files, only allow-listed entries served and unsupported entries return 404; required for all projects: `security.txt` (RFC 9116) and `llms.txt`; feature-gated by `IDEA.md`: `webfinger`, `openid-configuration`, `assetlinks.json`, `apple-app-site-association`, `mta-sts.txt`.
- Security reports pipeline: rotating `{security_id}` = `HMAC-SHA256(installation_secret, floor(unix_seconds/172800))` hex, first 16 chars, 48h rotation with current+previous window accepted; report bodies encrypted at rest with the project PGP key (fallback AES-256-GCM via `server.security.encryption_key`); maintainer notification and researcher acknowledgment both PGP-encrypted; all secret rotation / PGP keypair management flows through `--maintenance` CLI (`server.token` or root) with a sensitive-operation re-prompt and `audit.log` entry — never a web UI or admin API route.
- Cryptographic keys are project-level, generated on first start, stored in `server.db`/`server.yml`, never logged or returned by any API, always included in backups: `installation_secret` (32B, root HMAC/KDF secret), `cookie_signing_key` (auto-rotated 90d), `csrf_token_secret` (auto-rotated 180d), `server.security.encryption_key` (AES-256-GCM, canonical at-rest key for all sensitive data).
- Logging: `access.log` (apache/nginx/json), `server.log`/`error.log`/`app.log` (text/logfmt/json), `auth.log` (syslog RFC 3164, stable `reason=` codes for Fail2ban/SIEM), `audit.log` (JSON Lines only, append-only, tamper-evident, 0640 perms, `id`/`time`/`event`/`category`/`severity`/`actor`/`result` fields), `security.log` (fail2ban/syslog/cef/json/text) — every log FILE is raw text only (no ANSI, no emojis, no color); console/stdout output may be pretty and respects `NO_COLOR`/`TERM=dumb`. Successful health-check 2xx requests are suppressed from `access.log` by default; failures always log; never log tokens/secrets raw — redact by field name (`secret`, `key`, `password`, `token`).
- Compliance standards (GDPR, CCPA, HIPAA, SOC2, PCI-DSS, ISO27001, FedRAMP, LGPD, PIPEDA, APPI, PDPA) are all disabled by default and enabled individually; when multiple apply, the strictest per-requirement value wins (longest retention, strongest encryption, shortest breach-notification window, shortest session timeout); a right-to-erasure vs. retention-requirement conflict is resolved by anonymizing PII rather than deleting the record, preserving the audit trail.
- IP allowlist bypasses blocklists, rate limiting, GeoIP country blocking, and auto-block — it never bypasses CSRF protection, path-traversal checks, or TLS/SSL enforcement.

**PART 31 — Tor Hidden Service**
- All projects must support a built-in Tor v3 hidden service, auto-enabled whenever a Tor binary is detected on the host (no enable/disable config toggle) — implemented via `github.com/cretz/bine` to preserve `CGO_ENABLED=0` static-binary compatibility.
- The server binary starts and fully owns a dedicated Tor child process — never system Tor — with an isolated `DataDir`, `ControlPort 127.0.0.1:auto` (never a fixed/well-known port), and `SafeLogging` enabled to scrub sensitive data from Tor's own logs.
- Hidden service creation uses the `ADD_ONION` control command to map `.onion:{virtual_port}` to `127.0.0.1:{server_port}` — the server's existing HTTP listener, not a new port; v3 (ed25519) onion addresses only, persisted via a saved private key for a stable address across restarts.
- Optional outbound-through-Tor routing (`server.tor.use_network`, default `false`) is a separate capability from hidden-service hosting, exposed as a server-wide default with an optional per-user override.
- Tor's own directories are fixed and not operator-configurable: config `{config_dir}/tor/` (0700), data `{data_dir}/tor/` (0700), hidden-service keys `{data_dir}/tor/site/` (0700), log `{log_dir}/tor.log`; the Tor process inherits the server's (possibly privilege-dropped) running user so file ownership always matches.
- Tor is optional and best-effort: binary-not-found and startup/runtime failures log at INFO/WARN only and never block or crash server startup; console output stays silent during bootstrap (only a one-time onion-address success line, or a `connecting...` note past 30s), full verbosity is debug-mode only.

For complete details, see AI.md PART 9, 10, 11, 31
