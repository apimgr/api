# Contributing to api

## Development setup

Use Go 1.26.4 or the repository's `casjaysdev/go:latest` build image. Set
`CGO_ENABLED=0` for Go builds.

```sh
make dev
make test
docker run --rm --name "api-$(printf '%08x' "$RANDOM")" -e CGO_ENABLED=0 -v "$PWD:/app" -w /app casjaysdev/go:latest go vet ./...
docker run --rm --name "api-$(printf '%08x' "$RANDOM")" -e CGO_ENABLED=0 -v "$PWD:/app" -w /app casjaysdev/go:latest staticcheck ./...
docker run --rm --name "api-$(printf '%08x' "$RANDOM")" -v "$PWD:/app" -w /app python:3.12-slim sh -c 'pip install -r docs/requirements.txt && mkdocs build --strict --site-dir /tmp/api-docs-site'
```

Run the relevant checks after changing code, and update tests and documentation
with behavior changes. Do not add credentials, tokens, passwords, or private
keys to commits.

See the [translation guide](../docs/translating.md) for locale rules and review
requirements.

## Changes and pull requests

Create a focused branch from `main`, describe the user-visible behavior and
verification performed, and submit a pull request to `apimgr/api`. Include
reproduction steps for fixes and note any configuration or documentation
changes. Do not combine unrelated changes in one pull request.

## Vulnerability reports

Do not report vulnerabilities in public issues or pull requests. Use GitHub's
private vulnerability reporting at
<https://github.com/apimgr/api/security/advisories/new>. See
[SECURITY.md](SECURITY.md) for the disclosure process.
