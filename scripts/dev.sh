#!/usr/bin/env bash
set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(dirname "$SCRIPT_DIR")"
K2_BIN="$ROOT_DIR/desktop/src-tauri/binaries/k2-$(uname -m)-apple-darwin"

# Build k2 if missing or outdated
if [ ! -f "$K2_BIN" ] || [ "$ROOT_DIR/k2/cmd/k2/main.go" -nt "$K2_BIN" ]; then
  echo "Building k2..."
  cd "$ROOT_DIR/k2"
  go build -tags nowebapp -o "$K2_BIN" ./cmd/k2
fi

# Start k2 daemon in background
echo "Starting k2 daemon..."
"$K2_BIN" run &
K2_PID=$!

# Trap to kill k2 daemon on exit
cleanup() {
  echo "Stopping k2 daemon..."
  kill "$K2_PID" 2>/dev/null || true
  wait "$K2_PID" 2>/dev/null || true
}
trap cleanup EXIT INT TERM

# Start Tauri dev
cd "$ROOT_DIR/desktop"
yarn tauri dev
