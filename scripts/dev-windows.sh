#!/usr/bin/env bash
set -e

# Windows desktop dev: k2 daemon + Vite HMR + Tauri window.
# k2 is always built as amd64 (runs under WoW64 on ARM64 Windows).
# Tauri builds for the host target (aarch64 on ARM64, x86_64 on Intel).
#
# Prerequisites: Node.js, Yarn, Rust, Go, NSIS (for build only)
# Usage: make dev-windows

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(cygpath -m "$(dirname "$SCRIPT_DIR")")"
K2_BIN="$ROOT_DIR/desktop/src-tauri/binaries/k2-x86_64-pc-windows-msvc.exe"
K2_DEV_ADDR="127.0.0.1:11777"  # dev daemon port (system service uses :1777)
VERSION=$(node -p "require('$ROOT_DIR/package.json').version")

PIDS=()

cleanup() {
  echo ""
  echo "[dev-windows] Stopping all dev services..."
  for pid in "${PIDS[@]}"; do
    kill "$pid" 2>/dev/null || true
  done
  # Wait briefly for graceful shutdown
  for pid in "${PIDS[@]}"; do
    wait "$pid" 2>/dev/null || true
  done
  echo "[dev-windows] All services stopped."
}
trap cleanup EXIT INT TERM

# ── 1. Build k2 if missing or outdated ──
if [ ! -f "$K2_BIN" ] || [ "$ROOT_DIR/k2/cmd/k2/main.go" -nt "$K2_BIN" ]; then
  echo "[dev-windows] Building k2 (amd64, v$VERSION)..."

  # Ensure wintun DLLs exist (downloaded once, committed to gitignore)
  if [ ! -f "$ROOT_DIR/k2/daemon/wintun/wintun_amd64.dll" ]; then
    echo "[dev-windows] Downloading wintun DLLs..."
    cd "$ROOT_DIR/k2/daemon/wintun" && go run gen.go
  fi

  cd "$ROOT_DIR/k2"
  CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build \
    -tags nowebapp \
    -ldflags "-s -w -X main.version=$VERSION" \
    -o "$K2_BIN" ./cmd/k2
  echo "[dev-windows] Built: $K2_BIN"
fi

# ── 2. Pre-build (version.json) ──
mkdir -p "$ROOT_DIR/webapp/public"
echo "{\"version\":\"$VERSION\"}" > "$ROOT_DIR/webapp/public/version.json"

# ── 3. Start k2 daemon on dev port ──
K2_DEV_CONFIG="$ROOT_DIR/.k2-dev-config.yaml"
cat > "$K2_DEV_CONFIG" <<YAML
listen: "$K2_DEV_ADDR"
YAML

echo "[dev-windows] Starting k2 daemon on $K2_DEV_ADDR..."
"$K2_BIN" run -c "$K2_DEV_CONFIG" &
PIDS+=($!)

# Brief wait for daemon to bind the port
sleep 1

# ── 4. Start Vite dev server ──
export K2_DAEMON_PORT="${K2_DEV_ADDR##*:}"
echo "[dev-windows] Starting Vite dev server (K2_DAEMON_PORT=$K2_DAEMON_PORT)..."
cd "$ROOT_DIR/webapp"
yarn dev &
PIDS+=($!)

# Wait for Vite to be ready on :1420
echo "[dev-windows] Waiting for Vite on :1420..."
for i in $(seq 1 60); do
  if curl -s -o /dev/null http://localhost:1420 2>/dev/null; then
    echo "[dev-windows] Vite ready."
    break
  fi
  if [ "$i" -eq 60 ]; then
    echo "[dev-windows] Vite failed to start within 60s"
    exit 1
  fi
  sleep 1
done

# ── 5. Start Tauri dev window ──
echo "[dev-windows] Starting Tauri dev..."
cd "$ROOT_DIR/desktop"
yarn tauri dev &
PIDS+=($!)

echo "[dev-windows] All services running. Press Ctrl+C to stop."
echo "[dev-windows]   k2 daemon:  $K2_DEV_ADDR"
echo "[dev-windows]   Vite:       http://localhost:1420"
echo "[dev-windows]   Tauri:      building..."
wait
