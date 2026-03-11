#!/bin/sh
# release.sh — Generate checksums and upload to S3
#
# Usage: scripts/release.sh <version>
#
# Expects binaries in build/ directory (built by Makefile or CI).
# Uploads to S3 bucket (CloudFront serves from same bucket).
set -e

VERSION="$1"
[ -n "$VERSION" ] || { echo "Usage: scripts/release.sh <version>" >&2; exit 1; }

S3_BUCKET="${S3_BUCKET:-s3://d0.all7.cc/kaitu/k2}"
AWS_REGION="${AWS_DEFAULT_REGION:-ap-east-1}"

BUILD_DIR="build"
[ -d "$BUILD_DIR" ] || { echo "Error: build/ directory not found" >&2; exit 1; }

# 1. Generate checksums
echo "Generating checksums..."
cd "$BUILD_DIR"
if command -v sha256sum >/dev/null 2>&1; then
    sha256sum k2s-* k2-* > checksums.txt
elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 k2s-* k2-* > checksums.txt
else
    echo "Error: sha256sum or shasum required" >&2; exit 1
fi
cat checksums.txt
cd ..

# 2. Upload to S3
echo ""
echo "Uploading to ${S3_BUCKET}..."

# Binaries + checksums
aws s3 cp "$BUILD_DIR/" "${S3_BUCKET}/${VERSION}/" --recursive --exclude "*" \
    --include "k2s-*" --include "k2-*" --include "checksums.txt" \
    --region "$AWS_REGION"

# Version pointer (short cache for mutable files)
echo "$VERSION" | aws s3 cp - "${S3_BUCKET}/LATEST" \
    --region "$AWS_REGION" --cache-control "max-age=300"

# Beta: also update beta/LATEST
case "$VERSION" in
    *-beta*)
        echo "$VERSION" | aws s3 cp - "${S3_BUCKET}/beta/LATEST" \
            --region "$AWS_REGION" --cache-control "max-age=300"
        echo "  Updated beta/LATEST -> $VERSION"
        ;;
esac

# Install scripts (always latest)
aws s3 cp scripts/install-k2s.sh "${S3_BUCKET}/install-k2s.sh" \
    --content-type text/plain --region "$AWS_REGION"
aws s3 cp scripts/install-k2.sh "${S3_BUCKET}/install-k2.sh" \
    --content-type text/plain --region "$AWS_REGION"

echo ""
echo "Released k2 $VERSION"
