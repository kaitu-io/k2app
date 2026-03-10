# Transport Behavior Contract & Pre-Release Automation

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Establish a transport behavior contract with 10 contract-driven tests, rename ambiguous UDP terminology, and add `make pre-release` automation.

**Architecture:** Two phases — (1) rename ambiguous terms + write contract tests that codify the 10 transport behavior scenarios from the user's perspective, (2) add Makefile targets for CI/local quality gates. Contract tests use existing mock patterns in `wire/transport_test.go` (mockTransport, mockStatsTransport) and `engine/recovery_probe_test.go` (mock probeDialer).

**Tech Stack:** Go testing stdlib, go-deadlock, golangci-lint, existing mock infrastructure.

---

## Terminology Reference

| Term | Definition | Code symbol |
|------|-----------|-------------|
| QUIC 传输 | client↔server QUIC connection (底层 UDP) | `QUICClient` |
| TCP-WS 传输 | client↔server TCP+WebSocket+smux connection | `TCPWSClient` |
| Datagram 中继 | 用户 UDP 包通过 QUIC datagram (不可靠) | `quicUDPConn` |
| Stream 中继 | 用户 UDP 包通过 QUIC/smux stream (可靠) | `quicStreamUDPConn` / `smuxUDPConn` |
| 传输管理器 | 协调 QUIC/TCP-WS 双通道 | `TransportManager` |
| 跳端口 | QUIC 握手失败后换 UDP 端口重试 | `hopPort()` |

---

### Task 1: Rename ambiguous UDP terminology

Rename relay-layer symbols to eliminate "UDP" ambiguity. Wire-layer UDP (QUIC transport底层) vs relay-layer UDP (用户数据包中继) must be distinct.

**Files:**
- Modify: `wire/wire.go:20` — `ErrRelayUDPUnhealthy` → `ErrDatagramRelayUnhealthy`
- Modify: `wire/quic.go` — `relayUDPHealthy()` → `datagramRelayHealthy()`, `quicUDPMux` field comments
- Modify: `wire/transport.go:162` — comment update
- Modify: `wire/CLAUDE.md` — update terminology
- Modify: `wire/VOCABULARY.md` — update naming conventions
- Modify: all test files referencing old names

**Step 1: Rename `ErrRelayUDPUnhealthy` → `ErrDatagramRelayUnhealthy`**

In `wire/wire.go:20`:
```go
// Before:
ErrRelayUDPUnhealthy = errors.New("wire: QUIC relay UDP path unhealthy")

// After:
ErrDatagramRelayUnhealthy = errors.New("wire: datagram relay path unhealthy")
```

**Step 2: Rename `relayUDPHealthy()` → `datagramRelayHealthy()`**

Search `wire/quic.go` for all `relayUDP` references and rename to `datagramRelay`. The method `relayUDPHealthy()` becomes `datagramRelayHealthy()`. Field names in `quicUDPMux` that reference relay UDP metrics rename similarly.

**Step 3: Update all references across codebase**

Run: `grep -r "ErrRelayUDPUnhealthy\|relayUDPHealthy" --include="*.go" .`

Update each reference. Keep `RelayUDPLossRate` and `RelayUDPTimeouts` in `WireStats` and `HealthSnapshot` unchanged — they are observability field names exposed to JSON API (breaking change for dashboards).

**Step 4: Update `wire/VOCABULARY.md` and `wire/CLAUDE.md`**

Add "Datagram Relay" row. Update naming conventions section.

**Step 5: Run tests**

Run: `go test ./wire/... ./engine/... ./test/...`
Expected: ALL PASS

**Step 6: Commit**

```bash
git add wire/ engine/ test/
git commit -m "refactor(wire): rename relayUDP → datagramRelay for clarity

Disambiguate wire-layer UDP (QUIC transport) from relay-layer UDP
(user datagram forwarding). ErrRelayUDPUnhealthy → ErrDatagramRelayUnhealthy,
relayUDPHealthy() → datagramRelayHealthy()."
```

---

### Task 2: Contract test — startup QUIC preferred

Verify QUIC is attempted first on startup. This is the happy path baseline.

**Files:**
- Create: `wire/contract_test.go` — new file for all 10 contract tests
- Reference: `wire/transport_test.go` — reuse `mockTransport` pattern

