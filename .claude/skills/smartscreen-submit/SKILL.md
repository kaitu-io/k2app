---
name: smartscreen-submit
description: Submit exe to SmartScreen via Chrome DevTools MCP
---

# SmartScreen File Submission

Submit Windows exe to Microsoft Defender SmartScreen for reputation review using Chrome DevTools MCP.

## Prerequisites

- Chrome browser open with DevTools MCP connected
- Must be logged into Microsoft account at microsoft.com (the submission form requires auth)

## Step 1: Download the exe

```bash
VERSION=$(node -p "require('./package.json').version")
CDN_URL="https://d0.all7.cc/kaitu/desktop/${VERSION}/Kaitu_${VERSION}_x64.exe"
EXE_PATH="/tmp/Kaitu_${VERSION}_x64.exe"
curl -sfL -o "$EXE_PATH" "$CDN_URL" && ls -lh "$EXE_PATH"
```

If a specific version is requested, substitute it. Verify the file downloaded successfully (should be ~7-8MB).

## Step 2: Navigate to submission page

```
navigate_page → https://www.microsoft.com/en-us/wdsi/filesubmission
```

Wait for the page to load, then `take_snapshot` to see the current state.

### If not logged in

If the snapshot shows "Sign in" link, tell the user to log in manually in Chrome. Wait for them to confirm, then navigate again.

### If redirected to login after clicking Continue

Same — tell user to log in manually, then re-navigate.

## Step 3: Select role and continue

1. `take_snapshot` — find the "Software developer" radio button
2. `click` the "Software developer" option
3. `click` the "Continue" button
4. Wait 3 seconds for form to load
5. `take_snapshot` to verify form appeared

## Step 4: Fill the form

Use `take_snapshot` to find element UIDs, then fill:

1. **Product dropdown** — Click the dropdown trigger (usually a "Select" button), then click "Microsoft Defender Smartscreen"
2. **Company Name** — `fill` with `WORDGATE LLC`
3. **File upload** — `upload_file` with the exe path from Step 1
4. **Detection type** — Click "Incorrectly detected as malware/malicious"
5. **Detection name** — `fill` with `SmartScreen`
6. **Additional information** — `fill` with:

```
This is our officially signed Windows desktop installer for Kaitu VPN (version VERSION).

- Publisher: Kaitu (https://kaitu.io)
- Signed with: OV code signing certificate (SSL.com)
- Download: CDN_URL
- Built via GitHub Actions CI (https://github.com/kaitu-io/k2app)

This is a legitimate VPN application. We are submitting for SmartScreen reputation review as a software developer to ensure our users don't receive false-positive warnings during installation.
```

(Replace VERSION and CDN_URL with actual values)

7. `click` the "Continue" button to proceed to CAPTCHA page

## Step 5: Solve CAPTCHA

1. `take_snapshot` — find the CAPTCHA image and input field
2. `take_screenshot` with the CAPTCHA image element UID — read the characters visually
3. `click` the CAPTCHA input field
4. `type_text` the characters
5. `click` the "Submit" button

### If CAPTCHA fails

Click "New" to refresh the CAPTCHA image and retry from step 5.1.

## Step 6: Verify and cleanup

1. `take_snapshot` to verify submission succeeded (look for confirmation message)
2. Clean up: `rm /tmp/Kaitu_*_x64.exe`

## Troubleshooting

- **Login expired**: User must log in manually in Chrome. Chrome DevTools MCP uses the browser's existing session.
- **Form layout changed**: Use `take_snapshot` liberally to discover current element UIDs.
- **CAPTCHA unreadable**: Use `take_screenshot` on the CAPTCHA element for a closer look. If still unreadable, click "New" for a fresh one.
- **File upload fails**: Verify exe path exists and is absolute. Use `upload_file` tool with the file input element UID.
