# Transport Happy Eyeballs Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Race three transport candidates (QUIC-443, QUIC-hop, TCP-WS) with echo probe verification, replacing serial handshake-only validation.

**Architecture:** Wire layer owns transport construction and racing (`wire/race.go`, `wire/echo.go`). Engine calls `wire.RaceTransport()` at step 4/4.5. Runtime re-race triggered by echo failure in healthMonitor, executed via channel in engine. Replaces `recoveryProbe`.

**Tech Stack:** Go, quic-go, safego (panic recovery), deadlock mutex

**Spec:** `docs/plans/2026-03-19-transport-happy-eyeballs-design.md`

---

## File Structure

| File | Action | Responsibility |
|------|--------|---------------|
| `wire/stream.go` | Modify | Add `StreamTypeEcho = 0x04` constant |
| `wire/echo.go` | Create | `EchoProbe()` client function, echo constants |
| `wire/echo_test.go` | Create | Echo probe unit tests |
| `wire/race.go` | Create | `RaceTransport()`, candidate builders, stagger logic |
| `wire/race_test.go` | Create | Race unit tests (mock transports) |
| `wire/transport.go` | Modify | Rename `quic`→`primary`, add `primaryType`, `ReplacePrimary()`, `SetPrimary()` |
| `wire/transport_test.go` | Modify | Update tests for primary/tcpws naming |
| `wire/quic.go:1019-1036` | Modify | Add `StreamTypeEcho` case in `handleStream` |
| `engine/engine.go:161-211` | Modify | Replace step 4/4.5 with `wire.RaceTransport()` |
| `engine/engine.go:340-360` | Modify | Replace recoveryProbe with reRaceLoop, add echoLoop to healthMonitor |
| `engine/health.go` | Modify | Add `echoLoop()` goroutine, `reRaceChan`, atomic counter |
| `engine/recovery_probe.go` | Delete | Superseded by re-race |
| `engine/recovery_probe_test.go` | Delete | Tests for removed file |

---

### Task 1: Echo Probe Protocol — Stream Type Constant

**Files:**
- Modify: `wire/stream.go:16-20`

- [ ] **Step 1: Add StreamTypeEcho constant**

In `wire/stream.go`, after `StreamTypeUDPOverflow`:

```go
const (
	StreamTypeTCP         byte = 0x01
	StreamTypeUDP         byte = 0x02
	StreamTypeUDPOverflow byte = 0x03
	StreamTypeEcho        byte = 0x04
)
```

- [ ] **Step 2: Run existing tests to verify no breakage**

Run: `cd k2 && go test ./wire/... -short -count=1`
Expected: PASS (new constant is additive)

- [ ] **Step 3: Commit**

```bash
git add k2/wire/stream.go
git commit -m "wire: add StreamTypeEcho (0x04) constant for data-path verification"
```

---

### Task 2: Echo Probe — Client Implementation

**Files:**
- Create: `wire/echo.go`
- Create: `wire/echo_test.go`

- [ ] **Step 1: Write the failing test**

Create `wire/echo_test.go`:

```go
package wire

import (
	"context"
	"testing"
	"time"

	quic "github.com/apernet/quic-go"
)

func TestEchoProbe_Success(t *testing.T) {
	// Set up a QUIC server+client pair using test helpers.
	serverTLS, clientTLS := testTLSConfigs(t)
	serverTransport, serverAddr := testQUICTransport(t)
	clientTransport, _ := testQUICTransport(t)

	ln, err := serverTransport.Listen(serverTLS, DefaultQUICConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	// Server: accept stream, handle echo.
	go func() {
		conn, err := ln.Accept(context.Background())
		if err != nil {
			return
		}
		stream, err := conn.AcceptStream(context.Background())
		if err != nil {
			return
		}
		HandleEchoStream(stream)
	}()

	// Client: dial and probe.
	conn, err := clientTransport.Dial(context.Background(), serverAddr, clientTLS, DefaultQUICConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseWithError(0, "")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	rtt, err := EchoProbe(ctx, conn)
	if err != nil {
		t.Fatalf("EchoProbe failed: %v", err)
	}
	if rtt <= 0 {
		t.Errorf("expected positive RTT, got %v", rtt)
	}
}

func TestEchoProbe_Timeout(t *testing.T) {
	// Server that accepts but never responds.
	serverTLS, clientTLS := testTLSConfigs(t)
	serverTransport, serverAddr := testQUICTransport(t)
	clientTransport, _ := testQUICTransport(t)

	ln, err := serverTransport.Listen(serverTLS, DefaultQUICConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	go func() {
		conn, _ := ln.Accept(context.Background())
		stream, _ := conn.AcceptStream(context.Background())
		// Read but don't respond — simulate blocked Short Header path.
		_ = stream
	}()

	conn, err := clientTransport.Dial(context.Background(), serverAddr, clientTLS, DefaultQUICConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseWithError(0, "")

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	_, err = EchoProbe(ctx, conn)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
}

func TestEchoProbe_NonceMismatch(t *testing.T) {
	serverTLS, clientTLS := testTLSConfigs(t)
	serverTransport, serverAddr := testQUICTransport(t)
	clientTransport, _ := testQUICTransport(t)

	ln, err := serverTransport.Listen(serverTLS, DefaultQUICConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	// Server: respond with wrong nonce.
	go func() {
		conn, _ := ln.Accept(context.Background())
		stream, _ := conn.AcceptStream(context.Background())
		br := make([]byte, 100)
		stream.Read(br) // consume header + nonce
		stream.Write([]byte{0, 0, 0, 0, 0, 0, 0, 0}) // wrong nonce
		stream.Close()
	}()

	conn, err := clientTransport.Dial(context.Background(), serverAddr, clientTLS, DefaultQUICConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseWithError(0, "")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err = EchoProbe(ctx, conn)
	if err == nil {
		t.Fatal("expected nonce mismatch error, got nil")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd k2 && go test ./wire/ -run TestEchoProbe -v -count=1`
