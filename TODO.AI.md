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
osint/email; dev/format-json; validate/email; image/placeholder. This
batch exhausts every sub-tool that maps directly onto an existing,
non-stub API handler in api_utils.go — five JS executors now cover every
request shape found: `executeTool` (query-string GET), `executeToolTemplate`
(path-param GET), `executeToolBody` (raw-body POST), `executeToolQueryPost`
(POST-only, query-string params), `executeToolImage` (binary-image GET,
rendered as `<img>` not text). The pre-existing first batch — the 7 tools
that already had templates on disk (crypto/{hash,jwt,password},
datetime/now, network/{ip,dns}, text/uuid) — remains wired via
`toolPages()`/`toolPageHandler`/composite `pageTemplates` keys in
server.go, unchanged. Most of the ~240 linked sub-tool paths still have
neither a template nor a route because they need net-new backend
services (not just a template) — that is a larger, separate body of work:
confirm which remaining sub-tools need new services vs. are simply
unimplemented per IDEA.md non-goals, then design/build backend + template
+ `toolPages()` entry together, one commit per tool or small logical
group. Also unresolved: the 7 originally-wired tool templates (e.g.
tools/crypto/hash.tmpl) contain pre-existing inline `onclick="..."`
attributes, which violates frontend-rules.md's no-inline-JS rule —
track/fix as part of this same body of work or a dedicated
frontend-compliance pass.

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
