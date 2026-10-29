package wire

import (
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// blockingSender simulates quic-go's datagramQueue.Add() behavior:
// a fixed-capacity queue where Add blocks when full, Pop dequeues one.
type blockingSender struct {
	mu       sync.Mutex
	len      int32
	cap      int32
	popNotif chan struct{}
	closed   chan struct{}
}

func newBlockingSender(cap int32) *blockingSender {
	return &blockingSender{
		cap:      cap,
		popNotif: make(chan struct{}, 1),
		closed:   make(chan struct{}),
	}
}

func (s *blockingSender) SendDatagram(p []byte) error {
	s.mu.Lock()
	for {
		if s.len < s.cap {
			s.len++
			s.mu.Unlock()
			return nil
		}
		select {
		case <-s.popNotif:
		default:
		}
		s.mu.Unlock()
		select {
		case <-s.closed:
			return net.ErrClosed
		case <-s.popNotif:
		}
		s.mu.Lock()
	}
}

func (s *blockingSender) pop() {
	s.mu.Lock()
	if s.len > 0 {
		s.len--
	}
	s.mu.Unlock()
	select {
	case s.popNotif <- struct{}{}:
	default:
	}
}

func (s *blockingSender) LocalAddr() net.Addr {
	return &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 443}
}

// TestWriteTo_NoStarvation_WithInflightCap verifies the fix for the datagram
// queue starvation bug: with maxDatagramInflight=8 and silent drops,
// goroutines that exceed the inflight cap return immediately (no blocking).
//
// Before fix: all 25 goroutines entered datagramQueue.Add() → blocked for
// seconds → couldn't call target.Read() → Google QUIC handshake timed out.
//
// After fix: only 8 goroutines may block; the other 17 get silent drops
// and continue their target.Read() loop without interruption.
func TestWriteTo_NoStarvation_WithInflightCap(t *testing.T) {
	const (
		numSessions  = 25
		queueCap     = 32
		popRateHz    = 6  // simulated packer rate
		testDuration = 3 * time.Second
	)

	mock := newBlockingSender(queueCap)

	// Packer goroutine
	go func() {
		ticker := time.NewTicker(time.Second / time.Duration(popRateHz))
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				mock.pop()
			case <-mock.closed:
				return
			}
		}
	}()

	var sharedInflight atomic.Int32
	sessions := make([]*serverUDPConn, numSessions)
	for i := range sessions {
		sessions[i] = &serverUDPConn{
			qconn:        mock,
			sessionID:    uint32(i + 1),
			addr:         "142.250.183.35:443",
			readCh:       make(chan *UDPFrame, 32),
			done:         make(chan struct{}),
			sendInflight: &sharedInflight,
		}
	}

	var wg sync.WaitGroup
	var totalCalls atomic.Int64
	var totalDrops atomic.Int64
	var totalSent atomic.Int64
	var maxBlockMs atomic.Int64

	start := time.Now()

	for i := range sessions {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			sess := sessions[idx]
			payload := make([]byte, 1250)

			for time.Since(start) < testDuration {
				callStart := time.Now()
				n, err := sess.WriteTo(payload, nil)
				callMs := time.Since(callStart).Milliseconds()
				totalCalls.Add(1)

				if err != nil {
					t.Errorf("session %d: WriteTo returned error: %v (must never error after fix)", idx, err)
					return
				}

				if n == len(payload) && callMs < 1 {
					// Returned immediately — this was a silent drop
					totalDrops.Add(1)
				} else {
					totalSent.Add(1)
				}

				if callMs > maxBlockMs.Load() {
					maxBlockMs.Store(callMs)
				}

				// Simulate target.Read() — always available
				// KEY: goroutine must reach this point even when queue is saturated
				time.Sleep(100 * time.Microsecond)
			}
		}(i)
	}

	wg.Wait()
	close(mock.closed)

	calls := totalCalls.Load()
	drops := totalDrops.Load()
	sent := totalSent.Load()
	maxBlock := maxBlockMs.Load()

	t.Logf("Sessions: %d, Duration: %v", numSessions, testDuration)
	t.Logf("Total WriteTo calls: %d", calls)
	t.Logf("Sent (entered queue): %d", sent)
	t.Logf("Dropped (silent, inflight cap): %d (%.1f%%)", drops, float64(drops)/float64(calls)*100)
	t.Logf("Max single call block: %dms", maxBlock)

	// Gate 1: WriteTo must never return an error
	// (errors would cause HandleUDP to break and kill the session)
	// Already checked above with t.Errorf

	// Gate 2: Most goroutines must not block
	// With inflight=8 and 25 sessions, at least 17 sessions should drop instantly
	dropRatio := float64(drops) / float64(calls)
	if dropRatio < 0.5 {
		t.Errorf("Gate 2 FAILED: drop ratio %.1f%% — expected >50%% (most goroutines should drop, not block)", dropRatio*100)
	} else {
		t.Logf("Gate 2 PASSED: %.1f%% drops — majority of goroutines not blocked", dropRatio*100)
	}

	// Gate 3: Maximum blocking time must be bounded
	// Before fix: goroutines blocked for 3320ms average.
	// After fix: at most 8 goroutines block, each for ~8/popRate seconds.
	// Allow generous bound of 5 seconds.
	if maxBlock > 5000 {
		t.Errorf("Gate 3 FAILED: max block %dms > 5000ms — goroutine stuck too long", maxBlock)
	} else {
		t.Logf("Gate 3 PASSED: max block %dms < 5000ms", maxBlock)
	}

	// Gate 4: Total throughput should approximately match packer rate
	effectiveRate := float64(sent) / testDuration.Seconds()
	if effectiveRate < float64(popRateHz)*0.5 {
		t.Errorf("Gate 4 FAILED: effective rate %.1f/s — expected near %d/s", effectiveRate, popRateHz)
	} else {
		t.Logf("Gate 4 PASSED: effective rate %.1f/s ≈ packer rate %d/s", effectiveRate, popRateHz)
	}
}

