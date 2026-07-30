# Testing, Documentation & I18n/A11y Rules (PART 28, 29, 30)

⚠️ **These rules are NON-NEGOTIABLE. Violations are bugs.** ⚠️

**IDEA.md override:** no accounts/sessions/admin panel — nothing in PART 28
(testing), PART 29 (docs), or PART 30 (i18n/a11y) below implies otherwise;
never add auth-flow tests, admin-panel docs, or login-page a11y coverage.

## CRITICAL - NEVER DO
- Never run a binary, `go build`, or `go test` directly on the host — always
  Docker (`casjaysdev/go:latest` build, `alpine:latest` quick container
  test) or Incus (`debian:latest`/`images:debian/trixie` full OS/systemd
  test)
- Never use a bare `/tmp/` or unprefixed `mktemp -d` for test data — always
  `mktemp -d "${TMPDIR:-/tmp}/{project_org}/{project_name}-XXXXXX"`
- Never run `docker-compose.yml` or `docker-compose.dev.yml` for AI/automated
  testing — only `docker-compose.test.yml`, and prefer the `tests/` scripts
  over a raw temp-dir compose copy
- Never treat `./tests/*.sh` (Phase 2, binary validation) as a substitute
  for `*_test.go` (Phase 1, toolchain gate) or vice versa — both are
  required, they cover different things
- Never defer writing/updating `*_test.go` to a later pass — create/update
  it in the SAME work pass as the logic change
- Never use `pkill -f`/`pkill {name}`/`killall`/`kill -9` as a first resort,
  `docker rm $(docker ps -aq)`, `docker system prune`, or any other broad
  sweep — kill/remove only the exact, verified PID/container/image for this
  project
- Never let translation files drift — every language file must have the
  identical key set to `en.json`, no empty values, matching interpolation
  variables, and no orphaned keys (build-time validated)
- Never skip WCAG 2.1 AA requirements (focus rings, ARIA labels, 4.5:1
  contrast, skip link, `aria-live`, `prefers-reduced-motion`) when adding
  frontend UI
- Never let `docs/` or `mkdocs.yml`/`.readthedocs.yaml` drift from actual
  config/CLI/API/deployment behavior

## CRITICAL - ALWAYS DO
- Maintain both required test phases: **Phase 1 — Toolchain Gate**
  (`*_test.go` via `make test`, ≥60% Go coverage, fast, pre-commit gate) and
  **Phase 2 — Binary Validation** (`./tests/*.sh` via
  `./tests/run_tests.sh`, 100% endpoint coverage, tests the compiled binary
  end-to-end, manual/developer-initiated — not an automatic CI gate)
- Maintain the three root `tests/` scripts: `tests/run_tests.sh`
  (auto-detects Incus/Docker and dispatches), `tests/docker.sh` (builds via
  `casjaysdev/go:latest` → `binaries/`, tests in `alpine:latest`),
  `tests/incus.sh` (same build, tests in `debian:latest`/`images:debian/trixie`
  with full systemd — preferred when Incus is available)
- Test scripts MUST: use persistent `GO_CACHE`/`GO_BUILD` host cache dirs,
  build server + client (if `src/client/` exists) to `binaries/` (same
  output as `make build`, Makefile-first with a direct-docker fallback),
  install `curl bash file jq` in the Alpine test container, verify
  `--version`/`--help` for both binaries, perform a **binary rename test**
  (copy to a new name, confirm `--help` reflects the actual filename, not a
  hardcoded string), read `server.token` from the generated `server.yml`
  for any auth-required calls, test API endpoints with both the `.txt`
  extension and `Accept` header variants, test frontend smart content
  detection (JSON/HTML/text), run full CLI functionality tests against the
  live server (not just `--help`), and always `trap` cleanup on exit
  (kill server PID, delete container) with a non-zero exit code on failure
- Test content negotiation on every route: frontend routes get `text/html`
  AND `text/plain` checks; API routes get `application/json` AND
  `text/plain` checks; every `.txt`-suffixed endpoint gets a dedicated check
- Since this project has no auth (IDEA.md non-goal), Open API tests verify
  responses and rate-limiting (expect `429` after a burst), never
  login/token-bypass scenarios
- Follow strict process/container scoping: identify the exact PID/container
  first, graceful `kill`/`docker stop` before force, verify before removing,
  only ever touch resources named/tagged for this project
- Keep `docs/` (mkdocs-based, ReadTheDocs) in sync with config/CLI/API/
  deployment changes; required docs: `index.md`, `installation.md`,
  `configuration.md`, `api.md`, `security.md`, `integrations.md`,
  `development.md`, plus `mkdocs.yml`/`.readthedocs.yaml`/
  `docs/requirements.txt` and dark/light theme CSS
