## [ ] Wire per-tool detail pages linked from the 21 category pages
Read: AI.md PART 16 (frontend), src/server/template/page/*.tmpl
Each of the 21 category pages (text, crypto, datetime, network, convert,
dev, docker, fun, generate, geo, image, language, lorem, math, osint,
parse, research, system, testing, validate, weather) links to ~12
per-tool detail pages (e.g. /crypto/hash, /network/dns, /text/uuid) per
the PART 16 frontend-route-mirrors-API-route rule. The first batch — the
7 tools that already had templates on disk (crypto/{hash,jwt,password},
datetime/now, network/{ip,dns}, text/uuid) — is now wired via
`toolPages()`/`toolPageHandler`/composite `pageTemplates` keys in
server.go. Most of the ~240 linked sub-tool paths still have neither a
template nor a route. Remaining work: confirm which sub-tools map to
existing API handlers (api_utils.go and friends) vs. need new services,
then create templates + extend `toolPages()` + wire in small batches, one
commit per tool or small logical group. Also unresolved: the 7 wired
tool templates (e.g. tools/crypto/hash.tmpl) contain pre-existing inline
`onclick="..."` attributes, which violates frontend-rules.md's no-inline-
JS rule — track/fix as part of this same body of work or a dedicated
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
