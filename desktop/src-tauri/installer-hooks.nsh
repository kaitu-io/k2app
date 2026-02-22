; Kaitu Desktop - NSIS Installer Hooks
; Windows service lifecycle management for Tauri application
;
; Service lifecycle split strategy:
;   sc stop/start              — used directly in NSIS for precise lifecycle control
;   sc failure/query/qc        — used for configuration and verification
;   k2.exe service install     — sc create + sc start (k2 handles binPath + displayName)
;   k2.exe service uninstall   — sc delete (+ idempotent sc stop)
;
; WHY: k2.exe service uninstall does sc stop + sc delete atomically. The SCM may
; kill the process before VPN routes/DNS are restored. Splitting stop (NSIS sc stop)
; from uninstall (k2.exe service uninstall) gives the engine 10s of protected cleanup.

; ============================================================================
; Configuration - Replace these values as needed
; ============================================================================
!define SERVICE_NAME "kaitu"
!define SERVICE_EXE "k2.exe"

; ============================================================================
; NSIS_HOOK_PREINIT - System Requirements Check
; ============================================================================
; Runs at the very beginning of .onInit, before any other initialization
; Checks if the system meets minimum requirements (Windows 10 1809+)
;
; Windows 10 1809 (Build 17763) is required because:
; - SetInterfaceDnsSettings API (used for DNS configuration) requires this version
; - Earlier versions will fail with ERROR_PROC_NOT_FOUND
!macro NSIS_HOOK_PREINIT
  ; Get Windows version from kernel32.dll (more reliable than ${AtLeastWin10})
  GetDllVersion "$SYSDIR\kernel32.dll" $R0 $R1

  ; Extract version components
  IntOp $R2 $R0 >> 16      ; Major version
  IntOp $R2 $R2 & 0xFFFF
  IntOp $R3 $R1 >> 16      ; Build number
  IntOp $R3 $R3 & 0xFFFF

  ; Check: Windows 10+ (Major >= 10) AND Build >= 17763
  ${If} $R2 < 10
    MessageBox MB_OK|MB_ICONSTOP "$(windowsVersionTooOld)"
    Abort
  ${EndIf}

  ${If} $R2 == 10
  ${AndIf} $R3 < 17763
    MessageBox MB_OK|MB_ICONSTOP "$(windowsVersionTooOld)"
    Abort
  ${EndIf}
!macroend

; ============================================================================
; NSIS_HOOK_PREINSTALL - Pre-installation Cleanup
; ============================================================================
; Runs before copying files
; Stops existing service and cleans up old installation
!macro NSIS_HOOK_PREINSTALL
  DetailPrint "============================================"
  DetailPrint "Preparing for installation..."
  DetailPrint "============================================"

  ; Step 1: Stop desktop application (release file handles)
  DetailPrint "Stopping desktop application..."
  nsExec::ExecToStack 'taskkill /F /IM "${MAINBINARYNAME}.exe" /T'
  Pop $0
  Pop $1

  ; Step 2: Wait for file handles to be released
  DetailPrint "Waiting for file handles (3 seconds)..."
  Sleep 3000

  ; Step 3: Send stop signal — service stays registered with SCM
  ; This triggers engine.Stop() → VPN teardown → routes/DNS restoration
  DetailPrint "Stopping service (VPN cleanup)..."
  nsExec::ExecToStack 'sc stop ${SERVICE_NAME}'
  Pop $0
  Pop $1
  DetailPrint "Service stop result: $0"

  ; Step 4: Wait for VPN cleanup to complete
  ; Service is still registered, so SCM won't kill it during cleanup
  DetailPrint "Waiting for VPN cleanup (10 seconds)..."
  Sleep 10000

  ; Step 5: Deregister service (VPN already cleaned up, safe to delete)
  DetailPrint "Uninstalling service..."
  nsExec::ExecToStack '"$INSTDIR\${SERVICE_EXE}" service uninstall'
  Pop $0
  Pop $1
  DetailPrint "Service uninstall result: $0"

  ; Step 6: Wait for deregistration to complete
  Sleep 2000

  ; Step 7: Force kill any remaining processes (safety net)
  DetailPrint "Cleaning up processes..."
  nsExec::ExecToStack 'taskkill /F /IM "k2.exe" /T'
  Pop $0
  Pop $1
  Sleep 1000

  ; Step 8: Delete old files to ensure clean install
  DetailPrint "Removing old files..."
  Delete "$INSTDIR\${MAINBINARYNAME}.exe"
  Delete "$INSTDIR\k2.exe"
  Delete "$INSTDIR\wintun.dll"
  Delete "$INSTDIR\*.log"
  RMDir /r "$INSTDIR\_app"

  DetailPrint "============================================"
  DetailPrint "Cleanup completed"
  DetailPrint "============================================"
