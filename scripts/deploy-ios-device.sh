#!/usr/bin/env bash
# deploy-ios-device.sh — build + install + launch the iOS app on a physical
# device via devicectl (CoreDevice).
#
# Why this exists: Capacitor's `cap run ios` deploys through the legacy
# xctrace/native-run path. On iOS 17+ (and especially iOS 26), real devices
# speak only the CoreDevice tunnel and show up as "Offline" to xctrace, so
# `cap run ios --target <udid>` is rejected with "Invalid target ID" and only
# simulators remain selectable. `xcrun devicectl` reaches CoreDevice devices
# directly, so this script is the `make dev-ios` deploy step for real hardware.
# Simulators still go through `cap run` (the Makefile branches on IOS_DEVICE).
#
# Usage: scripts/deploy-ios-device.sh <udid>
#   <udid> — hardware UDID (e.g. 00008120-0016155034D1A01E). Both
#            `xcodebuild -destination id=` and `devicectl --device` accept it.
set -euo pipefail

UDID="${1:-}"
if [ -z "$UDID" ]; then
  echo "deploy-ios-device.sh: no device UDID given." >&2
  echo "  Plug in / pair an iPhone, or pass one explicitly." >&2
  echo "  Connected devices:" >&2
  xcrun devicectl list devices >&2 || true
  exit 1
fi

APP_DIR="$(cd "$(dirname "$0")/../mobile/ios/App" && pwd)"
WORKSPACE="$APP_DIR/App.xcworkspace"
SCHEME="App"
CONFIG="Debug"
DEST="id=$UDID"

echo "==> Building $SCHEME for device $UDID (automatic signing) ..."
xcodebuild \
  -workspace "$WORKSPACE" \
  -scheme "$SCHEME" \
  -configuration "$CONFIG" \
  -destination "$DEST" \
  -allowProvisioningUpdates \
  build

# Resolve the freshly built .app path from the same build settings xcodebuild
# used (default DerivedData — cached across runs, no clean rebuild).
BUILD_DIR="$(xcodebuild \
  -workspace "$WORKSPACE" \
  -scheme "$SCHEME" \
  -configuration "$CONFIG" \
  -destination "$DEST" \
  -showBuildSettings 2>/dev/null \
  | awk -F' = ' '/ CONFIGURATION_BUILD_DIR =/{print $2; exit}')"
APP_PATH="$BUILD_DIR/App.app"
if [ ! -d "$APP_PATH" ]; then
  echo "deploy-ios-device.sh: built app not found at $APP_PATH" >&2
  exit 1
fi
BUNDLE_ID="$(/usr/libexec/PlistBuddy -c 'Print :CFBundleIdentifier' "$APP_PATH/Info.plist")"

echo "==> Installing $BUNDLE_ID via devicectl ..."
xcrun devicectl device install app --device "$UDID" "$APP_PATH"

# Launch is best-effort: devicectl can't open an app on a locked screen
# (FBSOpenApplicationErrorDomain "Locked"). The install above is the part that
# matters — don't fail the deploy if auto-launch is denied; tell the user to
# unlock + tap instead.
echo "==> Launching $BUNDLE_ID ..."
if xcrun devicectl device process launch --device "$UDID" "$BUNDLE_ID"; then
  echo "==> Done — $BUNDLE_ID is running on $UDID."
else
  echo "==> $BUNDLE_ID is INSTALLED on $UDID, but auto-launch was denied" >&2
  echo "    (usually a locked iPhone). Unlock the device and tap the app icon," >&2
  echo "    or rerun: xcrun devicectl device process launch --device $UDID $BUNDLE_ID" >&2
fi
