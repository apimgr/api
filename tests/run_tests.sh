#!/usr/bin/env bash
# @@License : WTFPL

set -eo pipefail

# The basic tier is Docker, and every test script builds in Docker.
if command -v docker &>/dev/null; then
  echo "Docker detected - running the basic tests..."
  exec "$(dirname "$0")/docker.sh"
else
  echo "ERROR: docker not found: every test script builds with Docker" >&2
  echo "Install Docker: https://docker.com/" >&2
  exit 69
fi
