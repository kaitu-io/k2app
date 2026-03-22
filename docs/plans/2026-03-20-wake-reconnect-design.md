# Wake Reconnect Design

**Date**: 2026-03-20
**Trigger**: Feedback ticket #49 — user connected via QUIC, Mac sleeps, wakes, DNS through tunnel fails indefinitely while heartbeat reports `health=healthy`. User sees "connected" but can't browse.

## Problem

macOS sleep freezes Go's monotonic clock. QUIC KeepAlive (10s) and MaxIdleTimeout (30s) timers don't fire during sleep. After wake:

1. Server has already closed the QUIC connection (idle timeout fired server-side)
2. Client's cached QUIC session looks alive (no CONNECTION_CLOSE received — NAT mapping expired)
3. macOS doesn't always send a network change signal (10 wake events, only 1 had `SignalChanged`)
4. Health monitor reports `healthy, rttMs=0, loss=0` — metrics reflect a zombie QUIC session
5. DNS through tunnel times out 100% — 9679 failures over 5 hours, zero successful proxy DNS

The `DIAG: wake` event (wall-clock gap > 5s) detects the sleep reliably, but currently only logs — no recovery action.

## Solution

Extract wake detection into a `wakeDetector` struct embedded in `healthMonitor`. When wall-clock gap exceeds 30s, trigger `doNetworkReconnect()` — the same path as a network interface change.

### Why 30s threshold

- QUIC MaxIdleTimeout = 30s → server has certainly closed the connection
- UDP NAT mapping typically expires in 30-120s
- Short gaps (5-29s) may be system load spikes, not real sleep — reconnecting would be disruptive
- From the ticket: 7 of 10 wake events had sleep > 30s; all would trigger reconnect and resolve the issue

### Why `doNetworkReconnect()` (not `onHealthCritical()`)

Wake-after-sleep is semantically a network change: the UDP path is stale, NAT mappings expired, DNS connections are dead. `doNetworkReconnect()` does the right things:
- Resets DNS failure counter (stale after network change)
- Calls `reconnect()` → resets TransportManager, proxy DNS pool, health counters
- Clears non-client wire errors
- Broadcasts status update

`onHealthCritical()` skips DNS counter reset and stale error clearing — wrong semantics for wake.

## Design

### New type: `wakeDetector`

```go
// wakeDetector detects sleep/wake cycles via wall-clock gaps and triggers
// reconnect when the gap exceeds the reconnect threshold. Designed as a
// standalone unit embedded in healthMonitor — no goroutine, no lock, called
// from sample() at 1Hz.
type wakeDetector struct {
    lastWall           time.Time
    onWakeReconnect    func()   // wired after construction (like reRaceChan)
}
```

**Constants:**
- `wakeDiagThreshold = 5s` — emit `DIAG: wake` log (existing behavior, unchanged)
- `wakeReconnectThreshold = 30s` — trigger reconnect (new behavior)

**Method:**
```go
// checkWake compares wall-clock delta to detect sleep/wake. Returns the wall-clock
// gap and whether reconnect is needed. Called under hm.mu, but reconnect action
// MUST be executed outside the lock by the caller.
//
// Uses Round(0) to strip Go's monotonic reading, which freezes during macOS sleep.
// Wall clock advances normally, revealing the true gap.
func (wd *wakeDetector) checkWake(now time.Time, health, transport string, uptimeS int) (
    wallGap time.Duration, reconnect bool,
)
```

Logic:
1. First call: initialize `lastWall`, return (0, false)
2. Compute `wallGap = now.Round(0).Sub(lastWall.Round(0))`
3. Update `lastWall = now`
4. If `wallGap > wakeDiagThreshold`: emit `DIAG: wake` log with `sleepS`, `health`, `transport`, `uptimeS`
5. Return `(wallGap, wallGap >= wakeReconnectThreshold)`

Note: `checkWake` does NOT log whether reconnect actually executes — it only knows the threshold was met. The actual reconnect log (`"engine: wake reconnect"`) is emitted by the closure in engine.go when it runs, giving accurate observability.

### Changes to `healthMonitor`

**Struct:**
- Remove `lastSampleWall` field
- Add `wake wakeDetector` field

