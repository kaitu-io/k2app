#!/usr/bin/env bash
# Test suite for publish-mobile.sh
# Validates that the publish script:
#   1. Exists and is executable
#   2. Fails when S3 artifacts are missing
#   3. Generates valid JSON manifests with required fields
#   4. Uses relative URLs (not absolute) so manifests work with any CDN base
#      — unless the kaitu android mainland mirror is on (Test 11)
#   5. Generates iOS manifest with appstore_url
#   6. CI workflow has S3 upload steps targeting versioned directories
#
# Usage:
#   bash scripts/test-publish-mobile.sh

set -uo pipefail
# NOTE: -e intentionally omitted — test assertions use non-zero exit codes

PASS=0
FAIL=0

test_result() {
    if [ "$1" -eq 0 ]; then
        echo "  PASS: $2"
        PASS=$((PASS + 1))
    else
        echo "  FAIL: $2"
        FAIL=$((FAIL + 1))
    fi
}

WORK_TMPDIR=$(mktemp -d)
trap 'rm -rf "$WORK_TMPDIR"' EXIT

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(dirname "$SCRIPT_DIR")"
PUBLISH_SCRIPT="$SCRIPT_DIR/publish-mobile.sh"

echo "=== publish-mobile.sh test suite ==="
echo ""

# ---------------------------------------------------------------------------
# Test 1: Script exists and is executable
# ---------------------------------------------------------------------------
echo "--- Test 1: Script existence ---"
if test -x "$PUBLISH_SCRIPT" 2>/dev/null; then
    test_result 0 "publish-mobile.sh exists and is executable"
else
    test_result 1 "publish-mobile.sh exists and is executable"
fi

# ---------------------------------------------------------------------------
# Test 2: Fails when version artifacts are missing in S3
# ---------------------------------------------------------------------------
echo "--- Test 2: Missing artifact detection ---"
MOCK_S3="$WORK_TMPDIR/mock-s3"
mkdir -p "$MOCK_S3/kaitu/android"
mkdir -p "$MOCK_S3/kaitu/web"

if [ -x "$PUBLISH_SCRIPT" ]; then
    "$PUBLISH_SCRIPT" "99.99.99" --s3-base="$MOCK_S3/kaitu" --dry-run >/dev/null 2>&1
    EC=$?
    if [ "$EC" -ne 0 ]; then
        test_result 0 "fails with exit code != 0 when artifacts missing"
    else
        test_result 1 "fails with exit code != 0 when artifacts missing"
    fi
else
    # Script doesn't exist — that's a failure
    test_result 1 "fails with exit code != 0 when artifacts missing"
fi

# ---------------------------------------------------------------------------
# Test 3: Generates valid Android manifest
# ---------------------------------------------------------------------------
echo "--- Test 3: Android manifest generation ---"
mkdir -p "$MOCK_S3/kaitu/android/0.5.0"
echo "fake-apk-binary-content-for-hash-test" > "$MOCK_S3/kaitu/android/0.5.0/Kaitu-0.5.0.apk"


if [ -x "$PUBLISH_SCRIPT" ]; then
    "$PUBLISH_SCRIPT" "0.5.0" --s3-base="$MOCK_S3/kaitu" --dry-run >/dev/null 2>&1 || true
fi

ANDROID_MANIFEST="$MOCK_S3/kaitu/android/latest.json"
if [ -f "$ANDROID_MANIFEST" ]; then
    python3 -c "
import json, sys
m = json.load(open('$ANDROID_MANIFEST'))
required = ['version', 'url', 'hash', 'size', 'released_at']
missing = [k for k in required if k not in m]
if missing:
    print(f'  Missing fields: {missing}', file=sys.stderr)
    sys.exit(1)
sys.exit(0)
"
    test_result $? "android latest.json has all required fields (version, url, hash, size, released_at)"
else
    test_result 1 "android latest.json has all required fields (version, url, hash, size, released_at)"
fi

