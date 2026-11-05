package engine

// Dead-node detection with no cached connection (PVE S8, 2026-11-05): a node
// that refuses / kills / auth-rejects every connection leaves the echo probe
// nothing to test (wire.ErrEchoNoConnection), so before this the strike count
// never moved and outboundReplaceLoop never ran. A not-probed outcome now
// strikes only on user-dial evidence (wire TransportManager.ConnectFailures).

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kaitu-io/k2/wire"
)

var errNoConn = fmt.Errorf("%w: no cached connection", wire.ErrEchoNoConnection)

// deadNodeDialer: no cached connection (echo is never probed) and every user
// dial fails at connect. Counts every dial and every probe.
type deadNodeDialer struct {
	dials  atomic.Int64
	probes atomic.Int64
	fail   atomic.Bool // true: dials fail at connect; false: dials succeed
}

func newDeadNodeDialer() *deadNodeDialer {
	d := &deadNodeDialer{}
	d.fail.Store(true)
	return d
}

func (d *deadNodeDialer) outcome() error {
	d.dials.Add(1)
	if d.fail.Load() {
		return errors.New("wire: QUIC dial: connection refused")
	}
	return nil
}

func (d *deadNodeDialer) DialTCP(context.Context, string, []byte) (net.Conn, error) {
	if err := d.outcome(); err != nil {
		return nil, err
	}
	c1, c2 := net.Pipe()
	c2.Close()
	return c1, nil
}
func (d *deadNodeDialer) DialUDP(context.Context, string) (net.PacketConn, error) {
	return nil, d.outcome()
}
func (d *deadNodeDialer) DialStreamUDP(context.Context, string) (net.PacketConn, error) {
	return nil, d.outcome()
}
func (d *deadNodeDialer) Reset()       {}
func (d *deadNodeDialer) Close() error { return nil }
func (d *deadNodeDialer) RunEchoProbe(context.Context) error {
	d.probes.Add(1)
	return errNoConn
}

func newDeadNodeTransport(name string) (*outboundTransport, *deadNodeDialer) {
	d := newDeadNodeDialer()
	tm := wire.NewTransportManager()
	tm.SetPrimary(d, "quic-443")
	return &outboundTransport{tm: tm, name: name}, d
}

// userDial simulates one user-traffic dial through the outbound.
func userDial(ot *outboundTransport) {
	if c, err := ot.tm.DialTCP(context.Background(), "example.com:443", nil); err == nil {
		c.Close()
	}
}

func reraced(ch chan *outboundTransport) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

// dfClock drives user-dial evidence ages: the TransportManager's failure
// timestamps (SetConnFailClock) and healthMonitor.clock must share it.
type dfClock struct {
	mu sync.Mutex
	t  time.Time
}

func newDFClock() *dfClock { return &dfClock{t: time.Unix(1_000_000, 0)} }

func (c *dfClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *dfClock) advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func (c *dfClock) set(t time.Time) {
	c.mu.Lock()
	c.t = t
	c.mu.Unlock()
}

func useFakeClock(hm *healthMonitor, ots ...*outboundTransport) *dfClock {
	clk := newDFClock()
	hm.clock = clk.now
	for _, ot := range ots {
		ot.tm.SetConnFailClock(clk.now)
	}
	return clk
}

// probeStep is the interval tests advance between a user dial and the next
// probe: long enough that three strikes span dialFailReraceAfter(0).
const probeStep = 3 * time.Second

// evFailing is dial-failure evidence for a run old enough for any re-race.
func evFailing(fresh bool) dialFailEvidence {
	return dialFailEvidence{failing: true, fresh: fresh, runFor: time.Hour}
}

// --- pure functions -------------------------------------------------------

func TestEchoStepDial_NotProbedWithDialFailureStrikes(t *testing.T) {
	r := echoStepDial(0, 0, errNoConn, evFailing(true))
	if r.Classify != echoNotProbedDialFailing || r.NewStrikes != 1 || r.ShouldRerace {
		t.Fatalf("strike 1: %+v", r)
	}
	r = echoStepDial(r.NewTrust, r.NewStrikes, errNoConn, evFailing(true))
	if r.NewStrikes != 2 || r.ShouldRerace {
		t.Fatalf("strike 2: %+v", r)
	}
	r = echoStepDial(r.NewTrust, r.NewStrikes, errNoConn, evFailing(true))
	if r.NewStrikes != echoReraceThreshold || !r.ShouldRerace {
		t.Fatalf("strike 3 must re-race: %+v", r)
	}
}

// S9 cadence: while user dials are failing, every trust/strike state probes
// at most every echoIntervals[echoDialFailTrust] (2s); a fresh failure is a
// strike, a quiet failing tick keeps the count; a re-race needs both
// threshold strikes and the run age milestone.
func TestEchoStepDial_FailingCadenceGrid(t *testing.T) {
	if echoIntervals[echoDialFailTrust] != 2*time.Second {
		t.Fatalf("failing cadence = %v, want 2s (timeline in echoStepDial)", echoIntervals[echoDialFailTrust])
	}
	maxTrust := int32(len(echoIntervals)) - 1
	for trust := int32(0); trust <= maxTrust; trust++ {
		for strikes := int32(0); strikes <= echoReraceThreshold+1; strikes++ {
			for _, fresh := range []bool{true, false} {
				for _, k := range []int{0, 1, 2} {
					for _, age := range []time.Duration{dialFailReraceAfter(k) - time.Millisecond, dialFailReraceAfter(k)} {
						ev := dialFailEvidence{failing: true, fresh: fresh, runFor: age, reraces: k}
						got := echoStepDial(trust, strikes, errNoConn, ev)
						want := strikes
						if fresh {
							want++
						}
						if got.NewTrust != echoDialFailTrust || got.NewStrikes != want ||
							got.ShouldRerace != (want >= echoReraceThreshold && age >= dialFailReraceAfter(k)) {
							t.Fatalf("(trust=%d strikes=%d %+v): %+v", trust, strikes, ev, got)
						}
					}
				}
			}
		}
	}
}

