# Installation

API runs as a self-hosted server. Its companion command-line client, `api-cli`, is distributed separately in the same release.

## Docker Compose

The repository includes a production Compose stack. Run it from a deployment directory so its relative `./volumes/` mounts keep runtime data outside the source checkout:

```bash
mkdir -p "$HOME/api-deployment"
cp docker/docker-compose.yml "$HOME/api-deployment/compose.yml"
cd "$HOME/api-deployment"
docker compose -f compose.yml up -d
```

The Compose file runs the API server with Valkey, maps host address `172.17.0.1:64580` to the container's HTTP port `80`, and persists configuration and data under `$HOME/api-deployment/volumes/`. Set `HOST_PORT` to use another host port in the 64xxx range. Open `http://172.17.0.1:64580/` from the host.

To stop the stack:

```bash
docker compose -f compose.yml down
```

Keep `$HOME/api-deployment/volumes/config` and `$HOME/api-deployment/volumes/data` when upgrading so the server configuration and application data remain available.

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

The server provides native service management through its `--service` command. Use `api --service=--help` to see commands supported by the current platform. A non-root install creates, enables, and starts a per-user service on Linux or macOS; system-service installation and other system-service controls require root or Administrator access. Uninstall asks for confirmation before deleting application data and removes the service account/group only when this application created them. On macOS, a legacy `_api` account remains untouched while runtime directories are transferred to `api`; remove the legacy account manually when it is no longer needed.

## Verification

Check the server process and version with:

```bash
api --status
api --version
```

For a running server, request its health page at `/server/healthz` or its configured versioned API health route at `/api/{api_version}/server/healthz`. The unversioned `/api/healthz` alias is also available.

## Configuration

The server configuration file is `server.yml` in the platform-specific configuration directory. The CLI client has a separate `cli.yml` file. See [Configuration](configuration.md) and [CLI Reference](cli.md).
