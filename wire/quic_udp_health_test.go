package wire

import (
	"testing"
)

// ==========================================================================
// Tests for datagram health tracking on quicUDPMux
//
// The mux tracks consecutive ReadFrom timeouts. After datagramTimeoutThreshold
// consecutive timeouts, datagramHealthy() returns false, causing
// QUICClient.DialUDP to return ErrDatagramUnhealthy, triggering TCP-WS fallback.
// ==========================================================================

func TestQuicUDPMux_DatagramHealthy_Default(t *testing.T) {
	mux := &quicUDPMux{
		sessions: make(map[uint32]chan *UDPFrame),
		done:     make(chan struct{}),
	}
	if !mux.datagramHealthy() {
		t.Fatal("new mux should be healthy by default")
	}
}

func TestQuicUDPMux_DatagramHealthy_AfterTimeouts(t *testing.T) {
	mux := &quicUDPMux{
		sessions: make(map[uint32]chan *UDPFrame),
		done:     make(chan struct{}),
	}

	// Record timeouts up to threshold
	for i := int32(0); i < datagramTimeoutThreshold; i++ {
		if i > 0 && mux.datagramHealthy() {
			// Still healthy before reaching threshold
		}
		mux.recordTimeout()
	}

	if mux.datagramHealthy() {
		t.Fatalf("mux should be unhealthy after %d consecutive timeouts", datagramTimeoutThreshold)
	}
}

func TestQuicUDPMux_DatagramHealthy_ResetOnRecv(t *testing.T) {
	mux := &quicUDPMux{
		sessions: make(map[uint32]chan *UDPFrame),
		done:     make(chan struct{}),
	}

	// Make unhealthy
	for i := int32(0); i < datagramTimeoutThreshold; i++ {
		mux.recordTimeout()
	}
	if mux.datagramHealthy() {
		t.Fatal("should be unhealthy")
	}

	// Receive a datagram → resets to healthy
	mux.recordRecv()
	if !mux.datagramHealthy() {
		t.Fatal("should be healthy after receiving datagram")
	}
}

func TestQuicUDPMux_DatagramHealthy_BelowThreshold(t *testing.T) {
	mux := &quicUDPMux{
		sessions: make(map[uint32]chan *UDPFrame),
		done:     make(chan struct{}),
	}

	// One fewer than threshold — should still be healthy
	for i := int32(0); i < datagramTimeoutThreshold-1; i++ {
		mux.recordTimeout()
	}
	if !mux.datagramHealthy() {
		t.Fatal("should still be healthy below threshold")
	}
}