func TestDialFailReraceAfter(t *testing.T) {
	for k, want := range []time.Duration{8 * time.Second, 24 * time.Second, 40 * time.Second, 56 * time.Second} {
		if got := dialFailReraceAfter(k); got != want {
			t.Fatalf("dialFailReraceAfter(%d) = %v, want %v", k, got, want)
		}
	}
}

// Without dial evidence, or for any outcome other than not-probed,
// echoStepDial is echoStep — idle/offline cadence and the F1 cached-
// connection echo path are untouched.
func TestEchoStepDial_OtherwiseIdenticalToEchoStep(t *testing.T) {
	errs := []error{nil, errNoConn, context.DeadlineExceeded, errors.New("echo: read response: EOF")}
	maxTrust := int32(len(echoIntervals)) - 1
	for _, err := range errs {
		for trust := int32(0); trust <= maxTrust; trust++ {
			for strikes := int32(0); strikes <= echoReraceThreshold; strikes++ {
				ref := echoStep(trust, strikes, err)
				if got := echoStepDial(trust, strikes, err, dialFailEvidence{}); got != ref {
					t.Fatalf("no evidence, err=%v (%d,%d): %+v != %+v", err, trust, strikes, got, ref)
				}
				if wire.IsEchoNotProbed(err) {
					continue
				}
				for _, fresh := range []bool{true, false} {
					if got := echoStepDial(trust, strikes, err, evFailing(fresh)); got != ref {
						t.Fatalf("evidence must only affect not-probed; err=%v (%d,%d): %+v != %+v", err, trust, strikes, got, ref)
					}
				}
			}
		}
	}
}

func TestReadDialFailEvidence(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	first := now.Add(-20 * time.Second)
	cf := func(seq uint64, last time.Time) wire.ConnectFailureInfo {
		return wire.ConnectFailureInfo{Seq: seq, First: first, Last: last}
	}
	cases := []struct {
		name  string
		cf    wire.ConnectFailureInfo
		seen  uint64
		netUp bool
		want  dialFailEvidence
	}{
		{"new recent failure", cf(5, now.Add(-time.Second)), 4, true, dialFailEvidence{true, true, 20 * time.Second, 2}},
		{"no new failure since last probe", cf(5, now.Add(-time.Second)), 5, true, dialFailEvidence{true, false, 20 * time.Second, 2}},
		{"cleared by success or Reset", wire.ConnectFailureInfo{Seq: 5}, 4, true, dialFailEvidence{}},
		{"older than window", cf(5, now.Add(-echoDialFailWindow)), 4, true, dialFailEvidence{}},
		{"just inside window", cf(5, now.Add(-echoDialFailWindow+time.Millisecond)), 4, true, dialFailEvidence{true, true, 20 * time.Second, 2}},
		{"network down", cf(5, now.Add(-time.Second)), 4, false, dialFailEvidence{}},
	}
	for _, c := range cases {
		if got := readDialFailEvidence(c.cf, c.seen, now, 2, c.netUp); got != c.want {
			t.Errorf("%s: got %+v want %+v", c.name, got, c.want)
		}
	}
	if echoDialFailWindow > 30*time.Second {
		t.Fatalf("echoDialFailWindow = %v, must be ≤ 30s", echoDialFailWindow)
	}
}

// --- probeOnce with a real TransportManager -------------------------------

// Every probe preceded by failing user dials → strikes 1, 2, 3 → re-race.
func TestEchoProbe_DeadNodeNoConnFailingTrafficReRaces(t *testing.T) {
	ot, _ := newDeadNodeTransport("t-dead")
	hm, ch := newHMForEcho(t, []*outboundTransport{ot})
	clk := useFakeClock(hm, ot)
	st := &echoLoopState{}
	for i := 1; i <= echoReraceThreshold; i++ {
		userDial(ot)
		clk.advance(probeStep)
		hm.probeOnce(ot, st, time.Now())
		if i < echoReraceThreshold {
			if st.strikes != int32(i) {
				t.Fatalf("after probe %d: strikes=%d, want %d", i, st.strikes, i)
			}
			if reraced(ch) {
				t.Fatalf("re-raced after %d strikes", i)
			}
		}
	}
	if !reraced(ch) {
		t.Fatal("dead node with failing user dials never re-raced")
	}
}

// Release criterion (a): one failed dial followed by a successful one never
// accumulates; one failed dial followed by silence strikes at most once.
func TestEchoProbe_SingleDialJitterNeverReRaces(t *testing.T) {
	t.Run("fail then success", func(t *testing.T) {
		ot, d := newDeadNodeTransport("t-jitter")
		hm, ch := newHMForEcho(t, []*outboundTransport{ot})
		st := &echoLoopState{}
		userDial(ot) // the one failure (e.g. a TLS timeout)
		d.fail.Store(false)
		userDial(ot) // then a success
		for i := 0; i < 4*echoReraceThreshold; i++ {
			hm.probeOnce(ot, st, time.Now())
			if st.strikes != 0 {
				t.Fatalf("probe %d: strikes=%d after fail→success", i, st.strikes)
			}
		}
		if reraced(ch) {
			t.Fatal("single jitter re-raced")
		}
	})
	t.Run("fail then silence", func(t *testing.T) {
		ot, _ := newDeadNodeTransport("t-jitter-idle")
		hm, ch := newHMForEcho(t, []*outboundTransport{ot})
		st := &echoLoopState{}
		userDial(ot)
		maxStrikes := int32(0)
		for i := 0; i < 4*echoReraceThreshold; i++ {
			hm.probeOnce(ot, st, time.Now())
			maxStrikes = max(maxStrikes, st.strikes)
		}
		if maxStrikes > 1 || reraced(ch) {
			t.Fatalf("one failure struck %d times (rerace=%v)", maxStrikes, reraced(ch))
		}
	})
	t.Run("fail, success, fail alternating", func(t *testing.T) {
		ot, d := newDeadNodeTransport("t-jitter-alt")
		hm, ch := newHMForEcho(t, []*outboundTransport{ot})
		st := &echoLoopState{}
		for i := 0; i < 4*echoReraceThreshold; i++ {
			d.fail.Store(true)
			userDial(ot)
			hm.probeOnce(ot, st, time.Now())
			d.fail.Store(false)
			userDial(ot)
			hm.probeOnce(ot, st, time.Now())
		}
		if reraced(ch) {
			t.Fatal("isolated failures separated by successes re-raced")
		}
	})
}

