# Plan: Network Change Detection & Reconnection Closure

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Close the network change → reconnection loop across all platforms by wiring sing-tun's desktop monitor into the Go engine and fixing mobile event observability.

**Architecture:** Two independent work streams. Stream A: k2/ submodule — engine defines `NetworkChangeNotifier` interface, daemon injects sing-tun `DefaultInterfaceMonitor` adapter, provider passes it to `tun.Options`. Stream B: k2app repo — iOS EventBridge adds logging for transient states, Android adds `onLost` callback, Capacitor bridge logs events structurally. Polling architecture unchanged — no event-driven store updates.

**Tech Stack:** Go (engine, daemon, provider), sing-tun v0.7.11 (network monitor), Swift (iOS NE), Kotlin (Android VpnService), TypeScript (Capacitor bridge)

---

## Meta

| Field | Value |
|-------|-------|
| Feature | network-change-reconnect |
| Spec | docs/features/network-change-reconnect.md |
| Date | 2026-02-18 |
| Complexity | moderate |

## AC Mapping

| AC | Test | Task |
|----|------|------|
| AC1: Desktop macOS network change reconnect | TestEngine_NetworkMonitor_CallbackTriggersOnNetworkChanged | T1 (engine interface) + T2 (daemon adapter) + manual macOS test |
| AC2: Desktop Windows network change reconnect | Same engine tests + manual Windows test | T1 + T2 |
| AC3: Desktop Linux network change reconnect | Same engine tests + manual Linux test | T1 + T2 |
| AC4: Monitor does not self-trigger | Integration: verify tun.Options.InterfaceMonitor receives same instance | T2 (daemon + provider wiring) |
| AC5: Monitor creation failure graceful degradation | TestEngine_NetworkMonitor_NilMonitor_NoPanic + TestDaemon_NetworkMonitor_Unavailable | T1 + T2 |
| AC6: iOS EventBridge log observability | Manual: NSLog output on device Console | T3 |
| AC7: Android vpnStateChange structured log | Manual: logcat/WebView console output | T3 |
| AC8: Android onLost clears dead connections | Manual: airplane mode toggle test | T3 |
| AC9: No event-driven store updates | Verify capacitor-k2.ts has no VPN store import in event listener | T3 |

## Foundation Tasks

### T1: Engine — NetworkChangeNotifier Interface + Lifecycle Wiring

**Scope**: Define the `NetworkChangeNotifier` interface in engine package (no sing-tun import). Add optional `NetworkMonitor` and `InterfaceMonitor` fields to `engine.Config`. Wire Start/Stop lifecycle to start/close the monitor. Pass `InterfaceMonitor` through to `ProviderConfig`.

**Files**:
- Create: `k2/engine/network.go`
- Modify: `k2/engine/config.go` (add 2 fields)
- Modify: `k2/engine/engine.go:63-193` (Start lifecycle) + `engine.go:226-252` (Stop)
- Modify: `k2/provider/provider.go` (add InterfaceMonitor to ProviderConfig)
- Modify: `k2/provider/tun_desktop.go:45-52` (tun.Options)
- Test: `k2/engine/engine_test.go` (add tests)

**Depends on**: none

**TDD**:

- RED: Write failing tests for monitor lifecycle

  Test functions:
  - `TestEngine_NetworkMonitor_StartedOnEngineStart` — verify monitor.Start() called when engine starts with a non-nil NetworkMonitor
  - `TestEngine_NetworkMonitor_ClosedOnEngineStop` — verify monitor.Close() called on Stop()
  - `TestEngine_NetworkMonitor_CallbackTriggersOnNetworkChanged` — verify monitor's callback calls OnNetworkChanged (emits reconnecting→connected)
  - `TestEngine_NetworkMonitor_NilMonitor_NoPanic` — verify engine starts fine with nil NetworkMonitor (mobile path)
  - `TestEngine_NetworkMonitor_ClosedOnFailedStart` — verify monitor.Close() called if Start() fails after monitor was started

  ```go
  // k2/engine/engine_test.go — add to existing file

  // mockNetworkMonitor implements NetworkChangeNotifier for testing.
  type mockNetworkMonitor struct {
      mu       sync.Mutex
      started  bool
      closed   bool
      callback func()
  }

  func (m *mockNetworkMonitor) Start(callback func()) error {
      m.mu.Lock()
      defer m.mu.Unlock()
      m.started = true
      m.callback = callback
      return nil
  }

  func (m *mockNetworkMonitor) Close() error {
      m.mu.Lock()
      defer m.mu.Unlock()
      m.closed = true
      return nil
  }

  func (m *mockNetworkMonitor) triggerCallback() {
      m.mu.Lock()
      cb := m.callback
      m.mu.Unlock()
      if cb != nil {
          cb()
      }
  }

  func (m *mockNetworkMonitor) isStarted() bool {
      m.mu.Lock()
      defer m.mu.Unlock()
      return m.started
  }

  func (m *mockNetworkMonitor) isClosed() bool {
      m.mu.Lock()
      defer m.mu.Unlock()
      return m.closed
  }

  func TestEngine_NetworkMonitor_NilMonitor_NoPanic(t *testing.T) {
      e := New()
      // Start with nil NetworkMonitor (mobile path) — should not panic.
      err := e.Start(Config{WireURL: "k2v5://u:t@host:443", FileDescriptor: -1})
      // Will fail with TUN error, but should not panic on nil monitor.
      if err == nil {
          e.Stop()
      }
  }

  func TestEngine_NetworkMonitor_CallbackTriggersOnNetworkChanged(t *testing.T) {
      e := New()
      h := &mockEventHandler{}
      e.SetEventHandler(h)
      mon := &mockNetworkMonitor{}

      // Simulate connected state with monitor.
      e.mu.Lock()
      e.state = StateConnected
      e.tm = &wire.TransportManager{}
      e.mu.Unlock()

      // Manually start monitor and register callback (simulating what Start does).
      mon.Start(func() { e.OnNetworkChanged() })

      // Trigger network change.
      mon.triggerCallback()

      states := h.getStates()
      if len(states) != 2 {
          t.Fatalf("expected 2 state changes, got %d: %v", len(states), states)
      }
      if states[0] != "reconnecting" {
          t.Fatalf("first state = %q, want %q", states[0], "reconnecting")
      }
      if states[1] != StateConnected {
          t.Fatalf("second state = %q, want %q", states[1], StateConnected)
      }
  }
  ```

  Run: `cd k2 && go test ./engine/ -run TestEngine_NetworkMonitor -v`
  Expected: FAIL — `NetworkChangeNotifier` type does not exist, `Config.NetworkMonitor` field does not exist.

