#!/usr/bin/env bash
# Phase 0: Build k2 for Windows ARM64 and deploy to VM
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/env.sh"

echo "=== Phase 0: Build & Deploy k2 to Windows ==="

# Step 1: Build k2 for Windows ARM64
echo "[1/4] Building k2 for windows/arm64..."
cd "$K2_PROJECT_ROOT"
GOOS=windows GOARCH=arm64 CGO_ENABLED=0 go build -o /tmp/k2-windows.exe ./cmd/k2
echo "  Built: $(ls -lh /tmp/k2-windows.exe | awk '{print $5}')"

# Step 2: Ensure test directory exists on Windows
echo "[2/4] Creating test directory on Windows..."
win "mkdir C:\\Users\\david\\k2-test 2>nul & echo OK"

# Step 3: Upload binary
echo "[3/4] Uploading k2.exe to Windows..."
scp /tmp/k2-windows.exe "$WIN_USER@$WIN_HOST:k2-test/k2.exe"
echo "  Uploaded."

# Step 4: Verify binary runs
echo "[4/4] Verifying binary..."
VERSION=$(win "C:\\Users\\david\\k2-test\\k2.exe version 2>&1" || echo "FAILED")
echo "  k2 version: $VERSION"

if echo "$VERSION" | grep -qi "failed\|error\|not recognized"; then
    echo "  ERROR: k2.exe failed to run on Windows"
    exit 1
fi

echo ""
echo "=== Phase 0 complete ==="
