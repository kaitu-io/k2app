# iOS NE Memory Diagnostics & DNS Concurrency Guards

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add memory metrics to heartbeat DIAG for iOS NE jetsam diagnosis, and guard DNS concurrency to prevent memory spikes on network recovery.

**Architecture:** Three changes: (1) `runtime.ReadMemStats` in heartbeat DIAG log, (2) epoch-based cancellation for in-flight DNS queries on network change, (3) semaphore to cap concurrent upstream DNS queries. All changes are in k2 submodule (Go).

**Tech Stack:** Go, `runtime.ReadMemStats`, `context.AfterFunc` (Go 1.21+), buffered channel semaphore

**Background:** iOS NE was killed by iOS 5 times in ~2 hours (no stopTunnel = jetsam kill). Kill events 2 & 3 (25s/19s lifetime) correlated with DNS query storms on network recovery — 50+ domains × 2 racing servers = 100+ concurrent goroutines + UDP sockets. No memory data exists in logs to confirm.

---

### Task 1: Heartbeat memory metrics

**Files:**
- Modify: `k2/engine/health.go:1-10` (import), `k2/engine/health.go:202-220` (heartbeat block)

- [ ] **Step 1: Add runtime import and memory fields to heartbeat**

In `k2/engine/health.go`, add `"runtime"` to imports. In the heartbeat block (line 204, inside `if hm.diagCounter >= 30`), add `runtime.ReadMemStats` call and 3 new fields:

```go
// After hm.diagCounter = 0 (line 205), before slog.Info:
var ms runtime.MemStats
runtime.ReadMemStats(&ms)
tcp, udp := hm.tunnel.ActiveConns()
slog.Info("DIAG: heartbeat",
    "health", hm.health,
    "transport", ws.Transport,
    "loss", fmt.Sprintf("%.3f", hm.smoothWireLoss),
    "rttMs", fmt.Sprintf("%.0f", float64(ws.RTT)/float64(time.Millisecond)),
    "minRttMs", fmt.Sprintf("%.0f", float64(ws.MinRTT)/float64(time.Millisecond)),
    "txMB", fmt.Sprintf("%.1f", float64(tx)/(1024*1024)),
    "rxMB", fmt.Sprintf("%.1f", float64(rx)/(1024*1024)),
    "tcpConns", tcp,
    "udpConns", udp,
    "uptimeS", int(time.Since(hm.connectedAt).Seconds()),
    "fallback", ws.WireFallback,
    "heapMB", fmt.Sprintf("%.1f", float64(ms.HeapInuse)/(1024*1024)),
    "sysMB", fmt.Sprintf("%.1f", float64(ms.Sys)/(1024*1024)),
    "goroutines", runtime.NumGoroutine(),
)
```

Note: `ReadMemStats` runs every 30s (same as heartbeat), not every 1s sample. STW cost is negligible at this frequency.

- [ ] **Step 2: Run tests**

Run: `cd k2 && go test ./engine/... -short -count=1`
Expected: All existing tests pass (heartbeat is log output, no assertion changes needed).

- [ ] **Step 3: Update DIAG heartbeat documentation**

In `k2/CLAUDE.md`, update the Layer 1 heartbeat field list to include `heapMB`, `sysMB`, `goroutines`.

In `k2/engine/CLAUDE.md`, update the health.go description to mention memory metrics.

- [ ] **Step 4: Update quick-diag script**

In `scripts/k2-quick-diag.sh`, update the heartbeat grep/display to show the new memory fields if present. Add a memory trend check: if heapMB > 30 in any heartbeat, flag as WARN.

- [ ] **Step 5: Commit**

```bash
git add k2/engine/health.go k2/CLAUDE.md k2/engine/CLAUDE.md scripts/k2-quick-diag.sh
git commit -m "feat(engine): add memory metrics to DIAG heartbeat (heapMB, sysMB, goroutines)"
```

---

### Task 2: DNS CancelInflight — epoch-based upstream query cancellation

