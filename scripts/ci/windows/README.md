# SimpliSign Auto-Login Scripts

Automated code signing with SimpliSign for Windows builds in GitHub Actions.

## Overview

These scripts automate SimpliSign Desktop authentication using TOTP (Time-based One-Time Password), eliminating manual OTP input during CI/CD builds.

**Reference**: [How to Automate Signing Your Windows App with Certum](https://www.devas.life/how-to-automate-signing-your-windows-app-with-certum/)

## Files

| File | Purpose |
|------|---------|
| `simplisign_auto_login.py` | Python script for TOTP generation and GUI automation |
| `simplisign_login.ps1` | PowerShell wrapper for GitHub Actions |
| `deploy_to_runner.ps1` | Deployment script to copy files to Windows Runner |
| `start-runner-interactive.ps1` | Startup script to run GitHub Actions Runner interactively |
| `README.md` | This file |

## Critical Requirement: Interactive Session

**IMPORTANT**: Code signing with SimplySign requires the GitHub Actions runner to run in the **same Windows session** as SimplySign Desktop. This is a Windows security limitation - services (Session 0) cannot interact with desktop applications (Session 1+).

### Runner Configuration Options

**Option 1: Interactive Runner (Recommended for Code Signing)**
1. Stop the runner service: `Stop-Service actions.runner.*`
2. The `GH Runner.lnk` shortcut in Startup folder will auto-start the runner on user login
3. User must be logged into Windows desktop (RDP or console)
4. SimplySign Desktop must be running in the same session

**Option 2: Service Mode (No Code Signing)**
1. Runner runs as Windows service (default)
2. Code signing will fail silently (marked as `continue-on-error` in workflow)
3. Builds still succeed, but unsigned

### Verifying Session Configuration

```powershell
# Check runner session (should be > 0 for interactive)
Get-Process Runner.Listener | Select-Object ProcessName, SessionId

# Check SimplySign session (must match runner)
Get-Process SimplySignDesktop | Select-Object ProcessName, SessionId
```

## Prerequisites

### 1. SimpliSign Desktop

Install SimpliSign Desktop on Windows Runner:
- Download from: https://www.certum.eu/en/simplysign-desktop/
- Install and verify it runs

### 2. Python Dependencies

```powershell
pip install pywinauto pyotp
```

### 3. GitHub Secrets

Configure in repository Settings → Secrets and variables → Actions:

| Secret Name | Description | Example |
|-------------|-------------|---------|
| `SIMPLISIGN_TOTP_URI` | Full `otpauth://` URI from QR code | `otpauth://totp/Certum:user@example.com?secret=ABC123...&issuer=Certum` |
| `SIMPLISIGN_USERNAME` | SimpliSign account username | `user@example.com` |
| `SIMPLISIGN_PIN` | SimpliSign PIN (optional) | `1234` |

## Setup Instructions

### Step 1: Obtain TOTP URI

Choose one of the following methods:

**Method A: Reset 2FA in SimpliSign Account**
1. Log in to https://www.certum.eu/en/simplysign/
2. Go to Security Settings → Two-Factor Authentication
3. Select "Reset 2FA" or "Re-setup 2FA"
4. When QR code appears:
   - Right-click → "Inspect Element" (F12)
   - Search for `otpauth://` in HTML
   - Copy the full URI: `otpauth://totp/Certum:username?secret=BASE32&issuer=Certum`

**Method B: Contact Certum Support**
- Email: support@certum.pl
- Request: "Need to reset 2FA to get otpauth:// URI for CI/CD automation"

**Method C: Extract from existing QR code image**
- If you saved the QR code screenshot
- Use online decoder: https://zxing.org/w/decode
- Upload image → get `otpauth://` URI

### Step 2: Test TOTP Generation Locally

```powershell
# Test OTP generation without login
python simplisign_auto_login.py `
  --totp-uri "otpauth://totp/Certum:user@example.com?secret=ABC123&issuer=Certum" `
  --username "user@example.com" `
  --test-otp-only
```

Expected output:
```
Generated OTP: 123456
Test mode: OTP generation successful
```

Verify this OTP matches your mobile app.

### Step 3: Test Full Auto-Login

```powershell
# Ensure SimpliSign Desktop is running
# Run auto-login
python simplisign_auto_login.py `
  --totp-uri "otpauth://..." `
  --username "user@example.com" `
  --pin "1234"
```

Expected behavior:
1. Script connects to SimpliSign Desktop
2. Clicks "Connect" button (if not logged in)
3. Fills username, OTP, PIN
4. Clicks "Login"
5. Waits 5 seconds
6. Verifies "Connected" status

### Step 4: Deploy to Windows Runner

```powershell
# Run deployment script
.\deploy_to_runner.ps1
```

This copies scripts to `C:\actions-runner\scripts\`.

### Step 5: Configure GitHub Actions

Update `.github/workflows/test-build-windows.yml`:

```yaml
- name: Authenticate SimpliSign
  run: |
    C:\actions-runner\scripts\simplisign_login.ps1
  env:
    SIMPLISIGN_TOTP_URI: ${{ secrets.SIMPLISIGN_TOTP_URI }}
    SIMPLISIGN_USERNAME: ${{ secrets.SIMPLISIGN_USERNAME }}
    SIMPLISIGN_PIN: ${{ secrets.SIMPLISIGN_PIN }}
```

### Step 6: Enable Code Signing Step

Uncomment the signing step in workflow:

```yaml
- name: Sign installer with SimpliSign
  run: |
    $installer = Get-ChildItem -Path "client\desktop-tauri\src-tauri\target\release\bundle\nsis\*.exe" | Select-Object -First 1
    & "C:\Program Files (x86)\Windows Kits\10\bin\10.0.22621.0\x64\signtool.exe" sign `
      /fd SHA256 `
      /tr http://time.certum.pl `
      /td SHA256 `
      /d "Kaitu Desktop" `
      "$($installer.FullName)"
```

## Troubleshooting

### OTP Generation Fails

**Error**: `Invalid TOTP URI format`
- **Fix**: Verify URI starts with `otpauth://totp/` and contains `secret=` parameter
- **Check**: Use `--test-otp-only` flag to test URI parsing

### Login Window Not Found

**Error**: `ERROR: Login dialog did not appear`
- **Fix**: Ensure SimpliSign Desktop is installed and running
- **Check**: Manually launch SimpliSign Desktop and verify it opens

### OTP Mismatch

**Error**: Login fails with incorrect OTP
- **Fix**: Verify system time is synchronized
- **Check**: Run `w32tm /resync` on Windows

```powershell
w32tm /config /manualpeerlist:"time.windows.com" /syncfromflags:manual /reliable:YES /update
net stop w32time && net start w32time
w32tm /resync
```

### Python Packages Missing

**Error**: `ModuleNotFoundError: No module named 'pywinauto'`
- **Fix**: Install packages: `pip install pywinauto pyotp`
- **Note**: PowerShell wrapper auto-installs missing packages

### GUI Automation Fails

**Error**: `ElementNotFoundError: Connect button not found`
- **Fix**: Update auto_id selectors in Python script to match SimpliSign Desktop version
- **Debug**: Use pywinauto's `inspect.exe` tool to find correct control IDs

### Session Mismatch (Code Signing Fails)

**Error**: `SimplySign window not found` or `TimeoutError` in pywinauto
- **Cause**: Runner is in Session 0 (service), SimplySign is in Session 1 (desktop)
- **Fix**: Run the runner interactively, not as a service
- **Steps**:
  1. Stop the service: `Stop-Service actions.runner.*`
  2. Log into Windows desktop (RDP or console)
  3. The startup shortcut will auto-start the runner
  4. Or manually run: `C:\actions-runner\start-runner-interactive.ps1`

## Security Notes

- **Never commit TOTP URI to git** - Use GitHub Secrets only
- **Rotate TOTP secret** if exposed - Reset 2FA in SimpliSign account
- **Limit secret access** - Only admins should have access to GitHub Secrets
- **Audit logs** - Review GitHub Actions logs for unauthorized access

## Manual Testing on Windows Runner

```powershell
# SSH to Windows Runner
ssh david@<windows-runner-ip>

# Set environment variables
$env:SIMPLISIGN_TOTP_URI = "otpauth://..."
$env:SIMPLISIGN_USERNAME = "user@example.com"
$env:SIMPLISIGN_PIN = "1234"

# Run login script
C:\actions-runner\scripts\simplisign_login.ps1
```

## Integration with Workflow

Full example workflow step:

```yaml
- name: Build and sign desktop app
  run: |
    # Authenticate SimpliSign first
    C:\actions-runner\scripts\simplisign_login.ps1

    # Build app
    cd client/desktop-tauri
    yarn tauri build

    # Sign installer
    $installer = Get-ChildItem "src-tauri\target\release\bundle\nsis\*.exe" | Select-Object -First 1
    signtool sign /fd SHA256 /tr http://time.certum.pl /td SHA256 /d "Kaitu Desktop" $installer.FullName

    # Verify signature
    signtool verify /pa $installer.FullName
  env:
    SIMPLISIGN_TOTP_URI: ${{ secrets.SIMPLISIGN_TOTP_URI }}
    SIMPLISIGN_USERNAME: ${{ secrets.SIMPLISIGN_USERNAME }}
    SIMPLISIGN_PIN: ${{ secrets.SIMPLISIGN_PIN }}
```

## References

- [devas.life: Automate Windows Code Signing](https://www.devas.life/how-to-automate-signing-your-windows-app-with-certum/)
- [PyOTP Documentation](https://pyauth.github.io/pyotp/)
- [pywinauto Documentation](https://pywinauto.readthedocs.io/)
- [Certum SimplySign Desktop](https://www.certum.eu/en/simplysign-desktop/)

## License

MIT (same as Kaitu project)
