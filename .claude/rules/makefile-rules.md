# Makefile Rules (PART 25)

⚠️ **These rules are NON-NEGOTIABLE. Violations are bugs.** ⚠️

**The Makefile is for LOCAL DEVELOPMENT ONLY — it is NOT used by CI/CD.** CI/CD pipelines
(GitHub Actions, Gitea Actions, GitLab CI) implement their own build/test/release/docker
steps independently; they do not shell out to `make`.

## CRITICAL - NEVER DO
- Never add targets beyond the six core ones: `dev`, `local`, `build`, `test`, `release`, `docker`.
- Never hardcode `PROJECT_NAME`/`PROJECT_ORG` — always infer from `git remote get-url origin` or directory path.
- Never add `v` prefix to a non-numeric version tag (`vdev`, `vbeta`, `vdaily`, `v20251218` are all wrong).
- Never double the `v` prefix (`vv1.2.3`).
- Never build on the host directly — all `dev`/`local`/`build`/`test` targets run inside Docker (`casjaysdev/go:latest`).
- Never symlink or copy binaries out of `binaries/` for "installing," running, or testing — run them in place (`./binaries/app`). The only exception is the CI/CD release process copying stripped binaries into `releases/`.
- Never skip coverage enforcement (`>= 60%` minimum, override upward only in IDEA.md) or bypass it with `-short`, `-count=0`, or ignore directives.
- Never let `make dev` embed version info via `-ldflags` — it's a fast, unversioned build.

## CRITICAL - ALWAYS DO
- Always run `clean` first in `build` and `local` targets.
- Always use `release.txt` as the canonical version source when present (wins over `VERSION` env var and `devel` fallback).
- Always embed `Version`, `CommitID`, `BuildDate` (ISO 8601 UTC), and `OfficialSite` via `-ldflags` in `build`, `local`, and `release` builds (not `dev`).
- Always build for all 8 platforms in `build`/`release`: linux, darwin, windows, freebsd × amd64, arm64.
- Always use `CGO_ENABLED=0` and `GOFLAGS=-buildvcs=false` for Docker Go builds.
- Always output `make dev` builds to an isolated temp dir: `${TMPDIR:-/tmp}/${PROJECT_ORG}/${PROJECT_NAME}-XXXXXX/`.
- Always output `make local`/`make build` binaries to `binaries/` and `make release` archives to `releases/`.
- Always strip binaries before copying into a release.

## Key Rules Summary

### Six targets
| Target | Purpose | Output |
|---|---|---|
| `dev` | fast unversioned build | `${TMPDIR}/apimgr/api-XXXXXX/` |
| `local` | production-config build, local platform | `binaries/` |
| `build` | full release, all 8 platforms | `binaries/` |
| `test` | unit tests + coverage gate (>=60%) | coverage report |
| `release` | GitHub release with source archive | `releases/` |
| `docker` | build + push multi-arch image | `$REGISTRY` |

### Versioning
- `release.txt`: single-line canonical version (SemVer `MAJOR.MINOR.PATCH`), wins over env/tag/derived.
- `site.txt` (optional): single-line official hosted URL, wins over IDEA.md/README/env/CI secrets; never guess this value.
- Version priority: `release.txt` > `VERSION` env var > `devel` fallback.
- `v` prefix ONLY on numeric semver tags (`0.2.0`→`v0.2.0`); never on text (`dev`, `beta`, `daily`) or timestamps (`20251218060432`).

### Binary naming
- Pattern: `{project_name}[-type]-{os}-{arch}[.exe]`. Server: `api`, `api-linux-amd64`. CLI (if `src/client/` exists): `api-cli`, `api-cli-linux-amd64`.
- musl builds: strip before release, final name has no `-musl` suffix.

### Build matrix
linux/darwin/windows/freebsd × amd64/arm64 (8 combinations).

### Caching
- `GO_CACHE` (module cache) defaults to `~/go/pkg/mod` → container `/usr/local/share/go/pkg/mod`.
- `GO_BUILD` (compile cache) defaults to `~/.cache/go-build/api` → container `/usr/local/share/go/cache`, scoped per project.

### Never copy/symlink binaries
Binaries stay in `binaries/` until explicitly moved during the CI/CD release process (built, stripped, uploaded). No `ln -s`, no `cp` to system dirs, no local "install."

### Release artifacts (every GitHub release)
All 8 server binaries, all 8 CLI binaries (if CLI exists), `version.txt`, `api-{version}-source.tar.gz` (excludes `.git`, `.github`, `.gitea`, `binaries/`, `releases/`).

### Release types
| Type | Trigger | Version format | v prefix | Max releases |
|---|---|---|---|---|
| Stable | semver tag push | `X.Y.Z` | yes | unlimited |
| Beta | push to `beta` branch | `{YYYYMMDDHHMMSS}-beta` | no | unlimited |
| Daily | 3am UTC schedule + push to main | `{YYYYMMDDHHMMSS}` | no | 1 (rolling, replaces previous) |

- `make release` = manual local release only (uses `gh` CLI, deletes/recreates tag+release, marks `--latest`).
- All automated releases (stable/beta/daily) run through CI/CD, not the Makefile.

### Local development workflow order
`make dev` (iterate) → run in Docker to debug → `make test` (pre-commit gate) → `./tests/run_tests.sh` (binary validation) → `make local` (prod-config test) → `./tests/incus.sh` (preferred full systemd test) → `make build` (cross-platform, before release).

For complete details, see AI.md PART 25
