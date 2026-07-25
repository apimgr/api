## [ ] Wire per-tool detail pages linked from the 21 category pages
Read: AI.md PART 16 (frontend), src/server/template/page/*.tmpl
Each of the 21 category pages (text, crypto, datetime, network, convert,
dev, docker, fun, generate, geo, image, language, lorem, math, osint,
parse, research, system, testing, validate, weather) links to ~12
per-tool detail pages (e.g. /crypto/hash, /network/dns, /text/uuid) per
the PART 16 frontend-route-mirrors-API-route rule. Wired so far, spec-
compliant (all JS in static/js/app.js, no inline onsubmit/onclick):
docker/version; network/{subnet,ula,port}; weather/current; geo/ip;
convert/length; math/calculate; parse/json; fun/joke; lorem/person;
testing/http (nav category "testing", API route prefix stays /test);
osint/email; dev/format-json; validate/email; image/placeholder;
datetime/{convert,unix,add,diff}; crypto/{totp,random}. This batch
exhausts every sub-tool that maps directly onto an existing, non-stub
API handler in api_utils.go — five JS executors now cover every request
shape found: `executeTool` (query-string GET), `executeToolTemplate`
(path-param GET, also used for query-string-templated GETs like
crypto/totp), `executeToolBody` (raw-body POST), `executeToolQueryPost`
(POST-only, query-string params), `executeToolImage` (binary-image GET,
rendered as `<img>` not text). The pre-existing first batch — the 7 tools
that already had templates on disk (crypto/{hash,jwt,password},
datetime/now, network/{ip,dns}, text/uuid) — remains wired via
`toolPages()`/`toolPageHandler`/composite `pageTemplates` keys in
server.go. Of that first batch, crypto/hash, crypto/jwt, and network/dns
were found broken (templates fetched a nonexistent POST endpoint via
inline `<script>`) and have been fixed: net-new `apiCryptoJWTDecodeHandler`
and `apiNetworkDNSHandler` (composing existing `osintService.DNSLookup`)
added to api_utils.go, `/crypto/hash`, `/crypto/jwt/{token}`, and
`/network/dns/{domain}[/{type}]` API routes registered, and all three
templates rewritten to the inline-JS-free `data-template` executor
pattern (dns.tmpl's record-type select also trimmed to the 6 types the
backend actually supports: A, AAAA, CNAME, MX, TXT, NS). crypto/password
and crypto/bcrypt/pin/password-strength were already spec-compliant and
untouched. Most of the ~240 linked sub-tool paths still have neither a
template nor a route because they need net-new backend services (not
just a template) — that is a larger, separate body of work: confirm
which remaining sub-tools need new services vs. are simply unimplemented
per IDEA.md non-goals, then design/build backend + template +
`toolPages()` entry together, one commit per tool or small logical
group. Within the crypto category specifically, 7 linked sub-tools have
zero backend support and need net-new crypto services before they can be
wired: encrypt, decrypt (AES-256-GCM), hmac, rsa, ed25519, pgp,
certificate — scope each as its own commit/finding when picked up.

## [ ] Known permanent API gaps needing a future spec/dependency decision
Read: src/server/api_utils.go (apiGenerateQRHandler, apiLanguageDetectHandler,
apiResearchExtractHandler doc comments)
Three of the 16 wired API routes honestly return 501 NOT_SUPPORTED rather
than inventing behavior: generate/qr (no QR encoder exists anywhere in the
codebase or go.mod), language/detect (conflicts with IDEA.md's declared
non-goal of language auto-detection), and research/extract (research.go's
own source comment documents citation extraction from unstructured text as
unimplemented). Resolving these requires a user/spec decision — either add
a QR-encoding dependency, confirm language/detect should stay unsupported
per IDEA.md, or scope what "extraction" means for research/extract — not
further code guessing.
