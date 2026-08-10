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

// Switch back requires echo-verified recovery ALONE — no score comparison
// (spec §3.2 "切回为什么不做分数比较"). While tcpws serves traffic QUIC
// carries none, so scoreQUIC is unmeasurable and pinned at neutral; a
// score-ratio gate would be mathematically unreachable against a healthy
// tcpws and would strand users on the stateful fallback forever.
func TestSelector_SwitchBackRequiresEcho(t *testing.T) {
	base := time.Unix(1000, 0)
	st := newState("tcpws")
	st.nextReviewAt = base // review window open
	// Echo NOT verified → stay, regardless of scores.
	in := selectorInput{now: base, scoreQUIC: 0.9, scoreTCPWS: 0.5, quicEchoRecovered: false}
	_, d := selectorStep(st, in)
	if d != channelStay {
		t.Fatalf("no echo recovery → stay, got %v", d)
	}
	// Echo verified → switch back, even with a score ratio that would have
	// failed the old switchBackRatio gate (tcpws healthy, quic neutral).
	in2 := selectorInput{now: base, scoreQUIC: 0.5, scoreTCPWS: 0.99, quicEchoRecovered: true}
	_, d2 := selectorStep(st, in2)
	if d2 != channelSwitchToQUIC {
		t.Fatalf("echo alone → switch back, got %v", d2)
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

// Dead zone: switch-AWAY only. Ratio below switchAwayRatio → no movement
// away from QUIC. (Switch-back has no dead zone / no score gate at all —
// see TestSelector_SwitchBackRequiresEcho and
// TestRollback_FiresWhenTCPWSHealthyAndQUICEchoVerified: an echo-verified
// rollback fires regardless of scores, by design (spec §3.2).)
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
