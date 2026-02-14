# SimpliSign Keeper Control Script
#
# Usage:
#   .\simplisign_keeper_ctl.ps1 start     - Start the keeper daemon
#   .\simplisign_keeper_ctl.ps1 stop      - Stop the keeper daemon
#   .\simplisign_keeper_ctl.ps1 status    - Show status
#   .\simplisign_keeper_ctl.ps1 logs      - View recent logs
#   .\simplisign_keeper_ctl.ps1 logs -f   - Follow logs (like tail -f)
#   .\simplisign_keeper_ctl.ps1 once      - Run once and exit
#   .\simplisign_keeper_ctl.ps1 install   - Install to Startup folder
#   .\simplisign_keeper_ctl.ps1 uninstall - Remove from Startup folder

param(
    [Parameter(Position=0)]
    [ValidateSet("start", "stop", "status", "logs", "once", "install", "uninstall", "health")]
    [string]$Action = "status",

    [switch]$f  # Follow logs
)

$ScriptPath = "C:\actions-runner\scripts\simplisign_keeper.py"
$LogPath = "C:\actions-runner\logs\simplisign_keeper.log"
$StartupScript = "C:\actions-runner\scripts\start_simplisign_keeper.vbs"
$StartupFolder = "$env:APPDATA\Microsoft\Windows\Start Menu\Programs\Startup"
$StartupLink = "$StartupFolder\SimpliSignKeeper.vbs"
$HealthPort = 8778

function Get-KeeperProcess {
    Get-Process python -ErrorAction SilentlyContinue |
        Where-Object { $_.CommandLine -like "*simplisign_keeper*" }
}

function Show-Status {
    Write-Host "=== SimpliSign Keeper Status ===" -ForegroundColor Cyan

    $process = Get-KeeperProcess
    if ($process) {
        Write-Host "Status: RUNNING" -ForegroundColor Green
        Write-Host "PID: $($process.Id)" -ForegroundColor Gray
        Write-Host "Start Time: $($process.StartTime)" -ForegroundColor Gray

        # Try health check
        try {
            $health = Invoke-RestMethod -Uri "http://127.0.0.1:$HealthPort/health" -TimeoutSec 2
            Write-Host ""
            Write-Host "Health Check:" -ForegroundColor Cyan
            Write-Host "  SimpliSign Status: $($health.status)" -ForegroundColor Gray
            Write-Host "  Last Check: $($health.last_check)" -ForegroundColor Gray
            Write-Host "  Last Login: $($health.last_login)" -ForegroundColor Gray
            Write-Host "  Login Count: $($health.login_count)" -ForegroundColor Gray
            Write-Host "  Error Count: $($health.error_count)" -ForegroundColor Gray
        } catch {
            Write-Host ""
            Write-Host "Health endpoint not responding" -ForegroundColor Yellow
        }
    } else {
        Write-Host "Status: STOPPED" -ForegroundColor Red
    }

    # Check if installed in startup
    if (Test-Path $StartupLink) {
        Write-Host ""
        Write-Host "Auto-start: ENABLED (Startup folder)" -ForegroundColor Green
    } else {
        Write-Host ""
        Write-Host "Auto-start: DISABLED" -ForegroundColor Yellow
    }

    # Show recent log entries
    if (Test-Path $LogPath) {
        Write-Host ""
        Write-Host "Recent logs:" -ForegroundColor Cyan
        Get-Content $LogPath -Tail 5 | ForEach-Object { Write-Host "  $_" -ForegroundColor Gray }
    }
}

function Start-Keeper {
    $process = Get-KeeperProcess
    if ($process) {
        Write-Host "Keeper is already running (PID: $($process.Id))" -ForegroundColor Yellow
        return
    }

    # Check environment variables
    if (-not $env:SIMPLISIGN_TOTP_URI -or -not $env:SIMPLISIGN_USERNAME) {
        Write-Host "ERROR: Environment variables not set" -ForegroundColor Red
        Write-Host "Required: SIMPLISIGN_TOTP_URI, SIMPLISIGN_USERNAME" -ForegroundColor Yellow
        return
    }

    Write-Host "Starting SimpliSign Keeper..." -ForegroundColor Cyan

    # Start using VBS (hidden console)
    if (Test-Path $StartupScript) {
        & wscript.exe $StartupScript
    } else {
        # Fallback: start directly
        Start-Process python -ArgumentList "`"$ScriptPath`" --check-interval 300 --log-file `"$LogPath`" --http-port $HealthPort" -WindowStyle Hidden
    }

    Start-Sleep -Seconds 2

    $process = Get-KeeperProcess
    if ($process) {
        Write-Host "Keeper started (PID: $($process.Id))" -ForegroundColor Green
    } else {
        Write-Host "Failed to start keeper" -ForegroundColor Red
        Write-Host "Check logs: $LogPath" -ForegroundColor Yellow
    }
}

function Stop-Keeper {
    $process = Get-KeeperProcess
    if (-not $process) {
        Write-Host "Keeper is not running" -ForegroundColor Yellow
        return
    }

    Write-Host "Stopping SimpliSign Keeper (PID: $($process.Id))..." -ForegroundColor Cyan
    Stop-Process -Id $process.Id -Force
    Write-Host "Keeper stopped" -ForegroundColor Green
}

function Show-Logs {
    param([switch]$Follow)

    if (-not (Test-Path $LogPath)) {
        Write-Host "No log file found at: $LogPath" -ForegroundColor Yellow
        return
    }

    if ($Follow) {
        Write-Host "Following logs (Ctrl+C to stop)..." -ForegroundColor Cyan
        Get-Content $LogPath -Tail 20 -Wait
    } else {
        Get-Content $LogPath -Tail 50
    }
}

function Run-Once {
    Write-Host "Running SimpliSign Keeper once..." -ForegroundColor Cyan
    & python $ScriptPath --once --log-file $LogPath
}

function Install-Startup {
    if (-not (Test-Path $StartupScript)) {
        Write-Host "ERROR: Startup script not found at: $StartupScript" -ForegroundColor Red
        return
    }

    Write-Host "Installing to Startup folder..." -ForegroundColor Cyan
    Copy-Item $StartupScript $StartupLink -Force

    if (Test-Path $StartupLink) {
        Write-Host "Installed successfully" -ForegroundColor Green
        Write-Host "Location: $StartupLink" -ForegroundColor Gray
        Write-Host ""
        Write-Host "The keeper will start automatically on next login" -ForegroundColor Yellow
    } else {
        Write-Host "Failed to install" -ForegroundColor Red
    }
}

function Uninstall-Startup {
    if (Test-Path $StartupLink) {
        Write-Host "Removing from Startup folder..." -ForegroundColor Cyan
        Remove-Item $StartupLink -Force
        Write-Host "Removed successfully" -ForegroundColor Green
    } else {
        Write-Host "Not installed in Startup folder" -ForegroundColor Yellow
    }
}

function Show-Health {
    try {
        $health = Invoke-RestMethod -Uri "http://127.0.0.1:$HealthPort/health" -TimeoutSec 5
        $health | ConvertTo-Json
    } catch {
        Write-Host "Health endpoint not responding" -ForegroundColor Red
        Write-Host "Is the keeper running?" -ForegroundColor Yellow
    }
}

# Main
switch ($Action) {
    "start"     { Start-Keeper }
    "stop"      { Stop-Keeper }
    "status"    { Show-Status }
    "logs"      { Show-Logs -Follow:$f }
    "once"      { Run-Once }
    "install"   { Install-Startup }
    "uninstall" { Uninstall-Startup }
    "health"    { Show-Health }
    default     { Show-Status }
}
