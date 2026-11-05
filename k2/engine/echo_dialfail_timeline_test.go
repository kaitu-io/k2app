package engine

// S9 cadence ruling (2026-11-05): while user dials are failing with no live
// connection the echo probe speeds up to 2s, and re-races are gated by how
// long the run of failures has lasted. Two bounds, driven on a fake clock
// through the real probeOnce, TransportManager and outboundReplaceLoop:
//   - dead node, live fallback: Phase B starts ≤ 40s after the first failing
//     user dial (target 45s for DIAG: outbound-failover; 5s left for the
//     pre-Phase-B netcheck and the fallback's setup race, both instant here);
//   - a node refusing for only 20s, ~5s into the session: never Phase B.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kaitu-io/k2/wire"
)

// simNode is one k2s node on the fake timeline: alive or refusing, and
// whether the client holds a live connection to it (a kill closes it; the
// next answered dial re-opens it).
type simNode struct {
	alive atomic.Bool
	conn  atomic.Bool
}

// simDialer dials simNode: refused while it is down; echo is not-probed
// without a live connection, answered with one.
type simDialer struct{ n *simNode }

func (d *simDialer) dial() error {
	if !d.n.alive.Load() {
		return errors.New("wire: tcpws dial: connect: connection refused")
	}
	d.n.conn.Store(true)
	return nil
}

func (d *simDialer) DialTCP(context.Context, string, []byte) (net.Conn, error) {
	if err := d.dial(); err != nil {
		return nil, err
	}
	c1, c2 := net.Pipe()
	c2.Close()
	return c1, nil
}
func (d *simDialer) DialUDP(context.Context, string) (net.PacketConn, error) {
	return nil, d.dial()
}
func (d *simDialer) DialStreamUDP(context.Context, string) (net.PacketConn, error) {
	return nil, d.dial()
}
func (d *simDialer) Reset()       {}
func (d *simDialer) Close() error { return nil }
func (d *simDialer) RunEchoProbe(context.Context) error {
	if !d.n.conn.Load() {
		return errNoConn
	}
	return nil
}

type simRace struct {
	done chan error
}

// downWindow is one outage of the node: refusing from..to (to=0: forever).
type downWindow struct{ from, to time.Duration }

type timelineOpts struct {
	// trust0 is the echo trust index when the run starts at t=0 (connect:
	// 0; steady state: the 16s ceiling) and firstProbe is when the first
	// probe of the timeline fires.
	trust0     int32
	firstProbe time.Duration
	down       []downWindow
	dialEvery  time.Duration // user traffic: one dial per period, from t=0
	horizon    time.Duration
	noFallback bool // manual mode: no candidates
	// untilPhaseB: stop at the first Phase B (failover bound).
	untilPhaseB bool
}

type timelineResult struct {
	firstFail time.Duration // first failing user dial, -1 = none
	phaseBAt  time.Duration // first Phase-B setup of a fallback, -1 = none
	races     int           // Phase-A re-races on the node
	probes    int
	netchecks int
	warns     int // DIAG: echo-probe-fail at Warn
	state     string
}

// raceTime is the Phase-A budget against a dead node (wire raceTimeout).
const raceTime = 5 * time.Second

// warnCounter counts DIAG: echo-probe-fail records at Warn.
type warnCounter struct{ n atomic.Int64 }

func (w *warnCounter) Enabled(context.Context, slog.Level) bool { return true }
func (w *warnCounter) Handle(_ context.Context, r slog.Record) error {
	if r.Level == slog.LevelWarn && r.Message == "DIAG: echo-probe-fail" {
		w.n.Add(1)
	}
	return nil
}
func (w *warnCounter) WithAttrs([]slog.Attr) slog.Handler { return w }
func (w *warnCounter) WithGroup(string) slog.Handler      { return w }

