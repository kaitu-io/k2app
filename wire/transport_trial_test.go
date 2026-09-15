package wire

import (
	"testing"
	"time"
)

// Rollback-trial escalation (ticket #3747 follow-up). A rollback to QUIC is a
// trial; a switch-away inside rollbackTrialWindow grades it failed. The base
// flap schedule (30s << flapCount, capped 600s) is not enough on a path where
// the echo proof passes but bulk traffic fails: every window ends in a
// rollback that fails ~30s later, and anything that restarts the ramp (a
// re-race, a wake) replays it from 30s.

// The rollback branch must stamp the trial clock — without it switchAway has
// nothing to grade against and the escalation is dead code.
func TestSelector_RollbackStartsTrialClock(t *testing.T) {
	base := time.Unix(1000, 0)
	st := newState("tcpws")
	st.nextReviewAt = base
	out, d := selectorStep(st, selectorInput{now: base, quicEchoRecovered: true})
	if d != channelSwitchToQUIC {
		t.Fatalf("precondition: want rollback, got %v", d)
	}
	if !out.rollbackAt.Equal(base) {
		t.Fatalf("rollbackAt = %v, want %v (the trial starts at the rollback)", out.rollbackAt, base)
	}
}

func TestSelector_FailedTrialEscalatesReviewWindow(t *testing.T) {
	base := time.Unix(1000, 0)
	// Fast-path input: switches away unconditionally, so only the window math
	// is under test here.
	away := func(now time.Time) selectorInput {
		return selectorInput{now: now, consecConnFails: quicConnFailThreshold, scoreQUIC: 0.1, scoreTCPWS: 0.9}
	}
	baseWindow := func(flapCount int) time.Duration {
		return flapWindowBase << uint(flapCount)
	}

	// Control: no rollback ever happened → the base schedule applies.
	st := newState("quic")
	st.flapCount = 1
	out, _ := selectorStep(st, away(base))
	if got := out.nextReviewAt.Sub(base); got != baseWindow(1) {
		t.Fatalf("control: window = %v, want base schedule %v", got, baseWindow(1))
	}
	if out.failedTrials != 0 {
		t.Fatalf("control: failedTrials = %d, want 0", out.failedTrials)
	}

	// First failed trial: rolled back at base, switching away 30s later. The
	// base schedule would say 60s; the escalation says flapWindowMax.
	st = newState("quic")
	st.flapCount = 1
	st.rollbackAt = base
	out, _ = selectorStep(st, away(base.Add(30*time.Second)))
	if out.failedTrials != 1 {
		t.Fatalf("first failed trial: failedTrials = %d, want 1", out.failedTrials)
	}
	if got := out.nextReviewAt.Sub(base.Add(30 * time.Second)); got != flapWindowMax {
		t.Fatalf("first failed trial: window = %v, want %v", got, flapWindowMax)
	}

	// Boundary: exactly rollbackTrialWindow after the rollback still counts.
	st = newState("quic")
	st.rollbackAt = base
	out, _ = selectorStep(st, away(base.Add(rollbackTrialWindow)))
	if out.failedTrials != 1 {
		t.Fatalf("at the window boundary: failedTrials = %d, want 1", out.failedTrials)
	}

	// Second consecutive failed trial → sticky.
	st = newState("quic")
	st.flapCount = 2
	st.failedTrials = 1
	st.rollbackAt = base
	out, _ = selectorStep(st, away(base.Add(45*time.Second)))
	if out.failedTrials != 2 {
		t.Fatalf("second failed trial: failedTrials = %d, want 2", out.failedTrials)
	}
	if got := out.nextReviewAt.Sub(base.Add(45 * time.Second)); got != stickyReviewWindow {
		t.Fatalf("second failed trial: window = %v, want sticky %v", got, stickyReviewWindow)
	}

	// Third and beyond stay sticky (no further growth, no overflow).
	st = out
	st.active = "quic"
	st.rollbackAt = base.Add(2 * time.Hour)
	out, _ = selectorStep(st, away(base.Add(2*time.Hour+20*time.Second)))
	if out.failedTrials != 3 {
		t.Fatalf("third failed trial: failedTrials = %d, want 3", out.failedTrials)
	}
	if got := out.nextReviewAt.Sub(base.Add(2*time.Hour + 20*time.Second)); got != stickyReviewWindow {
		t.Fatalf("third failed trial: window = %v, want sticky %v", got, stickyReviewWindow)
	}

	// A successful trial (QUIC held longer than the window) forgets the
	// streak: the new degradation is a fresh event on the base schedule.
	st = newState("quic")
	st.flapCount = 3
	st.failedTrials = 2
	st.rollbackAt = base
	late := base.Add(rollbackTrialWindow + time.Second)
	out, _ = selectorStep(st, away(late))
	if out.failedTrials != 0 {
		t.Fatalf("after a successful trial: failedTrials = %d, want 0", out.failedTrials)
	}
	if got := out.nextReviewAt.Sub(late); got != baseWindow(3) {
		t.Fatalf("after a successful trial: window = %v, want base schedule %v", got, baseWindow(3))
	}
}