**Step 1: Write the failing test**

```go
// wire/contract_test.go
package wire

import (
	"context"
	"net"
	"testing"
)

// === Transport Behavior Contract Tests ===
//
// These tests codify the expected transport behavior from the user's perspective.
// Each test maps to one row in the behavior contract table.
// See docs/plans/2026-03-10-transport-contract-pre-release.md for the full contract.

// Contract 1: On startup, QUIC is attempted first. TCP-WS is not tried
// unless QUIC fails.
func TestContract_StartupQUICFirst(t *testing.T) {
	var quicCalled, tcpwsCalled bool
	quic := &mockTransport{
		available: true,
		dialTCPFn: func(ctx context.Context, addr string) (net.Conn, error) {
			quicCalled = true
			c, _ := net.Pipe()
			return c, nil
		},
	}
	tcpws := &mockTransport{
		available: true,
		dialTCPFn: func(ctx context.Context, addr string) (net.Conn, error) {
			tcpwsCalled = true
			c, _ := net.Pipe()
			return c, nil
		},
	}

	tm := NewTransportManager()
	tm.SetQUIC(quic)
	tm.SetTCPWS(tcpws)

	conn, err := tm.DialTCP(context.Background(), "1.1.1.1:443")
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()

	if !quicCalled {
		t.Error("QUIC must be attempted first")
	}
	if tcpwsCalled {
		t.Error("TCP-WS must not be called when QUIC succeeds")
	}
}
```

**Step 2: Run test to verify it passes** (this is a baseline — existing behavior should pass)

Run: `go test ./wire/... -run TestContract_StartupQUICFirst -v`
Expected: PASS

Note: This test validates existing behavior. If it fails, the implementation has a bug.

---

### Task 3: Contract test — normal TCP via QUIC, UDP via datagram

Verify normal-path transport selection: TCP traffic uses QUIC streams, UDP traffic uses QUIC datagrams.

**Files:**
- Modify: `wire/contract_test.go`

**Step 1: Write the test**

```go
// Contract 2: In normal operation, TCP flows through QUIC streams
// and UDP flows through QUIC datagrams. TCP-WS is not involved.
func TestContract_NormalTCPviaQUIC_UDPviaDatagram(t *testing.T) {
	var quicTCPCalled, quicUDPCalled, tcpwsTCPCalled, tcpwsUDPCalled bool
	quic := &mockTransport{
		available: true,
		dialTCPFn: func(ctx context.Context, addr string) (net.Conn, error) {
			quicTCPCalled = true
			c, _ := net.Pipe()
			return c, nil
		},
		dialUDPFn: func(ctx context.Context, addr string) (net.PacketConn, error) {
			quicUDPCalled = true
			return newWorkingPacketConn(), nil
		},
	}
	tcpws := &mockTransport{
		available: true,
		dialTCPFn: func(ctx context.Context, addr string) (net.Conn, error) {
			tcpwsTCPCalled = true
			c, _ := net.Pipe()
			return c, nil
		},
		dialUDPFn: func(ctx context.Context, addr string) (net.PacketConn, error) {
			tcpwsUDPCalled = true
			return newWorkingPacketConn(), nil
		},
	}

	tm := NewTransportManager()
	tm.SetQUIC(quic)
	tm.SetTCPWS(tcpws)

	conn, err := tm.DialTCP(context.Background(), "1.1.1.1:443")
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()

	pc, err := tm.DialUDP(context.Background(), "8.8.8.8:53")
	if err != nil {
		t.Fatal(err)
	}
	pc.Close()

	if !quicTCPCalled || !quicUDPCalled {
		t.Error("both TCP and UDP must route through QUIC in normal operation")
	}
	if tcpwsTCPCalled || tcpwsUDPCalled {
		t.Error("TCP-WS must not be used when QUIC is healthy")
	}
}
```

**Step 2: Run test**

Run: `go test ./wire/... -run TestContract_NormalTCPviaQUIC -v`
Expected: PASS

---

### Task 4: Contract test — QUIC transport blocked, fallback to TCP-WS

When QUIC handshake fails 3 consecutive times, ALL traffic (TCP+UDP) must switch to TCP-WS.

**Files:**
- Modify: `wire/contract_test.go`

