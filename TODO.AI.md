# AI.md Alignment Tasks

## P0: Project Contract

- [ ] Align IDEA.md business behavior with AI.md without changing Project variables.
- [ ] Inventory all project-defined API routes in IDEA.md and make route drift checking enforce the contract.
- [ ] Remove or complete any advertised page/API capability that is a stub, incomplete, or outside the declared product scope.

## P1: Server, Client, and Infrastructure

- [ ] Reconcile all required AI.md parts against implementation, including server configuration, security, persistence, services, scheduler, integrations, CLI client, and eight-platform builds.
- [ ] Reconcile every server page and tool page with AI.md frontend, accessibility, localization, and no-JavaScript requirements.
- [ ] Add complete Italian locale support required by AI.md PART 30; obtain a complete Italian translation source rather than shipping English fallback text as an Italian locale.
- [ ] Implement the coordinated-disclosure PGP key lifecycle from AI.md PART 11: Ed25519/Curve25519 key generation with expiry, installation-secret encrypted private-key storage, transactional rotation of retained reports, import/export/delete, keyserver publishing with durable retry state, metadata, and backup/restore verification.
- [ ] Bring Docker, Compose, CI/CD, release, and GitHub policy files into AI.md compliance.
- [ ] Add and verify the required GitLab, Gitea, Forgejo, and Jenkins CI workflows plus Renovate configuration under AI.md PART 27.

## P1: Documentation and Delivery

- [ ] Update README.md, docs/, API specifications, and command help to describe current implemented behavior.
- [ ] Regenerate the AI.md-derived rule files and refresh the project loaders.
- [ ] Run the required test, lint, workflow, build, documentation, and end-to-end verification gates; fix failures and verify pushed CI.
- [ ] Remove this temporary checklist after all alignment work is complete.
