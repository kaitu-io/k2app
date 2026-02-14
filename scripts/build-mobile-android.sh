#!/usr/bin/env bash
set -euo pipefail

# Android mobile build script for k2app.
# Builds gomobile AAR, syncs Capacitor, and assembles APK/AAB.
# Usage: bash scripts/build-mobile-android.sh [--debug]

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(dirname "$SCRIPT_DIR")"

cd "$ROOT_DIR"

BUILD_TYPE="assembleRelease"
for arg in "$@"; do
  case "$arg" in
    --debug) BUILD_TYPE="assembleDebug" ;;
    *) echo "Unknown argument: $arg"; exit 1 ;;
  esac
done

VERSION=$(node -p "require('./package.json').version")
echo "=== Building Kaitu $VERSION for Android ==="

# --- Pre-build + webapp ---
echo ""
echo "--- Pre-build ---"
make pre-build

echo ""
echo "--- Building webapp ---"
make build-webapp

# --- gomobile bind → AAR ---
echo ""
echo "--- Building k2mobile.aar (gomobile bind) ---"
make mobile-android

# --- Copy AAR into Android project ---
echo ""
echo "--- Copying AAR to Android project ---"
mkdir -p mobile/android/k2-mobile/libs
cp k2/build/k2mobile.aar mobile/android/k2-mobile/libs/
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

APK_DIR="mobile/android/app/build/outputs/apk"
if [ -d "$APK_DIR/release" ]; then
  APK=$(find "$APK_DIR/release" -name '*.apk' | head -1)
  if [ -n "$APK" ]; then
    cp "$APK" "$RELEASE_DIR/Kaitu-${VERSION}.apk"
    echo "Copied APK: $RELEASE_DIR/Kaitu-${VERSION}.apk"
  fi
elif [ -d "$APK_DIR/debug" ]; then
  APK=$(find "$APK_DIR/debug" -name '*.apk' | head -1)
  if [ -n "$APK" ]; then
    cp "$APK" "$RELEASE_DIR/Kaitu-${VERSION}-debug.apk"
    echo "Copied debug APK: $RELEASE_DIR/Kaitu-${VERSION}-debug.apk"
  fi
fi

echo ""
echo "=== Android build complete ==="
