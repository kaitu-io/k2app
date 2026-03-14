# Wire Probe: Handshake-Only Optimization

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace the end-to-end wire probe (`DialTCP("1.1.1.1:443")`) with a handshake-only probe that verifies client↔server wire connectivity without proxying to any target.

**Architecture:** Add `Handshake(ctx)` to `TransportManager` that calls `connect()` on QUIC (preferred) or TCP-WS (fallback), following the same fallback logic as `DialTCP` but stopping after the transport handshake succeeds. Engine step 4.5 and recovery probe both switch to `Handshake()`. UDP probe (step 4.6) removed entirely.

**Tech Stack:** Go, k2 wire package, engine package

---

## Background & Evidence

### Problem with current probe

Engine step 4.5 calls `tm.DialTCP(ctx, "1.1.1.1:443", nil)` which does:
1. QUIC handshake to k2 server (~600-900ms) ← **this is what we need**
2. Open H3 stream + write proxy header
3. Server `net.DialTimeout("tcp4", "1.1.1.1:443", 5s)` ← **wrong layer, adds ~1s**
4. Read server response

**Two bugs:**
- **False negative**: If 1.1.1.1 is unreachable from server (firewall, routing), probe fails even though tunnel works fine.
- **Wasted latency**: Server upstream dial to 1.1.1.1 adds ~500ms-1s on every connection. For AU server from China, this turns a 1.5s connection into 2.5-3s.

### Why probe cannot be removed entirely

Without any probe, the DNS error path has a **30-failure threshold** (`dnsWireErrorThreshold = 30` in `engine/dns_handler.go:38`) before `ReportWireError()` fires. Since DNS failures don't generate TCP traffic, the TCP error path (`tunnel.go:461`) never triggers. Health monitor sees no transport data (`ws.Transport == ""`) and stays "healthy". Result: **~15 seconds of false "connected" state** when server is unreachable.

### Why handshake-only is correct

`connect()` success proves:
- UDP/TCP reachability to server ✓
- TLS handshake (ECH, cert pinning, fingerprint) ✓
- QUIC protocol established ✓
- Server authentication (401/403 classified by `ClassifyError`) ✓

The cached connection is reused by subsequent `DialTCP()`/`DialUDP()` via fast path (`quic.go:309-320`, `tcpws.go:150-153`). No duplicate handshake.

Precedent: `TransportManager.quicProbeLoop()` (`transport.go:331`) already calls `qc.connect(probeCtx)` directly for QUIC recovery probing.

### Measured impact

Current: ~2.9s total connection time (AU server from China), of which ~1-1.5s is target dial.
After: ~1.5-2s total connection time. Same server, same network.

---

## Task 1: Add `Handshake()` to TransportManager

**Files:**
- Modify: `k2/wire/transport.go` (add `Handshake` method after `DialUDP`, ~line 254)
- Test: `k2/wire/transport_test.go` (new test) or `k2/wire/contract_test.go` (add contract test)

### Step 1: Write failing test

Add to `k2/wire/contract_test.go` (after existing contract tests):

```go
// TestContract_Handshake_QUICSuccess verifies that Handshake() completes
// the QUIC handshake without opening a proxy stream or dialing any target.
func TestContract_Handshake_QUICSuccess(t *testing.T) {
	tm, quicClient, _ := newTestTransportManager(t)
	defer tm.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := tm.Handshake(ctx)
	if err != nil {
		t.Fatalf("Handshake failed: %v", err)
	}

	// Verify QUIC handshake happened (connect was called).
	if quicClient.connectCalls() == 0 {
		t.Fatal("expected QUIC connect to be called")
	}

	// Verify NO proxy stream was opened (no DialTCP target).
	if quicClient.dialTCPCalls() > 0 {
		t.Fatal("Handshake should not open proxy streams")
	}
}

// TestContract_Handshake_QUICFails_TCPWSFallback verifies fallback behavior.
func TestContract_Handshake_QUICFails_TCPWSFallback(t *testing.T) {
	tm, quicClient, tcpwsClient := newTestTransportManager(t)
	defer tm.Close()

	// Make QUIC fail 3 times to trigger fallback.
	quicClient.setConnectError(fmt.Errorf("UDP blocked"))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := tm.Handshake(ctx)
	if err != nil {
		t.Fatalf("Handshake failed: %v", err)
	}

	// TCP-WS should have been used as fallback.
	if tcpwsClient.connectCalls() == 0 {
		t.Fatal("expected TCP-WS connect to be called")
	}
}

// TestContract_Handshake_BothFail verifies error when all transports fail.
func TestContract_Handshake_BothFail(t *testing.T) {
	tm, quicClient, tcpwsClient := newTestTransportManager(t)
	defer tm.Close()

	quicClient.setConnectError(fmt.Errorf("UDP blocked"))
	tcpwsClient.setConnectError(fmt.Errorf("TCP refused"))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := tm.Handshake(ctx)
	if err == nil {
		t.Fatal("Handshake should fail when both transports fail")
	}

	var me *MultiTransportError
	if !errors.As(err, &me) {
		t.Fatalf("expected MultiTransportError, got %T: %v", err, err)
	}
}
```