Expected: FAIL — `EchoProbe` and `HandleEchoStream` not defined

- [ ] **Step 3: Implement echo.go**

Create `wire/echo.go`:

```go
package wire

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	quic "github.com/apernet/quic-go"
)

const echoNonceLen = 8

// EchoProbe verifies the QUIC data path (Short Header packets) by opening a
// stream, sending a random nonce, and reading the echo response. Returns the
// round-trip time on success.
//
// This catches the failure mode where QUIC handshake (Long Header) succeeds
// but Short Header packets are dropped by middleboxes.
func EchoProbe(ctx context.Context, conn *quic.Conn) (time.Duration, error) {
	start := time.Now()

	stream, err := conn.OpenStreamSync(ctx)
	if err != nil {
		return 0, fmt.Errorf("echo: open stream: %w", err)
	}
	defer func() {
		stream.CancelRead(0)
		stream.Close()
	}()

	// Write stream header: H3 frame type + StreamTypeEcho + empty addr.
	if err := WriteStreamHeader(&stream, StreamTypeEcho, ""); err != nil {
		return 0, fmt.Errorf("echo: write header: %w", err)
	}

	// Write random nonce.
	var nonce [echoNonceLen]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return 0, fmt.Errorf("echo: generate nonce: %w", err)
	}
	if _, err := stream.Write(nonce[:]); err != nil {
		return 0, fmt.Errorf("echo: write nonce: %w", err)
	}

	// Read echo response.
	var resp [echoNonceLen]byte
	if _, err := io.ReadFull(&stream, resp[:]); err != nil {
		return 0, fmt.Errorf("echo: read response: %w", err)
	}

	if nonce != resp {
		return 0, errors.New("echo: nonce mismatch")
	}

	rtt := time.Since(start)
	slog.Debug("DIAG: echo-probe-ok", "rttMs", rtt.Milliseconds())
	return rtt, nil
}

// HandleEchoStream handles a server-side echo probe stream.
// Reads echoNonceLen bytes and writes them back.
func HandleEchoStream(stream *quic.Stream) {
	defer func() {
		(*stream).CancelRead(0)
		(*stream).Close()
	}()

	var buf [echoNonceLen]byte
	if _, err := io.ReadFull(stream, buf[:]); err != nil {
		slog.Debug("echo: read failed", "err", err)
		return
	}
	if _, err := (*stream).Write(buf[:]); err != nil {
		slog.Debug("echo: write failed", "err", err)
		return
	}
}
```

**quic.Stream type note**: In apernet/quic-go, `quic.Stream` is an interface (not a struct). `OpenStreamSync` returns `quic.Stream` (interface value). `EchoProbe` should pass `stream` (not `&stream`) to `WriteStreamHeader` and `io.ReadFull` — both accept interfaces directly. `HandleEchoStream` receives `quic.Stream` (not `*quic.Stream`) and calls methods directly on it. The existing `handleStream` in `quic.go` passes `*quic.Stream` but the pattern should be adapted to pass the interface value. Fix these during implementation:
- `WriteStreamHeader(stream, ...)` not `WriteStreamHeader(&stream, ...)`
- `io.ReadFull(stream, resp[:])` not `io.ReadFull(&stream, resp[:])`
- `HandleEchoStream(stream quic.Stream)` not `HandleEchoStream(stream *quic.Stream)`

