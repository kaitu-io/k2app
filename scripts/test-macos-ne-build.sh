#!/usr/bin/env bash
set -euo pipefail

# Test script for macOS NE xcframework build verification.
# Verifies that k2/build/K2MobileMacOS.xcframework was produced correctly
# by gomobile bind -target=macos.
#
# Usage: bash scripts/test-macos-ne-build.sh
# Expected: exits 0 after a successful `make mobile-macos` run
# Expected: exits non-zero (RED) when run before the build

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(dirname "$SCRIPT_DIR")"

cd "$ROOT_DIR"

# --- Colors and helpers ---
RED='\033[0;31m'
GREEN='\033[0;32m'
BOLD='\033[1m'
NC='\033[0m'

TOTAL=0
PASSED=0
FAILED=0
FAILURES=()

pass() {
  TOTAL=$((TOTAL + 1))
  PASSED=$((PASSED + 1))
  echo -e "  ${GREEN}PASS${NC} $1"
}

fail() {
  TOTAL=$((TOTAL + 1))
  FAILED=$((FAILED + 1))
  FAILURES+=("$1")
  echo -e "  ${RED}FAIL${NC} $1"
}

XCFRAMEWORK_DIR="k2/build/K2MobileMacOS.xcframework"

echo ""
echo -e "${BOLD}=== macOS NE xcframework Tests ===${NC}"

# test_xcframework_has_macos_slice
# Verifies that the xcframework contains the macos-arm64 slice directory,
# which gomobile bind -target=macos produces for Apple Silicon.
echo ""
echo -e "${BOLD}--- test_xcframework_has_macos_slice ---${NC}"

MACOS_SLICE_DIR="${XCFRAMEWORK_DIR}/macos-arm64"

if [ -d "$XCFRAMEWORK_DIR" ]; then
  pass "xcframework root exists: $XCFRAMEWORK_DIR"
else
  fail "xcframework root missing: $XCFRAMEWORK_DIR (run: make mobile-macos)"
fi

if [ -d "$MACOS_SLICE_DIR" ]; then
  pass "macos-arm64 slice directory exists: $MACOS_SLICE_DIR"
else
  fail "macos-arm64 slice directory missing: $MACOS_SLICE_DIR"
fi

# test_xcframework_has_header
# Verifies that the xcframework contains a K2Mobile.h header file,
# which the macOS NE appex needs to import the gomobile bindings.
echo ""
echo -e "${BOLD}--- test_xcframework_has_header ---${NC}"

# gomobile generates headers inside the slice under Headers/
if [ -d "$XCFRAMEWORK_DIR" ]; then
  HEADER_FILE=$(find "$XCFRAMEWORK_DIR" -name "K2Mobile.h" 2>/dev/null | head -1)
  if [ -n "$HEADER_FILE" ]; then
    pass "K2Mobile.h header found: $HEADER_FILE"
  else
    fail "K2Mobile.h header not found inside $XCFRAMEWORK_DIR"
  fi
else
  fail "K2Mobile.h header not found inside $XCFRAMEWORK_DIR (xcframework missing)"
fi

# Additional: verify Info.plist exists (valid xcframework structure)
INFO_PLIST="${XCFRAMEWORK_DIR}/Info.plist"
if [ -f "$INFO_PLIST" ]; then
  pass "Info.plist exists at xcframework root"
else
  fail "Info.plist missing from xcframework root: $INFO_PLIST"
fi

# Additional: verify at least one .dylib or .a is present in the macos slice
if [ -d "$MACOS_SLICE_DIR" ]; then
  LIB_COUNT=$(find "$MACOS_SLICE_DIR" \( -name "*.dylib" -o -name "*.a" \) 2>/dev/null | wc -l | tr -d ' ')
  if [ "$LIB_COUNT" -gt 0 ]; then
    pass "macos-arm64 slice contains $LIB_COUNT library file(s)"
  else
    fail "macos-arm64 slice contains no .dylib or .a library files"
  fi
else
  fail "macos-arm64 slice missing, cannot verify library files"
fi

# --- Summary ---
echo ""
echo -e "${BOLD}=== Summary ===${NC}"
echo -e "  ${PASSED}/${TOTAL} checks passed"

if [ "$FAILED" -gt 0 ]; then
  echo ""
  echo -e "  ${RED}Failures:${NC}"
  for f in "${FAILURES[@]}"; do
    echo -e "    - $f"
  done
  echo ""
  exit 1
else
  echo -e "  ${GREEN}All checks passed!${NC}"
  echo ""
  exit 0
fi
