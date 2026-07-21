# Project SPEC

Project: api
Role: Efficient loader for AI.md

⚠️ **THIS FILE IS AUTO-LOADED EVERY CONVERSATION. FOLLOW IT EXACTLY.** ⚠️

Purpose:
- This file is a short loader for the most important rules
- `AI.md` is the full source of truth
- For complete details, read the referenced PARTs in `AI.md`

## Asking Questions

- **Default to continuing work** - do not stop just to ask whether you should continue; if the next step is implied by the spec, the current task, or the current findings, continue
- **Never guess** - if the answer cannot be determined from `AI.md`, `IDEA.md`, the codebase, or repo state **and** the missing information materially changes behavior, scope, or safety, ASK the user
- **Do NOT ask for permission to keep going** - continue until the current task is complete, blocked by a real decision, or the user explicitly asks to pause
- **Question mark = question** - when user ends with `?`, answer/clarify, don't execute
- **Use AskUserQuestion wizard** - presents one question at a time with options + "Other" for custom input + Submit/Cancel; layout varies by context (yes/no, multi-select, with descriptions); less overwhelming than plain text questions

**Ask only when at least one of these is true:**
1. A required business/product decision is missing
2. Two or more reasonable implementations would produce materially different behavior
3. The action is destructive, irreversible, or impacts production/user data beyond normal safe development work
4. The spec explicitly says to ask or confirm
5. The user explicitly requested a plan, pause, or checkpoint before execution

**Do NOT ask just to confirm routine continuation:**
- after finishing one obvious sub-step and the next step is clear
- before running normal repo validations/checks already implied by the task
- before making tightly related follow-up edits required to keep the spec internally consistent
- before updating related docs/checklists/examples required by the same change

## Before ANY Code Change

1. Have I read the relevant PART in AI.md? (If no → read it)
2. Does this follow the spec EXACTLY? (If unsure → check spec)
3. Am I guessing or do I KNOW from the spec? (If guessing → read spec)
4. Would this pass the compliance checklist? (AI.md FINAL section)

**WHEN IN DOUBT: READ THE SPEC. DO NOT GUESS.**

## Binary Terminology
- **server** = `api` (main binary, runs as service)
- **client** = `api-cli` (REQUIRED companion, CLI/TUI/GUI)

## Key Placeholders
- `{project_name}` = api
- `{project_org}` = apimgr

## NEVER Do (Top 19) - VIOLATIONS ARE BUGS
1. Use bcrypt for config/backup passwords → Use Argon2id
2. Put Dockerfile in root → `docker/Dockerfile`
3. Use CGO → CGO_ENABLED=0 always
4. Hardcode dev values → Detect at runtime
5. Use external cron → Internal scheduler (PART 18)
6. Store config/backup passwords plaintext → Argon2id (API tokens use SHA-256)
7. Create premium tiers → All features free, no paywalls
8. Use Makefile in CI/CD → Explicit commands only
9. Guess or assume values that a command can produce → Run the command (`date`, `basename "$PWD"`, `git config user.email`, `git rev-parse --short HEAD`, `uname -m`, etc.) — when no command applies, read spec or ask user
10. Skip platforms → Build all 8 (linux/darwin/windows/freebsd × amd64/arm64)
11. Client-side rendering (React/Vue) → Server-side Go templates
12. Require JavaScript for core features → Progressive enhancement only
13. Let long strings break mobile → Use word-break CSS
14. Skip validation → Server validates EVERYTHING
15. Implement without reading spec → Read relevant PART first
16. Modify TEMPLATE.md or AI.md content → READ-ONLY SPEC. Project changes go in IDEA.md.
17. Edit `## Project variables` in IDEA.md without confirming with the user → Variables drive placeholder resolution used by AI.md; wrong values silently corrupt every reference
18. Read an image larger than 1000×1000 directly into context → Resize to ≤1000×1000 first via the fallback chain (`magick` → `convert` → `gm convert` → `vipsthumbnail` → `sips` → `ffmpeg`). If none are available, do NOT read the image — tell the user which tool to install. See "Large Image Handling" below.
19. Use a non-conforming IDEA.md without migration → If IDEA.md exists but lacks the three required sections (`## Project description`, `## Project variables`, `## Business logic`), STOP and migrate it before doing anything else. See "IDEA.md Migration" below.

