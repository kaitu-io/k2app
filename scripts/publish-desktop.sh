#!/usr/bin/env bash
set -euo pipefail

# Publish desktop release: generate latest.json files and upload to S3, then create GitHub Release.
# Run manually after CI build completes and S3 artifacts are verified.
# Channel is auto-detected from version string: -beta suffix → beta channel.
#
# Prerequisites:
#   - AWS CLI configured (AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY, AWS_DEFAULT_REGION)
#   - gh CLI authenticated (for GitHub Release creation)
#
# Usage:
#   bash scripts/publish-desktop.sh                  # auto-detect from package.json version
#   bash scripts/publish-desktop.sh --channel=beta   # force beta channel
#   AWS_DEFAULT_REGION=ap-east-1 bash scripts/publish-desktop.sh

export AWS_DEFAULT_REGION="${AWS_DEFAULT_REGION:-ap-east-1}"

VERSION=$(node -p "require('./package.json').version")

# Auto-detect channel from version; allow --channel override
if [[ "$VERSION" == *"-beta"* ]]; then
  CHANNEL="beta"
else
  CHANNEL="stable"
fi

for arg in "$@"; do
  case "$arg" in
    --channel=*) CHANNEL="${arg#*=}" ;;
  esac
done

if [ "$CHANNEL" != "stable" ] && [ "$CHANNEL" != "beta" ]; then
  echo "ERROR: Invalid channel '${CHANNEL}'. Must be 'stable' or 'beta'."
  exit 1
fi
PUB_DATE=$(date -u +"%Y-%m-%dT%H:%M:%S.000Z")
S3_VER="s3://d0.all7.cc/kaitu/desktop/${VERSION}"
S3_ROOT="s3://d0.all7.cc/kaitu/desktop"
REPO=$(gh repo view --json nameWithOwner -q .nameWithOwner)

# Beta publishes to beta/ subdirectory
if [ "$CHANNEL" = "beta" ]; then
  S3_MANIFEST="${S3_ROOT}/beta"
else
  S3_MANIFEST="${S3_ROOT}"
fi

echo "Publishing Kaitu Desktop v${VERSION} (channel=${CHANNEL})"
echo "S3 version path: ${S3_VER}"
echo "S3 manifest path: ${S3_MANIFEST}"
echo ""

# Verify S3 artifacts exist
echo "Checking S3 artifacts..."
aws s3 ls "${S3_VER}/" || { echo "ERROR: No artifacts found at ${S3_VER}/"; exit 1; }
echo ""

# Download .sig files to read signatures
TMPDIR=$(mktemp -d)
trap 'rm -rf "${TMPDIR}"' EXIT

aws s3 cp "${S3_VER}/" "${TMPDIR}/" --recursive \
  --exclude "*" --include "*.sig"

MACOS_SIG=$(cat "${TMPDIR}/Kaitu_${VERSION}_universal.app.tar.gz.sig" 2>/dev/null || echo "")
WINDOWS_SIG=$(cat "${TMPDIR}/Kaitu_${VERSION}_x64.exe.sig" 2>/dev/null || echo "")
LINUX_SIG=$(cat "${TMPDIR}/Kaitu_${VERSION}_amd64.tar.gz.sig" 2>/dev/null || echo "")

if [ -z "${MACOS_SIG}" ]; then
  echo "WARNING: macOS signature not found"
fi
if [ -z "${WINDOWS_SIG}" ]; then
  echo "WARNING: Windows signature not found"
fi
if [ -z "${LINUX_SIG}" ]; then
  echo "WARNING: Linux signature not found"
fi

# --- Pre-publish signature verification ---
# Download actual artifacts from S3 (no CDN cache) and verify each .sig matches.
# This prevents publishing a latest.json with stale signatures after artifact rebuilds.
PUBKEY=$(node -p "require('./desktop/src-tauri/tauri.conf.json').plugins.updater.pubkey" | base64 -d | tail -1)
echo "Updater pubkey: ${PUBKEY}"
echo ""

VERIFY_DIR="${TMPDIR}/verify"
mkdir -p "${VERIFY_DIR}"