**Files:**
- Modify: `k2/core/dns/middleware.go:44-97` (struct + constructor), `k2/core/dns/middleware.go:331-375` (queryUpstream)
- Test: `k2/core/dns/middleware_test.go`

- [ ] **Step 1: Write the failing test**

In `k2/core/dns/middleware_test.go`, add a test that verifies CancelInflight aborts pending queries:

```go
func TestCancelInflight(t *testing.T) {
	// Slow upstream that blocks until context cancelled.
	slowDNS := &slowQuerier{delay: 10 * time.Second}
	ctx := context.Background()
	mw := NewMiddleware(MiddlewareConfig{
		DirectDNS: slowDNS,
		DNSCache:  NewDNSCache(DNSCacheConfig{}),
		Filter:    NewFilter(FilterConfig{}),
		EngineCtx: ctx,
	})

	// Start a query in background.
	errCh := make(chan error, 1)
	go func() {
		query := new(dns.Msg)
		query.SetQuestion("example.com.", dns.TypeA)
		packed, _ := query.Pack()
		_, err := mw.HandleQuery(context.Background(), packed)
		errCh <- err
	}()

	// Wait for query to reach upstream.
	time.Sleep(50 * time.Millisecond)

	// Cancel all inflight.
	mw.CancelInflight()

	// Query should return quickly with error.
	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("expected error after CancelInflight")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("query did not return after CancelInflight")
	}
}

// slowQuerier is a DNSQuerier that blocks for a configurable duration.
type slowQuerier struct {
	delay time.Duration
}

func (s *slowQuerier) Query(ctx context.Context, query *dns.Msg) (*dns.Msg, error) {
	select {
	case <-time.After(s.delay):
		resp := new(dns.Msg)
		resp.SetReply(query)
		return resp, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd k2 && go test ./core/dns/... -run TestCancelInflight -v -count=1`
Expected: Compile error — `CancelInflight` not defined.

- [ ] **Step 3: Implement CancelInflight**

In `k2/core/dns/middleware.go`:

Add fields to `Middleware` struct:
```go
type Middleware struct {
	// ... existing fields ...

	// Epoch-based cancellation for in-flight upstream queries.
	// CancelInflight() cancels epochCtx, causing all pending upstream queries
	// to abort immediately (stale sockets released). A new epoch starts.
	epochMu     sync.Mutex
	epochCtx    context.Context
	epochCancel context.CancelFunc
}
```

Update `NewMiddleware` to initialize epoch context:
```go
func NewMiddleware(cfg MiddlewareConfig) *Middleware {
	m := &Middleware{
		directDNS: cfg.DirectDNS,
		proxyDNS:  cfg.ProxyDNS,
		dnsCache:  cfg.DNSCache,
		hosts:     cfg.Hosts,
		filter:    cfg.Filter,
		metrics:   cfg.Metrics,
		writer:    cfg.Writer,
		engineCtx: cfg.EngineCtx,
	}
	if cfg.EngineCtx != nil {
		m.epochCtx, m.epochCancel = context.WithCancel(cfg.EngineCtx)
	}
	return m
}
```

Add `CancelInflight` method:
```go
// CancelInflight cancels all pending upstream DNS queries and starts a new epoch.
// Called on network change — stale queries hold old UDP sockets that will never
// receive responses. Cancelling them releases memory immediately instead of
// waiting for 5-15s timeouts.
func (m *Middleware) CancelInflight() {
	m.epochMu.Lock()
	defer m.epochMu.Unlock()
	if m.epochCancel != nil {
		m.epochCancel()
	}
	if m.engineCtx != nil {
		m.epochCtx, m.epochCancel = context.WithCancel(m.engineCtx)
	}
}
```

