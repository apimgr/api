# AI.md Alignment Tasks

## P0: Project Contract

- [ ] Record the user-approved 8-letter owner-token cookie suffix in IDEA.md and mount the owner-token web routes that depend on it.
- [ ] Verify that IDEA.md behavior and its route inventory match AI.md and that route-drift checks enforce the contract.
- [ ] Finish the full implementation review for advertised pages and API capabilities, including any remaining stubs.

## P1: Server, Client, and Infrastructure

- [ ] Reconcile all required AI.md parts against implementation, including server configuration, security, persistence, services, scheduler, integrations, CLI client, and eight-platform builds.
- [ ] Reconcile every server page and tool page with AI.md frontend, accessibility, localization, and no-JavaScript requirements.
- [ ] Verify complete translated-key parity and Italian coverage under AI.md PART 30 after the current catalog changes.
- [ ] Verify the coordinated-disclosure PGP lifecycle implementation under AI.md PART 11, including expiry, encrypted storage, report rotation, import/export/delete, retry persistence, metadata, and backup/restore.
- [ ] Complete and verify AI.md PART 11 encryption-key rotation, including recovery from interrupted rotation and re-encryption of every dependent value.
- [ ] Bring Docker, Compose, CI/CD, release, and GitHub policy files into AI.md compliance.
- [ ] Add and verify the required GitLab, Gitea, Forgejo, and Jenkins CI workflows plus Renovate configuration under AI.md PART 27.

## Future External Provider Changes

- [ ] [Datamuse's API page](https://datamuse.com/api/) says an API key becomes required on 2027-02-01. Before that date, confirm the provider policy and add an encrypted operator-configured key or replace the dependency; the current thesaurus client sends no key.

## P1: Documentation and Delivery

- [ ] Update README.md, docs/, API specifications, and command help to describe current implemented behavior.
- [ ] Regenerate the AI.md-derived rule files and refresh the project loaders.
- [ ] Run the required test, lint, workflow, build, documentation, and end-to-end verification gates; fix failures and verify pushed CI.
- [ ] Remove this temporary checklist after all alignment work is complete.
