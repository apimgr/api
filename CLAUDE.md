## CLAUDE.md - Claude Code Implementation Guide

```markdown
# Claude Code Implementation Guide

## Project Overview
**Name**: API - The Everything API  
**Organization**: ApiMgr  
**Repository**: github.com/apimgr/api  
**Domain**: apimgr.us  
**License**: MIT  

## Project Structure
```
api/
├── cmd/
│   └── api/
│       └── main.go                 # Application entry point
├── internal/
│   ├── config/
│   │   └── config.go               # Configuration management
│   ├── handlers/
│   │   ├── api/
│   │   │   └── v1/                 # API version 1 handlers
│   │   │       ├── network/        # Network endpoints
│   │   │       ├── security/       # Security endpoints
│   │   │       ├── validate/       # Validation endpoints
│   │   │       ├── convert/        # Conversion endpoints
│   │   │       ├── datetime/       # Date/time endpoints
│   │   │       ├── weather/        # Weather endpoints
│   │   │       ├── codes/          # Code lookup endpoints
│   │   │       ├── dictionary/     # Dictionary endpoints
│   │   │       ├── generate/       # Generation endpoints
│   │   │       └── ...             # Other category handlers
│   │   └── web/                    # Web frontend handlers
│   ├── middleware/
│   │   ├── cors.go                 # CORS middleware
│   │   ├── ratelimit.go           # Rate limiting
│   │   ├── logging.go              # Request logging
│   │   ├── recovery.go            # Panic recovery
│   │   └── proxy.go               # Reverse proxy support
│   ├── models/
│   │   └── responses.go           # Response structures
│   ├── services/
│   │   ├── cache/                 # Caching service
│   │   ├── database/              # Database service
│   │   └── external/              # External API clients
│   └── utils/
│       ├── errors.go              # Error handling
│       └── helpers.go             # Helper functions
├── web/
│   ├── templates/
│   │   ├── layouts/
│   │   │   └── base.html          # Base layout
│   │   ├── partials/
│   │   │   ├── header.html       # Header component
│   │   │   ├── footer.html       # Footer component
│   │   │   └── nav.html          # Navigation
│   │   └── pages/
│   │       ├── index.html        # Homepage
│   │       ├── docs.html         # Documentation
│   │       └── tools.html        # Tools interface
│   └── static/
│       ├── css/
│       │   └── style.css         # Tailwind output
│       ├── js/
│       │   └── app.js            # Frontend JavaScript
│       └── img/                  # Images/icons
├── data/
│   ├── codes/                    # Static code data
│   ├── words/                    # Dictionary data
│   └── zones/                    # Timezone data
├── scripts/
│   ├── build.sh                  # Build script
│   ├── test.sh                   # Test script
│   └── deploy.sh                 # Deployment script
├── configs/
│   └── systemd/
│       └── api.service           # Systemd service file
├── Dockerfile                     # Docker configuration
├── docker-compose.yml            # Docker Compose
├── Makefile                      # Build automation
├── go.mod                        # Go modules
├── go.sum                        # Go dependencies
├── LICENSE.md                    # MIT license + third-party
├── README.md                     # Documentation
├── CLAUDE.md                     # This file
└── .github/
    └── workflows/
        └── ci.yml                # GitHub Actions
```

## Core Implementation Rules

### 1. No Code Generation
- Define structures and interfaces only
- Plan endpoints and routes
- Document expected behavior
- Create test cases
- Leave actual implementation empty

### 2. Response Format
Every endpoint MUST return this structure:
```go
type APIResponse struct {
    Success bool        `json:"success"`
    Data    interface{} `json:"data,omitempty"`
    Error   *ErrorInfo  `json:"error,omitempty"`
    Meta    *MetaInfo   `json:"meta"`
}

type ErrorInfo struct {
    Code      string      `json:"code"`
    Message   string      `json:"message"`
    Details   interface{} `json:"details,omitempty"`
    Timestamp string      `json:"timestamp"`
    Path      string      `json:"path"`
    RequestID string      `json:"request_id"`
}

type MetaInfo struct {
    Version     string     `json:"version"`
    Timestamp   string     `json:"timestamp"`
    RequestID   string     `json:"request_id"`
    RateLimit   *RateInfo  `json:"rate_limit,omitempty"`
    Cache       string     `json:"cache,omitempty"`
}
```

### 3. Path Parameter Pattern
Use path parameters instead of query strings where logical:
```go
// Good
router.HandleFunc("/api/v1/network/ip/{ip}", handleIPLookup)
router.HandleFunc("/api/v1/validate/email/{email}", handleEmailValidation)
router.HandleFunc("/api/v1/convert/currency/{from}/{to}/{amount}", handleCurrencyConversion)

