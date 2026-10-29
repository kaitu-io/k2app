# Per-Outbound Transport Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Each unique k2v5:// route gets its own TransportManager instead of sharing one.

**Architecture:** Move transport setup (ParseURL → resolve → race → TM) from engine.Start steps 2-4 into step 6.5's per-outbound loop. Health monitor, echo loop, reconnect, and reRaceLoop all iterate over multiple TMs. DNS uses the first k2v5 route's TCP-WS. wire/, core/, daemon/, appext/ are unchanged.

**Tech Stack:** Go, engine/ package only. `golang.org/x/sync/errgroup` for parallel race.

**Spec:** `docs/superpowers/specs/2026-04-08-per-outbound-transport-design.md`

---

### Task 1: Add outboundTransport type and new Engine fields (additive only)

**Files:**
- Modify: `engine/engine.go:39-64` (Engine struct)

**Strategy:** Add new fields alongside old ones. Do NOT remove `e.tm` or `e.wireCfg` yet.
Later tasks migrate each consumer one-by-one. Final task deletes the old fields.
This avoids a 20-site "fix all compilation errors" step.

- [ ] **Step 1: Add outboundTransport struct**

Add after the Engine struct (around line 65):

```go
// outboundTransport holds a TransportManager and its wire config for one k2v5 outbound.
// Engine-internal — not exported.
type outboundTransport struct {
	tm       *wire.TransportManager
	wireCfg  wire.Config
	dialAddr string
	name     string // log label, e.g. "k2v5-jp.example.com"
}
```

- [ ] **Step 2: Add new fields to Engine struct (keep old fields)**

Add these fields to the Engine struct (after line 60, before `reRaceChan`):

```go
outboundTMs []*outboundTransport  // all wire TMs, for health/reconnect
primaryTM   *outboundTransport    // first k2v5 route's TM (DNS transport)
```

**Do NOT remove** `tm`, `wireCfg`. They stay until the final cleanup task.

- [ ] **Step 3: Verify compilation**

Run: `go build ./engine/...`
Expected: PASS (additive only, nothing broken)

- [ ] **Step 4: Verify existing tests pass**

Run: `go test -short -count=1 ./engine/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add engine/engine.go
git commit -m "refactor(engine): add outboundTransport struct and new fields

Additive only — old tm/wireCfg fields preserved. New outboundTMs/primaryTM
fields coexist until migration is complete."
```

---

### Task 2: Extract setupOutboundTransport helper

**Files:**
- Modify: `engine/engine.go`

This task extracts the transport setup logic (currently steps 2-4) into a reusable function that will be called per-outbound in Task 3.

- [ ] **Step 1: Write the helper function**

Add a new function (after `buildOutboundMap`, around line 955):

```go
// setupOutboundTransport creates a TransportManager for a single k2v5:// URL.
// Performs: ParseURL → resolveDialAddr → RaceTransport → TM setup.
// The caller must close the returned outboundTransport on error paths.
func setupOutboundTransport(
	ctx context.Context,
	via string,
	directDialer wire.DirectDialer,
) (*outboundTransport, wire.Dialer, error) {
	wireCfg, err := wire.ParseURL(via)
	if err != nil {
		return nil, nil, fmt.Errorf("parse %s: %w", config.TruncateVia(via), err)
	}

	dialAddr, err := resolveDialAddr(ctx, wireCfg, directDialer)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve %s: %w", wireCfg.Host, err)
	}
	if dialAddr != "" {
		wireCfg.DialAddr = dialAddr
	}

	raceCtx, raceCancel := context.WithTimeout(ctx, 15*time.Second)
	raceResult, err := wire.RaceTransport(raceCtx, wireCfg, wireCfg.DialAddr, directDialer)
	raceCancel()
	if err != nil {
		return nil, nil, fmt.Errorf("transport race %s: %w", wireCfg.Host, err)
	}

	tm := wire.NewTransportManager()
	tm.SetPrimary(raceResult.Dialer, raceResult.TransportType)

	// Ensure TCP-WS is available as fallback and DNS transport.
	var tcpwsDialer wire.Dialer
	if raceResult.TCPWS != nil {
		tm.SetTCPWS(raceResult.TCPWS)
		tcpwsDialer = raceResult.TCPWS
	} else if raceResult.TransportType != "tcpws" {
		tcpwsClient := wire.NewTCPWSClient(wireCfg, nil)
		if directDialer != nil {
			tcpwsClient.SetDirectDialer(directDialer)
		}
		tm.SetTCPWS(tcpwsClient)
		tcpwsDialer = tcpwsClient
	} else {
		tcpwsDialer = raceResult.Dialer
	}

	tm.StartQuicProbe(ctx)

	ot := &outboundTransport{
		tm:       tm,
		wireCfg:  wireCfg,
		dialAddr: dialAddr,
		name:     fmt.Sprintf("k2v5-%s", wireCfg.Host),
	}

	slog.Info("engine: transport ready",
		"outbound", ot.name,
		"transport", raceResult.TransportType,
		"dialAddr", wireCfg.DialAddr,
	)

	return ot, tcpwsDialer, nil
}
```

