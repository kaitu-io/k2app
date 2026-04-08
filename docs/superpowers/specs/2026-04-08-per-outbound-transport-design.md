# Per-Outbound Transport Refactor

## Problem

engine.go step 6.5 creates all k2v5 wireOutbound instances with the **same TransportManager**:

```go
// engine.go:320 — ALL k2v5 outbounds share one TM
case strings.HasPrefix(via, "k2v5://"):
    outbounds[target] = core.NewWireOutbound("k2v5", tm, e)
```

TransportManager connects to one server (`client.Server`, derived from the first k2v5 route). When config has multiple k2v5 routes with different servers (e.g., Japan + US), all traffic goes through the same server. **This is a bug.**

Real-world scenario:
```yaml
routes:
  - via: "k2v5://A"   # Japan server
    match: { rule: ["a"] }
  - via: "k2v5://B"   # US server
    match: { rule: ["b", "a"] }
  - via: direct
```

Rule matching is correct (different Target indices for A and B). Transport binding is wrong (both use same TM → same server).

This also blocks k2subs-as-Dialer: each subscription needs its own transport to its own server pool.

## Scope

**In scope:**
- Per-unique-k2v5-URL TransportManager in engine step 6.5
- Move transport race (step 3-4) into step 6.5 per-outbound loop
- Health monitor, echo loop, reconnect adapted for multiple TMs
- DNS transport selection made explicit

**Out of scope:**
- k2subs-as-Dialer (follow-up, unblocked by this)
- k2p changes (already has independent transport)
- wire/transport.go changes (TransportManager itself unchanged)
- core/ changes (wireOutbound already abstract)
- daemon/ and appext/ changes (engine-internal refactor)

## Scale

Practical maximum: 2 k2v5 outbounds (e.g., Japan + US via k2subs). k2p is already independent. iOS memory and concurrency concerns do not apply at this scale.

## Design

### Engine struct changes

```go
// Before
type Engine struct {
    tm       *wire.TransportManager
    wireCfg  wire.Config
    // ...
}

// After
type Engine struct {
    outboundTMs []*outboundTransport  // all wire TMs, for health/reconnect iteration
    primaryTM   *outboundTransport    // first k2v5 route's TM — DNS uses its tcpws
    // wireCfg removed — each outboundTransport has its own
}

// New helper struct (engine-internal, unexported)
type outboundTransport struct {
    tm      *wire.TransportManager
    wireCfg wire.Config
    dialAddr string
    name    string  // for logs, e.g. "k2v5-jp", "k2v5-us"
}
```

### engine.Start changes

**Current flow:**
```
step 1:  rule download + build
step 2:  wire.ParseURL(client.Server)          ← single server
step 2.5: start NetworkMonitor
step 3:  resolveDialAddr                       ← single server
step 4:  RaceTransport → tm                    ← single TM
step 5:  Router
step 6:  Provider
step 6.5: outbound table (all k2v5 → same tm)  ← BUG
step 7:  ClientTunnel
step 8:  DNS (tcpwsDialer from tm)
step 9:  commit
```

**New flow:**
```
step 1:   rule download + build
step 2:   (removed — merged into step 6.5)
step 2.5: start NetworkMonitor
step 3-4: (removed — merged into step 6.5)
step 5:   Router
step 6:   Provider
step 6.5: for each unique k2v5:// via URL:
            wire.ParseURL → resolveDialAddr → RaceTransport → NewTM
            → outboundTransport → wireOutbound(tm)
          (parallel race for multiple URLs)
          designate first as primaryTM
step 7:   ClientTunnel
step 8:   DNS (tcpwsDialer from primaryTM.tm)
step 9:   commit
```

**Parallel race:** When multiple k2v5 URLs exist, run RaceTransport concurrently with `errgroup`. Total startup time = max(individual races), not sum. Single URL case is identical to current behavior.

**Dedup:** `buildOutboundMap` already deduplicates by via URL string. Same URL → same Target → same TM. Different URLs → different Targets → different TMs. No additional dedup needed.