# ---------------------------------------------------------------------------
# Test 4: Android URL is relative (not absolute)
# ---------------------------------------------------------------------------
echo "--- Test 4: Relative URL format ---"
if [ -f "$ANDROID_MANIFEST" ]; then
    python3 -c "
import json, sys
m = json.load(open('$ANDROID_MANIFEST'))
url = m.get('url', '')
if url.startswith('http://') or url.startswith('https://'):
    print(f'  URL is absolute: {url}', file=sys.stderr)
    sys.exit(1)
if not url:
    print('  URL is empty', file=sys.stderr)
    sys.exit(1)
# Expect pattern like: 0.5.0/Kaitu-0.5.0.apk
if '0.5.0/' not in url:
    print(f'  URL does not contain version directory: {url}', file=sys.stderr)
    sys.exit(1)
sys.exit(0)
"
    test_result $? "android url is relative (e.g. '0.5.0/Kaitu-0.5.0.apk')"
else
    test_result 1 "android url is relative (e.g. '0.5.0/Kaitu-0.5.0.apk')"
fi

# ---------------------------------------------------------------------------
# Test 5: iOS manifest generation
# ---------------------------------------------------------------------------
echo "--- Test 5: iOS manifest generation ---"
IOS_MANIFEST="$MOCK_S3/kaitu/ios/latest.json"
if [ -f "$IOS_MANIFEST" ]; then
    python3 -c "
import json, sys
m = json.load(open('$IOS_MANIFEST'))
required = ['version', 'appstore_url', 'released_at']
missing = [k for k in required if k not in m]
if missing:
    print(f'  Missing fields: {missing}', file=sys.stderr)
    sys.exit(1)
sys.exit(0)
"
    test_result $? "ios latest.json has all required fields (version, appstore_url, released_at)"
else
    test_result 1 "ios latest.json has all required fields (version, appstore_url, released_at)"
fi

# ---------------------------------------------------------------------------
# Test 6: Hash field uses sha256: prefix
# ---------------------------------------------------------------------------
echo "--- Test 6: Hash format ---"
if [ -f "$ANDROID_MANIFEST" ]; then
    python3 -c "
import json, sys
m = json.load(open('$ANDROID_MANIFEST'))
h = m.get('hash', '')
if not h.startswith('sha256:'):
    print(f'  Hash missing sha256: prefix: {h}', file=sys.stderr)
    sys.exit(1)
hex_part = h[len('sha256:'):]
if len(hex_part) != 64:
    print(f'  Hash hex part wrong length ({len(hex_part)}): {hex_part}', file=sys.stderr)
    sys.exit(1)
sys.exit(0)
"
    test_result $? "hash uses sha256: prefix with 64-char hex"
else
    test_result 1 "hash uses sha256: prefix with 64-char hex"
fi

# ---------------------------------------------------------------------------
# Test 8: Version in manifest matches requested version
# ---------------------------------------------------------------------------
echo "--- Test 7: Version consistency ---"
if [ -f "$ANDROID_MANIFEST" ]; then
    python3 -c "
import json, sys
m = json.load(open('$ANDROID_MANIFEST'))
if m.get('version') != '0.5.0':
    print(f'  Version mismatch: expected 0.5.0, got {m.get(\"version\")}', file=sys.stderr)
    sys.exit(1)
sys.exit(0)
"
    test_result $? "manifest version matches requested version (0.5.0)"
else
    test_result 1 "manifest version matches requested version (0.5.0)"
fi

# ---------------------------------------------------------------------------
# Test 9: CI workflow has S3 upload steps targeting versioned directories
# ---------------------------------------------------------------------------
echo "--- Test 8: CI workflow S3 upload configuration ---"
CI_WORKFLOW="$ROOT_DIR/.github/workflows/build-mobile.yml"
if [ -f "$CI_WORKFLOW" ]; then
    python3 -c "
import sys
content = open('$CI_WORKFLOW').read()
# Check that the workflow calls the S3 upload script
has_s3_upload = 'upload-release.sh' in content
if not has_s3_upload:
    print('  CI workflow does not reference upload-release.sh', file=sys.stderr)
    sys.exit(1)
