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

# Check if value is truthy (case-insensitive)
# Usage: if __is_truthy "$DEBUG"; then ...
__is_truthy() {
  local val="${1:-false}"
  val="${val,,}"
  [[ "$val" =~ ^(1|y|t|yes|true|on|ok|enable|enabled|sure|yep|yup|yeah|aye|si|oui|da|hai|affirmative|accept|allow|totally)$ ]]
}

echo "[entrypoint] $(date '+%Y-%m-%dT%H:%M:%S%z') Starting ${ENTRYPOINT_APP_NAME}..."

# Build flags from environment
# Always bind to 0.0.0.0:80 inside container (external port mapped by docker)
ENTRYPOINT_FLAGS="--address ${ADDRESS:-0.0.0.0} --port ${PORT:-80}"
[ -n "$MODE" ] && ENTRYPOINT_FLAGS="$ENTRYPOINT_FLAGS --mode $MODE"
__is_truthy "${DEBUG:-false}" && ENTRYPOINT_FLAGS="$ENTRYPOINT_FLAGS --debug"

# Start binary (binary handles ALL setup: dirs, perms, user/group, Tor, signals, etc.)
# exec replaces the shell so tini's forwarded signals (SIGTERM/SIGRTMIN+3) go
# straight to the binary as PID 1's direct child, per PART 26's entrypoint.sh
# tail requirement.
exec $ENTRYPOINT_APP_BIN $ENTRYPOINT_FLAGS "$@"