Returns `(outboundTransport, tcpwsDialer, error)`. The `tcpwsDialer` is needed by DNS setup — only the primary outbound's tcpwsDialer is used for DNS.

- [ ] **Step 2: Verify compilation**

Run: `go build ./engine/...`
Expected: PASS (function exists but not yet called)

- [ ] **Step 3: Commit**

```bash
git add engine/engine.go
git commit -m "refactor(engine): extract setupOutboundTransport helper

Encapsulates ParseURL → resolve → race → TM setup for one k2v5 URL.
Will be called per-outbound in the next task."
```

---

### Task 3: Refactor Start() — per-outbound transport setup

**Files:**
- Modify: `engine/engine.go` (Start method, lines 92-485)

This is the core task. Steps 2, 3, 4 are replaced by a per-outbound loop in step 6.5.

- [ ] **Step 1: Remove old steps 2, 3, 4**

Delete the following blocks from Start():
- Step 2 — `wire.ParseURL(client.Server)` (lines 159-163)
- Step 3 — `resolveDialAddr` (lines 181-196)
- Step 4 — entire block: RaceTransport, TransportManager creation, TCP-WS logic, QuicProbe (lines 198-260)

Keep step 2.5 (NetworkMonitor start, lines 165-179) — it stays where it is.

Also delete the `var tcpwsDialer wire.Dialer` declaration that was at line 236 (it moves into the new loop).

- [ ] **Step 2: Add errgroup import**

Add to imports:

```go
"golang.org/x/sync/errgroup"
```

Run `go get golang.org/x/sync` if not already in go.mod.

- [ ] **Step 3: Replace step 6.5 outbound table building**

Replace the current step 6.5 block (lines 311-330) with:

```go
	// 6.5. Build per-outbound transports and outbound table.
	//
	// Each unique k2v5:// URL gets its own TransportManager with independent
	// transport race. Multiple URLs race in parallel (errgroup).
	// buildOutboundMap already deduplicates by via URL string.
	outbounds := make([]core.Outbound, 2+len(outboundMap))
	outbounds[outboundDirect] = core.NewDirectOutbound(cfg.DirectDialer)
	outbounds[outboundReject] = nil

	// Collect unique k2v5 URLs in config route order (deterministic).
	// IMPORTANT: iterate client.Routes (ordered slice), NOT outboundMap (unordered map).
	// The first k2v5 route becomes primaryTM (DNS transport), so order matters.
	seen := make(map[string]bool)
	var k2v5URLs []string
	for _, route := range client.Routes {
		via := route.Via
		if !strings.HasPrefix(via, "k2v5://") || seen[via] {
			continue
		}
		if _, inMap := outboundMap[via]; !inMap {
			continue // not in outbound map (shouldn't happen, but defensive)
		}
		seen[via] = true
		k2v5URLs = append(k2v5URLs, via)
	}

	// Setup transports in parallel.
	type otResult struct {
		ot            *outboundTransport
		tcpwsDialer  wire.Dialer
		err           error
	}
	otResults := make([]otResult, len(k2v5URLs))

	if len(k2v5URLs) > 0 {
		var g errgroup.Group
		for i, via := range k2v5URLs {
			i, via := i, via
			g.Go(func() error {
				ot, tcpws, err := setupOutboundTransport(ctx, via, cfg.DirectDialer)
				otResults[i] = otResult{ot: ot, tcpwsDialer: tcpws, err: err}
				return nil // errors collected in results, not errgroup
			})
		}
		g.Wait()
	}

	// Check results and clean up on any failure.
	tmMap := make(map[string]*outboundTransport, len(k2v5URLs))
	var allOTs []*outboundTransport
	var primaryOT *outboundTransport
	var tcpwsDialer wire.Dialer // from primary outbound, for DNS

	for i, r := range otResults {
		if r.err != nil {
			// Clean up already-created TMs.
			for _, prev := range otResults[:i] {
				if prev.ot != nil {
					prev.ot.tm.Close()
				}
			}
			if cfg.NetworkMonitor != nil {
				cfg.NetworkMonitor.Close()
			}
			return e.fail(fmt.Errorf("outbound setup: %w", r.err))
		}
		tmMap[k2v5URLs[i]] = r.ot
		allOTs = append(allOTs, r.ot)
		if primaryOT == nil {
			primaryOT = r.ot
			tcpwsDialer = r.tcpwsDialer
		}
	}

	slog.Info("engine: step 6.5 — outbound transports ready",
		"k2v5Outbounds", len(allOTs),
		"elapsed", time.Since(startTime),
	)

	// Build outbound table.
	for via, target := range outboundMap {
		switch {
		case strings.HasPrefix(via, "k2v5://"):
			ot := tmMap[via]
			outbounds[target] = core.NewWireOutbound(ot.name, ot.tm, e)
		case strings.HasPrefix(via, "k2p://"):
			k2pOut, k2pErr := core.NewK2POutbound(via)
			if k2pErr != nil {
				slog.Warn("engine: k2p outbound failed", "err", k2pErr)
				continue
			}
			outbounds[target] = k2pOut
		}
	}
```

- [ ] **Step 4: Update step 9 commit to store outboundTMs**

In the commit block (around line 407-422), **add** new field assignments alongside old ones:

```go
// Keep old fields for now (other methods still reference them):
e.tm = primaryOT.tm                   // bridge: old consumers still use e.tm
e.wireCfg = primaryOT.wireCfg         // bridge: reRaceLoop still uses e.wireCfg

// Add new fields:
e.outboundTMs = allOTs
e.primaryTM = primaryOT
```

Keep `e.directDialer = cfg.DirectDialer`.

The old `tm` local variable from steps 2-4 no longer exists — use `primaryOT.tm` instead. The old `wireCfg` local variable is gone — use `primaryOT.wireCfg`.

- [ ] **Step 5: Update step 8 DNS to use tcpwsDialer from primary**

The `tcpwsDialer` variable is already set from `primaryOT` in the new step 6.5 code above. Verify that step 8 (DNS setup, around line 359) still references `tcpwsDialer` — it should, no changes needed.

- [ ] **Step 6: Keep healthMonitor creation unchanged (temporary)**

The healthMonitor constructor at line 416 still receives `tm` (old field).
Since Task 1 keeps the old `tm` field, and we still set `e.tm = primaryOT.tm`
in step 9 (see step 4), the existing `hm := newHealthMonitor(tm, tunnel, ...)`
works as-is. Task 6 will change the constructor signature to accept outboundTransport.

- [ ] **Step 7: Update engine start log**

Update the engine start log (around line 147) to include outbound count:

```go
slog.Info("engine: start",
	"server", extractServerHost(client.Server),
	"mode", client.Mode,
	// ... existing fields ...
)
```

No change needed — `client.Server` is still derived from first route by SetDefaults.