**`newHealthMonitor`:**
- No signature change. `onWakeReconnect` is wired post-construction (same pattern as `hm.reRaceChan` at engine.go:351), avoiding breakage of ~15 test call sites.

**`sample()`:**
- Replace 15-line inline wake detection block with:
  ```go
  _, wakeReconnect := hm.wake.checkWake(now, hm.health, ws.Transport,
      int(time.Since(hm.connectedAt).Seconds()))
  ```
- After `hm.mu.Unlock()`, execute wake reconnect **only if health-critical action didn't already fire** (prevents double-reconnect in the same tick):
  ```go
  if action != nil {
      action()
  }
  if wakeReconnect && action == nil && hm.wake.onWakeReconnect != nil {
      hm.wake.onWakeReconnect()
  }
  ```
- Rationale: if health FSM already triggered reconnect via `action()`, `reconnect()` inside it resets TransportManager and health counters. Firing wake reconnect on top would produce a second `ResetConnections()` and duplicate `StateReconnecting` broadcast within milliseconds.

### Changes to `engine.go`

**`Start()`:**
- Wire `hm.wake.onWakeReconnect` after construction (same line as `hm.reRaceChan`):
  ```go
  hm.reRaceChan = e.reRaceChan
  hm.wake.onWakeReconnect = func() {
      // Guard: skip if network is known down (e.g., airplane mode wake on mobile).
      // Mirrors onHealthCritical()'s network-up gate.
      // Note: during Stop(), netCoord may be nil — reconnect() guards via state check.
      e.mu.Lock()
      nc := e.netCoord
      e.mu.Unlock()
      if nc != nil && !nc.isNetworkUp() {
          slog.Debug("engine: wake reconnect skipped, network down")
          return
      }
      slog.Info("engine: wake reconnect")
      e.doNetworkReconnect()
  }
  ```
- The `netCoord.isNetworkUp()` guard prevents futile reconnects when waking in network-down state (common on mobile: airplane mode, underground). Desktop macOS typically has network up at wake, so the guard is a no-op for the ticket's scenario but correct for cross-platform use.

### DIAG event update

`DIAG: wake` event unchanged (same fields: `sleepS`, `health`, `transport`, `uptimeS`).

When reconnect actually executes, a separate `"engine: wake reconnect"` INFO log is emitted by the closure. This gives accurate observability — `DIAG: wake` without a following `wake reconnect` means the threshold wasn't met or a guard blocked it.

## Tests

### `TestWakeDetector` (unit, no goroutines)

Directly test `wakeDetector.checkWake()`:

| Case | Input wallGap | Expected |
|------|---------------|----------|
| First call (zero lastWall) | — | (0, false) |
| Normal tick (1s gap) | 1s | (1s, false) |
| Short sleep (10s) | 10s | (10s, false) — DIAG log emitted but no reconnect |
| Long sleep (60s) | 60s | (60s, true) — reconnect |
| Boundary (exactly 30s) | 30s | (30s, true) — reconnect |
| Below boundary (29s) | 29s | (29s, false) — no reconnect |

### `TestWakeDetector_CallbackOutsideLock` (integration)

Verify that `onWakeReconnect` is called outside `hm.mu` lock — same pattern as existing `deadlock_test.go:TestHealthMonitor_NoDeadlock`. Simulate a callback that acquires a shared mutex to prove no deadlock.

## Files changed

| File | Change |
|------|--------|
| `k2/engine/health.go` | Add `wakeDetector` type + constants; modify `healthMonitor` struct (remove `lastSampleWall`, add `wake wakeDetector`), modify `sample()` |
| `k2/engine/health_test.go` | Add `TestWakeDetector`, `TestWakeDetector_CallbackOutsideLock` |
| `k2/engine/engine.go` | Wire `hm.wake.onWakeReconnect` post-construction (no `newHealthMonitor` signature change) |
| `k2/engine/CLAUDE.md` | Update `health.go` description to mention `wakeDetector` |
| `k2/CLAUDE.md` | Add `reconnect` field to `DIAG: wake` event table |

## Not in scope

- DNS failure escalation (DNS failures → reconnect). Tracked separately.
- Active echo probe after wake. The reconnect already resets all connections — next echo probe (within 30s) validates the new path.
- Adaptive threshold. 30s is correct for QUIC MaxIdleTimeout. If server timeout changes, update the constant.
