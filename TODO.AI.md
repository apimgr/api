## [ ] Wire apiPlaceholderHandler routes to their real src/service/* implementations
Read: AI.md PART 14 (API structure, {ok,data}/{ok,error,message} envelope), IDEA.md lines 3/36/86-112
16 routes at src/server/server.go:1385 currently return a placeholder
instead of calling the already-implemented src/service/{math,docker,
weather,geo,convert,generate,validate,parse,language,test,osint,
research,fun,lorem,dev,image} packages. Each needs: request parsing,
a call into the matching service package, and a PART 14 envelope
response, following the existing api_network.go pattern. One commit
per service (or small logical group) per findings-based commit rules.

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
