# Installation

API runs as a self-hosted server. Its companion command-line client, `api-cli`, is distributed separately in the same release.

## Docker Compose

The repository includes the production Compose stack. From a clone, start it with:

```bash
docker compose -f docker/docker-compose.yml up -d
```

The Compose file runs the API server with Valkey, maps host address `172.17.0.1:64580` to the container's HTTP port `80`, and persists configuration and data under `docker/volumes/`. Open `http://172.17.0.1:64580/` from the host.

To stop the stack:

```bash
docker compose -f docker/docker-compose.yml down
```

Keep `docker/volumes/config` and `docker/volumes/data` when upgrading so the server configuration and application data remain available.

## Release binaries

Download the matching server and client binaries from [GitHub Releases](https://github.com/apimgr/api/releases). The server targets Linux, macOS, Windows, and FreeBSD on AMD64 and ARM64. The release assets are named `api-{os}-{arch}` and `api-cli-{os}-{arch}`; Windows assets end in `.exe`.

On Linux or macOS, make the server executable and start it:

```bash
chmod +x api-linux-amd64
./api-linux-amd64 --help
./api-linux-amd64
```

The server chooses its platform-specific configuration, data, cache, and log directories. Its default listen port is selected at runtime. The startup banner reports the active address. Set the listen port in `server.yml` or with the `--port` flag when a fixed port is needed.

The companion client can be run from its downloaded binary:

```bash
chmod +x api-cli-linux-amd64
./api-cli-linux-amd64 --help
```

## Build from source

The project builds inside the Docker-based Go toolchain and produces server and client binaries for all eight supported OS/architecture combinations:

```bash
git clone https://github.com/apimgr/api.git
cd api
make build
```

The resulting binaries are written to the git-ignored `binaries/` directory.

## Service management

The server provides native service management through its `--service` command. Use `api --service=--help` to see commands supported by the current platform. Installation and removal may create or delete system files, so run those commands as the system operator.

## Verification

Check the server process and version with:

```bash
api --status
api --version
```

For a running server, request its health page at `/server/healthz` or its configured versioned API health route at `/api/{api_version}/server/healthz`. The unversioned `/api/healthz` alias is also available.

## Configuration

The server configuration file is `server.yml` in the platform-specific configuration directory. The CLI client has a separate `cli.yml` file. See [Configuration](configuration.md) and [CLI Reference](cli.md).
