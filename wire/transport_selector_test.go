package wire

import (
	"context"
	"log/slog"
	"sync/atomic"
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

// The quality path must not fire when the primary IS the tcpws channel —
// there is no quic channel to switch away from. Pure-FSM half of C1's fix;
// TestSelector_TCPWSWonOutright_NoPhantomSwitchNoQuicRecovered drives the
// same topology through the real TransportManager.
func TestSelector_QualityPathInertWhenPrimaryIsTCPWS(t *testing.T) {
	base := time.Unix(1000, 0)
	st := newState("quic")
	st.aboveSince = base
	// Exactly the input that switches away in the normal topology (see
	// TestSelector_QualityPathNeedsSustainAndFreshBackup): measured tcpws vs a
	// scoreQUIC pinned at the neutral floor, sustained past the window, backup
	// fresh. The ONLY difference is who the primary is.
	in := selectorInput{
		now:         base.Add(qualitySustainWindow + time.Second),
		scoreQUIC:   0.25,
		scoreTCPWS:  1.0,
		backupFresh: backupFreshSamplesGate,
	}
	if _, d := selectorStep(st, selectorInput{
		now: in.now, scoreQUIC: in.scoreQUIC, scoreTCPWS: in.scoreTCPWS,
		backupFresh: in.backupFresh, primaryChannel: "quic",
	}); d != channelSwitchToTCPWS {
		t.Fatalf("control (quic primary): want channelSwitchToTCPWS, got %v — the input no longer satisfies the quality path, so the assertion below would be vacuous", d)
	}

	in.primaryChannel = "tcpws"
	if _, d := selectorStep(st, in); d != channelStay {
		t.Fatalf("tcpws primary: want channelStay, got %v — switching away from a quic channel that does not exist", d)
	}
}

// echoingMockTransport is a mockTransport that also implements EchoProber, so
// runProbeOnce's data-path verification section can reach it. Test-only.
type echoingMockTransport struct {
	mockTransport
	echoCalls atomic.Int32
	echoFn    func(ctx context.Context) error
}

func (m *echoingMockTransport) RunEchoProbe(ctx context.Context) error {
	m.echoCalls.Add(1)
	if m.echoFn != nil {
		return m.echoFn(ctx)
	}
	return nil
}

var _ EchoProber = (*echoingMockTransport)(nil)

// TestSelector_TCPWSWonOutright_NoPhantomSwitchNoQuicRecovered is the
// regression test for final-review C1: the whole FSM + recovery-probe pair,
// driven through the real TransportManager in the topology where TCP-WS won
// the startup race outright.
//
// In that topology engine's setupOutboundTransport installs the SAME dialer
// as tm.primary AND tm.tcpws (there is no QUIC dialer at all), while
// selState.active still holds its "quic" default. Every measurement — primary
// dials via channelForType(primaryType)=="tcpws", plus echo probes — lands on
// the tcpws score, so scoreTCPWS climbs to ~1.0 while scoreQUIC has no source
// whatsoever and sits at the neutral floor 0.25. `scoreTCPWS > scoreQUIC×1.5`
// is therefore permanently true, and the pre-fix FSM would:
//
//  1. "switch away" after the 10s sustain window, logging a fabricated
//     DIAG: transport-switch reason=quic-quality-degraded, and
//  2. flip FallbackActive → engage runProbeOnce, which (having lost the old
//     loop's `p.(*QUICClient)` guard) would echo-probe the TCPWS primary,
//     record those samples as QUIC availability, set quicEchoRecovered, and
//     produce an equally fabricated reason=quic-recovered — then repeat
//     forever with a doubling window.
//
// Mutation protocol (both halves verified RED/GREEN by hand):
//   - remove selectorStep's `in.primaryChannel == "tcpws"` early return →
//     the loop below fails at the first evaluateSelector past the sustain
//     window ("phantom switch away").
//   - remove runProbeOnce's `primaryChannel != "quic"` early return →
//     the second half fails on echoCalls / quicEchoRecovered / scoreQUIC.
func TestSelector_TCPWSWonOutright_NoPhantomSwitchNoQuicRecovered(t *testing.T) {
	var records []slog.Record
	prev := slog.Default()
	slog.SetDefault(slog.New(recordCollector{records: &records}))
	defer slog.SetDefault(prev)

	countReason := func(reason string) int {
		n := 0
		for _, r := range records {
			if r.Message != "DIAG: transport-switch" {
				continue
			}
			if v, ok := attrString(r, "reason"); ok && v == reason {
				n++
			}
		}
		return n
	}

	same := &echoingMockTransport{mockTransport: mockTransport{available: true}}
	tm := NewTransportManager()
	tm.SetPrimary(same, "tcpws")
	tm.SetTCPWS(same)

	ctx := context.Background()
	base := time.Now()

	// ~25s of healthy traffic and probes on the one real channel — well past
	// qualitySustainWindow (10s), with backupFresh far past its gate.
	for i := 0; i <= 25; i++ {
		now := base.Add(time.Duration(i) * time.Second)
		conn, err := tm.DialTCP(ctx, "203.0.113.1:443", nil)
		if err != nil {
			t.Fatalf("dial %d: %v", i, err)
		}
		conn.Close()
		tm.RecordEchoResult("tcpws", 12*time.Millisecond, nil, now)
		if d := tm.evaluateSelector(now); d != channelStay {
			t.Fatalf("evaluateSelector at t+%ds = %v, want channelStay — phantom switch away from a QUIC channel that was never created", i, d)
		}
	}

	// Guard against a vacuous pass: the quality-path condition really IS
	// satisfied here, so only the topology gate can be holding the switch.
	now := base.Add(25 * time.Second)
	if sq, st := tm.ScoreQUIC(now), tm.ScoreTCPWS(now); st <= sq*switchAwayRatio {
		t.Fatalf("precondition: scoreTCPWS %v must exceed scoreQUIC %v × %v, else this test proves nothing", st, sq, switchAwayRatio)
	}
	if got := tm.backupFresh.Load(); int(got) < backupFreshSamplesGate {
		t.Fatalf("precondition: backupFresh = %d, want ≥ %d", got, backupFreshSamplesGate)
	}
	if tm.FallbackActive() {
		t.Fatal("FallbackActive flipped — the FSM switched to a channel it was already on")
	}
	if n := countReason("quic-quality-degraded"); n != 0 {
		t.Fatalf("logged %d fabricated quic-quality-degraded switches", n)
	}

	// Second half: even if something else puts the selector in the tcpws state
	// with this topology (a manual SetFallbackActive, or a re-race that landed
	// on tcpws), the QUIC RECOVERY probe must stay out of it entirely — there
	// is no QUIC to recover.
	tm.forceActiveForTest("tcpws")
	echoesBefore := same.echoCalls.Load()
	quicBefore := tm.ScoreQUIC(now)

	tm.runProbeOnceForTest(ctx)

	if got := same.echoCalls.Load(); got != echoesBefore {
		t.Fatalf("recovery probe ran %d echo rounds against a non-QUIC primary (want 0) — double-probing the channel engine already probes", got-echoesBefore)
	}
	if tm.quicEchoRecovered.Load() {
		t.Fatal("quicEchoRecovered armed by probing a TCPWS primary — a rollback to a QUIC dialer that does not exist")
	}
	if got := tm.ScoreQUIC(now); got != quicBefore {
		t.Fatalf("scoreQUIC moved %v → %v — tcpws samples recorded under the quic channel", quicBefore, got)
	}
	if d := tm.evaluateSelector(now); d != channelStay {
		t.Fatalf("evaluateSelector after the probe = %v, want channelStay", d)
	}
	if n := countReason("quic-recovered"); n != 0 {
		t.Fatalf("logged %d fabricated quic-recovered rollbacks", n)
	}
}

// recordCollector is a minimal test-only slog.Handler that appends every
// record to a slice. The tests below drive evaluateSelector synchronously
// from a single goroutine, so no locking is needed (unlike
// engine/engine_test.go's channel-based captureHandler, which has to survive
// concurrent engine goroutines).
type recordCollector struct {
	records *[]slog.Record
}

func (h recordCollector) Enabled(context.Context, slog.Level) bool { return true }
func (h recordCollector) Handle(_ context.Context, r slog.Record) error {
	*h.records = append(*h.records, r)
	return nil
}
func (h recordCollector) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h recordCollector) WithGroup(string) slog.Handler      { return h }

