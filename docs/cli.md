# CLI reference

The project provides two binaries: `api` runs the server; `api-cli` connects to a configured server. This page describes the server binary. Use each binary's `--help` output for the options available in the built release.

## Server options

| Option | Purpose |
|--------|---------|
| `--help` | Show server usage |
| `--version` | Print version and build information |
| `--status` | Show local service status |
| `--config DIR` | Select the configuration directory |
| `--data DIR` | Select the data directory |
| `--cache DIR` | Select the cache directory |
| `--log DIR` | Select the log directory |
| `--backup DIR` | Select the backup directory |
| `--address ADDRESS` | Set the listen address |
| `--port PORT` | Set the listen port |
| `--baseurl PATH` | Set the public URL path prefix |
| `--mode MODE` | Select the configured application mode |
| `--debug` | Enable debug behavior |
| `--daemon` | Run as a background daemon where supported |
| `--pid FILE` | Set the PID file path |
| `--i2p` | Enable the I2P eepsite |
| `--color auto\|yes\|no` | Set colored output behavior |
| `--lang CODE` | Set the output language |
| `--shell completions\|init [SHELL]` | Print shell integration output |
| `--service ACTION` | Manage the native service |
| `--maintenance ACTION` | Run an operator maintenance command |
| `--update ACTION` | Check or apply an update |

Paths and values are validated by the server. Defaults are resolved from the platform and runtime environment; do not assume fixed root-owned paths for an unprivileged installation.

## Service management

The `--service` option takes a single action. Installation is written with two dashes:

```bash
sudo api --service --install
api --service --help
```

Supported actions include `start`, `restart`, `stop`, `reload`, `--install`, `--uninstall`, `--disable`, and `--help`. Available service managers depend on the operating system. Installation and removal affect the host service configuration and require the appropriate operator privileges.

## Maintenance and updates

Maintenance operations include setup, backup and restore, secret rotation, security-report management, and other operator workflows. The exact subcommands and arguments are shown by:

```bash
api --maintenance --help
api --update --help
```

Sensitive operations require operator authorization and are recorded in the audit log. Passwords for configuration and backup data are not accepted as command-line arguments; interactive flows prompt securely where required.

## Client

`api-cli` is the companion API client. Its options, commands, output formats, and configuration are documented by its own help output:

```bash
api-cli --help
api-cli [command] --help
```

The client can select the server URL and API version through its configuration or flags. Use separate linked tokens for independent MCP clients; never use an operator token as a general-purpose client credential.
