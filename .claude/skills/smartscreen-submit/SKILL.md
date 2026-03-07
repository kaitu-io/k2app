---
name: smartscreen-submit
description: Submit exe to SmartScreen via Playwright script
---

# SmartScreen File Submission

Run the Playwright script to automate form filling, then handle CAPTCHA via MCP.

## Step 1: Run the script

```bash
node .claude/skills/smartscreen-submit/submit.mjs [version]
```

Version is optional — defaults to `package.json` version. The script:
1. Downloads the exe from CDN
2. Opens browser with persistent login session
3. Navigates to https://www.microsoft.com/en-us/wdsi/filesubmission
4. Selects "Software developer", fills all form fields, uploads exe
5. Clicks Continue → pauses at CAPTCHA page

## Step 2: Solve CAPTCHA via Playwright MCP

The script pauses with the browser open at the CAPTCHA page. Use Playwright MCP to solve it:

1. `browser_snapshot` — find the CAPTCHA image and input field
2. `browser_take_screenshot` — screenshot the CAPTCHA image element
3. Read the characters from the screenshot
4. `browser_click` on the CAPTCHA input, then `browser_type` the characters
5. `browser_click` the **Submit** button
6. If CAPTCHA fails, click **New** to refresh and retry

After submission succeeds, press Enter in the terminal to let the script clean up.

## Troubleshooting

- **Login expired**: Script prompts for manual login. Session persists via `--user-data-dir`.
- **Script fails mid-form**: Browser stays open for debugging. Press Enter to close.
- **CAPTCHA retry**: Click "New" button via MCP to get fresh CAPTCHA.
- **Playwright not installed**: `npx playwright install chromium`
