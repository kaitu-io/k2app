# Deploy SimpliSign Scripts to Windows Runner
#
# This script copies SimpliSign automation scripts from the repository
# to the Windows Runner's script directory.
#
# Usage:
#   .\deploy_to_runner.ps1
#
# Or via SSH from Mac:
#   scp scripts/ci/windows/*.{ps1,py} david@<windows-ip>:/Users/david/
#   ssh david@<windows-ip> 'powershell -File /Users/david/deploy_to_runner.ps1'

$ErrorActionPreference = "Stop"

Write-Host "=== Deploy SimpliSign Scripts to Windows Runner ===" -ForegroundColor Cyan
Write-Host ""

# Define paths
$runnerScriptsDir = "C:\actions-runner\scripts"
$repoDir = $PSScriptRoot  # Directory where this script is located

# Create runner scripts directory if it doesn't exist
if (-not (Test-Path $runnerScriptsDir)) {
    Write-Host "Creating directory: $runnerScriptsDir" -ForegroundColor Yellow
    New-Item -ItemType Directory -Path $runnerScriptsDir -Force | Out-Null
}

# Files to deploy
$filesToDeploy = @(
    "simplisign_auto_login.py",
    "simplisign_login.ps1"
)

Write-Host "Source directory: $repoDir" -ForegroundColor Gray
Write-Host "Target directory: $runnerScriptsDir" -ForegroundColor Gray
Write-Host ""

# Copy files
$deployedCount = 0
foreach ($file in $filesToDeploy) {
    $sourcePath = Join-Path $repoDir $file
    $targetPath = Join-Path $runnerScriptsDir $file

    if (-not (Test-Path $sourcePath)) {
        Write-Host "✗ SKIP: $file (not found in source)" -ForegroundColor Red
        continue
    }

    try {
        Copy-Item -Path $sourcePath -Destination $targetPath -Force
        Write-Host "✓ Deployed: $file" -ForegroundColor Green
        $deployedCount++
    } catch {
        Write-Host "✗ ERROR: Failed to copy $file - $_" -ForegroundColor Red
    }
}

Write-Host ""
Write-Host "Deployed $deployedCount of $($filesToDeploy.Count) files" -ForegroundColor Cyan

# Verify deployment
Write-Host ""
Write-Host "Verifying deployment..." -ForegroundColor Cyan
$allPresent = $true

foreach ($file in $filesToDeploy) {
    $targetPath = Join-Path $runnerScriptsDir $file
    if (Test-Path $targetPath) {
        $fileSize = (Get-Item $targetPath).Length
        Write-Host "  ✓ $file ($fileSize bytes)" -ForegroundColor Green
    } else {
        Write-Host "  ✗ $file (missing)" -ForegroundColor Red
        $allPresent = $false
    }
}

# Install Python dependencies
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
    Write-Host "Installing missing packages: $($missingPackages -join ', ')" -ForegroundColor Yellow
    pip install $missingPackages -q

    if ($LASTEXITCODE -eq 0) {
        Write-Host "✓ Packages installed successfully" -ForegroundColor Green
    } else {
        Write-Host "✗ Failed to install packages" -ForegroundColor Red
        $allPresent = $false
    }
}

# Final status
Write-Host ""
if ($allPresent -and $deployedCount -eq $filesToDeploy.Count) {
    Write-Host "✓ Deployment successful!" -ForegroundColor Green
    Write-Host ""
    Write-Host "Next steps:" -ForegroundColor Cyan
    Write-Host "  1. Configure GitHub Secrets (SIMPLISIGN_TOTP_URI, SIMPLISIGN_USERNAME, SIMPLISIGN_PIN)" -ForegroundColor Yellow
    Write-Host "  2. Test login: C:\actions-runner\scripts\simplisign_login.ps1" -ForegroundColor Yellow
    Write-Host "  3. Enable signing in .github/workflows/test-build-windows.yml" -ForegroundColor Yellow
    exit 0
} else {
    Write-Host "✗ Deployment incomplete" -ForegroundColor Red
    exit 1
}