# Check android upload path exists
has_android = '--android' in content
if not has_android:
    print('  CI workflow missing --android upload step', file=sys.stderr)
    sys.exit(1)
sys.exit(0)
"
    test_result $? "CI workflow has S3 upload steps for versioned directories"
else
    test_result 1 "CI workflow has S3 upload steps for versioned directories"
fi

# ---------------------------------------------------------------------------
# Test 10: CI upload script writes to versioned S3 paths (not root)
# ---------------------------------------------------------------------------
echo "--- Test 9: CI upload uses versioned S3 paths ---"
UPLOAD_SCRIPT="$ROOT_DIR/scripts/ci/upload-release.sh"
if [ -f "$UPLOAD_SCRIPT" ]; then
    python3 -c "
import sys
content = open('$UPLOAD_SCRIPT').read()
# Verify the upload script uses versioned paths like /android/\${VERSION}/
# and /web/\${VERSION}/ (not just root)
has_android_versioned = 'android/\${VERSION}' in content or 'android/\$VERSION' in content
if not has_android_versioned:
    print('  upload script missing versioned android path', file=sys.stderr)
    sys.exit(1)
sys.exit(0)
"
    test_result $? "CI upload script targets versioned S3 directories"
else
    test_result 1 "CI upload script targets versioned S3 directories"
fi

# ---------------------------------------------------------------------------
# Test 10: Overleap Android is Play-only — no CDN manifest, exit 0
# ---------------------------------------------------------------------------
echo "--- Test 10: overleap android is Play-only ---"
# No android artifact is staged for overleap on purpose: the script must not
# even look for one (it exits before validation with a Play-only notice).
mkdir -p "$MOCK_S3/overleap"
OUT=$("$PUBLISH_SCRIPT" "0.5.0" --brand=overleap --platform=android --s3-base="$MOCK_S3/overleap" --dry-run 2>&1)
EC=$?
if [ "$EC" -eq 0 ] && echo "$OUT" | grep -q "Play-only"; then
    test_result 0 "overleap --platform=android exits 0 with a Play-only notice"
else
    echo "    exit=$EC output: $OUT" | head -5
    test_result 1 "overleap --platform=android exits 0 with a Play-only notice"
fi
if [ ! -f "$MOCK_S3/overleap/android/latest.json" ] && [ ! -f "$MOCK_S3/overleap/android/beta/latest.json" ]; then
    test_result 0 "overleap android manifests are never written"
else
    test_result 1 "overleap android manifests are never written"
fi
# --platform unset (both) narrows to iOS; with OVERLEAP_APPSTORE_URL set the iOS
# manifest is still published and the android one still is not.
OUT=$(OVERLEAP_APPSTORE_URL="https://apps.apple.com/app/id0000000000" "$PUBLISH_SCRIPT" "0.5.0" --brand=overleap --s3-base="$MOCK_S3/overleap" --dry-run 2>&1)
EC=$?
if [ "$EC" -eq 0 ] && [ -f "$MOCK_S3/overleap/ios/latest.json" ] && [ ! -f "$MOCK_S3/overleap/android/latest.json" ]; then
    test_result 0 "overleap (both platforms) publishes ios manifest only"
else
    echo "    exit=$EC output: $OUT" | head -5
    test_result 1 "overleap (both platforms) publishes ios manifest only"
fi

# Without OVERLEAP_APPSTORE_URL the iOS manifest still publishes, with the
# listing built from overleap's App Store Connect app id.
rm -f "$MOCK_S3/overleap/ios/latest.json"
OUT=$("$PUBLISH_SCRIPT" "0.5.0" --brand=overleap --platform=ios --s3-base="$MOCK_S3/overleap" --dry-run 2>&1)
EC=$?
if [ "$EC" -eq 0 ] && python3 -c "import json,sys; d=json.load(open(sys.argv[1])); sys.exit(0 if d['appstore_url']=='https://apps.apple.com/app/id6759199298' else 1)" "$MOCK_S3/overleap/ios/latest.json" 2>/dev/null; then
    test_result 0 "overleap ios manifest defaults to the App Store app id 6759199298"
