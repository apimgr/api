## [ ] Move or remove FRONTEND_IMPLEMENTATION_GUIDE.md
Read: AI.md PART 1

`FRONTEND_IMPLEMENTATION_GUIDE.md` is not in AI.md's exhaustive "Allowed Root
Files" list (PART 1). Migrate its still-relevant content into `docs/` (e.g.
`docs/development.md`) or into `AI.md`/`IDEA.md` as appropriate, then remove
it from the repo root — confirm with the user before deleting if any content
looks non-obsolete.

## [ ] Audit go.mod against AI.md PART 3 "Required Go Libraries"
Read: AI.md PART 3

go.mod is missing several libraries PART 3 lists as required for their
respective features: `golang.org/x/time/rate` (rate limiting — IDEA.md states
this is the sole abuse-prevention mechanism for the public API), `rs/cors`,
`github.com/go-playground/validator/v10`, `github.com/go-co-op/gocron/v2`,
`github.com/gorilla/websocket`, a cache driver (`redis/go-redis/v9` or
`bradfitz/gomemcache`), and `github.com/tursodatabase/libsql-client-go`.
Confirm for each: is the feature implemented via a hand-rolled equivalent
(acceptable if it meets the spec), genuinely unimplemented (needs building),
or not applicable to this project's scope — then either add the dependency
or document why it's not needed.
