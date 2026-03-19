# Transport Happy Eyeballs: Three-Way Transport Racing

**Date**: 2026-03-19
**Status**: Draft
**Scope**: k2 wire layer (client + server), engine transport assembly

## Problem

QUIC handshake success does not guarantee data flow. A middlebox (ISP DPI, CGNAT, CPE router) can allow QUIC Long Header packets (handshake) but drop all Short Header packets (streams, keepalive, datagrams). This creates a failure mode where:

1. `connect()` succeeds (Long Header handshake completes)
2. Server logs "accepted connection"
3. All subsequent `DialTCP`/`DialUDP` silently timeout (Short Header packets dropped)
4. `healthMonitor` reports `healthy` (QUIC stack doesn't detect the blockage)
5. TCP-WS fallback never triggers (`handshakeFailStreak` only counts handshake failures)

Real case: CT Guangdong user (14.205.94.63) on QUIC hop ports 40000-40019 — 4 servers, 7 connections, all handshakes succeeded, server received zero data (`totalReceived=0`). Root cause: ISP drops non-443 UDP Short Header packets.

### Why hop ports are problematic

Original design used high ports (40000-40019) to avoid GFW attention on 443. Research shows:

- **GFW** detects QUIC proxies by protocol behavior patterns, not by port number
- **ISP middleboxes** filter by port — non-443 UDP has no legitimate protocol protection (HTTP/3 uses 443)
- **Port 443/UDP** is whitelisted by ISP middleboxes because blocking it would break HTTP/3 (Google, YouTube, Cloudflare)

The optimal strategy: QUIC on 443 (best for ISP), hop ports as secondary (avoids single-port throttling), TCP-WS as fallback (guaranteed path).

## Design

### 1. Echo Probe Protocol

New stream type for verifying Short Header data flow.

**Wire format**:

```
Stream type: 0x04 (echo)
Client sends: [8 bytes random nonce]
Server sends: [8 bytes same nonce]
```

Note: `0x03` is already used by `StreamTypeUDPOverflow`. Echo uses `0x04`.

**Client flow**:
1. `OpenStreamSync(ctx)` — first Short Header packet
2. Write stream header with type=0x04
3. Write 8-byte random nonce
4. Read 8-byte response (timeout: 1s during race, 2s during health probe)
5. Verify nonce matches
6. Close stream

**Server flow**:
1. `AcceptStream` → `ReadStreamHeader` → type=0x04
2. Read 8 bytes → Write 8 bytes → Close stream

~30 bytes total. Verifies the complete Short Header path: stream open → data write → data read → round-trip success. Nonce prevents middlebox caching/replay.

**Why stream, not DATAGRAM**: We need to verify the stream path specifically (that's what `DialTCP` uses). DATAGRAM success wouldn't prove stream works. Also, DATAGRAM has compatibility concerns with some QUIC middleboxes.

### 2. Three-Way Happy Eyeballs Race

Staggered startup with priority order:

```
t=0ms     Start QUIC 443/UDP    → connect() + echoProbe()
t=300ms   Start QUIC hop/UDP    → connect() + echoProbe()  (skip if #1 already won)
t=800ms   Start TCP-WS 443/TCP  → connect()                (skip if #1/#2 already won)

Total timeout: 5s
Winner: first to complete successfully
```

**Implementation**: `wire.RaceTransport(ctx, cfg, dialAddr) (Dialer, string, error)`

The race lives in `wire/` (not `engine/`) because it constructs transport objects (QUICClient, TCPWSClient) with internal configuration (port override, hop disable). Engine calls this single function at step 4/4.5. This respects the layer boundary rule: wire owns transport construction details, engine only receives a `Dialer` interface.

```go
type RaceResult struct {
    Dialer        Dialer
    TransportType string   // "quic-443", "quic-hop", "tcpws"
}

func RaceTransport(ctx context.Context, cfg Config, dialAddr string) (*RaceResult, error) {
    raceStart := time.Now()
    results := make(chan raceResult, 3)
    raceCtx, raceCancel := context.WithTimeout(ctx, 5*time.Second)

    var wg sync.WaitGroup
    var spawned int

    // Cleanup: cancel context, wait for all goroutines, close loser dialers.
    // Runs in background goroutine to avoid blocking the winner's return.
    defer func() {
        raceCancel()
        go func() {
            wg.Wait()
            // drain results, close loser dialers
            for i := 0; i < spawned; i++ {
                if r := <-results; r.dialer != nil && r != winner {
                    r.dialer.Close()
                }
            }
        }()
    }()

    launch := func(name string, buildFn func(context.Context) raceResult) {
        spawned++
        wg.Add(1)
        go func() {
            defer wg.Done()
            results <- buildFn(raceCtx)
        }()
    }

    // Candidate 1: QUIC on port 443 (no hop)
    launch("quic-443", func(ctx) { buildAndProbeQUIC(ctx, cfg, dialAddr, port443, noHop) })

    // Stagger candidate 2 at t=300ms from race start
    if !waitOrWinner(results, 300ms - time.Since(raceStart)) {
        launch("quic-hop", func(ctx) { buildAndProbeQUIC(ctx, cfg, dialAddr, hopPorts) })
    }

    // Stagger candidate 3 at t=800ms from race start
    if !waitOrWinner(results, 800ms - time.Since(raceStart)) {
        launch("tcpws", func(ctx) { buildTCPWS(ctx, cfg, dialAddr) })
    }

    // Collect remaining results...
}
```

**Key details**:

- **Stagger uses absolute time from race start** — `800ms - time.Since(raceStart)` ensures correct timing regardless of when earlier candidates fail.
- **WaitGroup for loser cleanup** — All goroutines are tracked with `wg`. Cleanup waits for all goroutines to finish before closing loser dialers, preventing use-after-close panics.
- **Loser cleanup is async** — `wg.Wait()` + close runs in a background goroutine so the winner returns immediately.
- **TCP-WS does not need echo probe** — TCP three-way handshake + smux session setup already verify data round-trip. Only QUIC has the Long Header / Short Header split.
- **Early exit** — At each stagger point, check if a winner exists. If so, skip launching later candidates.
- **Echo timeout 1s during race** — Shorter than the 2s health probe timeout, since the race needs fast path selection.

**QUIC-443 candidate construction**: Uses the same `Config` but overrides `Port=443` and sets `HopStart=0, HopEnd=0` to disable hop. This is an internal wire-layer detail, not exposed to engine.

### 3. TransportManager Changes

Minimal changes to existing TransportManager:

```go
type TransportManager struct {
    mu          deadlock.RWMutex
    primary     Dialer    // race winner (was: quic)
    tcpws       Dialer    // always-available fallback (unchanged)
    primaryType string    // "quic-443" | "quic-hop" | "tcpws"

    fallbackActive atomic.Bool
    freshAfterReset atomic.Bool
    probeCancel context.CancelFunc
}
```

**DialTCP/DialUDP logic** (minimal change from current):
1. Try `primary` first (skip if fallback active)
2. On transport error → try `tcpws` (unless primary IS tcpws)
3. Proxy-layer errors → return directly (no fallback)

**When tcpws wins the race**: `primary` and `tcpws` point to the same `Dialer` instance. The "unless primary IS tcpws" pointer equality check handles this — no duplicate client, no fallback attempt.

**ReplacePrimary(dialer, transportType)**: New method for hot-swapping primary during runtime re-race. Sequence:
1. Lock, swap `primary` and `primaryType`, save old primary ref, unlock
2. Close old primary outside lock
3. In-flight `DialTCP`/`DialUDP` on the old primary will get connection errors — this is the same behavior as existing `ResetConnections()`, and the application layer (DNS retry, browser retry) handles it naturally.

### 4. Runtime Re-Race

**Trigger**: healthMonitor detects data-layer failure on primary transport.

Detection mechanism: periodic echo probe (reuse same echo protocol) on the primary transport. If echo fails N consecutive times (e.g., 3), trigger re-race.

**Critical: deadlock avoidance**. The echo probe and re-race trigger must NOT run inside `hm.mu` lock scope (to avoid the same self-deadlock pattern from 2026-03-09). Implementation:

```
healthMonitor.sample() — runs under hm.mu:
  → collect wire stats (existing)
  → health FSM transitions (existing)
  → does NOT call echoProbe or reRace

healthMonitor.echoLoop() — separate goroutine, NO lock:
  → every 30s: echoProbe(primary) outside any lock
  → increment/reset atomic failure counter
  → counter >= 3 → send on reRaceChan (non-blocking)

engine.reRaceLoop() — reads from reRaceChan:
  → calls wire.RaceTransport()
  → calls tm.ReplacePrimary(winner)
```

The echo loop and re-race are fully decoupled from `sample()` via an atomic counter and a channel. No lock nesting, no synchronous calls between healthMonitor and engine.

**Re-race does NOT restart engine**:
- TUN device, route table, DNS middleware — all preserved
- Only `RaceTransport()` runs again
- Winner replaces `tm.primary` via `ReplacePrimary()`
- Old primary closed
- User perceives brief stall, not a disconnect

**Replaces existing `recoveryProbe`**: The runtime re-race mechanism supersedes the existing `recoveryProbe` (background QUIC probe during TCP-WS fallback, `recovery_probe.go`). The re-race is strictly more capable: it races all three transports instead of only probing QUIC, and it verifies data flow (echo) instead of only handshake. `recoveryProbe` should be removed to avoid conflicting recovery attempts.

### 5. Backward Compatibility

| Client | Server | Behavior |
|--------|--------|----------|
| New | New | Three-way race + echo probe |
| New | Old | Echo returns unknown type error → race degrades to handshake-only (current behavior). Logs `DIAG: echo-probe-unsupported` WARN. |
| Old | New | No echo sent, server unaffected |
| Old | Old | Unchanged |

**Graceful degradation**: When echo probe fails with a protocol error (unknown stream type), the race treats handshake success as "good enough" — identical to current behavior. This ensures new clients work with old servers. A `DIAG: echo-probe-unsupported` WARN event is logged so operators know to update the server.

### 6. Configuration

**No new URL parameters needed**. Port 443 is already the URL's host port. Hop range is already in `hop=START-END`. The race strategy is pure client-side behavior.

**No server configuration needed**. Echo handler is unconditional — always registered when present.

**Internal wire-layer detail**: QUIC-443 candidate disables hop by setting `HopStart=0, HopEnd=0` on a cloned `Config`. This is not exposed in the URL or any external config.

### 7. DIAG Logging

| Event | Level | Trigger | Fields |
|-------|-------|---------|--------|
| `DIAG: transport-race-start` | INFO | Race begins | candidates |
| `DIAG: transport-race-winner` | INFO | Race complete | winner, winnerMs, tried |
| `DIAG: transport-race-fail` | WARN | All candidates failed | quic443Err, quicHopErr, tcpwsErr |
| `DIAG: echo-probe-ok` | DEBUG | Echo succeeded | transport, rttMs |
| `DIAG: echo-probe-fail` | WARN | Echo failed | transport, err |
| `DIAG: echo-probe-unsupported` | WARN | Server doesn't support echo | transport |
| `DIAG: transport-rerace` | WARN | Runtime re-race | reason, previousTransport |

### 8. Files Changed

**k2 (Go core)**:

| File | Change |
|------|--------|
| `wire/stream.go` | Add `StreamTypeEcho = 0x04` constant |
| `wire/echo.go` | New file: `EchoProbe(ctx, conn) error` (client), echo constants |
| `wire/race.go` | New file: `RaceTransport(ctx, cfg, dialAddr)`, candidate builders, stagger logic |
| `wire/transport.go` | Rename `quic` → `primary`, add `primaryType`, add `ReplacePrimary()` |
| `engine/engine.go` | Replace step 4/4.5 with single `wire.RaceTransport()` call |
| `engine/health.go` | Add `echoLoop()` goroutine (separate from `sample()`), atomic failure counter, `reRaceChan` |
| `engine/recovery_probe.go` | Remove (superseded by re-race mechanism) |
| `server/server.go` | Add `handleEcho(stream)` case in stream type switch |

**No changes to**: webapp, desktop, mobile, config format, URL format, CLAUDE.md conventions.

### 9. netCoordinator / Re-Race Mutual Exclusion

Two mechanisms can trigger transport changes concurrently:

- **netCoordinator** → `doNetworkReconnect()` → `reconnect()` → `tm.ResetConnections()` (network change)
- **echoLoop** → `reRaceChan` → `reRaceLoop()` → `wire.RaceTransport()` + `tm.ReplacePrimary()` (data-layer failure)

**Rule: netCoordinator reconnect aborts re-race; re-race does not interfere with reconnect.**

Mechanism: `reRaceLoop` uses the engine context (`e.cancel`). When `engine.Stop()` fires (either from user disconnect or `doNetworkReconnect`'s full restart path), the context is cancelled → `RaceTransport`'s `raceCtx` is cancelled → all race goroutines exit → `reRaceLoop` exits. There is no explicit mutex between the two — context cancellation provides the ordering guarantee.

Sequence when both fire simultaneously:
```
1. echoLoop detects failure → sends on reRaceChan
2. netCoordinator fires doNetworkReconnect()
3. doNetworkReconnect() → reconnect() → tm.ResetConnections()
   (resets primary transport, clears fallback — cleans up stale state)
4. hm.resetCounters() → resets echo failure counter to 0
5. reRaceLoop picks up the signal, starts RaceTransport
6. But ResetConnections already gave the primary a fresh connection
7. If echo probe works on the fresh connection → race succeeds quickly
8. If not → race finds a better transport
```

If `doNetworkReconnect` escalates to full `Stop()` + `Start()`:
```
1. engine.Stop() → e.state = StateDisconnected
2. hm.stop() → echoLoop exits (context cancelled)
3. reRaceCancel() → reRaceLoop exits (context cancelled)
   RaceTransport cancelled → wg.Wait() → losers closed → loop returns
4. tm.Close() → safe, no concurrent users
5. engine.Start() → fresh RaceTransport in step 4/4.5
```

### 10. Risk Analysis: Deadlock, Crash, Memory

#### 10.1 Deadlock — Exhaustive Scenario Audit

All new concurrent actors and their lock behavior:

| Actor | Goroutine | Locks held | Calls outward |
|-------|-----------|------------|---------------|
| `echoLoop` | hm goroutine | **none** | `echoProbe(primary)` — opens stream on primary dialer |
| `reRaceLoop` | engine goroutine | **none** during RaceTransport; `tm.mu.Lock` briefly in ReplacePrimary | `wire.RaceTransport()` → `tm.ReplacePrimary()` |
| `RaceTransport` | wire goroutine(s) | **none** — creates new objects, no shared state | `NewQUICClient()`, `connect()`, `echoProbe()` on NEW clients |
| `ReplacePrimary` | engine goroutine | `tm.mu.Lock` (brief: swap 2 pointers + 1 string) | `oldPrimary.Close()` **outside** lock |
| `engine.Stop()` | caller goroutine | `e.mu.Lock` (brief) | `hm.stop()`, `reRaceCancel()`, `tm.Close()` — all **outside** e.mu |

**D1: reRaceLoop vs engine.Stop()**

```
reRaceLoop                          engine.Stop()
─────────────                       ──────────────
wire.RaceTransport(ctx) running     e.mu.Lock()
  (no locks held, ctx=engine ctx)   e.state = Disconnected
  ...                               e.mu.Unlock()
  ...                               hm.stop()  ← echoLoop exits
  ...                               reRaceCancel() ← ctx cancelled
  raceCtx.Done() fires
  all race goroutines exit
  wg.Wait() returns
  cleanup closes losers
  RaceTransport returns error
                                    e.mu.Lock()
reRaceLoop checks ctx.Err()         tm = e.tm; e.tm = nil
  → ctx cancelled, exits            e.mu.Unlock()
  does NOT call ReplacePrimary      tm.Close() ← safe, reRaceLoop exited
```

**Verdict: No deadlock.** Key invariant: `reRaceLoop` checks `ctx.Err()` before `ReplacePrimary`. If context is cancelled, it closes the race winner and exits without touching `tm`.

**D2: echoLoop vs engine.Stop()**

```
echoLoop                            engine.Stop()
─────────                           ──────────────
echoProbe(primary) running          e.state = Disconnected
  OpenStreamSync on primary         hm.stop() → cancel echoLoop ctx
  ...stream open in progress...     → echoProbe ctx cancelled
  returns error (context cancelled) → echoLoop exits
                                    tm.Close() ← safe, echoLoop exited
```

**Verdict: No deadlock.** `hm.stop()` cancels echoLoop context and waits for it to exit before returning. `tm.Close()` happens after `hm.stop()`.

**D3: DialTCP vs ReplacePrimary**

```
DialTCP (goroutine A)              ReplacePrimary (goroutine B)
────────────────────               ───────────────────────────
tm.mu.RLock()
primary := tm.primary
tm.mu.RUnlock()
                                   tm.mu.Lock()
                                   old := tm.primary
                                   tm.primary = new
                                   tm.mu.Unlock()
primary.DialTCP(ctx, addr)         old.Close()
→ gets conn error (old closed)
→ falls back to tcpws
```

**Verdict: No deadlock.** RLock/Lock don't nest. DialTCP gets a transient error on the old primary, falls back to tcpws. Application-level retry on the new primary succeeds.

**D4: echoLoop vs reRaceLoop**

```
echoLoop                           reRaceLoop
─────────                          ────────────
echoProbe fails
atomic counter++ → reaches 3
send on reRaceChan (non-blocking)  receives from reRaceChan
                                   wire.RaceTransport()...
echoProbe on old primary           ...building new transports...
  → may succeed or fail
  → increments/resets counter
                                   tm.ReplacePrimary(winner)
                                   old primary closed
next echoProbe on NEW primary
  → tests new transport
```

**Verdict: No deadlock.** echoLoop holds no locks. reRaceLoop holds `tm.mu` only briefly in ReplacePrimary. echoLoop may test old primary during swap — gets error, increments counter. Next probe tests new primary. No conflict.

**D5: Two re-races triggered simultaneously**

`reRaceChan` is buffered(1). If echoLoop sends while reRaceLoop is already running a race, the send is dropped (channel full). Only one race runs at a time — the loop is sequential: `for { <-reRaceChan; RaceTransport(); ReplacePrimary() }`.

**Verdict: No deadlock, no duplicate races.**

#### 10.2 Crash — Exhaustive Scenario Audit

**CR1: Race goroutine panics during connect() or echoProbe()**

Risk: `go func() { results <- buildFn(raceCtx) }()` — if `buildFn` panics, the goroutine dies, `wg.Done()` never fires, `wg.Wait()` hangs forever, loser dialers leak.

**Fix**: All race goroutines must use `safego.Go()` (project's panic-safe goroutine launcher from `safego/`). `safego.Go` wraps with `recover()` + `slog.Error`. The launch function becomes:
```go
launch := func(name string, buildFn func(context.Context) raceResult) {
    spawned++
    wg.Add(1)
    safego.Go(func() {
        defer wg.Done()
        results <- buildFn(raceCtx)
    })
}
```
If buildFn panics, `safego.Go` recovers, `wg.Done()` fires (in defer), a zero-value `raceResult` with nil dialer is sent to results. Race continues with remaining candidates.

**CR2: Double-close when primary == tcpws**

Risk: `tm.Close()` closes both `primary` and `tcpws`. If tcpws won the race, `primary == tcpws` (same pointer). Calling `Close()` twice on the same `QUICClient` or `TCPWSClient`.

**Fix**: `tm.Close()` checks pointer equality: `if primary != tcpws { primary.Close() }; tcpws.Close()`. Same check in `ReplacePrimary`: if old primary == tcpws, don't close it (tcpws is still the fallback).

**CR3: ReplacePrimary called after tm.Close()**

Risk: `reRaceLoop` finishes `RaceTransport`, context check passes (not yet cancelled), then `engine.Stop()` nils `tm`. `reRaceLoop` calls `tm.ReplacePrimary()` on nil → nil pointer dereference.

**Fix**: `reRaceLoop` reads `tm` from engine under lock before calling ReplacePrimary:
```go
e.mu.Lock()
tm := e.tm
e.mu.Unlock()
if tm == nil { winner.Close(); return } // engine already stopped
tm.ReplacePrimary(winner, transportType)
```

**CR4: echoProbe on nil or closed primary**

Risk: After `engine.Stop()` nils `tm.primary`, echoLoop calls `echoProbe(nil)`.

**Fix**: echoLoop checks context before each probe. `hm.stop()` cancels echoLoop's context and calls `wg.Wait()`, ensuring echoLoop has exited before `tm.Close()`. Additionally, `echoProbe` checks for nil dialer at entry.

**CR5: Loser cleanup goroutine panics**

Risk: The deferred cleanup goroutine (`go func() { wg.Wait(); drain; close }`) could panic if a loser's `Close()` panics.

**Fix**: Cleanup goroutine uses `safego.Go()`. Each loser `Close()` is wrapped in a recover block. Individual Close failures are logged but don't prevent other losers from being cleaned up.

#### 10.3 Memory — Exhaustive Scenario Audit

**MEM1: Race goroutine leak on context cancellation**

Risk: `RaceTransport` context cancelled mid-race. Do all goroutines exit promptly?

Analysis:
- `QUICClient.connect()` calls `quic-go.Dial(ctx)` — respects context, returns on cancel ✓
- `TCPWSClient.connect()` calls `net.Dialer.DialContext(ctx)` — respects context ✓
- `echoProbe()` calls `OpenStreamSync(ctx)` — respects context ✓
- `wg.Wait()` in cleanup goroutine waits for all to finish ✓
- Cleanup goroutine closes all created dialers ✓

**Verdict: No leak.** All blocking operations respect context cancellation. WaitGroup ensures synchronization.

**MEM2: reRaceChan buffer overflow / goroutine block**

Risk: echoLoop sends on `reRaceChan` while reRaceLoop is busy with a 5s race.

**Fix**: `reRaceChan` is `make(chan struct{}, 1)`. Non-blocking send:
```go
select {
case reRaceChan <- struct{}{}:
default: // race already pending, skip
}
```
Duplicate triggers are harmless. Only one race runs at a time.

**MEM3: Accumulated transport objects across multiple re-races**

Risk: Each re-race creates up to 3 new transport objects. Do they accumulate?

Analysis: Winner replaces `tm.primary` → old primary is closed. Losers are closed by cleanup goroutine. Every re-race cleans up all objects it creates. Net accumulation: zero.

**MEM4: echoLoop goroutine leak on engine.Stop()**

Risk: echoLoop doesn't exit, holds reference to primary/tm after they're closed.

**Fix**: echoLoop uses engine context. `hm.stop()` cancels the context and calls `hm.wg.Wait()` which includes echoLoop's goroutine. After `hm.stop()` returns, echoLoop has exited. `tm.Close()` follows — safe.

**MEM5: Cleanup goroutine outlives engine**

Risk: The deferred cleanup goroutine in `RaceTransport` runs `wg.Wait()` in background. If a race goroutine is stuck in a slow `connect()` (despite context cancel), the cleanup goroutine lives as long as the slowest exit.

Analysis: quic-go's `Dial()` with cancelled context returns within milliseconds (it closes the UDP socket). TCPWSClient's `DialContext` also returns promptly. Maximum cleanup goroutine lifetime: a few hundred milliseconds after race returns.

**Worst case**: If quic-go has a bug and blocks longer, the cleanup goroutine holds references to loser QUICClient objects until they exit. This is bounded (at most 2 loser objects per re-race) and temporary. Not a sustained leak.

#### 10.4 Timing Correctness

**T1: Race stagger math**

```
raceStart = time.Now()

Candidate 1: launched at t=0
Candidate 2: waitOrWinner(300ms - time.Since(raceStart))
  If candidate 1 failed at t=50ms → timer = 250ms → launch at t=300ms ✓
  If candidate 1 still running at t=300ms → timer fires → launch at t=300ms ✓
  If candidate 1 succeeded at t=200ms → skip candidate 2 ✓

Candidate 3: waitOrWinner(800ms - time.Since(raceStart))
  Measured from raceStart, not from candidate 2 launch ✓
  If time.Since(raceStart) > 800ms → timer = 0 → launch immediately ✓
```

**T2: echoLoop vs sample() frequency**

- `sample()` runs at 1 Hz (health metrics)
- `echoLoop` runs every 30s (data-layer verification)
- No interaction between them — different goroutines, different concerns
- echoLoop failure counter is atomic — no race with sample()

**T3: re-race during initial connect**

`reRaceLoop` starts after `engine.Start()` step 9 (commit). At this point `tm.primary` is already set by the initial `RaceTransport`. echoLoop starts with hm. Both are idle until 30s after connect. No timing conflict with initial setup.

### 11. Implementation Invariants (must hold at all times)

These invariants are the contract between concurrent actors. Violation of any invariant is a bug.

1. **echoLoop holds no application locks** — it only uses atomic counter and non-blocking channel send.
2. **reRaceLoop checks `ctx.Err()` before `ReplacePrimary`** — if context is cancelled (engine stopping), close the winner and exit without touching tm.
3. **reRaceLoop reads `e.tm` under `e.mu` before calling ReplacePrimary** — if tm is nil, engine is stopped, close winner and exit.
4. **`hm.stop()` completes before `tm.Close()`** — ensures echoLoop has exited before primary dialer is closed.
5. **`reRaceCancel()` fires before `tm.Close()`** — ensures reRaceLoop has exited (or will exit before touching tm).
6. **`tm.Close()` checks `primary != tcpws` before closing primary** — prevents double-close when tcpws won the race.
7. **All race goroutines use `safego.Go()`** — panic in one candidate doesn't crash the process or leak goroutines.
8. **`reRaceChan` is buffered(1) with non-blocking send** — duplicate triggers are dropped, no goroutine blocks on send.
