# Binary Requirements Rules (PART 7, 8, 32)

⚠️ **These rules are NON-NEGOTIABLE. Violations are bugs.** ⚠️

## CRITICAL - NEVER DO
- Never build with CGO enabled — server/CLI are single static binaries, `CGO_ENABLED=0`, pure Go only (GUI cgo code is an isolated `gui` build tag, not the default build)
- Never embed security databases (GeoIP, blocklists, CVE, Trivy) in the binary — always download to `{data_dir}/security/` on first run and keep updated via the scheduler
- Never hand-roll flag parsing with manual `switch`/`os.Args` loops for the server's primary flag set — use stdlib `flag`; use `cobra`/`viper` only for the multi-command CLI client
- Never hardcode `localhost`, `127.0.0.1`, `0.0.0.0`, `[::1]`, or any static host/IP/port in code, templates, Swagger, or emails — always detect `{proto}`/`{fqdn}`/`{port}` per request
- Never display `0.0.0.0`, `127.0.0.1`, or `localhost` in banners/status output — show the most relevant valid FQDN/host/IP
- Never resolve `~`/`$HOME` again after privilege drop — the service account's HOME points at `{data_dir}`, so a late lookup nests user-style paths inside system data dirs
- Never let `--service start` respect the `daemonize` config setting — it always auto-detects the service manager and does the right thing
- Never write a PID file when running in a container (`isContainer()` true) — the runtime supervises the process and namespace-local PIDs are meaningless across namespaces
- Never implement `--tui`, `--cli`, `--gui`, or a `--mode tui/cli/gui` flag on the CLI — display mode is always auto-detected; `--mode` is reserved for `production`/`development` only
- Never attempt GUI over SSH/mosh — remote sessions always use TUI/CLI even if X11 forwarding (`DISPLAY`) is present
- Never use `strconv.ParseBool()` for CLI/Agent/server boolean flags — always use `config.ParseBool()`/`config.IsTruthy()`
- Never construct URLs with raw user input (`fmt.Sprintf` + unescaped values) — always use `url.PathEscape`/`url.QueryEscape` via the shared `urlutil` helpers
- Never disable bold/underline/italic, Unicode box-drawing, or output structure because of `NO_COLOR` — it only disables ANSI colors and emojis; use `TERM=dumb` to disable all ANSI escapes
- Never store the `server.token` (operator token) in the `api_tokens` DB table or in plaintext anywhere except `server.yml` — only its SHA-256 hash is cached in memory
- Never let API routes (`/api/...`) accept cookie-based auth — only `Authorization` header; only existing plain-POST web management forms may fall back to the `owner_token` cookie

## CRITICAL - ALWAYS DO
- Always ship a single self-contained static binary with `embed`-based templates/static/data assets and zero runtime dependencies
- Always auto-create `server.yml` and required directories on first run with sane defaults; show a banner with URLs and version
- Always handle SIGTERM/SIGINT/SIGQUIT/SIGRTMIN+3 (Docker STOPSIGNAL) with graceful shutdown; ignore SIGHUP (config auto-reloads via file watcher); SIGUSR1 reopens logs, SIGUSR2 dumps status — Unix only, Windows uses `os.Interrupt` only
- Always detect stale PID files on every startup and verify process identity (exact binary-name match) to guard against PID reuse
- Always bind privileged ports (<1024) while still root, then drop privileges to the `{project_name}` system user as early as possible, and verify the drop succeeded
- Always resolve directory mode (system vs user) ONCE from EUID at process start, before any privilege drop, and cache it for the process lifetime
- Always show the ACTUAL (possibly renamed) binary name in `--help`/`--version`/error messages, but hardcode `{project_name}` internally for User-Agent, default paths, config keys, and DB identifiers
- Always respect `NO_COLOR` (any non-empty value disables ANSI colors + emojis) and `TERM=dumb` (disables all ANSI escapes, forces CLI mode, forces plain text fallbacks for spinners/progress/tables)
- Always detect display mode (GUI/TUI/CLI/Headless) per the priority: SSH/mosh → TUI; local display → GUI; terminal only → TUI/CLI; none → Headless (server) or error (CLI)
- Always strip `:80` for HTTP and `:443` for HTTPS when building URLs; only non-standard ports appear
- Always resolve `{fqdn}`/`{proto}`/`{port}` from (in order) reverse-proxy headers, then `DOMAIN` env var, then `os.Hostname()`, then public IP, then localhost fallback — reverse-proxy preferred
- Always validate FQDNs with `golang.org/x/net/publicsuffix`; reject IPs and single-label hosts; block internal/dev-only TLDs (`.local`, `.test`, `.internal`, etc.) in production mode
- Always give every CLI flag a corresponding `cli.yml` config setting, and only persist `--server`/`--token` flag values to config when the current config value is empty or invalid
- Always support `--shell completions [SHELL]` and `--shell init [SHELL]` (auto-detect `$SHELL` when omitted) on every binary — built into the binary, not separate files
- Always keep CLI runtime state (config/data/cache/logs) in the invoking user's XDG/profile directories, even when the CLI binary is installed system-wide or run by root
- Always create parent directories and set correct permissions (`0700`/`0600` user mode, `0755`/`0644` root mode) before writing any file

