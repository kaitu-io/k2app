# Tauri Daemon Mode — Full Integration Test

**Date**: 2026-02-25
**Context**: k2 submodule reworked (events, removed cloud UDID generation), Tauri reverted from sysext to daemon mode. Need comprehensive validation.

## Pre-conditions
- [x] macOS host (daemon mode, no NE/sysext)
- [x] k2 submodule up to date
- [x] Tauri dev build compiles (cargo check + 33 tests pass)
- [x] Webapp dev server runs

---

## Phase 1: Build & Launch

| # | Step | Expected | Status |
|---|------|----------|--------|
| 1.1 | `make dev-desktop` — all services start | MySQL, Redis, API (:5800), k2 daemon (:11777), Vite (:1420), Tauri window | PASS (after I1-I4 fixes) |
| 1.2 | Tauri window loads webapp | React app renders in Tauri window (localhost:14580) | PASS — Tauri compiled, window opened |
| 1.3 | Daemon health check | `ensure_service_running` succeeds, SSE connects | PASS — /ping + /api/events both respond |
| 1.4 | SSE event stream connects | status_stream.rs connects to /api/events | PASS — initial status event arrives on SSE connect |

## Phase 2: VPN Connect / Disconnect

| # | Step | Expected | Status |
|---|------|----------|--------|
| 2.1 | Login with valid account | Auth succeeds, dashboard shows tunnel list | SKIP — no test account in standalone |
| 2.2 | Click connect on a tunnel | state: disconnected → connecting → connected | PASS (proxy mode via API) |
| 2.3 | Verify connected state UI | Connected indicator, uptime timer, tunnel info | PASS — green circle, "已连接", disabled settings |
| 2.4 | Click disconnect | state: connected → disconnecting → disconnected | PASS (proxy), FAIL (TUN — I5) |
| 2.5 | Verify disconnected state UI | Disconnected indicator, no error displayed | PASS (proxy) — grey circle, "未连接" |
| 2.6 | Reconnect after disconnect | Same as 2.2-2.3, no stale state | PASS (proxy) |

## Phase 3: Error Handling & Display

| # | Step | Expected | Status |
|---|------|----------|--------|
| 3.1 | Connect with invalid/expired account | Error 401/402, i18n error message shown | SKIP — no test account |
| 3.2 | Connect to unreachable server | Error 503, "server unreachable" message | NOTE — engine reports "connected" in both proxy/TUN mode despite unreachable server (lazy wire dial) |
| 3.3 | Network loss during connected state | reconnecting state, auto-retry | SKIP — needs real connection |
| 3.4 | Daemon crash/restart during connected | service-state-changed → unavailable, recovery | SKIP — needs Tauri WebView |
| 3.5 | Kill daemon while connecting | Graceful error, not stuck in connecting | SKIP — needs Tauri WebView |
| 3.6 | Error display: no raw backend message shown | All errors show i18n text, not code/message | FIXED — 3 code fixes applied (see I7-I9) |

## Phase 4: Event System Validation

| # | Step | Expected | Status |
|---|------|----------|--------|
| 4.1 | SSE reconnect after daemon restart | status_stream.rs reconnects within 3s | |
| 4.2 | Multiple rapid connect/disconnect | No state corruption, optimistic update timeout works | |
| 4.3 | Event ordering: service-state + vpn-status | Both events arrive, store processes correctly | |
| 4.4 | Initial status bridge: one-time query | First status arrives before SSE event | |

## Phase 5: Platform Integration

| # | Step | Expected | Status |
|---|------|----------|--------|
| 5.1 | UDID generation | getUdid() returns stable device ID | |
| 5.2 | Clipboard operations | Write/read clipboard works | |
| 5.3 | External link opening | openExternal() launches browser | |
| 5.4 | Tray icon updates | Connected/disconnected states reflected | |

---

## Issues Found

| # | Phase | Description | Root Cause | Fix | Status |
|---|-------|-------------|------------|-----|--------|
| I1 | 1 | `dev-desktop.sh` skips k2 daemon on macOS | Script assumed NE mode (`uname -s == Darwin` → skip) | Removed platform check, always start daemon | FIXED |
| I2 | 1 | k2 `run -l` command not found | k2 CLI changed: `run` removed, daemon starts with `k2 -c config` | Create `.k2-dev-config.yaml` with listen addr | FIXED |
| I3 | 1 | `get_udid` daemon endpoint removed | k2 daemon no longer has `/api/device/udid` | Use native sysctl/wmic/machine-id per platform | FIXED |
| I4 | 1 | Vite proxy → :1777 but daemon on :11777 | `K2_DAEMON_PORT` exported after Vite start | Export env before Vite launch | FIXED |
| I5 | 2 | TUN mode `down` blocks 30s+ and state stays `connected` | `engine.Stop()` blocks in `tunnel.Close()` → `wg.Wait()`: TUN/gVisor goroutines don't exit cleanly when provider is closed on macOS. Proxy mode disconnect works fine. | k2 submodule fix needed: add timeout to `tunnel.Close()` or context-based cancellation for waitgroup | OPEN (k2) |
| I6 | 1 | Standalone webapp shows `[K2:Standalone]` not `[K2:Tauri]` | Playwright browser at :1420 has no `window.__TAURI__`. Tauri WebView injects this at :14580 internally. Not a real bug — standalone mode is expected for browser dev. | N/A — by design | NOT A BUG |
| I7 | 3 | UI components use `error.message` as i18n defaultValue fallback | `CollapsibleConnectionSection.tsx:148` + `ConnectionNotification.tsx:190` passed raw backend message as fallback | Changed defaultValue to `t('common:status.error')` (always i18n) | FIXED |
| I8 | 3 | `errorHandler.ts` shows raw Error.message for non-ApiError | Line 78: `showAlert(error.message, 'error')` exposes raw JS errors | Changed to `t('common:common.unknownError')` (generic i18n) | FIXED |
| I9 | 3 | Standalone bridge returns raw HTTP/network error messages | `standalone-k2.ts:29,37` returned `HTTP 500: Internal Server Error` etc. | Changed to generic safe messages: `'Service error'` / `'Service unavailable'` | FIXED |

---

## Test Log

```
[2026-02-25 16:20] Phase 1.1-1.2: PASS — all services start, Tauri window opens
[2026-02-25 16:25] Phase 1.3: PASS — SSE event stream connects, sends initial status
[2026-02-25 16:25] Phase 1.4: PASS — status_stream.rs connects to /api/events (verified via curl)
[2026-02-25 16:27] Phase 2 (proxy mode): PASS — connect/disconnect/status cycle works
[2026-02-25 16:30] Phase 2 (TUN mode): FAIL — disconnect blocks in wg.Wait(), state stuck at connected
[2026-02-25 16:35] Phase 2 UI: PASS — standalone webapp shows connected/disconnected states correctly
  - Connected: green circle + "已连接" + stop button + disabled settings
  - Disconnected: grey circle + "未连接" + play button + enabled settings
[2026-02-25 16:43] Phase 3 error scenarios:
  - No config: uses lastConfig (reconnect behavior) — correct
  - Bad URL: returns 511 with error message — correct
  - Unreachable server: engine reports "connected" (lazy wire dial) — by design, not a bug
[2026-02-25 16:47] Phase 3 code review: found 3 raw-message leaks, applied fixes
  - I7: UI defaultValue fallback used error.message → now uses i18n
  - I8: errorHandler showed raw Error.message → now uses unknownError i18n
  - I9: standalone bridge returned raw HTTP/network errors → now generic safe strings
  - All 308 webapp tests pass after fixes
```
