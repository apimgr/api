# Project Audit

Started: 2026-11-23

This tracks verified open findings from the AI.md alignment audit. Remove each
finding when its implementation and required verification are complete. Remove
this file when no findings remain.

## Open Findings

- [ ] PART 8, owner-token web management: the page and action handlers exist but are not mounted. `IDEA.md` omits the required 8-letter `owner_token` cookie suffix; route wiring remains dependent on the user's explicit value.
- [ ] PART 11, encryption-key rotation: rotation and migration support is being completed; verify wrapped previous-key storage, interruption recovery, eager re-encryption of token/report/contact/DNS values, grace-period reads, and expiry cleanup.
- [ ] PART 11, coordinated disclosure: the PGP key lifecycle implementation now exists in the tree. Run its lifecycle, report-rotation, and backup/restore checks and fix any gaps found.
- [ ] PART 30, localization: complete Italian translations and the full CLI help catalogs now exist in the tree. Run locale-integrity and coverage checks and fix any gaps found.
