#!/usr/bin/env bash
set -euo pipefail

# macOS build script for k2app.
# Builds universal binary, creates .pkg installer, signs, and notarizes.
# Usage: bash scripts/build-macos.sh [--skip-notarization] [--single-arch] [--features=FEATURE]

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(dirname "$SCRIPT_DIR")"

cd "$ROOT_DIR"

# --- Argument parsing ---
SKIP_NOTARIZATION=false
SINGLE_ARCH=false
EXTRA_FEATURES=""
for arg in "$@"; do
  case "$arg" in
    --skip-notarization) SKIP_NOTARIZATION=true ;;
    --single-arch) SINGLE_ARCH=true ;;
    --features=*) EXTRA_FEATURES="${arg#--features=}" ;;
    *) echo "Unknown argument: $arg"; exit 1 ;;
  esac
done

# --- Read version ---
VERSION=$(node -p "require('./package.json').version")

if [ "$SINGLE_ARCH" = true ]; then
  echo "=== Building Kaitu $VERSION for macOS (single-arch) ==="
else
  echo "=== Building Kaitu $VERSION for macOS (universal) ==="
fi

# Determine native architecture for single-arch builds
if [ "$SINGLE_ARCH" = true ]; then
  NATIVE_ARCH=$(uname -m)
  if [ "$NATIVE_ARCH" = "arm64" ]; then
    K2_TARGET="aarch64-apple-darwin"
    K2_GOARCH="arm64"
  else
    K2_TARGET="x86_64-apple-darwin"
    K2_GOARCH="amd64"
  fi
fi

# --- Pre-build + webapp ---
echo ""
echo "--- Pre-build ---"
make pre-build

echo ""
echo "--- Building webapp ---"
make build-webapp

echo ""
echo "--- Building k2 (universal) ---"
make build-k2-macos
# When single-arch, Tauri expects k2-<target> (e.g. k2-aarch64-apple-darwin)
# but we always build universal. Copy to arch-specific name for sidecar resolution.
if [ "$SINGLE_ARCH" = true ]; then
  cp "desktop/src-tauri/binaries/k2-universal-apple-darwin" \
     "desktop/src-tauri/binaries/k2-$K2_TARGET"
else
  # Tauri universal build compiles each arch separately, each needs its own sidecar
  cp "desktop/src-tauri/binaries/k2-universal-apple-darwin" \
     "desktop/src-tauri/binaries/k2-aarch64-apple-darwin"
  cp "desktop/src-tauri/binaries/k2-universal-apple-darwin" \
     "desktop/src-tauri/binaries/k2-x86_64-apple-darwin"
fi

# --- Tauri build ---
echo ""
if [ "$SINGLE_ARCH" = true ]; then
  echo "--- Building Tauri app ($K2_TARGET) ---"
else
  echo "--- Building Tauri app (universal-apple-darwin) ---"
fi

cd desktop
# Always skip Tauri's built-in notarization — we re-sign after build with the
# hardened runtime, which changes the CDHash and invalidates Tauri's signature.
# After re-signing, we rebuild the .app.tar.gz so it matches the PKG binary,
# and PKG notarization (below) covers both artifacts via the shared CDHash.
_SAVED_APPLE_ID="${APPLE_ID:-}"
_SAVED_APPLE_PASSWORD="${APPLE_PASSWORD:-}"
_SAVED_APPLE_TEAM_ID="${APPLE_TEAM_ID:-}"
_SAVED_APPLE_CERTIFICATE="${APPLE_CERTIFICATE:-}"
_SAVED_APPLE_CERTIFICATE_PASSWORD="${APPLE_CERTIFICATE_PASSWORD:-}"
unset APPLE_ID APPLE_PASSWORD APPLE_TEAM_ID APPLE_CERTIFICATE APPLE_CERTIFICATE_PASSWORD

if [ "$SINGLE_ARCH" = true ]; then
  TAURI_ARGS="--target $K2_TARGET"
else
  TAURI_ARGS="--target universal-apple-darwin"
fi
if [ -n "$EXTRA_FEATURES" ]; then
  TAURI_ARGS="--features $EXTRA_FEATURES $TAURI_ARGS"
fi
yarn tauri build $TAURI_ARGS
cd "$ROOT_DIR"

# Restore Apple credentials for PKG signing + notarization
export APPLE_ID="$_SAVED_APPLE_ID"
export APPLE_PASSWORD="$_SAVED_APPLE_PASSWORD"
export APPLE_TEAM_ID="$_SAVED_APPLE_TEAM_ID"
export APPLE_CERTIFICATE="$_SAVED_APPLE_CERTIFICATE"
export APPLE_CERTIFICATE_PASSWORD="$_SAVED_APPLE_CERTIFICATE_PASSWORD"

# --- Locate .app bundle ---
if [ "$SINGLE_ARCH" = true ]; then
  BUNDLE_DIR="desktop/src-tauri/target/$K2_TARGET/release/bundle/macos"
else
  BUNDLE_DIR="desktop/src-tauri/target/universal-apple-darwin/release/bundle/macos"