// Release criterion (b): idle — no traffic, no live connection. Probe ticks
// create no dials and no strikes, and the trust ramp is the old one.
func TestEchoProbe_IdleNoConnNoDialsNoStrikes(t *testing.T) {
	ot, d := newDeadNodeTransport("t-idle")
	hm, ch := newHMForEcho(t, []*outboundTransport{ot})
	st := &echoLoopState{}
	const ticks = 200
	for i := 0; i < ticks; i++ {
		hm.probeOnce(ot, st, time.Now())
		if st.strikes != 0 {
			t.Fatalf("tick %d: strikes=%d while idle", i, st.strikes)
		}
	}
	if n := d.dials.Load(); n != 0 {
		t.Fatalf("probe path made %d dials while idle, want 0", n)
	}
	if n := d.probes.Load(); n != ticks {
		t.Fatalf("precondition: %d probes ran, want %d", n, ticks)
	}
	if reraced(ch) {
		t.Fatal("idle client re-raced")
	}
	if want := int32(len(echoIntervals) - 1); st.trustIdx != want {
		t.Fatalf("trustIdx=%d, want the 16s ceiling %d (cadence must back off as before)", st.trustIdx, want)
	}
}

// Failures from before a network Reset / ResetConnections never strike on
// the new path.
func TestEchoProbe_DialFailureBeforeResetDoesNotStrike(t *testing.T) {
	for name, reset := range map[string]func(*wire.TransportManager){
		"Reset":            (*wire.TransportManager).Reset,
		"ResetConnections": (*wire.TransportManager).ResetConnections,
	} {
		t.Run(name, func(t *testing.T) {
			ot, _ := newDeadNodeTransport("t-reset")
			hm, ch := newHMForEcho(t, []*outboundTransport{ot})
			st := &echoLoopState{}
			userDial(ot)
			userDial(ot)
			reset(ot.tm)
			for i := 0; i < 2*echoReraceThreshold; i++ {
				hm.probeOnce(ot, st, time.Now())
				if st.strikes != 0 {
					t.Fatalf("probe %d after %s: strikes=%d from pre-reset failures", i, name, st.strikes)
				}
			}
			if reraced(ch) {
				t.Fatal("re-raced on pre-reset evidence")
			}
		})
	}
}

// --- engine level: dead node → replace loop → fallback ---------------------

// A node refusing every dial with no cached connection, and user traffic
// trying: echo strikes reach the replace loop, Phase A fails twice on the
// dead node, and Phase B switches to the live fallback — the F2 "node dies"
// case PVE S8 never reached.
func TestEchoDeadNode_NoConnRefusingDialsFailsOver(t *testing.T) {
	hs := newFoHarness(t, foOpts{
		fallbacks: map[string][]string{foURLA: {foURLB}},
		net:       []checkResult{checkMatched},
	})
	hs.servers.setAlive("b.example", true) // a.example stays dead

	dead := newDeadNodeDialer()
	hs.e.mu.Lock()
	hs.target.tm.SetPrimary(dead, "quic-443")
	ot := hs.target
	hs.e.mu.Unlock()

	hm := newHealthMonitor(ot, []*outboundTransport{ot}, nil, nil, nil, time.Now())
	hm.reRaceChan = hs.e.reRaceChan
	clk := useFakeClock(hm, ot)
	st := &echoLoopState{}

	deadline := time.Now().Add(10 * time.Second)
	for hs.targetURL() != foURLB {
		if time.Now().After(deadline) {
			t.Fatalf("no failover: races on a=%d, target=%s", hs.servers.raceCount("a.example"), hs.targetURL())
		}
		hs.e.mu.Lock()
		cur := hs.target.tm
		hs.e.mu.Unlock()
		if cur != ot.tm {
			break // switched (target.tm replaced by Phase B)
		}
		userDial(ot)
		clk.advance(probeStep)
		hm.probeOnce(ot, st, time.Now())
		time.Sleep(time.Millisecond)
	}
	eventually(t, "target switched to b", func() bool { return hs.targetURL() == foURLB })
	if n := hs.servers.raceCount("a.example"); n < reRaceFailThresholdForReplace {
		t.Fatalf("Phase A ran %d times on the dead node, want ≥ %d", n, reRaceFailThresholdForReplace)
	}
	if st, le := hs.engineState(); st != StateConnected || le != nil {
		t.Fatalf("engine = %s / %v, want connected / nil", st, le)
	}
}

// The same dead node, but its rejection is Center's 402: once the loop is
// reached, the Phase-A race error carries 402 and Plan A fails with it
// without spending the fallback.
func TestEchoDeadNode_402RejectFailsWith402WithoutReplacement(t *testing.T) {
	hs := newFoHarness(t, foOpts{
		fallbacks: map[string][]string{foURLA: {foURLB}},
		net:       []checkResult{checkMatched},
	})
	hs.servers.setAlive("b.example", true)
	reject := &wire.ProxyLayerError{Err: fmt.Errorf("%w: %w", wire.ErrMetadataRejected,
		&wire.StreamError{Code: ErrCodePaymentRequired, Message: "payment required"})}
	raceTransportFn = func(ctx context.Context, cfg wire.Config, _ string, _ wire.DirectDialer, _ *wire.CookieJar) (*wire.RaceResult, error) {
		return nil, fmt.Errorf("transport race: %w", reject)
	}

	d := &rejectingDialer{deadNodeDialer: newDeadNodeDialer(), err: reject}
	hs.e.mu.Lock()
	hs.target.tm.SetPrimary(d, "quic-443")
	ot := hs.target
	hs.e.mu.Unlock()
	hm := newHealthMonitor(ot, []*outboundTransport{ot}, nil, nil, nil, time.Now())
	hm.reRaceChan = hs.e.reRaceChan
	clk := useFakeClock(hm, ot)
	st := &echoLoopState{}

	deadline := time.Now().Add(10 * time.Second)
	for {
		if s, _ := hs.engineState(); s != StateConnected {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("engine never failed on a 402-rejecting node")
		}
		userDial(ot)
		clk.advance(probeStep)
		hm.probeOnce(ot, st, time.Now())
		time.Sleep(time.Millisecond)
	}
	_, le := hs.engineState()
	if le == nil || le.Code != ErrCodePaymentRequired {
		t.Fatalf("lastError = %v, want 402", le)
	}
	hs.servers.mu.Lock()
	n := hs.servers.setups["b.example"]
	hs.servers.mu.Unlock()
	if n != 0 {
		t.Fatalf("fallback b was set up %d times on a 402 — must not spend candidates", n)
	}
}