**Test helper note**: Use existing `setupQUICPair(t)` from `wire/quic_test.go:151` which returns `(*QUICClient, *QUICServer, func())`. For echo tests, the server's `handleStream` dispatches echo after Task 3. For Task 2 tests (before server support), use raw QUIC listener + manual echo goroutine as shown.

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd k2 && go test ./wire/ -run TestEchoProbe -v -count=1`
Expected: 3 PASS

- [ ] **Step 5: Commit**

```bash
git add k2/wire/echo.go k2/wire/echo_test.go
git commit -m "wire: add EchoProbe + HandleEchoStream for data-path verification"
```

---

### Task 3: Server — Echo Stream Handler

**Files:**
- Modify: `wire/quic.go:1019-1036` (handleStream switch)

- [ ] **Step 1: Write the integration test**

Add to existing `wire/integration_test.go` or `wire/quic_test.go`:

```go
func TestQUICServer_EchoStream(t *testing.T) {
	// Use existing test server setup pattern from integration_test.go.
	// Connect client, call EchoProbe, verify success.
	env := setupQUICTestEnv(t) // use existing helper
	defer env.cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	conn, err := env.clientConn(ctx)
	if err != nil {
		t.Fatal(err)
	}

	rtt, err := EchoProbe(ctx, conn)
	if err != nil {
		t.Fatalf("EchoProbe through server failed: %v", err)
	}
	t.Logf("echo RTT: %v", rtt)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd k2 && go test ./wire/ -run TestQUICServer_EchoStream -v -count=1`
Expected: FAIL — server returns "unknown stream type" for 0x04

- [ ] **Step 3: Add echo case to handleStream**

In `wire/quic.go`, in the `handleStream` method at line 1019, add the echo case:

```go
	switch streamType {
	case StreamTypeTCP:
		sc := &quicStreamConn{
			Stream: stream,
			local:  qconn.LocalAddr(),
			remote: qconn.RemoteAddr(),
			br:     br,
		}
		s.sendTCP(tcpAcceptResult{conn: sc, addr: addr})
		slog.Debug("QUICServer.handleStream: TCP queued", "addr", addr)
	case StreamTypeUDP:
		s.handleUDPStream(stream, br)
	case StreamTypeUDPOverflow:
		s.handleUDPOverflowStream(br, state)
	case StreamTypeEcho:
		HandleEchoStream(stream)
	default:
		slog.Debug("QUICServer.handleStream: unknown stream type", "streamType", streamType)
		stream.Close()
	}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd k2 && go test ./wire/ -run TestQUICServer_EchoStream -v -count=1`
Expected: PASS

- [ ] **Step 5: Run full wire test suite**

Run: `cd k2 && go test ./wire/... -short -count=1`
Expected: All PASS

- [ ] **Step 6: Commit**

```bash
git add k2/wire/quic.go
git commit -m "wire: handle StreamTypeEcho in QUICServer.handleStream"
```

---

### Task 4: TransportManager — Rename quic→primary, Add ReplacePrimary

**Files:**
- Modify: `wire/transport.go`
- Modify: `wire/transport_test.go`

- [ ] **Step 1: Run existing transport tests as baseline**

Run: `cd k2 && go test ./wire/ -run TestTransport -v -count=1`
Expected: All PASS (baseline)

- [ ] **Step 2: Rename `quic` field to `primary` in TransportManager**

In `wire/transport.go`, change the struct and all internal references:

```go
type TransportManager struct {
	mu    deadlock.RWMutex
	primary     Dialer  // was: quic
	tcpws       Dialer
	primaryType string  // "quic-443", "quic-hop", "tcpws"

	fallbackActive  atomic.Bool
	freshAfterReset atomic.Bool
	probeCancel     context.CancelFunc
}
```

Rename `SetQUIC` → `SetPrimary`:

```go
func (tm *TransportManager) SetPrimary(d Dialer, transportType string) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.primary = d
	tm.primaryType = transportType
}
```

Update all internal references: `tm.quic` → `tm.primary` throughout the file. Keep `SetTCPWS` unchanged.

- [ ] **Step 3: Add ReplacePrimary method**

```go
// ReplacePrimary hot-swaps the primary transport during runtime re-race.
// Closes old primary outside lock. In-flight DialTCP/DialUDP on the old
// primary will get connection errors (same as ResetConnections behavior).
func (tm *TransportManager) ReplacePrimary(d Dialer, transportType string) {
	tm.mu.Lock()
	old := tm.primary
	tm.primary = d
	tm.primaryType = transportType
	tm.fallbackActive.Store(false)
	tm.mu.Unlock()

	// Close old primary outside lock (Rule #4).
	// Check pointer equality: if old == tcpws, don't close (tcpws still alive).
	if old != nil {
		tm.mu.RLock()
		isTCPWS := old == tm.tcpws
		tm.mu.RUnlock()
		if !isTCPWS {
			old.Close()
		}
	}

	slog.Info("wire: primary transport replaced",
		"type", transportType,
		"hadOld", old != nil,
	)
}
```

- [ ] **Step 4: Update Close() for double-close safety**

```go
func (tm *TransportManager) Close() error {
	if tm.probeCancel != nil {
		tm.probeCancel()
		tm.probeCancel = nil
	}

	tm.mu.Lock()
	primary := tm.primary
	tm.primary = nil
	tcpws := tm.tcpws
	tm.tcpws = nil
	tm.mu.Unlock()

	var firstErr error
	// Avoid double-close when primary == tcpws (tcpws won the race).
	if primary != nil && primary != tcpws {
		if err := primary.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if tcpws != nil {
		if err := tcpws.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
```

- [ ] **Step 5: Update engine.go to use new API**

In `engine/engine.go`, update step 4 (lines 161-181) to use `SetPrimary` instead of `SetQUIC`:

```go
tm := wire.NewTransportManager()
switch wireCfg.Transport {
case "quic":
	tm.SetPrimary(quicClient, "quic-hop")
case "tcpws":
	tm.SetTCPWS(tcpwsClient)
default:
	tm.SetPrimary(quicClient, "quic-hop")
	tm.SetTCPWS(tcpwsClient)
}
```

- [ ] **Step 6: Update transport_test.go**

Replace `SetQUIC` → `SetPrimary` in all test files. Search and replace.

- [ ] **Step 7: Run full test suite**

Run: `cd k2 && go test ./wire/... ./engine/... -short -count=1`
Expected: All PASS

- [ ] **Step 8: Commit**

```bash
git add k2/wire/transport.go k2/wire/transport_test.go k2/engine/engine.go
git commit -m "wire: rename quic→primary in TransportManager, add ReplacePrimary"
```

---

### Task 5: RaceTransport — Three-Way Happy Eyeballs

**Files:**
- Create: `wire/race.go`
- Create: `wire/race_test.go`

- [ ] **Step 1: Write the failing test — winner selection**

Create `wire/race_test.go`:

```go
package wire

import (
	"context"
	"testing"
	"time"
)

func TestRaceTransport_QUIC443Wins(t *testing.T) {
	// Create a test config with a QUIC server that supports echo.
	env := setupRaceTestEnv(t) // helper that creates server + returns Config
	defer env.cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := RaceTransport(ctx, env.cfg, env.dialAddr, nil)
	if err != nil {
		t.Fatalf("RaceTransport failed: %v", err)
	}
	defer result.Dialer.Close()

	if result.TransportType != "quic-443" {
		t.Errorf("expected quic-443 winner, got %s", result.TransportType)
	}
}

func TestRaceTransport_FallbackToTCPWS(t *testing.T) {
	// Config with invalid QUIC port (both 443 and hop will fail),
	// but valid TCP-WS.
	env := setupRaceTestEnv(t)
	defer env.cleanup()

	// Override config to make QUIC fail.
	cfg := env.cfg
	cfg.Port = 1 // invalid port, QUIC will fail
	cfg.HopStart = 2
	cfg.HopEnd = 3

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := RaceTransport(ctx, cfg, env.dialAddr, nil)
	if err != nil {
		t.Fatalf("RaceTransport failed: %v", err)
	}
	defer result.Dialer.Close()

	if result.TransportType != "tcpws" {
		t.Errorf("expected tcpws winner, got %s", result.TransportType)
	}
}

func TestRaceTransport_AllFail(t *testing.T) {
	cfg := Config{
		Host: "127.0.0.1",
		Port: 1, // nothing listening
		HopStart: 2,
		HopEnd: 3,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()

	_, err := RaceTransport(ctx, cfg, "127.0.0.1", nil)
	if err == nil {
		t.Fatal("expected error when all candidates fail")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd k2 && go test ./wire/ -run TestRaceTransport -v -count=1`
Expected: FAIL — `RaceTransport` not defined

- [ ] **Step 3: Implement race.go**

Create `wire/race.go`:

```go
package wire

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/kaitu-io/k2/safego"
)

const (
	raceTimeout       = 5 * time.Second
	staggerQUICHop    = 300 * time.Millisecond
	staggerTCPWS      = 800 * time.Millisecond
	echoTimeoutRace   = 1 * time.Second
)

// RaceResult is the output of a successful transport race.
type RaceResult struct {
	Dialer        Dialer
	TransportType string // "quic-443", "quic-hop", "tcpws"
	TCPWS         Dialer // always-available TCP-WS fallback (nil if tcpws won)
}

type raceCandidate struct {
	dialer        Dialer
	tcpws         Dialer // non-nil only for tcpws candidate
	transportType string
	err           error
}

// RaceTransport races three transport candidates and returns the first one
// where data can actually flow (verified by echo probe for QUIC).
//
// Priority order with staggered start:
//   t=0ms:   QUIC on port 443 (no hop) + echo probe
//   t=300ms: QUIC on hop ports + echo probe
//   t=800ms: TCP-WS on port 443 (no echo needed)
//
// directDialer may be nil (non-desktop platforms).
func RaceTransport(ctx context.Context, cfg Config, dialAddr string, directDialer DirectDialer) (*RaceResult, error) {
	raceStart := time.Now()
	raceCtx, raceCancel := context.WithTimeout(ctx, raceTimeout)

	results := make(chan raceCandidate, 3)
	var wg sync.WaitGroup
	var spawned int
	var winner *raceCandidate

	slog.Info("DIAG: transport-race-start", "candidates", 3)

	launch := func(name string, fn func(context.Context) raceCandidate) {
		spawned++
		wg.Add(1)
		safego.Go(func() {
			defer wg.Done()
			results <- fn(raceCtx)
		})
	}

	// Cleanup: cancel losers, wait for goroutines, close loser dialers.
	defer func() {
		raceCancel()
		go func() {
			wg.Wait()
			close(results)
			for r := range results {
				if r.err == nil && r.dialer != nil && (winner == nil || r.dialer != winner.dialer) {
					r.dialer.Close()
				}
				if r.tcpws != nil && (winner == nil || r.tcpws != winner.tcpws) {
					r.tcpws.Close()
				}
			}
		}()
	}()

	// --- Candidate 1: QUIC on port 443 (no hop) ---
	launch("quic-443", func(ctx context.Context) raceCandidate {
		return buildAndProbeQUIC(ctx, cfg, dialAddr, directDialer, cfg.Port, 0, 0, "quic-443")
	})

	var consumed int

	// --- Stagger: wait 300ms or early result ---
	if w := waitForWinner(results, staggerQUICHop-time.Since(raceStart), &consumed); w != nil {
		winner = w
		slog.Info("DIAG: transport-race-winner", "winner", w.transportType, "winnerMs", time.Since(raceStart).Milliseconds(), "tried", 1)
		return &RaceResult{Dialer: w.dialer, TransportType: w.transportType}, nil
	}

	// --- Candidate 2: QUIC on hop ports ---
	if cfg.HopStart > 0 && cfg.HopEnd > 0 {
		launch("quic-hop", func(ctx context.Context) raceCandidate {
			return buildAndProbeQUIC(ctx, cfg, dialAddr, directDialer, cfg.Port, cfg.HopStart, cfg.HopEnd, "quic-hop")
		})
	}

	// --- Stagger: wait until 800ms from race start ---
	remaining := staggerTCPWS - time.Since(raceStart)
	if w := waitForWinner(results, remaining, &consumed); w != nil {
		winner = w
		slog.Info("DIAG: transport-race-winner", "winner", w.transportType, "winnerMs", time.Since(raceStart).Milliseconds(), "tried", spawned)
		return &RaceResult{Dialer: w.dialer, TransportType: w.transportType}, nil
	}

	// --- Candidate 3: TCP-WS ---
	launch("tcpws", func(ctx context.Context) raceCandidate {
		return buildTCPWSCandidate(ctx, cfg, dialAddr, directDialer)
	})

	// --- Collect remaining results ---
	var errors [3]error
	for i := consumed; i < spawned; i++ {
		select {
		case r := <-results:
			if r.err == nil {
				winner = &r
				tried := i + 1
				slog.Info("DIAG: transport-race-winner", "winner", r.transportType, "winnerMs", time.Since(raceStart).Milliseconds(), "tried", tried)
				res := &RaceResult{Dialer: r.dialer, TransportType: r.transportType, TCPWS: r.tcpws}
				return res, nil
			}
			switch r.transportType {
			case "quic-443":
				errors[0] = r.err
			case "quic-hop":
				errors[1] = r.err
			case "tcpws":
				errors[2] = r.err
			}
		case <-raceCtx.Done():
			slog.Warn("DIAG: transport-race-fail",
				"quic443Err", errors[0],
				"quicHopErr", errors[1],
				"tcpwsErr", errors[2],
			)
			return nil, fmt.Errorf("transport race: all candidates failed or timed out")
		}
	}

	slog.Warn("DIAG: transport-race-fail",
		"quic443Err", errors[0],
		"quicHopErr", errors[1],
		"tcpwsErr", errors[2],
	)
	return nil, fmt.Errorf("transport race: quic-443: %v; quic-hop: %v; tcpws: %v", errors[0], errors[1], errors[2])
}

func buildAndProbeQUIC(ctx context.Context, cfg Config, dialAddr string, dd DirectDialer, port, hopStart, hopEnd int, name string) raceCandidate {
	// Clone config with port/hop overrides.
	qcfg := cfg
	qcfg.Port = port
	qcfg.HopStart = hopStart
	qcfg.HopEnd = hopEnd
	if dialAddr != "" {
		qcfg.DialAddr = dialAddr
	}

	qc := NewQUICClient(qcfg, nil)
	if dd != nil {
		qc.SetDirectDialer(dd)
	}

	// Step 1: QUIC handshake.
	conn, err := qc.connect(ctx)
	if err != nil {
		qc.Close()
		slog.Warn("DIAG: echo-probe-fail", "transport", name, "err", fmt.Sprintf("handshake: %v", err))
		return raceCandidate{transportType: name, err: err}
	}

	// Step 2: Echo probe (verify Short Header data path).
	echoCtx, echoCancel := context.WithTimeout(ctx, echoTimeoutRace)
	defer echoCancel()

	rtt, err := EchoProbe(echoCtx, conn)
	if err != nil {
		// Distinguish "old server doesn't support echo" from "path is blocked".
		// Old server returns stream error (unknown type) — treat as degraded win.
		// Blocked path returns timeout — treat as failure.
		if isEchoUnsupported(err) {
			slog.Warn("DIAG: echo-probe-unsupported", "transport", name)
			// Backward compat: handshake succeeded, echo unsupported → accept as winner.
			return raceCandidate{dialer: qc, transportType: name}
		}
		slog.Warn("DIAG: echo-probe-fail", "transport", name, "err", err)
		qc.Close()
		return raceCandidate{transportType: name, err: fmt.Errorf("echo probe: %w", err)}
	}

	slog.Debug("DIAG: echo-probe-ok", "transport", name, "rttMs", rtt.Milliseconds())
	return raceCandidate{dialer: qc, transportType: name}
}

func buildTCPWSCandidate(ctx context.Context, cfg Config, dialAddr string, dd DirectDialer) raceCandidate {
	tcfg := cfg
	if dialAddr != "" {
		tcfg.DialAddr = dialAddr
	}

	tc := NewTCPWSClient(tcfg, nil)
	if dd != nil {
		tc.SetDirectDialer(dd)
	}

	// TCP-WS: connect() verifies data path (TCP handshake + smux setup).
	_, err := tc.connect(ctx)
	if err != nil {
		tc.Close()
		return raceCandidate{transportType: "tcpws", err: err}
	}

	return raceCandidate{dialer: tc, tcpws: tc, transportType: "tcpws"}
}

// waitForWinner waits up to duration for a successful result on the channel.
// Returns the winner if found, nil if timeout or only failures received.
// consumed is incremented for each result read from the channel (caller must
// track this to avoid double-reading in the final collection loop).
func waitForWinner(results chan raceCandidate, d time.Duration, consumed *int) *raceCandidate {
	if d <= 0 {
		// Check non-blocking.
		select {
		case r := <-results:
			*consumed++
			if r.err == nil {
				return &r
			}
		default:
		}
		return nil
	}

	timer := time.NewTimer(d)
	defer timer.Stop()

	for {
		select {
		case r := <-results:
			*consumed++
			if r.err == nil {
				return &r
			}
			// Failed result — keep waiting for timer or next result.
			continue
		case <-timer.C:
			return nil
		}
	}
}
```

- [ ] **Step 4: Run tests**

Run: `cd k2 && go test ./wire/ -run TestRaceTransport -v -count=1 -timeout 30s`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add k2/wire/race.go k2/wire/race_test.go
git commit -m "wire: add RaceTransport three-way Happy Eyeballs with echo probe"
```

---

### Task 6: Engine — Replace Step 4/4.5 With RaceTransport

**Files:**
- Modify: `engine/engine.go:161-211`

- [ ] **Step 1: Replace step 4 + 4.5 with RaceTransport call**

Replace lines 161-211 in `engine/engine.go` with:

```go
	// 4. Transport race: three-way Happy Eyeballs.
	// Races QUIC-443, QUIC-hop, TCP-WS. First with verified data flow wins.
	slog.Info("engine: step 4 - transport race", "elapsed", time.Since(startTime))
	raceCtx, raceCancel := context.WithTimeout(ctx, 15*time.Second)
	raceResult, raceErr := wire.RaceTransport(raceCtx, wireCfg, wireCfg.DialAddr, cfg.DirectDialer)
	raceCancel()

	if raceErr != nil {
		slog.Warn("engine: transport race failed", "err", raceErr)
		if cfg.NetworkMonitor != nil {
			cfg.NetworkMonitor.Close()
		}
		return e.fail(fmt.Errorf("transport race: %w", raceErr))
	}

	tm := wire.NewTransportManager()
	tm.SetPrimary(raceResult.Dialer, raceResult.TransportType)
	if raceResult.TCPWS != nil {
		tm.SetTCPWS(raceResult.TCPWS)
	} else if raceResult.TransportType != "tcpws" {
		// Build a standalone TCP-WS client as fallback.
		tcpwsClient := wire.NewTCPWSClient(wireCfg, nil)
		if cfg.DirectDialer != nil {
			tcpwsClient.SetDirectDialer(cfg.DirectDialer)
		}
		tm.SetTCPWS(tcpwsClient)
	}

	slog.Info("engine: step 4 - transport race winner",
		"transport", raceResult.TransportType,
		"elapsed", time.Since(startTime),
	)
	slog.Info("DIAG: wire-handshake",
		"handshakeMs", time.Since(startTime).Milliseconds(),
		"transport", raceResult.TransportType,
	)
```

- [ ] **Step 2: Run engine tests**

Run: `cd k2 && go test ./engine/... -short -count=1`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add k2/engine/engine.go
git commit -m "engine: replace handshake probe with wire.RaceTransport at step 4"
```

---

### Task 7: Remove recoveryProbe, Add echoLoop + reRaceLoop

**Files:**
- Delete: `engine/recovery_probe.go`
- Delete: `engine/recovery_probe_test.go`
- Modify: `engine/health.go`
- Modify: `engine/engine.go:340-360`

- [ ] **Step 1: Delete recovery_probe.go and its test**

```bash
cd k2 && git rm engine/recovery_probe.go engine/recovery_probe_test.go
```

- [ ] **Step 2: Remove recoveryProbe references from engine.go**

In `engine/engine.go`:
- Remove `probe *recoveryProbe` field from Engine struct
- Remove `e.probe = newRecoveryProbe(tm, e)` from step 9
- Remove `probe.stop()` from `Stop()`
- Remove `probe.trigger()` from `reconnect()`
- Remove netCoordinator's probe stop callback

- [ ] **Step 3: Add reRaceChan and reRaceLoop to Engine**

Add to Engine struct:

```go
type Engine struct {
	// ... existing fields ...
	wireCfg      wire.Config          // stored at Start() for re-race
	directDialer wire.DirectDialer    // stored at Start() for re-race
	reRaceChan   chan struct{}
	reRaceCancel context.CancelFunc
}
```

Add `reRaceLoop`:

```go
func (e *Engine) reRaceLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-e.reRaceChan:
		}

		// Read current state under lock. wireCfg and directDialer are stored
		// in Engine during Start() to avoid re-parsing URL.
		e.mu.Lock()
		if e.state != StateConnected {
			e.mu.Unlock()
			continue
		}
		tm := e.tm
		wireCfg := e.wireCfg
		dd := e.directDialer
		e.mu.Unlock()

		if tm == nil {
			continue
		}

		slog.Warn("DIAG: transport-rerace", "reason", "echo-consecutive-fail", "previousTransport", tm.PrimaryType())

		raceCtx, raceCancel := context.WithTimeout(ctx, 15*time.Second)
		result, err := wire.RaceTransport(raceCtx, wireCfg, wireCfg.DialAddr, dd)
		raceCancel()

		if err != nil {
			slog.Warn("engine: re-race failed", "err", err)
			continue
		}

		// Check engine still connected before swapping.
		e.mu.Lock()
		currentTM := e.tm
		e.mu.Unlock()
		if currentTM == nil {
			// Engine stopped during race.
			result.Dialer.Close()
			continue
		}

		if ctx.Err() != nil {
			// Context cancelled (engine stopping).
			result.Dialer.Close()
			return
		}

		currentTM.ReplacePrimary(result.Dialer, result.TransportType)
	}
}
```

- [ ] **Step 4: Add echoLoop to healthMonitor**

In `engine/health.go`, add to `healthMonitor` struct:

```go
type healthMonitor struct {
	// ... existing fields ...
	echoFails   atomic.Int32
	reRaceChan  chan struct{} // non-nil: engine's re-race channel
}
```

Add `echoLoop`:

```go
func (hm *healthMonitor) echoLoop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		// healthMonitor already holds tm reference — use it directly.
		primary := hm.tm.Primary()
		if primary == nil {
			continue
		}

		// Use optional capability interface (layer boundary safe).
		// wire.EchoProber is implemented by QUICClient, not by TCPWSClient.
		ep, ok := primary.(wire.EchoProber)
		if !ok {
			// TCP-WS primary: no echo needed (TCP verifies data path).
			hm.echoFails.Store(0)
			continue
		}

		echoCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		err := ep.RunEchoProbe(echoCtx)
		cancel()

		if err != nil {
			fails := hm.echoFails.Add(1)
			if fails >= 3 && hm.reRaceChan != nil {
				select {
				case hm.reRaceChan <- struct{}{}:
				default: // race already pending
				}
				hm.echoFails.Store(0) // reset after triggering
			}
		} else {
			hm.echoFails.Store(0)
		}
	}
}
```

Note: `EchoProber` is a new optional capability interface in `wire/`. Add to `wire/echo.go`:

```go
// EchoProber is an optional capability interface for transports that need
// data-path verification (QUIC). TCPWSClient does not implement this.
// Engine type-asserts on this interface (not concrete type) per layer boundary rules.
type EchoProber interface {
	RunEchoProbe(ctx context.Context) error
}
```

`QUICClient` implements it:

```go
func (c *QUICClient) RunEchoProbe(ctx context.Context) error {
	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()
	if conn == nil {
		return errors.New("no cached connection")
	}
	_, err := EchoProbe(ctx, conn)
	return err
}
```

Add `EchoProber` to the Registered Optional Interfaces table in `k2/CLAUDE.md`.

- [ ] **Step 5: Wire up in engine.go step 9**

Replace recoveryProbe creation with reRaceLoop + echoLoop:

```go
	// Re-race loop: replaces recoveryProbe.
	e.reRaceChan = make(chan struct{}, 1)
	reRaceCtx, reRaceCancel := context.WithCancel(ctx)
	e.reRaceCancel = reRaceCancel
	safego.Go(func() { e.reRaceLoop(reRaceCtx) })

	// Store wireCfg + directDialer for re-race (avoids re-parsing URL).
	e.wireCfg = wireCfg
	e.directDialer = cfg.DirectDialer

	// Start health monitor with echo loop.
	hm := newHealthMonitor(tm, tunnel, cfg.HealthCallback, func() { e.onHealthCritical() }, time.Now())
	hm.reRaceChan = e.reRaceChan
	hm.start() // starts sample loop + echoLoop
	e.health = hm
