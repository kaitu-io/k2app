#!/usr/bin/env bash
# T2 — macOS NE helper static library test script
# Verifies K2NEHelper.swift, k2_ne_helper.h, and build.sh structure

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
NE_DIR="$ROOT_DIR/desktop/src-tauri/ne_helper"

SWIFT_FILE="$NE_DIR/K2NEHelper.swift"
HEADER_FILE="$NE_DIR/k2_ne_helper.h"
BUILD_SCRIPT="$NE_DIR/build.sh"

PASS=0
FAIL=0

pass() {
    local name="$1"
    echo "  PASS: $name"
    ((PASS++))
}

fail() {
    local name="$1"
    local reason="$2"
    echo "  FAIL: $name — $reason"
    ((FAIL++))
}

assert_contains() {
    local file="$1"
    local pattern="$2"
    local test_name="$3"
    if grep -q "$pattern" "$file" 2>/dev/null; then
        pass "$test_name"
    else
        fail "$test_name" "pattern not found: $pattern"
    fi
}

assert_file_exists() {
    local file="$1"
    local test_name="$2"
    if [ -f "$file" ]; then
        pass "$test_name"
    else
        fail "$test_name" "file not found: $file"
    fi
}

assert_executable() {
    local file="$1"
    local test_name="$2"
    if [ -x "$file" ]; then
        pass "$test_name"
    else
        fail "$test_name" "file not executable: $file"
    fi
}

echo "=============================="
echo " T2 NE Helper Tests"
echo "=============================="
echo ""
echo "Target: $NE_DIR"
echo ""

# Test 1: k2ne_install symbol
assert_contains "$SWIFT_FILE" '@_cdecl("k2ne_install")' \
    "test_k2ne_install_symbol_exists"

# Test 2: k2ne_start symbol
assert_contains "$SWIFT_FILE" '@_cdecl("k2ne_start")' \
    "test_k2ne_start_symbol_exists"

# Test 3: k2ne_stop symbol
assert_contains "$SWIFT_FILE" '@_cdecl("k2ne_stop")' \
    "test_k2ne_stop_symbol_exists"

# Test 4: sendProviderMessage + JSON serialization
assert_contains "$SWIFT_FILE" 'sendProviderMessage' \
    "test_k2ne_status_returns_json (sendProviderMessage present)"
assert_contains "$SWIFT_FILE" 'JSONSerialization' \
    "test_k2ne_status_returns_json (JSONSerialization present)"

# Test 5: ServiceResponse envelope format
assert_contains "$SWIFT_FILE" '"code"' \
    "test_k2ne_status_returns_service_response_envelope (code key)"
assert_contains "$SWIFT_FILE" '"message"' \
    "test_k2ne_status_returns_service_response_envelope (message key)"
assert_contains "$SWIFT_FILE" '"data"' \
    "test_k2ne_status_returns_service_response_envelope (data key)"

# Test 6: fallback using mapVPNStatus
assert_contains "$SWIFT_FILE" 'mapVPNStatus' \
    "test_k2ne_status_fallback_when_ne_inactive"

# Test 7: k2ne_reinstall symbol
assert_contains "$SWIFT_FILE" '@_cdecl("k2ne_reinstall")' \
    "test_k2ne_reinstall_symbol_exists"

# Test 8: k2ne_set_state_callback symbol
assert_contains "$SWIFT_FILE" '@_cdecl("k2ne_set_state_callback")' \
    "test_k2ne_set_state_callback_exists"

# Test 9: k2ne_free_string symbol
assert_contains "$SWIFT_FILE" '@_cdecl("k2ne_free_string")' \
    "test_k2ne_free_string_exists"

# Test 10: header declares all functions
assert_file_exists "$HEADER_FILE" "test_header_declares_all_functions (file exists)"
assert_contains "$HEADER_FILE" 'k2ne_install' \
    "test_header_declares_all_functions (k2ne_install)"
assert_contains "$HEADER_FILE" 'k2ne_start' \
    "test_header_declares_all_functions (k2ne_start)"
assert_contains "$HEADER_FILE" 'k2ne_stop' \
    "test_header_declares_all_functions (k2ne_stop)"
assert_contains "$HEADER_FILE" 'k2ne_status' \
    "test_header_declares_all_functions (k2ne_status)"
assert_contains "$HEADER_FILE" 'k2ne_reinstall' \
    "test_header_declares_all_functions (k2ne_reinstall)"
assert_contains "$HEADER_FILE" 'k2ne_set_state_callback' \
    "test_header_declares_all_functions (k2ne_set_state_callback)"
assert_contains "$HEADER_FILE" 'k2ne_free_string' \
    "test_header_declares_all_functions (k2ne_free_string)"

# Test 11: build.sh exists and is executable
assert_file_exists "$BUILD_SCRIPT" "test_build_script_exists (file exists)"
assert_executable "$BUILD_SCRIPT" "test_build_script_exists (is executable)"
assert_contains "$BUILD_SCRIPT" 'swiftc' \
    "test_build_script_exists (invokes swiftc)"
assert_contains "$BUILD_SCRIPT" 'K2NEHelper.swift' \
    "test_build_script_exists (references K2NEHelper.swift)"

# Test 12: bundle ID
assert_contains "$SWIFT_FILE" 'io.kaitu.desktop.tunnel' \
    "test_bundle_id_matches"

echo ""
echo "=============================="
echo " Results: $PASS passed, $FAIL failed"
echo "=============================="

if [ "$FAIL" -gt 0 ]; then
    exit 1
fi
exit 0
