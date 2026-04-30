# k2subs:// Server-Switching E2E Test — Design

**Date:** 2026-04-30
**Status:** Draft (awaiting review)
**Owner:** k2 / desktop daemon team
**Scope:** Go-level E2E tests for the k2subs:// server-switch decision surface

## 1. Problem

`k2subs://` is the desktop daemon's subscription URL scheme. The daemon resolves it to a list of `k2v5://` tunnels, picks one, and silently rotates to a different tunnel when the active one fails. The decision surface is large: weighted random selection, a probe-driven score adjustment with flake tolerance, per-session exclude state, periodic background refresh, an engine-side two-phase replacement loop, and atomic dialer hot-swap.

Each piece is unit-tested today, but no test reads end-to-end from "user gives a `k2subs://` URL" through "engine swaps to a different server because the wire died." Most user-perceptible failure modes — switching during ISP throttling, Center maintenance, switch storms, frequent toggle, regional pools — are absent from the suite.

This spec defines a single E2E test file that exercises every switch trigger through the real `Subscription` / `Manager` / `probe.Registry` / `engine.replaceOutboundServer` path, fakes only the wire-setup boundary, and runs in under 8 seconds with no external dependencies.

## 2. Goals & Non-goals

**Goals**

- Cover all 5 layers where switch decisions are made: `Subscription.Pick`, `Manager.NextURL`, `engine.replaceOutboundServer`, `Subscription.refreshLoop`, `Manager.Resolve`.
- Cover real-user scenarios: maintenance windows, switch storms, frequent toggle, stale probe data, regional pools, "fake-alive" servers.
- Run as part of `make quick-check` — sub-8-second total wall time, deterministic, no root, no TUN, no live network.
- Be the canonical place a future reader can read end-to-end to understand "what does k2subs do when the active server dies?"

**Non-goals**

- Wire-protocol correctness (already covered by `wire/contract_test.go` and `test/e2e_test.go`).
- Mobile / appext path — `k2subs://` is desktop-only per `mobile/CLAUDE.md`. Mobile receives a single `k2v5://` URL, with no switch logic.
- Daemon HTTP API blackbox — `POST /api/up` adds nothing for switch logic since the daemon delegates to `Manager` + `engine`.
- Replacing existing unit tests — `subscription/*_test.go`, `engine/outbound_replace_test.go`, and `probe/registry_test.go` stay as fast unit-level coverage.

## 3. Test Architecture

### 3.1 Real vs. fake

| Component | Real or fake | Why |
|---|---|---|
| `*config.Subscription` (URL parse, fetch, Pick, refresh, cache) | **Real** | This is the SUT. |
| `subscription.Manager` (Resolve, NextURL, exclude state, sessions) | **Real** | This is the SUT. |
| `probe.Registry` (TTL, flake tolerance, sentinel) | **Real** | This is the SUT. |
| `engine.Engine.replaceOutboundServer` (Phase B server swap) | **Real** | This is the SUT. |
| Subscription HTTP endpoint | **Fake** (`httptest.NewTLSServer`, hot-mutable handler) | Avoids spinning up Center. |
| `setupOutboundTransportFn` (wire-layer transport build) | **Fake** (injectable, like `engine/outbound_replace_test.go` already does) | Avoids real wire/QUIC/TUN. |
| `wire.TransportManager` / `wire.SwappableDialer` | **Real** | They expose the swap point we assert on. Constructed via `wire.NewTransportManager()` / `wire.NewSwappableDialer()` with no live transport — same pattern as existing contract tests. |
| `engine.Engine` | **Real** but with `state=Connected` / `outboundTMs` populated through a test export hook (instead of `Start()`). | `Start()` requires a real provider/TUN/wire. |

### 3.2 File layout

```
k2/test/k2subs_switch_e2e_test.go        — 22 t.Run subtests across 5 groups (A/B/C/D/E)
k2/test/k2subs_switch_helpers_test.go    — fakeSubsServer, fakeWireSetup, k2subsFixture
k2/engine/test_export_test.go            — engine test hooks (compiled only in test build)
```

`engine/test_export_test.go` adds these test-only exports (file ends in `_test.go`, so they never reach the production binary):

```go
// k2/engine/test_export_test.go
package engine

func NewTestTarget(url string) *outboundTransport { ... }
func AttachTestTarget(e *Engine, target *outboundTransport) { ... }
func TriggerReplaceForTest(e *Engine, target *outboundTransport, reason error, dd wire.DirectDialer, p OutboundProvider) error { ... }

// outboundTransport accessors used by package test
func (o *outboundTransport) URL() string { return o.url }
func (o *outboundTransport) TM() *wire.TransportManager { return o.tm }
func (o *outboundTransport) Swappable() *wire.SwappableDialer { return o.swappable }
```

