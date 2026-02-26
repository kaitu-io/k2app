#!/usr/bin/env bash
set -euo pipefail

# Windows test build script.
# Delegates to `make build-windows FEATURES=mcp-bridge` on a remote Windows VM.
# Usage: bash scripts/build-windows-test.sh [--tunnel] [--deploy]
#   --tunnel   Start SSH tunnel for Tauri MCP (port 19223 → VM:9223)
#   --deploy   Run the NSIS installer after build (silent install)

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(dirname "$SCRIPT_DIR")"

WIN_HOST="${WIN_HOST:-10.211.55.6}"
WIN_USER="${WIN_USER:-david}"
WIN_REPO="C:/Users/${WIN_USER}/projects/kaitu-io/k2app"
WIN_TARGET="x86_64-pc-windows-msvc"
SSH="ssh -o ConnectTimeout=10 -o StrictHostKeyChecking=no ${WIN_USER}@${WIN_HOST}"

START_TUNNEL=false
DEPLOY=false
for arg in "$@"; do
  case "$arg" in
    --tunnel) START_TUNNEL=true ;;
    --deploy) DEPLOY=true ;;
    *) echo "Unknown argument: $arg"; exit 1 ;;
  esac
done

VERSION=$(node -p "require('$ROOT_DIR/package.json').version")
echo "=== Building Kaitu $VERSION for Windows (test, unsigned, mcp-bridge) ==="
echo "Target: $WIN_TARGET"
echo "VM: ${WIN_USER}@${WIN_HOST}"

# --- Step 1: Ensure repo exists on Windows VM ---
echo ""
echo "--- Syncing repo to Windows VM ---"

REPO_EXISTS=$($SSH "if exist \"${WIN_REPO}\\.git\" (echo yes) else (echo no)" 2>/dev/null | tr -d '\r')

if [ "$REPO_EXISTS" = "yes" ]; then
  echo "Repo exists, pulling latest..."
  LOCAL_BRANCH=$(git branch --show-current)
  $SSH "cd /d ${WIN_REPO} && git fetch origin && git checkout ${LOCAL_BRANCH} && git reset --hard origin/${LOCAL_BRANCH} && git submodule update --init --recursive" 2>&1
else
  echo "Cloning repo..."
  $SSH "mkdir \"C:\\Users\\${WIN_USER}\\projects\\kaitu-io\" 2>NUL & git clone --recursive git@github.com:kaitu-io/k2app.git ${WIN_REPO}" 2>&1
fi

LOCAL_HEAD=$(git rev-parse HEAD)
echo "Local HEAD: $LOCAL_HEAD"
$SSH "cd /d ${WIN_REPO} && git rev-parse HEAD" 2>&1 | tr -d '\r'

# --- Step 2: Install dependencies ---
echo ""
echo "--- Installing dependencies ---"
$SSH "cd /d ${WIN_REPO} && yarn install --frozen-lockfile 2>&1" 2>&1 | tail -5

# --- Step 3: Build (delegates to make build-windows with mcp-bridge feature) ---
echo ""
echo "--- Building (make build-windows FEATURES=mcp-bridge) ---"
$SSH "cd /d ${WIN_REPO} && make build-windows FEATURES=mcp-bridge" 2>&1

# --- Step 4: Locate and copy installer ---
echo ""
echo "--- Collecting artifacts ---"

NSIS_DIR="${WIN_REPO}/desktop/src-tauri/target/${WIN_TARGET}/release/bundle/nsis"
INSTALLER_NAME="Kaitu_${VERSION}_x64-setup.exe"

$SSH "if exist \"${NSIS_DIR}/${INSTALLER_NAME}\" (echo found) else (echo not_found)" 2>&1 | tr -d '\r' | grep -q "found" || {
  echo "ERROR: Installer not found at ${NSIS_DIR}/${INSTALLER_NAME}"
  echo "Listing bundle dir:"
  $SSH "dir \"${NSIS_DIR}\" 2>NUL" 2>&1
  exit 1
}

RELEASE_DIR="$ROOT_DIR/release/$VERSION"
mkdir -p "$RELEASE_DIR"
scp "${WIN_USER}@${WIN_HOST}:${NSIS_DIR}/${INSTALLER_NAME}" "$RELEASE_DIR/" 2>&1
echo "Installer copied to: $RELEASE_DIR/$INSTALLER_NAME"

SIG_NAME="${INSTALLER_NAME}.sig"
scp "${WIN_USER}@${WIN_HOST}:${NSIS_DIR}/${SIG_NAME}" "$RELEASE_DIR/" 2>/dev/null && \
  echo "Signature copied to: $RELEASE_DIR/$SIG_NAME" || true

# --- Step 5: Deploy (optional) ---
if [ "$DEPLOY" = true ]; then
  echo ""
  echo "--- Deploying installer on Windows VM ---"
  $SSH "taskkill /F /IM Kaitu.exe 2>NUL & taskkill /F /IM k2.exe 2>NUL" 2>&1 || true
  sleep 2
  $SSH "\"${NSIS_DIR}/${INSTALLER_NAME}\" /S" 2>&1
  echo "Installer running (silent mode)..."
  sleep 10
  RUNNING=$($SSH "tasklist | findstr Kaitu.exe" 2>&1 | tr -d '\r')
  if [ -n "$RUNNING" ]; then
    echo "Kaitu.exe is running"
  else
    echo "WARNING: Kaitu.exe not detected after install"
    echo "Starting manually..."
    $SSH "start \"\" \"C:\\Program Files\\Kaitu\\Kaitu.exe\"" 2>&1 || true
    sleep 5
  fi
fi

# --- Step 6: SSH tunnel for Tauri MCP (optional) ---
if [ "$START_TUNNEL" = true ]; then
  echo ""
  echo "--- Starting SSH tunnel for Tauri MCP ---"
  echo "Forwarding localhost:19223 → ${WIN_HOST}:9223"
  echo "Use: driver_session with port 19223 to connect"
  echo "Press Ctrl+C to stop tunnel"
  ssh -N -L 19223:127.0.0.1:9223 "${WIN_USER}@${WIN_HOST}"
fi

# --- Summary ---
echo ""
echo "=== Build complete ==="
echo "Installer: $RELEASE_DIR/$INSTALLER_NAME"
echo ""
echo "To deploy:  make build-windows-test ARGS='--deploy'"
echo "To tunnel:  ssh -N -L 19223:127.0.0.1:9223 ${WIN_USER}@${WIN_HOST}"
echo "To connect: driver_session(action='start', port=19223)"
ls -la "$RELEASE_DIR/"
