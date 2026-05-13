# App Bypass Phase 0 — Verification Results

**Date:** 2026-05-13
**Branch:** v0.4.5/app-bypass

Tracks results of the blocking Phase 0 verification per spec §12. Each section either records a pass/fail or marks "deferred to user" if the step requires a real connection / device the controller cannot access.

---

## §12.3 — macOS helper-name verification

**Method:** `ps -ax -o comm | grep <App> | xargs basename | sort -u`

Captured on host machine (macOS) 2026-05-13. Three of five spec apps were running; remaining 2 (WeChat, Telegram) need user action.

### Google Chrome

```
Google Chrome
Google Chrome Helper
Google Chrome Helper (GPU)
Google Chrome Helper (Renderer)
chrome_crashpad_handler
```

5 distinct basenames. Confirms the plan's expectation that Chrome has multiple helpers (Renderer, GPU, plus the always-present crashpad).

**Note for fixtures:** "Google Chrome Helper (NetworkService)" was NOT in this sample — implies that variant only spawns when chrome's network service is actively dispatching. Real production may include it; test fixtures should accept both presence and absence.

### Slack (Electron)

```
ShipIt
Slack
Slack Helper
Slack Helper (Plugin)
Slack Helper (Renderer)
chrome_crashpad_handler
```

6 distinct basenames. Note `chrome_crashpad_handler` is **shared with Chrome** (Slack embeds Electron which embeds Chromium).

**Implication for the user:** Blacklisting Chrome → Slack's crashpad also gets blacklisted (and vice versa). Functionally harmless (crashpad doesn't generate network traffic that matters), but mention in docs if a user reports "I blacklisted Chrome but Slack stopped working" — almost certainly something else.

### Zoom

```
CptHost
ZoomClips
aomhost
caphost
zoom.us
```

5 distinct basenames. **Zoom does NOT follow the "App Helper" naming convention** — they use product code names (CptHost = "Captures Host", aomhost = "Audio/Media host"). This validates our bundle-prefix algorithm: we match by *path* (under bundleURL), not by name pattern. ✅

### WeChat — DEFERRED

User needs to launch WeChat on macOS then re-run `ps -ax -o comm | grep -i wechat`.

### Telegram — DEFERRED

Same: user launches Telegram then `ps -ax -o comm | grep -i telegram`.

---

## §12.2 — Attribution end-to-end smoke

**Status:** BLOCKED on test k2v5 URL.

`mcp__k2__status` returns `{"state":"disconnected"}`. `mcp__k2__account_info` returns "not logged in". No k2v5 URL is programmatically available; user must provide one (or log in via `mcp__k2__login` so MCP can fetch one).

**Smoke artifacts will be staged at** `/tmp/k2-phase0/` once a URL is available:

- `macos-config.json` — process_name=['curl'] direct route
- `windows-config.json` — process_name=['curl.exe']
- `linux-config.json` — process_name=['curl']
- `android-config.json` — package_name=['com.android.chrome']

**Test endpoint:** `https://ip.kaitu.io/`

| Platform | Test | Pass if |
|---|---|---|
| macOS | `curl https://ip.kaitu.io/` vs browser visit | IPs differ |
| Windows | `curl.exe https://ip.kaitu.io/` vs Edge | IPs differ |
| Linux | `curl https://ip.kaitu.io/` vs firefox | IPs differ |
| Android | Chrome vs Firefox visit | IPs differ |

---

## §12.4 — Icon scheme POC

**Status:** Deferred to user with running Tauri dev build / Android emulator.

The handler code is implemented and Rust-side compiles cleanly. Real `<img src="kaitu-icon://bundle/com.apple.Safari">` rendering test must be done in the live WebView's DevTools console.

User steps:
1. `cd /Users/david/projects/kaitu-io/k2app/.claude/worktrees/v0.4.5+app-bypass && make dev-macos`
2. Wait for window to appear
3. Open DevTools (Cmd+Option+I)
4. Paste into console:
   ```js
   const img = new Image();
   img.src = 'kaitu-icon://bundle/com.apple.Safari';
   img.onload = () => console.log('PASS', img.width, img.height);
   img.onerror = (e) => console.error('FAIL', e);
   document.body.appendChild(img);
   ```
5. Record result here

Same procedure for Android with `kaitu-icon://package/com.android.chrome` after `make dev-android`.

---

## Decision gate

Per spec §12.5:
- §12.2 attribution: requires real connect to evaluate. If <3 platforms pass → block v1 merge.
- §12.3 helper names: 3/5 captured already; 2 deferred. **Not blocking**, only used as test fixtures.
- §12.4 icon POC: best-effort; fallback to first-letter Avatar if scheme broken.

Currently: **2 of 3 sections blocked on user-only steps**. Test fixtures (§12.3 portion) and smoke artifacts ready for user execution.