These are the minimum needed to construct a target outside `Start()` and drive `replaceOutboundServer` without going through `reRaceChan`. State is set to `Connected` via the existing exported `engine.ForceConnected()`. ~25 lines total.

### 3.3 Helpers — `k2subs_switch_helpers_test.go`

```go
// fakeSubsServer wraps httptest.NewTLSServer with a hot-mutable response.
type fakeSubsServer struct {
    srv      *httptest.Server
    mu       sync.Mutex
    tunnels  []config.Tunnel
    refresh  *int
    fetches  atomic.Int64
    failNext atomic.Bool       // next request returns 503
    failAll  atomic.Bool       // all requests return 503 until cleared
    queryLog []string          // r.URL.RawQuery captured per request
}
func newFakeSubsServer(t *testing.T) *fakeSubsServer
func (f *fakeSubsServer) SetTunnels(t []config.Tunnel)
func (f *fakeSubsServer) SetRefresh(secs int)
func (f *fakeSubsServer) FailNext()
func (f *fakeSubsServer) FailAll(on bool)
func (f *fakeSubsServer) URL(creds string) string
func (f *fakeSubsServer) Close()

// fakeWireSetup replaces engine.setupOutboundTransportFn with a programmable
// stub. Tracks call order for assertions.
type fakeWireSetup struct {
    mu        sync.Mutex
    failURLs  map[string]error          // setup returns this error for matching URLs
    slowURLs  map[string]time.Duration  // setup blocks for this duration
    setupLog  []string                  // accumulated URLs setup was called with
    builtTMs  map[string]*wire.TransportManager  // URL -> built TM (for "currently held" assertions)
}
func (f *fakeWireSetup) Install(t *testing.T)        // replaces setupOutboundTransportFn, t.Cleanup restores
func (f *fakeWireSetup) Calls() []string             // snapshot
func (f *fakeWireSetup) BuiltTM(url string) *wire.TransportManager

// k2subsFixture composes the moving parts.
type k2subsFixture struct {
    Reg       *probe.Registry
    Mgr       *subscription.Manager
    Engine    *engine.Engine
    Target    *engine.outboundTransport  // attached to Engine via test export
    SubsSrv   *fakeSubsServer
    WireSetup *fakeWireSetup
    cacheDir  string
}
func newK2subsFixture(t *testing.T, initial []config.Tunnel) *k2subsFixture
func (f *k2subsFixture) Resolve(t *testing.T, ctx context.Context) (resolvedURL string)
func (f *k2subsFixture) TriggerSwitch(t *testing.T, ctx context.Context, reason error) error

// Assertion helpers
func mustPickWithin(t *testing.T, sub *config.Subscription, want string, n int)
func mustNeverPick(t *testing.T, sub *config.Subscription, banned string, n int)
func waitFetchCount(t *testing.T, srv *fakeSubsServer, atLeast int64, timeout time.Duration)
```

### 3.4 Per-test goroutine hygiene

Every subtest ends with:

```go
t.Cleanup(func() {
    fixture.Mgr.CloseAll()
    goleak.VerifyNone(t)
})
```

`goleak` is already a project dependency — see `wire/testmain_test.go`.

## 4. Scenarios

22 subtests in 5 groups. All run under `go test ./test/...` with `-race` and pass goleak.

### Group A — Pick layer in switch context (3 tests)

| ID | Subtest | Verifies |
|---|---|---|
| A1 | `A1_WeightedInitialPick` | `Resolve()` produces an initial pick whose distribution across 1000 cold-start fixtures matches `recommendScore` weights within 60–90% tolerance. |
| A2 | `A2_NextURLSwapsDialer` | Single `replaceOutboundServer` call: provider returns a different URL, `setupOutboundTransportFn` succeeds, `target.swappable.Current() == newTM`, `target.url == newURL`, `target.reRaceFailStreak == 0`. |
| A3 | `A3_TopK5Truncation` | 7 candidates with scores 0.9..0.3; over 1000 `NextURL` calls, the lowest 2 (0.3, 0.4) are never picked. |

### Group B — Probe ↔ switch interaction (4 tests)

