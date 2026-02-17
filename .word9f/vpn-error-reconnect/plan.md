# Plan: VPN State Contract, Error Handling & Reconnection

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Fix broken state contract across all platforms, add error synthesis in bridge layers, and implement network-change-driven reconnection for mobile.

**Architecture:** Both bridges get a `transformStatus()` that normalizes backend state values and synthesizes `error` from `disconnected + lastError`. Go engine gets `OnNetworkChanged()` + wire `ResetConnections()`. Android/iOS native layers get NetworkCallback registration.

**Tech Stack:** TypeScript (webapp bridges), Go (k2 engine/wire), Kotlin (Android), Swift (iOS)

---

## Meta

| Field | Value |
|-------|-------|
| Feature | vpn-error-reconnect |
| Spec | docs/features/vpn-error-reconnect.md |
| Date | 2026-02-17 |
| Complexity | moderate |

## AC Mapping

| AC | Test | Task |
|----|------|------|
| AC1: Desktop `isDisconnected` fix | test_tauri_transformStatus_stopped_maps_to_disconnected | T2 |
| AC2: Desktop button disabled when no tunnel | test_tauri_transformStatus_stopped_running_is_false | T2 |
| AC3: Mobile error display | test_capacitor_transformStatus_error_synthesis | T2 |
| AC4: Error click reconnect | test_handleToggleConnection_error_reconnects | T3 |
| AC5: Clear error on success | test_tauri_transformStatus_connected_clears_error, test_capacitor_transformStatus_connected_no_error | T2 |
| AC6: Manual disconnect no error | test_tauri_transformStatus_stopped_no_error_stays_disconnected, test_capacitor_transformStatus_disconnected_no_error | T2 |
| AC7: Android network reconnect | test_registerNetworkCallback_called_after_start, test_onAvailable_calls_onNetworkChanged | T4 |
| AC8: iOS network reconnect | test_startMonitoringNetwork_creates_monitor, test_pathSatisfied_calls_onNetworkChanged | T5 |
| AC9: Bridge output consistency | test_both_bridges_same_output_connected, test_both_bridges_same_output_error | T2 |

## Dependency Graph

```
F1 (Go engine) ──┬── T4 (Android NetworkCallback)
                  └── T5 (iOS NWPathMonitor)

T2 (Bridges) ──── T3 (Dashboard error UX)
```

- F1 and T2: fully parallel (different repos/directories)
- T4 and T5: fully parallel (different platform directories)
- T3 depends on T2 (needs bridge error synthesis for integration)
- T4, T5 depend on F1 (need `engine.OnNetworkChanged()`)

## Foundation Tasks

### F1: Go Engine — OnNetworkChanged + Wire ResetConnections

**Scope**: Add network change notification API to engine and connection reset capability to wire transports. This is the Go-level foundation that mobile NetworkCallback tasks depend on.

**Files**:
- Modify: `k2/wire/transport.go` — add `ResetConnections()` to `TransportManager`
- Modify: `k2/wire/quic.go` — add `resetConnection()` to `QUICClient`
- Modify: `k2/wire/tcpws.go` — add `resetConnection()` to `TCPWSClient`
- Modify: `k2/engine/engine.go` — add `OnNetworkChanged()` method
- Modify: `k2/mobile/mobile.go` — export `OnNetworkChanged()` for gomobile
- Create: `k2/wire/transport_test.go` — add reset tests (or extend existing)
- Modify: `k2/engine/engine_test.go` — add OnNetworkChanged tests

**Depends on**: none

**TDD**:

- RED: Write failing tests
  - Test functions:
    - `TestQUICClient_ResetConnection` — after reset, next DialTCP creates new QUIC conn (not reusing cached)
    - `TestQUICClient_ResetConnection_NotClosed` — after reset, client is NOT closed (can still dial)
    - `TestTCPWSClient_ResetConnection` — after reset, next DialTCP creates new smux session
    - `TestTCPWSClient_ResetConnection_NotClosed` — after reset, client is NOT closed
    - `TestTransportManager_ResetConnections` — resets both QUIC and TCPWS
    - `TestEngine_OnNetworkChanged_WhenConnected` — triggers handler.OnStateChange("reconnecting") then "connected"
    - `TestEngine_OnNetworkChanged_WhenDisconnected` — no-op (no state change, no error)
    - `TestEngine_OnNetworkChanged_Export` — gomobile wrapper calls inner

