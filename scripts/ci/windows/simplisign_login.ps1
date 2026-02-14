# SimpliSign Login PowerShell Wrapper
#
# This script wraps the Python auto-login script for use in GitHub Actions.
# Reads credentials from environment variables for security.
#
# Required Environment Variables:
#   SIMPLISIGN_TOTP_URI - Full otpauth:// URI from QR code
#   SIMPLISIGN_USERNAME - SimplySign account username
#   SIMPLISIGN_PIN      - (Optional) SimplySign PIN
#
# Usage in GitHub Actions:
#   - name: Authenticate SimpliSign
#     run: .\scripts\ci\windows\simplisign_login.ps1
#     env:
#       SIMPLISIGN_TOTP_URI: ${{ secrets.SIMPLISIGN_TOTP_URI }}
#       SIMPLISIGN_USERNAME: ${{ secrets.SIMPLISIGN_USERNAME }}
#       SIMPLISIGN_PIN: ${{ secrets.SIMPLISIGN_PIN }}

$ErrorActionPreference = "Stop"

Write-Host "=== SimpliSign Auto-Login ===" -ForegroundColor Cyan
Write-Host ""

# Check required environment variables
if (-not $env:SIMPLISIGN_TOTP_URI) {
    Write-Host "ERROR: SIMPLISIGN_TOTP_URI environment variable not set" -ForegroundColor Red
    Write-Host "This should be set in GitHub Secrets" -ForegroundColor Yellow
    exit 1
}

if (-not $env:SIMPLISIGN_USERNAME) {
    Write-Host "ERROR: SIMPLISIGN_USERNAME environment variable not set" -ForegroundColor Red
    exit 1
}

# Locate Python script
$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$pythonScript = Join-Path $scriptDir "simplisign_auto_login.py"

if (-not (Test-Path $pythonScript)) {
    Write-Host "ERROR: Python script not found at: $pythonScript" -ForegroundColor Red
    exit 1
}

Write-Host "Python script: $pythonScript" -ForegroundColor Gray
Write-Host "Username: $env:SIMPLISIGN_USERNAME" -ForegroundColor Gray

# Check Python installation
try {
    $pythonVersion = python --version 2>&1
    Write-Host "Python: $pythonVersion" -ForegroundColor Gray
} catch {
    Write-Host "ERROR: Python not found in PATH" -ForegroundColor Red
    exit 1
}

# Check required Python packages
Write-Host ""
Write-Host "Checking Python dependencies..." -ForegroundColor Cyan

$requiredPackages = @("pywinauto", "pyotp")
$missingPackages = @()

foreach ($package in $requiredPackages) {
    $installed = python -c "import $package" 2>&1
    if ($LASTEXITCODE -ne 0) {
        $missingPackages += $package
        Write-Host "  ✗ $package - NOT INSTALLED" -ForegroundColor Red
    } else {
        Write-Host "  ✓ $package - installed" -ForegroundColor Green
    }
}

if ($missingPackages.Count -gt 0) {
    Write-Host ""
    Write-Host "Installing missing packages..." -ForegroundColor Yellow
    pip install $missingPackages -q

    if ($LASTEXITCODE -ne 0) {
        Write-Host "ERROR: Failed to install Python packages" -ForegroundColor Red
        exit 1
    }
    Write-Host "Packages installed successfully" -ForegroundColor Green
}

# Build Python command arguments
$pythonArgs = @(
    $pythonScript,
    "--totp-uri", $env:SIMPLISIGN_TOTP_URI,
    "--username", $env:SIMPLISIGN_USERNAME
)

if ($env:SIMPLISIGN_PIN) {
    $pythonArgs += "--pin"
    $pythonArgs += $env:SIMPLISIGN_PIN
    Write-Host "PIN: ****** (provided)" -ForegroundColor Gray
}

# Execute login script
Write-Host ""
Write-Host "Executing auto-login..." -ForegroundColor Cyan
Write-Host "---" -ForegroundColor Gray

& python @pythonArgs

$exitCode = $LASTEXITCODE

Write-Host "---" -ForegroundColor Gray
Write-Host ""

if ($exitCode -eq 0) {
    Write-Host "✓ SimpliSign authentication successful" -ForegroundColor Green
    exit 0
} else {
    Write-Host "✗ SimpliSign authentication failed (exit code: $exitCode)" -ForegroundColor Red
    Write-Host ""
    Write-Host "Troubleshooting:" -ForegroundColor Yellow
    Write-Host "  1. Verify TOTP URI is correct (check GitHub Secrets)" -ForegroundColor Yellow
    Write-Host "  2. Ensure SimpliSign Desktop is installed and running" -ForegroundColor Yellow
    Write-Host "  3. Check Windows Event Viewer for application errors" -ForegroundColor Yellow
    Write-Host "  4. Try manual login to verify credentials" -ForegroundColor Yellow
    exit 1
}
