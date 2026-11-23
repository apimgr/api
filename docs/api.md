# API Reference

The REST API uses the version configured by `server.api_version` in `server.yml`; the default is `v1`. The base path is `/api/{api_version}/`. Utility routes are public. Server management routes use the credential rules documented in [Security](security.md).

## Request and response format

Use the method shown in the route table. Path and query values are supplied in the URL; POST operations accept JSON or form data as supported by the endpoint. Successful JSON responses use `{"ok": true, "data": ...}`. Errors use `{"ok": false, "error": "CODE", "message": "..."}` and the HTTP status carries the status code. Health endpoints use their documented flat health response. Routes ending in `.txt` return plain text; image-generation routes return PNG data.

## Shared server endpoints

- `GET /server/healthz` and `GET /api/{api_version}/server/healthz` report server health.
- `GET /api/healthz` is the direct unversioned health alias.
- `GET /api/swagger` serves the generated OpenAPI JSON; the versioned server route is `/api/{api_version}/server/swagger`.
- `POST /api/graphql` and `POST /api/{api_version}/server/graphql` serve GraphQL; `/server/docs/graphql` is the browser interface.
- `/server/docs/swagger` is the browser interface for OpenAPI.
- Metrics endpoints are internal and require their configured service credentials.

## Project utility routes

The routes below are registered by the project and covered by the route contract test. The generated OpenAPI document is served by a running instance at `/api/swagger`.

