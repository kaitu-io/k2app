---
name: smartscreen-submit
description: Submit exe to SmartScreen via Chrome DevTools MCP
---

# SmartScreen File Submission

Submit Windows exe to Microsoft Defender SmartScreen for reputation review using Chrome DevTools MCP.

## Prerequisites

- Chrome browser open with DevTools MCP connected. The MCP runs with `--autoConnect`: on the first call Chrome shows a「要允许远程调试吗？」consent dialog and every MCP call (even `list_pages`) hangs until the user clicks 允许 — never click it on their behalf, ask them.
- Must be logged into Microsoft account at microsoft.com (the submission form requires auth)
- The MCP only accepts file paths inside a configured workspace root (the repo checkout). `/tmp` and the session scratchpad are rejected with "Access denied … not within any of the configured workspace roots" — for both `upload_file` and `take_screenshot --filePath`.

## Step 1: Download the exe

```bash
VERSION=$(node -p "require('./package.json').version")
CDN_URL="https://d0.all7.cc/kaitu/desktop/${VERSION}/Kaitu_${VERSION}_x64.exe"
# Inside the repo on purpose: desktop/src-tauri/binaries/ is gitignored and is a workspace root the MCP will accept.
EXE_PATH="$(git rev-parse --show-toplevel)/desktop/src-tauri/binaries/Kaitu_${VERSION}_x64.exe"
curl -sfL -o "$EXE_PATH" "$CDN_URL" && ls -lh "$EXE_PATH"
osslsigncode verify -in "$EXE_PATH" 2>&1 | grep -E "Subject|Issuer" | head -4   # confirm signer/issuer before quoting them in Step 4
```

If a specific version is requested, substitute it. Verify the file downloaded successfully (~11 MB at 0.4.9; it grows with the embedded k2 sidecar). The signer is `Wordgate LLC`, issuer `Certum Code Signing 2021 CA` — if that ever changes, update the Step 4 text.

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
- Signed with: OV code signing certificate issued to Wordgate LLC by Certum Code Signing 2021 CA (Asseco Data Systems), RFC 3161 timestamped
- Download: CDN_URL
- Built via GitHub Actions CI (https://github.com/kaitu-io/k2app)

This is a legitimate VPN application. We are submitting for SmartScreen reputation review as a software developer to ensure our users don't receive false-positive warnings during installation.
```

(Replace VERSION and CDN_URL with actual values)

7. `click` the "Continue" button to proceed to CAPTCHA page

## Step 5: Solve CAPTCHA

1. `take_snapshot` — find the CAPTCHA input field ("Enter the characters you see"). The CAPTCHA image has NO uid in the a11y tree, so it cannot be screenshotted by element.
2. `click` the input field first — that scrolls the CAPTCHA into the viewport — then `take_screenshot` with no `filePath` and no `fullPage` (a viewport shot is sharp; the full-page one is downscaled and hard to read).
3. Microsoft's HIP CAPTCHA renders two rows of 4 characters; the answer is top row + bottom row concatenated (e.g. `6PQ6` over `6JDV` → `6PQ66JDV`).
4. `type_text` the characters (the input is already focused from step 2)
5. `click` the "Submit" button — an "Upload Progress" dialog appears (Uploading… then Submitting…)

### If CAPTCHA fails

Click "New" to refresh the CAPTCHA image and retry from step 5.1.

## Step 6: Verify and cleanup

1. `wait_for` the text `Submission ID` (timeout ≥ 120 s). Do NOT wait for "Thank you" / "submission" / "successfully" — those match text already on the review page and return instantly while the dialog still says "Submitting…". Success navigates to `https://www.microsoft.com/en-us/wdsi/submission/<uuid>` showing `Status: Submitted`; the table's Cloud/Client icons may still read "Malware detected" with `Final determination: Pending` — that is the pre-review state, not a failure. Report the Submission ID and that URL.
2. Clean up: `rm "$EXE_PATH"` (it sits inside the repo's gitignored binaries dir — leaving it there would confuse the next sidecar build)

## Troubleshooting

- **Login expired**: User must log in manually in Chrome. Chrome DevTools MCP uses the browser's existing session.
- **Form layout changed**: Use `take_snapshot` liberally to discover current element UIDs.
- **CAPTCHA unreadable**: Use `take_screenshot` on the CAPTCHA element for a closer look. If still unreadable, click "New" for a fresh one.
- **File upload fails**: Verify exe path exists and is absolute. Use `upload_file` tool with the file input element UID.
