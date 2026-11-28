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

REMOTE_URL="$(git remote get-url origin 2>/dev/null || true)"
PROJECT_NAME="$(sed -E 's#.*[:/]([^/]+)(\.git)?$#\1#' <<<"$REMOTE_URL")"
PROJECT_ORG="$(sed -E 's#.*[:/]([^/]+)/[^/]+(\.git)?$#\1#' <<<"$REMOTE_URL")"
PROJECT_NAME="${PROJECT_NAME:-$(basename "$PROJECT_ROOT")}"
PROJECT_ORG="${PROJECT_ORG:-$(basename "$(dirname "$PROJECT_ROOT")")}"
INTERNAL_NAME="$(sed -nE 's/^internal_name:[[:space:]]*([^[:space:]]+).*$/\1/p' IDEA.md | head -n1)"
INTERNAL_NAME="${INTERNAL_NAME:-$PROJECT_NAME}"
RUN_ID="$(od -An -N4 -tx1 /dev/urandom | tr -d ' \n')"

NETWORK_NAME="${PROJECT_NAME}-e2e-${RUN_ID}"
BROWSER_NAME="${PROJECT_NAME}-e2e-browser-${RUN_ID}"
TESTS_NAME="${PROJECT_NAME}-e2e-tests-${RUN_ID}"
E2E_SERVER_HOST="api.example.com"

if ! command -v docker >/dev/null 2>&1; then
  echo "ERROR: docker is required — E2E tests never run on the host" >&2
  exit 69
fi

GO_IMAGE="casjaysdev/go:latest"
BROWSER_IMAGE="chromedp/headless-shell:latest"
GO_CACHE="${GO_CACHE:-$HOME/go/pkg/mod}"
GO_BUILD="${GO_BUILD:-$HOME/.cache/go-build/${PROJECT_NAME}}"

mkdir -p "${TMPDIR:-/tmp}/${PROJECT_ORG}"
ARTIFACT_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/${PROJECT_ORG}/${INTERNAL_NAME}-XXXXXX")"
chmod 0700 "$ARTIFACT_ROOT"
UPLOAD_DIR="${ARTIFACT_ROOT}/uploads"
mkdir -m 0755 "$UPLOAD_DIR"
DOCKER_MEM="${DOCKER_MEM:-4g}"
DOCKER_CPUS="${DOCKER_CPUS:-2}"
DOCKER_CPU_SHARES="${DOCKER_CPU_SHARES:-256}"
BROWSER_MEM="${BROWSER_MEM:-2g}"
BROWSER_CPUS="${BROWSER_CPUS:-2}"
DOCKER_TIMEOUT="${DOCKER_TIMEOUT:-3600}"

# Stop only the containers and network this run created, SIGTERM first.
__cleanup() {
  local status=$?
  if timeout "$DOCKER_TIMEOUT" docker container inspect "$TESTS_NAME" >/dev/null 2>&1; then
    timeout "$DOCKER_TIMEOUT" docker stop -t 10 "$TESTS_NAME" >/dev/null 2>&1 || true
    timeout "$DOCKER_TIMEOUT" docker rm -f "$TESTS_NAME" >/dev/null 2>&1 || true
  fi
  if timeout "$DOCKER_TIMEOUT" docker container inspect "$BROWSER_NAME" >/dev/null 2>&1; then
    timeout "$DOCKER_TIMEOUT" docker stop -t 10 "$BROWSER_NAME" >/dev/null 2>&1 || true
    timeout "$DOCKER_TIMEOUT" docker rm -f "$BROWSER_NAME" >/dev/null 2>&1 || true
  fi
  if timeout "$DOCKER_TIMEOUT" docker network inspect "$NETWORK_NAME" >/dev/null 2>&1; then
    timeout "$DOCKER_TIMEOUT" docker network rm "$NETWORK_NAME" >/dev/null 2>&1 || true
  fi
  if [ "$status" -eq 0 ]; then
    rm -rf "$ARTIFACT_ROOT"
  else
    echo "Failure artifacts: ${ARTIFACT_ROOT}" >&2
  fi
  exit "$status"
}
trap __cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

mkdir -p "$GO_CACHE" "$GO_BUILD"

echo "=== Building ${PROJECT_NAME} (${GO_IMAGE}) ==="
timeout -k 30 "$DOCKER_TIMEOUT" docker run --rm \
  --no-healthcheck \
  --name "${PROJECT_NAME}-build-${RUN_ID}" \
  --memory="$DOCKER_MEM" --cpus="$DOCKER_CPUS" --cpu-shares="$DOCKER_CPU_SHARES" \
  -v "$PROJECT_ROOT:/app" \
  -v "${ARTIFACT_ROOT}:/artifacts" \
  -v "$GO_CACHE:/usr/local/share/go/pkg/mod" \
  -v "$GO_BUILD:/usr/local/share/go/cache" \
  -w /app -e CGO_ENABLED=0 -e GOFLAGS=-buildvcs=false \
  "$GO_IMAGE" sh -c "go generate ./src/swagger && GOOS=\$(go env GOOS) GOARCH=\$(go env GOARCH) \
    go build -tags e2e -buildvcs=false -trimpath -o /artifacts/${PROJECT_NAME} ./src"