- [ ] **Step 8: Update error cleanup paths in Start()**

After TMs are created in step 6.5, subsequent failure paths must close all TMs.
There are exactly 3 error paths after step 6.5 in the current code:

1. **ctx.Err() check before tunnel start** (around line 304):
   ```go
   if ctx.Err() != nil {
       if cfg.NetworkMonitor != nil { cfg.NetworkMonitor.Close() }
       for _, ot := range allOTs { ot.tm.Close() }
       return e.fail(ctx.Err())
   }
   ```

2. **tunnel.StartWithHandler failure** (around line 376):
   ```go
   if err := tunnel.StartWithHandler(ctx, dh); err != nil {
       if cfg.NetworkMonitor != nil { cfg.NetworkMonitor.Close() }
       for _, ot := range allOTs { ot.tm.Close() }
       return e.fail(fmt.Errorf("tunnel start: %w", err))
   }
   ```

3. **tunnel.Start failure** (proxy mode, around line 384):
   ```go
   if err := tunnel.Start(ctx); err != nil {
       if cfg.NetworkMonitor != nil { cfg.NetworkMonitor.Close() }
       for _, ot := range allOTs { ot.tm.Close() }
       return e.fail(fmt.Errorf("tunnel start: %w", err))
   }
   ```

Verify: `grep -n 'tm\.Close()' engine/engine.go` within Start() to confirm no other sites exist after step 6.5.

- [ ] **Step 9: Verify compilation**

Run: `go build ./engine/...`
Expected: PASS

- [ ] **Step 10: Run existing tests**

Run: `go test -short -count=1 ./engine/...`
Expected: PASS (single k2v5 route tests work as before)

- [ ] **Step 11: Commit**

```bash
git add engine/engine.go
git commit -m "refactor(engine): per-outbound transport setup in Start()

Each unique k2v5:// URL now gets its own TransportManager via
setupOutboundTransport(). Multiple URLs race in parallel with errgroup.
Steps 2-4 merged into step 6.5. DNS uses primary outbound's TCP-WS.
Single-route behavior unchanged."
```

---

### Task 4: Update Stop() and fail() for multi-TM cleanup

**Files:**
- Modify: `engine/engine.go`

- [ ] **Step 1: Update Stop()**

In Stop() (around lines 727-770), update the TM extraction and close:

```go
// Extract under lock — replace single tm with slice:
e.mu.Lock()
// ... existing extractions ...
outboundTMs := e.outboundTMs
e.outboundTMs = nil
e.primaryTM = nil
// ... rest of extractions ...
e.mu.Unlock()

// Close outside lock — replace single tm.Close():
// ... after prov.Close() ...
for _, ot := range outboundTMs {
	ot.tm.Close()
}
```

- [ ] **Step 2: Update fail()**

In fail() (around lines 841-869), add TM cleanup after the state change:

```go
func (e *Engine) fail(err error) error {
	e.mu.Lock()
	e.lastError = ClassifyError(err)
	// ... existing log ...

	nm := e.networkMonitor
	e.networkMonitor = nil

	// Clean up outbound transports.
	outboundTMs := e.outboundTMs
	e.outboundTMs = nil
	e.primaryTM = nil

	if e.cancel != nil {
		e.cancel()
		e.cancel = nil
	}
	e.setStateLocked(StateDisconnected)
	handler := e.handler
	status := e.buildStatusLocked()
	e.mu.Unlock()

	if handler != nil {
		handler.OnStatus(status)
	}
	if nm != nil {
		nm.Close()
	}
	for _, ot := range outboundTMs {
		ot.tm.Close()
	}
	return err
}
```

- [ ] **Step 3: Verify compilation and tests**