## Key Rules Summary

**Build & assets (PART 7):** Single static binary, `CGO_ENABLED=0`, pure Go. Embedded: `src/server/template/`, `src/server/static/`, `src/data/`. External/never-embedded: GeoIP (ip-location-db mmdb files), IP/domain blocklists, CVE (NVD), Trivy DB — all under `{data_dir}/security/*`, downloaded on first run, graceful degradation on download failure, kept fresh by the scheduler.

**Display detection (PART 7):** `src/common/display/detect.go` defines `DisplayMode` (Headless/CLI/TUI/GUI) and `DisplayEnv`, with per-platform `detect_unix.go`/`detect_windows.go`. `TERM=dumb` forces CLI mode, no ANSI/emoji/cursor control/spinners — use `[OK]`/`[ERROR]` text and `NN% complete` instead. Shared modules under `src/common/`: `display/`, `theme/`, `terminal/` (size breakpoints Micro→Massive, `GetTerminalSize()`), `banner/`, `version/`.

**Server CLI flags (PART 8):** Binary `{project_name}`, single-command (no subcommands), stdlib `flag`. Core flags: `--help --version --shell --mode --config --data --cache --log --backup --pid --address --port --baseurl --status --service --daemon --debug --color --lang --maintenance --update`. Directory flags default to `/etc|/var/lib|/var/cache|/var/log|/mnt/Backups` (root) or `~/.config|~/.local/share|~/.cache|~/.local/log|~/.local/share/Backups` (user) under `{internal_org}/{internal_name}`; all auto-create with proper perms and fail fast if not writable. `--backup` env fallback is `BACKUP_DIR`; system mode never falls back to a `$HOME`-derived path.

**PID files (PART 8):** Enabled by default, skipped entirely in containers. Stale/PID-reuse detection required via exact binary-name match (`/proc/{pid}/exe` on Linux, `ps`/Windows API elsewhere).

**Startup sequence (PART 8):** Immediate-exit flags → service/maintenance/update subcommands → root setup (create user/dirs/chown/perms, determine ports, bind privileged ports) → drop privileges → user-mode directory setup → logging → PID file → load config (first-run generates `server.yml`) → DB init → scheduler start → Tor start (optional) → HTTP listeners → signal handlers → ready.

**Daemonization (PART 8):** Foreground by default. `--service start` always auto-detects the manager (systemd/launchd/runit/s6/container/docker → foreground; sysv/rcd → daemonize) and ignores the `daemonize` config. Manual `--daemon` forks+`setsid()` on Unix; unsupported on Windows (use Windows Service instead).