// Bad
router.HandleFunc("/api/v1/network/ip?ip={ip}", handleIPLookup)
```

### 4. Help Endpoints
Every category MUST have a `:help` endpoint:
```go
router.HandleFunc("/api/v1/{category}/:help", handleCategoryHelp)
router.HandleFunc("/api/v1/{category}/:help/{format}", handleCategoryHelpFormatted)
```

### 5. Environment Variables
All configuration via environment variables, NO config files:
```go
var (
    apiHost     = getEnv("API_HOST", "0.0.0.0")
    apiPort     = getEnv("API_PORT", "8080")
    rateLimit   = getEnv("RATE_LIMIT", "100/minute")
    corsOrigins = getEnv("CORS_ORIGINS", "*")
    dbPath      = getEnv("DB_PATH", "/var/lib/api/data.db")
    logLevel    = getEnv("LOG_LEVEL", "info")
    logPath     = getEnv("LOG_PATH", "/var/log/api/app.log")
)
```

### 6. Error Codes
Standardized error codes:
```
{CATEGORY}_{SPECIFIC_ERROR}

Categories:
- VALIDATION_*
- AUTH_*
- RATE_*
- RESOURCE_*
- FORMAT_*
- NETWORK_*
- EXTERNAL_*
- SYSTEM_*
```

### 7. Rate Limiting
Default: 100 requests/minute per IP
Headers:
```
X-RateLimit-Limit: 100
X-RateLimit-Remaining: 95
X-RateLimit-Reset: 1704067200
```

### 8. CORS
Default: Allow all origins (`*`)
```go
w.Header().Set("Access-Control-Allow-Origin", "*")
w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PUT,DELETE,OPTIONS")
w.Header().Set("Access-Control-Allow-Headers", "*")
```

### 9. Reverse Proxy Support
Trust these headers:
```go
realIP := r.Header.Get("X-Real-IP")
if realIP == "" {
    realIP = r.Header.Get("X-Forwarded-For")
}
if realIP == "" {
    realIP = r.Header.Get("CF-Connecting-IP")
}
```

### 10. Dark Theme Default
Frontend defaults to dark theme:
```javascript
const defaultTheme = localStorage.getItem('theme') || 'dark';
document.documentElement.setAttribute('data-theme', defaultTheme);
```

## API Categories & Endpoints

### Total: 26 Categories, 500+ Endpoints

1. **Network** (`/api/v1/network`)
   - DNS, IP, WHOIS, SSL, Port checking
   - Domain/hostname parsing

2. **Security** (`/api/v1/security`)
   - Hashing (50+ algorithms including Argon2)
   - Password generation/validation
   - UUID, token generation

3. **Validation** (`/api/v1/validate`)
   - Email, phone, credit card
   - Address, IBAN, VAT

4. **Conversion** (`/api/v1/convert`)
   - Currency, units, temperature
   - Data formats (JSON↔XML↔YAML)

5. **DateTime** (`/api/v1/datetime`)
   - Timezone conversion
   - Date calculations
   - Business days

6. **Weather** (`/api/v1/weather`)
   - Current, forecast, historical
   - Air quality, UV index

7. **Codes** (`/api/v1/codes`)
   - Airport (IATA/ICAO)
   - Postal/ZIP codes
   - Country, currency codes

8. **Dictionary** (`/api/v1/dictionary`)
   - Definitions, synonyms, antonyms
   - Pronunciation, etymology

9. **Generate** (`/api/v1/generate`)
   - Lorem ipsum variants
   - Fake data, test data

10. **Filter** (`/api/v1/filter`)
    - Profanity checking
    - Username/password validation
    - Content safety

[... and 16 more categories]

## Implementation Guidelines

### Handler Template
```go
func handleEndpoint(w http.ResponseWriter, r *http.Request) {
    // 1. Parse parameters
    vars := mux.Vars(r)
    param := vars["param"]
    
    // 2. Validate input
    if err := validateInput(param); err != nil {
        sendError(w, "VALIDATION_INVALID_INPUT", err.Error(), 400)
        return
    }
    
    // 3. Process request
    result, err := processRequest(param)
    if err != nil {
        sendError(w, "SYSTEM_INTERNAL_ERROR", err.Error(), 500)
        return
    }
    
    // 4. Send response
    sendSuccess(w, result)
}
```

### External API Integration
Only use FREE, token-free, commercially-friendly APIs:
- OpenStreetMap (geocoding)
- IP-API (IP geolocation)
- REST Countries (country data)
- ExchangeRate-API (currency)

### Database Schema
SQLite for static data:
```sql
CREATE TABLE codes (
    type TEXT,
    code TEXT,
    data JSON,
    PRIMARY KEY (type, code)
);

