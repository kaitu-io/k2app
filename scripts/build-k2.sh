#!/usr/bin/env bash
set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(dirname "$SCRIPT_DIR")"

VERSION=$(node -p "require('$ROOT_DIR/package.json').version")
COMMIT=$(cd "$ROOT_DIR/k2" && git rev-parse --short HEAD)
TARGET="${1:-$(uname -m)-apple-darwin}"

echo "Building k2 $VERSION ($COMMIT) for $TARGET..."

cd "$ROOT_DIR/k2"
go build -tags nowebapp \
  -ldflags "-X main.version=$VERSION -X main.commit=$COMMIT" \
  -o "$ROOT_DIR/desktop/src-tauri/binaries/k2-$TARGET" \
  ./cmd/k2

echo "Built: desktop/src-tauri/binaries/k2-$TARGET"
