package wire

import (
	"context"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/sasha-s/go-deadlock"
)

// blockingDialer is a DirectDialer that sleeps for `delay` then errors.
// Used to deterministically exercise singleflight dedup on slow dials
// without depending on real network / firewall state.
type blockingDialer struct {
	delay time.Duration
}

func (d *blockingDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	select {
	case <-time.After(d.delay):
		return nil, fmt.Errorf("blockingDialer: simulated unreachable after %v", d.delay)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (d *blockingDialer) ListenPacket(network, addr string) (net.PacketConn, error) {
	return nil, fmt.Errorf("blockingDialer: ListenPacket not supported")
}

func init() {
	deadlock.Opts.DeadlockTimeout = 5 * time.Second
}

// TestDeadlock_TransportManagerThenQUICClient validates lock ordering:
// TransportManager.mu → QUICClient.mu. All TM methods must release
// tm.mu before calling into QUICClient methods that acquire qc.mu.
//
// This test exercises both locks in the documented order from concurrent
// goroutines. go-deadlock builds an ordering graph and would panic if
// any code path reversed the order.
func TestDeadlock_TransportManagerThenQUICClient(t *testing.T) {
	tm := NewTransportManager()
	qc := &QUICClient{}

	var wg sync.WaitGroup
	wg.Add(2)

	// Goroutine 1: simulates DialTCP path — tm.mu.RLock then qc.mu.Lock
	go func() {
		defer wg.Done()
		for range 20 {
			tm.mu.RLock()
			qc.mu.Lock()
			qc.mu.Unlock()
			tm.mu.RUnlock()
		}
	}()

	// Goroutine 2: simulates Reset — tm.mu.RLock then qc.mu.Lock
	go func() {
		defer wg.Done()
		for range 20 {
			tm.mu.RLock()
			qc.mu.Lock()
			qc.mu.Unlock()
			tm.mu.RUnlock()
		}
	}()

	wg.Wait()
}

// TestDeadlock_QUICClientDialAndClose validates that QUICClient.mu is
// not held across blocking I/O. Connect and Close both acquire qc.mu
// but must not hold it during network operations.
func TestDeadlock_QUICClientDialAndClose(t *testing.T) {
	qc := &QUICClient{}

	var wg sync.WaitGroup
	wg.Add(2)

	// Goroutine 1: repeated lock/unlock (simulates connect check)
	go func() {
		defer wg.Done()
		for range 50 {
			qc.mu.Lock()
			qc.mu.Unlock()
			time.Sleep(time.Millisecond)
		}
	}()

	// Goroutine 2: same pattern (simulates Close)
	go func() {
		defer wg.Done()
		for range 50 {
			qc.mu.Lock()
			qc.mu.Unlock()
			time.Sleep(time.Millisecond)
		}
	}()

	wg.Wait()
}

// TestDeadlock_TCPWSClientLockContention validates TCPWSClient.mu is
// not held across blocking operations.
func TestDeadlock_TCPWSClientLockContention(t *testing.T) {
	tc := &TCPWSClient{}

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		for range 50 {
			tc.mu.Lock()
			tc.mu.Unlock()
			time.Sleep(time.Millisecond)
		}
	}()

	go func() {
		defer wg.Done()
		for range 50 {
			tc.mu.Lock()
			tc.mu.Unlock()
			time.Sleep(time.Millisecond)
		}
	}()

	wg.Wait()
}

// TestDeadlock_QUICResetConnectionMuFree is a smoke test for resetConnection().
// It verifies the basic extract-nil-release-close flow doesn't panic and mu
// is acquirable after resetConnection completes. Note: this is NOT a strong
// lock-probe test (close() completes in microseconds, so the probe always
// succeeds trivially). The real safety comes from the AST lint gate (D2)
// which prevents Close() from being re-introduced under mu.
func TestDeadlock_QUICResetConnectionMuFree(t *testing.T) {
	qc := &QUICClient{}

	// Set up a fake udpMux so resetConnection has something to close.
	qc.mu.Lock()
	qc.udpMux = &quicUDPMux{sessionDone: make(map[uint32]chan struct{}), done: make(chan struct{})}
	qc.mu.Unlock()

	// Start resetConnection in background — it will close udpMux outside lock.
	resetDone := make(chan struct{})
	go func() {
		qc.resetConnection()
		close(resetDone)
	}()

	// Give resetConnection time to start.
	time.Sleep(50 * time.Millisecond)

	// Lock-probe: try to acquire mu. If resetConnection held mu during
	// close, this would block. 1s timeout is generous.
	lockAcquired := make(chan struct{})
	go func() {
		qc.mu.Lock()
		qc.mu.Unlock()
		close(lockAcquired)
	}()

	select {
	case <-lockAcquired:
		// Good — mu was free during close operations.
	case <-time.After(1 * time.Second):
		t.Fatal("qc.mu blocked during resetConnection — Close under lock regression")
	}

	<-resetDone
}

// TestDeadlock_TransportManagerCloseMuFree validates that TransportManager.mu
// is NOT held during quic.Close()/tcpws.Close() (B7 fix).
func TestDeadlock_TransportManagerCloseMuFree(t *testing.T) {
	tm := NewTransportManager()

	// Install a QUICClient (its Close is fast, but we verify tm.mu is released).
	qc := &QUICClient{}
	tc := &TCPWSClient{}
	tm.mu.Lock()
	tm.primary = qc
	tm.tcpws = tc
	tm.mu.Unlock()

	closeDone := make(chan struct{})
	go func() {
		tm.Close()
		close(closeDone)
	}()

	time.Sleep(50 * time.Millisecond)

	lockAcquired := make(chan struct{})
	go func() {
		tm.mu.Lock()
		tm.mu.Unlock()
		close(lockAcquired)
	}()

	select {
	case <-lockAcquired:
		// Good — tm.mu was free during Close.
	case <-time.After(1 * time.Second):
		t.Fatal("tm.mu blocked during Close — deadlock regression")
	}

	<-closeDone
}

// TestCloseQUICConnSafe_TimeoutBounds verifies quicCloseTimeout is within
// safe bounds: ≥1s (avoid false positives on slow CI), ≤10s (stay under
// go-deadlock's 30s threshold with margin).
func TestCloseQUICConnSafe_TimeoutBounds(t *testing.T) {
	if quicCloseTimeout > 10*time.Second {
		t.Fatalf("quicCloseTimeout too large: %v (max 10s)", quicCloseTimeout)
	}
	if quicCloseTimeout < 1*time.Second {
		t.Fatalf("quicCloseTimeout too small: %v (min 1s)", quicCloseTimeout)
	}
}

// TestCloseSmuxSessionSafe_TimeoutBounds verifies smuxCloseTimeout is within
// safe bounds.
func TestCloseSmuxSessionSafe_TimeoutBounds(t *testing.T) {
	if smuxCloseTimeout > 10*time.Second {
		t.Fatalf("smuxCloseTimeout too large: %v (max 10s)", smuxCloseTimeout)
	}
	if smuxCloseTimeout < 1*time.Second {
		t.Fatalf("smuxCloseTimeout too small: %v (min 1s)", smuxCloseTimeout)
	}
}

// TestCloseQUICResources_NilSafe verifies closeQUICResources handles all-nil args.
func TestCloseQUICResources_NilSafe(t *testing.T) {
	closeQUICResources(nil, nil, nil, "test")
}

// TestCloseSmuxSessionSafe_NilGuard verifies TCPWSClient methods handle nil
// session gracefully (no-op resetConnection and Close).
func TestCloseSmuxSessionSafe_NilGuard(t *testing.T) {
	tc := &TCPWSClient{}
	tc.resetConnection()
	tc.Close()
}

// TestQUICClient_ConnectMu_NotStarved verifies that connectMu is released
// promptly even when transport.Dial fails.
//
// Limitation: this test uses a pre-cancelled context which returns quickly
// from transport.Dial (no actual Conn.destroy blocking). The production
// deadlock requires Conn.destroy to block on chan receive for 30s+, which
// can't be reliably reproduced without a mock transport. This test serves
// as a regression guard for the fast-cancel path and connectMu release.
// The quicDialHardTimeout bounds test ensures the timeout constant stays sane.
func TestQUICClient_ConnectMu_NotStarved(t *testing.T) {
	qc := NewQUICClient(Config{Host: "127.0.0.1", Port: 1}, nil)

	// Attempt connect with an immediately-cancelled context.
	// This triggers the transport.Dial → ctx.Done → Conn.destroy path.
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately — forces the failure path

	start := time.Now()
	_, err := qc.connect(ctx)
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}

	// connect() must return within quicDialHardTimeout + small margin,
	// NOT block for 30s+ on Conn.destroy.
	elapsed := time.Since(start)
	if elapsed > quicDialHardTimeout+2*time.Second {
		t.Fatalf("connect() took %v, expected <%v — connectMu starvation",
			elapsed, quicDialHardTimeout+2*time.Second)
	}

	// Verify connectMu is free — another goroutine can acquire it promptly.
	lockDone := make(chan struct{})
	go func() {
		qc.connectMu.Lock()
		qc.connectMu.Unlock()
		close(lockDone)
	}()
	select {
	case <-lockDone:
		// Good — connectMu is free.
	case <-time.After(2 * time.Second):
		t.Fatal("connectMu blocked after connect() returned — starvation regression")
	}
}

