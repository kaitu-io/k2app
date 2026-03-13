#!/bin/bash
set -euo pipefail

# Sync local adb platform-tools to S3 if version differs from remote.
# Safe to run in CI (skips gracefully if local binaries absent).
# Never fails the build — errors produce warnings only.

# --- Config ---
S3_BUCKET="s3://d0.all7.cc/kaitu/android/tools"
CDN_BASE="https://d0.all7.cc/kaitu/android/tools"
LOCAL_ADB_DIR="tools/adb-platform-tools"
TOOLS_JSON="tools.json"

# --- Step 1: Check if local adb binaries exist ---
if [ ! -d "$LOCAL_ADB_DIR" ]; then
    echo "[sync-adb-tools] No local adb binaries found in $LOCAL_ADB_DIR, skipping"
    echo "[sync-adb-tools] To update: download Google platform-tools, extract adb, place in $LOCAL_ADB_DIR/"
    exit 0
fi

# --- Step 2: Read local version ---
LOCAL_VERSION=$(cat "$LOCAL_ADB_DIR/VERSION" 2>/dev/null || echo "unknown")

# --- Step 3: Fetch remote version ---
REMOTE_VERSION=$(curl -sf "$CDN_BASE/$TOOLS_JSON" | python3 -c "import sys,json; print(json.load(sys.stdin)['adb']['version'])" 2>/dev/null || echo "none")

# --- Step 4: Compare ---
if [ "$LOCAL_VERSION" = "$REMOTE_VERSION" ]; then
    echo "[sync-adb-tools] adb tools up-to-date (v$LOCAL_VERSION), skipping upload"
    exit 0
fi

echo "[sync-adb-tools] Local v$LOCAL_VERSION != Remote v$REMOTE_VERSION, uploading..."

# --- Step 5: Compute hashes + generate tools.json ---
DARWIN_ZIP="$LOCAL_ADB_DIR/platform-tools-darwin.zip"
WINDOWS_ZIP="$LOCAL_ADB_DIR/platform-tools-windows.zip"

json_entry() {
    local file="$1" name="$2"
    local hash size
    hash="sha256:$(shasum -a 256 "$file" | awk '{print $1}')"
    size=$(stat -f%z "$file" 2>/dev/null || stat --printf="%s" "$file")
    echo "\"url\": \"$name\", \"hash\": \"$hash\", \"size\": $size"
}

DARWIN_ENTRY=""
WINDOWS_ENTRY=""
[ -f "$DARWIN_ZIP" ]  && DARWIN_ENTRY=$(json_entry "$DARWIN_ZIP" "platform-tools-darwin.zip")
[ -f "$WINDOWS_ZIP" ] && WINDOWS_ENTRY=$(json_entry "$WINDOWS_ZIP" "platform-tools-windows.zip")

cat > "$LOCAL_ADB_DIR/$TOOLS_JSON" <<EOF
{
  "adb": {
    "version": "$LOCAL_VERSION",
    "files": {
      "darwin": { $DARWIN_ENTRY },
      "windows": { $WINDOWS_ENTRY }
    }
  }
}
EOF

# --- Step 6: Upload to S3 (original zips, unmodified) ---
if ! command -v aws &>/dev/null; then
    echo "[sync-adb-tools] WARNING: aws CLI not found, skipping upload"
    exit 0
fi

aws s3 cp "$LOCAL_ADB_DIR/$TOOLS_JSON" "$S3_BUCKET/$TOOLS_JSON" --content-type "application/json" || {
    echo "[sync-adb-tools] WARNING: S3 upload failed for tools.json"
    exit 0
}
[ -f "$DARWIN_ZIP" ]  && aws s3 cp "$DARWIN_ZIP"  "$S3_BUCKET/platform-tools-darwin.zip" || true
[ -f "$WINDOWS_ZIP" ] && aws s3 cp "$WINDOWS_ZIP" "$S3_BUCKET/platform-tools-windows.zip" || true

echo "[sync-adb-tools] Uploaded adb tools v$LOCAL_VERSION to S3"
