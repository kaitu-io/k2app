package wire

import (
	"bufio"
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"os"
	"testing"
	"time"
)

// Data-path distress (ticket #3757 follow-up, round 3). A QUIC path that
// passes the handshake and the echoes but drops the request itself leaves
// exactly one client-side trace: DialTCP opens the stream, writes the header,
// and the 5s response read times out. CC loss under-reports it (an
// app-limited sender completes no monitor interval), so the health FSM and
// the quality path can sit in their blind band for the whole session. The
// selector's data-path fast path keys on the share of unanswered requests
// in the last minute instead.

// The producer classifies: a deadline is an unanswered request, an EOF is a
// server that at least reacted. Both stay proxy-layer.
func TestReadResponseError_ClassifiesTimeout(t *testing.T) {
	timeout := readResponseError(os.ErrDeadlineExceeded)
	if !isProxyLayerError(timeout) || !isResponseTimeout(timeout) || !isUnanswered(timeout) {
		t.Fatalf("deadline: proxyLayer=%v responseTimeout=%v unanswered=%v, want true/true/true", isProxyLayerError(timeout), isResponseTimeout(timeout), isUnanswered(timeout))
	}
	// Wrapped once more by a caller — errors.As must still find it.
	wrapped := errors.Join(errors.New("tunnel: TCP dial failed"), timeout)
	if !isResponseTimeout(wrapped) {
		t.Fatal("wrapped deadline lost its classification")
	}
	// A producer that only sets ResponseTimeout still reads as unanswered.
	if !isUnanswered(&ProxyLayerError{Err: io.EOF, ResponseTimeout: true}) {
		t.Fatal("ResponseTimeout must imply Unanswered")
	}

	eof := readResponseError(io.EOF)
	if !isProxyLayerError(eof) || isResponseTimeout(eof) || !isUnanswered(eof) {
		t.Fatalf("EOF: proxyLayer=%v responseTimeout=%v unanswered=%v, want true/false/true", isProxyLayerError(eof), isResponseTimeout(eof), isUnanswered(eof))
	}
	// A server-coded rejection is answered, not timed out.
	se := &StreamError{Code: 1, Message: "refused"}
	if isResponseTimeout(se) || isUnanswered(se) {
		t.Fatal("StreamError must never read as a response timeout or as unanswered")
	}
}

// The pure predicate: a floor of three AND a third of the window.
func TestDataPathDistressed_Predicate(t *testing.T) {
	cases := []struct {
		timeouts, total int
		want            bool
	}{
		{0, 0, false}, {2, 2, false}, // under the floor, even at 100%
		{3, 3, true}, {3, 9, true}, // floor met, share ≥ 1/3
		{3, 10, false}, // 30%: a dead destination among working ones
		{4, 12, true}, {12, 12, true},
		{3, 30, false},
	}
	for _, c := range cases {
		if got := dataPathDistressed(c.timeouts, c.total); got != c.want {
			t.Errorf("timeouts=%d total=%d: got %v, want %v", c.timeouts, c.total, got, c.want)
		}
	}
}

// Pure FSM: distress switches away without the sustain window, but with the
// same positive-evidence gate as the quality path.
func TestSelector_DataPathTimeoutFastPath(t *testing.T) {
	now := time.Unix(1000, 0)
	base := selectorInput{now: now, scoreQUIC: 0.9, scoreTCPWS: 0.9, backupFresh: backupFreshSamplesGate, primaryChannel: "quic"}

	// Control: healthy scores, no distress → stay (the quality path is not
	// even close: 0.9 < 0.9×1.5).
	if _, d := selectorStep(newState("quic"), base); d != channelStay {
		t.Fatalf("control: want stay, got %v", d)
	}

	in := base
	in.dataPathDistressed = true
	out, d := selectorStep(newState("quic"), in)
	if d != channelSwitchToTCPWS {
		t.Fatalf("distressed + fresh good backup: want switch, got %v", d)
	}
	if out.active != "tcpws" || out.nextReviewAt.Sub(now) != flapWindowBase {
		t.Fatalf("switch bookkeeping: %+v", out)
	}

	// No fresh backup → no switch, and no sustain clock started either
	// (this path has none).
	in.backupFresh = backupFreshSamplesGate - 1
	out, d = selectorStep(newState("quic"), in)
	if d != channelStay || !out.aboveSince.IsZero() {
		t.Fatalf("no fresh backup: want stay with no clock, got %v %+v", d, out)
	}

	// Backup measured but below the positive-evidence floor → no switch.
	in.backupFresh = backupFreshSamplesGate
	in.scoreTCPWS = minSwitchTargetScore - 0.01
	if _, d = selectorStep(newState("quic"), in); d != channelStay {
		t.Fatalf("target under the floor: want stay, got %v", d)
	}

	// Topology gate still wins: no quic channel, nothing to switch away from.
	in.scoreTCPWS = 0.9
	in.primaryChannel = "tcpws"
	if _, d = selectorStep(newState("quic"), in); d != channelStay {
		t.Fatalf("tcpws primary: want stay, got %v", d)
	}
}

