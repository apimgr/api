# Project Structure Rules (PART 2, 3, 4)

⚠️ **These rules are NON-NEGOTIABLE. Violations are bugs.** ⚠️

## CRITICAL - NEVER DO
- Never use any license other than MIT for `api`; never depend on GPL/AGPL/LGPL (copyleft) licensed packages
- Never omit `LICENSE.md` from the project root, and never let its embedded third-party license section go stale
- Never hardcode `project_name`/`project_org` — always infer from `git remote get-url origin` or directory path
- Never change `internal_name` after first-time setup — it is frozen even across a project rename
- Never assume the current working directory is project root — resolve paths via `git rev-parse --show-toplevel` or the binary's own path
- Never use `.yaml` for the config file — always `server.yml`
- Never use `github.com/mattn/go-sqlite3`, `github.com/lib/pq`, `github.com/ooni/go-libtor`, `github.com/dgrijalva/jwt-go`, `github.com/gorilla/mux`, `github.com/go-redis/redis`, or any CGO-requiring library
- Never run `go` directly — use `make dev` / `make build` / `make test` / `make local`
- Never mix runtime directory purposes — user-editable config never goes in `data_dir`, app-managed data never goes in `config_dir`
- Never commit `binaries/`, `releases/`, `volumes/`, or `docker/rootfs/` — all gitignored
- Never skip AI-config directories (`.claude/`, `.cursor/`, `.aider/`, `.ai/`, `.windsurf/`) in `.gitignore`/`.dockerignore` — they are regenerated from AI.md, not committed
- Never use Docker-only paths (`/data/**`, `/config/**`) on native Linux/macOS/Windows/BSD — those are container-only

