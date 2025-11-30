# CasTools - Universal API Toolkit
## Complete Technical Specification v1.0.0

---

# SECTION 1: PROJECT FUNDAMENTALS

## 1.1 Project Identity

```yaml
name: api
organization: apimgr
tagline: "Everything but the kitchen sink... actually, that too."
license: MIT
license_file: LICENSE.md
readme_file: README.md
ai_sync_file: CLAUDE.md
```

## 1.2 Target Audience

| Audience | Technical Level | Priority |
|----------|-----------------|----------|
| Self-hosted | Low-Medium | Primary |
| SMB (Small/Medium Business) | Low-Medium | Primary |
| Enterprise | Medium-High | Secondary |

## 1.3 Non-Negotiable Rules

1. **Validate Everything** - All inputs must be validated before processing
2. **Sanitize Where Appropriate** - All user inputs sanitized before use
3. **Save Only What Is Valid** - Never persist invalid data
4. **Only Clear What Is Invalid** - Never destroy valid data
5. **Test Everything** - All features must have tests
6. **Show Tooltips/Documentation** - Help users understand features
7. **Security First** - Security never compromises usability
8. **Mobile First** - All UI works on mobile devices
9. **Sane Defaults** - Every setting has a sensible default
10. **Single Static Binary** - All assets embedded, no external files needed

## 1.4 Supported Platforms

| OS | Architectures | Service Manager |
|----|---------------|-----------------|
| Linux | amd64, arm64 | systemd, runit, openrc, s6 |
| macOS | amd64, arm64 | launchd |
| Windows | amd64, arm64 | Windows Service |
| FreeBSD | amd64, arm64 | rc.d |
| OpenBSD | amd64, arm64 | rc.d |
| NetBSD | amd64, arm64 | rc.d |

---

# SECTION 2: DIRECTORY STRUCTURE

## 2.1 Project Layout

```
./
├── README.md                    # Main documentation
├── LICENSE.md                   # MIT + embedded licenses
├── CLAUDE.md                    # AI sync file (always updated)
├── TODO.AI.md                   # Task tracking for AI
├── CHANGELOG.md                 # Version history
├── Makefile                     # Build automation
├── Dockerfile                   # Container build
├── docker-compose.yml           # Container orchestration
├── Jenkinsfile                  # CI/CD pipeline
├── release.txt                  # Version tracking
├── .gitignore                   # Git ignores
├── .dockerignore                # Docker ignores
├── src/                         # All source code
│   ├── main.go                  # Entry point
│   ├── cmd/                     # CLI commands
│   ├── internal/                # Private packages
│   │   ├── api/                 # API handlers
│   │   ├── config/              # Configuration
│   │   ├── crypto/              # Cryptography
│   │   ├── data/                # Embedded data
│   │   ├── graphql/             # GraphQL schema
│   │   ├── handlers/            # HTTP handlers
│   │   ├── middleware/          # HTTP middleware
│   │   ├── models/              # Data models
│   │   ├── services/            # Business logic
│   │   ├── validation/          # Input validation
│   │   └── web/                 # Embedded web UI
│   └── pkg/                     # Public packages
├── scripts/                     # Installation scripts
│   ├── README.md                # Script documentation
│   ├── install.sh               # Universal installer
│   ├── linux.sh                 # Linux-specific
│   ├── macos.sh                 # macOS-specific
│   └── windows.ps1              # Windows-specific
├── tests/                       # Test files
│   ├── unit/                    # Unit tests
│   ├── integration/             # Integration tests
│   └── e2e/                     # End-to-end tests
├── binaries/                    # Built binaries (dev)
└── releases/                    # Release binaries (prod)
```

## 2.2 Runtime Directory Layout

### With Root/Admin (Global Install)

| OS | Config Dir | Data Dir | Log Dir | Binary |
|----|------------|----------|---------|--------|
| Linux | /etc/api | /var/lib/api | /var/log/api | /usr/local/bin/api |
| macOS | /etc/api | /var/lib/api | /var/log/api | /usr/local/bin/api |
| Windows | C:\ProgramData\api\config | C:\ProgramData\api\data | C:\ProgramData\api\logs | C:\Program Files\api\api.exe |
| FreeBSD | /usr/local/etc/api | /var/db/api | /var/log/api | /usr/local/bin/api |

### Without Root (User Install)

| OS | Config Dir | Data Dir | Log Dir | Binary |
|----|------------|----------|---------|--------|
| Linux | ~/.config/api | ~/.local/share/api | ~/.local/share/api/logs | ~/.local/bin/api |
| macOS | ~/Library/Application Support/api | ~/Library/Application Support/api/data | ~/Library/Logs/api | ~/bin/api |
| Windows | %APPDATA%\api\config | %APPDATA%\api\data | %APPDATA%\api\logs | %LOCALAPPDATA%\Programs\api\api.exe |

---

# SECTION 3: CONFIGURATION

## 3.1 Configuration File Format

**File**: `config.yaml` (in config directory)

```yaml
# CasTools Configuration
# All settings have sane defaults - only override what you need

server:
  address: 0.0.0.0                    # Listen address (never shown to user)
  port: 64365                         # Single port = HTTP only; "80,443" = HTTP+HTTPS
  fqdn: tools.example.com             # Fully qualified domain name (required for SSL)
  base_url: ""                        # Auto-detected if empty
  read_timeout: 30s
  write_timeout: 30s
  idle_timeout: 120s
  shutdown_timeout: 10s
  max_request_size: 10MB
  max_upload_size: 50MB

ssl:
  enabled: false                      # Auto-enabled if port includes 443
  cert_dir: /etc/api/ssl/certs   # Where certs are stored
  
  # Let's Encrypt settings
  letsencrypt:
    enabled: false                    # Enable automatic certificate
    email: ""                         # Required for Let's Encrypt
    staging: false                    # Use staging for testing
    challenge: http-01                # http-01, tls-alpn-01, dns-01
    
    # DNS-01 challenge providers
    dns_provider: ""                  # cloudflare, route53, rfc2136, etc.
    dns_credentials: {}               # Provider-specific credentials
    
  # Manual certificate paths (checked first)
  # Also checks /etc/letsencrypt/live/{fqdn}/ automatically
  cert_file: ""
  key_file: ""

admin:
  username: admin                     # Admin username
  password: ""                        # If empty, generated on first run
  token: ""                           # API token, generated if empty
  session_timeout: 24h
  
ui:
  theme: dark                         # dark, light
  logo: ""                            # URL or empty for default
  favicon: ""                         # URL or empty for default
  title: "CasTools"                   # Browser title
  
security:
  admin_email: security@example.com   # security.txt contact
  cors_origins:
    - "*"                             # CORS allowed origins
  rate_limit:
    enabled: true
    requests: 100                     # Requests per window
    window: 60s                       # Time window
    by: ip                            # ip, token, or both
  headers:
    hsts: false                       # HTTP Strict Transport Security
    hsts_max_age: 31536000
    frame_options: DENY
    content_type_nosniff: true
    xss_protection: true

robots:
  rules:
    - { path: "/", allow: true }
    - { path: "/api", allow: true }
    - { path: "/admin", allow: false }
    - { path: "/swagger", allow: false }

logging:
  level: info                         # debug, info, warn, error
  format: json                        # json, text, apache
  output: stdout                      # stdout, stderr, file
  file: ""                            # Log file path if output=file
  access_format: apache               # apache, json, combined

cache:
  enabled: true
  ttl: 5m
  max_items: 10000
  max_size: 100MB

scheduler:
  enabled: true
  cert_renewal: "0 0 * * *"           # Daily at midnight

features:
  swagger: true
  graphql: true
  graphql_playground: true
  metrics: true
  web_ui: true
  pwa: true
```

## 3.2 Environment Variables

All config options can be set via environment variables with prefix `CASTOOLS_`:

```bash
CASTOOLS_SERVER_PORT=8080
CASTOOLS_SERVER_FQDN=tools.example.com
CASTOOLS_ADMIN_USERNAME=admin
CASTOOLS_ADMIN_PASSWORD=secretpassword
CASTOOLS_UI_THEME=dark
CASTOOLS_SSL_LETSENCRYPT_ENABLED=true
CASTOOLS_SSL_LETSENCRYPT_EMAIL=admin@example.com
```

**Priority Order**: Environment Variables > Config File > Defaults

## 3.3 Default Port Selection

If no port specified:
1. Find unused port in range 64000-64999
2. Save to config file for persistence
3. Display selected port on startup

---

# SECTION 4: CLI INTERFACE

## 4.1 Command Structure

```bash
api [global-flags] [command] [command-flags]
```

## 4.2 Global Flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| --help | -h | - | Show help |
| --version | -v | - | Show version |
| --config | -c | (auto) | Config directory path |
| --data | -d | (auto) | Data directory path |
| --address | -a | 0.0.0.0 | Listen address |
| --port | -p | 64365 | Port or port pair |

**Note**: --help, --version, --status require no privileges

## 4.3 Commands

### Service Management

```bash
api service start       # Start the service
api service stop        # Stop the service
api service restart     # Restart the service
api service reload      # Reload configuration
api service status      # Show service status
api service install     # Install as system service
api service uninstall   # Remove system service
api service enable      # Enable auto-start
api service disable     # Disable auto-start
api service help        # Show service help
```

### Status Check

```bash
api --status            # Exit codes: 0=running, 1=stopped, 2=error
```

## 4.4 Console Output Style

```
✅ CasTools v1.0.0 started successfully
📡 Listening on https://tools.example.com:443
🔒 SSL certificate valid until 2025-03-15
📊 Swagger UI: https://tools.example.com/swagger
🎮 GraphQL Playground: https://tools.example.com/playground
```

**Rules**:
- Use emojis for visual clarity
- Never show 0.0.0.0, 127.0.0.1, localhost
- Always show FQDN or actual IP
- Show only the most relevant address

---

# SECTION 5: API FUNDAMENTALS

## 5.1 API Structure

### Base URLs

| Endpoint | Purpose |
|----------|---------|
| `/` | Web UI (HTML) |
| `/api/v1/` | REST API (JSON) |
| `/graphql` | GraphQL endpoint |
| `/playground` | GraphQL Playground |
| `/swagger` | Swagger UI |
| `/swagger/doc.json` | OpenAPI spec |
| `/redoc` | ReDoc documentation |
| `/health` | Health check |
| `/metrics` | Prometheus metrics |
| `/robots.txt` | Robots file |
| `/security.txt` | Security contact |
| `/.well-known/` | Well-known URIs |

### URL Design Principles

1. **Use Path Parameters** - Prefer `/api/v1/hash/sha256/{input}` over query params
2. **Sensible Defaults** - `/api/v1/uuid` returns UUID v4 by default
3. **Optional Extensions** - `/api/v1/uuid.txt` returns plain text
4. **Hierarchical Structure** - `/api/v1/{category}/{subcategory}/{action}/{params}`

## 5.2 Response Formats

### JSON Response (Default)

```json
{
  "success": true,
  "data": {
    "result": "value"
  },
  "meta": {
    "timestamp": "2024-01-15T10:30:00Z",
    "request_id": "550e8400-e29b-41d4-a716-446655440000",
    "execution_ms": 42,
    "version": "1.0.0"
  }
}
```

### Error Response

```json
{
  "success": false,
  "error": {
    "code": "VALIDATION_ERROR",
    "message": "Input exceeds maximum length",
    "field": "input",
    "details": {
      "max_length": 10000,
      "actual_length": 15000
    }
  },
  "meta": {
    "timestamp": "2024-01-15T10:30:00Z",
    "request_id": "550e8400-e29b-41d4-a716-446655440000"
  }
}
```

### Text Response (with .txt extension)

```
550e8400-e29b-41d4-a716-446655440000
```

## 5.3 HTTP Status Codes

| Code | Usage |
|------|-------|
| 200 | Success |
| 201 | Created |
| 204 | No Content |
| 400 | Bad Request / Validation Error |
| 401 | Unauthorized |
| 403 | Forbidden |
| 404 | Not Found |
| 405 | Method Not Allowed |
| 408 | Request Timeout |
| 413 | Payload Too Large |
| 415 | Unsupported Media Type |
| 422 | Unprocessable Entity |
| 429 | Too Many Requests |
| 500 | Internal Server Error |
| 502 | Bad Gateway |
| 503 | Service Unavailable |
| 504 | Gateway Timeout |

## 5.4 Common Query Parameters

These query parameters work on ALL endpoints:

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `format` | string | json | Response format: json, xml, yaml |
| `pretty` | bool | false | Pretty print output |
| `callback` | string | - | JSONP callback function |

## 5.5 Input Validation Rules

### Global Validation

| Rule | Value | Description |
|------|-------|-------------|
| Max Input Length | 1MB | Maximum input size for any single field |
| Max Request Size | 10MB | Maximum total request body |
| Max Upload Size | 50MB | Maximum file upload |
| Max Array Items | 10000 | Maximum items in array parameters |
| Max String Length | 1000000 | Maximum characters in string |
| Encoding | UTF-8 | Required input encoding |

### Sanitization Rules

| Input Type | Sanitization |
|------------|--------------|
| HTML | Escape all tags unless explicitly allowed |
| SQL | Parameterized queries only |
| Shell | Never execute user input |
| File paths | Canonicalize and validate |
| URLs | Validate scheme and structure |
| Email | Validate format, normalize |

---

# SECTION 6: TEXT UTILITIES API

## 6.1 Lorem Ipsum Generators

### Default Lorem Ipsum

```
GET /api/v1/text/lorem
GET /api/v1/text/lorem.txt
```

**Default**: 5 paragraphs of classic Lorem Ipsum

**Response**:
```json
{
  "success": true,
  "data": {
    "text": "Lorem ipsum dolor sit amet...",
    "type": "paragraphs",
    "count": 5,
    "word_count": 250,
    "character_count": 1500
  }
}
```

### Parameterized Lorem Ipsum

```
GET /api/v1/text/lorem/{type}/{count}
GET /api/v1/text/lorem/{type}/{count}.txt
```

| Parameter | Type | Values | Default | Validation |
|-----------|------|--------|---------|------------|
| type | path | paragraphs, sentences, words | paragraphs | Must be valid type |
| count | path | 1-1000 | 5 | Integer, min 1, max 1000 |

**Examples**:
```
GET /api/v1/text/lorem/paragraphs/3
GET /api/v1/text/lorem/sentences/10
GET /api/v1/text/lorem/words/50
GET /api/v1/text/lorem/words/50.txt
```

### Alternative Lorem Types

```
GET /api/v1/text/hipsum/{type}/{count}          # Hipster ipsum
GET /api/v1/text/bacon/{type}/{count}           # Bacon ipsum
GET /api/v1/text/cupcake/{type}/{count}         # Cupcake ipsum
GET /api/v1/text/pirate/{type}/{count}          # Pirate ipsum
GET /api/v1/text/zombie/{type}/{count}          # Zombie ipsum
GET /api/v1/text/corporate/{type}/{count}       # Corporate buzzwords
GET /api/v1/text/techno/{type}/{count}          # Tech jargon
GET /api/v1/text/samuel/{type}/{count}          # Samuel L. Jackson ipsum
```

All follow same parameter pattern as lorem.

## 6.2 ID Generators

### UUID Generation

```
GET /api/v1/text/uuid
GET /api/v1/text/uuid.txt
GET /api/v1/text/uuid/{version}
GET /api/v1/text/uuid/{version}.txt
GET /api/v1/text/uuid/{version}/{count}
GET /api/v1/text/uuid/{version}/{namespace}/{name}    # For v5
```

| Parameter | Type | Values | Default | Validation |
|-----------|------|--------|---------|------------|
| version | path | 1, 4, 5, 6, 7, nil, max | 4 | Must be valid version |
| count | path | 1-1000 | 1 | Integer |
| namespace | path | dns, url, oid, x500, {uuid} | - | Required for v5 |
| name | path | any string | - | Required for v5 |

**Examples**:
```
GET /api/v1/text/uuid                           # Returns single UUID v4
GET /api/v1/text/uuid/4                         # Returns single UUID v4
GET /api/v1/text/uuid/4/10                      # Returns 10 UUID v4s
GET /api/v1/text/uuid/7                         # Returns UUID v7 (time-based)
GET /api/v1/text/uuid/5/dns/example.com         # Returns UUID v5 for DNS name
GET /api/v1/text/uuid/nil                       # Returns nil UUID (all zeros)
GET /api/v1/text/uuid/max                       # Returns max UUID (all ones)
```

**Response** (single):
```json
{
  "success": true,
  "data": {
    "uuid": "550e8400-e29b-41d4-a716-446655440000",
    "version": 4,
    "variant": "RFC 4122"
  }
}
```

**Response** (multiple):
```json
{
  "success": true,
  "data": {
    "uuids": [
      "550e8400-e29b-41d4-a716-446655440000",
      "6ba7b810-9dad-11d1-80b4-00c04fd430c8"
    ],
    "count": 2,
    "version": 4
  }
}
```

### Other ID Formats

