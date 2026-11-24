#!/usr/bin/env bash
# Validate the final image runtime contract and Tor bootstrap health (AI.md PART 26).

set -euo pipefail

PROJECT_NAME="$(git remote get-url origin 2>/dev/null | sed -E 's#.*[:/]([^/]+)(\.git)?$#\1#' || true)"
PROJECT_NAME="${PROJECT_NAME:-$(basename "$PWD")}"
IMAGE="${1:-}"
if [ -z "$IMAGE" ]; then
  echo "usage: tests/check_image.sh <image>" >&2
  exit 64
fi

new_container_name() {
  printf '%s-%04x%04x' "$PROJECT_NAME" "$RANDOM" "$RANDOM"
}

echo "Checking required commands in $IMAGE..."
docker run --rm --no-healthcheck --name "$(new_container_name)" \
  --entrypoint sh "$IMAGE" \
  -c 'for c in bash curl tor i2pd tini; do command -v "$c" || exit 1; done'

RUNTIME_CONTAINER="$(new_container_name)"
cleanup() {
  docker rm -f "$RUNTIME_CONTAINER" >/dev/null 2>&1 || true
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

echo "Checking Tor bootstrap health in $IMAGE..."
docker run -d --rm --no-healthcheck --name "$RUNTIME_CONTAINER" "$IMAGE" >/dev/null
TOR_BOOTSTRAP_TIMEOUT="${TOR_BOOTSTRAP_TIMEOUT:-180}"
if [[ ! "$TOR_BOOTSTRAP_TIMEOUT" =~ ^[0-9]+$ ]] || [ "$TOR_BOOTSTRAP_TIMEOUT" -lt 1 ]; then
  echo "ERROR: TOR_BOOTSTRAP_TIMEOUT must be a positive integer" >&2
  exit 64
fi
TOR_STATUS=""
TOR_ELAPSED=0
while [ "$TOR_ELAPSED" -lt "$TOR_BOOTSTRAP_TIMEOUT" ]; do
  RESPONSE="$(docker exec "$RUNTIME_CONTAINER" curl -q -LsS --max-time 5 -H 'Accept: application/json' http://127.0.0.1/server/healthz 2>/dev/null || true)"
  TOR_STATUS="$(sed -n 's/.*"tor":{[^}]*"status":"\([^"]*\)".*/\1/p' <<<"$RESPONSE")"
  case "$TOR_STATUS" in
    healthy)
      echo "PASS: Tor hidden service is healthy"
      break
      ;;
    starting|unavailable)
      ;;
    error)
      echo "FAIL: Tor reported error during image health check" >&2
      exit 1
      ;;
    "")
      ;;
    *)
      echo "FAIL: unexpected Tor status '$TOR_STATUS'" >&2
      exit 1
      ;;
  esac
  sleep 5
  TOR_ELAPSED=$((TOR_ELAPSED + 5))
done

if [ "$TOR_STATUS" != healthy ]; then
  if [ "${REQUIRE_OVERLAY:-0}" = 1 ]; then
    echo "FAIL: REQUIRE_OVERLAY=1 but Tor did not become healthy within ${TOR_BOOTSTRAP_TIMEOUT}s (status: ${TOR_STATUS:-unavailable})" >&2
    exit 1
  fi
  case "$TOR_STATUS" in
    starting|unavailable)
      echo "SKIP: no outbound network (Tor status: $TOR_STATUS)" >&2
      ;;
    "")
      echo "FAIL: unable to read Tor status from /server/healthz" >&2
      exit 1
      ;;
    *)
      echo "FAIL: Tor did not become healthy within ${TOR_BOOTSTRAP_TIMEOUT}s (status: $TOR_STATUS)" >&2
      exit 1
      ;;
  esac
fi