type rejectingDialer struct {
	*deadNodeDialer
	err error
}

func (d *rejectingDialer) DialTCP(context.Context, string, []byte) (net.Conn, error) {
	d.dials.Add(1)
	return nil, d.err
}

// --- stale wire error after a Phase-A success (PVE S3/S6 caveat) -----------

func TestPhaseASuccessClearsStaleWireError(t *testing.T) {
	hs := newFoHarness(t, foOpts{net: []checkResult{checkMatched}})
	hs.escalate() // a dead, no candidates → retry schedule, reconnecting 503
	hs.waitStatus("reconnecting 503", isReconnectingCode(ErrCodeServerUnreachable))
	hs.e.ReportWireError(errors.New("dial tcp: connection refused")) // a proxy-dial 503 during the outage
	if _, le := hs.engineState(); le == nil {
		t.Fatal("precondition: no wire error recorded")
	}

	hs.servers.setAlive("a.example", true)
	hs.nextTimer().fire()
	hs.waitStatus("connected with no error", func(s Status) bool {
		return s.State == StateConnected && s.Error == nil
	})
	if gs := hs.e.GetStatus(); gs.State != StateConnected || gs.Error != nil {
		t.Fatalf("GetStatus = %+v, want connected/nil right after the Phase-A success", gs)
	}
}

func TestPhaseASuccessKeepsStickyPaymentError(t *testing.T) {
	hs := newFoHarness(t, foOpts{net: []checkResult{checkMatched}})
	hs.servers.setAlive("a.example", true)
	hs.e.mu.Lock()
	hs.e.lastError = newEngineError(ErrCodePaymentRequired, "membership lapsed")
	hs.e.mu.Unlock()
	hs.signal() // Phase A succeeds
	eventually(t, "phase A ran", func() bool { return hs.servers.raceCount("a.example") == 1 })
	eventually(t, "streak settled", func() bool { return ReRaceFailStreak(hs.e, hs.target) == 0 })
	time.Sleep(20 * time.Millisecond)
	if _, le := hs.engineState(); le == nil || le.Code != ErrCodePaymentRequired {
		t.Fatalf("lastError = %v, want the sticky 402", le)
	}
}

// --- fix round 1 ------------------------------------------------------------

// Review C1, end to end: the node keeps answering the code-less legacy
// "unauthorized" (k2s cannot reach Center), with no live connection and user
// traffic trying. That must never strike, never re-race and never fail the
// engine with 401 — the client stays connected and heals on the next lazy
// redial once Center answers, as before the dead-node change.
func TestEchoDeadNode_CodeLessRejectNeverFails401(t *testing.T) {
	hs := newFoHarness(t, foOpts{
		fallbacks: map[string][]string{foURLA: {foURLB}},
		net:       []checkResult{checkMatched},
	})
	hs.servers.setAlive("b.example", true)
	codeLess := &wire.ProxyLayerError{Err: fmt.Errorf("%w: %w", wire.ErrMetadataRejected,
		fmt.Errorf("%w: unauthorized", wire.ErrStreamRejected))}
	raceTransportFn = func(ctx context.Context, cfg wire.Config, _ string, _ wire.DirectDialer, _ *wire.CookieJar) (*wire.RaceResult, error) {
		hs.servers.mu.Lock()
		hs.servers.races[cfg.Host]++
		hs.servers.mu.Unlock()
		return nil, fmt.Errorf("transport race: %w", codeLess)
	}
	d := &rejectingDialer{deadNodeDialer: newDeadNodeDialer(), err: codeLess}
	hs.e.mu.Lock()
	hs.target.tm.SetPrimary(d, "quic-443")
	ot := hs.target
	hs.e.mu.Unlock()
	hm := newHealthMonitor(ot, []*outboundTransport{ot}, nil, nil, nil, time.Now())
	hm.reRaceChan = hs.e.reRaceChan
	st := &echoLoopState{}

	for i := 0; i < 20*echoReraceThreshold; i++ {
		userDial(ot)
		hm.probeOnce(ot, st, time.Now())
		if st.strikes != 0 {
			t.Fatalf("probe %d: strikes=%d on a code-less reject", i, st.strikes)
		}
	}
	time.Sleep(20 * time.Millisecond) // let the loop act on anything queued
	if n := d.dials.Load(); n != 20*echoReraceThreshold {
		t.Fatalf("precondition: %d user dials, want %d", n, 20*echoReraceThreshold)
	}
	if n := hs.servers.raceCount("a.example"); n != 0 {
		t.Fatalf("code-less reject triggered %d re-races", n)
	}
	if s, le := hs.engineState(); s != StateConnected || le != nil {
		t.Fatalf("engine = %s / %v, want connected / nil (Center outage must not disconnect)", s, le)
	}
	if hs.h.fatalCount() != 0 {
		t.Fatalf("OnOutboundFatal = %d", hs.h.fatalCount())
	}
}

