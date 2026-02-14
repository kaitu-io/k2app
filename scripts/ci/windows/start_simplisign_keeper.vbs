' SimpliSign Keeper Startup Script
'
' Place this file in the Windows Startup folder:
'   %APPDATA%\Microsoft\Windows\Start Menu\Programs\Startup
'
' This VBS script runs Python without a visible console window.
' It reads credentials from User environment variables.

Set WshShell = CreateObject("WScript.Shell")
Set WshEnv = WshShell.Environment("User")

' Read credentials from User environment variables
totpUri = WshEnv("SIMPLISIGN_TOTP_URI")
username = WshEnv("SIMPLISIGN_USERNAME")
pin = WshEnv("SIMPLISIGN_PIN")

' Configuration
pythonPath = "python"
scriptPath = "C:\actions-runner\scripts\simplisign_keeper.py"
logPath = "C:\actions-runner\logs\simplisign_keeper.log"
checkInterval = "300"
httpPort = "8778"

' Build command with credentials
cmd = pythonPath & " """ & scriptPath & """"
cmd = cmd & " --totp-uri """ & totpUri & """"
cmd = cmd & " --username """ & username & """"
If pin <> "" Then
    cmd = cmd & " --pin """ & pin & """"
End If
cmd = cmd & " --check-interval " & checkInterval
cmd = cmd & " --log-file """ & logPath & """"
cmd = cmd & " --http-port " & httpPort

' Run hidden (0 = hidden, False = don't wait)
WshShell.Run cmd, 0, False
