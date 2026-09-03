# AI Assistant Rules (PART 0, 1)

⚠️ **These rules are NON-NEGOTIABLE. Violations are bugs.** ⚠️

## CRITICAL - NEVER DO
- Never guess or assume a requirement, file location, default value, or spec intent — STOP and ASK instead
- Never claim "done" without reading, searching, testing, and verifying first
- Never modify AI.md (PARTS 0-33) — it is read-only; project changes belong in IDEA.md
- Never "improve", "optimize", or "refactor" beyond what the spec/task requires
- Never create report/analysis files (AUDIT.md, COMPLIANCE.md, SUMMARY.md) — fix issues directly; `AUDIT.AI.md` is a temporary exception for explicit audits with 5+ issues, deleted when resolved
- Never use inline or below-line comments — comments always go ABOVE code; never comment in JSON files
- Never use plain `git commit` / `git push` — only `gitcommit <command>` after writing and re-reading `.git/COMMIT_MESS`
- Never let a subagent write `.git/COMMIT_MESS` or call `gitcommit` — only the parent instance commits
- Never add any attribution referencing the AI tool anywhere (code, comments, commits, PRs, docs) — no co-author trailers or generation notices naming the assistant
- Never read an image larger than 1000x1000 directly — resize to a tempdir copy first and read that
- Never run `go` commands directly on the local machine — all builds/tests happen in Docker/Incus containers
- Never jump between unfinished tasks — complete one feature fully before starting the next
- Never treat a non-conforming IDEA.md as authoritative without running the migration procedure
- Never expose sensitive data (credentials, connection strings, internal paths, stack traces) in API responses, health checks, logs, or error messages
- Never weaken security (authn/authz, TLS, CSRF/CSP/CORS, rate limiting, input validation) to improve usability — solve usability with better defaults/UX instead

## CRITICAL - ALWAYS DO
- Read the AI.md PART(s) relevant to the current task before implementing — do not pre-load the whole spec speculatively
- Read a file before editing it; search for existing patterns before creating new ones
- Ask numbered/lettered clarifying questions when the spec is ambiguous, and wait for the answer
- Verify every change with a real tool (tests, curl, build, browser) before declaring it done — "looks right" is not verification
- Update IDEA.md when features change; keep README.md, Swagger, GraphQL, and docs/ in sync with code
- Update TODO.AI.md / TODO.md as tasks progress; never delete/empty the human-owned TODO.md, only mark items done in place
- Use `.claude/rules/*.md` as the fast-loading summary; consult AI.md directly for full detail when needed
- Translate all new user-facing text (web, CLI, notifications, errors) — add keys to `en.json` and note other locales need the same key
- Trim whitespace on all text input; reject (don't trim) passwords with leading/trailing whitespace
- Use parameterized queries and name columns explicitly — never `SELECT *`, never string-concatenated SQL
- Follow the intent-revealing naming rule — no bare `Mode`, `Type`, `Status`, `Config`, `Get()`, `Init()`, etc.; qualify every name
- Keep README.md updated after every feature/config/CLI change, in the mandated section order, with a Disclaimer section
- Use the standard curl flags `-q -LSsf` in all docs/scripts/examples
- Use `{official_site}/path` for documentation URLs and `{fqdn}/path` (via `BuildURL`) for embedded/runtime URLs — never bare paths outside internal router registration

## Key Rules Summary
- **Session start**: read existing CLAUDE.md/.claude/rules, migrate stray content into IDEA.md, create/update the 13 `.claude/rules/*.md` files if missing or stale, check TODO.AI.md/TODO.md.
- **File hierarchy**: SPEC.md > AI.md > global CLAUDE.md. AI.md = HOW (read-only), IDEA.md = WHAT (editable, must follow AI.md), SPEC.md = optional project-specific override.
- **Mandatory workflow**: identify relevant PARTs → read them fully → implement exactly → re-check every 3-5 changes for drift.
- **Verification checklist before "done"**: read files, searched patterns, tested changes, verified output, no guessing, no rushing.
- **Red flags** ("this is probably what they meant", "I'll just assume", "let me quickly...") mean STOP and ask/test/slow down.
- **Check Files** (discovery) is not compliance verification; **Audit** (full compliance check + fixes) only runs on explicit "audit" command, tracked in AUDIT.AI.md if over 5 issues.
- **TODO.AI.md completion**: remove items only once resolved+committed; final commit uses fixed title `✅ all todo items have been completed ✅`.
- **Large images**: check dimensions with `identify`/`magick`/`sips`/etc. before reading; resize to ≤1000x1000 into `${TMPDIR:-/tmp}` first.
- **IDEA.md migration**: required sections are `## Project description`, `## Project variables`, `## Business logic` in that order; back up, map old content, get explicit approval before rewriting.
- **Commit workflow**: write `.git/COMMIT_MESS`, re-read it, run `gitcommit --dir {dir} all`; never `-m`; never bare `@name` in commit body; gitcommit both commits and pushes.
- **Code style**: comments above code only; Go uses tabs, most other files use 2-space indent; every file ends with exactly one trailing newline; `gofmt` for Go.
- **Rate limiting defaults**: 5 failed-auth attempts / 15 min lockout; configurable API limits per minute; 10 uploads/hour — all overridable via config.
- **Error responses**: users get minimal generic messages, logs get full detail with request IDs; never leak whether a resource/token exists.
- **Naming conventions**: files `lowercase_snake.go`, packages lowercase, exported `PascalCase`, unexported `camelCase`, constants `PascalCase`/`SCREAMING_SNAKE`, interfaces end in `-er`.
- **Full web app architecture**: every feature needs a web (HTML) route and a matching `/api/{api_version}/...` (JSON) route; api project's client (PART 32) is mandatory for every project.
- **Config**: everything lives in `server.yml`, hot-reloads without restart (except listen address/port/DB driver changes); no runtime config-mutation API.
- **Target audience**: assume self-hosted/SMB users are not tech-savvy — favor clarity, sane defaults, tooltips.

For complete details, see AI.md PART 0, 1
