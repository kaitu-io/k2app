package wire

import (
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
		sessions: make(map[uint32]chan *UDPFrame),
		done:     make(chan struct{}),
	}
	if !mux.datagramRelayHealthy() {
		t.Fatal("new mux should be healthy by default")
	}
}

func TestQuicUDPMux_DatagramHealthy_AfterTimeouts(t *testing.T) {
	mux := &quicUDPMux{
		sessions: make(map[uint32]chan *UDPFrame),
		done:     make(chan struct{}),
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
		sessions: make(map[uint32]chan *UDPFrame),
		done:     make(chan struct{}),
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
		sessions: make(map[uint32]chan *UDPFrame),
		done:     make(chan struct{}),
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
// Tests for sliding-window datagram loss rate tracking
// ==========================================================================

func TestQuicUDPMux_LossRate_NoData(t *testing.T) {
	mux := newQuicUDPMux(nil, nil, nil)
	rate := mux.evaluateRelayLossRate()
	if rate != 0 {
		t.Fatalf("loss rate with no data = %f, want 0", rate)
	}
}

func TestQuicUDPMux_LossRate_AllReceived(t *testing.T) {
	mux := newQuicUDPMux(nil, nil, nil)
	for i := 0; i < 20; i++ {
		mux.recordSend()
		mux.recordRecv()
	}
	mux.expireLossWindow()
	rate := mux.evaluateRelayLossRate()
	if rate != 0 {
		t.Fatalf("loss rate = %f, want 0", rate)
	}
}

func TestQuicUDPMux_LossRate_PartialLoss(t *testing.T) {
	mux := newQuicUDPMux(nil, nil, nil)
	for i := 0; i < 20; i++ {
		mux.recordSend()
		if i%2 == 0 {
			mux.recordRecv() // 50% recv = 50% loss
		}
	}
	mux.expireLossWindow()
	rate := mux.evaluateRelayLossRate()
	if rate < 0.45 || rate > 0.55 {
		t.Fatalf("loss rate = %f, want ~0.5", rate)
	}
}

func TestQuicUDPMux_LossRate_BelowMinSamples(t *testing.T) {
	mux := newQuicUDPMux(nil, nil, nil)
	for i := 0; i < 5; i++ {
		mux.recordSend() // only 5, below relayMinSamples
	}
	mux.expireLossWindow()
	rate := mux.evaluateRelayLossRate()
	if rate != 0 {
		t.Fatal("should return 0 with insufficient samples")
	}
}

func TestQuicUDPMux_DatagramHealthy_LossRateUnhealthy(t *testing.T) {
	mux := newQuicUDPMux(nil, nil, nil)
	for i := 0; i < 20; i++ {
		mux.recordSend()
		if i%2 == 0 {
			mux.recordRecv() // 50% loss > 40% threshold
		}
	}
	mux.expireLossWindow()
	if mux.datagramRelayHealthy() {
		t.Fatal("should be unhealthy with 50% loss rate")
	}
}

func TestQuicUDPMux_LossRate_WindowCaching(t *testing.T) {
	mux := newQuicUDPMux(nil, nil, nil)
	for i := 0; i < 20; i++ {
		mux.recordSend()
		mux.recordRecv()
	}
	mux.expireLossWindow()
	rate1 := mux.evaluateRelayLossRate()
	if rate1 != 0 {
		t.Fatalf("first evaluation = %f, want 0", rate1)
	}

	// Add more loss within same window — should return cached value
	for i := 0; i < 20; i++ {
		mux.recordSend()
		// no recv → 100% loss in this batch
	}
	rate2 := mux.evaluateRelayLossRate()
	if rate2 != rate1 {
		t.Fatalf("within window: got %f, want cached %f", rate2, rate1)
	}

	// Expire window — now should see the new loss
	mux.expireLossWindow()
	rate3 := mux.evaluateRelayLossRate()
	if rate3 < 0.9 {
		t.Fatalf("after window expire: got %f, want ~1.0", rate3)
	}
}

// expireLossWindowForTest verifies the helper actually forces re-evaluation.
func TestQuicUDPMux_ExpireLossWindow(t *testing.T) {
	mux := newQuicUDPMux(nil, nil, nil)
	// Evaluate once to set relayLossEvalAt
	mux.evaluateRelayLossRate()

	mux.lossMu.Lock()
	before := mux.relayLossEvalAt
	mux.lossMu.Unlock()

	mux.expireLossWindow()

	mux.lossMu.Lock()
	after := mux.relayLossEvalAt
	mux.lossMu.Unlock()

	if after >= before {
		t.Fatal("expireLossWindow should set relayLossEvalAt to the past")
	}

	_ = time.Now() // use time import
}
