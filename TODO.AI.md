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