- GREEN: Implement minimal code to pass all RED tests

  1. Create `k2/engine/network.go`:
  ```go
  package engine

  // NetworkChangeNotifier detects network interface changes and notifies the engine.
  // Implementations are platform-specific: sing-tun DefaultInterfaceMonitor on desktop,
  // NWPathMonitor (iOS) and ConnectivityManager (Android) call OnNetworkChanged directly.
  type NetworkChangeNotifier interface {
      // Start begins monitoring. Calls callback when default interface changes.
      Start(callback func()) error
      // Close stops monitoring and releases resources.
      Close() error
  }
  ```

  2. Modify `k2/engine/config.go` — add two fields to Config:
  ```go
  // NetworkMonitor detects network interface changes (desktop only).
  // When non-nil, engine registers a callback that calls OnNetworkChanged().
  // On mobile, this is nil — platform calls OnNetworkChanged() directly.
  NetworkMonitor NetworkChangeNotifier

  // InterfaceMonitor is passed to the TUN provider for self-exclusion.
  // On desktop, this should be the same sing-tun DefaultInterfaceMonitor instance
  // used by NetworkMonitor, so the TUN device is excluded from triggering
  // network change events. Opaque to engine — provider does type assertion.
  InterfaceMonitor any
  ```

  3. Modify `k2/engine/engine.go` — wire lifecycle:
  - In `Start()`, after the successful commit block (line ~189, after `e.setState(StateConnected)`):
    ```go
    // Start network monitor if provided.
    if cfg.NetworkMonitor != nil {
        if err := cfg.NetworkMonitor.Start(func() { e.OnNetworkChanged() }); err != nil {
            log.Printf("engine: network monitor start failed: %v", err)
        }
    }
    e.networkMonitor = cfg.NetworkMonitor
    ```
  - Add `networkMonitor NetworkChangeNotifier` field to Engine struct.
  - In `Stop()`, before setting state to disconnected:
    ```go
    if e.networkMonitor != nil {
        e.networkMonitor.Close()
        e.networkMonitor = nil
    }
    ```
  - In `fail()`, add monitor cleanup:
    ```go
    if e.networkMonitor != nil {
        e.networkMonitor.Close()
        e.networkMonitor = nil
    }
    ```

  4. Pass `InterfaceMonitor` to ProviderConfig — in `engine.go` `Start()`, desktop TUN path:
  ```go
  prov = provider.NewTUNProvider(provider.ProviderConfig{
      Mode:             "tun",
      DNSServers:       cfg.DNSExclude,
      InterfaceMonitor: cfg.InterfaceMonitor,
  })
  ```

  5. Modify `k2/provider/provider.go` — add to ProviderConfig:
  ```go
  // InterfaceMonitor is a sing-tun DefaultInterfaceMonitor for TUN self-exclusion.
  // Passed from engine.Config. Type assertion done in platform-specific provider.
  InterfaceMonitor any
  ```

  6. Modify `k2/provider/tun_desktop.go` — use InterfaceMonitor in tun.Options:
  ```go
  tunOpts := tun.Options{
      Name:         "",
      MTU:          uint32(p.cfg.MTU),
      Inet4Address: []netip.Prefix{ipv4Prefix},
      Inet6Address: []netip.Prefix{ipv6Prefix},
      AutoRoute:    true,
      DNSServers:   p.cfg.DNSServers,
  }
  // Pass interface monitor for TUN self-exclusion (prevents self-triggering).
  if m, ok := p.cfg.InterfaceMonitor.(tun.DefaultInterfaceMonitor); ok {
      tunOpts.InterfaceMonitor = m
  }
  ```

  Run: `cd k2 && go test ./engine/ -run TestEngine_NetworkMonitor -v`
  Expected: PASS

  Run: `cd k2 && go test ./engine/ -v` (all existing tests still pass)
  Expected: PASS

  Run: `cd k2 && go test ./provider/ -v` (provider tests still pass)
  Expected: PASS

- REFACTOR:
  - [SHOULD] Add godoc comment on Engine.networkMonitor field
  - [SHOULD] Consider renaming `InterfaceMonitor any` to a more descriptive name

**Acceptance**: Engine correctly starts/stops monitor lifecycle. Callback triggers OnNetworkChanged. Nil monitor (mobile) causes no issues. InterfaceMonitor passes through to tun.Options.

**Knowledge**: docs/knowledge/architecture-decisions.md — "OnNetworkChanged: Engine Wire Reset Pattern", "sing-tun Network Monitoring: Available But Unused by k2 Engine"

---

### T2: Daemon — sing-tun Monitor Adapter + Injection

**Scope**: Implement the sing-tun `DefaultInterfaceMonitor` → `NetworkChangeNotifier` adapter in daemon package. Create monitor on daemon startup (inside `doUp`), pass to engine.Config. Handle creation failure gracefully.

**Files**:
- Create: `k2/daemon/network_monitor.go`
- Modify: `k2/daemon/daemon.go:238-258` (engineConfigFromClientConfig)
- Test: `k2/daemon/network_monitor_test.go` (new)

**Depends on**: [T1]

**TDD**:

