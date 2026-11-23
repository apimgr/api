## Project description

API bundles text, cryptography, conversion, date/time, generator, validator, math, developer, network, parsing, geo, GeoIP/OSINT, weather, image, fun/faker, language, research, and Docker tools into one utility service. People can use its public HTTP and GraphQL APIs or its web interface. A companion command-line client supports terminal use.

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
- **Language**: language code/name lookup and listing, dictionary lookup (free Dictionary API), thesaurus lookup (free Datamuse API) — machine translation and language auto-detection are excluded, see non-goals
- **Research**: citation formatting (APA/MLA/Chicago), bibliography generation, DOI formatting/validation, arXiv paper lookup, ISBN book metadata lookup (free Open Library API)
- **Docker/container**: Dockerfile generation, compose file generation, image name parsing, container name validation, port/volume mapping formatting and parsing
- **Testing/QA helpers**: mock data generation (email/username/user/API response), assertion helpers, execution-time measurement, fixture generation
- **System**: health, liveness/readiness probes, system info, version info
- Full web frontend (dark/light/auto theme, mobile-first, installable as a PWA)
- Interactive tool pages where users input data and receive processed output
- Informational server pages (about, help, health, privacy, and contact)
- CLI client (`api-cli`) for using all tools from the terminal
- Machine-readable API documentation and a GraphQL endpoint alongside the REST API

**Non-goals:**
- No user accounts, registration, or login of any kind
- No admin web panel (server configured via `server.yml` only)
- No persistent storage of user-submitted data
- No paid tiers or API keys for ordinary utility access; no rate-limited access tiers
- No tools that require external paid or keyed APIs (e.g., SMS, payment processing, commercial translation requiring a key, commercial weather/geocoding requiring a key) — every outbound integration must be free, keyless, and commercial-use-friendly per that provider's terms; this applies to any tool category, not only OSINT/weather (system DNS resolver, public WHOIS protocol, direct TLS handshake, sapics/ip-location-db GeoIP data, keyless weather provider, and other free/keyless public APIs such as arXiv, Open Library, Datamuse, Free Dictionary API). Providers whose terms restrict use to personal/non-commercial/educational purposes (e.g. RainViewer) do NOT qualify and must not be used.
- No client-hardware-only tools (e.g., camera/microphone recorder, local chronometer/stopwatch) — this is a server API/CLI, not a client-side SPA

### Roles & permissions

There are no user accounts, profiles, registration, or login. Anonymous owners are credential-only identities without profiles; they can manage resources associated with their credentials. Public utility operations are available to anonymous visitors and API clients. Operator-only actions use the credentials and access rules defined by AI.md. Server administration is performed through configuration files and the command-line interface.

### Data model & sensitivity

Utility inputs are processed transiently and are not retained as user-submitted content. Server state required by AI.md, including anonymous resource ownership, credentials, audit events, and security records, is persisted separately from tool inputs.

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
| sapics/ip-location-db, CC BY 4.0 (downloaded at first run) | Trusted — HTTPS + checksum verified | Used for caller/target GeoIP lookup (`geo`/`osint` tools); no per-request outbound call |
| System DNS resolver (OSINT `DNSLookup`) | **Untrusted target, trusted resolver** | User-supplied domain resolved via the system resolver only; hard timeout; no credential forwarding |
| Public WHOIS protocol (OSINT `WHOISLookup`) | **Untrusted target** | Free, keyless TCP/43 protocol query to the domain's registrar-designated WHOIS server; hard timeout |
| Direct TLS handshake (OSINT `SSLInfo`) | **Untrusted target** | Connects to user-supplied `host:443` only to read the certificate; no data sent beyond the TLS handshake |
| Keyless weather/geocoding provider (`weather` tools) | Trusted provider, untrusted query | e.g. Open-Meteo — free, no API key; only location text/coordinates are sent |
| Free Dictionary API (`language/dictionary`) | Trusted provider, untrusted query | api.dictionaryapi.dev — free, no API key; only the looked-up word is sent |
| Datamuse API (`language/thesaurus`) | Trusted provider, untrusted query | api.datamuse.com — free, no API key; only the looked-up word is sent. Note: Datamuse requires a key starting 2027-01-01 |
| arXiv query API (`research/arxiv`) | Trusted provider, untrusted query | export.arxiv.org — free, no API key; only the arXiv ID is sent |
| Open Library Books API (`research/isbn`) | Trusted provider, untrusted query | openlibrary.org — free, no API key; only the ISBN is sent, descriptive User-Agent per their low-volume guidance |
| Incoming HTTP requests | **Untrusted** | All input validated and size-capped before processing |
| All tool inputs | **Untrusted** | Inputs are processed as data, never evaluated or executed |