> **Note to implementer:** The existing contract tests in `contract_test.go` use `newTestTransportManager` helper. If that helper doesn't expose `connectCalls()`/`dialTCPCalls()` tracking, you'll need to extend the mock clients. Check the existing mock infrastructure first and adapt the test accordingly. The key invariant is: **Handshake calls `connect()` but NOT `DialTCP()`**.

### Step 2: Run test to verify it fails

```bash
cd k2 && go test -run TestContract_Handshake -v ./wire/...
```
Expected: FAIL — `Handshake` method does not exist.

### Step 3: Implement `Handshake()` on TransportManager

Add to `k2/wire/transport.go` after `DialUDP` (after line 254):

```go
// Handshake verifies wire connectivity by completing the transport handshake
// (QUIC or TCP-WS) without opening a proxy stream or dialing any target.
// The cached connection is reused by subsequent DialTCP/DialUDP calls.
//
// Follows the same QUIC-preferred, TCP-WS-fallback logic as DialTCP:
// - Try QUIC connect first (skip if fallback active)
// - On QUIC failure, track handshake streak + try TCP-WS
// - Return MultiTransportError if both fail
func (tm *TransportManager) Handshake(ctx context.Context) error {
	tm.mu.RLock()
	quic := tm.quic
	tcpws := tm.tcpws
	tm.mu.RUnlock()

	slog.Debug("TransportManager: Handshake", "hasQUIC", quic != nil, "hasTCPWS", tcpws != nil)

	var quicErr error

	// Try QUIC first (preferred), skip if fallback is active.
	if quic != nil && !tm.fallbackActive.Load() {
		quicCtx, quicCancel := tm.quicContext(ctx, tcpws != nil)
		slog.Debug("TransportManager: Handshake trying QUIC")
		if qc, ok := quic.(*QUICClient); ok {
			_, err := qc.connect(quicCtx)
			quicCancel()
			if err == nil {
				tm.recordQuicSuccess()
				slog.Info("TransportManager: Handshake QUIC ok")
				return nil
			}
			slog.Debug("TransportManager: Handshake QUIC failed", "err", err)
			quicErr = err
			if qc.ConsecConnFails() >= quicConnFailThreshold {
				tm.SetFallbackActive(true)
			}
		} else {
			quicCancel()
		}
	}

	// Fall back to TCP-WS.
	if tcpws != nil {
		slog.Debug("TransportManager: Handshake trying TCP-WS")
		if tc, ok := tcpws.(*TCPWSClient); ok {
			_, err := tc.connect(ctx)
			if err == nil {
				slog.Info("TransportManager: Handshake TCP-WS ok")
				return nil
			}
			slog.Debug("TransportManager: Handshake TCP-WS failed", "err", err)
			if quicErr != nil {
				return &MultiTransportError{QUICErr: quicErr, TCPWSErr: err}
			}
			return fmt.Errorf("TCP-WS: %w", err)
		}
	}

	if quicErr != nil {
		return fmt.Errorf("QUIC: %w", quicErr)
	}
	return ErrNotConnected
}
```

**Design notes:**
- Uses `*QUICClient`/`*TCPWSClient` type assertions to access `connect()` — same pattern as `quicProbeLoop` (line 322-331) and `ResetConnections` (line 356-361). These are within the same `wire` package.
- Fallback threshold logic mirrors `DialTCP` (line 169-171).
- `recordQuicSuccess()` called on QUIC handshake success — same as `DialTCP` (line 157).
- Returns `MultiTransportError` when both fail — same as `DialTCP` (line 185).
- No `proxyLayerErr` check needed — `connect()` only returns transport errors (handshake failures), never proxy-layer errors.

