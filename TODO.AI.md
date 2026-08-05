# TODO (AI-tracked)

Backlog surfaced by the project health audit (2026-08-05). Items here are
either decision-level (need an owner call before implementing) or larger than
a safe mechanical audit fix. Security/logic/doc fixes that were safe and
mechanical have already been applied in-tree and are not listed here.

## Frontend sub-tool pages

- Remaining ~240 per-tool detail pages linked from the 21 category pages have
  neither a template (`template/page/tools/{category}/{tool}.tmpl`) nor a route
  yet. PART 16 requires a frontend route mirroring every API route. Referenced
  from `src/server/server.go` (`toolPages()` docstring and the category-page
  loop). Wire each remaining sub-tool page to its API route.

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

## Docs completeness (ReadTheDocs)

- Missing required pages per testing-rules: `docs/security.md` and
  `docs/integrations.md` do not exist. Required set is index, installation,
  configuration, api, cli, security, integrations, development.
- `docs/admin.md` exists but contradicts the IDEA.md non-goal "no admin web
  panel / no admin UI". Decision: remove it, or repurpose its content into
  operator-CLI documentation (`--service`, `--maintenance`) which is the actual
  administration surface.
- `docs/configuration.md` should document the `API_BACKUP_PASSWORD` and
  `SMTP_*` environment overrides (verify current coverage while there).

## Low test coverage (below the 60% gate)

- Several service packages are under the 60% coverage gate and need tests
  (baseline pre-fix figures): `language` ~2%, `sysservice` ~6%, `tor` ~18%,
  `research` ~21%, `parse` ~28%, `convert` ~32%, `paths` ~32%, `datetime`
  ~33%, `network` ~44%, `math` ~53%, `geoip` ~55%. Add table-driven tests to
  bring each to >=60%.