```

- [ ] **Step 6: Update hm.start() to launch echoLoop**

In `health.go`, modify `start()`:

```go
func (hm *healthMonitor) start() {
	hm.wg.Add(2) // was 1
	safego.Go(func() {
		defer hm.wg.Done()
		// existing sample loop
		...
	})
	safego.Go(func() {
		defer hm.wg.Done()
		hm.echoLoop(/* ctx from done channel */)
	})
}
```

- [ ] **Step 7: Update Stop() to cancel reRaceLoop**

In `engine.go Stop()`, after extracting hm and before tm.Close():

```go
	reRaceCancel := e.reRaceCancel
	e.reRaceCancel = nil
	e.mu.Unlock()

	if hm != nil {
		hm.stop() // stops echoLoop + sample loop
	}
	if reRaceCancel != nil {
		reRaceCancel() // stops reRaceLoop
	}
```

- [ ] **Step 8: Add Primary() method to TransportManager**

In `wire/transport.go`:

```go
func (tm *TransportManager) Primary() Dialer {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	return tm.primary
}

func (tm *TransportManager) PrimaryType() string {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	return tm.primaryType
}
```

- [ ] **Step 9: Run full test suite**

Run: `cd k2 && go test ./... -short -count=1 -timeout 120s`
Expected: All PASS (recovery_probe tests removed, no compilation errors)

- [ ] **Step 10: Commit**

```bash
git add -A k2/engine/ k2/wire/transport.go
git commit -m "engine: replace recoveryProbe with echoLoop + reRaceLoop