Update `queryUpstream` to use epoch context:
```go
func (m *Middleware) queryUpstream(ctx context.Context, domain string, action k2rule.Target, query *dns.Msg) (*dns.Msg, error) {
	// Derive from epoch context — CancelInflight() aborts all pending queries.
	m.epochMu.Lock()
	epochCtx := m.epochCtx
	m.epochMu.Unlock()

	if epochCtx != nil {
		qctx, qcancel := context.WithTimeout(epochCtx, upstreamTimeout)
		defer qcancel()
		// Also cancel if caller's context is done (engine shutdown).
		stop := context.AfterFunc(ctx, qcancel)
		defer stop()
		ctx = qctx
	} else {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, upstreamTimeout)
		defer cancel()
	}

	start := time.Now()
	// ... rest of switch statement unchanged ...
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd k2 && go test ./core/dns/... -run TestCancelInflight -v -count=1`
Expected: PASS

- [ ] **Step 5: Run all DNS tests**

Run: `cd k2 && go test ./core/dns/... -short -race -count=1`
Expected: All pass.

- [ ] **Step 6: Commit**

```bash
git add k2/core/dns/middleware.go k2/core/dns/middleware_test.go
git commit -m "feat(dns): add CancelInflight for epoch-based upstream query cancellation"
```

---

### Task 3: DNS global concurrency limiter

**Files:**
- Modify: `k2/core/dns/middleware.go:44-97` (struct + constructor), `k2/core/dns/middleware.go:331-375` (queryUpstream)
- Test: `k2/core/dns/middleware_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestQueryUpstreamConcurrencyLimit(t *testing.T) {
	// Track concurrent queries.
	var concurrent atomic.Int32
	var maxSeen atomic.Int32

	trackingDNS := &trackingQuerier{
		fn: func(ctx context.Context, query *dns.Msg) (*dns.Msg, error) {
			n := concurrent.Add(1)
			defer concurrent.Add(-1)
			for {
				old := maxSeen.Load()
				if n <= old || maxSeen.CompareAndSwap(old, n) {
					break
				}
			}
			time.Sleep(100 * time.Millisecond)
			resp := new(dns.Msg)
			resp.SetReply(query)
			resp.Answer = append(resp.Answer, &dns.A{
				Hdr: dns.RR_Header{Name: query.Question[0].Name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 60},
				A:   net.ParseIP("1.2.3.4"),
			})
			return resp, nil
		},
	}

	mw := NewMiddleware(MiddlewareConfig{
		DirectDNS: trackingDNS,
		DNSCache:  NewDNSCache(DNSCacheConfig{}),
		Filter:    NewFilter(FilterConfig{}),
		EngineCtx: context.Background(),
	})

	// Launch 20 queries for different domains concurrently.
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			query := new(dns.Msg)
			query.SetQuestion(fmt.Sprintf("domain%d.com.", i), dns.TypeA)
			packed, _ := query.Pack()
			mw.HandleQuery(context.Background(), packed)
		}(i)
	}
	wg.Wait()

	if max := maxSeen.Load(); max > int32(maxConcurrentUpstream) {
		t.Errorf("max concurrent upstream queries = %d, want <= %d", max, maxConcurrentUpstream)
	}
}

type trackingQuerier struct {
	fn func(context.Context, *dns.Msg) (*dns.Msg, error)
}

func (t *trackingQuerier) Query(ctx context.Context, query *dns.Msg) (*dns.Msg, error) {
	return t.fn(ctx, query)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd k2 && go test ./core/dns/... -run TestQueryUpstreamConcurrencyLimit -v -count=1`
Expected: FAIL — `maxConcurrentUpstream` not defined, or max exceeds limit.

- [ ] **Step 3: Implement the semaphore**

In `k2/core/dns/middleware.go`, add constant and field:

```go
const maxConcurrentUpstream = 8
```

Add to `Middleware` struct:
```go
type Middleware struct {
	// ... existing fields ...

	// querySem limits concurrent upstream DNS queries.
	// Prevents memory spikes when 50+ domains query simultaneously
	// (e.g., after network recovery on iOS).
	querySem chan struct{}
}
```

Update `NewMiddleware`:
```go
func NewMiddleware(cfg MiddlewareConfig) *Middleware {
	m := &Middleware{
		// ... existing fields ...
		querySem: make(chan struct{}, maxConcurrentUpstream),
	}
	// ... epoch init ...
	return m
}
```

