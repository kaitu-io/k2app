#!/usr/bin/env bash
set -euo pipefail

# macOS build script for k2app.
# Builds universal binary, creates .pkg installer, signs, and notarizes.
# Usage: bash scripts/build-macos.sh [--skip-notarization]

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(dirname "$SCRIPT_DIR")"

cd "$ROOT_DIR"

# --- Argument parsing ---
SKIP_NOTARIZATION=false
for arg in "$@"; do
  case "$arg" in
    --skip-notarization) SKIP_NOTARIZATION=true ;;
    *) echo "Unknown argument: $arg"; exit 1 ;;
  esac
done

# --- Read version ---
VERSION=$(node -p "require('./package.json').version")
echo "=== Building Kaitu $VERSION for macOS (universal) ==="

# --- Pre-build + webapp ---
echo ""
echo "--- Pre-build ---"
make pre-build

echo ""
echo "--- Building webapp ---"
make build-webapp

# --- Build k2 for both architectures ---
echo ""
echo "--- Building k2 (aarch64-apple-darwin) ---"
make build-k2 TARGET=aarch64-apple-darwin

echo ""
echo "--- Building k2 (x86_64-apple-darwin) ---"
make build-k2 TARGET=x86_64-apple-darwin

# --- Tauri build (universal binary) ---
echo ""
echo "--- Building Tauri app (universal-apple-darwin) ---"
cd desktop
yarn tauri build --target universal-apple-darwin --config src-tauri/tauri.bundle.conf.json
cd "$ROOT_DIR"

# --- Locate .app bundle ---
BUNDLE_DIR="desktop/src-tauri/target/universal-apple-darwin/release/bundle/macos"
APP_PATH="$BUNDLE_DIR/Kaitu.app"

if [ ! -d "$APP_PATH" ]; then
  echo "ERROR: $APP_PATH not found"
  exit 1
fi
echo "Found app bundle: $APP_PATH"

# --- Verify codesign ---
echo ""
echo "--- Verifying codesign ---"
codesign --verify --deep --strict "$APP_PATH"
echo "codesign verification passed"

# --- Create .pkg with pkgbuild ---
echo ""
echo "--- Creating .pkg installer ---"
RELEASE_DIR="release/$VERSION"
mkdir -p "$RELEASE_DIR"

PKG_UNSIGNED="$RELEASE_DIR/Kaitu-${VERSION}-unsigned.pkg"
PKG_SIGNED="$RELEASE_DIR/Kaitu-${VERSION}.pkg"

pkgbuild \
  --root "$APP_PATH" \
  --component-plist /dev/stdin \
  --identifier io.kaitu.desktop \
  --version "$VERSION" \
  --install-location "/Applications/Kaitu.app" \
  "$PKG_UNSIGNED" <<'PLIST'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<array>
  <dict>
    <key>BundleIsRelocatable</key>
    <false/>
    <key>BundleIsVersionChecked</key>
    <false/>
  </dict>
</array>
</plist>
PLIST

echo "Created unsigned pkg: $PKG_UNSIGNED"

# --- Sign .pkg with productsign (if identity available) ---
if [ -n "${APPLE_INSTALLER_IDENTITY:-}" ]; then
  echo ""
  echo "--- Signing .pkg ---"
  productsign --sign "$APPLE_INSTALLER_IDENTITY" "$PKG_UNSIGNED" "$PKG_SIGNED"
  rm -f "$PKG_UNSIGNED"
  echo "Signed pkg: $PKG_SIGNED"
else
  echo "APPLE_INSTALLER_IDENTITY not set, skipping pkg signing"
  mv "$PKG_UNSIGNED" "$PKG_SIGNED"
fi

# --- Notarize .pkg ---
if [ "$SKIP_NOTARIZATION" = true ]; then
  echo ""
  echo "--- Skipping notarization (--skip-notarization) ---"
elif [ -n "${APPLE_ID:-}" ] && [ -n "${APPLE_PASSWORD:-}" ] && [ -n "${APPLE_TEAM_ID:-}" ]; then
  echo ""
  echo "--- Notarizing .pkg ---"
  xcrun notarytool submit "$PKG_SIGNED" \
    --apple-id "$APPLE_ID" \
    --password "$APPLE_PASSWORD" \
    --team-id "$APPLE_TEAM_ID" \
    --wait

  echo "--- Stapling notarization ticket ---"
  xcrun stapler staple "$PKG_SIGNED"
  echo "Notarization complete"
else
  echo ""
  echo "Notarization credentials not set (APPLE_ID, APPLE_PASSWORD, APPLE_TEAM_ID), skipping"
fi

# --- Collect updater artifacts (.app.tar.gz + .sig) ---
echo ""
echo "--- Collecting artifacts ---"

APP_TAR_GZ=$(find "$BUNDLE_DIR" -name '*.app.tar.gz' -maxdepth 1 2>/dev/null | head -1)
if [ -n "$APP_TAR_GZ" ]; then
  cp "$APP_TAR_GZ" "$RELEASE_DIR/"
  echo "Copied: $(basename "$APP_TAR_GZ")"
fi

APP_SIG=$(find "$BUNDLE_DIR" -name '*.app.tar.gz.sig' -maxdepth 1 2>/dev/null | head -1)
if [ -n "$APP_SIG" ]; then
  cp "$APP_SIG" "$RELEASE_DIR/"
  echo "Copied: $(basename "$APP_SIG")"
fi

# --- Summary ---
echo ""
echo "=== Build complete ==="
echo "Release artifacts in $RELEASE_DIR/:"
ls -la "$RELEASE_DIR/"
