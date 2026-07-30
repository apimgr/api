# Backend, Security & Tor Rules (PART 9, 10, 11, 31)

⚠️ **These rules are NON-NEGOTIABLE. Violations are bugs.** ⚠️

**IDEA.md override:** IDEA.md non-goals declare no user accounts, no
registration/login, no admin web panel, and no persistent storage of
user-submitted data — this is a stateless, anonymous, no-auth public API/CLI
toolkit. Base-spec text in PART 10/11 that assumes user accounts, sessions,
password resets, email verification, TOTP/WebAuthn, or admin-panel auth does
NOT apply to this project's business logic. PART 10's generic schema
examples and PART 11's "Authentication & Identity Rules" describe mechanisms
this project has no legitimate use for (there is nothing to authenticate
except the single operator `server.token`, per `config-rules.md`).

**RESOLVED — previously flagged discrepancy:** the codebase used to create a
`users.db` with `admins`, `users`, `password_resets`, `email_verifications`,
and `totp_secrets` tables, plus a `sessions` table and a `config`/
`config_meta` key-value store in `server.db`. This was confirmed to be dead
scaffold code with zero real call sites (verified via `grep -rn`), and
directly contradicted both IDEA.md's non-goals AND AI.md's own "no admin web
UI" statements (multiple PARTs, e.g. lines 5357, 7177, 15816, 23502, 23540,
26818) — there was no actual AI.md-vs-IDEA.md conflict to resolve, only dead
code to delete. `users.db` and the `config`/`config_meta`/`sessions` tables
were removed from `src/database/database.go`; PART 18's required
`token_cleanup` task was repointed at `rate_limits` (the one real expiring
state this project has) via `database.CleanupExpiredRateLimits()`. See
`TODO.AI.md` for the full resolution note.

## CRITICAL - NEVER DO
- Never store operator-editable configuration in the database — `server.yml`
  is sole source of truth (see `config-rules.md`); there is no `config`/
  `config_meta` DB table in this codebase, and none should be reintroduced
- Never reintroduce user accounts, sessions, or admin-panel auth tables —
  IDEA.md forbids them and AI.md itself states there is no admin web UI; the
  `admins`/`users`/`sessions`/`password_resets`/`email_verifications`/
  `totp_secrets` tables were dead code and have been deleted, not sanctioned
  prior art to build on
- Never compare tokens/passwords/HMACs/signatures with `==`, `bytes.Equal`,
  or `strings.EqualFold` — always `crypto/subtle.ConstantTimeCompare`
- Never expose Tier-1 data (DB credentials, internal IPs/hostnames, any
  user's tokens, filesystem paths, account-existence signals, exact
  rate-limit thresholds) in any response, even in debug mode
- Never emit `_debug`, `Server-Timing`, or other Tier-3 diagnostics in
  production — debug fields are stripped by tag in the Output Sanitization
  Pipeline as a second layer, not just by "not enabling debug"
- Never use `DROP`/rename on an existing column — additive-only schema
  changes (add-new/migrate/deprecate pattern), no migration files or version
  tracking; every table is `CREATE TABLE IF NOT EXISTS`
- Never expose sequential integer primary keys in a public-facing surface —
  opaque IDs (UUID v4/v7) only; internal `BIGSERIAL` keys may stay internal
- Never log a raw secret, token, password, or PGP key material — log only a
  stable hash/prefix; the Output Sanitization Pipeline redacts any field
  whose name contains `secret`/`key`/`password`/`token`
- Never claim `/.well-known/**` for anything but the documented allow-listed
  entries — it is a root-owned protocol namespace, not a static-file bucket;
  unsupported entries return `404`, never a directory listing
- Never use system Tor or a shared/default Tor port (9050/9051) — the
  server owns and starts its OWN dedicated Tor process on
  `127.0.0.1:auto`-selected ports only
- Never make Tor's absence an error — missing Tor binary logs INFO and the
  server continues without hidden-service support
