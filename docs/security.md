# Security

This page describes the security controls and reporting routes currently available in API.

## Request handling

The server applies configured security headers, request-size limits, rate limits, origin checks for browser form posts, and trusted-proxy rules. Browser Reporting API requests are accepted at `/api/{api_version}/server/reports/{name}`. Bodies are size-capped, parsed as JSON, and reduced to an allowlist before security logging; report bodies are not returned.

## Coordinated disclosure

Repository vulnerabilities should be reported through the configured private repository reporting URL. Vulnerabilities in a deployed instance can be submitted from `/server/security`, which links to `/server/contact?security_id=…`. The rotating security ID is required to reveal the security-report form. The form uses a review and confirm step, CAPTCHA, request limits, and encrypted storage. Report content is not written to plaintext storage or included in ordinary logs.

| Endpoint | Behavior |
|----------|----------|
| `/.well-known/security.txt` | Publishes configured repository reporting, instance report, and security email contacts in preference order, plus expiry and policy information. |
| `/.well-known/pgp-key.asc` | Serves the configured project's ASCII-armored public key; returns `404` until a valid public key file exists. |
| `/server/security` | Human-readable security contacts, reporting instructions, and links to the policy, acknowledgments, public key when configured, and `security.txt`. |
| `/server/security/policy` | Displays the built-in disclosure policy or `web.security.policy` Markdown. |
| `/server/security/thanks` | Lists disclosed researchers who opted into credit. |
| `/server/security/report/{tracking_id}` | Shows report status and researcher-visible comments using the token in the emailed link. |
| `/api/{api_version}/server/security` | JSON mirror of the security overview. |
| `/api/{api_version}/server/security/policy` | Returns the disclosure policy as Markdown. |
| `/api/{api_version}/server/security/thanks` | Returns the hall-of-fame entries. |
| `/api/{api_version}/server/contact` | GET returns contact form fields and a CAPTCHA challenge; POST provides the review and confirm submission flow. A valid `security_id` selects the security-report fields. |

Security reports are encrypted with the configured AES-256-GCM key. When SMTP is enabled, the maintainer notification contains encrypted report data, and the researcher acknowledgment contains only a tracking ID and status URL. Without SMTP, reports remain stored and the tracking ID is returned in the form response. The project PGP key workflow is not available yet; `/.well-known/pgp-key.asc` is therefore absent until a key is provisioned.

Maintainers can inspect report metadata and update status from the CLI:

```text
api --maintenance security list [--status STATUS]
api --maintenance security show TRACKING_ID
api --maintenance security update TRACKING_ID [--status STATUS] [--comment TEXT] [--disclosure-date YYYY-MM-DD] [--severity LEVEL]
```

The command never prints report body content. Closed reports are removed by the non-skippable daily `security_cleanup` task after `server.security.reports.retention_days` (default 365 days).

## Other security endpoints

| Endpoint | Behavior |
|----------|----------|
| `/.well-known/llms.txt` and `/llms.txt` | Generate route-derived API discovery documents. |
| `/api/{api_version}/server/reports/{name}` | Accepts sanitized browser Reporting API reports. |
| `/api/{api_version}/server/mcp`, `/api/mcp`, `/mcp` | Streamable HTTP MCP endpoint using existing Bearer tokens and API authorization. Token-management routes are excluded from MCP tools. |
| `/server/healthz` and `/api/{api_version}/server/healthz` | Return the server health response. |

`web.security.report_url` configures the repository reporting contact. `server.contact.security.email` configures the security email contact. `web.security.expires` controls the `security.txt` expiry, and `web.security.policy` replaces the built-in disclosure policy with operator-supplied Markdown. Do not put secrets or report contents in public settings.

## Further reading

- [Configuration](configuration.md)
- [API reference](api.md)
- [Integrations and discovery](integrations.md)
- [CLI reference](cli.md)
