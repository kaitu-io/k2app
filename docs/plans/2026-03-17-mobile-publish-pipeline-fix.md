# Mobile Publish Pipeline Fix

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `make publish-mobile` the single human-controlled release gate for mobile, with CI only uploading build artifacts.

**Architecture:** CI (`upload-mobile-s3.sh`) strips manifest generation — only uploads APK + webapp.zip to versioned S3 directories. `publish-mobile.sh` becomes the sole manifest publisher: validates artifacts on S3, computes hashes, generates `latest.json` for android/web/ios, copies to beta directory, invalidates CDN. Slack notifications switch to CloudFront CDN URLs and iOS uses App Store URL.

**Tech Stack:** Bash scripts, AWS CLI, GitHub Actions YAML

---

## Current vs Target State

```
BEFORE (broken):
  CI: upload artifacts + generate latest.json (auto-publish, bypasses human)
  publish-mobile.sh: wrong S3 bucket, no iOS, never succeeds

AFTER (correct):
  CI: upload artifacts ONLY (android/{VER}/Kaitu-{VER}.apk, web/{VER}/webapp.zip)
  make publish-mobile: validate → hash → latest.json → beta copy → CDN invalidation
```

## Files

| File | Action | Purpose |
|------|--------|---------|
| `scripts/ci/upload-mobile-s3.sh` | Rewrite | Strip manifest generation, keep artifact upload only |
| `scripts/publish-mobile.sh` | Fix | Correct S3 bucket, add iOS manifest, add CDN invalidation |
| `.github/workflows/build-mobile.yml` | Edit | Remove CDN invalidation (moved to publish), fix Slack URLs |
| `.github/workflows/release-desktop.yml` | Edit | Fix Slack CDN URLs |

---

## Task 1: Simplify CI upload script (artifact-only)

Strip `upload-mobile-s3.sh` to only upload build artifacts. No manifest generation, no channel logic.

**Files:**
- Rewrite: `scripts/ci/upload-mobile-s3.sh`

- [ ] **Step 1: Rewrite `upload-mobile-s3.sh`**

```bash
#!/usr/bin/env bash
set -euo pipefail

# Upload mobile build artifacts to S3 versioned directories.
# Manifests are NOT generated here — use `make publish-mobile` for release.
#
# Usage:
#   bash scripts/ci/upload-mobile-s3.sh --android     # Upload APK
#   bash scripts/ci/upload-mobile-s3.sh --web          # Upload webapp.zip
#   bash scripts/ci/upload-mobile-s3.sh --all          # Both
#
# Required env vars:
#   AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY, AWS_DEFAULT_REGION

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(dirname "$(dirname "$SCRIPT_DIR")")"
cd "$ROOT_DIR"

S3_BUCKET="s3://d0.all7.cc/kaitu"
VERSION=$(node -p "require('./package.json').version")

UPLOAD_ANDROID=false
UPLOAD_WEB=false

for arg in "$@"; do
  case "$arg" in
    --android) UPLOAD_ANDROID=true ;;
    --web)     UPLOAD_WEB=true ;;
    --all)     UPLOAD_ANDROID=true; UPLOAD_WEB=true ;;
    --channel=*) ;; # ignored — kept for CI backwards compat, publish-mobile handles channels
    --ios)     ;; # ignored — iOS has no artifact to upload (App Store only)
    *) echo "Unknown argument: $arg"; exit 1 ;;
  esac
done

if ! $UPLOAD_ANDROID && ! $UPLOAD_WEB; then
  echo "Error: specify --android, --web, or --all"
  exit 1
fi

echo "Uploading artifacts for v${VERSION}..."

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

  aws s3 cp "$WEB_ZIP" "${S3_BUCKET}/web/${VERSION}/webapp.zip"
  echo "Uploaded: web/${VERSION}/webapp.zip"
  rm -f "$WEB_ZIP"
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
  aws s3 cp "$APK_PATH" "${S3_BUCKET}/android/${VERSION}/${APK_FILENAME}"
  echo "Uploaded: android/${VERSION}/${APK_FILENAME}"
fi

echo ""
echo "=== Artifact upload complete (no manifests — run 'make publish-mobile' to release) ==="
```

- [ ] **Step 2: Verify script syntax**

```bash
bash -n scripts/ci/upload-mobile-s3.sh && echo "syntax OK"
```

- [ ] **Step 3: Commit**