- Never enter maintenance mode for anything other than the two true
  critical errors (DB connection failure, file-write failure) — see
  `config-rules.md`

## CRITICAL - ALWAYS DO
- Every error response uses the canonical shape: `{"ok":true,"data":...}` on
  success, `{"ok":false,"error":...,"message":...,"details"?:...}` on
  failure (full shape authoritative in PART 14; PART 9 defines the standard
  error-code table and exponential-backoff retry pattern)
- Categorize response data into Tier 1 (never public, not even debug), Tier
  2 (always public — version/commit/uptime/aggregate metrics), Tier 3
  (debug-only diagnostics) per the Public Endpoint Safety Principle; default
  to Tier 3 when unsure between 2/3, Tier 1 when unsure between 1/3
- Pad failed-auth / sensitive-operation response timing to a fixed floor
  (≥100ms) so success/fail timing doesn't leak internal state
- Serve `robots.txt`, `/.well-known/security.txt` (RFC 9116), and
  `/.well-known/llms.txt` (+ `/llms.txt` alias) on every project — generated
  from config if no override file exists
- Emit the full security header set on every response (X-Content-Type-Options,
  X-Frame-Options, Referrer-Policy, COOP/COEP/CORP at their "everyone"
  defaults, Permissions-Policy generated from `web.permissions_policy`,
  Reporting-Endpoints/Report-To/NEL, CSP) — tighten only per `IDEA.md`'s
  declared audience/compliance/data_class values (this project declares
  none, so headers stay at loose "everyone" defaults per IDEA.md's
  Compliance declarations section)
- Honor `Sec-GPC: 1` as a privacy opt-out signal; do NOT honor `DNT` by
  default
- All log FILES are raw text only (no ANSI, no emojis, no control chars) —
  console/stdout output may be pretty and respects `NO_COLOR`
- `audit.log` is JSON Lines, append-only, tamper-evident, `keep: none` by
  default (delete on rotation, daily) — never log secrets, always log
  timestamp/IP/actor/result/event-id
- All compliance standards (`gdpr`, `ccpa`, `hipaa`, `soc2`, `pci_dss`,
  `iso27001`, `fedramp`, `lgpd`, `pipeda`, `appi`, `pdpa`) are disabled by
  default — enabled individually only when the project's `IDEA.md`
  explicitly declares the applicable value
- Tor hidden service is auto-enabled whenever a Tor binary is found on the
  system — no config flag to disable; outbound-via-Tor
  (`server.tor.use_network`) defaults to `false` and is a separate opt-in
  setting
- Tor directories are always `{config_dir}/tor/`, `{data_dir}/tor/`,
  `{log_dir}/tor.log` — never configurable, never hardcoded elsewhere; the
  dedicated Tor process runs as the same (post-privilege-drop) user as the
  server, as its child process

