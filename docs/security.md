# Security

This page describes the security model defined for the API server. For exact defaults and field-level configuration, see [Configuration](configuration.md).

## Credentials and access

The REST API is public where an operation is public. Bearer credentials link resources to an owner or authorize operator-only routes; they are not interchangeable. Owner and linked API tokens are stored as SHA-256 hashes. The global operator token is configured separately as `server.token`, validated against a hash, and never returned by an API. Cookies do not authenticate API requests.

MCP uses the same Bearer token validation and authorization as REST. Token management is not exposed through MCP. Public health, security overview, and documentation endpoints disclose only their documented public fields. Operator-only routes require the operator token.

## Security headers and request handling

The server applies its configured Content-Security-Policy, Permissions-Policy, HSTS (when TLS is enabled), and other security headers. Reverse-proxy headers are trusted only from configured trusted peers; the original TCP peer is used for that trust decision. User-provided content is escaped or sanitized before rendering. Request paths and input values are normalized and validated before use.

Rate limits distinguish read, write, health, and global burst classes. Defaults and response behavior are configurable under `server.rate_limit`. Browser Reporting API endpoints accept only the documented report formats, cap request size, sanitize logged fields, and do not echo report bodies.

## Public security endpoints

| Endpoint | Purpose |
|----------|---------|
| `/.well-known/security.txt` and `/security.txt` | RFC 9116 contact, policy, and expiry information, generated per request |
| `/.well-known/llms.txt` and `/llms.txt` | Public machine-readable application and API discovery |
| `/.well-known/pgp-key.asc` | Project security public key when one has been generated and publishing is enabled |
| `/server/security` | Human-readable security overview and reporting channels |
| `/server/security/policy` | Coordinated disclosure policy |
| `/server/security/thanks` | Public researcher acknowledgments who opted into credit |
| `/server/security/report/{tracking_id}` | Researcher-only status view using the emailed, rate-limited status token |
| `/api/{api_version}/server/security` | JSON mirror of the security overview |
| `/api/{api_version}/server/security/policy` | JSON policy content |
| `/api/{api_version}/server/security/thanks` | JSON acknowledgment entries |

Unknown entries in the `/.well-known/` namespace return `404`. Optional standards endpoints exist only when the corresponding feature is enabled. See [Integrations](integrations.md) for discovery and protocol behavior.

## Reporting a vulnerability

The preferred repository-code reporting channel is the configured GitHub private vulnerability reporting URL. For a deployed instance, use the security-report contact link on `/server/security` or in `/.well-known/security.txt`. The `security_id` rotates every 48 hours; the current and immediately previous window remain valid so a report started near a rotation boundary can finish.

The contact form enters security mode only when the supplied `security_id` is valid. Security reports use a two-step review and confirmation flow, server-side validation, rate limits, and CAPTCHA. The server stores report content encrypted at rest. It sends a PGP-encrypted maintainer notification when a project key exists; otherwise it uses an AES-encrypted attachment and warns the maintainer. The server issues a random `sec_` tracking ID. Logs contain the tracking ID and sanitized metadata, never report content or researcher contact details.

When SMTP is available, researchers receive an acknowledgment containing only the tracking ID and private status URL, never report content. The status token is limited to one use per UTC day and expires 30 days after closure. The status page shows triage status, maintainer comments, and an expected disclosure date; it does not expose report content. Maintainers manage records through the CLI maintenance security commands. There is no web administration interface for reports.

The built-in disclosure policy defines scope, excluded testing, coordinated disclosure, and safe harbor. An operator may replace the policy body using `web.security.policy`. Do not publish vulnerability details before coordinated disclosure.

## Security report configuration

`web.security.report_url` is the primary repository reporting channel. `web.security.contact` is the secondary email contact; `web.security.expires` controls the RFC 9116 expiry; `web.security.policy` customizes the disclosure policy. The local contact form appears in the published contact list only when enabled. Generated installation secrets and the AES-256-GCM encryption key are held in the protected secrets configuration and are never printed or returned by an API.

## Health endpoints

`/server/healthz` and `/api/{api_version}/server/healthz` return public-safe health information. `/api/healthz` is a direct alias when enabled by configuration. Health output does not reveal credentials, database structure, internal addresses, or detailed operator-only diagnostics.

## Further reading

- [Configuration](configuration.md)
- [API reference](api.md)
- [Integrations and discovery](integrations.md)
- [CLI reference](cli.md)
