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
- Seven interface locales: English, Arabic, German, Spanish, French, Japanese, and Chinese
- Anonymous credential-based ownership for resources that need an owner; no user accounts or login
- Configuration-driven health, security, localization, scheduling, backup, update, and Tor support
- The `api-cli` client for command-line and terminal use

## Production

### Docker Compose

From a clone of this repository, start the production Compose stack:

```bash
docker compose -f docker/docker-compose.yml up -d
```

The stack publishes HTTP on `172.17.0.1:64580`, persists configuration and data under `docker/volumes/`, and starts Valkey for caching. Open `http://172.17.0.1:64580/` from the host.

### Binary

Download the server binary for your operating system and architecture from [GitHub Releases](https://github.com/apimgr/api/releases). The release also includes `api-cli` binaries.

```bash
./api-linux-amd64 --help
./api-linux-amd64
```

The server chooses its configuration, data, and log directories according to the operating system. It reports its listening address at startup. Use `--config`, `--data`, `--log`, and the other directory flags to select explicit locations.

### System Service

The server includes native service installation and management for supported operating systems. See the [installation guide](https://apimgr-api.readthedocs.io/en/latest/installation/) for platform-specific steps and service commands.

## Client

Install the `api-cli` binary for your platform from [GitHub Releases](https://github.com/apimgr/api/releases). Configure the target server with `--server` or `server.primary` in the CLI configuration file.

```bash
api-cli --server http://172.17.0.1:64580 raw get /api/v1/text/uuid
api-cli --help
```

The CLI configuration supports an API version setting; its default is `v1`. The client also provides command groups, output formatting, shell completions, and an interactive terminal interface.

## Configuration

The server reads `server.yml` from its platform-specific configuration directory. Use `api --help` to see supported flags and `api --config DIR` to select a configuration directory. Server settings include the listen address and port, API version, database and cache, security controls, logging, integrations, and scheduled services.

The client has a separate `cli.yml` file. See the [server configuration reference](https://apimgr-api.readthedocs.io/en/latest/configuration/) and [CLI reference](https://apimgr-api.readthedocs.io/en/latest/cli/).

## API

The REST API base path is `/api/{api_version}/`; the default version is `v1`. Utility operations are public. Credentials are used for owner-linked and operator-protected features according to the server's credential model.

```bash
curl -q -LSsf http://172.17.0.1:64580/api/v1/text/uuid
curl -q -LSsf 'http://172.17.0.1:64580/api/v1/datetime/now?timezone=UTC'
```

Interactive API references are available at `/server/docs/swagger` and `/server/docs/graphql`. The corresponding JSON/OpenAPI and GraphQL endpoints are also available under the versioned API path, with the documented unversioned aliases.

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
make build
make test
```

`make build` produces server and client binaries for Linux, macOS, Windows, and FreeBSD on AMD64 and ARM64. See the [development guide](https://apimgr-api.readthedocs.io/en/latest/development/) before changing code or documentation.

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