**Config reload (PART 8):** File-watched `server.yml`; hot-reloadable (rate limits, CORS, branding, log level, notifications, headers) vs restart-required (`server.port`, `server.address`, `ssl.*`, `database.*`, `tor.*`, `server.daemonize`) — restart-required changes flag `pending_restart` in `/server/healthz`, never applied live.

**Database (PART 8):** SQLite (`server.db`) default, zero-config; optional remote libsql/Turso with SQLite local cache. Tables: `config`, `config_meta`, `rate_limits`, `audit_log`, `scheduler_tasks`, `scheduler_history`, `backups`, `api_tokens`. Two-tier auth: `server.token` (global operator, in `server.yml`, SHA-256 compared in memory, never in DB) and per-resource owner tokens (`tok_` + 32 base62 chars, SHA-256 hashed in `api_tokens`, shown once). JSON field is always `owner_token`; browser cookie/localStorage key is `{project_name}_owner_token_XXXXXX`.

**URL/FQDN detection (PART 8):** Three canonical vars `{proto}`/`{fqdn}`/`{port}` (+ `{baseurl}`, `{address}`, `{app_mode}` etc.) resolved per-request via reverse-proxy headers first (`X-Forwarded-Host/Proto/Port`, etc.), with smart domain learning/live-reload for wildcard inference. Client IP resolution: `CF-Connecting-IP` → `True-Client-IP` → `X-Real-IP` → `X-Forwarded-For` (leftmost) → `X-Client-IP` → `RemoteAddr`, and proxy headers only honored when the immediate peer is a trusted proxy. Every request gets a UUID v4 `X-Request-ID`.

**Client/CLI (PART 32):** Binary `{project_name}-cli`, config at `~/.config/{internal_org}/{internal_name}/cli.yml` (`0600`), built by the same Makefile as the server. Open API model — no auth required for public GETs; tokens are for ownership. Server discovery/auto-update mirrors PART 22's self-update flow via `/api/autodiscover` (`cli_versions`, `cli_min_version`), SHA-256 verified, atomic swap, then re-exec.

**CLI mode detection (PART 32):** No args in an interactive terminal → TUI (default, full app). Command/args present → CLI text mode. Piped/non-interactive → plain output. `-h`/`-v`/`--help`/`--version` always exit immediately, never launch TUI. Config override in `cli.yml` (`display.mode: auto|gui|tui`) exists for accessibility/preference, auto-detection stays the default. CLI is the ONLY binary with a full GUI/TUI app and setup wizard (native GTK4/Qt6 on Linux/BSD, Cocoa on macOS, Win32/WinUI on Windows — never Electron/webviews); server only shows status banners, no wizard, configured by editing `server.yml`.

**CLI responsive design (PART 32):** Same `terminal.SizeMode` breakpoints as PART 7 (Micro <40 cols → Massive 400+), full GUI DPI-scaled layouts for 4K+/1440p/1080p/720p/mobile. Universal flags: `--help/-h`, `--version/-v`, `--color`, `--lang` (only `-h`/`-v` have short forms). `--debug` is required on all binaries, not optional. Boolean parsing must use the shared truthy/falsey table, never `strconv.ParseBool`.

**CLI config/auth (PART 32):** Every setting configurable via `cli.yml` (server, auth, output, tui, logging, cache, defaults sections). Config precedence: CLI flag > env var (`{PROJECT_NAME}_*`) > config file > compiled default. `--server`/`--token` only persist to config when current value is empty/invalid. User-Agent is always `{project_name}-cli/{version}` regardless of renamed binary. Exit codes: 0 success, 1 general, 2 config, 3 connection, 4 auth, 5 not found, 64 usage error.

**When to build a CLI (PART 32):** Required for all projects per this spec's `client is REQUIRED`. Decision guidance favors CLI for developer/sysadmin/automation-facing projects and skips it for purely visual/consumer-facing products — but for `api`/`apimgr`, the CLI is mandatory regardless.

For complete details, see AI.md PART 7, 8, 32