```
GET /api/v1/text/ulid                           # ULID
GET /api/v1/text/ulid/{count}
GET /api/v1/text/nanoid                         # NanoID (21 chars default)
GET /api/v1/text/nanoid/{length}                # Custom length 1-256
GET /api/v1/text/nanoid/{length}/{count}
GET /api/v1/text/ksuid                          # K-Sortable UID
GET /api/v1/text/ksuid/{count}
GET /api/v1/text/xid                            # XID
GET /api/v1/text/xid/{count}
GET /api/v1/text/cuid                           # CUID
GET /api/v1/text/cuid/{count}
GET /api/v1/text/cuid2                          # CUID2
GET /api/v1/text/cuid2/{count}
GET /api/v1/text/snowflake                      # Snowflake ID
GET /api/v1/text/snowflake/{count}
GET /api/v1/text/objectid                       # MongoDB ObjectID
GET /api/v1/text/objectid/{count}
GET /api/v1/text/typeid/{prefix}                # TypeID with prefix
GET /api/v1/text/typeid/{prefix}/{count}
GET /api/v1/text/sqid/{numbers}                 # Sqids encode (comma-separated)
GET /api/v1/text/sqid/decode/{id}               # Sqids decode
```

**Validation for each**:

| ID Type | Length | Character Set | Max Count |
|---------|--------|---------------|-----------|
| ULID | 26 | Crockford Base32 | 1000 |
| NanoID | 1-256 | A-Za-z0-9_- | 1000 |
| KSUID | 27 | Base62 | 1000 |
| XID | 20 | Base32 hex | 1000 |
| CUID | 25 | a-z0-9 | 1000 |
| CUID2 | 24 | a-z0-9 | 1000 |
| Snowflake | 19 | Numeric | 1000 |
| ObjectID | 24 | Hex | 1000 |
| TypeID | varies | prefix_base32 | 1000 |

## 6.3 Text Case Conversion

```
GET /api/v1/text/case/{style}/{input}
GET /api/v1/text/case/{style}/{input}.txt
POST /api/v1/text/case/{style}
```

| Parameter | Type | Values | Default | Validation |
|-----------|------|--------|---------|------------|
| style | path | see below | - | Required, must be valid style |
| input | path | URL-encoded text | - | Max 10000 chars |

**Supported Styles**:

| Style | Example Input | Example Output |
|-------|---------------|----------------|
| camel | hello world | helloWorld |
| pascal | hello world | HelloWorld |
| snake | hello world | hello_world |
| screaming_snake | hello world | HELLO_WORLD |
| kebab | hello world | hello-world |
| screaming_kebab | hello world | HELLO-WORLD |
| dot | hello world | hello.world |
| path | hello world | hello/world |
| title | hello world | Hello World |
| sentence | hello world | Hello world |
| lower | Hello World | hello world |
| upper | hello world | HELLO WORLD |
| swap | Hello World | hELLO wORLD |
| capitalize | hello world | Hello World |
| constant | hello world | HELLO_WORLD |

**Examples**:
```
GET /api/v1/text/case/camel/hello%20world          # Returns: helloWorld
GET /api/v1/text/case/snake/helloWorld             # Returns: hello_world
GET /api/v1/text/case/kebab/HelloWorld.txt         # Returns: hello-world (plain text)
```

**POST Body** (for longer text):
```json
{
  "text": "this is a longer piece of text that needs conversion"
}
```

## 6.4 Text Encoding/Decoding

### Encode

```
GET /api/v1/text/encode/{encoding}/{input}
GET /api/v1/text/encode/{encoding}/{input}.txt
POST /api/v1/text/encode/{encoding}
```

### Decode

```
GET /api/v1/text/decode/{encoding}/{input}
GET /api/v1/text/decode/{encoding}/{input}.txt
POST /api/v1/text/decode/{encoding}
```

| Parameter | Type | Values | Default | Validation |
|-----------|------|--------|---------|------------|
| encoding | path | see below | - | Required |
| input | path | URL-encoded data | - | Max 1MB |

**Supported Encodings**:

| Encoding | Description | Example |
|----------|-------------|---------|
| base64 | Standard Base64 | SGVsbG8= |
| base64url | URL-safe Base64 | SGVsbG8 |
| base32 | Standard Base32 | JBSWY3DP |
| base32hex | Base32 Hex | 91IMOR3F |
| base16 | Hexadecimal | 48656c6c6f |
| hex | Alias for base16 | 48656c6c6f |
| base58 | Bitcoin Base58 | 9Ajdvzr |
| base58check | Base58 with checksum | - |
| base62 | Alphanumeric | 1C92 |
| base85 | ASCII85 | 87cURDZ |
| ascii85 | Alias for base85 | 87cURDZ |
| z85 | ZeroMQ Base85 | HelloWorld |
| url | URL encoding | Hello%20World |
| html | HTML entities | &lt;div&gt; |
| xml | XML entities | &lt;tag&gt; |
| unicode | Unicode escapes | \u0048\u0065 |
| punycode | IDN encoding | xn--... |
| quotedprintable | QP encoding | =48=65 |
| uuencode | UU encoding | begin 644... |
| yenc | yEnc encoding | =ybegin... |
| rot13 | ROT13 cipher | Uryyb |
| rot47 | ROT47 cipher | w6==@ |

**Examples**:
```
GET /api/v1/text/encode/base64/Hello%20World       # SGVsbG8gV29ybGQ=
GET /api/v1/text/decode/base64/SGVsbG8gV29ybGQ=    # Hello World
GET /api/v1/text/encode/hex/Hello.txt              # 48656c6c6f
GET /api/v1/text/encode/url/Hello%20World          # Hello%2520World
```

**Response**:
```json
{
  "success": true,
  "data": {
    "input": "Hello World",
    "output": "SGVsbG8gV29ybGQ=",
    "encoding": "base64",
    "input_length": 11,
    "output_length": 16
  }
}
```

## 6.5 Text Hashing

```
GET /api/v1/text/hash/{algorithm}/{input}
GET /api/v1/text/hash/{algorithm}/{input}.txt
POST /api/v1/text/hash/{algorithm}
GET /api/v1/text/hash/multi/{input}               # All common hashes at once
POST /api/v1/text/hash/multi
```

| Parameter | Type | Values | Default | Validation |
|-----------|------|--------|---------|------------|
| algorithm | path | see below | sha256 | Must be valid algorithm |
| input | path | URL-encoded text | - | Max 1MB |

**Supported Hash Algorithms**:

| Category | Algorithms |
|----------|------------|
| MD | md4, md5 |
| SHA-1 | sha1 |
| SHA-2 | sha224, sha256, sha384, sha512, sha512-224, sha512-256 |
| SHA-3 | sha3-224, sha3-256, sha3-384, sha3-512, shake128, shake256 |
| Keccak | keccak256, keccak512 |
| BLAKE | blake2s-256, blake2b-256, blake2b-384, blake2b-512, blake3 |
| RIPEMD | ripemd160 |
| Whirlpool | whirlpool |
| Tiger | tiger, tiger2 |
| xxHash | xxhash32, xxhash64, xxhash128 |
| MurmurHash | murmur3-32, murmur3-128 |
| CityHash | cityhash64, cityhash128 |
| FarmHash | farmhash64, farmhash128 |
| SipHash | siphash-2-4 |
| FNV | fnv1-32, fnv1-64, fnv1a-32, fnv1a-64 |
| CRC | crc16, crc32, crc32c, crc64 |
| Adler | adler32 |

**Examples**:
```
GET /api/v1/text/hash/sha256/hello                 # Single hash
GET /api/v1/text/hash/md5/hello.txt                # Plain text output
GET /api/v1/text/hash/multi/hello                  # Multiple hashes
```

**Response** (single):
```json
{
  "success": true,
  "data": {
    "algorithm": "sha256",
    "hash": "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824",
    "input_length": 5,
    "hash_length": 32,
    "hash_bits": 256
  }
}
```

**Response** (multi):
```json
{
  "success": true,
  "data": {
    "input_length": 5,
    "hashes": {
      "md5": "5d41402abc4b2a76b9719d911017c592",
      "sha1": "aaf4c61ddcc5e8a2dabede0f3b482cd9aea9434d",
      "sha256": "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824",
      "sha512": "9b71d224bd62f3785d96d46ad3ea3d73319bfbc2890caadae2dff72519673ca72323c3d99ba5c11d7c7acc6e14b8c5da0c4663475c2e5c3adef46f73bcdec043",
      "blake3": "ea8f163db38682925e4491c5e58d4bb3506ef8c14eb78a86e908c5624a67200f"
    }
  }
}
```

## 6.6 HMAC Generation

```
GET /api/v1/text/hmac/{algorithm}/{key}/{message}
GET /api/v1/text/hmac/{algorithm}/{key}/{message}.txt
POST /api/v1/text/hmac/{algorithm}
```

| Parameter | Type | Values | Default | Validation |
|-----------|------|--------|---------|------------|
| algorithm | path | md5, sha1, sha256, sha384, sha512, sha3-256, sha3-512, blake2b, blake3 | sha256 | Must be valid |
| key | path | URL-encoded key | - | Required, max 1KB |
| message | path | URL-encoded message | - | Required, max 1MB |

**POST Body**:
```json
{
  "key": "secret-key",
  "message": "message to authenticate",
  "key_encoding": "utf8",
  "output_format": "hex"
}
```

**Response**:
```json
{
  "success": true,
  "data": {
    "algorithm": "sha256",
    "hmac": "b613679a0814d9ec772f95d778c35fc5ff1697c493715653c6c712144292c5ad",
    "key_length": 10,
    "message_length": 23
  }
}
```

## 6.7 Text Compression

```
GET /api/v1/text/compress/{algorithm}/{input}
GET /api/v1/text/decompress/{algorithm}/{input}
POST /api/v1/text/compress/{algorithm}
POST /api/v1/text/decompress/{algorithm}
```

| Parameter | Type | Values | Default | Validation |
|-----------|------|--------|---------|------------|
| algorithm | path | gzip, deflate, zlib, brotli, zstd, lz4, snappy, lzma, xz | gzip | Must be valid |
| input | path | URL-encoded (base64 for binary) | - | Max 10MB |

**Compression Levels** (via query param):

```
?level=1-9    # 1=fastest, 9=best compression (default varies by algorithm)
```

**Response**:
```json
{
  "success": true,
  "data": {
    "algorithm": "gzip",
    "input_size": 10000,
    "output_size": 3500,
    "ratio": 0.35,
    "output": "H4sIAAAAAAAAA..."
  }
}
```

## 6.8 Regex Operations

```
POST /api/v1/text/regex/test
POST /api/v1/text/regex/match
POST /api/v1/text/regex/replace
POST /api/v1/text/regex/split
POST /api/v1/text/regex/extract
GET /api/v1/text/regex/validate/{pattern}
```

**Test Request**:
```json
{
  "pattern": "\\d+",
  "text": "abc123def456",
  "flags": "g"
}
```

**Test Response**:
```json
{
  "success": true,
  "data": {
    "matches": true,
    "match_count": 2,
    "matches": [
      {"match": "123", "index": 3, "groups": []},
      {"match": "456", "index": 9, "groups": []}
    ],
    "execution_time_us": 15
  }
}
```

**Replace Request**:
```json
{
  "pattern": "\\d+",
  "text": "abc123def456",
  "replacement": "XXX",
  "flags": "g"
}
```

**Flags**:

| Flag | Description |
|------|-------------|
| g | Global (all matches) |
| i | Case insensitive |
| m | Multiline |
| s | Dotall (. matches newline) |
| x | Extended (ignore whitespace) |
| U | Ungreedy |

## 6.9 Text Diff

```
POST /api/v1/text/diff
POST /api/v1/text/diff/unified
POST /api/v1/text/diff/html
POST /api/v1/text/patch/apply
POST /api/v1/text/patch/create
```

**Diff Request**:
```json
{
  "original": "Hello World",
  "modified": "Hello Universe",
  "context_lines": 3
}
```

**Diff Response**:
```json
{
  "success": true,
  "data": {
    "changes": [
      {"type": "equal", "value": "Hello "},
      {"type": "delete", "value": "World"},
      {"type": "insert", "value": "Universe"}
    ],
    "additions": 1,
    "deletions": 1,
    "unified": "--- original\n+++ modified\n@@ -1 +1 @@\n-Hello World\n+Hello Universe"
  }
}
```

## 6.10 Text Statistics

```
GET /api/v1/text/stats/{input}
POST /api/v1/text/stats
GET /api/v1/text/count/{input}
POST /api/v1/text/count
```

**Response**:
```json
{
  "success": true,
  "data": {
    "characters": 1500,
    "characters_no_spaces": 1200,
    "words": 250,
    "sentences": 15,
    "paragraphs": 5,
    "lines": 20,
    "bytes": 1500,
    "reading_time_seconds": 60,
    "speaking_time_seconds": 90,
    "readability": {
      "flesch_reading_ease": 65.5,
      "flesch_kincaid_grade": 8.2,
      "gunning_fog": 10.1,
      "smog_index": 9.5,
      "coleman_liau": 10.2,
      "automated_readability": 9.1
    },
    "frequency": {
      "top_words": [
        {"word": "the", "count": 25},
        {"word": "and", "count": 18}
      ],
      "unique_words": 180
    }
  }
}
```

## 6.11 Text Transformation

### Slugify

```
GET /api/v1/text/slugify/{input}
GET /api/v1/text/slugify/{input}.txt
```

| Query Param | Type | Default | Description |
|-------------|------|---------|-------------|
| separator | string | - | Separator character |
| lowercase | bool | true | Convert to lowercase |
| max_length | int | 100 | Maximum slug length |

**Example**:
```
GET /api/v1/text/slugify/Hello%20World%20%26%20Goodbye
# Returns: hello-world-and-goodbye
```

### Reverse

```
GET /api/v1/text/reverse/{input}
GET /api/v1/text/reverse/words/{input}      # Reverse word order
GET /api/v1/text/reverse/lines/{input}      # Reverse line order
```

### Truncate

```
GET /api/v1/text/truncate/{length}/{input}
```

| Query Param | Type | Default | Description |
|-------------|------|---------|-------------|
| ellipsis | string | ... | Truncation indicator |
| word_boundary | bool | true | Break at word boundary |

### Wrap

```
GET /api/v1/text/wrap/{width}/{input}
```

| Parameter | Type | Default | Validation |
|-----------|------|---------|------------|
| width | path | 80 | 1-1000 |

## 6.12 Line Operations

```
POST /api/v1/text/lines/sort
POST /api/v1/text/lines/sort/reverse
POST /api/v1/text/lines/sort/natural        # Natural sort (file1, file2, file10)
POST /api/v1/text/lines/sort/length         # Sort by length
POST /api/v1/text/lines/unique              # Remove duplicates
POST /api/v1/text/lines/dedupe              # Remove consecutive duplicates
POST /api/v1/text/lines/reverse             # Reverse order
POST /api/v1/text/lines/shuffle             # Random order
POST /api/v1/text/lines/number              # Add line numbers
POST /api/v1/text/lines/filter              # Filter by regex
POST /api/v1/text/lines/prefix              # Add prefix
POST /api/v1/text/lines/suffix              # Add suffix
POST /api/v1/text/lines/trim                # Trim whitespace
POST /api/v1/text/lines/join                # Join lines
POST /api/v1/text/lines/split               # Split into lines
```

**Sort Request**:
```json
{
  "text": "banana\napple\ncherry",
  "case_sensitive": false,
  "numeric": false
}
```

**Number Request**:
```json
{
  "text": "line one\nline two",
  "start": 1,
  "format": "%d: %s",
  "skip_empty": true
}
```

## 6.13 Text Extraction

```
POST /api/v1/text/extract/emails
POST /api/v1/text/extract/urls
POST /api/v1/text/extract/phones
POST /api/v1/text/extract/ips
POST /api/v1/text/extract/ipv4
POST /api/v1/text/extract/ipv6
POST /api/v1/text/extract/dates
POST /api/v1/text/extract/times
POST /api/v1/text/extract/numbers
POST /api/v1/text/extract/hashtags
POST /api/v1/text/extract/mentions
POST /api/v1/text/extract/creditcards
POST /api/v1/text/extract/ssns
POST /api/v1/text/extract/macs
```

**Request**:
```json
{
  "text": "Contact us at hello@example.com or support@test.org",
  "unique": true
}
```

**Response**:
```json
{
  "success": true,
  "data": {
    "matches": ["hello@example.com", "support@test.org"],
    "count": 2,
    "positions": [
      {"match": "hello@example.com", "start": 14, "end": 31},
      {"match": "support@test.org", "start": 35, "end": 51}
    ]
  }
}
```

## 6.14 Text Strip/Clean

```
GET /api/v1/text/strip/html/{input}
GET /api/v1/text/strip/whitespace/{input}
GET /api/v1/text/strip/accents/{input}
GET /api/v1/text/strip/emoji/{input}
GET /api/v1/text/strip/punctuation/{input}
GET /api/v1/text/strip/numbers/{input}
GET /api/v1/text/strip/nonascii/{input}
GET /api/v1/text/strip/control/{input}
POST /api/v1/text/strip/{type}
```

**Response**:
```json
{
  "success": true,
  "data": {
    "original": "<p>Hello World!</p>",
    "cleaned": "Hello World!",
    "removed_count": 7
  }
}
```

## 6.15 Cipher/Fun Encoding