### Step 4: Run test to verify it passes

```bash
cd k2 && go test -run TestContract_Handshake -v ./wire/...
```
Expected: PASS

### Step 5: Run all wire tests for regression

```bash
cd k2 && go test -short -race ./wire/...
```
Expected: All pass.

### Step 6: Commit

```bash
cd k2 && git add wire/transport.go wire/contract_test.go
git commit -m "feat(wire): add Handshake() — transport handshake without proxy stream"
```

---

## Task 2: Update `probeDialer` interface for recovery probe

**Files:**
- Modify: `k2/engine/recovery_probe.go:23-29` (change interface + update `run()`)
- Modify: `k2/engine/recovery_probe_test.go:13-42` (update mock)

### Step 1: Update mock to support both interfaces

In `k2/engine/recovery_probe_test.go`, the mock needs to implement `Handshake()`:

```go
// mockProbeDialer implements probeDialer for testing.
type mockProbeDialer struct {
	mu            sync.Mutex
	handshakeFunc func(ctx context.Context) error
	calls         int32 // atomic for race-safe reads
}

func (m *mockProbeDialer) Handshake(ctx context.Context) error {
	atomic.AddInt32(&m.calls, 1)
	m.mu.Lock()
	fn := m.handshakeFunc
	m.mu.Unlock()
	if fn != nil {
		return fn(ctx)
	}
	return nil // Default: success
}

func (m *mockProbeDialer) getCalls() int {
	return int(atomic.LoadInt32(&m.calls))
}

func (m *mockProbeDialer) setHandshakeFunc(fn func(ctx context.Context) error) {
	m.mu.Lock()
	m.handshakeFunc = fn
	m.mu.Unlock()
}
```

### Step 2: Update the `probeDialer` interface

In `k2/engine/recovery_probe.go:23-29`:

```go
// probeDialer is the subset of wire.TransportManager needed by recoveryProbe.
type probeDialer interface {
	Handshake(ctx context.Context) error
}

// Compile-time check: TransportManager satisfies probeDialer.
var _ probeDialer = (*wire.TransportManager)(nil)
```

### Step 3: Update `run()` to use `Handshake()` instead of `DialTCP()`

In `k2/engine/recovery_probe.go`, the `run()` method (line 70-131). Remove `probeTarget` constant (line 20). Change lines 95-98:

**Before:**
```go
slog.Info("recovery: probing wire connectivity", "attempt", attempt)
probeCtx, probeCancel := context.WithTimeout(ctx, probeDialTimeout)
conn, err := p.dialer.DialTCP(probeCtx, probeTarget, nil)
probeCancel()
```

**After:**
```go
slog.Info("recovery: probing wire connectivity", "attempt", attempt)
probeCtx, probeCancel := context.WithTimeout(ctx, probeDialTimeout)
err := p.dialer.Handshake(probeCtx)
probeCancel()
```

And remove `conn.Close()` on success (line 105), since `Handshake()` returns `error` not a connection:

**Before (lines 104-108):**
```go
if err == nil {
    conn.Close()
    slog.Info("recovery: probe succeeded", "attempt", attempt)
    p.engine.ClearWireError()
    return
}
```

**After:**
```go
if err == nil {
    slog.Info("recovery: probe succeeded", "attempt", attempt)
    p.engine.ClearWireError()
    return
}
```

### Step 4: Update all recovery probe tests

Update each test in `recovery_probe_test.go` to use the new mock API. The key change: `setDialFunc` → `setHandshakeFunc`, and the function signature changes from `func(ctx, addr, earlyData) (net.Conn, error)` to `func(ctx) error`.

For example, `TestRecoveryProbe_FailsThenRetries` (line 97):

**Before:**
```go
dialer.setDialFunc(func(ctx context.Context, addr string, earlyData []byte) (net.Conn, error) {
    return nil, fmt.Errorf("connection refused")
})
```

**After:**
```go
dialer.setHandshakeFunc(func(ctx context.Context) error {
    return fmt.Errorf("connection refused")
})
```

For success cases that previously returned `net.Pipe()`, just return `nil`.

### Step 5: Run recovery probe tests

```bash
cd k2 && go test -run TestRecoveryProbe -v ./engine/...
```
Expected: All pass.

### Step 6: Commit

