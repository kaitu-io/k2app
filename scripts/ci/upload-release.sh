#!/usr/bin/env bash
set -euo pipefail

# Upload release artifacts to S3 and invalidate both CDN distributions.
# Called by build scripts (local) and CI workflows.
#
# Usage:
#   bash scripts/ci/upload-release.sh --windows            # Upload Windows exe + sig
#   bash scripts/ci/upload-release.sh --macos              # Upload macOS pkg + app.tar.gz + sig
#   bash scripts/ci/upload-release.sh --linux              # Upload Linux tar.gz + sig + k2 binary
#   bash scripts/ci/upload-release.sh --android            # Upload Android APK
#   --web option REMOVED — Web OTA disabled due to native/webapp version mismatch risk (2026-03-22)
#   bash scripts/ci/upload-release.sh --windows --skip-cdn # Upload only, no CDN invalidation
#
# Each platform flag uploads ONLY its own artifacts, preventing cross-platform contamination.
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
    --windows)  PLATFORM="windows" ;;
    --macos)    PLATFORM="macos" ;;
    --linux)    PLATFORM="linux" ;;
    --desktop)  echo "ERROR: --desktop is deprecated. Use --windows, --macos, or --linux." >&2; exit 1 ;;
    --android)  PLATFORM="android" ;;
    --web)      echo "ERROR: --web is disabled. Web OTA removed due to native/webapp version mismatch risk (2026-03-22)." >&2; exit 1 ;;
    --skip-cdn) SKIP_CDN=true ;;
    *) echo "Unknown argument: $arg" >&2; exit 1 ;;
  esac
done

if [ -z "$PLATFORM" ]; then
  echo "Usage: $0 --windows|--macos|--linux|--android|--web [--skip-cdn]" >&2
  exit 1
fi

# Skip if AWS credentials not configured (local dev)
if ! aws sts get-caller-identity &>/dev/null; then
  echo "⚠ AWS credentials not configured — skipping S3 upload."
  echo "  To upload: export AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY, then re-run."
  exit 0
fi

RELEASE_DIR="release/${VERSION}"
S3_DEST="${S3_BUCKET}/desktop/${VERSION}"
INVALIDATION_PATH="/kaitu/desktop/${VERSION}/*"

upload_file() {
  local FILE="$1"
  local OPTIONAL="${2:-}"
  if [ ! -f "$FILE" ]; then
    if [ "$OPTIONAL" = "--optional" ]; then
      echo "  skip: $(basename "$FILE") (not found)"
      return 0
    fi
    echo "ERROR: $FILE not found. Build may have failed." >&2
    exit 1
  fi
  aws s3 cp "$FILE" "${S3_DEST}/$(basename "$FILE")"
}

echo "=== Uploading ${PLATFORM} v${VERSION} to S3 ==="

case "$PLATFORM" in
  windows)
    upload_file "${RELEASE_DIR}/Kaitu_${VERSION}_x64.exe"
    upload_file "${RELEASE_DIR}/Kaitu_${VERSION}_x64.exe.sig" --optional
    echo "Uploaded: Windows artifacts"
    ;;
  macos)
    upload_file "${RELEASE_DIR}/Kaitu_${VERSION}_universal.pkg"
    upload_file "${RELEASE_DIR}/Kaitu_${VERSION}_universal.app.tar.gz"
    upload_file "${RELEASE_DIR}/Kaitu_${VERSION}_universal.app.tar.gz.sig" --optional
    echo "Uploaded: macOS artifacts"
    ;;
  linux)
    upload_file "${RELEASE_DIR}/Kaitu_${VERSION}_amd64.tar.gz"
    upload_file "${RELEASE_DIR}/Kaitu_${VERSION}_amd64.tar.gz.sig" --optional
    upload_file "${RELEASE_DIR}/k2-linux-amd64"
    echo "Uploaded: Linux artifacts"
    ;;
  android)
    S3_DEST="${S3_BUCKET}/android/${VERSION}"
    INVALIDATION_PATH="/kaitu/android/${VERSION}/*"
    APK="${RELEASE_DIR}/Kaitu-${VERSION}.apk"
    if [ ! -f "$APK" ]; then
      echo "ERROR: $APK not found. Run 'make build-android' first." >&2; exit 1
    fi
    aws s3 cp "$APK" "${S3_DEST}/Kaitu-${VERSION}.apk"
    echo "Uploaded: android/${VERSION}/Kaitu-${VERSION}.apk"
    ;;
  # web) — removed, see --web error above
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
