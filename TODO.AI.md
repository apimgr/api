# TODO (AI-tracked)

Backlog surfaced by the project health audit (2026-08-05). Items here are
either decision-level (need an owner call before implementing) or larger than
a safe mechanical audit fix. Security/logic/doc fixes that were safe and
mechanical have already been applied in-tree and are not listed here.

## Pre-existing test flakiness: weather handlers hit a live external API

- `TestAPIWeatherCurrentHandler`, `TestAPIWeatherForecastHandler`,
  `TestAPIWeatherAstronomyHandler`, `TestAPIWeatherHistoricalHandler`,
  `TestAPIWeatherHourlyHandler`, and `TestAPIWeatherAlertsHandler` in
  `src/server/api_utils_test.go` call through to `src/service/weather` which
  hits the real `open-meteo.com`/`geocoding-api.open-meteo.com` endpoints
  with no mock — confirmed by direct reproduction (2026-09-02): the same
  test passes in isolation but intermittently 502s under the full-suite run,
  and `GetCurrentWeather` succeeds when called directly outside `go test`,
  so the failures track transient upstream rate-limiting, not a code
  defect. Reconfirmed 2026-09-03: CI's `test` job failed twice in a row
  (initial run + `gh run rerun --failed`) on `TestAPIWeatherAlertsHandler`
  specifically with a 502 from the same upstream, while local
  `make test` had passed clean pre-push — consistent with upstream
  rate-limiting hitting harder under GitHub Actions' shared IP ranges than
  from this host, not a regression from the commit under test. Pre-existing
  (untouched by the current reconciliation changeset, per `git log` on
  `api_utils_test.go`). Needs an HTTP transport fake/mock injected into the
  weather service for deterministic tests; out of scope for the current
  spec-reconciliation pass.

## IP lookup handlers leak raw internal error text — resolved 2026-09-02

- `osint.IPLookup` now returns the sentinel errors `ErrInvalidIP`,
  `ErrIPNotPermitted` and `ErrIPLookupFailed` (the last wrapping the raw
  `geoip.Get().Lookup` failure). A single shared `writeIPLookupError`
  helper in `src/server/api_utils.go` echoes only the two caller-input
  messages and returns a generic 500 "IP lookup is temporarily
  unavailable" for everything else, logging the full error chain
  server-side with `request_id`/`error_code`/`http_status`. All three call
  sites (`apiGeoIPHandler`, `apiOsintIPHandler`,
  `apiNetworkIPLookupHandler`) route through it so they cannot diverge.
  The raw caller input is no longer echoed in the invalid-IP message.

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

## SMTP env var / config wiring gap (code, not docs) — resolved 2026-08-30

