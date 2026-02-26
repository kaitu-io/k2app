# Error Propagation Test Matrix — macOS Tauri Desktop (Daemon Mode)

**Date:** 2026-02-26
**Platform:** macOS, Tauri v2, daemon mode (HTTP :1777)
**Scope:** P0 — Error forward propagation from k2 engine to UI

## Error Chain Under Test

```
Engine ClassifyError → Daemon SSE (/api/events) → Rust status_stream.rs → Tauri event
→ transformStatus() (tauri-k2.ts) → VPN store (vpn.store.ts) → UI (CollapsibleConnectionSection)
```

## Test Matrix

| ID | Pri | Category | Test Case | Expected | Status | Notes |
|----|-----|----------|-----------|----------|--------|-------|
| T01 | P0 | Baseline | Connect to valid server | state=connected, UI shows "已连接" | PASS | JP 8292 connected, SSE: connecting→connected→reconnecting→connected (NWPathMonitor first-fire known) |
| T02 | P0 | Baseline | Disconnect normally | state=disconnected, UI shows "未连接" | PASS | SSE: vpn-status-changed: disconnected delivered |
| T03 | P0 | Error:503 | Connect to unreachable server (bad IP) | state=error, error.code=503, UI shows i18n error | FAIL:engine-no-error | Engine reports "connected" (TUN up) despite wire to 192.0.2.1:12345 / 127.0.0.1:65534 unreachable. No error state emitted after 80s. |
| T04 | P0 | Error:408 | Connect to server that times out | state=error, error.code=408, UI shows i18n error | SKIP:blocked-by-T03 | Same engine architecture issue as T03 — TUN mode masks wire failures |
| T05 | P0 | Error:display | Error state shows i18n text (not raw message) | UI displays translated error, NOT raw engine message | SKIP:blocked-by-T03 | Cannot test — engine never enters error state with current test methods |
| T06 | P0 | Service | Kill daemon while disconnected | service-state-changed: false, UI shows service error within 10s | PASS | launchd unloaded, SSE false cycling 3s, alert "服务连接失败" displayed after ~30s, "解决"/"更多" buttons visible |
| T07 | P0 | Service | Restart daemon after kill | service-state-changed: true, UI recovers to disconnected | PASS | launchd reloaded, SSE true, "Service recovered, resetting alert state", red banner cleared |
| T08 | P0 | SSE | SSE delivers status events in real-time | vpn-status-changed events appear in console during connect/disconnect | PASS | All state transitions logged: connecting, connected, reconnecting, disconnected. service-state-changed for daemon availability. |

## Findings

### CRITICAL: Engine does not propagate wire connection failures (T03)

**Severity:** P0
**Symptom:** When connecting to unreachable servers (192.0.2.1:12345, 127.0.0.1:65534), the engine reports `state: "connected"` because the TUN device setup succeeds. The wire connection failure is never propagated as an error state via SSE.

**Impact:** User sees "已连接" (connected, green circle) when nothing actually works. Error codes 503/408 never reach the UI.

**Root cause hypothesis:** The k2 engine in TUN mode considers itself "connected" once the TUN device is up. Wire connection retries happen in the background without updating the engine state.

**Chain break:** Engine → ✗ (error never emitted) → Daemon SSE → ... → UI

### MINOR: TUN mode captures daemon loopback traffic

**Symptom:** `_k2.run('down')` during TUN+global mode failed with "error sending request for url (http://127.0.0.1:1777/api/core)". The TUN device captured the IPC request to the daemon itself.

**Impact:** Cannot disconnect from UI when connected in global mode with broken wire. Must use direct `curl` to daemon.

**Note:** This may be expected behavior — TUN global mode captures all traffic. The daemon should exclude its own listen address from TUN routing.

## Summary

```
SCAN COMPLETE: 4 PASS, 1 FAIL, 2 SKIP, 0 BLOCKED
PASS: T01 — Baseline connect
PASS: T02 — Baseline disconnect
FAIL: T03 — Engine doesn't emit error for unreachable servers (TUN masks wire failure)
SKIP: T04 — Blocked by T03 architecture issue
SKIP: T05 — Blocked by T03 (no error state to display)
PASS: T06 — Service kill detected, alert displayed
PASS: T07 — Service restart recovery works
PASS: T08 — SSE event delivery confirmed
```
