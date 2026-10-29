#!/usr/bin/env bash
# Phase 6: Crash recovery test — force-kill k2 and verify network recovers
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/env.sh"
PHASE_NAME="phase6-crash"
reset_counters

echo "=== Phase 6: Crash Recovery Test ==="

# Pre-check: k2 should be running
echo "[1] Pre-check..."
PROC=$(win "tasklist /FI \"IMAGENAME eq k2.exe\" /NH 2>nul" || echo "")
if ! echo "$PROC" | grep -qi "k2.exe"; then
    echo "  k2 not running — skipping crash test. Run 03-start-client.sh first."
    exit 0
fi
echo "  k2 is running."

# Snapshot during-TUN state
echo "[2] Snapshot pre-crash state..."
win "cmd /C \"route print -4 > C:\\Users\\david\\k2-test\\routes-during.txt & netsh interface ip show dns > C:\\Users\\david\\k2-test\\dns-during.txt\"" || true

# Kill background SSH session (if any)
BGPID=$(cat "$DIAG_DIR/daemon-ssh.pid" 2>/dev/null || echo "")
[ -n "$BGPID" ] && kill "$BGPID" 2>/dev/null || true

# FORCE KILL (simulates crash)
echo "[3] FORCE KILLING k2.exe..."
win "taskkill /F /IM k2.exe 2>nul & echo KILLED"
echo "  Waiting for Windows network stack recovery..."
sleep 10

# Post-crash: individual checks (avoids CMD batching/encoding issues)
echo "[4] Post-crash verification..."
mkdir -p "$DIAG_DIR"

# SSH check
SSH_OK=$(win "echo SSH_ALIVE" 2>&1 || echo "SSH_BROKEN")
if echo "$SSH_OK" | grep -q "SSH_ALIVE"; then
    pass "SSH alive after crash"
else
    fail "SSH BROKEN after crash"
    echo "  Waiting 90s for watchdog..."
    sleep 90
    SSH_OK=$(win "echo SSH_ALIVE" 2>&1)
    if echo "$SSH_OK" | grep -q "SSH_ALIVE"; then
        pass "SSH recovered via watchdog"
    else
        fail "SSH still broken — manual recovery needed"
        exit 1
    fi
fi

# Gateway
GW=$(win "ping -n 2 -w 2000 $WIN_GATEWAY" 2>&1 || echo "")
if echo "$GW" | LC_ALL=C grep -qi "TTL="; then
    pass "Gateway reachable after crash"
else
    fail "Gateway unreachable"
fi

# Internet
EXT=$(win "ping -n 2 -w 3000 8.8.8.8" 2>&1 || echo "")
if echo "$EXT" | LC_ALL=C grep -qi "TTL="; then
    pass "Internet reachable (8.8.8.8)"
else
    fail "Internet unreachable — routes may be stale"
    win "route print -4 0.0.0.0" 2>&1 | head -5 | while read -r l; do echo "    $l"; done
fi

# DNS
DNS=$(win "nslookup www.baidu.com 2>nul" 2>&1 || echo "")
if echo "$DNS" | LC_ALL=C grep -qi "address.*[0-9]\|addresses"; then
    pass "DNS working after crash"
else
    fail "DNS broken — settings may be stale"
fi

# TUN device
TUN=$(win "ipconfig /all" 2>&1 || echo "")
if echo "$TUN" | LC_ALL=C grep -qi "sing-tun"; then
    fail "TUN device still present after crash"
else
    pass "TUN device removed"
fi

# Stale routes
ROUTES=$(win "route print -4" 2>&1 || echo "")
if echo "$ROUTES" | grep -q "198\.18"; then
    fail "Stale TUN routes found"
    echo "$ROUTES" | grep "198\.18" | head -3 | while read -r l; do echo "    $l"; done
else
    pass "No stale TUN routes"
fi

# DNS stale check
DNS_CFG=$(win "netsh interface ip show dns" 2>&1 || echo "")
echo "$DNS_CFG" > "$DIAG_DIR/phase6-dns-after-crash.txt"
if echo "$DNS_CFG" | LC_ALL=C grep -qi "DHCP\|10\.211\.\|114\.114\|223\.5"; then
    pass "DNS settings restored to pre-TUN state"
else
    fail "DNS settings not restored after crash"
fi

# Restart test (use persistent background SSH session)
echo "[5] Restart test..."
$WIN_SSH "$WIN_K2_BIN -c $WIN_K2_CONFIG 2>&1" > "$DIAG_DIR/restart-session.log" 2>&1 &
RESTART_SSH_PID=$!
sleep 5
PING=$(win "curl -s http://127.0.0.1:1777/ping 2>nul" || echo "")
if echo "$PING" | grep -q "pong"; then
    pass "k2 restarts after crash"
    win "taskkill /F /IM k2.exe 2>nul" || true
    kill "$RESTART_SSH_PID" 2>/dev/null || true
    sleep 2
else
    fail "k2 failed to restart after crash"
    kill "$RESTART_SSH_PID" 2>/dev/null || true
    collect_diag "phase6-restart-fail"
fi

echo ""
echo "=== Phase 6 results: $PASS passed, $FAIL failed ==="
[ "$FAIL" -eq 0 ]