### step 6.5 pseudocode

```go
// Build per-URL transport managers.
type tmEntry struct {
    ot  *outboundTransport
    err error
}
tmMap := make(map[string]*outboundTransport) // via URL → transport

// Collect unique k2v5 URLs that need transport.
var k2v5URLs []string
for via := range outboundMap {
    if strings.HasPrefix(via, "k2v5://") {
        k2v5URLs = append(k2v5URLs, via)
    }
}

// Race transports in parallel.
results := make([]tmEntry, len(k2v5URLs))
var g errgroup.Group
for i, via := range k2v5URLs {
    i, via := i, via
    g.Go(func() error {
        wCfg, err := wire.ParseURL(via)
        if err != nil { results[i] = tmEntry{err: err}; return nil }
        
        dialAddr, err := resolveDialAddr(ctx, wCfg, cfg.DirectDialer)
        if err != nil { results[i] = tmEntry{err: err}; return nil }
        if dialAddr != "" { wCfg.DialAddr = dialAddr }
        
        raceCtx, raceCancel := context.WithTimeout(ctx, 15*time.Second)
        raceResult, err := wire.RaceTransport(raceCtx, wCfg, wCfg.DialAddr, cfg.DirectDialer)
        raceCancel()
        if err != nil { results[i] = tmEntry{err: err}; return nil }
        
        tm := wire.NewTransportManager()
        tm.SetPrimary(raceResult.Dialer, raceResult.TransportType)
        if raceResult.TCPWS != nil {
            tm.SetTCPWS(raceResult.TCPWS)
        } else if raceResult.TransportType != "tcpws" {
            tcpws := wire.NewTCPWSClient(wCfg, nil)
            if cfg.DirectDialer != nil { tcpws.SetDirectDialer(cfg.DirectDialer) }
            tm.SetTCPWS(tcpws)
        }
        tm.StartQuicProbe(ctx)
        
        results[i] = tmEntry{ot: &outboundTransport{
            tm: tm, wireCfg: wCfg, dialAddr: dialAddr,
            name: fmt.Sprintf("k2v5-%s", wCfg.Host),
        }}
        return nil
    })
}
g.Wait()

// Check results, build tmMap.
for i, r := range results {
    if r.err != nil {
        // Clean up already-created TMs.
        for _, prev := range results[:i] {
            if prev.ot != nil { prev.ot.tm.Close() }
        }
        return e.fail(fmt.Errorf("transport setup for %s: %w", k2v5URLs[i], r.err))
    }
    tmMap[k2v5URLs[i]] = r.ot
}

// Build outbound table.
for via, target := range outboundMap {
    switch {
    case strings.HasPrefix(via, "k2v5://"):
        ot := tmMap[via]
        outbounds[target] = core.NewWireOutbound(ot.name, ot.tm, e)
    case strings.HasPrefix(via, "k2p://"):
        // unchanged
    }
}

// Store for health/reconnect.
var allTMs []*outboundTransport
for _, url := range k2v5URLs {
    allTMs = append(allTMs, tmMap[url])
}
e.outboundTMs = allTMs
if len(allTMs) > 0 {
    e.primaryTM = allTMs[0]
}
```

### Health monitor changes

```go
// Before: single TM
func newHealthMonitor(tm *wire.TransportManager, ...) *healthMonitor

// After: multiple TMs
func newHealthMonitor(tms []*outboundTransport, ...) *healthMonitor
```

**sample() changes:**
- Iterate all TMs, aggregate WireStats (sum TX/RX, worst loss/RTT)
- `tcpRecentlyHealthy()` = any TM has recent TCP success
- Health FSM uses aggregated metrics (same thresholds)

**echoLoop changes:**
- Probe each TM's Primary() sequentially (stagger by 5s to avoid burst)
- Per-TM consecutive failure counter
- TM with 3 consecutive failures → signal re-race for that specific TM (not all)

