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
BIN_DIR="${BINDIR:-}"
BUILD_BIN_DIR_CREATED=0
if [ -z "$BIN_DIR" ]; then
  BUILD_TEMP_ROOT="/tmp/${PROJECT_ORG}"
  mkdir -p "$BUILD_TEMP_ROOT"
  BIN_DIR="$(mktemp -d "$BUILD_TEMP_ROOT/${INTERNAL_NAME}-build-XXXXXX")"
  BUILD_BIN_DIR_CREATED=1
elif [[ "$BIN_DIR" != /* ]]; then
  BIN_DIR="$PROJECT_ROOT/$BIN_DIR"
fi
BUILD_RELEASE_DIR="${BIN_DIR}-release"
TEST_PORT="64580"
RUN_ID="$(od -An -N4 -tx1 /dev/urandom | tr -d ' \n')"
DOCKER_MEM="${DOCKER_MEM:-4g}"
DOCKER_CPUS="${DOCKER_CPUS:-2}"
DOCKER_CPU_SHARES="${DOCKER_CPU_SHARES:-256}"
DOCKER_TIMEOUT="${DOCKER_TIMEOUT:-3600}"
NETWORK_NAME="${PROJECT_NAME}-test-${RUN_ID}"
NETWORK_CREATED=0

__cleanup() {
  local status=$?
  if [ "$NETWORK_CREATED" -eq 1 ]; then
    timeout "$DOCKER_TIMEOUT" docker network rm "$NETWORK_NAME" >/dev/null || status=1
  fi
  if [ "$BUILD_BIN_DIR_CREATED" -eq 1 ]; then
    make clean BINDIR="$BIN_DIR" RELDIR="$BUILD_RELEASE_DIR" >/dev/null || status=1
  fi
  exit "$status"
}
trap '__cleanup' EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

echo "Building server and companion client with make build..."
make build BINDIR="$BIN_DIR" RELDIR="$BUILD_RELEASE_DIR"
for binary in "$PROJECT_NAME" "$CLI_NAME"; do
  if [ ! -x "$BIN_DIR/$binary" ]; then
    echo "ERROR: expected build artifact is missing or not executable: $BIN_DIR/$binary" >&2
    exit 1
  fi
done

echo "Testing binaries and services in Alpine..."
timeout "$DOCKER_TIMEOUT" docker network create --driver bridge "$NETWORK_NAME" >/dev/null
NETWORK_CREATED=1
timeout -k 30 "$DOCKER_TIMEOUT" docker run --rm -i --no-healthcheck \
  --name "${PROJECT_NAME}-${RUN_ID}" \
  --network "$NETWORK_NAME" \
  --memory="$DOCKER_MEM" --cpus="$DOCKER_CPUS" --cpu-shares="$DOCKER_CPU_SHARES" \
  -v "$BIN_DIR:/app:ro" \
  -e PROJECT_NAME="$PROJECT_NAME" \
  -e PROJECT_ORG="$PROJECT_ORG" \
  -e INTERNAL_NAME="$INTERNAL_NAME" \
  -e CLI_NAME="$CLI_NAME" \
  -e TEST_PORT="$TEST_PORT" \
  -e REQUIRE_OVERLAY="${REQUIRE_OVERLAY:-0}" \
  -e TOR_WAIT="${TOR_WAIT:-300}" \
  -e I2P_WAIT="${I2P_WAIT:-600}" \
  -e TMPDIR=/tmp \
  alpine:latest sh -c 'apk add --no-cache bash >/dev/null && exec bash -s' <<'CONTAINER_SCRIPT'
set -eu
umask 077
FAILED=0
fail() { echo "FAILED: $*" >&2; FAILED=$((FAILED + 1)); }
pass() { echo "PASS: $*"; }

apk add --no-cache curl bash file jq tor i2pd tzdata ca-certificates >/dev/null
SERVER="/app/${PROJECT_NAME}"
CLIENT="/app/${CLI_NAME}"
mkdir -p "${TMPDIR:-/tmp}/${PROJECT_ORG}"
# Permit traversal to private test roots without exposing directory listings.
chmod 0711 "${TMPDIR:-/tmp}/${PROJECT_ORG}"
TEST_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/${PROJECT_ORG}/${INTERNAL_NAME}-XXXXXX")"
mkdir -p "$TEST_ROOT/config" "$TEST_ROOT/data" "$TEST_ROOT/cache" "$TEST_ROOT/log"
# The server drops to its service account and must traverse its private test root.
chmod 0711 "$TEST_ROOT"
mkdir -p "$TEST_ROOT/config/yaml.d"
cat > "$TEST_ROOT/config/yaml.d/security.yml" <<'SECURITY_CONFIG'
server:
  tokens:
    max_linked: 1
  rate_limit:
    read:
      requests: 100000
      window: 60
    write:
      requests: 100000
      window: 60
    health:
      requests: 100000
      window: 60
    strict:
      requests: 100000
      window: 60
    auth_failures:
      requests: 100000
      window: 60
    global_burst: 200000
  i2p:
    enabled: true
SECURITY_CONFIG
cat > "$TEST_ROOT/config/yaml.d/metrics.yml" <<'METRICS_CONFIG'
server:
  metrics:
    enabled: true
    root:
      enabled: true
METRICS_CONFIG
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
"$SERVER" --debug --config "$TEST_ROOT/config" --data "$TEST_ROOT/data" --cache "$TEST_ROOT/cache" --log "$TEST_ROOT/log" --port "$TEST_PORT" >"$TEST_ROOT/server.log" 2>&1 &
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
MCP_BODY='{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28"}}'
OWNER_TOKEN=""
LINKED_TOKEN=""
if [ -n "$OPERATOR_TOKEN" ]; then
  pass "first-start operator token is in secrets.yml"
  EFFECTIVE_LIMITS="$(curl -q -LsS --max-time 10 -H "Authorization: Bearer ${OPERATOR_TOKEN}" "http://127.0.0.1:${TEST_PORT}/debug/config" 2>/dev/null || true)"
  jq -e '.server.rate_limit.read.requests == 100000 and .server.rate_limit.write.requests == 100000 and .server.rate_limit.health.requests == 100000 and .server.rate_limit.strict.requests == 100000 and .server.rate_limit.auth_failures.requests == 100000 and .server.rate_limit.global_burst == 200000' <<<"$EFFECTIVE_LIMITS" >/dev/null 2>&1 || fail "test rate-limit overrides are not effective"
  jq -e '.server.metrics.enabled == true and .server.metrics.root.enabled == true' <<<"$EFFECTIVE_LIMITS" >/dev/null 2>&1 || fail "test metrics route overrides are not effective"
  CODE="$(curl -q -LsS -o /dev/null -w '%{http_code}' -H 'Content-Type: application/json' -H 'Accept: application/json, text/event-stream' -H 'Authorization: Bearer invalid-token' -d "$MCP_BODY" "http://127.0.0.1:${TEST_PORT}/api/v1/server/mcp" || true)"
  [ "$CODE" = 401 ] && pass "invalid operator bearer rejected by MCP" || fail "invalid MCP bearer returned HTTP ${CODE}"
  CODE="$(curl -q -LsS -o /dev/null -w '%{http_code}' -H 'Content-Type: application/json' -H 'Accept: application/json, text/event-stream' -H 'MCP-Protocol-Version: 2026-07-28' -H 'Mcp-Method: tools/list' -H "Authorization: Bearer ${OPERATOR_TOKEN}" -d "$MCP_BODY" "http://127.0.0.1:${TEST_PORT}/api/v1/server/mcp" || true)"
  [ "$CODE" = 200 ] && pass "operator bearer accepted by MCP" || fail "operator MCP request returned HTTP ${CODE}"
else
  fail "first-start operator token was not written to secrets.yml"
fi

echo "=== Owner-token authentication checks ==="
TOKEN_RESPONSE="$(curl -q -LsS --max-time 10 -X POST -H 'Accept: application/json' -H 'Content-Type: application/json' -d '{}' "http://127.0.0.1:${TEST_PORT}/api/v1/server/preferences/tokens/primary" || true)"
OWNER_TOKEN="$(jq -r '.data.owner_token // empty' <<<"$TOKEN_RESPONSE" 2>/dev/null || true)"
if [ -z "$OWNER_TOKEN" ]; then
  fail "anonymous primary owner token issuance"
else
  CODE="$(curl -q -LsS -o /dev/null -w '%{http_code}' -H 'Authorization: Bearer invalid-token' "http://127.0.0.1:${TEST_PORT}/api/v1/server/preferences/tokens" || true)"
  [ "$CODE" = 401 ] && pass "invalid owner token rejected" || fail "invalid owner token returned HTTP ${CODE}"
  CODE="$(curl -q -LsS -o /dev/null -w '%{http_code}' -H "Authorization: Bearer ${OWNER_TOKEN}" "http://127.0.0.1:${TEST_PORT}/api/v1/server/preferences/tokens" || true)"
  [ "$CODE" = 200 ] && pass "valid owner token accepted" || fail "valid owner token returned HTTP ${CODE}"
  LINKED_RESPONSE="$(curl -q -LsS --max-time 10 -X POST -H 'Accept: application/json' -H 'Content-Type: application/json' -H "Authorization: Bearer ${OWNER_TOKEN}" -d '{"name":"phase-two-cli","expires":"30d"}' "http://127.0.0.1:${TEST_PORT}/api/v1/server/preferences/tokens" || true)"
  LINKED_TOKEN="$(jq -r '.data.linked_token // empty' <<<"$LINKED_RESPONSE" 2>/dev/null || true)"
  LINKED_ID="$(jq -r '.data.id // empty' <<<"$LINKED_RESPONSE" 2>/dev/null || true)"
  [ -n "$LINKED_TOKEN" ] || fail "linked CLI token creation"
  [ -n "$LINKED_ID" ] || fail "linked token UUID response"
  if [ -x "$CLIENT" ]; then
    "$CLIENT" --version >/dev/null || fail "CLI --version"
    CLI_HEALTH="$("$CLIENT" --server "http://127.0.0.1:${TEST_PORT}" --token "$LINKED_TOKEN" raw get /api/v1/server/healthz 2>/dev/null || true)"
    if [ -n "$CLI_HEALTH" ] && grep -qi 'status' <<<"$CLI_HEALTH"; then
      pass "CLI authenticated health request"
    else
      fail "CLI authenticated health request returned unexpected output"
    fi
    CLI_READ="$("$CLIENT" --server "http://127.0.0.1:${TEST_PORT}" --token "$LINKED_TOKEN" raw get /api/v1/version 2>/dev/null || true)"
    if [ -n "$CLI_READ" ] && grep -qi 'version' <<<"$CLI_READ"; then
      pass "CLI authenticated read request"
    else
      fail "CLI authenticated read request returned unexpected output"
    fi
    CLI_WRITE="$("$CLIENT" --server "http://127.0.0.1:${TEST_PORT}" --token "$LINKED_TOKEN" raw post /api/v1/dev/format/json input=test 2>/dev/null || true)"
    if [ -n "$CLI_WRITE" ] && grep -q 'formatted' <<<"$CLI_WRITE"; then
      pass "CLI authenticated write request"
    else
      fail "CLI authenticated write request returned unexpected output"
    fi
    if "$CLIENT" --server "http://127.0.0.1:${TEST_PORT}" --token invalid-token raw get /api/v1/server/preferences/tokens >"$TEST_ROOT/cli-failure-output" 2>&1; then
      fail "CLI invalid-token request unexpectedly succeeded"
    elif grep -Eiq '401|unauthorized|server returned' "$TEST_ROOT/cli-failure-output"; then
      pass "CLI invalid-token failure"
    else
      fail "CLI invalid-token failure did not explain the error"
    fi
  fi

  TOKEN_API="http://127.0.0.1:${TEST_PORT}/api/v1/server/preferences/tokens"
  AUTH_OWNER=(-H "Authorization: Bearer ${OWNER_TOKEN}")
  LIST_RESPONSE="$(curl -q -LsS --max-time 10 -H 'Accept: application/json' -H "Authorization: Bearer ${OWNER_TOKEN}" "$TOKEN_API" || true)"
  if jq -e --arg id "$LINKED_ID" --arg last4 "${LINKED_TOKEN: -4}" '.data.primary.kind == "primary" and .data.max_linked == 1 and any(.data.linked[]; .id == $id and .kind == "linked" and (.token_masked | startswith("own_")) and (.token_masked | length == 36) and (.token_masked | endswith($last4)) and (has("prefix") | not) and (has("last4") | not) and (has("owner_id") | not))' <<<"$LIST_RESPONSE" >/dev/null 2>&1; then
    pass "linked token list contract and masking"
  else
    fail "linked token list contract or masking is invalid"
  fi
  CODE="$(curl -q -LsS -o /dev/null -w '%{http_code}' --max-time 10 -X POST -H 'Content-Type: application/json' "${AUTH_OWNER[@]}" -d '{"name":"phase-two-cli"}' "$TOKEN_API" || true)"
  [ "$CODE" = 409 ] && pass "duplicate linked token name conflict" || fail "duplicate linked token name returned HTTP ${CODE}"
  CODE="$(curl -q -LsS -o /dev/null -w '%{http_code}' --max-time 10 -X POST -H 'Content-Type: application/json' "${AUTH_OWNER[@]}" -d '{"name":"over-limit"}' "$TOKEN_API" || true)"
  [ "$CODE" = 409 ] && pass "linked token owner limit conflict" || fail "linked token owner limit returned HTTP ${CODE}"
  CODE="$(curl -q -LsS -o /dev/null -w '%{http_code}' --max-time 10 -X POST -H 'Content-Type: application/json' "${AUTH_OWNER[@]}" -d '{"name":"invalid-expiry","expires":"7d","expires_on":"2099-01-01"}' "$TOKEN_API" || true)"
  [ "$CODE" = 400 ] && pass "mutually exclusive linked expiry fields" || fail "both linked expiry fields returned HTTP ${CODE}"
  CODE="$(curl -q -LsS -o /dev/null -w '%{http_code}' --max-time 10 -X POST -H 'Content-Type: application/json' -H "Authorization: Bearer ${LINKED_TOKEN}" -d '{"name":"must-not-create"}' "$TOKEN_API" || true)"
  [ "$CODE" = 403 ] && pass "linked credential cannot manage tokens" || fail "linked credential token management returned HTTP ${CODE}"
  CODE="$(curl -q -LsS -o /dev/null -w '%{http_code}' --max-time 10 -X POST -H 'Content-Type: application/json' "${AUTH_OWNER[@]}" "$TOKEN_API/00000000-0000-7000-8000-000000000001/reveal" || true)"
  [ "$CODE" = 404 ] && pass "unknown linked token returns not found" || fail "unknown linked token returned HTTP ${CODE}"

  CODE="$(curl -q -LsS -o "$TEST_ROOT/token-reveal.json" -w '%{http_code}' --max-time 10 -X POST -H 'Accept: application/json' "${AUTH_OWNER[@]}" "$TOKEN_API/${LINKED_ID}/reveal" || true)"
  REVEALED="$(jq -r '.data.linked_token // empty' "$TEST_ROOT/token-reveal.json" 2>/dev/null || true)"
  [ "$CODE" = 200 ] && [ "$REVEALED" = "$LINKED_TOKEN" ] && pass "linked token reveal" || fail "linked token reveal returned HTTP ${CODE} or an unexpected token"

  CODE="$(curl -q -LsS -o "$TEST_ROOT/token-rename.json" -w '%{http_code}' --max-time 10 -X PATCH -H 'Accept: application/json' -H 'Content-Type: application/json' "${AUTH_OWNER[@]}" -d '{"name":"phase-two-renamed"}' "$TOKEN_API/${LINKED_ID}" || true)"
  jq -e --arg id "$LINKED_ID" '.data.id == $id and .data.name == "phase-two-renamed" and .data.expires_at != null' "$TEST_ROOT/token-rename.json" >/dev/null 2>&1 && [ "$CODE" = 200 ] && pass "linked token name-only patch preserves expiry" || fail "linked token name-only patch did not preserve expiry (HTTP ${CODE})"
  CODE="$(curl -q -LsS -o "$TEST_ROOT/token-expiry.json" -w '%{http_code}' --max-time 10 -X PATCH -H 'Accept: application/json' -H 'Content-Type: application/json' "${AUTH_OWNER[@]}" -d '{"expires":"7d"}' "$TOKEN_API/${LINKED_ID}" || true)"
  jq -e --arg id "$LINKED_ID" '.data.id == $id and .data.name == "phase-two-renamed" and .data.expires_at != null' "$TEST_ROOT/token-expiry.json" >/dev/null 2>&1 && [ "$CODE" = 200 ] && pass "linked token expiry-only patch preserves name" || fail "linked token expiry-only patch did not preserve name (HTTP ${CODE})"

  OLD_LINKED_TOKEN="$LINKED_TOKEN"
  CODE="$(curl -q -LsS -o "$TEST_ROOT/token-rotate.json" -w '%{http_code}' --max-time 10 -X POST -H 'Accept: application/json' "${AUTH_OWNER[@]}" "$TOKEN_API/${LINKED_ID}/rotate" || true)"
  LINKED_TOKEN="$(jq -r '.data.linked_token // empty' "$TEST_ROOT/token-rotate.json" 2>/dev/null || true)"
  jq -e --arg id "$LINKED_ID" '.data.id == $id and .data.name == "phase-two-renamed"' "$TEST_ROOT/token-rotate.json" >/dev/null 2>&1 && [ "$CODE" = 200 ] && [ -n "$LINKED_TOKEN" ] && [ "$LINKED_TOKEN" != "$OLD_LINKED_TOKEN" ] && pass "linked token rotates in place" || fail "linked token rotation failed (HTTP ${CODE})"
  CODE="$(curl -q -LsS -o /dev/null -w '%{http_code}' --max-time 10 -H "Authorization: Bearer ${OLD_LINKED_TOKEN}" "$TOKEN_API" || true)"
  [ "$CODE" = 401 ] && pass "rotated linked token is invalidated" || fail "old linked token returned HTTP ${CODE} after rotation"

  CODE="$(curl -q -LsS -o "$TEST_ROOT/token-revoke.json" -w '%{http_code}' --max-time 10 -X DELETE -H 'Accept: application/json' "${AUTH_OWNER[@]}" "$TOKEN_API/${LINKED_ID}" || true)"
  jq -e --arg id "$LINKED_ID" '.data.id == $id and .data.status == "revoked"' "$TEST_ROOT/token-revoke.json" >/dev/null 2>&1 && [ "$CODE" = 200 ] && pass "linked token revoke response" || fail "linked token revoke failed (HTTP ${CODE})"
  CODE="$(curl -q -LsS -o /dev/null -w '%{http_code}' --max-time 10 -H "Authorization: Bearer ${LINKED_TOKEN}" "$TOKEN_API" || true)"
  [ "$CODE" = 401 ] && pass "revoked linked token is invalidated" || fail "revoked linked token returned HTTP ${CODE}"
  AUTH_LINKED_RESPONSE="$(curl -q -LsS --max-time 10 -X POST -H 'Accept: application/json' -H 'Content-Type: application/json' -H "Authorization: Bearer ${OWNER_TOKEN}" -d '{"name":"phase-two-auth"}' "$TOKEN_API" || true)"
  LINKED_TOKEN="$(jq -r '.data.linked_token // empty' <<<"$AUTH_LINKED_RESPONSE" 2>/dev/null || true)"
  [ -n "$LINKED_TOKEN" ] || fail "linked token creation for authorization matrix"
fi

echo "=== MCP alias authentication checks ==="
for route in /api/v1/server/mcp /api/mcp /mcp; do
  MCP_HEADERS=(-H 'Content-Type: application/json' -H 'Accept: application/json, text/event-stream' -H 'MCP-Protocol-Version: 2026-07-28' -H 'Mcp-Method: tools/list')
  CODE="$(curl -q -LsS -o /dev/null -w '%{http_code}' "${MCP_HEADERS[@]}" -d "$MCP_BODY" "http://127.0.0.1:${TEST_PORT}${route}" || true)"
  [ "$CODE" = 401 ] || fail "${route} without bearer returned HTTP ${CODE}, expected 401"
  CODE="$(curl -q -LsS -o /dev/null -w '%{http_code}' "${MCP_HEADERS[@]}" -H 'Authorization: Bearer invalid-token' -d "$MCP_BODY" "http://127.0.0.1:${TEST_PORT}${route}" || true)"
  [ "$CODE" = 401 ] || fail "${route} with bad bearer returned HTTP ${CODE}, expected 401"
  CODE="$(curl -q -LsS -o /dev/null -w '%{http_code}' "${MCP_HEADERS[@]}" -H "Authorization: Bearer ${OWNER_TOKEN}" -d "$MCP_BODY" "http://127.0.0.1:${TEST_PORT}${route}" || true)"
  [ "$CODE" = 200 ] || fail "${route} with owner bearer returned HTTP ${CODE}, expected 200"
  CODE="$(curl -q -LsS -o /dev/null -w '%{http_code}' "${MCP_HEADERS[@]}" -d "$MCP_BODY" "http://127.0.0.1:${TEST_PORT}${route}?token=${OPERATOR_TOKEN}" || true)"
  [ "$CODE" = 401 ] || fail "${route} accepted a query-string bearer with HTTP ${CODE}"
  pass "${route} MCP bearer checks"
done

echo "=== Operator-token route matrix ==="
while IFS= read -r route; do
  [ -n "$route" ] || continue
  route_url="$route"
  case "$route" in
    */pprof/profile|*/pprof/trace) route_url="${route}?seconds=1" ;;
  esac
  success_status=200
  [ "$route" = /debug/error500 ] && success_status=500
  for auth_case in none invalid owner linked operator query; do
    auth_args=()
    request_url="http://127.0.0.1:${TEST_PORT}${route_url}"
    case "$auth_case" in
      invalid) auth_args=(-H 'Authorization: Bearer invalid-token') ;;
      owner) auth_args=(-H "Authorization: Bearer ${OWNER_TOKEN}") ;;
      linked) auth_args=(-H "Authorization: Bearer ${LINKED_TOKEN}") ;;
      operator) auth_args=(-H "Authorization: Bearer ${OPERATOR_TOKEN}") ;;
      query)
        case "$request_url" in
          *\?*) request_url="${request_url}&token=${OPERATOR_TOKEN}" ;;
          *) request_url="${request_url}?token=${OPERATOR_TOKEN}" ;;
        esac
        ;;
    esac
    code="$(curl -q -LsS --max-time 20 -o /dev/null -w '%{http_code}' "${auth_args[@]}" "$request_url" || true)"
    case "$auth_case" in
      none|invalid|query) expected=401 ;;
      owner|linked) expected=403 ;;
      operator) expected="$success_status" ;;
    esac
    [ "$code" = "$expected" ] || fail "${route} ${auth_case} auth returned HTTP ${code}, expected ${expected}"
  done
  pass "${route} operator-token matrix"
