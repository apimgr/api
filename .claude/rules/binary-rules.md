# Binary & Client Rules (PART 7, 8, 32)

⚠️ **These rules are NON-NEGOTIABLE. Violations are bugs.** ⚠️

**IDEA.md override:** IDEA.md non-goals declare no user accounts and no
resource ownership of any kind — every endpoint is public and anonymous.
Base-spec text in PART 8/32 that mentions "resource owner tokens" (issued
per-resource, distinct from `server.token`) does NOT apply to this project —
there are no user-owned resources to issue owner tokens for. The only token
concept that applies is the single operator `server.token` used for
sensitive/maintenance operations (PART 5, referenced from PART 8).

## CRITICAL - NEVER DO
- Never require CGO — `CGO_ENABLED=0` always; pure Go dependencies only
- Never ship runtime dependencies — single static binary, assets embedded via
  Go's `embed` package
- Never let the CLI binary's User-Agent change when the user renames the
  executable — it MUST always be `{project_name}-cli/{version}` (compiled in
  at build time via `-ldflags`), even when `--help`/`--version`/error text
  correctly reflect the renamed filename
- Never construct a client-side URL with raw `fmt.Sprintf`/string
  concatenation of user input — always encode path segments
  (`url.PathEscape`) and query values (`url.QueryEscape`/`url.Values.Encode`)
- Never invent new server CLI flags/subcommands outside the stdlib `flag`
  registered set in PART 8 — the server binary is single-command, no
  subcommands
- Never skip a platform in the 8-target release matrix
  (linux/darwin/windows/freebsd × amd64/arm64)
- Never make TUI unusable below 80x24 — phone-SSH-sized terminals are a
  required use case, not an edge case
- Never implement `--tui`/`--cli`/`--gui` flags — mode is auto-detected from
  TTY/environment, never operator-selected via flag

## CRITICAL - ALWAYS DO
- Server binary: no-argument default behavior is "initialize if needed, then
  start the server" — auto-create `server.yml` and required directories on
  first run, show a startup banner with URLs and version
- Handle SIGTERM/SIGINT (graceful shutdown), SIGHUP (config reload — never
  used to mean "restart"), consistent with PART 6's signal table; PID file
  enabled by default
- Client binary (`api-cli`) is REQUIRED for every project — no skip case
- Detect intent from bare arguments before requiring flags (stdin → file →
  directory → text detection order; explicit flags always override
  detection)
- Support `--output json|table|plain` on every command that returns
  structured data
- TUI auto-launches when appropriate (no explicit flag) and must be
  window-aware: detect terminal dimensions, handle resize, degrade
  gracefully below the documented breakpoints
- TUI theme must default to dark and mirror the server frontend's theme
  palette (PART 16)
- Use the standard exit-code table (0 success, 1 general error, 2 config
  error, 3 connection error, 4 auth error, 5 not found, 64 usage error)
- Build via `casjaysdev/go:latest` toolchain in CI, direct `go build
  -buildvcs=false -trimpath -ldflags "..."` — never `actions/setup-go`

## KEY DECISIONS (pre-answered)
| Question | Answer | Spec Reference |
|----------|--------|----------------|
| Does resource-owner-token behavior apply here? | No — IDEA.md declares no user-owned resources; only `server.token` (operator) exists | IDEA.md non-goals; PART 8 |
| Server CLI flag parser? | Actual implementation: stdlib `flag` package in `src/main.go` (help/version/mode/config/data/log/cache/backup/pid/address/port/baseurl/daemon/debug/color/lang/shell/status/service/maintenance/update) — matches PART 8 | `src/main.go` |
| Client CLI framework? | Actual implementation: a custom `Command`/registry pattern in `src/client/cmd/` (`Execute`, `register`, `findCommand`, `categoryCommands`) — NOT cobra/viper as PART 32 examples show. This is a deviation from the spec's illustrative code, not a violation of any NEVER-DO rule (PART 32 doesn't mandate cobra specifically), but it should be recorded so future edits match the actual pattern instead of reintroducing cobra | `src/client/cmd/root.go`, `registry.go` |
| Does the User-Agent follow the renamed binary? | No — always `{project_name}-cli/{version}` regardless of `os.Args[0]` | PART 32, User-Agent Rule |
| TUI library set | `github.com/charmbracelet/bubbletea`, `bubbles`, `lipgloss` — confirmed present in `go.mod` | PART 32, Required Libraries; `go.mod` |
| Is the PART 7/32 window-size (`SizeMode`) package implemented yet? | **Yes** — `src/common/terminal/size.go` (`SizeMode`, `TerminalSize`, `GetTerminalSize()`, `calculateMode()`, `Show*` helpers, 100% test coverage) plus `src/client/tui/layout.go` (`LayoutConfig`, `GetLayoutConfig()`, `GetSpacingForMode()`), wired into `src/client/tui/app.go`'s `Update()`/`applyLayout()`/`View()` so header/footer/border chrome degrades per breakpoint. Open follow-up: `MaxColumns`/`TruncateAt` unused (no multi-column list rendering yet) and no real sidebar widget renders for `ShowSidebar` modes — see `TODO.AI.md` | PART 7, PART 32 (Terminal Size Breakpoints) |
| Output formats required | JSON, table, plain — all three, selectable via `--output` | PART 32, Output Formats |
| Exit code for "not found"? | `5` | PART 32, Exit Codes |

## TERMINOLOGY
| Term | Meaning |
|------|---------|
| server | `api` binary — main service, runs single-command via stdlib `flag` |
| client | `api-cli` binary — REQUIRED CLI/TUI companion |
| Resource owner token | Per-resource auth token concept from base PART 8 — does NOT apply to this project (no user-owned resources) |
| `server.token` | The one operator-level token this project actually uses, for sensitive/maintenance ops |
| Smart argument detection | CLI infers stdin/file/directory/text intent from bare args instead of requiring flags |
| `SizeMode` | PART 7/32's terminal-breakpoint enum (Massive → Micro) — implemented in `src/common/terminal/size.go` |

## QUICK REFERENCE
- Single static binary, `CGO_ENABLED=0`, embedded assets, 8-platform matrix
- Server: no-arg default = init + start; SIGTERM/SIGINT graceful, SIGHUP reload
- Client: required for every project; stdlib-flag-style server, custom
  Command-registry-style client (not cobra) per actual `src/client/cmd/`
- User-Agent always `{project_name}-cli/{version}`, independent of binary rename
- All client URLs built through `url.PathEscape`/`url.QueryEscape` — never
  raw string concatenation
- TUI: bubbletea/bubbles/lipgloss, dark-default theme matching server
  frontend, degrades gracefully on small/phone-SSH terminals via
  `src/common/terminal` (`SizeMode`) + `src/client/tui/layout.go`
  (`LayoutConfig`/`GetLayoutConfig`/`GetSpacingForMode`), wired into
  `src/client/tui/app.go`
- CLI output: `--output json|table|plain`
- Standard exit codes: 0/1/2/3/4/5/64

---
For complete details, see AI.md PART 7, PART 8, PART 32
