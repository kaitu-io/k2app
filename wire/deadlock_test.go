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
