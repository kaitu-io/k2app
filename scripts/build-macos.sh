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
GOARCH=arm64 GOOS=darwin make build-k2 TARGET=aarch64-apple-darwin

echo ""
echo "--- Building k2 (x86_64-apple-darwin) ---"
GOARCH=amd64 GOOS=darwin make build-k2 TARGET=x86_64-apple-darwin

# --- Create universal k2 binary with lipo ---
echo ""
echo "--- Creating universal k2 binary ---"
K2_BIN_DIR="desktop/src-tauri/binaries"
lipo -create \
  "$K2_BIN_DIR/k2-aarch64-apple-darwin" \
  "$K2_BIN_DIR/k2-x86_64-apple-darwin" \
  -output "$K2_BIN_DIR/k2-universal-apple-darwin"
chmod +x "$K2_BIN_DIR/k2-universal-apple-darwin"
echo "Created universal binary: $K2_BIN_DIR/k2-universal-apple-darwin"

# --- Build gomobile macOS xcframework ---
echo ""
echo "--- Building gomobile macOS xcframework ---"
make mobile-macos

# --- Build libk2_ne_helper.a ---
echo ""
echo "--- Building NE helper static library ---"
cd "$ROOT_DIR/desktop/src-tauri/ne_helper"
bash build.sh universal
cd "$ROOT_DIR"

# Set env var for Rust build.rs to find the library
export NE_HELPER_LIB_DIR="$ROOT_DIR/desktop/src-tauri/ne_helper"

# --- Tauri build (universal binary) ---
echo ""
echo "--- Building Tauri app (universal-apple-darwin) ---"
cd desktop
if [ "$SKIP_NOTARIZATION" = true ]; then
  # Unset Apple credentials to prevent Tauri's built-in notarization
  unset APPLE_ID APPLE_PASSWORD APPLE_TEAM_ID
fi
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

# --- Build and inject KaituTunnel.appex ---
echo ""
echo "--- Building KaituTunnel.appex ---"
APPEX_BUILD_DIR=$(mktemp -d /tmp/kaitu-appex.XXXXXX)
APPEX_DIR="$APPEX_BUILD_DIR/KaituTunnel.appex/Contents/MacOS"
mkdir -p "$APPEX_DIR"

XCFW_PATH="$ROOT_DIR/k2/build/K2MobileMacOS.xcframework"
MACOS_SDK_PATH=$(xcrun --show-sdk-path --sdk macosx)

# Compile PacketTunnelProvider.swift → KaituTunnel executable
swiftc \
  -emit-executable \
  -module-name KaituTunnel \
  -sdk "$MACOS_SDK_PATH" \
  -target arm64-apple-macos12 \
  -F "$XCFW_PATH" \
  -framework K2MobileMacOS \
  "$ROOT_DIR/desktop/src-tauri/KaituTunnel/PacketTunnelProvider.swift" \
  -o "$APPEX_DIR/KaituTunnel"

# Copy Info.plist into appex bundle
cp "$ROOT_DIR/desktop/src-tauri/KaituTunnel/Info.plist" \
   "$APPEX_BUILD_DIR/KaituTunnel.appex/Contents/"

# Install appex into .app bundle PlugIns directory
mkdir -p "$APP_PATH/Contents/PlugIns"
cp -R "$APPEX_BUILD_DIR/KaituTunnel.appex" "$APP_PATH/Contents/PlugIns/"

# Determine signing identity (use env var or well-known default)
SIGN_IDENTITY="${APPLE_SIGNING_IDENTITY:-Developer ID Application: ALL NATION CONNECT TECHNOLOGY PTE. LTD. (NJT954Q3RH)}"

# Codesign the appex with its own entitlements
echo "--- Codesigning KaituTunnel.appex ---"
codesign --force --sign "$SIGN_IDENTITY" \
  --entitlements "$ROOT_DIR/desktop/src-tauri/KaituTunnel/KaituTunnel.entitlements" \
  --options runtime \
  "$APP_PATH/Contents/PlugIns/KaituTunnel.appex"

# Re-codesign main app (deep) now that PlugIns directory has changed
echo "--- Re-codesigning main app (deep) after PlugIns injection ---"
codesign --force --sign "$SIGN_IDENTITY" \
  --entitlements "$ROOT_DIR/desktop/src-tauri/entitlements.plist" \
  --options runtime \
  --deep "$APP_PATH"

rm -rf "$APPEX_BUILD_DIR"
echo "KaituTunnel.appex injected into $APP_PATH/Contents/PlugIns/"

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

# Stage only the .app for pkgbuild (exclude updater artifacts)
PKG_STAGE=$(mktemp -d /tmp/k2app-pkg-stage.XXXXXX)
cp -R "$APP_PATH" "$PKG_STAGE/"

# Create component plist with BundleIsRelocatable=false
COMPONENT_PLIST=$(mktemp /tmp/k2app-component.XXXXXX)
cat > "$COMPONENT_PLIST" <<'PLIST'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<array>
  <dict>
    <key>BundleHasStrictIdentifier</key>
    <true/>
    <key>BundleIsRelocatable</key>
    <false/>
    <key>BundleIsVersionChecked</key>
    <false/>
    <key>BundleOverwriteAction</key>
    <string>upgrade</string>
    <key>RootRelativeBundlePath</key>
    <string>Kaitu.app</string>
  </dict>
</array>
</plist>
PLIST

pkgbuild \
  --root "$PKG_STAGE" \
  --component-plist "$COMPONENT_PLIST" \
  --scripts "$ROOT_DIR/scripts/pkg-scripts" \
  --identifier io.kaitu.desktop \
  --version "$VERSION" \
  --install-location "/Applications" \
  "$PKG_UNSIGNED"

rm -rf "$PKG_STAGE" "$COMPONENT_PLIST"

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
