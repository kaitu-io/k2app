#!/usr/bin/env bash
set -euo pipefail

# Publish k2/k2s standalone binaries: upload to S3, generate latest.json.
# Two-phase release (mirrors desktop publish-release.sh pattern):
#   Phase 1: build-k2-standalone.sh produces binaries + CHECKSUMS.txt
#   Phase 2: this script uploads to S3 and publishes latest.json with checksums
#
# Prerequisites:
#   - AWS CLI configured
#   - build/k2-standalone/ populated by build-k2-standalone.sh (including CHECKSUMS.txt)
#
# Usage:
#   bash scripts/publish-k2.sh                # Publish current version
#   bash scripts/publish-k2.sh --dry-run      # Verify without uploading

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(dirname "$SCRIPT_DIR")"

export AWS_DEFAULT_REGION="${AWS_DEFAULT_REGION:-ap-east-1}"

VERSION=$(node -p "require('${ROOT_DIR}/package.json').version")
OUT_DIR="${ROOT_DIR}/build/k2-standalone"
S3_VER="s3://d0.all7.cc/kaitu/k2/${VERSION}"
S3_ROOT="s3://d0.all7.cc/kaitu/k2"
CLOUDFRONT="https://d13jc1jqzlg4yt.cloudfront.net/kaitu/k2"
D0="https://d0.all7.cc/kaitu/k2"
DRY_RUN=false

for arg in "$@"; do
    case "$arg" in
        --dry-run) DRY_RUN=true ;;
        *) echo "Unknown argument: $arg" >&2; exit 1 ;;
    esac
done

PLATFORMS=("linux-amd64" "linux-arm64" "darwin-amd64" "darwin-arm64")
BINARIES=("k2" "k2s")

echo "Publishing k2 standalone v${VERSION}"
echo "S3: ${S3_VER}/"
echo ""

# Verify all artifacts + CHECKSUMS.txt exist
echo "Checking artifacts..."
MISSING=false
for platform in "${PLATFORMS[@]}"; do
    for bin in "${BINARIES[@]}"; do
        file="${OUT_DIR}/${bin}-${platform}"
        if [ ! -f "$file" ]; then
            echo "  MISSING: ${bin}-${platform}"
            MISSING=true
        fi
    done
done

if [ ! -f "${OUT_DIR}/CHECKSUMS.txt" ]; then
    echo "  MISSING: CHECKSUMS.txt"
    MISSING=true
fi

if [ "$MISSING" = true ]; then
    echo "ERROR: Missing artifacts. Run 'make build-k2-standalone' first." >&2
    exit 1
fi
echo "  All 8 artifacts + CHECKSUMS.txt found"
echo ""

# Parse CHECKSUMS.txt into associative array: filename -> sha256 hash
declare -A CHECKSUMS
while IFS=' ' read -r hash filename; do
    # shasum output: "hash  filename" (two spaces) — strip leading/trailing whitespace from filename
    filename="${filename## }"
    CHECKSUMS["$filename"]="$hash"
done < "${OUT_DIR}/CHECKSUMS.txt"

# Helper: get checksum for a binary, with sha256: prefix
get_checksum() {
    local file="$1"
    local hash="${CHECKSUMS[$file]:-}"
    if [ -z "$hash" ]; then
        echo "ERROR: No checksum found for $file in CHECKSUMS.txt" >&2
        exit 1
    fi
    echo "sha256:${hash}"
}

# Upload binaries + CHECKSUMS.txt
echo "Uploading binaries..."
for platform in "${PLATFORMS[@]}"; do
    for bin in "${BINARIES[@]}"; do
        file="${OUT_DIR}/${bin}-${platform}"
        s3_path="${S3_VER}/${bin}-${platform}"
        if [ "$DRY_RUN" = true ]; then
            echo "  [dry-run] ${bin}-${platform}"
        else
            aws s3 cp "$file" "$s3_path" --quiet
            echo "  ${bin}-${platform}"
        fi
    done
done

if [ "$DRY_RUN" = true ]; then
    echo "  [dry-run] CHECKSUMS.txt"
else
    aws s3 cp "${OUT_DIR}/CHECKSUMS.txt" "${S3_VER}/CHECKSUMS.txt" \
        --content-type "text/plain" --quiet
    echo "  CHECKSUMS.txt"
fi
echo ""

# Generate latest.json manifests (compatible with k2 upgrade.go latestInfo struct)
TMPDIR=$(mktemp -d)
trap 'rm -rf "$TMPDIR"' EXIT

