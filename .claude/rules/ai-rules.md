# AI Behavior Rules (PART 0, 1)

⚠️ **These rules are NON-NEGOTIABLE. Violations are bugs.** ⚠️

## CRITICAL - NEVER DO
- Never modify PART 0-33 of `AI.md` — it is read-only spec, source of truth
- Never run plain `git commit` or `git push` — only `gitcommit --dir {dir} all` is allowed
- Never write `.git/COMMIT_MESS` or call `gitcommit` from a subagent — only the
  main/coordinating agent commits; subagents edit and report back
- Never use a bare `@name` in a commit message
- Never invent behavior, endpoints, config keys, or file layouts not in the spec
- Never restructure or delete existing pre-scaffolded project content while
  doing a bootstrap/verification pass — only create/fix what is missing
- Never change what a migrating project can't change: CLI flags/commands,
  Makefile structure, directory layout, config format, health endpoint
  format, API response format
- Never leave `TODO`/`FIXME`/`HACK` markers or stub/partial logic in committed code
- Never use inline comments — comments always go above the line they describe
- Never skip a PART when implementing a new project — "jumping around" is failure

## CRITICAL - ALWAYS DO
- Treat `AI.md` as SPEC IS LAW: implement exactly what it prescribes, nothing invented
- VERIFY EVERYTHING: check existing code against the spec before assuming it's correct
- For new projects: COMPLETE COVERAGE of PART 0 through PART 33 + FINAL, in order
- PART 32 (Client) is REQUIRED for every project — no skip case
- Keep `AI.md` in sync with actual project state
- Use `TODO.AI.md` for any unit of work with 3+ tasks
- Follow code style rules exactly: comments above only; formatting per
  filetype (Go = tabs; HTML/JSON/YAML/CSS/JS = 2 spaces; Makefile = tabs;
  shell = 2 spaces); every text file ends with a single trailing newline
- What a migrating project CAN customize: Dockerfile packages/base image,
  config values, routes, DB schema, business logic, UI/branding
- Run the Migration Verification Protocol before declaring migration/bootstrap complete

## KEY DECISIONS (pre-answered)
| Question | Answer | Spec Reference |
|----------|--------|----------------|
| Who commits, main agent or subagent? | Main/coordinating agent only | PART 0, Prohibited Actions |
| Can I restructure existing files during bootstrap? | No — create/fix missing pieces only | PART 0, Migrating Existing Projects |
| Is PART 32 (Client) optional for small projects? | No, required for ALL projects | PART 0, AI New Project Implementation Rules |
| Where do comments go? | Always above the code, never inline | PART 0, Code Style Rules |
| What commit path is allowed? | `gitcommit --dir {dir} all` only | PART 0, Prohibited Actions |

## TERMINOLOGY
| Term | Meaning |
|------|---------|
| SPEC IS LAW | `AI.md` defines required behavior; no deviation without explicit instruction |
| NO INVENTION | Never add endpoints/config/files the spec doesn't call for |
| VERIFY EVERYTHING | Confirm existing code matches spec before trusting it |
| COMPLETE COVERAGE | New projects implement every mandatory PART, not a subset |
| Migration | Bringing an existing project in line with a newer/different `AI.md` |
| Bootstrap | Initial pass creating/fixing a project against `AI.md` PART 0-6 |

## QUICK REFERENCE
- Read the relevant PART before touching code in that area
- Ask: is this in the spec, or am I guessing? If guessing, stop and read the spec
- New/changed pattern → `grep -rn` across the working set, fix every instance
- Every commit needs `COMMIT_MESS` written from a real `git diff`, not memory
- Subagents report back; they never commit

---
For complete details, see AI.md PART 0, PART 1
