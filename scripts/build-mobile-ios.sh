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
xcodebuild -workspace App.xcworkspace \
  -scheme App \
  -configuration Release \
  -destination 'generic/platform=iOS' \
  -archivePath build/App.xcarchive \
  archive

echo ""
echo "--- Verifying codesign ---"
codesign --verify --deep --strict build/App.xcarchive/Products/Applications/App.app
echo "Codesign verification passed"

cd "$ROOT_DIR"

# --- Export IPA ---
echo ""
echo "--- Exporting IPA ---"
cd mobile/ios/App
xcodebuild -exportArchive \
  -archivePath build/App.xcarchive \
  -exportOptionsPlist ../ExportOptions.plist \
  -exportPath build/ipa

cd "$ROOT_DIR"

RELEASE_DIR="release/${VERSION}"
mkdir -p "$RELEASE_DIR"

if [ -f "mobile/ios/App/build/ipa/App.ipa" ]; then
  cp "mobile/ios/App/build/ipa/App.ipa" "$RELEASE_DIR/Kaitu-${VERSION}.ipa"
  echo "IPA exported: $RELEASE_DIR/Kaitu-${VERSION}.ipa"
fi

echo ""
echo "=== iOS build complete ==="