// The same escalation driven through the real TransportManager's quality
// path — pins that evaluateSelector persists rollbackAt/failedTrials across
// evaluations and that the sticky window actually blocks an echo-verified
// rollback.
func TestEvaluateSelector_FailedTrialsEscalateToSticky(t *testing.T) {
	tm := NewTransportManager()
	tm.SetPrimary(&mockTransport{available: true}, "quic-443")
	tm.SetTCPWS(&mockTransport{available: true})
	tm.SetChannelProbeInterval("tcpws", 5*time.Second) // fresh window 15s

	// degrade drives the quality path from `at`: two fresh tcpws samples, QUIC
	// collapsed, then the sustain window. Returns the switch time.
	degrade := func(at time.Time) time.Time {
		t.Helper()
		tm.RecordEchoResult("tcpws", 40*time.Millisecond, nil, at)
		tm.RecordEchoResult("tcpws", 40*time.Millisecond, nil, at.Add(time.Second))
		tm.RecordQUICStats(WireStats{PacketLossRate: 0.6, RTT: 100 * time.Millisecond, MinRTT: 20 * time.Millisecond}, at)
		if d := tm.evaluateSelector(at); d != channelStay {
			t.Fatalf("t=%v: want stay while the sustain clock runs, got %v", at, d)
		}
		sw := at.Add(qualitySustainWindow + time.Second)
		if d := tm.evaluateSelector(sw); d != channelSwitchToTCPWS {
			t.Fatalf("t=%v: want switch-away, got %v", sw, d)
		}
		return sw
	}
	state := func() selectorState {
		tm.mu.Lock()
		defer tm.mu.Unlock()
		return tm.selState
	}
	rollback := func(at time.Time) {
		t.Helper()
		tm.quicEchoRecovered.Store(true)
		if d := tm.evaluateSelector(at); d != channelSwitchToQUIC {
			t.Fatalf("t=%v: want rollback, got %v (state %+v)", at, d, state())
		}
	}

	base := time.Unix(1000, 0)

	// Episode 1: base schedule, 30s window.
	sw1 := degrade(base)
	st := state()
	if st.failedTrials != 0 || st.nextReviewAt.Sub(sw1) != flapWindowBase {
		t.Fatalf("episode 1: %+v, want failedTrials 0 and a %v window", st, flapWindowBase)
	}
	rollback(st.nextReviewAt)

	// Episode 2: degrades 20s after the rollback → failed trial #1 → 600s.
	sw2 := degrade(st.nextReviewAt.Add(20 * time.Second))
	st = state()
	if st.failedTrials != 1 || st.nextReviewAt.Sub(sw2) != flapWindowMax {
		t.Fatalf("episode 2: %+v, want failedTrials 1 and a %v window", st, flapWindowMax)
	}
	// The echo-verified rollback is blocked for the whole 600s.
	tm.quicEchoRecovered.Store(true)
	if d := tm.evaluateSelector(sw2.Add(flapWindowMax - time.Second)); d != channelStay {
		t.Fatalf("episode 2: rollback fired inside the escalated window, got %v", d)
	}
	rollback(st.nextReviewAt)

	// Episode 3: fails again → sticky for an hour.
	sw3 := degrade(st.nextReviewAt.Add(20 * time.Second))
	st = state()
	if st.failedTrials != 2 || st.nextReviewAt.Sub(sw3) != stickyReviewWindow {
		t.Fatalf("episode 3: %+v, want failedTrials 2 and a %v window", st, stickyReviewWindow)
	}
	tm.quicEchoRecovered.Store(true)
	if d := tm.evaluateSelector(sw3.Add(stickyReviewWindow - time.Minute)); d != channelStay {
		t.Fatalf("episode 3: rollback fired inside the sticky window, got %v", d)
	}
	rollback(st.nextReviewAt)

	// QUIC now holds for longer than the trial window: the next degradation
	// is a new event and the streak is forgotten (base schedule, flapCount 3).
	sw4 := degrade(st.nextReviewAt.Add(rollbackTrialWindow + 30*time.Second))
	st = state()
	if st.failedTrials != 0 || st.nextReviewAt.Sub(sw4) != flapWindowBase<<3 {
		t.Fatalf("episode 4: %+v, want failedTrials 0 and a %v window", st, flapWindowBase<<3)
	}
}

