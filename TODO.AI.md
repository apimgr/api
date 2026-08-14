# TODO (AI-tracked)

Backlog surfaced by the project health audit (2026-08-05). Items here are
either decision-level (need an owner call before implementing) or larger than
a safe mechanical audit fix. Security/logic/doc fixes that were safe and
mechanical have already been applied in-tree and are not listed here.

## Frontend sub-tool pages (resolved — stale)

- Verified 2026-08-06: all 239 entries in `toolPages()` (215 working tools +
  24 permanent `501` stubs) already have both a route (`server.go`) and a
  template (`template/page/tools/{category}/{tool}.tmpl`) on disk. Diffed
  every `href="/{category}/{tool}"` link in all 21 category templates against
  `toolPages()` — zero missing. The "~240 missing" figure was a stale
  carryover from an earlier repo state. The `Count` badges in
  `allCategories()` (server.go) overstate per-category totals (e.g. crypto
  claims 147, 12 are named) — that's future scope/marketing copy, not a
  missing-route bug, and is out of scope for this item.

## Known permanent API gaps (documentation, not work)

- 28 endpoints intentionally return `501 NOT_SUPPORTED` because the behavior is
  a declared IDEA.md non-goal or falls outside the free/keyless trust boundary
  (e.g. `language/detect`, `language/translate`, `research/extract`,
  `research/bibtex`). Each has a real wired route and a matching frontend page
  rendered via `template/page/tools/unsupported.tmpl`. These are permanent by
  design — listed here only so the `src/server/server.go` references resolve.
  Do NOT "implement" them; doing so would violate IDEA.md scope.

## GraphQL resolver is a stub (resolved 2026-08-06)

- Replaced the hardcoded `executeQuery()` placeholder with a real hand-rolled
  GraphQL document parser (`src/graphql/parser.go`) and an executor that
  resolves selections against `BuildSchema()`'s actual `ResolveFunc` tree
  (aliases, nested selection filtering, literal/variable arguments). Added
  the previously SDL-only, resolver-missing mutations (`textReverse`,
  `textBase64Encode`, `textBase64Decode`, `textSlug`, `textHash`,
  `convertTimezone`) so the introspection SDL matches the executable schema,
  backed by the real `service/text` and `service/datetime` packages. The
  response-write error at the handler is now logged instead of ignored.
  `go test -cover ./src/graphql/...` passes at 75.4% coverage; full-project
  `go test ./...` and `gofmt`/`go vet` are clean.

## Docs completeness (ReadTheDocs) — resolved 2026-08-06

- Created `docs/security.md` (rate limiting, `security.txt`/
  `.well-known/security.txt`, `/server/contact`) and `docs/integrations.md`
  (`.well-known` discovery, API description formats, outbound providers),
  grounded in `src/server/server.go`/`ratelimit.go`/`config.go`.
- Deleted `docs/admin.md` (fictional web admin panel content — dashboard,
  2FA, multi-user admin accounts). Its legitimate operator-CLI surface
  (`--service`, `--maintenance`, monitoring, logs, troubleshooting) was
  folded into a new "Administration" section in `docs/cli.md`. Removed all
  other `docs/*.md` references to a web admin panel/UI (`index.md`,
  `api.md`, `configuration.md`, `installation.md`, `development.md`).
- `docs/configuration.md` now documents `API_BACKUP_PASSWORD` (real,
  `src/scheduler/tasks.go`). `SMTP_*` env vars are explicitly documented as
  NOT implemented (see gap below) rather than fabricated.

## SMTP env var / config wiring gap (code, not docs)

- `src/email/email.go` implements an SMTP client (`SMTPHost`, `SMTPPort`,
  `Username`, `Password`, `AutoDetectSMTP()`) but it is never wired into
  `src/config/config.go` or server startup — zero call sites outside the
  package itself. `SMTP_HOST`/`SMTP_PORT`/`SMTP_USERNAME`/`SMTP_PASSWORD`/
  `SMTP_TLS`/`SMTP_FROM_NAME`/`SMTP_FROM_EMAIL` (features-rules.md Email
  section) are not read anywhere. This means every SMTP-auto-detect/email
  notification feature in features-rules.md is currently unimplemented.
  Needs a decision: implement the config wiring, or scope it out for now.

## `--maintenance update`/`setup` reference a nonexistent `/admin` URL (code bug)

- `src/main.go`'s `--maintenance update` and `--maintenance setup` command
  handlers print messages pointing at `/admin` and `/admin/setup` web paths.
  No such routes exist (confirmed via `src/server/server.go`), and this
  contradicts the IDEA.md non-goal "no admin web panel". Needs a fix to the
  printed message text (point at the CLI/API instead).

## Low test coverage (below the 60% gate) — resolved 2026-08-06

- Added table-driven tests to all 11 packages. Overall project coverage is
  now 75.6% (`go test -cover ./...`). Ten of eleven packages clear 60%:
  `language` 2.2%→84.1%, `sysservice` 6.3%→63.1%, `tor` 17.8%→60.7%,
  `research` 21.3%→97.3%, `parse` 27.6%→91.6%, `convert` 31.8%→94.4%,
  `datetime` 33.0%→98.2%, `network` 47.3%→63.2%, `math` 53.3%→97.8%,
  `geoip` 55.0%→79.3%.