// TestWriteTo_OverflowBeforeDrop verifies the overflow-to-stream fallback:
// when datagram queue is saturated, WriteTo sends via overflow stream (STREAM
// frame) instead of dropping. Only drops as last resort if overflow also fails.
func TestWriteTo_OverflowBeforeDrop(t *testing.T) {
	mock := newBlockingSender(32)

	var sharedInflight atomic.Int32
	for i := int32(0); i < maxDatagramInflight; i++ {
		sharedInflight.Add(1)
	}

	payload := make([]byte, 1250)

	t.Run("without_overflow_drops_silently", func(t *testing.T) {
		sess := &serverUDPConn{
			qconn:        mock,
			sessionID:    1,
			addr:         "142.250.183.35:443",
			readCh:       make(chan *UDPFrame, 32),
			done:         make(chan struct{}),
			sendInflight: &sharedInflight,
			overflow:     nil, // no overflow stream available
		}

		n, err := sess.WriteTo(payload, nil)
		if err != nil {
			t.Fatalf("WriteTo returned error %v — must return nil", err)
		}
		if n != len(payload) {
			t.Fatalf("n=%d, want %d", n, len(payload))
		}
		if sess.txDrops.Load() != 1 {
			t.Fatalf("txDrops=%d, want 1 (no overflow → must drop)", sess.txDrops.Load())
		}
		if sess.txPackets.Load() != 0 {
			t.Fatalf("txPackets=%d, want 0", sess.txPackets.Load())
		}
		t.Logf("Without overflow: silent drop (%d, nil) ✅", n)
	})

	// NOTE: overflow stream integration (with real quic.Conn) is tested
	// by TestUDPOverflowStream_* in integration_test.go. Unit tests here
	// only verify the no-overflow fallback and the never-error invariant.

	t.Run("never_returns_error", func(t *testing.T) {
		// The critical invariant: WriteTo NEVER returns an error on inflight overflow.
		// Any error would cause HandleUDP to break out of its loop, killing the session.
		sess := &serverUDPConn{
			qconn:        mock,
			sessionID:    3,
			addr:         "142.250.183.35:443",
			readCh:       make(chan *UDPFrame, 32),
			done:         make(chan struct{}),
			sendInflight: &sharedInflight,
			overflow:     nil,
		}

		for range 100 {
			_, err := sess.WriteTo(payload, nil)
			if err != nil {
				t.Fatalf("WriteTo returned error on attempt — must NEVER error: %v", err)
			}
		}
		t.Logf("100 consecutive overflow WriteTo calls: all returned nil ✅")
	})
}

// TestWriteTo_ClientSide_SilentDrop verifies the client-side fix:
// quicUDPConn.WriteTo returns (len, nil) on inflight overflow while
// still calling recordTimeout for relay health monitoring.
func TestWriteTo_ClientSide_SilentDrop(t *testing.T) {
	mock := newBlockingSender(32)

	mux := &quicUDPMux{
		conn:        nil, // not used by WriteTo path
		sessions:    make(map[uint32]chan *UDPFrame),
		sessionDone: make(map[uint32]chan struct{}),
		lastActive:  make(map[uint32]*atomic.Int64),
		done:        make(chan struct{}),
	}

	// Fill inflight to max
	for i := int32(0); i < maxDatagramInflight; i++ {
		mux.sendInflight.Add(1)
	}

	conn := &quicUDPConn{
		qconn:      mock,
		sessionID:  1,
		targetAddr: "142.250.183.35:443",
		mux:        mux,
		readCh:     make(chan *UDPFrame, 32),
		reaped:     make(chan struct{}),
		done:       make(chan struct{}),
		created:    time.Now(),
	}

	payload := make([]byte, 1250)
	n, err := conn.WriteTo(payload, nil)
	if err != nil {
		t.Fatalf("client WriteTo returned error %v — must return nil (silent drop)", err)
	}
	if n != len(payload) {
		t.Fatalf("client WriteTo returned n=%d, want %d", n, len(payload))
	}
	if conn.txDrops.Load() != 1 {
		t.Fatalf("txDrops=%d, want 1", conn.txDrops.Load())
	}

	// Verify recordTimeout was called (for relay health monitoring)
	if mux.relayTimeouts.Load() != 1 {
		t.Fatalf("relayTimeouts=%d, want 1 (recordTimeout must be called for health monitoring)", mux.relayTimeouts.Load())
	}

	t.Logf("Client WriteTo correctly returns (%d, nil) + records timeout ✅", n)
}