```
GET /api/v1/text/cipher/rot13/{input}
GET /api/v1/text/cipher/rot47/{input}
GET /api/v1/text/cipher/atbash/{input}
GET /api/v1/text/cipher/caesar/{shift}/{input}
GET /api/v1/text/cipher/vigenere/encode/{key}/{input}
GET /api/v1/text/cipher/vigenere/decode/{key}/{input}
GET /api/v1/text/morse/encode/{input}
GET /api/v1/text/morse/decode/{input}
GET /api/v1/text/binary/encode/{input}
GET /api/v1/text/binary/decode/{input}
GET /api/v1/text/braille/encode/{input}
GET /api/v1/text/braille/decode/{input}
GET /api/v1/text/nato/{input}
GET /api/v1/text/piglatin/{input}
```

| Cipher | Parameters |
|--------|------------|
| caesar | shift: 1-25, default 13 |
| vigenere | key: alphabetic string |

## 6.16 String Similarity

```
GET /api/v1/text/similarity/levenshtein/{str1}/{str2}
GET /api/v1/text/similarity/hamming/{str1}/{str2}
GET /api/v1/text/similarity/jaro/{str1}/{str2}
GET /api/v1/text/similarity/jarowinkler/{str1}/{str2}
GET /api/v1/text/similarity/dice/{str1}/{str2}
GET /api/v1/text/similarity/cosine/{str1}/{str2}
GET /api/v1/text/similarity/jaccard/{str1}/{str2}
POST /api/v1/text/similarity/compare
```

**Response**:
```json
{
  "success": true,
  "data": {
    "string1": "hello",
    "string2": "hallo",
    "algorithm": "levenshtein",
    "distance": 1,
    "similarity": 0.8,
    "operations": [
      {"type": "substitute", "position": 1, "from": "e", "to": "a"}
    ]
  }
}
```

## 6.17 Phonetic Encoding

```
GET /api/v1/text/phonetic/soundex/{input}
GET /api/v1/text/phonetic/metaphone/{input}
GET /api/v1/text/phonetic/doublemetaphone/{input}
GET /api/v1/text/phonetic/nysiis/{input}
GET /api/v1/text/phonetic/cologne/{input}
```

**Response**:
```json
{
  "success": true,
  "data": {
    "input": "Robert",
    "soundex": "R163",
    "metaphone": "RBRT",
    "double_metaphone": ["RPRT", "RPRT"]
  }
}
```

---

# SECTION 7: CRYPTOGRAPHY API

## 7.1 Password Hashing

### Bcrypt

```
GET /api/v1/crypto/bcrypt/{password}
GET /api/v1/crypto/bcrypt/{cost}/{password}
GET /api/v1/crypto/bcrypt/verify/{password}/{hash}
POST /api/v1/crypto/bcrypt
POST /api/v1/crypto/bcrypt/verify
GET /api/v1/crypto/bcrypt/benchmark
GET /api/v1/crypto/bcrypt/benchmark/{target_ms}
```

| Parameter | Type | Values | Default | Validation |
|-----------|------|--------|---------|------------|
| password | path | URL-encoded | - | Required, 1-72 bytes |
| cost | path | 4-31 | 12 | Integer |
| hash | path | URL-encoded bcrypt hash | - | Must be valid bcrypt |
| target_ms | path | 50-5000 | 250 | Target milliseconds |

**Examples**:
```
GET /api/v1/crypto/bcrypt/mypassword              # Cost 12 (default)
GET /api/v1/crypto/bcrypt/14/mypassword           # Cost 14
GET /api/v1/crypto/bcrypt/verify/mypassword/$2a$12$...
GET /api/v1/crypto/bcrypt/benchmark/500           # Find cost for 500ms
```

**Hash Response**:
```json
{
  "success": true,
  "data": {
    "algorithm": "bcrypt",
    "version": "2a",
    "hash": "$2a$12$LQv3c1yqBWVHxkd0LHAkCOYz6TtxMQJqhN8/X4D.E5CvOQWxLpHfm",
    "cost": 12,
    "salt": "LQv3c1yqBWVHxkd0LHAkCO",
    "execution_ms": 250
  }
}
```

**Verify Response**:
```json
{
  "success": true,
  "data": {
    "valid": true,
    "algorithm": "bcrypt",
    "cost": 12,
    "needs_rehash": false,
    "execution_ms": 245
  }
}
```

**Benchmark Response**:
```json
{
  "success": true,
  "data": {
    "recommended_cost": 13,
    "target_ms": 500,
    "actual_ms": 487,
    "benchmarks": [
      {"cost": 10, "ms": 62},
      {"cost": 11, "ms": 124},
      {"cost": 12, "ms": 248},
      {"cost": 13, "ms": 487},
      {"cost": 14, "ms": 975}
    ]
  }
}
```

### Argon2

```
GET /api/v1/crypto/argon2/{password}
GET /api/v1/crypto/argon2/{variant}/{password}
GET /api/v1/crypto/argon2/verify/{password}/{hash}
POST /api/v1/crypto/argon2
POST /api/v1/crypto/argon2/verify
GET /api/v1/crypto/argon2/benchmark
```

| Parameter | Type | Values | Default | Validation |
|-----------|------|--------|---------|------------|
| password | path | URL-encoded | - | Required, max 4GB |
| variant | path | argon2i, argon2d, argon2id | argon2id | Must be valid variant |

**Query Parameters**:

| Param | Type | Default | Validation |
|-------|------|---------|------------|
| memory | int | 65536 | 8-4194304 (KB) |
| iterations | int | 3 | 1-1000 |
| parallelism | int | 4 | 1-255 |
| key_length | int | 32 | 4-1024 |
| salt_length | int | 16 | 8-1024 |

**Examples**:
```
GET /api/v1/crypto/argon2/mypassword                          # argon2id, defaults
GET /api/v1/crypto/argon2/argon2id/mypassword?memory=131072   # 128MB memory
GET /api/v1/crypto/argon2/argon2i/mypassword?iterations=10
```

**Response**:
```json
{
  "success": true,
  "data": {
    "algorithm": "argon2id",
    "version": 19,
    "hash": "$argon2id$v=19$m=65536,t=3,p=4$c29tZXNhbHQ$RdescudvJCsgt3ub+b+dWRWJTmaaJObG",
    "params": {
      "memory_kb": 65536,
      "iterations": 3,
      "parallelism": 4,
      "key_length": 32
    },
    "salt_base64": "c29tZXNhbHQ",
    "raw_hash_base64": "RdescudvJCsgt3ub+b+dWRWJTmaaJObG",
    "execution_ms": 150
  }
}
```

### Scrypt

```
GET /api/v1/crypto/scrypt/{password}
GET /api/v1/crypto/scrypt/verify/{password}/{hash}
POST /api/v1/crypto/scrypt
POST /api/v1/crypto/scrypt/verify
```

| Query Param | Type | Default | Validation |
|-------------|------|---------|------------|
| n | int | 32768 | Power of 2, 2-2^20 |
| r | int | 8 | 1-255 |
| p | int | 1 | 1-255 |
| key_length | int | 32 | 16-1024 |

**Response**:
```json
{
  "success": true,
  "data": {
    "algorithm": "scrypt",
    "hash": "$scrypt$n=32768,r=8,p=1$c29tZXNhbHQ$...",
    "params": {
      "n": 32768,
      "r": 8,
      "p": 1,
      "key_length": 32
    },
    "salt_base64": "c29tZXNhbHQ",
    "execution_ms": 200
  }
}
```

### PBKDF2

```
GET /api/v1/crypto/pbkdf2/{password}
GET /api/v1/crypto/pbkdf2/{hash_function}/{password}
GET /api/v1/crypto/pbkdf2/verify/{password}/{hash}
POST /api/v1/crypto/pbkdf2
POST /api/v1/crypto/pbkdf2/verify
```

| Parameter | Type | Values | Default | Validation |
|-----------|------|--------|---------|------------|
| hash_function | path | sha1, sha256, sha384, sha512 | sha256 | Must be valid |

| Query Param | Type | Default | Validation |
|-------------|------|---------|------------|
| iterations | int | 600000 | 1000-10000000 |
| key_length | int | 32 | 16-1024 |

**Response**:
```json
{
  "success": true,
  "data": {
    "algorithm": "pbkdf2",
    "hash_function": "sha256",
    "hash": "$pbkdf2-sha256$i=600000$c29tZXNhbHQ$...",
    "iterations": 600000,
    "key_length": 32,
    "salt_base64": "c29tZXNhbHQ",
    "execution_ms": 180
  }
}
```

## 7.2 Symmetric Encryption

### AES

```
GET /api/v1/crypto/aes/encrypt/{key}/{plaintext}
GET /api/v1/crypto/aes/decrypt/{key}/{ciphertext}
GET /api/v1/crypto/aes/{mode}/encrypt/{key}/{plaintext}
GET /api/v1/crypto/aes/{mode}/decrypt/{key}/{nonce}/{ciphertext}
POST /api/v1/crypto/aes/encrypt
POST /api/v1/crypto/aes/decrypt
```

| Parameter | Type | Values | Default | Validation |
|-----------|------|--------|---------|------------|
| mode | path | gcm, cbc, ctr, cfb, ofb | gcm | Must be valid mode |
| key | path | base64 or hex encoded | - | 16, 24, or 32 bytes |
| plaintext | path | base64 encoded | - | Max 10MB |
| ciphertext | path | base64 encoded | - | Max 10MB |
| nonce | path | base64 encoded | - | Required for some modes |

**Key Size Detection**:

| Key Length (bytes) | AES Variant |
|--------------------|-------------|
| 16 | AES-128 |
| 24 | AES-192 |
| 32 | AES-256 |

**Query Parameters**:

| Param | Type | Default | Description |
|-------|------|---------|-------------|
| key_encoding | string | base64 | base64, hex, utf8 |
| output_encoding | string | base64 | base64, hex |
| aad | string | - | Additional authenticated data (GCM only) |

**Encrypt Response**:
```json
{
  "success": true,
  "data": {
    "algorithm": "aes-256-gcm",
    "ciphertext": "base64...",
    "nonce": "base64...",
    "tag": "base64...",
    "key_bits": 256,
    "nonce_bytes": 12,
    "tag_bytes": 16
  }
}
```

### ChaCha20-Poly1305

```
GET /api/v1/crypto/chacha20/encrypt/{key}/{plaintext}
GET /api/v1/crypto/chacha20/decrypt/{key}/{nonce}/{ciphertext}
GET /api/v1/crypto/xchacha20/encrypt/{key}/{plaintext}
GET /api/v1/crypto/xchacha20/decrypt/{key}/{nonce}/{ciphertext}
POST /api/v1/crypto/chacha20/encrypt
POST /api/v1/crypto/chacha20/decrypt
```

| Parameter | Validation |
|-----------|------------|
| key | Exactly 32 bytes |
| nonce | 12 bytes (ChaCha20) or 24 bytes (XChaCha20) |

### Other Symmetric Ciphers

```
GET /api/v1/crypto/3des/encrypt/{key}/{plaintext}
GET /api/v1/crypto/3des/decrypt/{key}/{iv}/{ciphertext}
GET /api/v1/crypto/blowfish/encrypt/{key}/{plaintext}
GET /api/v1/crypto/blowfish/decrypt/{key}/{iv}/{ciphertext}
GET /api/v1/crypto/twofish/encrypt/{key}/{plaintext}
GET /api/v1/crypto/twofish/decrypt/{key}/{iv}/{ciphertext}
GET /api/v1/crypto/camellia/encrypt/{key}/{plaintext}
GET /api/v1/crypto/camellia/decrypt/{key}/{iv}/{ciphertext}
```

## 7.3 Asymmetric Cryptography

### RSA Key Generation

```
GET /api/v1/crypto/rsa/generate
GET /api/v1/crypto/rsa/generate/{bits}
POST /api/v1/crypto/rsa/generate
```

| Parameter | Type | Values | Default | Validation |
|-----------|------|--------|---------|------------|
| bits | path | 2048, 3072, 4096 | 2048 | Must be valid size |

**Query Parameters**:

| Param | Type | Default | Description |
|-------|------|---------|-------------|
| format | string | pem | pem, der, jwk |
| public_exponent | int | 65537 | 3 or 65537 |

**Response**:
```json
{
  "success": true,
  "data": {
    "algorithm": "rsa",
    "key_size": 2048,
    "public_exponent": 65537,
    "private_key": "-----BEGIN RSA PRIVATE KEY-----\n...",
    "public_key": "-----BEGIN PUBLIC KEY-----\n...",
    "fingerprint": {
      "sha256": "SHA256:...",
      "md5": "MD5:..."
    },
    "jwk": {
      "kty": "RSA",
      "n": "...",
      "e": "AQAB"
    }
  }
}
```

### RSA Operations

```
POST /api/v1/crypto/rsa/encrypt
POST /api/v1/crypto/rsa/decrypt
POST /api/v1/crypto/rsa/sign
POST /api/v1/crypto/rsa/verify
POST /api/v1/crypto/rsa/info
```

**Encrypt Request**:
```json
{
  "public_key": "-----BEGIN PUBLIC KEY-----...",
  "plaintext": "Hello World",
  "padding": "oaep",
  "hash": "sha256"
}
```

| Padding | Description |
|---------|-------------|
| oaep | OAEP (recommended) |
| pkcs1v15 | PKCS#1 v1.5 |

**Sign Request**:
```json
{
  "private_key": "-----BEGIN RSA PRIVATE KEY-----...",
  "message": "Message to sign",
  "padding": "pss",
  "hash": "sha256"
}
```

| Sign Padding | Description |
|--------------|-------------|
| pss | PSS (recommended) |
| pkcs1v15 | PKCS#1 v1.5 |

### ECDSA

```
GET /api/v1/crypto/ecdsa/generate
GET /api/v1/crypto/ecdsa/generate/{curve}
POST /api/v1/crypto/ecdsa/generate
POST /api/v1/crypto/ecdsa/sign
POST /api/v1/crypto/ecdsa/verify
```

| Parameter | Type | Values | Default | Validation |
|-----------|------|--------|---------|------------|
| curve | path | p224, p256, p384, p521, secp256k1 | p256 | Must be valid curve |

**Curve Details**:

| Curve | Bits | Use Case |
|-------|------|----------|
| p224 | 224 | Legacy compatibility |
| p256 | 256 | General purpose (NIST) |
| p384 | 384 | Higher security |
| p521 | 521 | Maximum security |
| secp256k1 | 256 | Bitcoin/Ethereum |

### Ed25519

```
GET /api/v1/crypto/ed25519/generate
POST /api/v1/crypto/ed25519/generate
POST /api/v1/crypto/ed25519/sign
POST /api/v1/crypto/ed25519/verify
```

**Response**:
```json
{
  "success": true,
  "data": {
    "algorithm": "ed25519",
    "private_key": "-----BEGIN PRIVATE KEY-----...",
    "public_key": "-----BEGIN PUBLIC KEY-----...",
    "private_key_raw": "base64...",
    "public_key_raw": "base64..."
  }
}
```

### X25519 (Key Exchange)

```
GET /api/v1/crypto/x25519/generate
POST /api/v1/crypto/x25519/derive
```

**Derive Request**:
```json
{
  "private_key": "base64...",
  "public_key": "base64..."
}
```

**Derive Response**:
```json
{
  "success": true,
  "data": {
    "shared_secret": "base64...",
    "shared_secret_hex": "..."
  }
}
```

## 7.4 Key Derivation Functions

### HKDF

```
GET /api/v1/crypto/hkdf/{hash}/{key_material}/{length}
POST /api/v1/crypto/hkdf
POST /api/v1/crypto/hkdf/extract
POST /api/v1/crypto/hkdf/expand
```

| Parameter | Type | Values | Default | Validation |
|-----------|------|--------|---------|------------|
| hash | path | sha256, sha384, sha512 | sha256 | Must be valid |
| key_material | path | base64 encoded | - | Required |
| length | path | 1-1024 | 32 | Output length in bytes |

**Query Parameters**:

| Param | Type | Description |
|-------|------|-------------|
| salt | string | Optional salt (base64) |
| info | string | Optional context info (base64) |

### PBKDF2 (as KDF)

```
GET /api/v1/crypto/kdf/pbkdf2/{hash}/{password}/{length}
```

## 7.5 JWT (JSON Web Tokens)

### Generate JWT

```
POST /api/v1/crypto/jwt/generate
GET /api/v1/crypto/jwt/generate/{algorithm}/{secret}
```

**POST Request**:
```json
{
  "header": {
    "alg": "HS256",
    "typ": "JWT"
  },
  "payload": {
    "sub": "1234567890",
    "name": "John Doe",
    "iat": 1516239022,
    "exp": 1516325422
  },
  "secret": "your-secret-key",
  "private_key": "-----BEGIN RSA PRIVATE KEY-----..."
}
```

**Supported Algorithms**:

| Algorithm | Type | Key Requirement |
|-----------|------|-----------------|
| HS256, HS384, HS512 | HMAC | Shared secret |
| RS256, RS384, RS512 | RSA | RSA key pair |
| ES256, ES384, ES512 | ECDSA | EC key pair |
| PS256, PS384, PS512 | RSA-PSS | RSA key pair |
| EdDSA | EdDSA | Ed25519 key pair |

