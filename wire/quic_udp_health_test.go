package wire

import (
	"sync/atomic"
	"testing"
	"time"
)

// ==========================================================================
// Tests for datagram health tracking on quicUDPMux
//
// The mux tracks consecutive ReadFrom timeouts. After relayTimeoutThreshold
// consecutive timeouts, datagramRelayHealthy() returns false, causing
// QUICClient.DialUDP to return ErrDatagramRelayUnhealthy, triggering TCP-WS fallback.
// ==========================================================================

func TestQuicUDPMux_DatagramHealthy_Default(t *testing.T) {
	mux := &quicUDPMux{
		sessions:    make(map[uint32]chan *UDPFrame),
		sessionDone: make(map[uint32]chan struct{}),
		done:        make(chan struct{}),
	}
	if !mux.datagramRelayHealthy() {
		t.Fatal("new mux should be healthy by default")
	}
}

func TestQuicUDPMux_DatagramHealthy_AfterTimeouts(t *testing.T) {
	mux := &quicUDPMux{
		sessions:    make(map[uint32]chan *UDPFrame),
		sessionDone: make(map[uint32]chan struct{}),
		done:        make(chan struct{}),
	}

	// Record timeouts up to threshold
	for i := int32(0); i < relayTimeoutThreshold; i++ {
		if i > 0 && mux.datagramRelayHealthy() {
			// Still healthy before reaching threshold
		}
		mux.recordTimeout()
	}

	if mux.datagramRelayHealthy() {
		t.Fatalf("mux should be unhealthy after %d consecutive timeouts", relayTimeoutThreshold)
	}
}

func TestQuicUDPMux_DatagramHealthy_ResetOnRecv(t *testing.T) {
	mux := &quicUDPMux{
		sessions:    make(map[uint32]chan *UDPFrame),
		sessionDone: make(map[uint32]chan struct{}),
		done:        make(chan struct{}),
	}

	// Make unhealthy
	for i := int32(0); i < relayTimeoutThreshold; i++ {
		mux.recordTimeout()
	}
	if mux.datagramRelayHealthy() {
		t.Fatal("should be unhealthy")
	}

	// Receive a datagram → resets to healthy
	mux.recordRecv()
	if !mux.datagramRelayHealthy() {
		t.Fatal("should be healthy after receiving datagram")
	}
}

func TestQuicUDPMux_DatagramHealthy_BelowThreshold(t *testing.T) {
	mux := &quicUDPMux{
		sessions:    make(map[uint32]chan *UDPFrame),
		sessionDone: make(map[uint32]chan struct{}),
		done:        make(chan struct{}),
	}

	// One fewer than threshold — should still be healthy
	for i := int32(0); i < relayTimeoutThreshold-1; i++ {
		mux.recordTimeout()
	}
	if !mux.datagramRelayHealthy() {
		t.Fatal("should still be healthy below threshold")
	}
}

// ==========================================================================
// Tests for consecutive-recv hop counter recovery
// ==========================================================================

func TestQuicUDPMux_ConsecutiveRecvs_HopReset(t *testing.T) {
	hopAttempts := &atomic.Int32{}
	hopExhausted := &atomic.Int64{}
	hopAttempts.Store(2)
	hopExhausted.Store(time.Now().UnixNano())

	mux := newQuicUDPMux(nil, hopAttempts, hopExhausted)

	// Single recv must NOT reset hop counter
	mux.recordRecv()
	if hopAttempts.Load() != 2 {
		t.Fatalf("hopAttempts = %d, want 2 (single recv must not reset)", hopAttempts.Load())
	}

	// N-1 more recvs (total = relayRecoveryRecvs - 1) — still not enough
	for i := int32(1); i < relayRecoveryRecvs-1; i++ {
		mux.recordRecv()
	}
	if hopAttempts.Load() != 2 {
		t.Fatalf("hopAttempts = %d, want 2 (below threshold)", hopAttempts.Load())
	}

	// One more recv reaches threshold → reset
	mux.recordRecv()
	if hopAttempts.Load() != 0 {
		t.Fatalf("hopAttempts = %d, want 0 after %d consecutive recvs", hopAttempts.Load(), relayRecoveryRecvs)
	}
	if hopExhausted.Load() != 0 {
		t.Fatalf("hopExhausted = %d, want 0 after recovery", hopExhausted.Load())
	}
}

func TestQuicUDPMux_ConsecutiveRecvs_TimeoutBreaksStreak(t *testing.T) {
	hopAttempts := &atomic.Int32{}
	hopAttempts.Store(2)

	mux := newQuicUDPMux(nil, hopAttempts, nil)

	// Build up streak to threshold - 2
	for i := int32(0); i < relayRecoveryRecvs-2; i++ {
		mux.recordRecv()
	}

	// Timeout breaks streak
	mux.recordTimeout()

	// Need full threshold again from scratch
	for i := int32(0); i < relayRecoveryRecvs-1; i++ {
		mux.recordRecv()
	}
	if hopAttempts.Load() != 2 {
		t.Fatalf("hopAttempts = %d, want 2 (streak broken by timeout)", hopAttempts.Load())
	}

	// One more completes the new streak
	mux.recordRecv()
	if hopAttempts.Load() != 0 {
		t.Fatal("hopAttempts should be 0 after new full streak")
	}
}

func TestQuicUDPMux_ConsecutiveRecvs_NoHopNoReset(t *testing.T) {
	// When hopAttempts is 0, consecutive recvs don't try to reset
	hopAttempts := &atomic.Int32{}
	mux := newQuicUDPMux(nil, hopAttempts, nil)

	for i := int32(0); i < relayRecoveryRecvs+5; i++ {
		mux.recordRecv()
	}
	// Should not panic or misbehave
	if hopAttempts.Load() != 0 {
		t.Fatal("hopAttempts should remain 0")
	}
}