// attrString returns the string value of attribute key on r, and whether it
// was present.
func attrString(r slog.Record, key string) (string, bool) {
	var val string
	var found bool
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == key {
			val, found = a.Value.String(), true
			return false
		}
		return true
	})
	return val, found
}

// TestEvaluateSelector_ChannelReviewAndFlapDampedGating covers Task 8's R1
// (flap-damped) and R2 (channel-review spam gating). evaluateSelector runs
// on every Dial*/Handshake call AND every echo tick — while tcpws is active
// and the review window hasn't closed, a naive "log every review" would
// spam at dial rate. Both DIAG logs below must fire at most once per review
// window (identified by selState.nextReviewAt), except the rollback itself
// (channelSwitchToQUIC), which is never rate-limited.
func TestEvaluateSelector_ChannelReviewAndFlapDampedGating(t *testing.T) {
	var records []slog.Record
	prev := slog.Default()
	slog.SetDefault(slog.New(recordCollector{records: &records}))
	defer slog.SetDefault(prev)

	tm := NewTransportManager()
	base := time.Unix(1000, 0)
	tm.forceActiveForTest("tcpws")
	tm.mu.Lock()
	tm.selState.nextReviewAt = base.Add(30 * time.Second)
	tm.mu.Unlock()

	countMsg := func(msg string) int {
		n := 0
		for _, r := range records {
			if r.Message == msg {
				n++
			}
		}
		return n
	}
	countReasonAttr := func(msg, reason string) int {
		n := 0
		for _, r := range records {
			if r.Message != msg {
				continue
			}
			if v, ok := attrString(r, "reason"); ok && v == reason {
				n++
			}
		}
		return n
	}

	// Echo not yet recovered: two evaluateSelector calls within the same
	// window (simulating dial-rate calls) must yield exactly one
	// channel-review log, not two.
	if d := tm.evaluateSelector(base); d != channelStay {
		t.Fatalf("expected stay, got %v", d)
	}
	if d := tm.evaluateSelector(base.Add(time.Second)); d != channelStay {
		t.Fatalf("expected stay, got %v", d)
	}
	if got := countMsg("DIAG: channel-review"); got != 1 {
		t.Fatalf("channel-review logged %d times before echo recovery, want 1", got)
	}
	if got := countReasonAttr("DIAG: transport-switch", "flap-damped"); got != 0 {
		t.Fatalf("flap-damped logged %d times before echo recovery, want 0", got)
	}

	// Echo recovers but the review window is still open: repeated calls
	// must yield exactly one flap-damped log, and must NOT re-trigger
	// channel-review (still the same window).
	tm.quicEchoRecovered.Store(true)
	if d := tm.evaluateSelector(base.Add(2 * time.Second)); d != channelStay {
		t.Fatalf("expected stay (flap-damped), got %v", d)
	}
	if d := tm.evaluateSelector(base.Add(3 * time.Second)); d != channelStay {
		t.Fatalf("expected stay (flap-damped), got %v", d)
	}
	if got := countMsg("DIAG: channel-review"); got != 1 {
		t.Fatalf("channel-review logged %d times during flap-damped window, want still 1", got)
	}
	if got := countReasonAttr("DIAG: transport-switch", "flap-damped"); got != 1 {
		t.Fatalf("flap-damped logged %d times, want exactly 1", got)
	}

	// Window elapses: rollback fires. This channel-review log is
	// unconditional (not rate-limited) even though the window identity
	// (nextReviewAt) hasn't changed since the last "stay" log.
	if d := tm.evaluateSelector(base.Add(31 * time.Second)); d != channelSwitchToQUIC {
		t.Fatalf("expected switch back to quic, got %v", d)
	}
	if got := countMsg("DIAG: channel-review"); got != 2 {
		t.Fatalf("channel-review logged %d times after rollback, want 2 (1 stay + 1 switch)", got)
	}
	if got := countReasonAttr("DIAG: transport-switch", "quic-recovered"); got != 1 {
		t.Fatalf("quic-recovered logged %d times, want exactly 1", got)
	}
}
