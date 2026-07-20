## Project description

API is a full-stack Go web application bundling multiple utility services into a single unified interface — targeting feature parity and beyond with tools like [it-tools](https://it-tools.tech). It provides text, crypto, encoding/conversion, date/time, generator, validator, math, developer, network, parsing, geo, GeoIP/OSINT, weather, image, fun/faker, language, research, and Docker utilities through a versioned REST API, GraphQL endpoint, and a server-side rendered web UI with interactive tooling. Deployed as a single self-contained static binary.

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

**In scope — target parity++ with tools like [it-tools](https://it-tools.tech), organized by category:**

- **Text**: encoding/decoding, hashing, case conversion, slugify, string comparison and analysis, compression, regex tooling, markup conversion (Markdown/HTML/BBCode), text ciphers, data extraction (emails/URLs/IPs/phones from free text), line manipulation, lorem-ipsum generation with themed variants
- **Crypto**: password hashing and verification, password/PIN generation and strength scoring, one-time-password generation and verification, message authentication codes, asymmetric keypair generation, symmetric and asymmetric encryption/decryption, JSON Web Token encode/decode/verify, mnemonic seed phrase generation, HTTP Basic Auth credential encoding
- **Convert**: numeric base conversion, encoding conversion, structured-data format conversion (JSON/YAML/TOML/XML/CSV), unit conversion (temperature, distance, weight, volume, duration), color format conversion, IPv4 address/subnet notation conversion, roman numerals, delimiter/list reshaping, container run-command to compose-file conversion
- **Date/time**: current time by timezone, epoch conversion, duration arithmetic, timezone lookup/conversion, human-readable duration formatting
- **Generators**: unique identifiers (UUID and similar), random strings/passwords/tokens/API keys, random color/MAC/IPv4, QR codes (including Wi-Fi QR codes), crontab expressions
- **Validators**: email, URL, IP, domain, phone, payment card checksum, IBAN, JSON, UUID, MAC address, and general format/length checks
- **Math**: arithmetic, trigonometry, logarithms, rounding, aggregate statistics, factorial, primality, GCD/LCM, random number generation, percentage calculations, expression evaluation
- **Developer utilities**: structured-data formatting and diffing, escaping/unescaping for common languages, code/text indentation, SQL formatting, file-permission (chmod) calculation, HTTP status code and MIME type reference lookup, keyboard keycode reference, HTML meta tag generation
- **Network**: caller IP and header inspection, user-agent parsing, MAC address vendor lookup, subnet calculation, IPv6 unique-local-address generation, random port suggestion
- **Parsing**: structured-data parsing (JSON/XML/CSV), URL and query-string parsing, date/number/boolean parsing, user-agent and email parsing
- **Geo**: great-circle distance, midpoint, bearing, destination point, coordinate validation and format conversion
- **GeoIP / OSINT** (see AI.md PART 19 for GeoIP database handling): caller/target IP geolocation, WHOIS lookup, DNS lookup, TLS certificate inspection — all reachable only through free, keyless mechanisms
- **Weather**: current conditions, forecast, location search, lookup by coordinates, unit conversions — backed only by a free, keyless data source, consistent with the no-paid/keyed-API non-goal below
- **Image**: resize, crop, rotate, metadata inspection, placeholder image generation
- **Fun**: dice roll, coin flip, random choice, magic-8-ball-style responses, fortune messages, yes/no, random emoji, joke delivery, shuffle, rock-paper-scissors
- **Lorem/faker**: lorem-ipsum text generation, fake person/address/company data for testing
- **Language**: language code/name lookup and listing (machine translation and language auto-detection are excluded — see non-goals)
- **Research**: citation formatting (APA/MLA/Chicago), bibliography generation, DOI formatting/validation
- **Docker/container**: Dockerfile generation, compose file generation, image name parsing, container name validation, port/volume mapping formatting and parsing
- **Testing/QA helpers**: mock data generation (email/username/user/API response), assertion helpers, execution-time measurement, fixture generation
- **System**: health, liveness/readiness probes, system info, version info
- Full web frontend (dark/light/auto theme, mobile-first, installable as a PWA)
- Interactive tool pages where users input data and receive processed output
- Informational server pages (about, help, health, privacy, terms — see AI.md PART 14 for route patterns)
- CLI client (`api-cli`) for using all tools from the terminal
- Machine-readable API documentation and a GraphQL endpoint alongside the REST API

**Non-goals:**
- No user accounts, registration, or login of any kind
- No admin web panel (server configured via `server.yml` only)
- No persistent storage of user-submitted data
- No paid tiers, no API keys, no rate-limited access tiers
- No tools that require external paid or keyed APIs (e.g., SMS, payment processing, commercial translation, commercial weather/geocoding) — every outbound integration must be free and keyless (system DNS resolver, public WHOIS protocol, direct TLS handshake, MaxMind GeoLite2, keyless weather provider)
- No client-hardware-only tools (e.g., camera/microphone recorder, local chronometer/stopwatch) — this is a server API/CLI, not a client-side SPA

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
| MaxMind GeoLite2 (downloaded at first run) | Trusted — HTTPS + checksum verified | Used for caller/target GeoIP lookup (`geo`/`osint` tools); no per-request outbound call |
| System DNS resolver (OSINT `DNSLookup`) | **Untrusted target, trusted resolver** | User-supplied domain resolved via the system resolver only; hard timeout; no credential forwarding |
| Public WHOIS protocol (OSINT `WHOISLookup`) | **Untrusted target** | Free, keyless TCP/43 protocol query to the domain's registrar-designated WHOIS server; hard timeout |
| Direct TLS handshake (OSINT `SSLInfo`) | **Untrusted target** | Connects to user-supplied `host:443` only to read the certificate; no data sent beyond the TLS handshake |
| Keyless weather/geocoding provider (`weather` tools) | Trusted provider, untrusted query | e.g. Open-Meteo — free, no API key; only location text/coordinates are sent |
| Incoming HTTP requests | **Untrusted** | All input validated and size-capped before processing |
| All tool inputs | **Untrusted** | Inputs are processed as data, never evaluated or executed |

Outbound calls exist only for the OSINT and weather tool families above, all user-directed (the caller explicitly names the domain/IP/host/location to query) and all free/keyless — never used to relay credentials or reach services on the caller's behalf. This is a bounded, intentional SSRF surface, not general server-side request forgery; see mitigations below.

Failure mode for GeoIP: if databases unavailable, the geo/network tools return IP only without GeoIP fields. All other tools are unaffected.

### Threat model & abuse cases

**Primary assets:** service availability; user-submitted data (confidentiality during request processing).

**Attacker/abuser goals:**
- DoS via expensive hash or encoding operations at high rate
- SSRF/internal-network scanning via URL, DNS, WHOIS, or SSL-info inputs — mitigated by only resolving caller-supplied hostnames through the system resolver with a timeout, no credential forwarding, and blocking loopback/link-local/private (RFC 1918) and other non-routable targets before any DNS/WHOIS/TLS call is made
- Input bomb: extremely large input to hashing or encoding endpoint to exhaust memory/CPU
- Log injection via crafted tool inputs

**Defenses:**
- Rate limiting on all endpoints
- Request body size cap (enforced before processing, not after reading)
- All tool inputs treated as opaque data — never executed (no `eval`, no `exec`, no `os.Open` on user input)
- DNS/WHOIS/SSL/weather tool timeouts (hard deadline, not configurable by caller) and private/loopback/link-local target blocking before any outbound call
- Outputs logged only at path level, never body content

**Non-threats (explicitly out of scope):**
- Account enumeration — no accounts exist
- Privilege escalation — no roles exist

### Security decisions & exceptions

- **No authentication on any endpoint**: intentional. Public utility API. Rate limiting is the sole abuse-prevention mechanism.
- **GeoIP databases downloaded at runtime**: intentional for size and freshness. Integrity checked via HTTPS.
- **All responses include `Access-Control-Allow-Origin: *`**: intentional. Public API designed for cross-origin browser use.
- **MD5 and SHA-1 available in hash tool**: intentional. These are provided as utility tools for interoperability (legacy systems, checksum verification), not for password hashing. The API documentation explicitly notes they are cryptographically broken for security purposes.