Add semaphore acquire/release in `queryUpstream`, at the top (after epoch context setup, before the switch):
```go
// Limit concurrent upstream queries — prevents memory spikes from
// burst DNS (e.g., 50+ domains after iOS network recovery).
select {
case m.querySem <- struct{}{}:
	defer func() { <-m.querySem }()
case <-ctx.Done():
	return nil, ctx.Err()
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd k2 && go test ./core/dns/... -run TestQueryUpstreamConcurrencyLimit -v -count=1`
Expected: PASS

- [ ] **Step 5: Run all DNS tests**

Run: `cd k2 && go test ./core/dns/... -short -race -count=1`
Expected: All pass.

- [ ] **Step 6: Commit**

```bash
git add k2/core/dns/middleware.go k2/core/dns/middleware_test.go
git commit -m "feat(dns): add global concurrency limiter for upstream queries (max 8)"
```

---

### Task 4: Wire CancelInflight into engine reconnect

**Files:**
- Modify: `k2/engine/dns_handler.go` (add proxy method)
- Modify: `k2/engine/engine.go:381-426` (reconnect method)

- [ ] **Step 1: Add CancelInflight proxy on dnsHandler**

In `k2/engine/dns_handler.go`, add:

```go
// CancelInflight cancels all pending upstream DNS queries.
// Called on network change — stale queries hold old sockets.
func (h *dnsHandler) CancelInflight() {
	if h.dns != nil {
		h.dns.CancelInflight()
	}
}
```

- [ ] **Step 2: Call CancelInflight in engine reconnect**

In `k2/engine/engine.go`, in the `reconnect()` method, after extracting `pdns` (line 393) but before resetting connections (line 401), add `dh` extraction and call:

```go
func (e *Engine) reconnect() bool {
	e.mu.Lock()
	if e.state != StateConnected || e.paused {
		e.mu.Unlock()
		return false
	}
	handler := e.handler
	tm := e.tm
	pdns := e.proxyDNS
	dh := e.dnsH
	e.mu.Unlock()

	// ... existing handler.OnStatus ...

	// Cancel pending DNS queries — they hold stale UDP sockets that
	// will never receive responses from the old network path.
	if dh != nil {
		dh.CancelInflight()
	}

	// Reset cached connections — next dial will create fresh ones.
	if tm != nil {
		tm.ResetConnections()
	}
	// ... rest unchanged ...
```

- [ ] **Step 3: Run engine tests**

Run: `cd k2 && go test ./engine/... -short -race -count=1`
Expected: All pass.

- [ ] **Step 4: Run full test suite**

Run: `cd k2 && go test -short -race -count=1 ./...`
Expected: All pass.

- [ ] **Step 5: Commit**

```bash
git add k2/engine/dns_handler.go k2/engine/engine.go
git commit -m "feat(engine): cancel in-flight DNS queries on network reconnect"
```

---

### Task 5: Update CLAUDE.md conventions

**Files:**
- Modify: `k2/CLAUDE.md` (heartbeat fields)
- Modify: `k2/core/dns/CLAUDE.md` (concurrency limit, CancelInflight)

- [ ] **Step 1: Update k2/CLAUDE.md**

In the Layer 1 heartbeat required fields, add `heapMB`, `sysMB`, `goroutines`.

Add a new DIAG event is NOT needed — these are fields on the existing heartbeat, not new events.

- [ ] **Step 2: Update k2/core/dns/CLAUDE.md**

Add to the middleware description:
- `CancelInflight()` — epoch-based cancellation of pending upstream queries (called by engine on network reconnect)
- `maxConcurrentUpstream = 8` — semaphore limits concurrent upstream queries to prevent memory spikes

- [ ] **Step 3: Commit**

```bash
git add k2/CLAUDE.md k2/core/dns/CLAUDE.md
git commit -m "docs: update CLAUDE.md with memory metrics and DNS concurrency guards"
```
