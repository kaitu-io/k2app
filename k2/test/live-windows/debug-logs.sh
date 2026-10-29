#!/usr/bin/env bash
# Debug helper: view logs from Windows client and/or HK server
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/env.sh"

MODE="${1:-both}"

case "$MODE" in
    client)
        echo "=== Windows client log (last 100) ==="
        win "powershell -NoProfile -Command \"Get-Content $WIN_K2_LOG -Tail 100\" 2>nul" || echo "(no log)"
        ;;
    server)
        echo "=== Server log (last 50) ==="
        ssh "$K2_SERVER_USER@$K2_SERVER_IP" "sudo tail -50 /var/log/k2s.log" 2>/dev/null || echo "(no server log)"
        ;;
    both)
        echo "=== Windows client log (last 30) ==="
        win "powershell -NoProfile -Command \"Get-Content $WIN_K2_LOG -Tail 30\" 2>nul" || echo "(no log)"
        echo ""
        echo "=== Server log (last 30) ==="
        ssh "$K2_SERVER_USER@$K2_SERVER_IP" "sudo tail -30 /var/log/k2s.log" 2>/dev/null || echo "(no server log)"
        ;;
    errors)
        echo "=== Windows client errors ==="
        win "findstr /I /C:\"error\" /C:\"fatal\" /C:\"panic\" $WIN_K2_LOG 2>nul" | tail -30 || echo "(none)"
        echo ""
        echo "=== Server errors ==="
        ssh "$K2_SERVER_USER@$K2_SERVER_IP" "sudo grep -i 'error\|fatal\|panic' /var/log/k2s.log 2>/dev/null | tail -30" || echo "(none)"
        ;;
    watchdog)
        echo "=== Watchdog log ==="
        win "type C:\\Users\\david\\k2-test\\watchdog.log 2>nul" || echo "(no watchdog log)"
        ;;
    routes)
        echo "=== Current Windows route table ==="
        win "route print -4 2>nul"
        ;;
    dns)
        echo "=== Current Windows DNS config ==="
        win "netsh interface ip show dns 2>nul"
        ;;
    status)
        echo "=== k2 status ==="
        win "curl -s http://127.0.0.1:1777/api/core -d \"{\\\"action\\\":\\\"status\\\"}\" 2>nul" || echo "(daemon not running)"
        echo ""
        echo "=== k2 process ==="
        win "tasklist /FI \"IMAGENAME eq k2.exe\" 2>nul"
        ;;
    *)
        echo "Usage: $0 [client|server|both|errors|watchdog|routes|dns|status]"
        ;;
esac