```bash
git add scripts/ci/upload-mobile-s3.sh
git commit -m "fix(ci): strip manifest generation from upload-mobile-s3.sh

CI now only uploads build artifacts (APK + webapp.zip) to versioned
S3 directories. Manifest generation (latest.json) is the sole
responsibility of 'make publish-mobile' — the human-controlled
release gate. --ios and --channel flags kept for backwards compat
but are no-ops."
```

---

## Task 2: Fix publish-mobile.sh (S3 bucket + iOS + CDN invalidation)

Fix the S3 bucket, add iOS manifest generation, add CloudFront invalidation, use relative URLs consistently.

**Files:**
- Rewrite: `scripts/publish-mobile.sh`

- [ ] **Step 1: Rewrite `publish-mobile.sh`**

```bash
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
```

- [ ] **Step 2: Verify script syntax**

```bash
bash -n scripts/publish-mobile.sh && echo "syntax OK"
```

- [ ] **Step 3: Run dry-run test**

```bash
scripts/publish-mobile.sh 0.4.0-beta.6 --dry-run
```
Expected: Shows `[dry-run] Would upload/copy ...` messages for android, web, ios manifests. No actual S3 operations.

- [ ] **Step 4: Run local mock test**

```bash
# Create mock S3 directory with fake artifacts
MOCK_DIR=$(mktemp -d)
mkdir -p "$MOCK_DIR/android/0.4.0-beta.6" "$MOCK_DIR/web/0.4.0-beta.6"
echo "fake-apk" > "$MOCK_DIR/android/0.4.0-beta.6/Kaitu-0.4.0-beta.6.apk"
echo "fake-zip" > "$MOCK_DIR/web/0.4.0-beta.6/webapp.zip"

# Run with mock S3
scripts/publish-mobile.sh 0.4.0-beta.6 --s3-base="$MOCK_DIR"

# Verify outputs
echo "--- beta manifests ---"
cat "$MOCK_DIR/android/beta/latest.json"
cat "$MOCK_DIR/web/beta/latest.json"
cat "$MOCK_DIR/ios/beta/latest.json"

echo "--- beta artifact copies ---"
ls "$MOCK_DIR/android/beta/0.4.0-beta.6/"
ls "$MOCK_DIR/web/beta/0.4.0-beta.6/"

# Verify stable manifests NOT created (beta version)
ls "$MOCK_DIR/android/latest.json" 2>&1 || echo "no stable android manifest (correct for beta)"
ls "$MOCK_DIR/web/latest.json" 2>&1 || echo "no stable web manifest (correct for beta)"

rm -rf "$MOCK_DIR"
```
Expected: Beta manifests created with relative URLs. Stable manifests NOT created. iOS manifest has `appstore_url`. Artifacts copied to beta directories.

- [ ] **Step 5: Commit**

```bash
git add scripts/publish-mobile.sh
git commit -m "fix(publish): correct S3 bucket, add iOS manifest, add CDN invalidation

- S3 bucket: kaitu-releases → d0.all7.cc/kaitu (matches CI upload)
- Added iOS manifest generation (version + appstore_url)
- Added CloudFront invalidation after publish
- Relative URLs for client resolveDownloadURL() compatibility
- Consistent artifact validation with checkmark output"
```

---

## Task 3: Remove CDN invalidation from CI workflow

CI should only invalidate for artifact paths (versioned directories), not manifest paths. Since CI no longer generates manifests, the broad wildcard invalidation is unnecessary — artifact uploads to new versioned paths don't need cache invalidation (they're new keys).

**Files:**
- Modify: `.github/workflows/build-mobile.yml` (remove CDN invalidation + channel detection + simplify upload flags)

- [ ] **Step 1: Remove iOS CDN invalidation step**

Delete lines 194-205 (the entire "Invalidate CloudFront CDN cache" step in `build-ios` job). The step is:
```yaml
      - name: Invalidate CloudFront CDN cache
        if: success()
        env:
          AWS_ACCESS_KEY_ID: ${{ secrets.AWS_ACCESS_KEY_ID }}
          AWS_SECRET_ACCESS_KEY: ${{ secrets.AWS_SECRET_ACCESS_KEY }}
          AWS_DEFAULT_REGION: ap-northeast-1
          DISTRIBUTION_ID: ${{ secrets.CLOUDFRONT_DISTRIBUTION_ID }}
        run: |
          VERSION=$(node -p "require('./package.json').version")
          aws cloudfront create-invalidation \
            --distribution-id "$DISTRIBUTION_ID" \
            --paths "/kaitu/web/*" "/kaitu/ios/*"
```

- [ ] **Step 2: Remove Android CDN invalidation step**

