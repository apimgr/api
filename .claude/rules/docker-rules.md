# Docker Rules (PART 26)

⚠️ **These rules are NON-NEGOTIABLE. Violations are bugs.** ⚠️

## CRITICAL - NEVER DO
- Never put `LABEL` blocks (static or ARG-driven) in `docker/Dockerfile` —
  Dockerfiles stay clean; ALL OCI labels/annotations are applied at build
  time by CI via `--label`/`docker/metadata-action` `annotations:`
- Never set `ENV MODE=...` in `docker/Dockerfile` — the binary defaults to
  production on its own; only compose files set `MODE` per environment
- Never `mkdir` application directories in the Dockerfile or entrypoint —
  the binary handles ALL directory/permission/user/Tor setup at startup
  based on env vars; the entrypoint's job is env defaults + signal handling
  + `exec` only
- Never use `docker-compose.yml` or `docker-compose.dev.yml` for AI/automated
  testing — those two are human-only; only `docker-compose.test.yml` is
  AI-usable, and only via the `tests/` scripts, never run directly in the
  project directory
- Never include a `build:` or `version:` key in any compose file; never use
  `.env` files or `${VAR}` interpolation syntax in compose — env vars are
  hardcoded per compose file
- Never run compose files from inside the project directory — always copy
  to a `mktemp -d "${TMPDIR:-/tmp}/{project_org}/{project_name}-XXXXXX"` dir
  first
- Never run the runtime container as root without justification — non-root
  user is required; the one sanctioned exception is `setcap
  cap_net_bind_service=+ep` on the binary to allow a non-root user to bind a
  privileged (<1024) port
- Never modify `ENTRYPOINT`/`CMD` casually — format is fixed:
  `ENTRYPOINT ["tini", "-p", "SIGTERM", "--", "/usr/local/bin/entrypoint.sh"]`

## CRITICAL - ALWAYS DO
- Multi-stage build: builder stage `FROM casjaysdev/go:latest`, runtime
  stage `FROM alpine:latest`
- Runtime image installs `git`, `curl`, `bash`, `tini`, `tor`
- Binary lands at `/usr/local/bin/{project_name}`, entrypoint at
  `/usr/local/bin/entrypoint.sh`, init process is `tini`
- Container listens on internal port 80 always (external port mapping is a
  compose/runtime concern, never baked into the image)
- `STOPSIGNAL SIGRTMIN+3`; `HEALTHCHECK` timing: `start-period=10m`,
  `interval=5m`, `timeout=15s`
- Entrypoint script MUST end with `exec "$@"` so signals propagate correctly
  to the main process
- Container paths: config `/config/{project_name}/` (server.yml, ssl/,
  tor/), data `/data/{project_name}/` (security, geoip, uploads, cache,
  tor/), db `/data/db/sqlite/server.db` (or `/data/db/valkey/`), logs
  `/data/log/{project_name}/`, backups `/data/backups/{project_name}/`
- Three compose files required: `docker-compose.yml` (prod, `:latest` tag,
  persistent cache, no DEBUG/MODE set), `docker-compose.dev.yml` (dev,
  `:devel` tag, DEBUG=1/MODE=dev, human-use only), `docker-compose.test.yml`
  (test, `:latest` tag, ephemeral tmpfs cache, DEBUG=1/MODE=dev, AI/automated
  testing use)
- Every compose file: `name: {project_name}`, `container_name:
  {project_name}-app`, `pull_policy: always`, `restart: always`, mandatory
  `x-logging:` anchor
- Port mapping: dev `{port}:80`, prod `172.17.0.1:{port}:80`
- Image tags on release: `:latest`, `:{version}`, `:{YYMM}`, `:{commit}`;
  dev-only tags (`:dev`, `:test`) never pushed to a registry

## KEY DECISIONS (pre-answered)
| Question | Answer | Spec Reference |
|----------|--------|----------------|
| Builder / runtime base images? | `casjaysdev/go:latest` (builder) / `alpine:latest` (runtime) | PART 26, Dockerfile Requirements |
| Where do OCI labels get applied? | CI, via `--label`/`docker/metadata-action` annotations — never `LABEL` in the Dockerfile | PART 26, OCI Meta Labels |
| Who creates app directories? | The binary, at startup, from env vars — never the Dockerfile or entrypoint | PART 26, Dockerfile Rules |
| Does actual `docker/Dockerfile` match spec? | **No — 3 known deviations** (see below); flagged, not fixed, per task scope | Verified against `/root/Projects/github/apimgr/api/docker/Dockerfile` |
| Does actual `entrypoint.sh` match spec? | **No — deviation**: it performs `setup_directories()` (mkdir + chown/chmod for Tor) and starts Tor itself, where spec says the binary owns all setup and Tor startup, and the entrypoint should only set env defaults, trap signals, and `exec` | Verified against `docker/rootfs/usr/local/bin/entrypoint.sh` |
| Internal container port? | Always 80 | PART 26, Container Networking |
| Non-root exception? | `setcap cap_net_bind_service=+ep` to bind <1024 as non-root — actual Dockerfile uses this correctly | PART 26, Dockerfile Rules |
| Which compose files can AI run? | Only `docker-compose.test.yml`, and only via `tests/` scripts in a temp-dir copy | PART 28 (cross-ref), AI Docker Compose Rules |
| Can compose files use `.env`/`${VAR}`? | No — hardcoded env vars only, no `.env`, no `${VAR}` interpolation | PART 26, Docker Compose Requirements |

## TERMINOLOGY
| Term | Meaning |
|------|---------|
| Builder stage | `casjaysdev/go:latest` stage that compiles the Go binaries |
| Runtime stage | `alpine:latest` stage that ships only the compiled binary + minimal runtime deps |
| OCI labels | 14 required metadata labels/annotations, applied by CI at build time, never as static `LABEL` lines |
| `docker-compose.yml` | Production compose, human-only, `:latest` tag |
| `docker-compose.dev.yml` | Development compose, human-only, `:devel` tag, DEBUG/MODE set |
| `docker-compose.test.yml` | Test compose, the ONLY one AI/automation may run, ephemeral cache |
| `rootfs/` | Docker build-time overlay directory containing `entrypoint.sh` |
| `setcap` exception | The one sanctioned way to let a non-root runtime user bind a privileged port |

## QUICK REFERENCE
- `docker/Dockerfile` (prod image) + `docker/Dockerfile.dev` (dev image) +
  `docker/rootfs/usr/local/bin/entrypoint.sh` (build-time overlay)
- No `LABEL` blocks, no `ENV MODE`, no manual `mkdir` in the Dockerfile —
  all three are known live discrepancies in this repo's current
  `docker/Dockerfile` (see KEY DECISIONS) and should be tracked in
  `TODO.AI.md` if not already
- `entrypoint.sh` currently does directory setup + Tor startup itself,
  which is spec-owned-by-binary territory — also a known discrepancy, not
  fixed by this pass
- Three compose files, hardcoded env vars, no `build:`/`version:` keys,
  `x-logging:` anchor mandatory
- AI/automated testing: `docker-compose.test.yml` only, run from a temp dir,
  never the project directory, preferably invoked via `tests/docker.sh`

---
For complete details, see AI.md PART 26
