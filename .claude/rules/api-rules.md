# API Rules (PART 13, 14, 15)

⚠️ **These rules are NON-NEGOTIABLE. Violations are bugs.** ⚠️

## CRITICAL - NEVER DO
- Never expose credentials, connection strings, internal IPs, file paths, env vars,
  config contents, secrets, or stack traces in `/server/healthz` or any health/status
  response — checks are `"ok"`/`"error"` only, never detail (e.g. `"database": "ok"`,
  never `"database": "libsql://user:token@host/db"`).
- Never add sub-routes under `/server/healthz` (no `/server/healthz/db`, etc.).
- Never keep legacy/removed endpoints "for compatibility" — delete them completely,
  no redirects, no deprecation shims, no forked/duplicate handler trees.
- Never hardcode API version — always use `APIBasePath()`/`{api_version}`, never `v1`
  literal in code.
- Never use singular resource nouns, mixed case, underscores, verbs, or trailing
  slashes in routes (`/api/{api_version}/item`, `/Items`, `/api_keys`, `/items/`,
  `/getItems` are all wrong).
- Never put query params where a path param belongs (`?id=123` instead of `/123`).
- Never emit a bare JSON array at response root — always wrap (`{"data": [...]}`).
- Never invent a different error shape per endpoint; never put status code in the
  body; never use bare `{"error": "..."}` with no `ok`/code; never add ad-hoc
  top-level fields like `reason`/`retry_after` (use `details` or HTTP headers).
- Never manually edit generated OpenAPI JSON or GraphQL schema files, and never
  place swagger/graphql files outside `src/swagger/` / `src/graphql/`.
- Never serve OpenAPI as YAML or add a `.json` suffix route — JSON only, fixed path.
- Never implement client-side routing/rendering (SPA), business logic in JS, or
  make JS required for core functionality — server renders, JS only enhances.
- Never replicate an external service's entire API surface (view/list/delete/
  pagination) unless the user explicitly asked for route/API/client compatibility —
  default is feature compatibility only, using our own routes.
- Never partially implement an RFC-defined protocol (DNS, DHCP, SMTP, HTTP, FTP,
  NTP, WebDAV, etc.) — full RFC compliance is mandatory once that protocol is the
  thing being built.
- Never use icons/emojis/ASCII art/color in log files or stdout log output — logs
  are always raw plain text; icons are console/banner only.
- Never let the app manage or auto-renew certificates found under
  `/etc/letsencrypt/live/**` — that tree is system(certbot)-owned.
- Never set `DOMAIN` to an overlay address (`.onion`, `.i2p`, `.exit`) — those are
  app-generated/managed separately.

## CRITICAL - ALWAYS DO
- Always version API routes: `/api/{api_version}/...`; always use plural,
  lowercase, hyphenated nouns with no trailing slash.
- Always mount unversioned aliases (`/api/swagger`, `/api/graphql`, `/api/healthz`,
  `/healthz`) as the exact same handler as their versioned/canonical route — direct
  serve, never a redirect (curl/SDKs don't follow redirects reliably; POST+redirect
  is unsafe; caching and latency both suffer).
- Always accept auth tokens from any of the supported headers (PART 8 priority
  order) plus `?token=` query param on protected API endpoints.
- Always give every `/api/{api_version}/*` and `/**` frontend endpoint content
  negotiation: JSON default on API routes (text via `.txt`, `Accept: text/plain`,
  or non-interactive HTTP-tool detection); HTML default on frontend routes (text
  for CLI tools, no-JS HTML for text browsers, JSON for our own CLI client).
- Always wrap success responses as `{"ok": true, "data": {...}}` and errors as
  `{"ok": false, "error": "CODE", "message": "...", "details": {}}` — identical
  shape everywhere, HTTP status carries the status code.
- Always paginate list responses with `{"data": [...], "pagination": {"page",
  "limit" (default 250), "total", "pages"}}`.
- Always keep Swagger and GraphQL specs generated from code at build time and in
  sync with each other and the live API — never manually maintained.
- Always give every API feature with a UI a matching frontend route at the same
  path shape (`/api/{v}/items` ↔ `/items`) and make that frontend fully functional
  without JavaScript (progressive enhancement).
- Always follow the certificate lookup order on startup: `/etc/letsencrypt/live/
  domain/` → `/etc/letsencrypt/live/{fqdn}/` → `{config_dir}/ssl/letsencrypt/{fqdn}/`
  (app auto-renews 7 days before expiry) → `{config_dir}/ssl/local/{fqdn}/` (manual)
  → request new cert via Let's Encrypt if none found.
