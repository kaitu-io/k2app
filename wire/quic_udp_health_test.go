package wire

import "testing"

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
