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

### WeChat — DEFERRED
User must launch WeChat then run `ps -ax -o comm | grep -i wechat`.

### Telegram — DEFERRED
Same.

---

## §12.4 — Icon scheme POC

**Status:** DEFERRED to user (needs running Tauri dev build + DevTools).

Handler code in `desktop/src-tauri/src/icon_protocol.rs` compiles and registers via `register_uri_scheme_protocol`. Real `<img src="kaitu-icon://bundle/com.apple.Safari">` rendering test must be done by user:

```
cd /Users/david/projects/kaitu-io/k2app/.claude/worktrees/v0.4.5+app-bypass
make dev-macos
# wait for window, Cmd+Opt+I to open DevTools, then in console:
const img = new Image();
img.src = 'kaitu-icon://bundle/com.apple.Safari';
img.onload = () => console.log('PASS', img.width, img.height);
img.onerror = (e) => console.error('FAIL', e);
document.body.appendChild(img);
```

---

## §12.5 Decision gate

Per spec §12.5: "v1 GA 标准 = 至少 3 平台过；不接受'全 4 平台砍只剩 1'".

Current status:
- **macOS:** ✅ PASS (after fixes 3c56a62 + 82025a9)
- **Windows:** deferred — but only nil-stub for now; needs v2 work to wire a Windows process searcher
- **Linux desktop:** deferred — needs no-PackageResolver Linux variant
- **Android:** deferred — code path looks correct, needs real-device validation

**Risk:** if Windows + Linux + Android all need additional submodule work, GA bar (≥3) may not be met. macOS alone is insufficient. Recommend the user prioritize Android validation (largest user base after macOS, and the code path already wires LinuxProcessSearcher).

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
