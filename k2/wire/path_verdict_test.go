package wire

import (
	"errors"
	"testing"
	"time"
)

var errTestEcho = errors.New("echo timeout (test)")

// qualitySwitchedTM returns a TransportManager whose selector has just left a
// QUIC primary through the real quality path (evaluateSelector), at `now`.
func qualitySwitchedTM(t *testing.T, now time.Time) *TransportManager {
	t.Helper()
	tm := NewTransportManager()
	tm.SetPrimary(&mockDialer{}, "quic-443")
	tm.SetTCPWS(&mockDialer{})
	for i := 0; i < 4; i++ { // quic availability → ~0; tcpws measured good and fresh
		at := now.Add(time.Duration(i-4) * time.Second)
		tm.RecordEchoResult("quic", 0, errTestEcho, at)
		tm.RecordEchoResult("tcpws", 50*time.Millisecond, nil, at)
	}
	tm.mu.Lock()
	tm.selState.aboveSince = now.Add(-qualitySustainWindow - time.Second)
	tm.mu.Unlock()
	if d := tm.evaluateSelector(now); d != channelSwitchToTCPWS {
		t.Fatalf("setup: want a quality switch-away, got %v (scoreQUIC=%.2f scoreTCPWS=%.2f fresh=%d)",
			d, tm.ScoreQUIC(now), tm.ScoreTCPWS(now), tm.scoreTCPWS.FreshSamples(now))
	}
	return tm
}

func TestPathVerdict_NoOpinionWithoutQualitySwitch(t *testing.T) {
	now := time.Unix(10_000, 0)
	tm := NewTransportManager()
	tm.SetPrimary(&mockDialer{}, "quic-443")
	tm.SetTCPWS(&mockDialer{})
	if _, o := tm.PathVerdict(now); o != PathNoOpinion {
		t.Fatalf("fresh manager: want no opinion, got %v", o)
	}
	// A lost race / handshake streak is about one server, not the network.
	tm.SetFallbackActiveWithReason(true, "quic-race-lost")
	if _, o := tm.PathVerdict(now); o != PathNoOpinion {
		t.Fatalf("race-lost fallback: want no opinion, got %v", o)
	}
}

func TestPathVerdict_BadWhileTCPWSServesAfterQualitySwitch(t *testing.T) {
	now := time.Unix(10_000, 0)
	tm := qualitySwitchedTM(t, now)
	v, o := tm.PathVerdict(now.Add(5 * time.Second))
	if o != PathQUICBad {
		t.Fatalf("want bad, got %v", o)
	}
	if want := now.Add(flapWindowBase); !v.ReviewAt.Equal(want) {
		t.Fatalf("ReviewAt = %v, want the selector's own window %v", v.ReviewAt, want)
	}
}

func TestPathVerdict_UnprovenTrialAtStopIsAFailedTrial(t *testing.T) {
	// Ticket #4055, session 1: switched away at +34s, echo-verified rollback
	// 33s later, then 2.5 min on QUIC with nothing loading, disconnect.
	now := time.Unix(10_000, 0)
	tm := qualitySwitchedTM(t, now)
	back := now.Add(flapWindowBase + time.Second)
	tm.quicEchoRecovered.Store(true)
	if d := tm.evaluateSelector(back); d != channelSwitchToQUIC {
		t.Fatalf("setup: want rollback, got %v", d)
	}

	stop := back.Add(150 * time.Second) // past rollbackTrialWindow, nothing answered
	v, o := tm.PathVerdict(stop)
	if o != PathQUICBad {
		t.Fatalf("an unproven trial must not read as a healthy QUIC path, got %v", o)
	}
	if v.FailedTrials != 1 || !v.ReviewAt.Equal(stop.Add(flapWindowMax)) {
		t.Fatalf("want one failed trial reviewed at stop+%v, got failedTrials=%d reviewAt=+%v",
			flapWindowMax, v.FailedTrials, v.ReviewAt.Sub(stop))
	}
	if tm.ActiveChannel() != "quic" {
		t.Fatal("PathVerdict must not change the live selector")
	}
}

func TestPathVerdict_ProvenTrialClears(t *testing.T) {
	now := time.Unix(10_000, 0)
	tm := qualitySwitchedTM(t, now)
	back := now.Add(flapWindowBase + time.Second)
	tm.quicEchoRecovered.Store(true)
	tm.evaluateSelector(back)
	tm.recordQUICDataPath(nil, back.Add(time.Second))
	tm.recordQUICDataPath(nil, back.Add(time.Second+rollbackTrialWindow))
	if _, o := tm.PathVerdict(back.Add(5 * time.Minute)); o != PathQUICProven {
		t.Fatalf("QUIC answered requests across the trial window: want proven, got %v", o)
	}
}