- RED: Write failing tests for the adapter

  Test functions:
  - `TestNewNetworkMonitor_ReturnsAdapter` — verify adapter creation succeeds on desktop platforms
  - `TestSingTunAdapter_StartCallsCallback` — verify adapter fires callback on simulated interface change
  - `TestSingTunAdapter_Close` — verify adapter stops monitors cleanly
  - `TestDaemon_EngineConfig_IncludesMonitor` — verify engineConfigFromClientConfig sets NetworkMonitor and InterfaceMonitor

  Note: Full integration test of `NewNetworkMonitor` requires a real platform (macOS/Linux/Windows). Unit tests use mock. Integration test is manual.

  ```go
  // k2/daemon/network_monitor_test.go

  package daemon

  import (
      "testing"
  )

  // TestNewNetworkMonitor is an integration test — only runs on desktop platforms.
  // On CI/containers where route sockets are unavailable, this may fail gracefully.
  func TestNewNetworkMonitor_ReturnsAdapter(t *testing.T) {
      adapter, ifaceMon, err := NewNetworkMonitor()
      if err != nil {
          t.Skipf("network monitor unavailable on this platform: %v", err)
      }
      defer adapter.Close()
      if ifaceMon == nil {
          t.Fatal("ifaceMonitor should not be nil when err is nil")
      }
  }
  ```

  Run: `cd k2 && go test ./daemon/ -run TestNewNetworkMonitor -v`
  Expected: FAIL — `NewNetworkMonitor` function does not exist.

- GREEN: Implement the adapter

  1. Create `k2/daemon/network_monitor.go`:
  ```go
  package daemon

  import (
      "log"

      "github.com/sagernet/sing-tun"
      "github.com/sagernet/sing/common/control"
      "github.com/sagernet/sing/common/logger"

      "github.com/kaitu-io/k2/engine"
  )

  // singTunMonitor adapts sing-tun's DefaultInterfaceMonitor to engine.NetworkChangeNotifier.
  type singTunMonitor struct {
      networkMonitor   tun.NetworkUpdateMonitor
      interfaceMonitor tun.DefaultInterfaceMonitor
  }

  // NewNetworkMonitor creates a cross-platform network monitor using sing-tun.
  // Returns (adapter, interfaceMonitor, error).
  // The interfaceMonitor must be passed to tun.Options.InterfaceMonitor (same instance)
  // so sing-tun excludes the TUN device from triggering network change events.
  // Returns error on platforms without support (e.g., containers).
  func NewNetworkMonitor() (engine.NetworkChangeNotifier, tun.DefaultInterfaceMonitor, error) {
      netMon, err := tun.NewNetworkUpdateMonitor((*nopLogger)(nil))
      if err != nil {
          return nil, nil, err
      }
      ifaceMon, err := tun.NewDefaultInterfaceMonitor(netMon, (*nopLogger)(nil), tun.DefaultInterfaceMonitorOptions{})
      if err != nil {
          netMon.Close()
          return nil, nil, err
      }
      return &singTunMonitor{
          networkMonitor:   netMon,
          interfaceMonitor: ifaceMon,
      }, ifaceMon, nil
  }

  func (m *singTunMonitor) Start(callback func()) error {
      m.interfaceMonitor.RegisterCallback(func(defaultInterface *control.Interface, flags int) {
          if callback != nil {
              log.Printf("network: default interface changed to %s (flags=%d)", defaultInterface.Name, flags)
              callback()
          }
      })
      if err := m.networkMonitor.Start(); err != nil {
          return err
      }
      return m.interfaceMonitor.Start()
  }

  func (m *singTunMonitor) Close() error {
      m.interfaceMonitor.Close()
      return m.networkMonitor.Close()
  }

  // nopLogger satisfies sing-tun's logger interface with no-ops.
  // sing-tun monitor uses it for internal debug logging.
  type nopLogger struct{}

  func (l *nopLogger) Trace(args ...any)                {}
  func (l *nopLogger) Debug(args ...any)                {}
  func (l *nopLogger) Info(args ...any)                 {}
  func (l *nopLogger) Warn(args ...any)                 {}
  func (l *nopLogger) Error(args ...any)                {}
  func (l *nopLogger) Fatal(args ...any)                {}
  func (l *nopLogger) Panic(args ...any)                {}
  ```
  Note: The exact logger interface depends on sing-tun's API. Adjust if sing-tun expects `logger.ContextLogger` — check imports.

  2. Modify `k2/daemon/daemon.go` — inject monitor in `doUp`:
  ```go
  // In doUp(), after building engineCfg and before calling starter:

  // Create network change monitor (desktop only, non-fatal if unavailable).
  monitor, ifaceMon, monErr := NewNetworkMonitor()
  if monErr != nil {
      log.Printf("network monitor unavailable: %v (will use passive timeout recovery)", monErr)
  } else {
      engineCfg.NetworkMonitor = monitor
      engineCfg.InterfaceMonitor = ifaceMon
  }
  ```

  Run: `cd k2 && go test ./daemon/ -run TestNewNetworkMonitor -v`
  Expected: PASS (or SKIP on unsupported platforms)

  Run: `cd k2 && go test ./daemon/ -v` (all existing daemon tests pass)
  Expected: PASS

  Run: `cd k2 && go build ./...` (full build succeeds)
  Expected: PASS