fi
APP_PATH="$BUNDLE_DIR/Kaitu.app"

if [ ! -d "$APP_PATH" ]; then
  echo "ERROR: $APP_PATH not found"
  exit 1
fi
echo "Found app bundle: $APP_PATH"

# --- Sign app bundle with hardened runtime ---
echo ""
echo "--- Codesigning app bundle ---"
SIGN_IDENTITY="${APPLE_SIGNING_IDENTITY:-Developer ID Application: ALL NATION CONNECT TECHNOLOGY PTE. LTD. (NJT954Q3RH)}"

# Sign k2 sidecar with hardened runtime
codesign --force --sign "$SIGN_IDENTITY" \
  --options runtime \
  "$APP_PATH/Contents/MacOS/k2"

# Sign main app
codesign --force --sign "$SIGN_IDENTITY" \
  --options runtime \
  "$APP_PATH"

echo "--- Verifying codesign ---"
codesign --verify --deep --strict "$APP_PATH"
echo "codesign verification passed"

# --- Rebuild .app.tar.gz from re-signed .app ---
# Tauri's tar.gz was created BEFORE our codesign --force re-signing, so it contains
# the old CDHash. The PKG (built from re-signed .app) gets notarized, but the old
# tar.gz binary is NOT notarized → Gatekeeper rejects it on macOS 10.15+.
# Fix: re-create tar.gz from the re-signed .app so both share the same CDHash.
echo ""
echo "--- Rebuilding .app.tar.gz from re-signed app ---"
REBUILT_TAR_GZ="$BUNDLE_DIR/Kaitu.app.tar.gz"
tar czf "$REBUILT_TAR_GZ" -C "$BUNDLE_DIR" Kaitu.app
echo "Rebuilt: $REBUILT_TAR_GZ ($(du -h "$REBUILT_TAR_GZ" | cut -f1))"

# Re-sign the tar.gz with Tauri updater key (minisign) if available.
# The old .sig matches the old tar.gz; we need a new .sig for the rebuilt tar.gz.
if [ -n "${TAURI_SIGNING_PRIVATE_KEY:-}" ]; then
  echo "--- Re-signing .app.tar.gz with Tauri updater key ---"
  # Ensure minisign is available
  if ! command -v minisign &>/dev/null; then
    echo "Installing minisign..."
    brew install --quiet minisign 2>/dev/null || {
      echo "ERROR: minisign not available and brew install failed"
      exit 1
    }
  fi
  echo "$TAURI_SIGNING_PRIVATE_KEY" | base64 -d > /tmp/minisign.key
  echo "${TAURI_SIGNING_PRIVATE_KEY_PASSWORD:-}" | minisign -S -s /tmp/minisign.key -m "$REBUILT_TAR_GZ"
  # Convert .minisig to Tauri's base64 .sig format
  base64 < "${REBUILT_TAR_GZ}.minisig" | tr -d '\n' > "$BUNDLE_DIR/Kaitu.app.tar.gz.sig"
  rm -f /tmp/minisign.key "${REBUILT_TAR_GZ}.minisig"
  echo "Updater signature regenerated"
else
  echo "WARN: TAURI_SIGNING_PRIVATE_KEY not set, skipping updater re-sign"
  rm -f "$BUNDLE_DIR/Kaitu.app.tar.gz.sig"
fi

# --- Create .pkg with pkgbuild ---
echo ""
echo "--- Creating .pkg installer ---"
RELEASE_DIR="release/$VERSION"
mkdir -p "$RELEASE_DIR"

PKG_UNSIGNED="$RELEASE_DIR/Kaitu_${VERSION}_universal-unsigned.pkg"
PKG_SIGNED="$RELEASE_DIR/Kaitu_${VERSION}_universal.pkg"

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

# Tauri generates Kaitu.app.tar.gz — rename to Kaitu_{VERSION}_universal.app.tar.gz
APP_TAR_GZ=$(find "$BUNDLE_DIR" -name '*.app.tar.gz' -maxdepth 1 2>/dev/null | head -1)
if [ -n "$APP_TAR_GZ" ]; then
  cp "$APP_TAR_GZ" "$RELEASE_DIR/Kaitu_${VERSION}_universal.app.tar.gz"
  echo "Renamed: $(basename "$APP_TAR_GZ") → Kaitu_${VERSION}_universal.app.tar.gz"
fi

APP_SIG=$(find "$BUNDLE_DIR" -name '*.app.tar.gz.sig' -maxdepth 1 2>/dev/null | head -1)
if [ -n "$APP_SIG" ]; then
  cp "$APP_SIG" "$RELEASE_DIR/Kaitu_${VERSION}_universal.app.tar.gz.sig"
  echo "Renamed: $(basename "$APP_SIG") → Kaitu_${VERSION}_universal.app.tar.gz.sig"
fi

# --- Summary ---
echo ""
echo "=== Build complete ==="
echo "Release artifacts in $RELEASE_DIR/:"
ls -la "$RELEASE_DIR/"
