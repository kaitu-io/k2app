param([string]$File)

# Dynamic signtool discovery — search all installed Windows SDK versions
$signtool = $null
$sdkRoot = "C:\Program Files (x86)\Windows Kits\10\bin"
if (Test-Path $sdkRoot) {
    $signtool = Get-ChildItem -Path $sdkRoot -Recurse -Filter "signtool.exe" -ErrorAction SilentlyContinue |
        Where-Object { $_.FullName -like "*\x64\*" } |
        Sort-Object { [version]($_.Directory.Parent.Name -replace '[^0-9.]','') } -Descending -ErrorAction SilentlyContinue |
        Select-Object -First 1 -ExpandProperty FullName
}

if (-not $signtool) {
    Write-Warning "signtool.exe not found in any Windows SDK version"
    Write-Warning "Install Windows SDK: winget install Microsoft.WindowsSDK.10.0.22621"
    exit 1
}

Write-Host "Using signtool: $signtool"
Write-Host "Signing: $File"
& $signtool sign /fd SHA256 /tr http://time.certum.pl /td SHA256 /d "Kaitu Desktop" $File

if ($LASTEXITCODE -ne 0) {
    Write-Warning "Signing failed (exit code: $LASTEXITCODE)"
    exit 1
}

& $signtool verify /pa $File
Write-Host "Signed successfully: $File"
