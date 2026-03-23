# VPN Machine Safety-Net Poll

**Date**: 2026-03-23
**Status**: Draft
**Scope**: `webapp/src/stores/vpn-machine.store.ts` + tests

---

## Problem

Users on iOS see the UI stuck in `reconnecting` (0.4.1+) even when the VPN engine has recovered. They must manually toggle VPN off/on to restore the display.

### Root Cause (Structural, Code-Based)

The iOS K2Plugin fires `vpnStateChange` exclusively via `NEVPNStatusDidChange` (K2Plugin.swift `registerStatusObserver()`):

```swift
NotificationCenter.default.addObserver(
    forName: .NEVPNStatusDidChange,
    object: vpnManager?.connection, ...
) { notification in
    let state = mapVPNStatus(connection.status)   // NEVPNStatus enum only
    self?.notifyListeners("vpnStateChange", data: ["state": state])
}
```

`NEVPNStatus` is a system-level enum: `.connecting / .connected / .disconnecting / .disconnected / .reasserting / .invalid`. Engine-level error overlays (`state=connected + error:{code:108}`) are invisible to this enum — the system VPN stays `.connected` regardless of wire errors. Therefore:

| Engine event | iOS `vpnStateChange` fired? | Android `vpnStateChange` fired? |
|---|---|---|
| VPN connect / disconnect (system) | ✅ | ✅ |
| `error:{code:108}` appears on connected | ❌ never | ✅ via `onStatus()` |
| `error` clears → clean `connected` | ❌ never | ✅ via `onStatus()` |

On Android, `K2Plugin.onStatus(statusJSON)` (K2Plugin.kt `onStatus()`) is called by the VPN service on every engine state change — including error overlay transitions — so push delivery is reliable. On iOS there is no equivalent mechanism.

**The only way to observe error overlay changes on iOS is `getStatus()`**, which calls `sendProviderMessage("status")` to the NE process and receives the full JSON response including the error field.

### Full Stuck Scenario (iOS)

1. App opens. Initial `getStatus()` returns `{state:"connected", error:{code:108}}`.
2. `transformStatus()` synthesizes `state='error', retrying=true`.
3. VPNMachine: `BACKEND_ERROR + isRetrying=true` → `reconnecting`.
4. Engine clears wire error → NE process now has `{state:"connected"}` (no error).
5. `NEVPNStatus` stays `.connected` throughout. No `NEVPNStatusDidChange` fires. No `vpnStateChange` event.
6. UI stays in `reconnecting` indefinitely — until the safety-net poll fires and calls `getStatus()`, which returns clean `{state:"connected"}` → `BACKEND_CONNECTED` → `reconnecting → connected`.

---

## Solution: Safety-Net Poll

Add a **15-second periodic poll** inside the event-driven initialization branch, after the existing one-time initial query. The poll calls `window._k2.run('status')` and pipes the result through the existing `dispatchStatus()` helper — the same function used by push events.

```
Primary path (fast):   NE push → onStatusChange → dispatchStatus()
Safety-net (15s):      setInterval → run('status') → dispatchStatus()
```

Both paths call the same `dispatchStatus()`. VPNMachine dispatch is idempotent: same-state transitions are no-ops (e.g., machine already `connected`, poll returns `connected` → `BACKEND_CONNECTED` in `connected` state → no transition defined → no-op).

### Why 15 Seconds