Run: `go build ./engine/... && go test -short -count=1 ./engine/...`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add engine/engine.go
git commit -m "refactor(engine): Stop/fail close all outbound TMs"
```

---

### Task 5: Update reconnect, Pause, Wake for multi-TM

**Files:**
- Modify: `engine/engine.go`

- [ ] **Step 1: Update reconnect()**

In reconnect() (around lines 490-532), replace single tm with iteration.

**Ordering is critical** (shared tcpwsDialer contract, see comment at lines 507-511):
1. `dh.CancelInflight()` — abort pending DNS queries FIRST
2. All TMs `ResetConnections()` — resets smux sessions (including primaryTM's tcpws used by DNS)
3. `pdns.ResetConnections()` — clean proxyConn pool LAST

```go
// Old (line 497):
tm := e.tm

// New:
outboundTMs := e.outboundTMs
```

Then replace the reset call (line 517):

```go
// Old:
tm.ResetConnections()

// New — ordering preserved: CancelInflight (already above) → all TMs → pdns (below)
for _, ot := range outboundTMs {
	ot.tm.ResetConnections()
}
```

Remove the single `tm` variable.

- [ ] **Step 2: Update Pause()**

In Pause() (around lines 625-649), replace single tm:

```go
// Old (line 634):
tm := e.tm

// New:
outboundTMs := e.outboundTMs
```

Then replace reset (lines 640-641):

```go
// Old:
if tm != nil {
	tm.ResetConnections()
}

// New:
for _, ot := range outboundTMs {
	ot.tm.ResetConnections()
}
```

- [ ] **Step 3: Update Wake()**

Same pattern as Pause(), in Wake() (around lines 652-675):

```go
// Old (line 661):
tm := e.tm

// New:
outboundTMs := e.outboundTMs
```

Then replace reset (lines 666-667):

```go
// Old:
if tm != nil {
	tm.ResetConnections()
}

// New:
for _, ot := range outboundTMs {
	ot.tm.ResetConnections()
}
```

- [ ] **Step 4: Verify compilation and tests**

Run: `go build ./engine/... && go test -short -count=1 ./engine/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add engine/engine.go
git commit -m "refactor(engine): reconnect/Pause/Wake iterate all outbound TMs"
```

---

### Task 6: Update healthMonitor for multiple TMs

**Files:**
- Modify: `engine/health.go`

- [ ] **Step 1: Update healthMonitor struct**

Replace the `tm` field (line 108):

```go
// Old:
tm *wire.TransportManager

// New:
primaryTM *outboundTransport   // for health FSM (WireStats, TCPRecentlyHealthy)
tms       []*outboundTransport // for echoLoop (probe each independently)
```

Same package — no import needed.

- [ ] **Step 2: Update constructor**

Change `newHealthMonitor` (line 139) signature:

```go
// Old:
func newHealthMonitor(
	tm *wire.TransportManager,
	tunnel *core.ClientTunnel,
	...
) *healthMonitor

// New:
func newHealthMonitor(
	primaryTM *outboundTransport,
	tms []*outboundTransport,
	tunnel *core.ClientTunnel,
	...
) *healthMonitor
```

Update field assignments inside:

```go
// Old:
tm: tm,

// New:
primaryTM: primaryTM,
tms:       tms,
```

- [ ] **Step 3: Update sample() — use primaryTM for health FSM**

In sample() (around line 195-197), replace single-TM reads with primaryTM:

```go
// Old:
ws := hm.tm.WireStats()
tx, rx := hm.tunnel.TrafficBytes()
tcpHealthy := hm.tm.TCPRecentlyHealthy(30 * time.Second)