!macroend

; ============================================================================
; NSIS_HOOK_POSTINSTALL - Post-installation Setup
; ============================================================================
; Runs after all files are installed
; Installs service, configures recovery, verifies configuration
!macro NSIS_HOOK_POSTINSTALL
  DetailPrint "============================================"
  DetailPrint "Configuring service..."
  DetailPrint "============================================"

  ; Step 1: Install and start service
  DetailPrint "Installing and starting ${SERVICE_NAME} service..."
  nsExec::ExecToStack '"$INSTDIR\${SERVICE_EXE}" service install'
  Pop $0
  Pop $1
  DetailPrint "Service up result: $0"
  Sleep 2000

  ; Step 2: Configure service recovery options (Keepalive)
  ; - reset=86400: Reset failure count after 24 hours (86400 seconds)
  ; - actions: restart/5000 = restart after 5 seconds
  ;   (first failure, second failure, subsequent failures)
  DetailPrint "Configuring service recovery (keepalive)..."
  nsExec::ExecToStack 'sc failure ${SERVICE_NAME} reset= 86400 actions= restart/5000/restart/5000/restart/5000'
  Pop $0
  Pop $1
  DetailPrint "Recovery config result: $0"

  ; Step 3: Verify service configuration (binPath, start type)
  DetailPrint "Verifying service configuration..."
  nsExec::ExecToStack 'sc qc ${SERVICE_NAME}'
  Pop $0
  Pop $1
  DetailPrint "$1"

  ; Step 4: Verify service is running
  DetailPrint "Verifying service status..."
  nsExec::ExecToStack 'sc query ${SERVICE_NAME}'
  Pop $0
  Pop $1
  DetailPrint "$1"

  ; Step 5: Create taskbar shortcut
  DetailPrint "Creating taskbar shortcut..."
  ReadRegStr $0 HKCU "Software\Microsoft\Windows\CurrentVersion\Explorer\Shell Folders" "User Pinned"
  ${If} $0 != ""
    CreateShortCut "$0\TaskBar\${PRODUCTNAME}.lnk" "$INSTDIR\${MAINBINARYNAME}.exe" "" "$INSTDIR\${MAINBINARYNAME}.exe" 0
  ${Else}
    StrCpy $0 "$APPDATA\Microsoft\Internet Explorer\Quick Launch\User Pinned\TaskBar"
    CreateDirectory "$0"
    CreateShortCut "$0\${PRODUCTNAME}.lnk" "$INSTDIR\${MAINBINARYNAME}.exe" "" "$INSTDIR\${MAINBINARYNAME}.exe" 0
  ${EndIf}

  ; Step 6: Clear Windows PCA records (prevents forced admin elevation)
  DetailPrint "Clearing compatibility records..."
  nsExec::ExecToStack 'reg delete "HKCU\Software\Microsoft\Windows NT\CurrentVersion\AppCompatFlags\Compatibility Assistant\Store" /v "$INSTDIR\${MAINBINARYNAME}.exe" /f'
  Pop $0
  Pop $1
  nsExec::ExecToStack 'reg delete "HKCU\Software\Microsoft\Windows NT\CurrentVersion\AppCompatFlags\Compatibility Assistant\Store" /v "$INSTDIR\${SERVICE_EXE}" /f'
  Pop $0
  Pop $1
  nsExec::ExecToStack 'reg delete "HKCU\Software\Microsoft\Windows NT\CurrentVersion\AppCompatFlags\Layers" /v "$INSTDIR\${MAINBINARYNAME}.exe" /f'
  Pop $0
  Pop $1
  nsExec::ExecToStack 'reg delete "HKCU\Software\Microsoft\Windows NT\CurrentVersion\AppCompatFlags\Layers" /v "$INSTDIR\${SERVICE_EXE}" /f'
  Pop $0
  Pop $1

  ; Step 7: Launch desktop application
  DetailPrint "Starting application..."
  Exec '"$INSTDIR\${MAINBINARYNAME}.exe"'

  DetailPrint "============================================"
  DetailPrint "Installation completed"
  DetailPrint "============================================"
