## [ ] Wire per-tool detail pages linked from the 21 category pages
Read: AI.md PART 16 (frontend), src/server/template/page/*.tmpl
Each of the 21 category pages (text, crypto, datetime, network, convert,
dev, docker, fun, generate, geo, image, language, lorem, math, osint,
parse, research, system, testing, validate, weather) links to ~12
per-tool detail pages (e.g. /crypto/hash, /network/dns, /text/uuid) per
the PART 16 frontend-route-mirrors-API-route rule. Only a handful of
these have templates on disk today (src/server/template/page/tools/
{crypto,datetime,network,text}/*.tmpl covering hash/jwt/password, now,
ip/dns, uuid) and none are routed — all currently 404. Most of the
~240 linked sub-tool paths have neither a template nor a route. This is
separate, much larger scope than the category index pages (already
wired) and needs its own plan: confirm which sub-tools map to existing
API handlers (api_utils.go and friends) vs. need new services, then wire
templates + routes + pageTemplates entries in small batches, one commit
per tool or small logical group.

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