### Decode JWT (No Verification)

```
GET /api/v1/crypto/jwt/decode/{token}
POST /api/v1/crypto/jwt/decode
```

**Response**:
```json
{
  "success": true,
  "data": {
    "header": {
      "alg": "HS256",
      "typ": "JWT"
    },
    "payload": {
      "sub": "1234567890",
      "name": "John Doe",
      "iat": 1516239022,
      "exp": 1516325422
    },
    "signature": "base64...",
    "expired": false,
    "expires_at": "2024-01-15T10:30:00Z",
    "issued_at": "2024-01-14T10:30:00Z"
  }
}
```

### Verify JWT

```
POST /api/v1/crypto/jwt/verify
```

**Request**:
```json
{
  "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
  "secret": "your-secret-key",
  "public_key": "-----BEGIN PUBLIC KEY-----...",
  "verify_exp": true,
  "verify_nbf": true,
  "verify_iat": true,
  "audience": "expected-audience",
  "issuer": "expected-issuer"
}
```

**Response**:
```json
{
  "success": true,
  "data": {
    "valid": true,
    "payload": { },
    "claims_valid": {
      "exp": true,
      "nbf": true,
      "iat": true,
      "aud": true,
      "iss": true
    }
  }
}
```

## 7.6 TOTP/HOTP (One-Time Passwords)

### Generate TOTP Secret

```
GET /api/v1/crypto/totp/generate
GET /api/v1/crypto/totp/generate/{issuer}/{account}
POST /api/v1/crypto/totp/generate
```

| Parameter | Type | Default | Validation |
|-----------|------|---------|------------|
| issuer | path | CasTools | 1-64 chars, URL-safe |
| account | path | user | 1-64 chars |

**Query Parameters**:

| Param | Type | Default | Values |
|-------|------|---------|--------|
| algorithm | string | SHA1 | SHA1, SHA256, SHA512 |
| digits | int | 6 | 6, 8 |
| period | int | 30 | 15-300 seconds |
| secret_length | int | 20 | 16-64 bytes |

**Response**:
```json
{
  "success": true,
  "data": {
    "secret": "JBSWY3DPEHPK3PXP",
    "secret_hex": "48656c6c6f21",
    "secret_base64": "SGVsbG8h",
    "uri": "otpauth://totp/CasTools:user@example.com?secret=JBSWY3DPEHPK3PXP&issuer=CasTools&algorithm=SHA1&digits=6&period=30",
    "qr_code_svg": "<svg>...</svg>",
    "qr_code_png_base64": "data:image/png;base64,...",
    "params": {
      "algorithm": "SHA1",
      "digits": 6,
      "period": 30
    },
    "current_code": "123456",
    "current_code_valid_until": "2024-01-15T10:30:30Z",
    "current_code_remaining_seconds": 15
  }
}
```

### Generate Current TOTP Code

```
GET /api/v1/crypto/totp/code/{secret}
GET /api/v1/crypto/totp/code/{secret}.txt
```

**Response** (JSON):
```json
{
  "success": true,
  "data": {
    "code": "123456",
    "valid_until": "2024-01-15T10:30:30Z",
    "remaining_seconds": 15,
    "period": 30
  }
}
```

**Response** (TXT):
```
123456
```

### Verify TOTP Code

```
GET /api/v1/crypto/totp/verify/{secret}/{code}
POST /api/v1/crypto/totp/verify
```

| Query Param | Type | Default | Description |
|-------------|------|---------|-------------|
| window | int | 1 | Accept codes ±N periods |

**Response**:
```json
{
  "success": true,
  "data": {
    "valid": true,
    "used_window": 0,
    "server_code": "123456",
    "provided_code": "123456"
  }
}
```

### HOTP (Counter-Based)

```
GET /api/v1/crypto/hotp/generate/{issuer}/{account}
GET /api/v1/crypto/hotp/code/{secret}/{counter}
GET /api/v1/crypto/hotp/verify/{secret}/{code}/{counter}
```

### Recovery Codes

```
GET /api/v1/crypto/otp/recovery
GET /api/v1/crypto/otp/recovery/{count}
GET /api/v1/crypto/otp/recovery/{count}/{length}
```

| Parameter | Type | Default | Validation |
|-----------|------|---------|------------|
| count | path | 10 | 1-20 |
| length | path | 8 | 6-16 |

**Query Parameters**:

| Param | Type | Default | Description |
|-------|------|---------|-------------|
| format | string | groups | groups, plain, numeric |
| group_size | int | 4 | Characters per group |

**Response**:
```json
{
  "success": true,
  "data": {
    "codes": [
      "a4b7-c9d2",
      "e5f8-g1h3",
      "...etc"
    ],
    "hashes": [
      "sha256hash1...",
      "sha256hash2..."
    ],
    "format": "groups",
    "count": 10
  }
}
```

## 7.7 Password Generation

### Random Password

```
GET /api/v1/crypto/password
GET /api/v1/crypto/password/{length}
GET /api/v1/crypto/password/{length}/{count}
```

| Parameter | Type | Default | Validation |
|-----------|------|---------|------------|
| length | path | 16 | 4-256 |
| count | path | 1 | 1-100 |

**Query Parameters**:

| Param | Type | Default | Description |
|-------|------|---------|-------------|
| uppercase | bool | true | Include A-Z |
| lowercase | bool | true | Include a-z |
| numbers | bool | true | Include 0-9 |
| symbols | bool | true | Include !@#$%^&*... |
| exclude_similar | bool | false | Exclude 0O1lI |
| exclude_ambiguous | bool | false | Exclude {}[]()/\'"`~ |
| exclude | string | - | Custom characters to exclude |
| require_all | bool | true | Must contain all enabled types |

**Examples**:
```
GET /api/v1/crypto/password                                    # 16 chars, all types
GET /api/v1/crypto/password/32                                 # 32 chars
GET /api/v1/crypto/password/20/5                               # 5 passwords, 20 chars each
GET /api/v1/crypto/password/16?symbols=false                   # No symbols
GET /api/v1/crypto/password/16?exclude_similar=true            # No 0, O, 1, l, I
```

**Response**:
```json
{
  "success": true,
  "data": {
    "password": "xK9#mP2$vN7&qR4!",
    "length": 16,
    "entropy_bits": 105.2,
    "strength": "very_strong",
    "crack_time": {
      "online_throttled": "centuries",
      "online_unthrottled": "centuries",
      "offline_slow": "centuries",
      "offline_fast": "34 years"
    },
    "character_sets": {
      "uppercase": true,
      "lowercase": true,
      "numbers": true,
      "symbols": true
    }
  }
}
```

### Passphrase (Diceware)

```
GET /api/v1/crypto/passphrase
GET /api/v1/crypto/passphrase/{words}
GET /api/v1/crypto/passphrase/{words}/{count}
```

| Parameter | Type | Default | Validation |
|-----------|------|---------|------------|
| words | path | 6 | 3-20 |
| count | path | 1 | 1-100 |

**Query Parameters**:

| Param | Type | Default | Description |
|-------|------|---------|-------------|
| separator | string | - | Word separator |
| capitalize | bool | true | Capitalize first letter |
| include_number | bool | false | Add random number |
| wordlist | string | eff_large | eff_large, eff_short, bip39, diceware |

**Response**:
```json
{
  "success": true,
  "data": {
    "passphrase": "Correct-Horse-Battery-Staple-7-Widget",
    "words": ["Correct", "Horse", "Battery", "Staple", "7", "Widget"],
    "word_count": 6,
    "entropy_bits": 77.5,
    "strength": "strong",
    "crack_time": {
      "offline_fast": "3 centuries"
    }
  }
}
```

### PIN

```
GET /api/v1/crypto/pin
GET /api/v1/crypto/pin/{length}
GET /api/v1/crypto/pin/{length}/{count}
```

| Parameter | Type | Default | Validation |
|-----------|------|---------|------------|
| length | path | 4 | 3-12 |
| count | path | 1 | 1-100 |

## 7.8 Password Analysis

### Strength Check

```
GET /api/v1/crypto/password/strength/{password}
POST /api/v1/crypto/password/strength
```

**Response**:
```json
{
  "success": true,
  "data": {
    "score": 4,
    "strength": "very_strong",
    "entropy_bits": 105.2,
    "crack_time": {
      "online_throttled": "centuries",
      "online_unthrottled": "centuries",
      "offline_slow": "centuries",
      "offline_fast": "34 years"
    },
    "feedback": {
      "warning": "",
      "suggestions": []
    },
    "analysis": {
      "length": 16,
      "has_uppercase": true,
      "has_lowercase": true,
      "has_numbers": true,
      "has_symbols": true,
      "is_common": false,
      "is_sequential": false,
      "is_repeated": false,
      "dictionary_match": false
    }
  }
}
```

### Breach Check (k-anonymity)

```
GET /api/v1/crypto/password/breach/{password}
POST /api/v1/crypto/password/breach
```

**Note**: Uses k-anonymity - only first 5 chars of hash sent to API

**Response**:
```json
{
  "success": true,
  "data": {
    "breached": true,
    "breach_count": 3861493,
    "recommendation": "Do not use this password"
  }
}
```

## 7.9 Certificate Tools

### Decode X.509 Certificate

```
POST /api/v1/crypto/cert/decode
GET /api/v1/crypto/cert/decode/{pem_base64}
```

**Response**:
```json
{
  "success": true,
  "data": {
    "version": 3,
    "serial_number": "01:23:45:67:89:AB:CD:EF",
    "signature_algorithm": "SHA256-RSA",
    "issuer": {
      "common_name": "Example CA",
      "organization": "Example Inc",
      "country": "US"
    },
    "subject": {
      "common_name": "example.com",
      "organization": "Example Inc",
      "country": "US"
    },
    "validity": {
      "not_before": "2024-01-01T00:00:00Z",
      "not_after": "2025-01-01T00:00:00Z",
      "is_valid": true,
      "days_remaining": 180
    },
    "subject_alt_names": [
      "example.com",
      "www.example.com",
      "*.example.com"
    ],
    "public_key": {
      "algorithm": "RSA",
      "size": 2048
    },
    "fingerprints": {
      "sha256": "AA:BB:CC:...",
      "sha1": "DD:EE:FF:...",
      "md5": "11:22:33:..."
    },
    "extensions": {
      "basic_constraints": "CA:FALSE",
      "key_usage": ["Digital Signature", "Key Encipherment"],
      "extended_key_usage": ["Server Authentication", "Client Authentication"]
    },
    "is_ca": false,
    "is_self_signed": false
  }
}
```

### Generate Self-Signed Certificate

```
POST /api/v1/crypto/cert/generate
GET /api/v1/crypto/cert/generate/{common_name}
```

**Query Parameters**:

| Param | Type | Default | Description |
|-------|------|---------|-------------|
| days | int | 365 | Validity period |
| key_size | int | 2048 | RSA key size |
| key_type | string | rsa | rsa, ecdsa, ed25519 |
| curve | string | p256 | ECDSA curve |

**POST Request**:
```json
{
  "common_name": "example.com",
  "organization": "Example Inc",
  "country": "US",
  "state": "California",
  "locality": "San Francisco",
  "san": ["example.com", "www.example.com", "*.example.com"],
  "ip_san": ["192.168.1.1"],
  "days": 365,
  "key_type": "rsa",
  "key_size": 2048,
  "is_ca": false
}
```

### Generate CSR

```
POST /api/v1/crypto/cert/csr/generate
```

### SSH Key Generation

```
GET /api/v1/crypto/ssh/generate
GET /api/v1/crypto/ssh/generate/{type}
GET /api/v1/crypto/ssh/generate/{type}/{bits}
POST /api/v1/crypto/ssh/fingerprint
```

| Parameter | Type | Values | Default |
|-----------|------|--------|---------|
| type | path | rsa, ecdsa, ed25519 | ed25519 |
| bits | path | 2048, 3072, 4096 (RSA); 256, 384, 521 (ECDSA) | 4096/256 |

**Query Parameters**:

| Param | Type | Default | Description |
|-------|------|---------|-------------|
| comment | string | - | Key comment |
| passphrase | string | - | Encrypt private key |

**Response**:
```json
{
  "success": true,
  "data": {
    "type": "ed25519",
    "private_key": "-----BEGIN OPENSSH PRIVATE KEY-----\n...",
    "public_key": "ssh-ed25519 AAAA... comment",
    "fingerprint": {
      "sha256": "SHA256:...",
      "md5": "MD5:..."
    },
    "authorized_keys": "ssh-ed25519 AAAA... comment"
  }
}
```

## 7.10 Random Generation

```
GET /api/v1/crypto/random/bytes/{count}
GET /api/v1/crypto/random/hex/{count}
GET /api/v1/crypto/random/base64/{count}
GET /api/v1/crypto/random/int
GET /api/v1/crypto/random/int/{max}
GET /api/v1/crypto/random/int/{min}/{max}
GET /api/v1/crypto/random/float
GET /api/v1/crypto/random/float/{min}/{max}
GET /api/v1/crypto/random/bool
GET /api/v1/crypto/random/choice/{options}
```

| Endpoint | Parameters | Default | Validation |
|----------|------------|---------|------------|
| bytes | count: 1-10000 | 32 | Integer |
| hex | count: 1-10000 | 32 | Byte count |
| base64 | count: 1-10000 | 32 | Byte count |
| int | min, max | 0, 2^63-1 | min < max |
| float | min, max | 0.0, 1.0 | min < max |
| choice | comma-separated | - | 2-1000 options |

**Examples**:
```
GET /api/v1/crypto/random/bytes/16              # 16 random bytes (base64)
GET /api/v1/crypto/random/hex/32                # 64 hex characters
GET /api/v1/crypto/random/int/1/100             # Random int 1-100
GET /api/v1/crypto/random/choice/red,green,blue # Random selection
```

---

*[Document continues in Part 2 with Network Tools, DateTime, Weather, Geo, Math, Conversion, Generators, Validators, Parsers, Fun, Lorem, Dev Tools, Images, System, GraphQL, Web UI, Config, Build, Security]*
# CasTools Specification - Part 2

---

# SECTION 8: NETWORK TOOLS API

## 8.1 IP Address Operations

### Get Client IP

```
GET /api/v1/network/ip
GET /api/v1/network/ip.txt
```

**Response** (JSON):
```json
{
  "success": true,
  "data": {
    "ip": "203.0.113.42",
    "version": 4,
    "type": "public"
  }
}
```

**Response** (TXT):
```
203.0.113.42
```

### IP Information & Geolocation

```
GET /api/v1/network/ip/info
GET /api/v1/network/ip/{ip}/info
GET /api/v1/network/ip/{ip}
```

| Parameter | Type | Validation |
|-----------|------|------------|
| ip | path | Valid IPv4 or IPv6 |

**Examples**:
```
GET /api/v1/network/ip/info                      # Client IP info
GET /api/v1/network/ip/8.8.8.8/info              # Specific IP info
GET /api/v1/network/ip/8.8.8.8                   # Shorthand
GET /api/v1/network/ip/2001:4860:4860::8888      # IPv6
```

**Response**:
```json
{
  "success": true,
  "data": {
    "ip": "8.8.8.8",
    "version": 4,
    "type": "public",
    "hostname": "dns.google",
    "location": {
      "continent": "North America",
      "continent_code": "NA",
      "country": "United States",
      "country_code": "US",
      "region": "California",
      "region_code": "CA",
      "city": "Mountain View",
      "zip": "94035",
      "latitude": 37.386,
      "longitude": -122.0838,
      "timezone": "America/Los_Angeles",
      "timezone_offset": -28800,
      "currency": "USD"
    },
    "network": {
      "asn": 15169,
      "org": "GOOGLE",
      "isp": "Google LLC",
      "domain": "google.com"
    },
    "security": {
      "is_proxy": false,
      "is_vpn": false,
      "is_tor": false,
      "is_datacenter": true,
      "is_anonymous": false,
      "threat_level": "low"
    }
  }
}
```

### IP Validation & Classification

```
GET /api/v1/network/ip/validate/{ip}
GET /api/v1/network/ip/type/{ip}
GET /api/v1/network/ip/version/{ip}
```

**Type Response**:
```json
{
  "success": true,
  "data": {
    "ip": "192.168.1.1",
    "version": 4,
    "type": "private",
    "class": "C",
    "is_loopback": false,
    "is_private": true,
    "is_public": false,
    "is_multicast": false,
    "is_broadcast": false,
    "is_link_local": false,
    "is_reserved": false,
    "is_documentation": false,
    "rfc": "RFC 1918"
  }
}
```

### IP Conversion

```
GET /api/v1/network/ip/convert/{ip}
GET /api/v1/network/ip/to-int/{ip}
GET /api/v1/network/ip/from-int/{integer}
GET /api/v1/network/ip/to-binary/{ip}
GET /api/v1/network/ip/to-hex/{ip}
GET /api/v1/network/ip/expand/{ipv6}
GET /api/v1/network/ip/compress/{ipv6}
GET /api/v1/network/ip/reverse/{ip}
GET /api/v1/network/ip/arpa/{ip}
```

