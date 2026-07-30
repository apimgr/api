# CI/CD Workflow Rules (PART 27)

⚠️ **These rules are NON-NEGOTIABLE. Violations are bugs.** ⚠️

**Provider note:** `git remote get-url origin` resolves to
`https://github.com/apimgr/api` — this project uses **GitHub Actions only**
(`.github/workflows/`). Gitea/Forgejo/GitLab/Jenkins equivalents in the base
spec do not apply here.

## CRITICAL - NEVER DO
- Never use Makefile targets inside any workflow step — CI/CD always issues
  explicit `go build`/`go test` commands, never `make build`/`make test`
- Never use host-path caching in CI — use CI-native cache actions
  (`actions/cache`), never bind-mount a developer's local Go cache dirs
- Never pin a third-party Action to a mutable tag — every `uses:` MUST be
  SHA-pinned with a version comment (e.g.
  `actions/checkout@9c091bb21b7c1c1d1991bb908d89e4e9dddfe3e0  # v7.0.0`)
- Never let a `secret-scan` job use `github.event.repository.default_branch`
  as a diff fallback — use `github.event.before`/`github.event.after` with
  an empty-string fallback for `schedule` triggers, never the default branch
- Never skip a platform in `release.yml`'s build matrix — full 8-platform
  matrix (linux/darwin/windows/freebsd × amd64/arm64) every release
- Never set static/hardcoded `VERSION`/`COMMIT_ID`/`BUILD_DATE` env values —
  always computed in a "Set build info" step (`release.txt` or
  `GITHUB_REF_NAME`, `git rev-parse --short HEAD`, formatted date)
- Never let a branch-push workflow run share a cancellation group with a
  different ref, and never let a tag-release workflow cancel a different
  tag's in-flight run

## CRITICAL - ALWAYS DO
- Maintain the two required workflows: `ci.yml` (lint/test/build/security
  gates on push+PR) and `release.yml` (tag-triggered 8-platform release
  build + GitHub Release creation)
- Treat `beta.yml`, `daily.yml`, `docker.yml` as optional/project-specific —
  include only when the project actually needs that release channel
- Run all Go toolchain steps inside `container: image: casjaysdev/go:latest`
- Enforce ≥60% coverage in the `test` job (`go test -cover` + `bc -l`
  percentage gate), failing the job below threshold
- Include security jobs — `secret-scan` (TruffleHog), `workflow-policy`,
  `vuln-scan`, `image-scan` — running on push/PR and on a weekly schedule,
  with `if: github.event_name != 'schedule'` guarding the non-security jobs
- Use `concurrency: group: ${{ github.workflow }}-${{ github.ref }},
  cancel-in-progress: true` on branch-push workflows
- In `release.yml`: build all 8 platforms in a matrix job, upload artifacts
  per platform, then a separate `release` job downloads all artifacts,
  computes `VERSION`/`RELEASE_TAG` (v-prefix rule from PART 25), writes
  `version.txt`, builds a source tarball (excluding `.git`, `.github`,
  `.gitea`, `binaries/`, `releases/`), and publishes via
  `softprops/action-gh-release`
- If a `docker.yml` workflow exists: build via `docker/build-push-action`
  with QEMU + Buildx, apply `labels:` (per-platform image layers) AND
  `annotations:` (manifest index, visible cross-platform) — never static
  Dockerfile `LABEL`s

## KEY DECISIONS (pre-answered)
| Question | Answer | Spec Reference |
|----------|--------|----------------|
| Which provider/workflow dir? | GitHub Actions, `.github/workflows/` (confirmed via `git remote get-url origin`) | PART 27, Provider Table |
| Which workflows are mandatory? | `ci.yml`, `release.yml` | PART 27, Workflow Files |
| Which workflows are optional? | `beta.yml`, `daily.yml`, `docker.yml` — none currently exist in this repo; their absence is spec-sanctioned, not a compliance gap | PART 27, Workflow Files |
| Does actual `ci.yml` match the spec's job set? | **Partial** — has `secret-scan`, `lint`, `test`, `build`, `vuln-check` (spec names the equivalent job `vuln-scan`); **missing** `workflow-policy` and `image-scan` jobs the spec's security-jobs note calls for | Verified against `/root/Projects/github/apimgr/api/.github/workflows/ci.yml` |
| CI caching strategy? | CI-native (`actions/cache`), never developer host paths | PART 27, CI/CD vs Local Development |
| Toolchain container for Go steps? | `casjaysdev/go:latest`, every job | PART 27, GitHub Actions |
| Action pinning? | Full commit SHA + version comment, never a tag | PART 27, Action Pinning |
| Coverage gate? | ≥60%, enforced in the `test` job, fails below threshold | PART 27 / PART 28, Test Coverage |
| Release version/tag logic? | `release.txt` canonical; `v` prefix only for numeric semver (PART 25 rule reused) | PART 27, release.yml |
| Beta version format? | `{YYYYMMDDHHMMSS}-beta`, marked prerelease, no `v` prefix | PART 27, beta.yml |
| Daily version format? | `{YYYYMMDDHHMMSS}`, no suffix, rolling single `daily` release/tag (old one replaced) | PART 27, daily.yml |

## TERMINOLOGY
| Term | Meaning |
|------|---------|
| `ci.yml` | Required — lint, test (coverage gate), build, security jobs; runs on push/PR |
| `release.yml` | Required — tag-triggered 8-platform build + GitHub Release publish |
| `beta.yml` | Optional — push-to-`beta`-branch prerelease channel |
| `daily.yml` | Optional — scheduled (3am UTC) + main/master push rolling release |
| `docker.yml` | Optional — builds/pushes the container image with proper labels/annotations |
| Set build info step | The workflow step that computes `VERSION`/`COMMIT_ID`/`BUILD_DATE` at run time, never hardcoded |
| SHA-pin | Referencing a GitHub Action by full commit SHA with a `# vX.Y.Z` trailing comment |

## QUICK REFERENCE
- Only `.github/workflows/` matters for this project (GitHub-only)
- `ci.yml` + `release.yml` exist and are required; `beta.yml`/`daily.yml`/
  `docker.yml` are absent, which is allowed (all three are optional)
- Known gap in actual `ci.yml`: missing `workflow-policy` and `image-scan`
  jobs, and `vuln-check` vs. spec's `vuln-scan` naming — not fixed by this
  pass, should be tracked in `TODO.AI.md` if not already
- Every `go build`/`go test` in CI is explicit — never routed through `make`
- All Actions SHA-pinned with version comments; coverage gate ≥60%
- Release always covers all 8 platforms; version/tag logic follows PART
  25's `release.txt` + v-prefix rules

---
For complete details, see AI.md PART 27