// runDeadNodeTimeline is a discrete-event simulation on a fake clock that
// drives the real probeOnce, TransportManager (user-dial evidence) and
// outboundReplaceLoop (Phase A, escalate, Phase B, retry schedule). The
// driver only acts while the replace loop is idle (e.replaceLoopIdle) or
// blocked in a Phase-A race it controls, so fake time is exact.
func runDeadNodeTimeline(t *testing.T, o timelineOpts) timelineResult {
	t.Helper()
	wc := &warnCounter{}
	prevLog := slog.Default()
	slog.SetDefault(slog.New(wc))
	t.Cleanup(func() { slog.SetDefault(prevLog) })

	idle := make(chan struct{}, 1)
	fb := map[string][]string{foURLA: {foURLB}}
	if o.noFallback {
		fb = nil
	}
	hs := newFoHarness(t, foOpts{
		fallbacks: fb,
		net:       []checkResult{checkMatched},
		idle: func() {
			select {
			case idle <- struct{}{}:
			default:
			}
		},
	})
	hs.servers.setAlive("b.example", true)
	node := &simNode{}
	node.alive.Store(true)
	node.conn.Store(true)

	races := make(chan simRace)
	clk := newDFClock()
	t0 := clk.now()
	at := func() time.Duration { return clk.now().Sub(t0) }
	raceTransportFn = func(ctx context.Context, cfg wire.Config, _ string, _ wire.DirectDialer, _ *wire.CookieJar) (*wire.RaceResult, error) {
		r := simRace{done: make(chan error)}
		select {
		case races <- r:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		var err error
		select {
		case err = <-r.done:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		if err != nil {
			return nil, err
		}
		node.conn.Store(true)
		return &wire.RaceResult{Dialer: &simDialer{n: node}, TransportType: "tcpws"}, nil
	}

	hs.e.mu.Lock()
	ot := hs.target
	ot.tm.SetPrimary(&simDialer{n: node}, "tcpws")
	hs.e.mu.Unlock()
	ot.tm.SetConnFailClock(clk.now)
	hm := newHealthMonitor(ot, []*outboundTransport{ot}, nil, nil, nil, time.Now())
	hm.clock = clk.now
	hs.e.wireHealthRecovered(hm)
	sig := make(chan *outboundTransport, 1)
	hm.reRaceChan = sig
	st := &echoLoopState{trustIdx: o.trust0}

	res := timelineResult{firstFail: -1, phaseBAt: -1}
	var inflight *simRace
	var raceEnd time.Time
	var timer *foTimer
	nextDial := t0
	nextProbe := t0.Add(o.firstProbe)
	setups := func() int {
		hs.servers.mu.Lock()
		defer hs.servers.mu.Unlock()
		return hs.servers.setups["b.example"]
	}
	// settle waits until the loop is idle again or blocked in a race.
	settle := func() {
		select {
		case r := <-races:
			inflight, raceEnd = &r, clk.now().Add(raceTime)
			res.races++
		case <-idle:
		case <-time.After(5 * time.Second):
			t.Fatalf("t=%v: replace loop neither idle nor racing", at())
		}
		for {
			select {
			case tm := <-hs.timers:
				timer = &tm // the latest armed timer is the live one
				continue
			default:
			}
			break
		}
		if res.phaseBAt < 0 && setups() > 0 {
			res.phaseBAt = at()
		}
	}
	drainIdle := func() {
		select {
		case <-idle:
		default:
		}
	}
	settle() // the loop's first idle

	for at() <= o.horizon && !(o.untilPhaseB && res.phaseBAt >= 0) {
		if s, _ := hs.engineState(); s != StateConnected {
			break
		}
		next := nextDial
		if nextProbe.Before(next) {
			next = nextProbe
		}
		if inflight != nil && raceEnd.Before(next) {
			next = raceEnd
		}
		if inflight == nil && timer != nil && timer.at.Before(next) {
			next = timer.at
		}
		clk.set(next)
		hs.clock.advanceTo(next)
		now := at()
		down := false
		for _, w := range o.down {
			if now >= w.from && (w.to == 0 || now < w.to) {
				down = true
			}
		}
		if node.alive.Load() == down {
			node.alive.Store(!down)
			if down {
				node.conn.Store(false) // the kill closes the cached connection
			}
		}

		switch {
		case inflight != nil && !raceEnd.After(clk.now()):
			var err error
			if !node.alive.Load() {
				err = fmt.Errorf("transport race: dial %s: connection refused", "a.example")
			}
			drainIdle()
			inflight.done <- err
			inflight = nil
			settle()
		case inflight == nil && timer != nil && !timer.at.After(clk.now()):
			tm := timer
			timer = nil
			drainIdle()
			tm.ch <- time.Time{}
			settle()
		case !nextProbe.After(clk.now()):
			hm.probeOnce(ot, st, time.Now())
			res.probes++
			nextProbe = clk.now().Add(echoIntervals[st.trustIdx])
			select {
			case target := <-sig:
				if inflight == nil {
					drainIdle()
					select {
					case hs.e.reRaceChan <- target:
					case <-time.After(5 * time.Second):
						t.Fatal("replace loop not consuming")
					}
					settle()
				} else {
					// The loop is busy in a race: queue it (cap 1) or drop
					// it, like the probe's select-default does.
					select {
					case hs.e.reRaceChan <- target:
					default:
					}
				}
			default:
			}
		default: // user dial
			before := ot.tm.ConnectFailures().Seq
			if !(o.untilPhaseB && res.phaseBAt >= 0) {
				userDial(ot)
			}
			if res.firstFail < 0 && ot.tm.ConnectFailures().Seq != before {
				res.firstFail = now
			}
			nextDial = clk.now().Add(o.dialEvery)
		}
	}
	res.netchecks = hs.net.callCount()
	res.warns = int(wc.n.Load())
	res.state, _ = hs.engineState()
	return res
}

// Bound 1: dead node with a live fallback. Steady state (16s cadence), the
// first probe after the death anywhere in the 16s window, user traffic at
// 1-3s per failing dial (PVE S9 measured ~1.6 refused connects/s).
func TestDeadNodeTimeline_FailoverWithin45s(t *testing.T) {
	const bound = 40 * time.Second // 45s target − 5s netcheck + fallback setup
	maxTrust := int32(len(echoIntervals) - 1)
	worst := time.Duration(0)
	for _, every := range []time.Duration{time.Second, 2 * time.Second, 3 * time.Second} {
		for phase := time.Second; phase <= echoIntervals[maxTrust]; phase += time.Second {
			name := fmt.Sprintf("dial%v/probe+%v", every, phase)
			r := runDeadNodeTimeline(t, timelineOpts{
				trust0: maxTrust, firstProbe: 60*time.Second + phase,
				down: []downWindow{{from: 60 * time.Second}}, dialEvery: every,
				horizon: 5 * time.Minute, untilPhaseB: true,
			})
			if r.firstFail < 0 || r.phaseBAt < 0 {
				t.Fatalf("%s: no failover (%+v)", name, r)
			}
			took := r.phaseBAt - r.firstFail
			worst = max(worst, took)
			t.Logf("%s: Phase B %v after the first failing dial", name, took)
			if took > bound {
				t.Errorf("%s: Phase B %v after the first failing dial (races=%d), bound %v", name, took, r.races, bound)
			}
			if r.races != reRaceFailThresholdForReplace {
				t.Errorf("%s: %d Phase-A re-races before Phase B, want %d", name, r.races, reRaceFailThresholdForReplace)
			}
		}
	}
	t.Logf("worst case: Phase B %v after the first failing user dial", worst)
}

// Bound 2: k2s stopped for 20s about 5s after connect, with traffic. The
// node must come back to the same session: at most one failed Phase-A, no
// Phase B, and no failover for the rest of the run.
func TestDeadNodeTimeline_20sRefusalNeverPhaseB(t *testing.T) {
	for _, death := range []time.Duration{3 * time.Second, 5 * time.Second, 6 * time.Second, 8 * time.Second} {
		for _, every := range []time.Duration{time.Second, 2 * time.Second, 3 * time.Second} {
			name := fmt.Sprintf("down@%v/dial%v", death, every)
			r := runDeadNodeTimeline(t, timelineOpts{
				trust0: 0, firstProbe: echoIntervals[0],
				down:      []downWindow{{from: death, to: death + 20*time.Second}},
				dialEvery: every, horizon: 3 * time.Minute,
			})
			if r.firstFail < 0 {
				t.Fatalf("%s: precondition: no failing user dial", name)
			}
			if r.phaseBAt >= 0 {
				t.Errorf("%s: Phase B at %v for a 20s refusal (races=%d)", name, r.phaseBAt, r.races)
			}
			t.Logf("%s: first failing dial %v, Phase-A re-races %d", name, r.firstFail, r.races)
		}
	}
}

// Idle: no traffic on a dead node — the timeline never strikes, never
// re-races and the cadence backs off to the 16s ceiling as before.
func TestDeadNodeTimeline_IdleNoStrikesOldCadence(t *testing.T) {
	ot, d := newDeadNodeTransport("t-idle-clock")
	hm, ch := newHMForEcho(t, []*outboundTransport{ot})
	clk := useFakeClock(hm, ot)
	st := &echoLoopState{}
	var intervals []time.Duration
	for i := 0; i < 50; i++ {
		hm.probeOnce(ot, st, time.Now())
		iv := echoIntervals[st.trustIdx]
		intervals = append(intervals, iv)
		clk.advance(iv)
		if st.strikes != 0 || reraced(ch) {
			t.Fatalf("probe %d: strikes=%d rerace=%v while idle", i, st.strikes, reraced(ch))
		}
	}
	for i, want := range []time.Duration{2, 4, 8, 16, 16, 16} {
		if intervals[i] != want*time.Second {
			t.Fatalf("idle cadence %v, want 2s,4s,8s,16s,16s… (unchanged)", intervals[:6])
		}
	}
	if d.dials.Load() != 0 {
		t.Fatalf("idle probes dialled %d times", d.dials.Load())
	}
}

// The failing cadence stops with the evidence: 30s after the last failure
// the probe backs off again.
func TestEchoProbe_FailingCadenceEndsWithTheRun(t *testing.T) {
	ot, _ := newDeadNodeTransport("t-cadence-end")
	hm, _ := newHMForEcho(t, []*outboundTransport{ot})
	clk := useFakeClock(hm, ot)
	st := &echoLoopState{trustIdx: int32(len(echoIntervals) - 1)}
	userDial(ot)
	clk.advance(time.Second)
	hm.probeOnce(ot, st, time.Now())
	if echoIntervals[st.trustIdx] != 2*time.Second {
		t.Fatalf("failing: next probe in %v, want 2s", echoIntervals[st.trustIdx])
	}
	clk.advance(echoDialFailWindow)
	for i := 0; i < len(echoIntervals); i++ {
		hm.probeOnce(ot, st, time.Now())
	}
	if want := int32(len(echoIntervals) - 1); st.trustIdx != want || st.strikes != 0 {
		t.Fatalf("30s after the last failure: trust=%d strikes=%d, want %d/0", st.trustIdx, st.strikes, want)
	}
}

// The run-age gate: three fresh strikes inside 8s do not re-race; the 2nd
// re-race of the same run waits for 24s.
func TestEchoProbe_ReraceMilestonesFromFirstFailure(t *testing.T) {
	ot, d := newDeadNodeTransport("t-milestone")
	hm, ch := newHMForEcho(t, []*outboundTransport{ot})
	clk := useFakeClock(hm, ot)
	st := &echoLoopState{}
	var fired []time.Duration
	start := clk.now()
	for clk.now().Sub(start) < 40*time.Second {
		userDial(ot)
		clk.advance(time.Second)
		hm.probeOnce(ot, st, time.Now())
		if reraced(ch) {
			fired = append(fired, clk.now().Sub(start))
		}
	}
	if len(fired) < 2 || fired[0] != dialFailReraceAfter(0) || fired[1] != dialFailReraceAfter(1) {
		t.Fatalf("re-races at %v, want first at %v and second at %v", fired, dialFailReraceAfter(0), dialFailReraceAfter(1))
	}
	// The node answers once: that run is over. A new run starts again from
	// the first milestone, not from where the old one stopped.
	d.fail.Store(false)
	userDial(ot)
	d.fail.Store(true)
	fired = nil
	start = clk.now()
	for clk.now().Sub(start) < 20*time.Second {
		userDial(ot)
		clk.advance(time.Second)
		hm.probeOnce(ot, st, time.Now())
		if reraced(ch) {
			fired = append(fired, clk.now().Sub(start))
		}
	}
	if len(fired) != 1 || fired[0] != dialFailReraceAfter(0) {
		t.Fatalf("new run: re-races at %v, want exactly one at %v", fired, dialFailReraceAfter(0))
	}
}

// Review A ruling: two separate 20s refusals in one session, the node
// answering in between — the first one's failed Phase-A must not count
// toward the second: no Phase B.
func TestDeadNodeTimeline_TwoRefusalsOneSessionNeverPhaseB(t *testing.T) {
	for _, every := range []time.Duration{time.Second, 2 * time.Second, 3 * time.Second} {
		name := fmt.Sprintf("dial%v", every)
		r := runDeadNodeTimeline(t, timelineOpts{
			trust0: 0, firstProbe: echoIntervals[0],
			down: []downWindow{
				{from: 5 * time.Second, to: 25 * time.Second},
				{from: 125 * time.Second, to: 145 * time.Second},
			},
			dialEvery: every, horizon: 4 * time.Minute,
		})
		if r.phaseBAt >= 0 {
			t.Errorf("%s: Phase B at %v after two separate 20s refusals (races=%d)", name, r.phaseBAt, r.races)
		}
		if r.races != 2 {
			t.Errorf("%s: %d Phase-A re-races, want 2 (one failed per refusal — precondition)", name, r.races)
		}
		t.Logf("%s: Phase-A re-races %d, Phase B %v", name, r.races, r.phaseBAt)
	}
}

// Review C ruling: manual mode (no candidates), the node dead for 10
// minutes, a background app failing a dial every 5s. The fast cadence ends
// 60s into the run, the echo-probe-fail Warn is logged once, and the retry
// schedule (5s, 10s, then 30s) — not echo signals — paces netcheck + Phase-A.
func TestDeadNodeTimeline_ManualModeDeadNodeBounded(t *testing.T) {
	const death = 60 * time.Second
	r := runDeadNodeTimeline(t, timelineOpts{
		trust0: int32(len(echoIntervals) - 1), firstProbe: death + 16*time.Second,
		down: []downWindow{{from: death}}, dialEvery: 5 * time.Second,
		horizon: death + 10*time.Minute, noFallback: true,
	})
	t.Logf("10 min dead, manual: probes=%d warns=%d phaseA=%d netchecks=%d state=%s", r.probes, r.warns, r.races, r.netchecks, r.state)
	if r.state != StateConnected {
		t.Fatalf("engine %s: manual mode on a dead node must stay on the retry schedule", r.state)
	}
	if r.phaseBAt >= 0 {
		t.Fatal("Phase B set up a node with no candidates")
	}
	// Probes: ≤30 in the 60s fast window, then the 16s backoff (≤36 in the
	// remaining 9 min) plus the ramp.
	if r.probes > 70 {
		t.Errorf("probes = %d in 10 min, want ≤ 70", r.probes)
	}
	if r.warns != 1 {
		t.Errorf("DIAG: echo-probe-fail Warn lines = %d, want 1 per failure run", r.warns)
	}
	// Phase-A: 2 echo re-races, then the retry schedule: 5s, 10s, then one
	// per 30s+5s race ≈ 17 more in the remaining ~9.3 min.
	if r.races > 22 {
		t.Errorf("Phase-A attempts = %d in 10 min, want ≤ 22 (retry backoff, not echo signals)", r.races)
	}
	if r.netchecks > r.races {
		t.Errorf("netchecks = %d > Phase-A attempts %d", r.netchecks, r.races)
	}
}
