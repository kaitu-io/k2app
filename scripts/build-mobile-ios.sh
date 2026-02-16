#!/usr/bin/env bash
set -euo pipefail

# iOS mobile build script for k2app.
# Builds gomobile xcframework, syncs Capacitor, and archives for iOS.
# Usage: bash scripts/build-mobile-ios.sh [--skip-archive]

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(dirname "$SCRIPT_DIR")"

cd "$ROOT_DIR"

SKIP_ARCHIVE=false
for arg in "$@"; do
  case "$arg" in
    --skip-archive) SKIP_ARCHIVE=true ;;
    *) echo "Unknown argument: $arg"; exit 1 ;;
  esac
done

VERSION=$(node -p "require('./package.json').version")
echo "=== Building Kaitu $VERSION for iOS ==="

# --- Pre-build + webapp ---
echo ""
echo "--- Pre-build ---"
make pre-build

echo ""
echo "--- Building webapp ---"
make build-webapp

# --- gomobile bind → xcframework ---
echo ""
echo "--- Building K2Mobile.xcframework (gomobile bind) ---"
make mobile-ios

# --- Copy xcframework into iOS project ---
echo ""
echo "--- Copying xcframework to iOS project ---"
cp -r k2/build/K2Mobile.xcframework mobile/ios/App/
echo "Copied K2Mobile.xcframework"

# --- Capacitor sync ---
echo ""
echo "--- Syncing Capacitor iOS ---"
cd mobile && npx cap sync ios
cd "$ROOT_DIR"

# --- Pod install ---
echo ""
echo "--- Installing CocoaPods ---"
cd mobile/ios/App && pod install --repo-update
cd "$ROOT_DIR"

if [ "$SKIP_ARCHIVE" = true ]; then
  echo ""
  echo "--- Skipping archive (--skip-archive) ---"
  echo "=== Build preparation complete ==="
  exit 0
fi

# --- xcodebuild archive ---
echo ""
echo "--- Archiving iOS app ---"
cd mobile/ios/App

XCODEBUILD_ARGS=(
  -workspace App.xcworkspace
  -scheme App
  -configuration Release
  -destination 'generic/platform=iOS'
  -archivePath build/App.xcarchive
  archive
)

# Enable automatic provisioning if ASC API key is available (CI)
if [ -n "${APP_STORE_CONNECT_KEY_ID:-}" ] && [ -n "${APP_STORE_CONNECT_ISSUER_ID:-}" ]; then
  KEY_PATH="$HOME/private_keys/AuthKey_${APP_STORE_CONNECT_KEY_ID}.p8"
  if [ -f "$KEY_PATH" ]; then
    XCODEBUILD_ARGS+=(
      -allowProvisioningUpdates
      -authenticationKeyPath "$KEY_PATH"
      -authenticationKeyID "$APP_STORE_CONNECT_KEY_ID"
      -authenticationKeyIssuerID "$APP_STORE_CONNECT_ISSUER_ID"
    )
    echo "Using App Store Connect API key for automatic signing"
  fi
fi

xcodebuild "${XCODEBUILD_ARGS[@]}"

echo ""
echo "--- Verifying codesign ---"
codesign --verify --deep --strict build/App.xcarchive/Products/Applications/App.app
echo "Codesign verification passed"

cd "$ROOT_DIR"

# --- Export IPA ---
echo ""
echo "--- Exporting IPA ---"
cd mobile/ios/App

EXPORT_ARGS=(
  -exportArchive
  -archivePath build/App.xcarchive
  -exportOptionsPlist ../ExportOptions.plist
  -exportPath build/ipa
)

if [ -n "${APP_STORE_CONNECT_KEY_ID:-}" ] && [ -n "${APP_STORE_CONNECT_ISSUER_ID:-}" ]; then
  KEY_PATH="$HOME/private_keys/AuthKey_${APP_STORE_CONNECT_KEY_ID}.p8"
  if [ -f "$KEY_PATH" ]; then
    EXPORT_ARGS+=(
      -allowProvisioningUpdates
      -authenticationKeyPath "$KEY_PATH"
      -authenticationKeyID "$APP_STORE_CONNECT_KEY_ID"
      -authenticationKeyIssuerID "$APP_STORE_CONNECT_ISSUER_ID"
    )
  fi
fi

xcodebuild "${EXPORT_ARGS[@]}"

cd "$ROOT_DIR"

RELEASE_DIR="release/${VERSION}"
mkdir -p "$RELEASE_DIR"

if [ -f "mobile/ios/App/build/ipa/App.ipa" ]; then
  cp "mobile/ios/App/build/ipa/App.ipa" "$RELEASE_DIR/Kaitu-${VERSION}.ipa"
  echo "IPA exported: $RELEASE_DIR/Kaitu-${VERSION}.ipa"
fi

echo ""
echo "=== iOS build complete ==="
