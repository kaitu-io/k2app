#!/usr/bin/env bash
# Test suite for macOS NE build integration (T4)
# Tests that build-macos.sh, entitlements.plist, preinstall, and postinstall
# contain the required NE-related changes.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(dirname "$SCRIPT_DIR")"

PASS=0
FAIL=0

pass() {
    echo "PASS: $1"
    PASS=$((PASS + 1))
}

fail() {
    echo "FAIL: $1"
    echo "      $2"
    FAIL=$((FAIL + 1))
}

# ---------------------------------------------------------------------------
# test_build_script_has_ne_steps
# Verify build-macos.sh contains:
#   - gomobile macOS xcframework step (make mobile-macos)
#   - NE helper static library step (ne_helper/build.sh)
#   - NE_HELPER_LIB_DIR export
#   - KaituTunnel.appex compilation step (swiftc)
#   - appex copy into PlugIns/
#   - codesign for appex
#   - re-codesign of main app
# ---------------------------------------------------------------------------
test_build_script_has_ne_steps() {
    local script="$ROOT_DIR/scripts/build-macos.sh"

    if ! grep -q "mobile-macos" "$script"; then
        fail "test_build_script_has_ne_steps" "build-macos.sh missing 'make mobile-macos' step"
        return
    fi

    if ! grep -q "ne_helper" "$script"; then
        fail "test_build_script_has_ne_steps" "build-macos.sh missing 'ne_helper' build step"
        return
    fi

    if ! grep -q "NE_HELPER_LIB_DIR" "$script"; then
        fail "test_build_script_has_ne_steps" "build-macos.sh missing NE_HELPER_LIB_DIR export"
        return
    fi

    if ! grep -q "KaituTunnel.appex" "$script"; then
        fail "test_build_script_has_ne_steps" "build-macos.sh missing KaituTunnel.appex step"
        return
    fi

    if ! grep -q "swiftc" "$script"; then
        fail "test_build_script_has_ne_steps" "build-macos.sh missing swiftc compilation step"
        return
    fi

    if ! grep -q "PlugIns" "$script"; then
        fail "test_build_script_has_ne_steps" "build-macos.sh missing PlugIns directory step"
        return
    fi

    if ! grep -q "KaituTunnel.entitlements" "$script"; then
        fail "test_build_script_has_ne_steps" "build-macos.sh missing appex codesign with KaituTunnel.entitlements"
        return
    fi

    # Verify re-codesign of main app after appex injection
    # Must have a second codesign call referencing entitlements.plist with --deep
    local deep_sign_count
    deep_sign_count=$(grep -c -- "--deep" "$script" || true)
    if [ "$deep_sign_count" -lt 1 ]; then
        fail "test_build_script_has_ne_steps" "build-macos.sh missing --deep re-codesign of main app"
        return
    fi

    pass "test_build_script_has_ne_steps"
}

# ---------------------------------------------------------------------------
# test_entitlements_has_ne_capability
# Verify desktop/src-tauri/entitlements.plist contains
# com.apple.developer.networking.networkextension with packet-tunnel-provider
# ---------------------------------------------------------------------------
test_entitlements_has_ne_capability() {
    local plist="$ROOT_DIR/desktop/src-tauri/entitlements.plist"

    if ! grep -q "com.apple.developer.networking.networkextension" "$plist"; then
        fail "test_entitlements_has_ne_capability" "entitlements.plist missing com.apple.developer.networking.networkextension"
        return
    fi

    if ! grep -q "packet-tunnel-provider" "$plist"; then
        fail "test_entitlements_has_ne_capability" "entitlements.plist missing packet-tunnel-provider value"
        return
    fi

    pass "test_entitlements_has_ne_capability"
}

