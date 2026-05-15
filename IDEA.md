## Project description

API is a full-stack Go web application bundling multiple utility services into a single unified interface. It provides text manipulation, cryptographic operations, date/time utilities, and network/GeoIP tools through a versioned REST API, GraphQL endpoint, and a server-side rendered web UI with interactive tooling. Deployed as a single self-contained static binary.

## Project variables

project_name: api
project_org: apimgr
internal_name: api
internal_org: apimgr
app_name: Universal API Toolkit
repo: https://github.com/apimgr/api
license: MIT
binary: api
client_binary: api-cli

## Business logic

### Product scope & non-goals

**In scope:**
- **Text tools**: Base64 encode/decode, URL encode/decode, HTML encode/decode, case conversion, string hashing
- **Crypto tools**: Hash generation (MD5, SHA-1, SHA-256, SHA-512), HMAC, password generation, JWT encode/decode/verify, random token generation
- **Date/time tools**: Timezone conversion, date arithmetic, timestamp formatting, Unix/epoch conversion, ISO 8601 formatting
- **Network tools**: Caller IP detection (IPv4/IPv6), request header inspection, GeoIP lookup (country, city, ASN, coordinates)
- Full web frontend (server-side Go templates, dark/light/auto theme, PWA, mobile-first)
- Interactive tool pages where users input data and receive processed output
- Server pages: `/server/about`, `/server/help`, `/server/healthz`, `/server/privacy`, `/server/terms`
- CLI client (`api-cli`) for using all tools from the terminal
- OpenAPI/Swagger docs at `/api/{api_version}/server/swagger`
- GraphQL at `/graphql`

**Non-goals:**
- No user accounts, registration, or login of any kind
- No admin web panel (server configured via `server.yml` only)
- No persistent storage of user-submitted data
- No paid tiers, no API keys, no rate-limited access tiers
- No tools that require external paid APIs (e.g., SMS, payment processing)

### Roles & permissions

There are no user roles. All endpoints are public and require no authentication.

| Actor | Access |
|-------|--------|
| **Anonymous visitor (browser)** | Full access to all web tool pages and API endpoints |
| **Anonymous API client (curl/CLI)** | Full access to all API endpoints |
| **Server operator** | Configures server via `server.yml` only; no web management interface |

### Data model & sensitivity

This project processes transient inputs — no data is stored server-side. Inputs are request parameters; outputs are computed responses.

**Sensitivity considerations:**
- Users may submit sensitive values (passwords, tokens) to hashing/encoding tools. These are processed in memory and never logged or stored.
- GeoIP lookup reveals the caller's IP to the server (standard HTTP behavior). Results are not stored.
- JWT decode exposes payload content server-side; this is expected and intentional.

**Logging rules:**
- Never log request body contents
- Never log query parameter values for hashing or encoding endpoints
- Log only: method, path, status code, response time, anonymized source IP

### Trust boundaries & external services

| Boundary | Trust level | Notes |
|----------|-------------|-------|
| MaxMind GeoLite2 (downloaded at first run) | Trusted — HTTPS + checksum verified | Used for caller GeoIP lookup only |
| Incoming HTTP requests | **Untrusted** | All input validated and size-capped before processing |
| All tool inputs | **Untrusted** | Inputs are processed as data, never evaluated or executed |

No external API calls are made on behalf of user requests (no SSRF surface).

Failure mode for GeoIP: if databases unavailable, the network tool returns IP only without GeoIP fields. All other tools are unaffected.

### Threat model & abuse cases

**Primary assets:** service availability; user-submitted data (confidentiality during request processing).

**Attacker/abuser goals:**
- DoS via expensive hash or encoding operations at high rate
- SSRF via URL inputs (e.g., DNS lookup, WHOIS) — mitigated by only resolving caller-supplied hostnames through system resolver with timeout and no credential forwarding
- Input bomb: extremely large input to hashing or encoding endpoint to exhaust memory/CPU
- Log injection via crafted tool inputs

**Defenses:**
- Rate limiting on all endpoints
- Request body size cap (enforced before processing, not after reading)
- All tool inputs treated as opaque data — never executed (no `eval`, no `exec`, no `os.Open` on user input)
- DNS/network tool timeouts (hard deadline, not configurable by caller)
- Outputs logged only at path level, never body content

**Non-threats (explicitly out of scope):**
- Account enumeration — no accounts exist
- Privilege escalation — no roles exist

### Security decisions & exceptions

- **No authentication on any endpoint**: intentional. Public utility API. Rate limiting is the sole abuse-prevention mechanism.
- **GeoIP databases downloaded at runtime**: intentional for size and freshness. Integrity checked via HTTPS.
- **All responses include `Access-Control-Allow-Origin: *`**: intentional. Public API designed for cross-origin browser use.
- **MD5 and SHA-1 available in hash tool**: intentional. These are provided as utility tools for interoperability (legacy systems, checksum verification), not for password hashing. The API documentation explicitly notes they are cryptographically broken for security purposes.
