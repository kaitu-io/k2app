#!/usr/bin/env bash
set -euo pipefail

# macOS NE xcframework build script for k2app.
# Produces K2MobileMacOS.xcframework via gomobile bind -target=macos.
# The resulting xcframework is used by the KaituTunnel.appex (Network Extension).
#
# Usage: bash scripts/build-mobile-macos-ne.sh

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(dirname "$SCRIPT_DIR")"

cd "$ROOT_DIR"

# --- Read version ---
VERSION=$(node -p "require('./package.json').version")
echo "=== Building K2MobileMacOS.xcframework v${VERSION} for macOS NE ==="

# --- Build xcframework via Makefile target ---
echo ""
echo "--- Building gomobile macOS xcframework ---"
make mobile-macos

# --- Verify output ---
echo ""
echo "--- Verifying output ---"
XCFRAMEWORK_DIR="k2/build/K2MobileMacOS.xcframework"

if [ ! -d "$XCFRAMEWORK_DIR" ]; then
  echo "ERROR: $XCFRAMEWORK_DIR not found after build"
  exit 1
fi
echo "xcframework exists: $XCFRAMEWORK_DIR"

MACOS_SLICE_DIR="${XCFRAMEWORK_DIR}/macos-arm64"
if [ ! -d "$MACOS_SLICE_DIR" ]; then
  echo "ERROR: macos-arm64 slice missing from $XCFRAMEWORK_DIR"
  exit 1
fi
echo "macos-arm64 slice exists: $MACOS_SLICE_DIR"

HEADER_FILE=$(find "$XCFRAMEWORK_DIR" -name "K2Mobile.h" 2>/dev/null | head -1)
if [ -z "$HEADER_FILE" ]; then
  echo "ERROR: K2Mobile.h header not found inside $XCFRAMEWORK_DIR"
  exit 1
fi
echo "K2Mobile.h header found: $HEADER_FILE"

# --- Summary ---
echo ""
echo "=== macOS NE xcframework ready ==="
echo "  Location: $XCFRAMEWORK_DIR"
echo "  For use by: KaituTunnel.appex (Network Extension)"