Outbound calls are permitted for any tool family, not only OSINT/weather, provided each call is user-directed (the caller explicitly names the domain/IP/host/location/query to look up), the provider is free, keyless, and commercial-use-friendly per its terms, and the same SSRF mitigations below apply — never used to relay credentials or reach services on the caller's behalf. This is a bounded, intentional SSRF surface, not general server-side request forgery; see mitigations below. Each new outbound provider gets its own row in the table above once implemented.

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

### Security decisions & exceptions

- **No user accounts or login**: intentional. Anonymous resource ownership and operator access follow the credential model defined by AI.md; public utility operations remain available without an account.
- **GeoIP databases downloaded at runtime**: intentional for size and freshness. Integrity checked via HTTPS.
- **MD5 and SHA-1 available in hash tool**: intentional. These are provided as utility tools for interoperability (legacy systems, checksum verification), not for password hashing. The API documentation explicitly notes they are cryptographically broken for security purposes.

### API routes

Project-defined API routes are inventoried below. AI.md PART 14 defines the shared server-template routes; those are not repeated here.

- GET /api/{api_version}/convert/area: no authentication; area. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/convert/area/{value}/{from}/{to}: no authentication; area value from to. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/convert/color: no authentication; color. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/convert/currency: no authentication; currency. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/convert/data: no authentication; data. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/convert/data/{value}/{from}/{to}: no authentication; data value from to. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/convert/energy: no authentication; energy. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/convert/energy/{value}/{from}/{to}: no authentication; energy value from to. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/convert/length: no authentication; length. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/convert/length/{value}/{from}/{to}: no authentication; length value from to. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/convert/pressure: no authentication; pressure. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/convert/pressure/{value}/{from}/{to}: no authentication; pressure value from to. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/convert/speed: no authentication; speed. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/convert/speed/{value}/{from}/{to}: no authentication; speed value from to. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/convert/temperature: no authentication; temperature. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/convert/temperature/{value}/{from}/{to}: no authentication; temperature value from to. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/convert/time: no authentication; time. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/convert/time/{value}/{from}/{to}: no authentication; time value from to. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/convert/volume: no authentication; volume. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/convert/volume/{value}/{from}/{to}: no authentication; volume value from to. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/convert/weight: no authentication; weight. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/convert/weight/{value}/{from}/{to}: no authentication; weight value from to. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/crypto/bcrypt: no authentication; bcrypt. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/crypto/bcrypt/hash: no authentication; bcrypt hash. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/crypto/bcrypt/hash/{password}: no authentication; bcrypt hash password. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/crypto/bcrypt/verify: no authentication; bcrypt verify. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/crypto/bcrypt/verify/{password}/{hash}: no authentication; bcrypt verify password hash. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/crypto/bcrypt/{cost}/{password}: no authentication; bcrypt cost password. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/crypto/bcrypt/{password}: no authentication; bcrypt password. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/crypto/hash: no authentication; hash. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/crypto/hash.txt: no authentication; hash.txt. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/crypto/hash/{algorithm}/{input}: no authentication; hash algorithm input. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/crypto/hash/{algorithm}/{input}.txt: no authentication; hash algorithm input.txt. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/crypto/jwt: no authentication; jwt. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/crypto/jwt/{token}: no authentication; jwt token. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/crypto/password: no authentication; password. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/crypto/password.txt: no authentication; password.txt. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/crypto/password/strength: no authentication; password strength. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/crypto/password/strength/{password}: no authentication; password strength password. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/crypto/password/{length}: no authentication; password length. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/crypto/password/{length}.txt: no authentication; password length.txt. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/crypto/pin: no authentication; pin. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/crypto/pin.txt: no authentication; pin.txt. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/crypto/pin/{length}: no authentication; pin length. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/crypto/pin/{length}.txt: no authentication; pin length.txt. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/crypto/random/bytes: no authentication; random bytes. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/crypto/random/bytes/{count}: no authentication; random bytes count. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/crypto/random/hex: no authentication; random hex. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/crypto/random/hex/{count}: no authentication; random hex count. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/crypto/totp/code: no authentication; totp code. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/crypto/totp/code.txt: no authentication; totp code.txt. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/crypto/totp/code/{secret}: no authentication; totp code secret. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/crypto/totp/code/{secret}.txt: no authentication; totp code secret.txt. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/crypto/totp/generate: no authentication; totp generate. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/crypto/totp/secret: no authentication; totp secret. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/crypto/totp/verify: no authentication; totp verify. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/crypto/totp/verify/{secret}/{code}: no authentication; totp verify secret code. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/datetime/add: no authentication; add. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/datetime/add/{timestamp}/{duration}: no authentication; add timestamp duration. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/datetime/calendar: no authentication; calendar. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/datetime/calendar/{year}/{month}: no authentication; calendar year month. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/datetime/convert: no authentication; convert. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/datetime/convert/{timestamp}: no authentication; convert timestamp. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/datetime/convert/{timestamp}/{timezone}: no authentication; convert timestamp timezone. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/datetime/cron: no authentication; cron. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/datetime/diff: no authentication; diff. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/datetime/diff/{timestamp1}/{timestamp2}: no authentication; diff timestamp1 timestamp2. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/datetime/format: no authentication; format. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/datetime/format/{timestamp}/{format}: no authentication; format timestamp format. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/datetime/moon: no authentication; moon. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/datetime/moon/{date}: no authentication; moon date. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/datetime/now: no authentication; now. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/datetime/now.txt: no authentication; now.txt. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/datetime/now/{timezone}: no authentication; now timezone. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/datetime/parse: no authentication; parse. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/datetime/parse/{value}: no authentication; parse value. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/datetime/sunrise: no authentication; sunrise. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/datetime/sunrise/{lat}/{lon}: no authentication; sunrise lat lon. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/datetime/sunrise/{lat}/{lon}/{date}: no authentication; sunrise lat lon date. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/datetime/timestamp: no authentication; timestamp. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/datetime/timestamp.txt: no authentication; timestamp.txt. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/datetime/timezone: no authentication; timezone. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/datetime/timezone/convert: no authentication; timezone convert. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/datetime/timezone/convert/{timestamp}/{from}/{to}: no authentication; timezone convert timestamp from to. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/datetime/timezone/{timezone}: no authentication; timezone timezone. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/datetime/timezones: no authentication; timezones. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/datetime/to-unix: no authentication; to unix. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/datetime/to-unix/{datetime}: no authentication; to unix datetime. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/datetime/workdays: no authentication; workdays. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/datetime/workdays/{start}/{end}: no authentication; workdays start end. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/dev/cron: no authentication; cron. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/dev/echo: no authentication; echo. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/dev/jwt: no authentication; jwt. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/dev/jwt/{token}: no authentication; jwt token. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/docker/best-practices: no authentication; best practices. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/docker/network-helper: no authentication; network helper. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/docker/port-mapping: no authentication; port mapping. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/docker/version: no authentication; version. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/docker/volume-helper: no authentication; volume helper. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/fun/compliment: no authentication; compliment. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/fun/dad-joke: no authentication; dad joke. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/fun/fact: no authentication; fact. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/fun/fortune: no authentication; fortune. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/fun/insult: no authentication; insult. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/fun/joke: no authentication; joke. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/fun/meme: no authentication; meme. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/fun/motivational: no authentication; motivational. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/fun/programming-joke: no authentication; programming joke. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/fun/quote: no authentication; quote. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/fun/riddle: no authentication; riddle. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/fun/trivia: no authentication; trivia. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/generate/api-docs: no authentication; api docs. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/generate/avatar: no authentication; avatar. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/generate/barcode: no authentication; barcode. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/generate/config: no authentication; config. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/generate/dockerfile: no authentication; dockerfile. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/generate/gitignore: no authentication; gitignore. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/generate/identicon: no authentication; identicon. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/generate/license: no authentication; license. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/generate/placeholder: no authentication; placeholder. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/generate/placeholder/{width}/{height}: no authentication; placeholder width height. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/generate/qr: no authentication; qr. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/generate/ssh-key: no authentication; ssh key. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/geo/bbox: no authentication; bbox. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/geo/bearing: no authentication; bearing. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/geo/country: no authentication; country. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/geo/distance: no authentication; distance. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/geo/geocode: no authentication; geocode. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/geo/geohash: no authentication; geohash. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/geo/h3: no authentication; h3. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/geo/ip: no authentication; ip. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/geo/ip/{ip}: no authentication; ip ip. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/geo/midpoint: no authentication; midpoint. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/geo/pluscode: no authentication; pluscode. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/geo/reverse: no authentication; reverse. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/geo/timezone: no authentication; timezone. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/image/avatar: no authentication; avatar. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/image/barcode: no authentication; barcode. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/image/identicon: no authentication; identicon. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/image/placeholder: no authentication; placeholder. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/image/placeholder/{width}/{height}: no authentication; placeholder width height. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/image/qr: no authentication; qr. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/language/phonetic: no authentication; phonetic. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/lorem/address: no authentication; address. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/lorem/company: no authentication; company. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/lorem/person: no authentication; person. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/math/base: no authentication; base. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/math/calculate: no authentication; calculate. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/math/fibonacci: no authentication; fibonacci. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/math/prime: no authentication; prime. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/math/prime/{n}: no authentication; prime n. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/math/random: no authentication; random. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/math/random/{min}/{max}: no authentication; random min max. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/math/sequence: no authentication; sequence. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/math/stats: no authentication; stats. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/network/dns: no authentication; dns. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/network/dns/{domain}: no authentication; dns domain. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/network/dns/{domain}/{type}: no authentication; dns domain type. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/network/headers: no authentication; headers. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/network/ip: no authentication; ip. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/network/ip/{ip}: no authentication; ip ip. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/network/mac: no authentication; mac. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/network/mac/{mac}: no authentication; mac mac. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/network/ping: no authentication; ping. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/network/port: no authentication; port. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/network/ssl: no authentication; ssl. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/network/subnet: no authentication; subnet. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.


