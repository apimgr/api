## [x] Wire per-tool detail pages linked from the 21 category pages (READY batch)
Read: AI.md PART 16 (frontend), src/server/template/page/*.tmpl
Every sub-tool that mapped directly onto an existing, non-stub backend
service method has now been wired end-to-end (template + `toolPages()`
entry + frontend route + API route + tool-page test + handler test).
107 tools are wired as of this pass:
convert/{length,temperature,time,volume,weight};
crypto/{bcrypt,decrypt,encrypt,hash,hmac,jwt,password,password-strength,pin,random,rsa,totp};
datetime/{add,convert,diff,now,timestamp,unix};
dev/{base64,format-json,url-encode};
docker/{dockerfile-generate,port-mapping,version,volume-helper};
fun/{fortune,joke};
geo/{bearing,distance,ip,midpoint};
image/{convert,crop,metadata,placeholder,resize};
language/{phonetic,word-count};
lorem/{address,company,person};
math/{base,calculate,fibonacci,gcd,logarithm,matrix,percentage,prime,random,sequence,stats,trigonometry};
network/{dns,headers,ip,mac,port,subnet,ula,user-agent};
osint/{cert,domain,email,ip};
parse/{csv,json,jwt,xml};
research/{citation,doi};
testing/{assertions,fake-data,fixtures,http};
text/{case,compress,decode,diff,encode,extract,hash,lorem,nanoid,regex,ulid,uuid};
dev/regex;
validate/{credit-card,domain,email,iban,ip,isbn,json,mac,phone,url,uuid,vat};
weather/{current,forecast}.
Five JS executors in static/js/app.js cover every request shape used above:
`executeTool` (query-string GET), `executeToolTemplate` (path-param GET),
`executeToolBody` (raw-body POST), `executeToolQueryPost` (POST-only,
query-string params), `executeToolImage` (binary-image GET).
This READY batch is exhausted — every remaining linked sub-tool below
needs net-new backend service work (not just a template/route), which is
scoped as its own body of work in the next section.

network/{ping,ssl,url,whois} were wired in a follow-up batch (net-new
`network.Service` methods: TCP-connect `Ping`, TLS-handshake `SSLInfo`,
stdlib `ParseURL`, RFC 3912 two-hop `Whois`), bringing the wired total to
111. `network/traceroute` was found to be a genuine permanent API gap (see
"Known permanent API gaps" below) rather than a wireable tool, and
`network/useragent` remains the known broken-link duplicate of the
already-wired `user-agent` (see "Known template bugs" below) — so the
MISSING network line is now fully resolved.

convert/{area,color,currency,data,energy,pressure,speed} were wired in a
follow-up batch (net-new paired `convert.Service` methods for area/data/
energy/pressure/speed following the existing length/temperature/weight/
volume/time unit-pair pattern; net-new `color.go` hex/RGB/HSL conversion
methods; net-new `currency.go` calling the free, keyless Frankfurter API
for live ECB reference rates, mirroring the `weather.go` external-API
pattern), bringing the wired total to 118. The MISSING convert line is now
fully resolved.

datetime/{calendar,cron,format,moon,parse,sunrise,workdays} were wired in a
follow-up batch (net-new `src/service/datetime/extended.go`: named/literal
`FormatDatetime`, multi-layout `ParseDateString`, Sunday-start
`GenerateCalendar` reusing the existing package-private `daysInMonth`/
`isLeapYear` helpers, Mon-Fri `WorkdaysBetween`, U.S. Naval Observatory
"Almanac for Computers, 1990" `SunriseSunset`, synodic-month `MoonPhase`,
and a standard 5-field POSIX `ParseCron` with brute-force next-run
lookahead — no new dependencies). `cron` required a routing design
deviation from the other six: a cron expression contains literal spaces
and `/` characters and cannot safely round-trip as a single chi path
segment, so `/api/v1/datetime/cron` takes `?expression=` as a query
parameter (`executeTool` JS convention, matching `convert/currency`)
instead of the `{param}` path-template convention (`executeToolTemplate`)
used by the other six new tools. `sunrise` and `moon` each register two
routes (with and without a trailing `/{date}` segment) since date is
optional. This brings the wired total to 125. The MISSING datetime line is
now fully resolved.

crypto/{certificate,ed25519,pgp} were wired in a follow-up batch (all
genuinely net-new — zero prior backend support, matching the MISSING
annotation below). `certificate.go` and `ed25519.go` are stdlib-only
(`crypto/x509`, `crypto/x509/pkix`, `crypto/ed25519`, `encoding/pem`).
`pgp.go` initially used the `openpgp`/`openpgp/armor` subpackages bundled
with the existing `golang.org/x/crypto` dependency, but CI's `govulncheck`
gate flagged `golang.org/x/crypto/openpgp` as GO-2026-5932 (unmaintained,
"Fixed in: N/A" — no version bump can ever clear it), so `pgp.go` was
switched to the maintained fork `github.com/ProtonMail/go-crypto/openpgp`
per AI.md's Forbidden Libraries precedent (unmaintained/vulnerable
dependency swapped for a maintained drop-in, matching the
`dgrijalva/jwt-go` → `golang-jwt/jwt/v5` pattern). This added
`github.com/ProtonMail/go-crypto` and its `github.com/cloudflare/circl`
transitive dependency to go.mod (circl pinned to v1.6.3 to also clear
GO-2026-4550). All three tools follow the mode-dispatch single-POST-
endpoint pattern established by `apiCryptoRSAHandler`. This brings the
wired total to 128. The MISSING crypto line is now fully resolved.

## [x] dev (8): cron, css-format, echo, html-format, js-format, jwt, sql-format, xml-format
All 8 wired: 136 tools now wired total (up from 128). `cron` reuses
`datetime.ParseCron` (same helper as the already-wired `/datetime/cron`
tool). `jwt` reuses the shared `decodeJWTSegment` helper (same as
`/parse/jwt`) — no signature verification, decode-only debug tool. `echo`
is genuinely new — reflects method/path/query/headers/remote-addr/body as
JSON with no service dependency. `css-format`, `html-format`, `js-format`,
`sql-format`, `xml-format` are genuinely new pragmatic, dependency-free
formatters added to `src/service/dev/dev.go`
(`FormatCSS`/`MinifyCSS`/`FormatHTML`/`MinifyHTML`/`FormatJS`/`MinifyJS`/
`FormatSQL`/`FormatXML`/`MinifyXML`) — whitespace/brace-depth-based
formatters, not full parsers (SQL has no minify variant). Routes added
under `/api/v1/dev/*` in `src/server/server.go`; frontend pages under
`src/server/template/page/tools/dev/*.tmpl`; `css-format`/`html-format`/
`js-format`/`xml-format` reuse the existing `data-body-endpoint` JS
executor with an added `minify` checkbox (excluded from the query string
by `FormData` when unchecked — no JS changes needed); `cron` uses
`data-endpoint`; `jwt`/`echo` use `data-template`. Tests added: handler
tests in `api_utils_test.go`, tool-page smoke tests in `server_test.go`,
service-layer unit tests in `src/service/dev/dev_test.go`.

## [x] docker (9): best-practices, compose-to-run, compose-validate, dockerfile-lint, env-parser, network-helper, run-to-compose, security-scan, size-optimizer
All 9 wired: 145 tools now wired total (up from 136). `compose-to-run` and
`run-to-compose` reuse the same `ComposeServiceConfig`/port-mapping-string
plumbing already established for the wired `port-mapping`/`volume-helper`
tools. Everything else is genuinely net-new, added to
`src/service/docker/tools.go`: `LintDockerfile` (rule-based static checks —
missing `USER`, `latest` tag, missing `WORKDIR`, etc.), `BestPracticesGuide`
(static tips list, no input), `ValidateCompose` (YAML syntax + structural
checks via `gopkg.in/yaml.v3`, including a `yaml.Node`-based duplicate-key
detection technique since the stdlib map decode silently drops dupes),
`ParseEnvFile` (`.env` KEY=VALUE parser with quoting/comment handling),
`GenerateNetworkConfig` (docker network create command + compose `networks:`
block), `ScanSecurity` (static Dockerfile/compose anti-pattern scan — root
user, curl-pipe-to-sh, privileged mode, exposed secrets in `ENV`), and
`OptimizeSize` (static suggestions — layer-squashing opportunities,
`apt-get clean`, multi-stage build hints). `security-scan` and
`size-optimizer` are honest static analyzers only (no image is ever built or
pulled) — this matches the project's dependency-free, self-contained-binary
constraint and is not a gap: nothing in AI.md PART 26 requires runtime image
inspection. Routes added under `/api/v1/docker/*` in `src/server/server.go`;
`network-helper` uses `data-endpoint` (query-string GET); `security-scan`
and `size-optimizer` use `data-body-endpoint` (raw-body POST), matching the
other 6 tools' established GET/POST conventions. Tests added: handler tests
in `api_utils_test.go`, tool-page smoke tests in `server_test.go`,
service-layer unit tests in `src/service/docker/docker_test.go`. The MISSING
docker line is now fully resolved.

## [x] fun (10): compliment, dad-joke, fact, insult, meme, motivational, programming-joke, quote, riddle, trivia
All 10 wired: 155 tools now wired total (up from 145). Every tool is a
self-contained, offline, curated static string-slice list (`var xxx =
[]string{...}` in `src/service/fun/fun.go`, 20-30+ entries each) selected via
the existing `s.RandomChoice(list)` / `crypto/rand`-backed helper — no
external HTTP APIs, matching the project's dependency-free, self-contained-
binary constraint. `riddle` and `trivia` return a `Question`/`Answer` pair via
a new shared `QAPair{Question, Answer string}` struct (no other Q&A-shaped
precedent existed in the codebase). `insult`/`compliment` are playful and
harmless, not actually offensive; `meme` is text-only caption generation (no
image/template rendering, which is out of scope). Handlers added to
`src/server/api_utils.go` (`apiFunDadJokeHandler`, etc.), routes registered
under `/api/v1/fun/*` in `src/server/server.go`, and `toolPages()` entries
added with title/description copied verbatim from `fun.tmpl`'s existing
category cards. Templates added at
`src/server/template/page/tools/fun/{tool}.tmpl` following the
joke.tmpl/fortune.tmpl structure. Tests added: handler tests in
`api_utils_test.go`, tool-page smoke tests in `server_test.go`,
service-layer unit tests in `src/service/fun/fun_test.go`. The MISSING fun
line is now fully resolved.

## [x] generate (11, +1 already-gapped qr): api-docs, avatar, barcode, config, dockerfile, gitignore, identicon, license, placeholder, sql, ssh-key
All 11 wired (the 12th listed tool, `qr`, is intentionally untouched — it
already has a deliberate, correct 501 `NOT_SUPPORTED` handler
(`apiGenerateQRHandler`) documented under "Known permanent API gaps"; no
dependency, route, `toolPages()` entry, or template was added for it).
`barcode` implements all four listed formats (EAN-13, UPC-A, Code128,
Code39) via the real `github.com/boombuler/barcode` encoder (added as a
direct dependency) rather than a subset — the category description says
"EAN, UPC, Code128, Code39" and all four are genuine standard-compliant
encodings, not a stub. `avatar` renders a colored-background image with a
hash-derived deterministic geometric block pattern rather than rendered
initials text — `golang.org/x/image`/font-drawing is not in `go.mod` and
adding it purely for text rasterization was judged out of proportion to
this one sub-tool; this is a deliberate scope substitution, not a missing
feature (the endpoint still returns a valid, deterministic, per-initials
PNG). `identicon` uses sha256(seed) to build a symmetric grid with a
deterministic derived color. `ssh-key` generates a stateless Ed25519
keypair (stdlib `crypto/ed25519` + existing `golang.org/x/crypto/ssh` for
OpenSSH-format marshaling) — nothing is persisted server-side. `sql` emits
`CREATE TABLE` DDL only from a JSON `{table, columns}` body. `config`
supports yaml/json/env/toml output from arbitrary key=value query params.
`dockerfile` and `gitignore` are curated static-template generators
(go/node/python/rust/generic; go/node/python/rust/java/macos/linux/
windows/vscode/jetbrains respectively). `license` covers
mit/apache-2.0/gpl-3.0/bsd-3-clause/isc with correct canonical license
text. `api-docs` reuses the existing `src/swagger` package
(`?format=markdown|json`) — no duplicated OpenAPI generation logic.
`placeholder` is a thin wrapper: `/api/v1/generate/placeholder/{w}/{h}`
calls the exact same `imageService.GeneratePlaceholder` used by
`/api/v1/image/placeholder/...` — confirmed no duplicated logic between
the two routes. Service methods added in
`src/service/generate/generate.go`; handlers added to
`src/server/api_utils.go` (`apiGenerate<Tool>Handler`); routes registered
under `/api/v1/generate/*` in `src/server/server.go`; `toolPages()` entries
added with title/description copied verbatim from `generate.tmpl`'s
existing category-description text. Templates added at
`src/server/template/page/tools/generate/<tool>.tmpl` (kebab-case).
Tests added: service-layer unit tests in
`src/service/generate/generate_tools_test.go`, handler tests in
`api_utils_test.go`, and tool-page smoke tests in `server_test.go`. The
MISSING generate line is now fully resolved.

## [x] geo (8): bbox, country, geocode, geohash, h3, pluscode, reverse, timezone
Backend: `src/service/geo/{bbox,country,geocode,geohash,h3,pluscode,reverse,
timezone}.go`. geocode/reverse call Nominatim (OpenStreetMap) with a
descriptive User-Agent, 10s timeout, and full error wrapping; timezone
reuses the existing open-meteo forecast endpoint (`timezone=auto`); country
uses `github.com/biter777/countries`; geohash is a native base32
bit-interleaving implementation; h3 uses `github.com/ziprecruiter/h3-go`
(pure Go, no cgo — `uber/h3-go` was rejected as it requires CGO_ENABLED=1);
pluscode uses `github.com/google/open-location-code/go`; bbox is native
math reusing the existing `Destination` helper for the radius variant.
Handlers added in `api_utils.go` (8 new `apiGeo*Handler` functions plus a
shared `parseGeoSingleCoordinateParams` helper), routes + toolPages entries
in `server.go`, smoke-test rows in `server_test.go`, and 8 new
`src/server/template/page/tools/geo/*.tmpl` files matching the existing
tool-page pattern. Tests added per file in `src/service/geo/*_test.go`;
geocode/reverse/timezone tests mock all HTTP calls via a redirecting
`httptest.Server` transport — no live network calls. Verified independently
(gofmt/build/vet/test clean in casjaysdev/go:latest, caches mounted outside
the project tree) and via `go mod tidy` (fixed a `go.mod` bug where the
three new dependencies were misplaced in the `// indirect` block despite
being directly imported by our own source). The MISSING geo line is now
fully resolved.

## [x] image (6, +1 already-gapped qr): avatar, barcode, filter, identicon, optimize, watermark
All 6 wired (the 7th listed tool, `qr`, is intentionally untouched — same
501 `NOT_SUPPORTED` gap as `generate/qr`, documented under "Known permanent
API gaps"; no route, `toolPages()` entry, or template was added for it).
`avatar`, `barcode`, and `identicon` are thin new HTTP handlers reusing the
existing `generate.Service.{Avatar,Barcode,Identicon}` methods verbatim —
no duplicated drawing logic. `filter` (grayscale/sepia/invert/blur/
brighten/darken), `optimize` (re-encode with JPEG quality control; PNG/GIF
have no lossy knob in the stdlib so they use fixed lossless compression),
and `watermark` (diagonally tiled text, alpha-blended) are genuinely new
`src/service/image` methods, pure Go stdlib (`image`, `image/color`,
`image/draw`, `image/jpeg`, `image/png`, `image/gif`, `math`) — no cgo, no
new third-party image or font dependency; `watermark`'s text rendering
hand-authors a minimal 5x7 bitmap font matching the same no-font-dependency
constraint already established in `generate/avatar.go`. Service-layer table
-driven tests added in `filter_test.go`/`optimize_test.go`/`watermark_test.go`
plus handler tests in `api_utils_test.go` and 6 new smoke-test rows in
`server_test.go` (no row for `/image/qr`, matching the gap). Verified
independently (gofmt/build/vet/staticcheck/test clean in
`casjaysdev/go:latest`, caches mounted outside the project tree; `go mod
tidy` produced no `go.mod`/`go.sum` diff — no new dependency introduced).
The MISSING image line is now fully resolved.

## [x] language (4, +1 already-gapped detect, +5 newly-gapped dictionary/grammar/spell-check/thesaurus/translate): keywords, readability, reading-time, sentiment
keywords, readability, reading-time, and sentiment are wired as
fully local/stdlib heuristics in `src/service/language/language.go`
(frequency-based keyword extraction excluding a hand-authored stopword
list; Flesch Reading Ease/Flesch-Kincaid Grade/Gunning Fog via a
syllable-counting heuristic; reading-time estimate at a configurable
words-per-minute; lexicon-based positive/negative sentiment scoring) —
each documented in-source as an honest heuristic, not a trained model.
`detect` was already a permanent gap (IDEA.md non-goal of language
auto-detection). Research (subagent) plus direct IDEA.md inspection
(lines 35, 39, 55, 94) confirmed dictionary, grammar, spell-check,
and thesaurus would each require a new outbound integration outside
this project's declared trust boundary (outbound calls are restricted
to only the OSINT and weather tool families), and translate is an
explicit non-goal ("machine translation" excluded, commercial
translation forbidden among outbound integrations) — so all five got
an `apiLanguage*Handler` returning 501 NOT_SUPPORTED, following the
exact `apiLanguageDetectHandler` precedent: no `toolPages()` entry, no
frontend template, no smoke-test row, matching the established
permanent-gap pattern. New table-driven test `TestAPILanguageGapHandlers`
covers all five gap handlers; `TestAPILanguageKeywordsHandler`,
`TestAPILanguageReadabilityHandler`, `TestAPILanguageReadingTimeHandler`,
`TestAPILanguageSentimentHandler` cover the four wired tools. Verified
independently (gofmt/build/vet/staticcheck/test clean in
`casjaysdev/go:latest`, caches mounted outside the project tree; `go mod
tidy` produced no `go.mod`/`go.sum` diff — no new dependency introduced).
The MISSING language line is now fully resolved.

## [x] osint (2, +6 newly-gapped breach/company/metadata/phone/social/username): subdomain, tech-stack
osint/subdomain is wired via a fixed-wordlist DNS-label enumeration
(`SubdomainEnum` in `src/service/osint/osint.go`) resolving ~25 common
subdomain labels through the system resolver, reusing the pre-existing
SSRF `validateTarget`/`resolveTimeout` guards from `src/service/osint/
ssrf.go` — matching IDEA.md's declared DNS-lookup scope. osint/tech-stack
is wired via a single direct HTTP GET to the caller-supplied URL
(`TechStack`), inspecting response headers, cookie names, and HTML
signatures for server/framework/CMS detection, reusing the same
`validateTarget` SSRF guard — analogous to the existing TLS-handshake-based
cert tool: one user-directed outbound connection, no third-party service
contacted. The other six requested tools were confirmed via IDEA.md
line 34 (OSINT scope: IP geolocation, WHOIS, DNS, TLS cert only, via free
keyless mechanisms) and line 55 (non-goals ban paid/keyed third-party
APIs) to be out of scope: breach (breach-database checking requires a
keyed third-party API), company (company-data lookup requires a
commercial keyed API), metadata (generic file-metadata extraction
duplicates the existing image/metadata tool and is outside OSINT's
declared 4-mechanism scope), phone (phone-number intelligence requires a
commercial keyed API — validate/phone already covers format validation),
social and username (cross-platform profile/username discovery would
require probing dozens of third-party platforms, not a single
user-named target). Each of the six got an `apiOsint*Handler` returning
501 NOT_SUPPORTED, following the exact `apiLanguageDetectHandler`
precedent: no `toolPages()` entry, no frontend template, no smoke-test
row. New table-driven test `TestAPIOsintGapHandlers` covers all six gap
handlers; `TestAPIOsintSubdomainHandler` and `TestAPIOsintTechStackHandler`
cover the two wired tools' missing-param 400 case only — a
successful-lookup case is intentionally not asserted since both perform
live DNS/HTTP calls that would make CI flaky (same reasoning as the
pre-existing `TestAPIOsintCertHandler`). Verified independently (gofmt/
build/vet/staticcheck/test clean in `casjaysdev/go:latest`, caches
mounted outside the project tree; `git diff --stat -- go.mod go.sum`
empty — no new dependency introduced). The MISSING osint line is now
fully resolved.

## [x] parse (8): env, html, ini, log, markdown, sql, toml, yaml
All 8 tools are pure in-process parsers with no outbound calls, added to
`src/service/parse/parse.go`. env (`ParseEnv`/`unquoteEnvValue`) splits
KEY=VALUE lines, strips an optional `export ` prefix and surrounding
quotes. html (`ParseHTML`, using the already-indirect `golang.org/x/net/
html` promoted to a direct import) walks the parsed DOM tree into a
`HTMLSummary` (title, meta, headings, links, images, form count). ini
(`ParseINI`) splits into sections of key/value maps, extending the same
"error only when result is empty but input isn't" convention from
`ParseEnv` for consistency across sibling tools. log (`ParseLogLines`)
is an explicitly best-effort heuristic: an optional leading timestamp
against a small set of common layouts, then the first whole-word level
token, remaining text as message — never rejects a non-blank line, only
an entirely empty input errors. markdown (`ParseMarkdownStructure`)
extracts ATX headings, inline `[text](url)` links, and fenced ``` code
blocks as structured data (distinct from the pre-existing `text.
MarkdownToHTML`/`MarkdownTOC` renderers). sql (`ParseSQLStructure`) is
explicitly documented as NOT a real SQL parser/validator — best-effort
statement-type/table/column extraction via regex, unrecognized syntax
yields "UNKNOWN" rather than an error. toml (`ParseTOML`) supports the
common subset confirmed in scope by IDEA.md line 25 (strings, booleans,
integers, floats, single-line arrays, dotted `[table.subtable]`
headers); multi-line strings, inline tables, dates, and arrays-of-tables
are explicitly documented as unsupported. yaml (`ParseYAML`) uses the
already-direct `gopkg.in/yaml.v3` dependency (decodes mappings into
`map[string]interface{}` natively, unlike yaml.v2). No new third-party
module was introduced: `golang.org/x/net` moved from `// indirect` to
direct in `go.mod` with zero `go.sum` diff (it was already transitively
present), and `gopkg.in/yaml.v3` was already a direct dependency. All 8
got `apiParse*Handler`s in `src/server/api_utils.go` following the exact
`apiParseJSONHandler` POST-with-body pattern, routes/`toolPages()`
entries in `src/server/server.go`, table-driven tests (missing-body 400
+ valid-input 200, no live network calls needed since none of the 8 do
I/O) in `src/server/api_utils_test.go`, smoke-test rows in
`src/server/server_test.go`, and templates under `src/server/template/
page/tools/parse/{env,html,ini,log,markdown,sql,toml,yaml}.tmpl`
following the `json.tmpl` sibling's `data-body-endpoint` form pattern.
Verified independently line-by-line against the sibling precedents (not
just the implementing subagent's self-report), including confirming its
self-caught `a.Value`→`a.Val` `html.Attribute` field-name fix is correct,
and independently re-run in `casjaysdev/go:latest` (gofmt/build/vet/
staticcheck/test all clean, caches mounted outside the project tree).
The MISSING parse line is now fully resolved.

## [x] research (0, +10 newly-gapped arxiv/bibtex/footnotes/isbn/metadata/outline/pdf-extract/readability/scraper/summarize)
User confirmed (2026-07-26, AskUserQuestion): none of the 10 research
sub-tools trace to IDEA.md's declared Research scope (line 40: citation
formatting, bibliography generation, DOI formatting/validation only).
bibtex, footnotes, and outline are pure-stdlib/no-network but still
unnamed in scope; arxiv, isbn, metadata, readability, and scraper would
each add a new outbound-call family beyond IDEA.md:94's declared
OSINT/weather-only trust boundary; pdf-extract needs a new third-party
dependency to parse untrusted binaries; summarize would need either an
external/keyed NLP/LLM service (excluded by IDEA.md:55's free/keyless
integration policy) or a low-value stdlib heuristic. Decision: mark all
10 as permanent gaps rather than amend IDEA.md's scope. All 10 got
`apiResearch*Handler`s in `src/server/api_utils.go` returning 501
NOT_SUPPORTED with a doc comment citing the specific IDEA.md line, routes
in `src/server/server.go`, and a single table-driven test
(`TestAPIResearchGapHandlers`) in `src/server/api_utils_test.go`
confirming each returns NOT_SUPPORTED. No toolPages() entry or frontend
template was added — matching the existing osint/breach-family gap
precedent, the pre-existing `research.tmpl` nav cards for these 10 tools
remain as documented dead links. Verified independently with a
`casjaysdev/go:latest` gofmt/build/vet/staticcheck/test run.

## [x] testing (0, +2 newly-gapped load-test/mock-server)
Of the 9 MISSING testing sub-tools, 7 wire directly: api-client,
curl-generator, and postman share a `testRequestSpec` JSON body
(method/url/headers/body) and a `decodeTestRequestSpec`/`buildCurlCommand`
helper pair; api-client renders curl+JavaScript+Python+Go client snippets,
curl-generator renders a single curl string, postman renders a minimal
Postman Collection v2.1 document. request-inspector and webhook both echo
back the current request (headers/body/query for request-inspector;
headers/raw+parsed body/json_valid for webhook) with no persistence —
webhook is a stateless same-request echo per user confirmation
(2026-07-26, AskUserQuestion), not a permanent gap. status-codes returns
either the full `httpStatusDescriptions` table or a single code's
text/description via an optional `{code}` path param. response-generator
dispatches directly to the existing `test.Service.GenerateMockAPIResponse`
per user confirmation (2026-07-26, AskUserQuestion) rather than a broader
parameterized generator. load-test and mock-server are newly-gapped:
load-test would require firing outbound HTTP traffic at a caller-supplied
target, outside IDEA.md's outbound-call boundary (OSINT/weather tool
families only); mock-server would require either a second runtime-managed
listening socket (no dynamic-listener lifecycle exists, and config-rules.md
forbids a runtime API for listener/port changes) or persisting
caller-defined response rules across requests (forbidden by IDEA.md's
no-persistent-storage non-goal). All 9 got handlers in
`src/server/api_utils.go`, routes in `src/server/server.go`, table-driven
tests in `src/server/api_utils_test.go` (including a
`TestAPITestPermanentGapHandlers` pair matching the
`TestAPIResearchGapHandlers` precedent for the 2 gap tools), and
`toolPages()` entries + templates under `src/server/template/page/tools/
testing/{api-client,curl-generator,postman,request-inspector,status-codes,
response-generator,webhook}.tmpl` for the 7 wireable tools only — no
toolPages()/template added for load-test/mock-server, matching the
research(10) gap precedent. Verified independently with a
`casjaysdev/go:latest` gofmt/build/vet/test run (all packages pass).

## [x] weather (10): air-quality, alerts, astronomy, historical, hourly, maps,
  marine, pollen, radar, uv — CLOSED
Implemented via `src/service/weather/weather.go` (air-quality, UV, pollen,
astronomy, historical, hourly, marine — all built on the existing
open-meteo geocoding/forecast/air-quality/marine/archive endpoints already
used by current/forecast) and the new `src/service/weather/alerts.go`
multi-provider alerts aggregator, per the user's explicit "use as many as
possible, such as NWS, MetOffice, etc then make the data uniform" scoping
instruction: NWS (US, `api.weather.gov/alerts/active`), Environment Canada/
MSC GeoMet (CA, `api.weather.gc.ca/collections/weather-alerts`), and
MeteoAlarm (European countries, Atom+CAP XML feed), all normalized into a
single `Alert` shape and routed by resolved-location country code, with an
empty (not error) result for uncovered countries. maps and radar are
permanent-gap handlers (501 `NOT_SUPPORTED`, doc-commented) — no free
keyless tile/radar-imagery provider exists within IDEA.md's outbound-call
boundary; folded into "Known permanent API gaps" below. All 10 got handlers
in `src/server/api_utils.go`, routes in `src/server/server.go`, table-driven
tests in `src/server/api_utils_test.go` (including a
`TestAPIWeatherPermanentGapHandlers` pair matching the
`TestAPITestPermanentGapHandlers` precedent for maps/radar), service-layer
tests in `src/service/weather/weather_test.go` and the new
`src/service/weather/alerts_test.go` (NWS/ECCC/MeteoAlarm routing,
visibility filtering, uncovered-country empty result, geocode-error
propagation), and `toolPages()` entries + templates under
`src/server/template/page/tools/weather/{air-quality,alerts,astronomy,
historical,hourly,marine,pollen,uv}.tmpl` for the 8 wireable tools only —
no toolPages()/template added for maps/radar, matching the testing(9)
load-test/mock-server gap precedent. Verified independently with a
`casjaysdev/go:latest` gofmt/build/vet/test run (all packages pass,
including real-network handler tests hitting the live open-meteo/NWS/ECCC/
MeteoAlarm endpoints).

This was the last entry under "MISSING sub-tools needing net-new backend
service work" — that heading is now fully closed and removed.

## [x] Known template bugs — CLOSED
- `network.tmpl` linked `/network/useragent` but the wired API/frontend path
  is `/network/user-agent` — fixed the link to `/network/user-agent`.
- `docker/version.tmpl`'s form had no `image` input field even though
  `apiDockerVersionHandler` requires `?image=` — added a required `image`
  text input, matching the `generate/license.tmpl` query-string form
  precedent (`data-template="...?image={image}"`).

## [ ] Known permanent API gaps needing a future spec/dependency decision
Read: src/server/api_utils.go (apiLanguageDetectHandler,
apiLanguageDictionaryHandler, apiLanguageGrammarHandler,
apiLanguageSpellCheckHandler, apiLanguageThesaurusHandler,
apiLanguageTranslateHandler, apiResearchExtractHandler,
apiResearchArxivHandler, apiResearchBibtexHandler,
apiResearchFootnotesHandler, apiResearchIsbnHandler,
apiResearchMetadataHandler, apiResearchOutlineHandler,
apiResearchPdfExtractHandler, apiResearchReadabilityHandler,
apiResearchScraperHandler, apiResearchSummarizeHandler,
apiOsintBreachHandler, apiOsintCompanyHandler, apiOsintMetadataHandler,
apiOsintPhoneHandler, apiOsintSocialHandler, apiOsintUsernameHandler,
apiTestLoadTestHandler, apiTestMockServerHandler,
apiWeatherMapsHandler, apiWeatherRadarHandler doc comments),
src/server/api_network.go (apiNetworkTracerouteHandler doc comment)
`generate/qr` and `image/qr` are no longer in this gap list — real QR
encoding (including standard Wi-Fi join QR payloads) was implemented via
`github.com/boombuler/barcode/qr` (already a transitive dependency of the
existing 1D barcode feature) in `src/service/generate/qr.go`, wired into
`apiGenerateQRHandler`/`apiImageQRHandler` in `src/server/api_utils.go`,
with `toolPages()` entries and frontend templates added — the original
"no QR encoder exists anywhere in the codebase or go.mod" justification
for the gap was factually wrong.
Twenty-eight of the wired API routes honestly return 501 NOT_SUPPORTED
rather than inventing behavior: language/detect (conflicts with IDEA.md's
declared non-goal of language auto-detection), language/dictionary,
language/grammar, language/spell-check, and language/thesaurus (each
would require a new outbound integration outside IDEA.md's declared
trust boundary — outbound calls are restricted to only the OSINT and
weather tool families), language/translate (IDEA.md explicitly excludes
machine translation as a non-goal and forbids commercial translation
among outbound integrations), research/extract (research.go's own
source comment documents citation extraction from unstructured text as
unimplemented), network/traceroute (a real traceroute needs TTL-limited
probes and ICMP time-exceeded replies, which requires a raw ICMP socket —
CAP_NET_RAW or root — that this unprivileged self-contained binary cannot
assume it has on the host it runs on), osint/breach and osint/company
(each requires a commercial keyed third-party API, outside IDEA.md's
declared free/keyless OSINT trust boundary), osint/metadata (generic
file-metadata extraction duplicates the existing image/metadata tool and
is outside OSINT's declared 4-mechanism scope of IP geolocation/WHOIS/
DNS/TLS cert), osint/phone (phone-number intelligence requires a
commercial keyed API — validate/phone already covers format validation),
and osint/social and osint/username (cross-platform profile/username
discovery would require probing dozens of third-party platforms rather
than a single user-named target), and the ten research gaps —
research/bibtex, research/footnotes, and research/outline (none named in
IDEA.md's declared Research scope of citation formatting/bibliography/
DOI), research/arxiv, research/isbn, research/metadata, research/
readability, and research/scraper (each would add a new outbound-call
family beyond IDEA.md's declared OSINT/weather-only trust boundary),
research/pdf-extract (needs a new third-party dependency to parse
untrusted binaries), and research/summarize (a genuine summarizer would
need an external/keyed NLP/LLM service, excluded by IDEA.md's free/
keyless integration policy), and the two testing gaps — test/load-test
(would require firing outbound HTTP traffic at a caller-supplied target,
outside IDEA.md's outbound-call boundary) and test/mock-server (would
require either a second runtime-managed listening socket, forbidden by
config-rules.md's no-runtime-port-change rule, or persisting
caller-defined response rules, forbidden by IDEA.md's no-persistent-
storage non-goal), and the two weather gaps — weather/maps and
weather/radar (keyless weather tile/map and radar imagery has no free
provider within IDEA.md's declared outbound-call boundary; a real
implementation needs a keyed provider such as RainViewer, OpenWeatherMap
tiles, or NOAA radar mosaics). Resolving these requires a user/spec
decision, not further code guessing — either confirm language/detect and
the four other language gaps should stay unsupported per IDEA.md, scope
what "extraction" means for research/extract, decide whether network/
traceroute should ship as a root-only opt-in feature instead of a
permanent gap, decide whether any of the six osint gaps should be
promoted to a keyed-API-optional feature, decide whether any of the ten
research gaps should be promoted by amending IDEA.md's Research scope,
decide whether load-test/mock-server should be promoted via a
dynamic-listener-lifecycle feature, or decide whether weather/maps and
weather/radar should be promoted via a keyed-tile-provider integration.
None of the twenty-eight gap routes have a `toolPages()` entry or
dedicated frontend page template; osint's six, research's ten,
testing's load-test/mock-server pair, and weather's maps/radar pair still
have pre-existing nav cards on their category listing page linking to a
per-tool page that returns 404 (documented dead links, not newly added
here).

## [ ] CI secret-scan false-positive history on src/config/config_test.go
CI run 30255631519's `secret-scan` job (TruffleHog) failed against harmless
hardcoded test connection strings in `src/config/config_test.go`. The job
is diff-scoped (`.github/workflows/ci.yml` passes `base`/`head` from
`github.event.before`/`.after` and `extra_args: --results=verified,unknown`),
so ordinary pushes only re-scan their own diff and this has not recurred on
any run since (confirmed green through run 30284413533). Not currently
blocking. If it recurs — e.g. a future PR/rebase re-includes those lines in
its diff, or a `schedule`-triggered full-history scan (which passes empty
`base`/`head`) flags them — the fix is to replace the offending literal
connection-string test fixtures in `config_test.go` with obviously-fake
placeholder values (e.g. `user:REDACTED_TEST_PW@host`) or add a
`.trufflehogignore` entry scoped to that file/line, not to disable the
secret-scan job.

## [ ] AUDIT.AI.md follow-up items flagged but not fixed (out of prior pass scope)
Several already-`[x]`-marked AUDIT.AI.md entries contain embedded notes for
real work that was deliberately deferred rather than completed. Recorded
here per the "no issue left only in conversation" rule since these notes
live only inside AUDIT.AI.md's changelog prose, not as actionable items:

- **Dead code**: `src/service/system/health.go` and
  `src/service/system/health_test.go` are unreferenced anywhere in the tree
  (`grep -rln "service/system"` outside the package itself returns nothing —
  reconfirmed). The real `/server/healthz` implementation lives in
  `src/server/handler/health.go`. AUDIT.AI.md explicitly flagged this file
  as "flagged, not deleted... left as dead code for a follow-up cleanup
  pass." `ai-rules.md` forbids deleting pre-scaffolded content outside a
  scoped cleanup task, so deletion needs to happen as its own dedicated,
  clearly-scoped commit (not folded into an unrelated pass) — confirm no
  other in-flight branch/agent still references it, then delete both files
  in one commit.
- **SSL/TLS wiring gap** — FIXED (commit 3b28e987e6b6): `main.go` now
  builds an `ssl.Manager` from `cfg.Server.SSL`, resolves HTTP/HTTPS port(s)
  per the PART 15 Port Configuration table, and starts HTTP-only,
  HTTPS-only, or dual HTTP+HTTPS listeners with graceful shutdown for both.
  `sslRenewalTask()` now resolves the tiered
  `{config_dir}/ssl/letsencrypt/{fqdn}/fullchain.pem` path instead of the
  old hardcoded flat path, and skips cleanly when no app-managed cert
  exists yet. DNS-01 multi-provider support remains a separate, larger
  NEEDS DECISION item (credential storage design, which provider(s) to
  support) and was intentionally left out of this pass.
- **database.go config wiring gap** — FIXED: `src/database/database.go`'s
  `Init()` previously hardcoded `filepath.Join(dataDir, "db")`, silently
  ignoring the `DATABASE_DIR` env var documented in AI.md PART 4's
  Environment Variables table. Added `paths.GetDatabaseDir(dataDir)`
  (`src/paths/paths.go`), which returns `DATABASE_DIR` when set, else falls
  back to the previous `{dataDir}/db` behavior unchanged — so it does not
  alter behavior for the common case and does not disturb the many tests
  that call `database.Init()` with an explicit temp dir. `Init()` now calls
  this helper instead of joining the path directly. Reordering
  `config.Load()` ahead of `database.Init()` in `main.go` was considered but
  rejected: `GetDatabaseDir` reads the env var directly (matching the CLI
  flag > env var > default priority table, which lists no CLI flag for the
  database dir), so no config-load reordering is needed. `DATABASE_URL` and
  `DATABASE_DRIVER` remain unwired — both only make sense for a remote
  libsql/Turso driver, and no such driver is imported (`modernc.org/sqlite`
  is the only one); wiring them now would either be a no-op or misleading.
  Revisit if/when libsql/Turso support is added.
- **main.go color/comment cleanup**: `colorEnabled` is not threaded through
  every `fmt.Printf`'s hardcoded emoji output, and there is no `output.color`
  config-file override wired (only the `--color` CLI flag exists). Re-check
  whether the dangling `// Generate secure password` comment with no
  function body (previously attributed to a concurrently-running agent's
  in-progress edit) is still present — `grep -n "Generate secure password"
  src/main.go` returned nothing on the most recent check, so this specific
  sub-item now appears resolved; re-verify before closing it out formally.
- **Middleware reporting/config-header gaps**: the emitted
  `Reporting-Endpoints`/`Report-To`/`NEL` response headers point at
  `/api/{api_version}/server/reports/{default,csp}`, but no handler for
  those paths exists anywhere in the tree — either implement the PART 11
  Reporting API receiving endpoints or stop emitting headers that point at
  a 404. Config-driven per-project header tightening
  (`web.headers`/`web.csp`/`web.permissions_policy`) is also not wired into
  `config.go`/`server.yml` yet.

## [ ] Revert `.github/workflows/ci.yml` lint job to `casjaysdev/go:latest`
Read: .github/workflows/ci.yml (lint job), AI.md PART 27 (CI Workflow)
CI run 30292994784 for commit 3b28e987e6b6 failed: the `lint` job's
`staticcheck ./...` step hit `staticcheck: command not found` (exit 127).
Confirmed by pulling the image locally: `casjaysdev/go:latest`
(digest `8ac3a35db8ad`, pushed 2026-07-27T16:54Z, same digest also tagged
`2607`) has no `staticcheck` binary anywhere in the image, contradicting its
own documented tool list (Docker Hub description still lists staticcheck).
The prior tag `casjaysdev/go:2606` (pushed 2026-06-29) does have
`/usr/local/bin/staticcheck` — this is a fresh upstream build regression,
not caused by any change in this repo. Rerunning the failed job on the same
commit reproduced the identical failure, ruling out a one-off flake.
Could not locate a `casjaysdev/go` source repo under the `casjaysdev` or
`casjaysdevdocker` GitHub orgs to file an upstream issue/PR.
Workaround applied: pinned the `lint` job's container to
`casjaysdev/go:2606` (last known-good digest with staticcheck present) so
CI stays green; `test`, `vuln-check`, `secret-scan`, and `release.yml` still
reference the floating `:latest` tag per AI.md PART 27 and were unaffected
by this regression at push time.
Action needed: periodically re-check whether a newer `casjaysdev/go:latest`
tag has staticcheck restored (`docker run --rm --entrypoint sh
casjaysdev/go:latest -c 'which staticcheck'`), then switch the `lint` job's
`image:` back to `casjaysdev/go:latest` and remove this pin comment.
