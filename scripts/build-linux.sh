#!/usr/bin/env bash
set -euo pipefail

# Linux build script for k2app (runs on macOS host).
#
# Only the Rust/Tauri compilation runs in Docker (needs webkit2gtk linking).
# The Docker container runs ARM64 natively (no QEMU) and cross-compiles
# to x86_64 using gcc-x86-64-linux-gnu + multiarch webkit2gtk:amd64 libs.
#
# Everything else runs natively on macOS for speed:
#   - Go cross-compile (GOOS=linux GOARCH=amd64)
#   - Webapp build (pure JS)
#   - tar.gz packaging
#
# Caching (all on host for max speed):
#   ~/.cache/k2app-linux/cargo/    Cargo registry + git index
#   ~/.cache/k2app-linux/target/   Rust compile cache (incremental)
#   ~/.cache/k2app-linux/node_modules/          Root JS deps (Linux-native)
#   ~/.cache/k2app-linux/webapp-node_modules/   webapp JS deps
#   ~/.cache/k2app-linux/desktop-node_modules/  desktop JS deps
#
# Output: release/VERSION/Kaitu_VERSION_amd64.tar.gz

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(dirname "$SCRIPT_DIR")"
cd "$ROOT_DIR"

IMAGE_NAME="k2app-linux-builder"
CACHE_DIR="$HOME/.cache/k2app-linux"

VERSION=$(node -p "require('./package.json').version")
K2_COMMIT=$(git rev-parse --short HEAD 2>/dev/null || echo "")
K2_BUILD_LOG_LEVEL="${K2_BUILD_LOG_LEVEL:-debug}"
export K2_BUILD_LOG_LEVEL K2_COMMIT

echo "=== Building k2app Linux v${VERSION} ==="

# --- Step 1: pre-build + webapp (macOS native, fast) ---
echo "--- [host] Pre-build + webapp ---"
make pre-build
make build-webapp

# --- Step 2: Go cross-compile k2 for Linux (macOS native, fast) ---
echo "--- [host] Go cross-compile k2 for Linux ---"
make build-k2-linux

# --- Step 3: Tauri cross-compile in Docker (ARM64 native, no QEMU) ---
echo "--- [docker] Cross-compiling Tauri binary (ARM64 → x86_64) ---"

# Build Docker image (no --platform = native ARM64, cached by layer cache)
docker build \
    -t "$IMAGE_NAME" \
    -f docker/Dockerfile.linux-build \
    .

# Create host cache directories
mkdir -p "$CACHE_DIR"/{cargo,target,node_modules,webapp-node_modules,desktop-node_modules}

# Docker bind mounts:
# - /src            ← repo root
# - /cargo-cache    ← cargo registry + git (persisted on host)
# - target/         ← Rust compile cache (persisted on host)
# - node_modules/*  ← Linux-native JS deps (isolated from macOS host)
docker run --rm \
    -v "$ROOT_DIR:/src" \
    -v "$CACHE_DIR/cargo:/cargo-cache" \
    -v "$CACHE_DIR/target:/src/desktop/src-tauri/target" \
    -v "$CACHE_DIR/node_modules:/src/node_modules" \
    -v "$CACHE_DIR/webapp-node_modules:/src/webapp/node_modules" \
    -v "$CACHE_DIR/desktop-node_modules:/src/desktop/node_modules" \
    -e "K2_BUILD_LOG_LEVEL=$K2_BUILD_LOG_LEVEL" \
    "$IMAGE_NAME"

# --- Step 4: Package tar.gz (macOS native) ---
echo "--- [host] Packaging tar.gz ---"
K2_BIN="desktop/src-tauri/binaries"
RELEASE_DIR="release/${VERSION}"
mkdir -p "${RELEASE_DIR}/linux-pkg"
cp "$CACHE_DIR/target/x86_64-unknown-linux-gnu/release/k2app" "${RELEASE_DIR}/linux-pkg/k2app"
cp "${K2_BIN}/k2-x86_64-unknown-linux-gnu" "${RELEASE_DIR}/linux-pkg/k2"
cp desktop/src-tauri/icons/128x128.png "${RELEASE_DIR}/linux-pkg/kaitu.png"
chmod +x "${RELEASE_DIR}/linux-pkg/k2app" "${RELEASE_DIR}/linux-pkg/k2"
(cd "${RELEASE_DIR}/linux-pkg" && tar czf "../Kaitu_${VERSION}_amd64.tar.gz" k2app k2 kaitu.png)
rm -rf "${RELEASE_DIR}/linux-pkg"

# Verify
TARBALL="${RELEASE_DIR}/Kaitu_${VERSION}_amd64.tar.gz"
SIZE=$(du -h "$TARBALL" | cut -f1)
echo ""
echo "=== Linux build complete ==="
echo "  $TARBALL ($SIZE)"
echo ""
echo "Contents:"
tar tzf "$TARBALL"