- GET /api/{api_version}/network/ula: no authentication; ula. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/network/url: no authentication; url. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/network/user-agent: no authentication; user agent. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/network/whois: no authentication; whois. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.



- GET /api/{api_version}/osint/cert: no authentication; cert. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/osint/cert/{domain}: no authentication; cert domain. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.



- GET /api/{api_version}/osint/domain: no authentication; domain. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/osint/domain/{domain}: no authentication; domain domain. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/osint/email: no authentication; email. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/osint/email/{email}: no authentication; email email. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/osint/ip: no authentication; ip. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/osint/ip/{ip}: no authentication; ip ip. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.







- GET /api/{api_version}/osint/subdomain: no authentication; subdomain. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/osint/subdomain/{domain}: no authentication; subdomain domain. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/osint/tech-stack: no authentication; tech stack. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.



- GET /api/{api_version}/parse/jwt: no authentication; jwt. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/parse/jwt/{token}: no authentication; jwt token. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/research/doi: no authentication; doi. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/research/doi/{path}: no authentication; doi path. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/test/fake-data: no authentication; fake data. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/test/fixture: no authentication; fixture. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/test/fixture/{type}: no authentication; fixture type. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/test/http: no authentication; http. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.



- GET /api/{api_version}/test/request-inspector: no authentication; request inspector. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/test/response-generator: no authentication; response generator. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/test/status-codes: no authentication; status codes. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/test/status-codes/{code}: no authentication; status codes code. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/text/case: no authentication; case. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/text/case.txt: no authentication; case.txt. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/text/case/{style}/{input}: no authentication; case style input. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/text/case/{style}/{input}.txt: no authentication; case style input.txt. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/text/decode: no authentication; decode. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/text/decode.txt: no authentication; decode.txt. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/text/decode/{encoding}/{input}: no authentication; decode encoding input. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/text/decode/{encoding}/{input}.txt: no authentication; decode encoding input.txt. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/text/encode: no authentication; encode. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/text/encode.txt: no authentication; encode.txt. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/text/encode/{encoding}/{input}: no authentication; encode encoding input. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/text/encode/{encoding}/{input}.txt: no authentication; encode encoding input.txt. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/text/hash: no authentication; hash. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/text/hash.txt: no authentication; hash.txt. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/text/hash/multi: no authentication; hash multi. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/text/hash/multi/{input}: no authentication; hash multi input. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/text/hash/{algorithm}/{input}: no authentication; hash algorithm input. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/text/hash/{algorithm}/{input}.txt: no authentication; hash algorithm input.txt. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/text/lorem: no authentication; lorem. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/text/lorem.txt: no authentication; lorem.txt. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/text/lorem/{type}: no authentication; lorem type. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/text/lorem/{type}/{count}: no authentication; lorem type count. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/text/lorem/{type}/{count}.txt: no authentication; lorem type count.txt. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/text/nanoid: no authentication; nanoid. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/text/reverse: no authentication; reverse. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/text/reverse.txt: no authentication; reverse.txt. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/text/reverse/{input}: no authentication; reverse input. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/text/reverse/{input}.txt: no authentication; reverse input.txt. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/text/rot13: no authentication; rot13. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/text/rot13.txt: no authentication; rot13.txt. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/text/rot13/{input}: no authentication; rot13 input. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/text/rot13/{input}.txt: no authentication; rot13 input.txt. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/text/ulid: no authentication; ulid. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/text/uuid: no authentication; uuid. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/text/uuid.txt: no authentication; uuid.txt. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/text/uuid/{version}: no authentication; uuid version. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/text/uuid/{version}.txt: no authentication; uuid version.txt. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/text/uuid/{version}/{count}: no authentication; uuid version count. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/weather/air-quality: no authentication; air quality. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/weather/air-quality/{location}: no authentication; air quality location. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/weather/alerts: no authentication; alerts. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/weather/alerts/{location}: no authentication; alerts location. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/weather/astronomy: no authentication; astronomy. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/weather/astronomy/{location}: no authentication; astronomy location. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/weather/current: no authentication; current. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/weather/current/{location}: no authentication; current location. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/weather/forecast: no authentication; forecast. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/weather/forecast/{location}: no authentication; forecast location. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/weather/historical: no authentication; historical. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/weather/historical/{location}: no authentication; historical location. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/weather/hourly: no authentication; hourly. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/weather/hourly/{location}: no authentication; hourly location. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.