**Convert Response** (comprehensive):
```json
{
  "success": true,
  "data": {
    "ip": "192.168.1.1",
    "version": 4,
    "decimal": 3232235777,
    "hex": "0xC0A80101",
    "binary": "11000000.10101000.00000001.00000001",
    "octal": "030052000401",
    "reverse": "1.1.168.192",
    "arpa": "1.1.168.192.in-addr.arpa",
    "ipv6_mapped": "::ffff:192.168.1.1",
    "ipv6_compatible": "::192.168.1.1"
  }
}
```

**IPv6 Expand/Compress**:
```
GET /api/v1/network/ip/expand/2001:db8::1
# Returns: 2001:0db8:0000:0000:0000:0000:0000:0001

GET /api/v1/network/ip/compress/2001:0db8:0000:0000:0000:0000:0000:0001
# Returns: 2001:db8::1
```

## 8.2 CIDR & Subnet Calculator

### CIDR Information

```
GET /api/v1/network/cidr/{cidr}
GET /api/v1/network/cidr/info/{cidr}
```

| Parameter | Validation |
|-----------|------------|
| cidr | Valid CIDR notation (e.g., 192.168.1.0/24) |

**Response**:
```json
{
  "success": true,
  "data": {
    "cidr": "192.168.1.0/24",
    "network": "192.168.1.0",
    "broadcast": "192.168.1.255",
    "netmask": "255.255.255.0",
    "wildcard": "0.0.0.255",
    "prefix_length": 24,
    "first_host": "192.168.1.1",
    "last_host": "192.168.1.254",
    "total_addresses": 256,
    "usable_hosts": 254,
    "host_range": "192.168.1.1 - 192.168.1.254",
    "binary_netmask": "11111111.11111111.11111111.00000000",
    "class": "C",
    "is_private": true
  }
}
```

### CIDR Operations

```
GET /api/v1/network/cidr/contains/{cidr}/{ip}
GET /api/v1/network/cidr/expand/{cidr}
GET /api/v1/network/cidr/split/{cidr}/{new_prefix}
POST /api/v1/network/cidr/merge
POST /api/v1/network/cidr/exclude
```

**Contains**:
```
GET /api/v1/network/cidr/contains/192.168.1.0%2F24/192.168.1.100
# Returns: {"contains": true}
```

**Expand** (list all IPs - limited to /20 or smaller):
```
GET /api/v1/network/cidr/expand/192.168.1.0%2F30
```
```json
{
  "success": true,
  "data": {
    "cidr": "192.168.1.0/30",
    "addresses": [
      "192.168.1.0",
      "192.168.1.1",
      "192.168.1.2",
      "192.168.1.3"
    ],
    "count": 4
  }
}
```

**Split**:
```
GET /api/v1/network/cidr/split/192.168.0.0%2F24/26
```
```json
{
  "success": true,
  "data": {
    "original": "192.168.0.0/24",
    "subnets": [
      "192.168.0.0/26",
      "192.168.0.64/26",
      "192.168.0.128/26",
      "192.168.0.192/26"
    ],
    "count": 4
  }
}
```

### Subnet Calculator

```
GET /api/v1/network/subnet/mask/{prefix}
GET /api/v1/network/subnet/prefix/{mask}
GET /api/v1/network/subnet/wildcard/{mask}
GET /api/v1/network/subnet/hosts/{prefix}
POST /api/v1/network/subnet/calculate
POST /api/v1/network/vlsm/calculate
POST /api/v1/network/supernet/calculate
```

**Mask from Prefix**:
```
GET /api/v1/network/subnet/mask/24
# Returns: {"mask": "255.255.255.0", "prefix": 24}
```

**VLSM Calculator Request**:
```json
{
  "network": "192.168.1.0/24",
  "subnets": [
    {"name": "Sales", "hosts": 50},
    {"name": "IT", "hosts": 20},
    {"name": "Management", "hosts": 10}
  ]
}
```

## 8.3 DNS Lookup

### Basic DNS Lookup

```
GET /api/v1/network/dns/{domain}
GET /api/v1/network/dns/{type}/{domain}
GET /api/v1/network/dns/lookup/{domain}
```

| Parameter | Type | Values | Default |
|-----------|------|--------|---------|
| domain | path | Valid domain | Required |
| type | path | a, aaaa, mx, txt, ns, cname, soa, ptr, srv, caa, dnskey, ds, tlsa, naptr, any | a |

**Examples**:
```
GET /api/v1/network/dns/example.com              # A records (default)
GET /api/v1/network/dns/a/example.com            # A records (explicit)
GET /api/v1/network/dns/mx/example.com           # MX records
GET /api/v1/network/dns/txt/example.com          # TXT records
GET /api/v1/network/dns/any/example.com          # All record types
```

**Response** (A record):
```json
{
  "success": true,
  "data": {
    "domain": "example.com",
    "type": "A",
    "records": [
      {"value": "93.184.216.34", "ttl": 86400}
    ],
    "query_time_ms": 45,
    "nameserver": "8.8.8.8"
  }
}
```

**Response** (MX record):
```json
{
  "success": true,
  "data": {
    "domain": "example.com",
    "type": "MX",
    "records": [
      {"priority": 10, "value": "mail.example.com", "ttl": 3600},
      {"priority": 20, "value": "mail2.example.com", "ttl": 3600}
    ]
  }
}
```

### Specialized DNS Lookups

```
GET /api/v1/network/dns/reverse/{ip}
GET /api/v1/network/dns/ptr/{ip}
GET /api/v1/network/dns/dmarc/{domain}
GET /api/v1/network/dns/spf/{domain}
GET /api/v1/network/dns/dkim/{domain}/{selector}
GET /api/v1/network/dns/bimi/{domain}
GET /api/v1/network/dns/mta-sts/{domain}
GET /api/v1/network/dns/trace/{domain}
GET /api/v1/network/dns/propagation/{domain}
```

**Reverse DNS**:
```
GET /api/v1/network/dns/reverse/8.8.8.8
# Returns: {"hostname": "dns.google"}
```

**DMARC**:
```
GET /api/v1/network/dns/dmarc/example.com
```
```json
{
  "success": true,
  "data": {
    "domain": "example.com",
    "dmarc_domain": "_dmarc.example.com",
    "raw": "v=DMARC1; p=reject; rua=mailto:dmarc@example.com",
    "parsed": {
      "version": "DMARC1",
      "policy": "reject",
      "subdomain_policy": null,
      "percentage": 100,
      "rua": ["mailto:dmarc@example.com"],
      "ruf": [],
      "adkim": "relaxed",
      "aspf": "relaxed"
    },
    "valid": true
  }
}
```

**SPF**:
```
GET /api/v1/network/dns/spf/example.com
```
```json
{
  "success": true,
  "data": {
    "domain": "example.com",
    "raw": "v=spf1 include:_spf.google.com ~all",
    "parsed": {
      "version": "spf1",
      "mechanisms": [
        {"qualifier": "+", "type": "include", "value": "_spf.google.com"},
        {"qualifier": "~", "type": "all", "value": null}
      ],
      "all_qualifier": "~",
      "lookup_count": 2
    },
    "valid": true,
    "warnings": []
  }
}
```

## 8.4 WHOIS Lookup

```
GET /api/v1/network/whois/{query}
GET /api/v1/network/whois/domain/{domain}
GET /api/v1/network/whois/ip/{ip}
GET /api/v1/network/whois/asn/{asn}
```

**Domain WHOIS Response**:
```json
{
  "success": true,
  "data": {
    "domain": "example.com",
    "registrar": {
      "name": "RESERVED-Internet Assigned Numbers Authority",
      "url": "http://www.iana.org",
      "abuse_email": "abuse@iana.org",
      "abuse_phone": "+1-310-301-5800"
    },
    "dates": {
      "created": "1995-08-14T04:00:00Z",
      "updated": "2023-08-14T07:01:38Z",
      "expires": "2024-08-13T04:00:00Z",
      "age_days": 10410
    },
    "status": [
      "clientDeleteProhibited",
      "clientTransferProhibited",
      "clientUpdateProhibited"
    ],
    "nameservers": [
      "a.iana-servers.net",
      "b.iana-servers.net"
    ],
    "dnssec": true,
    "raw": "..."
  }
}
```

## 8.5 SSL/TLS Certificate Checker

```
GET /api/v1/network/ssl/{host}
GET /api/v1/network/ssl/check/{host}
GET /api/v1/network/ssl/check/{host}/{port}
GET /api/v1/network/ssl/chain/{host}
GET /api/v1/network/ssl/ciphers/{host}
GET /api/v1/network/ssl/protocols/{host}
```

| Parameter | Type | Default | Validation |
|-----------|------|---------|------------|
| host | path | Required | Valid hostname |
| port | path | 443 | 1-65535 |

**Response**:
```json
{
  "success": true,
  "data": {
    "host": "example.com",
    "port": 443,
    "valid": true,
    "certificate": {
      "subject": {
        "common_name": "example.com",
        "organization": "Example Inc"
      },
      "issuer": {
        "common_name": "DigiCert TLS RSA SHA256 2020 CA1",
        "organization": "DigiCert Inc"
      },
      "validity": {
        "not_before": "2024-01-01T00:00:00Z",
        "not_after": "2025-01-01T00:00:00Z",
        "days_remaining": 180,
        "is_expired": false,
        "is_not_yet_valid": false
      },
      "san": [
        "example.com",
        "www.example.com"
      ],
      "fingerprints": {
        "sha256": "AA:BB:CC:...",
        "sha1": "DD:EE:FF:..."
      },
      "public_key": {
        "algorithm": "RSA",
        "size": 2048
      },
      "signature_algorithm": "SHA256-RSA",
      "serial_number": "0A:1B:2C:3D:..."
    },
    "chain": [
      {"subject": "example.com", "issuer": "DigiCert TLS RSA SHA256 2020 CA1"},
      {"subject": "DigiCert TLS RSA SHA256 2020 CA1", "issuer": "DigiCert Global Root CA"}
    ],
    "chain_valid": true,
    "protocol": "TLSv1.3",
    "cipher": {
      "name": "TLS_AES_256_GCM_SHA384",
      "bits": 256
    },
    "hsts": {
      "enabled": true,
      "max_age": 31536000,
      "include_subdomains": true,
      "preload": true
    },
    "ocsp": {
      "status": "good",
      "produced_at": "2024-01-15T10:00:00Z",
      "next_update": "2024-01-22T10:00:00Z"
    },
    "ct": {
      "scts_count": 2,
      "logs": ["Google Argon2024", "Cloudflare Nimbus2024"]
    },
    "grade": "A+"
  }
}
```

## 8.6 MAC Address Tools

```
GET /api/v1/network/mac/info/{mac}
GET /api/v1/network/mac/lookup/{mac}
GET /api/v1/network/mac/vendor/{mac}
GET /api/v1/network/mac/generate
GET /api/v1/network/mac/generate/{vendor}
GET /api/v1/network/mac/validate/{mac}
GET /api/v1/network/mac/format/{mac}/{format}
GET /api/v1/network/oui/search/{query}
```

| Parameter | Validation |
|-----------|------------|
| mac | Valid MAC (any format) |
| format | colon, hyphen, dot, bare |
| vendor | OUI prefix or vendor name |

**MAC Formats Accepted**:
```
00:1A:2B:3C:4D:5E
00-1A-2B-3C-4D-5E
001A.2B3C.4D5E
001A2B3C4D5E
```

**Info Response**:
```json
{
  "success": true,
  "data": {
    "mac": "00:1A:2B:3C:4D:5E",
    "normalized": "001A2B3C4D5E",
    "formats": {
      "colon": "00:1A:2B:3C:4D:5E",
      "hyphen": "00-1A-2B-3C-4D-5E",
      "dot": "001A.2B3C.4D5E",
      "bare": "001A2B3C4D5E"
    },
    "oui": "00:1A:2B",
    "vendor": {
      "name": "Ayecom Technology Co., Ltd.",
      "address": "Taiwan",
      "country": "TW"
    },
    "type": "unicast",
    "is_multicast": false,
    "is_broadcast": false,
    "is_local": false,
    "is_universal": true
  }
}
```

**Generate Random MAC**:
```
GET /api/v1/network/mac/generate                 # Random MAC
GET /api/v1/network/mac/generate?local=true      # Locally administered
GET /api/v1/network/mac/generate?multicast=true  # Multicast
GET /api/v1/network/mac/generate/apple           # Apple vendor prefix
```

## 8.7 Port Information

```
GET /api/v1/network/port/{port}
GET /api/v1/network/port/info/{port}
GET /api/v1/network/port/search/{query}
GET /api/v1/network/port/range/{start}/{end}
GET /api/v1/network/port/common
GET /api/v1/network/port/random
GET /api/v1/network/port/random/{start}/{end}
```

| Parameter | Type | Validation |
|-----------|------|------------|
| port | path | 1-65535 |
| query | path | Service name to search |

**Port Info Response**:
```json
{
  "success": true,
  "data": {
    "port": 443,
    "protocol": "tcp",
    "service": "https",
    "description": "HTTP Secure (HTTPS)",
    "assignee": "IETF",
    "status": "official",
    "alternatives": [
      {"port": 8443, "description": "Alternative HTTPS"}
    ],
    "security": {
      "encrypted": true,
      "well_known": true,
      "privileged": true
    }
  }
}
```

**Common Ports**:
```
GET /api/v1/network/port/common
GET /api/v1/network/port/common?category=web
GET /api/v1/network/port/common?category=database
GET /api/v1/network/port/common?category=mail
```

## 8.8 URL Operations

```
GET /api/v1/network/url/parse/{url}
POST /api/v1/network/url/parse
POST /api/v1/network/url/build
GET /api/v1/network/url/encode/{text}
GET /api/v1/network/url/decode/{text}
GET /api/v1/network/url/validate/{url}
GET /api/v1/network/url/normalize/{url}
POST /api/v1/network/url/shorten
GET /api/v1/network/url/expand/{code}
```

**Parse Response**:
```json
{
  "success": true,
  "data": {
    "url": "https://user:pass@example.com:8080/path/to/page?query=value&foo=bar#section",
    "valid": true,
    "components": {
      "scheme": "https",
      "username": "user",
      "password": "pass",
      "host": "example.com",
      "port": 8080,
      "path": "/path/to/page",
      "query": "query=value&foo=bar",
      "fragment": "section"
    },
    "query_params": {
      "query": "value",
      "foo": "bar"
    },
    "path_segments": ["path", "to", "page"],
    "origin": "https://example.com:8080",
    "href": "https://user:pass@example.com:8080/path/to/page?query=value&foo=bar#section",
    "tld": "com",
    "domain": "example.com",
    "subdomain": null,
    "is_ip": false,
    "is_localhost": false,
    "is_secure": true
  }
}
```

**URL Shortener** (in-memory, ephemeral):
```
POST /api/v1/network/url/shorten
{"url": "https://example.com/very/long/path"}
```
```json
{
  "success": true,
  "data": {
    "original": "https://example.com/very/long/path",
    "code": "abc123",
    "short_url": "https://tools.example.com/s/abc123",
    "expires_at": "2024-01-16T10:30:00Z"
  }
}
```

## 8.9 User Agent Operations

```
GET /api/v1/network/useragent
GET /api/v1/network/useragent/parse/{ua}
POST /api/v1/network/useragent/parse
GET /api/v1/network/useragent/generate
GET /api/v1/network/useragent/generate/{type}
GET /api/v1/network/useragent/list
GET /api/v1/network/useragent/list/{type}
```

| Parameter | Values |
|-----------|--------|
| type | chrome, firefox, safari, edge, opera, mobile, bot, random |

**Parse Response**:
```json
{
  "success": true,
  "data": {
    "raw": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
    "browser": {
      "name": "Chrome",
      "version": "120.0.0.0",
      "major": 120,
      "engine": "Blink",
      "engine_version": "120.0.0.0"
    },
    "os": {
      "name": "Windows",
      "version": "10",
      "platform": "Win64"
    },
    "device": {
      "type": "desktop",
      "vendor": null,
      "model": null
    },
    "is_bot": false,
    "is_mobile": false,
    "is_tablet": false,
    "is_desktop": true
  }
}
```

## 8.10 HTTP Status Codes

```
GET /api/v1/network/http/status
GET /api/v1/network/http/status/{code}
GET /api/v1/network/http/status/category/{category}
```

| Parameter | Values |
|-----------|--------|
| code | 100-599 |
| category | 1xx, 2xx, 3xx, 4xx, 5xx, informational, success, redirection, client_error, server_error |

**Response**:
```json
{
  "success": true,
  "data": {
    "code": 404,
    "message": "Not Found",
    "category": "Client Error",
    "description": "The server cannot find the requested resource.",
    "spec": "RFC 7231",
    "mdn_url": "https://developer.mozilla.org/en-US/docs/Web/HTTP/Status/404",
    "is_error": true,
    "is_success": false,
    "is_redirect": false
  }
}
```

## 8.11 ASN Lookup

```
GET /api/v1/network/asn/{asn}
GET /api/v1/network/asn/lookup/{ip}
GET /api/v1/network/asn/{asn}/prefixes
GET /api/v1/network/asn/{asn}/peers
```

