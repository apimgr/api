# API Structure & SSL Rules (PART 13, 14, 15)

⚠️ **These rules are NON-NEGOTIABLE. Violations are bugs.** ⚠️

**IDEA.md override:** IDEA.md non-goals declare no user accounts, no admin
panel, no auth/sessions. PART 13-15 base spec text about auth-related
metrics/sessions does not apply to public API routes here — every endpoint
is public, unauthenticated, rate-limited only. Do not add auth/session
fields to health or API responses on the strength of generic PART 13/14
example code.

## CRITICAL - NEVER DO
- Never invent endpoints, routes, or fields not in PART 13/14 — canonical
  route/response shapes are law
- Never add invented fields to `HealthResponse` (e.g. no `Node`, `Cluster`,
  `Service`, hostname leaks) — the canonical struct/field order is fixed
- Never expose `os.Hostname()`, internal paths, or config internals via
  `/server/healthz` or any health/status endpoint
- Never serve old/removed API doc routes — PART 14 explicitly states
  `/openapi`, `/openapi.json`, and `/graphql` (GET/POST at root) are
  "no longer served"; do not implement redirects from the old paths
- Never use non-plural, non-lowercase, verb-containing, or trailing-slash
  route segments — routes are versioned, plural nouns, lowercase, hyphens
- Never let YAML be an OpenAPI output format — Swagger/OpenAPI is JSON-only
- Never let Swagger and GraphQL schemas drift out of sync with the actual
  routes — both are required to be kept current together
- Never implement DNS-01 ACME challenge without a prior product decision on
  provider scope (see KEY DECISIONS) — do not silently add a single
  hardcoded DNS provider
- Never let debug mode or any mode bypass ACME/TLS validation or cert
  ownership checks
- Never let self-signed fallback silently replace a working Let's Encrypt
  cert — self-signed is a fallback only for Tor/I2P/ACME-failure cases

## CRITICAL - ALWAYS DO
- Auto-detect client type (our CLI, text browser, HTTP tool/curl, browser)
  and content-negotiate per PART 14 rules on every API route
- Return the canonical success/error JSON envelope: `{ok: true, data: {...}}`
  / `{ok: false, error: "CODE", message: "...", details: {}}`
- Register all three required health routes: `/server/healthz`,
  `/api/healthz`, `/api/{api_version}/server/healthz` (plain `/healthz` may
  exist as an alias)
- Follow SemVer for `{project_version}`; stable/beta/daily version formats
  per PART 13
- Serve Swagger/OpenAPI (JSON only) and GraphQL at their PART-14-mandated
  paths, sourced from `src/swagger/` and `src/graphql/`
- Renew Let's Encrypt certs at the 7-day-before-expiry threshold (not 30)
- Fall back to a self-signed certificate when ACME/Let's Encrypt is
  unavailable (e.g. Tor/I2P hidden services, or ACME failure)
- Apply the 4-tier certificate lookup/ownership model from PART 15 before
  issuing or reusing any certificate
- Support HTTP-01 and TLS-ALPN-01 challenge types for real; treat DNS-01 as
  gated behind a pending product decision (see below)

## KEY DECISIONS (pre-answered)
| Question | Answer | Spec Reference |
|----------|--------|----------------|
| Response envelope | `{ok, data}` success / `{ok, error, message, details}` error | PART 14 |
| Health routes | `/server/healthz`, `/api/healthz`, `/api/{api_version}/server/healthz` (+ optional `/healthz` alias) | PART 13 |
| Old `/openapi`, `/openapi.json`, `/graphql` root routes | Removed by spec — must not be served; **current `src/server/server.go` still registers `/openapi.json`, `/swagger`, and `/graphql` (GET+POST) at root and does not yet register the canonical `/server/docs/swagger`, `/server/docs/graphql`, or `/api/autodiscover` paths — this is an open compliance gap, not an intentional deviation** | PART 14 |
| OpenAPI format | JSON only, never YAML | PART 14 |
| Renewal threshold | 7 days before expiry (not the generic 30) | PART 15; confirmed in `src/ssl/` per AUDIT.AI.md |
| Self-signed fallback | Implemented (`src/ssl/selfsigned.go`) for Tor/I2P and ACME-failure cases | PART 15; AUDIT.AI.md |
| DNS-01 multi-provider ACME | **Not implemented** — `PerformDNS01Challenge`/`PerformHTTP01Challenge`/`PerformTLSALPN01Challenge` in `src/ssl/acme.go` return explicit "not implemented" for DNS-01; full multi-provider DNS-01 (go-acme/lego, `server.tls.dns_credentials.*` encrypted storage) is a **NEEDS DECISION** item per AUDIT.AI.md, not yet product-scoped | PART 15; AUDIT.AI.md |
| ssl.Manager/autocert wiring | Not yet wired into `main.go` (no `ListenAndServeTLS`/`:443` call found) — open gap per AUDIT.AI.md | PART 15; AUDIT.AI.md |
| Scheduler SSL renewal path | `src/scheduler/tasks.go`'s `sslRenewalTask()` still uses a hardcoded flat `{data_dir}/ssl/cert.pem` path, inconsistent with the tiered cert layout — open gap per AUDIT.AI.md | PART 15; AUDIT.AI.md |
| Route style | Versioned, plural nouns, lowercase, hyphens, no trailing slash, no verbs | PART 14 |

## TERMINOLOGY
| Term | Meaning |
|------|---------|
| `HealthResponse` | Canonical fixed-field-order struct served by all health routes |
| Client type detection | Auto-classifying request as our CLI / text browser / HTTP tool / browser to shape response format |
| 4-tier certificate model | PART 15's ownership/lookup precedence for which cert serves a given hostname |
| ACME challenge types | HTTP-01, TLS-ALPN-01 (implemented), DNS-01 (not implemented, gated) |
| Self-signed fallback | Locally generated cert used only when ACME is unavailable or fails |

## QUICK REFERENCE
- Health: `src/server/handler/health.go` implements client-detection
  (`isOurCliClient`, `isTextBrowser`, `isHttpTool`, `isNonInteractiveClient`)
  and `HTML2TextConverter` — these live in the health handler, not a
  separate `src/common/httputil/detect.go`; spec code blocks are
  illustrative of behavior, not a mandated file path
- Swagger/GraphQL source: `src/swagger/`, `src/graphql/` (standardized
  locations); **route registration in `src/server/server.go` does not yet
  match PART 14's canonical paths — flagged as an open gap above**
- SSL: `src/ssl/{acme,acme_test,selfsigned,selfsigned_test,ssl,ssl_test}.go`
- No admin/auth fields ever appear in health or API JSON — IDEA.md override
- DNS-01 ACME: do not implement piecemeal; requires the NEEDS DECISION
  product/dependency call documented in AUDIT.AI.md first

---
For complete details, see AI.md PART 13, PART 14, PART 15
