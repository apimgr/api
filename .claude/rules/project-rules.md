# Project Structure & Paths Rules (PART 2, 3, 4)

⚠️ **These rules are NON-NEGOTIABLE. Violations are bugs.** ⚠️

## CRITICAL - NEVER DO
- Never hardcode a bare `/path` in embedded code (Go/JS/templates/config) —
  always build a request-aware or `{fqdn}`-qualified URL; bare paths are only
  OK for internal router registration
- Never use relative paths in documentation when `{official_site}` is defined
- Never use `curl` without the standard `-q -LSsf` flag set in docs/scripts/CI
- Never use `/data/**` or `/config/**` paths outside of the Docker/container
  build — those are Docker-only, not native OS paths
- Never use `.yaml` for the config file name — it is always `server.yml`
- Never expose sensitive data (credentials, connection strings, internal
  paths/IPs, config internals) in `/server/healthz`, API responses, error
  messages, logs, or HTML/templates
- Never let a token/password be retrievable after first display — show once only

## CRITICAL - ALWAYS DO
- Follow the OS-specific path table exactly for privileged vs. non-privileged
  users on Linux, macOS, BSD, Windows, and Docker
- Use `{internal_org}/{internal_name}` in all native OS paths
- Use `{project_name}` subdirectory under Docker's `/config` and `/data` roots
- Keep README.md in sync with any feature, config, CLI flag, or deployment change
- Use `{official_site}` full URLs in documentation; `{fqdn}` in embedded code
- Validate all input; only ever persist valid data; never destroy valid data
  with invalid data
- Make every setting configurable via `server.yml`; support hot-reload for
  all settings except listen address/port/DB driver (which may require restart)
- Mask sensitive values in UI (`••••••••` or last 4 chars only)

## KEY DECISIONS (pre-answered)
| Question | Answer | Spec Reference |
|----------|--------|----------------|
| What is the config filename? | Always `server.yml`, never `.yaml` | PART 4 |
| Linux non-root logs dir? | `~/.local/log/{org}/{name}/` (NOT under `.local/share`) | PART 4, Linux User |
| Linux non-root backup dir? | `~/.local/share/Backups/{org}/{name}/` | PART 4, Linux User |
| Docker config/data paths? | `/config/{project_name}/`, `/data/{project_name}/`, `/data/log/{project_name}/` | PART 4, Docker/Container |
| Full URL or relative path in docs? | Full `{official_site}` URL if defined, else relative | PART 3, URL Standards |
| Full URL or relative path in Go/JS code? | `{fqdn}`-qualified via `BuildURL(r, ...)` (request-aware, proxy-header safe) | PART 3, Embedded Code URLs |
| Standard curl flags? | `-q -LSsf` (add `-I`/`-o`/`-H`/`-X`/`-d` as needed) | PART 3, curl Command Standard |

## TERMINOLOGY
| Term | Meaning |
|------|---------|
| `{official_site}` | Public canonical URL used in docs/examples |
| `{fqdn}` | Runtime-configured hostname used in embedded/generated links |
| `internal_org` / `internal_name` | Org/project identifiers used in native OS filesystem paths |
| Privileged path | Path used when running as root/Administrator |
| User path | Path used when running as a non-privileged user (XDG/AppData/Library) |
| Docker-only path | `/config` and `/data` roots, valid only inside a container |

## QUICK REFERENCE
- Config file: `server.yml` everywhere, at the OS- or container-appropriate config dir
- SQLite DB always lives under `.../db/server.db` on every OS
- Docs use `{official_site}/path`; code uses `{fqdn}/path` or `BuildURL`
- Every curl example: `curl -q -LSsf {url}` (plus method/data/header flags as needed)
- `src/paths` implements `GetDefaultDirs()`/`GetBackupDir()` per this table —
  container branch must include the `{project_name}` subdirectory

---
For complete details, see AI.md PART 2, PART 3, PART 4
