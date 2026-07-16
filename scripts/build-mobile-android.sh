#!/usr/bin/env bash
set -euo pipefail

# Android mobile build script for k2app.
# Builds gomobile AAR, syncs Capacitor, and assembles APK/AAB.
# Usage: bash scripts/build-mobile-android.sh [--debug]

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(dirname "$SCRIPT_DIR")"

cd "$ROOT_DIR"

# BRAND is the cross-layer contract name (Makefile: BRAND, K2_BRAND export).
# This is a CI entry point invoked directly (not via `make build-android`), so
# a bare `make build-webapp` here would silently fall back to kaitu — the
# same bug that hit desktop's build-macos.sh (fixed in dd2d8608). Every
# `make` invocation below must carry BRAND=$BRAND explicitly.
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
# regardless of BRAND above (confirmed: a stale mobile/android/app/build
# overleap-flavor APK was found shipping "appId":"io.kaitu" in
# assets/capacitor.config.json — this export is the fix).
export K2_BRAND="$BRAND"

DEBUG=false
for arg in "$@"; do
  case "$arg" in
    --debug) DEBUG=true ;;
    *) echo "Unknown argument: $arg"; exit 1 ;;
  esac
done
# Per-flavor task, not the bare aggregate `assembleRelease`/`assembleDebug`
# (which would build BOTH flavors — mobile/android/app/build.gradle declares
# a `brand` flavor dimension with kaitu/overleap flavors, see productFlavors).
# Flavor name in the Gradle task is the capitalized brand: kaitu -> Kaitu.
if [ "$DEBUG" = true ]; then
  BUILD_TYPE="assemble${BRAND_PRODUCT}Debug"
else
  BUILD_TYPE="assemble${BRAND_PRODUCT}Release"
fi

VERSION=$(node -p "require('./package.json').version")
echo "=== Building $BRAND_PRODUCT $VERSION for Android ==="
echo "  BRAND: $BRAND"
echo "  Gradle task: $BUILD_TYPE"

# --- Pre-build + webapp ---
echo ""
echo "--- Pre-build ---"
make pre-build BRAND="$BRAND"

echo ""
echo "--- Building webapp ---"
make build-webapp BRAND="$BRAND"

# --- gomobile bind → AAR ---
echo ""
echo "--- Building k2mobile.aar (gomobile bind) ---"
make appext-android BRAND="$BRAND"

# --- Copy AAR into Android project ---
echo ""
echo "--- Copying AAR to Android project ---"
mkdir -p mobile/android/app/libs
cp k2/build/k2mobile.aar mobile/android/app/libs/
echo "Copied k2mobile.aar"

# --- Capacitor sync ---
echo ""
echo "--- Syncing Capacitor Android ---"
cd mobile && npx cap sync android
cd "$ROOT_DIR"

# --- Gradle build ---
echo ""
echo "--- Building Android ($BUILD_TYPE) ---"
cd mobile/android
./gradlew "$BUILD_TYPE"
cd "$ROOT_DIR"

# --- Collect artifacts ---
RELEASE_DIR="release/${VERSION}"
mkdir -p "$RELEASE_DIR"

# Flavored output: outputs/apk/<flavor>/<buildType>/ (flavor = lowercase
# brand name — matches productFlavors in mobile/android/app/build.gradle).
APK_DIR="mobile/android/app/build/outputs/apk/${BRAND}"
if [ -d "$APK_DIR/release" ]; then
  APK=$(find "$APK_DIR/release" -maxdepth 1 -name '*.apk' | head -1)
  if [ -n "$APK" ]; then
    cp "$APK" "$RELEASE_DIR/${BRAND_PRODUCT}-${VERSION}.apk"
    echo "Copied APK: $RELEASE_DIR/${BRAND_PRODUCT}-${VERSION}.apk"
  fi
elif [ -d "$APK_DIR/debug" ]; then
  APK=$(find "$APK_DIR/debug" -maxdepth 1 -name '*.apk' | head -1)
  if [ -n "$APK" ]; then
    cp "$APK" "$RELEASE_DIR/${BRAND_PRODUCT}-${VERSION}-debug.apk"
    echo "Copied debug APK: $RELEASE_DIR/${BRAND_PRODUCT}-${VERSION}-debug.apk"
  fi
fi

echo ""
echo "=== Android build complete ==="