// Review I1: strikes from before a Reset never combine with a failure after
// it. Two strikes, Reset, one fresh failure → strikes 1, no re-race.
func TestEchoProbe_StrikesDroppedAcrossReset(t *testing.T) {
	for name, reset := range map[string]func(*wire.TransportManager){
		"Reset":            (*wire.TransportManager).Reset,
		"ResetConnections": (*wire.TransportManager).ResetConnections,
	} {
		t.Run(name, func(t *testing.T) {
			ot, _ := newDeadNodeTransport("t-reset-strikes")
			hm, ch := newHMForEcho(t, []*outboundTransport{ot})
			st := &echoLoopState{}
			for i := 0; i < echoReraceThreshold-1; i++ {
				userDial(ot)
				hm.probeOnce(ot, st, time.Now())
			}
			if st.strikes != echoReraceThreshold-1 {
				t.Fatalf("precondition: strikes=%d, want %d", st.strikes, echoReraceThreshold-1)
			}
			reset(ot.tm)
			userDial(ot) // first dial on the new path fails
			hm.probeOnce(ot, st, time.Now())
			if st.strikes != 1 {
				t.Fatalf("strikes=%d after %s + one failure, want 1", st.strikes, name)
			}
			if reraced(ch) {
				t.Fatalf("re-raced on strikes carried across %s", name)
			}
		})
	}
}

// Review I2 (controller ruling): while the engine's network is reported down
// (mobile airplane mode / no network), failing dials never strike.
func TestEchoProbe_NetworkDownFailingDialsNoStrike(t *testing.T) {
	ot, _ := newDeadNodeTransport("t-netdown")
	hm, ch := newHMForEcho(t, []*outboundTransport{ot})
	var up atomic.Bool
	hm.netUp = up.Load
	clk := useFakeClock(hm, ot)
	st := &echoLoopState{}
	for i := 0; i < 4*echoReraceThreshold; i++ {
		userDial(ot)
		clk.advance(probeStep)
		hm.probeOnce(ot, st, time.Now())
		if st.strikes != 0 {
			t.Fatalf("probe %d: strikes=%d with the network down", i, st.strikes)
		}
	}
	if reraced(ch) {
		t.Fatal("re-raced with the network down")
	}
	// Offline cadence is the old not-probed ramp, not the failing 2s.
	if want := int32(len(echoIntervals) - 1); st.trustIdx != want {
		t.Fatalf("network down: trustIdx=%d, want the 16s ceiling %d", st.trustIdx, want)
	}
	// Control: the same traffic with the network up does strike.
	up.Store(true)
	for i := 0; i < echoReraceThreshold; i++ {
		userDial(ot)
		clk.advance(probeStep)
		hm.probeOnce(ot, st, time.Now())
	}
	if !reraced(ch) {
		t.Fatal("control: network up, failing dials should re-race")
	}
}

// The engine wires hm.netUp to its netCoordinator.
func TestEngine_HealthNetUpFollowsNetCoordinator(t *testing.T) {
	e := New()
	hm := newHealthMonitor(nil, nil, nil, nil, nil, time.Now())
	nc := newNetCoordinator(func() {}, func() {})
	t.Cleanup(nc.close)
	e.mu.Lock()
	e.netCoord = nc
	e.mu.Unlock()
	e.wireHealthNetUp(hm)
	if !hm.networkUp() {
		t.Fatal("network up at start, gate reports down")
	}
	nc.handleEvent(&NetEvent{Signal: SignalUnavailable, Source: "test"})
	if hm.networkUp() {
		t.Fatal("netCoordinator reports down, gate reports up")
	}
}

// PVE S6: a successful Phase-B swap clears a stale non-user-action wire
// error left by proxy dials on the dead node; 401/402 would stay sticky
// (ClearWireError, covered by TestPhaseASuccessKeepsStickyPaymentError).
func TestPhaseBSwapClearsStaleWireError(t *testing.T) {
	hs := newFoHarness(t, foOpts{
		fallbacks: map[string][]string{foURLA: {foURLB}},
		net:       []checkResult{checkMatched},
	})
	hs.servers.setAlive("b.example", true)
	hs.e.ReportWireError(errors.New("dial tcp: i/o timeout")) // 503 from the dead node
	if _, le := hs.engineState(); le == nil {
		t.Fatal("precondition: no wire error recorded")
	}
	hs.escalate()
	eventually(t, "target switched to b", func() bool { return hs.targetURL() == foURLB })
	eventually(t, "wire error cleared by the swap", func() bool {
		_, le := hs.engineState()
		return le == nil
	})
	if gs := hs.e.GetStatus(); gs.State != StateConnected || gs.Error != nil {
		t.Fatalf("GetStatus = %+v, want connected/nil after the Phase-B swap", gs)
	}
}

// PVE S8 timing: the FIRST Phase-A that returns 401/402 fails the engine
// with that code — no second echo-strike cycle to reach the streak
// threshold, no replacement spent.
func TestReplaceLoop_FirstPhaseAUserActionFailsImmediately(t *testing.T) {
	for _, code := range []int{ErrCodeAuthRejected, ErrCodePaymentRequired} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			hs := newFoHarness(t, foOpts{
				fallbacks: map[string][]string{foURLA: {foURLB}},
				net:       []checkResult{checkMatched},
			})
			hs.servers.setAlive("b.example", true)
			rej := &wire.ProxyLayerError{Err: fmt.Errorf("%w: %w", wire.ErrMetadataRejected,
				&wire.StreamError{Code: code, Message: "center verdict"})}
			raceTransportFn = func(ctx context.Context, cfg wire.Config, _ string, _ wire.DirectDialer, _ *wire.CookieJar) (*wire.RaceResult, error) {
				hs.servers.mu.Lock()
				hs.servers.races[cfg.Host]++
				hs.servers.mu.Unlock()
				return nil, fmt.Errorf("transport race: %w", rej)
			}
			hs.signal() // exactly one echo signal → one Phase-A
			eventually(t, "engine failed", func() bool {
				s, _ := hs.engineState()
				return s != StateConnected
			})
			if _, le := hs.engineState(); le == nil || le.Code != code {
				t.Fatalf("lastError = %v, want %d", le, code)
			}
			if n := hs.servers.raceCount("a.example"); n != 1 {
				t.Fatalf("Phase-A ran %d times, want 1", n)
			}
			if hs.provider.count() != 0 {
				t.Fatalf("NextURL called %d times on a %d", hs.provider.count(), code)
			}
			// OnOutboundFatal fires right after fail(), outside e.mu.
			eventually(t, "OnOutboundFatal", func() bool { return hs.h.fatalCount() == 1 })
		})
	}
}

// --- fix round 2 ------------------------------------------------------------

