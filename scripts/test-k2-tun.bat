@echo off
:: K2 TUN Mode Test Launcher
:: Right-click → Run as administrator, OR double-click (auto-elevates via UAC)

:: Check for admin
net session >nul 2>&1
if %errorlevel% neq 0 (
    echo Requesting administrator privileges...
    powershell -Command "Start-Process '%~f0' -Verb RunAs"
    exit /b
)

:: Set paths
set K2_BIN=%~dp0..\desktop\src-tauri\binaries\k2-x86_64-pc-windows-msvc.exe
set K2_CFG=%~dp0..\k2-test-config.yml

echo ============================================
echo   K2 TUN Mode Test - Running as Admin
echo ============================================
echo.
echo Binary: %K2_BIN%
echo Config: %K2_CFG%
echo.
echo Logs:   C:\Users\david\k2-debug.log
echo API:    http://127.0.0.1:1778
echo.
echo Press Ctrl+C to stop the daemon.
echo ============================================
echo.

:: Clear old debug log
del /f "C:\Users\david\k2-debug.log" 2>nul

:: Run k2 daemon in foreground (blocks until Ctrl+C)
"%K2_BIN%" -c "%K2_CFG%"

echo.
echo K2 daemon stopped.
pause
