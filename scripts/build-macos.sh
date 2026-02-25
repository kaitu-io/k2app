#!/usr/bin/env bash
set -euo pipefail

# macOS build script for k2app.
# Builds universal binary, creates .pkg installer, signs, and notarizes.
# Usage: bash scripts/build-macos.sh [--skip-notarization] [--single-arch] [--features=FEATURE] [--ne-mode]

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(dirname "$SCRIPT_DIR")"

cd "$ROOT_DIR"

# --- Argument parsing ---
SKIP_NOTARIZATION=false
SINGLE_ARCH=false
EXTRA_FEATURES=""
NE_MODE=false
for arg in "$@"; do
  case "$arg" in
    --skip-notarization) SKIP_NOTARIZATION=true ;;
    --single-arch) SINGLE_ARCH=true ;;
    --features=*) EXTRA_FEATURES="${arg#--features=}" ;;
    --ne-mode) NE_MODE=true ;;
    *) echo "Unknown argument: $arg"; exit 1 ;;
  esac
done

# Append ne-mode feature when building sysext variant
if [ "$NE_MODE" = true ]; then
  if [ -n "$EXTRA_FEATURES" ]; then
    EXTRA_FEATURES="$EXTRA_FEATURES,ne-mode"
  else
    EXTRA_FEATURES="ne-mode"
  fi
fi

# --- Read version ---
VERSION=$(node -p "require('./package.json').version")

if [ "$NE_MODE" = true ]; then
  if [ "$SINGLE_ARCH" = true ]; then
    echo "=== Building Kaitu $VERSION for macOS (NE/sysext mode, single-arch) ==="
  else
    echo "=== Building Kaitu $VERSION for macOS (NE/sysext mode, universal) ==="
  fi
else
  if [ "$SINGLE_ARCH" = true ]; then
    echo "=== Building Kaitu $VERSION for macOS (daemon mode, single-arch) ==="
  else
    echo "=== Building Kaitu $VERSION for macOS (daemon mode, universal) ==="
  fi
fi

# Determine native architecture for single-arch builds
if [ "$SINGLE_ARCH" = true ]; then
  NATIVE_ARCH=$(uname -m)
  if [ "$NATIVE_ARCH" = "arm64" ]; then
    K2_TARGET="aarch64-apple-darwin"
    K2_GOARCH="arm64"
    NE_ARCH="arm64"
  else
    K2_TARGET="x86_64-apple-darwin"
    K2_GOARCH="amd64"
    NE_ARCH="x86_64"
  fi
fi

# --- Pre-build + webapp ---
echo ""
echo "--- Pre-build ---"
make pre-build

echo ""
echo "--- Building webapp ---"
make build-webapp

if [ "$NE_MODE" != true ]; then
  # --- Build k2 (daemon mode only) ---
  if [ "$SINGLE_ARCH" = true ]; then
    echo ""
    echo "--- Building k2 ($K2_TARGET) ---"
    GOARCH=$K2_GOARCH GOOS=darwin make build-k2 TARGET=$K2_TARGET
  else
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
  fi
fi

if [ "$NE_MODE" = true ]; then
  # --- Build gomobile macOS xcframework (NE mode only) ---
  echo ""
  echo "--- Building gomobile macOS xcframework ---"
  make mobile-macos

  # --- Build libk2_ne_helper.a (NE mode only) ---
  echo ""
  echo "--- Building NE helper static library ---"
  cd "$ROOT_DIR/desktop/src-tauri/ne_helper"
  if [ "$SINGLE_ARCH" = true ]; then
    bash build.sh --arch "$NE_ARCH"
  else
    bash build.sh --arch universal
  fi
  cd "$ROOT_DIR"

  # Set env var for Rust build.rs to find the library
  export NE_HELPER_LIB_DIR="$ROOT_DIR/desktop/src-tauri/ne_helper"
fi

# --- Tauri build ---
echo ""
if [ "$SINGLE_ARCH" = true ]; then
  echo "--- Building Tauri app ($K2_TARGET) ---"