func codeLessReject() error {
	return &wire.ProxyLayerError{Err: fmt.Errorf("%w: %w", wire.ErrMetadataRejected,
		fmt.Errorf("%w: unauthorized", wire.ErrStreamRejected))}
}

func codedReject(code int) error {
	return &wire.ProxyLayerError{Err: fmt.Errorf("%w: %w", wire.ErrMetadataRejected,
		&wire.StreamError{Code: code, Message: "center verdict"})}
}

func TestReplaceLoopUserAction(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int // 0 = not user action
	}{
		{"code-less metadata reject", codeLessReject(), 0},
		{"bare legacy stream reject", fmt.Errorf("%w: unauthorized", wire.ErrStreamRejected), 0},
		{"code-less inside a race exit", fmt.Errorf("transport race: %w", errors.Join(codeLessReject(), errors.New("tcpws: timeout"))), 0},
		{"coded 401", codedReject(401), 401},
		{"coded 402", codedReject(402), 402},
		{"coded 403", codedReject(403), 403},
		{"coded 503 node auth fault", codedReject(503), 0},
		{"transport error", errors.New("dial: connection refused"), 0},
	}
	for _, c := range cases {
		ce := replaceLoopUserAction(c.err)
		got := 0
		if ce != nil {
			got = ce.Code
			if ce.Category != CategoryClient {
				t.Errorf("%s: category %s, want client", c.name, ce.Category)
			}
		}
		if got != c.want {
			t.Errorf("%s: user action %d, want %d", c.name, got, c.want)
		}
	}
	// Initial-connect classification is deliberately unchanged.
	if ce := ClassifyError(codeLessReject()); ce.Code != ErrCodeAuthRejected {
		t.Fatalf("ClassifyError(code-less) = %d, must stay 401 (out of scope)", ce.Code)
	}
}

// N2 ruling, end to end: during the re-race the current node and every
// candidate answer the code-less reject (Center unreachable from the nodes).
// No fail, never 401, reconnecting 503; once auth works again → connected.
func TestReplaceLoop_CodeLessRejectEverywhereRetriesThenRecovers(t *testing.T) {
	hs := newFoHarness(t, foOpts{
		fallbacks: map[string][]string{foURLA: {foURLB, foURLC}},
		net:       []checkResult{checkMatched},
	})
	var authOK atomic.Bool
	raceTransportFn = func(ctx context.Context, cfg wire.Config, _ string, _ wire.DirectDialer, _ *wire.CookieJar) (*wire.RaceResult, error) {
		hs.servers.mu.Lock()
		hs.servers.races[cfg.Host]++
		hs.servers.mu.Unlock()
		if authOK.Load() {
			return &wire.RaceResult{Dialer: &stubDialer{}, TransportType: "quic-443"}, nil
		}
		return nil, fmt.Errorf("transport race: %w", codeLessReject())
	}
	hs.servers.mu.Lock()
	hs.servers.setupErrs["b.example"] = codeLessReject()
	hs.servers.setupErrs["c.example"] = codeLessReject()
	hs.servers.mu.Unlock()

	hs.escalate()
	hs.waitStatus("reconnecting 503", isReconnectingCode(ErrCodeServerUnreachable))
	if s, le := hs.engineState(); s != StateConnected || le != nil {
		t.Fatalf("engine = %s / %v, want connected / nil (a Center outage must not end the session)", s, le)
	}
	if hs.h.sawCode(ErrCodeAuthRejected) || hs.h.sawState(StateDisconnected) {
		t.Fatal("status showed 401 or disconnected for a code-less reject")
	}
	hs.servers.mu.Lock()
	setupsB, setupsC := hs.servers.setups["b.example"], hs.servers.setups["c.example"]
	hs.servers.mu.Unlock()
	if setupsB != 1 || setupsC != 1 {
		t.Fatalf("candidate rotation: setups b=%d c=%d, want 1 each", setupsB, setupsC)
	}
	if hs.h.fatalCount() != 0 {
		t.Fatalf("OnOutboundFatal = %d, want 0", hs.h.fatalCount())
	}

	// A wait tick while Center is still down keeps retrying.
	before := hs.servers.raceCount("a.example")
	hs.nextTimer().fire()
	next := hs.nextTimer() // armed only after the tick's Phase-A failed
	if n := hs.servers.raceCount("a.example"); n != before+1 {
		t.Fatalf("wait tick races on a = %d, want %d", n, before+1)
	}
	if s, _ := hs.h.last(); !isReconnectingCode(ErrCodeServerUnreachable)(s) {
		t.Fatalf("status after a failed tick = %+v, want reconnecting/503", s)
	}

	authOK.Store(true)
	next.fire()
	hs.waitStatus("connected again", func(s Status) bool { return s.State == StateConnected && s.Error == nil })
	if s, le := hs.engineState(); s != StateConnected || le != nil {
		t.Fatalf("engine = %s / %v after auth recovered", s, le)
	}
}

// 403 ruling: a coded 403 is user action like 401/402.
func TestReplaceLoop_Coded403(t *testing.T) {
	t.Run("Phase-A 403 fails immediately", func(t *testing.T) {
		hs := newFoHarness(t, foOpts{
			fallbacks: map[string][]string{foURLA: {foURLB}},
			net:       []checkResult{checkMatched},
		})
		hs.servers.setAlive("b.example", true)
		raceTransportFn = func(ctx context.Context, cfg wire.Config, _ string, _ wire.DirectDialer, _ *wire.CookieJar) (*wire.RaceResult, error) {
			return nil, fmt.Errorf("transport race: %w", codedReject(ErrCodeForbidden))
		}
		hs.signal()
		eventually(t, "engine failed", func() bool { s, _ := hs.engineState(); return s != StateConnected })
		_, le := hs.engineState()
		if le == nil || le.Code != ErrCodeForbidden || le.Category != CategoryClient {
			t.Fatalf("lastError = %+v, want 403/client", le)
		}
		if hs.provider.count() != 0 {
			t.Fatalf("NextURL called %d times on a Phase-A 403", hs.provider.count())
		}
	})
	t.Run("Phase-B candidate 403 outranks 503 at exhaustion", func(t *testing.T) {
		hs := newFoHarness(t, foOpts{
			fallbacks: map[string][]string{foURLA: {foURLB, foURLC}},
			net:       []checkResult{checkMatched},
		})
		// a refuses (transport), b answers 403, c refuses.
		hs.servers.mu.Lock()
		hs.servers.setupErrs["b.example"] = codedReject(ErrCodeForbidden)
		hs.servers.mu.Unlock()
		hs.escalate()
		eventually(t, "engine failed", func() bool { s, _ := hs.engineState(); return s != StateConnected })
		_, le := hs.engineState()
		if le == nil || le.Code != ErrCodeForbidden {
			t.Fatalf("lastError = %+v, want the candidate's 403 over 503", le)
		}
	})
}

