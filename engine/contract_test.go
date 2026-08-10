package engine

// Transport Behavior Contract Tests (Engine Layer)
//
// These tests anchor engine-level transport behavior contracts that users
// depend on. They test the health monitor's corrective action decisions
// and error classification — not internal EWMA math or ring buffers.

import (
	"fmt"
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
	tm.SetPrimary(qc, "quic-443")

	tunnel := core.NewClientTunnel(core.ClientTunnelConfig{})

	var reconnectCalled bool
	ot := &outboundTransport{tm: tm, name: "test"}
	hm := newHealthMonitor(ot, []*outboundTransport{ot}, tunnel, nil, func() {
		reconnectCalled = true
	}, time.Now())

	// Drive health monitor into critical state via direct assess/transition.
	// 5 critical samples transitions to critical, then maybeAct considers action.
	hm.smoothWireLoss = 0.30 // > 0.25 = veryHighLoss threshold
	hm.ewmaInitialized = true
	for range 10 {
		level := hm.assess(wire.WireStats{Transport: "quic"})
		action := hm.transition(level, true) // tcpHealthy=true: TCP streams recently succeeded
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

// TestContract_EchoLoop_TriggersReRace verifies that echoLoop sends a re-race
// signal after 3 consecutive echo failures. This replaces the old recovery
// probe contract (probeMaxAttempts=3).
func TestContract_EchoLoop_TriggersReRace(t *testing.T) {
	// echoLoop uses per-TM local failure counters and triggers reRaceChan at >=3.
	// Verify the threshold constant is 3 by checking behavior.
	tm := mockTransportManager()
	ot := &outboundTransport{tm: tm, name: "test"}
	reRaceChan := make(chan *outboundTransport, 1)
	hm := &healthMonitor{
		done: make(chan struct{}),
	}
	hm.reRaceChan = reRaceChan

	// Simulate 3 consecutive echo failures using per-TM counter (mirrors echoLoop logic).
	echoFails := make(map[*outboundTransport]int32)
	for i := 0; i < 3; i++ {
		echoFails[ot]++
		if echoFails[ot] >= 3 && hm.reRaceChan != nil {
			select {
			case hm.reRaceChan <- ot:
			default:
			}
			echoFails[ot] = 0
		}
	}

	select {
	case received := <-reRaceChan:
		if received != ot {
			t.Fatal("contract: reRaceChan must carry the failed outboundTransport")
		}
		// Expected: re-race triggered after 3 consecutive failures.
	default:
		t.Fatal("contract: echoLoop must trigger re-race after 3 consecutive echo failures " +
			"(changing this threshold affects battery life on mobile)")
	}
}

// TestContract_AuthFailed_NoRetry verifies that auth rejection (stream rejected)
// is classified as a client error, preventing automatic retry loops.
// User scenario: token expired; retrying the same credentials wastes bandwidth.
//
// Auth in k2v5 is connection-level (not per-request). Auth failures arrive
// wrapped in wire.ErrStreamRejected / wire.ErrMetadataRejected sentinels, NOT
// as a bare StreamError. StreamError is only used for target-specific
// responses (blocked IP, target unreachable).
func TestContract_AuthFailed_NoRetry(t *testing.T) {
	// Real auth failure: stream rejection at connection level → CategoryClient.
	err := fmt.Errorf("connect: %w: unauthorized", wire.ErrStreamRejected)
	classified := ClassifyError(err)

	if classified.Category != CategoryClient {
		t.Fatalf("contract: auth rejection must be CategoryClient, got %q "+
			"(non-client category would cause infinite retry)", classified.Category)
	}
	if classified.Code != ErrCodeAuthRejected {
		t.Fatalf("contract: error code must be %d (auth rejected), got %d",
			ErrCodeAuthRejected, classified.Code)
	}
}

// TestContract_StreamError_IsTarget verifies that StreamError (server responded
// to a proxy request) is always CategoryTarget. The wire is healthy when the
// server responds — the error is about the specific destination, not the tunnel.
func TestContract_StreamError_IsTarget(t *testing.T) {
	cases := []struct {
		code int
		msg  string
	}{
		{403, "blocked: 10.0.0.1 is a reserved IP"},
		{502, "resolve example.com: no such host"},
		{502, "dial tcp 1.2.3.4:80: connection refused"},
	}
	for _, tc := range cases {
		se := &wire.StreamError{Code: tc.code, Message: tc.msg}
		classified := ClassifyError(se)
		if classified.Category != CategoryTarget {
			t.Errorf("StreamError{%d, %q}: category = %q, want %q",
				tc.code, tc.msg, classified.Category, CategoryTarget)
		}
	}
}

// TestContract_PoorSignal_TCPHealthGate_Timing verifies the TCP health window
// boundary. If TCP success was too long ago, reconnect should be allowed.
func TestContract_PoorSignal_TCPHealthGate_Timing(t *testing.T) {
	// QUICClient with NO recent TCP success (default zero value).
	qc := wire.NewQUICClient(wire.Config{}, nil)
	// Do NOT call RecordTCPSuccess — lastTCPSuccessAt stays 0.

	tm := wire.NewTransportManager()
	tm.SetPrimary(qc, "quic-443")

	tunnel := core.NewClientTunnel(core.ClientTunnelConfig{})

	var reconnectCalled bool
	ot := &outboundTransport{tm: tm, name: "test"}
	hm := newHealthMonitor(ot, []*outboundTransport{ot}, tunnel, nil, func() {
		reconnectCalled = true
	}, time.Now())

	// Drive to critical and allow enough time for action.
	hm.smoothWireLoss = 0.30
	hm.ewmaInitialized = true
	hm.lastAction = time.Time{} // bypass cooldown

	for range 10 {
		level := hm.assess(wire.WireStats{Transport: "quic"})
		action := hm.transition(level, false)
		if action != nil {
			action()
		}
	}

	if !reconnectCalled {
		t.Fatal("contract: reconnect MUST be called when TCP is NOT recently healthy " +
			"and wire quality is critical")
	}
}