Delete lines 373-384 (the entire "Invalidate CloudFront CDN cache" step in `build-android` job). The step is:
```yaml
      - name: Invalidate CloudFront CDN cache
        if: success()
        env:
          AWS_ACCESS_KEY_ID: ${{ secrets.AWS_ACCESS_KEY_ID }}
          AWS_SECRET_ACCESS_KEY: ${{ secrets.AWS_SECRET_ACCESS_KEY }}
          AWS_DEFAULT_REGION: ap-northeast-1
          DISTRIBUTION_ID: ${{ secrets.CLOUDFRONT_DISTRIBUTION_ID }}
        run: |
          VERSION=$(node -p "require('./package.json').version")
          aws cloudfront create-invalidation \
            --distribution-id "$DISTRIBUTION_ID" \
            --paths "/kaitu/android/*"
```

- [ ] **Step 3: Update iOS CI upload step — remove --ios flag**

Change line 192 from:
```yaml
        run: bash scripts/ci/upload-mobile-s3.sh --web --ios --channel=${{ steps.channel.outputs.channel }}
```
to:
```yaml
        run: bash scripts/ci/upload-mobile-s3.sh --web
```
(`--ios` is now a no-op in the script, but removing it makes intent clear. `--channel` is also no longer needed.)

- [ ] **Step 4: Update Android CI upload step — remove --channel flag**

Change line 371 from:
```yaml
        run: bash scripts/ci/upload-mobile-s3.sh --android --channel=${{ steps.channel.outputs.channel }}
```
to:
```yaml
        run: bash scripts/ci/upload-mobile-s3.sh --android
```

- [ ] **Step 5: Remove "Detect release channel" steps (now unused)**

Delete the `Detect release channel` step in **both** `build-ios` (lines 174-185) and `build-android` (lines 353-364) jobs. These steps output `channel` which is no longer consumed.

iOS step to delete:
```yaml
      - name: Detect release channel
        id: channel
        shell: bash
        run: |
          VERSION=$(node -p "require('./package.json').version")
          if [[ "$VERSION" == *"-beta"* ]]; then
            CHANNEL="beta"
          else
            CHANNEL="stable"
          fi
          echo "channel=${CHANNEL}" >> "$GITHUB_OUTPUT"
          echo "Release channel: ${CHANNEL} (version: ${VERSION})"
```

Android step to delete: identical block.

- [ ] **Step 6: Commit**

```bash
git add .github/workflows/build-mobile.yml
git commit -m "fix(ci): remove manifest generation and CDN invalidation from mobile CI

CI now only uploads artifacts. Channel detection, manifest generation,
and CDN invalidation are the responsibility of 'make publish-mobile'.
Removed: CloudFront invalidation steps, --ios/--channel flags, channel
detection steps."
```

---

## Task 4: Fix Slack notification URLs (P1)

Two fixes: (1) CDN URLs use CloudFront instead of d0.all7.cc, (2) iOS Slack notification shows App Store URL.

**Files:**
- Modify: `.github/workflows/build-mobile.yml` (iOS + Android Slack steps)
- Modify: `.github/workflows/release-desktop.yml` (Desktop Slack step)

Note: Line numbers below reference the file AFTER Task 3 edits. Match by step name content, not line numbers.

- [ ] **Step 1: Fix iOS Slack notification — use App Store URL**

In the "Notify Slack on iOS success" step, change from:
```yaml
      - name: Notify Slack on iOS success
        if: success()
        run: |
          VERSION=$(node -p "require('./package.json').version")
          CDN_BASE="https://d0.all7.cc/kaitu/ios"
          ./scripts/ci/notify-slack.sh deploy-success \
            --version "${VERSION}" \
            --platforms "iOS" \
            --download-url "<${CDN_BASE}/Kaitu-${VERSION}.ipa|iOS IPA>"
        env:
          SLACK_WEBHOOK_RELEASE: ${{ secrets.SLACK_WEBHOOK_RELEASE }}
```
to:
```yaml
      - name: Notify Slack on iOS success
        if: success()
        run: |
          VERSION=$(node -p "require('./package.json').version")
          ./scripts/ci/notify-slack.sh deploy-success \
            --version "${VERSION}" \
            --platforms "iOS" \
            --download-url "<https://apps.apple.com/app/id6759199298|App Store>"
        env:
          SLACK_WEBHOOK_RELEASE: ${{ secrets.SLACK_WEBHOOK_RELEASE }}
```

- [ ] **Step 2: Fix Android Slack notification — use CloudFront CDN + fix path**

