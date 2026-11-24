# CLI reference

The project provides two binaries: `api` runs the server, and `api-cli` is the companion client. The client opens its interactive TUI when run interactively without a command; a category and command run a single request and print the selected output format. Use `--help` for the complete command list in the installed build.

## Client options

| Option | Purpose |
|--------|---------|
| `-h`, `--help` | Show usage, global flags, and registered commands |
| `-v`, `--version` | Print version, commit, and build date |
| `--server URL` | Select the server for this invocation; a valid URL is saved only when the configured server is empty or invalid |
| `--token TOKEN` | Supply an API token |
| `--token-file FILE` | Read the API token from a file |
| `--config NAME` | Select a CLI configuration profile |
| `--debug` | Enable request debugging |
| `--color auto\|yes\|no` | Select color behavior |
| `--lang CODE` | Select the output language |
| `--output json\|table\|plain` | Select output format; the flag overrides `output.format` in `cli.yml` |
| `--shell completions [SHELL]` | Print shell completion code |
| `--shell init [SHELL]` | Print the shell initialization line |
| `--shell help` | Show shell integration help |

Supported shell names are `bash`, `zsh`, `fish`, `sh`, `dash`, `ksh`, `powershell`, and `pwsh`. If omitted, the shell is detected from `$SHELL`.

Client examples:

```bash
api-cli --server https://api.example.com system health
api-cli --server https://api.example.com --output json text uuid
api-cli --token-file "$HOME/.config/apimgr/api/token" network ip
api-cli --shell completions bash
```

The token lookup order is `--token-file`, `--token`, `API_TOKEN`, `auth.token` in the CLI profile, then the profile's `auth.token_file`. Keep token files private. The client stores its configuration under the invoking user's config directory; it does not use the server's `server.yml`.

Command groups currently include `crypto`, `datetime`, `network`, `raw`, `system`, and `text`. Run `api-cli GROUP` to list that group's commands; global `--help` lists every registered command.

For scripting, the client's process exit codes distinguish general failures (`1`), configuration (`2`), connection (`3`), authentication (`4`), missing resources (`5`), and usage errors (`64`).

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
| `--color auto\|yes\|no` | Set colored output behavior |
| `--lang CODE` | Set the output language |
| `--shell completions\|init [SHELL]` | Print shell integration output |
| `--service ACTION` | Manage the native service |
| `--maintenance ACTION` | Run an operator maintenance command |
| `--update ACTION` | Check or apply an update |
| `--tor ACTION` | Inspect or manage the Tor hidden service |
| `--i2p` | Enable the I2P eepsite for this run |

The server publishes operator-provisioned CLI releases from `{data_dir}/cli/` by default. `/api/autodiscover` advertises only artifacts whose `sha256.txt` hash matches the file; `/cli/binaries/{api-cli-os-arch}` serves those binaries. Set `cli.binary_download.require_auth` to require a valid Bearer token.

The local `--status` output includes the I2P provider state and address when available. `--i2p` overrides the configured I2P enabled value for that server run.

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

Security report maintenance supports metadata listing, review, and status updates:

```text
api --maintenance security list [--status STATUS]
api --maintenance security show TRACKING_ID
api --maintenance security export TRACKING_ID [--output FILE]
api --maintenance security update TRACKING_ID [--status STATUS] [--comment TEXT] [--disclosure-date YYYY-MM-DD] [--severity LEVEL]
```

The `show` command omits report bodies. `export` writes the encrypted report bytes with mode `0600` (or writes to standard output with `--output -`); it never decrypts report content. Closed reports are purged by the daily security cleanup task after the configured retention period.

## Client

`api-cli` is the companion API client. Its options, commands, output formats, and configuration are documented by its own help output:

```bash
api-cli --help
api-cli [command] --help
```

The client can select the server URL and API version through its configuration or flags. Use separate linked tokens for independent MCP clients; never use an operator token as a general-purpose client credential.
