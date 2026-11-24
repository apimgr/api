#!/usr/bin/env bash
# @@License : WTFPL

# Phase 2 — binary validation in an Alpine application container.
set -eo pipefail

if ! command -v docker >/dev/null 2>&1; then
  echo "ERROR: docker not found: binary validation requires Docker" >&2
  exit 69
fi

PROJECT_ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$PROJECT_ROOT"
REMOTE_URL="$(git remote get-url origin 2>/dev/null || true)"
PROJECT_ORG="$(sed -E 's#.*[:/]([^/]+)/[^/]+(\.git)?$#\1#' <<<"$REMOTE_URL")"
PROJECT_NAME="$(basename "$PROJECT_ROOT")"
INTERNAL_NAME="$(sed -nE 's/^internal_name:[[:space:]]*([^[:space:]]+).*$/\1/p' IDEA.md | head -n1)"
PROJECT_ORG="${PROJECT_ORG:-$(basename "$(dirname "$PROJECT_ROOT")")}"
INTERNAL_NAME="${INTERNAL_NAME:-$PROJECT_NAME}"
CLI_NAME="${PROJECT_NAME}-cli"
BIN_DIR="${BINDIR:-binaries}"
TEST_PORT="64580"
RUN_ID="$(od -An -N4 -tx1 /dev/urandom | tr -d ' \n')"
DOCKER_MEM="${DOCKER_MEM:-4g}"
DOCKER_CPUS="${DOCKER_CPUS:-2}"
DOCKER_CPU_SHARES="${DOCKER_CPU_SHARES:-256}"
DOCKER_TIMEOUT="${DOCKER_TIMEOUT:-3600}"
echo "Building server and companion client with make build..."
make build BINDIR="$BIN_DIR"

echo "Testing binaries and services in Alpine..."
timeout -k 30 "$DOCKER_TIMEOUT" docker run --rm --no-healthcheck \
  --name "${PROJECT_NAME}-${RUN_ID}" \
  --memory="$DOCKER_MEM" --cpus="$DOCKER_CPUS" --cpu-shares="$DOCKER_CPU_SHARES" \
  -v "$PROJECT_ROOT/$BIN_DIR:/app:ro" \
  -e PROJECT_NAME="$PROJECT_NAME" \
  -e PROJECT_ORG="$PROJECT_ORG" \
  -e INTERNAL_NAME="$INTERNAL_NAME" \
  -e CLI_NAME="$CLI_NAME" \
  -e TEST_PORT="$TEST_PORT" \
  -e REQUIRE_OVERLAY="${REQUIRE_OVERLAY:-0}" \
  -e TOR_WAIT="${TOR_WAIT:-300}" \
  -e I2P_WAIT="${I2P_WAIT:-600}" \
  -e TMPDIR=/tmp \
  alpine:latest sh -s <<'CONTAINER_SCRIPT'
set -eu
FAILED=0
fail() { echo "FAILED: $*" >&2; FAILED=$((FAILED + 1)); }
pass() { echo "PASS: $*"; }

