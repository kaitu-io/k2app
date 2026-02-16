#!/usr/bin/env bash
set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(dirname "$SCRIPT_DIR")"
K2_BIN="$ROOT_DIR/desktop/src-tauri/binaries/k2-$(uname -m)-apple-darwin"
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
  # Stop docker-compose
  docker compose -f "$API_DIR/docker-compose.yml" stop 2>/dev/null || true
  echo "All services stopped."
}
trap cleanup EXIT INT TERM

# ── 1. Start MySQL + Redis via docker-compose ──
echo "[dev] Starting MySQL + Redis..."
docker compose -f "$API_DIR/docker-compose.yml" up -d

# Wait for MySQL to be ready
echo "[dev] Waiting for MySQL..."
for i in $(seq 1 30); do
  if docker compose -f "$API_DIR/docker-compose.yml" exec -T mysql mysqladmin ping -u root -p123456 --silent 2>/dev/null; then
    echo "[dev] MySQL ready."
    break
  fi
  if [ "$i" -eq 30 ]; then
    echo "[dev] MySQL failed to start within 30s"
    exit 1
  fi
  sleep 1
done

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

# ── 4. Start k2 daemon ──
echo "[dev] Starting k2 daemon..."
"$K2_BIN" run &
PIDS+=($!)

# ── 5. Start Tauri dev (with MCP Bridge) ──
echo "[dev] Starting Tauri dev..."
cd "$ROOT_DIR/desktop"
yarn tauri dev --features mcp-bridge