- GET /api/{api_version}/weather/marine: no authentication; marine. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/weather/marine/{location}: no authentication; marine location. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/weather/pollen: no authentication; pollen. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/weather/pollen/{location}: no authentication; pollen location. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.



- GET /api/{api_version}/weather/uv: no authentication; uv. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- GET /api/{api_version}/weather/uv/{location}: no authentication; uv location. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- POST /api/{api_version}/crypto/bcrypt/verify: no authentication; bcrypt verify. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- POST /api/{api_version}/crypto/certificate: no authentication; certificate. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- POST /api/{api_version}/crypto/decrypt: no authentication; decrypt. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- POST /api/{api_version}/crypto/ed25519: no authentication; ed25519. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- POST /api/{api_version}/crypto/encrypt: no authentication; encrypt. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- POST /api/{api_version}/crypto/hmac: no authentication; hmac. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- POST /api/{api_version}/crypto/password/strength: no authentication; password strength. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- POST /api/{api_version}/crypto/pgp: no authentication; pgp. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- POST /api/{api_version}/crypto/rsa: no authentication; rsa. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- POST /api/{api_version}/dev/base64: no authentication; base64. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- POST /api/{api_version}/dev/format/css: no authentication; format css. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- POST /api/{api_version}/dev/format/html: no authentication; format html. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- POST /api/{api_version}/dev/format/js: no authentication; format js. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- POST /api/{api_version}/dev/format/json: no authentication; format json. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- POST /api/{api_version}/dev/format/sql: no authentication; format sql. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- POST /api/{api_version}/dev/format/xml: no authentication; format xml. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- POST /api/{api_version}/dev/regex: no authentication; regex. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- POST /api/{api_version}/dev/url-encode: no authentication; url encode. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- POST /api/{api_version}/docker/compose-to-run: no authentication; compose to run. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- POST /api/{api_version}/docker/compose-validate: no authentication; compose validate. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- POST /api/{api_version}/docker/dockerfile-generate: no authentication; dockerfile generate. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- POST /api/{api_version}/docker/dockerfile-lint: no authentication; dockerfile lint. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- POST /api/{api_version}/docker/env-parser: no authentication; env parser. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- POST /api/{api_version}/docker/run-to-compose: no authentication; run to compose. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- POST /api/{api_version}/docker/security-scan: no authentication; security scan. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- POST /api/{api_version}/docker/size-optimizer: no authentication; size optimizer. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- POST /api/{api_version}/generate/sql: no authentication; sql. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- POST /api/{api_version}/image/convert: no authentication; convert. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- POST /api/{api_version}/image/crop: no authentication; crop. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- POST /api/{api_version}/image/filter: no authentication; filter. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- POST /api/{api_version}/image/metadata: no authentication; metadata. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- POST /api/{api_version}/image/optimize: no authentication; optimize. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- POST /api/{api_version}/image/resize: no authentication; resize. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- POST /api/{api_version}/image/watermark: no authentication; watermark. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.