// N1: a Reset that lands DURING a probe must still drop that probe's strike
// at the next not-probed probe.
func TestEchoProbe_ResetDuringProbeDropsItsStrike(t *testing.T) {
	ot, d := newDeadNodeTransport("t-reset-during")
	hm, ch := newHMForEcho(t, []*outboundTransport{ot})
	st := &echoLoopState{}
	// Probe 1 runs on a live (dying) connection: it times out, and a network
	// Reset lands while it is running.
	ep := &resetDuringEcho{deadNodeDialer: d, tm: ot.tm}
	ot.tm.SetPrimary(ep, "quic-443")
	hm.probeOnce(ot, st, time.Now())
	if st.strikes != 1 {
		t.Fatalf("precondition: probe 1 strikes=%d, want 1 (echo timeout)", st.strikes)
	}
	// From now on there is no connection; the first post-Reset dial fails.
	ep.noConn.Store(true)
	userDial(ot)
	hm.probeOnce(ot, st, time.Now())
	if st.strikes != 1 {
		t.Fatalf("strikes=%d, want 1: the strike from the probe the Reset interrupted carried over", st.strikes)
	}
	if reraced(ch) {
		t.Fatal("re-raced")
	}
}

type resetDuringEcho struct {
	*deadNodeDialer
	tm     *wire.TransportManager
	noConn atomic.Bool
}

func (r *resetDuringEcho) RunEchoProbe(context.Context) error {
	if r.noConn.Load() {
		return errNoConn
	}
	r.tm.Reset() // the network changed while the probe was in flight
	return context.DeadlineExceeded
}

// N3: a clear racing a fail() must not erase the session's final error.
func TestClearWireError_NotWhenDisconnected(t *testing.T) {
	e := New()
	e.mu.Lock()
	e.state = StateDisconnected
	e.lastError = newEngineError(ErrCodeServerUnreachable, "outbound exhausted")
	e.mu.Unlock()
	e.ClearWireError()
	e.mu.Lock()
	le := e.lastError
	e.mu.Unlock()
	if le == nil {
		t.Fatal("ClearWireError erased the final error of a disconnected engine")
	}
	// Control: connected → cleared.
	e.mu.Lock()
	e.state = StateConnected
	e.mu.Unlock()
	e.ClearWireError()
	e.mu.Lock()
	le = e.lastError
	e.mu.Unlock()
	if le != nil {
		t.Fatalf("control: connected engine kept %v", le)
	}
}

// --- fix round 3 ------------------------------------------------------------

// N2 widened + B: the current node is dead (transport error, NOT code-less)
// and every candidate answers code-less (Center outage). No fail, no
// disconnect, reconnecting 503; the retry ticks re-run Phase B, and once
// Center answers again the client fails over to a candidate.
func TestRetry_DeadNodeCodeLessCandidatesThenCenterRecovers(t *testing.T) {
	hs := newFoHarness(t, foOpts{
		fallbacks: map[string][]string{foURLA: {foURLB, foURLC}},
		net:       []checkResult{checkMatched},
	})
	// a: dead (connection refused). b, c: Center unreachable.
	hs.servers.mu.Lock()
	hs.servers.setupErrs["b.example"] = codeLessReject()
	hs.servers.setupErrs["c.example"] = codeLessReject()
	hs.servers.mu.Unlock()

	hs.escalate()
	hs.waitStatus("reconnecting 503", isReconnectingCode(ErrCodeServerUnreachable))
	tm := hs.nextTimer()
	if s, le := hs.engineState(); s != StateConnected || le != nil {
		t.Fatalf("engine = %s / %v, want connected (a Center outage must not end the session)", s, le)
	}

	// A wait tick while Center is still down: Phase A on a, then Phase B
	// over the (Reset) candidates — still code-less → stays scheduled.
	tm.fire()
	tm = hs.nextTimer()
	hs.servers.mu.Lock()
	setupsB := hs.servers.setups["b.example"]
	hs.servers.mu.Unlock()
	if setupsB != 2 {
		t.Fatalf("setups on b after one wait tick = %d, want 2 (escalate + the tick's Phase B)", setupsB)
	}
	if s, _ := hs.engineState(); s != StateConnected || hs.h.sawState(StateDisconnected) || hs.h.sawCode(ErrCodeAuthRejected) {
		t.Fatalf("engine left connected / showed 401 during the outage")
	}

	// Center recovers: b admits. The next tick fails over (a stays dead).
	hs.servers.mu.Lock()
	delete(hs.servers.setupErrs, "b.example")
	hs.servers.mu.Unlock()
	hs.servers.setAlive("b.example", true)
	tm.fire()
	eventually(t, "failed over to b on a retry tick", func() bool { return hs.targetURL() == foURLB })
	hs.waitStatus("connected, no hold", func(s Status) bool { return s.State == StateConnected && s.Error == nil })
	if hs.h.fatalCount() != 0 {
		t.Fatalf("OnOutboundFatal = %d", hs.h.fatalCount())
	}
}