Three-way transport re-race triggered by consecutive echo probe failures.
Decoupled from healthMonitor.sample() via atomic counter + channel.
No lock nesting between echoLoop, reRaceLoop, and engine.Stop()."
```

---

### Task 8: Contract Tests — End-to-End Verification

**Files:**
- Create: `wire/race_contract_test.go`

- [ ] **Step 1: Write contract test — echo probe detects blocked path**

```go
func TestContract_EchoDetectsBlockedShortHeader(t *testing.T) {
	// Set up a server that accepts QUIC handshake but never processes streams.
	// This simulates a middlebox that passes Long Header but drops Short Header.
	// Verify that EchoProbe returns error (timeout).
	// Verify that RaceTransport falls back to next candidate.
}
```

- [ ] **Step 2: Write contract test — re-race on primary failure**

```go
func TestContract_ReRaceOnPrimaryFailure(t *testing.T) {
	// Set up engine with QUIC-443 as primary.
	// After connection, make QUIC-443 echo fail (close the server).
	// Verify echoLoop triggers re-race.
	// Verify primary switches to next available transport.
}
```

- [ ] **Step 3: Write contract test — backward compat with old server**

```go
func TestContract_OldServerNoEcho(t *testing.T) {
	// Set up server WITHOUT echo handler (old server).
	// Verify RaceTransport still succeeds (degrades to handshake-only).
	// Verify DIAG: echo-probe-unsupported is logged.
}
```

- [ ] **Step 4: Run contract tests**

Run: `cd k2 && go test ./wire/ -run TestContract -v -count=1 -timeout 60s`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add k2/wire/race_contract_test.go
git commit -m "wire: add contract tests for transport race and echo probe"
```

