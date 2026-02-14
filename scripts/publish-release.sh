#!/usr/bin/env bash
set -euo pipefail

# Publish release: generate latest.json files and upload to S3, then create GitHub Release.
# Run manually after CI build completes and S3 artifacts are verified.
#
# Prerequisites:
#   - AWS CLI configured (AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY, AWS_DEFAULT_REGION)
#   - gh CLI authenticated (for GitHub Release creation)
#
# Usage:
#   bash scripts/publish-release.sh
#   AWS_DEFAULT_REGION=ap-east-1 bash scripts/publish-release.sh

export AWS_DEFAULT_REGION="${AWS_DEFAULT_REGION:-ap-east-1}"

VERSION=$(node -p "require('./package.json').version")
PUB_DATE=$(date -u +"%Y-%m-%dT%H:%M:%S.000Z")
S3_VER="s3://d0.all7.cc/kaitu/desktop/${VERSION}"
S3_ROOT="s3://d0.all7.cc/kaitu/desktop"
REPO=$(gh repo view --json nameWithOwner -q .nameWithOwner)

echo "Publishing Kaitu Desktop v${VERSION}"
echo "S3 version path: ${S3_VER}"
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

MACOS_SIG=$(cat "${TMPDIR}"/*.app.tar.gz.sig 2>/dev/null || echo "")
WINDOWS_SIG=$(cat "${TMPDIR}"/*_x64-setup.nsis.zip.sig 2>/dev/null || echo "")

if [ -z "${MACOS_SIG}" ]; then
  echo "WARNING: macOS signature not found"
fi
if [ -z "${WINDOWS_SIG}" ]; then
  echo "WARNING: Windows signature not found"
fi

# Generate cloudfront.latest.json
cat > "${TMPDIR}/cloudfront.latest.json" << EOF
{
  "version": "${VERSION}",
  "notes": "See https://github.com/${REPO}/releases/tag/v${VERSION}",
  "pub_date": "${PUB_DATE}",
  "platforms": {
    "darwin-universal": {
      "url": "https://d13jc1jqzlg4yt.cloudfront.net/kaitu/desktop/${VERSION}/Kaitu.app.tar.gz",
      "signature": "${MACOS_SIG}"
    },
    "windows-x86_64": {
      "url": "https://d13jc1jqzlg4yt.cloudfront.net/kaitu/desktop/${VERSION}/Kaitu_${VERSION}_x64-setup.nsis.zip",
      "signature": "${WINDOWS_SIG}"
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
    "darwin-universal": {
      "url": "https://d0.all7.cc/kaitu/desktop/${VERSION}/Kaitu.app.tar.gz",
      "signature": "${MACOS_SIG}"
    },
    "windows-x86_64": {
      "url": "https://d0.all7.cc/kaitu/desktop/${VERSION}/Kaitu_${VERSION}_x64-setup.nsis.zip",
      "signature": "${WINDOWS_SIG}"
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
aws s3 cp "${TMPDIR}/cloudfront.latest.json" "${S3_ROOT}/cloudfront.latest.json" --acl public-read
aws s3 cp "${TMPDIR}/d0.latest.json" "${S3_ROOT}/d0.latest.json" --acl public-read
echo "latest.json files uploaded to S3"

# Create GitHub Release
gh release create "v${VERSION}" \
  --title "Kaitu v${VERSION}" \
  --notes "## Kaitu Desktop v${VERSION}

| Platform | Installer | Auto-Update |
|----------|-----------|-------------|
| **macOS** (Universal) | \`.pkg\` | \`.app.tar.gz\` |
| **Windows** (x64) | \`.exe\` | \`.nsis.zip\` |
"

echo ""
echo "Done! Published Kaitu Desktop v${VERSION}"