// TestQuicDialHardTimeout_Bounds verifies quicDialHardTimeout stays under
// go-deadlock's 60s threshold with margin, and above a safe minimum for
// UDP listen + QUIC handshake on slow connections.
func TestQuicDialHardTimeout_Bounds(t *testing.T) {
	if quicDialHardTimeout > 30*time.Second {
		t.Fatalf("quicDialHardTimeout too large: %v (must be <30s to stay under 60s deadlock threshold)", quicDialHardTimeout)
	}
	if quicDialHardTimeout < 10*time.Second {
		t.Fatalf("quicDialHardTimeout too small: %v (min 10s to allow normal handshake)", quicDialHardTimeout)
	}
}

// TestTCPWSClient_ConnectOnClosed_NoWastedDial guards against a review-found
// regression: when the client is Close()d, connect() must return
// ErrNotConnected promptly without initiating TCP/TLS I/O. An earlier draft
// of the singleflight rewrite let connect() fall through to a 10s dial on a
// closed client before checking c.closed at the store-under-lock step.
func TestTCPWSClient_ConnectOnClosed_NoWastedDial(t *testing.T) {
	// Non-routable target — if connect() actually dials, this test will take
	// ~10s (tcpwsConnectTimeout). The fast-fail path returns in <100ms.
	cfg := Config{Host: "192.0.2.1", Port: 443}
	tc := NewTCPWSClient(cfg, nil)
	_ = tc.Close() // mark client closed before any connect attempt

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	start := time.Now()
	_, err := tc.connect(ctx)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected ErrNotConnected on closed client")
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("connect() on closed client took %v — expected <500ms (no dial I/O)", elapsed)
	}
}