```bash
cd k2 && git add engine/recovery_probe.go engine/recovery_probe_test.go
git commit -m "refactor(engine): recovery probe uses Handshake() instead of DialTCP"
```

---

## Task 3: Replace engine step 4.5 and remove step 4.6

**Files:**
- Modify: `k2/engine/engine.go:179-208` (replace probe logic)
- Modify: `k2/engine/engine_test.go:296-334` (update `TestEngineStart_WireProbeFailure`)

### Step 1: Update engine step 4.5

In `k2/engine/engine.go`, replace lines 179-208:

**Before:**
```go
// 4.5 Verify wire connectivity before setting up TUN/provider.
// Without this check, engine reports "connected" after TUN starts even if
// wire server is unreachable — DNS handler silently absorbs wire failures
// and ReportWireError() never triggers (no TCP connections created).
slog.Info("engine: step 4.5 - wire connectivity probe", "elapsed", time.Since(startTime))
probeCtx, probeCancel := context.WithTimeout(ctx, 15*time.Second)
probeConn, probeErr := tm.DialTCP(probeCtx, "1.1.1.1:443", nil)
probeCancel()
if probeErr != nil {
    slog.Warn("engine: wire probe failed", "err", probeErr)
    if cfg.NetworkMonitor != nil {
        cfg.NetworkMonitor.Close()
    }
    tm.Close()
    return e.fail(fmt.Errorf("wire probe: %w", probeErr))
}
probeConn.Close()
slog.Info("engine: step 4.5 - wire probe ok", "elapsed", time.Since(startTime))

// 4.6 Optional UDP probe — non-fatal, logs warning if UDP broken.
// Runtime datagram health tracking (Step 2) handles fallback.
udpProbeCtx, udpProbeCancel := context.WithTimeout(ctx, 5*time.Second)
udpConn, udpErr := tm.DialUDP(udpProbeCtx, "8.8.8.8:53")
udpProbeCancel()
if udpErr != nil {
    slog.Warn("engine: wire UDP probe failed (DNS will use fallback)", "err", udpErr)
} else {
    udpConn.Close()
    slog.Info("engine: step 4.6 - wire UDP probe ok", "elapsed", time.Since(startTime))
}
```

**After:**
```go
// 4.5 Verify wire handshake (client ↔ server) before setting up TUN/provider.
// Completes QUIC or TCP-WS handshake only — does NOT proxy to any target.
// Without this check, engine reports "connected" after TUN starts even if
// the server is unreachable. DNS failures are silently accumulated (threshold=30)
// and the health monitor sees no transport data, creating a ~15s false-connected window.
// The cached connection is reused by subsequent DialTCP/DialUDP (fast path).
slog.Info("engine: step 4.5 - wire handshake probe", "elapsed", time.Since(startTime))
hsCtx, hsCancel := context.WithTimeout(ctx, 15*time.Second)
hsErr := tm.Handshake(hsCtx)
hsCancel()
if hsErr != nil {
    slog.Warn("engine: wire handshake failed", "err", hsErr)
    if cfg.NetworkMonitor != nil {
        cfg.NetworkMonitor.Close()
    }
    tm.Close()
    return e.fail(fmt.Errorf("wire handshake: %w", hsErr))
}
slog.Info("engine: step 4.5 - wire handshake ok", "elapsed", time.Since(startTime))
```

**Key changes:**
- `tm.DialTCP(probeCtx, "1.1.1.1:443", nil)` → `tm.Handshake(hsCtx)`
- No `probeConn.Close()` needed (Handshake returns error, not connection)
- Step 4.6 (UDP probe to 8.8.8.8:53) removed entirely — same target-dependency problem, and UDP health is handled at runtime by the datagram relay health tracker
- Error message: `"wire probe"` → `"wire handshake"`

### Step 2: Update `TestEngineStart_WireProbeFailure`

In `k2/engine/engine_test.go:296-334`, update the error string assertion:

**Before (line 314):**
```go
if !strings.Contains(err.Error(), "wire probe") {
    t.Errorf("error = %q, want to contain 'wire probe'", err.Error())
}
```

**After:**
```go
if !strings.Contains(err.Error(), "wire handshake") {
    t.Errorf("error = %q, want to contain 'wire handshake'", err.Error())
}
```

### Step 3: Check for any other references to "wire probe" in engine tests

Search for `wire probe` or `probeTarget` or `1.1.1.1` in engine test files and update:
- `k2/engine/engine_test.go` — the assertion above
- `k2/engine/engine.go:182` — comment updated in step 1

