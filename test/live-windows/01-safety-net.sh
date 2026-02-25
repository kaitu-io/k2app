#!/usr/bin/env bash
# Phase 1: Install network safety layers before TUN testing
# Ensures SSH stays alive even when k2 TUN modifies routes/DNS
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/env.sh"

echo "=== Phase 1: Network Safety Setup ==="

# Layer 1: Persistent route for Parallels subnet
# This ensures 10.211.55.0/24 always goes through the Parallels gateway,
# even if TUN adds a default route
echo "[1/3] Adding persistent route for Parallels subnet..."
win "route -p add $WIN_SUBNET mask $WIN_MASK $WIN_GATEWAY metric 1 2>nul & echo ROUTE_OK"
echo "  Route: $WIN_SUBNET/24 → $WIN_GATEWAY (metric 1, persistent)"

# Verify route exists
echo "  Verifying route..."
ROUTE_CHECK=$(win "route print -4 $WIN_SUBNET" 2>&1)
if echo "$ROUTE_CHECK" | grep -q "$WIN_GATEWAY"; then
    echo "  Route verified."
else
    echo "  WARNING: Route not found in table. Output:"
    echo "$ROUTE_CHECK" | head -5
fi

# Layer 2: Upload and install watchdog scheduled task
echo "[2/3] Installing watchdog..."

# Upload watchdog script
cat > /tmp/k2-watchdog.ps1 <<'WATCHDOG_PS1'
# k2 test watchdog — runs every 30s, kills k2 if SSH gateway unreachable
$gateway = "10.211.55.1"
$logFile = "C:\Users\david\k2-test\watchdog.log"
$timestamp = Get-Date -Format "yyyy-MM-dd HH:mm:ss"

# Test gateway connectivity (2 pings, 1s timeout each)
$ping = Test-Connection -ComputerName $gateway -Count 2 -Delay 1 -Quiet -ErrorAction SilentlyContinue
if (-not $ping) {
    "$timestamp ALERT: Gateway unreachable, killing k2..." | Out-File -Append $logFile
    # Kill k2 process
    Get-Process -Name "k2" -ErrorAction SilentlyContinue | Stop-Process -Force
    Start-Sleep -Seconds 2
    # Restore DNS to DHCP
    $adapters = Get-NetAdapter | Where-Object { $_.Status -eq "Up" -and $_.Name -notlike "*sing-tun*" }
    foreach ($a in $adapters) {
        Set-DnsClientServerAddress -InterfaceIndex $a.ifIndex -ResetServerAddresses
    }
    # Remove any TUN default routes (0.0.0.0/0 pointing to 198.18.x.x)
    Get-NetRoute -DestinationPrefix "0.0.0.0/0" -ErrorAction SilentlyContinue |
        Where-Object { $_.NextHop -like "198.18.*" } |
        Remove-NetRoute -Confirm:$false -ErrorAction SilentlyContinue
    "$timestamp Recovery complete." | Out-File -Append $logFile
} else {
    # Quiet log every 10 minutes (avoid log spam)
    if ((Get-Date).Minute % 10 -eq 0 -and (Get-Date).Second -lt 35) {
        "$timestamp OK: Gateway reachable." | Out-File -Append $logFile
    }
}
WATCHDOG_PS1

scp /tmp/k2-watchdog.ps1 "$WIN_USER@$WIN_HOST:k2-test/watchdog.ps1"

# Create scheduled task (runs every 30 seconds via task repetition)
win "schtasks /Create /TN \"k2-watchdog\" /TR \"powershell -NoProfile -ExecutionPolicy Bypass -File C:\\Users\\david\\k2-test\\watchdog.ps1\" /SC MINUTE /MO 1 /RL HIGHEST /F 2>nul & echo TASK_OK"

echo "  Watchdog installed (scheduled task: k2-watchdog)"

# Layer 3: Upload emergency restore script (manual fallback)
echo "[3/3] Uploading emergency restore script..."

cat > /tmp/emergency-restore.ps1 <<'EMERGENCY_PS1'
# Emergency network restore — run from Parallels Desktop window if SSH breaks
Write-Host "=== Emergency Network Restore ===" -ForegroundColor Red

# Kill k2
Write-Host "[1] Killing k2..."
Get-Process -Name "k2" -ErrorAction SilentlyContinue | Stop-Process -Force
Start-Sleep -Seconds 2

# Reset DNS on all active adapters
Write-Host "[2] Resetting DNS..."
Get-NetAdapter | Where-Object { $_.Status -eq "Up" } | ForEach-Object {
    Set-DnsClientServerAddress -InterfaceIndex $_.ifIndex -ResetServerAddresses
    Write-Host "  Reset DNS on: $($_.Name)"
}

# Remove TUN routes
Write-Host "[3] Removing TUN routes..."
Get-NetRoute -DestinationPrefix "0.0.0.0/0" -ErrorAction SilentlyContinue |
    Where-Object { $_.NextHop -like "198.18.*" } |
    Remove-NetRoute -Confirm:$false -ErrorAction SilentlyContinue
Get-NetRoute -ErrorAction SilentlyContinue |
    Where-Object { $_.NextHop -like "198.18.*" } |
    Remove-NetRoute -Confirm:$false -ErrorAction SilentlyContinue

# Flush DNS cache
Write-Host "[4] Flushing DNS cache..."
ipconfig /flushdns | Out-Null

# Verify
Write-Host ""
Write-Host "[5] Verification:"
$ping = Test-Connection -ComputerName "10.211.55.1" -Count 1 -Quiet -ErrorAction SilentlyContinue
if ($ping) {
    Write-Host "  Gateway: OK" -ForegroundColor Green
} else {
    Write-Host "  Gateway: UNREACHABLE — reboot VM" -ForegroundColor Red
}

Write-Host ""
Write-Host "Done. If still broken, reboot the VM." -ForegroundColor Yellow
EMERGENCY_PS1

scp /tmp/emergency-restore.ps1 "$WIN_USER@$WIN_HOST:k2-test/emergency-restore.ps1"
echo "  Emergency script: C:\\Users\\david\\k2-test\\emergency-restore.ps1"

# Final safety verification: can we still SSH?
echo ""
echo "Safety verification..."
PING_RESULT=$(win "ping -n 1 -w 2000 $WIN_GATEWAY" 2>&1)
if echo "$PING_RESULT" | grep -qi "TTL=\|ttl="; then
    echo "  Gateway ping: OK"
else
    echo "  WARNING: Gateway ping failed"
fi

SSH_CHECK=$(win "echo SSH_ALIVE" 2>&1)
if [ "$SSH_CHECK" = "SSH_ALIVE" ]; then
    echo "  SSH: OK"
else
    echo "  WARNING: SSH check failed"
fi

echo ""
echo "=== Phase 1 complete ==="
echo "Safety layers:"
echo "  1. Persistent route: $WIN_SUBNET/24 → $WIN_GATEWAY"
echo "  2. Watchdog: schtasks k2-watchdog (every 1 min)"
echo "  3. Emergency: C:\\Users\\david\\k2-test\\emergency-restore.ps1"