done <<'OPERATOR_ROUTES'
/debug/pprof
/debug/pprof/heap
/debug/pprof/goroutine
/debug/pprof/allocs
/debug/pprof/block
/debug/pprof/mutex
/debug/pprof/threadcreate
/debug/pprof/cmdline
/debug/pprof/profile
/debug/pprof/symbol
/debug/pprof/trace
/debug/vars
/debug/config
/debug/routes
/debug/cache
/debug/db
/debug/scheduler
/debug/memory
/debug/goroutines
/debug/error500
OPERATOR_ROUTES

echo "=== Metrics-token authentication matrix ==="
read_metrics_token() {
  local service="$1"
  awk -v key="$service" '
    /^server:[[:space:]]*$/ { in_server=1; next }
    in_server && /^[^[:space:]]/ { in_server=0 }
    in_server && /^    metrics:[[:space:]]*$/ { in_metrics=1; next }
    in_metrics && /^    [^[:space:]]/ { in_metrics=0 }
    in_metrics && /^        auth:[[:space:]]*$/ { in_auth=1; next }
    in_auth && /^        [^[:space:]]/ { in_auth=0 }
    in_auth && /^            tokens:[[:space:]]*$/ { in_tokens=1; next }
    in_tokens && /^            [^[:space:]]/ { in_tokens=0 }
    in_tokens && $1 == key ":" {
      sub(/^[^:]+:[[:space:]]*/, "")
      gsub(/["\047]/, "")
      print
      exit
    }
  ' "$TEST_ROOT/config/secrets.yml" 2>/dev/null || true
}
for service in prometheus grafana loki; do
  SERVICE_FAILED=0
  METRICS_TOKEN="$(read_metrics_token "$service")"
  if [ -z "$METRICS_TOKEN" ]; then
    fail "${service} metrics token missing from secrets.yml"
    continue
  fi
  case "$service" in
    prometheus) metrics_type='text/plain' ;;
    *) metrics_type='application/json' ;;
  esac
  for route in "/server/metrics/${service}" "/api/v1/server/metrics/${service}" "/api/metrics/${service}" "/metrics/${service}"; do
    for auth_case in none invalid owner valid query; do
      auth_args=()
      request_url="http://127.0.0.1:${TEST_PORT}${route}"
      case "$auth_case" in
        invalid) auth_args=(-H 'Authorization: Bearer invalid-token') ;;
        owner) auth_args=(-H "Authorization: Bearer ${OWNER_TOKEN}") ;;
        valid) auth_args=(-H "Authorization: Bearer ${METRICS_TOKEN}") ;;
        query) request_url="${request_url}?token=${METRICS_TOKEN}" ;;
      esac
      headers_file="$TEST_ROOT/metrics-headers"
      code="$(curl -q -LsS --max-time 15 -o /dev/null -D "$headers_file" -w '%{http_code}' -H 'Accept: application/json' "${auth_args[@]}" "$request_url" || true)"
      case "$auth_case" in
        valid) expected=200 ;;
        *) expected=401 ;;
      esac
      if [ "$code" != "$expected" ]; then
        fail "${route} ${auth_case} auth returned HTTP ${code}, expected ${expected}"
        SERVICE_FAILED=1
      fi
      if [ "$auth_case" = valid ]; then
        content_type="$(awk 'BEGIN{IGNORECASE=1} /^Content-Type:/ {sub(/^[^:]*:[[:space:]]*/, ""); gsub(/\r/, ""); print; exit}' "$headers_file")"
        case "$content_type" in
          "$metrics_type"*) ;;
          *)
            fail "${route} returned ${content_type:-no content type}, expected ${metrics_type}"
            SERVICE_FAILED=1
            ;;
        esac
      fi
    done
  done
  if [ "$SERVICE_FAILED" -eq 0 ]; then
    pass "${service} metrics token and aliases"
  fi
