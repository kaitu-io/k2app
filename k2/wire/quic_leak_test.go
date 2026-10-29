package wire

import (
	"net"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ============================================================================
// SendDatagram inflight saturation regression tests
//
// These tests verify the atomic inflight counter that replaced the old
// goroutine+timeout pattern. The old pattern (go func + time.After) leaked
// goroutines when SendDatagram blocked under congestion. The new pattern
// uses an atomic counter to drop packets immediately when the queue is
// saturated — no goroutine spawned, no leak possible.
// ============================================================================

// slowDatagramSender blocks SendDatagram for a configurable duration,
// simulating a congested QUIC datagram queue.
type slowDatagramSender struct {
	delay time.Duration
}

func (s *slowDatagramSender) SendDatagram(p []byte) error {
	time.Sleep(s.delay)
	return nil
}

func (s *slowDatagramSender) LocalAddr() net.Addr {
	return &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0}
}

// TestWriteTo_InflightSaturation_NoGoroutineLeak verifies that when
// maxDatagramInflight concurrent WriteTo calls are in-flight, additional
// calls return ErrDatagramSendTimeout immediately without spawning goroutines.
func TestWriteTo_InflightSaturation_NoGoroutineLeak(t *testing.T) {
	// Sender that blocks long enough for us to saturate the counter.
	sender := &slowDatagramSender{delay: 500 * time.Millisecond}
	mux := &quicUDPMux{
		sessions:    make(map[uint32]chan *UDPFrame),
		sessionDone: make(map[uint32]chan struct{}),
		lastActive:  make(map[uint32]*atomic.Int64),
		done:        make(chan struct{}),
	}
	conn := &quicUDPConn{
		sessionID:  1,
		targetAddr: "8.8.8.8:53",
		qconn:      sender,
		readCh:     make(chan *UDPFrame, 1),
		reaped:     make(chan struct{}),
		done:       make(chan struct{}),
		mux:        mux,
	}

	baseline := runtime.NumGoroutine()

	// Fill up the inflight slots with blocking WriteTo calls.
	var wg sync.WaitGroup
	for i := int32(0); i < maxDatagramInflight; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			conn.WriteTo([]byte("fill"), nil)
		}()
	}

	// Brief pause to let goroutines enter SendDatagram.
	time.Sleep(50 * time.Millisecond)

	// Now the inflight counter should be at maxDatagramInflight.
	// Additional WriteTo calls should return immediately with a silent drop
	// (protocol-correct for unreliable DATAGRAM — no error returned).
	n, err := conn.WriteTo([]byte("overflow"), nil)
	if err != nil {
		t.Fatalf("expected nil error (silent drop) when saturated, got %v", err)
	}
	if n != len([]byte("overflow")) {
		t.Fatalf("expected n=%d (silent drop returns len), got %d", len([]byte("overflow")), n)
	}

	// The overflow call should NOT have spawned a goroutine.
	// Goroutine count should be baseline + maxDatagramInflight (the fill goroutines).
	current := runtime.NumGoroutine()
	maxExpected := baseline + int(maxDatagramInflight) + 5 // small margin
	if current > maxExpected {
		t.Fatalf("unexpected goroutines: baseline=%d, current=%d, max_expected=%d",
			baseline, current, maxExpected)
	}

	// Wait for fill goroutines to complete.
	wg.Wait()

	// After all goroutines exit, count should return to baseline.
	if !waitForGoroutineCount(t, baseline+5, 3*time.Second) {
		t.Fatalf("goroutine leak: baseline=%d, current=%d",
			baseline, runtime.NumGoroutine())
	}

	// Inflight counter should be back to zero.
	if v := mux.sendInflight.Load(); v != 0 {
		t.Fatalf("sendInflight not reset: got %d, want 0", v)
	}
}

// TestWriteTo_InflightCounter_Decrements verifies that the inflight counter
// decrements correctly after SendDatagram completes, preventing permanent
// saturation.
func TestWriteTo_InflightCounter_Decrements(t *testing.T) {
	sender := &slowDatagramSender{delay: 0} // instant send
	mux := &quicUDPMux{
		sessions:    make(map[uint32]chan *UDPFrame),
		sessionDone: make(map[uint32]chan struct{}),
		lastActive:  make(map[uint32]*atomic.Int64),
		done:        make(chan struct{}),
	}
	conn := &quicUDPConn{
		sessionID:  1,
		targetAddr: "8.8.8.8:53",
		qconn:      sender,
		readCh:     make(chan *UDPFrame, 1),
		reaped:     make(chan struct{}),
		done:       make(chan struct{}),
		mux:        mux,
	}

	// Send 100 packets sequentially — all should succeed (counter resets each time).
	for i := 0; i < 100; i++ {
		_, err := conn.WriteTo([]byte("test"), nil)
		if err != nil {
			t.Fatalf("iteration %d: unexpected error: %v", i, err)
		}
	}

	if v := mux.sendInflight.Load(); v != 0 {
		t.Fatalf("sendInflight not zero after sequential sends: got %d", v)
	}
}

// waitForGoroutineCount polls until runtime.NumGoroutine() drops to maxCount
// or below, returning true on success. Returns false if timeout expires.
func waitForGoroutineCount(t *testing.T, maxCount int, timeout time.Duration) bool {
	t.Helper()
	deadline := time.After(timeout)
	for {
		if runtime.NumGoroutine() <= maxCount {
			return true
		}
		select {
		case <-deadline:
			return false
		case <-time.After(50 * time.Millisecond):
		}
	}
}