- All shell scripts under `tests/` are WTFPL-licensed (`# @@License : WTFPL`
  header) per PART 28's shell-completions/scripting convention

## KEY DECISIONS (pre-answered)
| Question | Answer | Spec Reference |
|----------|--------|----------------|
| What are the two required test phases? | Phase 1 (`*_test.go`, `make test`, ≥60% coverage) and Phase 2 (`./tests/*.sh`, `./tests/run_tests.sh`, 100% endpoint coverage) | PART 28, Testing Strategy |
| Where do the 3 test scripts live, and did they exist before this pass? | `tests/run_tests.sh`, `tests/docker.sh`, `tests/incus.sh` — none existed (only `tests/.gitkeep`); created in this pass | PART 28, Test Scripts |
| Which container images for testing? | Building = `casjaysdev/go:latest`; container test = `alpine:latest`; full-OS test = `debian:latest`/`images:debian/trixie` (Incus) | PART 28, Container Usage |
| Incus vs Docker preference? | Incus preferred (full systemd, ~5s startup); Docker is the always-available fallback (faster, ~1s, used when Incus absent) | PART 28, Incus vs Docker |
| Temp dir structure? | `{tmpdir}/{project_org}/{project_name}-XXXXXX/`, never bare `/tmp/` | PART 28, Temp Directory Structure |
| Is Phase 2 run automatically in CI? | No — manual/developer-initiated only | PART 28, Testing Strategy |
| Does IDEA.md's no-auth stance change Open-API testing? | Yes — tests verify responses + rate limiting only, never login/token-bypass, consistent with "no accounts" | IDEA.md non-goals; PART 28, Testing Open API Routes |
| Actual project API version / health route / default port used in test scripts? | `/api/v1/...` prefix confirmed in `src/server/handler/*.go` `@Router` annotations; health at `/server/healthz` + `/api/v1/server/healthz` + unversioned `/api/healthz` alias (`src/server/logger.go`, `src/server/server_test.go`); default port `64580` (`src/main.go`, matches PART 28's own example port) | Verified against actual repo source, not guessed |
| Does `src/client/` (the `api-cli` companion binary) exist? | Yes — `src/client/main.go` + `cmd/`/`api/`/`output/`/`tui/`/`config/`/`paths/` subpackages; test scripts build and exercise it | Verified against `/root/Projects/github/apimgr/api/src/client/` |
| i18n key-set validation? | Build-time Makefile-target validation: identical keys to `en.json`, no empty values, matching interpolation vars, no orphans | PART 30, Build-Time Validation |
| a11y baseline? | WCAG 2.1 AA (keyboard ops, focus rings, ARIA, 4.5:1 contrast, alt text, labels, aria-live, skip link, heading hierarchy, reduced-motion) | PART 30, Accessibility |
| Docs platform? | ReadTheDocs via `mkdocs.yml` + `.readthedocs.yaml`, `docs/` tree with the 7 required pages | PART 29, Required Files |

## TERMINOLOGY
| Term | Meaning |
|------|---------|
| Phase 1 (Toolchain Gate) | `*_test.go` unit tests via `make test`; ≥60% coverage; pre-commit |
| Phase 2 (Binary Validation) | `./tests/*.sh` integration tests against the compiled binary; 100% endpoint coverage; manual |
| `tests/run_tests.sh` | Auto-detects Incus/Docker, dispatches to `incus.sh` or `docker.sh` |
| `tests/docker.sh` | Builds + tests the binaries inside `alpine:latest` |
| `tests/incus.sh` | Builds + tests the binaries inside a full Debian/systemd Incus container |
| Binary rename test | Verifies `--help`/`--version` reflect the actual (possibly renamed) binary filename |
| Smart content detection | A route auto-responding HTML/text/JSON based on `Accept`/client type |
| RTD | ReadTheDocs — the documentation hosting platform this project targets |

## QUICK REFERENCE
- `make test` = Phase 1 (Go unit tests, ≥60% coverage); `./tests/run_tests.sh`
  = Phase 2 (compiled-binary integration tests) — both required, neither
  replaces the other
- Test scripts always build to `binaries/` via `casjaysdev/go:latest`,
  never `go build` on host
- Every test/build/cleanup path uses a project-org-prefixed temp dir or an
  exact-name-scoped container/PID — never broad kills or prunes
- API routes tested with `.txt` + `Accept` header variants; frontend routes
  tested for HTML/text/JSON smart detection
- No login/session/admin-panel testing anywhere — IDEA.md forbids the
  feature entirely
- `docs/` (mkdocs/RTD) and i18n/a11y baselines apply across the whole
  frontend and API surface, kept in sync with actual behavior

---
For complete details, see AI.md PART 28, PART 29, PART 30