else
  echo "--- Building Tauri app (universal-apple-darwin) ---"
fi

cd desktop
# Always skip Tauri's built-in notarization — we re-sign after build (sysext
# injection in NE mode, or simple hardened runtime in daemon mode), which
# invalidates any Tauri-applied notarization ticket. PKG notarization
# (below) covers the final signed bundle.
_SAVED_APPLE_ID="${APPLE_ID:-}"
_SAVED_APPLE_PASSWORD="${APPLE_PASSWORD:-}"
_SAVED_APPLE_TEAM_ID="${APPLE_TEAM_ID:-}"
_SAVED_APPLE_CERTIFICATE="${APPLE_CERTIFICATE:-}"
_SAVED_APPLE_CERTIFICATE_PASSWORD="${APPLE_CERTIFICATE_PASSWORD:-}"
unset APPLE_ID APPLE_PASSWORD APPLE_TEAM_ID APPLE_CERTIFICATE APPLE_CERTIFICATE_PASSWORD

TAURI_ARGS="--config src-tauri/tauri.bundle.conf.json"
if [ "$SINGLE_ARCH" = true ]; then
  TAURI_ARGS="--target $K2_TARGET $TAURI_ARGS"
else
  TAURI_ARGS="--target universal-apple-darwin $TAURI_ARGS"
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

