# DIAG Three-Layer Diagnostic Logging System

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a three-layer diagnostic logging system (`DIAG:`) to k2 that enables quick problem identification during real-world network testing, and codify the logging rules as a constitutional section in `k2/CLAUDE.md`.

**Architecture:** Layer 1 is a 30-second periodic heartbeat log in `healthMonitor`. Layer 2 adds event-driven `DIAG:` logs at key decision points (DNS slow/fail, proxy dial slow/fail, QUIC handshake fail, transport switch). Layer 3 is existing DEBUG logs (already comprehensive, no changes needed). All DIAG logs use the `DIAG:` prefix at INFO/WARN level, making them `grep`-able.

**Tech Stack:** Go `log/slog` (stdlib), atomic counters, no new dependencies.

---

### Task 1: Logging Constitution in k2/CLAUDE.md

**Files:**
- Modify: `k2/CLAUDE.md` (add new section after "Code Style")

**Step 1: Add the "Diagnostic Logging Constitution" section to CLAUDE.md**

Insert this section after "## Code Style" and before "## Deadlock detection":

```markdown
## Diagnostic Logging Constitution (never violate)

All diagnostic logs use `log/slog` (stdlib). The `DIAG:` prefix is reserved for operational diagnostic logs that enable field troubleshooting without DEBUG mode.

### Three Layers

| Layer | Level | Trigger | Purpose | grep |
|-------|-------|---------|---------|------|
| 1. Heartbeat | INFO | Every 30s (healthMonitor) | "Is the tunnel alive and healthy?" | `grep "DIAG: heartbeat"` |
| 2. Events | INFO/WARN | On anomaly (threshold-based) | "What went wrong?" | `grep "DIAG:" \| grep -v heartbeat` |
| 3. Full trace | DEBUG | All operations (existing 471 logs) | "Exactly what happened?" | Switch level at runtime |

### Layer 1 Rules — Heartbeat

- Emitted every 30 seconds from `healthMonitor.sample()`.
- Fixed prefix: `"DIAG: heartbeat"`.
- Required fields: `health`, `transport`, `loss` (smoothed), `rtt`, `minRtt`, `tx`, `rx`, `tcpConns`, `udpConns`, `uptime`, `fallback`.
- Human-readable sizes (MB) and durations (ms/s).
- MUST NOT log at 1Hz — only every 30 samples.

### Layer 2 Rules — Events

Every Layer 2 log MUST:
1. Use `"DIAG: <event-name>"` as the message (e.g. `"DIAG: dns-slow"`, `"DIAG: proxy-dial-fail"`).
2. Include context fields that answer "where" and "why" (domain, addr, transport, error, latency).
3. Use INFO for performance observations, WARN for failures.
4. Be threshold-gated — do NOT log every slow query, only above threshold.

Reserved event names:

| Event | Level | Threshold | Fields |
|-------|-------|-----------|--------|
| `DIAG: dns-slow` | INFO | latency > 500ms | domain, upstream, latencyMs |
| `DIAG: dns-fail` | WARN | on error | domain, upstream, err |
| `DIAG: proxy-dial-fail` | WARN | on error | dest, transport, err |
| `DIAG: proxy-dial-slow` | INFO | latency > 3s | dest, transport, latencyMs |
| `DIAG: quic-handshake-fail` | WARN | on handshake error | addr, consecutiveFails, err |
| `DIAG: transport-switch` | WARN | on QUIC↔TCP-WS transition | from, to, reason |
| `DIAG: udp-relay-timeout` | WARN | consecutive > 2 | consecutiveCount, threshold |
| `DIAG: conn-highwater` | INFO | active > 100 | tcpCount, udpCount |
| `DIAG: connected` | INFO | engine connected | server, mode, dialMs |
| `DIAG: session-end` | INFO | engine stop | uptime, totalTx, totalRx |

### Layer 3 Rules — Full Trace

- Switch at runtime: `curl -X POST http://127.0.0.1:1778/api/log-level -d '{"level":"debug"}'`
- No changes to existing DEBUG logs. They are already comprehensive.
- When adding new code, use DEBUG for per-operation details, DIAG for threshold-gated observations.

### grep Playbook

```bash
# Quick health check (is it working?)
grep "DIAG: heartbeat" k2.log | tail -20

# Find problems (what went wrong?)
grep "DIAG:" k2.log | grep -v heartbeat

# DNS layer issues
grep "DIAG: dns" k2.log

# Transport layer issues
grep "DIAG: quic\|DIAG: transport" k2.log

# Session summary
grep "DIAG: connected\|DIAG: session-end" k2.log

# All WARN-level diagnostics
grep "level=WARN" k2.log | grep "DIAG:"
```

