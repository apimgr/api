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

## GraphQL resolver is a stub (decision needed)

- `src/graphql/graphql.go` `executeQuery()` is a hardcoded placeholder: it
  pattern-matches on the query string and returns fixed values (uptime `3600`,
  version `1.0.0`, commit `unknown`) plus a default "full resolver
  implementation in progress" message. `ResolveFunc` (line 35) is effectively
  dead — the resolver tree is never used. This violates api-rules (GraphQL must
  be real and stay in sync with REST) and ai-rules (no stubs/placeholders).
  Also: the `json.NewEncoder(w).Encode(resp)` return error at the handler is
  ignored. Decision: build real resolvers backed by the same services the REST
  handlers use, or remove the GraphQL surface until it can be implemented.

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

## Low test coverage (below the 60% gate)

- Several service packages are under the 60% coverage gate and need tests
  (baseline pre-fix figures): `language` ~2%, `sysservice` ~6%, `tor` ~18%,
  `research` ~21%, `parse` ~28%, `convert` ~32%, `paths` ~32%, `datetime`
  ~33%, `network` ~44%, `math` ~53%, `geoip` ~55%. Add table-driven tests to
  bring each to >=60%.