// New — use primaryTM for health assessment.
// WHY NOT aggregate across all TMs: RTT/MinRTT are per-server metrics.
// Japan server (30ms RTT) and US server (200ms RTT) are both healthy,
// but aggregating worst-case RTT (200ms) with smallest MinRTT (25ms)
// falsely triggers degradation (200 > 3*25). Per-TM health would require
// a distributed FSM — overkill for max 2 outbounds.
// echoLoop already probes each TM independently for transport failure.
var ws wire.WireStats
var tcpHealthy bool
if hm.primaryTM != nil {
	ws = hm.primaryTM.tm.WireStats()
	tcpHealthy = hm.primaryTM.tm.TCPRecentlyHealthy(30 * time.Second)
}
tx, rx := hm.tunnel.TrafficBytes()
```

If `hm.primaryTM` is nil, `ws` stays zero-valued — same as current behavior with no transport.

- [ ] **Step 4: Update echoLoop() — probe each TM**

Replace the current echoLoop (lines 409-448) with per-TM probing:

```go
func (hm *healthMonitor) echoLoop() {
	defer hm.wg.Done()
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	// Per-TM consecutive failure counters.
	echoFails := make(map[*outboundTransport]*atomic.Int32)
	for _, ot := range hm.tms {
		echoFails[ot] = &atomic.Int32{}
	}

	for {
		select {
		case <-hm.done:
			return
		case <-ticker.C:
		}

		for _, ot := range hm.tms {
			primary := ot.tm.Primary()
			if primary == nil {
				continue
			}

			ep, ok := primary.(wire.EchoProber)
			if !ok {
				echoFails[ot].Store(0)
				continue
			}

			echoCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			err := ep.RunEchoProbe(echoCtx)
			cancel()

			if err != nil {
				fails := echoFails[ot].Add(1)
				if fails >= 3 && hm.reRaceChan != nil {
					select {
					case hm.reRaceChan <- ot:
					default:
					}
					echoFails[ot].Store(0)
				}
			} else {
				echoFails[ot].Store(0)
			}
		}
	}
}
```

**Important:** The `reRaceChan` type changes from `chan struct{}` to `chan *outboundTransport` — this carries which TM needs re-race. Update this in the healthMonitor struct (line 136):

```go
// Old:
reRaceChan chan struct{}

// New:
reRaceChan chan *outboundTransport
```

Also update the `echoFails` field — remove the single `echoFails atomic.Int32` (line 135) since we now use per-TM counters local to echoLoop.

- [ ] **Step 5: Update caller in engine.go**

In engine.go, update the healthMonitor creation (around line 416):

```go
// Old (temporary from Task 3):
var hmTM *wire.TransportManager
if primaryOT != nil {
	hmTM = primaryOT.tm
}
hm := newHealthMonitor(hmTM, tunnel, ...)

// New:
hm := newHealthMonitor(primaryOT, allOTs, tunnel, e.broadcastSnapshot, e.doNetworkReconnect, time.Now())
```

Update reRaceChan type in engine.go (around line 425):

```go
// Old:
e.reRaceChan = make(chan struct{}, 1)

// New:
e.reRaceChan = make(chan *outboundTransport, 1)
```

Also update the Engine struct field (line 62):

```go
// Old:
reRaceChan   chan struct{}

// New:
reRaceChan chan *outboundTransport
```

- [ ] **Step 6: Update health_test.go**

All tests that call `mockTransportManager()` + `newHealthMonitor(tm, ...)` need updating:

```go
// Old pattern:
tm := mockTransportManager()
hm := newHealthMonitor(tm, tunnel, onSnapshot, onReconnect, time.Now())