verify_signature() {
  local NAME="$1" SIG_VAR="$2" PLATFORM="$3"
  if [ -z "${SIG_VAR}" ]; then
    echo "  SKIP ${PLATFORM}: no signature"
    return 0
  fi
  echo "  Downloading ${NAME}..."
  aws s3 cp "${S3_VER}/${NAME}" "${VERIFY_DIR}/${NAME}" --quiet
  echo "${SIG_VAR}" | base64 -d > "${VERIFY_DIR}/${NAME}.minisig"
  if minisign -V -P "${PUBKEY}" -m "${VERIFY_DIR}/${NAME}" -x "${VERIFY_DIR}/${NAME}.minisig" -q 2>/dev/null; then
    echo "  PASS ${PLATFORM}: ${NAME}"
  elif minisign -V -H -P "${PUBKEY}" -m "${VERIFY_DIR}/${NAME}" -x "${VERIFY_DIR}/${NAME}.minisig" -q 2>/dev/null; then
    echo "  PASS ${PLATFORM}: ${NAME} (prehashed)"
  else
    echo "  FAIL ${PLATFORM}: signature does not match ${NAME}"
    echo ""
    echo "ERROR: Signature verification failed for ${PLATFORM}."
    echo "The .sig on S3 does not match the artifact. Was the artifact rebuilt without re-signing?"
    exit 1
  fi
  rm -f "${VERIFY_DIR}/${NAME}" "${VERIFY_DIR}/${NAME}.minisig"
}

echo "Verifying signatures against S3 artifacts..."
verify_signature "Kaitu_${VERSION}_universal.app.tar.gz" "${MACOS_SIG}" "macOS"
verify_signature "Kaitu_${VERSION}_x64.exe" "${WINDOWS_SIG}" "Windows"
verify_signature "Kaitu_${VERSION}_amd64.tar.gz" "${LINUX_SIG}" "Linux"
echo "All signatures verified."
echo ""

# Generate cloudfront.latest.json
# All 3 macOS keys (aarch64, x86_64, universal) point to the same universal binary.
# Tauri updater queries {os}-{arch} (e.g. darwin-aarch64) with NO fallback to darwin-universal,
# so we must list all arch keys to support upgrades from older arch-specific builds.
cat > "${TMPDIR}/cloudfront.latest.json" << EOF
{
  "version": "${VERSION}",
  "notes": "See https://github.com/${REPO}/releases/tag/v${VERSION}",
  "pub_date": "${PUB_DATE}",
  "platforms": {
    "darwin-aarch64": {
      "url": "https://d13jc1jqzlg4yt.cloudfront.net/kaitu/desktop/${VERSION}/Kaitu_${VERSION}_universal.app.tar.gz",
      "signature": "${MACOS_SIG}"
    },
    "darwin-x86_64": {
      "url": "https://d13jc1jqzlg4yt.cloudfront.net/kaitu/desktop/${VERSION}/Kaitu_${VERSION}_universal.app.tar.gz",
      "signature": "${MACOS_SIG}"
    },
    "darwin-universal": {
      "url": "https://d13jc1jqzlg4yt.cloudfront.net/kaitu/desktop/${VERSION}/Kaitu_${VERSION}_universal.app.tar.gz",
      "signature": "${MACOS_SIG}"
    },
    "windows-x86_64": {
      "url": "https://d13jc1jqzlg4yt.cloudfront.net/kaitu/desktop/${VERSION}/Kaitu_${VERSION}_x64.exe",
      "signature": "${WINDOWS_SIG}"
    },
    "linux-x86_64": {
      "url": "https://d13jc1jqzlg4yt.cloudfront.net/kaitu/desktop/${VERSION}/Kaitu_${VERSION}_amd64.tar.gz",
      "signature": "${LINUX_SIG}"
    }
  }
}
EOF

