#!/usr/bin/env bash
# T8 RED: Verify slog migration — fails if old log. calls remain, passes after migration.
set -uo pipefail

SIDECAR_DIR="$(cd "$(dirname "$0")" && pwd)"
PASS=0
FAIL=0

check() {
    local name="$1"
    local result="$2"  # "pass" or "fail"
    if [ "$result" = "pass" ]; then
        echo "  PASS: $name"
        PASS=$((PASS + 1))
    else
        echo "  FAIL: $name"
        FAIL=$((FAIL + 1))
    fi
}

echo "=== T8 slog migration tests ==="

# Test 1: No old log.Printf/Println/Fatalf calls remain
echo ""
echo "--- Test: no old log. calls ---"
OLD_LOG_COUNT=$(grep -rn 'log\.Printf\|log\.Println\|log\.Fatalf\|log\.Print\b' \
    "$SIDECAR_DIR" --include='*.go' | grep -v '_test\.go' | wc -l | tr -d ' ') || OLD_LOG_COUNT=0
if [ "$OLD_LOG_COUNT" -eq 0 ]; then
    check "no old log.Printf/Println/Fatalf calls" "pass"
else
    check "no old log.Printf/Println/Fatalf calls (found $OLD_LOG_COUNT)" "fail"
    grep -rn 'log\.Printf\|log\.Println\|log\.Fatalf\|log\.Print\b' \
        "$SIDECAR_DIR" --include='*.go' | grep -v '_test\.go' | head -20 || true
fi

# Test 2: No import "log" (stdlib) remains — only "log/slog" is allowed
echo ""
echo "--- Test: no import log (only log/slog) ---"
OLD_IMPORT_COUNT=$(grep -rn '"log"' \
    "$SIDECAR_DIR" --include='*.go' | grep -v '_test\.go' | wc -l | tr -d ' ') || OLD_IMPORT_COUNT=0
if [ "$OLD_IMPORT_COUNT" -eq 0 ]; then
    check "no 'import log' (old stdlib)" "pass"
else
    check "no 'import log' (old stdlib) — found $OLD_IMPORT_COUNT" "fail"
    grep -rn '"log"' "$SIDECAR_DIR" --include='*.go' | grep -v '_test\.go' | head -20 || true
fi

# Test 3: slog calls are present
echo ""
echo "--- Test: slog calls are present ---"
SLOG_COUNT=$(grep -rn 'slog\.' \
    "$SIDECAR_DIR" --include='*.go' | grep -v '_test\.go' | wc -l | tr -d ' ') || SLOG_COUNT=0
if [ "$SLOG_COUNT" -gt 0 ]; then
    check "slog. calls present ($SLOG_COUNT calls)" "pass"
else
    check "slog. calls present (none found)" "fail"
fi

# Test 4: Build compiles
echo ""
echo "--- Test: go build compiles ---"
BUILD_OUTPUT=""
BUILD_EXIT=0
BUILD_OUTPUT=$(cd "$SIDECAR_DIR" && go build ./... 2>&1) || BUILD_EXIT=$?
if [ "$BUILD_EXIT" -eq 0 ]; then
    check "go build ./... succeeds" "pass"
else
    check "go build ./... succeeds" "fail"
    echo "    Build output: $BUILD_OUTPUT"
fi

# Test 5: slog.SetDefault called in main.go
echo ""
echo "--- Test: slog.SetDefault in main.go ---"
if grep -q 'slog\.SetDefault' "$SIDECAR_DIR/main.go"; then
    check "slog.SetDefault configured in main()" "pass"
else
    check "slog.SetDefault configured in main()" "fail"
fi

# Test 6: log.SetFlags removed from main.go (old pattern)
echo ""
echo "--- Test: log.SetFlags removed ---"
if grep -q 'log\.SetFlags' "$SIDECAR_DIR/main.go" 2>/dev/null; then
    check "log.SetFlags removed" "fail"
else
    check "log.SetFlags removed" "pass"
fi

echo ""
echo "=== Results: PASS=$PASS FAIL=$FAIL ==="
if [ "$FAIL" -gt 0 ]; then
    exit 1
fi
exit 0