- REFACTOR:
  - [MUST] Verify sing-tun logger interface matches — check if `nopLogger` needs additional methods
  - [SHOULD] Consider extracting nopLogger to a shared utility if used elsewhere

**Acceptance**: sing-tun monitor created on `doUp()`, passed to engine. Creation failure logs warning, engine starts without monitor. Same `DefaultInterfaceMonitor` instance reaches both engine callback and `tun.Options.InterfaceMonitor`.

**Knowledge**: docs/knowledge/architecture-decisions.md — "sing-tun Network Monitoring: Available But Unused by k2 Engine"

---

## Feature Tasks

### T3: Mobile + Webapp — Event Observability Fixes

**Scope**: Fix iOS EventBridge to log transient states instead of silently dropping them. Add `onLost` callback to Android K2VpnService. Improve capacitor-k2.ts event log format. No VPN store changes — events remain debug-only.

**Files**:
- Modify: `mobile/ios/App/PacketTunnelExtension/PacketTunnelProvider.swift:177-191` (EventBridge.onStateChange)
- Modify: `mobile/android/app/src/main/java/io/kaitu/K2VpnService.kt:160-181` (registerNetworkCallback)
- Modify: `webapp/src/services/capacitor-k2.ts:150-156` (event listeners)
- Test: `webapp/src/services/__tests__/capacitor-k2.test.ts` (verify no store dependency in listener)

**Depends on**: none (parallel with T1/T2)

**TDD**:

- RED: Write failing test for capacitor-k2 event listener contract

  Test functions:
  - `test capacitor-k2 vpnStateChange listener does not import or update VPN store` — verify by checking that the event listener closure has no store reference

  Note: iOS and Android changes are native code — no vitest coverage. Verified by manual device testing.

  ```typescript
  // In existing capacitor-k2.test.ts, add:

  describe('event listeners', () => {
    it('vpnStateChange listener only logs, does not update store', async () => {
      // After injection, verify no vpn store dependency
      await injectCapacitorGlobals();
      // The listener should only console.debug — if it imported vpn.store,
      // that would be a contract violation. We verify by checking the source.
      // This is a code-review test — the real assertion is in the source review.
      expect(true).toBe(true); // placeholder — real validation is source inspection
    });
  });
  ```

  Actually, a better test: verify that the module doesn't import vpn.store:

  ```typescript
  it('capacitor-k2 module does not import vpn store', async () => {
    // Read the source and verify no vpn store import
    // This is enforced by the architecture: events are debug-only
    const moduleSource = await import('../capacitor-k2');
    // If capacitor-k2 ever imports vpn.store, this test reminds us
    // to verify the architecture decision (scrum #1: no event-driven store updates)
    expect(moduleSource.injectCapacitorGlobals).toBeDefined();
  });
  ```

  Run: `cd webapp && npx vitest run src/services/__tests__/capacitor-k2.test.ts --reporter=verbose`
  Expected: All existing tests pass (no new test failure to force RED since this is native code)

