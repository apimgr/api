# Integrations

The API server exposes documented HTTP interfaces and standards-based discovery endpoints. Optional integrations are enabled only when the corresponding feature is configured; an unknown `/.well-known/` path returns `404` and never redirects.

## API clients

| Interface | Endpoint |
|-----------|----------|
| REST | `/api/{api_version}/...` |
| `/api/autodiscover` | Unversioned CLI/server discovery; overlay addresses are published only when their `expose` setting is enabled and the service is active |
| `/cli/binaries/{api-cli-os-arch}` | Operator-provisioned CLI release binary, served only when its `sha256.txt` checksum matches; optionally requires a Bearer token |
| OpenAPI JSON | `/api/{api_version}/server/swagger`; direct alias `/api/swagger` |
| Swagger UI | `/server/docs/swagger` |
| GraphQL | POST `/api/{api_version}/server/graphql`; direct alias `/api/graphql` |
| GraphiQL UI | `/server/docs/graphql` |
| MCP Streamable HTTP | `/api/{api_version}/server/mcp`; aliases `/api/mcp` and `/mcp` |

MCP uses the same Bearer tokens, validation, authorization, rate limits, and audit rules as the matching REST operations. Token-management operations are not MCP tools. See the [API reference](api.md) for request and response formats.

## Public discovery and security

| Endpoint | Purpose and availability |
|----------|--------------------------|
| `/robots.txt` | Configured crawler rules and sitemap reference |
| `/sitemap.xml` | Always-on sitemap containing indexable public pages allowed by `robots.txt` |
| `/feed.xml` | Always-on Atom 1.0 feed |
| `/rss.xml` | Always-on RSS 2.0 feed containing the same items as the Atom feed |
| `/manifest.json` | Web application manifest for installable browser clients |
| `/llms.txt` and `/.well-known/llms.txt` | Always-on application and API discovery for machine clients |
| `/.well-known/security.txt` | RFC 9116 contacts, expiry, and policy |
| `/.well-known/pgp-key.asc` | Security public key when generated and enabled |
| `/.well-known/acme-challenge/{token}` | HTTP-01 ACME challenge response while a challenge is active; answered on the existing HTTP listener |

The current project implements only the entries listed above; feature-gated entries are served only while their matching feature is active, and unknown or unsupported `/.well-known/*` paths return `404`. Absolute URLs in discovery responses are resolved for the current request and honor only trusted proxy headers. See [Security](security.md#other-security-endpoints) for the complete enabled-path summary.

## Identity and federation

The project has no user accounts, account profiles, OAuth/OIDC provider, or federation identity service. Anonymous owners are credential-only identities, and API access uses operator, owner, or linked Bearer tokens. There are no native app association or federation discovery endpoints enabled.

## Outbound webhooks

Contact destinations support the built-in `telegram`, `discord`, `slack`, `mattermost`, `pushover`, `gotify`, and `generic` adapters. Configure a role's URL under `server.contact.{role}.webhooks.{transport}`; fallback between roles and secret handling are described in [Configuration](configuration.md#contact-destinations-and-webhooks).

Every webhook request carries `X-Webhook-ID` for retry deduplication, `X-Webhook-Timestamp` as Unix seconds, the event name in `X-Webhook-Event`, and `X-Webhook-Signature` in `sha256=<hex_hmac>` form. The signing secret is stored in protected `secrets.yml`. The `generic` adapter sends JSON with `role`, `event`, `subject`, `body`, `severity`, `timestamp`, `project_name`, `project_version`, `app_url`, and optional `tracking_id`. Non-2xx responses are retried with exponential backoff at 1 minute, 5 minutes, 15 minutes, 1 hour, 6 hours, and 24 hours; after the final attempt, the delivery is dropped and logged.

## Overlay services

Tor hidden services and the optional I2P eepsite use separate private loopback backends. I2P is enabled by default in the application container and opt-in elsewhere. If neither a local `i2pd` binary nor an operator-provided SAM bridge is available, the server continues without an active I2P backend. Configuration and status details are in [Configuration](configuration.md). Public autodiscovery omits overlay addresses unless the matching `expose` setting is enabled.

## Data providers

Some utilities use external providers; provider calls and required configuration are documented with the affected feature and in the server configuration reference. When enabled, GeoIP downloads and checksum-verifies the fixed ip-location-db datasets on first run into `{data_dir}/security/geoip`, then refreshes them weekly. Existing data remains usable when a download fails; missing or older-than-21-day data appears as degraded in health checks. The service does not imply that every tool or provider is active on every installation.

## Security reporting

The repository vulnerability channel and deployed-instance reporting flow are described in [Security](security.md). The public security overview and RFC 9116 file link to the appropriate reporting channels.