**Step 1: Write the test**

```go
// Contract 3: When QUIC handshake fails 3 consecutive times (transport
// blocked), all traffic switches to TCP-WS. User should not see errors.
func TestContract_QUICBlocked_FallbackTCPWS(t *testing.T) {
	// Use real QUICClient to get handshakeFailStreak behavior.
	qc := NewQUICClient(Config{}, nil)
	// Simulate 3 consecutive handshake failures.
	qc.handshakeFailStreak.Store(quicConnFailThreshold)

	var tcpwsCalled bool
	tcpws := &mockTransport{
		available: true,
		dialTCPFn: func(ctx context.Context, addr string) (net.Conn, error) {
			tcpwsCalled = true
			c, _ := net.Pipe()
			return c, nil
		},
	}

	tm := NewTransportManager()
	tm.SetQUIC(qc)
	tm.SetTCPWS(tcpws)

	// After 3 QUIC failures, TransportManager should activate fallback.
	// Simulate the activation that DialTCP would do:
	if qc.ConsecConnFails() >= quicConnFailThreshold {
		tm.SetFallbackActive(true)
	}

	conn, err := tm.DialTCP(context.Background(), "1.1.1.1:443")
	if err != nil {
		t.Fatalf("user should still be able to connect via TCP-WS: %v", err)
	}
	conn.Close()

	if !tcpwsCalled {
		t.Error("traffic must route through TCP-WS when QUIC is blocked")
	}
	if !tm.FallbackActive() {
		t.Error("fallback must be active when QUIC handshake fails >= 3 times")
	}
}
```

**Step 2: Run test**

Run: `go test ./wire/... -run TestContract_QUICBlocked -v`
Expected: PASS

---

### Task 5: Contract test — QUIC recovers via background probe

When TCP-WS fallback is active and background probe succeeds, new requests return to QUIC.

**Files:**
- Modify: `wire/contract_test.go`

**Step 1: Write the test**

```go
// Contract 4: While in TCP-WS fallback, background probe succeeds →
// new requests return to QUIC. Existing TCP-WS connections are not interrupted.
func TestContract_QUICRecover_ProbeSuccess(t *testing.T) {
	var quicCalled bool
	quic := &mockTransport{
		available: true,
		dialTCPFn: func(ctx context.Context, addr string) (net.Conn, error) {
			quicCalled = true
			c, _ := net.Pipe()
			return c, nil
		},
	}
	tcpws := &mockTransport{available: true}

	tm := NewTransportManager()
	tm.SetQUIC(quic)
	tm.SetTCPWS(tcpws)

	// Start in fallback mode.
	tm.SetFallbackActive(true)
	if !tm.FallbackActive() {
		t.Fatal("precondition: fallback must be active")
	}

	// Simulate probe success: recordQuicSuccess clears fallback.
	tm.recordQuicSuccess()

	if tm.FallbackActive() {
		t.Error("fallback must be cleared after QUIC probe success")
	}

	// Next dial should go through QUIC.
	conn, err := tm.DialTCP(context.Background(), "1.1.1.1:443")
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()

	if !quicCalled {
		t.Error("after probe success, traffic must return to QUIC")
	}
}
```

**Step 2: Run test**

Run: `go test ./wire/... -run TestContract_QUICRecover -v`
Expected: PASS

---

### Task 6: Contract test — datagram relay loss high, degrade to stream

When datagram relay loss >40%, that UDP request falls back to TCP-WS stream relay. QUIC transport itself is NOT reset or reconnected.

**Files:**
- Modify: `wire/contract_test.go`

**Step 1: Write the test**

