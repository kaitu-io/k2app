#!/usr/bin/env bash
set -euo pipefail

# Upload mobile build artifacts to S3 and generate latest.json manifests.
#
# Usage:
#   bash scripts/ci/upload-mobile-s3.sh --android   # Upload APK + android/latest.json
#   bash scripts/ci/upload-mobile-s3.sh --web        # Upload webapp.zip + web/latest.json
#   bash scripts/ci/upload-mobile-s3.sh --ios        # Upload ios/latest.json (metadata only)
#   bash scripts/ci/upload-mobile-s3.sh --all        # All of the above
#
# Required env vars:
#   AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY, AWS_DEFAULT_REGION
#
# S3 layout:
#   s3://d0.all7.cc/kaitu/web/{version}/webapp.zip
#   s3://d0.all7.cc/kaitu/web/latest.json
#   s3://d0.all7.cc/kaitu/android/{version}/Kaitu-{version}.apk
#   s3://d0.all7.cc/kaitu/android/latest.json
#   s3://d0.all7.cc/kaitu/ios/latest.json

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(dirname "$(dirname "$SCRIPT_DIR")")"
cd "$ROOT_DIR"

S3_BUCKET="s3://d0.all7.cc/kaitu"
CDN_BASE="https://d0.all7.cc/kaitu"
VERSION=$(node -p "require('./package.json').version")
RELEASED_AT=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

UPLOAD_ANDROID=false
UPLOAD_WEB=false
UPLOAD_IOS=false

for arg in "$@"; do
  case "$arg" in
    --android) UPLOAD_ANDROID=true ;;
    --web)     UPLOAD_WEB=true ;;
    --ios)     UPLOAD_IOS=true ;;
    --all)     UPLOAD_ANDROID=true; UPLOAD_WEB=true; UPLOAD_IOS=true ;;
    *) echo "Unknown argument: $arg"; exit 1 ;;
  esac
done

if ! $UPLOAD_ANDROID && ! $UPLOAD_WEB && ! $UPLOAD_IOS; then
  echo "Error: specify --android, --web, --ios, or --all"
  exit 1
fi

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

  WEB_HASH=$(shasum -a 256 "$WEB_ZIP" | awk '{print $1}')
  WEB_SIZE=$(stat -f%z "$WEB_ZIP" 2>/dev/null || stat -c%s "$WEB_ZIP" 2>/dev/null)

  aws s3 cp "$WEB_ZIP" "${S3_BUCKET}/web/${VERSION}/webapp.zip"

  cat > /tmp/web-latest.json <<EOF
{
  "version": "${VERSION}",
  "url": "${CDN_BASE}/web/${VERSION}/webapp.zip",
  "hash": "sha256:${WEB_HASH}",
  "size": ${WEB_SIZE},
  "released_at": "${RELEASED_AT}"
}
EOF
  aws s3 cp /tmp/web-latest.json "${S3_BUCKET}/web/latest.json" \
    --content-type "application/json"

  echo "Web OTA uploaded: ${CDN_BASE}/web/${VERSION}/webapp.zip"
  echo "Manifest: ${CDN_BASE}/web/latest.json"
  rm -f "$WEB_ZIP" /tmp/web-latest.json
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
  APK_HASH=$(shasum -a 256 "$APK_PATH" | awk '{print $1}')
  APK_SIZE=$(stat -f%z "$APK_PATH" 2>/dev/null || stat -c%s "$APK_PATH" 2>/dev/null)

  aws s3 cp "$APK_PATH" "${S3_BUCKET}/android/${VERSION}/${APK_FILENAME}"

  cat > /tmp/android-latest.json <<EOF
{
  "version": "${VERSION}",
  "url": "${CDN_BASE}/android/${VERSION}/${APK_FILENAME}",
  "hash": "sha256:${APK_HASH}",
  "size": ${APK_SIZE},
  "released_at": "${RELEASED_AT}",
  "min_android": 26
}
EOF
  aws s3 cp /tmp/android-latest.json "${S3_BUCKET}/android/latest.json" \
    --content-type "application/json"

  echo "APK uploaded: ${CDN_BASE}/android/${VERSION}/${APK_FILENAME}"
  echo "Manifest: ${CDN_BASE}/android/latest.json"
  rm -f /tmp/android-latest.json
fi

# --- iOS (metadata only) ---
if $UPLOAD_IOS; then
  echo ""
  echo "=== Uploading iOS manifest ==="

  cat > /tmp/ios-latest.json <<EOF
{
  "version": "${VERSION}",
  "appstore_url": "https://apps.apple.com/app/id6448744655",
  "released_at": "${RELEASED_AT}"
}
EOF
  aws s3 cp /tmp/ios-latest.json "${S3_BUCKET}/ios/latest.json" \
    --content-type "application/json"

  echo "iOS manifest: ${CDN_BASE}/ios/latest.json"
  rm -f /tmp/ios-latest.json
fi

echo ""
echo "=== Mobile S3 upload complete ==="