---

### Task 9: Update k2/CLAUDE.md Constitutional Tables

**Files:**
- Modify: `k2/CLAUDE.md`

- [ ] **Step 1: Remove recoveryProbe.mu from Lock Ordering Graph**

In `k2/CLAUDE.md`, remove `→ recoveryProbe.mu (state)` from the lock ordering graph.

- [ ] **Step 2: Add EchoProber to Registered Optional Interfaces table**

```
| EchoProber | wire | QUICClient | engine (echo health probe) |
```

- [ ] **Step 3: Add new DIAG events to reserved event names table**

```
| `DIAG: transport-race-start` | INFO | race begins | candidates |
| `DIAG: transport-race-winner` | INFO | race complete | winner, winnerMs, tried |
| `DIAG: transport-race-fail` | WARN | all candidates failed | quic443Err, quicHopErr, tcpwsErr |
| `DIAG: echo-probe-ok` | DEBUG | echo succeeded | transport, rttMs |
| `DIAG: echo-probe-fail` | WARN | echo failed | transport, err |
| `DIAG: echo-probe-unsupported` | WARN | server doesn't support echo | transport |
| `DIAG: transport-rerace` | WARN | runtime re-race | reason, previousTransport |
```

- [ ] **Step 4: Commit**

```bash
git add k2/CLAUDE.md
git commit -m "docs: update k2/CLAUDE.md lock ordering, DIAG events, optional interfaces"
```