## CRITICAL - ALWAYS DO
- Always include `LICENSE.md` with the MIT template plus an embedded third-party licenses section (compact table for 10+ deps, full text only when the license requires it, e.g. BSD-3-Clause non-endorsement clause)
- Always add the GitHub-detectable license badge: `[![License](https://img.shields.io/github/license/apimgr/api)](LICENSE.md)`
- Always update `LICENSE.md` when a dependency is added, removed, or upgraded (verify license hasn't changed)
- Always use `modernc.org/sqlite` (pure Go, `CGO_ENABLED=0`) for SQLite, with driver aliases `sqlite`/`sqlite2`/`sqlite3` all normalizing to `sqlite`
- Always use `github.com/tursodatabase/libsql-client-go` for libSQL/Turso (remote-only, requires a URL)
- Always target all 4 OSes (Linux, BSD, macOS, Windows) and both architectures (AMD64, ARM64)
- Always use the latest stable Go version — never pin a specific version in go.mod, Docker, or CI
- Always place all source under `src/`, all Docker files under `docker/`, all ReadTheDocs content under `docs/`
- Always start `.gitignore` with the two required header lines (`# gitignore created on MM/DD/YY at HH:MM` then literal `ignoredirmessage`)
- Always keep `.claude/rules/*.md` (13 files) in sync with AI.md PART groupings

## Key Rules Summary

### License (PART 2)
- MIT only; `LICENSE.md` required in root; copyright holder is `apimgr` (or named individual/org)
- Third-party attribution required for MIT/Apache-2.0/BSD/ISC/MPL-2.0; optional for public-domain/CC0/WTFPL; GPL family forbidden entirely
- Use `go-licenses csv ./...` / `go-licenses save ./...` (pre-installed in `casjaysdev/go:latest`) to scan; CI should fail the build on any GPL/AGPL/LGPL match
- Docker license metadata goes via OCI annotation `org.opencontainers.image.licenses=MIT` at build time, not a Dockerfile `LABEL`

### Variables (PART 3)
- `{project_name}` = `api` (lowercase, may be renamed later), `{project_org}` = `apimgr`, `{internal_name}` = `api` (frozen forever), `{internal_org}` = `apimgr`
- UPPER_SNAKE forms (`{PROJECT_NAME}` etc.) are for env vars/Makefile only
- `{plist_name}` (macOS) = `io.github.apimgr.api`
- Recommended local path: `~/Projects/github/apimgr/api` (this repo already follows it)

### Directory Layout (PART 3)
- Standard tree: `.github/workflows/`, `.claude/rules/` (13 required files), `docs/` (MkDocs/ReadTheDocs only), `src/`, `scripts/`, `tests/` (`run_tests.sh`, `docker.sh`, `incus.sh`), `docker/` (`Dockerfile`, `Dockerfile.dev`, 3 compose files, `rootfs/`), `binaries/`, `releases/`, `volumes/` (all four gitignored)
- Root files: `README.md`, `LICENSE.md`, `AI.md`, `TODO.AI.md`, `TODO.md`, `PLAN.AI.md`, `PLAN.md`, `Jenkinsfile`, `release.txt`, `site.txt`
- `.dockerignore` excludes git/CI/volumes/binaries/releases/tests/docs/Makefile/IDE/AI-config, but MUST keep `src/`, `go.mod`, `go.sum`, `docker/` (including `docker/rootfs/`)

### Path Convention (PART 3)
- All paths are relative to project root (`git rev-parse --show-toplevel`), never `$PWD`
- Scripts resolve root programmatically (`BASH_SOURCE`-relative in bash, `os.Executable()` in Go)

### Go Toolchain (PART 3)
- Router: `go-chi/chi/v5`; Tor: `cretz/bine`; WebSocket: `gorilla/websocket`; CORS: `rs/cors`; scheduler: `go-co-op/gocron/v2`; rate limit: `golang.org/x/time/rate`; validation: `go-playground/validator/v10`; cache: `redis/go-redis/v9` (Valkey/Redis) or `bradfitz/gomemcache`; YAML: `gopkg.in/yaml.v3`; UUID: `google/uuid`
- All builds/tests happen inside `casjaysdev/go:latest` (Docker) — never `setup-go` action, never a pinned Go version

### OS-Specific Paths (PART 4)
- Linux privileged: binary `/usr/local/bin/api`, config `/etc/apimgr/api/server.yml`, data `/var/lib/apimgr/api/`, logs `/var/log/apimgr/api/server.log`, PID `/var/run/apimgr/api.pid`, systemd unit `/etc/systemd/system/api.service`
- Linux user: config `~/.config/apimgr/api/server.yml`, data `~/.local/share/apimgr/api/`, cache `~/.cache/apimgr/api/`, logs `~/.local/log/apimgr/api/`
- macOS privileged: config `/Library/Application Support/apimgr/api/`, LaunchDaemon `/Library/LaunchDaemons/io.github.apimgr.api.plist`
- macOS user: config `~/Library/Application Support/apimgr/api/`, LaunchAgent `~/Library/LaunchAgents/io.github.apimgr.api.plist`
- BSD privileged: config `/usr/local/etc/apimgr/api/`, data `/var/db/apimgr/api/`, rc.d script `/usr/local/etc/rc.d/api`
- Windows privileged: `%ProgramData%\apimgr\api\`; user: `%AppData%\apimgr\api\` (config) / `%LocalAppData%\apimgr\api\` (data/cache/logs)
- Docker/container only: config `/config/api/server.yml`, data `/data/api/`, logs `/data/log/api/`, SQLite `/data/db/sqlite/`, internal port `80`
- Every OS follows the same subdirectory shape under the app root: config file is always `server.yml`; SSL under `ssl/` (`letsencrypt/`, `local/`); security DBs under `security/` (`geoip/`, `blocklists/`, `cve/`, `trivy/`); SQLite DB under `db/server.db`
- Runtime directory purpose: `{config_dir}` = user-editable, `{data_dir}` = app-managed, `{log_dir}` = logs, `{backup_dir}` = backup archives

For complete details, see AI.md PART 2, 3, 4
