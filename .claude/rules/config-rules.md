# Configuration Rules (PART 5, 6, 12)

⚠️ **These rules are NON-NEGOTIABLE. Violations are bugs.** ⚠️

## CRITICAL - NEVER DO
- Never use `.yaml` as the config filename — always `server.yml`; auto-migrate
  `server.yaml` → `server.yml` on startup if found
- Never parse booleans with `strconv.ParseBool()` — always
  `config.ParseBool()` / `config.IsTruthy()` (accepts yes/no, on/off,
  enable/disable, oui/non, etc. — see full truthy/falsy table in PART 5)
- Never put YAML comments inline — always on the line above (exception:
  GitHub Actions SHA-pin `# vX.Y.Z` annotations stay inline for Renovate)
- Never fail startup on an invalid config value — warn and replace with the
  default; the server must always start with sane defaults
- Never store user accounts or operator-editable configuration in the
  database — `server.yml` is the sole source of truth; DB holds resource
  state, owner tokens, audit log only
- Never let debug mode (`--debug`/`DEBUG=true`) bypass `server.token` auth or
  any security check, in ANY mode including production — debug affects
  verbosity/diagnostics ONLY
- Never expose config internals, secrets, or webhook signing secrets in
  `/debug/config`, `/server/healthz`, or any response — sanitize/mask; a
  webhook secret is returned once at creation time, never again
- Never honor `X-Forwarded-*` headers from a peer outside `trusted_proxies`
  (private ranges + configured `additional` allow-list) — drop and fall back
  to `r.Host`/`r.RemoteAddr`
- Never provide a runtime API to change `server.port` — config file edit +
  restart only
- Never put `Retry-After` or other operational metadata in a maintenance/
  rate-limit JSON body — those go in headers (`Retry-After`,
  `X-Maintenance-*`), never top-level body fields

## CRITICAL - ALWAYS DO
- Resolve mode via: `--mode` CLI flag > `MODE` env var > default
  (`production`); resolve debug via: `--debug` CLI flag > `DEBUG` env var >
  `--mode debug`/`MODE=debug` alias > default (`false`)
- Treat `--mode debug`/`MODE=debug` as an alias for `development` + debug on
  — but an explicitly set `DEBUG` env var or `--debug` flag always wins over
  the alias
- On first run with no configured port, pick a random unused port in
  64000-64999 and persist it to `server.yml`; reuse the saved port on every
  subsequent start
- Enter maintenance mode ONLY for the two true critical errors: database
  connection failure, or inability to write files (disk full/permissions).
  All other errors are recoverable — the server always attempts self-healing
  and never enters maintenance for them
- In maintenance mode: reject writes with `503` + canonical error body,
  keep reads working, retry self-healing every 30s, auto-recover and notify
  when the issue resolves
- Categorize every config change on file-watch as hot-reloadable, graceful-
  reload (component swapped without downtime), or restart-required — never
  silently apply a restart-required change live
- Use Argon2id for config/backup passwords and SHA-256 for API tokens (see
  root CLAUDE.md Top-19 rule #1/#6) — PART 5/12 does not override this
- Validate and normalize every path (config values, CLI flags, API params,
  HTTP request paths) through `SafePath`/`validatePath` before use

## KEY DECISIONS (pre-answered)
| Question | Answer | Spec Reference |
|----------|--------|----------------|
| Config filename? | `server.yml`, auto-migrated from `server.yaml` | PART 5 |
| Where does config live? | `server.yml` only; DB never stores config/accounts | PART 5 |
| How to parse booleans? | `config.ParseBool()`/`IsTruthy()`, never `strconv.ParseBool()` | PART 5 |
| Invalid config value at load? | Warn + replace with default, never fail startup | PART 12 |
| Mode/debug resolution order? | CLI flag > env var > alias > default | PART 6 |
| Does `--debug` bypass auth? | Never, in any mode | PART 6 |
| What triggers maintenance mode? | Only DB connection error or file-write failure | PART 5 |
| Can `server.port` change without restart? | No — config edit + restart, no runtime API | PART 5, 12 |
| Which settings hot-reload? | `rate_limit.*`, `cors.*`, `branding.*`/`seo.*`, `logging.level`, `notifications.*`/`smtp.*`, `security.headers.*` | PART 11 (referenced from PART 5) |
| Which settings require restart? | `server.port`, `server.address`, `ssl.*`, `server.daemonize`, `database.*`, `tor.*` | PART 11 (referenced from PART 5) |
| When are `X-Forwarded-*` headers trusted? | Only when immediate peer IP is in `trusted_proxies` | PART 12 |

## TERMINOLOGY
| Term | Meaning |
|------|---------|
| `server.yml` | Sole source-of-truth YAML config file (never `.yaml`) |
| Hot-reload | Setting applied immediately from a file-watch change, no restart |
| Graceful reload | Component (log file, router, `http.Server`) swapped/reopened live, no full process restart |
| Requires restart | Setting that needs a full process restart to take effect (bound socket, TLS listener, DB pool, Tor child process) |
| Maintenance mode | Read-only, self-healing state entered only on a critical error |
| Critical error | Database connection failure or file-write failure — the ONLY two triggers |
| Debug mode | `--debug`/`DEBUG=true`; adds diagnostics/verbosity, never disables auth or security |
| Trusted proxy | Peer IP (private range or `trusted_proxies.additional`) whose `X-Forwarded-*` headers are honored |

## QUICK REFERENCE
- Config is always `server.yml` at the OS-appropriate dir; never `.yaml`
- Booleans always via `config.ParseBool()`/`IsTruthy()`
- Invalid config value → warn + default, never crash
- Mode/debug: CLI flag > env var > alias > default; debug never touches auth
- Restart-required settings: port, address, `ssl.*`, daemonize, `database.*`, `tor.*`
- Maintenance mode is only for DB/file-write critical errors; writes get `503` + `Retry-After` header, never a body field
- `X-Forwarded-*` headers only honored from `trusted_proxies` peers

---
For complete details, see AI.md PART 5, PART 6, PART 12
