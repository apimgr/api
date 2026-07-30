# Makefile Rules (PART 25)

⚠️ **These rules are NON-NEGOTIABLE. Violations are bugs.** ⚠️

## CRITICAL - NEVER DO
- Never add a seventh Makefile target — only `dev`, `local`, `build`, `test`,
  `release`, `docker` exist; do not add `lint`, `fmt`, `install`, etc. as
  top-level targets
- Never use Makefile targets inside CI/CD workflows — CI/CD always uses
  explicit `go build`/`go test` commands (see PART 27); the Makefile is a
  local/developer convenience only
- Never guess the version — always resolve via `release.txt` (canonical) >
  `VERSION` env var > `devel` fallback; never hardcode a version string
- Never add a `v` prefix to a non-numeric-semver tag (`dev`, `beta`,
  timestamps) — only numeric semver (`1.2.3`) gets `v1.2.3`
- Never copy or symlink binaries between platform-specific output names —
  each of the 8 platform binaries must be built independently by `go build`
- Never skip a platform in `make build`/`make release` — all 8
  (linux/darwin/windows/freebsd × amd64/arm64) every time
- Never bind-mount ephemeral/anonymous Go caches — always use the persistent
  host paths (`GO_CACHE` → `~/go/pkg/mod`, `GO_BUILD` →
  `~/.cache/go-build/{project_name}`)
- Never build directly on host — every Makefile target that compiles Go code
  runs inside `casjaysdev/go:latest` via Docker

## CRITICAL - ALWAYS DO
- Keep exactly six targets: `dev` (quick local build to
  `${TMPDIR}/{project_org}/{project_name}-XXXXXX/`), `local` (production-like
  single-platform build + run for smoke testing), `build` (compile all 8
  release platforms to `binaries/`), `test` (Go unit tests with coverage
  inside Docker, ≥60% enforced), `release` (tag + cross-platform release
  binaries), `docker` (build the container image)
- Embed build info via `-ldflags`: `Version`, `CommitID`
  (`git rev-parse --short HEAD`), `BuildDate`, `OfficialSite` (from
  `site.txt` if present, never guessed)
- Name binaries `{project_name}[-type]-{os}-{arch}[.exe]` (`.exe` only on
  Windows); client binaries get `-cli` (or whatever `client_binary` from
  IDEA.md names it) inserted as `[-type]`
- Produce release artifacts: all 8 server binaries always, all 8 CLI
  binaries when `src/client/` exists, `version.txt`, and a source tarball
- Follow the Local Development Workflow stage order: coding → `make dev`,
  quick container test → Docker, Phase 1 toolchain gate → `make test`,
  Phase 2 binary validation → `./tests/run_tests.sh`, production-like test →
  `make local`, release → `make build`
- CGO_ENABLED=0 always; pass `-e GOFLAGS=-buildvcs=false` on every
  Docker-mounted Go build to avoid the mounted-`.git` UID mismatch failure

## KEY DECISIONS (pre-answered)
| Question | Answer | Spec Reference |
|----------|--------|----------------|
| How many Makefile targets? | Exactly 6: `dev`, `local`, `build`, `test`, `release`, `docker` | PART 25, Makefile Targets |
| Version source of truth? | `release.txt` (canonical) > `VERSION` env > `devel` | PART 25, Version Handling |
| When does a version get a `v` prefix? | Only numeric semver (`1.2.3`) → `v1.2.3`; never for `dev`/`beta`/timestamps | PART 25, Version Tag Rules |
| Binary naming pattern? | `{project_name}[-type]-{os}-{arch}[.exe]` | PART 25, Binary Naming |
| Build matrix? | 8 platforms: linux/darwin/windows/freebsd × amd64/arm64, no skips | PART 25, Build Matrix |
| Does this repo's actual `Makefile` match the spec example? | Yes — verified line-for-line against PART 25's Makefile Implementation block (same variable inference, LDFLAGS, `GO_DOCKER_RUN` pattern, all 6 targets) | Verified against `/root/Projects/github/apimgr/api/Makefile` |
| Are Makefile targets used in CI? | No — CI/CD always issues explicit `go build`/`go test` (PART 27); Makefile is local-dev only | PART 25 / PART 27, CI/CD vs Local Development |
| Go module cache paths? | `GO_CACHE` → `~/go/pkg/mod` (mod cache), `GO_BUILD` → `~/.cache/go-build/{project_name}` (build cache), both bind-mounted into `casjaysdev/go:latest` | PART 25, Go Caching |

## TERMINOLOGY
| Term | Meaning |
|------|---------|
| `release.txt` | Canonical version file at project root; wins over any env var or git tag |
| `site.txt` | Optional file holding the official site URL embedded as `OfficialSite`; never guessed if absent |
| `dev` target | Quick local build for iterative development, output to a temp dir |
| `local` target | Single-platform production-like build + run, for smoke testing before release |
| `build` target | Full 8-platform release compile to `binaries/` |
| `release` target | Tags + produces the full cross-platform release artifact set |
| `docker` target | Builds the container image via `docker/Dockerfile` |
| Stable release | Tag push, numeric semver, release name `v{version}` |
| Beta release | Push to `beta` branch, version `{YYYYMMDDHHMMSS}-beta`, no `v` prefix |
| Daily release | Scheduled (3am UTC) + push to main/master, single rolling `daily` release, max 1 kept |

## QUICK REFERENCE
- Six targets only — `dev`, `local`, `build`, `test`, `release`, `docker`;
  never add more
- Version precedence: `release.txt` > `VERSION` env > `devel`
- `v` prefix only for numeric semver, never for text/timestamp versions
- 8-platform matrix, every time, no exceptions
- Binaries never copied/symlinked between platform names — each built
  independently
- CI never calls `make` — explicit `go build`/`go test` only (PART 27)
- Actual repo `Makefile` verified to match PART 25's spec example exactly

---
For complete details, see AI.md PART 25
