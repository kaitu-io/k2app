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

# BRAND is the cross-layer contract name (Makefile: BRAND, K2_BRAND export).
# This is a CI entry point invoked directly (not via `make build-ios`), so a
# bare `make build-webapp` here would silently fall back to kaitu — the same
# bug that hit desktop's build-macos.sh (fixed in dd2d8608). Every `make`
# invocation below must carry BRAND=$BRAND explicitly.
BRAND="${BRAND:-kaitu}"
case "$BRAND" in
  kaitu|overleap) ;;
  *) echo "::error::BRAND must be 'kaitu' or 'overleap', got '$BRAND'" >&2; exit 1 ;;
esac
BRAND_PRODUCT=$([ "$BRAND" = "overleap" ] && echo "Overleap" || echo "Kaitu")
# mobile/capacitor.config.ts reads process.env.K2_BRAND directly (same
# contract as Makefile's `export K2_BRAND`). `make` exports it to its own
# recipes automatically, but this script calls `npx cap sync` itself — without
# this export, cap sync silently falls back to kaitu's appId/appName
# regardless of BRAND above (confirmed on the Android side: a stale
# overleap-flavor APK build was found shipping "appId":"io.kaitu" in
# assets/capacitor.config.json — this export is the fix, applies equally here).
export K2_BRAND="$BRAND"

VERSION=$(node -p "require('./package.json').version")

# Derive iOS-compatible MARKETING_VERSION + CURRENT_PROJECT_VERSION.
#
# Marketing version: predecessor "ANC" shipped at 3.0.1; current Kaitu series
# started on App Store at 4.0. Internal 0.x.y is rewritten to user-visible 4.x.y
# (0.4.4 → 4.4.4, 0.5.1 → 4.5.1) so Apple's downgrade check sees a higher version
# than the legacy 3.x.
#
# Bundle version layout (decimal):
#   400000 + MINOR*10000 + PATCH*100 + SLOT
#   SLOT = beta_num (1..98) for `-beta.N` pre-release, or 99 for final release.
# Guarantees beta.1 < beta.2 < … < release < next.beta.1 monotonically across
# minor/patch bumps, and stays above the 4xx range already burned on TestFlight
# (ASC was at 406 when we switched schemes — see 2026-05-18 incident).
#
# Constraints (script aborts if violated — forces rework when we outgrow them):
#   V_MAJOR == 0, MINOR ∈ [0, 99], PATCH ∈ [0, 99], beta_num ∈ [1, 98].
IFS='.' read -r V_MAJOR V_MINOR V_PATCH <<< "${VERSION%%-*}"
if [[ "$V_MAJOR" != "0" ]]; then
  echo "::error::Build-number scheme only supports 0.x.y (got $VERSION)." >&2
  echo "When bumping past 0.x.y, redesign scripts/build-mobile-ios.sh:" \
       "the 0→4 marketing remap and the 400000 bundle base both need rework." >&2
  exit 1
fi
if (( V_MINOR > 99 || V_PATCH > 99 )); then
  echo "::error::MINOR/PATCH > 99 not supported by current scheme (got $VERSION)." >&2
  exit 1
fi

MARKETING_VERSION="4.${V_MINOR}.${V_PATCH}"

SLOT=99
if [[ "$VERSION" == *"-beta."* ]]; then
  BETA_NUM="${VERSION##*-beta.}"
  if [[ ! "$BETA_NUM" =~ ^[0-9]+$ ]] || (( BETA_NUM < 1 || BETA_NUM > 98 )); then
    echo "::error::beta number must be in [1, 98], got '$BETA_NUM' (from $VERSION)." >&2
    exit 1
  fi
  SLOT=$BETA_NUM
fi

BUILD_NUMBER=$(( 400000 + V_MINOR * 10000 + V_PATCH * 100 + SLOT ))

echo "=== Building $BRAND_PRODUCT $VERSION for iOS ==="
echo "  BRAND: $BRAND"
echo "  MARKETING_VERSION: $MARKETING_VERSION"
echo "  CURRENT_PROJECT_VERSION: $BUILD_NUMBER"

# --- Pre-build + webapp ---
echo ""
echo "--- Pre-build ---"
make pre-build BRAND="$BRAND"

echo ""
echo "--- Building webapp ---"
make build-webapp BRAND="$BRAND"

# --- gomobile bind → xcframework ---
echo ""
echo "--- Building K2Mobile.xcframework (gomobile bind) ---"
make appext-ios BRAND="$BRAND"

# --- Copy xcframework into iOS project ---
echo ""
echo "--- Copying xcframework to iOS project ---"
cp -r k2/build/K2Mobile.xcframework mobile/ios/App/
echo "Copied K2Mobile.xcframework"

# --- Apply brand (xcconfig + display names + icon) ---
echo ""
echo "--- Applying iOS brand ($BRAND) ---"
bash scripts/apply-ios-brand.sh "$BRAND"

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
  cp "mobile/ios/App/build/ipa/App.ipa" "$RELEASE_DIR/${BRAND_PRODUCT}-${VERSION}.ipa"
  echo "IPA exported: $RELEASE_DIR/${BRAND_PRODUCT}-${VERSION}.ipa"
fi

echo ""
echo "=== iOS build complete ==="
