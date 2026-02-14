# Start GitHub Actions Runner in Interactive Mode
# This script starts SimpliSign Desktop and the GitHub Actions runner
# Place a shortcut to this in shell:startup for auto-start on login

$ErrorActionPreference = "Stop"

Write-Host "=== Starting Interactive GitHub Actions Runner ===" -ForegroundColor Cyan

# Start SimpliSign Desktop if not running
$simplisign = Get-Process SimplySignDesktop -ErrorAction SilentlyContinue
if (-not $simplisign) {
    Write-Host "Starting SimplySign Desktop..." -ForegroundColor Yellow
    Start-Process "C:\Program Files\Certum\SimplySign Desktop\SimplySignDesktop.exe"
    Start-Sleep -Seconds 5
}

# Change to runner directory
Set-Location C:\actions-runner

# Start runner interactively (blocks)
Write-Host "Starting GitHub Actions Runner..." -ForegroundColor Green
.\run.cmd
