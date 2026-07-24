## [ ] Wire the 18 orphaned page templates into initTemplates()/routes
Read: AI.md PART 16 (frontend), src/server/server.go initTemplates()
18 templates under src/server/template/page/ (categories, convert, dev,
docker, fun, generate, geo, image, language, lorem, math, network,
osint, parse, research, system, testing, validate, weather) plus
page/tools/ exist on disk but have no handler/route wiring them up,
unlike text/crypto/datetime which already follow the working pattern
(textPageHandler, cryptoPageHandler). Needs a handler func + route
registration + pageTemplates entry per page, one commit per page or
small logical group.

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
