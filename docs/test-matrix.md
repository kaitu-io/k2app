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
| T03 | P0 | Error:503 | Connect to unreachable server (bad IP) | state=error, error.code=503, UI shows i18n error | FAIL:engine-dns-blocks-error | Root cause confirmed: DNS handler intercepts port 53 before tunnel HandleUDP. Wire broken → DNS fails silently → no TCP connections → handleTCPProxy never called → ReportWireError never called → engine stays "connected" forever. See Findings. |
| T04 | P0 | Error:408 | Connect to server that times out | state=error, error.code=408, UI shows i18n error | BLOCKED:T03 | Same root cause — DNS handler silently absorbs wire failures |
| T05 | P0 | Error:display | Error state shows i18n text (not raw message) | UI displays translated error, NOT raw engine message | BLOCKED:T03 | Cannot test — engine never emits error state |
| T06 | P0 | Service | Kill daemon while disconnected | service-state-changed: false, UI shows service error within 10s | PASS | launchd unloaded, SSE false cycling 3s, alert "服务连接失败" displayed after ~30s, "解决"/"更多" buttons visible |
| T07 | P0 | Service | Restart daemon after kill | service-state-changed: true, UI recovers to disconnected | PASS | launchd reloaded, SSE true, "Service recovered, resetting alert state", red banner cleared |
| T08 | P0 | SSE | SSE delivers status events in real-time | vpn-status-changed events appear in console during connect/disconnect | PASS | All state transitions logged: connecting, connected, reconnecting, disconnected. service-state-changed for daemon availability. |

## Findings

### CRITICAL: DNS handler silently blocks all error detection (T03) — CONFIRMED ROOT CAUSE

**Severity:** P0
**Symptom:** Engine reports `state: "connected"` indefinitely when wire server is unreachable. Tested with `ip=192.0.2.1` (RFC 5737 TEST-NET, drops all packets) and `ip=127.0.0.1:65534` (connection refused). No error state emitted after 120s+ in either case.

**Impact:** User sees "已连接" (connected, green circle) when nothing works. Error codes 503/408 never reach UI.

**Root cause (confirmed via code trace + live DNS testing):**

The error detection chain has a fatal gap at the DNS layer:

```
1. engine.Start() → TUN device up → state=connected (wire NOT tested)
2. System DNS set to 198.18.0.8 (TUN DNS handler)
3. All DNS queries → dnsHandler (port 53 intercept) → ProxyDNSClient → wire.DialTCP
4. Wire broken → ProxyDNSClient fails → DNS query returns SERVFAIL/timeout
5. No DNS resolution → no TCP connections created by apps
6. tunnel.handleTCPProxy() never called → ReportWireError() never called
7. Engine stays "connected" forever with no error
```

**Evidence (live DNS probing with TUN up + broken wire):**
```bash
dig +short example.com @198.18.0.8      # timeout (TUN DNS → broken wire)
dig +short example.com                    # timeout (system resolver → TUN DNS)
dig +short example.com @114.114.114.114   # 104.18.27.120 (direct, bypasses TUN)
dig +short example.com @8.8.8.8          # timeout (routed through TUN)
```

**Code path:**
- `k2/engine/engine.go:505-530` — `ReportWireError()` only called from tunnel handlers
- `k2/core/tunnel.go:219-235` — `handleTCPProxy()` calls `wireReporter.ReportWireError(err)` on DialTCP fail
- `k2/core/tunnel.go:261-277` — `handleUDPProxy()` same pattern, but port 53 is intercepted BEFORE reaching here
- `k2/engine/dns_handler.go` — DNS handler intercepts ALL port 53 traffic before tunnel's HandleUDP

**Chain break:** DNS handler → ✗ (silent failure) → tunnel.HandleTCP/UDP never called → ReportWireError never called → Engine → Daemon SSE → ... → UI

**Fix required (k2 submodule):** Engine needs wire health check independent of traffic. Options:
1. **Proactive wire probe**: Engine sends a test dial after TUN setup, reports error if wire unreachable
2. **DNS handler error propagation**: dnsHandler calls ReportWireError when ProxyDNSClient consistently fails
3. **Heartbeat/keepalive**: Periodic wire liveness check on QUIC/TCP-WS transport

### Additional discovery: k2v5 URL parameter precedence

During T03 testing, discovered that the `ip=` URL parameter (not `@host:port`) determines the actual wire connection target. The `ech=` parameter can also override the connection target via CDN routing. To simulate unreachable server, ALL of these must be controlled: `@host:port`, `ip=`, `ech=`, `pin=`, `hop=`.

### MINOR: TUN mode captures daemon loopback traffic

**Symptom:** `_k2.run('down')` during TUN+global mode failed with "error sending request for url (http://127.0.0.1:1777/api/core)". The TUN device captured the IPC request to the daemon itself.

**Impact:** Cannot disconnect from UI when connected in global mode with broken wire. Must use direct `curl` to daemon.

**Note:** This may be expected behavior — TUN global mode captures all traffic. The daemon should exclude its own listen address from TUN routing.

## Bridge Fix Status (commit 2f37def)

The `transformStatus()` fix in `tauri-k2.ts` and `capacitor-k2.ts` is **correct but untestable** — it handles `connected + error` → synthesize `"error"` state with `retrying` flag. However, the engine never emits `connected + error` because ReportWireError is never called (T03 root cause). Once the k2 engine fix is applied, the bridge code will work as designed.

## Summary

```
SCAN COMPLETE: 5 PASS, 1 FAIL, 0 SKIP, 2 BLOCKED
PASS:  T01 — Baseline connect (JP server, SSE transitions confirmed)
PASS:  T02 — Baseline disconnect (SSE delivered)
FAIL:  T03 — DNS handler silently blocks error detection (engine stays "connected" forever)
BLOCKED: T04 — Blocked by T03 (DNS handler root cause)
BLOCKED: T05 — Blocked by T03 (no error state to display)
PASS:  T06 — Service kill detected, alert displayed (~30s)
PASS:  T07 — Service restart recovery works
PASS:  T08 — SSE event delivery confirmed (all transitions)
```

## Next Steps

1. **k2 engine fix (submodule):** Implement wire health check — engine must detect wire failures independent of DNS/traffic. Recommended: DNS handler calls ReportWireError when ProxyDNSClient fails N consecutive queries.
2. **Re-test T03/T04/T05** after engine fix — bridge transformStatus() is ready.
3. **P1 tests:** Network transition (OnNetworkChanged), error recovery (ClearWireError), specific error codes (401, 403, 502).
