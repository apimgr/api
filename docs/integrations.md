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
| `/llms.txt` and `/.well-known/llms.txt` | Always-on application and API discovery for machine clients |
| `/.well-known/security.txt` | RFC 9116 contacts, expiry, and policy |
| `/.well-known/pgp-key.asc` | Security public key when generated and enabled |
| `/.well-known/webfinger` | Optional; enabled only when the instance publishes account identities or federation |
| `/.well-known/openid-configuration` | Optional; enabled only when the server operates as an OIDC provider |
| `/.well-known/oauth-protected-resource` | Optional; enabled only when OAuth is enabled for MCP or the API |
| `/.well-known/oauth-authorization-server` | Optional; enabled only when this server is an OAuth authorization server |
| `/.well-known/assetlinks.json` | Optional Android app association |
| `/.well-known/apple-app-site-association` | Optional Apple app association |
| `/.well-known/mta-sts.txt` | Optional; enabled only when this host owns inbound mail policy |
| `/.well-known/gpc.json` | Optional; enabled only when Global Privacy Control is honored |
| `/.well-known/nodeinfo` | Optional; enabled with WebFinger for federation |
| `/.well-known/openpgpkey/` | Optional Web Key Directory publication |

The server allowlists well-known entries. Feature-gated endpoints are not served merely because a client requests them. Absolute URLs in discovery responses are resolved for the current request and honor only trusted proxy headers.

## Overlay services

Tor hidden services and the optional I2P eepsite use separate private loopback backends. I2P is enabled by default in the application container when an `i2pd` or SAM provider is available; configuration and status details are in [Configuration](configuration.md). Public autodiscovery omits overlay addresses unless the matching `expose` setting is enabled.

## Data providers

Some utilities use external providers; provider calls and required configuration are documented with the affected feature and in the server configuration reference. GeoIP uses local database files when enabled. The service does not imply that every tool or provider is active on every installation.

## Security reporting

The repository vulnerability channel and deployed-instance reporting flow are described in [Security](security.md). The public security overview and RFC 9116 file link to the appropriate reporting channels.
