# Security

This page documents the security-related behavior of the API server: rate
limiting, the public `security.txt` endpoint, and how to report a security
issue.

## Rate Limiting

Every request is rate limited per client IP using a sliding-window counter
(`server.rate_limit` in `server.yml`). Requests are classified before the
per-class limit is checked:

| Class | Matches | Default limit |
|-------|---------|----------------|
| `health` | health-check paths | 120 requests / 60s |
| `read` | `GET`/`HEAD` requests | 120 requests / 60s |
| `write` | all other methods (`POST`, `PUT`, `PATCH`, `DELETE`) | 10 requests / 60s |
| `global` | absolute ceiling across all classes combined | 240 requests / 60s |

The `global` ceiling is checked first; if it is exceeded, the request is
rejected regardless of which class it would otherwise fall into. Rate
limiting can be disabled entirely with `server.rate_limit.enabled: false`.

Counters are stored in the configured cache backend (in-process `memory` by
default, or a shared `valkey`/`redis` store when `server.cache` is
configured) so limits are consistent across multiple server processes
sharing the same cache.

### Response headers

Every rate-limited response includes:

```
X-RateLimit-Limit: 120
X-RateLimit-Remaining: 95
X-RateLimit-Reset: 1705315900
```

When the limit is exceeded, the server returns `429 Too Many Requests` with
a `Retry-After`-bearing response; no rate-limit detail is placed in the
JSON body.

### Fail-open behavior

If the cache backend used for rate-limit counters is unreachable, the
request is allowed through (fail open) and a warning is logged. A degraded
cache never blocks legitimate traffic.

## `security.txt`

The server publishes an [RFC 9116](https://www.rfc-editor.org/rfc/rfc9116)
compliant `security.txt` at two paths, both served by the same handler:

- `/security.txt`
- `/.well-known/security.txt`

The response is plain text and currently includes:

```
Contact: mailto:security@example.com
Expires: 2026-01-01T00:00:00Z
Preferred-Languages: en
```

`Contact` comes from `web.security.email` in `server.yml`; `Expires` is
generated from the configured expiry date. There is no `/server/security`
page or `security_id`-gated report endpoint in the current server — reports
go directly to the configured security contact address shown in
`security.txt`, and general contact submissions go through
[`/server/contact`](#contact-page).

## Contact page

`/server/contact` renders the general contact page, which surfaces the
configured security contact email (`web.security.email`) alongside general
contact information. It does not currently take a `security_id` query
parameter — there is no rotating-token report flow wired into the contact
handler.

## Security headers

Standard security headers (`X-Content-Type-Options`, `X-Frame-Options`,
`Referrer-Policy`, Content-Security-Policy, etc.) are applied to every
response. CSP behavior is stricter in `production` mode than in
`development` mode — see [Configuration](configuration.md#application-modes)
for the mode differences.

## Reporting a vulnerability

To report a security issue, use the contact address published in
`security.txt` (`GET /security.txt` or `GET /.well-known/security.txt`).
Do not open a public issue for undisclosed vulnerabilities.

## Next Steps

- [Configuration reference](configuration.md)
- [CLI reference](cli.md)
- [API reference](api.md)