| Method | Path | Authentication | Purpose | Request/response summary |
|--------|------|----------------|---------|----------------------------|
| `GET` | `/api/{api_version}/convert/area` | Public | area | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/convert/area/{value}/{from}/{to}` | Public | area value from to | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/convert/color` | Public | color | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/convert/currency` | Public | currency | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/convert/data` | Public | data | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/convert/data/{value}/{from}/{to}` | Public | data value from to | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/convert/energy` | Public | energy | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/convert/energy/{value}/{from}/{to}` | Public | energy value from to | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/convert/length` | Public | length | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/convert/length/{value}/{from}/{to}` | Public | length value from to | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/convert/pressure` | Public | pressure | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/convert/pressure/{value}/{from}/{to}` | Public | pressure value from to | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/convert/speed` | Public | speed | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/convert/speed/{value}/{from}/{to}` | Public | speed value from to | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/convert/temperature` | Public | temperature | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/convert/temperature/{value}/{from}/{to}` | Public | temperature value from to | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/convert/time` | Public | time | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/convert/time/{value}/{from}/{to}` | Public | time value from to | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/convert/volume` | Public | volume | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/convert/volume/{value}/{from}/{to}` | Public | volume value from to | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/convert/weight` | Public | weight | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/convert/weight/{value}/{from}/{to}` | Public | weight value from to | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/crypto/bcrypt` | Public | bcrypt | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/crypto/bcrypt/hash` | Public | bcrypt hash | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/crypto/bcrypt/hash/{password}` | Public | bcrypt hash password | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/crypto/bcrypt/verify` | Public | bcrypt verify | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/crypto/bcrypt/verify/{password}/{hash}` | Public | bcrypt verify password hash | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/crypto/bcrypt/{cost}/{password}` | Public | bcrypt cost password | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/crypto/bcrypt/{password}` | Public | bcrypt password | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/crypto/hash` | Public | hash | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/crypto/hash.txt` | Public | hash.txt | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/crypto/hash/{algorithm}/{input}` | Public | hash algorithm input | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/crypto/hash/{algorithm}/{input}.txt` | Public | hash algorithm input.txt | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/crypto/jwt` | Public | jwt | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/crypto/jwt/{token}` | Public | jwt token | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/crypto/password` | Public | password | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/crypto/password.txt` | Public | password.txt | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/crypto/password/strength` | Public | password strength | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/crypto/password/strength/{password}` | Public | password strength password | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/crypto/password/{length}` | Public | password length | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/crypto/password/{length}.txt` | Public | password length.txt | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/crypto/pin` | Public | pin | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/crypto/pin.txt` | Public | pin.txt | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/crypto/pin/{length}` | Public | pin length | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/crypto/pin/{length}.txt` | Public | pin length.txt | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/crypto/random/bytes` | Public | random bytes | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/crypto/random/bytes/{count}` | Public | random bytes count | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/crypto/random/hex` | Public | random hex | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/crypto/random/hex/{count}` | Public | random hex count | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/crypto/totp/code` | Public | totp code | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/crypto/totp/code.txt` | Public | totp code.txt | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/crypto/totp/code/{secret}` | Public | totp code secret | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/crypto/totp/code/{secret}.txt` | Public | totp code secret.txt | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/crypto/totp/generate` | Public | totp generate | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/crypto/totp/secret` | Public | totp secret | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/crypto/totp/verify` | Public | totp verify | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/crypto/totp/verify/{secret}/{code}` | Public | totp verify secret code | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/datetime/add` | Public | add | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/datetime/add/{timestamp}/{duration}` | Public | add timestamp duration | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/datetime/calendar` | Public | calendar | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/datetime/calendar/{year}/{month}` | Public | calendar year month | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/datetime/convert` | Public | convert | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/datetime/convert/{timestamp}` | Public | convert timestamp | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/datetime/convert/{timestamp}/{timezone}` | Public | convert timestamp timezone | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/datetime/cron` | Public | cron | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/datetime/diff` | Public | diff | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/datetime/diff/{timestamp1}/{timestamp2}` | Public | diff timestamp1 timestamp2 | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/datetime/format` | Public | format | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/datetime/format/{timestamp}/{format}` | Public | format timestamp format | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/datetime/moon` | Public | moon | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/datetime/moon/{date}` | Public | moon date | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/datetime/now` | Public | now | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/datetime/now.txt` | Public | now.txt | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/datetime/now/{timezone}` | Public | now timezone | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/datetime/parse` | Public | parse | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/datetime/parse/{value}` | Public | parse value | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/datetime/sunrise` | Public | sunrise | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/datetime/sunrise/{lat}/{lon}` | Public | sunrise lat lon | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/datetime/sunrise/{lat}/{lon}/{date}` | Public | sunrise lat lon date | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/datetime/timestamp` | Public | timestamp | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/datetime/timestamp.txt` | Public | timestamp.txt | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/datetime/timezone` | Public | timezone | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/datetime/timezone/convert` | Public | timezone convert | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/datetime/timezone/convert/{timestamp}/{from}/{to}` | Public | timezone convert timestamp from to | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/datetime/timezone/{timezone}` | Public | timezone timezone | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/datetime/timezones` | Public | timezones | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/datetime/to-unix` | Public | to unix | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/datetime/to-unix/{datetime}` | Public | to unix datetime | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/datetime/workdays` | Public | workdays | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/datetime/workdays/{start}/{end}` | Public | workdays start end | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/dev/cron` | Public | cron | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/dev/echo` | Public | echo | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/dev/jwt` | Public | jwt | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/dev/jwt/{token}` | Public | jwt token | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/docker/best-practices` | Public | best practices | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/docker/network-helper` | Public | network helper | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/docker/port-mapping` | Public | port mapping | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/docker/version` | Public | version | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/docker/volume-helper` | Public | volume helper | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/fun/compliment` | Public | compliment | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/fun/dad-joke` | Public | dad joke | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/fun/fact` | Public | fact | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/fun/fortune` | Public | fortune | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/fun/insult` | Public | insult | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/fun/joke` | Public | joke | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/fun/meme` | Public | meme | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/fun/motivational` | Public | motivational | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/fun/programming-joke` | Public | programming joke | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/fun/quote` | Public | quote | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/fun/riddle` | Public | riddle | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/fun/trivia` | Public | trivia | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/generate/api-docs` | Public | api docs | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/generate/avatar` | Public | avatar | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/generate/barcode` | Public | barcode | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/generate/config` | Public | config | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/generate/dockerfile` | Public | dockerfile | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/generate/gitignore` | Public | gitignore | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/generate/identicon` | Public | identicon | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/generate/license` | Public | license | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/generate/placeholder` | Public | placeholder | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/generate/placeholder/{width}/{height}` | Public | placeholder width height | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/generate/qr` | Public | qr | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/generate/ssh-key` | Public | ssh key | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/geo/bbox` | Public | bbox | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/geo/bearing` | Public | bearing | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/geo/country` | Public | country | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/geo/distance` | Public | distance | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/geo/geocode` | Public | geocode | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/geo/geohash` | Public | geohash | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/geo/h3` | Public | h3 | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/geo/ip` | Public | ip | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/geo/ip/{ip}` | Public | ip ip | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/geo/midpoint` | Public | midpoint | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/geo/pluscode` | Public | pluscode | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/geo/reverse` | Public | reverse | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/geo/timezone` | Public | timezone | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/image/avatar` | Public | avatar | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/image/barcode` | Public | barcode | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/image/identicon` | Public | identicon | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/image/placeholder` | Public | placeholder | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/image/placeholder/{width}/{height}` | Public | placeholder width height | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/image/qr` | Public | qr | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/language/phonetic` | Public | phonetic | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/lorem/address` | Public | address | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/lorem/company` | Public | company | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/lorem/person` | Public | person | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/math/base` | Public | base | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/math/calculate` | Public | calculate | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/math/fibonacci` | Public | fibonacci | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/math/prime` | Public | prime | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/math/prime/{n}` | Public | prime n | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/math/random` | Public | random | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/math/random/{min}/{max}` | Public | random min max | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/math/sequence` | Public | sequence | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/math/stats` | Public | stats | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/network/dns` | Public | dns | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/network/dns/{domain}` | Public | dns domain | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/network/dns/{domain}/{type}` | Public | dns domain type | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/network/headers` | Public | headers | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/network/ip` | Public | ip | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/network/ip/{ip}` | Public | ip ip | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/network/mac` | Public | mac | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/network/mac/{mac}` | Public | mac mac | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/network/ping` | Public | ping | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/network/port` | Public | port | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/network/ssl` | Public | ssl | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/network/subnet` | Public | subnet | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/network/ula` | Public | ula | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/network/url` | Public | url | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/network/user-agent` | Public | user agent | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/network/whois` | Public | whois | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/osint/cert` | Public | cert | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/osint/cert/{domain}` | Public | cert domain | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/osint/domain` | Public | domain | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/osint/domain/{domain}` | Public | domain domain | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/osint/email` | Public | email | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/osint/email/{email}` | Public | email email | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/osint/ip` | Public | ip | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/osint/ip/{ip}` | Public | ip ip | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/osint/subdomain` | Public | subdomain | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/osint/subdomain/{domain}` | Public | subdomain domain | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/osint/tech-stack` | Public | tech stack | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/parse/jwt` | Public | jwt | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/parse/jwt/{token}` | Public | jwt token | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/research/doi` | Public | doi | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/research/doi/{path}` | Public | doi path | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/test/fake-data` | Public | fake data | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/test/fixture` | Public | fixture | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/test/fixture/{type}` | Public | fixture type | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/test/http` | Public | http | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/test/request-inspector` | Public | request inspector | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/test/response-generator` | Public | response generator | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/test/status-codes` | Public | status codes | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/test/status-codes/{code}` | Public | status codes code | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/text/case` | Public | case | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/text/case.txt` | Public | case.txt | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/text/case/{style}/{input}` | Public | case style input | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/text/case/{style}/{input}.txt` | Public | case style input.txt | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/text/decode` | Public | decode | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/text/decode.txt` | Public | decode.txt | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/text/decode/{encoding}/{input}` | Public | decode encoding input | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/text/decode/{encoding}/{input}.txt` | Public | decode encoding input.txt | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/text/encode` | Public | encode | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/text/encode.txt` | Public | encode.txt | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/text/encode/{encoding}/{input}` | Public | encode encoding input | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/text/encode/{encoding}/{input}.txt` | Public | encode encoding input.txt | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/text/hash` | Public | hash | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/text/hash.txt` | Public | hash.txt | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/text/hash/multi` | Public | hash multi | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/text/hash/multi/{input}` | Public | hash multi input | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/text/hash/{algorithm}/{input}` | Public | hash algorithm input | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/text/hash/{algorithm}/{input}.txt` | Public | hash algorithm input.txt | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/text/lorem` | Public | lorem | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/text/lorem.txt` | Public | lorem.txt | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/text/lorem/{type}` | Public | lorem type | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/text/lorem/{type}/{count}` | Public | lorem type count | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/text/lorem/{type}/{count}.txt` | Public | lorem type count.txt | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/text/nanoid` | Public | nanoid | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/text/reverse` | Public | reverse | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/text/reverse.txt` | Public | reverse.txt | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/text/reverse/{input}` | Public | reverse input | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/text/reverse/{input}.txt` | Public | reverse input.txt | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/text/rot13` | Public | rot13 | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/text/rot13.txt` | Public | rot13.txt | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/text/rot13/{input}` | Public | rot13 input | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/text/rot13/{input}.txt` | Public | rot13 input.txt | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/text/ulid` | Public | ulid | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/text/uuid` | Public | uuid | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/text/uuid.txt` | Public | uuid.txt | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/text/uuid/{version}` | Public | uuid version | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/text/uuid/{version}.txt` | Public | uuid version.txt | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/text/uuid/{version}/{count}` | Public | uuid version count | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/weather/air-quality` | Public | air quality | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/weather/air-quality/{location}` | Public | air quality location | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/weather/alerts` | Public | alerts | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/weather/alerts/{location}` | Public | alerts location | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/weather/astronomy` | Public | astronomy | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/weather/astronomy/{location}` | Public | astronomy location | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/weather/current` | Public | current | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/weather/current/{location}` | Public | current location | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/weather/forecast` | Public | forecast | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/weather/forecast/{location}` | Public | forecast location | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/weather/historical` | Public | historical | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/weather/historical/{location}` | Public | historical location | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/weather/hourly` | Public | hourly | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/weather/hourly/{location}` | Public | hourly location | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/weather/marine` | Public | marine | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/weather/marine/{location}` | Public | marine location | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/weather/pollen` | Public | pollen | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/weather/pollen/{location}` | Public | pollen location | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/weather/uv` | Public | uv | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `GET` | `/api/{api_version}/weather/uv/{location}` | Public | uv location | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `POST` | `/api/{api_version}/crypto/bcrypt/verify` | Public | bcrypt verify | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `POST` | `/api/{api_version}/crypto/certificate` | Public | certificate | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `POST` | `/api/{api_version}/crypto/decrypt` | Public | decrypt | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `POST` | `/api/{api_version}/crypto/ed25519` | Public | ed25519 | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `POST` | `/api/{api_version}/crypto/encrypt` | Public | encrypt | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `POST` | `/api/{api_version}/crypto/hmac` | Public | hmac | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `POST` | `/api/{api_version}/crypto/password/strength` | Public | password strength | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `POST` | `/api/{api_version}/crypto/pgp` | Public | pgp | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `POST` | `/api/{api_version}/crypto/rsa` | Public | rsa | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `POST` | `/api/{api_version}/dev/base64` | Public | base64 | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `POST` | `/api/{api_version}/dev/format/css` | Public | format css | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `POST` | `/api/{api_version}/dev/format/html` | Public | format html | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `POST` | `/api/{api_version}/dev/format/js` | Public | format js | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `POST` | `/api/{api_version}/dev/format/json` | Public | format json | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `POST` | `/api/{api_version}/dev/format/sql` | Public | format sql | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `POST` | `/api/{api_version}/dev/format/xml` | Public | format xml | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `POST` | `/api/{api_version}/dev/regex` | Public | regex | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `POST` | `/api/{api_version}/dev/url-encode` | Public | url encode | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `POST` | `/api/{api_version}/docker/compose-to-run` | Public | compose to run | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `POST` | `/api/{api_version}/docker/compose-validate` | Public | compose validate | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `POST` | `/api/{api_version}/docker/dockerfile-generate` | Public | dockerfile generate | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `POST` | `/api/{api_version}/docker/dockerfile-lint` | Public | dockerfile lint | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `POST` | `/api/{api_version}/docker/env-parser` | Public | env parser | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `POST` | `/api/{api_version}/docker/run-to-compose` | Public | run to compose | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `POST` | `/api/{api_version}/docker/security-scan` | Public | security scan | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `POST` | `/api/{api_version}/docker/size-optimizer` | Public | size optimizer | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `POST` | `/api/{api_version}/generate/sql` | Public | sql | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `POST` | `/api/{api_version}/image/convert` | Public | convert | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `POST` | `/api/{api_version}/image/crop` | Public | crop | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `POST` | `/api/{api_version}/image/filter` | Public | filter | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `POST` | `/api/{api_version}/image/metadata` | Public | metadata | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `POST` | `/api/{api_version}/image/optimize` | Public | optimize | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `POST` | `/api/{api_version}/image/resize` | Public | resize | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `POST` | `/api/{api_version}/image/watermark` | Public | watermark | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `POST` | `/api/{api_version}/language/dictionary` | Public | dictionary | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `POST` | `/api/{api_version}/language/keywords` | Public | keywords | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `POST` | `/api/{api_version}/language/readability` | Public | readability | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `POST` | `/api/{api_version}/language/reading-time` | Public | reading time | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `POST` | `/api/{api_version}/language/sentiment` | Public | sentiment | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `POST` | `/api/{api_version}/language/thesaurus` | Public | thesaurus | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `POST` | `/api/{api_version}/language/word-count` | Public | word count | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `POST` | `/api/{api_version}/math/matrix` | Public | matrix | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `POST` | `/api/{api_version}/parse/csv` | Public | csv | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `POST` | `/api/{api_version}/parse/env` | Public | env | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `POST` | `/api/{api_version}/parse/html` | Public | html | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `POST` | `/api/{api_version}/parse/ini` | Public | ini | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `POST` | `/api/{api_version}/parse/json` | Public | json | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `POST` | `/api/{api_version}/parse/log` | Public | log | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `POST` | `/api/{api_version}/parse/markdown` | Public | markdown | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `POST` | `/api/{api_version}/parse/sql` | Public | sql | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `POST` | `/api/{api_version}/parse/toml` | Public | toml | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `POST` | `/api/{api_version}/parse/xml` | Public | xml | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `POST` | `/api/{api_version}/parse/yaml` | Public | yaml | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `POST` | `/api/{api_version}/research/arxiv` | Public | arxiv | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `POST` | `/api/{api_version}/research/citation` | Public | citation | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `POST` | `/api/{api_version}/research/isbn` | Public | isbn | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `POST` | `/api/{api_version}/test/api-client` | Public | api client | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `POST` | `/api/{api_version}/test/assert` | Public | assert | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `POST` | `/api/{api_version}/test/curl-generator` | Public | curl generator | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `POST` | `/api/{api_version}/test/postman` | Public | postman | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `POST` | `/api/{api_version}/test/request-inspector` | Public | request inspector | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `POST` | `/api/{api_version}/test/webhook` | Public | webhook | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `POST` | `/api/{api_version}/text/compress` | Public | compress | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `POST` | `/api/{api_version}/text/diff` | Public | diff | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `POST` | `/api/{api_version}/text/extract` | Public | extract | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `POST` | `/api/{api_version}/text/regex` | Public | regex | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `POST` | `/api/{api_version}/text/stats` | Public | stats | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `POST` | `/api/{api_version}/validate/credit-card` | Public | credit card | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `POST` | `/api/{api_version}/validate/domain` | Public | domain | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `POST` | `/api/{api_version}/validate/email` | Public | email | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `POST` | `/api/{api_version}/validate/iban` | Public | iban | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `POST` | `/api/{api_version}/validate/ip` | Public | ip | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `POST` | `/api/{api_version}/validate/isbn` | Public | isbn | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `POST` | `/api/{api_version}/validate/json` | Public | json | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `POST` | `/api/{api_version}/validate/mac` | Public | mac | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `POST` | `/api/{api_version}/validate/phone` | Public | phone | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `POST` | `/api/{api_version}/validate/url` | Public | url | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `POST` | `/api/{api_version}/validate/uuid` | Public | uuid | applicable path, query, or POST body input. Response: requested result in the endpoint format. |
| `POST` | `/api/{api_version}/validate/vat` | Public | vat | applicable path, query, or POST body input. Response: requested result in the endpoint format. |

## Content negotiation

JSON is the default for API operations. Use `Accept: text/plain` or the `.txt` route where available for plain text. Image endpoints return `image/png`. Browser pages render HTML and provide forms that work without JavaScript.

## Interactive references

- OpenAPI UI: `/server/docs/swagger`
- OpenAPI JSON: `/api/swagger`
- GraphQL UI: `/server/docs/graphql`
- GraphQL endpoint: `/api/graphql`

For CLI access, see [CLI Reference](cli.md). For credential and rate-limit behavior, see [Security](security.md).
