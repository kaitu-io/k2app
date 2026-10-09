#!/usr/bin/env bash
set -euo pipefail

# Mobile release gate: validate CI-built artifacts, compute hashes, publish manifests.
# This is the ONLY script that updates latest.json — CI only uploads artifacts.
#
# Channel is auto-detected from version string: -beta suffix → beta channel.
# Beta is a superset of stable. Stable releases update BOTH channel manifests.
# Artifacts are copied to the beta directory so relative URLs resolve correctly.
#
# Directory structure on S3 (s3://d0.all7.cc/{brand}/, brand = kaitu|overleap):
#   android/{VER}/{Kaitu|Overleap}-{VER}.apk      ← CI uploads here
#   android/latest.json                          ← this script publishes
#   android/beta/{VER}/{Kaitu|Overleap}-{VER}.apk ← this script copies
#   android/beta/latest.json                     ← this script publishes
#   ios/latest.json                              ← this script publishes
#   ios/beta/latest.json                         ← this script publishes
#
# --brand=kaitu|overleap selects the S3/CDN prefix and artifact filename
# prefix. Falls back to $K2_BRAND, then "kaitu". The App Store link is built
# from each brand's App Store Connect app id (kaitu 6448744655, overleap
# 6759199298) — known before the listing goes live, so nothing waits on review.
# OVERLEAP_APPSTORE_URL overrides overleap's.
# overleap android is Play-only: --platform=android exits 0 without touching
# S3, and --platform=both narrows to ios.
#
# kaitu android's manifest url is ABSOLUTE and points at the mainland mirror
# (see "Mainland mirror" below); --no-mirror falls back to the relative CDN url.
#
# Usage:
#   make publish-mobile VERSION=0.5.0            # Real S3 publish (stable)
#   make publish-mobile VERSION=0.5.0-beta.1     # Real S3 publish (auto-detects beta)
#   scripts/publish-mobile.sh 0.5.0 --dry-run    # Verify without uploading
#   scripts/publish-mobile.sh 0.5.0 --s3-base=/tmp/mock-s3/kaitu --dry-run  # Local test
#   scripts/publish-mobile.sh 0.5.0 --brand=overleap --dry-run              # Overleap brand

VERSION="${1:-}"
S3_BUCKET="d0.all7.cc"
S3_BASE=""
DRY_RUN=false
CHANNEL=""
PLATFORM=""  # empty = both, "android" or "ios"
BRAND="${K2_BRAND:-kaitu}"
NO_MIRROR=false

# Parse arguments
shift || true
for arg in "$@"; do
    case "$arg" in
        --s3-base=*) S3_BASE="${arg#*=}" ;;
        --dry-run) DRY_RUN=true ;;
        --channel=*) CHANNEL="${arg#*=}" ;;
        --platform=*) PLATFORM="${arg#*=}" ;;
        --brand=*) BRAND="${arg#*=}" ;;
        --no-mirror) NO_MIRROR=true ;;
        *) echo "Unknown argument: $arg" >&2; exit 1 ;;
    esac
done

if [ -z "$VERSION" ]; then
    echo "Usage: $0 VERSION [--s3-base=PATH] [--dry-run] [--channel=stable|beta] [--platform=android|ios] [--brand=kaitu|overleap] [--no-mirror]" >&2
    exit 1
fi

if [ "$BRAND" != "kaitu" ] && [ "$BRAND" != "overleap" ]; then
    echo "ERROR: Invalid brand '${BRAND}'. Must be 'kaitu' or 'overleap'." >&2
    exit 1
fi
BRAND_PRODUCT=$([ "$BRAND" = "overleap" ] && echo "Overleap" || echo "Kaitu")
S3_PREFIX="${BRAND}"
CDN_PRIMARY="https://d13jc1jqzlg4yt.cloudfront.net/${BRAND}"
if [ "$BRAND" = "overleap" ]; then
    APPSTORE_URL="${OVERLEAP_APPSTORE_URL:-https://apps.apple.com/app/id6759199298}"
else
    APPSTORE_URL="https://apps.apple.com/app/id6448744655"
fi

if [ -n "$PLATFORM" ] && [ "$PLATFORM" != "android" ] && [ "$PLATFORM" != "ios" ]; then
    echo "ERROR: Invalid platform '${PLATFORM}'. Must be 'android' or 'ios'." >&2
    exit 1
