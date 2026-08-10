package wire

import (
	"testing"
	"time"
)

func newState(active string) selectorState {
	return selectorState{active: active}
}

// Fast path: 3 consecutive handshake failures switch away immediately.
func TestSelector_FastPathHandshakeFail(t *testing.T) {
	now := time.Unix(1000, 0)
	st := newState("quic")
	in := selectorInput{now: now, consecConnFails: 3, scoreQUIC: 0.9, scoreTCPWS: 0.5}
	_, d := selectorStep(st, in)
	if d != channelSwitchToTCPWS {
		t.Fatalf("handshake-fail should switch away, got %v", d)
	}
}

// Quality path needs sustain window AND fresh backup samples.
func TestSelector_QualityPathNeedsSustainAndFreshBackup(t *testing.T) {
	base := time.Unix(1000, 0)
	st := newState("quic")
	// tcpws 0.9 > quic 0.5×1.5=0.75 holds, but no sustain yet and no fresh backup.
	in := selectorInput{now: base, scoreQUIC: 0.5, scoreTCPWS: 0.9, backupFresh: 0}
	st, d := selectorStep(st, in)
	if d != channelStay {
		t.Fatalf("no fresh backup → stay, got %v", d)
	}
	// Now backup is fresh, and 10s elapsed with condition holding.
	in2 := selectorInput{now: base.Add(qualitySustainWindow + time.Second), scoreQUIC: 0.5, scoreTCPWS: 0.9, backupFresh: backupFreshSamplesGate}
	_, d2 := selectorStep(st, in2)
	if d2 != channelSwitchToTCPWS {
		t.Fatalf("sustained + fresh backup → switch, got %v", d2)
	}
}

// Switch back requires echo-verified recovery AND asymmetric ratio.
func TestSelector_SwitchBackRequiresEchoAndRatio(t *testing.T) {
	base := time.Unix(1000, 0)
	st := newState("tcpws")
	st.nextReviewAt = base // review window open
	// Ratio met (quic 0.9 > tcpws 0.5×1.2=0.6) but echo NOT verified → stay.
	in := selectorInput{now: base, scoreQUIC: 0.9, scoreTCPWS: 0.5, quicEchoRecovered: false}
	_, d := selectorStep(st, in)
	if d != channelStay {
		t.Fatalf("no echo recovery → stay, got %v", d)
	}
	// Echo verified + ratio met → switch back.
	in2 := selectorInput{now: base, scoreQUIC: 0.9, scoreTCPWS: 0.5, quicEchoRecovered: true}
	_, d2 := selectorStep(st, in2)
	if d2 != channelSwitchToQUIC {
		t.Fatalf("echo+ratio → switch back, got %v", d2)
	}
}

// Switch back blocked before review window (flap damping).
func TestSelector_SwitchBackBlockedBeforeReviewWindow(t *testing.T) {
	base := time.Unix(1000, 0)
	st := newState("tcpws")
	st.nextReviewAt = base.Add(60 * time.Second) // window not yet open
	in := selectorInput{now: base, scoreQUIC: 0.99, scoreTCPWS: 0.3, quicEchoRecovered: true}
	_, d := selectorStep(st, in)
	if d != channelStay {
		t.Fatalf("before review window → stay (damped), got %v", d)
	}
}

// Flap window doubles per round-trip, capped at flapWindowMax.
func TestSelector_FlapWindowDoublesCapped(t *testing.T) {
	base := time.Unix(1000, 0)
	// Simulate a full QUIC→TCPWS switch and read nextReviewAt growth.
	st := newState("quic")
	st.flapCount = 5 // 30×2^5 = 960s > 600 cap
	in := selectorInput{now: base, consecConnFails: 3, scoreQUIC: 0.1, scoreTCPWS: 0.9}
	out, d := selectorStep(st, in)
	if d != channelSwitchToTCPWS {
		t.Fatalf("expected switch, got %v", d)
	}
	window := out.nextReviewAt.Sub(base)
	if window != flapWindowMax {
		t.Fatalf("flap window = %v, want capped %v", window, flapWindowMax)
	}
}

// Dead zone: ratio between 0.833 and 1.5 → no movement in either direction.
func TestSelector_DeadZoneNoMovement(t *testing.T) {
	base := time.Unix(1000, 0)
	onQUIC := newState("quic")
	// Set aboveSince and advance time to simulate sustained condition holding.
	onQUIC.aboveSince = base
	// ratio tcpws/quic = 0.6/0.5 = 1.2 (< 1.5) → stay on QUIC.
	_, d := selectorStep(onQUIC, selectorInput{now: base.Add(qualitySustainWindow + time.Second), scoreQUIC: 0.5, scoreTCPWS: 0.6, backupFresh: backupFreshSamplesGate})
	if d != channelStay {
		t.Fatalf("ratio 1.2 on QUIC → stay, got %v", d)
	}
	onTCPWS := newState("tcpws")
	onTCPWS.nextReviewAt = base
	// ratio tcpws/quic = 0.5/0.55 = 0.91 (> 0.833) → stay on TCP-WS.
	_, d2 := selectorStep(onTCPWS, selectorInput{now: base, scoreQUIC: 0.55, scoreTCPWS: 0.5, quicEchoRecovered: true})
	if d2 != channelStay {
		t.Fatalf("ratio 0.91 on TCP-WS → stay, got %v", d2)
	}
}

// Flap window doubling: verify flapCount=1 yields 60s (30s<<1).
func TestSelector_FlapWindowDoubling(t *testing.T) {
	base := time.Unix(1000, 0)
	st := newState("quic")
	st.flapCount = 1 // 30s<<1 = 60s
	in := selectorInput{now: base, consecConnFails: 3, scoreQUIC: 0.1, scoreTCPWS: 0.9}
	out, d := selectorStep(st, in)
	if d != channelSwitchToTCPWS {
		t.Fatalf("expected switch, got %v", d)
	}
	window := out.nextReviewAt.Sub(base)
	expected := time.Duration(30) * time.Second << uint(1) // 30s<<1 = 60s
	if window != expected {
		t.Fatalf("flap window = %v, want %v", window, expected)
	}
}
