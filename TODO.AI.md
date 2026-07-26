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

## [ ] MISSING sub-tools needing net-new backend service work (50 linked, unwired)
Read: src/server/template/page/{category}.tmpl for the exact linked path,
src/service/{category}/ for whatever backend already exists in that area
None of these have a corresponding template under
`src/server/template/page/tools/{category}/` yet. Confirm per-tool whether
it needs a brand-new service method, a new third-party dependency, or is
out of scope per IDEA.md non-goals, before wiring — do not guess behavior.
One commit per tool or small logical group when picked up.

- parse (8): env, html, ini, log, markdown, sql, toml, yaml
- research (10): arxiv, bibtex, footnotes, isbn, metadata, outline,
  pdf-extract, readability, scraper, summarize
- testing (9): api-client, curl-generator, load-test, mock-server,
  postman, request-inspector, response-generator, status-codes, webhook
  (mock-server needs a genuinely new configurable dynamic-response
  backend — no existing `test.Service` method covers it; response-generator
  may already be covered by testing/http's `GenerateMockAPIResponse` or may
  need its own distinct implementation — needs a scoping decision before
  either is picked up)
- weather (10): air-quality, alerts, astronomy, historical, hourly, maps,
  marine, pollen, radar, uv

## [ ] Known template bugs (tracked, not yet fixed)
- `network.tmpl` links `/network/useragent` but the wired API/frontend path
  is `/network/user-agent` — fix the link, do not add a second route.
- `docker/version.tmpl`'s form has no `image` input field even though
  `apiDockerVersionHandler` requires `?image=` — form submits with a
  missing required param.

## [ ] Known permanent API gaps needing a future spec/dependency decision
Read: src/server/api_utils.go (apiGenerateQRHandler, apiLanguageDetectHandler,
apiLanguageDictionaryHandler, apiLanguageGrammarHandler,
apiLanguageSpellCheckHandler, apiLanguageThesaurusHandler,
apiLanguageTranslateHandler, apiResearchExtractHandler,
apiOsintBreachHandler, apiOsintCompanyHandler, apiOsintMetadataHandler,
apiOsintPhoneHandler, apiOsintSocialHandler, apiOsintUsernameHandler doc
comments), src/server/api_network.go (apiNetworkTracerouteHandler doc comment)
Fifteen of the wired API routes honestly return 501 NOT_SUPPORTED rather
than inventing behavior: generate/qr (no QR encoder exists anywhere in the
codebase or go.mod), language/detect (conflicts with IDEA.md's declared
non-goal of language auto-detection), language/dictionary,
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
than a single user-named target). Resolving these requires a user/spec
decision — either add a QR-encoding dependency, confirm language/detect
and the four other language gaps should stay unsupported per IDEA.md,
scope what "extraction" means for research/extract, decide whether
network/traceroute should ship as a root-only opt-in feature instead of
a permanent gap, or decide whether any of the six osint gaps should be
promoted to a keyed-API-optional feature — not further code guessing.
None of the fifteen gap routes have a `toolPages()` entry or frontend
template (API-only, no dead frontend link to a page that doesn't exist).