fi

# Overleap Android ships through Google Play only: no CDN APK, no android
# manifest (the app's APK self-update lane is off — see k2_apk_updates in
# mobile/android/app/src/overleap/res/values/brand.xml). Placed before the
# artifact validation so that --platform=both narrows to iOS first.
# exit 0: expected state, CI legs stay green.
if [ "$BRAND" = overleap ] && [ "$PLATFORM" != ios ]; then
    echo "WARN: overleap android is Play-only — skipping android manifest."
    if [ "$PLATFORM" = android ]; then
        echo "Nothing published (overleap android is Play-only)."
        exit 0
    fi
    PLATFORM="ios"
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

# --- Mainland mirror (kaitu android) ---
# Every installed client opens the manifest's APK url in the system browser —
# including old builds (0.4.7) whose update code can never change again. With
# the VPN on, CloudFront is proxy-routed under the cn rule, so a user whose
# tunnel is degraded cannot download the update at all (ticket #3902).
# cdn.jsdmirror.com is listed in v2fly geolocation-cn (direct under the cn rule)
# and serves jsDelivr /gh/ paths from mainland nodes.
#
# Each APK goes into kaitu-io/app-dist as a content-addressed tag pointing at an
# orphan commit that holds only that file, and the manifest points at the tag:
# the same url can never serve different bytes, however long a mirror caches.
# Env overrides exist for scripts/test-publish-mobile.sh only.
if [ -n "${APP_DIST_REMOTE+x}" ]; then APP_DIST_REMOTE_SET=true; else APP_DIST_REMOTE_SET=false; fi
APP_DIST_REMOTE="${APP_DIST_REMOTE:-git@github.com:kaitu-io/app-dist.git}"
APP_DIST_CDN_BASE="${APP_DIST_CDN_BASE:-https://cdn.jsdmirror.com/gh/kaitu-io/app-dist}"
APP_DIST_RETRY_DELAY="${APP_DIST_RETRY_DELAY:-20}"
# jsDelivr does not serve single /gh/ files over 20 MB.
APP_DIST_MAX_BYTES=20000000

# Local mock-S3 runs (tests) only mirror when given a remote explicitly.
MIRROR=false
if [ "$BRAND" = kaitu ] && [ "$NO_MIRROR" = false ]; then
    if ! use_local || [ "$APP_DIST_REMOTE_SET" = true ]; then
        MIRROR=true
    fi
fi

# mirror_apk FILE FILENAME SHA256_HEX SIZE — prints the mirror url on stdout.
mirror_apk() {
    local local_file="$1" filename="$2" sha="$3" size="$4"
    if [ "$size" -gt "$APP_DIST_MAX_BYTES" ]; then
        echo "ERROR: $filename is $size bytes; the mirror serves at most $APP_DIST_MAX_BYTES (jsDelivr /gh/ file limit)." >&2
        echo "       Shrink the APK, or rerun with --no-mirror (users on a degraded tunnel then cannot download the update)." >&2
        return 1
    fi
    local tag="android-${VERSION}-${sha:0:12}"
    local path="android/${VERSION}/${filename}"
    local url="${APP_DIST_CDN_BASE}@${tag}/${path}"

    if [ "$DRY_RUN" = true ] && ! use_local; then
        echo "[dry-run] Would push $path to $APP_DIST_REMOTE as tag $tag" >&2
        echo "$url"
        return 0
    fi

    if git ls-remote --exit-code --tags "$APP_DIST_REMOTE" "refs/tags/$tag" >/dev/null 2>&1; then
        echo "  Mirror tag $tag already exists (content-addressed) — reusing" >&2
    else
        local repo="$WORK_TMPDIR/app-dist"
        rm -rf "$repo"
        mkdir -p "$repo/$(dirname "$path")"
        cp "$local_file" "$repo/$path"
        git -c init.defaultBranch=dist init -q "$repo"
        git -C "$repo" add "$path"
        git -C "$repo" -c user.name="k2app publish" -c user.email="noreply@kaitu.io" \
            -c commit.gpgsign=false commit -q -m "$path"
        git -C "$repo" -c tag.gpgsign=false tag "$tag"
        git -C "$repo" push -q "$APP_DIST_REMOTE" "refs/tags/$tag" >&2
        echo "  Pushed mirror tag $tag" >&2
    fi

    # Read it back through the mirror: the manifest must never point at bytes
    # nobody has seen served.
    local got="$WORK_TMPDIR/mirror-check" i
    for i in 1 2 3 4 5 6; do
        if curl -fsSL --max-time 300 -o "$got" "$url" 2>/dev/null \
            && [ "$(shasum -a 256 "$got" | cut -d' ' -f1)" = "$sha" ]; then
            echo "  ✓ Mirror serves identical bytes: $url" >&2
            echo "$url"
            return 0
        fi
        echo "  Mirror not serving the exact bytes yet (attempt $i/6)" >&2
        [ "$i" -lt 6 ] && sleep "$APP_DIST_RETRY_DELAY"
    done
    echo "ERROR: $url never served sha256 $sha — manifest not published." >&2
    return 1
}

