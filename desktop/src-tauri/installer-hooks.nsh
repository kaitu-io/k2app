; Kaitu Desktop - NSIS Installer Hooks
; Windows service lifecycle management for Tauri application
;
; Service management unified with 'k2 svc' commands:
;   - k2.exe svc up      Install/update and start service (handles all cases)
;   - k2.exe svc down    Stop and uninstall service
;   - k2.exe svc status  Show service status
;
; 'svc up' automatically handles:
;   - Migration from k2 if needed
;   - Service installation if not present
;   - Service updates if binary path changed
;   - Ensures service is running
;
; IMPORTANT: Do NOT use sc.exe for service creation/deletion.
; Only use sc.exe for: querying status (sc query) and configuring recovery (sc failure)

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

  ; Step 3: Stop and uninstall service using unified 'svc down' command
  ; This command handles: stop VPN, cleanup routes/DNS, uninstall service
  ; Works for both k2 and legacy k2
  DetailPrint "Stopping and uninstalling service..."
  nsExec::ExecToStack '"$INSTDIR\${SERVICE_EXE}" svc down'
  Pop $0
  Pop $1
  DetailPrint "Service down result: $0"

  ; Step 4: Wait for VPN cleanup to complete
  ; Service needs time to: stop VPN, restore routes, cleanup interfaces
  DetailPrint "Waiting for cleanup (10 seconds)..."
  Sleep 10000

  ; Step 5: Force kill any remaining processes (safety measure)
  DetailPrint "Cleaning up processes..."
  nsExec::ExecToStack 'taskkill /F /IM "k2.exe" /T'
  Pop $0
  Pop $1
  nsExec::ExecToStack 'taskkill /F /IM "k2.exe" /T'
  Pop $0
  Pop $1
  Sleep 1000

  ; Step 6: Delete old files to ensure clean install
  DetailPrint "Removing old files..."
  Delete "$INSTDIR\${MAINBINARYNAME}.exe"
  Delete "$INSTDIR\k2.exe"
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
; Uses unified 'svc up' command to handle all service setup
!macro NSIS_HOOK_POSTINSTALL
  DetailPrint "============================================"
  DetailPrint "Configuring service..."
  DetailPrint "============================================"

  ; Step 1: Install/update and start service using unified 'svc up' command
  ; This command automatically handles:
  ; - Migration from k2 if needed
  ; - Service installation if not present
  ; - Service updates if binary path changed
  ; - Starting the service
  DetailPrint "Installing and starting ${SERVICE_NAME} service..."
  nsExec::ExecToStack '"$INSTDIR\${SERVICE_EXE}" svc up'
  Pop $0
  Pop $1
  DetailPrint "Service up result: $0"
  Sleep 3000

  ; Step 2: Configure service recovery options (Keepalive)
  ; This ensures the service auto-restarts on failure
  ; - reset=86400: Reset failure count after 24 hours (86400 seconds)
  ; - actions: restart/5000 = restart after 5 seconds
  ;   (first failure, second failure, subsequent failures)
  DetailPrint "Configuring service recovery (keepalive)..."
  nsExec::ExecToStack 'sc failure ${SERVICE_NAME} reset= 86400 actions= restart/5000/restart/5000/restart/5000'
  Pop $0
  Pop $1
  DetailPrint "Recovery config result: $0"

  ; Step 3: Verify service is running
  DetailPrint "Verifying service status..."
  nsExec::ExecToStack 'sc query ${SERVICE_NAME}'
  Pop $0
  Pop $1
  DetailPrint "$1"

  ; Step 6: Create taskbar shortcut
  DetailPrint "Creating taskbar shortcut..."
  ReadRegStr $0 HKCU "Software\Microsoft\Windows\CurrentVersion\Explorer\Shell Folders" "User Pinned"
  ${If} $0 != ""
    CreateShortCut "$0\TaskBar\${PRODUCTNAME}.lnk" "$INSTDIR\${MAINBINARYNAME}.exe" "" "$INSTDIR\${MAINBINARYNAME}.exe" 0
  ${Else}
    StrCpy $0 "$APPDATA\Microsoft\Internet Explorer\Quick Launch\User Pinned\TaskBar"
    CreateDirectory "$0"
    CreateShortCut "$0\${PRODUCTNAME}.lnk" "$INSTDIR\${MAINBINARYNAME}.exe" "" "$INSTDIR\${MAINBINARYNAME}.exe" 0
  ${EndIf}

  ; Step 7: Clear Windows PCA records (prevents forced admin elevation)
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

  ; Step 8: Launch desktop application
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
; Uses unified 'svc down' command to stop and uninstall service
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

  ; Step 3: Stop and uninstall service using unified 'svc down' command
  ; This command handles: stop VPN, cleanup routes/DNS, uninstall service
  DetailPrint "Stopping and uninstalling ${SERVICE_NAME} service..."
  nsExec::ExecToStack '"$INSTDIR\${SERVICE_EXE}" svc down'
  Pop $0
  Pop $1
  DetailPrint "Service down result: $0"

  ; Step 4: Wait for VPN cleanup
  DetailPrint "Waiting for cleanup (10 seconds)..."
  Sleep 10000

  ; Step 5: Force kill any remaining processes
  DetailPrint "Cleaning up processes..."
  nsExec::ExecToStack 'taskkill /F /IM "k2.exe" /T'
  Pop $0
  Pop $1
  Sleep 1000

  ; Step 7: Remove shortcuts
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