// B: the target entered the schedule for a Center outage while its node was
// alive (everything code-less); then the node dies. Once Center is back the
// retry tick must leave the dead node for a live candidate rather than race
// it forever.
func TestRetry_CodeLessScheduleNodeDiesThenFailsOver(t *testing.T) {
	hs := newFoHarness(t, foOpts{
		fallbacks: map[string][]string{foURLA: {foURLB}},
		net:       []checkResult{checkMatched},
	})
	var aState atomic.Int32 // 0 code-less, 1 dead
	raceTransportFn = func(ctx context.Context, cfg wire.Config, _ string, _ wire.DirectDialer, _ *wire.CookieJar) (*wire.RaceResult, error) {
		hs.servers.mu.Lock()
		hs.servers.races[cfg.Host]++
		hs.servers.mu.Unlock()
		if aState.Load() == 0 {
			return nil, fmt.Errorf("transport race: %w", codeLessReject())
		}
		return nil, errors.New("transport race: dial a.example: connection refused")
	}
	hs.servers.mu.Lock()
	hs.servers.setupErrs["b.example"] = codeLessReject()
	hs.servers.mu.Unlock()

	hs.escalate()
	hs.waitStatus("reconnecting 503", isReconnectingCode(ErrCodeServerUnreachable))
	tm := hs.nextTimer()

	aState.Store(1) // the node dies; Center still down
	tm.fire()
	tm = hs.nextTimer()
	if hs.targetURL() != foURLA {
		t.Fatalf("target moved to %s while every candidate is code-less", hs.targetURL())
	}

	hs.servers.mu.Lock()
	delete(hs.servers.setupErrs, "b.example")
	hs.servers.mu.Unlock()
	hs.servers.setAlive("b.example", true) // Center back
	tm.fire()
	eventually(t, "left the dead node for b", func() bool { return hs.targetURL() == foURLB })
	if s, le := hs.engineState(); s != StateConnected || le != nil {
		t.Fatalf("engine = %s / %v after the failover", s, le)
	}
}

// Review A: the target's Phase-A failure streak decays when its node is
// seen answering again — an answered user dial ending the failure run, or a
// successful echo — but not on a Reset.
func TestEchoProbe_RecoveryResetsReRaceStreak(t *testing.T) {
	ot, d := newDeadNodeTransport("t-recover")
	hm, _ := newHMForEcho(t, []*outboundTransport{ot})
	e := New()
	e.wireHealthRecovered(hm)
	streak := func() int {
		e.mu.Lock()
		defer e.mu.Unlock()
		return ot.reRaceFailStreak
	}
	set := func(n int) {
		e.mu.Lock()
		ot.reRaceFailStreak = n
		e.mu.Unlock()
	}
	st := &echoLoopState{}

	t.Run("answered dial ends the run", func(t *testing.T) {
		userDial(ot)
		hm.probeOnce(ot, st, time.Now()) // sees the run
		set(1)
		d.fail.Store(false)
		userDial(ot) // the node answers
		d.fail.Store(true)
		hm.probeOnce(ot, st, time.Now())
		if streak() != 0 {
			t.Fatalf("streak = %d after the node answered, want 0", streak())
		}
	})
	t.Run("Reset is not recovery", func(t *testing.T) {
		userDial(ot)
		hm.probeOnce(ot, st, time.Now())
		set(1)
		ot.tm.Reset()
		hm.probeOnce(ot, st, time.Now())
		if streak() != 1 {
			t.Fatalf("streak = %d after a Reset, want 1 kept", streak())
		}
	})
	t.Run("echo answered", func(t *testing.T) {
		set(1)
		eo := &echoStubDialer{} // nil probe fn → echo OK
		ot.tm.SetPrimary(eo, "quic-443")
		hm.probeOnce(ot, st, time.Now())
		if streak() != 0 {
			t.Fatalf("streak = %d after an answered echo, want 0", streak())
		}
	})
}

// Review D: strikes frozen through a failure run's quiet ticks are dropped
// when the run ends, so one echo failure on the fresh connection that
// follows cannot re-race on its own.
func TestEchoProbe_FrozenStrikesDroppedWhenRunEnds(t *testing.T) {
	ot, d := newDeadNodeTransport("t-frozen")
	hm, ch := newHMForEcho(t, []*outboundTransport{ot})
	clk := useFakeClock(hm, ot)
	st := &echoLoopState{}
	for i := 0; i < echoReraceThreshold-1; i++ {
		userDial(ot)
		clk.advance(time.Second)
		hm.probeOnce(ot, st, time.Now())
	}
	if st.strikes != echoReraceThreshold-1 {
		t.Fatalf("precondition: strikes=%d", st.strikes)
	}
	// The node answers (run over) and the next probe reaches a fresh
	// connection whose echo fails once.
	d.fail.Store(false)
	userDial(ot)
	eo := &echoStubDialer{}
	eo.fn = func(context.Context) error { return context.DeadlineExceeded }
	ot.tm.SetPrimary(eo, "quic-443")
	hm.probeOnce(ot, st, time.Now())
	if st.strikes != 1 || reraced(ch) {
		t.Fatalf("strikes=%d rerace=%v: frozen strikes leaked into the echo path", st.strikes, reraced(ch))
	}
}

// Review E: a Phase-B swap installs a TransportManager whose failure seq
// restarts at 0; its first failure must be fresh to the probe even when the
// old seq happened to be 1.
func TestEchoProbe_SwapRestartsDialFailureBookkeeping(t *testing.T) {
	ot, _ := newDeadNodeTransport("t-swap")
	hm, _ := newHMForEcho(t, []*outboundTransport{ot})
	clk := useFakeClock(hm, ot)
	st := &echoLoopState{}
	userDial(ot) // old TM: seq 1
	clk.advance(time.Second)
	hm.probeOnce(ot, st, time.Now())
	if st.strikes != 1 {
		t.Fatalf("precondition: strikes=%d", st.strikes)
	}
	// Phase B swaps in a new TM (dead too); its first failure is seq 1.
	nt, _ := newDeadNodeTransport("t-swap-new")
	nt.tm.SetConnFailClock(clk.now)
	ot.tm = nt.tm
	userDial(ot)
	clk.advance(time.Second)
	hm.probeOnce(ot, st, time.Now())
	if st.connFailSeen != 1 || st.strikes != 2 {
		t.Fatalf("after the swap: seen=%d strikes=%d, want the new TM's failure counted (seen 1, strikes 2)", st.connFailSeen, st.strikes)
	}
}
