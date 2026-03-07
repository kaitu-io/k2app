#Requires -RunAsAdministrator
<#
.SYNOPSIS
    Automated smoke test for k2 Windows Service lifecycle.
    Exercises: install -> ping -> idempotent reinstall -> stop -> uninstall.

.DESCRIPTION
    Automates scenarios 1-4 from docs/test-windows-service.md.
    Must run as Administrator. Uses the k2 binary at the standard install path.

.PARAMETER K2Path
    Path to k2.exe binary. Default: desktop\src-tauri\binaries\k2-x86_64-pc-windows-msvc.exe
#>
param(
    [string]$K2Path = "desktop\src-tauri\binaries\k2-x86_64-pc-windows-msvc.exe"
)

$ErrorActionPreference = "Stop"
$pass = 0
$fail = 0

function Test-Step {
    param([string]$Name, [scriptblock]$Action)
    Write-Host "`n--- $Name ---" -ForegroundColor Cyan
    try {
        & $Action
        Write-Host "  PASS" -ForegroundColor Green
        $script:pass++
    } catch {
        Write-Host "  FAIL: $_" -ForegroundColor Red
        $script:fail++
    }
}

# Verify k2 binary exists
if (-not (Test-Path $K2Path)) {
    Write-Host "k2 binary not found at: $K2Path" -ForegroundColor Red
    Write-Host "Build first: make build-k2-windows" -ForegroundColor Yellow
    exit 1
}

$k2Abs = (Resolve-Path $K2Path).Path
Write-Host "Using k2 binary: $k2Abs"

# Cleanup any previous test state
Write-Host "`n=== Cleanup ===" -ForegroundColor Yellow
sc.exe stop kaitu 2>$null
sc.exe delete kaitu 2>$null
Start-Sleep -Seconds 2

# Test 1: Fresh install
Test-Step "Fresh install" {
    & $k2Abs service install
    Start-Sleep -Seconds 3
    $status = sc.exe query kaitu | Select-String "STATE"
    if ($status -notmatch "RUNNING") {
        throw "Service not RUNNING after install: $status"
    }
}

# Test 2: Daemon responds to ping
Test-Step "Daemon ping" {
    Start-Sleep -Seconds 2
    $response = Invoke-RestMethod -Uri "http://127.0.0.1:1777/ping" -TimeoutSec 5
    if ($response.code -ne 0) {
        throw "Ping response code = $($response.code), expected 0"
    }
}

# Test 3: Idempotent reinstall
Test-Step "Idempotent reinstall" {
    & $k2Abs service install
    Start-Sleep -Seconds 3
    $status = sc.exe query kaitu | Select-String "STATE"
    if ($status -notmatch "RUNNING") {
        throw "Service not RUNNING after reinstall: $status"
    }
}

# Test 4: Stop service
Test-Step "Stop service" {
    sc.exe stop kaitu
    Start-Sleep -Seconds 3
    $status = sc.exe query kaitu | Select-String "STATE"
    if ($status -notmatch "STOPPED") {
        throw "Service not STOPPED: $status"
    }
}

# Test 5: Uninstall
Test-Step "Uninstall service" {
    & $k2Abs service uninstall
    Start-Sleep -Seconds 2
    $result = sc.exe query kaitu 2>&1
    if ($result -notmatch "1060") {
        throw "Service still exists after uninstall"
    }
}

# Summary
Write-Host "`n=== Results ===" -ForegroundColor Yellow
Write-Host "  Pass: $pass" -ForegroundColor Green
Write-Host "  Fail: $fail" -ForegroundColor $(if ($fail -gt 0) { "Red" } else { "Green" })

if ($fail -gt 0) { exit 1 }
