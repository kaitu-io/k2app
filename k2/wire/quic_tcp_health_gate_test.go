package wire

import (
	"testing"
	"time"
)

// ==========================================================================
// Tests for TCP health gate on QUICClient
//
// When TCP streams recently succeeded (within tcpHealthWindow), DialUDP skips
// destructive reconnects and returns ErrDatagramRelayUnhealthy instead. This
// prevents normal datagram packet loss from killing healthy TCP streams.
// ==========================================================================

func TestTCPHealthGate_HealthyAfterRecentSuccess(t *testing.T) {
	c := &QUICClient{}
	// Simulate recent TCP success
	c.lastTCPSuccessAt.Store(time.Now().UnixNano())

	if !c.tcpRecentlyHealthy(tcpHealthWindow) {
		t.Fatal("should report TCP healthy immediately after success")
	}
}

func TestTCPHealthGate_UnhealthyWithoutAnySuccess(t *testing.T) {
	c := &QUICClient{}
	// No TCP success recorded (zero value)
	if c.tcpRecentlyHealthy(tcpHealthWindow) {
		t.Fatal("should not report TCP healthy when no TCP success recorded")
	}
}

func TestTCPHealthGate_UnhealthyAfterWindowExpiry(t *testing.T) {
	c := &QUICClient{}
	// Simulate TCP success that's older than the health window
	c.lastTCPSuccessAt.Store(time.Now().Add(-tcpHealthWindow - time.Second).UnixNano())

	if c.tcpRecentlyHealthy(tcpHealthWindow) {
		t.Fatal("should not report TCP healthy after window expires")
	}
}

func TestTCPHealthGate_ResetConnectionClearsLastTCPSuccess(t *testing.T) {
	c := &QUICClient{
		cfg: Config{Host: "test", Port: 443},
	}
	c.lastTCPSuccessAt.Store(time.Now().UnixNano())

	c.resetConnection()

	if c.lastTCPSuccessAt.Load() != 0 {
		t.Fatal("resetConnection should clear lastTCPSuccessAt")
	}
}