## KEY DECISIONS (pre-answered)
| Question | Answer | Spec Reference |
|----------|--------|----------------|
| Does the auth/session machinery in PART 11 apply to this project? | No — IDEA.md forbids accounts/sessions/admin panel; the only credential concept is `server.token` | IDEA.md non-goals; PART 11 |
| Does `users.db` (admins/users/sessions/password_resets/email_verifications/totp_secrets) still exist? | **No — deleted.** Was dead scaffold code contradicting both IDEA.md and AI.md's own "no admin web UI" statements; removed along with `GetUsersDB()`/`createUsersSchema()` | `src/database/database.go`; `TODO.AI.md` |
| Does a `config`/`config_meta` DB table belong in `server.db`? | No per `config-rules.md` ("DB never stores config") — removed, was dead/unused | `config-rules.md`; PART 5 |
| Database engine actually used | SQLite via `modernc.org/sqlite` (pure-Go driver, CGO-free), WAL mode, `_busy_timeout=5000`, single file (`server.db`) | `src/database/database.go` |
| Schema creation pattern | `CREATE TABLE IF NOT EXISTS` + `CREATE INDEX IF NOT EXISTS`, idempotent, no migration files — matches PART 10 | `src/database/database.go`; PART 10 |
| Connection pool sizing actually used | `SetMaxOpenConns(25)` / `SetMaxIdleConns(5)` on `server.db` | `src/database/database.go` |
| Is Tor hidden service (PART 31) implemented yet? | **Yes** — `src/tor` package (`config.go`, `torrc.go`, `dirs.go`, `binary.go`, `manager.go`, `service.go` + tests) implements the dedicated Tor process, v3 persistent onion via `ADD_ONION`, and `github.com/cretz/bine v0.2.0` is in `go.mod`; wired into `src/main.go` (start after listener confirmed accepting, stop first on shutdown) and `src/scheduler/tasks.go`'s `torHealthTask()`. Still open: privilege-drop-aware process ownership, `api-cli tor` subcommands, and PART 12 FQDN/`.onion` `security.txt` integration — tracked in `TODO.AI.md` | `src/tor/`; `go.mod`; PART 31 |
| CSP/security-header middleware location | `src/server/middleware.go` and `src/config/config.go` implement CSP/header config — no dedicated `src/security` package | `src/server/middleware.go`, `src/config/config.go` |
| Compliance posture for this project | IDEA.md declares no `audience`/`compliance`/`data_class`/etc. values → all `web.headers.*` stay at loose "everyone" defaults; all `server.compliance.*` flags stay `false` | IDEA.md, Compliance declarations; PART 11 |
| Which log is machine-parseable only? | `audit.log` — JSON only, no text format option | PART 11, Logging |

## TERMINOLOGY
| Term | Meaning |
|------|---------|
| Tier 1 / 2 / 3 | Public Endpoint Safety Principle's risk classification — never-public / always-public-safe / debug-only |
| Output Sanitization Pipeline | The mandatory chokepoint every public response passes through: allow-list fields, redact sensitive query params, strip internal IPs/paths, truncate, strip dev-only fields, constant-time finalize |
| `installation_secret` | Root 32-byte secret generated on first start; underlies `{security_id}` HMAC and the PGP private-key KDF — rotated only via `--maintenance secret rotate` |
| `{security_id}` | Rotating (48h) HMAC-derived token appearing only in `security.txt`'s `Contact:` line, gating the security-report mode of `/server/contact` |
| Well-known namespace | `/.well-known/**` — root-owned protocol/discovery namespace; never a general static-file bucket |
| Additive-only schema | Never `DROP`/rename a column; add new columns/tables, migrate data, deprecate old ones |
| Dedicated Tor process | The server's own Tor instance (never system Tor), started as the server's child process, using `127.0.0.1:auto` control/SOCKS ports |
| Flagged discrepancy | An issue found during rules-file authoring; the `users.db`/dead-schema discrepancy noted here was resolved by deletion — see banner note above |

## QUICK REFERENCE
- Canonical response shape: `{ok,data}` / `{ok,error,message,details?}`
  (PART 9); error-tier classification per Public Endpoint Safety Principle
  (PART 11)
- SQLite via `modernc.org/sqlite`, single file (`server.db`),
  `CREATE TABLE IF NOT EXISTS`, additive-only changes, no migrations
- Constant-time compare for every secret comparison; opaque IDs on every
  public-facing resource identifier
- Security headers + CSP emitted by default at loose "everyone" settings;
  tightened only via IDEA.md-declared compliance/audience values (none
  declared here)
- `robots.txt`, `security.txt` (RFC 9116), `llms.txt` always served;
  `/.well-known/**` never a general file bucket
- All compliance standards off by default; audit log is JSON Lines,
  append-only, `keep: none`
- Tor hidden service is implemented: `src/tor/` package + `bine` dependency,
  wired into `main.go`/`scheduler/tasks.go`; CLI subcommands and
  privilege-drop-aware ownership remain open per `TODO.AI.md`

---
For complete details, see AI.md PART 9, PART 10, PART 11, PART 31