- GREEN: Implement minimal code
  - `QUICClient.resetConnection()`: lock → close conn/transport/udpMux → nil them → keep `closed = false`
  - `TCPWSClient.resetConnection()`: lock → close sess/udpMux → nil them → keep `closed = false`
  - `TransportManager.ResetConnections()`: call resetConnection on both quic and tcpws
  - `Engine.OnNetworkChanged()`: if state == connected → call handler.OnStateChange("reconnecting") → tm.ResetConnections() → handler.OnStateChange("connected")
  - `mobile.Engine.OnNetworkChanged()`: call inner.OnNetworkChanged()

- REFACTOR:
  - [SHOULD] Extract close-without-shutdown pattern from Close() and resetConnection() to avoid duplication

**Acceptance**: Engine.OnNetworkChanged() triggers wire reset when connected, no-op otherwise. Wire clients can reconnect after reset.

**Knowledge**: docs/knowledge/framework-gotchas.md — "QUIC/smux Dead Connection Caching" entry

---

## Feature Tasks

### T2: Bridge transformStatus — Tauri + Capacitor

**Scope**: Add `transformStatus()` to Tauri bridge (currently pure passthrough). Fix Capacitor bridge `transformStatus()` to synthesize error state. Both bridges must produce identical `StatusResponseData` for equivalent input.

**Files**:
- Modify: `webapp/src/services/tauri-k2.ts` — add `transformStatus()`, use in status path
- Modify: `webapp/src/services/capacitor-k2.ts` — add error synthesis to existing `transformStatus()`
- Modify: `webapp/src/services/__tests__/tauri-k2.test.ts` — update + add transform tests
- Modify: `webapp/src/services/__tests__/capacitor-k2.test.ts` — update + add error synthesis tests

**Depends on**: none

**TDD**:

- RED: Write failing tests
  - Test functions (Tauri):
    - `test_tauri_transformStatus_stopped_maps_to_disconnected` — daemon returns `{state:"stopped"}` → bridge returns `{state:"disconnected", running:false}`
    - `test_tauri_transformStatus_stopped_with_error_maps_to_error` — `{state:"stopped", error:"timeout"}` → `{state:"error", error:{code:570, message:"timeout"}}`
    - `test_tauri_transformStatus_connected_passes_through` — `{state:"connected", connected_at:"..."}` → `{state:"connected", running:true, startAt:...}`
    - `test_tauri_transformStatus_connecting_passes_through` — `{state:"connecting"}` → `{state:"connecting", running:true}`
    - `test_tauri_transformStatus_stopped_no_error_stays_disconnected` — `{state:"stopped"}` → `{state:"disconnected"}`, no error field
    - `test_tauri_status_action_uses_transformStatus` — _k2.run('status') returns transformed data
  - Test functions (Capacitor):
    - `test_capacitor_transformStatus_error_synthesis` — `{state:"disconnected", error:"DNS failed"}` → `{state:"error", error:{code:570,...}}`
    - `test_capacitor_transformStatus_disconnected_no_error` — `{state:"disconnected"}` → `{state:"disconnected"}`, no error
    - `test_capacitor_transformStatus_connected_no_error` — `{state:"connected", connectedAt:"..."}` → stays connected, no error
  - Test functions (Consistency):
    - `test_both_bridges_same_output_connected` — both produce same shape for connected state
    - `test_both_bridges_same_output_error` — both produce same shape for error state

