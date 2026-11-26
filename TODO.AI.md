# Future External Provider Changes

- [ ] [Datamuse's API page](https://datamuse.com/api/) says an API key becomes required on 2027-02-01. Before that date, confirm the provider policy and add an encrypted operator-configured key or replace the dependency; the current thesaurus client sends no key.

# AI.md Alignment

- [ ] Audit and implement AI.md PARTS 0–12: repository structure, metadata, paths, configuration, modes, binaries, CLI, errors, caching, database, security, logging, and server configuration.
- [ ] Audit and implement AI.md PARTS 13–16: health/versioning, routes and response contracts, TLS, every server-rendered page, accessibility, SEO, static assets, and content negotiation.
- [ ] Audit and implement AI.md PARTS 17–24: email, notifications, scheduler, GeoIP, metrics, backup/restore, updates, privilege handling, and service managers.
- [ ] Audit and implement AI.md PARTS 25–28: Makefile, Docker, all provider CI workflows, container behavior, and both testing phases.
- [ ] Audit and implement AI.md PARTS 29–32: current MkDocs documentation, translations and accessibility, Tor/I2P overlays, and complete api-cli parity.
- [ ] Reconcile README.md, IDEA.md, generated API descriptions, `.claude/rules/`, and all operator/user documentation with implemented behavior without changing AI.md.
- [ ] Run the required containerized build, tests, lint, workflow checks, documentation checks, and applicable binary validation; resolve every failure before completion.