CREATE TABLE cache (
    key TEXT PRIMARY KEY,
    value TEXT,
    expires_at INTEGER
);
```

### Frontend Requirements
- Dark theme default
- Mobile responsive (98% width <720px, 90% ≥720px)
- Footer always at bottom (not sticky)
- Accessibility compliant (WCAG 2.1 AA)
- Cookie-based preferences (no backend storage)

### Performance Targets
- Response time: <100ms (cached)
- Response time: <500ms (external API)
- Startup time: <2 seconds
- Memory usage: <100MB idle
- CPU usage: <5% idle

## Testing Strategy

### Unit Tests
```go
func TestEndpoint(t *testing.T) {
    // Test valid input
    // Test invalid input
    // Test edge cases
    // Test error handling
}
```

### Integration Tests
- Test each category
- Test rate limiting
- Test CORS
- Test error responses

### Load Tests
- 1000 req/sec sustained
- 5000 req/sec burst
- 10000 concurrent connections

## Deployment

### Docker
```dockerfile
FROM golang:1.21-alpine AS builder
# Build stage

FROM alpine:latest
# Runtime stage
```

### Systemd
```ini
[Unit]
Description=API Service
After=network.target

[Service]
Type=simple
User=api
ExecStart=/usr/local/bin/api
Restart=always

[Install]
WantedBy=multi-user.target
```

## Monitoring

### Health Check
```
GET /api/v1/health
GET /api/v1/status
GET /healthz
```

### Metrics
- Request count
- Response times
- Error rates
- Rate limit hits

## Security Considerations

1. **Input Validation**: Validate everything
2. **Rate Limiting**: Prevent abuse
3. **CORS**: Configured but open by default
4. **Headers**: Security headers on all responses
5. **Logging**: Log errors, not sensitive data
6. **Proxy**: Support reverse proxy headers

## External Dependencies

### Go Modules
- `gorilla/mux` - Routing
- `go-redis/redis` - Caching (optional)
- `mattn/go-sqlite3` - Database
- `golang.org/x/crypto` - Hashing
- `golang.org/x/time/rate` - Rate limiting

### Frontend
- Tailwind CSS (CDN)
- Alpine.js (CDN)
- No build process required

## Development Workflow

1. **Feature Branch**: Create from main
2. **Implementation**: Follow patterns
3. **Testing**: Unit + integration
4. **Documentation**: Update README/help
5. **Pull Request**: Review required
6. **CI/CD**: Jenkins pipeline

## Common Patterns

### Batch Operations
```go
POST /api/v1/batch
{
    "operations": [...],
    "parallel": true
}
```

### Format Selection
```
GET /endpoint?format=json|xml|yaml|csv
Accept: application/json
```

### Pagination
```
GET /endpoint?page=1&limit=20
```

## Notes for Claude Code

When implementing with Claude Code:
1. Start with router setup
2. Implement one category fully as template
3. Replicate pattern for other categories
4. Add middleware layer
5. Implement caching
6. Add frontend templates
7. Write tests
8. Create Docker setup

Remember:
- No authentication required
- Everything is public
- Rate limiting is the main protection
- Keep it simple and fast
- Document everything with :help endpoints

## Questions/Clarifications Needed

Before implementation:
1. Redis required or optional for caching?
2. PostgreSQL option or SQLite only?
3. CDN for static assets?
4. Backup strategy for data?
5. Log rotation handled by system or app?

## Version History

- v1.0.0 - Initial implementation (current)
- Future: GraphQL support?
- Future: WebSocket support?
- Future: gRPC support?
```

This CLAUDE.md file provides a complete implementation guide for Claude Code, with all the context, patterns, and rules needed to build the API service correctly. It includes the structure, conventions, and specific requirements while following the "no code generation" rule - focusing on planning and organization instead.
