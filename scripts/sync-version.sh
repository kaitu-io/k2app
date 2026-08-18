#!/usr/bin/env bash
set -euo pipefail

# Sync version from package.json → Cargo.toml, build.gradle, project.pbxproj.
# Called by `make pre-build` (and thus by all build targets).
#
# Updates:
#   desktop/src-tauri/Cargo.toml              version = "..."
#   mobile/android/app/build.gradle           versionCode + versionName
#   mobile/plugins/k2-plugin/ios/.../K2Helpers.swift  let k2AppVersion
#   mobile/ios/App/App.xcodeproj/project.pbxproj  MARKETING_VERSION + CURRENT_PROJECT_VERSION

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(dirname "$SCRIPT_DIR")"
cd "$ROOT_DIR"

VERSION=$(node -p "require('./package.json').version")

# Extract major.minor.patch (strip pre-release suffix)
BASE_VERSION=$(echo "$VERSION" | sed 's/-.*//')
MAJOR=$(echo "$BASE_VERSION" | cut -d. -f1)
MINOR=$(echo "$BASE_VERSION" | cut -d. -f2)
PATCH=$(echo "$BASE_VERSION" | cut -d. -f3)
VERSION_CODE=$((MAJOR * 10000 + MINOR * 100 + PATCH))

echo "Syncing version: ${VERSION} (code: ${VERSION_CODE})"

# Cross-platform sed -i (macOS requires '' suffix, Linux/Windows Git Bash does not)
sedi() {
  if [[ "$OSTYPE" == "darwin"* ]]; then
    sed -i '' "$@"
  else
    sed -i "$@"
  fi
}

# --- Cargo.toml ---
sedi "s/^version = \".*\"/version = \"${VERSION}\"/" desktop/src-tauri/Cargo.toml

# --- Android build.gradle ---
sedi "s/versionCode [0-9]*/versionCode ${VERSION_CODE}/" mobile/android/app/build.gradle
sedi "s/versionName \".*\"/versionName \"${VERSION}\"/" mobile/android/app/build.gradle

# --- iOS plugin compile-time version ---
# This one is not cosmetic: it is the native version the web-OTA min_native
# gate compares a manifest against (K2Plugin.appVersion).
sedi "s/^let k2AppVersion = \".*\"/let k2AppVersion = \"${VERSION}\"/" \
  mobile/plugins/k2-plugin/ios/Plugin/K2Helpers.swift

# --- iOS project.pbxproj (all targets, all configs) ---
# These two are DERIVED, not the raw version: iOS remaps 0.x.y -> 4.x.y and packs
# the build number into bands (App Store Connect permanently burns every accepted
# number). Ask the production derivation for them rather than restating it here —
# a second copy of the formula goes on producing plausible-looking numbers after
# the real one changes. --print-build-number exits before touching the toolchain,
# so this does not re-enter `make pre-build`.
read -r IOS_MARKETING IOS_BUILD < <(bash scripts/build-mobile-ios.sh --print-build-number)
PBXPROJ="mobile/ios/App/App.xcodeproj/project.pbxproj"
sedi "s/MARKETING_VERSION = .*;/MARKETING_VERSION = ${IOS_MARKETING};/" "$PBXPROJ"
sedi "s/CURRENT_PROJECT_VERSION = .*;/CURRENT_PROJECT_VERSION = ${IOS_BUILD};/" "$PBXPROJ"

echo "  Cargo.toml, build.gradle, K2Helpers.swift, project.pbxproj updated."