**Response**:
```json
{
  "success": true,
  "data": {
    "asn": 15169,
    "name": "GOOGLE",
    "description": "Google LLC",
    "country": "US",
    "registry": "arin",
    "allocated": "2000-03-30",
    "prefixes_v4": 500,
    "prefixes_v6": 200,
    "peers_count": 150,
    "website": "https://www.google.com"
  }
}
```

## 8.12 Request Echo (HTTP Bin)

```
GET /api/v1/network/echo
POST /api/v1/network/echo
ANY /api/v1/network/echo/anything
GET /api/v1/network/echo/headers
GET /api/v1/network/echo/ip
GET /api/v1/network/echo/method
GET /api/v1/network/echo/status/{code}
GET /api/v1/network/echo/delay/{ms}
GET /api/v1/network/echo/redirect/{count}
GET /api/v1/network/echo/cookies
GET /api/v1/network/echo/cookies/set/{name}/{value}
GET /api/v1/network/echo/basic-auth/{user}/{pass}
GET /api/v1/network/echo/bearer
GET /api/v1/network/echo/cache/{seconds}
GET /api/v1/network/echo/gzip
GET /api/v1/network/echo/deflate
GET /api/v1/network/echo/brotli
GET /api/v1/network/echo/bytes/{n}
GET /api/v1/network/echo/stream/{n}
GET /api/v1/network/echo/drip
GET /api/v1/network/echo/range/{n}
POST /api/v1/network/echo/form
POST /api/v1/network/echo/multipart
```

**Echo Response**:
```json
{
  "success": true,
  "data": {
    "method": "GET",
    "url": "https://tools.example.com/api/v1/network/echo?foo=bar",
    "headers": {
      "Accept": "*/*",
      "User-Agent": "curl/7.64.1",
      "X-Forwarded-For": "203.0.113.42"
    },
    "query": {
      "foo": "bar"
    },
    "body": null,
    "form": null,
    "files": null,
    "json": null,
    "origin": "203.0.113.42",
    "timestamp": "2024-01-15T10:30:00Z"
  }
}
```

---

# SECTION 9: DATE & TIME API

## 9.1 Current Time

```
GET /api/v1/datetime/now
GET /api/v1/datetime/now.txt
GET /api/v1/datetime/now/{timezone}
GET /api/v1/datetime/timestamp
GET /api/v1/datetime/timestamp.txt
GET /api/v1/datetime/timestamp/ms
GET /api/v1/datetime/timestamp/ns
```

**Response**:
```json
{
  "success": true,
  "data": {
    "unix": 1705312200,
    "unix_ms": 1705312200000,
    "unix_ns": 1705312200000000000,
    "iso8601": "2024-01-15T10:30:00Z",
    "rfc2822": "Mon, 15 Jan 2024 10:30:00 +0000",
    "rfc3339": "2024-01-15T10:30:00Z",
    "rfc3339_nano": "2024-01-15T10:30:00.000000000Z",
    "http": "Mon, 15 Jan 2024 10:30:00 GMT",
    "human": "Monday, January 15, 2024 at 10:30:00 AM UTC",
    "relative": "now",
    "timezone": "UTC",
    "offset": "+00:00",
    "offset_seconds": 0,
    "is_dst": false,
    "day_of_week": 1,
    "day_of_week_name": "Monday",
    "day_of_year": 15,
    "week_number": 3,
    "quarter": 1,
    "is_leap_year": true
  }
}
```

## 9.2 Timestamp Conversion

```
GET /api/v1/datetime/convert/{timestamp}
GET /api/v1/datetime/convert/{timestamp}/{timezone}
GET /api/v1/datetime/from-unix/{timestamp}
GET /api/v1/datetime/to-unix/{datetime}
POST /api/v1/datetime/parse
POST /api/v1/datetime/format
```

| Parameter | Validation |
|-----------|------------|
| timestamp | Unix seconds (10 digits), milliseconds (13 digits), or nanoseconds (19 digits) |
| timezone | Valid IANA timezone or offset |
| datetime | ISO 8601 or common formats |

**Examples**:
```
GET /api/v1/datetime/convert/1705312200
GET /api/v1/datetime/convert/1705312200/America%2FNew_York
GET /api/v1/datetime/convert/1705312200000              # Milliseconds
GET /api/v1/datetime/to-unix/2024-01-15T10:30:00Z
```

**Parse Request** (for complex formats):
```json
{
  "datetime": "January 15, 2024 10:30 AM",
  "format": "auto",
  "timezone": "America/New_York"
}
```

**Format Request**:
```json
{
  "timestamp": 1705312200,
  "format": "YYYY-MM-DD HH:mm:ss",
  "timezone": "Europe/London"
}
```

## 9.3 Date Arithmetic

```
GET /api/v1/datetime/add/{timestamp}/{duration}
GET /api/v1/datetime/subtract/{timestamp}/{duration}
GET /api/v1/datetime/diff/{timestamp1}/{timestamp2}
POST /api/v1/datetime/add
POST /api/v1/datetime/diff
```

**Duration Format**:
```
1h          # 1 hour
30m         # 30 minutes
1d          # 1 day
2w          # 2 weeks
3M          # 3 months
1y          # 1 year
1d12h30m    # 1 day, 12 hours, 30 minutes
P1DT12H30M  # ISO 8601 duration
```

**Examples**:
```
GET /api/v1/datetime/add/1705312200/1d               # Add 1 day
GET /api/v1/datetime/add/1705312200/2w3d             # Add 2 weeks 3 days
GET /api/v1/datetime/subtract/1705312200/1M          # Subtract 1 month
GET /api/v1/datetime/diff/1705312200/1705398600      # Difference
```

**Diff Response**:
```json
{
  "success": true,
  "data": {
    "from": "2024-01-15T10:30:00Z",
    "to": "2024-01-16T10:30:00Z",
    "difference": {
      "years": 0,
      "months": 0,
      "weeks": 0,
      "days": 1,
      "hours": 24,
      "minutes": 1440,
      "seconds": 86400,
      "milliseconds": 86400000,
      "human": "1 day"
    }
  }
}
```

## 9.4 Timezone Operations

```
GET /api/v1/datetime/timezone/list
GET /api/v1/datetime/timezone/info/{timezone}
GET /api/v1/datetime/timezone/convert/{timestamp}/{from}/{to}
GET /api/v1/datetime/timezone/offset/{timezone}
GET /api/v1/datetime/timezone/abbreviations
```

**List Response** (abbreviated):
```json
{
  "success": true,
  "data": {
    "timezones": [
      {"name": "America/New_York", "offset": "-05:00", "abbreviation": "EST"},
      {"name": "America/Los_Angeles", "offset": "-08:00", "abbreviation": "PST"},
      {"name": "Europe/London", "offset": "+00:00", "abbreviation": "GMT"},
      {"name": "Asia/Tokyo", "offset": "+09:00", "abbreviation": "JST"}
    ],
    "count": 400
  }
}
```

**Timezone Info**:
```json
{
  "success": true,
  "data": {
    "name": "America/New_York",
    "abbreviation": "EST",
    "offset": "-05:00",
    "offset_seconds": -18000,
    "is_dst": false,
    "dst_start": "2024-03-10T02:00:00",
    "dst_end": "2024-11-03T02:00:00",
    "current_time": "2024-01-15T05:30:00-05:00"
  }
}
```

## 9.5 Cron Expression Parser

```
GET /api/v1/datetime/cron/parse/{expression}
GET /api/v1/datetime/cron/next/{expression}
GET /api/v1/datetime/cron/next/{expression}/{count}
GET /api/v1/datetime/cron/previous/{expression}/{count}
GET /api/v1/datetime/cron/between/{expression}/{start}/{end}
GET /api/v1/datetime/cron/validate/{expression}
POST /api/v1/datetime/cron/generate
```

**Expression Format**: URL-encoded, space-separated (use %20 or +)

```
GET /api/v1/datetime/cron/parse/0+0+*+*+*              # Every day at midnight
GET /api/v1/datetime/cron/parse/*/5+*+*+*+*            # Every 5 minutes
GET /api/v1/datetime/cron/next/0+9+*+*+1-5/5           # Next 5 occurrences
```

**Parse Response**:
```json
{
  "success": true,
  "data": {
    "expression": "0 0 * * *",
    "valid": true,
    "description": "At 00:00 (midnight) every day",
    "fields": {
      "minute": {"value": "0", "description": "At minute 0"},
      "hour": {"value": "0", "description": "At hour 0 (midnight)"},
      "day_of_month": {"value": "*", "description": "Every day"},
      "month": {"value": "*", "description": "Every month"},
      "day_of_week": {"value": "*", "description": "Every day of week"}
    },
    "next_occurrences": [
      "2024-01-16T00:00:00Z",
      "2024-01-17T00:00:00Z",
      "2024-01-18T00:00:00Z"
    ]
  }
}
```

**Generate Request**:
```json
{
  "minute": 0,
  "hour": 9,
  "day_of_month": "*",
  "month": "*",
  "day_of_week": "1-5"
}
```

## 9.6 Calendar & Date Information

```
GET /api/v1/datetime/calendar/{year}/{month}
GET /api/v1/datetime/week/{date}
GET /api/v1/datetime/weekday/{date}
GET /api/v1/datetime/quarter/{date}
GET /api/v1/datetime/leapyear/{year}
GET /api/v1/datetime/days-in-month/{year}/{month}
GET /api/v1/datetime/age/{birthdate}
GET /api/v1/datetime/countdown/{target}
```

**Calendar Response**:
```json
{
  "success": true,
  "data": {
    "year": 2024,
    "month": 1,
    "month_name": "January",
    "days_in_month": 31,
    "first_day_of_week": 1,
    "weeks": [
      [null, 1, 2, 3, 4, 5, 6],
      [7, 8, 9, 10, 11, 12, 13],
      [14, 15, 16, 17, 18, 19, 20],
      [21, 22, 23, 24, 25, 26, 27],
      [28, 29, 30, 31, null, null, null]
    ],
    "is_leap_year": true
  }
}
```

**Age Response**:
```json
{
  "success": true,
  "data": {
    "birthdate": "1990-05-15",
    "age": {
      "years": 33,
      "months": 8,
      "days": 0
    },
    "total_days": 12298,
    "next_birthday": "2024-05-15",
    "days_until_birthday": 121,
    "zodiac": "Taurus",
    "chinese_zodiac": "Horse",
    "generation": "Millennial"
  }
}
```

## 9.7 Sunrise/Sunset & Astronomy

```
GET /api/v1/datetime/sun/{lat}/{lon}
GET /api/v1/datetime/sun/{lat}/{lon}/{date}
GET /api/v1/datetime/sunrise/{lat}/{lon}
GET /api/v1/datetime/sunset/{lat}/{lon}
GET /api/v1/datetime/golden-hour/{lat}/{lon}
GET /api/v1/datetime/blue-hour/{lat}/{lon}
GET /api/v1/datetime/twilight/{lat}/{lon}
GET /api/v1/datetime/day-length/{lat}/{lon}
GET /api/v1/datetime/moon/{date}
GET /api/v1/datetime/moon/phase/{date}
GET /api/v1/datetime/moonrise/{lat}/{lon}
GET /api/v1/datetime/moonset/{lat}/{lon}
```

**Sun Response**:
```json
{
  "success": true,
  "data": {
    "location": {"latitude": 40.7128, "longitude": -74.0060},
    "date": "2024-01-15",
    "timezone": "America/New_York",
    "sunrise": "07:14:00",
    "sunset": "16:55:00",
    "solar_noon": "12:04:30",
    "day_length": "09:41:00",
    "civil_twilight": {
      "dawn": "06:45:00",
      "dusk": "17:24:00"
    },
    "nautical_twilight": {
      "dawn": "06:12:00",
      "dusk": "17:57:00"
    },
    "astronomical_twilight": {
      "dawn": "05:39:00",
      "dusk": "18:30:00"
    },
    "golden_hour": {
      "morning": {"start": "07:14:00", "end": "07:50:00"},
      "evening": {"start": "16:19:00", "end": "16:55:00"}
    },
    "blue_hour": {
      "morning": {"start": "06:45:00", "end": "07:14:00"},
      "evening": {"start": "16:55:00", "end": "17:24:00"}
    }
  }
}
```

**Moon Response**:
```json
{
  "success": true,
  "data": {
    "date": "2024-01-15",
    "phase": {
      "name": "Waxing Crescent",
      "illumination": 0.25,
      "age_days": 4.5,
      "emoji": "🌒"
    },
    "next_phases": {
      "new_moon": "2024-01-11",
      "first_quarter": "2024-01-18",
      "full_moon": "2024-01-25",
      "last_quarter": "2024-02-02"
    }
  }
}
```

## 9.8 Holidays

```
GET /api/v1/datetime/holidays/{country}/{year}
GET /api/v1/datetime/holidays/{country}
GET /api/v1/datetime/holidays/next/{country}
GET /api/v1/datetime/holidays/is-holiday/{country}/{date}
```

| Parameter | Validation |
|-----------|------------|
| country | ISO 3166-1 alpha-2 (US, GB, DE, etc.) |
| year | 1900-2100 |

**Response**:
```json
{
  "success": true,
  "data": {
    "country": "US",
    "year": 2024,
    "holidays": [
      {"date": "2024-01-01", "name": "New Year's Day", "type": "national"},
      {"date": "2024-01-15", "name": "Martin Luther King Jr. Day", "type": "national"},
      {"date": "2024-02-19", "name": "Presidents' Day", "type": "national"}
    ],
    "count": 11
  }
}
```

## 9.9 Business Days

```
GET /api/v1/datetime/workdays/add/{date}/{days}
GET /api/v1/datetime/workdays/add/{date}/{days}/{country}
GET /api/v1/datetime/workdays/between/{start}/{end}
GET /api/v1/datetime/workdays/is-workday/{date}
GET /api/v1/datetime/workdays/is-workday/{date}/{country}
```

**Response**:
```json
{
  "success": true,
  "data": {
    "start": "2024-01-15",
    "business_days": 10,
    "result": "2024-01-29",
    "skipped_weekends": 4,
    "skipped_holidays": 1,
    "country": "US"
  }
}
```

---

# SECTION 10: WEATHER API

**Data Source**: Open-Meteo (free, no API key)

## 10.1 Current Weather

```
GET /api/v1/weather/current
GET /api/v1/weather/current/{lat}/{lon}
GET /api/v1/weather/current/city/{city}
GET /api/v1/weather/{lat}/{lon}
```

| Parameter | Type | Validation |
|-----------|------|------------|
| lat | path | -90 to 90 |
| lon | path | -180 to 180 |
| city | path | URL-encoded city name |

**No parameters**: Uses client IP geolocation

**Query Parameters**:

| Param | Type | Default | Values |
|-------|------|---------|--------|
| units | string | metric | metric, imperial, kelvin |

**Response**:
```json
{
  "success": true,
  "data": {
    "location": {
      "city": "New York",
      "region": "New York",
      "country": "United States",
      "country_code": "US",
      "latitude": 40.7128,
      "longitude": -74.0060,
      "timezone": "America/New_York",
      "elevation_m": 10
    },
    "current": {
      "time": "2024-01-15T10:30:00-05:00",
      "temperature": {
        "value": 5.2,
        "unit": "°C",
        "feels_like": 2.1
      },
      "humidity": 65,
      "dew_point": -1.2,
      "pressure": {
        "value": 1013.25,
        "unit": "hPa",
        "trend": "steady"
      },
      "wind": {
        "speed": 15.5,
        "unit": "km/h",
        "direction": 315,
        "direction_cardinal": "NW",
        "gust": 25.0
      },
      "precipitation": {
        "value": 0,
        "unit": "mm",
        "probability": 10
      },
      "cloud_cover": 25,
      "visibility": {
        "value": 10,
        "unit": "km"
      },
      "uv_index": 2,
      "condition": {
        "code": 2,
        "text": "Partly Cloudy",
        "icon": "partly-cloudy-day",
        "emoji": "⛅"
      }
    },
    "units": "metric"
  }
}
```

## 10.2 Forecast

```
GET /api/v1/weather/forecast/{lat}/{lon}
GET /api/v1/weather/forecast/{lat}/{lon}/{days}
GET /api/v1/weather/forecast/city/{city}
GET /api/v1/weather/forecast/city/{city}/{days}
GET /api/v1/weather/hourly/{lat}/{lon}
GET /api/v1/weather/hourly/{lat}/{lon}/{hours}
GET /api/v1/weather/daily/{lat}/{lon}
GET /api/v1/weather/daily/{lat}/{lon}/{days}
```

| Parameter | Type | Default | Validation |
|-----------|------|---------|------------|
| days | path | 7 | 1-16 |
| hours | path | 24 | 1-168 |

**Daily Forecast Response**:
```json
{
  "success": true,
  "data": {
    "location": { },
    "daily": [
      {
        "date": "2024-01-15",
        "temperature": {
          "min": -2,
          "max": 8,
          "unit": "°C"
        },
        "precipitation": {
          "total": 0,
          "probability": 10,
          "unit": "mm"
        },
        "wind": {
          "max_speed": 20,
          "unit": "km/h"
        },
        "uv_index_max": 3,
        "condition": {
          "text": "Partly Cloudy",
          "icon": "partly-cloudy-day",
          "emoji": "⛅"
        },
        "sunrise": "07:14",
        "sunset": "16:55"
      }
    ],
    "units": "metric"
  }
}
```

