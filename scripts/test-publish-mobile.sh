#!/usr/bin/env bash
# Test suite for publish-mobile.sh
# Validates that the publish script:
#   1. Exists and is executable
#   2. Fails when S3 artifacts are missing
#   3. Generates valid JSON manifests with required fields
#   4. Uses relative URLs (not absolute) so manifests work with any CDN base
#   5. Generates webapp manifest separately from Android
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

mkdir -p "$MOCK_S3/kaitu/web/0.5.0"
echo "fake-webapp-zip-content-for-hash-test" > "$MOCK_S3/kaitu/web/0.5.0/webapp.zip"

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
# Test 5: Generates valid webapp manifest
# ---------------------------------------------------------------------------
echo "--- Test 5: Web OTA manifest generation ---"
WEB_MANIFEST="$MOCK_S3/kaitu/web/latest.json"
if [ -f "$WEB_MANIFEST" ]; then
    python3 -c "
import json, sys
m = json.load(open('$WEB_MANIFEST'))
required = ['version', 'url', 'hash', 'size', 'released_at']
missing = [k for k in required if k not in m]
if missing:
    print(f'  Missing fields: {missing}', file=sys.stderr)
    sys.exit(1)
sys.exit(0)
"
    test_result $? "web latest.json has all required fields"
else
    test_result 1 "web latest.json has all required fields"
fi

# ---------------------------------------------------------------------------
# Test 6: Web URL is also relative
# ---------------------------------------------------------------------------
echo "--- Test 6: Web URL relative format ---"
if [ -f "$WEB_MANIFEST" ]; then
    python3 -c "
import json, sys
m = json.load(open('$WEB_MANIFEST'))
url = m.get('url', '')
if url.startswith('http://') or url.startswith('https://'):
    print(f'  URL is absolute: {url}', file=sys.stderr)
    sys.exit(1)
if not url:
    print('  URL is empty', file=sys.stderr)
    sys.exit(1)
# Expect pattern like: 0.5.0/webapp.zip
if '0.5.0/' not in url:
    print(f'  URL does not contain version directory: {url}', file=sys.stderr)
    sys.exit(1)
sys.exit(0)
"
    test_result $? "web url is relative (e.g. '0.5.0/webapp.zip')"
else
    test_result 1 "web url is relative (e.g. '0.5.0/webapp.zip')"
fi

# ---------------------------------------------------------------------------
# Test 7: Hash field uses sha256: prefix
# ---------------------------------------------------------------------------
echo "--- Test 7: Hash format ---"
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
echo "--- Test 8: Version consistency ---"
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
echo "--- Test 9: CI workflow S3 upload configuration ---"
CI_WORKFLOW="$ROOT_DIR/.github/workflows/build-mobile.yml"
if [ -f "$CI_WORKFLOW" ]; then
    python3 -c "
import sys
content = open('$CI_WORKFLOW').read()
# Check that the workflow calls the S3 upload script
has_s3_upload = 'upload-mobile-s3.sh' in content
if not has_s3_upload:
    print('  CI workflow does not reference upload-mobile-s3.sh', file=sys.stderr)
    sys.exit(1)
# Check both android and web+ios paths exist
has_android = '--android' in content
has_web = '--web' in content or '--all' in content
if not has_android:
    print('  CI workflow missing --android upload step', file=sys.stderr)
    sys.exit(1)
if not has_web:
    print('  CI workflow missing --web upload step', file=sys.stderr)
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
echo "--- Test 10: CI upload uses versioned S3 paths ---"
UPLOAD_SCRIPT="$ROOT_DIR/scripts/ci/upload-mobile-s3.sh"
if [ -f "$UPLOAD_SCRIPT" ]; then
    python3 -c "
import sys
content = open('$UPLOAD_SCRIPT').read()
# Verify the upload script uses versioned paths like /android/\${VERSION}/
# and /web/\${VERSION}/ (not just root)
has_android_versioned = 'android/\${VERSION}' in content or 'android/\$VERSION' in content
has_web_versioned = 'web/\${VERSION}' in content or 'web/\$VERSION' in content
if not has_android_versioned:
    print('  upload script missing versioned android path', file=sys.stderr)
    sys.exit(1)
if not has_web_versioned:
    print('  upload script missing versioned web path', file=sys.stderr)
    sys.exit(1)
sys.exit(0)
"
    test_result $? "CI upload script targets versioned S3 directories"
else
    test_result 1 "CI upload script targets versioned S3 directories"
fi

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
