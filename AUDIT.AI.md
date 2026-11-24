# Project Audit

Started: 2026-11-23

This is a temporary tracking file for the exhaustive AI.md alignment audit. Remove it after all listed findings are fixed and committed.

## Pass 5: Spec Compliance

- [ ] PART 2, dependency licenses: `LICENSE.md` omits direct Go dependencies and has stale x/* versions versus `go.mod`; run the prescribed `go-licenses` scan inside the toolchain container and reconcile all attributions. BSD-3 non-endorsement clause added.
- [ ] PART 11, security.txt: `src/server/server.go:1384` emits only an email Contact, Expires, and Preferred-Languages; mandatory report URL, rotating security ID contact, and Policy link are missing (AI.md 15357-15428).
- [ ] PART 11, coordinated disclosure: no security report persistence, encryption, researcher status page, policy/thanks pages, or maintenance commands exist; see `src/database` and `src/server` against AI.md 15575-15770.
- [ ] PART 11, MCP: no canonical endpoint or aliases exist in `src/server/server.go`, despite the always-on token-authenticated MCP requirements in AI.md 15496-15574.
- [ ] PART 16, llms.txt: no `/.well-known/llms.txt` or `/llms.txt` routes existed in `src/server/server.go` despite AI.md 15308 and 15428-15495. A handler is being implemented; route/config wiring remains.
- [ ] PART 26, final image packages: production and development Dockerfiles omitted required `i2pd`, `tzdata`, and `ca-certificates` (AI.md 35628-35630); Dockerfile edits made, pending primary validation and commit.
- [ ] PART 26, image contract checks: shipped-image builds had no required command-presence or Tor health checks (AI.md 35628-35630). Shared checker and CI/test wiring are implemented, pending primary validation and commit.

## Pass 4: Documentation

- [ ] PART 2/3: README has duplicate System Service sections; current README is primary-owned and requires reconciliation.
- [ ] PART 11/16 security and AI-discovery documentation and routes are absent or incomplete alongside the corresponding implementation; update README and `docs/` after features are integrated.