- `src/paths` remains below the gate (31.6%→33.2%): `GetDefaultDirs`,
  `GetCacheDir`, `GetBackupDir`, `DefaultPIDPath` early-return via
  `IsRunningInContainer()`, which is unconditionally true inside the
  mandated `casjaysdev/go:latest` test container (`/.dockerenv` present) —
  their OS-specific (Windows/macOS/BSD/Linux) and root-vs-user branches are
  structurally unreachable in-container without mocking `os.Stat`/
  `os.Geteuid`, which would require touching source logic (out of scope for
  a test-only pass). Needs either an injectable filesystem/euid abstraction
  in `src/paths` (source change, own commit) or an accepted permanent
  exception documented in IDEA.md.

## Inline `style=""` across tool page templates — resolved 2026-08-07

- All 216 `src/server/template/page/tools/**/*.tmpl` files converted:
  breadcrumb-style notes (`style="color: var(--text-muted); font-size:
  0.875rem;"`) moved to a shared `.tool-note` class in `public.css`; result
  containers using `style="display:none;"` swapped for the `hidden`
  attribute. `app.js` updated to set `resultDiv.hidden = false` instead of
  `resultDiv.style.display = 'block'` in all 5 tool-execution functions
  (also fixed a pre-existing bug: `executeTool()` was missing the unhide
  call its 4 sibling functions had). Also deduplicated the modal-backdrop/
  mobile-nav-overlay scrim color into a new theme-invariant
  `--overlay-color` CSS variable in `common.css`. Verified
  `grep -rl 'style="' src/server/template` returns zero matches project-wide.

## Inline event handlers / inline `<script>` blocks — resolved 2026-08-14

- Removed all 6 remaining `onclick=`/`onsubmit=` inline handlers
  (`page/error.tmpl`, `page/index.tmpl`, and the 4 legacy tool pages
  `tools/datetime/now.tmpl`, `tools/network/ip.tmpl`,
  `tools/crypto/password.tmpl`, `tools/text/uuid.tmpl`) in favor of
  `data-back`/`data-favorite`/`data-copy` attributes wired centrally in
  `app.js`. The 4 legacy tool pages also had a trailing inline `<script>`
  block (also forbidden by frontend-rules.md) defining their own
  `execute*Tool()` function — these were moved verbatim into `app.js` and
  wired via a `legacyToolForms` id→handler map in the existing
  `DOMContentLoaded` listener. `network/ip.tmpl`'s form/result ids were
  renamed `ip-form`/`ip-result` → `network-ip-form`/`network-ip-result` to
  avoid an id collision with `geo/ip.tmpl` and `osint/ip.tmpl` (both also
  use `id="ip-form"` wired generically via `data-template`). Verified
  `grep -rlE 'on(click|change|submit|input|load)=' src/server/template` and
  `grep -rl '<script>' src/server/template` both return zero matches;
  `go build ./... && go vet ./...` clean in `casjaysdev/go:latest`.

## `network/ip.tmpl` arbitrary-IP lookup calls a nonexistent route (pre-existing bug)

- The page's form allows entering any IP and calls
  `/api/v1/network/ip/{ip}` when non-empty, but `src/server/server.go`
  only registers `r.Get("/ip", apiNetworkCallerHandler)` under `/network/`
  — there is no `/ip/{ip}` path-param route. `apiNetworkCallerHandler`
  (`src/server/api_network.go`) takes no `ip` argument at all and only
  ever returns the caller's own request info, so submitting any IP other
  than blank silently returns caller info instead of a lookup for that
  IP (or 404s, depending on router matching) — this predates the
  2026-08-14 inline-handler cleanup, which preserved the existing (broken)
  call shape unchanged since fixing it requires a product decision: add a
  real `/network/ip/{ip}` backend route (and GeoIP-backed lookup), or
  restrict the frontend field to caller-IP-only and drop the free-text
  input. Needs an owner call before implementing.

## `error.tmpl` inline `<style>` block (separate violation, not yet fixed)

- `page/error.tmpl`'s `{{define "page-scripts"}}` block contains an inline
  `<style>` tag (`.error-section`/`.error-content`/etc. rules) — a
  frontend-rules.md violation distinct from the inline-event-handler/
  inline-`<script>` cleanup done 2026-08-14 (out of scope for that fix).
  Needs the rules moved into `public.css` (or a page-specific stylesheet
  per the existing CSS-organization convention) as its own fix.

## Bugs found during coverage work (not fixed, need triage)

- `src/service/parse/parse.go` `parseLogLine`: timestamp layouts are
  matched via a fixed-width prefix slice (`remaining[:len(layout)]`) sized
  to the layout string's length, not the actual value's length — RFC3339
  timestamps with a `Z` suffix (shorter than the 25-char layout) are
  silently unparseable.
- `src/sysservice/service.go` `installRunit()` (~line 249): discards the
  `os.Symlink` return value — a failed symlink still reports install
  success while leaving the service unlinked from `/var/service`.
- `src/service/network/network.go` `whoisQuery`: hardcodes port 43
  internally via `net.JoinHostPort(server, "43")`, so a `server` argument
  that already contains a port fails. May be intentional (WHOIS convention
  is always port 43) — needs a design decision, not an assumed fix.
