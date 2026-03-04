# K2 TUN Mode Admin Launcher
# Run this script from PowerShell: .\scripts\start-k2-admin.ps1
# It will request UAC elevation, start k2 daemon, and show logs.

$ErrorActionPreference = "Stop"
$k2Root = Split-Path -Parent (Split-Path -Parent $MyInvocation.MyCommand.Path)
$k2Bin = Join-Path $k2Root "desktop\src-tauri\binaries\k2-x86_64-pc-windows-msvc.exe"
$k2Cfg = Join-Path $k2Root "k2-test-config.yml"
$k2Log = "C:\Users\david\k2-debug.log"

# Check if already admin
$isAdmin = ([Security.Principal.WindowsPrincipal] [Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)

if (-not $isAdmin) {
    Write-Host "Requesting admin privileges..." -ForegroundColor Yellow
    Start-Process powershell.exe -ArgumentList "-NoProfile -ExecutionPolicy Bypass -File `"$($MyInvocation.MyCommand.Path)`"" -Verb RunAs
    exit
}

# We are admin now
Write-Host "============================================" -ForegroundColor Green
Write-Host "  K2 TUN Mode Test - Running as Admin" -ForegroundColor Green
Write-Host "============================================" -ForegroundColor Green
Write-Host ""
Write-Host "Binary: $k2Bin"
Write-Host "Config: $k2Cfg"
Write-Host "Log:    $k2Log"
Write-Host "API:    http://127.0.0.1:1778"
Write-Host ""
Write-Host "From Git Bash, use:" -ForegroundColor Cyan
Write-Host "  ./scripts/test-k2-ctl.sh up       # Connect"
Write-Host "  ./scripts/test-k2-ctl.sh status    # Check status"
Write-Host "  ./scripts/test-k2-ctl.sh logs      # Tail logs"
Write-Host "  ./scripts/test-k2-ctl.sh test      # Run tests"
Write-Host "  ./scripts/test-k2-ctl.sh down      # Disconnect"
Write-Host ""
Write-Host "Press Ctrl+C to stop the daemon." -ForegroundColor Yellow
Write-Host "============================================" -ForegroundColor Green
Write-Host ""

# Clear old log
Remove-Item $k2Log -ErrorAction SilentlyContinue

# Run k2 daemon (foreground, blocks until Ctrl+C)
& $k2Bin -c $k2Cfg

Write-Host ""
Write-Host "K2 daemon stopped." -ForegroundColor Red
Read-Host "Press Enter to close"
