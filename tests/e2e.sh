#!/usr/bin/env bash
# @@License : WTFPL

# Browser E2E suite (AI.md PART 28 — "Browser E2E Testing").
# Manual, developer-initiated only: never called by run_tests.sh, never by
# "make test", never a required CI check. Builds the server binary with the
# casjaysdev/go:latest toolchain image, starts a chromedp/headless-shell
# sidecar, then runs "go test -tags e2e ./tests/e2e/..." in a second container
# on a private network so the browser can reach the server under test.

set -eo pipefail

PROJECT_ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$PROJECT_ROOT"

PROJECT_NAME="$(basename "$PROJECT_ROOT")"
PROJECT_ORG="$(basename "$(dirname -- "$PROJECT_ROOT")")"
RUN_ID="$(tr -dc 'a-z0-9' </dev/urandom | head -c8)"

NETWORK_NAME="${PROJECT_NAME}-e2e-${RUN_ID}"
BROWSER_NAME="${PROJECT_NAME}-e2e-browser-${RUN_ID}"
TESTS_NAME="${PROJECT_NAME}-e2e-tests-${RUN_ID}"

GO_IMAGE="casjaysdev/go:latest"
BROWSER_IMAGE="chromedp/headless-shell:latest"
GO_CACHE="${GO_CACHE:-$HOME/go/pkg/mod}"
GO_BUILD="${GO_BUILD:-$HOME/.cache/go-build/${PROJECT_NAME}}"

ARTIFACT_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/${PROJECT_ORG}/${PROJECT_NAME}-XXXXXX" 2>/dev/null || {
  mkdir -p "${TMPDIR:-/tmp}/${PROJECT_ORG}"
  mktemp -d "${TMPDIR:-/tmp}/${PROJECT_ORG}/${PROJECT_NAME}-XXXXXX"
})"

# Stop only the containers and network this run created, SIGTERM first.
cleanup() {
  local status=$?
  if docker container inspect "$TESTS_NAME" >/dev/null 2>&1; then
    docker stop -t 10 "$TESTS_NAME" >/dev/null 2>&1 || true
    docker rm -f "$TESTS_NAME" >/dev/null 2>&1 || true
  fi
  if docker container inspect "$BROWSER_NAME" >/dev/null 2>&1; then
    docker stop -t 10 "$BROWSER_NAME" >/dev/null 2>&1 || true
    docker rm -f "$BROWSER_NAME" >/dev/null 2>&1 || true
  fi
  if docker network inspect "$NETWORK_NAME" >/dev/null 2>&1; then
    docker network rm "$NETWORK_NAME" >/dev/null 2>&1 || true
  fi
  echo "Artifacts: ${ARTIFACT_ROOT}"
  exit "$status"
}
trap cleanup EXIT INT TERM

command -v docker >/dev/null 2>&1 || {
  echo "ERROR: docker is required — E2E tests never run on the host" >&2
  exit 1
}

mkdir -p "$GO_CACHE" "$GO_BUILD" "$PROJECT_ROOT/binaries"

echo "=== Building ${PROJECT_NAME} (${GO_IMAGE}) ==="
timeout 900 docker run --rm \
  --name "${PROJECT_NAME}-build-${RUN_ID}" \
  -v "$PROJECT_ROOT:/app" \
  -v "$GO_CACHE:/usr/local/share/go/pkg/mod" \
  -v "$GO_BUILD:/usr/local/share/go/cache" \
  -w /app -e CGO_ENABLED=0 -e GOFLAGS=-buildvcs=false \
  "$GO_IMAGE" sh -c "GOOS=\$(go env GOOS) GOARCH=\$(go env GOARCH) \
    go build -buildvcs=false -trimpath -o /app/binaries/${PROJECT_NAME} ./src"

echo "=== Creating network ${NETWORK_NAME} ==="
docker network create "$NETWORK_NAME" >/dev/null

echo "=== Starting headless browser ${BROWSER_NAME} ==="
docker run -d --rm \
  --name "$BROWSER_NAME" \
  --network "$NETWORK_NAME" \
  --shm-size=2g \
  "$BROWSER_IMAGE" \
  --no-sandbox \
  --disable-gpu \
  --disable-dev-shm-usage \
  --remote-debugging-address=0.0.0.0 \
  --remote-debugging-port=9222 >/dev/null

echo "Waiting for the browser to accept CDP connections..."
READY=0
for _ in $(seq 1 60); do
  if docker run --rm --network "$NETWORK_NAME" "$GO_IMAGE" \
    sh -c "wget -q -O - http://${BROWSER_NAME}:9222/json/version >/dev/null 2>&1"; then
    READY=1
    break
  fi
  sleep 2
done
if [ "$READY" -ne 1 ]; then
  echo "ERROR: ${BROWSER_NAME} never became ready on port 9222" >&2
  docker logs "$BROWSER_NAME" 2>&1 | tail -n 40 >&2 || true
  exit 1
fi

echo "=== Running E2E suite (all three tiers) ==="
timeout 2400 docker run --rm \
  --name "$TESTS_NAME" \
  --network "$NETWORK_NAME" \
  --network-alias "$TESTS_NAME" \
  -v "$PROJECT_ROOT:/app" \
  -v "$GO_CACHE:/usr/local/share/go/pkg/mod" \
  -v "$GO_BUILD:/usr/local/share/go/cache" \
  -v "${ARTIFACT_ROOT}:/artifacts" \
  -w /app \
  -e CGO_ENABLED=0 \
  -e GOFLAGS=-buildvcs=false \
  -e E2E_BINARY="/app/binaries/${PROJECT_NAME}" \
  -e E2E_BROWSER_URL="http://${BROWSER_NAME}:9222" \
  -e E2E_SERVER_HOST="$TESTS_NAME" \
  -e E2E_ARTIFACT_ROOT="/artifacts" \
  "$GO_IMAGE" go test -tags e2e -count=1 -timeout 35m -v ./tests/e2e/...