### Adding New DIAG Events

1. Pick a descriptive event name (kebab-case, 2-3 words).
2. Add to the table above in this section.
3. Gate with a threshold — never log on every occurrence.
4. Include "where" + "why" fields.
5. Use INFO for observations, WARN for failures.
```

**Step 2: Verify the edit renders correctly**

Run: `head -200 k2/CLAUDE.md`
Expected: New section visible after "Code Style"

**Step 3: Commit**

```bash
cd k2 && git add CLAUDE.md && git commit -m "docs: add Diagnostic Logging Constitution to CLAUDE.md"
```

---

### Task 2: Connection Counter Infrastructure in ClientTunnel

**Files:**
- Modify: `k2/core/tunnel.go:80-98` (add atomic counters to ClientTunnel struct)
- Modify: `k2/core/tunnel.go:180-190` (increment TCP counter in HandleTCP)
- Modify: `k2/core/tunnel.go:277-290` (increment UDP counter in HandleUDP)
- Test: `k2/core/tunnel_test.go` (add test for ActiveConns)

**Step 1: Write the failing test**

Add to `k2/core/tunnel_test.go`:

```go
func TestActiveConns(t *testing.T) {
	tunnel := &ClientTunnel{}
	tcp, udp := tunnel.ActiveConns()
	if tcp != 0 || udp != 0 {
		t.Fatalf("expected 0/0, got %d/%d", tcp, udp)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd k2 && go test -run TestActiveConns ./core/...`
Expected: FAIL — `ActiveConns` not defined

**Step 3: Add atomic counters and ActiveConns method**

In `k2/core/tunnel.go`, add to the ClientTunnel struct (after `rxBytes` line 97):

```go
	activeTCP atomic.Int64 // currently active TCP handler goroutines
	activeUDP atomic.Int64 // currently active UDP handler goroutines
```

Add the ActiveConns method:

```go
// ActiveConns returns the number of currently active TCP and UDP handler goroutines.
func (t *ClientTunnel) ActiveConns() (tcp, udp int64) {
	return t.activeTCP.Load(), t.activeUDP.Load()
}
```

In `HandleTCP` (after `t.wg.Add(1)` at line 187), add:

```go
	t.activeTCP.Add(1)
	defer t.activeTCP.Add(-1)
```

In `HandleUDP` (after `t.wg.Add(1)` at line 284), add:

```go
	t.activeUDP.Add(1)
	defer t.activeUDP.Add(-1)
```

**Step 4: Run test to verify it passes**

Run: `cd k2 && go test -run TestActiveConns ./core/...`
Expected: PASS

**Step 5: Run full core tests**

Run: `cd k2 && go test ./core/...`
Expected: All pass

**Step 6: Commit**

```bash
cd k2 && git add core/tunnel.go core/tunnel_test.go && git commit -m "feat(core): add ActiveConns counter for DIAG heartbeat"
```

---

### Task 3: Layer 1 — Heartbeat in healthMonitor

**Files:**
- Modify: `k2/engine/health.go:54-78` (add diagInterval counter and tunnel ref for conn counts)
- Modify: `k2/engine/health.go:129-187` (emit heartbeat every 30 samples)
- Modify: `k2/engine/engine.go:332` (pass connectedAt to healthMonitor)
- Test: `k2/engine/health_test.go` (add heartbeat test)

**Step 1: Write the failing test**

Add to `k2/engine/health_test.go`:

```go
func TestHealthMonitorHeartbeat(t *testing.T) {
	// Verify that after 30 samples, a DIAG heartbeat is logged.
	// Use a log handler that captures output.
	var buf bytes.Buffer
	handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	slog.SetDefault(slog.New(handler))
	defer slog.SetDefault(slog.Default())

	tm := wire.NewTransportManager()
	tunnel := core.NewClientTunnel(core.ClientTunnelConfig{
		Dialer: tm,
	})

	hm := newHealthMonitor(tm, tunnel, nil, func() {})
	hm.connectedAt = time.Now().Add(-60 * time.Second)

	// Run 30 samples
	for i := 0; i < 30; i++ {
		hm.sample()
	}

	if !strings.Contains(buf.String(), "DIAG: heartbeat") {
		t.Fatalf("expected DIAG heartbeat after 30 samples, got: %s", buf.String())
	}
}
```

Note: The exact test may need adaptation based on exported/unexported access. The key pattern is: 30 calls to `sample()` should produce exactly one `DIAG: heartbeat` log.

**Step 2: Run test to verify it fails**

Run: `cd k2 && go test -run TestHealthMonitorHeartbeat ./engine/...`
Expected: FAIL — no heartbeat log produced

**Step 3: Implement heartbeat in healthMonitor**

In `k2/engine/health.go`, add to the `healthMonitor` struct:

```go
	diagCounter int       // samples since last DIAG heartbeat
	connectedAt time.Time // engine connected timestamp for uptime calc
```

In `newHealthMonitor`, add a `connectedAt time.Time` parameter and store it.

In `k2/engine/engine.go:332`, pass `time.Now()` as `connectedAt` to `newHealthMonitor`.

In `k2/engine/health.go:sample()`, after writing to ring buffer (after line 173), add:

```go
	// Layer 1 DIAG heartbeat — every 30 seconds.
	hm.diagCounter++
	if hm.diagCounter >= 30 {
		hm.diagCounter = 0
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
		)
	}
```

This code runs inside `hm.mu.Lock()` which is allowed per the concurrency rules exception for slog under state lock.

**Step 4: Run test to verify it passes**

Run: `cd k2 && go test -run TestHealthMonitorHeartbeat ./engine/...`
Expected: PASS

**Step 5: Run full engine tests**

Run: `cd k2 && go test ./engine/...`
Expected: All pass

**Step 6: Commit**

```bash
cd k2 && git add engine/health.go engine/health_test.go engine/engine.go && git commit -m "feat(engine): add Layer 1 DIAG heartbeat every 30s"
```

---

### Task 4: Layer 2 — DIAG: connected + session-end

**Files:**
- Modify: `k2/engine/engine.go:346` (add DIAG connected after "engine: connected")
- Modify: `k2/engine/engine.go:490` (add DIAG session-end in Stop)

**Step 1: Add DIAG: connected**

In `k2/engine/engine.go`, right after the existing `slog.Info("engine: connected", ...)` at line 346, add:

```go
	slog.Info("DIAG: connected",
		"server", extractServerHost(client.Server),
		"mode", client.Mode,
		"ruleGlobal", client.Rule.Global,
		"dialMs", time.Since(startTime).Milliseconds(),
	)
```

**Step 2: Add DIAG: session-end**

In `k2/engine/engine.go:Stop()`, before changing state to disconnected (around line 490), capture connectedAt and traffic. After the existing `slog.Info("engine: stop", ...)`, add:

```go
	var sessionUptime time.Duration
	var sessionTx, sessionRx int64
	if !e.connectedAt.IsZero() {
		sessionUptime = time.Since(e.connectedAt)
	}
	if e.tunnel != nil {
		sessionTx, sessionRx = e.tunnel.TrafficBytes()
	}
```

Then after the final status broadcast (after `stopHandler.OnStatus(stopStatus)`), add:

```go
	if sessionUptime > 0 {
		slog.Info("DIAG: session-end",
			"uptimeS", int(sessionUptime.Seconds()),
			"txMB", fmt.Sprintf("%.1f", float64(sessionTx)/(1024*1024)),
			"rxMB", fmt.Sprintf("%.1f", float64(sessionRx)/(1024*1024)),
		)
	}
```

**Step 3: Run engine tests**

Run: `cd k2 && go test ./engine/...`
Expected: All pass

**Step 4: Commit**

```bash
cd k2 && git add engine/engine.go && git commit -m "feat(engine): add DIAG connected/session-end events"
```

---

### Task 5: Layer 2 — DIAG: dns-slow + dns-fail

**Files:**
- Modify: `k2/core/dns/middleware.go:259-291` (add timing + DIAG logs in queryUpstream)

**Step 1: Add DIAG logging to queryUpstream**

In `k2/core/dns/middleware.go:queryUpstream()`, wrap each upstream call with timing:

```go
func (m *Middleware) queryUpstream(ctx context.Context, domain string, action k2rule.Target, query *dns.Msg) (*dns.Msg, error) {
	ctx, cancel := context.WithTimeout(ctx, upstreamTimeout)
	defer cancel()

	start := time.Now()

	switch action {
	case k2rule.TargetDirect:
		m.metrics.IncrDirect()
		resp, err := m.directDNS.Query(ctx, query)
		elapsed := time.Since(start)
		if err != nil {
			m.metrics.IncrError()
			slog.Warn("DNS direct query failed", "domain", domain, "err", err)
			slog.Warn("DIAG: dns-fail", "domain", domain, "upstream", "direct", "err", err)
			return nil, err
		}
		if elapsed > 500*time.Millisecond {
			slog.Info("DIAG: dns-slow", "domain", domain, "upstream", "direct", "latencyMs", elapsed.Milliseconds())
		}
		slog.Debug("DNS routed direct", "domain", domain)
		return resp, nil

	case k2rule.TargetProxy:
		m.metrics.IncrProxy()
		resp, err := m.proxyDNS.Query(ctx, query)
		elapsed := time.Since(start)
		if err != nil {
			m.metrics.IncrError()
			slog.Warn("DIAG: dns-fail", "domain", domain, "upstream", "proxy", "err", err)
			return nil, fmt.Errorf("DNS proxy query failed for %s: %w", domain, err)
		}
		if elapsed > 500*time.Millisecond {
			slog.Info("DIAG: dns-slow", "domain", domain, "upstream", "proxy", "latencyMs", elapsed.Milliseconds())
		}
		slog.Debug("DNS routed proxy", "domain", domain)
		return resp, nil

	default:
		return nil, fmt.Errorf("unexpected routing action: %d", action)
	}
}
```

**Step 2: Run DNS tests**

Run: `cd k2 && go test ./core/dns/...`
Expected: All pass

**Step 3: Commit**

```bash
cd k2 && git add core/dns/middleware.go && git commit -m "feat(dns): add DIAG dns-slow/dns-fail events"
```

---

### Task 6: Layer 2 — DIAG: proxy-dial-fail + proxy-dial-slow

**Files:**
- Modify: `k2/core/tunnel.go:419-435` (handleTCPProxy — add timing + DIAG logs)
- Modify: `k2/core/tunnel.go:461-477` (handleUDPProxy — add timing + DIAG logs)

**Step 1: Add DIAG logging to handleTCPProxy**

Replace the `handleTCPProxy` function body:

```go
func (t *ClientTunnel) handleTCPProxy(local net.Conn, dest string, earlyData []byte) {
	start := time.Now()
	remote, err := t.dialer.DialTCP(t.ctx, dest, earlyData)
	dialLatency := time.Since(start)
	if err != nil {
		slog.Warn("tunnel: proxy TCP dial failed", "dest", dest, "err", err)
		slog.Warn("DIAG: proxy-dial-fail", "dest", dest, "transport", "tcp", "err", err)
		if t.wireReporter != nil {
			t.wireReporter.ReportWireError(err)
		}
		return
	}
	if dialLatency > 3*time.Second {
		slog.Info("DIAG: proxy-dial-slow", "dest", dest, "transport", "tcp", "latencyMs", dialLatency.Milliseconds())
	}
	if t.wireReporter != nil {
		t.wireReporter.ClearWireError()
	}
	defer remote.Close()
	untrack := t.conns.track(remote)
	defer untrack()
	pipe(local, remote, &t.txBytes, &t.rxBytes)
}
```

**Step 2: Add DIAG logging to handleUDPProxy**

Replace the `handleUDPProxy` function body:

```go
func (t *ClientTunnel) handleUDPProxy(local net.PacketConn, dest string) {
	start := time.Now()
	remote, err := t.dialer.DialUDP(t.ctx, dest)
	dialLatency := time.Since(start)
	if err != nil {
		slog.Warn("tunnel: proxy UDP dial failed", "dest", dest, "err", err)
		slog.Warn("DIAG: proxy-dial-fail", "dest", dest, "transport", "udp", "err", err)
		if t.wireReporter != nil {
			t.wireReporter.ReportWireError(err)
		}
		return
	}
	if dialLatency > 3*time.Second {
		slog.Info("DIAG: proxy-dial-slow", "dest", dest, "transport", "udp", "latencyMs", dialLatency.Milliseconds())
	}
	if t.wireReporter != nil {
		t.wireReporter.ClearWireError()
	}
	defer remote.Close()
	untrack := t.conns.track(remote)
	defer untrack()
	pipePacket(local, remote, nil, &t.txBytes, &t.rxBytes)
}
```

**Step 3: Run core tests**

Run: `cd k2 && go test ./core/...`
Expected: All pass

**Step 4: Commit**

```bash
cd k2 && git add core/tunnel.go && git commit -m "feat(core): add DIAG proxy-dial-fail/slow events"
```

---

### Task 7: Layer 2 — DIAG: quic-handshake-fail + transport-switch

**Files:**
- Modify: `k2/wire/quic.go:399-402` (add DIAG after handshake failure)
- Modify: `k2/wire/transport.go:94-101` (add DIAG on fallback activation/recovery)

**Step 1: Add DIAG: quic-handshake-fail**

In `k2/wire/quic.go:connect()`, after the existing `c.handshakeFailStreak.Add(1)` at line 401, add:

```go
		slog.Warn("DIAG: quic-handshake-fail",
			"addr", addr,
			"consecutiveFails", c.handshakeFailStreak.Load(),
			"err", err,
		)
```

**Step 2: Add DIAG: transport-switch**

In `k2/wire/transport.go:SetFallbackActive()`, replace the existing slog.Warn with:

```go
	if active {
		if tm.fallbackActive.CompareAndSwap(false, true) {
			slog.Warn("wire: QUIC fallback activated")
			slog.Warn("DIAG: transport-switch", "from", "quic", "to", "tcpws", "reason", "quic-handshake-threshold")
		}
	} else {
		tm.fallbackActive.Store(false)
	}
```

In `k2/wire/transport.go:recordQuicSuccess()`, after the existing slog.Info, add:

```go
	if wasFallback {
		slog.Info("wire: QUIC recovered from fallback")
		slog.Info("DIAG: transport-switch", "from", "tcpws", "to", "quic", "reason", "quic-recovered")
	}
```

**Step 3: Run wire tests**

Run: `cd k2 && go test -short ./wire/...`
Expected: All pass

**Step 4: Commit**

```bash
cd k2 && git add wire/quic.go wire/transport.go && git commit -m "feat(wire): add DIAG quic-handshake-fail/transport-switch events"
```

---

### Task 8: Layer 2 — DIAG: wire-error (in engine)

**Files:**
- Modify: `k2/engine/engine.go:737` (add DIAG after ReportWireError classification)

**Step 1: Add DIAG: wire-error**

In `k2/engine/engine.go:ReportWireError()`, after the existing `slog.Warn("engine: wire error", ...)` at ~line 737, add:

```go
	slog.Warn("DIAG: wire-error",
		"code", newErr.Code,
		"category", newErr.Category,
		"message", newErr.Message,
	)
```

**Step 2: Run engine tests**

Run: `cd k2 && go test ./engine/...`
Expected: All pass

**Step 3: Commit**

```bash
cd k2 && git add engine/engine.go && git commit -m "feat(engine): add DIAG wire-error event"
```

---

### Task 9: Full Integration Test

**Step 1: Run all k2 tests**

Run: `cd k2 && go test -short ./...`
Expected: All pass

**Step 2: Verify DIAG grep works on test output**

Run: `cd k2 && go test -v -run TestHealthMonitorHeartbeat ./engine/... 2>&1 | grep "DIAG:"`
Expected: At least one DIAG heartbeat line visible

**Step 3: Build client binary**

Run: `cd k2 && go build -tags nowebapp -o /tmp/k2-diag-test ./cmd/k2`
Expected: Build succeeds

**Step 4: Final commit (if any fixups needed)**

```bash
cd k2 && git add -A && git commit -m "test: verify DIAG logging integration"
```

---

## Summary: What Gets grep-able

After implementation, the operator has these search patterns:

```bash
grep "DIAG: heartbeat"         # 30s periodic health (Layer 1)
grep "DIAG: connected"         # Tunnel established
grep "DIAG: session-end"       # Tunnel torn down + summary
grep "DIAG: dns-slow"          # DNS latency > 500ms
grep "DIAG: dns-fail"          # DNS upstream failure
grep "DIAG: proxy-dial-fail"   # Wire proxy dial failure
grep "DIAG: proxy-dial-slow"   # Wire proxy dial > 3s
grep "DIAG: quic-handshake"    # QUIC handshake failure
grep "DIAG: transport-switch"  # QUIC ↔ TCP-WS transition
grep "DIAG: wire-error"        # Classified engine error
grep "DIAG:"                   # Everything diagnostic
grep "DIAG:" | grep -v heartbeat  # Problems only
```

Total changes: ~120 lines across 6 files + CLAUDE.md constitution. Zero new dependencies. Zero new goroutines. All inside existing code paths.
