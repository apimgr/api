## [x] Wire per-tool detail pages linked from the 21 category pages (READY batch)
Read: AI.md PART 16 (frontend), src/server/template/page/*.tmpl
Every sub-tool that mapped directly onto an existing, non-stub backend
service method has now been wired end-to-end (template + `toolPages()`
entry + frontend route + API route + tool-page test + handler test).
103 tools are wired as of this pass:
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
math/{calculate,gcd,logarithm,percentage,prime,random,stats,trigonometry};
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

## [ ] MISSING sub-tools needing net-new backend service work (137 linked, unwired)
Read: src/server/template/page/{category}.tmpl for the exact linked path,
src/service/{category}/ for whatever backend already exists in that area
None of these have a corresponding template under
`src/server/template/page/tools/{category}/` yet. Confirm per-tool whether
it needs a brand-new service method, a new third-party dependency, or is
out of scope per IDEA.md non-goals, before wiring — do not guess behavior.
One commit per tool or small logical group when picked up.

- convert (7): area, color, currency, data, energy, pressure, speed
- crypto (3): certificate, ed25519, pgp
  (X.509 cert, Ed25519, PGP — zero backend support today; each needs its own
  net-new crypto service method. encrypt/decrypt/rsa/hmac were wired this
  pass onto pre-existing crypto_extended.go/crypto.go functions)
- datetime (7): calendar, cron, format, moon, parse, sunrise, workdays
- dev (8): cron, css-format, echo, html-format, js-format, jwt,
  sql-format, xml-format
- docker (9): best-practices, compose-to-run, compose-validate,
  dockerfile-lint, env-parser, network-helper, run-to-compose,
  security-scan, size-optimizer
- fun (10): compliment, dad-joke, fact, insult, meme, motivational,
  programming-joke, quote, riddle, trivia
- generate (12): api-docs, avatar, barcode, config, dockerfile, gitignore,
  identicon, license, placeholder, qr, sql, ssh-key
- geo (8): bbox, country, geocode, geohash, h3, pluscode, reverse, timezone
- image (7): avatar, barcode, filter, identicon, optimize, qr, watermark
- language (10): detect, dictionary, grammar, keywords, readability,
  reading-time, sentiment, spell-check, thesaurus, translate
- math (4): base, fibonacci, matrix, sequence
- network (6): ping, ssl, traceroute, url, useragent, whois
  (`useragent` here is the broken-link duplicate of the already-wired
  `user-agent` — see bug list below, not a real missing tool)
- osint (8): breach, company, metadata, phone, social, subdomain,
  tech-stack, username
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
apiResearchExtractHandler doc comments)
Three of the wired API routes honestly return 501 NOT_SUPPORTED rather
than inventing behavior: generate/qr (no QR encoder exists anywhere in the
codebase or go.mod), language/detect (conflicts with IDEA.md's declared
non-goal of language auto-detection), and research/extract (research.go's
own source comment documents citation extraction from unstructured text as
unimplemented). Resolving these requires a user/spec decision — either add
a QR-encoding dependency, confirm language/detect should stay unsupported
per IDEA.md, or scope what "extraction" means for research/extract — not
further code guessing.