// The window through the real TransportManager: what feeds it, what expires
// or clears it, and that it drives an actual switch with its own reason.
func TestTransportManager_DataPathDistressWindow(t *testing.T) {
	var primaryErr error
	primary := &mockTransport{available: true, dialTCPFn: func(ctx context.Context, addr string, earlyData []byte) (net.Conn, error) {
		if primaryErr != nil {
			return nil, primaryErr
		}
		c, _ := net.Pipe()
		return c, nil
	}}
	tm := NewTransportManager()
	tm.SetPrimary(primary, "quic-443")
	tm.SetTCPWS(&mockTransport{available: true})
	tm.SetChannelProbeInterval("tcpws", 5*time.Second)

	dial := func() error {
		t.Helper()
		c, err := tm.DialTCP(context.Background(), "example.com:443", nil)
		if c != nil {
			c.Close()
		}
		return err
	}
	counts := func() (int, int) {
		tm.mu.RLock()
		defer tm.mu.RUnlock()
		return tm.dataPathCountsLocked(time.Now())
	}

	// Three unanswered dials → distressed. Two are not.
	primaryErr = readResponseError(os.ErrDeadlineExceeded)
	for i := 1; i <= quicRespTimeoutThreshold; i++ {
		if err := dial(); !isResponseTimeout(err) {
			t.Fatalf("dial %d: want the timeout surfaced unchanged (no tcpws fallback for proxy-layer), got %v", i, err)
		}
		if got, want := tm.DataPathDistressed(time.Now()), i >= quicRespTimeoutThreshold; got != want {
			t.Fatalf("after %d timeouts: distressed=%v, want %v", i, got, want)
		}
	}
	// Availability was NOT touched: proxy-layer errors are not transport
	// evidence (unchanged contract) — the score is still the unmeasured
	// neutral×neutral floor, not a failure-driven value below it.
	if s := tm.ScoreQUIC(time.Now()); s != scoreNeutral*scoreNeutral {
		t.Fatalf("scoreQUIC = %v: a response timeout must not be an availability sample (want untouched %v)", s, scoreNeutral*scoreNeutral)
	}

	// Answered dials dilute the share: 3 timeouts among 9 requests still
	// distress (a third), the 10th answer clears it.
	primaryErr = &StreamError{Code: 1, Message: "refused"} // answered (a status), no fallback
	for i := 0; i < 6; i++ {
		dial()
	}
	if to, tot := counts(); to != 3 || tot != 9 || !tm.DataPathDistressed(time.Now()) {
		t.Fatalf("3/9: counts %d/%d distressed %v, want 3/9 true", to, tot, tm.DataPathDistressed(time.Now()))
	}
	primaryErr = nil
	dial()
	if tm.DataPathDistressed(time.Now()) {
		t.Fatal("3/10: a dead destination among working ones must not distress")
	}

	// Transport-layer failures and unanswered-but-not-timeout reads (EOF)
	// are not events at all — neither timeouts nor answers.
	primaryErr = errors.New("wire: open stream: connection closed")
	dial()
	primaryErr = readResponseError(io.EOF)
	dial()
	if to, tot := counts(); to != 3 || tot != 10 {
		t.Fatalf("transport error / EOF must not be recorded: counts %d/%d, want 3/10", to, tot)
	}

	// Staleness: events older than dataPathWindow fall out.
	tm.mu.Lock()
	tm.resetDataPathLocked()
	old := time.Now().Add(-dataPathWindow - time.Second)
	for i := 0; i < 3; i++ {
		tm.dpRing[i] = dataPathEvent{at: old}
	}
	tm.dpIdx = 3
	tm.mu.Unlock()
	if tm.DataPathDistressed(time.Now()) {
		t.Fatal("stale timeouts must not read as distress")
	}

	// Same-path resets KEEP the window (ResetConnections, re-race from
	// quic); a rollback and a re-race from a serving tcpws clear it.
	fill := func() {
		tm.mu.Lock()
		tm.resetDataPathLocked()
		for i := 0; i < 3; i++ {
			tm.dpRing[i] = dataPathEvent{at: time.Now()}
		}
		tm.dpIdx = 3
		tm.mu.Unlock()
	}
	fill()
	tm.ResetConnections()
	if !tm.DataPathDistressed(time.Now()) {
		t.Fatal("ResetConnections must keep the data-path window (same path)")
	}
	tm.ReplacePrimary(primary, "quic-443")
	if !tm.DataPathDistressed(time.Now()) {
		t.Fatal("a re-race from quic must keep the data-path window (same path)")
	}
	tm.mu.Lock()
	tm.selState.active = "tcpws"
	tm.mu.Unlock()
	tm.ReplacePrimary(primary, "quic-443")
	if tm.DataPathDistressed(time.Now()) {
		t.Fatal("a re-race from a serving tcpws starts a trial and must clear the window")
	}

	// End to end: distress + two fresh good tcpws samples → switch-away by
	// the data-path path (quality path is nowhere near: scores are neutral,
	// 0.25 vs 0.25×1.5).
	tm.mu.Lock()
	tm.selState = selectorState{active: "quic"}
	tm.mu.Unlock()
	now := time.Now()
	tm.RecordEchoResult("tcpws", 40*time.Millisecond, nil, now)
	tm.RecordEchoResult("tcpws", 40*time.Millisecond, nil, now.Add(time.Second))
	primaryErr = readResponseError(os.ErrDeadlineExceeded)
	for i := 0; i < quicRespTimeoutThreshold; i++ {
		dial()
	}
	if tm.ActiveChannel() != "tcpws" {
		t.Fatalf("want the data-path fast path to have switched, active=%s", tm.ActiveChannel())
	}

	// Rollback wipes the window so the trial is not graded before it runs.
	tm.mu.Lock()
	tm.selState.nextReviewAt = time.Time{}
	tm.mu.Unlock()
	tm.quicEchoRecovered.Store(true)
	if d := tm.evaluateSelector(time.Now()); d != channelSwitchToQUIC {
		t.Fatalf("want rollback, got %v", d)
	}
	if tm.DataPathDistressed(time.Now()) {
		t.Fatal("rollback must clear the data-path window")
	}
	// ...and with fresh tcpws samples still in the window, the very next
	// evaluation must NOT switch away again.
	if d := tm.evaluateSelector(time.Now()); d != channelStay {
		t.Fatalf("right after rollback: want stay, got %v", d)
	}
}