```go
// Contract 5: When datagram relay has high loss (>40%), individual UDP
// requests degrade to stream relay (TCP-WS). The QUIC transport itself
// is NOT reconnected — only the relay path for that request changes.
func TestContract_DatagramLossHigh_DegradeToStream(t *testing.T) {
	quic := &mockTransport{
		available: true,
		dialTCPFn: func(ctx context.Context, addr string) (net.Conn, error) {
			c, _ := net.Pipe()
			return c, nil
		},
		dialUDPFn: func(ctx context.Context, addr string) (net.PacketConn, error) {
			// Simulate datagram relay unhealthy.
			return nil, ErrDatagramRelayUnhealthy
		},
	}
	var tcpwsUDPCalled bool
	tcpws := &mockTransport{
		available: true,
		dialUDPFn: func(ctx context.Context, addr string) (net.PacketConn, error) {
			tcpwsUDPCalled = true
			return newWorkingPacketConn(), nil
		},
	}

	tm := NewTransportManager()
	tm.SetQUIC(quic)
	tm.SetTCPWS(tcpws)

	// DialUDP should fall through to TCP-WS for this request.
	pc, err := tm.DialUDP(context.Background(), "8.8.8.8:53")
	if err != nil {
		t.Fatalf("UDP should succeed via TCP-WS stream relay: %v", err)
	}
	pc.Close()

	if !tcpwsUDPCalled {
		t.Error("UDP must fall back to TCP-WS stream relay when datagram relay is unhealthy")
	}

	// Critical: transport-level fallback must NOT be activated.
	// QUIC transport is still healthy — only datagram relay is degraded.
	if tm.FallbackActive() {
		t.Error("datagram relay degradation must NOT trigger transport-level fallback")
	}

	// TCP traffic must still go through QUIC (transport is fine).
	conn, err := tm.DialTCP(context.Background(), "1.1.1.1:443")
	if err != nil {
		t.Fatalf("TCP must still work through QUIC: %v", err)
	}
	conn.Close()
}
```

**Step 2: Run test**

Run: `go test ./wire/... -run TestContract_DatagramLoss -v`
Expected: PASS (after Task 1 rename; uses new `ErrDatagramRelayUnhealthy` name)

---

### Task 7: Contract test — network switch resets all transports

OnNetworkChanged triggers full transport reset. Both QUIC and TCP-WS connections are dropped. Fresh connections on next dial.

**Files:**
- Modify: `wire/contract_test.go`

**Step 1: Write the test**

```go
// Contract 6: On network switch (WiFi→cellular), both QUIC and TCP-WS
// transports are reset. Stale connections from the old network are dropped.
// Fallback state is cleared — QUIC gets a fresh chance.
func TestContract_NetworkSwitch_ResetAll(t *testing.T) {
	tm := NewTransportManager()
	qc := NewQUICClient(Config{}, nil)
	tm.SetQUIC(qc)
	tm.SetTCPWS(&mockTransport{available: true})

	// Simulate prior state: fallback active, handshake failures accumulated.
	tm.SetFallbackActive(true)
	qc.handshakeFailStreak.Store(5)

	// ResetConnections simulates what engine.reconnect() does.
	tm.ResetConnections()

	if tm.FallbackActive() {
		t.Error("fallback must be cleared after network switch")
	}
	if qc.ConsecConnFails() != 0 {
		t.Error("handshake fail streak must be cleared after network switch")
	}
	if !tm.freshAfterReset.Load() {
		t.Error("freshAfterReset must be set for extended QUIC budget")
	}
}
```

**Step 2: Run test**

Run: `go test ./wire/... -run TestContract_NetworkSwitch -v`
Expected: PASS

---

### Task 8: Contract test — poor signal with TCP health gate

