# Goroutine Leak Test Gates Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add goleak-based goroutine leak detection across all packages and targeted regression tests for known orphaned-goroutine patterns.

**Architecture:** Two layers — (1) goleak `TestMain` in every package with tests catches any future goroutine leak at CI level, (2) targeted tests for the specific "timeout-abandoned goroutine" pattern (SendDatagram, tunnel Close) verify that known bug patterns don't regress.

**Tech Stack:** `go.uber.org/goleak` (already in go.mod), `runtime` goroutine counting, `time.After` + mock `datagramSender`

---

## Background

### Bug Pattern: Orphaned Goroutine

```go
go func() {
    ch <- result{blockingCallWithoutContext()}
}()
select {
case r := <-ch:     // happy path
case <-time.After(timeout):  // caller returns, goroutine still blocked
}
```

Caller abandons the goroutine on timeout, but the blocking call has no cancellation mechanism. Goroutine accumulates until the underlying resource (QUIC connection) closes.

### Existing Defenses

| Mechanism | Scope | What it does NOT cover |
|-----------|-------|----------------------|
| `connSem` semaphore | `core/tunnel.go` HandleTCP/HandleUDP | Wire layer (SendDatagram), provider accept loops |
| `safego.Go/Loop` | engine, daemon, wire, provider | Panic recovery only — no leak detection, no concurrency limit |
| `goleak` TestMain | `wire/` only | core/, core/dns/, server/, engine/, daemon/, appext/ — 6 packages unprotected |

### Known Issues Found

| # | Location | Pattern | Priority |
|---|----------|---------|----------|
| 1 | `wire/quic.go` quicUDPConn.WriteTo | SendDatagram timeout → orphaned goroutine | P0 (CI broken) |
| 2 | `wire/quic.go` serverUDPConn.WriteTo | Same pattern, server side | P0 |
| 3 | `core/tunnel.go` Close() | wg.Wait timeout → orphaned goroutine | P2 (rare, self-heals) |

#1-2 are being fixed separately. This plan builds the **test gates** to catch regressions and future occurrences.

---

## Task 1: goleak TestMain for `core/`

**Files:**
- Create: `core/testmain_test.go`

**Step 1: Create the goleak TestMain**

```go
package core

import (
	"testing"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
```

**Step 2: Run tests to verify goleak works**

Run: `go test ./core/ -count=1`
Expected: PASS — no leaked goroutines in existing tests.

If any test fails with goleak detection, investigate: it means there's an existing leak. Add the minimal `goleak.IgnoreTopFunction(...)` only for known third-party goroutines (quic-go, smux, sing-tun), never for our own code.

**Step 3: Commit**

```bash
git add core/testmain_test.go
git commit -m "test(core): add goleak TestMain for goroutine leak detection"
```

---

## Task 2: goleak TestMain for `core/dns/`

**Files:**
- Create: `core/dns/testmain_test.go`

**Step 1: Create the goleak TestMain**

```go
package dns

import (
	"testing"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
```

**Step 2: Run tests to verify**

Run: `go test -short ./core/dns/ -count=1`
Expected: PASS (use `-short` to skip live DNS tests).

**Step 3: Commit**

```bash
git add core/dns/testmain_test.go
git commit -m "test(dns): add goleak TestMain for goroutine leak detection"
```

---

## Task 3: goleak TestMain for `server/`

**Files:**
- Create: `server/testmain_test.go`

**Step 1: Create the goleak TestMain**

Server tests use quic-go and smux internally, so we need the same ignores as wire/.

```go
package server

import (
	"testing"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m,
		// quic-go background listeners.
		goleak.IgnoreTopFunction("github.com/apernet/quic-go.(*packetHandlerMap).listen"),
		goleak.IgnoreTopFunction("github.com/apernet/quic-go.(*Transport).listen"),
		goleak.IgnoreTopFunction("github.com/apernet/quic-go.(*Transport).runReceiveLoop"),
		// smux session background goroutines.
		goleak.IgnoreTopFunction("github.com/xtaci/smux/v2.(*Session).keepalive"),
		goleak.IgnoreTopFunction("github.com/xtaci/smux/v2.(*Session).sendLoop"),
		goleak.IgnoreTopFunction("github.com/xtaci/smux/v2.(*Session).recvLoop"),
		goleak.IgnoreTopFunction("github.com/xtaci/smux/v2.(*Session).shaperLoop"),
	)
}
```

