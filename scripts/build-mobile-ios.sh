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

# Derive iOS-compatible version numbers.
# App Store Connect requires MARKETING_VERSION to be pure X.Y.Z (no pre-release suffix).
# CURRENT_PROJECT_VERSION is a monotonically increasing integer build number.
# Scheme: major*10000 + minor*100 + patch for release, + beta_num for pre-release.
# e.g. 0.4.0 → 400, 0.4.0-beta.2 → 402, 1.2.3 → 10203, 1.2.3-beta.5 → 10208
MARKETING_VERSION="${VERSION%%-*}"  # strip everything after first hyphen
# iOS App Store: strip leading "0." — previous app version was 3.0.1,
# so 0.x.y would be rejected as a downgrade. 0.4.0 → 4.0, 0.5.1 → 5.1.
if [[ "$MARKETING_VERSION" == 0.* ]]; then
  MARKETING_VERSION="${MARKETING_VERSION#0.}"
fi
IFS='.' read -r V_MAJOR V_MINOR V_PATCH <<< "${VERSION%%-*}"
BUILD_NUMBER=$(( V_MAJOR * 10000 + V_MINOR * 100 + V_PATCH ))
if [[ "$VERSION" == *"-beta."* ]]; then
  BETA_NUM="${VERSION##*-beta.}"
  BUILD_NUMBER=$(( BUILD_NUMBER + BETA_NUM ))
fi

echo "=== Building Kaitu $VERSION for iOS ==="
echo "  MARKETING_VERSION: $MARKETING_VERSION"
echo "  CURRENT_PROJECT_VERSION: $BUILD_NUMBER"

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
make appext-ios

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
  "MARKETING_VERSION=$MARKETING_VERSION"
  "CURRENT_PROJECT_VERSION=$BUILD_NUMBER"
  "K2_APP_VERSION=$VERSION"
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