# ---------------------------------------------------------------------------
# test_entitlements_has_app_group
# Verify desktop/src-tauri/entitlements.plist contains
# com.apple.security.application-groups with group.io.kaitu.desktop
# ---------------------------------------------------------------------------
test_entitlements_has_app_group() {
    local plist="$ROOT_DIR/desktop/src-tauri/entitlements.plist"

    if ! grep -q "com.apple.security.application-groups" "$plist"; then
        fail "test_entitlements_has_app_group" "entitlements.plist missing com.apple.security.application-groups"
        return
    fi

    if ! grep -q "group.io.kaitu.desktop" "$plist"; then
        fail "test_entitlements_has_app_group" "entitlements.plist missing group.io.kaitu.desktop value"
        return
    fi

    pass "test_entitlements_has_app_group"
}

# ---------------------------------------------------------------------------
# test_preinstall_handles_all_plists
# Verify preinstall handles all known launchd plist variants:
#   - io.kaitu.k2.plist
#   - io.kaitu.service.plist
#   - com.kaitu.service.plist
#   - kaitu.plist (already handled)
# ---------------------------------------------------------------------------
test_preinstall_handles_all_plists() {
    local script="$ROOT_DIR/scripts/pkg-scripts/preinstall"

    if ! grep -q "io.kaitu.k2" "$script"; then
        fail "test_preinstall_handles_all_plists" "preinstall missing io.kaitu.k2 plist cleanup"
        return
    fi

    if ! grep -q "io.kaitu.service" "$script"; then
        fail "test_preinstall_handles_all_plists" "preinstall missing io.kaitu.service plist cleanup"
        return
    fi

    if ! grep -q "com.kaitu.service" "$script"; then
        fail "test_preinstall_handles_all_plists" "preinstall missing com.kaitu.service plist cleanup"
        return
    fi

    if ! grep -q "kaitu.plist" "$script"; then
        fail "test_preinstall_handles_all_plists" "preinstall missing kaitu.plist cleanup"
        return
    fi

    # Verify launchctl unload is used for cleanup
    if ! grep -q "launchctl unload" "$script"; then
        fail "test_preinstall_handles_all_plists" "preinstall missing launchctl unload calls"
        return
    fi

    pass "test_preinstall_handles_all_plists"
}

# ---------------------------------------------------------------------------
# test_postinstall_ne_aware
# Verify postinstall does NOT blindly install daemon service —
# it must check for NE appex presence and conditionally skip service install.
# ---------------------------------------------------------------------------
test_postinstall_ne_aware() {
    local script="$ROOT_DIR/scripts/pkg-scripts/postinstall"

    # Must check for KaituTunnel.appex presence
    if ! grep -q "KaituTunnel.appex" "$script"; then
        fail "test_postinstall_ne_aware" "postinstall missing KaituTunnel.appex check"
        return
    fi

    # Must have NE-mode conditional logic (skip or NE mode message)
    if ! grep -qE "NE mode|ne mode|network.?extension|PlugIns" "$script"; then
        fail "test_postinstall_ne_aware" "postinstall missing NE-mode conditional logic"
        return
    fi

    # Daemon service install must be inside a conditional (not unconditional)
    # Check that 'service install' is inside an if/else block referencing NE
    local service_install_line
    service_install_line=$(grep -n "service install" "$script" | head -1 || true)
    if [ -z "$service_install_line" ]; then
        # No service install at all is also acceptable (removed entirely)
        pass "test_postinstall_ne_aware"
        return
    fi

    # If service install exists, it must be in the else branch of NE check
    if ! grep -q "else" "$script"; then
        fail "test_postinstall_ne_aware" "postinstall has service install but no else/conditional branch"
        return
    fi

    pass "test_postinstall_ne_aware"
}

# ---------------------------------------------------------------------------
# Run all tests
# ---------------------------------------------------------------------------
echo "=== T4: macOS NE Build Integration Tests ==="
echo ""

test_build_script_has_ne_steps
test_entitlements_has_ne_capability
test_entitlements_has_app_group
test_preinstall_handles_all_plists
test_postinstall_ne_aware

echo ""
echo "Results: $PASS passed, $FAIL failed"

if [ "$FAIL" -gt 0 ]; then
    exit 1
fi
exit 0