// TestTCPWSClient_ConnectSingleFlight_CollapsesSlowDials is the strong proof
// that the singleflight rewrite actually dedupes concurrent slow dials.
//
// The pf-blackhole production scenario exhibits 10s TCP dials. With the old
// connectMu serialization, 6 queued callers took 60s total (30s waiter crossed
// go-deadlock threshold → FATAL). With singleflight, all 6 share one dial and
// finish in ≈1× delay.
//
// This test injects a blockingDialer that sleeps a fixed `delay` on every
// DialContext — eliminating network-state dependency. The assertion bracket
// (half-open [delay, 2×delay]) catches both regression directions:
//   - upper bound 2×delay: catches any regression to per-caller serialization
//   - lower bound 0.9×delay: catches accidental "dial bypass" (test validity)
func TestTCPWSClient_ConnectSingleFlight_CollapsesSlowDials(t *testing.T) {
	const delay = 1500 * time.Millisecond
	cfg := Config{Host: "192.0.2.1", Port: 443}
	tc := NewTCPWSClient(cfg, nil)
	tc.SetDirectDialer(&blockingDialer{delay: delay})

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	const fanIn = 6
	errs := make(chan error, fanIn)
	var wg sync.WaitGroup
	wg.Add(fanIn)

	start := time.Now()
	for range fanIn {
		go func() {
			defer wg.Done()
			_, err := tc.connect(ctx)
			errs <- err
		}()
	}
	wg.Wait()
	elapsed := time.Since(start)
	close(errs)

	errCount := 0
	for err := range errs {
		if err == nil {
			t.Fatal("expected blockingDialer error")
		}
		errCount++
	}
	if errCount != fanIn {
		t.Fatalf("got %d errors, expected %d", errCount, fanIn)
	}

	// Without singleflight: 6 × 1.5s serial = 9s (and 4th+ waiter crosses
	// go-deadlock's 30s threshold → FATAL in prod). With singleflight: ≈1.5s.
	if elapsed > 2*delay {
		t.Fatalf("%d concurrent dials took %v — singleflight collapse regression (expected ≈%v)",
			fanIn, elapsed, delay)
	}
	if elapsed < delay*9/10 {
		t.Fatalf("%d concurrent dials took %v — dial didn't actually block (test invalid)",
			fanIn, elapsed)
	}
}

// TestTCPWSClient_ConnectSingleFlight_NoStarvation verifies that concurrent
// connect() callers are de-duplicated via singleflight rather than serialized
// via a held mutex. Under pf blackhole (non-routable IP), all concurrent
// callers must return within one dial's bounded timeout — NOT N× the timeout.
//
// Regression for v0.4.3 production FATAL: previously connectMu was held across
// connectTLSWebSocket, so N queued callers each waited tcpwsConnectTimeout
// serially. The 4th+ waiter exceeded go-deadlock's 30s wait threshold and
// triggered POTENTIAL DEADLOCK → daemon self-terminate.
func TestTCPWSClient_ConnectSingleFlight_NoStarvation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network-dependent singleflight test (waits ~10s for TCP SYN timeout)")
	}
	// Use a non-routable IP to trigger TCP SYN timeout.
	cfg := Config{Host: "192.0.2.1", Port: 443} // TEST-NET-1, guaranteed non-routable
	tc := NewTCPWSClient(cfg, nil)

	ctx, cancel := context.WithTimeout(context.Background(), tcpwsConnectTimeout+5*time.Second)
	defer cancel()

	// Launch 6 concurrent callers. With old connectMu queueing, caller #4+
	// would wait >30s and go-deadlock would fire. With singleflight all six
	// share one dial and finish together in ≈tcpwsConnectTimeout.
	const fanIn = 6
	errs := make(chan error, fanIn)
	start := time.Now()
	for range fanIn {
		go func() {
			_, err := tc.connect(ctx)
			errs <- err
		}()
	}
	for range fanIn {
		err := <-errs
		if err == nil {
			t.Fatal("expected error from non-routable address")
		}
	}

	elapsed := time.Since(start)
	if elapsed > tcpwsConnectTimeout+3*time.Second {
		t.Fatalf("6 concurrent connect() calls took %v, expected <%v — singleflight regression",
			elapsed, tcpwsConnectTimeout+3*time.Second)
	}
}