- Resolved per owner decision (build it now). Added
  `server.notifications.email.smtp.*`/`.from.*` to
  `src/config/config.go` (`NotificationsConfig`/`EmailConfig`/
  `SMTPConfig`/`FromConfig`, matching AI.md PART 17's YAML shape exactly),
  with `defaultConfig()` seeding `port: 587`/`tls: auto`. Added
  `applySMTPEnvOverrides` (mirrors the existing
  `applyDatabaseEnvOverrides` pattern) reading all 7 `SMTP_*` env vars,
  called from both `Load()` paths (first-run and existing-config). Added
  `initSMTP(cfg)` in `src/main.go`, called once at startup right after
  GeoIP init: auto-detects via `email.AutoDetectSMTP()` when no host is
  configured (persisting the result via `config.Save`), or
  connection-tests an explicitly configured host every startup; builds
  and registers a process-wide `email.Client` via the new
  `email.Set`/`Get`/`Enabled` singleton (mirrors `src/tor.Set`/`Get`) so
  other packages can send mail without importing the composition root.
  Email is fully disabled (not queued/retried) when no working SMTP
  server is found, per "No SMTP = No emails."
- Scope note: `email.AutoDetectSMTP()` only probes priorities 1-2 of
  AI.md's 7-tier host table (`127.0.0.1`, `172.17.0.1` Docker bridge) —
  see the new gap logged immediately below for priorities 3-7.
- Verified `go build ./... && go vet ./...` clean, and
  `go test ./config/... ./email/... -cover` (86.3%/83.3%) plus the full
  `go test ./... -cover` suite (all packages pass, no regressions) in
  `casjaysdev/go:latest`.

## `AutoDetectSMTP` priorities 3-7 not implemented (spec gap, 2026-08-30)

- AI.md PART 17's SMTP auto-detection table has 7 priority tiers;
  `email.AutoDetectSMTP()` (see resolved SMTP wiring item above) only
  implements tiers 1-2 (`127.0.0.1`, `172.17.0.1`). Tiers 3-7 —
  `{gateway_ip}` (default gateway), `{fqdn}` (from `GetFQDN`),
  `{global_ipv4}`, `mail.{fqdn}`, `smtp.{fqdn}` — are not probed.
  Deliberately deferred: no gateway-IP, FQDN, or global-IPv4 detection
  helper exists anywhere in this codebase yet (grepped `src/` before
  scoping this), and building that detection infrastructure from
  scratch is materially larger than "wire the existing SMTP client into
  config." Needs its own implementation pass once FQDN/gateway/public-IP
  detection helpers exist (PART 8's `{fqdn}` resolution chain is the
  natural place to add them).

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

## `network/ip.tmpl` arbitrary-IP lookup calls a nonexistent route — resolved 2026-08-30

- Resolved per owner decision (add the backend route): added
  `apiNetworkIPLookupHandler` (`src/server/api_network.go`), reusing
  `osintService.IPLookup` — the same ip-location-db-backed GeoIP lookup (with
  private/loopback/link-local rejection) already used by
  `/api/{version}/geo/ip/{ip}` and `/api/{version}/osint/ip/{ip}` — and
  registered `r.Get("/ip/{ip}", apiNetworkIPLookupHandler)` under
  `/api/{version}/network/` in `src/server/server.go`, matching what
  `network/ip.tmpl`/`app.js` already called. Verified with
  `go build ./... && go vet ./... && go test ./src/server/... -cover`
  (78.0%) in `casjaysdev/go:latest`.

## `confirm()` in `confirmDelete()` — resolved 2026-08-14

- `app.js`'s `confirmDelete(form, message)` used `window.confirm()`
  (frontend-rules.md: never `alert()`/`confirm()`/`prompt()`). It had zero
  callers anywhere in the templates (dead code), so there was no markup to
  migrate. Replaced it with the exact `data-confirm-dialog` pattern from
  AI.md PART 16 ("Delete confirmation uses the native `<dialog>` pattern -
  never `confirm()`"): a top-level `querySelectorAll('[data-confirm-dialog]')`
  listener that opens the dialog named by the trigger button's
  `data-confirm-dialog` attribute via `showModal()`; per the spec, the
  dialog's Cancel button closes via `<form method="dialog">` (zero JS) and
  its Confirm button submits the real delete form via the HTML5
  `form="{form-id}"` attribute — no per-call message/state passed through
  JS. No template currently declares a delete form, so this is
  infrastructure only; the next feature that needs delete confirmation
  should follow the exact markup shown at AI.md line ~23933. Verified
  `grep -n "confirm(\|alert(\|prompt(" src/server/static/js/app.js` matches
  only a comment; `go build ./... && go vet ./... && go test ./... -cover`
  clean in `casjaysdev/go:latest`.

## Email template translation pipeline not implemented (spec gap, 2026-08-29)

- AI.md was updated to clarify (PART 30 cross-ref, email template section):
  the plain-substitution email template engine cannot call functions, so
  `{{t "key"}}`/`{{tf "key" ...}}` do NOT work in `.tmpl`-free email bodies —
  every translated string must be resolved server-side via
  `i18n.Translate`/`i18n.TranslateFormat` BEFORE rendering, then passed in as
  an already-translated plain `{variable}`. This is moot for right now:
  `src/email/email.go` has no template loader/renderer at all (no
  `src/server/template/email/` directory, no `{config_dir}/template/email/`
  override support, no i18n package under `src/common/i18n`), and per the
  existing SMTP wiring-gap item above, the whole email feature is
  unimplemented and unwired into config/startup. Needs a design/build pass
  (email templates + i18n package) before this translation rule is
  actionable; tracked here so it isn't lost.

## CLI self-update temp path not yet implemented (spec gap, 2026-08-29)

- AI.md PART 22/32 (CLI Auto-Update) specifies the downloaded update binary
  is saved to `/tmp/{project_org}/{internal_name}-XXXXXX/cli.update.tmp`
  (just corrected in the AI.md update from `{project_name}` to
  `{internal_name}`). `src/client/` has no `--update`/self-update command or
  download/verify/swap logic at all yet (checked `src/client/cmd/` — no
  update.go). Nothing to fix in code today; log the correct tmp-path
  convention here so whoever implements CLI self-update starts from the
  right spec value.

## Bugs found during coverage work — resolved 2026-08-30

- `src/service/parse/parse.go` `parseLogLine` — resolved: fixed the
  fixed-width prefix slice bug by trying decreasing prefix lengths per
  layout, so shorter `Z`-suffixed RFC3339 timestamps parse correctly.
  Verified with `go test ./src/service/parse/... -cover` (92.4%).
- `src/sysservice/service.go` `installRunit()` — resolved: now checks the
  `os.Symlink` return value and returns an error (unless the link already
  exists) instead of silently discarding it. Verified with
  `go test ./src/sysservice/... -cover` (74.7%).
- `src/service/network/network.go` `whoisQuery` hardcoding port 43 —
  confirmed intentional (WHOIS protocol standard port); not a bug, no
  code change needed.

## Missing PWA support — resolved 2026-08-14

- frontend-rules.md ALWAYS DO: ship a complete PWA — `/manifest.json` with
  all icon sizes incl. maskable, a service worker (install/activate/fetch
  lifecycle), and an offline fallback page. None of this existed except a
  bare `manifestHandler` with no icons array. Added: 192/512 "any" +
  192/512 "maskable" PNGs plus `apple-touch-icon.png`/`favicon.ico` under
  `src/server/static/images/`; `manifestHandler` now emits the full
  `icons` array, `scope`, `orientation`, `categories`, and serves with
  `Cache-Control: no-cache` + a build-stamped `ETag` (AI.md PART 16: a
  cached service worker/manifest delays every other update mechanism);
  new embedded `/sw.js` (cache-first for `/static/*`, network-first with
  `/offline.html` fallback for HTML navigations, network-only for
  `/api/*`, `castools-cache-v0.0.1` versioned with old-cache purge on
  `activate`, `SKIP_WAITING` message handling) and `/offline.html` routes
  in `server.go`; `head.tmpl` gained the `apple-touch-icon` link and iOS
  `apple-mobile-web-app-*` meta tags (iOS has no `beforeinstallprompt`);
  `header.tmpl` gained an offline indicator and an install-app button;
  `components.css` gained `.offline-indicator`/`.update-banner` styles
  (existing CSS variables only, no new palette values); `app.js` gained
  service-worker registration with update-notification banner
  (`showUpdateNotification`/`updateApp`), `beforeinstallprompt`-driven
  install flow (`installApp`/`isInstalledPWA`) with a manual "Add to Home
  Screen" fallback for iOS Safari (`isIOSSafari`), and online/offline
  event wiring for the indicator. Background Sync / IndexedDB
  offline-write queueing was intentionally not added — this app has no
  offline-writable mutating flow for it to queue. iOS splash-screen
  (`apple-touch-startup-image`) meta tags from AI.md's fuller example were
  also not added — the 192/512 any+maskable icon set is the minimum
  frontend-rules.md itself calls out and splash screens are a separate,
  smaller polish item; revisit if a future audit flags it. Verified
  `go build ./... && go vet ./... && go test ./... -cover` clean in
  `casjaysdev/go:latest` (`src/server` coverage 72.5%, all packages ≥60%
  except the two pre-existing zero-coverage entrypoint/const packages
  `src`/`src/common/theme`, unrelated to this change).

## AI.md PART 27 transcription bug (spec-content issue, not code)

- AI.md's canonical `beta.yml`/`daily.yml` "Compute version" shell steps
  (around lines 33911, 33913, 34091) write `>> "$$GITHUB_OUTPUT"` (doubled
  `$`), which bash would expand to the current process PID instead of the
  `GITHUB_OUTPUT` env var, silently breaking the version hand-off from the
  `version` job to `build`/`release`. AI.md is read-only (project rule), so
  this cannot be fixed at the source. Workaround applied: the actual
  `.github/workflows/beta.yml` and `.github/workflows/daily.yml` files in
  this repo use the correct single-`$` `>> "$GITHUB_OUTPUT"` form. No other
  file references this snippet. Flag for the user to correct in AI.md
  upstream if/when it is next revised; no repo-side action remains.

## AI.md spec-reconciliation backlog (opened 2026-09-02)

AI.md was rewritten across ~20 commits since the last full reconciliation
(2026-07-30). A PART-by-PART audit of the current 48,520-line spec against
the tree surfaced the following. Items marked *(in progress)* are being
implemented in this pass; the rest are unstarted and are recorded here so
they are not lost.

### Resolved 2026-09-02 (metrics/branding/asset pass)

- PART 20 — every metric family declared in the spec now has a real call
  site. `src/database/metrics.go` instruments query duration/errors and
  samples pool stats; `src/cache/` records hits/misses/evictions/size;
  `src/scheduler/` records task duration and outcome at the single
  `execute()` chokepoint; `src/tor/metrics.go` records enabled/running/
  circuit gauges; `src/server/ratelimit.go` records `ratelimit_requests_
  total`/`ratelimit_blocked_total` with fixed `global`/`per_ip` label
  values; `src/metrics/authmw.go` records `auth_attempts_total`;
  `src/metrics/middleware.go` counts `tor_requests_total` by matching a
  `.onion` Host. No raw IP, SQL text, key, or user input reaches a label.
- PART 9/16 — all static asset references in `head.tmpl` and
  `scripts.tmpl`, plus the four `/manifest.json` icon URLs, now go through
  `assetURL()` so they carry the build stamp.
- PART 16 — site identity is no longer hardcoded in templates:
  `head.tmpl` (title, description, apple-mobile-web-app-title, og:title,
  og:description), `header.tmpl` (`.site-brand`), `index.tmpl` and the
  `/manifest.json` `name`/`short_name`/`description` all read from
  `server.branding.*` via the new `PageData.SiteDescription` field.
- PART 13 — `page/healthz.tmpl` and the now-orphaned
  `partial/public/status-banner.tmpl` were deleted; the health page is
  rendered directly by `src/server/handler/health.go` so HTML, JSON and
  plain text all derive from one `HealthResponse`. `handler` cannot import
  the template FS (it lives in package `server`, which imports `handler`),
  so a `.tmpl` was not an option.

### Resolved 2026-09-03 (PART 16 branding + header pass)

- PART 32 — `src/common/urlutil/encode.go` created (`BuildAPIURL`,
  `EncodePathSegment`, `EncodeQueryValue`, `BuildQueryString`); the package
  did not exist at all despite the rules file already citing it.
- PART 16 — `src/common/urlutil/fetch.go` created: SSRF-safe remote branding
  image fetch (https-only, private/loopback/link-local/multicast rejection,
  per-hop redirect re-validation capped at 5, content-type allow-list, size
  cap via `io.LimitReader`).
- PART 16 — `server.branding.favicon` / `server.branding.logo` added to
  `BrandingConfig`; `src/server/branding.go` resolves the three sources
  (empty = embedded default, absolute path = local file served through
  `/branding/{favicon,logo}`, `https://` = remote URL) and backs
  `/favicon.ico`. `PageData.FaviconURL`/`LogoURL` render them.
- PART 16 — header/nav merged into the single row with the four zones in
  order (brand, centered links, preferences, theme toggle). `nav.tmpl` no
  longer emits its own row wrapper and `layout/public.tmpl` no longer
  templates it separately; `public.css` replaced the two-row header/nav
  rules with the single-row flex layout plus `.header-actions`,
  `.header-link`, `.theme-button`, `.site-logo`.
- PART 16 — theme toggle now exists. `NextTheme()` in `src/server/theme.go`
  plus the `nextTheme` template func give the toggle a server-computed
  target; the toggle is a real `/server/preferences` POST form so it works
  with JavaScript disabled. `static/js/theme.js` adds instant preview only
  (no `preventDefault()`). `theme.toggle` added to all seven locales.
- Page-breaking bug — `header.tmpl` referenced `.User` / `.User.Username`
  and `/auth/login` `/auth/logout`; `PageData` has no `User` field and
  those routes do not exist, so every public page render would fail at
  template execution. The block is removed (no accounts exist).
- Test coverage — `src/server/render_test.go` added. Nothing in the suite
  parsed or executed the templates before, which is why the `.User` bug
  above passed a green build (`html/template` resolves field references at
  execution time, not parse time). The new tests execute the public layout
  for three pages and assert the PART 16 single-row header markers and the
  server-computed `nextTheme` target.

### Newly discovered 2026-09-03 — not yet fixed

- PART 16 "Profile/Preferences Zone" (AI.md 24929-24944) specifies a
  dropdown variant of the preferences control when an `owner_token` cookie
  exists ("Manage my {resource}" + divider + "Preferences"). Only the
  `{{else}}` plain-link branch is implemented, because nothing in this
  project ever sets an `owner_token` cookie — there are no owned resources.
  If per-resource ownership is ever added, the dropdown branch and the
  `.dropdown`/`.dropdown-menu`/`.dropdown-item`/`.dropdown-divider` CSS
  must be built at the same time.

- PART 16 "Image Scaling" (AI.md 25821-25836) requires automatic
  generation of scaled branding derivatives (logo 200px/50px, favicon
  16/32/48/180/192/512, OG image 1200x630), cached locally, with remote
  sources re-fetched daily. Nothing of this exists — `branding.go` serves
  the configured image as-is. Needs a pure-Go image pipeline
  (`golang.org/x/image/draw`) plus a scheduler task for the daily re-fetch.
- PART 16 CSS token names: the spec's blocks use `--color-primary`,
  `--space-4`, `--radius-md`, `--font-mono` while this project derives
  `--primary-color`, `--spacing-md`, `--border-radius` from
  `src/common/theme/colors.go`. The prior decision (lines 822-837) was to
  keep the project's names; the genuinely new scales the spec adds
  (typography xs-4xl, 4px spacing scale, radius scale, `--shadow-xl`,
  `--shadow-inner`, z-index layers, `--transition-slow`, `--font-serif`)
  are still unadded.

### Newly discovered 2026-09-02 — not yet fixed

- `src/server/static/offline.html` and `src/server/static/js/app.js:109`
  still hardcode "CasTools". `offline.html` is served as a static asset,
  so making it branding-aware means either templating it at serve time or
  accepting the deviation — needs a decision.
- `src/scheduler/update_task.go:97,114`, `src/scheduler/api.go:142`,
  `src/scheduler/tasks.go:362` and `src/scheduler/scheduler.go:495` run raw
  SQL against `database.GetServerDB()`, bypassing the PART 20 DB
  instrumentation entirely. Either export an instrumented API from
  `src/database` and change those five call sites, or move the queries into
  the database package.
- No application authentication middleware exists yet, so
  `RecordAuthAttempt` currently fires only for the metrics-endpoint bearer
  gate. It must also cover operator-token and owner-token verification once
  the PART 8 two-tier auth lands, and `SetActiveSessions` has no call site
  at all until sessions exist.

### In progress this pass

- PART 26/27 — Dockerfile must not create or switch to a non-root `USER`
  (rule reversed: the binary owns user creation and privilege drop).
  Compose healthchecks must be exec-form JSON arrays; compose `name:`/
  `networks:` suffixed per mode (`-dev`/`-test`); `DEBUG: 1` -> `true`,
  `MODE: dev` -> `development`; test compose uses the `:devel` image.
  `:devel` builds move to a dedicated `build-devel` job in `docker.yml`
  (daily 4am UTC cron) and drop off the standard push job; Jenkinsfile
  gains a matching "Docker: Devel" stage. *(in progress)*
- PART 6 — `debug` is now a genuine third `MODE` value, explicit opt-in
  only, not a `MODE=debug` alias. Development mode no longer permits
  showing sensitive data. Caching becomes config-driven, not mode-driven.
  *(in progress)*
- PART 13 — health `status` enum expands to six values with fixed HTTP
  codes (`healthy`/`degraded`/`restart_required` 200,
  `unhealthy`/`maintenance`/`shutting_down` 503); `maintenance` moves from
  a `mode` value to a `status` value; new `restart_reason` and project
  `tagline` fields; `Branding.Name` renamed to `Branding.Title` plus a new
  `Branding.Tagline`. Test-script healthz assertions change from
  `"ok":true` to `"status":"healthy"`. *(in progress)*
- PART 9/16 — version-change purge: `api_build` cookie plus
  `Clear-Site-Data: "cache", "storage"` (never `"cookies"`) on mismatch,
  HTML responses only. `/sw.js` and `/manifest.json` get
  `Cache-Control: no-cache` and a build-stamp `ETag`. New `.badge-debug`
  CSS, six-state status banner, `.onion-address`/`.i2p-address` scroll
  boxes, `{{ if and .TorEnabled .TorRunning .OnionAddress }}` template
  guard, zero-padded footer date. *(in progress)*
- PART 28 — new required `tests/e2e.sh` plus `tests/e2e/` behind the `e2e`
  build tag, using `github.com/chromedp/chromedp`, with three mandatory
  tiers (SSR / no-JS browser / full browser). Standalone only: never part
  of `make test`, `run_tests.sh`, or any CI gate. *(in progress)*
- PART 29 — `mkdocs.yml` nav references a nonexistent `docs/admin.md` and
  omits `security.md`/`integrations.md`; `.readthedocs.yaml` pins stale
  `ubuntu-22.04`/Python 3.11 instead of `ubuntu-24.04`/3.12.
  *(in progress)*

### Unstarted — large subsystems

- PART 22 (Update Command) — resolved 2026-09-02. `src/update/` now
  implements the GitHub Releases check, channel filtering
  (stable/beta/daily), `defer_days` gating, SHA-256 verification and
  platform-specific binary replacement, dispatched from `--update`.
  Remaining sub-item: the package fetches `sha256.txt` while the spec names
  the release asset `checksums.txt` — confirm which name the release
  workflow actually publishes and align the two.
- PART 30 (i18n) — resolved 2026-09-02. `src/common/i18n/` ships the seven
  required catalogs (en, es, zh, fr, ar, de, ja) embedded via `go:embed`,
  named `{token}` formatting, CLDR plural categories, RTL direction for
  Arabic, `LanguageMiddleware` in the server chain, the `?lang=` -> cookie
  -> `Accept-Language` -> `en` resolution chain, `t`/`tf`/`tp`/`dir`
  template funcs, `<html lang dir>` in the public layout, and
  `i18n.Validate()` fail-fast key validation at startup.
- PART 30 (a11y) is almost entirely unimplemented. Only two files under
  `src/server/template/` carry ARIA attributes; no `.sr-only` /
  `.sr-only-focusable` class exists in any stylesheet; no skip-to-content
  link exists in any layout or partial.
- PART 23/24 (privilege escalation and service user). No escalation
  detection or flow exists (`sudo`/`su`/`pkexec`/`doas`/`osascript`/UAC).
  No dedicated `api` service user/group creation, no UID/GID matching, no
  safe-range selection (200-899 Linux/BSD, 200-399 macOS), no reserved-ID
  avoidance — and the generated systemd unit hardcodes `User=root`/
  `Group=root`, violating the never-run-permanently-as-root rule.
- PART 21 (Backup and Restore). Manifest is named `backup.json` instead of
  `manifest.json` and omits `created_by`, `app_version`, `contents[]`,
  `encryption_method`, and `checksum`. No SHA-256 archive checksum is
  computed. The entire post-creation verification pipeline is missing
  (exists / size>0 / checksum / decrypt test / manifest readable / content
  extraction / DB integrity). No backup audit events are emitted. Retention
  is a flat keep-last-N with no weekly/monthly/yearly tiers and no
  `max_total_size` cap. Filenames do not match the spec pattern and the
  full-versus-incremental split does not exist. No pre-backup disk-space
  check (`backup.skipped_disk_full` can never fire). `Restore()` takes no
  caller-identity parameter, so the required authorization matrix cannot be
  enforced.
- PART 17 (Email). SMTP auto-detection implements only 2 of 7 tiers.
  `src/server/template/email/` does not exist — none of the 10 required
  templates, no `{variable}` substitution engine, no custom-override
  resolution from `{config_dir}/template/email/`. No per-event toggles
  under `server.notifications.email.events`, and therefore no suppression
  rules.
- PART 20 (Metrics) — mostly resolved 2026-09-02. All canonical and aliased
  paths are mounted (`/server/metrics[/{service}]`,
  `/api/{api_version}/server/metrics[/{service}]`, `/api/metrics[/{service}]`
  and the `root.enabled`-gated `/metrics[/{service}]`), per-service bearer
  tokens are mandatory with a 403-empty-body response when unset, and the
  system/runtime/scheduler/grafana/loki collectors exist. Remaining: the db,
  auth, cache, tor and rate-limit recorders are defined but not yet called
  from their owning packages, so those series stay at zero.
- PART 19 (GeoIP). No `deny_countries`/`allow_countries`/`geoip.presets`
  config wiring exists anywhere. Note the spec change: the database set
  collapsed to `asn.mmdb`, `geo-whois-asn-country.mmdb` (renamed from
  `country.mmdb`), `dbip-city-ipv4.mmdb`, `dbip-city-ipv6.mmdb`, all on a
  weekly cadence; the `geoip.databases.whois` key was removed entirely and
  nothing may be labelled "WHOIS"; GeoLite2 is banned outright.
- PART 31.2 (I2P eepsite) is unimplemented. Opt-in, `server.i2p.enabled`
  defaults false, so this is compliant-by-default today, but the config
  keys, `{data_dir}/i2p/`, `{config_dir}/i2p/tunnels.conf`, healthz fields,
  and the `i2p_health` scheduler task are all absent.
- PART 31.1 (Tor) was rewritten: hidden-service creation moves from
  `bine control.AddOnion()` to a static torrc regenerated every startup, a
  dedicated random loopback backend port wrapped in PROXY protocol
  (`github.com/pires/go-proxyproto`), and new torrc hardening
  (`ExitRelay 0`, `ExitPolicy reject *:*`, no `ORPort`,
  `SocksPort 127.0.0.1:auto`). The outbound Tor client must never proxy a
  destination supplied by an inbound `.onion` visitor.
- PART 18 (Scheduler). Only 7 of 11 required tasks are registered; missing
  `blocklist_update`, `cve_update`, `update_check`, `backup_hourly`. No
  task body fires `scheduler_error` or any dedicated failure notification.

### Unstarted — smaller mechanical items

- PART 16 — stateless preference export/import: resolved 2026-09-02.
  `src/server/preferences.go` serves `/server/preferences` (GET page, POST
  save), `/server/preferences/export`, `/server/preferences/import` and the
  four `/api/v1/server/preferences*` mirrors. Only `theme` and `lang` are
  carried; export emits both the full import URL and the base64url short
  code (AI.md 23128-23131 — the earlier "attachment file download" reading
  of this item was wrong); import validates against the enum/BCP-47
  allowlists, drops unknown keys, then answers `303 See Other`. No DB
  storage. The non-canonical `POST /api/v1/theme` route and its
  `HandleThemeSwitch` handler were deleted outright rather than aliased.
- PART 16 — new theme palette: dark switches to Dracula and light to GitHub
  Light, with `--bg-color`/`--text-color`/`--border-color` renamed to the
  canonical `--color-bg`/`--color-text`/`--color-border`. CLI/TUI must use
  the new `TerminalPalette` (256-colour ANSI, `\033[38;5;`) via
  `StylesFromTerminalPalette`, not the hex `ThemePalette`.
- PART 3 — Go path-resolution variables rename `projectOrg`/`projectName`
  to `internalOrg`/`internalName`, and `mktemp -d` patterns must use
  `INTERNAL_NAME`.
- Root `site.txt` (canonical official-site URL) does not exist. Its value
  must not be guessed — needs a user decision.
- Cross-cutting scale requirement: sustain 500,000 or more concurrently
  open connections (non-blocking I/O, bounded pools, backpressure).
  Currently unverified; no load-test evidence exists.

### PART 9-16 compliance audit findings (2026-09-02)

A line-by-line audit of PART 9, 10, 11, 13, 14, 15 and 16 against the tree
produced 31 findings. Grouped by severity; none of these are fixed yet
unless a later line says otherwise.

Critical — entire subsystems absent:

- CSRF protection does not exist. `src/server/middleware.go:47-92` documents
  deliberately skipping it, which is not an IDEA.md-declared exception. The
  spec requires the stateless double-submit cookie pattern (`csrf_token`
  cookie, `SameSite=Strict`, not HttpOnly, constant-time compared against
  the header or form field) on every mutating non-Bearer browser request.
  Seven image-tool POST templates carry no `csrf_token` field.
- None of the four mandatory project secrets are implemented:
  `installation_secret`, `cookie_signing_key` (90-day rotation),
  `csrf_token_secret` (180-day rotation), `server.security.encryption_key`.
  Generated on first start, never logged, always included in backups.
- The `server.contact.*` tree is non-compliant: only a flat
  `SecurityConfig.Contact string` exists (`src/config/config.go:606,838`).
  Required is `admin`/`security`/`abuse`/`general`, each with `email` plus
  `webhooks`, the documented fallback cascade computed fresh per dispatch,
  and signed outbound webhooks (`X-Webhook-Signature` HMAC-SHA256,
  `-Timestamp`, `-ID`, `-Event`) with 1m/5m/15m/1h/6h/24h retry backoff.
- `server.tracking.*` (google, matomo, piwik, owa, fathom, plausible,
  umami, simple, cloudflare, each with its own ID/URL validation) is
  entirely unimplemented.
- `server.privacy.*` and the cookie-consent banner are unimplemented.
  `privacyPageHandler` (`src/server/server.go:1136-1144`) renders a fully
  static page with none of the auto-generated sections, and the
  `data.sold`-driven CCPA copy switch does not exist.
- The announcements / site-banner subsystem (`web.announcements`, per-item
  `start`/`end` window, `dismissed_announcements` cookie, zero-JS POST
  dismissal) is missing.

High:

- No `context.WithTimeout` on any database query — every call site in
  `src/database/*.go` uses bare `Query`/`Exec`. The spec mandates per-class
  timeouts (5s simple SELECT, 15s JOIN, 10s write, 60s bulk, 30s
  transaction).
- No `WithTransaction` helper, no optimistic-locking `version` column
  pattern, and no `SQLITE_BUSY`/"database is locked" retry with backoff.
- Connection pool is hardcoded 25/5 at `src/database/database.go:104-105`
  with no `max_lifetime`, no `max_idle_time`, and no deployment-tier config.
- No idempotent `ALTER TABLE ADD COLUMN` path and no `isColumnExistsError`
  helper, so the required no-migration-files schema-evolution model cannot
  work.
- The API version is hardcoded `v1` instead of going through
  `APIBasePath()`: `src/server/server.go` around lines 108, 109, 124, 164;
  roughly 26 routes in `src/server/handler/text.go:594-635`; 13 paths in
  `src/swagger/swagger.go`; and `src/server/logger.go:57`.
- The canonical envelope is bypassed by the ~26 handlers fed through
  `src/server/handler/text.go:12-22`.
- The list pagination envelope (`{"data":[...],"pagination":{...}}`,
  default limit 250) is unimplemented.
- Swagger is hand-written and covers 13 of 407 routes;
  `src/swagger/annotations.go` does not exist. The spec requires generation
  from code at build time.
- GraphQL is hand-written (`GenerateSchemaSDL` returns a literal string);
  `schema.go` and `resolvers.go` do not exist.
- The audit log is missing the mandatory `id`, `category`, `severity`,
  `actor` and `result` fields and millisecond precision
  (`src/server/logger.go:412-427`, `src/database/database.go:168-178`).
- `/.well-known/llms.txt` is not served.
- `/api/autodiscover` does not exist, which also blocks the CLI's
  server-discovery and self-update flow (PART 32).
- Content-negotiation helpers exist only inside
  `src/server/handler/health.go:196-241` and are not wired into any other
  route, so the `.txt` suffix / `Accept: text/plain` / text-browser /
  CLI-client matrix is unimplemented site-wide.
- DNS-01 ACME is unimplemented (`src/ssl/acme.go:52-62`,
  `src/ssl/ssl.go:99-103`); only HTTP-01/TLS-ALPN-01 paths exist.
- I2P is entirely absent (also recorded under PART 31.2 above).

Medium:

- The contact page renders no form and prints `SecurityEmail` publicly,
  which the spec forbids.
- The `/server/security` route (the required link target instead of a raw
  `/.well-known/security.txt` link) is missing.
- The help page is missing its required sections.
- `/sitemap.xml` is advertised in `robots.txt`
  (`src/server/server.go:1244`) but no route serves it.
- `sw.js` has `respondWith` branches that can resolve `undefined`
  (lines 63-74, 77-92, 94-97); every branch must resolve a real `Response`.
- `<html lang>` — resolved 2026-09-02. `layout/public.tmpl` now renders
  `lang="{{.Lang}}" dir="{{.LangDir}}"` from the resolved language.
- `src/server/static/css/admin.css` is dead code still loaded by
  `partial/head.tmpl:15-16` (see the open question below).
- The tier-1 certificate location `/etc/letsencrypt/live/domain/` is never
  checked (`src/ssl/ssl.go:188-208`).
- The autocert `DirCache` layout does not match the spec's
  `{config_dir}/ssl/letsencrypt/{fqdn}/` shape (`src/ssl/ssl.go:105`).
- Health `FeaturesInfo`/`ChecksInfo` omit I2P.

### Wave-1 implementation hand-offs still open (2026-09-02)

Follow-up work identified by the parallel implementation pass that lands
outside the file scope of the agent that found it:

- `src/server/server.go:61` must register `r.Use(versionPurgeMiddleware)`
  (the middleware and its tests now exist in `src/server/versionpurge.go`).
- `buildETag()` (`src/server/server.go:1292`) should derive from
  `assetStamp()` so the ETag is build-stamped.
- `PageData` (`src/server/server.go:630-651`) needs `TorEnabled`,
  `TorRunning`, `OnionAddress`, `I2PEnabled`, `I2PRunning` and
  `I2PAddress`; `newPageData` (line 667) must set `BuildDate` formatted as
  `January 02, 2006 at 15:04:05 MST`.
- 208 of 215 forms have no `action`/`method` attribute, so the site is not
  usable without JavaScript — a direct violation of the
  works-without-JS rule.
- Static assets have neither an `?v=` build stamp nor a `Cache-Control`
  header.
- `src/server/template/page/healthz.tmpl` is dead code: the route resolves
  to `handler.ServerHealthz`, whose `writeHealthHTML` builds HTML inline.
- `src/server/middleware.go:214` should gate on `mode.IsVerboseMode()`
  rather than `IsDevelopment()` now that `debug` is a distinct mode.
- `BuildHealthResponse` still hardcodes tor/geoip to false and every stat
  to 0, and `checkScheduler()` returns "ok" on both branches.
- `docs/stylesheets/light.css` uses `--light-accent-orange: #ff8c00`
  (taken verbatim from AI.md) which is about 2.2:1 against white and fails
  the WCAG AA requirement the same spec imposes; `#b35c00` passes. Needs a
  user call because the failing value is the spec's own.
- The Jenkinsfile beta stage's source-tarball `tar` invocation is written
  single-line while the surrounding stages wrap theirs, leaving the file
  inconsistent in formatting only.
- PART 0-8 has not been audited this pass — the agent assigned to it was
  killed before reporting. Needs a re-run.

### Wave-3 results (2026-09-02)

Resolved this pass:

- PART 5/6/12 config trees, PART 8/10 `config`/`config_meta`/`app_secrets`/
  `api_tokens` schema (the AI.md combined `AFTER INSERT OR UPDATE OR DELETE`
  trigger is split into three triggers because SQLite cannot parse the
  combined form), and first-run generation of `server.security.encryption_key`
  and `server.token`.
- PART 11 secret rotation: `api --maintenance secret rotate <name>` for
  `installation_secret` and `encryption_key` only, root-or-operator-token
  authorized, typed confirmation, emitting
  `security.installation_secret_rotated` /
  `security.encryption_key_rotated`. `cookie_signing_key` and
  `csrf_token_secret` are auto-rotated and are rejected by hand.
  `database.WriteAuditEvent` added as the shared audit writer.
- PART 19 attribution: both required notices now appear verbatim in
  `LICENSE.md` (with the three-database CC BY 4.0 table) and visibly on
  `/server/about`. Four stale dependency versions in `LICENSE.md` were
  synced to `go.mod` in the same pass.
- PART 19 country blocking is wired into the middleware chain
  (`src/server/geoblock.go`), placed after rate limiting so a blocked
  request still consumes its budget, before auth so it never substitutes
  for one, failing open on every uncertainty and logging
  `security.country_blocked`.
- PART 17 email (`src/email/**`), PART 21 backup (`src/backup/**`),
  PART 18 scheduler (`src/scheduler/**`) and PART 23/24 service manager
  (`src/sysservice/**`) implemented by their scoped agents.

Still open from this pass:

- The `src/email` `Notifier` is complete but not yet wired into the
  scheduler, backup, SSL and update call sites.
- The db/auth/cache/scheduler/tor/ratelimit metric recorders are defined
  but never called from their owning packages.
- PART 31.1 Tor rewrite (static torrc + PROXY protocol) and PART 31.2 I2P
  are unstarted.
- PART 22 release asset name: the code expects `sha256.txt`, the spec says
  `checksums.txt`. Needs reconciling.
- Accessibility sweep (`.sr-only`, skip links, ARIA) not yet done.

### Wave-4 results (2026-09-02)

Resolved this pass:

- PART 16 CSRF protection implemented: `server.csrf` config tree
  (`src/config/csrf.go`), stateless double-submit middleware
  (`src/server/csrf.go`) wired after `secFetchValidationMiddleware`, and a
  `csrfField` template function that renders the hidden input. Validation
  runs only on POST/PUT/PATCH/DELETE without a bearer credential; the only
  bypasses are a bearer header, a safe method, a WebSocket upgrade, an
  `exempt_paths` glob, and the programmatic `/api/**` surface — there is
  deliberately no Origin-based bypass. Failures return 403 `CSRF_FAILED` and
  log `security.csrf_failure`. The stale "no CSRF middleware exists in this
  project" note in `src/server/middleware.go` was corrected.
- The `/api/**` bypass resolves a conflict inside AI.md: PART 16's "When
  CSRF Validation Runs" table would validate every tokenless POST, but PART 8
  mandates that `/api/...` accept the `Authorization` header ONLY and ignore
  cookies, PART 11 lists "public paths" as an automatic bypass, and PART 16's
  own threat model marks a public endpoint POST as "n/a — nothing to abuse".
  Routes that never read a cookie carry no ambient authority to forge, and
  every documented endpoint in `docs/api.md` is a tokenless public POST, so
  validating them would break every non-browser client without closing an
  attack path. Cross-origin abuse there is covered by the Sec-Fetch-Site
  layer and per-IP rate limiting.
- `PageData` gained a `CSRFToken` field populated from the request context,
  and all seven server-rendered upload forms under
  `src/server/template/page/tools/image/` now emit `{{csrfField .CSRFToken}}`.
  `static/js/app.js` reads the non-HttpOnly cookie and sends `X-CSRF-Token`
  on every mutating fetch. The middleware parses multipart bodies to find the
  hidden field without consuming the upload.
- PART 18: the scheduler no longer sits behind `cfg.Server.Schedule.Enabled`
  in `src/main.go` — it is mandatory and always starts.
- PART 24: `printServiceHelp` now delegates to `sysservice.HelpText`, so
  `--service --help` carries the required live status block.

Still open from this pass:

- The 208 forms that still lack `action`/`method` attributes will each need
  `{{csrfField .CSRFToken}}` as they are made to work without JavaScript.
- `i2p_health` has no scheduler task because there is no I2P implementation
  yet (see PART 31.2 above).

### Stale generated files

- `.claude/rules/features-rules.md` and `.claude/rules/config-rules.md`
  still describe the pre-rewrite GeoIP scheme (four database types
  including a separate "WHOIS" dataset), which the spec removed.
- `.claude/rules/backend-rules.md` PART 31 section documents only Tor and
  predates both the Tor rewrite and the new I2P feature; the scheduler task
  table in `features-rules.md` omits `i2p_health`.
- All 13 `.claude/rules/*.md` files should be regenerated from the current
  AI.md once the implementation backlog above has settled.

### PART 10/19 correctness pass — resolved 2026-09-02

- Every `context.Background()` call site in `src/database/cleanup.go` (4)
  and `src/database/scheduler.go` (5) now carries a per-class deadline from
  the new `src/database/timeouts.go`, matching AI.md PART 10's table
  (5s simple SELECT / 15s JOIN / 10s write / 60s bulk / 5m schema).
  This supersedes the "still run every statement with `context.Background()`"
  item above.
- `database.Init` now publishes `serverDB` under `mu.Lock()` only after the
  handle is opened, pool-configured, pinged and migrated, closing the latent
  data race recorded above. `createSchema`/`createServerSchema` take the
  handle explicitly because they run before publication.
- The connection pool gained the two settings PART 10 requires and the code
  was missing: `SetConnMaxLifetime(5m)` and `SetConnMaxIdleTime(1m)`.
- `VacuumDatabases` read the package-level handle directly; it now goes
  through the mutex-guarded `GetServerDB()`.
- PART 19 bans MaxMind GeoLite2 outright (AI.md line 28235). Every
  data-source reference to it was replaced with `sapics/ip-location-db` in
  `src/service/osint/osint.go`, `src/server/api_network.go`,
  `src/server/api_utils.go` and `IDEA.md` (2 lines: the keyless-provider
  list and the trust-boundary table). The remaining `MaxMind` strings in
  `src/geoip/geoip_test.go` are correct — MMDB is the MaxMind *file format*
  and the test hand-builds one.
- `server.geoip` was parsed but never consumed. `src/main.go` now calls
  `geoip.Get().LoadFromConfig(...)` and `src/scheduler/tasks.go`'s weekly
  refresh calls `geoip.DownloadFromConfig(...)`, so the operator's `dir`,
  `enabled` flag and per-database selection are honoured at both startup
  and refresh.
- The country database filename moved to the spec's
  `geo-whois-asn-country.mmdb` via named constants in `src/geoip/geoip.go`,
  with `PresetNames`/`ValidatePresets` added for country-list presets.
  Invalid presets warn and are dropped; they never fail startup.
- The CC BY 4.0 DB-IP/NRO attribution is now also rendered on the five
  pages that display GeoIP data (`page/geo.tmpl`, `tools/geo/ip.tmpl`,
  `tools/geo/country.tmpl`, `tools/osint/ip.tmpl`, `tools/network/ip.tmpl`)
  via the new `partial/public/geoip_attribution.tmpl`. It was already
  correct in `LICENSE.md:58-71` and `about.tmpl:24-25` — an earlier entry in
  this file claiming it was missing was wrong and has been removed.

### Newly discovered 2026-09-02 (second pass) — not yet fixed

- **170 tool forms could not work without JavaScript — 168 fixed
  2026-09-02.** The handlers did not accept the shape an HTML form emits
  (89 `data-template` path-param GETs read via `chi.URLParam` only, 53
  `data-body-endpoint` POSTs treating the raw body as input, 15
  `data-query-post-endpoint`, 10 `data-image-template`, 3 legacy forms).
  Fixed server-side in the new `src/server/formparam.go`:
  `formInputMiddleware` merges urlencoded/multipart form fields into the
  query string for `/api/**` POST/PUT/PATCH through a bounded 1MB
  `MaxBytesReader` (explicit query wins over a same-named field; the
  original request is left untouched so `access.log` never sees merged
  values); `paramValue()` replaced all 130 `chi.URLParam` call sites so a
  path param falls back to a query param; `registerFormFallbacks()` walks
  the finished router and registers 81 parameterless prefixes, skipping any
  collision with an explicit route. `readRequestBody` in `api_utils.go`
  now honours a `body=` form field, covering all 53 body endpoints at once.
  Covered by 8 tests in `src/server/formparam_test.go`.
- **Two tool forms remain JS-only, deliberately** — `/api/v1/network/ip`
  and `/api/v1/text/uuid` already own their parameterless paths
  (`apiNetworkCallerHandler`, `apiUUIDHandler`), so registering a fallback
  there would have silently changed an existing endpoint's meaning.
  Follow-up: retarget the network-IP template to
  `/api/v1/geo/ip?ip=…`, which serves the same lookup with no JS. UUID's
  `?version=` works without JS; only the batch `count` form does not.
- **`cli.yml` is now schema-complete against AI.md 44984-45061 —
  implemented 2026-09-02.** `src/client/config/config.go` gained the
  missing `output`, `logging`, and `cache` sections, the top-level `debug`
  key, and the absent fields on `server` (`api_version`, `timeout`,
  `retry`, `retry_delay`), `auth` (`token_file`), and `tui` (`enabled`,
  `unicode`), each defaulted to the spec's documented value. Every new
  setting is wired to real behaviour rather than parsed and ignored:
  `api.NewWithOptions` consumes the timeout/retry settings,
  `tui.enabled: false` forces CLI mode in `cmd.Execute`, `output.format`
  sits between `--output` and `defaults.output` in the format precedence
  chain, `auth.token_file` is a resolution fallback, and `debug` ORs with
  `--debug`. Malformed durations fall back to the compiled default rather
  than failing startup, per PART 5.
- **The CLI had no HTTP retry logic at all — added 2026-09-02.**
  `src/client/api/client.go` now loops `attempt()` up to `server.retry`
  times spaced by `server.retry_delay`. `isRetryable()` retries transport
  errors, 429, and 5xx, and never retries any other 4xx, per PART 9's
  "4xx errors are never retried". Three tests in `client_test.go` prove
  each branch.
- **Six previously-listed items were verified stale and removed** rather
  than re-implemented, because the tree already satisfies them:
  `main.BuildEpoch` (AI.md's "Embedded Build Info" specifies `BuildDate` is
  *derived* from the `BuildEpoch` ldflag at process start — `src/main.go:61`
  does exactly that, and Makefile/Dockerfile/all CI workflows stamp
  `BuildEpoch` consistently); systemd `ProtectHome` (already `yes` at
  `service_managers.go:38` with all four `ReadWritePaths`, asserted by
  `service_lifecycle_test.go:519`); the Windows Virtual Service Account
  (`service.go:27` defines `NT SERVICE\api`, used at
  `service_windows.go:41`, asserted at `service_lifecycle_test.go:708`);
  `--service --uninstall` directory/user removal (`service.go:218` confirms
  then 232 `RemoveAll`s all five dirs); the service worker `CACHE_VERSION`
  (it is `'v__SERVER_VERSION__'`, a build-stamped placeholder, not a
  hardcoded literal); and `server.scheduler.*` config representation
  (`src/config/config.go:48` has `Scheduler SchedulerConfig`, defaulted at
  line 762).
- Latent JS bug — fixed 2026-09-02. `executeDateTimeTool()` in
  `src/server/static/js/app.js` built `/api/v1/datetime/now?tz=...` while
  `apiDateTimeNowHandler` (`src/server/server.go:2174`) reads `?timezone=`,
  so the timezone field had never worked. The JS now sends `timezone`.
- 14 WCAG AA contrast failures in the canonical palette — fixed 2026-09-02
  in `src/common/theme/colors.go` and the three stylesheets together. Dark
  `--on-color` was doing two jobs: text on the saturated fills (where white
  measured 1.72–2.65:1) and text on the theme-invariant dark chip used by
  `.toast`/`.update-banner`/`.code-content`. The roles were split —
  `--on-color` is now theme-scoped (`#1a1b26` on dark, 6.46–9.96:1) and a
  new `--on-dark-color` keeps the invariant chip role. Also raised: dark
  `--text-muted` `#565f89`→`#868eb3` (5.32:1), dark `--border-color`
  `#414868`→`#656f9f` (3.52:1), light `--primary` `#2e7de9`→`#186ce0`
  (4.93:1), light `--text-muted` `#6172b0`→`#5466a8` (4.51:1), light
  `--border-color` `#c0caf5`→`#7d8bca` (3.27:1), and light `--focus-ring`
  re-derived from the new primary (3.81:1). Borders were solved against
  both the background and the surface, since cards render borders on the
  surface. AI.md dictates none of these hex values — its `ThemePalette`
  block is a Dracula example the project already departs from — so only its
  binding "both themes MUST pass WCAG AA" requirement applied.
- A stale on-disk `country.mmdb` will be ignored and re-downloaded under
  the new `geo-whois-asn-country.mmdb` name. No migration or cleanup was
  added because the spec does not call for one.
- i18n gaps in the new a11y markup: the mobile menu toggle borrows
  `nav.main_navigation` where a dedicated `nav.toggle_navigation` would
  read better; footer "Preferences" and header "User menu"/"Login"/
  "Install App" are still hardcoded English with no translation key.
- `app.js` should announce into the new server-rendered `#a11y-status` /
  `#a11y-alert` live regions instead of creating its own runtime announcer,
  and should return focus to the trigger element on modal close — the one
  part of the modal pattern that native `<dialog>` does not handle.
- PART 30 a11y remains partly open inside `src/server/template/page/**`:
  heading hierarchy, form label association, `alt` text and page-level
  `<dialog>` ARIA. The layout, partials and CSS are done.

### Verified-stale entries removed 2026-09-02

Four backlog items were re-checked against the tree and found already
satisfied; they were removed rather than carried forward:

- `--service --disable` was recorded as "systemd only disables, must also
  stop". `sysservice.Disable()` (`src/sysservice/service.go:294`) already
  runs `systemctl stop` before `systemctl disable`, and every other manager
  arm stops first too.
- OpenRC and SysVinit init-script templates were recorded as missing.
  Both exist (`installOpenRC` and `installSysVinit` in
  `src/sysservice/service_managers.go:150,228`).
- The GeoIP CC BY 4.0 attribution was recorded as appearing "nowhere in the
  tree". It was already verbatim in `LICENSE.md:58-71` and
  `page/about.tmpl:24-25`, exactly as AI.md:28247-28255 requires.
- `backup_daily` was recorded as "registered disabled by default". It is
  registered `Enabled: true` on `0 2 * * *` at
  `src/scheduler/tasks.go:89-97`, matching PART 18.

### Spec conflicts resolved in AI.md's favour

- The `prefers-reduced-motion` block uses `!important`, prescribed verbatim
  by AI.md PART 16 (~line 22562) and functionally required to override
  animation declarations. This contradicts `.claude/rules/frontend-rules.md`'s
  "never `!important` except print styles". AI.md wins; a comment above the
  block records why.
- PART 19 states the admin panel must let operators save GeoIP presets, but
  this project has no admin web UI — the spec's own frontend rules and
  IDEA.md's non-goals both forbid one. Implemented as config-file-only with
  listing and validation helpers. User decision (2026-09): "follow AI.md,
  there is no Admin UI!" — no web UI of any kind (admin-labeled or
  standalone) will be built for this. GeoIP presets remain config-file-only,
  editable via `server.yml` and surfaced read-only via the CLI/status output
  where useful.

### Resolved decisions (user, 2026-09)

- Dark-theme filled controls (primary/danger buttons, badges, the toggle
  knob) now render dark text instead of white, because white failed WCAG at
  1.72–2.65:1 on the pastel fills. User decision: ship it as implemented.
- `docs/stylesheets/light.css` `--light-accent-orange: #ff8c00` fails WCAG
  AA at roughly 2.2:1 (`#b35c00` would pass). The failing value comes from
  AI.md itself. User decision: keep AI.md's literal value unchanged — this
  is a deliberate, documented exception to the project's WCAG-AA rule and
  must not be "corrected" in any future pass.
- `src/server/static/css/admin.css` implemented a full admin sidebar layout
  and was loaded unconditionally by `src/server/template/partial/head.tmpl`,
  contradicting the spec's "no admin web UI" rule. User decision: deleted
  the file and removed the `{{else if eq .Layout "admin"}}` branch that
  loaded it from `head.tmpl`.
- `site.txt` / `{official_site}`: the canonical hosted URL is unknown and
  must not be inferred. User decision: leave unset for now; `site.txt` is
  not created and any `{official_site}` reference stays unresolved until
  the user provides the real value.
