# Configuration Rules (PART 5, 6, 12)

⚠️ **These rules are NON-NEGOTIABLE. Violations are bugs.** ⚠️

## CRITICAL - NEVER DO
- Never write inline YAML comments — all comments go on the line ABOVE the setting (exception: GitHub Actions SHA-pin `# vX.Y.Z` annotations stay inline)
- Never use `strconv.ParseBool()` — always use `config.ParseBool()` / `config.IsTruthy()` for every boolean source (env vars, config file, CLI flags, API params, form inputs, query strings)
- Never fail startup on invalid config — warn and substitute the default instead
- Never let debug mode bypass authentication or security checks, in any mode, including production
- Never enable debug endpoints (`/debug/*`, pprof, expvar) unless `--debug` CLI flag or `DEBUG=true` is explicitly set — `development` mode alone does NOT enable them
- Never store user accounts or operator-editable configuration in the database — `server.yml` is the sole config source of truth; DB stores resource state, tokens (hashed), audit log only
- Never trust `X-Forwarded-*` headers from a peer outside `trusted_proxies` (private ranges + configured `additional` allow-list)
- Never evaluate `trusted_proxies`/`isTrustedPeer()` against a rewritten `r.RemoteAddr` — always check the original TCP peer address preserved before any real-IP middleware rewrite
- Never leak clearnet FQDN, clearnet email, or `Preferred-Languages` in any response served over Tor (`tor.onion_address` match)
- Never bind privileged ports (<1024) as a non-elevated user — must escalate or fall back to a random 64xxx port
- Never run the service permanently as root unless the project has a documented, justified exception in IDEA.md
- Never put `Retry-After` or other operational metadata as top-level JSON body fields — headers only, body carries `details`
- Never re-resolve the backup directory path at cleanup time — reuse the path resolved and cached at startup step 7
- Never introduce flat aliases or migration shims for the canonical `server.contact.*` keys

## CRITICAL - ALWAYS DO
- Always normalize and validate every path (config values, HTTP paths, file paths, API params) via `SafePath`/`normalizePath`/`validatePath` — reject `..`, disallow uppercase/invalid chars, cap segment length at 64 and full path at 2048
- Always run `PathSecurityMiddleware` at execution position #3 (after URL normalize + request-ID, before auth/routing) per the fixed 10-step middleware order
- Always persist the selected port (random or explicit) to `server.yml` on first run so it survives restarts — no runtime API for changing it
- Always drop privileges after privileged setup on Unix (root → bind ports → create dirs/user → drop to `{project_name}` service user) unless a documented permanent-root exception applies
- Always require `server.token` OR root for sensitive maintenance operations (`setup`, `restore`, `mode`, `pgp` key ops, `secret rotate`) once the server is past first-run
- Always enter maintenance mode (read-only, 503 on writes, self-healing retries) only for the two truly critical error classes: DB connection failure and inability to write files; every other error is recoverable
- Always auto-migrate `server.yaml` → `server.yml` on startup if found
- Always fall back through the contact resolution chain (`security`→`admin`, `abuse`→`general`→`admin`, `general`→`admin`) when a role-specific email is empty, computed fresh per dispatch (never cached across requests)
- Always sign outbound webhooks with `X-Webhook-Signature` (HMAC-SHA256), `X-Webhook-Timestamp`, `X-Webhook-ID`, `X-Webhook-Event` headers, and retry non-2xx with exponential backoff (1m,5m,15m,1h,6h,24h)
- Always adjust privacy/consent messaging dynamically based on `server.privacy.data.sold` (never-sold vs may-be-sold copy)

## Key Rules Summary

**Config storage & precedence.** `server.yml` is the sole source of truth for settings; database (SQLite local or libsql/Turso remote) stores resource state, hashed tokens, audit log — never accounts/config. Location: `/etc/apimgr/api/server.yml` (root) or `~/.config/apimgr/api/server.yml` (user). Config format rules: clean/intuitive, everything configurable, sane built-in defaults, single-line comments under 140 chars above the setting.

**Booleans.** Accept a large truthy/falsy word set (yes/no, on/off, enable/disable, si/non, oui/niet, y/n, t/f, etc.), case-insensitive, via `config.ParseBool(s, default)` / `config.IsTruthy(s)` / `config.IsFalsy(s)`. Empty string uses the default; invalid value is an error (not silently defaulted) except at CLI/env layer where `MustParseBool` panics only during init.