!macroend

; ============================================================================
; NSIS_HOOK_PREUNINSTALL - Pre-uninstallation Cleanup
; ============================================================================
; Runs before removing files during uninstallation
; Splits stop (VPN cleanup) from uninstall (deregistration) for safe teardown
!macro NSIS_HOOK_PREUNINSTALL
  DetailPrint "============================================"
  DetailPrint "Preparing for uninstallation..."
  DetailPrint "============================================"

  ; Step 1: Stop desktop application
  DetailPrint "Stopping desktop application..."
  nsExec::ExecToStack 'taskkill /F /IM "${MAINBINARYNAME}.exe" /T'
  Pop $0
  Pop $1

  ; Step 2: Wait for file handles
  DetailPrint "Waiting for file handles (3 seconds)..."
  Sleep 3000

  ; Step 3: Send stop signal — service stays registered with SCM
  DetailPrint "Stopping service (VPN cleanup)..."
  nsExec::ExecToStack 'sc stop ${SERVICE_NAME}'
  Pop $0
  Pop $1
  DetailPrint "Service stop result: $0"

  ; Step 4: Wait for VPN cleanup (routes/DNS restoration)
  DetailPrint "Waiting for VPN cleanup (10 seconds)..."
  Sleep 10000

  ; Step 5: Deregister service (VPN already cleaned up)
  DetailPrint "Uninstalling service..."
  nsExec::ExecToStack '"$INSTDIR\${SERVICE_EXE}" service uninstall'
  Pop $0
  Pop $1
  DetailPrint "Service uninstall result: $0"

  ; Step 6: Wait for deregistration to complete
  Sleep 2000

  ; Step 7: Force kill any remaining processes (safety net)
  DetailPrint "Cleaning up processes..."
  nsExec::ExecToStack 'taskkill /F /IM "k2.exe" /T'
  Pop $0
  Pop $1
  Sleep 1000

  ; Step 8: Remove shortcuts
  DetailPrint "Removing shortcuts..."
  ; Desktop shortcut (current user)
  Delete "$DESKTOP\${PRODUCTNAME}.lnk"
  ; Desktop shortcut (all users)
  SetShellVarContext all
  Delete "$DESKTOP\${PRODUCTNAME}.lnk"
  SetShellVarContext current

  ; Taskbar shortcut
  ReadRegStr $0 HKCU "Software\Microsoft\Windows\CurrentVersion\Explorer\Shell Folders" "User Pinned"
  ${If} $0 != ""
    Delete "$0\TaskBar\${PRODUCTNAME}.lnk"
  ${Else}
    Delete "$APPDATA\Microsoft\Internet Explorer\Quick Launch\User Pinned\TaskBar\${PRODUCTNAME}.lnk"
  ${EndIf}

  DetailPrint "============================================"
  DetailPrint "Cleanup completed"
  DetailPrint "============================================"
!macroend
