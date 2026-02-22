#!/usr/bin/env bash
# T5 — macOS NE migration verification tests
# Tests that dead macOS daemon code is cleaned up and docs are updated.
# Run from repo root.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SERVICE_RS="$REPO_ROOT/desktop/src-tauri/src/service.rs"
DESKTOP_CLAUDE="$REPO_ROOT/desktop/CLAUDE.md"
ROOT_CLAUDE="$REPO_ROOT/CLAUDE.md"

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

# ---------------------------------------------------------------------------
# test_no_admin_reinstall_macos_function
# Verify that service.rs does NOT contain the dead admin_reinstall_service_macos()
# function body. T3 replaced macOS reinstall path with admin_reinstall_ne() in ne.rs.
# ---------------------------------------------------------------------------
test_no_admin_reinstall_macos_function() {
    local test_name="test_no_admin_reinstall_macos_function"
    if grep -q "fn admin_reinstall_service_macos" "$SERVICE_RS"; then
        fail "$test_name — admin_reinstall_service_macos() still present in service.rs (expected deleted)"
    else
        pass "$test_name"
    fi
}

# ---------------------------------------------------------------------------
# test_desktop_claude_md_has_ne_module
# Verify desktop/CLAUDE.md documents the ne.rs module.
# ---------------------------------------------------------------------------
test_desktop_claude_md_has_ne_module() {
    local test_name="test_desktop_claude_md_has_ne_module"
    if grep -q "ne\.rs" "$DESKTOP_CLAUDE"; then
        pass "$test_name"
    else
        fail "$test_name — desktop/CLAUDE.md does not mention ne.rs module"
    fi
}

# ---------------------------------------------------------------------------
# test_desktop_claude_md_has_ne_ipc
# Verify desktop/CLAUDE.md documents NE-specific IPC routing (cfg macOS).
# ---------------------------------------------------------------------------
test_desktop_claude_md_has_ne_ipc() {
    local test_name="test_desktop_claude_md_has_ne_ipc"
    # Must document macOS routing for daemon_exec → ne (cfg target_os = "macos")
    if grep -q "macOS" "$DESKTOP_CLAUDE" && grep -q "ne_action\|ne\.rs\|NE helper\|Network Extension" "$DESKTOP_CLAUDE"; then
        pass "$test_name"
    else
        fail "$test_name — desktop/CLAUDE.md missing NE-specific IPC routing documentation"
    fi
}

# ---------------------------------------------------------------------------
# test_root_claude_md_has_ne_vocabulary
# Verify CLAUDE.md has NE-related domain vocabulary entries.
# ---------------------------------------------------------------------------
test_root_claude_md_has_ne_vocabulary() {
    local test_name="test_root_claude_md_has_ne_vocabulary"
    # Must contain at least one of the new NE vocabulary terms
    if grep -q "KaituTunnel\.appex\|libk2_ne_helper\|ensure_ne_installed" "$ROOT_CLAUDE"; then
        pass "$test_name"
    else
        fail "$test_name — CLAUDE.md missing NE domain vocabulary (KaituTunnel.appex / libk2_ne_helper.a / ensure_ne_installed)"
    fi
}

# ---------------------------------------------------------------------------
# Run all tests
# ---------------------------------------------------------------------------
echo "=== T5 macOS NE Migration Tests ==="
echo ""

test_no_admin_reinstall_macos_function
test_desktop_claude_md_has_ne_module
test_desktop_claude_md_has_ne_ipc
test_root_claude_md_has_ne_vocabulary

echo ""
echo "=== Results: $PASS passed, $FAIL failed ==="

if [ "$FAIL" -gt 0 ]; then
    exit 1
fi
exit 0