// New pattern:
tm := mockTransportManager()
ot := &outboundTransport{tm: tm, name: "test"}
hm := newHealthMonitor(ot, []*outboundTransport{ot}, tunnel, onSnapshot, onReconnect, time.Now())
```

Apply this to every test in health_test.go that creates a healthMonitor. There are approximately 15 test functions that need this change — search for `newHealthMonitor(tm,`.

Also update deadlock_test.go (lines 29, 68, 108) — same pattern.

- [ ] **Step 7: Verify compilation and tests**

Run: `go build ./engine/... && go test -short -count=1 ./engine/...`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add engine/health.go engine/engine.go engine/health_test.go engine/deadlock_test.go
git commit -m "refactor(engine): healthMonitor supports multiple outbound TMs

sample() aggregates worst-case loss/RTT across all TMs.
echoLoop probes each TM independently with per-TM failure counters.
reRaceChan now carries the specific outboundTransport that needs re-race."
```

---

### Task 7: Update reRaceLoop for per-TM re-race

**Files:**
- Modify: `engine/engine.go` (reRaceLoop, around line 1141)

- [ ] **Step 1: Update reRaceLoop to use per-TM requests**

Replace the function (lines 1141-1189):

```go
func (e *Engine) reRaceLoop(ctx context.Context) {
	for {
		var target *outboundTransport
		select {
		case <-ctx.Done():
			return
		case target = <-e.reRaceChan:
		}

		e.mu.Lock()
		if e.state != StateConnected {
			e.mu.Unlock()
			continue
		}
		dd := e.directDialer
		e.mu.Unlock()

		if target == nil || target.tm == nil {
			continue
		}

		slog.Warn("DIAG: transport-rerace",
			"reason", "echo-consecutive-fail",
			"outbound", target.name,
			"previousTransport", target.tm.PrimaryType(),
		)

		raceCtx, raceCancel := context.WithTimeout(ctx, 15*time.Second)
		result, err := wire.RaceTransport(raceCtx, target.wireCfg, target.wireCfg.DialAddr, dd)
		raceCancel()

		if err != nil {
			slog.Warn("engine: re-race failed", "outbound", target.name, "err", err)
			continue
		}

		if ctx.Err() != nil {
			result.Dialer.Close()
			return
		}

		// Verify the target TM still exists in the engine.
		e.mu.Lock()
		found := false
		for _, ot := range e.outboundTMs {
			if ot == target {
				found = true
				break
			}
		}
		e.mu.Unlock()

		if !found {
			result.Dialer.Close()
			continue
		}

		target.tm.ReplacePrimary(result.Dialer, result.TransportType)
	}
}
```

Key changes:
- Receives `*outboundTransport` from channel (not empty struct)
- Uses target's wireCfg for race (not global e.wireCfg which no longer exists)
- Verifies target still exists in engine before replacing

- [ ] **Step 2: Verify compilation and tests**

Run: `go build ./engine/... && go test -short -count=1 ./engine/...`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add engine/engine.go
git commit -m "refactor(engine): reRaceLoop handles per-outbound re-race

Receives specific outboundTransport from echoLoop, races only that TM's
transport, verifies it still exists before replacing."
```

---

### Task 8: Add multi-outbound tests

**Files:**
- Modify: `engine/engine_test.go`

- [ ] **Step 1: Test buildOutboundMap with multiple k2v5 URLs**

```go
func TestBuildOutboundMap_MultipleK2V5(t *testing.T) {
	routes := []config.RouteConfig{
		{Via: "k2v5://jp-server:443", Match: rule.MatchConfig{DomainSuffix: []string{".jp"}}},
		{Via: "k2v5://us-server:443", Match: rule.MatchConfig{All: true}},
	}
	m := buildOutboundMap(routes)

	if len(m) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(m))
	}
	jpTarget := m["k2v5://jp-server:443"]
	usTarget := m["k2v5://us-server:443"]
	if jpTarget == usTarget {
		t.Fatal("jp and us should have different targets")
	}
	if jpTarget < 2 || usTarget < 2 {
		t.Fatal("wire targets should be >= 2")
	}
}
```

- [ ] **Step 2: Test buildOutboundMap deduplication**

```go
func TestBuildOutboundMap_DedupSameURL(t *testing.T) {
	routes := []config.RouteConfig{
		{Via: "k2v5://server:443", Match: rule.MatchConfig{DomainSuffix: []string{".jp"}}},
		{Via: "k2v5://server:443", Match: rule.MatchConfig{All: true}},
	}
	m := buildOutboundMap(routes)

	if len(m) != 1 {
		t.Fatalf("expected 1 entry (dedup), got %d", len(m))
	}
}
```

- [ ] **Step 3: Test setupOutboundTransport with invalid URL**

```go
func TestSetupOutboundTransport_InvalidURL(t *testing.T) {
	ctx := context.Background()
	_, _, err := setupOutboundTransport(ctx, "not-a-valid-url", nil)
	if err == nil {
		t.Fatal("expected error for invalid URL")
	}
}
```

- [ ] **Step 4: Run tests**

Run: `go test -short -count=1 ./engine/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add engine/engine_test.go
git commit -m "test(engine): add multi-outbound and setup transport tests"
```

---

### Task 9: Update CLAUDE.md documentation

**Files:**
- Modify: `CLAUDE.md`
- Modify: `engine/CLAUDE.md`

- [ ] **Step 1: Update engine CLAUDE.md architecture diagram**

In `engine/CLAUDE.md`, update the Start() flow to reflect the new step ordering:

```
├── 1. k2rule.Init (routing rules)
├── 2.5. Start NetworkMonitor (DirectDialer needs DefaultInterface)
├── 5. Router
├── 6. Provider
├── 6.5. Per-outbound transport setup:
│      for each unique k2v5:// → ParseURL → resolve → RaceTransport → TM
│      (parallel via errgroup)
├── 7. ClientTunnel
├── 8. DNS middleware (TUN/TProxy mode only)
│      DNS proxy dialer = primaryTM's TCP-WS (control-plane)
├── 9. Commit → StateConnected + start reRaceLoop
├── 10. HealthMonitor start (1Hz wire stats + health FSM + echoLoop)
│       sample() uses primaryTM for health FSM
│       echoLoop probes each TM independently
└── 11. Background QUIC probe (per-TM, active during TCP-WS fallback)
```

- [ ] **Step 2: Update CLAUDE.md Lock Ordering Graph if needed**

No new mutexes added — `outboundTransport` has no mutex. No changes needed.

- [ ] **Step 3: Update CLAUDE.md Status section**

Add to the Done section: "Per-outbound transport (each k2v5 route gets independent TransportManager)"

- [ ] **Step 4: Commit**

```bash
git add CLAUDE.md engine/CLAUDE.md
git commit -m "docs: update architecture for per-outbound transport"
```

---

### Task 10: Remove deprecated tm/wireCfg fields

**Files:**
- Modify: `engine/engine.go`

Now that all consumers use outboundTMs/primaryTM, remove the old bridge fields.

- [ ] **Step 1: Remove old fields from Engine struct**

Remove from Engine struct:
```go
tm             *wire.TransportManager  // line 50 — REMOVE
wireCfg      wire.Config               // line 60 — REMOVE
```

- [ ] **Step 2: Remove bridge assignments in Start() step 9**

Remove from the commit block:
```go
e.tm = primaryOT.tm              // REMOVE — was bridge
e.wireCfg = primaryOT.wireCfg    // REMOVE — was bridge
```

- [ ] **Step 3: Remove old cleanup in Stop()**

Remove from Stop():
```go
tm := e.tm; e.tm = nil           // REMOVE — replaced by outboundTMs loop
```
And the corresponding `tm.Close()` call.

- [ ] **Step 4: Fix any remaining compilation errors**

Run `go build ./engine/...`. If any reference to `e.tm` or `e.wireCfg` remains, it's a migration bug from earlier tasks. Fix each one by routing through `e.outboundTMs` or `e.primaryTM`.

Expected: zero remaining references (all migrated in Tasks 3-7).

- [ ] **Step 5: Verify tests**

Run: `go test -short -count=1 ./engine/...`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add engine/engine.go
git commit -m "refactor(engine): remove deprecated tm/wireCfg fields

All consumers now use outboundTMs/primaryTM. Bridge fields removed."
```

---

### Task 11: Final verification

**Files:** None (testing only)

- [ ] **Step 1: Run full test suite**

```bash
go test -short -count=1 ./...
```

Expected: ALL PASS

- [ ] **Step 2: Run engine tests with race detector**

```bash
go test -race -count=1 -timeout 120s ./engine/...
```

Expected: PASS (no races)

- [ ] **Step 3: Build all binaries**

```bash
go build -o /dev/null ./cmd/k2
go build -o /dev/null ./cmd/k2s
```

Expected: PASS

- [ ] **Step 4: Run quick-check**

```bash
make quick-check
```

Expected: PASS