**Step 2: Run tests to verify**

Run: `go test ./server/ -count=1`
Expected: PASS. If goleak catches leaks, investigate before adding ignores.

**Step 3: Commit**

```bash
git add server/testmain_test.go
git commit -m "test(server): add goleak TestMain for goroutine leak detection"
```

---

## Task 4: goleak TestMain for `engine/`

**Files:**
- Create: `engine/testmain_test.go`

**Step 1: Create the goleak TestMain**

Engine tests may start quic-go transports and health monitor goroutines. Health monitor uses `safego.Go` which spawns actual goroutines — tests must properly call `stop()` to avoid goleak failures. If engine tests properly clean up (which they should), no ignores needed.

```go
package engine

import (
	"testing"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m,
		// quic-go background listeners (engine integration tests use real QUIC).
		goleak.IgnoreTopFunction("github.com/apernet/quic-go.(*packetHandlerMap).listen"),
		goleak.IgnoreTopFunction("github.com/apernet/quic-go.(*Transport).listen"),
		goleak.IgnoreTopFunction("github.com/apernet/quic-go.(*Transport).runReceiveLoop"),
		// smux session background goroutines.
		goleak.IgnoreTopFunction("github.com/xtaci/smux/v2.(*Session).keepalive"),
		goleak.IgnoreTopFunction("github.com/xtaci/smux/v2.(*Session).sendLoop"),
		goleak.IgnoreTopFunction("github.com/xtaci/smux/v2.(*Session).recvLoop"),
		goleak.IgnoreTopFunction("github.com/xtaci/smux/v2.(*Session).shaperLoop"),
	)
}
```

**Step 2: Run tests to verify**

Run: `go test ./engine/ -count=1`
Expected: PASS. Engine tests use mock transports mostly, but contract tests may use real ones.

**Step 3: Commit**

```bash
git add engine/testmain_test.go
git commit -m "test(engine): add goleak TestMain for goroutine leak detection"
```

---

## Task 5: goleak TestMain for `daemon/`

**Files:**
- Create: `daemon/testmain_test.go`

**Step 1: Create the goleak TestMain**

Daemon tests are mostly unit tests for state, API, paths, etc. Should not need ignores.

```go
package daemon

import (
	"testing"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
```

**Step 2: Run tests to verify**

Run: `go test ./daemon/ -count=1`
Expected: PASS.

**Step 3: Commit**

```bash
git add daemon/testmain_test.go
git commit -m "test(daemon): add goleak TestMain for goroutine leak detection"
```

---

## Task 6: goleak TestMain for `appext/`

**Files:**
- Create: `appext/testmain_test.go`

**Step 1: Create the goleak TestMain**

```go
package appext

import (
	"testing"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
```

**Step 2: Run tests to verify**

Run: `go test ./appext/ -count=1`
Expected: PASS.

**Step 3: Commit**

```bash
git add appext/testmain_test.go
git commit -m "test(appext): add goleak TestMain for goroutine leak detection"
```

---

## Task 7: Regression test — SendDatagram repeated timeout must not leak goroutines

**Context:** This is the core regression test for #1-2. It uses the existing `datagramSender` interface mock to simulate repeated SendDatagram blocking, triggers multiple WriteTo timeouts, and asserts goroutine count stays bounded.

**Files:**
- Modify: `wire/quic_test.go` (add test, or create `wire/quic_leak_test.go` if quic_test.go is large)

**Step 1: Check existing test file**

Read `wire/quic_test.go` to understand how `datagramSender` is mocked in existing tests (e.g., `TestQuicUDPConn_WriteTo_SendDatagramTimeout`). The mock pattern and test helper setup will be reused.

**Step 2: Write the failing test**

The test sends N WriteTo calls that all timeout. After all timeouts, it checks that goroutine count hasn't grown by N. The key assertion: goroutines spawned by WriteTo must eventually exit (either via channel buffer or connection close), not accumulate indefinitely.