// A trial is graded by whether QUIC answered anything, not by wall clock.
func TestSelector_UnprovenTrialGradesFailed(t *testing.T) {
	base := time.Unix(1000, 0)
	away := func(now time.Time) selectorInput {
		return selectorInput{now: now, consecConnFails: quicConnFailThreshold, primaryChannel: "quic"}
	}
	late := base.Add(rollbackTrialWindow + 10*time.Minute)

	// Rolled back at base, idle, then failing ten minutes past the window
	// with nothing ever answered: failed trial.
	st := newState("quic")
	st.flapCount = 1
	st.rollbackAt = base
	out, _ := selectorStep(st, away(late))
	if out.failedTrials != 1 || out.nextReviewAt.Sub(late) != flapWindowMax {
		t.Fatalf("unproven late failure: %+v, want failedTrials 1 and a %v window", out, flapWindowMax)
	}

	// Same clock, but QUIC answered across a window in between: successful
	// trial, the new degradation is a fresh event on the base schedule.
	st = newState("quic")
	st.flapCount = 1
	st.rollbackAt = base
	st.trialProven = true
	out, _ = selectorStep(st, away(late))
	if out.failedTrials != 0 || out.nextReviewAt.Sub(late) != flapWindowBase<<1 {
		t.Fatalf("proven late failure: %+v, want failedTrials 0 and the base window", out)
	}

	// Proven but inside the window still fails: one answered request on a
	// fresh per-flow bucket does not make a working path.
	st = newState("quic")
	st.rollbackAt = base
	st.trialProven = true
	out, _ = selectorStep(st, away(base.Add(30*time.Second)))
	if out.failedTrials != 1 {
		t.Fatalf("proven early failure: failedTrials %d, want 1", out.failedTrials)
	}

	// The rollback itself clears the proof.
	st = newState("tcpws")
	st.trialProven = true
	st.firstAnsweredAt = base
	out, d := selectorStep(st, selectorInput{now: base, quicEchoRecovered: true})
	if d != channelSwitchToQUIC || out.trialProven || !out.firstAnsweredAt.IsZero() {
		t.Fatalf("rollback must start an unproven trial: %v %+v", d, out)
	}
}

