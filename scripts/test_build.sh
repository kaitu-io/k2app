#!/usr/bin/env bash
set -e
# E2E build verification script for k2app.
# Validates the build pipeline without running the full app.

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(dirname "$SCRIPT_DIR")"

cd "$ROOT_DIR"

# --- Colors and helpers ---
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BOLD='\033[1m'
NC='\033[0m' # No Color

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

skip() {
  echo -e "  ${YELLOW}SKIP${NC} $1"
}

section() {
  echo ""
  echo -e "${BOLD}=== $1 ===${NC}"
}

# ============================================================
# 1. Version consistency
# ============================================================
section "Version Consistency"

PKG_VERSION=$(node -p "require('./package.json').version")
echo "  package.json version: $PKG_VERSION"

if [ "$PKG_VERSION" = "0.4.0" ]; then
  pass "package.json version is 0.4.0"
else
  fail "package.json version: expected 0.4.0, got $PKG_VERSION"
fi

# Run make pre-build to generate version.json
if ! make pre-build > /dev/null 2>&1; then
  fail "make pre-build failed"
elif [ -f webapp/public/version.json ]; then
  VJSON=$(node -p "require('./webapp/public/version.json').version")
  if [ "$VJSON" = "$PKG_VERSION" ]; then
    pass "version.json matches package.json ($VJSON)"
  else
    fail "version.json mismatch: expected $PKG_VERSION, got $VJSON"
  fi
else
  fail "make pre-build did not create webapp/public/version.json"
fi

CARGO_VERSION=$(sed -n 's/^version = "\(.*\)"/\1/p' desktop/src-tauri/Cargo.toml | head -1)
if [ "$CARGO_VERSION" = "$PKG_VERSION" ]; then
  pass "Cargo.toml version matches package.json ($CARGO_VERSION)"
else
  fail "Cargo.toml version mismatch: expected $PKG_VERSION, got $CARGO_VERSION"
fi

# ============================================================
# 2. Webapp build
# ============================================================
section "Webapp Build"

echo "  Installing dependencies..."
yarn install --frozen-lockfile > /dev/null 2>&1 || yarn install > /dev/null 2>&1 || true

echo "  Building webapp..."
if (cd webapp && yarn build) > /dev/null 2>&1; then
  pass "yarn build succeeded"
else
  fail "yarn build failed"
fi

if [ -f webapp/dist/index.html ]; then
  pass "dist/index.html exists"
else
  fail "dist/index.html not found"
fi

JS_COUNT=$(find webapp/dist/assets -name '*.js' 2>/dev/null | wc -l | tr -d ' ')
CSS_COUNT=$(find webapp/dist/assets -name '*.css' 2>/dev/null | wc -l | tr -d ' ')

if [ "$JS_COUNT" -gt 0 ]; then
  pass "dist/assets/ contains JS files ($JS_COUNT)"
else
  fail "dist/assets/ contains no JS files"
fi

if [ "$CSS_COUNT" -gt 0 ]; then
  pass "dist/assets/ contains CSS files ($CSS_COUNT)"
else
  fail "dist/assets/ contains no CSS files"
fi

# ============================================================
# 3. k2 Go build
# ============================================================
section "k2 Go Build"

ARCH=$(uname -m)
# Normalize arch: arm64 stays as-is for the binary name
TARGET="${ARCH}-apple-darwin"
K2_BIN="desktop/src-tauri/binaries/k2-${TARGET}"

if command -v go &> /dev/null; then
  echo "  Building k2 for $TARGET..."
  if "$SCRIPT_DIR/build-k2.sh" "$TARGET" > /dev/null 2>&1; then
    pass "build-k2.sh succeeded for $TARGET"
  else
    fail "build-k2.sh failed for $TARGET"
  fi

  if [ -f "$K2_BIN" ]; then
    pass "k2 binary exists at $K2_BIN"
  else
    fail "k2 binary not found at $K2_BIN"
  fi

  if [ -x "$K2_BIN" ]; then
    pass "k2 binary is executable"
  else
    fail "k2 binary is not executable"
  fi
else
  skip "Go is not installed -- skipping k2 build"
fi

# ============================================================
# 4. Cargo check (Rust compilation)
# ============================================================
section "Cargo Check"

echo "  Running cargo check in desktop/src-tauri..."
if (cd desktop/src-tauri && cargo check) > /dev/null 2>&1; then
  pass "cargo check succeeded"
else
  fail "cargo check failed"
fi

# ============================================================
# 5. Test suites
# ============================================================
section "Test Suites"

echo "  Running vitest..."
if (cd webapp && yarn test) > /dev/null 2>&1; then
  pass "vitest passed"
else
  fail "vitest failed"
fi

echo "  Running cargo test..."
if (cd desktop/src-tauri && cargo test) > /dev/null 2>&1; then
  pass "cargo test passed"
else
  fail "cargo test failed"
fi

# ============================================================
# 6. TypeScript check
# ============================================================
section "TypeScript Check"

echo "  Running tsc --noEmit..."
if (cd webapp && npx tsc --noEmit) > /dev/null 2>&1; then
  pass "TypeScript check passed"
else
  fail "TypeScript check failed"
fi

# ============================================================
# Summary
# ============================================================
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
