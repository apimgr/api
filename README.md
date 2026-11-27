# API — Universal API Toolkit

[![CI](https://github.com/apimgr/api/actions/workflows/ci.yml/badge.svg)](https://github.com/apimgr/api/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/apimgr/api)](https://github.com/apimgr/api/releases)
[![License](https://img.shields.io/github/license/apimgr/api)](LICENSE.md)
[![Documentation](https://readthedocs.org/projects/apimgr-api/badge/?version=latest)](https://apimgr-api.readthedocs.io)

## About

API is a self-hosted utility service with a server-rendered web interface, a versioned REST API, GraphQL, and a companion command-line client.

## Features

- Text, cryptography, conversion, date and time, generators, validators, math, and developer utilities
- Parsing, network, geolocation, weather, image, language, research, Docker, testing, and entertainment tools
- Server-rendered tool pages that support form submissions without JavaScript
- Machine-readable REST, OpenAPI, and GraphQL interfaces
- Eight interface locales: English, Spanish, French, German, Italian, Arabic, Chinese, and Japanese
- Anonymous credential-based ownership for resources that need an owner; no user accounts or login
- Browser owner-token management at `/server/preferences/tokens`, with a returning-owner preferences menu, an HttpOnly project-specific cookie, and JavaScript-free forms
- Guest cookie consent and privacy controls, a configured data-protection contact page, and dismissible operator announcements
- Configuration-driven health, security, localization, scheduling, backup, update, Tor support, and optional I2P eepsites
- Backup restoration enforces configurable archive, expanded-size, file-count, per-file, compression-ratio, path, and permission limits
- Explicitly enabled, operator-token-protected runtime diagnostics and profiling
- The `api-cli` client for command-line and terminal use

## Production

### Docker Compose

Copy the production Compose file into a deployment directory so its relative `./volumes/` mounts keep runtime data outside the source checkout:

```bash
mkdir -p "$HOME/api-deployment"
cp docker/docker-compose.yml "$HOME/api-deployment/compose.yml"
cd "$HOME/api-deployment"
docker compose -f compose.yml up -d
```

The stack publishes HTTP on `172.17.0.1:64580`, persists configuration and data under `$HOME/api-deployment/volumes/`, and starts Valkey for caching. Open `http://172.17.0.1:64580/` from the host. See [Installation](https://apimgr-api.readthedocs.io/en/latest/installation/) for upgrades and other deployment options.

### Binary

Download the server binary for your operating system and architecture from [GitHub Releases](https://github.com/apimgr/api/releases). The release also includes `api-cli` binaries.

```bash
curl -q -LSsf -O https://github.com/apimgr/api/releases/latest/download/api-linux-amd64
chmod +x api-linux-amd64
./api-linux-amd64 --help
```

The server chooses its configuration, data, and log directories according to the operating system. It reports its listening address at startup. Use `--config`, `--data`, `--log`, and the other directory flags to select explicit locations.

### System Service

On a supported Unix-like host, install the native service from the downloaded binary:

```bash
sudo ./api-linux-amd64 --service --install
```

The command installs, enables, and starts the service using the host's supported service manager. See the [installation guide](https://apimgr-api.readthedocs.io/en/latest/installation/) for platform-specific details and service lifecycle commands.

## Client

Install the `api-cli` binary for your platform from [GitHub Releases](https://github.com/apimgr/api/releases). Configure the target server with `--server` or `server.primary` in the CLI configuration file.

```bash
api-cli --server http://172.17.0.1:64580 raw get /api/v1/text/uuid
api-cli raw request PATCH /api/v1/server/preferences/tokens/TOKEN_ID name=automation
api-cli --help
```

`raw request` supports GET, POST, PUT, PATCH, and DELETE so API operations without a dedicated CLI command remain available. The CLI configuration supports an API version setting; its default is `v1`. The client also provides command groups, output formatting, shell completions, and an interactive terminal interface.

## Configuration

The server reads `server.yml` from its platform-specific configuration directory. Use `api --help` to see supported flags and `api --config DIR` to select a configuration directory. SQLite is the default database; remote libSQL/Turso can use an optional local SQLite cache for local-first reads, remote writes with cache refresh, and read-only operation from a fresh cache while offline. Other server settings include the listen address and port, API version, security controls, logging, integrations, and scheduled services.

The client has a separate `cli.yml` file. See the [server configuration reference](https://apimgr-api.readthedocs.io/en/latest/configuration/) and [CLI reference](https://apimgr-api.readthedocs.io/en/latest/cli/).

## API

The REST API base path is `/api/{api_version}/`; the default version is `v1`. Utility operations are public. Credentials are used for owner-linked and operator-protected features according to the server's credential model.

```bash
curl -q -LSsf http://172.17.0.1:64580/api/v1/text/uuid
curl -q -LSsf 'http://172.17.0.1:64580/api/v1/datetime/now?timezone=UTC'
```

Interactive API references are available at `/server/docs/swagger` and `/server/docs/graphql`. The corresponding JSON/OpenAPI and read-only GraphQL endpoints are also available under the versioned API path, with the documented unversioned aliases; GraphQL accepts GET and POST queries.

## Other

- [Documentation](https://apimgr-api.readthedocs.io)
- [Installation](https://apimgr-api.readthedocs.io/en/latest/installation/)
- [API reference](https://apimgr-api.readthedocs.io/en/latest/api/)
- [Security model](https://apimgr-api.readthedocs.io/en/latest/security/)
- [Integrations](https://apimgr-api.readthedocs.io/en/latest/integrations/)
- [Translation guide](https://apimgr-api.readthedocs.io/en/latest/translating/)
- [Source code and issue tracker](https://github.com/apimgr/api)

## Development

Build and test inside the project’s Docker-based Go toolchain:

```bash
make dev
make local
make build
make test
```

`make dev` builds into a temporary directory; `make local` creates production-style local binaries; `make build` produces server and client binaries for Linux, macOS, Windows, and FreeBSD on AMD64 and ARM64. All Go builds and tests run in the project Docker toolchain. `make release` publishes a manually prepared stable `X.Y.Z` release; `make beta` tags a clean `main` commit for CI to publish; devel releases come from CI. See the [development guide](https://apimgr-api.readthedocs.io/en/latest/development/) before changing code or documentation.

`make test` runs the Go test suite. Run `tests/run_tests.sh` for Docker-based binary, HTTP, owner-token, Tor, and I2P integration checks. `tests/incus.sh` and `tests/e2e.sh` are separate, explicit test tiers. The production image runtime-command and Tor health contract is checked after `make docker` and in CI.

To scan Go dependencies for incompatible licenses and refresh the attribution files, run the repository script in the Go toolchain container:

```bash
docker run --rm --no-healthcheck \
  --name "api-$(tr -dc 'a-z0-9' </dev/urandom | head -c8)" \
  -v "$PWD":/app -w /app \
  -e CGO_ENABLED=0 -e GOFLAGS=-buildvcs=false \
  casjaysdev/go:latest bash scripts/verify-licenses.sh
```

The script checks the shipped Go packages and writes its temporary license report under a project-scoped OS temp directory. It removes that directory when it exits; update `LICENSE.md` from reviewed dependency license changes instead of committing generated reports.

## Disclaimer

This software is provided “as is” without warranty of any kind.

- **No warranty:** The maintainers are not responsible for damages, data loss, or other issues arising from use of this software.
- **Not professional advice:** The software does not provide legal, financial, medical, or other professional advice.
- **Third-party services:** External service providers have separate terms that apply to their use.
- **Security:** The project follows security practices, but no software can be guaranteed free of vulnerabilities.
- **Production use:** Review the configuration and evaluate the service for your environment before deployment.

By using this software, you acknowledge that you have read and understood this disclaimer.

## License

MIT. See [LICENSE.md](LICENSE.md).