done

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
    */mcp|/api/mcp|/api/graphql|/api/*/server/graphql|/api/metrics|/api/metrics/*|/api/*/server/metrics|/api/*/server/metrics/*|*/preferences/tokens*|*/server/reports*) continue ;;
  esac
  route="$(jq -nr --arg route "$route_template" '$route | gsub("\\{[^}]+\\}"; "sample") | gsub("\\*"; "sample")')"
  case "$route_template" in
    *.txt|*/crypto/random/hex|*/crypto/random/hex/*) accepts="text/plain" ;;
    *) accepts="application/json text/plain" ;;
  esac
  for accept in $accepts; do
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
    case "$code" in
      2*) ;;
      *) continue ;;
    esac
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

echo "=== Browser reporting endpoint checks ==="
for report_name in csp default nel; do
  case "$report_name" in
    csp) report_body='{"csp-report":{"document-uri":"https://example.invalid/"}}'; report_type='application/csp-report' ;;
    *) report_body='[{"type":"test","url":"https://example.invalid/"}]'; report_type='application/reports+json' ;;
  esac
  CODE="$(curl -q -LsS --max-time 15 -o /dev/null -w '%{http_code}' -X POST -H "Content-Type: ${report_type}" -H 'Accept: application/json' --data "$report_body" "http://127.0.0.1:${TEST_PORT}/api/v1/server/reports/${report_name}" || true)"
  [ "$CODE" = 204 ] && pass "${report_type} report accepted at ${report_name}" || fail "${report_name} report returned HTTP ${CODE}"
done

echo "=== GraphQL route checks ==="
for route in /api/graphql /api/v1/server/graphql; do
  for method in GET POST; do
    if [ "$method" = GET ]; then
      CODE="$(curl -q -G -LsS --max-time 15 -o "$TEST_ROOT/graphql-response" -w '%{http_code}' -H 'Accept: application/json' --data-urlencode 'query={ health { status uptime } }' "http://127.0.0.1:${TEST_PORT}${route}" || true)"
    else
      CODE="$(curl -q -LsS --max-time 15 -o "$TEST_ROOT/graphql-response" -w '%{http_code}' -H 'Accept: application/json' -H 'Content-Type: application/json' --data '{"query":"{ health { status uptime } }"}' "http://127.0.0.1:${TEST_PORT}${route}" || true)"
    fi
    if [ "$CODE" = 200 ] && jq -e '.data.health.status' "$TEST_ROOT/graphql-response" >/dev/null 2>&1; then
      pass "$method $route GraphQL query"
    else
      fail "$method $route GraphQL query returned HTTP ${CODE} or invalid data"
    fi
  done
done

for path in /api/v1/server/healthz.txt /api/v1/text/uuid.txt /robots.txt /.well-known/security.txt /llms.txt /.well-known/llms.txt; do
  if ! curl -q -LsSf "http://127.0.0.1:${TEST_PORT}${path}" -o /dev/null; then
    fail ".txt route ${path}"
  fi
done

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