| ID | Subtest | Verifies |
|---|---|---|
| B1 | `B1_SentinelNoPenalty` | All URLs have `Score = -1` (echo unsupported). `Pick` falls back to pure recommendScore. |
| B2 | `B2_SingleZeroFlakeTolerated` | One `score=0` record on URL X. `NextURL` may still pick X — single zero is "no data", not exclusion. |
| B3 | `B3_DoubleZeroHardExclude` | Two consecutive `score=0` records on URL X. 50 `NextURL` calls never return X. |
| B4 | `B4_AllExcludedFiresFatal` | All tunnels have score=0 ×2. `NextURL` returns `"all subscribed tunnels failed"`; `replaceOutboundServer` returns the wrapped error; engine's caller would invoke `OnOutboundFatal`. |

### Group C — Refresh ↔ switch interaction (4 tests)

| ID | Subtest | Verifies |
|---|---|---|
| C1 | `C1_RefreshAddsTunnel` | Initial `[A, B]`; refresh changes server response to `[A, B, C]`; after refresh fires, `NextURL` excluding `[A, B]` returns C. |
| C2 | `C2_RefreshRemovesCurrentURL` | Connected to A; refresh changes pool to `[B, C]`; `NextURL("k2v5://A")` returns B or C without error (A not present in pool ≠ failure). |
| C3 | `C3_RefreshFailPreservesData` | After Resolve, server returns 503 indefinitely; in-memory tunnel list survives; `NextURL` continues working from the last successful list. After server recovers, next refresh syncs the new list. |
| C4 | `C4_ServerOverridesRefreshInterval` | URL has `?refresh=1800`; server returns `refresh:1`. Within 2.5 seconds, ≥2 fetches occur. (Existing unit test; replicated here against the full fixture.) |

### Group D — Lifecycle / concurrency (5 tests)

| ID | Subtest | Verifies |
|---|---|---|
| D1 | `D1_OfflineWithCache` | 1st fixture fetches & caches; 2nd fixture with same URL: `failAll = true`; `Resolve()` succeeds via cache, picked URL is from cached list. |
| D2 | `D2_OfflineNoCache` | `failAll = true`, no prior cache; `Resolve()` returns error wrapping "fetch failed (no cache)". |
| D3 | `D3_MultiRouteIsolation` | `cfg.Routes` has 2 distinct `k2subs://` URLs; 2 sessions created; per-session `excluded` arrays don't bleed across sessions. |
| D4 | `D4_StopDuringPhaseB` | `setupOutboundTransportFn` blocks 200ms; ctx canceled at 100ms; `replaceOutboundServer` returns `context.Canceled`; the in-flight new TM is closed (verified by inspecting `wire.TransportManager` post-cleanup state). goleak passes. |
| D5 | `D5_SetupFailEscalatesExclude` | Provider returns A→fail, B→fail, C→success. Provider's `calls` recorded with `currentURL` progression `[origin, A, B]`. `setupLog == [A, B, C]`. Final `target.url == C`. |

### Group E — Real-user scenarios (6 tests)

| ID | User Story | Verifies |
|---|---|---|
| E1 | Subway flake — toggle 5×. | 5 successive `Resolve → TriggerSwitch → Mgr.CloseAll` cycles, each in a fresh fixture; `goleak.VerifyNone(t)` passes after every cycle; `Mgr.SessionsLen() == 0` post-CloseAll. |
| E2 | Center maintenance window. | After Resolve, `failAll = true`. Without sleeping, trigger 3 `replaceOutboundServer` calls back-to-back (the test does not actually wait for refresh ticks). All 3 switches succeed using the pre-fetched tunnel list. `fakeSubsServer.fetches` shows refresh attempts during `failAll` returned 503 (verified via at least one `DIAG: subs-refresh-fail` log capture). After clearing `failAll`, the next refresh tick syncs a new tunnel list and a subsequent switch can pick from it. The "30 seconds" in the user story has no real-time analog in the test — this is a logical sequence, not a time-based one. |
| E3 | Switch storm. | Spawn 5 goroutines that each call `TriggerReplaceForTest` against the same target with distinct provider URL queues. Verifies: (a) `-race` detector stays silent across `target.swappable.Swap` + the four protected fields under `e.mu` (`tm`, `wireCfg`, `dialAddr`, `name`, `url`, `reRaceFailStreak`); (b) `target.swappable.Current()` after all goroutines return equals exactly one of the built new TMs; (c) every other built new TM was Close()d (tracked via the `closeTracker` Dialer wrapper from §6). The test does NOT assert how many concurrent calls "win" — production serializes via the single `outboundReplaceLoop` goroutine, but this test exercises the lower-level race-safety contract. |
| E4 | Overnight idle, stale probe data. | Build `probe.Registry` entry with `MeasuredAt = now - 20min` (TTL is 15min), `score = 0`. `Score(url)` returns `(0, false)` — entry expired; `NextURL` may pick the URL. |
| E5 | Regional pool — `?country=jp`. | k2subs URL contains `?country=jp&refresh=1800`. `Subscription.endpoint` strips `refresh` but keeps `country=jp`. `fakeSubsServer.queryLog[0]` contains `country=jp` and not `refresh=`. |
| E6 | "Fake-alive" server farm. | All URLs return success from `setupOutboundTransportFn` initially. Test invokes `TriggerReplaceForTest` 5 times, each time the just-installed URL is added to a synthetic ban list and the next `NextURL` excludes it. After 5 cycles all URLs are excluded; the 6th call returns "all subscribed tunnels failed"; `OnOutboundFatal` invoked exactly once. |

