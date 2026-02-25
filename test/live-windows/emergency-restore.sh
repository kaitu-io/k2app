#!/usr/bin/env bash
# Emergency network restore — run from Mac if SSH is still working
# If SSH is broken, use Parallels Desktop window to run emergency-restore.ps1
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/env.sh"

echo "=== Emergency Network Restore (via SSH) ==="

echo "[1] Killing k2..."
win "taskkill /F /IM k2.exe 2>nul & echo KILLED" || echo "  (could not kill — SSH may be broken)"
sleep 2

echo "[2] Resetting DNS..."
win "powershell -NoProfile -Command \"Get-NetAdapter | Where-Object { \$_.Status -eq 'Up' } | ForEach-Object { Set-DnsClientServerAddress -InterfaceIndex \$_.ifIndex -ResetServerAddresses }\" 2>nul" || echo "  (could not reset DNS)"

echo "[3] Removing TUN routes..."
win "powershell -NoProfile -Command \"Get-NetRoute -ErrorAction SilentlyContinue | Where-Object { \$_.NextHop -like '198.18.*' } | Remove-NetRoute -Confirm:\$false -ErrorAction SilentlyContinue\" 2>nul" || echo "  (could not remove routes)"

echo "[4] Flushing DNS cache..."
win "ipconfig /flushdns 2>nul" || echo "  (could not flush)"

echo "[5] Verification..."
PING=$(win "ping -n 1 -w 2000 $WIN_GATEWAY 2>nul" || echo "FAILED")
if echo "$PING" | grep -qi "TTL="; then
    echo "  Gateway: OK"
else
    echo "  Gateway: UNREACHABLE"
fi

PING_EXT=$(win "ping -n 1 -w 3000 8.8.8.8 2>nul" || echo "FAILED")
if echo "$PING_EXT" | grep -qi "TTL="; then
    echo "  Internet: OK"
else
    echo "  Internet: UNREACHABLE — reboot VM"
fi

echo ""
echo "Done. If still broken, reboot the Parallels VM."
