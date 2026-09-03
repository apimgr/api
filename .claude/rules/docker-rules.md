# Docker Rules (PART 26)

⚠️ **These rules are NON-NEGOTIABLE. Violations are bugs.** ⚠️

## CRITICAL - NEVER DO
- Never place a Dockerfile or docker-compose.yml in the project root — always under `docker/`.
- Never modify `ENTRYPOINT` or `CMD` — all customization goes in `docker/rootfs/usr/local/bin/entrypoint.sh`.
- Never skip `exec "$@"` (or `exec <binary> ... "$@"`) at the end of entrypoint.sh — without it, signals never reach the app as PID 1 and graceful shutdown breaks.
- Never add `LABEL` blocks to the Dockerfile — all OCI metadata is applied at build time by CI via `--label`/`--annotation` (manifest-index annotations, since registries read multi-arch metadata from there, not per-platform layers).
- Never bake `MODE` or `DEBUG` into the image — the binary defaults to production; compose files set them explicitly.
- Never create `.env`, `.env.example`, or `.env.sample` files, and never require one for the stack to start — always use inline `${VAR:-default}` fallbacks so the stack works with zero `.env`; operators may still override via `.env` or shell env. Environment variables are always YAML map style (`KEY: value`), never list style (`- KEY=value`).
- Never include `build:` or `version:` keys in any docker-compose file.
- Never run `docker compose` from the project directory — always copy the compose file to a temp dir (`${TMPDIR:-/tmp}/{project_org}/{internal_name}-XXXXXX/`) and run from there; never mount volumes to `{project_root}/volumes/`.
- Never commit runtime `./volumes/` content from local runs — only `docker/rootfs/` (build-time overlay) is committed.
- Never push `:dev` or `:test` image tags to the production registry.
- Never use `docker/docker-compose.yml` (production) or `docker/docker-compose.dev.yml` for AI/automated use — dev and production compose are human-only; AI/automated testing uses `tests/run_tests.sh`/`tests/docker.sh` or, as fallback, `docker/docker-compose.test.yml`.
- Never leave the runtime process running as root after startup — the container starts as root only so the binary can bind internal port 80, and the binary itself must then drop to the `api` service user.

## CRITICAL - ALWAYS DO
- Always use a multi-stage Dockerfile: builder stage `casjaysdev/go:latest`, runtime stage `alpine:latest`.
- Always let the binary own user/group creation and the privilege drop — the Dockerfile carries NO `USER` directive and creates no user/group; the container starts as root so the binary can bind port 80, then drops to the `api` service user itself.
- Always use `tini` as init: `ENTRYPOINT [ "tini", "-p", "SIGTERM", "--", "/usr/local/bin/entrypoint.sh" ]`.
- Always set `STOPSIGNAL SIGRTMIN+3` (systemd-compatible graceful shutdown).
- Always expose internal port `80` and healthcheck via `{binary} --status` (start 10m, interval 5m, timeout 15s, retries 3 for the Dockerfile; compose files may use shorter intervals with a 90s start_period).
- Always mount exactly two volumes in compose: `./volumes/config:/config:z` and `./volumes/data:/data:z` (`:z` in production; dev/temp-dir runs may omit it).
- Always include the `x-logging: &default-logging` anchor (`max-size: '5m'`, `max-file: '1'`, `driver: json-file`) and reference it on every service.
- Always name resources consistently: top-level `name: api`, main service `api`, container `api-app`, cache service `api-cache`/`api-cache` container, network `api` (`external: false`).
- Always use the temp-dir workflow to run compose: create temp dir, copy compose file, create `volumes/config` and `volumes/data`, `cd` there, `docker compose up`.

## Key Rules Summary

### Directory layout
```
docker/
├── Dockerfile              # production
├── Dockerfile.dev          # :devel tag, debug mode
├── docker-compose.yml      # production — human use only
├── docker-compose.dev.yml  # development — human use only
├── docker-compose.test.yml # automated testing — AI use (fallback)
└── rootfs/usr/local/bin/entrypoint.sh   # build-time overlay, REQUIRED
```
Build context is project root; Dockerfile referenced as `-f docker/Dockerfile`.

### Container paths (binary's dirs)
`/config/api/` (server.yml, ssl/, tor/), `/data/api/` (uploads, cache, security/, tor/), `/data/db/sqlite/server.db`, `/data/db/valkey/`, `/data/log/api/`, `/data/backups/api/`. Host side: `./volumes/config` → `/config`, `./volumes/data` → `/data`. Database file is always named `server.db`.

### OCI labels/annotations
Required set includes `maintainer`, `org.opencontainers.image.{vendor,authors,title,base.name,description,licenses,created,version,schema-version,revision,url,source,documentation,vcs-type}`, `com.github.containers.toolbox=false`. Applied via `docker/metadata-action` + `docker/build-push-action` `annotations:` output in CI, not Dockerfile `LABEL`.

### Entrypoint contract
Minimal: set env defaults (`TZ`, `CONFIG_DIR`, `DATA_DIR`), optionally start auxiliary services, build CLI flags from env (`ADDRESS`, `PORT`, `DEBUG`), trap `SIGTERM SIGINT SIGQUIT` for graceful shutdown, then `exec` the binary. The binary — not the entrypoint — handles directory creation, permissions, user/group creation, and Tor management.

### Environment variables (entrypoint-level)
`TZ` (default `America/New_York`), `MODE` (`production`/`development`), `DEBUG` (`false` default; enables `/debug/*` regardless of MODE), `ADDRESS` (`0.0.0.0`), `PORT` (`80`). Boolean env vars accept any truthy/falsy form. Tor auto-enables if the `tor` binary is present — no explicit flag needed.

### Compose variants
| File | Tag | Cache | Env | Audience |
|---|---|---|---|---|
| `docker-compose.yml` | `:latest` | Valkey, persistent volume | no DEBUG/MODE | production, human |
| `docker-compose.dev.yml` | `:devel` | none (in-process) | `DEBUG: 1`, `MODE: dev` | dev, human |
| `docker-compose.test.yml` | `:latest` | Valkey, `tmpfs` ephemeral | `DEBUG: 1`, `MODE: dev` | AI/automated testing |

### Port mapping
Internal always `80` (override via `PORT`). External random `64xxx`. Production binds `172.17.0.1:{port}:80` (bridge only, reverse proxy handles external). Development binds `{port}:80` (all interfaces).

### Tor in container
Included and auto-enabled; binary owns Tor entirely (config in `/config/api/tor/torrc`, data/keys in `/data/api/tor/`, survives restarts via volume).

### Image tags
Release: `:latest`, `:{version}`, `:{YYMM}`, `:{commit-7char}` pushed to the platform registry. Local dev: `api:dev`, `api:test` (local Docker daemon only, never pushed). All release images built for `linux/amd64` and `linux/arm64`.

For complete details, see AI.md PART 26
