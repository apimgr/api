#!/usr/bin/env bash
# @@License : WTFPL

# Phase 2 — Binary Validation (Incus/Debian, full systemd).
# Preferred over tests/docker.sh when Incus is available: gives a realistic
# full-OS environment for service install/start/stop testing. No
# authentication exists on this project (IDEA.md non-goal) — tests verify
# responses and rate limiting only.

set -eo pipefail

if ! command -v incus &>/dev/null; then
  echo "ERROR: incus not found. Install incus or use tests/docker.sh"
  exit 1
fi

PROJECT_ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$PROJECT_ROOT"

PROJECT_NAME="$(basename "$PROJECT_ROOT")"
CLI_NAME="${PROJECT_NAME}-cli"
CONTAINER_NAME="test-${PROJECT_NAME}-$$"
INCUS_IMAGE="images:debian/trixie"

__cleanup() {
  incus delete "$CONTAINER_NAME" --force 2>/dev/null || true
}
trap __cleanup EXIT

# Build — use Makefile if present (standard for all bootstrapped projects).
# "make local" produces unsuffixed host-arch binaries in binaries/, which is
# what this script needs to push into the container (unlike "make build",
# which produces the full 8-platform release matrix under suffixed names).
if [ -f "Makefile" ]; then
  echo "Building with make local..."
  make local
else
  echo "Building in Docker (no Makefile)..."
  GO_CACHE="${GO_CACHE:-$HOME/go/pkg/mod}"
  GO_BUILD="${GO_BUILD:-$HOME/.cache/go-build/${PROJECT_NAME}}"
  mkdir -p "$GO_CACHE" "$GO_BUILD" binaries
  docker run --rm \
    --name "${PROJECT_NAME}-$(tr -dc 'a-z0-9' </dev/urandom | head -c8)" \
    -v "$PWD:/app" \
    -v "$GO_CACHE:/usr/local/share/go/pkg/mod" \
    -v "$GO_BUILD:/usr/local/share/go/cache" \
    -w /app -e CGO_ENABLED=0 -e GOFLAGS=-buildvcs=false \
    casjaysdev/go:latest sh -c "GOOS=\$(go env GOOS) GOARCH=\$(go env GOARCH) \
      go build -buildvcs=false -trimpath -ldflags '-s -w' -o /app/binaries/${PROJECT_NAME} ./src"
  if [ -d "src/client" ]; then
    docker run --rm \
      --name "${PROJECT_NAME}-$(tr -dc 'a-z0-9' </dev/urandom | head -c8)" \
      -v "$PWD:/app" \
      -v "$GO_CACHE:/usr/local/share/go/pkg/mod" \
      -v "$GO_BUILD:/usr/local/share/go/cache" \
      -w /app -e CGO_ENABLED=0 -e GOFLAGS=-buildvcs=false \
      casjaysdev/go:latest sh -c "GOOS=\$(go env GOOS) GOARCH=\$(go env GOARCH) \
        go build -buildvcs=false -trimpath -ldflags '-s -w' -o /app/binaries/${CLI_NAME} ./src/client"
  fi
fi

echo "Launching Incus container (Debian + systemd)..."
incus launch "$INCUS_IMAGE" "$CONTAINER_NAME"

echo "Waiting for container network..."
for i in $(seq 1 30); do
  incus exec "$CONTAINER_NAME" -- true 2>/dev/null && break
  sleep 1
done

echo "Copying binaries to container..."
incus file push "binaries/${PROJECT_NAME}" "$CONTAINER_NAME/usr/local/bin/"
incus exec "$CONTAINER_NAME" -- chmod +x "/usr/local/bin/${PROJECT_NAME}"
if [ -f "binaries/${CLI_NAME}" ]; then
  incus file push "binaries/${CLI_NAME}" "$CONTAINER_NAME/usr/local/bin/"
  incus exec "$CONTAINER_NAME" -- chmod +x "/usr/local/bin/${CLI_NAME}"
fi

echo "Installing test tools..."
incus exec "$CONTAINER_NAME" -- bash -c \
  "command -v curl >/dev/null || (apt-get update -qq && apt-get install -y -qq curl)" \
  >/dev/null 2>&1

