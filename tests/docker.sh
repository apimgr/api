#!/usr/bin/env bash
# @@License : WTFPL

# Phase 2 — Binary Validation (Docker/Alpine).
# Builds the server and CLI binaries via the casjaysdev/go:latest toolchain
# image, then exercises the compiled binaries end-to-end inside an
# alpine:latest container. No authentication exists on this project
# (IDEA.md non-goal) — tests verify responses and rate limiting only.

set -eo pipefail

PROJECT_ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$PROJECT_ROOT"

PROJECT_NAME="$(basename "$PROJECT_ROOT")"
CLI_NAME="${PROJECT_NAME}-cli"
TEST_PORT="64580"

# Build — use Makefile if present (standard for all bootstrapped projects).
# "make local" produces unsuffixed host-arch binaries in binaries/, which is
# what this script needs to run directly (unlike "make build", which
# produces the full 8-platform release matrix under suffixed filenames).
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

echo "Testing in Docker (Alpine)..."
docker run --rm \
  --name "${PROJECT_NAME}-$(tr -dc 'a-z0-9' </dev/urandom | head -c8)" \
  -v "$PWD/binaries:/app" \
  -e PROJECT_NAME="$PROJECT_NAME" \
  -e CLI_NAME="$CLI_NAME" \
  -e TEST_PORT="$TEST_PORT" \
  alpine:latest sh -c '
    set -e
    FAILED=0
    apk add --no-cache curl bash file jq >/dev/null

    chmod +x "/app/${PROJECT_NAME}"
    [ -f "/app/${CLI_NAME}" ] && chmod +x "/app/${CLI_NAME}"

    echo "=== Version Check ==="
    "/app/${PROJECT_NAME}" --version

    echo "=== Help Check ==="
    "/app/${PROJECT_NAME}" --help

    echo "=== Binary Info ==="
    ls -lh "/app/${PROJECT_NAME}"
    file "/app/${PROJECT_NAME}"

    echo "=== Starting Server for API Tests ==="
    "/app/${PROJECT_NAME}" --port "$TEST_PORT" >/tmp/server.log 2>&1 &
    SERVER_PID=$!
    for i in $(seq 1 30); do
      curl -q -LSs -o /dev/null "http://localhost:${TEST_PORT}/server/healthz" && break
      sleep 1
    done

    echo "=== API Endpoint Tests ==="
    curl -q -LSsf "http://localhost:${TEST_PORT}/api/v1/server/healthz" || { echo "FAILED: /api/v1/server/healthz"; FAILED=1; }
    curl -q -LSsf "http://localhost:${TEST_PORT}/api/healthz" || { echo "FAILED: /api/healthz"; FAILED=1; }
    curl -q -LSsf "http://localhost:${TEST_PORT}/api/v1/text/uuid" || { echo "FAILED: /api/v1/text/uuid"; FAILED=1; }
    curl -q -LSsf "http://localhost:${TEST_PORT}/api/v1/text/uuid.txt" || { echo "FAILED: /api/v1/text/uuid.txt"; FAILED=1; }
    curl -q -LSsf -H "Accept: application/json" "http://localhost:${TEST_PORT}/server/healthz" || { echo "FAILED: Accept application/json"; FAILED=1; }
    curl -q -LSsf -H "Accept: text/plain" "http://localhost:${TEST_PORT}/server/healthz" || { echo "FAILED: Accept text/plain"; FAILED=1; }

    echo "=== Frontend Smart Detection Tests ==="
    curl -q -LSsf -H "Accept: text/html" "http://localhost:${TEST_PORT}/" || { echo "FAILED: frontend Accept text/html"; FAILED=1; }
    curl -q -LSsf -H "Accept: text/plain" "http://localhost:${TEST_PORT}/" || { echo "FAILED: frontend Accept text/plain"; FAILED=1; }

    echo "=== Open API Smoke Test ==="
    curl -q -LSsf "http://localhost:${TEST_PORT}/server/healthz" | grep -q "\"ok\":true" \
      && echo "PASS: Health endpoint works" \
      || { echo "FAILED: Health endpoint"; FAILED=1; }

    echo "=== Binary Rename Tests ==="
    cp "/app/${PROJECT_NAME}" /app/renamed-server
    chmod +x /app/renamed-server
    if /app/renamed-server --help 2>&1 | grep -q "renamed-server"; then
      echo "PASS: Server binary rename works (--help shows actual name)"
    else
      echo "FAILED: Server --help does not show renamed binary name"
      FAILED=1
    fi

    echo "=== Client Tests (if exists) ==="
    if [ -f "/app/${CLI_NAME}" ]; then
      "/app/${CLI_NAME}" --version || { echo "FAILED: CLI --version"; FAILED=1; }
      "/app/${CLI_NAME}" --help || { echo "FAILED: CLI --help"; FAILED=1; }

      cp "/app/${CLI_NAME}" /app/renamed-cli
      chmod +x /app/renamed-cli
      if /app/renamed-cli --help 2>&1 | grep -q "renamed-cli"; then
        echo "PASS: CLI binary rename works"
      else
        echo "FAILED: CLI --help does not show renamed binary name"
        FAILED=1
      fi

      echo "--- CLI Full Functionality Tests ---"
      "/app/${CLI_NAME}" --server "http://localhost:${TEST_PORT}" health || { echo "CLI health failed"; FAILED=1; }
    else
      echo "client not built - skipping"
    fi

    echo "=== Stopping Server ==="
    kill "$SERVER_PID"
    wait "$SERVER_PID" 2>/dev/null || true

    if [ "$FAILED" -ne 0 ]; then
      echo "=== One or more tests FAILED ==="
      exit 1
    fi
    echo "=== All tests passed ==="
  '

echo "Docker tests completed successfully"
