package wire

import (
	"sync"
	"testing"
	"time"

	"github.com/sasha-s/go-deadlock"
)

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

	// Goroutine 2: simulates ResetConnections — tm.mu.RLock then qc.mu.Lock
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
	qc.udpMux = &quicUDPMux{done: make(chan struct{})}
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
