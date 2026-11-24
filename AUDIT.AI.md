# Project Audit

Started: 2026-11-23

This is a temporary tracking file for the exhaustive AI.md alignment audit. Remove it after all listed findings are fixed and committed.

## Pass 5: Spec Compliance

- [ ] PART 2, dependency licenses: `LICENSE.md` omits direct Go dependencies and has stale x/* versions versus `go.mod`; run the prescribed `go-licenses` scan inside the toolchain container and reconcile all attributions. BSD-3 non-endorsement clause added.
- [ ] PART 11, security.txt and public security overview: required report URL, expiry, policy link and security email are now emitted, but the instance security-id contact was removed because `/server/contact` does not validate it or enter security-report mode. Reintroduce that contact only with the validated two-window flow, request logging and form integration required by AI.md 15575-15640. The security overview also lacks the complete RFC contact rendering and PGP-key link (AI.md 15357-15428, 15690-15720).
- [ ] PART 11, coordinated disclosure: no security report persistence/encryption, security-mode contact form, researcher notification/status token, status/thanks pages, PGP key management, or security maintenance commands exist; see `src/database` and `src/server` against AI.md 15575-15770.
- [ ] PART 11, MCP: no canonical endpoint or aliases exist in `src/server/server.go`, despite the always-on token-authenticated MCP requirements in AI.md 15496-15574.
- [ ] PART 11, llms.txt: both required routes and a route-derived handler now exist, but it does not implement the required authentication/token instructions, MCP listing, API contact source, schema/custom-section config, or route classification excluding every admin-only route (AI.md 15428-15495). Its output currently states that token auth and MCP are unavailable so it does not advertise missing functionality.
- [ ] PART 5/16, Page Content and area-file loading: only `yaml.d/branding.yml` terms content is now read; the shared `operatorHTML` contract is currently implemented for terms only. General drop-in merge order, all area files, validation and last-good retention, 0600/0700 permission enforcement, migration from `server.yml`, full-directory hot reload, backup integration, and all other operator content entries remain absent (`src/config/config.go`, `src/server/page_content.go`; AI.md 7926-7982, 7944-7982, 27199-27234).
- [ ] PART 18, task panic isolation: scheduled task panics previously escaped `execute` and could terminate the process. Per-task recovery and a failed-run test are now authored in `src/scheduler/scheduler.go` and `src/scheduler/scheduler_test.go` (AI.md 30490-30497); primary validation/commit pending.
- [ ] PART 19, GeoIP upstream integrity/licensing: source still used retired jsDelivr assets and the obsolete combined country file, had no checksum verification, and attributed ASN/Country data to CC BY 4.0. Source URLs/files now match the required GitHub assets, checksum verification precedes atomic replacement, and LICENSE/IDEA/page attributions now say ASN/Country PDDL and City CC BY 4.0 (AI.md 30940-30968); primary validation/commit pending.

## Pass 2: Code Quality

- [ ] PART 20, metrics handler error handling: Grafana and Loki JSON encoders discarded `Encode` errors; both now log encoding failures in `src/metrics/handlers.go` (primary validation/commit pending).
- [ ] PART 20, metrics credentials: service tokens remain in `server.yml` and empty tokens disable their service instead of generating `mtr_` values in `secrets.yml` (`src/config/metrics.go`, `src/metrics/authmw.go`; AI.md 8023-8050, 31111). This depends on the missing shared secrets loader tracked above.
- [ ] PART 26, final image packages: production and development Dockerfiles omitted required `i2pd`, `tzdata`, and `ca-certificates` (AI.md 35628-35630); Dockerfile edits made, pending primary validation and commit.
- [ ] PART 26, image contract checks: shipped-image builds had no required command-presence or Tor health checks (AI.md 35628-35630). Shared checker and CI/test wiring are implemented, pending primary validation and commit.

## Pass 4: Documentation

- [ ] PART 2/3: README has duplicate System Service sections; current README is primary-owned and requires reconciliation.
- [ ] PART 4/16 security docs and routes were incomplete; `docs/security.md` now describes only current behavior and clearly lists absent features. Reconcile README and complete this documentation after the corresponding AI-required security implementation lands.