In the "Notify Slack on Android success" step, change:
```yaml
          CDN_BASE="https://d0.all7.cc/kaitu/android"
          ./scripts/ci/notify-slack.sh deploy-success \
            --version "${VERSION}" \
            --platforms "Android" \
            --download-url "<${CDN_BASE}/Kaitu-${VERSION}.apk|Android APK>"
```
to:
```yaml
          CDN_BASE="https://d13jc1jqzlg4yt.cloudfront.net/kaitu/android"
          ./scripts/ci/notify-slack.sh deploy-success \
            --version "${VERSION}" \
            --platforms "Android" \
            --download-url "<${CDN_BASE}/${VERSION}/Kaitu-${VERSION}.apk|Android APK>"
```
(Two fixes: CDN domain + add `${VERSION}/` path segment.)

- [ ] **Step 3: Fix Desktop Slack notification — use CloudFront CDN**

In `release-desktop.yml`, in the "Notify Slack on build success" step, change:
```yaml
          CDN_BASE="https://d0.all7.cc/kaitu/desktop/${VERSION}"
```
to:
```yaml
          CDN_BASE="https://d13jc1jqzlg4yt.cloudfront.net/kaitu/desktop/${VERSION}"
```

- [ ] **Step 4: Commit**

```bash
git add .github/workflows/build-mobile.yml .github/workflows/release-desktop.yml
git commit -m "fix(ci): use CloudFront CDN for Slack URLs, iOS shows App Store

- iOS Slack notification: IPA download link → App Store URL
- Android Slack: d0.all7.cc → d13jc1jqzlg4yt.cloudfront.net (CDN)
- Desktop Slack: d0.all7.cc → d13jc1jqzlg4yt.cloudfront.net (CDN)"
```

---

## Verification

After all tasks:

```bash
# Syntax check both scripts
bash -n scripts/ci/upload-mobile-s3.sh && echo "upload OK"
bash -n scripts/publish-mobile.sh && echo "publish OK"

# YAML syntax check
python3 -c "import yaml; yaml.safe_load(open('.github/workflows/build-mobile.yml'))" && echo "mobile yml OK"
python3 -c "import yaml; yaml.safe_load(open('.github/workflows/release-desktop.yml'))" && echo "desktop yml OK"

# End-to-end local mock test
MOCK_DIR=$(mktemp -d)
mkdir -p "$MOCK_DIR/android/0.4.0-beta.6" "$MOCK_DIR/web/0.4.0-beta.6"
dd if=/dev/urandom bs=1024 count=100 > "$MOCK_DIR/android/0.4.0-beta.6/Kaitu-0.4.0-beta.6.apk" 2>/dev/null
dd if=/dev/urandom bs=1024 count=50 > "$MOCK_DIR/web/0.4.0-beta.6/webapp.zip" 2>/dev/null

scripts/publish-mobile.sh 0.4.0-beta.6 --s3-base="$MOCK_DIR"

echo "=== Verify manifests ==="
echo "--- android beta ---"
python3 -m json.tool "$MOCK_DIR/android/beta/latest.json"
echo "--- web beta ---"
python3 -m json.tool "$MOCK_DIR/web/beta/latest.json"
echo "--- ios beta ---"
python3 -m json.tool "$MOCK_DIR/ios/beta/latest.json"

echo "=== Verify artifact copies ==="
ls -la "$MOCK_DIR/android/beta/0.4.0-beta.6/"
ls -la "$MOCK_DIR/web/beta/0.4.0-beta.6/"

echo "=== Verify stable NOT published (beta version) ==="
[ ! -f "$MOCK_DIR/android/latest.json" ] && echo "✓ no stable android"
[ ! -f "$MOCK_DIR/web/latest.json" ] && echo "✓ no stable web"
[ ! -f "$MOCK_DIR/ios/latest.json" ] && echo "✓ no stable ios"

echo "=== Verify manifest content ==="
# URL should be relative
grep -q '"url": "0.4.0-beta.6/' "$MOCK_DIR/android/beta/latest.json" && echo "✓ relative URL"
# Hash should be sha256:
grep -q '"hash": "sha256:' "$MOCK_DIR/android/beta/latest.json" && echo "✓ sha256 hash"
# iOS should have appstore_url
grep -q '"appstore_url": "https://apps.apple.com/' "$MOCK_DIR/ios/beta/latest.json" && echo "✓ appstore URL"
# min_android present
grep -q '"min_android": 26' "$MOCK_DIR/android/beta/latest.json" && echo "✓ min_android"

rm -rf "$MOCK_DIR"
echo "=== All checks passed ==="
```