apk add --no-cache curl bash file jq tor i2pd tzdata ca-certificates >/dev/null
SERVER="/app/${PROJECT_NAME}"
CLIENT="/app/${CLI_NAME}"
mkdir -p "${TMPDIR:-/tmp}/${PROJECT_ORG}"
TEST_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/${PROJECT_ORG}/${INTERNAL_NAME}-XXXXXX")"
mkdir -p "$TEST_ROOT/config" "$TEST_ROOT/data" "$TEST_ROOT/cache" "$TEST_ROOT/log"
SERVER_PID=""
cleanup() {
  if [ -n "$SERVER_PID" ]; then
    kill "$SERVER_PID" 2>/dev/null || true
    wait "$SERVER_PID" 2>/dev/null || true
  fi
  rm -rf "$TEST_ROOT"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

echo "=== Binary checks ==="
if [ -x "$SERVER" ]; then
  "$SERVER" --version || fail "server --version"
  "$SERVER" --help >/dev/null || fail "server --help"
  file "$SERVER" || fail "server file information"
else
  fail "server binary is missing or not executable"
fi
cp "$SERVER" "$TEST_ROOT/renamed-server"
chmod +x "$TEST_ROOT/renamed-server"
if "$TEST_ROOT/renamed-server" --help 2>&1 | grep 'renamed-server' >/dev/null; then
  pass "server binary rename"
else
  fail "server help does not show the actual renamed binary"
fi

if [ -x "$CLIENT" ]; then
  "$CLIENT" --version || fail "client --version"
  "$CLIENT" --help >/dev/null || fail "client --help"
  cp "$CLIENT" "$TEST_ROOT/renamed-cli"
  chmod +x "$TEST_ROOT/renamed-cli"
  if "$TEST_ROOT/renamed-cli" --help 2>&1 | grep 'renamed-cli' >/dev/null; then
    pass "client binary rename"
  else
    fail "client help does not show the actual renamed binary"
  fi
else
  echo "SKIP: companion client binary is absent" >&2
fi

echo "=== Starting isolated server ==="
"$SERVER" --config "$TEST_ROOT/config" --data "$TEST_ROOT/data" --cache "$TEST_ROOT/cache" --log "$TEST_ROOT/log" --port "$TEST_PORT" >"$TEST_ROOT/server.log" 2>&1 &
SERVER_PID=$!
READY=0
for _ in $(seq 1 60); do
  if curl -q -LsS --max-time 2 -o /dev/null "http://127.0.0.1:${TEST_PORT}/server/healthz"; then
    READY=1
    break
  fi
  sleep 1
done
if [ "$READY" -ne 1 ]; then
  cat "$TEST_ROOT/server.log" >&2
  fail "server did not start"
  exit 1
fi

OPERATOR_TOKEN="$(awk '
  /^server:[[:space:]]*$/ { in_server=1; next }
  in_server && /^[^[:space:]]/ { in_server=0 }
  in_server && /^[[:space:]]+token:/ {
    sub(/^[[:space:]]+token:[[:space:]]*/, "")
    gsub(/["\047]/, "")
    print
    exit
  }
' "$TEST_ROOT/config/secrets.yml" 2>/dev/null || true)"
if [ -n "$OPERATOR_TOKEN" ]; then
  pass "first-start operator token is in secrets.yml"
  MCP_BODY='{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}'
  CODE="$(curl -q -LsS -o /dev/null -w '%{http_code}' -H 'Content-Type: application/json' -H 'Accept: application/json, text/event-stream' -H 'Authorization: Bearer invalid-token' -d "$MCP_BODY" "http://127.0.0.1:${TEST_PORT}/api/v1/server/mcp" || true)"
  [ "$CODE" = 401 ] && pass "invalid operator bearer rejected by MCP" || fail "invalid MCP bearer returned HTTP ${CODE}"
  CODE="$(curl -q -LsS -o /dev/null -w '%{http_code}' -H 'Content-Type: application/json' -H 'Accept: application/json, text/event-stream' -H "Authorization: Bearer ${OPERATOR_TOKEN}" -d "$MCP_BODY" "http://127.0.0.1:${TEST_PORT}/api/v1/server/mcp" || true)"
  [ "$CODE" = 200 ] && pass "operator bearer accepted by MCP" || fail "operator MCP request returned HTTP ${CODE}"
else
  fail "first-start operator token was not written to secrets.yml"
fi

echo "=== Public route and content-negotiation checks ==="
for path in / /server/about /server/help /server/privacy /server/contact /server/terms /server/security /server/healthz; do
  if ! curl -q -LsSf -H 'Accept: text/html' "http://127.0.0.1:${TEST_PORT}${path}" -o /dev/null; then
    fail "HTML route ${path}"
  fi
  if ! curl -q -LsSf -H 'Accept: text/plain' "http://127.0.0.1:${TEST_PORT}${path}" -o /dev/null; then
    fail "plain-text route ${path}"
  fi
done

curl -q -LsSf -H 'Accept: application/json' "http://127.0.0.1:${TEST_PORT}/api/swagger" -o "$TEST_ROOT/openapi.json" || fail "OpenAPI route manifest"
jq -r '
  .paths | to_entries[] as $path
  | $path.value | to_entries[]
  | select(.key == "get" or .key == "post" or .key == "put" or .key == "patch" or .key == "delete")
  | "\(.key)\t\($path.key)"
' "$TEST_ROOT/openapi.json" > "$TEST_ROOT/api-routes.tsv" || fail "extracting registered API route manifest"
while IFS="$(printf '\t')" read -r method route_template; do
  [ -n "$method" ] || continue
  case "$route_template" in
    */mcp|/api/mcp) continue ;;
  esac
  route="$(jq -nr --arg route "$route_template" '$route | gsub("\\{[^}]+\\}"; "sample") | gsub("\\*"; "sample")')"
  for accept in application/json text/plain; do
    response_file="$TEST_ROOT/api-response"
    response_type_file="$TEST_ROOT/api-content-type"
    case "$method" in
      post|put|patch) code="$(curl -q -LsS --max-time 15 -o "$response_file" -w '%{http_code}' -D "$response_type_file" -X "$(printf '%s' "$method" | tr '[:lower:]' '[:upper:]')" -H "Accept: ${accept}" -H 'Content-Type: application/json' --data '{}' "http://127.0.0.1:${TEST_PORT}${route}" 2>/dev/null || true)" ;;
      *) code="$(curl -q -LsS --max-time 15 -o "$response_file" -w '%{http_code}' -D "$response_type_file" -X "$(printf '%s' "$method" | tr '[:lower:]' '[:upper:]')" -H "Accept: ${accept}" "http://127.0.0.1:${TEST_PORT}${route}" 2>/dev/null || true)" ;;
    esac
    if [ "$code" = 000 ]; then
      fail "${method} ${route} with ${accept} did not return"
      continue
    fi
    content_type="$(awk 'BEGIN{IGNORECASE=1} /^Content-Type:/ {sub(/^[^:]*:[[:space:]]*/, ""); gsub(/\r/, ""); print; exit}' "$response_type_file")"
    case "$accept" in
      application/json)
        case "$content_type" in application/json*) jq empty "$response_file" >/dev/null 2>&1 || fail "${method} ${route} returned invalid JSON" ;; *) fail "${method} ${route} returned ${content_type:-no content type} for application/json" ;; esac
        ;;
      text/plain)
        case "$content_type" in text/plain*) ;; *) fail "${method} ${route} returned ${content_type:-no content type} for text/plain" ;; esac
        ;;
    esac
  done