// A re-race replaces the CONNECTION, not the path: ReplacePrimary keeps the
// flap and trial history and, when tcpws was serving, treats the install as a
// return to quic (a trial). Before this, ReplacePrimary reset the whole
// selector state, so every re-race restarted the anti-flap ramp at 30s.
func TestReplacePrimary_KeepsPathHistoryAndStartsTrialFromTCPWS(t *testing.T) {
	tm := NewTransportManager()
	tm.SetPrimary(&mockTransport{available: true}, "quic-443")
	tm.SetTCPWS(&mockTransport{available: true})

	old := time.Unix(1000, 0)
	tm.mu.Lock()
	tm.selState = selectorState{
		active:       "tcpws",
		flapCount:    3,
		failedTrials: 1,
		rollbackAt:   old,
		nextReviewAt: old.Add(flapWindowMax),
		aboveSince:   old,
	}
	tm.mu.Unlock()
	tm.quicEchoRecovered.Store(true)

	before := time.Now()
	tm.ReplacePrimary(&mockTransport{available: true}, "quic-443")

	tm.mu.Lock()
	st := tm.selState
	tm.mu.Unlock()
	if st.active != "quic" {
		t.Fatalf("active = %q, want quic (the race verified the new primary)", st.active)
	}
	if st.flapCount != 3 || st.failedTrials != 1 {
		t.Fatalf("path history wiped: flapCount %d failedTrials %d, want 3/1", st.flapCount, st.failedTrials)
	}
	if st.rollbackAt.Before(before) {
		t.Fatalf("rollbackAt = %v, want ≥ %v: installing a quic primary over a serving tcpws is a trial", st.rollbackAt, before)
	}
	if !st.aboveSince.IsZero() || !st.nextReviewAt.IsZero() {
		t.Fatalf("connection-scoped state must restart: aboveSince %v nextReviewAt %v", st.aboveSince, st.nextReviewAt)
	}
	if tm.quicEchoRecovered.Load() {
		t.Fatal("echo verification of the OLD primary survived")
	}

	// The race-lost install (engine re-arms fallback right after
	// ReplacePrimary): the race just preferred tcpws over a connected QUIC —
	// graded as a failed trial, so the review window escalates instead of
	// restarting at 30s.
	tm.SetFallbackActiveWithReason(true, "quic-race-lost")
	tm.mu.Lock()
	st = tm.selState
	tm.mu.Unlock()
	if st.failedTrials != 2 {
		t.Fatalf("race-lost after a serving tcpws: failedTrials = %d, want 2", st.failedTrials)
	}
	if got := time.Until(st.nextReviewAt); got < stickyReviewWindow-time.Minute {
		t.Fatalf("race-lost after a serving tcpws: review in %v, want ≈ %v", got, stickyReviewWindow)
	}

	// From quic (the primary died and was re-raced while serving): no trial
	// starts, the existing clock is kept.
	tm.mu.Lock()
	tm.selState = selectorState{active: "quic", flapCount: 2, rollbackAt: old}
	tm.mu.Unlock()
	tm.ReplacePrimary(&mockTransport{available: true}, "quic-443")
	tm.mu.Lock()
	st = tm.selState
	tm.mu.Unlock()
	if !st.rollbackAt.Equal(old) || st.flapCount != 2 {
		t.Fatalf("re-race while on quic: %+v, want rollbackAt %v and flapCount 2 untouched", st, old)
	}
}
