; Kaitu Desktop - NSIS Installer Hooks
; Windows service lifecycle management for Tauri application
;
; CRITICAL: PREINSTALL must fully stop k2.exe process and release file lock
; before NSIS copies new files. Diagnostic log: $TEMP\kaitu-preinstall.log

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
; Runs before copying files. Diagnostic version — writes to $TEMP\kaitu-preinstall.log
!macro NSIS_HOOK_PREINSTALL
  DetailPrint "============================================"
  DetailPrint "Preparing for installation..."
  DetailPrint "============================================"

  ; === Diagnostic log: capture state at each step ===
  FileOpen $9 "$TEMP\kaitu-preinstall.log" w
  FileWrite $9 "=== PREINSTALL started ===$\r$\n"
  FileWrite $9 "INSTDIR=$INSTDIR$\r$\n"
  FileClose $9

  ; Step 1: Check initial state
  DetailPrint "Checking initial state..."
  nsExec::ExecToStack 'sc query ${SERVICE_NAME}'
  Pop $0
  Pop $1
  FileOpen $9 "$TEMP\kaitu-preinstall.log" a
  FileWrite $9 "[1] sc query ${SERVICE_NAME}: exit=$0 output=$1$\r$\n"
  FileClose $9

  nsExec::ExecToStack 'tasklist /FI "IMAGENAME eq k2.exe" /NH'
  Pop $0
  Pop $1
  FileOpen $9 "$TEMP\kaitu-preinstall.log" a
  FileWrite $9 "[1] tasklist k2.exe: exit=$0 output=$1$\r$\n"
  FileClose $9

  ; Step 2: Stop desktop application
  DetailPrint "Stopping desktop application..."
  nsExec::ExecToStack 'taskkill /F /IM "${MAINBINARYNAME}.exe" /T'
  Pop $0
  Pop $1

  ; Step 3: Disable SCM recovery (prevent auto-restart on crash)
  DetailPrint "Disabling service recovery..."
  nsExec::ExecToStack 'sc failure ${SERVICE_NAME} reset= 0 actions= ///  '
  Pop $0
  Pop $1
  FileOpen $9 "$TEMP\kaitu-preinstall.log" a
  FileWrite $9 "[3] sc failure ${SERVICE_NAME}: exit=$0 output=$1$\r$\n"
  FileClose $9

  ; Step 4: Stop service SYNCHRONOUSLY — net stop waits for full stop
  ; (sc stop is async and returns immediately, net stop blocks until stopped)
  DetailPrint "Stopping kaitu service (synchronous)..."
  nsExec::ExecToStack 'net stop ${SERVICE_NAME}'
  Pop $0
  Pop $1
  DetailPrint "net stop ${SERVICE_NAME}: exit=$0"
  FileOpen $9 "$TEMP\kaitu-preinstall.log" a
  FileWrite $9 "[4] net stop ${SERVICE_NAME}: exit=$0 output=$1$\r$\n"
  FileClose $9

  nsExec::ExecToStack 'net stop k2'
  Pop $0
  Pop $1
  nsExec::ExecToStack 'net stop kaitu-service'
  Pop $0
  Pop $1

  ; Step 5: Check state after net stop
  nsExec::ExecToStack 'sc query ${SERVICE_NAME}'
  Pop $0
  Pop $1
  FileOpen $9 "$TEMP\kaitu-preinstall.log" a
  FileWrite $9 "[5] sc query after net stop: exit=$0 output=$1$\r$\n"
  FileClose $9

  nsExec::ExecToStack 'tasklist /FI "IMAGENAME eq k2.exe" /NH'
  Pop $0
  Pop $1
  FileOpen $9 "$TEMP\kaitu-preinstall.log" a
  FileWrite $9 "[5] tasklist after net stop: exit=$0 output=$1$\r$\n"
  FileClose $9

  ; Step 6: Delete service records (prevent any restart)
  DetailPrint "Deleting service records..."
  nsExec::ExecToStack 'sc delete ${SERVICE_NAME}'
  Pop $0
  Pop $1
  FileOpen $9 "$TEMP\kaitu-preinstall.log" a
  FileWrite $9 "[6] sc delete ${SERVICE_NAME}: exit=$0 output=$1$\r$\n"
  FileClose $9

  nsExec::ExecToStack 'sc delete k2'
  Pop $0
  Pop $1
  nsExec::ExecToStack 'sc delete kaitu-service'
  Pop $0
  Pop $1

  ; Step 7: Force kill any remaining processes
  DetailPrint "Force killing processes..."
  nsExec::ExecToStack 'taskkill /F /IM "k2.exe" /T'
  Pop $0
  Pop $1
  FileOpen $9 "$TEMP\kaitu-preinstall.log" a
  FileWrite $9 "[7] taskkill /F k2.exe: exit=$0 output=$1$\r$\n"
  FileClose $9

  nsExec::ExecToStack 'taskkill /F /IM "kaitu-service.exe" /T'
  Pop $0
  Pop $1

  ; Step 8: Wait for process exit + file handle release
  DetailPrint "Waiting for file handles (5 seconds)..."
  Sleep 5000

  ; Step 9: Verify process is gone
  nsExec::ExecToStack 'tasklist /FI "IMAGENAME eq k2.exe" /NH'
  Pop $0
  Pop $1
  FileOpen $9 "$TEMP\kaitu-preinstall.log" a
  FileWrite $9 "[9] tasklist after kill+wait: exit=$0 output=$1$\r$\n"
  FileClose $9

  ; If still running, try kill again and wait more
  nsExec::ExecToStack 'taskkill /F /IM "k2.exe"'
  Pop $0
  Pop $1
  FileOpen $9 "$TEMP\kaitu-preinstall.log" a
  FileWrite $9 "[9] retry taskkill: exit=$0$\r$\n"
  FileClose $9
  Sleep 3000

  ; Step 10: Delete old files
  DetailPrint "Removing old files..."
  Delete "$INSTDIR\${MAINBINARYNAME}.exe"
  Delete "$INSTDIR\k2.exe"
  Delete "$INSTDIR\kaitu-service.exe"
  Delete "$INSTDIR\wintun.dll"
  Delete "$INSTDIR\*.log"
  RMDir /r "$INSTDIR\_app"

  ; Step 11: Verify k2.exe file is deletable
  IfFileExists "$INSTDIR\k2.exe" 0 _pre_k2_gone
    FileOpen $9 "$TEMP\kaitu-preinstall.log" a
    FileWrite $9 "[11] k2.exe STILL EXISTS after Delete — file is LOCKED$\r$\n"
    FileClose $9
    DetailPrint "WARNING: k2.exe still locked!"
    ; Last resort: try again
    nsExec::ExecToStack 'taskkill /F /IM "k2.exe"'
    Pop $0
    Pop $1
    Sleep 3000
    Delete "$INSTDIR\k2.exe"
    Goto _pre_k2_check_done
  _pre_k2_gone:
    FileOpen $9 "$TEMP\kaitu-preinstall.log" a
    FileWrite $9 "[11] k2.exe deleted OK$\r$\n"
    FileClose $9
  _pre_k2_check_done:

  ; Step 12: Clear WebView2 HTTP cache only (preserves localStorage with auth tokens)
  ; localStorage lives in EBWebView/Default/Local Storage/ — must NOT be deleted
  ; Preserves user preferences: update-channel, pre-beta-log-level
  DetailPrint "Clearing WebView2 cache..."
  RMDir /r "$LOCALAPPDATA\io.kaitu.desktop\EBWebView\Default\Cache"
  RMDir /r "$LOCALAPPDATA\io.kaitu.desktop\EBWebView\Default\Code Cache"
  FileOpen $9 "$TEMP\kaitu-preinstall.log" a
  FileWrite $9 "[12] EBWebView cache cleared (Local Storage preserved)$\r$\n"
  FileClose $9

  FileOpen $9 "$TEMP\kaitu-preinstall.log" a
  FileWrite $9 "=== PREINSTALL completed ===$\r$\n"
  FileClose $9

  DetailPrint "============================================"
  DetailPrint "Cleanup completed"
  DetailPrint "============================================"