```go
func TestWriteTo_RepeatedTimeout_NoGoroutineLeak(t *testing.T) {
	// blockingSender blocks SendDatagram until close(unblock).
	// Each call blocks on <-unblock, simulating a full datagram queue.
	unblock := make(chan struct{})
	sender := &blockingSender{unblock: unblock}

	mux := newTestQuicUDPMux(sender) // use existing test helper
	conn := mux.getOrCreate(1, "8.8.8.8:53")

	baseline := runtime.NumGoroutine()

	const N = 20
	for i := 0; i < N; i++ {
		_, err := conn.WriteTo([]byte("test"), nil)
		if !errors.Is(err, ErrDatagramSendTimeout) {
			t.Fatalf("iteration %d: expected ErrDatagramSendTimeout, got %v", i, err)
		}
	}

	// Unblock all pending SendDatagram goroutines.
	close(unblock)

	// Wait for goroutines to drain.
	deadline := time.After(5 * time.Second)
	for {
		current := runtime.NumGoroutine()
		// Allow small margin (±5) for runtime/GC goroutines.
		if current <= baseline+5 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("goroutine leak: baseline=%d, current=%d (leaked ~%d)",
				baseline, current, current-baseline)
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// blockingSender blocks SendDatagram until unblock is closed.
type blockingSender struct {
	unblock <-chan struct{}
}

func (s *blockingSender) SendDatagram(p []byte) error {
	<-s.unblock
	return nil
}

func (s *blockingSender) LocalAddr() net.Addr {
	return &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1234}
}
```

**Step 3: Run test to verify behavior**

Run: `go test -run TestWriteTo_RepeatedTimeout_NoGoroutineLeak -v -timeout 30s ./wire/`

Expected behavior depends on whether the SendDatagram fix is already merged:
- **If fix is merged**: PASS — goroutines drain after `close(unblock)`.
- **If fix is NOT merged**: PASS anyway because `close(unblock)` releases all goroutines. The real value is catching a **future regression** where someone removes the cancellation mechanism.

To make this test truly regression-proof, add a variant that does NOT call `close(unblock)` — simulating permanent blockage — and instead closes the QUIC connection (via `mux.close()`). Goroutines should exit via connection context cancellation.

**Step 4: Write the permanent-block variant**

```go
func TestWriteTo_PermanentBlock_ConnectionClose_DrainsGoroutines(t *testing.T) {
	// Sender that blocks forever (no unblock signal).
	sender := &foreverBlockingSender{done: make(chan struct{})}

	mux := newTestQuicUDPMux(sender)
	conn := mux.getOrCreate(1, "8.8.8.8:53")

	baseline := runtime.NumGoroutine()

	const N = 10
	for i := 0; i < N; i++ {
		_, err := conn.WriteTo([]byte("test"), nil)
		if !errors.Is(err, ErrDatagramSendTimeout) {
			t.Fatalf("iteration %d: expected ErrDatagramSendTimeout, got %v", i, err)
		}
	}

	// Simulate QUIC connection close — should unblock all goroutines.
	close(sender.done)
	mux.close()

	deadline := time.After(5 * time.Second)
	for {
		current := runtime.NumGoroutine()
		if current <= baseline+5 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("goroutine leak after connection close: baseline=%d, current=%d",
				baseline, current)
		case <-time.After(100 * time.Millisecond):
		}
	}
}

type foreverBlockingSender struct {
	done chan struct{}
}

func (s *foreverBlockingSender) SendDatagram(p []byte) error {
	<-s.done
	return net.ErrClosed
}

func (s *foreverBlockingSender) LocalAddr() net.Addr {
	return &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1234}
}
```

**Step 5: Run both tests**

Run: `go test -run "TestWriteTo_RepeatedTimeout_NoGoroutineLeak|TestWriteTo_PermanentBlock" -v -timeout 30s ./wire/`
Expected: Both PASS.

**Step 6: Commit**

```bash
git add wire/quic_leak_test.go  # or wire/quic_test.go if appended
git commit -m "test(wire): add goroutine leak regression tests for SendDatagram timeout"
```