else
    echo "    exit=$EC output: $OUT" | head -5
    test_result 1 "overleap ios manifest defaults to the App Store app id 6759199298"
fi

# ---------------------------------------------------------------------------
# Test 11: kaitu android mainland mirror (push tag → read back → absolute url)
# ---------------------------------------------------------------------------
echo "--- Test 11: kaitu android mainland mirror ---"
# A bare repo stands in for kaitu-io/app-dist; a tiny HTTP server stands in for
# jsDelivr's /gh/<repo>@<tag>/<path>, answering from the bare repo by tag.
# FAKE_CDN_CORRUPT=1 makes it serve altered bytes.
MIRROR_REMOTE="$WORK_TMPDIR/app-dist.git"
git init -q --bare "$MIRROR_REMOTE"
CDN_PORT_FILE="$WORK_TMPDIR/cdn.port"
cat > "$WORK_TMPDIR/fake_cdn.py" <<'PY'
import http.server, os, re, subprocess, sys
repo, port_file = sys.argv[1], sys.argv[2]
class H(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        m = re.match(r"^/gh/[^/]+/[^@]+@([^/]+)/(.+)$", self.path)
        if not m:
            self.send_error(404); return
        r = subprocess.run(["git", "--git-dir", repo, "show", f"{m.group(1)}:{m.group(2)}"], capture_output=True)
        if r.returncode != 0:
            self.send_error(404); return
        body = r.stdout + (b"x" if os.path.exists(repo + "/CORRUPT") else b"")
        self.send_response(200); self.send_header("Content-Length", str(len(body))); self.end_headers()
        self.wfile.write(body)
    def log_message(self, *a): pass
s = http.server.HTTPServer(("127.0.0.1", 0), H)
open(port_file, "w").write(str(s.server_address[1]))
s.serve_forever()
PY
python3 "$WORK_TMPDIR/fake_cdn.py" "$MIRROR_REMOTE" "$CDN_PORT_FILE" &
CDN_PID=$!
for _ in $(seq 1 50); do [ -s "$CDN_PORT_FILE" ] && break; sleep 0.1; done
CDN_BASE="http://127.0.0.1:$(cat "$CDN_PORT_FILE")/gh/kaitu-io/app-dist"

mirror_publish() {  # mirror_publish VERSION [extra args...]
    local v="$1"; shift
    APP_DIST_REMOTE="$MIRROR_REMOTE" APP_DIST_CDN_BASE="$CDN_BASE" APP_DIST_RETRY_DELAY=0 \
        "$PUBLISH_SCRIPT" "$v" --platform=android --s3-base="$MOCK_S3/kaitu" "$@" 2>&1
}

MIRROR_S3="$MOCK_S3/kaitu/android/0.6.0"
mkdir -p "$MIRROR_S3"
head -c 4096 /dev/urandom > "$MIRROR_S3/Kaitu-0.6.0.apk"
APK_SHA=$(shasum -a 256 "$MIRROR_S3/Kaitu-0.6.0.apk" | cut -d' ' -f1)
WANT_URL="$CDN_BASE@android-0.6.0-${APK_SHA:0:12}/android/0.6.0/Kaitu-0.6.0.apk"

OUT=$(mirror_publish 0.6.0)
EC=$?
GOT_URL=$(python3 -c "import json,sys; print(json.load(open(sys.argv[1]))['url'])" "$MOCK_S3/kaitu/android/latest.json" 2>/dev/null)
if [ "$EC" -eq 0 ] && [ "$GOT_URL" = "$WANT_URL" ]; then
    test_result 0 "manifest url is the content-addressed mirror url"
else
    echo "    exit=$EC url=$GOT_URL want=$WANT_URL"; echo "$OUT" | tail -5
    test_result 1 "manifest url is the content-addressed mirror url"
fi
# The tag must hold exactly the APK bytes, and nothing else.
if [ "$(git --git-dir "$MIRROR_REMOTE" show "android-0.6.0-${APK_SHA:0:12}:android/0.6.0/Kaitu-0.6.0.apk" | shasum -a 256 | cut -d' ' -f1)" = "$APK_SHA" ] \
    && [ "$(git --git-dir "$MIRROR_REMOTE" ls-tree -r --name-only "android-0.6.0-${APK_SHA:0:12}")" = "android/0.6.0/Kaitu-0.6.0.apk" ]; then
    test_result 0 "mirror tag holds exactly the APK"
else
    test_result 1 "mirror tag holds exactly the APK"
fi

# Re-publishing the same bytes reuses the tag instead of failing on push.
OUT=$(mirror_publish 0.6.0)
EC=$?
if [ "$EC" -eq 0 ] && echo "$OUT" | grep -q "already exists"; then
    test_result 0 "re-publishing identical bytes reuses the existing tag"
else
    echo "    exit=$EC"; echo "$OUT" | tail -5
    test_result 1 "re-publishing identical bytes reuses the existing tag"
fi

# A mirror serving different bytes must block the manifest.
mkdir -p "$MOCK_S3/kaitu/android/0.6.1"
head -c 4096 /dev/urandom > "$MOCK_S3/kaitu/android/0.6.1/Kaitu-0.6.1.apk"
touch "$MIRROR_REMOTE/CORRUPT"
OUT=$(mirror_publish 0.6.1)
EC=$?
rm -f "$MIRROR_REMOTE/CORRUPT"
LATEST_VER=$(python3 -c "import json,sys; print(json.load(open(sys.argv[1]))['version'])" "$MOCK_S3/kaitu/android/latest.json" 2>/dev/null)
if [ "$EC" -ne 0 ] && [ "$LATEST_VER" = "0.6.0" ]; then
    test_result 0 "mirror serving wrong bytes fails and leaves latest.json untouched"
else
    echo "    exit=$EC latest=$LATEST_VER"; echo "$OUT" | tail -5
    test_result 1 "mirror serving wrong bytes fails and leaves latest.json untouched"
fi

# Over the jsDelivr file limit: fail loudly rather than silently fall back.
mkdir -p "$MOCK_S3/kaitu/android/0.6.2"
head -c 20000001 /dev/zero > "$MOCK_S3/kaitu/android/0.6.2/Kaitu-0.6.2.apk"
OUT=$(mirror_publish 0.6.2)
EC=$?
LATEST_VER=$(python3 -c "import json,sys; print(json.load(open(sys.argv[1]))['version'])" "$MOCK_S3/kaitu/android/latest.json" 2>/dev/null)
if [ "$EC" -ne 0 ] && echo "$OUT" | grep -q "20000000" && [ "$LATEST_VER" = "0.6.0" ]; then
    test_result 0 "APK over 20 MB fails the publish"
else
    echo "    exit=$EC latest=$LATEST_VER"; echo "$OUT" | tail -5
    test_result 1 "APK over 20 MB fails the publish"
fi

# --no-mirror is the explicit escape hatch: relative url plus a warning.
OUT=$(mirror_publish 0.6.2 --no-mirror)
EC=$?
GOT_URL=$(python3 -c "import json,sys; print(json.load(open(sys.argv[1]))['url'])" "$MOCK_S3/kaitu/android/latest.json" 2>/dev/null)
if [ "$EC" -eq 0 ] && [ "$GOT_URL" = "0.6.2/Kaitu-0.6.2.apk" ] && echo "$OUT" | grep -q "WARN: --no-mirror"; then
    test_result 0 "--no-mirror publishes the relative url with a warning"
else
    echo "    exit=$EC url=$GOT_URL"; echo "$OUT" | tail -5
    test_result 1 "--no-mirror publishes the relative url with a warning"
fi
kill "$CDN_PID" 2>/dev/null

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------
echo ""
echo "==========================================="
echo "Results: $PASS passed, $FAIL failed out of $((PASS + FAIL)) tests"
echo "==========================================="
if [ "$FAIL" -eq 0 ]; then
    exit 0
else
    exit 1
fi
