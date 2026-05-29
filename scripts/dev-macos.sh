#!/usr/bin/env bash
set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(dirname "$SCRIPT_DIR")"
K2_BIN="$ROOT_DIR/desktop/src-tauri/binaries/k2-$(uname -m)-apple-darwin"
K2_DEV_ADDR="127.0.0.1:11777"  # dev daemon port (avoid conflict with system service on :1777)
API_BIN="$ROOT_DIR/api/cmd/kaitu-center"
API_DIR="$ROOT_DIR/api"

PIDS=()

cleanup() {
  echo ""
  echo "Stopping all dev services..."
  for pid in "${PIDS[@]}"; do
    kill "$pid" 2>/dev/null || true
  done
  for pid in "${PIDS[@]}"; do
    wait "$pid" 2>/dev/null || true
  done
  echo "All services stopped."
}
trap cleanup EXIT INT TERM

# ── 1. Verify shared dev containers (dev-mariadb / dev-redis) ──
# User-level long-running containers managed via mysql-dev / redis-dev MCP.
# Project no longer ships its own docker-compose — see api/docker-compose.yml.deprecated.
echo "[dev-desktop] Checking shared dev containers..."
missing=()
nc -z 127.0.0.1 3306 2>/dev/null || missing+=("dev-mariadb (127.0.0.1:3306)")
nc -z 127.0.0.1 6379 2>/dev/null || missing+=("dev-redis (127.0.0.1:6379)")
if [ ${#missing[@]} -ne 0 ]; then
  echo "[dev-desktop] ERROR: shared dev container(s) not reachable: ${missing[*]}" >&2
  echo "[dev-desktop] Start them via the mysql-dev / redis-dev MCP, or run 'docker ps' to verify." >&2
  exit 1
fi
echo "[dev-desktop] MySQL + Redis reachable."

# ── 2. Build and start API server ──
echo "[dev-desktop] Building API server..."
cd "$ROOT_DIR/api/cmd"
go build -o "$API_BIN" .

echo "[dev-desktop] Starting API server on :5800..."
cd "$API_DIR"
"$API_BIN" start -f -c "$API_DIR/config.yml" &
PIDS+=($!)
sleep 2

# ── 3. Build and start k2 daemon ──
# Rebuild if binary is missing or any non-test .go source under k2/ is newer.
# (`find … -quit` short-circuits on the first match.)
if [ ! -f "$K2_BIN" ] || \
   [ -n "$(find "$ROOT_DIR/k2" -name '*.go' -not -name '*_test.go' -newer "$K2_BIN" -print -quit 2>/dev/null)" ]; then
  echo "[dev-desktop] Building k2..."
  cd "$ROOT_DIR/k2"
  go build -tags nowebapp -o "$K2_BIN" ./cmd/k2
fi
# Create minimal dev config for k2 daemon
K2_DEV_CONFIG="$ROOT_DIR/.k2-dev-config.yaml"
cat > "$K2_DEV_CONFIG" <<YAML
listen: "$K2_DEV_ADDR"
YAML

echo "[dev-desktop] Starting k2 daemon on $K2_DEV_ADDR..."
# k2 needs root for TUN device (actual VPN connection). Run with sudo.
sudo "$K2_BIN" run -c "$K2_DEV_CONFIG" &
PIDS+=($!)

# ── 5. Start Vite dev server ──
# Export K2_DAEMON_PORT so Vite proxy targets the dev daemon (for standalone browser testing)
export K2_DAEMON_PORT="${K2_DEV_ADDR##*:}"
echo "[dev-desktop] Starting Vite dev server..."
cd "$ROOT_DIR/webapp"
yarn dev &
PIDS+=($!)

# Wait for Vite to be ready on :1420
echo "[dev-desktop] Waiting for Vite on :1420..."
for i in $(seq 1 60); do
  if curl -s -o /dev/null http://localhost:1420 2>/dev/null; then
    echo "[dev-desktop] Vite ready."
    break
  fi
  if [ "$i" -eq 60 ]; then
    echo "[dev-desktop] Vite failed to start within 60s"
    exit 1
  fi
  sleep 1
done

# ── 6. Launch Tauri dev window (with dev daemon port) ──
echo "[dev-desktop] Starting Tauri dev (K2_DAEMON_PORT=${K2_DEV_ADDR##*:})..."
export K2_DAEMON_PORT="${K2_DEV_ADDR##*:}"
cd "$ROOT_DIR/desktop"
yarn tauri dev --features mcp-bridge &
PIDS+=($!)

echo "[dev-desktop] All services running. Press Ctrl+C to stop."
wait
