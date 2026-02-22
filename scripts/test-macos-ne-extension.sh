#!/usr/bin/env bash
# T1: macOS NE App Extension verification tests
# Tests for KaituTunnel.appex — the macOS Network Extension target
# These are structure/content verification tests, not build tests.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

TUNNEL_DIR="$REPO_ROOT/desktop/src-tauri/KaituTunnel"
SWIFT_FILE="$TUNNEL_DIR/PacketTunnelProvider.swift"
PLIST_FILE="$TUNNEL_DIR/Info.plist"
ENTITLEMENTS_FILE="$TUNNEL_DIR/KaituTunnel.entitlements"

PASS=0
FAIL=0

pass() {
    echo "PASS: $1"
    PASS=$((PASS + 1))
}

fail() {
    echo "FAIL: $1"
    FAIL=$((FAIL + 1))
}

# Test 1: Info.plist has correct NSExtensionPointIdentifier
test_info_plist_ne_point() {
    if [ ! -f "$PLIST_FILE" ]; then
        fail "test_info_plist_ne_point — Info.plist does not exist at $PLIST_FILE"
        return
    fi
    if grep -q "com.apple.networkextension.packet-tunnel" "$PLIST_FILE"; then
        pass "test_info_plist_ne_point — NSExtensionPointIdentifier = com.apple.networkextension.packet-tunnel"
    else
        fail "test_info_plist_ne_point — NSExtensionPointIdentifier not found or incorrect"
    fi
}

# Test 2: Entitlements has packet-tunnel-provider
test_entitlements_packet_tunnel() {
    if [ ! -f "$ENTITLEMENTS_FILE" ]; then
        fail "test_entitlements_packet_tunnel — KaituTunnel.entitlements does not exist at $ENTITLEMENTS_FILE"
        return
    fi
    if grep -q "com.apple.developer.networking.networkextension" "$ENTITLEMENTS_FILE" && \
       grep -q "packet-tunnel-provider" "$ENTITLEMENTS_FILE"; then
        pass "test_entitlements_packet_tunnel — entitlements has networkextension + packet-tunnel-provider"
    else
        fail "test_entitlements_packet_tunnel — missing com.apple.developer.networking.networkextension or packet-tunnel-provider"
    fi
}

# Test 3: Info.plist contains correct bundle identifier io.kaitu.desktop.tunnel
test_info_plist_bundle_id() {
    if [ ! -f "$PLIST_FILE" ]; then
        fail "test_info_plist_bundle_id — Info.plist does not exist"
        return
    fi
    if grep -q "io.kaitu.desktop.tunnel" "$PLIST_FILE"; then
        pass "test_info_plist_bundle_id — bundle ID io.kaitu.desktop.tunnel found"
    else
        fail "test_info_plist_bundle_id — bundle ID io.kaitu.desktop.tunnel not found in Info.plist"
    fi
}

# Test 4: PacketTunnelProvider.swift contains NEDNSSettings with matchDomains
test_ptp_has_dns_settings() {
    if [ ! -f "$SWIFT_FILE" ]; then
        fail "test_ptp_has_dns_settings — PacketTunnelProvider.swift does not exist at $SWIFT_FILE"
        return
    fi
    if grep -q "NEDNSSettings" "$SWIFT_FILE" && grep -q "matchDomains" "$SWIFT_FILE"; then
        pass "test_ptp_has_dns_settings — NEDNSSettings and matchDomains found"
    else
        fail "test_ptp_has_dns_settings — NEDNSSettings or matchDomains missing from PacketTunnelProvider.swift"
    fi
}

# Test 5: PacketTunnelProvider.swift contains NEIPv4Route.default() and NEIPv6Route.default()
test_ptp_has_default_route() {
    if [ ! -f "$SWIFT_FILE" ]; then
        fail "test_ptp_has_default_route — PacketTunnelProvider.swift does not exist"
        return
    fi
    if grep -q "NEIPv4Route.default()" "$SWIFT_FILE" && grep -q "NEIPv6Route.default()" "$SWIFT_FILE"; then
        pass "test_ptp_has_default_route — NEIPv4Route.default() and NEIPv6Route.default() found"
    else
        fail "test_ptp_has_default_route — NEIPv4Route.default() or NEIPv6Route.default() missing"
    fi
}

# Test 6: PacketTunnelProvider.swift uses group.io.kaitu.desktop (not group.io.kaitu)
test_ptp_app_group_desktop() {
    if [ ! -f "$SWIFT_FILE" ]; then
        fail "test_ptp_app_group_desktop — PacketTunnelProvider.swift does not exist"
        return
    fi
    if grep -q "group.io.kaitu.desktop" "$SWIFT_FILE"; then
        pass "test_ptp_app_group_desktop — group.io.kaitu.desktop found"
    else
        fail "test_ptp_app_group_desktop — group.io.kaitu.desktop not found; app group must be desktop-specific"
    fi
    # Also verify it does NOT use the iOS-only group.io.kaitu (without .desktop suffix)
    if grep -q '"group.io.kaitu"' "$SWIFT_FILE"; then
        fail "test_ptp_app_group_desktop — found iOS-only group.io.kaitu (without .desktop); must not be used in macOS target"
    fi
}

# Test 7: Info.plist has CFBundleExecutable, CFBundleVersion, CFBundleShortVersionString
test_info_plist_has_version_fields() {
    if [ ! -f "$PLIST_FILE" ]; then
        fail "test_info_plist_has_version_fields — Info.plist does not exist"
        return
    fi
    local missing=()
    grep -q "CFBundleExecutable" "$PLIST_FILE" || missing+=("CFBundleExecutable")
    grep -q "CFBundleVersion" "$PLIST_FILE" || missing+=("CFBundleVersion")
    grep -q "CFBundleShortVersionString" "$PLIST_FILE" || missing+=("CFBundleShortVersionString")
    if [ ${#missing[@]} -eq 0 ]; then
        pass "test_info_plist_has_version_fields — CFBundleExecutable, CFBundleVersion, CFBundleShortVersionString all present"
    else
        fail "test_info_plist_has_version_fields — missing fields: ${missing[*]}"
    fi
}

# Run all tests
echo "=== T1: macOS NE Extension Verification Tests ==="
echo ""
test_info_plist_ne_point
test_entitlements_packet_tunnel
test_info_plist_bundle_id
test_ptp_has_dns_settings
test_ptp_has_default_route
test_ptp_app_group_desktop
test_info_plist_has_version_fields
echo ""
echo "Results: PASS=$PASS FAIL=$FAIL"

if [ "$FAIL" -gt 0 ]; then
    exit 1
fi
exit 0