---

## Task 8: Regression test — tunnel.Close timeout must not permanently leak goroutines

**Context:** `core/tunnel.go:151` spawns a goroutine for `wg.Wait()` with 5s timeout. If timeout fires, the goroutine is orphaned until all handlers complete. This test verifies the goroutine eventually exits (not permanently leaked).

**Files:**
- Modify: `core/tunnel_test.go`

**Step 1: Read existing tunnel test patterns**

Read `core/tunnel_test.go` to understand how `ClientTunnel` is constructed in tests and what mocks are available.

**Step 2: Write the test**

```go
func TestTunnelClose_Timeout_GoroutineEventuallyExits(t *testing.T) {
	// Create a tunnel with a handler that takes longer than closeTimeout (5s).
	// Verify: Close() returns after timeout, and the wg.Wait goroutine
	// exits once the handler finally completes.

	ctx, cancel := context.WithCancel(context.Background())
	tunnel := &ClientTunnel{
		ctx:     ctx,
		cancel:  cancel,
		conns:   newConnTracker(),
		connSem: semaphore.NewWeighted(100),
	}

	// Simulate a slow handler by adding to wg without completing.
	handlerDone := make(chan struct{})
	tunnel.wg.Add(1)
	go func() {
		<-handlerDone // block until test releases
		tunnel.wg.Done()
	}()

	baseline := runtime.NumGoroutine()

	// Close should return after closeTimeout (5s) even though handler is stuck.
	start := time.Now()
	tunnel.Close()
	elapsed := time.Since(start)

	// Verify Close returned via timeout, not via handler completion.
	if elapsed < 4*time.Second || elapsed > 7*time.Second {
		t.Fatalf("Close took %v, expected ~5s (closeTimeout)", elapsed)
	}

	// At this point, wg.Wait goroutine is still alive (handler not done).
	// Release the handler.
	close(handlerDone)

	// Wait for leaked goroutine to drain.
	deadline := time.After(3 * time.Second)
	for {
		current := runtime.NumGoroutine()
		if current <= baseline+2 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("wg.Wait goroutine leaked: baseline=%d, current=%d",
				baseline, current)
		case <-time.After(100 * time.Millisecond):
		}
	}
}
```

**Step 3: Run test**

Run: `go test -run TestTunnelClose_Timeout_GoroutineEventuallyExits -v -timeout 30s ./core/`
Expected: PASS (takes ~5s for the Close timeout + ~1s for handler release).

**Step 4: Commit**

```bash
git add core/tunnel_test.go
git commit -m "test(core): add regression test for tunnel.Close timeout goroutine leak"
```

---

## Task 9: Verify full test suite passes with all goleak gates

**Step 1: Run all tests with race detector**

Run: `go test -short -race -count=1 ./core/ ./core/dns/ ./server/ ./engine/ ./daemon/ ./appext/ ./wire/`

Expected: ALL PASS. If any goleak failure surfaces, it means an **existing leak** was found — this is valuable! Investigate and fix before merging.

**Step 2: Run make quick-check**

Run: `make quick-check`
Expected: PASS.

**Step 3: Final commit (if any fixes were needed)**

```bash
git commit -m "fix: resolve goroutine leaks detected by new goleak gates"
```

---

## Summary

| Task | What | Priority | Time |
|------|------|----------|------|
| 1-6 | goleak TestMain for 6 packages | P0 | ~2 min each |
| 7 | SendDatagram timeout leak regression test | P0 | ~5 min |
| 8 | tunnel.Close timeout leak regression test | P2 | ~5 min |
| 9 | Full verification | P0 | ~3 min |

Total: ~30 minutes. After this, **any goroutine leak in any package will fail CI automatically**.

### What this does NOT cover (separate tasks)

- **#4 tproxy per-packet goroutine** — needs design change (add semaphore), not test gate
- **#5-6 server unbounded streams** — needs design change (add connection limit)
- **#8 overflow stream unbounded** — needs design change (limit accepted streams)

These should be tracked as separate P1 TODOs in `docs/todos/`.
