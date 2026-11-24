#!/usr/bin/env bash
set -eo pipefail

# =============================================================================
# Container Entrypoint Script - MINIMAL
# Only: set env, start binary, handle signals
# Binary handles: directories, permissions, user/group, Tor, etc.
# =============================================================================

ENTRYPOINT_APP_NAME="api"
ENTRYPOINT_APP_BIN="/usr/local/bin/${ENTRYPOINT_APP_NAME}"

# Export environment defaults (binary reads these)
export TZ="${TZ:-America/New_York}"
export CONFIG_DIR="${CONFIG_DIR:-/config/${ENTRYPOINT_APP_NAME}}"
export DATA_DIR="${DATA_DIR:-/data/${ENTRYPOINT_APP_NAME}}"

# Check if value is truthy (case-insensitive)
# Usage: if __is_truthy "$DEBUG"; then ...
__is_truthy() {
  local val="${1:-false}"
  val="${val,,}"
  [[ "$val" =~ ^(1|y|t|yes|true|on|ok|enable|enabled|sure|yep|yup|yeah|aye|si|oui|da|hai|affirmative|accept|allow|totally)$ ]]
}

echo "[entrypoint] $(date '+%Y-%m-%dT%H:%M:%S%z') Starting ${ENTRYPOINT_APP_NAME}..."

# Build flags from environment with each value kept as a single argument.
ENTRYPOINT_FLAGS=(--address "${LISTEN:-[::]}" --port "${PORT:-80}")
if [ -n "${MODE:-}" ]; then
  ENTRYPOINT_FLAGS+=(--mode "$MODE")
fi
if __is_truthy "${DEBUG:-false}"; then
  ENTRYPOINT_FLAGS+=(--debug)
fi

# Start binary (binary handles ALL setup: dirs, perms, user/group, Tor, signals, etc.)
# exec replaces the shell so tini's forwarded signals (SIGTERM/SIGRTMIN+3) go
# straight to the binary as PID 1's direct child, per PART 26's entrypoint.sh
# tail requirement.
exec "$ENTRYPOINT_APP_BIN" "${ENTRYPOINT_FLAGS[@]}" "$@"
