# Development

The server is Go software and the companion client is part of the same repository. Follow the project specification in `AI.md` and product scope in `IDEA.md`; `AI.md` is read-only. Keep source changes in `src/`, tests alongside their packages or under the documented test directories, and MkDocs material in `docs/`.

## Build environment

Go builds and tests run in the project's Docker toolchain with `CGO_ENABLED=0`. Use the Makefile targets for normal development and release builds; the Makefile invokes the required containerized Go toolchain.

```bash
make dev
make local
make build
make test
```

`make dev` builds the server and client into an isolated temporary directory for quick iteration. `make local` creates production-style local binaries, and `make build` builds the local pair plus all eight server and client platform targets. Build products are written to the ignored `binaries/` directory. All builds run in one bounded Docker toolchain container with `CGO_ENABLED=0`.

`make test` runs the complete Go package suite with coverage in Docker. `make release` is for manual, local stable releases only: set the version in `release.txt` to a stable `X.Y.Z`; it rejects devel and prerelease versions, builds all targets, prepares checksums and a source archive, and publishes the `vX.Y.Z` GitHub release. Beta and devel releases are produced by CI. `make docker` builds and pushes the multi-architecture container; set `REGISTRY` for the destination before running it.

## Tests and lint

Run the repository's documented test and lint targets through the project validation workflow before submitting changes. Go commands must run in Docker and use `CGO_ENABLED=0`. The tests cover API behavior, server-rendered pages, generated API descriptions, clients, and platform-specific behavior where applicable. Do not rely on a documentation-only edit to justify claims about runtime behavior; compare the relevant requirement in `AI.md` and implementation first.

`make test` runs the Go test suite. For the basic binary-validation tier, run `tests/run_tests.sh`; it requires Docker and runs `tests/docker.sh` without falling back to Incus. That script uses `make build` to place server and client binaries in `binaries/`, then runs command-line, HTTP, owner-token, Tor, and I2P checks in an Alpine container. All runtime data stays in a project-prefixed OS temporary directory inside that container. Overlay bootstrap checks may print `SKIP` when outbound networking is unavailable; `REQUIRE_OVERLAY=1` makes healthy overlay services mandatory.

Run `tests/incus.sh` explicitly for the full service lifecycle under systemd and OpenRC. `tests/e2e.sh` is also an explicit, on-demand browser suite. The production image command-presence and Tor bootstrap contract is checked after `make docker` and in CI, not by the basic binary-validation runner.

## Server-rendered pages

Pages use Go `html/template` and the shared server shell. Keep the primary flow usable without JavaScript, use semantic labels and headings, provide keyboard-accessible controls, and preserve responsive behavior at narrow widths. Use the shared template functions and components rather than introducing a second page framework. Interactive API explorers are the documented exception to the no-JavaScript requirement; surrounding page chrome and explanatory content remain usable without JavaScript.

## Documentation

The `docs/` directory is exclusively the MkDocs documentation set. Update the relevant pages when a feature, route, setting, client command, protocol, or operator workflow changes. `docs/configuration.md` covers configuration, `docs/api.md` covers routes and formats, and the security and integration pages describe the corresponding public surfaces. The translation workflow is in [Translating](translating.md).

Preview the documentation with the project's MkDocs dependencies and verify internal links before submitting documentation changes.

## Contribution workflow

Keep changes focused and documented. Add or update tests for behavior changes, update `IDEA.md` when the product scope changes, and keep `README.md` current. Follow the repository's commit-message and gitcommit workflow. Do not commit generated binaries, runtime data, logs, or credentials.