## ALWAYS Do - NON-NEGOTIABLE
1. Read AI.md before implementing ANY feature
2. Server-side processing (server does the work, client displays)
3. Mobile-first responsive CSS
4. All features work without JavaScript
5. Tor hidden service support (auto-enabled if Tor found)
6. Built-in scheduler, GeoIP, metrics, email, backup, update
7. All settings configurable via API and config file
8. Client binary for ALL projects
9. Commit often via `gitcommit <command>` — small, focused commits, each with a fresh accurate `.git/COMMIT_MESS`. See "gitcommit Script" → "Commit Cadence". Do NOT hoard unrelated changes into one big commit. **Subagents do not commit** — complete edits and report back to the parent instance; the parent reviews the diff and owns the commit.

## File Locations
- Config: `{config_dir}/server.yml`
- Data: `{data_dir}/`
- Logs: `{log_dir}/`
- Source: `src/`
- Docker: `docker/`

## Where to Find Details
- AI behavior: `.claude/rules/ai-rules.md` (PART 0, 1)
- Project structure: `.claude/rules/project-rules.md` (PART 2, 3, 4)
- Frontend/WebUI: `.claude/rules/frontend-rules.md` (PART 16)
- Full spec: `AI.md` (~46k lines) ← **SOURCE OF TRUTH**

## Current Project State
- Bootstrap pass (PART 0-6) plus a full line-by-line spec-compliance audit
  (`AUDIT.AI.md`) have both landed against the pre-existing scaffolded Go
  repo; work spans well past PART 0-6 at this point — see `AUDIT.AI.md` for
  the authoritative, itemized list of what's fixed vs. still open per PART.
- IDEA.md non-goals (no admin panel, no auth/sessions, no user accounts) are
  confirmed authoritative over base AI.md; `src/admin/`, `src/session/`, and
  related admin/session template and config code were removed accordingly.
- `src/mode/` wired to the PART 6 mode+debug priority chain (CLI > env > alias > default); `main.go` now calls `appmode.Initialize`.
- `src/paths/paths.go` now implements `GetCacheDir()`/`--cache` and a
  writable-fallback `GetBackupDir()`/`--backup`, both wired into `main.go`.
- `src/sysservice/service.go` (cross-platform systemd/runit/launchd/Windows/
  BSD rc.d service management) is wired into `main.go`'s `--service`
  subcommands, replacing the prior Linux-only hand-rolled implementation.
- CI/CD (`.github/workflows/ci.yml`, `release.yml`), `Makefile`, and
  `docker/Dockerfile` have been brought into PART 25-27 compliance
  (`casjaysdev/go:latest` toolchain, 8-platform release matrix, non-root
  Docker user, secret scanning).
- Several previously-stubbed service packages (`crypto`, `network`, `test`,
  `weather`, `osint`, `image`) have real implementations now; multiple
  packages remain open per `AUDIT.AI.md` Pass 1/3/5/6 (security hardening,
  scheduler cron parsing, geoip MMDB migration, `api-cli` client source
  under PART 32, metrics/logger subsystems).
- `.claude/rules/ai-rules.md` and `.claude/rules/project-rules.md` created (PART 0-4 coverage).
- `.claude/rules/config-rules.md` (PART 5, 6, 12) NOT yet created — PART 12 (Server Configuration) has not been read; do not create a partial file, read PART 12 first.
- Later-PART rule files (frontend-rules.md PART 16, etc.) not yet created — out of scope for this pass.
