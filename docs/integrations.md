# Integrations

This page covers how the API server exposes itself to other tools —
`.well-known` discovery files, the API description formats it serves, and
the external data providers it calls out to. It documents only what is
currently implemented; it is not a roadmap.

## `.well-known` discovery

The `/.well-known/` namespace is reserved and root-owned: only allow-listed
entries are served, everything else returns `404`. Currently implemented:

| Path | Content | Notes |
|------|---------|-------|
| `/.well-known/security.txt` | RFC 9116 `security.txt` | same handler as `/security.txt` |

No identity-provider discovery endpoints are implemented yet — there is no
`/.well-known/webfinger`, `/.well-known/openid-configuration`,
`/.well-known/assetlinks.json`, `/.well-known/apple-app-site-association`,
or `/.well-known/mta-sts.txt` route. If your integration depends on one of
these, it is not currently available.

## Machine-readable site files

| Path | Purpose |
|------|---------|
| `/robots.txt` | Crawler allow/deny rules from `web.robots.allow` / `web.robots.deny` |
| `/manifest.json` | Web app manifest (PWA metadata: name, theme color, start URL) |
| `/security.txt` and `/.well-known/security.txt` | RFC 9116 security contact — see [Security](security.md) |

## API description formats

Every API feature is available through three parallel interfaces, kept in
sync from the same route handlers:

| Interface | Endpoint | Notes |
|-----------|----------|-------|
| REST/JSON | `/api/v1/...` | primary interface, content-negotiated (see [API Reference](api.md)) |
| OpenAPI/Swagger | spec at `/api/swagger` (unversioned alias) and `/api/v1/server/swagger` (versioned canonical) — same handler, no redirect | UI served at `/server/docs/swagger` |
| GraphQL | query endpoint at `/api/graphql` (unversioned alias) and `/api/v1/server/graphql` (versioned canonical) | UI (GraphiQL) served at `/server/docs/graphql` |

The OpenAPI spec is JSON only — there is no YAML representation and no
`.json` suffix route (the path is fixed).

## Outbound integrations (external data providers)

A small number of tool endpoints call out to third-party public data
providers rather than computing results locally:

| Category | Provider | Notes |
|----------|----------|-------|
| Weather (`/api/v1/weather/*`) | [Open-Meteo](https://open-meteo.com/) (`api.open-meteo.com`, `geocoding-api.open-meteo.com`, `air-quality-api.open-meteo.com`, `marine-api.open-meteo.com`, `archive-api.open-meteo.com`) | keyless public API |

GeoIP lookups (`/api/v1/geo/ip/{ip}`, `/api/v1/network/geoip`) use locally
downloaded MMDB databases rather than a live third-party API call — see
[Security](security.md) for how those databases are kept current.

## What is not implemented

To avoid confusion when integrating against this server, the following are
explicitly **not** available in the current codebase, even though they may
be referenced by the wider project specification:

- No OAuth/OpenID Connect identity-provider endpoints (no
  `/.well-known/openid-configuration`, no authorization/token endpoints)
- No WebFinger endpoint
- No app-linking files (`assetlinks.json`, `apple-app-site-association`)
- No `mta-sts.txt`

## Next Steps

- [API Reference](api.md)
- [Security](security.md)
- [Configuration](configuration.md)