### What is NOT covered (and why)

- **Real wire / QUIC / TLS handshake failure** — `test/e2e_test.go` and `test/live/` already cover this.
- **Phase A re-race against the same URL** — that's wire-layer reconnect, not server switch. Already in `engine/contract_test.go`.
- **`Subscription.Pick` weighted distribution edge cases** — covered by 13 tests in `subscription/subscription_test.go`.
- **Daemon HTTP `/api/up` lifecycle** — adds no switch-logic coverage.
- **Mobile k2v5:// manual selection** — has no switch logic.

## 5. Error Injection Surface

| Injection point | Controller | Used by |
|---|---|---|
| Subscription endpoint state | `fakeSubsServer.{SetTunnels,SetRefresh,FailNext,FailAll}` | C1–C4, D1, D2, E2 |
| Probe registry score | `reg.Record(url, ProbeStats{...})` | B1–B4, E4 |
| Wire setup outcome | `fakeWireSetup.{failURLs,slowURLs}` | A2, D4, D5, E6 |
| Cache state | direct `os.WriteFile` to `cacheDir/subs-*.json` | D1, D2 |
| Context cancellation | `ctx, cancel := WithCancel(...)` | D4 |
| Time skew (probe TTL) | construct `MeasuredAt = now - past` directly | E4 |

No time-mocking library is introduced — TTL expiry is tested by writing a measurement with a back-dated `MeasuredAt`.

## 6. Optional Wire-Layer Addition

`wire.TransportManager` does not currently expose a `Closed() bool` accessor. D4 needs to assert "the in-flight new TM that was orphaned by ctx cancellation got Close()d." Two options:

1. Add `func (tm *TransportManager) Closed() bool { return tm.primary == nil && tm.tcpws == nil }` (5 lines, matches existing close semantics).
2. In the fixture, wrap the inner `Dialer` returned to `setupOutboundTransportFn` with a `closeTracker` that records `Close()` calls.

I'll go with option 2 — keeps the production wire package untouched. If reviewers prefer option 1, swap is trivial.

## 7. Runtime Budget

| Group | Estimate | Notes |
|---|---|---|
| A | <500ms | A1 runs 1000 fixtures × cheap Resolve path |
| B | <300ms | Pure logic, no sleeps |
| C | ~3s | C4 needs 2.5s for refresh×2 |
| D | <1s | D4 has 200ms block |
| E | ~3s | E1 toggle ×5; E2 simulated maintenance window |
| **Total** | **<8s** | Fits inside `make quick-check` |

All scenarios run under `-race`. None require `-short` skipping. None need root, TUN, or external network.

## 8. CI

`go test ./test/...` is already the canonical test path. The new file is included automatically. No new Make targets, no new CI workflow, no new env vars.

## 9. Implementation Order

(Exact ordering will be enumerated in the implementation plan; this is the rough shape.)

1. Add `engine/test_export_test.go` with 4 hooks + 3 accessors.
2. Add `test/k2subs_switch_helpers_test.go` with `fakeSubsServer`, `fakeWireSetup`, `k2subsFixture`.
3. Implement A2 as smoke (single switch, full path) — verify the fixture works.
4. Add A1, A3 (Pick-layer scenarios).
5. Add B1–B4 (probe-driven scenarios).
6. Add C1–C4 (refresh scenarios).
7. Add D1–D5 (lifecycle / concurrency).
8. Add E1–E6 (real-user scenarios).
9. Run with `-race`, confirm goleak clean across all subtests.
10. Run `make quick-check`. Ensure no regressions.
11. Stage commit, request review.

## 10. Open Questions

None at this time. (Resolved during brainstorming: package home = `k2/test/`, fakes for wire setup, real Manager/Engine/Registry, no daemon HTTP layer, no time-mocking library.)