- Engine typically clears `code=108` within 2–10s of the error occurring.
- Worst-case: user sees `reconnecting` for up to 15s before UI self-corrects. Acceptable.
- 30s: too long — real user impact observed at ~20s window (ticket #63).
- 10s: marginal improvement, higher IPC call rate for negligible gain.
- Battery/IPC cost: 1 `sendProviderMessage` per 15s is negligible relative to the VPN tunnel.

### Error Handling

Poll failures are **fully silent**: no `SERVICE_UNREACHABLE` dispatch, regardless of whether `run('status')` throws or returns `code !== 0`. The poll is an eventual-consistency supplement, not a service availability probe. `SERVICE_UNREACHABLE` remains the sole responsibility of `onServiceStateChange`, which runs independently. This is an intentional asymmetry with the standalone polling fallback (which does dispatch `SERVICE_UNREACHABLE`) — the standalone fallback is the *primary* health monitor for that mode; the safety-net poll is not.

### Tauri Desktop

Tauri desktop also uses event-driven mode. The poll will fire there too every 15s, but SSE delivery is reliable so results will be idempotent no-ops. If the daemon is down (`serviceDown` state), `run('status')` will throw and be silently swallowed — the machine stays in `serviceDown` correctly via the existing `onServiceStateChange` mechanism.

---

## Implementation

**Single file change**: `webapp/src/stores/vpn-machine.store.ts`

**Location**: After the one-time initial status query block, before the `return () => {` cleanup statement — both within the `if (window._k2?.onServiceStateChange && window._k2?.onStatusChange)` event-driven branch.

```typescript
// Safety-net poll: on iOS, NEVPNStatusDidChange does not fire for engine-level error
// overlay changes (connected+error ↔ connected). This poll is the only reliable path
// to recover from a stale reconnecting state on iOS. Silent failures are intentional —
// this is not a health probe; NE push events remain the primary delivery mechanism.
const safetyNetInterval = setInterval(async () => {
  try {
    const resp = await window._k2.run('status') as any;
    if (resp.code === 0 && resp.data) {
      dispatchStatus(resp.data);
    }
    // resp.code !== 0: silent. Not a service health failure — handled by onServiceStateChange.
  } catch {
    // Silent: poll is eventual-consistency supplement, not primary health probe.
  }
}, 15_000);
```

Update the cleanup `return` statement to include `clearInterval`:

```typescript
return () => {
  unsubService();
  unsubStatus();
  clearInterval(safetyNetInterval);   // ← add this line
  clearReconnectDebounce();
  useVPNMachineStore.setState({ state: 'idle', error: null, isRetrying: false, networkAvailable: true, initialization: null });
};
```

**Total diff**: ~14 lines in one function.

---

## Test Plan

New `describe` block in `vpn-machine.store.test.ts`. All assertions are **behavioral** (check `useVPNMachineStore.getState().state`), consistent with the existing test pattern. No spying on `dispatch` directly.

Because `initializeVPNMachine()` fires an async initial-query Promise, each test must flush microtasks (`await Promise.resolve()` or equivalent) after `vi.advanceTimersByTime()` to ensure the initial query and any poll callbacks have resolved before asserting.

**Mock setup** (shared across the new describe block):

```typescript
// Cast to any to satisfy IK2Vpn interface — test-only mock
(window as any)._k2 = {
  onServiceStateChange: vi.fn((cb) => { cb(true); return () => {}; }),
  onStatusChange: vi.fn(() => () => {}),
  run: vi.fn().mockResolvedValue({ code: 0, data: { state: 'connected' } }),
};
```

**Test cases**:

```
describe('initializeVPNMachine — safety-net poll (event-driven mode)')

  ✓ poll fires at 15s: advance 14999ms → run not yet called again;
                        advance 1ms more → run called (verify via mock call count)

  ✓ poll fires again at 30s: advance 30000ms + flush microtasks
                               → run called three times (1 initial + 2 polls)

  ✓ recovery: machine starts in reconnecting; poll returns {state:'connected'}
               → advance 15s + flush → store.state === 'connected'

  ✓ cleanup stops poll: call cleanup(); advance 30000ms
                          → run not called after cleanup

  ✓ silent on error: run() rejects → no SERVICE_UNREACHABLE dispatch
                      → store.state unchanged

  ✓ idempotent when already connected: machine in connected, poll returns connected
                                         → store.state stays 'connected' (no transition)
```

The **recovery test** (third case) is the end-to-end scenario. Setup: mock `run` to return `{state:'connected', error:{code:108}, retrying:true}` on the first call (initial query puts machine into `reconnecting`), then swap mock to return clean `{state:'connected'}` before advancing 15s. Verify `store.state === 'connected'` after the poll fires.

---

## Non-Goals

- Does not modify native K2Plugin.swift or K2VpnService.kt
- Does not replace NE push as the primary delivery path
- Does not add state-aware poll frequency (no speed-up in `reconnecting`, no pause in `idle`)
- Does not fix the Android case (already works via `onStatus()` push)
- Does not add service health detection (that remains `onServiceStateChange`'s role)
- Does not address `vpnError` events from iOS (terminal disconnect errors — different code path)

---

## Files Changed

| File | Change |
|------|--------|
| `webapp/src/stores/vpn-machine.store.ts` | +14 lines: `setInterval` + `clearInterval` in event-driven branch |
| `webapp/src/stores/__tests__/vpn-machine.store.test.ts` | +~50 lines: new describe block with 6 test cases |