## 10.3 Air Quality

```
GET /api/v1/weather/air-quality/{lat}/{lon}
GET /api/v1/weather/aqi/{lat}/{lon}
```

**Response**:
```json
{
  "success": true,
  "data": {
    "location": { },
    "air_quality": {
      "index": 42,
      "category": "Good",
      "color": "#00E400",
      "dominant_pollutant": "pm2_5",
      "pollutants": {
        "pm2_5": {"value": 8.5, "unit": "µg/m³"},
        "pm10": {"value": 15.2, "unit": "µg/m³"},
        "o3": {"value": 35, "unit": "µg/m³"},
        "no2": {"value": 12, "unit": "µg/m³"},
        "so2": {"value": 5, "unit": "µg/m³"},
        "co": {"value": 200, "unit": "µg/m³"}
      },
      "health_recommendation": "Air quality is satisfactory, and air pollution poses little or no risk."
    }
  }
}
```

## 10.4 UV Index

```
GET /api/v1/weather/uv/{lat}/{lon}
GET /api/v1/weather/uv/{lat}/{lon}/{date}
```

**Response**:
```json
{
  "success": true,
  "data": {
    "location": { },
    "uv": {
      "index": 3,
      "category": "Moderate",
      "color": "#FFFF00",
      "max_index": 5,
      "max_time": "12:30",
      "safe_exposure_time": {
        "skin_type_1": 30,
        "skin_type_2": 40,
        "skin_type_3": 50,
        "skin_type_4": 70,
        "skin_type_5": 90,
        "skin_type_6": 120
      },
      "recommendation": "Wear sunscreen and sunglasses."
    }
  }
}
```

## 10.5 Historical Weather

```
GET /api/v1/weather/history/{lat}/{lon}/{date}
GET /api/v1/weather/history/{lat}/{lon}/{start_date}/{end_date}
```

| Parameter | Validation |
|-----------|------------|
| date | YYYY-MM-DD, up to 2 years back |
| start_date, end_date | Max 30 day range |

---

# SECTION 11: GEOLOCATION API

**Data Sources**: ip-api.com, Open-Meteo Geocoding (free, no key)

## 11.1 IP Geolocation

```
GET /api/v1/geo/ip
GET /api/v1/geo/ip/{ip}
```

See Network section 8.1 for detailed response.

## 11.2 Geocoding (Address to Coordinates)

```
GET /api/v1/geo/search/{query}
GET /api/v1/geo/geocode/{query}
```

| Parameter | Type | Validation |
|-----------|------|------------|
| query | path | URL-encoded address, max 200 chars |

**Query Parameters**:

| Param | Type | Default | Description |
|-------|------|---------|-------------|
| limit | int | 5 | Max results 1-10 |
| country | string | - | Filter by country code |
| language | string | en | Response language |

**Response**:
```json
{
  "success": true,
  "data": {
    "query": "1600 Pennsylvania Avenue, Washington DC",
    "results": [
      {
        "name": "The White House",
        "display_name": "The White House, 1600 Pennsylvania Avenue NW, Washington, DC 20500, USA",
        "latitude": 38.8977,
        "longitude": -77.0365,
        "type": "building",
        "importance": 0.95,
        "address": {
          "house_number": "1600",
          "road": "Pennsylvania Avenue NW",
          "city": "Washington",
          "state": "District of Columbia",
          "postcode": "20500",
          "country": "United States",
          "country_code": "US"
        },
        "bounding_box": {
          "min_lat": 38.8970,
          "max_lat": 38.8984,
          "min_lon": -77.0378,
          "max_lon": -77.0352
        }
      }
    ],
    "count": 1
  }
}
```

## 11.3 Reverse Geocoding (Coordinates to Address)

```
GET /api/v1/geo/reverse/{lat}/{lon}
GET /api/v1/geo/address/{lat}/{lon}
```

**Response**:
```json
{
  "success": true,
  "data": {
    "latitude": 40.7128,
    "longitude": -74.0060,
    "display_name": "New York City Hall, 260 Broadway, New York, NY 10007, USA",
    "address": {
      "building": "New York City Hall",
      "house_number": "260",
      "road": "Broadway",
      "neighbourhood": "Civic Center",
      "city": "New York",
      "county": "New York County",
      "state": "New York",
      "postcode": "10007",
      "country": "United States",
      "country_code": "US"
    }
  }
}
```

## 11.4 Distance Calculation

```
GET /api/v1/geo/distance/{lat1}/{lon1}/{lat2}/{lon2}
POST /api/v1/geo/distance
```

**Query Parameters**:

| Param | Type | Default | Values |
|-------|------|---------|--------|
| unit | string | km | km, mi, m, ft, nm |
| formula | string | haversine | haversine, vincenty |

**Response**:
```json
{
  "success": true,
  "data": {
    "from": {"latitude": 40.7128, "longitude": -74.0060, "name": "New York"},
    "to": {"latitude": 34.0522, "longitude": -118.2437, "name": "Los Angeles"},
    "distance": {
      "kilometers": 3935.75,
      "miles": 2445.55,
      "meters": 3935750,
      "feet": 12911909,
      "nautical_miles": 2125.13
    },
    "bearing": {
      "initial": 273.4,
      "final": 256.8,
      "cardinal": "W"
    },
    "formula": "haversine"
  }
}
```

## 11.5 Bearing & Direction

```
GET /api/v1/geo/bearing/{lat1}/{lon1}/{lat2}/{lon2}
GET /api/v1/geo/midpoint/{lat1}/{lon1}/{lat2}/{lon2}
GET /api/v1/geo/destination/{lat}/{lon}/{bearing}/{distance}
```

**Destination Parameters**:

| Parameter | Type | Description |
|-----------|------|-------------|
| bearing | path | Degrees 0-360 |
| distance | path | Distance in km (default) |

**Query Param**: `unit=km|mi|m|nm`

## 11.6 Geohash Operations

```
GET /api/v1/geo/geohash/encode/{lat}/{lon}
GET /api/v1/geo/geohash/encode/{lat}/{lon}/{precision}
GET /api/v1/geo/geohash/decode/{geohash}
GET /api/v1/geo/geohash/neighbors/{geohash}
GET /api/v1/geo/geohash/bounds/{geohash}
```

| Parameter | Type | Default | Validation |
|-----------|------|---------|------------|
| precision | path | 9 | 1-12 |
| geohash | path | - | Valid geohash string |

**Encode Response**:
```json
{
  "success": true,
  "data": {
    "latitude": 40.7128,
    "longitude": -74.0060,
    "geohash": "dr5ru6j2c",
    "precision": 9,
    "bounds": {
      "min_lat": 40.71278,
      "max_lat": 40.71282,
      "min_lon": -74.00604,
      "max_lon": -74.00600
    },
    "neighbors": {
      "n": "dr5ru6j2f",
      "ne": "dr5ru6j2g",
      "e": "dr5ru6j2d",
      "se": "dr5ru6j29",
      "s": "dr5ru6j28",
      "sw": "dr5ru6j22",
      "w": "dr5ru6j23",
      "nw": "dr5ru6j26"
    }
  }
}
```

## 11.7 H3 Index Operations

```
GET /api/v1/geo/h3/encode/{lat}/{lon}
GET /api/v1/geo/h3/encode/{lat}/{lon}/{resolution}
GET /api/v1/geo/h3/decode/{h3index}
GET /api/v1/geo/h3/neighbors/{h3index}
GET /api/v1/geo/h3/distance/{h3index1}/{h3index2}
```

| Parameter | Type | Default | Validation |
|-----------|------|---------|------------|
| resolution | path | 9 | 0-15 |

## 11.8 Country Information

```
GET /api/v1/geo/countries
GET /api/v1/geo/country/{code}
GET /api/v1/geo/country/{code}/states
GET /api/v1/geo/country/{code}/cities
GET /api/v1/geo/country/name/{name}
```

| Parameter | Validation |
|-----------|------------|
| code | ISO 3166-1 alpha-2 or alpha-3 |
| name | Country name (partial match) |

**Country Response**:
```json
{
  "success": true,
  "data": {
    "name": "United States",
    "official_name": "United States of America",
    "alpha2": "US",
    "alpha3": "USA",
    "numeric": "840",
    "capital": "Washington, D.C.",
    "region": "Americas",
    "subregion": "Northern America",
    "population": 331002651,
    "area_km2": 9833520,
    "languages": [{"code": "en", "name": "English"}],
    "currencies": [{"code": "USD", "name": "US Dollar", "symbol": "$"}],
    "calling_code": "+1",
    "tld": ".us",
    "timezones": ["America/New_York", "America/Chicago", "America/Denver", "America/Los_Angeles"],
    "flag": {
      "emoji": "🇺🇸",
      "svg": "https://flagcdn.com/us.svg",
      "png": "https://flagcdn.com/w320/us.png"
    },
    "coordinates": {"latitude": 37.0902, "longitude": -95.7129},
    "borders": ["CAN", "MEX"]
  }
}
```

## 11.9 Coordinates Validation

```
GET /api/v1/geo/validate/{lat}/{lon}
GET /api/v1/geo/validate/lat/{lat}
GET /api/v1/geo/validate/lon/{lon}
POST /api/v1/geo/contains
```

**Contains Request** (point in polygon):
```json
{
  "point": {"lat": 40.7128, "lon": -74.0060},
  "polygon": [
    {"lat": 40.70, "lon": -74.02},
    {"lat": 40.72, "lon": -74.02},
    {"lat": 40.72, "lon": -74.00},
    {"lat": 40.70, "lon": -74.00}
  ]
}
```

---

# SECTION 12: MATH & NUMBERS API

## 12.1 Expression Evaluation

```
GET /api/v1/math/eval/{expression}
GET /api/v1/math/calculate/{expression}
POST /api/v1/math/eval
```

| Parameter | Validation |
|-----------|------------|
| expression | URL-encoded math expression, max 1000 chars |

**Supported Operations**:
```
+, -, *, /, ^ (power), % (modulo)
sqrt(), cbrt(), abs(), floor(), ceil(), round()
sin(), cos(), tan(), asin(), acos(), atan()
sinh(), cosh(), tanh()
log(), log10(), log2(), ln()
exp(), pow()
min(), max(), avg()
factorial(), gcd(), lcm()
pi, e, phi (golden ratio)
```

**Examples**:
```
GET /api/v1/math/eval/2+2                        # 4
GET /api/v1/math/eval/sqrt(16)                   # 4
GET /api/v1/math/eval/sin(pi%2F2)                # 1
GET /api/v1/math/eval/2%5E10                     # 1024 (2^10)
GET /api/v1/math/eval/factorial(10)              # 3628800
```

**Response**:
```json
{
  "success": true,
  "data": {
    "expression": "2 + 2 * 3",
    "result": 8,
    "result_formatted": "8",
    "steps": [
      {"operation": "2 * 3", "result": 6},
      {"operation": "2 + 6", "result": 8}
    ]
  }
}
```

## 12.2 Random Numbers

```
GET /api/v1/math/random
GET /api/v1/math/random/{max}
GET /api/v1/math/random/{min}/{max}
GET /api/v1/math/random/{min}/{max}/{count}
GET /api/v1/math/random/float
GET /api/v1/math/random/float/{min}/{max}
GET /api/v1/math/random/gaussian
GET /api/v1/math/random/gaussian/{mean}/{stddev}
```

| Parameter | Type | Default | Validation |
|-----------|------|---------|------------|
| min | path | 0 | Integer |
| max | path | 100 | Integer, > min |
| count | path | 1 | 1-10000 |
| mean | path | 0 | Float |
| stddev | path | 1 | Float > 0 |

**Query Parameters**:

| Param | Type | Default | Description |
|-------|------|---------|-------------|
| unique | bool | false | No duplicates |
| sorted | bool | false | Sort results |

**Response**:
```json
{
  "success": true,
  "data": {
    "min": 1,
    "max": 100,
    "count": 5,
    "numbers": [42, 17, 89, 3, 56],
    "sum": 207,
    "average": 41.4
  }
}
```

## 12.3 Prime Numbers

```
GET /api/v1/math/prime/check/{n}
GET /api/v1/math/prime/is/{n}
GET /api/v1/math/prime/list/{start}/{end}
GET /api/v1/math/prime/first/{count}
GET /api/v1/math/prime/nth/{n}
GET /api/v1/math/prime/next/{n}
GET /api/v1/math/prime/previous/{n}
GET /api/v1/math/prime/factorize/{n}
GET /api/v1/math/prime/factors/{n}
GET /api/v1/math/prime/count/{n}
GET /api/v1/math/prime/between/{start}/{end}
```

| Parameter | Type | Validation |
|-----------|------|------------|
| n | path | Positive integer, max 10^15 |
| start, end | path | Positive integers, max range 1000000 |
| count | path | 1-100000 |

**Check Response**:
```json
{
  "success": true,
  "data": {
    "number": 17,
    "is_prime": true,
    "factors": [17],
    "divisors": [1, 17],
    "next_prime": 19,
    "previous_prime": 13
  }
}
```

**Factorize Response**:
```json
{
  "success": true,
  "data": {
    "number": 84,
    "is_prime": false,
    "prime_factors": [2, 2, 3, 7],
    "prime_factorization": "2² × 3 × 7",
    "unique_factors": [2, 3, 7],
    "factor_pairs": [[1, 84], [2, 42], [3, 28], [4, 21], [6, 14], [7, 12]],
    "divisors": [1, 2, 3, 4, 6, 7, 12, 14, 21, 28, 42, 84],
    "divisor_count": 12,
    "divisor_sum": 224
  }
}
```

## 12.4 Sequences

```
GET /api/v1/math/fibonacci/{n}
GET /api/v1/math/fibonacci/sequence/{count}
GET /api/v1/math/fibonacci/check/{n}
GET /api/v1/math/factorial/{n}
GET /api/v1/math/factorial/double/{n}
GET /api/v1/math/triangular/{n}
GET /api/v1/math/triangular/sequence/{count}
GET /api/v1/math/square/{n}
GET /api/v1/math/cube/{n}
GET /api/v1/math/catalan/{n}
GET /api/v1/math/lucas/{n}
GET /api/v1/math/perfect/{n}
GET /api/v1/math/perfect/check/{n}
```

| Parameter | Type | Validation |
|-----------|------|------------|
| n | path | Non-negative integer |
| count | path | 1-1000 |

**Fibonacci Response**:
```json
{
  "success": true,
  "data": {
    "n": 10,
    "value": 55,
    "sequence": [0, 1, 1, 2, 3, 5, 8, 13, 21, 34, 55],
    "golden_ratio_approximation": 1.618033988749895
  }
}
```

## 12.5 Number Theory

```
GET /api/v1/math/gcd/{a}/{b}
GET /api/v1/math/lcm/{a}/{b}
GET /api/v1/math/gcd/extended/{a}/{b}
GET /api/v1/math/modular/inverse/{a}/{m}
GET /api/v1/math/modular/exp/{base}/{exp}/{mod}
GET /api/v1/math/totient/{n}
GET /api/v1/math/mobius/{n}
```

**GCD Response**:
```json
{
  "success": true,
  "data": {
    "a": 48,
    "b": 18,
    "gcd": 6,
    "lcm": 144,
    "coprime": false,
    "bezout": {
      "x": -1,
      "y": 3,
      "equation": "48 × (-1) + 18 × 3 = 6"
    }
  }
}
```

## 12.6 Statistics

```
POST /api/v1/math/stats
GET /api/v1/math/stats/mean/{numbers}
GET /api/v1/math/stats/median/{numbers}
GET /api/v1/math/stats/mode/{numbers}
GET /api/v1/math/stats/variance/{numbers}
GET /api/v1/math/stats/stddev/{numbers}
GET /api/v1/math/stats/range/{numbers}
GET /api/v1/math/stats/quartiles/{numbers}
GET /api/v1/math/stats/percentile/{numbers}/{p}
GET /api/v1/math/stats/zscore/{value}/{mean}/{stddev}
POST /api/v1/math/stats/correlation
POST /api/v1/math/stats/regression
```

| Parameter | Format |
|-----------|--------|
| numbers | Comma-separated (e.g., 1,2,3,4,5) |

**Full Stats Response**:
```json
{
  "success": true,
  "data": {
    "count": 10,
    "sum": 55,
    "mean": 5.5,
    "median": 5.5,
    "mode": null,
    "variance": 8.25,
    "stddev": 2.872,
    "stderr": 0.908,
    "min": 1,
    "max": 10,
    "range": 9,
    "quartiles": {
      "q1": 3,
      "q2": 5.5,
      "q3": 8
    },
    "iqr": 5,
    "skewness": 0,
    "kurtosis": -1.2,
    "coefficient_of_variation": 0.522,
    "geometric_mean": 4.529,
    "harmonic_mean": 3.414
  }
}
```

## 12.7 Base Conversion

```
GET /api/v1/math/base/convert/{value}/{from}/{to}
GET /api/v1/math/binary/{decimal}
GET /api/v1/math/binary/to-decimal/{binary}
GET /api/v1/math/hex/{decimal}
GET /api/v1/math/hex/to-decimal/{hex}
GET /api/v1/math/octal/{decimal}
GET /api/v1/math/octal/to-decimal/{octal}
```