- POST /api/{api_version}/language/dictionary: no authentication; dictionary. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.


- POST /api/{api_version}/language/keywords: no authentication; keywords. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- POST /api/{api_version}/language/readability: no authentication; readability. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- POST /api/{api_version}/language/reading-time: no authentication; reading time. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- POST /api/{api_version}/language/sentiment: no authentication; sentiment. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.


- POST /api/{api_version}/language/thesaurus: no authentication; thesaurus. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.


- POST /api/{api_version}/language/word-count: no authentication; word count. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- POST /api/{api_version}/math/matrix: no authentication; matrix. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- POST /api/{api_version}/parse/csv: no authentication; csv. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- POST /api/{api_version}/parse/env: no authentication; env. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- POST /api/{api_version}/parse/html: no authentication; html. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- POST /api/{api_version}/parse/ini: no authentication; ini. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- POST /api/{api_version}/parse/json: no authentication; json. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- POST /api/{api_version}/parse/log: no authentication; log. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- POST /api/{api_version}/parse/markdown: no authentication; markdown. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- POST /api/{api_version}/parse/sql: no authentication; sql. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- POST /api/{api_version}/parse/toml: no authentication; toml. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- POST /api/{api_version}/parse/xml: no authentication; xml. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- POST /api/{api_version}/parse/yaml: no authentication; yaml. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- POST /api/{api_version}/research/arxiv: no authentication; arxiv. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.