### Step 4: Run engine tests

```bash
cd k2 && go test -run TestEngine -v ./engine/...
```
Expected: All pass.

### Step 5: Run all engine tests including contracts

```bash
cd k2 && go test -short -race ./engine/...
```
Expected: All pass.

### Step 6: Commit

```bash
cd k2 && git add engine/engine.go engine/engine_test.go
git commit -m "perf(engine): handshake-only wire probe — remove target dial and UDP probe

Wire probe now verifies QUIC/TCP-WS handshake without proxying to 1.1.1.1.
Removes false negatives from target unreachability and saves ~1s per connection.
UDP probe (step 4.6) removed — runtime datagram health handles UDP fallback."
```

---

## Task 4: Update wire contract test for probe context cancellation

**Files:**
- Modify: `k2/wire/tcpws_test.go:963-1054` (may need updating if it references `probeTarget` or `"1.1.1.1:443"`)

### Step 1: Check `TestTCPWSClient_ProbeContextCancellation`

Read `k2/wire/tcpws_test.go:963-1060`. This test simulates the engine wire probe bug where probe context cancellation killed the cached TCP-WS session.

**If the test uses `DialTCP(probeCtx, "1.1.1.1:443", nil)`:** The test is testing TCP-WS session survival across context cancellation — this behavior is still relevant. The `addr` parameter in DialTCP is just a proxy target; the real test is that context cancellation doesn't break the cached smux session. **Leave the test as-is** — it tests a TCP-WS transport behavior, not the engine probe design.

**If the test references `probeTarget` constant from engine package:** Update the reference. But since this is in `wire/` package, it likely uses a literal string.

### Step 2: Run wire tests

```bash
cd k2 && go test -run TestTCPWSClient_ProbeContext -v ./wire/...
```
Expected: PASS (no changes needed if test uses literal addr).

### Step 3: Commit (only if changes were needed)

```bash
cd k2 && git add wire/tcpws_test.go
git commit -m "test(wire): update probe context test for handshake-only probe"
```

---

## Task 5: Update CLAUDE.md documentation

**Files:**
- Modify: `k2/engine/CLAUDE.md` (update architecture diagram step 4.5 description)
- Modify: `k2/CLAUDE.md` (update if "wire probe" or "1.1.1.1" is mentioned)

### Step 1: Update `k2/engine/CLAUDE.md`

In the Architecture section, change:
```
├── 4.5. Wire connectivity probe (TCP + UDP)
```
To:
```
├── 4.5. Wire handshake probe (QUIC/TCP-WS handshake only, no target dial)
```

### Step 2: Check `k2/CLAUDE.md` for references

Search for "wire probe", "1.1.1.1", "step 4.5", "step 4.6". Update any mentions to reflect handshake-only design.

### Step 3: Commit

```bash
cd k2 && git add engine/CLAUDE.md CLAUDE.md
git commit -m "docs: update CLAUDE.md for handshake-only wire probe"
```

---

## Task 6: Full regression test

### Step 1: Run all k2 tests

```bash
cd k2 && go test -short -race ./...
```
Expected: All pass.

### Step 2: Run engine contract tests specifically

```bash
cd k2 && go test -run TestContract -v ./engine/... ./wire/...
```
Expected: All pass.

### Step 3: Run wire race tests (longer timeout)

```bash
cd k2 && go test -race -timeout 300s ./wire/...
```
Expected: All pass.

---

## Verification Checklist

After all tasks complete, verify:

- [ ] `tm.Handshake(ctx)` exists and follows QUIC-first, TCP-WS-fallback logic
- [ ] Engine step 4.5 calls `tm.Handshake()`, NOT `tm.DialTCP("1.1.1.1:443")`
- [ ] Engine step 4.6 (UDP probe to 8.8.8.8:53) is removed
- [ ] Recovery probe calls `Handshake()`, NOT `DialTCP(probeTarget)`
- [ ] `probeTarget = "1.1.1.1:443"` constant is removed from recovery_probe.go
- [ ] No remaining references to `"1.1.1.1"` or `"8.8.8.8"` in engine/ (except DNS config defaults)
- [ ] `TestEngineStart_WireProbeFailure` asserts `"wire handshake"` not `"wire probe"`
- [ ] All tests pass: `go test -short -race ./...`
- [ ] CLAUDE.md updated