if [ "$NE_MODE" = true ]; then
  # --- Build and inject System Extension (NE mode only) ---
  echo ""
  echo "--- Building KaituTunnel.systemextension ---"
  SYSEXT_BUILD_DIR=$(mktemp -d /tmp/kaitu-sysext.XXXXXX)
  SYSEXT_NAME="io.kaitu.desktop.tunnel.systemextension"
  SYSEXT_DIR="$SYSEXT_BUILD_DIR/$SYSEXT_NAME/Contents/MacOS"
  mkdir -p "$SYSEXT_DIR"

  XCFW_PATH="$ROOT_DIR/k2/build/K2MobileMacOS.xcframework"
  MACOS_SDK_PATH=$(xcrun --show-sdk-path --sdk macosx)

  # Resolve the framework slice directory inside the xcframework
  # xcframework structure: K2MobileMacOS.xcframework/macos-arm64_x86_64/K2MobileMacOS.framework
  XCFW_SLICE_DIR=$(find "$XCFW_PATH" -name "K2MobileMacOS.framework" -maxdepth 2 -type d | head -1)
  XCFW_SLICE_PARENT=$(dirname "$XCFW_SLICE_DIR")

  # Compile PacketTunnelProvider.swift → KaituTunnel executable
  SYSEXT_SWIFT_SRC="$ROOT_DIR/desktop/src-tauri/KaituTunnel/PacketTunnelProvider.swift"

  compile_sysext() {
    local target="$1"
    local output="$2"
    swiftc \
      -emit-executable \
      -module-name KaituTunnel \
      -sdk "$MACOS_SDK_PATH" \
      -target "$target" \
      -F "$XCFW_SLICE_PARENT" \
      -framework K2MobileMacOS \
      "$SYSEXT_SWIFT_SRC" \
      -o "$output"
  }

  if [ "$SINGLE_ARCH" = true ]; then
    compile_sysext "${NE_ARCH}-apple-macos12" "$SYSEXT_DIR/KaituTunnel"
  else
    # Universal: compile both architectures and merge with lipo
    compile_sysext "arm64-apple-macos12" "$SYSEXT_DIR/KaituTunnel-arm64"
    compile_sysext "x86_64-apple-macos12" "$SYSEXT_DIR/KaituTunnel-x86_64"
    lipo -create "$SYSEXT_DIR/KaituTunnel-arm64" "$SYSEXT_DIR/KaituTunnel-x86_64" \
      -output "$SYSEXT_DIR/KaituTunnel"
    rm -f "$SYSEXT_DIR/KaituTunnel-arm64" "$SYSEXT_DIR/KaituTunnel-x86_64"
  fi

  # Copy Info.plist into systemextension bundle
  cp "$ROOT_DIR/desktop/src-tauri/KaituTunnel/Info.plist" \
     "$SYSEXT_BUILD_DIR/$SYSEXT_NAME/Contents/"

  # Install systemextension into .app bundle
  SYSEXT_DEST="$APP_PATH/Contents/Library/SystemExtensions"
  mkdir -p "$SYSEXT_DEST"
  cp -R "$SYSEXT_BUILD_DIR/$SYSEXT_NAME" "$SYSEXT_DEST/"

  # --- Embed provisioning profiles ---
  echo ""
  echo "--- Embedding provisioning profiles ---"
  PROFILE_DIR="${PROFILE_DIR:-$ROOT_DIR/desktop/src-tauri/profiles}"

  APP_PROFILE="$PROFILE_DIR/Kaitu_Desktop_Developer_ID.provisionprofile"
  TUNNEL_PROFILE="$PROFILE_DIR/Kaitu_Tunnel_Developer_ID.provisionprofile"

  if [ -f "$APP_PROFILE" ]; then
    cp "$APP_PROFILE" "$APP_PATH/Contents/embedded.provisionprofile"
    echo "Embedded app profile: $APP_PROFILE"
  else
    echo "WARNING: App profile not found at $APP_PROFILE — skipping"
  fi

  if [ -f "$TUNNEL_PROFILE" ]; then
    cp "$TUNNEL_PROFILE" "$SYSEXT_DEST/$SYSEXT_NAME/Contents/embedded.provisionprofile"
    echo "Embedded tunnel profile: $TUNNEL_PROFILE"
  else
    echo "WARNING: Tunnel profile not found at $TUNNEL_PROFILE — skipping"
  fi

  # --- Add NSSystemExtensionUsageDescription to main app Info.plist ---
  /usr/libexec/PlistBuddy -c "Add :NSSystemExtensionUsageDescription string 'Kaitu uses a system extension to provide VPN functionality.'" \
    "$APP_PATH/Contents/Info.plist" 2>/dev/null || \
  /usr/libexec/PlistBuddy -c "Set :NSSystemExtensionUsageDescription 'Kaitu uses a system extension to provide VPN functionality.'" \
    "$APP_PATH/Contents/Info.plist"

  # Determine signing identity (use env var or well-known default)
  SIGN_IDENTITY="${APPLE_SIGNING_IDENTITY:-Developer ID Application: ALL NATION CONNECT TECHNOLOGY PTE. LTD. (NJT954Q3RH)}"

  # Sign each component individually (NOT --deep) to avoid applying NE restricted
  # entitlement to the k2 sidecar binary. AMFI validates each binary independently
  # and rejects binaries that have restricted entitlements without a matching profile.

  echo "--- Codesigning k2 sidecar (hardened runtime only, no NE entitlements) ---"
  codesign --force --sign "$SIGN_IDENTITY" \
    --options runtime \
    "$APP_PATH/Contents/MacOS/k2"

  echo "--- Codesigning KaituTunnel.systemextension ---"
  codesign --force --sign "$SIGN_IDENTITY" \
    --entitlements "$ROOT_DIR/desktop/src-tauri/KaituTunnel/KaituTunnel.entitlements" \
    --options runtime \
    "$SYSEXT_DEST/$SYSEXT_NAME"

  echo "--- Codesigning main app bundle (with NE entitlements) ---"
  codesign --force --sign "$SIGN_IDENTITY" \
    --entitlements "$ROOT_DIR/desktop/src-tauri/entitlements.plist" \
    --options runtime \
    "$APP_PATH"

  rm -rf "$SYSEXT_BUILD_DIR"
  echo "KaituTunnel.systemextension injected into $SYSEXT_DEST/"

  # --- Verify codesign ---
  echo ""
  echo "--- Verifying codesign ---"
  codesign --verify --deep --strict "$APP_PATH"
  echo "codesign verification passed"

else
  # --- Daemon mode: sign app bundle without sysext injection ---
  echo ""
  echo "--- Codesigning app bundle (daemon mode) ---"
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
fi

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