// Through the TransportManager: answered dials across a window after the
// rollback prove the trial; ReplacePrimary from a serving tcpws starts an
// unproven one.
func TestTransportManager_TrialProvenByAnsweredDial(t *testing.T) {
	primary := &mockTransport{available: true}
	tm := NewTransportManager()
	tm.SetPrimary(primary, "quic-443")
	tm.SetTCPWS(&mockTransport{available: true})

	state := func() selectorState {
		tm.mu.RLock()
		defer tm.mu.RUnlock()
		return tm.selState
	}

	tm.mu.Lock()
	tm.selState = selectorState{active: "tcpws", trialProven: true}
	tm.mu.Unlock()
	tm.quicEchoRecovered.Store(true)
	if d := tm.evaluateSelector(time.Now()); d != channelSwitchToQUIC {
		t.Fatalf("want rollback, got %v", d)
	}
	if state().trialProven {
		t.Fatal("rollback must start an unproven trial")
	}
	c, err := tm.DialTCP(context.Background(), "example.com:443", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	c.Close()
	first := state()
	if first.trialProven || first.firstAnsweredAt.IsZero() {
		t.Fatalf("one answered dial is a fresh bucket's burst, not proof: %+v", first)
	}
	// A second answer inside the window still is not proof; one at the
	// window is.
	tm.recordQUICDataPath(nil, first.firstAnsweredAt.Add(rollbackTrialWindow-time.Second))
	if state().trialProven {
		t.Fatal("answers inside the trial window must not prove it")
	}
	// An unanswered read (EOF) a window later is not an answer either.
	tm.recordQUICDataPath(readResponseError(io.EOF), first.firstAnsweredAt.Add(rollbackTrialWindow))
	if state().trialProven {
		t.Fatal("an EOF on the response read must not prove the trial")
	}
	tm.recordQUICDataPath(&StreamError{Code: 1, Message: "refused"}, first.firstAnsweredAt.Add(rollbackTrialWindow))
	if !state().trialProven {
		t.Fatal("an answered (even rejected) dial a full window after the first must prove the trial")
	}

	// ReplacePrimary while tcpws serves: a new, unproven trial. From quic:
	// the proof is kept (same trial, new connection).
	tm.mu.Lock()
	tm.selState = selectorState{active: "tcpws", trialProven: true, rollbackAt: time.Unix(1000, 0), firstAnsweredAt: time.Unix(1001, 0)}
	tm.mu.Unlock()
	tm.ReplacePrimary(primary, "quic-443")
	if st := state(); st.trialProven || !st.firstAnsweredAt.IsZero() || st.active != "quic" {
		t.Fatalf("ReplacePrimary from tcpws: %+v, want unproven quic trial", st)
	}
	tm.mu.Lock()
	tm.selState = selectorState{active: "quic", trialProven: true, rollbackAt: time.Unix(1000, 0)}
	tm.mu.Unlock()
	tm.ReplacePrimary(primary, "quic-443")
	if st := state(); !st.trialProven {
		t.Fatalf("ReplacePrimary from quic: %+v, want the proof kept", st)
	}
}

// The classification must hold for the error the REAL production path
// produces — quic-go's own stream deadline error, wrapped by
// ReadStreamResponse — not just for os.ErrDeadlineExceeded. quic.go's
// pre-existing warn line uses a bare `err.(net.Error)` assertion on the
// wrapped error and never fires (the emulation logs show the proxy-dial-fail
// lines with no "stream response timeout" companion), which is exactly the
// mistake this test guards readResponseError against.
func TestReadResponseError_RealQUICStreamDeadline(t *testing.T) {
	ln, addr, cleanupServer := echoTestServer(t)
	defer cleanupServer()
	done := make(chan struct{})
	go func() {
		conn, err := ln.Accept(context.Background())
		if err != nil {
			return
		}
		stream, err := conn.AcceptStream(context.Background())
		if err != nil {
			return
		}
		<-done // never answers — the black-hole data path
		stream.Close()
	}()
	conn, cleanupClient := echoTestClient(t, addr)
	defer cleanupClient()
	defer close(done)

	stream, err := conn.OpenStreamSync(context.Background())
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	if err := WriteStreamHeader(stream, StreamTypeTCP, "example.com:443"); err != nil {
		t.Fatalf("write header: %v", err)
	}
	stream.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	_, _, _, err = ReadStreamResponse(bufio.NewReader(stream))
	if err == nil {
		t.Fatal("expected the response read to time out")
	}
	classified := readResponseError(err)
	if !isResponseTimeout(classified) {
		t.Fatalf("real quic-go stream deadline not classified as a response timeout: %v", err)
	}
	if !isProxyLayerError(classified) || !isUnanswered(classified) {
		t.Fatalf("classification lost proxy-layer/unanswered: %v", classified)
	}
}

// Distress can flip by window expiry alone: successes recorded before the
// outage age out of dataPathWindow while no new dial lands on quic (each
// in-flight one is still waiting on its 5 s deadline). The switch then fires
// with reason=quic-datapath-timeout, but the rising-edge DIAG (with the
// timeout/request counts) was only evaluated at record time — and once the
// active channel is tcpws, recordQUICDataPath is never called again. Seen in
// the Ubuntu netns run s1-big-dense-mobile: transport-switch logged, the
// DIAG line never. The selector must log the edge itself.
func TestTransportManager_DataPathDistressEdgeLoggedOnExpiry(t *testing.T) {
	col := &syncRecordCollector{}
	prevLog := slog.Default()
	slog.SetDefault(slog.New(col))
	t.Cleanup(func() { slog.SetDefault(prevLog) })

	tm := NewTransportManager()
	tm.SetPrimary(&mockTransport{available: true}, "quic-443")
	tm.SetTCPWS(&mockTransport{available: true})

	t0 := time.Now()
	for i := 0; i < 10; i++ {
		tm.recordQUICDataPath(nil, t0)
	}
	for i := 0; i < 3; i++ {
		tm.recordQUICDataPath(readResponseError(os.ErrDeadlineExceeded), t0.Add(time.Second))
	}
	if n := len(col.byMessage("DIAG: quic-datapath-timeout")); n != 0 {
		t.Fatalf("3 timeouts among 13 requests is below the share threshold; DIAG logged %d times", n)
	}

	// 61 s later the ten successes have aged out; the three timeouts have not.
	later := t0.Add(dataPathWindow + time.Second)
	if !tm.DataPathDistressed(later) {
		t.Fatal("expected distress once the successes expired")
	}
	tm.evaluateSelector(later)
	if n := len(col.byMessage("DIAG: quic-datapath-timeout")); n != 1 {
		t.Fatalf("selector saw distress but DIAG rising edge logged %d times, want 1", n)
	}
	// Steady state: no re-log while it stays distressed.
	tm.evaluateSelector(later.Add(time.Second))
	tm.recordQUICDataPath(readResponseError(os.ErrDeadlineExceeded), later.Add(2*time.Second))
	if n := len(col.byMessage("DIAG: quic-datapath-timeout")); n != 1 {
		t.Fatalf("edge re-logged while distress persisted: %d", n)
	}
}
