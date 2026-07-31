## [ ] Wire libSQL/Turso database driver
AI.md PART 3 "Required Go Libraries" → Database Drivers documents
`github.com/tursodatabase/libsql-client-go` as a required driver (aliases
`libsql`, `turso`) alongside `modernc.org/sqlite`, with a `normalizeDriver()`
example. `src/config/config.go` has a `DatabaseConfig.Driver` field (and a
`DATABASE_DRIVER` env override) but `src/database/database.go`'s `Init()`
ignores it entirely — it hardcodes `sql.Open("sqlite", ...)` against a path
derived from `paths.GetDatabaseDir(dataDir)`, never `cfg.Server.Database.URL`
or `.Driver`. Needs: driver normalization (sqlite/sqlite2/sqlite3 ->
sqlite, libsql/turso -> libsql), a libsql open path requiring a remote URL
(no embedded/local mode, per AI.md), and validation per the
`validateLibSQL` example in AI.md PART 3. Add `go get
github.com/tursodatabase/libsql-client-go` when implementing.

## [ ] Add server.cache.* config schema
AI.md PART 12 "Cache Configuration" documents a full `server.cache.*` YAML
block (`type: none|memory|valkey|redis`, url/host/port/username/password/db,
tls, pool_size, prefix, ttl) with `memory` as the default and Valkey/Redis
as the optional distributed backend. `src/config/config.go` has no `Cache`
struct at all, so this documented config surface doesn't exist yet. Current
rate limiting (`src/server/ratelimit.go`) already implements the `memory`
(in-process) default correctly per PART 12's "Rate Limiting" section
(sliding window counters), so nothing is broken today, but there's no way
for an operator to configure `type: valkey`/`redis` for counters to persist
across restarts / be shared across instances. When implementing: add the
config struct, wire `github.com/redis/go-redis/v9` as the backend for
`valkey`/`redis` types, and switch the rate limiter's per-class counters to
use it when configured.

## [ ] Evaluate go-playground/validator/v10 adoption
AI.md PART 3 "Required Go Libraries" lists `github.com/go-playground/validator/v10`
for input validation. Current handlers (e.g. `src/server/handler/*.go`) use
manual ad hoc checks (length caps, type parsing, `net/mail`, `net/url`,
etc.) rather than struct-tag validation — this matches `docs/development.md`'s
documented "Code Guidelines" convention and works correctly for this
project's simple scalar-parameter tool endpoints, but doesn't match the
library AI.md names. Evaluate whether adopting `validator/v10` for
consistency is worth the handler-by-handler refactor, or whether the manual
approach should be documented in AI.md as the accepted implementation for
this project's request shape.

## [ ] Wire backup.encryption_password config schema
AI.md PART 21 "Setting/Changing Backup Password" documents the backup
encryption password as `backup.encryption_password` in `server.yml` (set at
initial config, changeable later, mandatory when
`server.compliance.enabled: true`). `src/config/config.go` has no `Backup`
struct / `encryption_password` field at all, and `src/scheduler/tasks.go`'s
`backupTask()` instead reads the password from an `API_BACKUP_PASSWORD`
environment variable that appears nowhere in AI.md. Result: the documented
config surface doesn't exist, and there is an undocumented env var acting as
a stand-in. Nothing is broken today (backups run unencrypted when unset, per
PART 21's "optional unless compliance"), but the compliance-mandatory
encryption path (block backups / warn when compliance is on and no password
set) is also absent. When implementing: add the `backup.*` config struct
(including `encryption_password`), have `backupTask()` read
`cfg.Server.Backup.EncryptionPassword` (env override optional, but then
document `API_BACKUP_PASSWORD` in AI.md), and enforce the compliance-mode
mandatory-encryption behavior from PART 21.

## [ ] Block DNS lookups that resolve to non-routable targets
IDEA.md "Threat model" states private/loopback/link-local targets are
blocked "before any DNS/WHOIS/TLS call is made." `src/service/osint/osint.go`
`DNSLookup()` blocks only *literal* private-IP inputs (line ~179); a hostname
that resolves to an RFC 1918 / loopback address is looked up and its A/AAAA
records returned unfiltered, unlike `WHOISLookup`/`SSLInfo`/`TechStack` which
all call `validateTarget()`. Low severity (DNS resolution does not connect to
the target), but it is a minor internal-address disclosure vector and an
inconsistency with the other OSINT functions. Fix: for A/AAAA, drop any
resolved address for which `isBlockedIP` is true (or error if all are
blocked); keep MX/TXT/NS/CNAME as-is.