# k2 client — cloudfront.latest.json
cat > "${TMPDIR}/cloudfront.latest.json" <<EOF
{
  "version": "${VERSION}",
  "binaries": {
    "linux-amd64": "${CLOUDFRONT}/${VERSION}/k2-linux-amd64",
    "linux-arm64": "${CLOUDFRONT}/${VERSION}/k2-linux-arm64",
    "darwin-amd64": "${CLOUDFRONT}/${VERSION}/k2-darwin-amd64",
    "darwin-arm64": "${CLOUDFRONT}/${VERSION}/k2-darwin-arm64"
  },
  "checksums": {
    "linux-amd64": "$(get_checksum k2-linux-amd64)",
    "linux-arm64": "$(get_checksum k2-linux-arm64)",
    "darwin-amd64": "$(get_checksum k2-darwin-amd64)",
    "darwin-arm64": "$(get_checksum k2-darwin-arm64)"
  }
}
EOF

# k2 client — d0.latest.json
cat > "${TMPDIR}/d0.latest.json" <<EOF
{
  "version": "${VERSION}",
  "binaries": {
    "linux-amd64": "${D0}/${VERSION}/k2-linux-amd64",
    "linux-arm64": "${D0}/${VERSION}/k2-linux-arm64",
    "darwin-amd64": "${D0}/${VERSION}/k2-darwin-amd64",
    "darwin-arm64": "${D0}/${VERSION}/k2-darwin-arm64"
  },
  "checksums": {
    "linux-amd64": "$(get_checksum k2-linux-amd64)",
    "linux-arm64": "$(get_checksum k2-linux-arm64)",
    "darwin-amd64": "$(get_checksum k2-darwin-amd64)",
    "darwin-arm64": "$(get_checksum k2-darwin-arm64)"
  }
}
EOF

# k2s server — k2s-cloudfront.latest.json
cat > "${TMPDIR}/k2s-cloudfront.latest.json" <<EOF
{
  "version": "${VERSION}",
  "binaries": {
    "linux-amd64": "${CLOUDFRONT}/${VERSION}/k2s-linux-amd64",
    "linux-arm64": "${CLOUDFRONT}/${VERSION}/k2s-linux-arm64",
    "darwin-amd64": "${CLOUDFRONT}/${VERSION}/k2s-darwin-amd64",
    "darwin-arm64": "${CLOUDFRONT}/${VERSION}/k2s-darwin-arm64"
  },
  "checksums": {
    "linux-amd64": "$(get_checksum k2s-linux-amd64)",
    "linux-arm64": "$(get_checksum k2s-linux-arm64)",
    "darwin-amd64": "$(get_checksum k2s-darwin-amd64)",
    "darwin-arm64": "$(get_checksum k2s-darwin-arm64)"
  }
}
EOF

# k2s server — k2s-d0.latest.json
cat > "${TMPDIR}/k2s-d0.latest.json" <<EOF
{
  "version": "${VERSION}",
  "binaries": {
    "linux-amd64": "${D0}/${VERSION}/k2s-linux-amd64",
    "linux-arm64": "${D0}/${VERSION}/k2s-linux-arm64",
    "darwin-amd64": "${D0}/${VERSION}/k2s-darwin-amd64",
    "darwin-arm64": "${D0}/${VERSION}/k2s-darwin-arm64"
  },
  "checksums": {
    "linux-amd64": "$(get_checksum k2s-linux-amd64)",
    "linux-arm64": "$(get_checksum k2s-linux-arm64)",
    "darwin-amd64": "$(get_checksum k2s-darwin-amd64)",
    "darwin-arm64": "$(get_checksum k2s-darwin-arm64)"
  }
}
EOF

echo "cloudfront.latest.json:"
cat "${TMPDIR}/cloudfront.latest.json"
echo ""

if [ "$DRY_RUN" = true ]; then
    echo "[dry-run] Would upload 4 latest.json files to ${S3_ROOT}/"
else
    aws s3 cp "${TMPDIR}/cloudfront.latest.json" "${S3_ROOT}/cloudfront.latest.json" \
        --content-type "application/json"
    aws s3 cp "${TMPDIR}/d0.latest.json" "${S3_ROOT}/d0.latest.json" \
        --content-type "application/json"
    aws s3 cp "${TMPDIR}/k2s-cloudfront.latest.json" "${S3_ROOT}/k2s-cloudfront.latest.json" \
        --content-type "application/json"
    aws s3 cp "${TMPDIR}/k2s-d0.latest.json" "${S3_ROOT}/k2s-d0.latest.json" \
        --content-type "application/json"
    echo "4 latest.json files uploaded"
fi

echo ""
echo "Published k2 standalone v${VERSION}"
if [ "$DRY_RUN" = true ]; then
    echo "(dry-run mode — no actual S3 uploads)"
fi