---

### Task 10: Update Layer CLAUDE.md Documentation

**Files:**
- Modify: `wire/CLAUDE.md`
- Modify: `engine/CLAUDE.md`

- [ ] **Step 1: Update wire/CLAUDE.md**

Add to Files section:
```
- `echo.go` — EchoProbe (client) + HandleEchoStream (server), StreamTypeEcho=0x04
- `race.go` — RaceTransport: three-way Happy Eyeballs (QUIC-443 → QUIC-hop → TCP-WS), staggered start, echo verification
```

Add to Gotchas section:
```
- `EchoProbe` verifies Short Header data path — QUIC handshake success (Long Header) does NOT prove data can flow. Middleboxes can allow Long Header but drop Short Header packets. Always echo-verify QUIC before declaring it the winner.
- `RaceTransport` creates independent QUICClient instances per candidate. Loser cleanup is async via WaitGroup + safego.Go. Winner is returned immediately.
```

- [ ] **Step 2: Update engine/CLAUDE.md**

Update architecture diagram to show step 4 as "Transport race" instead of "Build transports".
Remove recoveryProbe from file list.
Add echoLoop and reRaceLoop to the architecture.

- [ ] **Step 3: Commit**

```bash
git add k2/wire/CLAUDE.md k2/engine/CLAUDE.md
git commit -m "docs: update CLAUDE.md for transport race architecture"
```
