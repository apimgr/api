# Project Audit

Started: 2026-11-23

This tracks verified open findings from the AI.md alignment audit. Remove each
finding when its implementation and required verification are complete. Remove
this file when no findings remain.

## Open Findings

- [ ] PART 8, owner-token web management: the page and action handlers exist but are not mounted. `IDEA.md` also omits the required `owner_token` cookie identifier from Project variables, so the fixed eight-letter suffix cannot be wired without generating and recording that project value.
- [ ] PART 11, coordinated disclosure: report intake, encrypted storage/export, researcher status, and security pages exist, but the required `--maintenance pgp` key lifecycle is absent: generate, rotate, import, public/private export, delete, metadata, keyserver publishing with durable retry, and key backup/restore verification.
- [ ] PART 5/16, operator page content: `operatorHTML` currently resolves content only for the terms page. Implement the shared area-file merge/validation contract, permissions, last-good retention and hot reload, migration, and backup integration for every configured content entry.
- [ ] PART 30, Italian localization: Italian locale files exist, but required complete translated coverage has not been established; replace English fallback entries with a complete verified Italian translation and run translation-integrity checks.