echo "=== Creating network ${NETWORK_NAME} ==="
timeout "$DOCKER_TIMEOUT" docker network create --internal --subnet=192.0.2.0/24 "$NETWORK_NAME" >/dev/null
NETWORK_GATEWAY="$(timeout "$DOCKER_TIMEOUT" docker network inspect --format '{{(index .IPAM.Config 0).Gateway}}' "$NETWORK_NAME")"
NETWORK_PREFIX="${NETWORK_GATEWAY%.*}"
BROWSER_IP="${NETWORK_PREFIX}.2"
TESTS_IP="${NETWORK_PREFIX}.3"

echo "=== Starting headless browser ${BROWSER_NAME} ==="
timeout -k 30 "$DOCKER_TIMEOUT" docker run -d --rm --no-healthcheck \
  --name "$BROWSER_NAME" \
  --network "$NETWORK_NAME" \
  --ip "$BROWSER_IP" \
  --memory="$BROWSER_MEM" --cpus="$BROWSER_CPUS" --cpu-shares="$DOCKER_CPU_SHARES" \
  --shm-size=2g \
  -v "${UPLOAD_DIR}:/uploads:ro" \
  "$BROWSER_IMAGE" \
  --no-sandbox \
  --disable-gpu \
  --disable-dev-shm-usage \
  --remote-debugging-address=0.0.0.0 >/dev/null

echo "Waiting for the browser to accept CDP connections..."
READY=0
for _ in $(seq 1 60); do
  PROBE_NAME="${PROJECT_NAME}-e2e-probe-${RUN_ID}"
  if timeout -k 30 "$DOCKER_TIMEOUT" docker run --rm --no-healthcheck --name "$PROBE_NAME" \
    --memory=256m --cpus=0.25 --cpu-shares="$DOCKER_CPU_SHARES" --network "$NETWORK_NAME" "$GO_IMAGE" \
    sh -c "wget -q -O - http://${BROWSER_IP}:9222/json/version >/dev/null 2>&1"; then
    READY=1
    break
  fi
  sleep 2
done
if [ "$READY" -ne 1 ]; then
  echo "ERROR: ${BROWSER_NAME} never became ready on port 9222" >&2
  timeout "$DOCKER_TIMEOUT" docker logs "$BROWSER_NAME" 2>&1 | tail -n 40 >&2 || true
  exit 1
fi

echo "=== Running E2E suite (all three tiers) ==="
GO_TEST_ARGS=()
if [ -n "${E2E_TEST_RUN:-}" ]; then
  GO_TEST_ARGS+=(-run "$E2E_TEST_RUN")
fi
timeout -k 30 2400 docker run --rm \
  --no-healthcheck \
  --name "$TESTS_NAME" \
  --memory="$DOCKER_MEM" --cpus="$DOCKER_CPUS" --cpu-shares="$DOCKER_CPU_SHARES" \
  --network "$NETWORK_NAME" \
  --ip "$TESTS_IP" \
  --network-alias "$TESTS_NAME" \
  --network-alias "$E2E_SERVER_HOST" \
  --add-host "example.com:${TESTS_IP}" \
  --add-host "whois.iana.org:${TESTS_IP}" \
  -v "$PROJECT_ROOT:/app" \
  -v "$GO_CACHE:/usr/local/share/go/pkg/mod" \
  -v "$GO_BUILD:/usr/local/share/go/cache" \
  -v "${UPLOAD_DIR}:/uploads" \
  -v "${ARTIFACT_ROOT}:/artifacts" \
  -w /app \
  -e CGO_ENABLED=0 \
  -e GOFLAGS=-buildvcs=false \
  -e E2E_BINARY="/artifacts/${PROJECT_NAME}" \
  -e E2E_BROWSER_URL="http://${BROWSER_IP}:9222" \
  -e E2E_SERVER_HOST="$E2E_SERVER_HOST" \
  -e E2E_FIXTURE_IP="$TESTS_IP" \
  -e E2E_ARTIFACT_ROOT="/artifacts" \
  -e E2E_UPLOAD_ROOT="/uploads" \
  "$GO_IMAGE" sh -c "go generate ./src/swagger && go test -tags e2e -count=1 -timeout 35m -v ${GO_TEST_ARGS[*]} ./tests/e2e/..." \
  2>&1 | tee "${ARTIFACT_ROOT}/e2e-test.log"
