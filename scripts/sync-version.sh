#!/usr/bin/env bash
set -euo pipefail

# Sync version from package.json → Cargo.toml, build.gradle, project.pbxproj.
# Called by `make pre-build` (and thus by all build targets).
#
# Updates:
#   desktop/src-tauri/Cargo.toml              version = "..."
#   mobile/android/app/build.gradle           versionCode + versionName
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

# --- iOS project.pbxproj (all targets, all configs) ---
PBXPROJ="mobile/ios/App/App.xcodeproj/project.pbxproj"
sedi "s/MARKETING_VERSION = .*;/MARKETING_VERSION = ${VERSION};/" "$PBXPROJ"
sedi "s/CURRENT_PROJECT_VERSION = .*;/CURRENT_PROJECT_VERSION = ${VERSION_CODE};/" "$PBXPROJ"

echo "  Cargo.toml, build.gradle, project.pbxproj updated."