!macroend

; ============================================================================
; NSIS_HOOK_POSTINSTALL - Post-installation Setup
; ============================================================================
; Runs after all files are installed
; Installs service (idempotent), verifies configuration
!macro NSIS_HOOK_POSTINSTALL
  DetailPrint "============================================"
  DetailPrint "Configuring service..."
  DetailPrint "============================================"

  ; Diagnostic: verify binary was written successfully
  DetailPrint "Binary path: $INSTDIR\${SERVICE_EXE}"
  IfFileExists "$INSTDIR\${SERVICE_EXE}" 0 _post_binary_missing
    DetailPrint "Binary exists: YES"
    Goto _post_binary_check_done
  _post_binary_missing:
    DetailPrint "Binary exists: NO — CRITICAL"
  _post_binary_check_done:

  ; Step 1: Install and start service (idempotent — handles legacy cleanup + recovery config)
  DetailPrint "Installing ${SERVICE_NAME} service..."
  nsExec::ExecToStack '"$INSTDIR\${SERVICE_EXE}" service install'
  Pop $0
  Pop $1
  DetailPrint "Service install exit=$0"
  DetailPrint "Service install output=$1"

  ; Write diagnostic file to install directory (persists after install)
  FileOpen $9 "$INSTDIR\install-diag.log" w
  FileWrite $9 "service_install_exit=$0$\r$\n"
  FileWrite $9 "service_install_output=$1$\r$\n"
  FileClose $9

  Sleep 3000

  ; Step 2: Verify service is running
  DetailPrint "Verifying service status..."
  nsExec::ExecToStack 'sc query ${SERVICE_NAME}'
  Pop $0
  Pop $1
  DetailPrint "Service query exit=$0"

  ; Append verification to diagnostic file
  FileOpen $9 "$INSTDIR\install-diag.log" a
  FileWrite $9 "post_install_sc_query_exit=$0$\r$\n"
  FileWrite $9 "post_install_sc_query_output=$1$\r$\n"
  FileClose $9

  ; Step 3: Create taskbar shortcut
  DetailPrint "Creating taskbar shortcut..."
  ReadRegStr $0 HKCU "Software\Microsoft\Windows\CurrentVersion\Explorer\Shell Folders" "User Pinned"
  ${If} $0 != ""
    CreateShortCut "$0\TaskBar\${PRODUCTNAME}.lnk" "$INSTDIR\${MAINBINARYNAME}.exe" "" "$INSTDIR\${MAINBINARYNAME}.exe" 0
  ${Else}
    StrCpy $0 "$APPDATA\Microsoft\Internet Explorer\Quick Launch\User Pinned\TaskBar"
    CreateDirectory "$0"
    CreateShortCut "$0\${PRODUCTNAME}.lnk" "$INSTDIR\${MAINBINARYNAME}.exe" "" "$INSTDIR\${MAINBINARYNAME}.exe" 0
  ${EndIf}

  ; Step 4: Clear Windows PCA records (prevents forced admin elevation)
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

  ; Step 5: Launch desktop application (as non-elevated user)
  ; Skip in update mode — .onInstSuccess handles restart with correct args
  ${If} $UpdateMode <> 1
    DetailPrint "Starting application..."
    nsis_tauri_utils::RunAsUser "$INSTDIR\${MAINBINARYNAME}.exe" ""
  ${EndIf}

  ; Step 6: Add installation directory to system PATH
  ;         Enables `k2` command in new terminal sessions
  DetailPrint "Adding to system PATH..."
  ReadRegStr $R0 HKLM "SYSTEM\CurrentControlSet\Control\Session Manager\Environment" "Path"
  ${StrLoc} $R1 "$R0" "$INSTDIR" ">"
  ${If} $R1 == ""
    WriteRegExpandStr HKLM "SYSTEM\CurrentControlSet\Control\Session Manager\Environment" "Path" "$R0;$INSTDIR"
    SendMessage ${HWND_BROADCAST} ${WM_SETTINGCHANGE} 0 "STR:Environment" /TIMEOUT=5000
    DetailPrint "Added $INSTDIR to system PATH"
  ${Else}
    DetailPrint "$INSTDIR already in system PATH"
  ${EndIf}

  DetailPrint "============================================"
  DetailPrint "Installation completed"
  DetailPrint "============================================"
