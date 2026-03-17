#!/usr/bin/env bash
set -euo pipefail

# Upload mobile build artifacts to S3 versioned directories.
# Manifests are NOT generated here — use `make publish-mobile` for release.
#
# Usage:
#   bash scripts/ci/upload-mobile-s3.sh --android     # Upload APK
#   bash scripts/ci/upload-mobile-s3.sh --web          # Upload webapp.zip
#   bash scripts/ci/upload-mobile-s3.sh --all          # Both
#
# Required env vars:
#   AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY, AWS_DEFAULT_REGION

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(dirname "$(dirname "$SCRIPT_DIR")")"
cd "$ROOT_DIR"

S3_BUCKET="s3://d0.all7.cc/kaitu"
VERSION=$(node -p "require('./package.json').version")

UPLOAD_ANDROID=false
UPLOAD_WEB=false

for arg in "$@"; do
  case "$arg" in
    --android) UPLOAD_ANDROID=true ;;
    --web)     UPLOAD_WEB=true ;;
    --all)     UPLOAD_ANDROID=true; UPLOAD_WEB=true ;;
    --channel=*) ;; # ignored — kept for CI backwards compat, publish-mobile handles channels
    --ios)     ;; # ignored — iOS has no artifact to upload (App Store only)
    *) echo "Unknown argument: $arg"; exit 1 ;;
  esac
done

if ! $UPLOAD_ANDROID && ! $UPLOAD_WEB; then
  echo "Error: specify --android, --web, or --all"
  exit 1
fi

echo "Uploading artifacts for v${VERSION}..."

# --- Web OTA ---
if $UPLOAD_WEB; then
  echo ""
  echo "=== Uploading Web OTA bundle ==="

  WEBAPP_DIST="webapp/dist"
  if [ ! -d "$WEBAPP_DIST" ]; then
    echo "Error: $WEBAPP_DIST not found. Run 'make build-webapp' first."
    exit 1
  fi

  WEB_ZIP="/tmp/webapp-${VERSION}.zip"
  (cd "$WEBAPP_DIST" && zip -r "$WEB_ZIP" .)

  aws s3 cp "$WEB_ZIP" "${S3_BUCKET}/web/${VERSION}/webapp.zip"
  echo "Uploaded: web/${VERSION}/webapp.zip"
  rm -f "$WEB_ZIP"
fi

# --- Android APK ---
if $UPLOAD_ANDROID; then
  echo ""
  echo "=== Uploading Android APK ==="

  APK_PATH=$(find release/"${VERSION}" -name "Kaitu-*.apk" -type f 2>/dev/null | head -1)
  if [ -z "$APK_PATH" ]; then
    echo "Error: No APK found in release/${VERSION}/"
    exit 1
  fi

  APK_FILENAME=$(basename "$APK_PATH")
  aws s3 cp "$APK_PATH" "${S3_BUCKET}/android/${VERSION}/${APK_FILENAME}"
  echo "Uploaded: android/${VERSION}/${APK_FILENAME}"
fi

echo ""
echo "=== Artifact upload complete (no manifests — run 'make publish-mobile' to release) ==="