- POST /api/{api_version}/research/citation: no authentication; citation. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.



- POST /api/{api_version}/research/isbn: no authentication; isbn. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.







- POST /api/{api_version}/test/api-client: no authentication; api client. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- POST /api/{api_version}/test/assert: no authentication; assert. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- POST /api/{api_version}/test/curl-generator: no authentication; curl generator. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.



- POST /api/{api_version}/test/postman: no authentication; postman. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- POST /api/{api_version}/test/request-inspector: no authentication; request inspector. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- POST /api/{api_version}/test/webhook: no authentication; webhook. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- POST /api/{api_version}/text/compress: no authentication; compress. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- POST /api/{api_version}/text/diff: no authentication; diff. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- POST /api/{api_version}/text/extract: no authentication; extract. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- POST /api/{api_version}/text/regex: no authentication; regex. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- POST /api/{api_version}/text/stats: no authentication; stats. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- POST /api/{api_version}/validate/credit-card: no authentication; credit card. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- POST /api/{api_version}/validate/domain: no authentication; domain. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- POST /api/{api_version}/validate/email: no authentication; email. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- POST /api/{api_version}/validate/iban: no authentication; iban. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- POST /api/{api_version}/validate/ip: no authentication; ip. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- POST /api/{api_version}/validate/isbn: no authentication; isbn. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- POST /api/{api_version}/validate/json: no authentication; json. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- POST /api/{api_version}/validate/mac: no authentication; mac. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- POST /api/{api_version}/validate/phone: no authentication; phone. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- POST /api/{api_version}/validate/url: no authentication; url. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- POST /api/{api_version}/validate/uuid: no authentication; uuid. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

- POST /api/{api_version}/validate/vat: no authentication; vat. Request: applicable path, query, or POST body input. Response: requested result in the endpoint format.

### Product data and compliance scope

The product has no user profiles, payment processing, or user accounts. It does not intentionally collect PHI or cardholder data. The server may process source IPs and other request metadata as described above; AI.md defines the required security and privacy behavior.
