#!/usr/bin/env bash
# Phase 3: Start k2 client in TUN mode on Windows (requires admin)
# Uses a persistent background SSH session to keep the daemon alive.
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/env.sh"
PHASE_NAME="phase3-start"

echo "=== Phase 3: Start k2 Client (Windows TUN mode) ==="

# Step 1: Kill old k2 + kaitu-service (shares port 1777) + clear state + snapshot network
echo "[1] Cleanup + snapshot pre-TUN state..."
win "cmd /C \"net stop kaitu 2>nul & taskkill /F /IM kaitu-service.exe 2>nul & taskkill /F /IM k2.exe 2>nul & mkdir C:\\Users\\david\\.config\\k2 2>nul & del /Q $WIN_K2_LOG 2>nul & del /Q C:\\Users\\david\\.config\\k2\\k2.pid 2>nul & del /Q C:\\ProgramData\\k2\\state.json 2>nul & route print -4 0.0.0.0 > C:\\Users\\david\\k2-test\\routes-before.txt 2>nul & netsh interface ip show dns > C:\\Users\\david\\k2-test\\dns-before.txt 2>nul & echo READY\"" || true
sleep 2

# Step 2: Start k2 daemon in a persistent background SSH session
# The SSH session stays open, keeping the daemon alive.
echo "[2] Starting k2 daemon (background SSH session)..."
$WIN_SSH "$WIN_K2_BIN -c $WIN_K2_CONFIG 2>&1" > "$DIAG_DIR/daemon-session.log" 2>&1 &
DAEMON_SSH_PID=$!
echo "$DAEMON_SSH_PID" > "$DIAG_DIR/daemon-ssh.pid"
echo "  SSH session PID: $DAEMON_SSH_PID"
sleep 4

# Step 3: Wait for daemon API
echo "[3] Waiting for daemon HTTP API..."
for i in $(seq 1 15); do
    RESULT=$(win "curl -s http://127.0.0.1:1777/ping 2>nul" || echo "")
    if echo "$RESULT" | grep -q "pong"; then
        echo "  API ready after ${i}s"
        break
    fi
    if ! kill -0 "$DAEMON_SSH_PID" 2>/dev/null; then
        echo "  ERROR: k2 daemon SSH session exited prematurely"
        cat "$DIAG_DIR/daemon-session.log" 2>/dev/null | tail -20
        collect_diag "phase3-daemon-crash"
        exit 1
    fi
    sleep 1
done

# Step 4: Connect tunnel
echo "[4] Connecting tunnel (k2 ctl up)..."
win "$WIN_K2_BIN ctl up $WIN_K2_CONFIG 2>&1" || true

# Step 5: Wait for connection
echo "[5] Waiting for connection..."
for i in $(seq 1 20); do
    STATUS=$(win "curl -s http://127.0.0.1:1777/api/core -d \"{\\\"action\\\":\\\"status\\\"}\" 2>nul" || echo "{}")
    STATE=$(echo "$STATUS" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('data',{}).get('state','unknown'))" 2>/dev/null || echo "unknown")

    if [ "$STATE" = "connected" ]; then
        echo "  Connected after ${i}s!"
        break
    elif [ "$STATE" = "stopped" ]; then
        ERROR=$(echo "$STATUS" | python3 -c "import sys,json; d=json.load(sys.stdin); e=d.get('data',{}).get('error',{}); print(e.get('message','') if isinstance(e,dict) else str(e))" 2>/dev/null || echo "")
        if [ -n "$ERROR" ]; then
            echo "  ERROR: Connection failed: $ERROR"
            echo "  Full status: $STATUS"
            collect_diag "phase3-connect-fail"
            exit 1
        fi
    fi
    echo "  State: $STATE (${i}s)..."
    sleep 1
done

# Step 6: Post-tunnel verification (ONE SSH call: SSH liveness + TUN + routes + DNS + status)
echo "[6] Post-tunnel verification..."
VERIFY=$(win "cmd /C \"echo SSH_ALIVE & echo ==TUN== & ipconfig /all & echo ==ROUTES== & route print -4 & echo ==DNS== & netsh interface ip show dns & echo ==STATUS== & curl -s http://127.0.0.1:1777/api/core -d \"{\\\"action\\\":\\\"status\\\"}\" 2>nul\"" 2>&1 || echo "SSH_BROKEN")

if ! echo "$VERIFY" | head -3 | grep -q "SSH_ALIVE"; then
    echo "  CRITICAL: SSH broken after tunnel connect!"
    echo "  Watchdog should recover in ~60s."
    echo "  Manual: open Parallels → run emergency-restore.ps1"
    exit 1
fi
echo "  SSH: OK"

# Parse and display TUN info
echo "  TUN device:"
echo "$VERIFY" | LC_ALL=C sed -n '/==TUN==/,/==ROUTES==/p' | grep -A 3 -i "sing-tun\|198\.18\." | head -6 | while read -r l; do echo "    $l"; done

echo "  TUN routes:"
echo "$VERIFY" | LC_ALL=C sed -n '/==ROUTES==/,/==DNS==/p' | grep "198\.18" | head -5 | while read -r l; do echo "    $l"; done

echo "  Status:"
echo "$VERIFY" | LC_ALL=C sed -n '/==STATUS==/,$p' | tail -1

# Save post-TUN state for later comparison
mkdir -p "$DIAG_DIR"
echo "$VERIFY" > "$DIAG_DIR/phase3-post-tun-state.txt"

echo ""
echo "=== Phase 3 complete ==="
echo "To stop: bash $SCRIPT_DIR/07-stop-cleanup.sh"