WORK_TMPDIR=$(mktemp -d)
trap 'rm -rf "$WORK_TMPDIR"' EXIT

# Define artifact paths (CI uploads to {channel}/{VERSION}/)
android_artifact="android/${VERSION}/${BRAND_PRODUCT}-${VERSION}.apk"

# Validate artifacts exist
echo "Validating artifacts for v${VERSION}..."
if [ "$PLATFORM" != "ios" ]; then
    if ! check_artifact "$android_artifact"; then
        echo "ERROR: Missing artifact: $android_artifact" >&2
        echo "Aborting: artifact missing. Run CI build first." >&2
        exit 1
    else
        echo "  ✓ $android_artifact"
    fi
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

    # Relative URL: VERSION/filename (resolved against manifest baseURL by client);
    # kaitu android uses the absolute mainland-mirror url instead.
    local rel_url="${VERSION}/${filename}"
    if [ "$channel" = android ] && [ "$MIRROR" = true ]; then
        rel_url=$(mirror_apk "$local_file" "$filename" "${hash#sha256:}" "$size") || exit 1
    elif [ "$channel" = android ] && [ "$BRAND" = kaitu ] && [ "$NO_MIRROR" = true ]; then
        echo "  WARN: --no-mirror — users on a degraded tunnel cannot download this update (ticket #3902)." >&2
    fi

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

if [ "$PLATFORM" != "ios" ]; then
    generate_manifest "android" "$android_artifact" ',
  "min_android": 26'
fi

# --- iOS manifest (metadata only, no artifact) ---
# Note: iOS clients only read ios/latest.json (no beta path awareness).
# Beta iOS distribution is handled by TestFlight, not our manifest system.
# For beta versions, we write ios/beta/latest.json (unused but consistent).
# For stable versions, we write both ios/latest.json and ios/beta/latest.json.


if [ "$PLATFORM" != "android" ]; then
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
fi  # end platform != android

# --- CloudFront CDN invalidation ---

if [ "$DRY_RUN" = false ] && ! use_local; then
    CDN_ID_D0="${CLOUDFRONT_DISTRIBUTION_ID:-E3W144CRNT652P}"
    CDN_ID_DL="E34P52R7B93FSC"
    echo ""
    echo "Invalidating CDN caches..."
    CDN_PATHS=()
    [ "$PLATFORM" != "ios" ] && CDN_PATHS+=("/${BRAND}/android/*")
    [ "$PLATFORM" != "android" ] && CDN_PATHS+=("/${BRAND}/ios/*")
    for DIST_ID in "$CDN_ID_D0" "$CDN_ID_DL"; do
        aws cloudfront create-invalidation \
            --distribution-id "$DIST_ID" \
            --paths "${CDN_PATHS[@]}" \
            --no-cli-pager --output text > /dev/null
    done
    echo "CDN invalidated: d0.all7.cc + dl.kaitu.io"
fi

PLATFORM_LABEL="${PLATFORM:-mobile}"
echo ""
if [ "$CHANNEL" = "beta" ]; then
    echo "Published ${PLATFORM_LABEL} v${VERSION} beta manifests successfully."
else
    echo "Published ${PLATFORM_LABEL} v${VERSION} manifests successfully."
fi
if [ "$DRY_RUN" = true ]; then
    echo "(dry-run mode — no actual S3 uploads)"
fi