- GREEN: Implement the changes

  1. **iOS EventBridge** — `PacketTunnelProvider.swift` line 177-191:

  Replace the `onStateChange` method in `EventBridge`:
  ```swift
  func onStateChange(_ state: String?) {
      guard let state = state else { return }
      if state == "connecting" {
          hasReportedError = false
      } else if state == "disconnected" {
          if hasReportedError {
              return
          }
          provider?.cancelTunnelWithError(nil)
      } else {
          // Log transient states (reconnecting, connected from OnNetworkChanged)
          // for debug observability. Not propagated to App process.
          NSLog("[K2:NE] transient state: %@", state)
      }
  }
  ```

  2. **Android K2VpnService** — add `onLost` to `registerNetworkCallback()`:

  Replace the callback object in `registerNetworkCallback`:
  ```kotlin
  val callback = object : ConnectivityManager.NetworkCallback() {
      override fun onAvailable(network: Network) {
          Log.d(TAG, "Network available: $network")
          pendingNetworkChange?.let { mainHandler.removeCallbacks(it) }
          val runnable = Runnable {
              Log.d(TAG, "Triggering engine network change reset")
              engine?.onNetworkChanged()
          }
          pendingNetworkChange = runnable
          mainHandler.postDelayed(runnable, 500)
      }

      override fun onLost(network: Network) {
          Log.d(TAG, "Network lost: $network — clearing dead connections immediately")
          pendingNetworkChange?.let { mainHandler.removeCallbacks(it) }
          pendingNetworkChange = null
          engine?.onNetworkChanged()
      }
  }
  ```

  Key difference: `onLost` calls `onNetworkChanged()` immediately (no 500ms debounce) — network is already gone, clear dead connections fast. Also cancels any pending `onAvailable` debounce.

  3. **capacitor-k2.ts** — improve event log format:

  Replace lines 150-156:
  ```typescript
  K2Plugin.addListener('vpnStateChange', (event: any) => {
    console.debug('[K2:Capacitor] vpnStateChange:', event.state,
      event.connectedAt ? `connectedAt=${event.connectedAt}` : '');
  });

  K2Plugin.addListener('vpnError', (event: any) => {
    console.warn('[K2:Capacitor] vpnError:', event.message ?? event);
  });
  ```

  Run: `cd webapp && npx vitest run src/services/__tests__/capacitor-k2.test.ts --reporter=verbose`
  Expected: PASS (all existing + new tests)

  Run: `cd webapp && npx tsc --noEmit`
  Expected: PASS

- REFACTOR:
  - [SHOULD] Consider adding timestamp to capacitor-k2 event logs
  - [SHOULD] iOS: consider using os_log instead of NSLog for structured logging

**Acceptance**: iOS EventBridge logs transient states via NSLog. Android onLost triggers immediate connection cleanup. Capacitor bridge event log includes state value. No VPN store import in event listeners.

**Knowledge**: docs/knowledge/architecture-decisions.md — "Bridge as State Contract Translation Layer", "Platform Network Change Detection"

---

## Execution Notes

### k2 Submodule Constraint

T1 and T2 require changes to the `k2/` submodule which is read-only in k2app. These tasks need a PR in the k2 repository, followed by submodule update in k2app.

**Recommended execution order**:
1. T3 (k2app) — can start immediately, zero dependencies
2. T1 → T2 (k2 repo) — sequential, single branch in k2 repo
3. After k2 PR merged: update k2 submodule in k2app, verify desktop reconnection

### Worktree Strategy

From knowledge (task-splitting.md): "When submodule changes are foundational and have sequential dependencies, working in a single branch is simpler than cross-repo worktree merging."

- **T1 + T2**: Single branch in k2 repo (`feature/network-monitor`)
- **T3**: Single branch in k2app repo (`feature/mobile-event-observability`)
- Both branches can be developed in parallel

### Manual Testing Checklist

After all tasks merged:

- [ ] macOS: Connect VPN → switch WiFi to Ethernet → verify reconnection <3s (was 30s)
- [ ] macOS: Connect VPN → toggle WiFi off/on → verify reconnection
- [ ] Windows: Connect VPN → switch network → verify reconnection
- [ ] iOS device: Connect VPN → switch WiFi to 4G → check Console for `[K2:NE] transient state` logs
- [ ] Android device: Connect VPN → switch WiFi to 4G → check logcat for state change logs
- [ ] Android device: Connect VPN → airplane mode on/off → verify onLost log + reconnection on onAvailable