- Always strip default ports from displayed URLs (`:80`, `:443`).
- Always resolve FQDN in priority order: reverse-proxy headers → `DOMAIN` env →
  `os.Hostname()` → `$HOSTNAME` → public IPv6 → public IPv4 → `localhost`.

## Key Rules Summary
- **Health checks:** `/server/healthz` (frontend, content-negotiated per PART 16
  rules) and `/api/{api_version}/server/healthz` (JSON default). Optional root
  `/healthz` alias only when `server.healthz.root.enabled: true`, mounting the same
  handler, no redirect. Response has a fixed field order: project, status,
  version/build, runtime (uptime/mode/timestamp), features (public only, e.g. Tor,
  GeoIP), checks (ok/error), stats (public aggregates), then app-specific
  extensions. All data must be public-safe for an unauthenticated viewer.
- **Versioning:** SemVer `MAJOR.MINOR.PATCH`, start at `1.0.0`, no `v` prefix in
  the version string (git tags do get `v`). Beta = `YYYYMMDDHHMMSS-beta`, daily =
  `YYYYMMDDHHMMSS`. Version source priority: `release.txt` → git tag → `dev`.
- **Route rules:** all API routes under `/api/{api_version}/`; scopes are
  `/server/*` (server-owned) and `/*` (project resources). Path params identify
  resources; query params filter/sort/paginate. Frontend routes mirror API routes
  1:1 except pure UX redirects.
- **Formatting:** every response/file ends with exactly one trailing newline; JSON
  responses use 2-space indent via `MarshalIndent`; HTML/YAML/CSS/JS use 2 spaces;
  Go/Makefiles use tabs; no trailing whitespace.
- **Content negotiation:** API routes — `.txt` suffix > `Accept: text/plain` >
  non-interactive client (curl/wget) > default JSON. Frontend routes — `Accept:
  text/html` > `Accept: text/plain` > browser User-Agent → HTML > CLI → text >
  default HTML. Three client categories: our own CLI (`{project_name}-cli/` UA,
  gets JSON), text browsers (lynx/w3m/links/elinks, get server-rendered no-JS
  HTML), HTTP tools (curl/wget/httpie, get `HTML2TextConverter()` plain text).
- **API types:** every project ships REST + Swagger/OpenAPI (JSON only, no YAML)
  + GraphQL, generated at build time from code, always in sync, themed to match
  the site's light/dark/auto theme system. Files live only in `src/swagger/` and
  `src/graphql/`.
- **External compatibility:** default to feature/behavior compatibility using our
  own routes; only add the external service's actual route surface when the user
  explicitly asks for route/API/client compatibility. Full protocols (Matrix,
  ActivityPub, WebDAV, RFC-defined standards) always require full spec compliance,
  not partial.
- **Root/system endpoints:** `/`, `/server/healthz`, `/server/docs/swagger`,
  `/server/docs/graphql`, `/metrics`, `/api/autodiscover`, `/api/swagger`,
  `/api/graphql`, `/api/healthz` (unversioned direct aliases), and their versioned
  `/api/{api_version}/server/*` counterparts. Old `/openapi`, `/openapi.json`,
  root `/graphql` are removed permanently, no redirects.
- **TLS/Let's Encrypt:** built into every project; supports HTTP-01, TLS-ALPN-01,
  DNS-01 (any lego-supported DNS provider, credentials AES-256-GCM encrypted at
  rest). `DOMAIN` env accepts a comma-separated list (first = primary; shared base
  infers a wildcard). Dev TLDs (`.local`, `.test`, project-name TLDs, etc.) fall
  back to the public IP for display/access. Port modes: single port = HTTP unless
  it's 443 (HTTPS-only); dual ports = first HTTP, second HTTPS; `ssl.enabled` can
  force HTTPS on any single port. Overlay networks (Tor/I2P) use HTTP by default
  (self-signed certs if HTTPS is forced) and inherit HTTPS-only mode from clearnet.
- **Startup banner:** responsive to terminal width (full ASCII+icons ≥80 cols,
  icons+text 60-79, minimal 40-59, single line <40, plain text under NO_COLOR/
  TERM=dumb); logs themselves are always raw plain text regardless of banner mode.

For complete details, see AI.md PART 13, 14, 15