# Generate d0.latest.json
cat > "${TMPDIR}/d0.latest.json" << EOF
{
  "version": "${VERSION}",
  "notes": "See https://github.com/${REPO}/releases/tag/v${VERSION}",
  "pub_date": "${PUB_DATE}",
  "platforms": {
    "darwin-aarch64": {
      "url": "https://d0.all7.cc/kaitu/desktop/${VERSION}/Kaitu_${VERSION}_universal.app.tar.gz",
      "signature": "${MACOS_SIG}"
    },
    "darwin-x86_64": {
      "url": "https://d0.all7.cc/kaitu/desktop/${VERSION}/Kaitu_${VERSION}_universal.app.tar.gz",
      "signature": "${MACOS_SIG}"
    },
    "darwin-universal": {
      "url": "https://d0.all7.cc/kaitu/desktop/${VERSION}/Kaitu_${VERSION}_universal.app.tar.gz",
      "signature": "${MACOS_SIG}"
    },
    "windows-x86_64": {
      "url": "https://d0.all7.cc/kaitu/desktop/${VERSION}/Kaitu_${VERSION}_x64.exe",
      "signature": "${WINDOWS_SIG}"
    },
    "linux-x86_64": {
      "url": "https://d0.all7.cc/kaitu/desktop/${VERSION}/Kaitu_${VERSION}_amd64.tar.gz",
      "signature": "${LINUX_SIG}"
    }
  }
}
EOF

echo "cloudfront.latest.json:"
cat "${TMPDIR}/cloudfront.latest.json"
echo ""
echo "d0.latest.json:"
cat "${TMPDIR}/d0.latest.json"
echo ""

# Upload latest.json files
aws s3 cp "${TMPDIR}/cloudfront.latest.json" "${S3_MANIFEST}/cloudfront.latest.json"
aws s3 cp "${TMPDIR}/d0.latest.json" "${S3_MANIFEST}/d0.latest.json"
echo "latest.json files uploaded to ${S3_MANIFEST}/"

# Beta channel is a superset of stable — sync stable release to beta manifest
if [ "$CHANNEL" = "stable" ]; then
  echo "Syncing stable release to beta channel..."
  aws s3 cp "${TMPDIR}/cloudfront.latest.json" "${S3_ROOT}/beta/cloudfront.latest.json"
  aws s3 cp "${TMPDIR}/d0.latest.json" "${S3_ROOT}/beta/d0.latest.json"
  echo "Beta manifest updated to stable v${VERSION}"
fi

# Create GitHub Release (stable only — beta skips GitHub Release)
if [ "$CHANNEL" = "stable" ]; then
  if gh release view "v${VERSION}" &>/dev/null; then
    echo "GitHub Release v${VERSION} already exists, skipping."
  else
    gh release create "v${VERSION}" \
      --title "Kaitu v${VERSION}" \
      --notes "## Kaitu Desktop v${VERSION}

| Platform | Installer | Auto-Update |
|----------|-----------|-------------|
| **macOS** (Universal) | \`.pkg\` | \`.app.tar.gz\` |
| **Windows** (x64) | \`.exe\` | \`.exe\` (auto-update) |
| **Linux** (x86_64) | \`tar.gz\` | \`tar.gz\` (auto-update) |
"
    echo ""
    echo "GitHub Release created for v${VERSION}"
  fi
fi

# --- CDN invalidation (both distributions) ---
CDN_ID_D0="${CLOUDFRONT_DISTRIBUTION_ID:-E3W144CRNT652P}"
CDN_ID_DL="E34P52R7B93FSC"
echo ""
echo "Invalidating CDN caches..."
for DIST_ID in "$CDN_ID_D0" "$CDN_ID_DL"; do
  aws cloudfront create-invalidation \
    --distribution-id "$DIST_ID" \
    --paths "/kaitu/desktop/cloudfront.latest.json" \
            "/kaitu/desktop/beta/cloudfront.latest.json" \
            "/kaitu/desktop/d0.latest.json" \
            "/kaitu/desktop/beta/d0.latest.json" \
    --no-cli-pager --output text > /dev/null
done
echo "CDN invalidated: d0.all7.cc + dl.kaitu.io"

echo ""
if [ "$CHANNEL" = "stable" ]; then
  echo "Done! Published Kaitu Desktop v${VERSION} (stable + GitHub Release)"
else
  echo "Done! Published Kaitu Desktop v${VERSION} (beta channel only, no GitHub Release)"
fi