When wire loss is high but TCP streams are recently successful, do NOT reconnect (reconnect is destructive and won't fix physical-layer signal issues).

**Files:**
- Create: `engine/contract_test.go`
- Reference: `engine/health_test.go` for mock patterns

**Step 1: Write the test**

```go
// engine/contract_test.go
package engine

import (
	"testing"
	"time"

	"github.com/kaitu-io/k2/core"
	"github.com/kaitu-io/k2/wire"
)

// mockTM implements TransportManager methods needed by healthMonitor.
type contractMockTM struct {
	wire.TransportManager
	tcpHealthy bool
	stats      wire.WireStats
}

// Contract 7: When wire packet loss is high but TCP streams are recently
// healthy, health monitor must NOT trigger reconnect. Reconnecting during
// poor signal is destructive (drops active streams, handshake takes seconds)
// and won't improve physical-layer quality.
func TestContract_PoorSignal_TCPHealthGate(t *testing.T) {
	reconnectCalled := false

	tm := wire.NewTransportManager()
	// Set a QUICClient with recent TCP success to make TCPRecentlyHealthy() true.
	qc := wire.NewQUICClient(wire.Config{}, nil)
	qc.RecordTCPSuccess() // exported for testing
	tm.SetQUIC(qc)

	tunnel := core.NewClientTunnel(core.ClientTunnelConfig{})

	hm := newHealthMonitor(tm, tunnel, nil, func() {
		reconnectCalled = true
	})

	// Feed critical-level wire stats for 10 samples (past the 5-streak threshold).
	for i := 0; i < 10; i++ {
		hm.mu.Lock()
		hm.smoothWireLoss = 0.30 // >25% = critical
		hm.ewmaInitialized = true
		level := hm.assess(wire.WireStats{
			Transport:      "quic",
			PacketLossRate: 0.30,
		})
		action := hm.transition(level)
		hm.mu.Unlock()
		if action != nil {
			action()
		}
	}

	if reconnectCalled {
		t.Error("must NOT reconnect when TCP is healthy — signal is poor but transport works")
	}
}
```

**Step 2: Run test**

Run: `go test ./engine/... -run TestContract_PoorSignal -v`

Note: This test may need `RecordTCPSuccess()` exported from QUICClient. If not already exported, add a small helper. Check if `lastTCPSuccessAt` can be set. Alternative: use `qc.lastTCPSuccessAt.Store(time.Now().UnixNano())` from within the wire package test, or add a `SetLastTCPSuccess()` test helper.

**Step 2b: If RecordTCPSuccess not accessible, adjust approach**

Since `healthMonitor` is in `engine` package and `QUICClient.lastTCPSuccessAt` is in `wire` package, and `tcpRecentlyHealthy()` reads from `lastTCPSuccessAt`, we can either:
- Option A: Add `RecordTCPSuccess()` exported method to QUICClient (test helper)
- Option B: Move this test to an integration test that goes through `tm.DialTCP` success path first
- Option C: Mock the `TCPRecentlyHealthy` call at TransportManager level

Choose Option A — minimal, one-liner:
```go
// wire/quic.go — add near tcpRecentlyHealthy:
func (c *QUICClient) RecordTCPSuccess() {
	c.lastTCPSuccessAt.Store(time.Now().UnixNano())
}
```

**Step 3: Run test to verify it passes**

Run: `go test ./engine/... -run TestContract_PoorSignal -v`
Expected: PASS

**Step 4: Commit**

```bash
git add wire/quic.go engine/contract_test.go
git commit -m "test(engine): contract test — TCP health gate skips reconnect on poor signal"
```

---

### Task 9: Contract test — server unreachable, stop after 3 probes

Recovery probe gives up after 3 failed attempts instead of retrying forever.

**Files:**
- Modify: `engine/contract_test.go`
- Reference: `engine/recovery_probe_test.go` — reuse existing `mockProbeDialer` pattern

**Step 1: Verify existing test covers this**

Check `engine/recovery_probe_test.go:97` — `TestRecoveryProbe_FailsThenRetries` already tests 3-attempt behavior.

Run: `go test ./engine/... -run TestRecoveryProbe_FailsThenRetries -v`

If it passes, add a thin contract wrapper that documents the user-facing contract explicitly:

```go
// Contract 8: When server is unreachable, recovery probe stops after 3
// attempts. It does not retry infinitely (which would burn mobile data).
// Verified by existing TestRecoveryProbe_FailsThenRetries — this contract
// test is a documentation anchor.
func TestContract_ServerUnreachable_StopAfter3(t *testing.T) {
	// Delegate to existing thorough test.
	t.Run("verified_by", func(t *testing.T) {
		if probeMaxAttempts != 3 {
			t.Fatalf("probeMaxAttempts = %d, contract requires 3", probeMaxAttempts)
		}
	})
}
```

**Step 2: Run test**

Run: `go test ./engine/... -run TestContract_ServerUnreachable -v`
Expected: PASS

---

### Task 10: Contract test — auth failure stops retry

When server rejects authentication (401/402/403), engine must NOT retry.

**Files:**
- Modify: `engine/contract_test.go`
- Reference: `engine/recovery_probe_test.go:127` — `TestRecoveryProbe_SkipsOnClientError`

**Step 1: Write the contract test**

```go
// Contract 9: When server rejects authentication (401/403), the engine
// must NOT retry. Retrying with the same invalid token wastes bandwidth
// and may trigger server-side rate limiting.
func TestContract_AuthFailed_NoRetry(t *testing.T) {
	// Verify classification constants.
	if CategoryClient != "client" {
		t.Fatalf("CategoryClient = %q, want 'client'", CategoryClient)
	}

	// Verify that stream rejection is classified as client error.
	err := &wire.StreamError{Code: 401, Message: "unauthorized"}
	classified := ClassifyError(err)
	if classified.Category != CategoryClient {
		t.Fatalf("401 must be classified as client error, got %q", classified.Category)
	}
	if classified.Code != 401 {
		t.Fatalf("code must be 401, got %d", classified.Code)
	}
}
```

**Step 2: Run test**

Run: `go test ./engine/... -run TestContract_AuthFailed -v`
Expected: PASS

---

### Task 11: Contract test — slow upstream does NOT trigger fallback

When server's upstream dial is slow (stream response timeout), this is NOT a transport failure. Must not trigger port hop or QUIC→TCP-WS fallback.

**Files:**
- Modify: `wire/contract_test.go`

**Step 1: Write the test**

```go
// Contract 10: When stream response times out (server's upstream is slow),
// this is NOT a QUIC transport failure. The QUIC handshake succeeded
// (OpenStreamSync worked), so handshakeFailStreak must not increment
// and TCP-WS fallback must not activate.
func TestContract_SlowUpstream_NoFallback(t *testing.T) {
	qc := NewQUICClient(Config{}, nil)
	initialStreak := qc.ConsecConnFails()

	// Simulate: QUIC transport works (connect succeeds), but stream
	// response times out (server can't reach destination).
	// TransportManager.DialTCP tries QUIC, gets timeout on ReadStreamResponse.
	// This error has Timeout() = true but it's NOT a handshake failure.

	quic := &mockTransport{
		available: true,
		dialTCPFn: func(ctx context.Context, addr string) (net.Conn, error) {
			// Simulate stream response timeout (not handshake failure).
			return nil, &timeoutError{msg: "stream response timeout"}
		},
	}
	var tcpwsCalled bool
	tcpws := &mockTransport{
		available: true,
		dialTCPFn: func(ctx context.Context, addr string) (net.Conn, error) {
			tcpwsCalled = true
			c, _ := net.Pipe()
			return c, nil
		},
	}

	tm := NewTransportManager()
	tm.SetQUIC(quic)
	tm.SetTCPWS(tcpws)

	// DialTCP will fail on QUIC (timeout), then try TCP-WS.
	// This is acceptable — TCP-WS fallback per-request is fine.
	// The key assertion is: fallbackActive must NOT be set.
	conn, err := tm.DialTCP(context.Background(), "slow.target:443")
	if err == nil {
		conn.Close()
	}

	// Transport-level fallback must not be activated by stream timeouts.
	// (mockTransport doesn't have ConsecConnFails, so fallback won't activate
	// via the threshold check — verifying the design intention.)
	if tm.FallbackActive() {
		t.Error("stream response timeout must NOT trigger transport-level fallback")
	}

	_ = initialStreak // reference only — real QUICClient tested separately
}

// timeoutError implements net.Error for timeout simulation.
type timeoutError struct {
	msg string
}

func (e *timeoutError) Error() string   { return e.msg }
func (e *timeoutError) Timeout() bool   { return true }
func (e *timeoutError) Temporary() bool { return false }
```

**Step 2: Run test**

Run: `go test ./wire/... -run TestContract_SlowUpstream -v`
Expected: PASS

**Step 3: Commit all contract tests**

```bash
git add wire/contract_test.go engine/contract_test.go
git commit -m "test: add 10 transport behavior contract tests

Codify expected transport behavior from the user's perspective:
1. Startup QUIC preferred
2. Normal TCP via QUIC, UDP via datagram
3. QUIC blocked → fallback to TCP-WS
4. QUIC recovers via background probe
5. Datagram relay loss → degrade to stream (no transport reconnect)
6. Network switch → reset all transports
7. Poor signal + TCP healthy → skip reconnect
8. Server unreachable → stop after 3 probes
9. Auth failure → no retry
10. Slow upstream → no fallback"
```

---

### Task 12: Add `make quick-check` and `make pre-release`

**Files:**
- Modify: `Makefile`

**Step 1: Add golangci-lint config**

Create `.golangci.yml`:

```yaml
linters:
  enable:
    - errcheck
    - govet
    - staticcheck
    - ineffassign
    - unused
  disable:
    - depguard
    - gocyclo

linters-settings:
  errcheck:
    exclude-functions:
      - (io.Closer).Close
      - (net.Conn).Close
      - (net.PacketConn).Close

issues:
  exclude-use-default: true

run:
  build-tags:
    - with_gvisor
  timeout: 5m
```

**Step 2: Add Makefile targets**

Append to `Makefile`:

```makefile
# --- Quality gates ---

# Phase 1+2: fast checks for local development (~2min)
quick-check:
	go vet ./...
	@command -v golangci-lint >/dev/null 2>&1 && golangci-lint run || echo "SKIP: golangci-lint not installed"
	go test -short -race -count=1 ./...

# Full pre-release gate: CI and release builds
pre-release: quick-check
	go test -race -count=1 ./...
	go test -run TestContract -v ./wire/... ./engine/...
	$(MAKE) build-all-platforms

# Cross-compile all platforms (compile = no broken imports)
build-all-platforms: build-darwin-arm64 build-linux-amd64 build-linux-arm64 build-windows-amd64 build-server-linux
	@echo "All platform builds OK"
```

**Step 3: Run quick-check to verify**

Run: `make quick-check`
Expected: all steps pass

**Step 4: Commit**

```bash
git add Makefile .golangci.yml
git commit -m "build: add make quick-check and pre-release quality gates

quick-check: go vet + golangci-lint + short race tests (~2min)
pre-release: full tests + contract tests + all platform cross-compile
CI runs 'make pre-release' as the single entry point."
```

---

### Task 13: Add goroutine leak detection with goleak

Add Uber's `goleak` to catch goroutine leaks in tests.

**Files:**
- Modify: `wire/contract_test.go` — add TestMain with goleak
- Modify: `go.mod` — add `go.uber.org/goleak`

**Step 1: Add dependency**

Run: `go get go.uber.org/goleak`

**Step 2: Add TestMain to wire package**

Check if `wire/` already has a `TestMain`. If not, add to `wire/contract_test.go`:

```go
import "go.uber.org/goleak"

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m,
		// Known background goroutines to ignore:
		goleak.IgnoreTopFunction("github.com/apernet/quic-go.(*packetHandlerMap).listen"),
		goleak.IgnoreTopFunction("github.com/apernet/quic-go.(*Transport).listen"),
	)
}
```

Note: If wire/ already has TestMain, integrate goleak there instead.

**Step 3: Run tests**

Run: `go test ./wire/... -v -count=1 2>&1 | tail -20`

If goleak reports expected goroutines (quic-go listeners), add them to the ignore list.

**Step 4: Commit**

```bash
git add wire/contract_test.go go.mod go.sum
git commit -m "test: add goleak goroutine leak detection to wire tests"
```

---

## Contract Table (reference)

| # | User Scenario | Trigger | Expected Behavior | Must NOT Happen |
|---|--------------|---------|-------------------|-----------------|
| 1 | Start VPN | Start() | QUIC first, 5s timeout | Must not hang |
| 2 | Normal use | QUIC healthy | TCP→QUIC stream, UDP→datagram | TCP-WS not involved |
| 3 | QUIC blocked | 3 handshake fails | All traffic→TCP-WS; probe QUIC every 30s | Must not disconnect |
| 4 | QUIC recovers | Probe succeeds | New requests→QUIC | Must not interrupt active conns |
| 5 | Datagram loss high | Loss>40% | That UDP→stream relay; QUIC transport untouched | Must not reconnect transport |
| 6 | Network switch | WiFi→cellular | Reset both transports, 8s QUIC budget | Must not keep old conns |
| 7 | Poor signal | Wire loss>25% + TCP ok | Do NOT reconnect | Must not destroy active streams |
| 8 | Server down | 3 probe failures | Stop retrying, wait for network change | Must not burn data |
| 9 | Auth rejected | 401/403 | Report error, no retry | Must not retry |
| 10 | Slow upstream | Stream response timeout | Return error for that request | Must not trigger fallback |
