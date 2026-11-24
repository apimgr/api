# Security

This page describes security controls that are currently implemented.

## Request handling

The server applies configured security headers, rate limits and request-size limits. Forwarded host and protocol information is used only according to the trusted-proxy configuration. Browser Reporting API requests are accepted at `/api/{api_version}/server/reports/{name}`; bodies are size-capped, parsed as JSON, and reduced to an allowlisted set of fields before security logging. The endpoint returns no report data.

## Current public security endpoints

| Endpoint | Current behavior |
|----------|------------------|
| `/.well-known/security.txt` | Emits configured repository contact and security email contacts, expiry, policy URL, and preferred language. |
| `/.well-known/llms.txt` and `/llms.txt` | Generate a route-derived API discovery document. |
| `/server/security` | Displays the repository reporting link, security email, expiry and disclosure-policy link. |
| `/server/security/policy` | Displays the built-in coordinated-disclosure policy or the configured `web.security.policy` Markdown. |
| `/api/{api_version}/server/security` | Returns the security contacts, expiry and policy URL. |
| `/api/{api_version}/server/security/policy` | Returns the policy as Markdown. |

The GitHub private vulnerability reporting URL is the available private channel for repository vulnerabilities. `/server/contact` is a general contact form; it does not currently provide a validated security-report mode. Do not send vulnerability details through that general form.

## Features not yet implemented

The coordinated-disclosure workflow described by AI.md is not yet available. The server does not currently persist or encrypt instance vulnerability reports, issue researcher status tokens, send report acknowledgments or encrypted maintainer notifications, provide status or acknowledgments pages, or expose security-report maintenance commands. It also does not currently serve a project PGP key. The generated security page and `security.txt` therefore expose only the repository reporting URL and configured security email; they do not publish an instance report link.

Owner and linked API tokens can be issued and managed through the API token endpoints. The stateless MCP endpoint is available at `/api/{api_version}/server/mcp`, with `/api/mcp` and `/mcp` aliases. It requires an existing bearer token and reuses the API token validator. MCP tool calls are dispatched through the server router; token management routes are excluded from the tool list. See `/server/help/mcp` for client setup and current tool availability.

## Security contact configuration

`web.security.report_url` configures the repository reporting contact. The security email is resolved from `server.contact.security.email` and its configured fallback. `web.security.expires` controls the expiry emitted by `security.txt`; `web.security.policy` replaces the built-in disclosure policy with operator-supplied Markdown. Do not include secrets or report contents in these public settings.

## Health endpoints

`/server/healthz` and `/api/{api_version}/server/healthz` return health information. See [API reference](api.md) for the current response contract.

## Further reading

- [Configuration](configuration.md)
- [API reference](api.md)
- [Integrations and discovery](integrations.md)
- [CLI reference](cli.md)