done < "$TEST_ROOT/api-routes.tsv"
for path in /api/v1/server/healthz.txt /api/v1/text/uuid.txt /robots.txt /.well-known/security.txt /llms.txt /.well-known/llms.txt; do
  if ! curl -q -LsSf "http://127.0.0.1:${TEST_PORT}${path}" -o /dev/null; then
    fail ".txt route ${path}"
  fi
done

echo "=== Owner-token authentication checks ==="
TOKEN_RESPONSE="$(curl -q -LsS --max-time 10 -X POST -H 'Content-Type: application/json' -d '{}' "http://127.0.0.1:${TEST_PORT}/api/v1/server/preferences/tokens/primary" || true)"
OWNER_TOKEN="$(jq -r '.data.owner_token // empty' <<<"$TOKEN_RESPONSE" 2>/dev/null || true)"
if [ -z "$OWNER_TOKEN" ]; then
  fail "anonymous primary owner token issuance"
else
  CODE="$(curl -q -LsS -o /dev/null -w '%{http_code}' -H 'Authorization: Bearer invalid-token' "http://127.0.0.1:${TEST_PORT}/api/v1/server/preferences/tokens" || true)"
  [ "$CODE" = 401 ] && pass "invalid owner token rejected" || fail "invalid owner token returned HTTP ${CODE}"
  CODE="$(curl -q -LsS -o /dev/null -w '%{http_code}' -H "Authorization: Bearer ${OWNER_TOKEN}" "http://127.0.0.1:${TEST_PORT}/api/v1/server/preferences/tokens" || true)"
  [ "$CODE" = 200 ] && pass "valid owner token accepted" || fail "valid owner token returned HTTP ${CODE}"
  if [ -x "$CLIENT" ]; then
    "$CLIENT" --server "http://127.0.0.1:${TEST_PORT}" --token "$OWNER_TOKEN" health >/dev/null || fail "CLI authenticated health request"
  fi
