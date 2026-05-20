# App Bypass Phase 0 — Verification Results

**Date:** 2026-05-13
**Branch:** v0.4.5/app-bypass

---

## §12.2 — Attribution end-to-end smoke

### macOS sysext / daemon path — **✅ PASS (after 2 k2-submodule fixes)**

**Initial finding:** Production k2 0.4.3 (system Kaitu app) does NOT do process_name attribution on macOS. Hand-crafted ClientConfig with bypass route was ignored — both curl and other tools got VPN exit IP.

**Root-cause investigation (2 bugs found):**

1. **`daemon.engineConfigFromClientConfig` never set `ProcessSearcher`** — `engine.Config.ProcessSearcher` was nil → `tunnel.processSearcher` nil → `meta.ProcessName` always empty → process_name rules never fire. Fix: wire `provider.DarwinProcessSearcher{}` on darwin builds. **Commit `3c56a62`** (k2 submodule).

2. **`provider/process_darwin.go` `lsofLookup` returned the daemon's own command name** — `lsof -i tcp@<ip>:<port>` returns BOTH endpoints (daemon listening + client connecting). Original parser returned first `c<command>` line, which was the daemon (lower PID). Fix: switch to `-F pc` format, track PIDs, skip `os.Getpid()`. **Commit `82025a9`** (k2 submodule).

**Smoke verification with patched binary `/tmp/k2-phase0/k2-with-attribution` (proxy mode on port 1778):**

| Test | Routes | Expected | Got | Verdict |
|---|---|---|---|---|
| Positive | `process_name:['curl']` direct + match-all k2v5 | curl via SOCKS5 → ISP IP | `49.237.39.249` (= ISP) | ✅ PASS |
| Negative | `process_name:['wget']` direct + match-all k2v5 | curl via SOCKS5 → VPN IP | `38.54.23.249` (= server) | ✅ PASS |

Both sides of the rule engine match work: matched process bypasses tunnel, non-matched process tunnels. Attribution chain is functional.

### Windows desktop — DEFERRED to user

Needs `make build-windows` + run on Windows host with k2v5 URL. Same smoke procedure (curl.exe vs Edge). The fix in commit `3c56a62` only wires Darwin — Windows still has `newProcessSearcher() → nil` in `daemon/process_other.go`. Windows attribution support is a **v2 ticket** (Windows process lookup via `GetExtendedTcpTable2` not yet implemented).

### Linux desktop — DEFERRED to user

Needs `make build-linux` + run on Linux host. Linux already has `provider.LinuxProcessSearcher` but it requires a `PackageResolver` (Android-only). For Linux desktop a no-PackageResolver variant is needed. Currently `daemon/process_other.go` returns nil. **v2 ticket.**

### Android — DEFERRED to user

Needs real Android device or emulator. `appext/process_linux.go:makeProcessSearcher` wires `NewLinuxProcessSearcher(pr)` correctly. Should work, but unverified at smoke level.

---

## §12.3 — macOS helper-name verification

Captured 2026-05-13 via `ps -ax -o comm | grep <App> | xargs basename | sort -u` with apps running.

### Google Chrome
```
Google Chrome
Google Chrome Helper
Google Chrome Helper (GPU)
Google Chrome Helper (Renderer)
chrome_crashpad_handler
```
5 basenames. Note `chrome_crashpad_handler` shared with any Electron app.

### Slack (Electron)
```
ShipIt
Slack
Slack Helper
Slack Helper (Plugin)
Slack Helper (Renderer)
chrome_crashpad_handler
```
6 basenames. **`chrome_crashpad_handler` is shared with Chrome** — blacklisting one's crashpad has no effect on the user (no traffic) but worth noting in support docs.

### Zoom
```
CptHost
ZoomClips
aomhost
caphost
zoom.us
```
5 basenames. **Validates path-prefix algorithm**: Zoom doesn't use "<App> Helper" naming convention; product code names. The macOS bundle enumeration in `app_list.rs` filters by path-under-bundleURL, not name pattern → works correctly.

### Telegram — **CAPTURED 2026-05-14**
```
Telegram
```
**1 basename**. Telegram macOS 是 single-process bundle，没有 `Contents/Helpers/`；`/Applications/Telegram.app/Contents/MacOS/` 只有一个 `Telegram` 可执行 + `Frameworks/` 里的 swift dylibs。`pids_by_type(All)` 前缀匹配 bundle URL 只命中主进程 PID。bundleId = `ru.keepcoder.Telegram`. **Validates single-process branch** of `app_list.rs::macos::enumerate` — `collect_helper_basenames` 返回 1 元素时 UI 显示 "屏蔽 1 个进程"。

### WeChat — STILL DEFERRED
Worktree machine 没装微信。需在装有 WeChat for macOS 的开发机上跑 `ps -ax -o comm | grep -i wechat` 抓 helper basenames。不阻塞 v0.4.5（UI / 规则匹配链路跟具体 bundle 解耦）。

---

## §12.4 — Tauri icon POC (macOS) — **✅ PASS (2026-05-14)**

Tested via Tauri MCP against running dev build (`yarn tauri dev --features mcp-bridge`, Vite :1420). Verification: `_platform.appList.listRunning()` returned 45 apps; spot-fetched icons via `<img src="kaitu-icon://bundle/...">`:

| bundle | naturalW×H | verdict |
|---|---|---|
| `com.apple.Safari` | 1024×1024 | ✅ |
| `com.apple.finder` | 1024×1024 | ✅ |
| `com.apple.loginwindow` | 1024×1024 | ✅ (generic system icon — Apple design) |
| `com.apple.controlcenter` | 1024×1024 | ✅ |
| `com.googlecode.iterm2` | 1024×1024 | ✅ |
| `com.microsoft.VSCode` | 1024×1024 | ✅ |
| `com.tinyspeck.slackmacgap` | 1024×1024 | ✅ |
| `com.runningwithcrayons.Alfred` | 1024×1024 | ✅ |

**Gotcha**: WebKit `fetch()` API refuses custom URI schemes — must test via `new Image()` or `<img src>`. Production webapp uses `<img>` so this is fine; just don't waste time grepping for `fetch('kaitu-icon://...')`.

**End-to-end UX smoke** (drove webview at `/app-bypass`):
- Rule card + count summary + 智能分流国家 picker rendered correctly
- 添加更多 list shows running apps with icons + 加入 buttons
- Clicking 加入 on Slack: count `0 手动` → `1 手动`, "我手动添加(1)" section appears with "屏蔽 6 个进程", Slack auto-dedups from available list
- Clicking 移除: count back to 0, manual section disappears, Slack returns to available

Screenshots: `/tmp/k2-phase0/appbypass-macos-{initial,rendered,slack-filter,slack-added}.png`

### UX follow-ups

**Fix 1 + 2 applied in same session (2026-05-14)** to `desktop/src-tauri/src/app_list.rs::macos::enumerate`:

1. **System-daemon noise** — RESOLVED. Skip any `bundle_id.starts_with("com.apple")`. Hides ~15 system daemons (loginwindow / dock / WindowManager / controlcenter / systemuiserver / Spotlight / etc.) and also `com.apple.Safari` / `com.apple.Mail` / `com.apple.Messages` / `com.apple.Photos` / etc. **Trade-off acknowledged**: Apple-bundled user apps disappear from the autocomplete list — power users wanting to bypass Safari can use "+ 手动添加" button (process name input).
2. **Duplicate entries** — RESOLVED. `NSWorkspace.runningApplications()` returns one entry per PID; multi-process bundles (Chrome × 2, Docker × 2, Chrome Helper × 3 in test) had duplicate rows. Added `seen_ids: HashSet<String>` dedup, first-seen wins (all instances of same bundle have identical bundle_url so process_names collected are identical anyway).
3. **Search filter false-positive** — NOT REPRODUCED. JS simulation of `filteredAvailable` logic returns correct 2-entry match for "slack". Tauri MCP synthetic input events do not propagate to React `onChange` reliably on WebKit, so I cannot conclusively test via automation. **Pending: real keyboard typing by user to confirm.**

**Before/after counts** (same dev machine): `listRunning()` 48 → 16; 0 Apple bundles, 0 duplicates. Cargo build 5.93s, zero warnings. Regression: Telegram add → "屏蔽 1 个进程" (correct, single-process bundle); Chrome add → "屏蔽 5 个进程" (correct, all helpers collected).

**Fix 4: Sub-bundle filtering** — RESOLVED (same session). User pointed out that searching "chrome" produced both "Google Chrome" and "Google Chrome Helper" as separate entries despite the main bundle's processNames already including all helper basenames. Same for Slack / Docker Desktop / DeepLUninstall. Added post-pass in `enumerate()`: drop any candidate whose `bundle_path` is a strict descendant of another candidate's `bundle_path` (e.g. `/Applications/Slack.app/.../Slack Helper (Plugin).app` lives under `/Applications/Slack.app`). DeepLUninstall.app also dropped, confirming it's nested inside DeepL.app (not a sibling install).

Final count: 48 → 12 on dev machine, one row per user-facing top-level app. Cargo rebuild 6.24s.

Screenshots: `/tmp/k2-phase0/appbypass-macos-{initial,rendered,slack-filter,slack-added,after-dedup-filter,final-v2}.png` (6 stages).

---

## §12.5 Decision gate

Per spec §12.5: "v1 GA 标准 = 至少 3 平台过；不接受'全 4 平台砍只剩 1'".

Current status (2026-05-14):
- **macOS:** ✅ PASS — attribution (commits `3c56a62` + `82025a9`) + icon POC + full UX smoke
- **Android:** ✅ PASS — Redmi K40 Pro UAT, kernel-level `VpnService.Builder.addDisallowedApplication`
- **Windows:** deferred to v0.5.x — `k2/daemon/process_other.go` is nil-stub; needs `GetExtendedTcpTable2` searcher (per Spec §12.5 推荐 Option A: webapp `features.appBypass` 在 Windows 上 platform-gate 为 false 出 v0.4.5)
- **Linux desktop:** deferred — needs no-PackageResolver LinuxProcessSearcher variant

**GA bar:** 2/3 met (macOS + Android). Spec §12.5 says ≥3 platforms. **Pending user decision** to relax that bar for v0.4.5 or implement Windows attribution before GA.

---

## Test artifacts staged at `/tmp/k2-phase0/`

```
k2-with-attribution      ← patched k2 binary (21 MB) with both fixes
k2-proxy-config.yml      ← run config for proxy-mode smoke
k2v5-url.txt             ← real k2v5 URL extracted from system daemon
up-bypass-proxy.json     ← /api/core up params: bypass curl
up-bypass-wget.json      ← /api/core up params: bypass wget (negative control)
macos-config.json        ← TUN-mode config template (curl bypass)
windows-config.json
linux-config.json
android-config.json
template.md              ← user runbook
```

All files survive across sessions; user can re-run smokes with these as templates after the next packaged build.