### Reconnect changes

**doNetworkReconnect / reconnect:**
```go
// Before
e.tm.ResetConnections()

// After
for _, ot := range e.outboundTMs {
    ot.tm.ResetConnections()
}
```

DNS cancel + proxy DNS reset remain unchanged (still one ProxyDNSClient using primaryTM's tcpws).

**reRaceLoop changes:**

Currently one channel triggers re-race for the single TM. Change to per-TM signaling:

```go
// healthMonitor signals which TM needs re-race
type reRaceRequest struct {
    ot *outboundTransport
}

// reRaceLoop receives requests
case req := <-e.reRaceChan:
    // Race only for req.ot, replace only req.ot.tm's primary
```

### DNS transport

```go
// Before
tcpwsDialer = raceResult.TCPWS  // from single race

// After
tcpwsDialer = e.primaryTM.tm    // first k2v5 route's TM
// TCP-WS extracted the same way, just from primaryTM's race result
```

If primaryTM reconnects (network change), its tcpws session resets, DNS auto-reconnects on next query. Same behavior as today.

### Stop() changes

```go
// Before (line 770)
tm.Close()

// After
for _, ot := range e.outboundTMs {
    ot.tm.Close()
}
```

Order unchanged: networkMonitor → cancel → provider → **all TMs** → outbounds → tunnel → ruleEngine.

### Status / logging

`client.Server` field semantics: derived from first k2v5 route (unchanged). Status reports primary server. Per-outbound transport type available via outboundTMs for future UI enhancement but not required now.

Engine start log includes all servers:
```go
slog.Info("engine: start",
    "servers", len(e.outboundTMs),
    "primary", e.primaryTM.wireCfg.Host,
)
```

### WireErrorReporter isolation

Current `ReportWireError` / `ClearWireError` on Engine affect global state. With multiple TMs, a wire error from TM-B should not trigger reconnect of TM-A.

Change: `wireOutbound` error reporting becomes per-TM. Engine tracks error state per `outboundTransport`. Only the failing TM's reconnect is triggered.

```go
// wireOutbound already has a name field — use it to route errors
func (e *Engine) ReportWireErrorForOutbound(name string, err error)
func (e *Engine) ClearWireErrorForOutbound(name string)
```

### Error path cleanup

If any RaceTransport in the parallel group fails, all already-created TMs must be closed before returning error. The pseudocode above handles this.

If engine.Start fails after TMs are created (e.g., provider creation fails), `fail()` must close all TMs. Add to fail():

```go
for _, ot := range e.outboundTMs {
    ot.tm.Close()
}
e.outboundTMs = nil
e.primaryTM = nil
```

## Files changed

| File | Change |
|------|--------|
| `engine/engine.go` | Struct fields, step 3-4 merged into 6.5, Stop(), fail() |
| `engine/health.go` | Multi-TM sample, per-TM echo, per-TM re-race signal |
| `engine/engine.go` (reconnect) | Iterate all TMs in ResetConnections |
| `engine/engine.go` (reRaceLoop) | Per-TM re-race requests |
| `engine/error.go` | Per-outbound WireErrorReporter (or Engine methods) |

**Unchanged:**
- `wire/` — TransportManager, RaceTransport, all wire internals
- `core/` — wireOutbound, tunnel, router, outbound interface
- `daemon/` — engine-internal refactor, daemon unaffected
- `appext/` — same
- `config/` — same
- `provider/` — same

## Testing

- Existing engine tests (single k2v5 route) must pass unchanged — regression guard
- New test: two k2v5 routes with different servers → verify different TMs created
- New test: one TM fails echo → only that TM re-races
- New test: network reconnect → all TMs reset
- New test: Stop() closes all TMs
- Contract tests in wire/ unchanged (they test TransportManager directly)

## Migration

No config changes. No wire protocol changes. No daemon API changes. Pure engine-internal refactor. Single k2v5 route behavior is identical to before (one TM, one outboundTransport).