fi

echo "=== Tor and I2P health checks ==="
HEALTH=''
TOR_OK=0
I2P_OK=0
ELAPSED=0
MAX_WAIT="$I2P_WAIT"
[ "$TOR_WAIT" -le "$MAX_WAIT" ] || MAX_WAIT="$TOR_WAIT"
while [ "$ELAPSED" -lt "$MAX_WAIT" ]; do
  HEALTH="$(curl -q -LsS --max-time 5 -H 'Accept: application/json' "http://127.0.0.1:${TEST_PORT}/server/healthz" 2>/dev/null || true)"
  TOR_STATUS="$(jq -r '.features.tor.status // empty' <<<"$HEALTH" 2>/dev/null || true)"
  I2P_STATUS="$(jq -r '.features.i2p.status // empty' <<<"$HEALTH" 2>/dev/null || true)"
  [ "$TOR_STATUS" = healthy ] && TOR_OK=1
  [ "$I2P_STATUS" = healthy ] && I2P_OK=1
  [ "$TOR_OK" = 1 ] && [ "$I2P_OK" = 1 ] && break
  sleep 5
  ELAPSED=$((ELAPSED + 5))
done
if [ "$(jq -r '.features.i2p.enabled // false' <<<"$HEALTH" 2>/dev/null || true)" != true ]; then
  fail "I2P is not enabled in the application container"
fi
if [ "$(jq -r '.features.i2p.provider // empty' <<<"$HEALTH" 2>/dev/null || true)" != i2pd ]; then
  fail "I2P provider is not i2pd"
fi
if [ "$TOR_OK" = 1 ]; then
  jq -e '.features.tor.hostname | test("^[a-z2-7]{56}\\.onion$")' <<<"$HEALTH" >/dev/null && pass "Tor hidden-service health" || fail "Tor hostname is not a v3 onion address"
else
  if [ "$REQUIRE_OVERLAY" = 1 ]; then fail "Tor did not become healthy within ${TOR_WAIT}s"; else echo "SKIP: Tor bootstrap did not become healthy" >&2; fi
fi
if [ "$I2P_OK" = 1 ]; then
  jq -e '.features.i2p.hostname | endswith(".b32.i2p")' <<<"$HEALTH" >/dev/null && pass "I2P destination health" || fail "I2P hostname is invalid"
  [ "$(jq -r '.checks.i2p // empty' <<<"$HEALTH")" = ok ] && pass "I2P health check" || fail "I2P checks.i2p is not ok"
else
  if [ "$REQUIRE_OVERLAY" = 1 ]; then fail "I2P did not become healthy within ${I2P_WAIT}s"; else echo "SKIP: I2P bootstrap did not become healthy" >&2; fi
fi

if [ "$FAILED" -ne 0 ]; then
  echo "${FAILED} check(s) failed" >&2
  exit 1
fi
echo "All Docker binary-validation checks passed"
CONTAINER_SCRIPT
