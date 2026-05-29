#!/usr/bin/env bash
set -e

# Webapp browser-only dev (no Tauri). Starts k2 daemon for standalone webapp
# testing. For Tauri desktop dev, use `make dev-desktop` instead.
# Note: On macOS, the Tauri app uses Network Extension (not daemon).

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(dirname "$SCRIPT_DIR")"
K2_BIN="$ROOT_DIR/desktop/src-tauri/binaries/k2-$(uname -m)-apple-darwin"
K2_DEV_ADDR="127.0.0.1:11777"  # dev daemon port (system service uses :1777)
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
echo "[dev] Checking shared dev containers..."
missing=()
nc -z 127.0.0.1 3306 2>/dev/null || missing+=("dev-mariadb (127.0.0.1:3306)")
nc -z 127.0.0.1 6379 2>/dev/null || missing+=("dev-redis (127.0.0.1:6379)")
if [ ${#missing[@]} -ne 0 ]; then
  echo "[dev] ERROR: shared dev container(s) not reachable: ${missing[*]}" >&2
  echo "[dev] Start them via the mysql-dev / redis-dev MCP, or run 'docker ps' to verify." >&2
  exit 1
fi
echo "[dev] MySQL + Redis reachable."

# ── 2. Build and start API server ──
echo "[dev] Building API server..."
cd "$ROOT_DIR/api/cmd"
go build -o "$API_BIN" .

echo "[dev] Starting API server on :5800..."
cd "$API_DIR"
"$API_BIN" start -f -c "$API_DIR/config.yml" &
PIDS+=($!)
sleep 2

# ── 3. Build k2 if missing or outdated ──
if [ ! -f "$K2_BIN" ] || [ "$ROOT_DIR/k2/cmd/k2/main.go" -nt "$K2_BIN" ]; then
  echo "[dev] Building k2..."
  cd "$ROOT_DIR/k2"
  go build -tags nowebapp -o "$K2_BIN" ./cmd/k2
fi

# ── 4. Start k2 daemon (dev port, system service keeps :1777) ──
K2_DEV_CONFIG="$ROOT_DIR/.k2-dev-config.yaml"
cat > "$K2_DEV_CONFIG" <<YAML
listen: "$K2_DEV_ADDR"
YAML

echo "[dev] Starting k2 daemon on $K2_DEV_ADDR..."
"$K2_BIN" run -c "$K2_DEV_CONFIG" &
PIDS+=($!)

# ── 5. Start Vite dev server ──
export K2_DAEMON_PORT="${K2_DEV_ADDR##*:}"
echo "[dev] Starting Vite dev server (proxy → :$K2_DAEMON_PORT)..."
cd "$ROOT_DIR/webapp"
yarn dev &
PIDS+=($!)

# Wait for Vite to be ready on :1420
echo "[dev] Waiting for Vite on :1420..."
for i in $(seq 1 60); do
  if curl -s -o /dev/null http://localhost:1420 2>/dev/null; then
    echo "[dev] Vite ready."
    break
  fi
  if [ "$i" -eq 60 ]; then
    echo "[dev] Vite failed to start within 60s"
    exit 1
  fi
  sleep 1
done

# ── 6. Open in browser ──
echo "[dev] Opening http://localhost:1420 ..."
open "http://localhost:1420" 2>/dev/null || true

echo "[dev] All services running. Press Ctrl+C to stop."
wait
