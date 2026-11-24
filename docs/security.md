# Security

This page describes the security controls and reporting routes currently available in API.

## Request handling

The server applies configured security headers, request-size limits, rate limits, origin checks for browser form posts, and trusted-proxy rules. Browser Reporting API requests are accepted at `/api/{api_version}/server/reports/{name}`. Bodies are size-capped, parsed as JSON, and reduced to an allowlist before security logging; report bodies are not returned.

## Coordinated disclosure

Repository vulnerabilities should be reported through the configured private repository reporting URL. Vulnerabilities in a deployed instance can be submitted from `/server/security`, which links to `/server/contact?security_id=…`. The rotating security ID is required to reveal the security-report form. The form uses a review and confirm step, CAPTCHA, request limits, and encrypted storage. Report content is not written to plaintext storage or included in ordinary logs.

| Endpoint | Behavior |
|----------|----------|
| `/.well-known/security.txt` | Publishes configured repository reporting, instance report, and security email contacts in preference order, plus expiry and policy information. |
| `/.well-known/pgp-key.asc` | Serves the project's ASCII-armored public key only when `web.security.publish_pgp_key` is enabled and a valid public key file exists; otherwise returns `404`. |
| `/server/security` | Human-readable security contacts, reporting instructions, and links to the policy, acknowledgments, public key when configured, and `security.txt`. |
| `/server/security/policy` | Displays the built-in disclosure policy or `web.security.policy` Markdown. |
| `/server/security/thanks` | Lists disclosed researchers who opted into credit. |
| `/server/security/report/{tracking_id}` | Shows report status and researcher-visible comments using the token in the emailed link. |
| `/api/{api_version}/server/security` | JSON mirror of the security overview. |
| `/api/{api_version}/server/security/policy` | Returns the disclosure policy as Markdown. |
| `/api/{api_version}/server/security/thanks` | Returns the hall-of-fame entries. |
| `/api/{api_version}/server/contact` | GET returns contact form fields and a CAPTCHA challenge; POST provides the review and confirm submission flow. A valid `security_id` selects the security-report fields. |

Security report bodies are encrypted with the project's PGP public key when one is provisioned; otherwise they use AES-256-GCM with the configured server encryption key. Researcher contact details are encrypted separately with AES-256-GCM. When SMTP is enabled, the maintainer notification contains the encrypted report body (PGP encrypted when the project key exists, or as an AES-encrypted attachment otherwise). The researcher acknowledgment contains only a tracking ID and status URL. Without SMTP, reports remain stored and the tracking ID is returned in the form response.

Rotate the AES key with `api --maintenance secret rotate encryption_key`. The server wraps the previous key under the new key in `app_secrets`, then re-encrypts owner-token copies, AES-encrypted report bodies, researcher contacts, and DNS provider credentials. The 30-day grace key stays wrapped in the database; startup resumes an interrupted data migration, and scheduled cleanup removes the wrapped key after expiry. PGP-encrypted report bodies are unchanged by AES-key rotation.

The report lifecycle is `Received`, `Triaged`, `Confirmed`, `Patching`, then `Disclosed` or `Won't Fix`. Maintainers can attach researcher-visible comments and an expected disclosure date. The status URL contains a one-use-per-UTC-day token; it expires 30 days after the report closes. The status page exposes triage state and approved comments, never the report body. The form issues a `sec_` tracking ID; ordinary logs and audit entries contain report metadata only, not submitted report content.

The public key route returns `404` until a valid key is provisioned and publishing is enabled. Operators manage the disclosure keypair with `api --maintenance pgp`: `generate`, `rotate`, `publish`, `export public`, `export private`, `import`, `delete`, and `metadata`. The key uses Ed25519 signing and Curve25519 encryption; the private key is encrypted using a key derived from the installation secret. Rotation re-encrypts retained PGP-protected report bodies transactionally before activating the new key, keeps the retired private key encrypted for 30 days, and publishes a transition signed by the old key. Keyserver retries persist in `security/keyservers.state` and run through the built-in `keyserver_retry` task. Private-key export requires the sensitive-operation confirmation flow and writes mode `0600` output. Deleting the key makes reports encrypted to it unreadable.

Maintainers can inspect report metadata, export encrypted report bytes, and update status from the CLI:

