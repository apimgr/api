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

**Amendment (2026-07-24):** the user authorized outbound calls to any free,
keyless, commercial-friendly external API across all tool categories, not
only OSINT/weather. `dictionary` and `thesaurus` were subsequently promoted
from 501 stubs to real handlers backed by the free Dictionary API and
Datamuse API respectively (see "Known permanent API gaps" section above for
the current record); `grammar`, `spell-check`, and `translate` remain
permanent gaps — the former two are unnamed in IDEA.md's declared Language
scope, and `translate` remains an explicit non-goal.

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

**Amendment (2026-07-24):** the user authorized outbound calls to any free,
keyless, commercial-friendly external API across all tool categories, not
only OSINT/weather. `arxiv` and `isbn` were subsequently promoted from 501
stubs to real handlers backed by the free arXiv query API and Open Library
Books API respectively (see "Known permanent API gaps" section above for the
current record); `bibtex`, `footnotes`, `metadata`, `outline`, `pdf-extract`,
`readability`, `scraper`, and `summarize` remain permanent gaps — the first
four are unnamed in IDEA.md's declared Research scope, `pdf-extract` still
needs a new third-party dependency, `summarize` still needs either a keyed
NLP/LLM service or a low-value heuristic, and `metadata`/`readability`/
`scraper` are excluded for the separate reason that they'd fetch an
arbitrary caller-supplied URL rather than query one fixed trusted provider.

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
apiLanguageGrammarHandler, apiLanguageSpellCheckHandler,
apiLanguageTranslateHandler, apiResearchExtractHandler,
apiResearchBibtexHandler, apiResearchFootnotesHandler,
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
`language/dictionary`, `language/thesaurus`, `research/arxiv`, and
`research/isbn` are also no longer in this gap list. The user explicitly
authorized outbound calls to any free, keyless, commercial-friendly
external API across all tool categories (not just OSINT/weather), so
these four were implemented for real: `language/dictionary` and
`language/thesaurus` call the free Dictionary API
(api.dictionaryapi.dev) and Datamuse (api.datamuse.com) respectively in
`src/service/language/language.go`; `research/arxiv` and `research/isbn`
call the free arXiv query API (export.arxiv.org) and Open Library Books
API (openlibrary.org) respectively in `src/service/research/research.go`.
All four got real `toolPages()` entries (no `reason` field), real
frontend templates, and real handlers in `api_utils.go` replacing the
prior 501 stubs. IDEA.md's trust-boundary table and Language/Research
scope bullets were updated to name the four new providers/tools. Note:
Datamuse's API requires a key starting 2027-01-01 per its own
documentation — `language.go` has a code comment tracking this; when
that date approaches, `Thesaurus` will need either a Datamuse key
(disqualifying it under the keyless-only policy) or a replacement
keyless synonym provider.
Twenty-four of the wired API routes honestly return 501 NOT_SUPPORTED
rather than inventing behavior: language/detect (conflicts with IDEA.md's
declared non-goal of language auto-detection), language/grammar and
language/spell-check (neither is named in IDEA.md's declared Language
scope of code/name lookup, listing, dictionary, and thesaurus),
language/translate (IDEA.md explicitly excludes machine translation as a
non-goal and forbids commercial translation among outbound integrations),
research/extract (research.go's own source comment documents citation
extraction from unstructured text as unimplemented), network/traceroute
(a real traceroute needs TTL-limited probes and ICMP time-exceeded
replies, which requires a raw ICMP socket — CAP_NET_RAW or root — that
this unprivileged self-contained binary cannot assume it has on the host
it runs on), osint/breach and osint/company (each requires a commercial
keyed third-party API, outside IDEA.md's declared free/keyless OSINT
trust boundary), osint/metadata (generic file-metadata extraction
duplicates the existing image/metadata tool and is outside OSINT's
declared 4-mechanism scope of IP geolocation/WHOIS/DNS/TLS cert),
osint/phone (phone-number intelligence requires a commercial keyed API —
validate/phone already covers format validation), and osint/social and
osint/username (cross-platform profile/username discovery would require
probing dozens of third-party platforms rather than a single user-named
target), and the eight remaining research gaps — research/bibtex,
research/footnotes, and research/outline (none named in IDEA.md's
declared Research scope of citation formatting/bibliography/DOI/arXiv/
ISBN), research/metadata, research/readability, and research/scraper
(none named in that same declared Research scope), research/pdf-extract
(needs a new third-party dependency to parse untrusted binaries), and
research/summarize (a genuine summarizer would need an external/keyed
NLP/LLM service, excluded by IDEA.md's free/keyless integration policy),
and the two testing gaps — test/load-test (would require firing outbound
HTTP traffic at a caller-supplied target, outside IDEA.md's outbound-call
boundary) and test/mock-server (would require either a second
runtime-managed listening socket, forbidden by config-rules.md's
no-runtime-port-change rule, or persisting caller-defined response
rules, forbidden by IDEA.md's no-persistent-storage non-goal), and the
two weather gaps — weather/maps and weather/radar (keyless weather
tile/map and radar imagery has no free, commercial-friendly provider
within IDEA.md's declared outbound-call boundary; a real implementation
needs a keyed provider such as OpenWeatherMap tiles or NOAA radar
mosaics — RainViewer was considered but disqualified, its free tier is
personal/non-commercial-use-only). Resolving these requires a user/spec
decision, not further code guessing — either confirm language/detect and
the remaining language gaps should stay unsupported per IDEA.md, scope
what "extraction" means for research/extract, decide whether network/
traceroute should ship as a root-only opt-in feature instead of a
permanent gap, decide whether any of the six osint gaps should be
promoted to a keyed-API-optional feature, decide whether any of the
eight remaining research gaps should be promoted by amending IDEA.md's
Research scope, decide whether load-test/mock-server should be promoted
via a dynamic-listener-lifecycle feature, or decide whether weather/maps
and weather/radar should be promoted via a keyed-tile-provider
integration.
All twenty-four gap routes now have a `toolPages()` entry (with a
per-tool `reason` string sourced from IDEA.md's scope/non-goal language)
and render through the shared `template/page/tools/unsupported.tmpl`
partial instead of 404ing — this satisfies PART 16's "frontend mirrors
API route" rule for routes that are wired but permanently return
`501 NOT_SUPPORTED`, without fabricating functionality IDEA.md excludes.
The underlying promotion decisions listed above (language/detect,
research/extract, network/traceroute opt-in, osint keyed-API promotion,
research scope amendment, load-test/mock-server dynamic-listener
feature, weather keyed-tile-provider integration) remain open and still
need a user/spec decision.

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

## [ ] Missing `.claude/rules/*.md` files (9 of 13 mandated files)
Read: AI.md PART 0
PART 0's `.claude/rules/` trigger condition mandates all 13 grouped
cheatsheet files exist. Only 4 exist today: `ai-rules.md` (PART 0, 1),
`project-rules.md` (PART 2, 3, 4), `config-rules.md` (PART 5, 6, 12),
`frontend-rules.md` (PART 16). Still missing, each requiring its source
PART(s) read first (never fabricate content without reading the spec):
`binary-rules.md` (PART 7, 8, 32), `backend-rules.md` (PART 9, 10, 11, 31),
`api-rules.md` (PART 13, 14, 15), `features-rules.md` (PART 17-22),
`service-rules.md` (PART 23, 24), `makefile-rules.md` (PART 25),
`docker-rules.md` (PART 26), `cicd-rules.md` (PART 27), `testing-rules.md`
(PART 28, 29, 30). A prior session's CLAUDE.md note explicitly deferred
these as "out of scope for this pass" — deferral confirmed still valid,
not silently dropped.

## [ ] Missing required `tests/` scripts (`run_tests.sh`, `docker.sh`, `incus.sh`)
Read: AI.md PART 3 (structure requirement), PART 28 (behavioral spec —
auto-detect Docker/Incus, `docker.sh` against `alpine:latest`, `incus.sh`
against `debian:latest` preferred for full systemd testing)
`tests/` currently contains only `.gitkeep` — the three REQUIRED
repository-root integration-test scripts from the PART 3 directory
structure table are absent. PART 28 defines their expected behavior in
more detail (see "Phase 2 — Binary Validation" table and "Typical
workflow" example around AI.md line 31599-31627) and must be read in full
before authoring them, since they must run against the compiled binary
(not `go test`) and pick the right container backend.

## [x] AUDIT.AI.md follow-up items flagged but not fixed (out of prior pass scope)
Several already-`[x]`-marked AUDIT.AI.md entries contain embedded notes for
real work that was deliberately deferred rather than completed. Recorded
here per the "no issue left only in conversation" rule since these notes
live only inside AUDIT.AI.md's changelog prose, not as actionable items:

- **Dead code** — FIXED: `src/service/system/health.go` and
  `src/service/system/health_test.go` were unreferenced anywhere in the tree
  (reconfirmed via `grep -rln "service/system" --include="*.go" .` outside
  the package itself, no matches). The real `/server/healthz`
  implementation lives in `src/server/handler/health.go`. Deleted both dead
  files (and the now-empty `src/service/system/` directory) in their own
  scoped commit per `ai-rules.md`.
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
- **main.go color/comment cleanup** — FIXED: two of the three original
  sub-claims were already stale (confirmed via grep before this fix: zero
  raw `fmt.Print*` calls in `src/main.go` bypass `cprintf`/`cprintln`, and
  the dangling `// Generate secure password` comment does not exist). The
  remaining genuine gap — no `output.color` config-file override wired, only
  `--color` CLI/`NO_COLOR` env/auto-detect — is now fixed per AI.md PART 8's
  4-tier priority order (CLI flag > config file > `NO_COLOR` env > auto-
  detect): added `config.OutputConfig` (`Color`/`Emoji` tri-state pointers)
  to `config.Config` in `src/config/config.go`; split `src/clihelpers.go`'s
  single `colorEnabled` into `colorEnabled` + `emojiEnabled`, with
  `applyColorMode(colorFlag, configColor *bool)` now taking the config-file
  value at its correct priority tier, and a new `applyEmojiOverride
  (configEmoji *bool)` implementing the spec's `output.emoji: true`
  override (forces emoji back on even when color/NO_COLOR disabled it).
  `main.go` calls `applyColorMode(*colorFlag, nil)` once before
  `config.Load()` (for CLI-only early-exit commands that never load config),
  then re-calls `applyColorMode(*colorFlag, cfg.Output.Color)` +
  `applyEmojiOverride(cfg.Output.Emoji)` right after `config.Load()`
  succeeds, so the config-file tier only applies on the full server-run
  path — no side effects (e.g. config-file creation) added to `--help`/
  `--version`/`--status`/etc. Not addressed here (separate, pre-existing
  gap, not part of this TODO item): the auto-detect tier's illustrative
  AI.md pseudocode also does a TTY check (`term.IsTerminal`), which
  `applyColorMode`'s auto-detect fallback still lacks (only `TERM=dumb` is
  checked) — logged as a new follow-up below.
- **applyColorMode auto-detect has no TTY check** — FIXED: added
  `term.IsTerminal(int(os.Stdout.Fd()))` to `applyColorMode`'s auto-detect
  fallback branch in `src/clihelpers.go`, checked ahead of `TERM=dumb`
  (matching AI.md PART 8's `ColorEnabled` pseudocode order), so output piped
  to a file/log now correctly disables color/emoji. `golang.org/x/term` was
  already a direct `go.mod` dependency (via the client TUI) — no dependency
  change needed.
- **Middleware reporting/config-header gaps**: the emitted
  `Reporting-Endpoints`/`Report-To`/`NEL` response headers point at
  `/api/{api_version}/server/reports/{default,csp}`, but no handler for
  those paths exists anywhere in the tree — either implement the PART 11
  Reporting API receiving endpoints or stop emitting headers that point at
  a 404. FIXED: added `src/server/reports.go` with a generic
  `POST /api/v1/server/reports/{name}` handler (registered in
  `src/server/server.go`) covering `default`/`csp`/`nel`/any future report
  name per PART 11's "all report endpoints share the same rules" — caps the
  body at 64KB, validates content type, logs an allowlisted field set via
  the existing `Logger.LogSecurity`, always responds 204 (log-don't-reject).
  Config-driven per-project header tightening
  (`web.headers`/`web.csp`/`web.permissions_policy`) is now wired into
  `config.go`/`server.yml`: `securityHeadersMiddleware` in
  `src/server/middleware.go` is fully config-driven off
  `cfg.Web.CSP`/`cfg.Web.Headers`/`cfg.Web.PermissionsPolicy` (CSP directive
  extra/override per-source, dev-mode report-only auto-degrade, HSTS,
  Permissions-Policy, cross-origin isolation headers, Sec-GPC/DNT opt-out
  logging), with `config.DefaultConfig()` exported for callers/tests.
  Deferred to separate follow-up items below (out of scope for this pass):
  CSP `connect-src`/`frame-ancestors`/`form-action` auto-detection from
  DOMAIN/reverse-proxy hosts, `Sec-Fetch-*` request validation, Clear-Site-
  Data emission, Server-Timing, and the IDEA.md → Header Tightening
  Auto-Map first-run pre-fill.

## [x] Security-header follow-ups deferred from config-header wiring pass
- [x] **CSP source auto-detection**: FIXED — new `src/server/origins.go`
  implements the AI.md PART 16 "CORS Allow-list Resolution Order" and its
  shared `{learned_origins}` set (steps 2+3: every `DOMAIN` env hostname as
  an `https://` origin, plus hostnames observed via `X-Forwarded-Host` from
  trusted proxies only, gated on `isTrustedPeer`/`trusted_proxies`). `web.cors`
  migrated from a bare string to a nested `config.CORSConfig{AllowedOrigins
  []string}` struct with a custom `UnmarshalYAML` that auto-migrates legacy
  `cors: "somevalue"` YAML into `cors: {allowed_origins: ["somevalue"]}` on
  load. `resolveCORSOrigins` implements the full 4-step order (explicit
  config; a sole `""` entry disables CORS and stops resolution; DOMAIN env;
  trusted-proxy-learned; default `*`), consumed by a new hand-rolled
  `corsMiddleware` in `src/server/cors.go` (replacing the static
  `go-chi/cors` wiring in `src/server/server.go`) that sends
  `Access-Control-Allow-Credentials: true` only when the resolved list is
  explicit, never with `*`. `securityHeadersMiddleware` in
  `src/server/middleware.go` now injects the same `learnedOrigins(cfg, r)`
  set (DOMAIN + proxy-learned only, excluding explicit config and the `*`
  fallback per spec) into `connect-src`, `frame-ancestors`, and
  `form-action` defaults, so the operator no longer has to list their own
  domain in `connect_src_extra`. Tests added in
  `src/server/origins_test.go`, `src/server/cors_test.go`, and
  `src/server/middleware_test.go` (resolution order, disabling, wildcard
  short-circuit, trusted-vs-untrusted-proxy learning, credentials-only-
  when-explicit, CSP directive reflection) and
  `src/config/config_test.go` (legacy bare-string → struct auto-migration).
  Verified in Docker (`casjaysdev/go:latest`): `gofmt -l .` clean,
  `go build ./...` and `go vet ./...` clean, `go test ./...` passes.
- [x] **`Sec-Fetch-*` request validation**: FIXED — added
  `secFetchValidationMiddleware` in `src/server/middleware.go`, a separate
  pre-handler middleware (not part of the response-header-emission
  `securityHeadersMiddleware`), gated on `cfg.Web.Headers.SecFetchValidation`
  and wired into the chain in `src/server/server.go` right after
  `securityHeadersMiddleware(cfg)`. Implements all 4 AI.md "Sec-Fetch-*
  Request Validation" checks with present-and-bad-only semantics (absent
  header always passes through): cross-site `Sec-Fetch-Site` on
  state-changing methods rejected unless a Bearer credential is present
  (`hasBearerToken`, since this IDEA.md-scoped project has no
  cookies/sessions/CSRF-token exempt_paths to check instead); `navigate`
  `Sec-Fetch-Mode` against `/api/*` rejected on state-changing methods (GET/
  HEAD navigation to the API surface is allowed); a present-but-not-`?1`
  `Sec-Fetch-User` rejected on Bearer-authenticated navigate state-changers;
  cross-site `iframe` `Sec-Fetch-Dest` rejected against the hardcoded
  `frame-ancestors 'self'` (no per-path allow-list config exists here).
  Rejections use the existing canonical `writeEnvelopeError` PART 14 JSON
  envelope via a new `writeSecFetchRejected` helper. Tests added in
  `src/server/middleware_test.go` covering disabled-config pass-through, all
  absent-header pass-through, all 4 reject conditions, the Bearer bypass, and
  the GET/HEAD `/api/*` navigate exception. Verified in Docker
  (`casjaysdev/go:latest`): `gofmt -l .` clean, `go build ./...` and
  `go vet ./...` clean, `go test ./src/server/...` passes
  (`ok github.com/apimgr/api/src/server`).
- **Clear-Site-Data**: `cfg.Web.Headers.ClearSiteData.*` config fields exist
  but are unused — no token-revocation/consent-withdrawal endpoints exist
  in this IDEA.md-scoped project (no accounts/sessions/admin panel) to emit
  it from. Reserved for future use if such an endpoint is ever added.
- [x] **Server-Timing**: FIXED — new `src/server/servertiming.go` implements
  the AI.md PART 11 "Server-Timing (Debug Mode Only)" header
  (`Server-Timing: total;dur=18.7`), gated on `mode.IsDebugEnabled()` AND
  the existing (previously unused) `cfg.Web.Headers.ServerTimingInDebugOnly`
  operator toggle — production never emits it, since
  `mode.IsDebugEnabled()` is independent of any config value.
  `serverTimingMiddleware` is registered first in the `src/server/server.go`
  chain (ahead of `realIPMiddleware`) so a `*serverTimingWriter` wraps the
  raw `http.ResponseWriter` under every later layer and captures `total` on
  every response including early rejections; it locates a wrapped writer
  via a new `Unwrap() http.ResponseWriter` method added to the existing
  logging `responseWriter` in `src/server/logging_middleware.go` (the Go
  1.20+ `http.ResponseController` convention already used by chi's
  `compressResponseWriter`). `db` is intentionally NOT implemented:
  grepping the codebase shows no HTTP handler ever calls
  `database.GetServerDB()`/`GetUsersDB()` directly — only `src/main.go`
  (startup) and `src/scheduler/tasks.go` (background scheduler, not
  per-request) do — so there is no per-request DB call path to source a
  `db` span from without inventing one. `render` was investigated
  (`renderPage` is a realistic single chokepoint) but NOT wired up: timing
  it requires buffering `ExecuteTemplate` output so the header can still be
  set before the first byte reaches the client, and buffering changes
  `renderPage`'s error behavior from a masked partial-200 (bytes already
  flushed before a mid-render template error surfaces, so `http.Error`'s
  `WriteHeader(500)` becomes a silent no-op) to a clean 500. That surfaced
  a pre-existing, unrelated bug — `server.PageData` has no `Layout` field
  while `partial/head.tmpl` unconditionally reads `.Layout` — breaking
  ~150 previously-passing template-route tests. Fixing that bug is out of
  scope for this change (see the new gap bullet below); `recordServerTiming`/
  `findServerTimingWriter`/`RecordTiming` remain in `servertiming.go`, fully
  implemented and unit-tested, ready for `renderPage` (or any other call
  site) to opt into a `render`/other named span once the `Layout` bug is
  fixed separately. Tests added in `src/server/servertiming_test.go`:
  header absent when debug is off; present with a `total;dur=` entry when
  debug is on; entries match the spec's `name;dur=N.N` comma-separated
  format (exercised directly via `recordServerTiming` in
  `TestServerTimingMiddleware_HeaderFormat`); operator toggle suppresses
  the header even while debug mode is on. Verified in Docker
  (`casjaysdev/go:latest`): `gofmt -l .` clean, `go build ./...` and
  `go vet ./...` clean, `go test ./...` passes.
- [x] **`server.PageData` missing `Layout` field**: FIXED — app-breaking
  (every page shipped a truncated `<head>` in production, missing
  `public.css`, `manifest.json`, `theme-color`, the Open Graph tags, and
  the per-page `head-extra` block), so fixed immediately rather than
  deferred per CLAUDE.md's app-breaking-bug exception. `partial/head.tmpl`
  lines 12/14 read `.Layout` (`{{if eq .Layout "public"}}` /
  `{{else if eq .Layout "admin"}}`) but `server.PageData`
  (`src/server/server.go`) had no `Layout` field, so every page render hit
  a template-execution error on that line; `renderPage` executes the
  template directly into the live `http.ResponseWriter`, so bytes emitted
  before the error already flushed an implicit 200 and the subsequent
  `http.Error` call was a silent no-op. Added `Layout string` to
  `PageData` and set it to `"public"` in `newPageData` — confirmed against
  AI.md PART 16 "Layout Separation" ("All web routes are public — there is
  no admin web UI"; only `public.tmpl` exists), so the `admin` branch in
  `head.tmpl` is intentionally dead per spec and left as-is. Verified in
  Docker (`casjaysdev/go:latest`): `gofmt -l .` clean, `go build ./...`
  and `go vet ./...` clean, `go test ./src/server/...` passes.
- [x] **IDEA.md → Header Tightening Auto-Map**: FIXED — new
  `src/config/ideamap.go` implements AI.md's "IDEA.md → Header Tightening
  Auto-Map" trigger table. `config.Load()` calls `applyIdeaHeaderAutoMap`
  before `Save()` on the first-run "config file does not exist" branch,
  parsing a new `## Compliance declarations` IDEA.md section
  (`audience`/`compliance`/`data_class`/`uses_sharedarraybuffer`/
  `uses_wasm_threads`/`embeds_third_party`, `key: value` lines,
  comma-separated multi-values) via `parseComplianceDeclarations`. Covers the
  COPPA, HIPAA, PCI-DSS, GDPR/CCPA/UK-GDPR/LGPD, GLBA, FERPA, and
  SharedArrayBuffer/WASM-threads rows with strictest-wins merge across
  COOP/COEP/CORP/`Referrer-Policy`/`HonorSecGPC`/
  `ClearSiteData.ExecutionContexts`. `Referrer-Policy` is now config-driven
  (`cfg.Web.Headers.ReferrerPolicy`, `securityHeadersMiddleware` in
  `src/server/middleware.go`) instead of hardcoded, falling back to the
  historical `strict-origin-when-cross-origin` default when empty. Rows with
  no implementable config surface in this IDEA.md-scoped project (HIPAA
  Cache-Control on PHI endpoints; PCI-DSS/GLBA frame-ancestors/
  X-Frame-Options on payment pages — no accounts/payment pages exist) are
  explicitly documented as intentional no-ops rather than fabricated. Since
  `config.Load()` runs before `server.InitLogger()`, changes are buffered via
  `config.LastAutoTightenChanges()` and logged to the existing setup audit
  log (`logger.LogAudit("header_auto_tighten", ...)`) from `src/main.go`
  once the logger is available. This project's own `IDEA.md` gained a
  `## Compliance declarations` section documenting its empty (no accounts,
  no payment pages, no PHI/cardholder data) declaration state. Tests added
  in `src/config/ideamap_test.go` covering the declaration parser (section
  scoping, comma-separated values, case-insensitivity, unknown-key
  rejection), each trigger row, strictest-wins merges across combined
  compliances, and graceful IDEA.md-not-found degradation. Verified in
  Docker (`casjaysdev/go:latest`): `gofmt -l .` clean, `go build ./...` and
  `go vet ./...` clean, `go test ./...` passes (full suite, all packages).

## [x] Revert `.github/workflows/ci.yml` lint job to `casjaysdev/go:latest`
FIXED: `docker run --rm --name ... --entrypoint sh casjaysdev/go:latest -c
'which staticcheck && staticcheck -version'` now succeeds (`staticcheck
2026.1 (v0.7.0)` at `/usr/local/bin/staticcheck`) — the upstream image has
restored the tool. Reverted the `lint` job's `container.image` back to
`casjaysdev/go:latest` (removed the `casjaysdev/go:2606` pin and its
tracking comment), matching AI.md PART 27's lint job spec exactly.

## [x] go-lint findings from the toolPages()/unsupported.tmpl pass (unrelated pre-existing issues)
A `go-lint` scoped run flagged 10 pre-existing convention violations
unrelated to the 28-route `toolPages()` fix that surfaced them; none are
app-breaking, so they were logged here rather than blocking that commit.
- [x] `docker/Dockerfile` line 21: `go build` missing inline `-buildvcs=false`
  flag — fixed, added `-buildvcs=false -trimpath` and a `GOFLAGS=-buildvcs=false`
  env-var prefix to the builder-stage `go build` invocation (2026-07-28)
- [x] `docker/Dockerfile` line 21: `go build` missing `-trimpath` — fixed in
  the same edit above (2026-07-28)
- [x] `docker/Dockerfile`: `GO_DOCKER` build stage missing
  `-e GOFLAGS=-buildvcs=false` — fixed via the inline `GOFLAGS=-buildvcs=false`
  env-var prefix on the `go build` line (2026-07-28)
- [x] `src/main.go` lines 170, 180, 222, 232, 243, 290: `log.Fatalf` used
  instead of `os.Exit` with the correct sysexits code — fixed; replaced each
  with `log.Printf` + `os.Exit(N)` using locally-defined sysexits constants
  (`exUnavailable`=69 DB init, `exConfig`=78 config load / TLS config,
  `exUsage`=64 invalid `--mode`, `exOSErr`=71 daemonize failure,
  `exCantCreat`=73 PID file write failure) per `go_conventions.md` (2026-07-28)
- [x] `src/graphql/theme.go` lines 89-104: client-side React rendering
  present — reviewed against AI.md's GraphQL/Swagger theming section
  (line ~18779): GraphiQL is a spec-mandated interactive third-party
  explorer UI (`/server/docs/graphql`) that inherently ships its own
  React/CodeMirror bundle via CDN script tags; this is the documented
  exception to the "no client-side JS framework" rule, not a violation —
  no code change made (2026-07-28)
- [x] `src/client/main.go` lines 14-18: build-info variables are named `version`/
  `commit`/`buildDate` instead of the required `Version`/`CommitID`/
  `BuildDate`; the release workflow's `-X 'main.Version=...'` ldflags target
  names that don't exist in this file, so the client binary's runtime
  version info is silently unset — found by a `go-lint` pass during the
  language/research 4-tool scope-broadening (2026-07-24). Fixed by renaming
  to `Version`/`CommitID`/`BuildDate` (2026-07-28). While fixing, found a
  related gap: AI.md line 663 requires LDFLAGS to also set
  `-X 'main.OfficialSite=...'`, but neither `src/main.go` nor
  `src/client/main.go` declared an `OfficialSite` var (silently ignored by
  the linker) and `docker/Dockerfile`'s LDFLAGS omitted the flag entirely
  (Makefile and release.yml already had it correctly). Fixed by adding
  `OfficialSite = ""` to both `main.go` files and adding the `OFFICIAL_SITE`
  ARG + `-X 'main.OfficialSite=${OFFICIAL_SITE}'` to the Dockerfile
  (2026-07-28)