- GREEN: Implement minimal code
  - Tauri bridge: add `transformStatus(raw)` function:
    1. Map `"stopped"` → `"disconnected"`
    2. If `raw.error && state === 'disconnected'` → `state = 'error'`, create ControlError
    3. Map `connected_at` → `startAt` (Unix seconds)
    4. Set `running`, `networkAvailable: true`, `retrying: false`
  - Tauri bridge: in `run()`, when `action === 'status'` and `response.code === 0`, apply `transformStatus(response.data)`
  - Capacitor bridge: add error synthesis line: `if (state === 'disconnected' && raw.error) state = 'error'`
  - Update existing capacitor test `test_k2_run_status_with_error_maps_to_ControlError` — change mock from `state:'error'` to `state:'disconnected'` (engine never returns 'error' directly)

- REFACTOR:
  - [SHOULD] Extract shared `transformStatus` logic into a common utility (both bridges use same rules)
  - [SHOULD] Add JSDoc to `transformStatus` explaining state normalization contract

**Acceptance**: `_k2.run('status')` on both platforms returns normalized `StatusResponseData` with error synthesis. Desktop `isDisconnected` correctly true when daemon is stopped. Existing bridge tests updated to match real backend behavior.

**Knowledge**: docs/knowledge/architecture-decisions.md — "sing-tun Network Monitoring" entry (bridge as contract translation layer)

---

### T3: Dashboard Error UX

**Scope**: Add error state handling to Dashboard's `handleToggleConnection`. Fix guard condition to include error state.

**Files**:
- Modify: `webapp/src/pages/Dashboard.tsx` — error branch in handleToggleConnection + guard fix

**Depends on**: [T2]

**TDD**:

- RED: Write failing tests
  - Test functions:
    - `test_handleToggleConnection_error_reconnects` — when isError && !isRetrying, calls _k2.run('up') (not 'down')
    - `test_handleToggleConnection_error_no_tunnel_blocked` — when isError && !hasTunnel, does nothing
    - `test_handleToggleConnection_disconnected_connects` — normal connect path unchanged
    - `test_handleToggleConnection_connected_disconnects` — normal disconnect path unchanged

  Note: Dashboard tests may require mocking useVPNStatus hook. If no existing Dashboard test file exists, create `webapp/src/pages/__tests__/Dashboard.test.tsx` with minimal setup.

- GREEN: Implement minimal code
  - Add guard: `if ((isDisconnected || isError) && !activeTunnelInfo.domain) return;`
  - Add error branch before disconnect check:
    ```ts
    if (isError && !isRetrying) {
      setOptimisticState('connecting');
      const config = { ... };  // same assembly as connect path
      await window._k2.run('up', config);
    }
    ```
  - Extract config assembly into local helper to avoid duplication between error-reconnect and connect paths

- REFACTOR:
  - [MUST] Extract config assembly (`server`, `rule`) into `assembleConfig()` helper — used by both connect and error-reconnect paths, avoids code duplication
  - [SHOULD] Add `isError` to useVPNStatus destructuring comment for clarity

**Acceptance**: Error state clicking triggers reconnect. No-tunnel guard works for both disconnected and error states.

---

### T4: Android NetworkCallback

**Scope**: Register `ConnectivityManager.NetworkCallback` in `K2VpnService` to detect network changes and trigger engine reconnection.

**Files**:
- Modify: `mobile/android/app/src/main/java/io/kaitu/K2VpnService.kt` — add NetworkCallback registration

**Depends on**: [F1]

**TDD**:

- RED: Write failing tests
  - Test functions (Kotlin unit tests or instrumented tests):
    - `test_registerNetworkCallback_called_after_start` — verify callback is registered after engine.start() succeeds
    - `test_unregisterNetworkCallback_called_on_stop` — verify callback is unregistered in stopVpn()
    - `test_onAvailable_calls_onNetworkChanged` — when onAvailable fires, engine.onNetworkChanged() is called

  Note: Android VPN service testing may require Robolectric or instrumented tests. If test infrastructure doesn't exist, write integration-verifiable code with clear logging and skip automated tests (mark as "manual verification required").

