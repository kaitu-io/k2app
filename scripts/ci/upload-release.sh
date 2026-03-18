#!/usr/bin/env bash
set -euo pipefail

# Upload release artifacts to S3 and invalidate both CDN distributions.
# Called by build scripts (local) and CI workflows.
#
# Usage:
#   bash scripts/ci/upload-release.sh --desktop          # Upload release/{VER}/ → desktop/{VER}/
#   bash scripts/ci/upload-release.sh --android           # Upload release/{VER}/ → android/{VER}/
#   bash scripts/ci/upload-release.sh --web               # Zip webapp/dist → web/{VER}/webapp.zip
#   bash scripts/ci/upload-release.sh --desktop --skip-cdn  # Upload only, no CDN invalidation
#
# Desktop uploads everything in release/{VERSION}/ (pkg, tar.gz, sig, exe).
# Android uploads Kaitu-{VERSION}.apk from release/{VERSION}/.
# Web zips webapp/dist/ and uploads as webapp.zip.
#
# Skips gracefully if AWS credentials are not configured (local dev without AWS).
#
# Environment:
#   AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY — required (skips if missing)
#   AWS_DEFAULT_REGION — defaults to ap-northeast-1

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(dirname "$(dirname "$SCRIPT_DIR")")"
cd "$ROOT_DIR"

export AWS_DEFAULT_REGION="${AWS_DEFAULT_REGION:-ap-northeast-1}"

S3_BUCKET="s3://d0.all7.cc/kaitu"
CDN_ID_D0="E3W144CRNT652P"
CDN_ID_DL="E34P52R7B93FSC"
VERSION=$(node -p "require('./package.json').version")

PLATFORM=""
SKIP_CDN=false

for arg in "$@"; do
  case "$arg" in
    --desktop)  PLATFORM="desktop" ;;
    --android)  PLATFORM="android" ;;
    --web)      PLATFORM="web" ;;
    --skip-cdn) SKIP_CDN=true ;;
    *) echo "Unknown argument: $arg" >&2; exit 1 ;;
  esac
done

if [ -z "$PLATFORM" ]; then
  echo "Usage: $0 --desktop|--android|--web [--skip-cdn]" >&2
  exit 1
fi

# Skip if AWS credentials not configured (local dev)
if ! aws sts get-caller-identity &>/dev/null; then
  echo "⚠ AWS credentials not configured — skipping S3 upload."
  echo "  To upload: export AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY, then re-run."
  exit 0
fi

S3_DEST="${S3_BUCKET}/${PLATFORM}/${VERSION}"
INVALIDATION_PATH="/kaitu/${PLATFORM}/${VERSION}/*"

echo "=== Uploading ${PLATFORM} v${VERSION} to S3 ==="

case "$PLATFORM" in
  desktop)
    if [ ! -d "release/${VERSION}" ]; then
      echo "ERROR: release/${VERSION}/ not found. Run build first." >&2; exit 1
    fi
    aws s3 cp "release/${VERSION}/" "${S3_DEST}/" --recursive
    echo "Uploaded: desktop/${VERSION}/"
    ;;
  android)
    APK="release/${VERSION}/Kaitu-${VERSION}.apk"
    if [ ! -f "$APK" ]; then
      echo "ERROR: $APK not found. Run 'make build-android' first." >&2; exit 1
    fi
    aws s3 cp "$APK" "${S3_DEST}/Kaitu-${VERSION}.apk"
    echo "Uploaded: android/${VERSION}/Kaitu-${VERSION}.apk"
    ;;
  web)
    if [ ! -d "webapp/dist" ]; then
      echo "ERROR: webapp/dist not found. Run 'make build-webapp' first." >&2; exit 1
    fi
    TMPZIP=$(mktemp /tmp/webapp-XXXXXX.zip)
    (cd webapp/dist && zip -qr "$TMPZIP" .)
    aws s3 cp "$TMPZIP" "${S3_DEST}/webapp.zip"
    rm -f "$TMPZIP"
    echo "Uploaded: web/${VERSION}/webapp.zip"
    ;;
esac

# --- CDN invalidation (both distributions) ---
if [ "$SKIP_CDN" = false ]; then
  echo ""
  echo "Invalidating CDN caches..."
  for DIST_ID in "$CDN_ID_D0" "$CDN_ID_DL"; do
    aws cloudfront create-invalidation \
      --distribution-id "$DIST_ID" \
      --paths "$INVALIDATION_PATH" \
      --no-cli-pager --output text > /dev/null
  done
  echo "CDN invalidated: d0.all7.cc + dl.kaitu.io (${INVALIDATION_PATH})"
fi

echo "=== Upload complete ==="
