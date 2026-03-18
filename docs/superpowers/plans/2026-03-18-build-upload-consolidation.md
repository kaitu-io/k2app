# Build → Upload → CDN Consolidation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Every `build-*` target completes with S3 upload + dual CDN invalidation. Remove separate upload targets. Consolidate duplicate logic into one shared script.

**Architecture:** Create `scripts/ci/upload-release.sh` — shared script that uploads `release/{VERSION}/` to S3 and invalidates both CloudFront distributions. Build scripts/targets call it at the end. CI workflows replace inline upload steps with the same script.

**Tech Stack:** Bash, AWS CLI (s3, cloudfront)

---

## CDN Distribution IDs

| Distribution | Domain | Current Usage |
|---|---|---|
| `E3W144CRNT652P` | `d13jc1jqzlg4yt.cloudfront.net` / `d0.all7.cc` | Existing — used in publish scripts |
| `E34P52R7B93FSC` | `dl.kaitu.io` | NEW — same S3 origin, not yet invalidated |

Both share the same S3 origin: `d0.all7.cc.s3.ap-northeast-1.amazonaws.com`

## File Structure

| File | Action | Responsibility |
|---|---|---|
| `scripts/ci/upload-release.sh` | **Create** | Shared: S3 upload + dual CDN invalidation |
| `scripts/ci/upload-mobile-s3.sh` | **Delete** | Replaced by upload-release.sh |
| `scripts/build-macos.sh` | **Modify** (end) | Add upload-release.sh call |
| `Makefile` | **Modify** | `build-windows`, `build-android` add upload call |
| `.github/workflows/release-desktop.yml` | **Modify** | Replace 3 inline upload steps + CDN step with upload-release.sh |
| `.github/workflows/build-mobile.yml` | **Modify** | Replace upload-mobile-s3.sh calls with upload-release.sh |
| `scripts/publish-desktop.sh` | **Modify** | Add dl.kaitu.io CDN invalidation |
| `scripts/publish-mobile.sh` | **Modify** | Add dl.kaitu.io CDN invalidation |
| `CLAUDE.md` | **Modify** | Update commands section |

---

### Task 1: Create shared upload-release.sh

**Files:**
- Create: `scripts/ci/upload-release.sh`

- [ ] **Step 1: Write the script**

```bash
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
# Environment:
#   AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY — required
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
EXTRA_S3_ARGS=""

for arg in "$@"; do
  case "$arg" in
    --desktop)  PLATFORM="desktop" ;;
    --android)  PLATFORM="android" ;;
    --web)      PLATFORM="web" ;;
    --skip-cdn) SKIP_CDN=true ;;
    --include=*|--exclude=*) EXTRA_S3_ARGS="$EXTRA_S3_ARGS $arg" ;;
    *) echo "Unknown argument: $arg" >&2; exit 1 ;;
  esac
done

if [ -z "$PLATFORM" ]; then
  echo "Usage: $0 --desktop|--android|--web [--skip-cdn]" >&2
  exit 1
fi

S3_DEST="${S3_BUCKET}/${PLATFORM}/${VERSION}"
INVALIDATION_PATH="/kaitu/${PLATFORM}/${VERSION}/*"

echo "=== Uploading ${PLATFORM} v${VERSION} to S3 ==="

case "$PLATFORM" in
  desktop)
    if [ ! -d "release/${VERSION}" ]; then
      echo "ERROR: release/${VERSION}/ not found. Run build first." >&2; exit 1
    fi
    aws s3 cp "release/${VERSION}/" "${S3_DEST}/" --recursive $EXTRA_S3_ARGS
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
```

- [ ] **Step 2: Make executable**

```bash
chmod +x scripts/ci/upload-release.sh
```

- [ ] **Step 3: Commit**

```bash
git add scripts/ci/upload-release.sh
git commit -m "feat: add shared upload-release.sh for S3 upload + dual CDN invalidation"
```

---

### Task 2: Wire build-macos to upload

**Files:**
- Modify: `scripts/build-macos.sh` (end of file, after artifact collection)

- [ ] **Step 1: Add upload call at end of build-macos.sh**

After the final `ls -la` block, add:

```bash
# --- Upload to S3 + CDN invalidation ---
echo ""
echo "--- Uploading to S3 ---"
bash "$ROOT_DIR/scripts/ci/upload-release.sh" --desktop
```

- [ ] **Step 2: Commit**

---

### Task 3: Wire build-windows to upload

**Files:**
- Modify: `Makefile` (`build-windows` target)

- [ ] **Step 1: Add upload call at end of build-windows**

After the `ls -la` line, add:

```makefile
	bash scripts/ci/upload-release.sh --desktop \
		--exclude "*" --include "*.exe" --include "*.exe.sig"
```

