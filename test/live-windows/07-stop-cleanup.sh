#!/usr/bin/env bash
# Phase 7: Stop k2 and clean up everything
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/env.sh"

echo "=== Phase 7: Stop k2 & Cleanup ==="

# Graceful disconnect
echo "[1] Disconnecting tunnel..."
win "$WIN_K2_BIN ctl down 2>nul & echo DOWN_OK" || true
sleep 2

# Stop daemon
echo "[2] Stopping daemon..."
win "taskkill /F /IM k2.exe 2>nul & echo KILL_OK" || true
# Kill background SSH session that hosts the daemon
if [ -f "$DIAG_DIR/daemon-ssh.pid" ]; then
    DAEMON_PID=$(cat "$DIAG_DIR/daemon-ssh.pid")
    kill "$DAEMON_PID" 2>/dev/null || true
    echo "  Killed daemon SSH session (PID $DAEMON_PID)"
fi
sleep 2

# Verify stopped
PROC=$(win "tasklist /FI \"IMAGENAME eq k2.exe\" /NH 2>nul" || echo "")
if echo "$PROC" | grep -qi "k2.exe"; then
    echo "  Force killing..."
    win "taskkill /F /IM k2.exe 2>nul" || true
    sleep 2
fi
echo "  k2 stopped."

# Verify TUN device removed
echo "[3] Checking TUN device removed..."
TUN_CHECK=$(win "ipconfig /all" 2>&1 | LC_ALL=C grep -i "sing-tun" || echo "NONE")
if [ "$TUN_CHECK" = "NONE" ]; then
    echo "  TUN device: removed"
else
    echo "  WARNING: TUN device still present: $TUN_CHECK"
fi

# Verify routes restored
echo "[4] Checking routes restored..."
STALE=$(win "route print -4 198.18.0.0 2>nul" | grep "198\.18" || echo "NONE")
if [ "$STALE" = "NONE" ] || ! echo "$STALE" | grep -q "198\.18"; then
    echo "  Routes: clean (no TUN routes)"
else
    echo "  WARNING: Stale TUN routes found:"
    echo "    $STALE"
fi

# Verify DNS restored
echo "[5] Checking DNS restored..."
DNS_TEST=$(win "nslookup www.baidu.com 2>nul" || echo "FAILED")
if echo "$DNS_TEST" | grep -qi "address.*[0-9]\|addresses"; then
    echo "  DNS: working"
else
    echo "  WARNING: DNS not working, resetting..."
    win "powershell -NoProfile -Command \"Get-NetAdapter | Where-Object { \$_.Status -eq 'Up' } | ForEach-Object { Set-DnsClientServerAddress -InterfaceIndex \$_.ifIndex -ResetServerAddresses }\" 2>nul"
    echo "  DNS reset complete."
fi

# Remove watchdog scheduled task
echo "[6] Removing watchdog scheduled task..."
win "schtasks /Delete /TN \"k2-watchdog\" /F 2>nul & echo WATCHDOG_REMOVED"

# Remove persistent route (optional — safe to leave)
echo "[7] Removing persistent route..."
win "route delete $WIN_SUBNET mask $WIN_MASK $WIN_GATEWAY 2>nul & echo ROUTE_REMOVED"

# Clear stale state
echo "[8] Clearing state..."
win "del /Q C:\\ProgramData\\k2\\state.json 2>nul & echo STATE_CLEARED"

# Final verification
echo ""
echo "[9] Final verification..."
PING=$(win "ping -n 1 -w 2000 $WIN_GATEWAY" 2>&1 || echo "FAILED")
if echo "$PING" | LC_ALL=C grep -q "TTL="; then
    echo "  Gateway: OK"
else
    echo "  Gateway: FAILED"
fi

PING_EXT=$(win "ping -n 1 -w 3000 8.8.8.8" 2>&1 || echo "FAILED")
if echo "$PING_EXT" | LC_ALL=C grep -q "TTL="; then
    echo "  Internet: OK"
else
    echo "  Internet: FAILED"
fi

# Log summary
echo ""
echo "Log summary:"
if win "type $WIN_K2_LOG >nul 2>nul"; then
    LINES=$(win "find /C /V \"\" $WIN_K2_LOG 2>nul" | grep -oE "[0-9]+" | tail -1 || echo "?")
    ERRORS=$(win "findstr /I /C:\"error\" $WIN_K2_LOG 2>nul" | wc -l || echo "0")
    echo "  Total lines: $LINES"
    echo "  Errors: $ERRORS"
    echo ""
    echo "Last 10 lines:"
    win "powershell -NoProfile -Command \"Get-Content $WIN_K2_LOG -Tail 10\" 2>nul" | while read -r line; do echo "  $line"; done
else
    echo "  (no log file)"
fi

echo ""
echo "=== Phase 7 complete — Windows cleaned up ==="
