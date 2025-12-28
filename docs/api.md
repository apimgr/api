# API Reference

Complete reference for all API endpoints. All endpoints return JSON unless otherwise specified.

## Base URL

```
http://localhost:64580/api/v1
```

## Authentication

Most utility endpoints do not require authentication. Admin endpoints require authentication via:

- **Bearer Token** - `Authorization: Bearer <token>` header
- **Session Cookie** - For web admin panel

## Response Format

### Success Response

```json
{
  "result": "...",
  "status": 200
}
```

### Error Response

```json
{
  "error": "Error message",
  "status": 400,
  "request_id": "abc123def456"
}
```

## Health Check

### GET /healthz

Check service health status.

**Response:**

```json
{
  "status": "ok",
  "uptime": 3600,
  "version": "1.0.0",
  "timestamp": "2025-01-15T10:30:45Z"
}
```

---

## Text Utilities

### POST /api/v1/text/uppercase

Convert text to uppercase.

**Request:**

```json
{
  "text": "hello world"
}
```

**Response:**

```json
{
  "result": "HELLO WORLD"
}
```

### POST /api/v1/text/lowercase

Convert text to lowercase.

**Request:**

```json
{
  "text": "HELLO WORLD"
}
```

**Response:**

```json
{
  "result": "hello world"
}
```

### POST /api/v1/text/reverse

Reverse a text string.

**Request:**

```json
{
  "text": "hello"
}
```

**Response:**

```json
{
  "result": "olleh"
}
```

### POST /api/v1/text/base64/encode

Encode text to Base64.

**Request:**

```json
{
  "text": "hello world"
}
```

**Response:**

```json
{
  "result": "aGVsbG8gd29ybGQ="
}
```

### POST /api/v1/text/base64/decode

Decode Base64 to text.

**Request:**

```json
{
  "text": "aGVsbG8gd29ybGQ="
}
```

**Response:**

```json
{
  "result": "hello world"
}
```

### POST /api/v1/text/slug

Convert text to URL-friendly slug.

**Request:**

```json
{
  "text": "Hello World Example"
}
```

**Response:**

```json
{
  "result": "hello-world-example"
}
```

### POST /api/v1/text/hash

Generate hash of text.

**Request:**

```json
{
  "text": "hello",
  "algorithm": "sha256"
}
```

**Response:**

```json
{
  "result": "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
}
```

---

## Cryptographic Utilities

### POST /api/v1/crypto/hash/md5

Generate MD5 hash.

**Request:**

```json
{
  "input": "hello world"
}
```

**Response:**

```json
{
  "hash": "5eb63bbbe01eeed093cb22bb8f5acdc3",
  "algorithm": "md5"
}
```

### POST /api/v1/crypto/hash/sha256

Generate SHA-256 hash.

**Request:**

```json
{
  "input": "hello world"
}
```

**Response:**

```json
{
  "hash": "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9",
  "algorithm": "sha256"
}
```

### POST /api/v1/crypto/bcrypt/hash

Hash password with bcrypt.

**Request:**

```json
{
  "password": "mypassword",
  "cost": 10
}
```

**Response:**

```json
{
  "hash": "$2a$10$N9qo8uLOickgx2ZMRZoMye...",
  "algorithm": "bcrypt"
}
```

### POST /api/v1/crypto/bcrypt/compare

Verify bcrypt password.

**Request:**

```json
{
  "password": "mypassword",
  "hash": "$2a$10$N9qo8uLOickgx2ZMRZoMye..."
}
```

**Response:**

```json
{
  "match": true
}
```

### GET /api/v1/crypto/random/string

Generate random string.

**Query Parameters:**
- `length` - String length (default: 32)
- `charset` - Character set: `alphanumeric`, `alpha`, `numeric`, `hex` (default: `alphanumeric`)

**Response:**

```json
{
  "result": "Kf8jN2mP9qR5tY7wX3zA",
  "length": 20
}
```

### GET /api/v1/crypto/random/uuid

Generate UUID.

**Response:**