func TestApplyPathVerdict_StartsOnTCPWSAndHoldsUntilReview(t *testing.T) {
	now := time.Unix(20_000, 0)
	tm := NewTransportManager()
	tm.SetPrimary(&mockDialer{}, "quic-443")
	tm.SetTCPWS(&mockDialer{})
	v := PathVerdict{ReviewAt: now.Add(5 * time.Minute), FailedTrials: 1, FlapCount: 2}

	if !tm.ApplyPathVerdict(v, now) {
		t.Fatal("want the serving channel to change")
	}
	if tm.ActiveChannel() != "tcpws" {
		t.Fatalf("active = %s, want tcpws", tm.ActiveChannel())
	}

	// Echo proof before the review time must not roll back.
	tm.quicEchoRecovered.Store(true)
	if d := tm.evaluateSelector(now.Add(time.Minute)); d != channelStay {
		t.Fatalf("before ReviewAt: want stay, got %v", d)
	}
	// After it, the normal echo-verified rollback applies.
	tm.quicEchoRecovered.Store(true)
	if d := tm.evaluateSelector(now.Add(5*time.Minute + time.Second)); d != channelSwitchToQUIC {
		t.Fatalf("after ReviewAt with echo proof: want rollback, got %v", d)
	}

	// The carried count is live: this trial failing is the SECOND failure.
	tm.mu.Lock()
	tm.selState = switchAway(tm.selState, now.Add(5*time.Minute+30*time.Second))
	ft := tm.selState.failedTrials
	tm.mu.Unlock()
	if ft != 2 {
		t.Fatalf("failedTrials after the carried trial fails = %d, want 2", ft)
	}

	// And the applied state is itself evidence for the next session.
	if _, o := tm.PathVerdict(now.Add(6 * time.Minute)); o != PathQUICBad {
		t.Fatalf("an applied verdict still serving on tcpws must carry forward, got %v", o)
	}
}

func TestApplyPathVerdict_NoQUICPrimaryIsNoOp(t *testing.T) {
	now := time.Unix(20_000, 0)
	tm := NewTransportManager()
	d := &mockDialer{}
	tm.SetPrimary(d, "tcpws") // tcpws won the race outright
	tm.SetTCPWS(d)
	if tm.ApplyPathVerdict(PathVerdict{ReviewAt: now.Add(time.Minute)}, now) {
		t.Fatal("nothing to hold back when there is no quic channel")
	}
	if tm.FallbackActive() {
		t.Fatal("must not fabricate a fallback on a tcpws-primary topology")
	}
}

func TestReset_ForgetsPathEvidence(t *testing.T) {
	now := time.Unix(10_000, 0)
	tm := qualitySwitchedTM(t, now)
	tm.Reset() // explicit network change
	if _, o := tm.PathVerdict(now.Add(time.Second)); o != PathNoOpinion {
		t.Fatalf("a new network starts with no opinion, got %v", o)
	}
}

func TestPathVerdict_EvidenceTimeIsTheQualitySwitch(t *testing.T) {
	// The time worth remembering is when QUIC was last SEEN bad, not when the
	// session ended and not when a later session merely carried the verdict:
	// a memory dated by Stop renews itself on every reconnect (real-device
	// run 2026-11-13: seed → 88s session → the 10-minute bound restarted).
	now := time.Unix(10_000, 0)
	tm := qualitySwitchedTM(t, now)
	v, o := tm.PathVerdict(now.Add(5 * time.Minute))
	if o != PathQUICBad || !v.EvidenceAt.Equal(now) {
		t.Fatalf("want evidence dated at the switch-away, got outcome=%v evidenceAt=+%v", o, v.EvidenceAt.Sub(now))
	}

	// A session that only carried the verdict reports the original date, also
	// when it ends on an unproven rollback trial.
	next := NewTransportManager()
	next.SetPrimary(&mockDialer{}, "quic-443")
	next.SetTCPWS(&mockDialer{})
	start := now.Add(6 * time.Minute)
	next.ApplyPathVerdict(v, start)
	if got, _ := next.PathVerdict(start.Add(time.Minute)); !got.EvidenceAt.Equal(now) {
		t.Fatalf("carried verdict re-dated: evidenceAt=+%v", got.EvidenceAt.Sub(now))
	}
	back := v.ReviewAt.Add(time.Second)
	if !back.After(start) {
		back = start.Add(time.Second)
	}
	next.quicEchoRecovered.Store(true)
	if d := next.evaluateSelector(back); d != channelSwitchToQUIC {
		t.Fatalf("setup: want rollback, got %v", d)
	}
	got, o := next.PathVerdict(back.Add(time.Minute))
	if o != PathQUICBad || !got.EvidenceAt.Equal(now) {
		t.Fatalf("unproven trial at stop re-dated the evidence: outcome=%v evidenceAt=+%v", o, got.EvidenceAt.Sub(now))
	}
}

func TestReplacePrimary_KeepsPathEvidenceAndItsDate(t *testing.T) {
	// A re-race installs a new primary on the SAME network: what the selector
	// learned about the path, and when, must survive it.
	now := time.Unix(10_000, 0)
	tm := qualitySwitchedTM(t, now)
	tm.ReplacePrimary(&mockDialer{}, "quic-443")
	v, o := tm.PathVerdict(now.Add(time.Minute))
	if o != PathQUICBad || !v.EvidenceAt.Equal(now) {
		t.Fatalf("re-race lost the path evidence: outcome=%v evidenceAt=%v", o, v.EvidenceAt)
	}
}