!macroend

; ============================================================================
; NSIS_HOOK_PREUNINSTALL - Pre-uninstallation Cleanup
; ============================================================================
; Runs before removing files during uninstallation
!macro NSIS_HOOK_PREUNINSTALL
  DetailPrint "============================================"
  DetailPrint "Preparing for uninstallation..."
  DetailPrint "============================================"

  ; Step 1: Stop desktop application
  DetailPrint "Stopping desktop application..."
  nsExec::ExecToStack 'taskkill /F /IM "${MAINBINARYNAME}.exe" /T'
  Pop $0
  Pop $1

  ; Step 2: Disable recovery + stop service synchronously
  DetailPrint "Stopping services..."
  nsExec::ExecToStack 'sc failure ${SERVICE_NAME} reset= 0 actions= ///  '
  Pop $0
  Pop $1
  nsExec::ExecToStack 'net stop ${SERVICE_NAME}'
  Pop $0
  Pop $1
  nsExec::ExecToStack 'net stop k2'
  Pop $0
  Pop $1
  nsExec::ExecToStack 'net stop kaitu-service'
  Pop $0
  Pop $1

  ; Step 3: Delete service records
  DetailPrint "Deleting service records..."
  nsExec::ExecToStack 'sc delete ${SERVICE_NAME}'
  Pop $0
  Pop $1
  nsExec::ExecToStack 'sc delete k2'
  Pop $0
  Pop $1
  nsExec::ExecToStack 'sc delete kaitu-service'
  Pop $0
  Pop $1

  ; Step 4: Force kill remaining processes
  DetailPrint "Cleaning up processes..."
  nsExec::ExecToStack 'taskkill /F /IM "k2.exe" /T'
  Pop $0
  Pop $1
  nsExec::ExecToStack 'taskkill /F /IM "kaitu-service.exe" /T'
  Pop $0
  Pop $1

  ; Step 5: Wait for file handles
  DetailPrint "Waiting for file handles (5 seconds)..."
  Sleep 5000

  ; Step 6: Remove shortcuts
  DetailPrint "Removing shortcuts..."
  Delete "$DESKTOP\${PRODUCTNAME}.lnk"
  SetShellVarContext all
  Delete "$DESKTOP\${PRODUCTNAME}.lnk"
  SetShellVarContext current

  ReadRegStr $0 HKCU "Software\Microsoft\Windows\CurrentVersion\Explorer\Shell Folders" "User Pinned"
  ${If} $0 != ""
    Delete "$0\TaskBar\${PRODUCTNAME}.lnk"
  ${Else}
    Delete "$APPDATA\Microsoft\Internet Explorer\Quick Launch\User Pinned\TaskBar\${PRODUCTNAME}.lnk"
  ${EndIf}

  ; Step 7: Remove installation directory from system PATH
  DetailPrint "Removing from system PATH..."
  ReadRegStr $R0 HKLM "SYSTEM\CurrentControlSet\Control\Session Manager\Environment" "Path"
  ; Remove ";$INSTDIR" (middle or end entry)
  ${UnStrRep} $R1 "$R0" ";$INSTDIR" ""
  ; Remove "$INSTDIR;" (first entry with others after)
  ${UnStrRep} $R1 "$R1" "$INSTDIR;" ""
  ; Remove "$INSTDIR" alone (sole entry — unlikely but safe)
  ${UnStrRep} $R1 "$R1" "$INSTDIR" ""
  ; Only write back if PATH actually changed
  StrCmp $R1 $R0 _path_unchanged
    WriteRegExpandStr HKLM "SYSTEM\CurrentControlSet\Control\Session Manager\Environment" "Path" "$R1"
    SendMessage ${HWND_BROADCAST} ${WM_SETTINGCHANGE} 0 "STR:Environment" /TIMEOUT=5000
    DetailPrint "Removed $INSTDIR from system PATH"
    Goto _path_remove_done
  _path_unchanged:
    DetailPrint "$INSTDIR not found in system PATH"
  _path_remove_done:

  DetailPrint "============================================"
  DetailPrint "Cleanup completed"
  DetailPrint "============================================"
!macroend
