# Project SPEC

Project: api

Role: Efficient loader for AI.md

`AI.md` is the read-only implementation source of truth; `IDEA.md` defines this product. Read each relevant AI.md PART before making changes, and follow `.claude/rules/*.md` for the selected topic.

- Ask only when required behavior is missing or materially ambiguous; first check AI.md, IDEA.md, and repository state. Never guess product behavior.
- Never edit `AI.md` or change `IDEA.md` `## Project variables` without the user's confirmation.
- `api` is the server; `api-cli` is its required companion. There are no user accounts; owners are credential-only identities.
- Keep the server-rendered Go interface mobile-first, accessible, translated, and usable without JavaScript for core functions.
- Go builds and tests run in the Docker toolchain with `CGO_ENABLED=0`; follow `.claude/rules/testing-rules.md` and the test wrapper.
- CI supports GitHub Actions, GitLab CI, Gitea Actions, Forgejo Actions, and Jenkins; CI uses explicit commands, never `make`.
- Project configuration, paths, and implementations follow AI.md PARTS 3–12; services and APIs follow PARTS 13–24; build/deploy/test follows PARTS 25–29; i18n/frontend follow PARTS 16 and 30; overlays/client follow PARTS 31–32.
- Keep `AGENTS.md` as an exact file copy of this loader. Use repository TODO rules for implementation work; shared `TODO.API.md` is only for verified issues inside AI.md.
- Use the required `gitcommit` workflow for commits. Do not add AI attribution or co-author trailers.
