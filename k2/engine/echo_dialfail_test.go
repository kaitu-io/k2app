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

// --- pure functions -------------------------------------------------------

func TestEchoStepDial_NotProbedWithDialFailureStrikes(t *testing.T) {
	r := echoStepDial(0, 0, errNoConn, true)
	if r.Classify != echoNotProbedDialFailing || r.NewStrikes != 1 || r.ShouldRerace {
		t.Fatalf("strike 1: %+v", r)
	}
	r = echoStepDial(r.NewTrust, r.NewStrikes, errNoConn, true)
	if r.NewStrikes != 2 || r.ShouldRerace {
		t.Fatalf("strike 2: %+v", r)
	}
	r = echoStepDial(r.NewTrust, r.NewStrikes, errNoConn, true)
	if r.NewStrikes != echoReraceThreshold || !r.ShouldRerace {
		t.Fatalf("strike 3 must re-race: %+v", r)
	}
}

// Trust arithmetic (hence probe cadence) must be the not-probed one in every
// state, with or without dial evidence: only strikes may differ.
func TestEchoStepDial_TrustArithmeticUnchangedGrid(t *testing.T) {
	maxTrust := int32(len(echoIntervals)) - 1
	for trust := int32(0); trust <= maxTrust+1; trust++ {
		for strikes := int32(0); strikes <= echoReraceThreshold+1; strikes++ {
			ref := echoStep(trust, strikes, errNoConn)
			got := echoStepDial(trust, strikes, errNoConn, true)
			if got.NewTrust != ref.NewTrust {
				t.Fatalf("(trust=%d strikes=%d): trust %d, not-probed gives %d — cadence changed", trust, strikes, got.NewTrust, ref.NewTrust)
			}
			if got.NewStrikes != strikes+1 || got.ShouldRerace != (strikes+1 >= echoReraceThreshold) {
				t.Fatalf("(trust=%d strikes=%d): %+v", trust, strikes, got)
			}
		}
	}
}

// Without dial evidence, or for any outcome other than not-probed,
// echoStepDial is echoStep.
func TestEchoStepDial_OtherwiseIdenticalToEchoStep(t *testing.T) {
	errs := []error{nil, errNoConn, context.DeadlineExceeded, errors.New("echo: read response: EOF")}
	maxTrust := int32(len(echoIntervals)) - 1
	for _, err := range errs {
		for trust := int32(0); trust <= maxTrust; trust++ {
			for strikes := int32(0); strikes <= echoReraceThreshold; strikes++ {
				ref := echoStep(trust, strikes, err)
				if got := echoStepDial(trust, strikes, err, false); got != ref {
					t.Fatalf("no evidence, err=%v (%d,%d): %+v != %+v", err, trust, strikes, got, ref)
				}
				if wire.IsEchoNotProbed(err) {
					continue
				}
				if got := echoStepDial(trust, strikes, err, true); got != ref {
					t.Fatalf("evidence must only affect not-probed; err=%v (%d,%d): %+v != %+v", err, trust, strikes, got, ref)
				}
			}
		}
	}
}

func TestNotProbedDialFailing(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	cases := []struct {
		name      string
		seq, seen uint64
		last      time.Time
		want      bool
	}{
		{"new recent failure", 5, 4, now.Add(-time.Second), true},
		{"no new failure since last probe", 5, 5, now.Add(-time.Second), false},
		{"cleared by success or Reset", 5, 4, time.Time{}, false},
		{"older than window", 5, 4, now.Add(-echoDialFailWindow), false},
		{"just inside window", 5, 4, now.Add(-echoDialFailWindow + time.Millisecond), true},
	}
	for _, c := range cases {
		if got := notProbedDialFailing(c.seq, c.seen, c.last, now); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
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
	st := &echoLoopState{}
	for i := 1; i <= echoReraceThreshold; i++ {
		userDial(ot)
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