- GREEN: Implement minimal code
  - Add `networkCallback` field (`ConnectivityManager.NetworkCallback?`)
  - Add `registerNetworkCallback()` private method:
    - Get `ConnectivityManager` system service
    - Create `NetworkRequest` with `NET_CAPABILITY_INTERNET`
    - Register callback
  - Add `unregisterNetworkCallback()` private method
  - Call `registerNetworkCallback()` after `engine?.start()` succeeds in `startVpn()`
  - Call `unregisterNetworkCallback()` at start of `stopVpn()`
  - In callback `onAvailable`: call `engine?.onNetworkChanged()`

- REFACTOR:
  - [SHOULD] Add 500ms debounce to onAvailable to prevent rapid-fire reconnects during network flapping

**Acceptance**: VPN service registers network callback on connect, unregisters on disconnect. Network change triggers engine reconnection.

**Knowledge**: docs/knowledge/framework-gotchas.md — "Android Bans Netlink Sockets" entry (explains why native callback is needed)

---

### T5: iOS NWPathMonitor

**Scope**: Add `NWPathMonitor` to `PacketTunnelProvider` to detect network changes and trigger engine reconnection in the NE process.

**Files**:
- Modify: `mobile/ios/App/PacketTunnel/PacketTunnelProvider.swift` — add NWPathMonitor

**Depends on**: [F1]

**TDD**:

- RED: Write failing tests
  - Test functions (Swift unit tests):
    - `test_startMonitoringNetwork_creates_monitor` — verify monitor is started
    - `test_stopMonitoringNetwork_cancels_monitor` — verify monitor is cancelled on stop
    - `test_pathSatisfied_calls_onNetworkChanged` — when path.status == .satisfied, engine.onNetworkChanged() is called

  Note: PacketTunnelProvider testing requires NE test harness. If test infrastructure doesn't exist, write integration-verifiable code with clear os_log entries and skip automated tests (mark as "manual verification required").

- GREEN: Implement minimal code
  - Import `Network` framework
  - Add `pathMonitor` property (`NWPathMonitor`)
  - Add `startMonitoringNetwork()`:
    - Set `pathUpdateHandler` — if `path.status == .satisfied`, call `engine?.onNetworkChanged()`
    - Start on `.global(qos: .utility)` queue
  - Add `stopMonitoringNetwork()`: call `pathMonitor.cancel()`
  - Call `startMonitoringNetwork()` after engine start succeeds in `startTunnel()`
  - Call `stopMonitoringNetwork()` in `stopTunnel()`

- REFACTOR:
  - [SHOULD] Add 500ms debounce using DispatchWorkItem to prevent rapid triggers

**Acceptance**: NE process monitors network path changes. Network recovery triggers engine wire reconnection.

**Knowledge**: docs/knowledge/architecture-decisions.md — iOS VPN two-process architecture (NE runs independently)

---

## Execution Order

```
Phase 1 (parallel):  F1 (Go engine)  ‖  T2 (Bridges)
Phase 2 (parallel):  T3 (Dashboard, after T2)  ‖  T4 (Android, after F1)  ‖  T5 (iOS, after F1)
```

Critical path: F1 → T4/T5 or T2 → T3 (whichever is longer)

## Test Commands

```bash
# F1: Go engine + wire
cd k2 && go test ./engine/... ./wire/... -v -run "Reset|OnNetworkChanged"

# T2: Bridge tests
cd webapp && npx vitest run src/services/__tests__/tauri-k2.test.ts src/services/__tests__/capacitor-k2.test.ts

# T3: Dashboard tests
cd webapp && npx vitest run src/pages/__tests__/Dashboard.test.tsx

# T4: Android (manual verification or instrumented test)
cd mobile/android && ./gradlew test

# T5: iOS (manual verification or XCTest)
cd mobile/ios && xcodebuild test -scheme PacketTunnel -destination 'platform=iOS Simulator'

# Full webapp test suite
cd webapp && npx vitest run
```