**Env vars.** Runtime (always checked): `NO_COLOR`, `TERM`, `DOMAIN`, `MODE`, `DATABASE_DRIVER`, `DATABASE_URL`, `SMTP_*`. Init-only (first run only, then ignored): `CONFIG_DIR`, `DATA_DIR`, `LOG_DIR`, `DATABASE_DIR`, `BACKUP_DIR`, `PORT`, `LISTEN`, `APPLICATION_NAME`, `APPLICATION_TAGLINE`.

**Ports.** Default is a random unused port in 64000-64999, saved to config on first run. Port 80 auto-enables HTTP-01 ACME challenge; 443 auto-enables TLS-ALPN-01 + SSL. Dual-port format `"8090,8443"` (HTTP,HTTPS). Privileged ports (<1024) require elevation; the binary auto-detects privilege (`isElevated`/`canEscalate`/`execElevated`, platform-specific Unix/Windows implementations), binds while root, then drops to a dedicated `{project_name}` system user/group (shell `nologin`, home `/var/lib/apimgr/api`). Sensitive maintenance ops (`setup`, `restore`, `mode`, `pgp`, `secret rotate`) require `server.token` or root, not just file access.

**Modes.** Mode resolution: `--mode` flag > `MODE` env > default `production`. Debug resolution: `--debug` flag > `DEBUG` env > `--mode debug`/`MODE=debug` alias > default false. `MODE=debug` = development + debug on, but an explicit `DEBUG` always wins over the alias. Four states: production, production+debug, development, development+debug. Debug endpoints (`/debug/pprof/*`, `/debug/vars`, `/debug/config`, `/debug/routes`, `/debug/cache`, `/debug/db`, `/debug/scheduler`) return 404 unless debug is explicitly enabled — development mode alone never exposes them.

**Maintenance mode.** Only DB-connection and file-write failures are critical; self-healing retries every 30s with capped/backoff strategies (log cleanup, disk cleanup, credential re-check). API writes return 503 with canonical error body; `Retry-After` and `X-Maintenance-*` live in headers only.

**Base URL / trusted proxies.** `server.baseurl` resolved from `X-Forwarded-Prefix` > `X-Forwarded-Path` > `X-Script-Name` > config > `/`. Proxy headers (FQDN, proto, port, base path, client IP families) are honored only from a trusted peer: private ranges (RFC1918, loopback, link-local, unique-local) always trusted, plus a configurable `trusted_proxies.additional` allow-list (IP/CIDR/DNS, refreshed every 5 min). Otherwise headers are dropped and `r.Host`/`r.RemoteAddr` are used.

**Tor.** `tor.onion_address` match on `Host` header is priority-0 FQDN resolution (before proxy headers), always HTTP, port stripped. All Tor responses must use only the onion address and `tor.contact_email` (omitted, never falling back to clearnet) across security.txt, llms.txt, OpenAPI, OAuth redirects, CORS, etc.

**Rate limiting.** Sliding window per IP in `server.db` (or external cache if configured): read 120/min, write 10/min, health 120/min, global burst ceiling 240/min. 429 response uses `Retry-After` header, never a body field.

**Contact config.** Unified `server.contact.{admin,security,abuse,general}` tree, each with `email` + `webhooks` (telegram/discord/slack/generic/any custom adapter). `admin` is the universal fallback. `security` defaults to `security@{fqdn}` (RFC 2142); `abuse`/`general` default empty and cascade to each other then to admin. Only these four canonical keys are permitted.

**Analytics tracking.** `server.tracking.{type,id,url}` supports google, matomo, piwik, owa, fathom, plausible, umami, simple, cloudflare — each with its own ID/URL validation rules and self-hosted vs cloud URL requirement.

**Privacy & consent.** `server.privacy.data.sold` (default false) drives dynamic messaging across the consent banner, cookie descriptions, and privacy-page content (never-sold copy vs may-be-sold/CCPA copy). Three cookie categories: essential (always on), preferences, analytics (opt-out model, default enabled, `Decline` disables non-essential).

**Cache.** Optional, defaults to in-process `memory`; `valkey`/`redis` supported via `url` or discrete `host/port/password` fields (url wins if both set). Used for sessions, rate-limit counters, optional response caching. Key prefix defaults to `api:`.

For complete details, see AI.md PART 5, 6, 12
