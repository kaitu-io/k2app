param([string]$File)

$signtool = "C:\Program Files (x86)\Windows Kits\10\bin\10.0.22621.0\x64\signtool.exe"

if (-not (Test-Path $signtool)) {
    Write-Warning "signtool.exe not found, skipping signing"
    exit 0
}

Write-Host "Signing: $File"
& $signtool sign /fd SHA256 /tr http://time.certum.pl /td SHA256 /d "Kaitu Desktop" $File

if ($LASTEXITCODE -ne 0) {
    Write-Warning "Signing failed (exit code: $LASTEXITCODE)"
    exit 0
}
