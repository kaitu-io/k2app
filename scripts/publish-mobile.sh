#!/usr/bin/env bash
set -euo pipefail

# Mobile release gate: validate CI-built artifacts, compute hashes, publish manifests.
# This is the ONLY script that updates latest.json — CI only uploads artifacts.
#
# Channel is auto-detected from version string: -beta suffix → beta channel.
# Beta is a superset of stable. Stable releases update BOTH channel manifests.
# Artifacts are copied to the beta directory so relative URLs resolve correctly.
#
# Directory structure on S3 (s3://d0.all7.cc/kaitu/):
#   android/{VER}/Kaitu-{VER}.apk              ← CI uploads here
#   android/latest.json                          ← this script publishes
#   android/beta/{VER}/Kaitu-{VER}.apk          ← this script copies
#   android/beta/latest.json                     ← this script publishes
#   web/{VER}/webapp.zip                         ← CI uploads here
#   web/latest.json                              ← this script publishes
#   web/beta/{VER}/webapp.zip                    ← this script copies
#   web/beta/latest.json                         ← this script publishes
#   ios/latest.json                              ← this script publishes
#   ios/beta/latest.json                         ← this script publishes
#
# Usage:
#   make publish-mobile VERSION=0.5.0            # Real S3 publish (stable)
#   make publish-mobile VERSION=0.5.0-beta.1     # Real S3 publish (auto-detects beta)
#   scripts/publish-mobile.sh 0.5.0 --dry-run    # Verify without uploading
#   scripts/publish-mobile.sh 0.5.0 --s3-base=/tmp/mock-s3/kaitu --dry-run  # Local test

VERSION="${1:-}"
S3_BUCKET="d0.all7.cc"
S3_PREFIX="kaitu"
S3_BASE=""
DRY_RUN=false
CHANNEL=""

CDN_PRIMARY="https://d13jc1jqzlg4yt.cloudfront.net/kaitu"
APPSTORE_URL="https://apps.apple.com/app/id6759199298"

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
        aws s3 ls "s3://$S3_BUCKET/$S3_PREFIX/$path" >/dev/null 2>&1
    fi
}

# Download artifact to compute hash/size
download_artifact() {
    local path="$1"
    local dest="$2"
    if use_local; then
        cp "$S3_BASE/$path" "$dest"
    else
        aws s3 cp "s3://$S3_BUCKET/$S3_PREFIX/$path" "$dest" --quiet
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
        echo "[dry-run] Would upload $src to s3://$S3_BUCKET/$S3_PREFIX/$path"
    else
        aws s3 cp "$src" "s3://$S3_BUCKET/$S3_PREFIX/$path" --content-type "$content_type"
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
        echo "[dry-run] Would copy s3://$S3_BUCKET/$S3_PREFIX/$src_path → $dst_path"
    else
        aws s3 cp "s3://$S3_BUCKET/$S3_PREFIX/$src_path" "s3://$S3_BUCKET/$S3_PREFIX/$dst_path"
    fi
}

WORK_TMPDIR=$(mktemp -d)
trap 'rm -rf "$WORK_TMPDIR"' EXIT

# Define artifact paths (CI uploads to {channel}/{VERSION}/)
android_artifact="android/${VERSION}/Kaitu-${VERSION}.apk"
web_artifact="web/${VERSION}/webapp.zip"

# Validate all artifacts exist
echo "Validating artifacts for v${VERSION}..."
MISSING=false
for artifact in "$android_artifact" "$web_artifact"; do
    if ! check_artifact "$artifact"; then
        echo "ERROR: Missing artifact: $artifact" >&2
        MISSING=true
    else
        echo "  ✓ $artifact"
    fi
done

if [ "$MISSING" = true ]; then
    echo "Aborting: one or more artifacts missing. Run CI build first." >&2
    exit 1
fi

echo ""
echo "Channel: ${CHANNEL} | Version: ${VERSION}"
echo ""

# --- Generate and publish manifests for android/web ---

generate_manifest() {
    local channel="$1"
    local artifact="$2"
    local extra_fields="${3:-}"
    local filename
    filename=$(basename "$artifact")
    local local_file="$WORK_TMPDIR/$filename"

    echo "Processing $channel..."
    download_artifact "$artifact" "$local_file"

    # Compute hash and size
    local hash="sha256:$(shasum -a 256 "$local_file" | cut -d' ' -f1)"
    local size
    size=$(stat -f%z "$local_file" 2>/dev/null || stat -c%s "$local_file" 2>/dev/null)

    # Relative URL: VERSION/filename (resolved against manifest baseURL by client)
    local rel_url="${VERSION}/${filename}"

    # Generate latest.json
    local manifest="$WORK_TMPDIR/${channel}-latest.json"
    cat > "$manifest" <<MANIFEST_EOF
{
  "version": "${VERSION}",
  "url": "${rel_url}",
  "hash": "${hash}",
  "size": ${size},
  "released_at": "${RELEASED_AT}"${extra_fields}
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

generate_manifest "android" "$android_artifact" ',
  "min_android": 26'
generate_manifest "web" "$web_artifact"

# --- iOS manifest (metadata only, no artifact) ---
# Note: iOS clients only read ios/latest.json (no beta path awareness).
# Beta iOS distribution is handled by TestFlight, not our manifest system.
# For beta versions, we write ios/beta/latest.json (unused but consistent).
# For stable versions, we write both ios/latest.json and ios/beta/latest.json.

echo "Processing ios..."
ios_manifest="$WORK_TMPDIR/ios-latest.json"
cat > "$ios_manifest" <<IOS_EOF
{
  "version": "${VERSION}",
  "appstore_url": "${APPSTORE_URL}",
  "released_at": "${RELEASED_AT}"
}
IOS_EOF

if [ "$CHANNEL" = "beta" ]; then
    upload_file "ios/beta/latest.json" "$ios_manifest"
    echo "  Published ios/beta/latest.json"
else
    upload_file "ios/latest.json" "$ios_manifest"
    echo "  Published ios/latest.json"
    upload_file "ios/beta/latest.json" "$ios_manifest"
    echo "  Published ios/beta/latest.json"
fi

# --- CloudFront CDN invalidation ---

if [ "$DRY_RUN" = false ] && ! use_local; then
    DISTRIBUTION_ID="${CLOUDFRONT_DISTRIBUTION_ID:-}"
    if [ -n "$DISTRIBUTION_ID" ]; then
        echo ""
        echo "Invalidating CloudFront cache..."
        aws cloudfront create-invalidation \
            --distribution-id "$DISTRIBUTION_ID" \
            --paths "/kaitu/android/*" "/kaitu/web/*" "/kaitu/ios/*" \
            --no-cli-pager
        echo "CDN cache invalidated."
    else
        echo ""
        echo "Note: Set CLOUDFRONT_DISTRIBUTION_ID env var to auto-invalidate CDN cache."
    fi
fi

echo ""
if [ "$CHANNEL" = "beta" ]; then
    echo "Published mobile v${VERSION} beta manifests successfully."
else
    echo "Published mobile v${VERSION} manifests successfully."
fi
if [ "$DRY_RUN" = true ]; then
    echo "(dry-run mode — no actual S3 uploads)"
fi
