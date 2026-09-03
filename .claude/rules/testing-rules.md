# Testing & Docs Rules (PART 28, 29, 30)

⚠️ **These rules are NON-NEGOTIABLE. Violations are bugs.** ⚠️

## CRITICAL - NEVER DO
- Never run Go binaries, `go build`, or `go test` directly on the host — always inside a container (Docker `casjaysdev/go:latest` for building; Alpine for quick container tests; Incus `debian:latest` for full OS/systemd tests)
- Never run `docker-compose.yml` or `docker-compose.dev.yml` for AI-driven testing — only `docker-compose.test.yml` via `tests/*.sh` scripts is allowed
- Never use a bare `/tmp` path or omit the org prefix for temp dirs — always `/tmp/{project_org}/{internal_name}-XXXXXX/`
- Never run `pkill -f`, `killall`, or `docker system prune`/broad sweeps — always identify the exact PID/container first
- Never SIGKILL a process without first attempting graceful SIGTERM
- Never treat Phase 2 binary-validation scripts (`./tests/*.sh`) as part of the CI gate — they are manual/developer-initiated only, never wired into CI
- Never commit with test coverage below 60% (`go test -cover` enforced gate)
- Never put non-RTD/MkDocs files inside `docs/` — that directory is strictly for ReadTheDocs/MkDocs content
- Never hardcode UI strings outside the i18n translation files — no user-facing string bypasses the translation layer
- Never ship a language selection UI without the full fallback chain: `?lang=` query param → cookie → `Accept-Language` header → default `en`
- Never fail a build silently on a missing/invalid i18n key — build-time key validation must catch it
- Never omit ARIA roles/labels, skip links, or keyboard focus management from interactive UI — a11y is not optional
- Never ship color choices that fail WCAG 2.1 AA contrast ratios (4.5:1 minimum)

## CRITICAL - ALWAYS DO
- Always follow the two-phase testing strategy: Phase 1 "Toolchain Gate" = `*_test.go` via `make test`, ≥60% coverage, runs pre-commit and in CI; Phase 2 "Binary Validation" = `./tests/*.sh` scripts, 100% endpoint coverage, run manually/by the developer, never in CI
- Always test content negotiation across `Accept: text/html`, `text/plain`, `application/json`, and the `.txt` endpoint extension
- Always use the strict temp dir structure `/tmp/{project_org}/{internal_name}-XXXXXX/` for any test scratch space
- Always generate config files at runtime rather than committing them — tests must not rely on committed config artifacts
- Always identify the exact PID or container name before any stop/kill operation, and prefer graceful shutdown (SIGTERM) before SIGKILL
- Always keep `docs/` scoped strictly to ReadTheDocs/MkDocs content, with the required pages: `index.md`, `installation.md`, `configuration.md`, `api.md`, `cli.md`, `security.md`, `integrations.md`, `development.md`
- Always maintain both `docs/stylesheets/dark.css` and `docs/stylesheets/light.css` in sync, keeping WCAG AA contrast in both themes
- Always support all 7 required languages: en, es, zh, fr, ar, de, ja
- Always resolve language via the fallback chain: `?lang=` query param → cookie → `Accept-Language` header → default `en`
- Always embed translations via `go:embed` and share a single `src/common/i18n` package across all binaries
- Always validate translation keys at build time
- Always use CLDR plural categories for pluralized strings and support RTL layout for Arabic
- Always translate CLI/agent/server output as well as web UI, API/Swagger/GraphQL responses, and email content
- Always implement WCAG 2.1 AA accessibility: skip links, ARIA live regions/modal/landmark patterns, visible focus management, screen-reader announcements, sufficient color contrast, keyboard shortcuts, and `.sr-only` CSS for screen-reader-only text
- Always update `docs/security.md` to reflect the actual enabled `/.well-known/*` entries and note that unknown entries return 404

## Key Rules Summary
- **Host System Safety Rule**: no host-affecting commands outside a container or VM boundary — this applies to all build, test, and binary-execution steps
- **Container tiers**: Docker `casjaysdev/go:latest` (build), Alpine `alpine:latest` (quick container tests), Incus `debian:latest` (full OS/systemd integration tests)
- **Test scripts**: `tests/docker.sh`, `tests/incus.sh`, `tests/run_tests.sh` drive Phase 2 binary validation; these exercise every endpoint including content-negotiation variants
- **Coverage enforcement**: `go test -cover ./...` must report ≥60%; this is a pre-commit and CI gate (Phase 1 only)
- **ReadTheDocs stack**: `mkdocs.yml`, `.readthedocs.yaml`, `docs/requirements.txt` at repo root/docs; dark/light theme CSS under `docs/stylesheets/`; project-specific palette customization allowed but must preserve WCAG AA and be documented in the project's AI.md
- **Required docs pages**: `docs/index.md` (overview/quick start/features/links), `docs/installation.md` (Docker/binary/systemd), `docs/configuration.md` (server.yml + env var overrides), `docs/api.md` (REST/Swagger/GraphQL), `docs/security.md` (rate limiting, public security endpoints, `/.well-known/security.txt`, `/server/security`, `/server/contact?security_id=`), `docs/integrations.md` (identity providers, discovery endpoints, platform integrations), `docs/development.md` (build/test/contribute), `docs/cli.md`
- **i18n architecture**: single shared `src/common/i18n` package, embedded via `go:embed`, covers Go templates, HTML, JS, API/Swagger/GraphQL, email, and CLI/agent/server text output
- **i18n language list**: en, es, zh, fr, ar, de, ja — Arabic requires RTL support; CLDR plural rules govern all pluralized strings; date/time/number formatting is locale-aware
- **Language resolution order**: `?lang=` query param → cookie → `Accept-Language` header → default `en`
- **a11y core requirements**: skip-to-content links, ARIA live regions, modal dialog patterns, landmark roles, accessible forms, managed focus order, screen-reader announcements for dynamic content, keyboard shortcuts, `.sr-only` CSS class for visually-hidden screen-reader text, and documented a11y testing requirements
- **Placeholders**: this project is `{project_name}=api`, `{project_org}=apimgr`, `{internal_name}=api`, `{internal_org}=apimgr`

For complete details, see AI.md PART 28, 29, 30