```json
{
  "uuid": "550e8400-e29b-41d4-a716-446655440000",
  "version": 4
}
```

---

## Date/Time Utilities

### GET /api/v1/datetime/now

Get current timestamp.

**Query Parameters:**
- `timezone` - Timezone (default: UTC)
- `format` - Output format: `iso8601`, `rfc3339`, `unix` (default: `iso8601`)

**Response:**

```json
{
  "timestamp": "2025-01-15T10:30:45Z",
  "timezone": "UTC",
  "unix": 1705315845
}
```

### POST /api/v1/datetime/convert

Convert between timezones.

**Request:**

```json
{
  "timestamp": "2025-01-15T10:30:45Z",
  "from_timezone": "UTC",
  "to_timezone": "America/New_York"
}
```

**Response:**

```json
{
  "original": "2025-01-15T10:30:45Z",
  "converted": "2025-01-15T05:30:45-05:00",
  "timezone": "America/New_York"
}
```

### POST /api/v1/datetime/parse

Parse date/time string.

**Request:**

```json
{
  "datetime": "2025-01-15 10:30:45",
  "format": "2006-01-02 15:04:05"
}
```

**Response:**

```json
{
  "timestamp": "2025-01-15T10:30:45Z",
  "unix": 1705315845
}
```

### POST /api/v1/datetime/add

Add duration to timestamp.

**Request:**

```json
{
  "timestamp": "2025-01-15T10:30:45Z",
  "duration": "2h30m"
}
```

**Response:**

```json
{
  "original": "2025-01-15T10:30:45Z",
  "result": "2025-01-15T13:00:45Z",
  "added": "2h30m0s"
}
```

### POST /api/v1/datetime/diff

Calculate time difference.

**Request:**

```json
{
  "start": "2025-01-15T10:00:00Z",
  "end": "2025-01-15T12:30:00Z"
}
```

**Response:**

```json
{
  "difference": "2h30m0s",
  "seconds": 9000,
  "minutes": 150,
  "hours": 2.5
}
```

### GET /api/v1/datetime/unix

Get Unix timestamp.

**Response:**

```json
{
  "unix": 1705315845,
  "milliseconds": 1705315845000
}
```

---

## Network Utilities

### GET /api/v1/network/ip

Get client IP address.

**Response:**

```json
{
  "ip": "203.0.113.42",
  "forwarded_for": null
}
```

### GET /api/v1/network/headers

Get request headers.

**Response:**

```json
{
  "headers": {
    "User-Agent": "Mozilla/5.0...",
    "Accept": "application/json",
    "X-Request-ID": "abc123"
  }
}
```

### GET /api/v1/network/useragent

Parse user agent.

**Response:**

```json
{
  "user_agent": "Mozilla/5.0...",
  "browser": "Chrome",
  "version": "120.0",
  "os": "Windows",
  "device": "Desktop"
}
```

### GET /api/v1/network/geoip

GeoIP lookup.

**Query Parameters:**
- `ip` - IP address to lookup (default: client IP)

**Response:**

```json
{
  "ip": "203.0.113.42",
  "country": "US",
  "region": "California",
  "city": "San Francisco",
  "latitude": 37.7749,
  "longitude": -122.4194
}
```

---

## Rate Limiting

All endpoints are rate limited:

- **Authenticated:** 100 requests per minute
- **Unauthenticated:** 20 requests per minute

Rate limit headers are included in responses:

```
X-RateLimit-Limit: 100
X-RateLimit-Remaining: 95
X-RateLimit-Reset: 1705315900
```

When rate limit is exceeded:

```json
{
  "error": "Rate limit exceeded",
  "status": 429,
  "retry_after": 60
}
```

## Interactive Documentation

For interactive API exploration, visit:

- **Swagger UI:** `http://localhost:64580/openapi`
- **GraphQL:** `http://localhost:64580/graphql`

## Next Steps

- [Configure the server](configuration.md)
- [Set up admin panel](admin.md)
- [CLI reference](cli.md)
