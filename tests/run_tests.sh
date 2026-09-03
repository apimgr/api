#!/usr/bin/env bash
# @@License : WTFPL

# Phase 2 — Binary Validation entry point.
# Detects the available container runtime and dispatches to the matching
# test script. Incus is preferred (full systemd, realistic OS); Docker is
# the always-available fallback.

set -eo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"

if command -v incus &>/dev/null; then
  echo "Incus detected - running full systemd tests..."
  exec "$SCRIPT_DIR/incus.sh"
elif command -v docker &>/dev/null; then
  echo "Docker detected - running container tests..."
  exec "$SCRIPT_DIR/docker.sh"
else
  echo "ERROR: Neither incus nor docker found"
  echo "Please install one of the following:"
  echo "  - Incus (preferred): https://linuxcontainers.org/incus/"
  echo "  - Docker (fallback): https://docker.com/"
  exit 1
fi
