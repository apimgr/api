# CI/CD Rules (PART 27)

⚠️ **These rules are NON-NEGOTIABLE. Violations are bugs.** ⚠️

## CRITICAL - NEVER DO
- Never use a Makefile inside CI/CD workflows — CI/CD build steps call `go build` explicitly with full flags
- Never use host cache directories inside CI/CD — use CI-native caching (`actions/cache`, GitLab `cache:`, Jenkins workspace-scoped dirs)
- Never omit `-buildvcs=false` from CI/CD `go build` invocations (mounted/checked-out `.git` UID mismatches break vcs stamping)
- Never skip the LDFLAGS version-stamping vars (`main.Version`, `main.CommitID`, `main.BuildDate`, `main.OfficialSite`) on any release/daily/beta/docker build
- Never build fewer than all 8 platform targets (linux/darwin/windows/freebsd × amd64/arm64) for release, beta, and daily workflows
- Never append a `-musl` suffix to any binary name
- Never tag the `latest` Docker tag on beta or devel builds — `latest` is release-tag only
- Never skip OCI labels/annotations (`org.opencontainers.image.*`) on `docker buildx build`
- Never let coverage drop below 60% in the CI test gate (`go test -cover`)
- Never pin third-party GitHub/Gitea/Forgejo Actions to a tag — pin to a full commit SHA
- Never run `docker-compose.yml`/`docker-compose.dev.yml` from CI or as AI actions — CI uses `docker buildx build` / `docker run` directly
- Never leave workflow concurrency ungated — branch pushes must auto-cancel superseded runs; tag/release runs must never auto-cancel

## CRITICAL - ALWAYS DO
- Always keep CI/CD build patterns and local dev patterns strictly separate: CI/CD = explicit `go build` + CI-native cache; local dev = Makefile-first + host cache dirs + `casjaysdev/go:latest` in Docker
- Always build the full 8-platform matrix: linux, darwin, windows, freebsd × amd64, arm64
- Always build the CLI (`./src/client`) as 8 additional `-cli` suffixed binaries when `src/client/` exists (`HAS_CLI` gate)
- Always stamp `LDFLAGS` with `main.Version`, `main.CommitID`, `main.BuildDate`, `main.OfficialSite`
- Always run `go test -v -cover ./...` as a Test stage/step before Release/Docker stages
- Always tag Docker images per build type: release → `commit_id`, `version`, `latest`, `YYMM`; beta → `commit_id`, `beta`, `devel`; all other branches → `commit_id`, `devel`
- Always set full OCI labels and buildx manifest annotations (vendor, authors, title, description, licenses=MIT, version, created, revision, url, source, documentation) on every Docker build
- Always use `docker buildx build --platform linux/amd64,linux/arm64` for multi-arch Docker images
- Always mirror workflow triggers/equivalence exactly across providers: `release.yml`/tag push ↔ `BUILD_TYPE=='release'`; `beta.yml`/beta branch ↔ `BUILD_TYPE=='beta'`; `daily.yml`/schedule+main ↔ `BUILD_TYPE=='daily'`; `docker.yml`/all branches ↔ Docker stage always runs
- Always clean up the workspace/container after every build (`cleanWs()` in Jenkins post-always; `--rm` on every `docker run`)
- Always give docker containers a unique random name: `--name "${PROJECT_NAME}-$(tr -dc 'a-z0-9' </dev/urandom | head -c8)"`

## Key Rules Summary
- **Providers supported**: GitHub Actions, Gitea Actions, Forgejo Actions, GitLab CI, Jenkins — all four must stay behaviorally equivalent (see Triggers/Feature comparison tables in AI.md PART 27)
- **Workflow files**: `ci.yml` (test gate on push/PR), `release.yml` (tag push, stable), `beta.yml` (beta branch), `daily.yml` (schedule + main/master), `docker.yml` (all branches and tags)
- **Toolchain image**: `casjaysdev/go:latest` for all Go builds in CI containers — `CGO_ENABLED=0`, `GOOS`/`GOARCH` set per target, `-trimpath -ldflags "${LDFLAGS}"`
- **Go cache mounts (CI)**: `${GO_CACHE:-$HOME/go/pkg/mod}` → `/usr/local/share/go/pkg/mod`; `${GO_BUILD:-$HOME/.cache/go-build/${PROJECT_NAME}}` → `/usr/local/share/go/cache`
- **Binary output naming**: `{project_name}-{os}-{arch}` (add `.exe` for windows); CLI variants: `{project_name}-cli-{os}-{arch}`
- **Registry**: `REGISTRY = "{registry_host}/{project_org}/{internal_name}"` — works with ghcr.io, registry.gitlab.com, gitea/forgejo, docker.io; login via `echo "${GIT_TOKEN}" | docker login <host> -u <org> --password-stdin`
- **Jenkins-specific**: requires `amd64` and `arm64` agent labels, Docker + buildx on the amd64 runner, credentials as Secret text (`github-token`, `gitea-token`, `forgejo-token`, `gitlab-token`, `dockerhub-token`) with provider-specific token scopes (e.g. GitHub `write:packages`, `read:packages`, `delete:packages`)
- **Release artifacts**: `releases/version.txt` + copied platform binaries; stable release additionally produces a `{project_name}-{version}-source.tar.gz` excluding `.git`, `.github`, `.gitea`, `.forgejo`, `binaries`, `releases`, `*.tar.gz`
- **Makefile has exactly 6 targets**: `dev`, `local`, `build`, `test`, `release`, `docker` — CI never invokes these; CI calls `go build`/`go test`/`docker buildx build` directly
- **Docker base**: tini as init, Alpine base image
- **Concurrency policy**: branch pushes auto-cancel in-flight runs for the same ref; tag/release runs never auto-cancel

For complete details, see AI.md PART 27