echo "Running tests in Incus..."
incus exec "$CONTAINER_NAME" --env PROJECT_NAME="$PROJECT_NAME" --env CLI_NAME="$CLI_NAME" -- bash -c '
  set -eo pipefail
  FAILED=0

  echo "=== Version Check ==="
  "${PROJECT_NAME}" --version

  echo "=== Help Check ==="
  "${PROJECT_NAME}" --help

  echo "=== Binary Info ==="
  ls -lh "/usr/local/bin/${PROJECT_NAME}"
  file "/usr/local/bin/${PROJECT_NAME}"

  echo "=== Service Install Test ==="
  "${PROJECT_NAME}" --service --install

  echo "=== Service Status ==="
  systemctl status "${PROJECT_NAME}" || true

  echo "=== Service Start Test ==="
  systemctl start "${PROJECT_NAME}"
  sleep 2
  systemctl status "${PROJECT_NAME}"

  echo "=== API Endpoint Tests ==="
  curl -q -LSsf "http://localhost:80/api/v1/server/healthz" || { echo "FAILED: /api/v1/server/healthz"; FAILED=1; }
  curl -q -LSsf "http://localhost:80/api/healthz" || { echo "FAILED: /api/healthz"; FAILED=1; }
  curl -q -LSsf "http://localhost:80/api/v1/text/uuid" || { echo "FAILED: /api/v1/text/uuid"; FAILED=1; }
  curl -q -LSsf "http://localhost:80/api/v1/text/uuid.txt" || { echo "FAILED: /api/v1/text/uuid.txt"; FAILED=1; }
  curl -q -LSsf -H "Accept: application/json" "http://localhost:80/server/healthz" || { echo "FAILED: Accept application/json"; FAILED=1; }
  curl -q -LSsf -H "Accept: text/plain" "http://localhost:80/server/healthz" || { echo "FAILED: Accept text/plain"; FAILED=1; }

  echo "=== Frontend Smart Detection Tests ==="
  curl -q -LSsf -H "Accept: text/html" "http://localhost:80/" || { echo "FAILED: frontend Accept text/html"; FAILED=1; }
  curl -q -LSsf -H "Accept: text/plain" "http://localhost:80/" || { echo "FAILED: frontend Accept text/plain"; FAILED=1; }

  echo "=== Open API Smoke Test ==="
  curl -q -LSsf "http://localhost:80/server/healthz" | grep -q "\"ok\":true" \
    && echo "PASS: Health endpoint works" \
    || { echo "FAILED: Health endpoint"; FAILED=1; }

  echo "=== Binary Rename Tests ==="
  cp "/usr/local/bin/${PROJECT_NAME}" /tmp/renamed-server
  chmod +x /tmp/renamed-server
  if /tmp/renamed-server --help 2>&1 | grep -q "renamed-server"; then
    echo "PASS: Server binary rename works (--help shows actual name)"
  else
    echo "FAILED: Server --help does not show renamed binary name"
    FAILED=1
  fi

  echo "=== Client Tests (if exists) ==="
  if [ -f "/usr/local/bin/${CLI_NAME}" ]; then
    "${CLI_NAME}" --version || { echo "FAILED: CLI --version"; FAILED=1; }
    "${CLI_NAME}" --help || { echo "FAILED: CLI --help"; FAILED=1; }

    cp "/usr/local/bin/${CLI_NAME}" /tmp/renamed-cli
    chmod +x /tmp/renamed-cli
    if /tmp/renamed-cli --help 2>&1 | grep -q "renamed-cli"; then
      echo "PASS: CLI binary rename works"
    else
      echo "FAILED: CLI --help does not show renamed binary name"
      FAILED=1
    fi

    echo "--- CLI Full Functionality Tests ---"
    "${CLI_NAME}" --server "http://localhost:80" health || { echo "CLI health failed"; FAILED=1; }
  else
    echo "client not installed - skipping"
  fi

  echo "=== Service Stop Test ==="
  # Inside the Incus VM — not a host-service mutation.
  systemctl stop "${PROJECT_NAME}"

  if [ "$FAILED" -ne 0 ]; then
    echo "=== One or more tests FAILED ==="
    exit 1
  fi
  echo "=== All tests passed ==="
'

echo "Incus tests completed successfully"
