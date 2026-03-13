package engine

// Transport Behavior Contract Tests (Engine Layer)
//
// These tests anchor engine-level transport behavior contracts that users
// depend on. They test the health monitor's corrective action decisions
// and error classification — not internal EWMA math or ring buffers.

import (
	"testing"
	"time"

	"github.com/kaitu-io/k2/core"
	"github.com/kaitu-io/k2/wire"
)

// TestContract_PoorSignal_TCPHealthGate verifies that high packet loss does NOT
// trigger a destructive reconnect when TCP streams are still working.
// User scenario: YouTube video streaming on a lossy cellular connection. The
// video plays fine (TCP is healthy) but QUIC wire stats show 30%+ loss.
// Reconnecting would kill the active video stream for no benefit.
func TestContract_PoorSignal_TCPHealthGate(t *testing.T) {
	// Set up a real TransportManager with a QUICClient that has recent TCP success.
	qc := wire.NewQUICClient(wire.Config{}, nil)
	qc.RecordTCPSuccess() // marks TCP as recently healthy

	tm := wire.NewTransportManager()
	tm.SetQUIC(qc)

	tunnel := core.NewClientTunnel(core.ClientTunnelConfig{})

	var reconnectCalled bool
	hm := newHealthMonitor(tm, tunnel, nil, func() {
		reconnectCalled = true
	}, time.Now())

	// Drive health monitor into critical state via direct assess/transition.
	// 5 critical samples transitions to critical, then maybeAct considers action.
	hm.smoothWireLoss = 0.30 // > 0.25 = veryHighLoss threshold
	hm.ewmaInitialized = true
	for range 10 {
		level := hm.assess(wire.WireStats{Transport: "quic"})
		action := hm.transition(level)
		if action != nil {
			action()
		}
	}

	if hm.health != HealthCritical {
		t.Fatalf("precondition: health should be critical, got %s", hm.health)
	}
	if reconnectCalled {
		t.Fatal("contract: reconnect must NOT be called when TCP is recently healthy " +
			"(would kill active streams like video playback)")
	}
}

// TestContract_ServerUnreachable_StopAfter3 verifies that the recovery probe
// gives up after exactly 3 attempts. This prevents infinite retry loops that
// drain battery on mobile devices.
// User scenario: server is down for maintenance; client should stop probing
// quickly and wait for next network change event.
func TestContract_ServerUnreachable_StopAfter3(t *testing.T) {
	if probeMaxAttempts != 3 {
		t.Fatalf("contract: probeMaxAttempts must be 3, got %d "+
			"(changing this affects battery life on mobile)", probeMaxAttempts)
	}
}

// TestContract_AuthFailed_NoRetry verifies that a 401 auth rejection is
// classified as a client error, preventing automatic retry loops.
// User scenario: user's subscription expired; retrying the same credentials
// would waste bandwidth and potentially trigger rate limiting.
func TestContract_AuthFailed_NoRetry(t *testing.T) {
	err := &wire.StreamError{Code: 401, Message: "auth rejected"}
	classified := ClassifyError(err)

	if classified.Category != CategoryClient {
		t.Fatalf("contract: 401 StreamError must be CategoryClient, got %q "+
			"(non-client category would cause infinite retry)", classified.Category)
	}
	if classified.Code != 401 {
		t.Fatalf("contract: error code must be 401 (server-provided), got %d", classified.Code)
	}
}

// TestContract_PoorSignal_TCPHealthGate_Timing verifies the TCP health window
// boundary. If TCP success was too long ago, reconnect should be allowed.
func TestContract_PoorSignal_TCPHealthGate_Timing(t *testing.T) {
	// QUICClient with NO recent TCP success (default zero value).
	qc := wire.NewQUICClient(wire.Config{}, nil)
	// Do NOT call RecordTCPSuccess — lastTCPSuccessAt stays 0.

	tm := wire.NewTransportManager()
	tm.SetQUIC(qc)

	tunnel := core.NewClientTunnel(core.ClientTunnelConfig{})

	var reconnectCalled bool
	hm := newHealthMonitor(tm, tunnel, nil, func() {
		reconnectCalled = true
	}, time.Now())

	// Drive to critical and allow enough time for action.
	hm.smoothWireLoss = 0.30
	hm.ewmaInitialized = true
	hm.lastAction = time.Time{} // bypass cooldown

	for range 10 {
		level := hm.assess(wire.WireStats{Transport: "quic"})
		action := hm.transition(level)
		if action != nil {
			action()
		}
	}

	if !reconnectCalled {
		t.Fatal("contract: reconnect MUST be called when TCP is NOT recently healthy " +
			"and wire quality is critical")
	}
}
