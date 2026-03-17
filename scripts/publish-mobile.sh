#!/usr/bin/env bash
set -euo pipefail

# Two-phase release: publish mobile manifests (latest.json) for Android APK and Web OTA.
# Phase 1 (CI): build-mobile.yml uploads artifacts to S3 versioned directories
# Phase 2 (this script): validates artifacts exist, computes hashes, generates latest.json
# Channel is auto-detected from version string: -beta suffix → beta channel.
#
# Beta is a superset of stable. Stable releases are published to BOTH channels.
# Artifacts are copied to the beta directory so relative URLs resolve correctly.
#
# Directory structure:
#   android/0.5.0/Kaitu-0.5.0.apk              ← stable artifact
#   android/latest.json                          ← stable manifest
#   android/beta/0.5.0/Kaitu-0.5.0.apk          ← copy for beta channel
#   android/beta/0.5.0-beta.1/Kaitu-...apk      ← beta artifact
#   android/beta/latest.json                     ← beta manifest
#
# Usage:
#   scripts/publish-mobile.sh VERSION [--s3-base=PATH] [--dry-run] [--channel=stable|beta]
#
# Examples:
#   make publish-mobile VERSION=0.5.0            # Real S3 publish (stable)
#   make publish-mobile VERSION=0.5.0-beta.1     # Real S3 publish (auto-detects beta)
#   scripts/publish-mobile.sh 0.5.0 --dry-run    # Verify without uploading
#   scripts/publish-mobile.sh 0.5.0 --s3-base=/tmp/mock-s3/kaitu --dry-run  # Local test

VERSION="${1:-}"
S3_BUCKET="kaitu-releases"
S3_BASE=""
DRY_RUN=false
CHANNEL=""

# Parse arguments
shift || true
for arg in "$@"; do
    case "$arg" in
        --s3-base=*) S3_BASE="${arg#*=}" ;;
        --dry-run) DRY_RUN=true ;;
        --channel=*) CHANNEL="${arg#*=}" ;;
        *) echo "Unknown argument: $arg" >&2; exit 1 ;;
    esac
done

if [ -z "$VERSION" ]; then
    echo "Usage: $0 VERSION [--s3-base=PATH] [--dry-run] [--channel=stable|beta]" >&2
    exit 1
fi

# Auto-detect channel from version if not explicitly set
if [ -z "$CHANNEL" ]; then
    if [[ "$VERSION" == *"-beta"* ]]; then
        CHANNEL="beta"
    else
        CHANNEL="stable"
    fi
fi

if [ "$CHANNEL" != "stable" ] && [ "$CHANNEL" != "beta" ]; then
    echo "ERROR: Invalid channel '${CHANNEL}'. Must be 'stable' or 'beta'." >&2
    exit 1
fi

RELEASED_AT=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

# Determine if using local filesystem or real S3
use_local() { [ -n "$S3_BASE" ]; }

# File existence check
check_artifact() {
    local path="$1"
    if use_local; then
        [ -f "$S3_BASE/$path" ]
    else
        aws s3 ls "s3://$S3_BUCKET/$path" >/dev/null 2>&1
    fi
}

# Download artifact to compute hash/size
download_artifact() {
    local path="$1"
    local dest="$2"
    if use_local; then
        cp "$S3_BASE/$path" "$dest"
    else
        aws s3 cp "s3://$S3_BUCKET/$path" "$dest" --quiet
    fi
}

# Upload file (manifest or artifact copy)
upload_file() {
    local path="$1"
    local src="$2"
    local content_type="${3:-application/json}"
    if use_local; then
        mkdir -p "$(dirname "$S3_BASE/$path")"
        cp "$src" "$S3_BASE/$path"
    elif [ "$DRY_RUN" = true ]; then
        echo "[dry-run] Would upload $src to s3://$S3_BUCKET/$path"
    else
        aws s3 cp "$src" "s3://$S3_BUCKET/$path" --content-type "$content_type"
    fi
}

# Copy artifact within S3 (or local)
copy_s3() {
    local src_path="$1"
    local dst_path="$2"
    if use_local; then
        mkdir -p "$(dirname "$S3_BASE/$dst_path")"
        cp "$S3_BASE/$src_path" "$S3_BASE/$dst_path"
    elif [ "$DRY_RUN" = true ]; then
        echo "[dry-run] Would copy s3://$S3_BUCKET/$src_path → s3://$S3_BUCKET/$dst_path"
    else
        aws s3 cp "s3://$S3_BUCKET/$src_path" "s3://$S3_BUCKET/$dst_path"
    fi
}

WORK_TMPDIR=$(mktemp -d)
trap 'rm -rf "$WORK_TMPDIR"' EXIT

# Define artifact paths (CI always uploads to {channel}/{VERSION}/)
android_artifact="android/${VERSION}/Kaitu-${VERSION}.apk"
web_artifact="web/${VERSION}/webapp.zip"

# Validate all artifacts exist
echo "Validating artifacts for v${VERSION}..."
MISSING=false
for artifact in "$android_artifact" "$web_artifact"; do
    if ! check_artifact "$artifact"; then
        echo "ERROR: Missing artifact: $artifact" >&2
        MISSING=true
    fi
done

if [ "$MISSING" = true ]; then
    echo "Aborting: one or more artifacts missing. Run CI build first." >&2
    exit 1
fi

# Generate and publish manifests for a channel type (android/web)
generate_manifest() {
    local channel="$1"
    local artifact="$2"
    local filename
    filename=$(basename "$artifact")
    local local_file="$WORK_TMPDIR/$filename"

    echo "Processing $channel..."
    download_artifact "$artifact" "$local_file"

    # Compute hash and size
    local hash="sha256:$(shasum -a 256 "$local_file" | cut -d' ' -f1)"
    local size
    size=$(stat -f%z "$local_file" 2>/dev/null || stat -c%s "$local_file" 2>/dev/null)

    # Relative URL: always VERSION/filename (works for both stable and beta
    # because artifacts are copied into each channel's directory)
    local rel_url="${VERSION}/${filename}"

    # Generate latest.json
    local manifest="$WORK_TMPDIR/${channel}-latest.json"
    cat > "$manifest" <<MANIFEST_EOF
{
  "version": "${VERSION}",
  "url": "${rel_url}",
  "hash": "${hash}",
  "size": ${size},
  "released_at": "${RELEASED_AT}",
  "min_android": 26
}
MANIFEST_EOF

    # Copy artifact to beta directory (beta is superset of stable)
    local beta_artifact_path="${channel}/beta/${VERSION}/${filename}"
    copy_s3 "$artifact" "$beta_artifact_path"
    echo "  Copied artifact → $beta_artifact_path"

    if [ "$CHANNEL" = "beta" ]; then
        # Beta: only update beta manifest
        upload_file "${channel}/beta/latest.json" "$manifest"
        echo "  Published ${channel}/beta/latest.json"
    else
        # Stable: update both stable and beta manifests
        upload_file "${channel}/latest.json" "$manifest"
        echo "  Published ${channel}/latest.json"
        upload_file "${channel}/beta/latest.json" "$manifest"
        echo "  Published ${channel}/beta/latest.json"
    fi
}

generate_manifest "android" "$android_artifact"
generate_manifest "web" "$web_artifact"

echo ""
if [ "$CHANNEL" = "beta" ]; then
    echo "Published mobile v${VERSION} beta manifests successfully."
else
    echo "Published mobile v${VERSION} manifests successfully."
fi
if [ "$DRY_RUN" = true ]; then
    echo "(dry-run mode - no actual S3 uploads)"
fi
