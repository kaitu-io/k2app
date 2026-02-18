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
  docker compose -f "$API_DIR/docker-compose.yml" stop 2>/dev/null || true
  echo "All services stopped."
}
trap cleanup EXIT INT TERM

# ── 1. Start MySQL + Redis via docker-compose ──
echo "[dev-desktop] Starting MySQL + Redis..."
docker compose -f "$API_DIR/docker-compose.yml" up -d

echo "[dev-desktop] Waiting for MySQL..."
for i in $(seq 1 30); do
  if docker compose -f "$API_DIR/docker-compose.yml" exec -T mysql mysqladmin ping -u root -p123456 --silent 2>/dev/null; then
    echo "[dev-desktop] MySQL ready."
    break
  fi
  if [ "$i" -eq 30 ]; then
    echo "[dev-desktop] MySQL failed to start within 30s"
    exit 1
  fi
  sleep 1
done

# ── 2. Build and start API server ──
echo "[dev-desktop] Building API server..."
cd "$ROOT_DIR/api/cmd"
go build -o "$API_BIN" .

echo "[dev-desktop] Starting API server on :5800..."
cd "$API_DIR"
"$API_BIN" start -f -c "$API_DIR/config.yml" &
PIDS+=($!)
sleep 2

# ── 3. Build k2 if missing or outdated ──
if [ ! -f "$K2_BIN" ] || [ "$ROOT_DIR/k2/cmd/k2/main.go" -nt "$K2_BIN" ]; then
  echo "[dev-desktop] Building k2..."
  cd "$ROOT_DIR/k2"
  go build -tags nowebapp -o "$K2_BIN" ./cmd/k2
fi

# ── 4. Start k2 daemon on dev port ──
echo "[dev-desktop] Starting k2 daemon on $K2_DEV_ADDR..."
"$K2_BIN" run -l "$K2_DEV_ADDR" &
PIDS+=($!)

# ── 5. Start Vite dev server ──
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
yarn tauri dev &
PIDS+=($!)

echo "[dev-desktop] All services running. Press Ctrl+C to stop."
wait