- [ ] **Step 2: Commit**

---

### Task 4: Wire build-android to upload

**Files:**
- Modify: `Makefile` (`build-android` target)

- [ ] **Step 1: Add upload call at end of build-android**

After the `ls -la` line, add:

```makefile
	bash scripts/ci/upload-release.sh --android
```

- [ ] **Step 2: Commit**

---

### Task 5: Update CI desktop workflow

**Files:**
- Modify: `.github/workflows/release-desktop.yml`

- [ ] **Step 1: Replace 3 platform-specific upload steps + CDN step with single step**

Replace the "Upload macOS to S3", "Upload Windows to S3", "Upload Linux to S3", and "Invalidate CloudFront CDN cache" steps with:

```yaml
      - name: Upload to S3 + invalidate CDN
        if: success()
        env:
          AWS_ACCESS_KEY_ID: ${{ secrets.AWS_ACCESS_KEY_ID }}
          AWS_SECRET_ACCESS_KEY: ${{ secrets.AWS_SECRET_ACCESS_KEY }}
          AWS_DEFAULT_REGION: ap-northeast-1
        run: |
          bash scripts/ci/upload-release.sh --desktop
          # Linux: also upload k2 binary alongside tar.gz (used by install-linux.sh)
          if [ "${{ matrix.platform }}" = "Linux" ]; then
            VERSION=$(node -p "require('./package.json').version")
            aws s3 cp "desktop/src-tauri/binaries/k2-x86_64-unknown-linux-gnu" \
              "s3://d0.all7.cc/kaitu/desktop/${VERSION}/k2-linux-amd64"
          fi
```

Note: Desktop uploads everything recursively (all platforms produce different file types in `release/{VERSION}/`), so no platform-specific filtering needed — each CI runner only has its own platform's files in the directory.

- [ ] **Step 2: Commit**

---

### Task 6: Update CI mobile workflow

**Files:**
- Modify: `.github/workflows/build-mobile.yml`

- [ ] **Step 1: Replace upload-mobile-s3.sh calls**

In iOS job, replace:
```yaml
        run: bash scripts/ci/upload-mobile-s3.sh --web
```
with:
```yaml
        run: bash scripts/ci/upload-release.sh --web
```

In Android job, replace:
```yaml
        run: bash scripts/ci/upload-mobile-s3.sh --android
```
with:
```yaml
        run: bash scripts/ci/upload-release.sh --android
```

- [ ] **Step 2: Commit**

---

### Task 7: Delete upload-mobile-s3.sh

**Files:**
- Delete: `scripts/ci/upload-mobile-s3.sh`

- [ ] **Step 1: Remove the file**

```bash
git rm scripts/ci/upload-mobile-s3.sh
```

- [ ] **Step 2: Commit**

---

### Task 8: Add dl.kaitu.io CDN to publish scripts

**Files:**
- Modify: `scripts/publish-desktop.sh` (CDN invalidation section)
- Modify: `scripts/publish-mobile.sh` (CDN invalidation section)

- [ ] **Step 1: Update publish-desktop.sh**

Replace the CDN invalidation block (lines 186-195) with dual-CDN:

```bash
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
```

- [ ] **Step 2: Update publish-mobile.sh**

Replace the CDN invalidation block (lines 245-253) with dual-CDN:

```bash
if [ "$DRY_RUN" = false ] && ! use_local; then
    CDN_ID_D0="${CLOUDFRONT_DISTRIBUTION_ID:-E3W144CRNT652P}"
    CDN_ID_DL="E34P52R7B93FSC"
    echo ""
    echo "Invalidating CDN caches..."
    for DIST_ID in "$CDN_ID_D0" "$CDN_ID_DL"; do
        aws cloudfront create-invalidation \
            --distribution-id "$DIST_ID" \
            --paths "/kaitu/android/*" "/kaitu/web/*" "/kaitu/ios/*" \
            --no-cli-pager --output text > /dev/null
    done
    echo "CDN invalidated: d0.all7.cc + dl.kaitu.io"
fi
```

- [ ] **Step 3: Commit**

---

### Task 9: Clean up Makefile and docs

**Files:**
- Modify: `Makefile` (remove `publish-desktop` if redundant, update comments)
- Modify: `CLAUDE.md` (update commands section)

- [ ] **Step 1: Verify no remaining references to upload-mobile-s3.sh**

```bash
grep -r "upload-mobile-s3" .
```

- [ ] **Step 2: Update CLAUDE.md commands to reflect new flow**

Add to Quick Commands:
```
make build-android               # Build APK + upload to S3 + CDN invalidation
make publish-mobile VERSION=x.y.z  # Generate + upload mobile latest.json (phase 2 release)
```

- [ ] **Step 3: Commit**
