#!/usr/bin/env bash

set -eo pipefail

echo "Checking for incompatible licenses..."

PROJECT_ROOT="$(git rev-parse --show-toplevel)"
REMOTE_URL="$(git -C "$PROJECT_ROOT" remote get-url origin 2>/dev/null || true)"
PROJECT_ORG="$(sed -E 's#.*[:/]([^/]+)/[^/]+(\.git)?$#\1#' <<< "$REMOTE_URL")"
PROJECT_NAME="$(basename "$PROJECT_ROOT")"
INTERNAL_NAME="$(sed -nE 's/^internal_name:[[:space:]]*([^[:space:]]+).*$/\1/p' "$PROJECT_ROOT/IDEA.md" | head -n1)"
PROJECT_ORG="${PROJECT_ORG:-$(basename "$(dirname "$PROJECT_ROOT")")}"
INTERNAL_NAME="${INTERNAL_NAME:-$PROJECT_NAME}"
cd "$PROJECT_ROOT"

command -v go-licenses >/dev/null 2>&1 || {
  echo "ERROR: go-licenses not found — run inside casjaysdev/go:latest" >&2
  exit 69
}

echo "Scanning dependencies..."
licenses=$(go-licenses csv ./...)
if grep -iE 'GPL|AGPL|LGPL' <<< "$licenses"; then
  echo "ERROR: Copyleft license detected!" >&2
  echo "Remove the dependency or find an alternative." >&2
  exit 1
fi

echo "✓ All licenses are compatible"

echo "Generating license report..."
mkdir -p "${TMPDIR:-/tmp}/${PROJECT_ORG}"
LICDIR="$(mktemp -d "${TMPDIR:-/tmp}/${PROJECT_ORG}/${INTERNAL_NAME}-XXXXXX")"
chmod 700 "$LICDIR"
trap 'find "$LICDIR" -depth -delete' EXIT
go-licenses csv ./... > "$LICDIR/licenses.csv"
go-licenses save ./... --ignore modernc.org/mathutil --save_path="$LICDIR/third_party_licenses"

MATHUTIL_VERSION="$(go list -m -f '{{.Version}}' modernc.org/mathutil)"
MATHUTIL_LICENSE="$(go env GOMODCACHE)/modernc.org/mathutil@${MATHUTIL_VERSION}/LICENSE"
if [ ! -s "$MATHUTIL_LICENSE" ]; then
  echo "ERROR: modernc.org/mathutil license file is missing; its verified BSD-3-Clause attribution is in LICENSE.md" >&2
  exit 1
fi
mkdir -p "$LICDIR/third_party_licenses/modernc.org/mathutil"
cp "$MATHUTIL_LICENSE" "$LICDIR/third_party_licenses/modernc.org/mathutil/LICENSE"

echo "✓ License report generated in the project-scoped temp directory and removed on exit"
echo "✓ modernc.org/mathutil is explicitly attributed as BSD-3-Clause in LICENSE.md"