| Parameter | Type | Validation |
|-----------|------|------------|
| from, to | path | 2-36 |
| value | path | Valid in source base |

**Response**:
```json
{
  "success": true,
  "data": {
    "input": "255",
    "from_base": 10,
    "to_base": 16,
    "result": "FF",
    "all_bases": {
      "binary": "11111111",
      "octal": "377",
      "decimal": "255",
      "hex": "FF"
    }
  }
}
```

## 12.8 Roman Numerals

```
GET /api/v1/math/roman/encode/{number}
GET /api/v1/math/roman/to-roman/{number}
GET /api/v1/math/roman/decode/{roman}
GET /api/v1/math/roman/to-decimal/{roman}
GET /api/v1/math/roman/validate/{roman}
```

| Parameter | Validation |
|-----------|------------|
| number | 1-3999 |
| roman | Valid Roman numeral |

**Response**:
```json
{
  "success": true,
  "data": {
    "decimal": 1984,
    "roman": "MCMLXXXIV",
    "breakdown": [
      {"symbol": "M", "value": 1000},
      {"symbol": "CM", "value": 900},
      {"symbol": "LXXX", "value": 80},
      {"symbol": "IV", "value": 4}
    ]
  }
}
```

## 12.9 Trigonometry

```
GET /api/v1/math/trig/sin/{angle}
GET /api/v1/math/trig/cos/{angle}
GET /api/v1/math/trig/tan/{angle}
GET /api/v1/math/trig/asin/{value}
GET /api/v1/math/trig/acos/{value}
GET /api/v1/math/trig/atan/{value}
GET /api/v1/math/trig/atan2/{y}/{x}
GET /api/v1/math/trig/sinh/{value}
GET /api/v1/math/trig/cosh/{value}
GET /api/v1/math/trig/tanh/{value}
GET /api/v1/math/trig/all/{angle}
```

**Query Parameters**:

| Param | Type | Default | Values |
|-------|------|---------|--------|
| unit | string | radians | radians, degrees |

**All Trig Response**:
```json
{
  "success": true,
  "data": {
    "angle": 45,
    "unit": "degrees",
    "radians": 0.7853981633974483,
    "sin": 0.7071067811865476,
    "cos": 0.7071067811865476,
    "tan": 1,
    "cot": 1,
    "sec": 1.4142135623730951,
    "csc": 1.4142135623730951
  }
}
```

## 12.10 Combinatorics

```
GET /api/v1/math/combinations/{n}/{r}
GET /api/v1/math/permutations/{n}/{r}
GET /api/v1/math/binomial/{n}/{k}
GET /api/v1/math/multinomial/{n}/{...k}
GET /api/v1/math/derangements/{n}
GET /api/v1/math/stirling/{n}/{k}
GET /api/v1/math/bell/{n}
GET /api/v1/math/partition/{n}
```

**Response**:
```json
{
  "success": true,
  "data": {
    "n": 10,
    "r": 3,
    "combinations": 120,
    "permutations": 720,
    "formula_c": "10! / (3! × 7!)",
    "formula_p": "10! / 7!"
  }
}
```

## 12.11 Mathematical Constants

```
GET /api/v1/math/constants
GET /api/v1/math/constant/pi
GET /api/v1/math/constant/pi/{digits}
GET /api/v1/math/constant/e
GET /api/v1/math/constant/e/{digits}
GET /api/v1/math/constant/phi
GET /api/v1/math/constant/sqrt2
GET /api/v1/math/constant/sqrt3
GET /api/v1/math/constant/ln2
GET /api/v1/math/constant/ln10
```

| Parameter | Type | Default | Validation |
|-----------|------|---------|------------|
| digits | path | 50 | 1-10000 |

**Response**:
```json
{
  "success": true,
  "data": {
    "name": "pi",
    "symbol": "π",
    "value": "3.14159265358979323846264338327950288419716939937510",
    "digits": 50,
    "description": "Ratio of circumference to diameter of a circle",
    "continued_fraction": [3, 7, 15, 1, 292, 1, 1, 1, 2, 1]
  }
}
```

## 12.12 Equations

```
POST /api/v1/math/quadratic
GET /api/v1/math/quadratic/{a}/{b}/{c}
POST /api/v1/math/cubic
POST /api/v1/math/polynomial
POST /api/v1/math/linear-system
```

**Quadratic Response** (ax² + bx + c = 0):
```json
{
  "success": true,
  "data": {
    "equation": "x² - 5x + 6 = 0",
    "a": 1,
    "b": -5,
    "c": 6,
    "discriminant": 1,
    "discriminant_type": "positive",
    "roots": {
      "x1": 3,
      "x2": 2,
      "type": "real_distinct"
    },
    "vertex": {"x": 2.5, "y": -0.25},
    "axis_of_symmetry": 2.5,
    "factored_form": "(x - 3)(x - 2)"
  }
}
```

---

*[Document continues in Part 3 with Unit Conversion, Generators, Validators, Parsers, Fun, Lorem, Dev Tools, Images, System, GraphQL Schema, Web UI, Configuration, Embedded Data, Project Structure, Build & Deployment, Security, Performance]*

## 22.7 Converters

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/dev/base/convert` | POST | Number base |
| `/dev/ieee754/convert` | POST | IEEE 754 float |
| `/dev/endian/swap` | POST | Endianness swap |
| `/dev/bit/manipulate` | POST | Bit operations |

## 22.8 Linters

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/dev/lint/json` | POST | JSON lint |
| `/dev/lint/yaml` | POST | YAML lint |
| `/dev/lint/xml` | POST | XML lint |
| `/dev/lint/html` | POST | HTML lint |
| `/dev/lint/css` | POST | CSS lint |
| `/dev/lint/sql` | POST | SQL lint |
| `/dev/lint/dockerfile` | POST | Dockerfile lint |
| `/dev/lint/shell` | POST | Shell lint |
| `/dev/lint/markdown` | POST | Markdown lint |

---

# 23. IMAGES

**Base Path:** `/api/v1/image`  
**Endpoints:** 68

## 23.1 Generation

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/image/placeholder` | GET | Placeholder image |
| `/image/solid` | GET | Solid color |
| `/image/gradient` | GET | Gradient |
| `/image/noise` | GET | Noise texture |
| `/image/checkerboard` | GET | Checkerboard |
| `/image/stripes` | GET | Stripes |
| `/image/grid` | GET | Grid pattern |
| `/image/dots` | GET | Dot pattern |
| `/image/text` | GET | Text image |

**Placeholder Parameters:**
| Parameter | Type | Default |
|-----------|------|---------|
| `width` | integer | `300` |
| `height` | integer | `200` |
| `bg` | string | `#cccccc` |
| `fg` | string | `#333333` |
| `text` | string | `{w}x{h}` |
| `format` | string | `png` |

## 23.2 Codes

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/image/qrcode` | POST | QR code |
| `/image/barcode` | POST | Barcode |
| `/image/datamatrix` | POST | Data Matrix |
| `/image/aztec` | POST | Aztec code |
| `/image/pdf417` | POST | PDF417 |

## 23.3 Avatars

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/image/avatar` | GET | Random avatar |
| `/image/identicon` | GET | Identicon |
| `/image/pixel-art` | GET | Pixel art avatar |
| `/image/initials` | GET | Initials avatar |
| `/image/gravatar` | GET | Gravatar |
| `/image/robohash` | GET | Robohash |
| `/image/dicebear` | GET | DiceBear avatar |

## 23.4 Social

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/image/favicon` | POST | Favicon generator |
| `/image/og` | POST | Open Graph image |
| `/image/twitter-card` | POST | Twitter card |
| `/image/social-card` | POST | Social card |

## 23.5 Badges

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/image/badge` | GET | Shield badge |
| `/image/badge/flat` | GET | Flat style |
| `/image/badge/plastic` | GET | Plastic style |

## 23.6 Charts

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/image/chart/sparkline` | POST | Sparkline |
| `/image/chart/pie` | POST | Pie chart |
| `/image/chart/bar` | POST | Bar chart |
| `/image/chart/line` | POST | Line chart |
| `/image/chart/donut` | POST | Donut chart |
| `/image/chart/gauge` | POST | Gauge |
| `/image/chart/progress` | POST | Progress bar |

## 23.7 Icons

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/image/icon` | GET | Icon SVG |
| `/image/emoji` | GET | Emoji image |
| `/image/flag/{country}` | GET | Country flag |

## 23.8 Manipulation

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/image/resize` | POST | Resize |
| `/image/crop` | POST | Crop |
| `/image/rotate` | POST | Rotate |
| `/image/flip` | POST | Flip |
| `/image/mirror` | POST | Mirror |

## 23.9 Filters

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/image/filter/grayscale` | POST | Grayscale |
| `/image/filter/sepia` | POST | Sepia |
| `/image/filter/invert` | POST | Invert |
| `/image/filter/blur` | POST | Blur |
| `/image/filter/sharpen` | POST | Sharpen |
| `/image/filter/brightness` | POST | Brightness |
| `/image/filter/contrast` | POST | Contrast |
| `/image/filter/saturation` | POST | Saturation |

## 23.10 Composition

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/image/overlay` | POST | Overlay images |
| `/image/watermark` | POST | Add watermark |
| `/image/border` | POST | Add border |
| `/image/rounded` | POST | Rounded corners |
| `/image/circle` | POST | Circle crop |

## 23.11 Utilities

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/image/thumbnail` | POST | Generate thumbnail |
| `/image/metadata` | POST | Image metadata |
| `/image/exif` | POST | EXIF data |
| `/image/hash` | POST | Image hash |
| `/image/phash` | POST | Perceptual hash |
| `/image/compare` | POST | Compare images |
| `/image/compress` | POST | Compress |
| `/image/optimize` | POST | Optimize |

## 23.12 Format Conversion

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/image/convert` | POST | Convert format |
| `/image/svg-to-png` | POST | SVG to PNG |
| `/image/to-webp` | POST | To WebP |
| `/image/to-avif` | POST | To AVIF |
| `/image/to-base64` | POST | To Base64 |
| `/image/from-base64` | POST | From Base64 |
| `/image/svg/optimize` | POST | Optimize SVG |
| `/image/sprite` | POST | Create sprite |
| `/image/concat` | POST | Concatenate |
| `/image/gif` | POST | Create GIF |

---

# 24. HEALTH & SYSTEM

**Base Path:** `/api/v1/system` and root  
**Endpoints:** 18

## 24.1 Health Checks

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/health` | GET | Overall health |
| `/health/live` | GET | Liveness probe |
| `/health/ready` | GET | Readiness probe |
| `/health/startup` | GET | Startup probe |

**Health Response:**
```json
{
  "status": "healthy",
  "version": "1.0.0",
  "uptime": "72h15m30s",
  "uptime_seconds": 260130,
  "timestamp": "2024-01-15T10:30:00Z",
  "checks": {
    "memory": {"status": "healthy", "allocated_mb": 45},
    "goroutines": {"status": "healthy", "count": 12},
    "cache": {"status": "healthy", "items": 1234}
  }
}
```

## 24.2 Metrics

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/metrics` | GET | Prometheus metrics |
| `/system/metrics` | GET | JSON metrics |

## 24.3 Info

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/system/info` | GET | System info |
| `/system/version` | GET | Version info |
| `/system/uptime` | GET | Uptime |
| `/system/stats` | GET | API statistics |
| `/system/endpoints` | GET | All endpoints |
| `/system/endpoints/count` | GET | Endpoint count |

## 24.4 Documentation

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/swagger/doc.json` | GET | OpenAPI JSON |
| `/swagger/doc.yaml` | GET | OpenAPI YAML |
| `/graphql/schema` | GET | GraphQL schema |
| `/system/changelog` | GET | Changelog |

## 24.5 Utilities

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/system/time` | GET | Server time |
| `/system/echo` | ANY | Echo request |
| `/system/ping` | GET | Ping/pong |

---

# 25. GRAPHQL SCHEMA

GraphQL endpoint: `/graphql`  
GraphQL Playground: `/playground`

Full GraphQL schema with Query and Mutation types for all major features. Includes custom scalars (JSON, DateTime, BigInt, Bytes, UUID), comprehensive input types, and enums for all options.

---

# 26. WEB UI STRUCTURE

## 26.1 Pages

| Path | Description |
|------|-------------|
| `/` | Dashboard with quick tools |
| `/text` | Text utilities |
| `/crypto` | Cryptography tools |
| `/network` | Network tools |
| `/docker` | Docker tools |
| `/datetime` | Date & time tools |
| `/weather` | Weather tools |
| `/geo` | Geolocation tools |
| `/math` | Math & numbers |
| `/convert` | Unit conversion |
| `/generate` | Generators |
| `/validate` | Validators |
| `/parse` | Parsers & formatters |
| `/language` | Language tools |
| `/test` | Testing tools |
| `/osint` | OSINT tools |
| `/research` | Research tools |
| `/fun` | Fun & content |
| `/lorem` | Fake data |
| `/dev` | Developer tools |
| `/image` | Image tools |
| `/swagger` | Swagger UI |
| `/redoc` | ReDoc |
| `/playground` | GraphQL Playground |

## 26.2 Settings

| Setting | Options | Default |
|---------|---------|---------|
| Theme | `light`, `dark`, `system` | `system` |
| Accent Color | Any hex color | `#4f46e5` |
| Font Size | `small`, `medium`, `large` | `medium` |
| Default Format | `json`, `yaml`, `xml` | `json` |

---

# 27. CONFIGURATION

## 27.1 Environment Variables

| Variable | Type | Default | Description |
|----------|------|---------|-------------|
| `CASTOOLS_HOST` | string | `0.0.0.0` | Listen host |
| `CASTOOLS_PORT` | int | `8080` | Listen port |
| `CASTOOLS_LOG_LEVEL` | string | `info` | Log level |
| `CASTOOLS_LOG_FORMAT` | string | `json` | Log format |
| `CASTOOLS_REQUEST_TIMEOUT` | duration | `30s` | Request timeout |
| `CASTOOLS_MAX_REQUEST_SIZE` | size | `10MB` | Max body |
| `CASTOOLS_RATE_LIMIT` | int | `100` | Req/minute |
| `CASTOOLS_CORS_ORIGINS` | string | `*` | CORS origins |
| `CASTOOLS_CACHE_ENABLED` | bool | `true` | Enable cache |
| `CASTOOLS_CACHE_TTL` | duration | `5m` | Cache TTL |
| `CASTOOLS_CACHE_MAX_SIZE` | size | `100MB` | Max cache |

## 27.2 YAML Configuration

Full YAML config support with all server, limits, rate limiting, CORS, security headers, logging, cache, and feature flag options.

---

# 28. EMBEDDED DATA

| Category | Size |
|----------|------|
| Text (lorem, words) | ~2MB |
| Names (multi-locale) | ~5MB |
| Geographic data | ~10MB |
| Network data | ~3MB |
| Fun content | ~5MB |
| Generators data | ~2MB |
| Language data | ~15MB |
| Templates | ~1.5MB |
| **Total** | **~45MB** |

---

# 29. PROJECT STRUCTURE

```
api/
├── cmd/api/main.go
├── internal/
│   ├── api/
│   │   ├── router.go
│   │   ├── middleware/
│   │   └── handlers/
│   ├── graphql/
│   ├── services/
│   ├── models/
│   ├── data/
│   ├── cache/
│   └── config/
├── web/
│   ├── src/
│   └── dist/
├── api/openapi.yaml
├── Makefile
├── Dockerfile
├── docker-compose.yml
├── go.mod
└── README.md
```

---

# 30. BUILD & DEPLOYMENT

## 30.1 Build Targets

- `make build` - Build binary
- `make build-all` - Cross-platform builds
- `make docker` - Docker image
- `make docker-multi` - Multi-arch Docker
- `make test` - Run tests
- `make lint` - Run linter

## 30.2 Dockerfile

Multi-stage build with Go 1.22-alpine builder, Node 20-alpine for web, and Alpine 3.19 runtime. Final image ~75MB.

## 30.3 docker-compose.yml

Single service with health checks, resource limits (256MB memory, 1 CPU), and all environment variables.

## 30.4 Performance Targets

| Metric | Target |
|--------|--------|
| Startup time | < 500ms |
| Binary size | < 75MB |
| Memory (idle) | < 50MB |
| Memory (peak) | < 256MB |
| Response p50 | < 10ms |
| Response p95 | < 50ms |
| Response p99 | < 100ms |
| Concurrent connections | 10,000+ |
| Requests/second | 50,000+ |

## 30.5 Security

- Input validation (go-playground/validator)
- Rate limiting (token bucket per IP)
- Request size limits
- Timeouts (read/write/request)
- Security headers (HSTS, CSP, X-Frame-Options)
- No shell execution
- No file system access (embedded only)
- Memory safety (cache limits, LRU eviction)
- Constant-time comparison (crypto)

---

# END OF SPECIFICATION

**Document Version:** 1.0.0  
**Last Updated:** 2024-01-15  
**Total Endpoints:** 1,418  
**Estimated Binary Size:** ~75MB  
**Estimated Development Time:** 6-8 months

