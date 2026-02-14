#!/usr/bin/env bash
set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(dirname "$SCRIPT_DIR")"

cd "$ROOT_DIR"

VERSION=$(node -p "require('./package.json').version")
echo "Version from package.json: $VERSION"
test "$VERSION" = "0.4.0" || { echo "FAIL: expected 0.4.0, got $VERSION"; exit 1; }

make pre-build
VJSON=$(node -p "require('./webapp/public/version.json').version")
echo "Version from version.json: $VJSON"
test "$VJSON" = "$VERSION" || { echo "FAIL: version.json mismatch"; exit 1; }

echo "PASS: Version propagation works"