```text
api --maintenance security list [--status STATUS]
api --maintenance security show TRACKING_ID
api --maintenance security export TRACKING_ID [--output FILE]
api --maintenance security update TRACKING_ID [--status STATUS] [--comment TEXT] [--disclosure-date YYYY-MM-DD] [--severity LEVEL]
```

The `show` command never prints report body content. `export` writes only encrypted bytes, using a mode `0600` file by default. Closed reports are removed by the non-skippable daily `security_cleanup` task after `server.security.reports.retention_days` (default 365 days).

## Owner tokens

The API has no user accounts or passwords. An owner is an anonymous identity identified by its primary `own_` token. The token page at `/server/preferences/tokens` lists masked values, supports restoring an identity by pasting its primary token, and can reveal or rotate tokens on explicit request. Primary-token rotation preserves the owner's resources and linked tokens. Revoking the primary token deletes the owner, all linked tokens, and all resources belonging to that owner.

Linked tokens are independently revocable credentials for automation and MCP clients. They have a unique name per owner and an optional expiry (`never`, `7d`, `30d`, `90d`, `1y`, or a UTC date). A linked token can access its owner's resources but cannot reveal, rotate, create, update, or revoke tokens. API token management requires a primary Bearer token; API routes ignore cookies. The token value is stored as a SHA-256 lookup hash plus an AES-256-GCM encrypted copy so it can be explicitly revealed. Raw values are never included in list responses, logs, or backups in plaintext. See [API Reference](api.md#owner-and-linked-token-management) for the route table.

## Backup archive and restore limits

Backups are `.tar.gz` archives, or `.tar.gz.enc` when encryption is configured. Each archive includes `manifest.json` with snapshot metadata and SHA-256 checksums. Full backups contain the selected configuration and data files; daily and hourly incrementals contain changed files, their base-backup identity/checksum, and deleted paths. Restoring an incremental requires its full base from the same backup directory and applies both to reach the recorded snapshot.

With the libSQL/Turso driver, backups never include the remote database. When the local SQLite cache is enabled and present, its snapshot is archived as `server.db`; restore seeds the configured remote database from that snapshot only when the remote database is empty. A populated remote database is left unchanged.

Restore is an operator CLI operation (`api --maintenance restore FILE`), not a public page or API endpoint. Encrypted backups prompt for the backup password; it is separate from `server.token`. Restore validates the archive, checksum, manifest, database integrity, and incremental base before applying changes. Configured `server.restore` limits cap the compressed archive at 1 GiB, total expanded contents at 4 GiB, file count at 10,000, individual file size at 512 MiB, and expansion ratio at 100:1 by default. Limits apply to the combined base and incremental archive. See [Configuration](configuration.md) for the limit keys.

## Other security endpoints

The server currently exposes these public discovery paths: `/.well-known/security.txt` and `/.well-known/llms.txt` are always available; `/.well-known/pgp-key.asc` is available only when a valid public key is provisioned and publishing is enabled; and `/.well-known/acme-challenge/{token}` is available only while an HTTP-01 ACME challenge is active. `/llms.txt` is an additional alias, and `/robots.txt` serves the configured crawler rules. Other `/.well-known/*` paths, including feature-gated standards not implemented by this project, return `404`.

| Endpoint | Behavior |
|----------|----------|
| `/.well-known/llms.txt` and `/llms.txt` | Generate route-derived API discovery documents. |
| `/api/{api_version}/server/reports/{name}` | Accepts sanitized browser Reporting API reports. |
| `/api/{api_version}/server/mcp`, `/api/mcp`, `/mcp` | Streamable HTTP MCP endpoint using existing Bearer tokens and API authorization. Token-management routes are excluded from MCP tools. |
| `/server/healthz` and `/api/{api_version}/server/healthz` | Return the server health response. |

`web.security.report_url` configures the repository reporting contact. `server.contact.security.email` configures the security email contact. `web.security.expires` controls the `security.txt` expiry, `web.security.policy` replaces the built-in disclosure policy with operator-supplied Markdown, and `web.security.publish_pgp_key` controls public key discovery. Do not put secrets or report contents in public settings.

## Further reading

- [Configuration](configuration.md)
- [API reference](api.md)
- [Integrations and discovery](integrations.md)
- [CLI reference](cli.md)
