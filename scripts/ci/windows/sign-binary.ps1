param([string]$File)

# Tauri signCommand entry point — called once per binary during bundling.
# Based on the proven sign.ps1 approach: dynamic signtool discovery, no /sha1.

$sdkRoot = "C:\Program Files (x86)\Windows Kits\10\bin"
$signtool = Get-ChildItem -Path $sdkRoot -Recurse -Filter "signtool.exe" -ErrorAction SilentlyContinue |
    Where-Object { $_.FullName -like "*\x64\*" } |
    Sort-Object { [version]($_.Directory.Parent.Name -replace '[^0-9.]','') } -Descending -ErrorAction SilentlyContinue |
    Select-Object -First 1 -ExpandProperty FullName

if (-not $signtool) {
    Write-Error "signtool.exe not found in Windows SDK"
    exit 1
}

# Authenticode description follows the brand (K2_BRAND is exported by the
# Makefile and reaches Tauri's signCommand). Kaitu keeps the historical literal.
$desc = if ($env:K2_BRAND -eq 'overleap') { 'Overleap' } else { 'Kaitu' }

& $signtool sign /fd SHA256 /tr http://time.certum.pl /td SHA256 /d $desc $File
exit $LASTEXITCODE
