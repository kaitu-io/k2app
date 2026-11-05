package engine

// F2 flow simulator: a deterministic, randomized discrete-event simulation of
// node failover. It drives the REAL engine code — outboundReplaceLoop, Phase
// A / escalate / holdUntilOnline / retrySet / retryOnce / retryPhaseB, the
// verdicts (replace_verdict.go), compositeProvider + staticProvider, the
// exhaustion cap clock, buildStatusLocked, netCoordinator → doNetworkReconnect,
// and the health monitor's real detector (probeOnce → echoStepDial /
// readDialFailEvidence over a real wire.TransportManager's user-dial
// evidence) — through thousands of random 60-minute worlds on one fake clock,
// and checks invariants I1–I6 at every step.
//
// Faked, at the edges only:
//   - raceTransportFn (Phase-A re-race) and setupOutboundTransportFn
//     (Phase-B candidate setup): answered from the world model, with a fake
//     duration (dead 5s, hang 9.5s, …);
//   - e.netcheck (active netcheck results) and directTrack.LastOK (passive);
//   - network up/down/changed: real netCoordinator.handleEvent;
//   - holdTimerFn / holdNow (fake clock), TransportManager.SetConnFailClock
//     and healthMonitor.clock (same fake clock);
//   - the user's traffic (TransportManager.DialTCP through a fake node
//     dialer) and the echo probe's schedule (the driver calls probeOnce at
//     echoIntervals[trust], as echoProbeLoop would).
//
// Determinism: the replace loop runs on its own goroutine, but every place it
// can block is a handshake with the driver (replaceLoopIdle, the hold's
// timer, each fake call). The driver only acts while the loop is parked in a
// handshake and releases it only when exactly one of the channels its next
// select listens to is ready, so a seed always replays the same run.
//
// Knobs: K2_SIM_SEEDS (count, default simDefaultSeeds), K2_SIM_SEED (run one
// seed, verbose), K2_SIM_SEED_BASE, K2_SIM_PAUSE=0 (no pause/wake events —
// on by default since the fix wave made the replace loop honour Pause),
// K2_SIM_NOMIN=1 (skip minimization), K2_SIM_MIN_RUNS (minimization budget,
// default 60 reruns; only the first failing seed is minimized),
// K2_SIM_NO_ACCEPTALL=1 (no accept-all captive networks), K2_SIM_ALLOW=FA,FB,M3
// (downgrade the known finding classes in simClasses to a log line).

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"log/slog"
	"math/rand"
	"net"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kaitu-io/k2/config"
	"github.com/kaitu-io/k2/wire"
)

const (
	simHorizon      = 60 * time.Minute
	simDefaultSeeds = 30
	// simMinDwell: every world state (node, network, Center, pause) lasts at
	// least this long. It exceeds the longest retry interval (30s) plus the
	// longest tick (simMaxChain ≈ 44s), so every state is seen by at least
	// one tick: an oracle window the engine could not have sampled is never
	// held against it.
	simMinDwell = 90 * time.Second

	// Spec values the oracles hold the engine to, written out rather than
	// read from the product constants (exhaustFailAfter, longPendingAfter,
	// longPendingInterval): a mutation of the constant must not move the
	// oracle with it (spec §4.2 amendment 2026-11-05; final-review-2 M4).
	simSpecCap         = 10 * time.Minute
	simSpecLongPending = 10 * time.Minute
	simSpecLongTick    = 60 * time.Second
	// simSpecCodeLessPhaseB: a tick whose Phase A answered code-less runs
	// Phase B at most this often per target (8a57c0740 FB ruling), so a
	// working candidate behind a code-less current node is reached within
	// this plus one tick spacing plus one round.
	simSpecCodeLessPhaseB = 2 * time.Minute
)

// Fake durations of the edge calls (fake clock).
const (
	simDurOK       = 800 * time.Millisecond  // handshake + metadata answered
	simDurRefused  = 200 * time.Millisecond  // RST
	simDurTimeout  = 5 * time.Second         // wire raceTimeout (transport step)
	simDurHang     = 9500 * time.Millisecond // metaPhaseBudget: handshake done, verdict never came
	simDurIface    = 50 * time.Millisecond   // ENETUNREACH
	simDurNCFast   = 300 * time.Millisecond  // a netcheck target answered
	simDurNCNoResp = netcheckTimeout         // nothing answered
)

// ---------------------------------------------------------------------------
// World model

type simNet int

const (
	snOnline simNet = iota
	snIfaceDown
	snNoInternet
	snCaptive
	snAcceptAll // passive direct dials OK, active netcheck sees a portal
)

func (n simNet) String() string {
	return [...]string{"online", "iface-down", "no-internet", "captive", "accept-all-captive"}[n]
}

// code is the hold code the engine should show for this network.
func (n simNet) code() int {
	switch n {
	case snIfaceDown:
		return ErrCodeNetworkUnavailable
	case snNoInternet:
		return ErrCodeNoInternet
	case snCaptive, snAcceptAll:
		return ErrCodeCaptivePortal
	}
	return 0
}

func (n simNet) ifaceUp() bool { return n != snIfaceDown }

type simCenter int

const (
	scOK simCenter = iota
	scRefusing
	scHanging
)

func (c simCenter) String() string { return [...]string{"ok", "refusing", "hanging"}[c] }

type simKind int

const (
	skAlive simKind = iota
	skDead
	skCoded
	skCodeLess
)

type simNodeState struct {
	kind    simKind
	code    int  // skCoded: 401/402/403
	refused bool // skDead: RST (fast) vs blackhole (5s)
}

func (n simNodeState) String() string {
	switch n.kind {
	case skAlive:
		return "alive"
	case skDead:
		if n.refused {
			return "dead(refused)"
		}
		return "dead(timeout)"
	case skCoded:
		return "coded-" + strconv.Itoa(n.code)
	default:
		return "code-less"
	}
}

type simEventKind int

const (
	evNode simEventKind = iota
	evNet
	evCenter
	evShellDrop    // the shell provider stops owning the URL the target is on
	evShellRestore // every dropped URL is owned again
	evPause
	evWake
)

type simEvent struct {
	at      time.Duration
	kind    simEventKind
	node    int
	st      simNodeState
	net     simNet
	changed bool // iface stays up: platform also reports SignalChanged
	center  simCenter
}

func (ev simEvent) String() string {
	at := fmtDur(ev.at)
	switch ev.kind {
	case evNode:
		return fmt.Sprintf("%s node n%d → %s", at, ev.node, ev.st)
	case evNet:
		ch := ""
		if ev.changed {
			ch = " (+SignalChanged)"
		}
		return fmt.Sprintf("%s network → %s%s", at, ev.net, ch)
	case evCenter:
		return fmt.Sprintf("%s center → %s", at, ev.center)
	case evShellDrop:
		return fmt.Sprintf("%s shell refresh drops the current URL", at)
	case evShellRestore:
		return fmt.Sprintf("%s shell refresh restores all URLs", at)
	case evPause:
		return fmt.Sprintf("%s Pause()", at)
	default:
		return fmt.Sprintf("%s Wake()", at)
	}
}

type simScenario struct {
	seed       int64
	mode       string // manual | static | shell
	nodes      int    // node 0 = the session's first node
	noTargets  bool   // ClientConfig.Netcheck empty → netUnknown (k2r / CLI shape)
	traffic    bool   // the user has traffic (user dials)
	dialEvery  time.Duration
	direct     bool // direct (non-tunnel) traffic → passive netcheck evidence
	firstProbe time.Duration
	horizon    time.Duration
	events     []simEvent
}

func (sc *simScenario) header() string {
	return fmt.Sprintf("seed=%d mode=%s nodes=%d noTargets=%v traffic=%v dialEvery=%v direct=%v firstProbe=%v horizon=%v events=%d",
		sc.seed, sc.mode, sc.nodes, sc.noTargets, sc.traffic, sc.dialEvery, sc.direct, sc.firstProbe, sc.horizon, len(sc.events))
}

func fmtDur(d time.Duration) string {
	d = d.Round(100 * time.Millisecond)
	m := int(d / time.Minute)
	s := float64(d%time.Minute) / float64(time.Second)
	return fmt.Sprintf("%02d:%04.1f", m, s)
}

func simDwell(r *rand.Rand, mean time.Duration) time.Duration {
	return simMinDwell + time.Duration(r.ExpFloat64()*float64(mean))
}

// simGen builds a random world for seed. Every entity starts healthy (the
// session connected) and changes state at random times.
func simGen(seed int64, withPause bool) *simScenario {
	r := rand.New(rand.NewSource(seed))
	noAcceptAll := os.Getenv("K2_SIM_NO_ACCEPTALL") == "1"
	sc := &simScenario{seed: seed, horizon: simHorizon}
	cand := 0
	switch x := r.Float64(); {
	case x < 0.15:
		sc.mode = "manual"
	case x < 0.70:
		sc.mode = "static"
		cand = 1 + r.Intn(3)
	default:
		sc.mode = "shell"
		cand = r.Intn(4)
	}
	sc.nodes = 1 + cand
	sc.noTargets = r.Float64() < 0.10
	sc.traffic = r.Float64() < 0.85
	sc.dialEvery = time.Duration(1+r.Intn(3)) * time.Second
	sc.direct = r.Float64() < 0.5
	sc.firstProbe = time.Duration(r.Intn(16000)) * time.Millisecond

	// Nodes.
	pDead := []float64{0.15, 0.45, 0.8}[r.Intn(3)]
	pCoded := []float64{0, 0.03, 0.08}[r.Intn(3)]
	pCodeLess := []float64{0, 0.04}[r.Intn(2)]
	nodeMean := []time.Duration{2 * time.Minute, 6 * time.Minute, 15 * time.Minute}[r.Intn(3)]
	for i := 0; i < sc.nodes; i++ {
		cur := simNodeState{kind: skAlive}
		t := time.Duration(0)
		for {
			t += simDwell(r, nodeMean)
			if t >= sc.horizon {
				break
			}
			var nx simNodeState
			for {
				u := r.Float64()
				switch {
				case u < pDead:
					nx = simNodeState{kind: skDead, refused: r.Intn(2) == 0}
				case u < pDead+pCoded:
					nx = simNodeState{kind: skCoded, code: []int{401, 402, 403}[r.Intn(3)]}
				case u < pDead+pCoded+pCodeLess:
					nx = simNodeState{kind: skCodeLess}
				default:
					nx = simNodeState{kind: skAlive}
				}
				if nx != cur {
					break
				}
			}
			cur = nx
			sc.events = append(sc.events, simEvent{at: t, kind: evNode, node: i, st: nx})
		}
	}

	// Network.
	if chaos := r.Intn(3); chaos > 0 {
		onMean := []time.Duration{0, 15 * time.Minute, 4 * time.Minute}[chaos]
		offMean := []time.Duration{0, 2 * time.Minute, 4 * time.Minute}[chaos]
		cur := snOnline
		t := time.Duration(0)
		for {
			if cur == snOnline {
				t += simDwell(r, onMean)
			} else {
				t += simDwell(r, offMean)
			}
			if t >= sc.horizon {
				break
			}
			nx := snOnline
			if cur == snOnline || r.Intn(3) == 0 {
				for nx = cur; nx == cur || (noAcceptAll && nx == snAcceptAll); {
					nx = simNet(r.Intn(5))
				}
			}
			ev := simEvent{at: t, kind: evNet, net: nx}
			ev.changed = cur.ifaceUp() && nx.ifaceUp() && r.Intn(2) == 0
			sc.events = append(sc.events, ev)
			cur = nx
		}
	}

	// Center.
	if c := r.Intn(3); c > 0 {
		okMean := []time.Duration{0, 15 * time.Minute, 6 * time.Minute}[c]
		outMean := []time.Duration{0, 3 * time.Minute, 12 * time.Minute}[c]
		t := time.Duration(0)
		for {
			t += simDwell(r, okMean)
			if t >= sc.horizon {
				break
			}
			sc.events = append(sc.events, simEvent{at: t, kind: evCenter, center: simCenter(1 + r.Intn(2))})
			t += simDwell(r, outMean)
			if t >= sc.horizon {
				break
			}
			sc.events = append(sc.events, simEvent{at: t, kind: evCenter, center: scOK})
		}
	}

	// Shell refreshes that drop the URL the target is on.
	if sc.mode == "shell" && r.Float64() < 0.4 {
		t := time.Duration(0)
		for {
			t += simDwell(r, 10*time.Minute)
			if t >= sc.horizon {
				break
			}
			sc.events = append(sc.events, simEvent{at: t, kind: evShellDrop})
			t += simDwell(r, 6*time.Minute)
			if t >= sc.horizon {
				break
			}
			sc.events = append(sc.events, simEvent{at: t, kind: evShellRestore})
		}
	}

	// Pause / Wake (iOS screen lock, Android memory pressure).
	if withPause && r.Float64() < 0.5 {
		t := time.Duration(0)
		for {
			t += simDwell(r, 10*time.Minute)
			if t >= sc.horizon {
				break
			}
			sc.events = append(sc.events, simEvent{at: t, kind: evPause})
			t += simDwell(r, 5*time.Minute)
			if t >= sc.horizon {
				break
			}
			sc.events = append(sc.events, simEvent{at: t, kind: evWake})
		}
	}
	sort.SliceStable(sc.events, func(i, j int) bool { return sc.events[i].at < sc.events[j].at })
	return sc
}

// ---------------------------------------------------------------------------
// Fake edges

// simShell is a k2subs subscription.Manager shape: owns a URL set, its
// exclusions never expire (only Reset clears them), and a refresh can stop
// owning a URL.
type simShell struct {
	mu       sync.Mutex
	urls     []string
	dropped  map[string]bool
	excluded map[string]bool
}

func (p *simShell) OwnsURL(u string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.dropped[u] {
		return false
	}
	for _, x := range p.urls {
		if x == u {
			return true
		}
	}
	return false
}

func (p *simShell) NextURL(cur string, _ error) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.excluded[cur] = true
	for _, u := range p.urls {
		if u != cur && !p.excluded[u] && !p.dropped[u] {
			return u, nil
		}
	}
	return "", errors.New("subscription: all tunnels excluded")
}

func (p *simShell) Reset() {
	p.mu.Lock()
	p.excluded = map[string]bool{}
	p.mu.Unlock()
}

// simNodeDialer is the target TransportManager's primary dialer for one node.
// It holds a cached connection (conn) valid for the node's epoch: a node
// state change or a network change kills it, Reset/Close drop it.
type simNodeDialer struct {
	s     *simRun
	node  int
	mu    sync.Mutex
	conn  bool
	epoch uint64
}

func (d *simNodeDialer) connected() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.conn && d.epoch == d.s.nodes[d.node].epoch
}

func (d *simNodeDialer) dial() error {
	if d.connected() {
		return nil
	}
	res := d.s.attempt(d.node)
	if res.err != nil {
		return res.err
	}
	d.mu.Lock()
	d.conn, d.epoch = true, d.s.nodes[d.node].epoch
	d.mu.Unlock()
	return nil
}

func (d *simNodeDialer) DialTCP(context.Context, string, []byte) (net.Conn, error) {
	if err := d.dial(); err != nil {
		return nil, err
	}
	c1, c2 := net.Pipe()
	c2.Close()
	return c1, nil
}
func (d *simNodeDialer) DialUDP(context.Context, string) (net.PacketConn, error) {
	return nil, d.dial()
}
func (d *simNodeDialer) DialStreamUDP(context.Context, string) (net.PacketConn, error) {
	return nil, d.dial()
}
func (d *simNodeDialer) Reset() {
	d.mu.Lock()
	d.conn = false
	d.mu.Unlock()
}
func (d *simNodeDialer) Close() error { d.Reset(); return nil }
func (d *simNodeDialer) RunEchoProbe(context.Context) error {
	if d.connected() {
		return nil
	}
	return errNoConn
}

// ---------------------------------------------------------------------------
// Driver

type simBlockKind int

const (
	bkIdle     simBlockKind = iota // outboundReplaceLoop about to select
	bkHold                         // holdUntilOnline about to select
	bkRace                         // raceTransportFn (Phase A)
	bkSetup                        // setupOutboundTransportFn (Phase B)
	bkNetcheck                     // e.netcheck (active probe)
)

type simResp struct {
	dead   bool
	err    error
	dialer *simNodeDialer
	ot     *outboundTransport
	nc     []checkResult
}

type simReq struct {
	kind   simBlockKind
	host   string
	url    string
	timer  *simTimer // bkHold
	resume chan simResp
}

type simTimer struct {
	d       time.Duration
	at      time.Time
	ch      chan time.Time
	hold    bool
	stopped bool
	fired   bool
}

type simCall struct {
	req  *simReq
	at   time.Time
	resp simResp
}

type simAttempt struct {
	err  error
	dur  time.Duration
	what string // ok | dead | coded-40x | code-less | hang | net
	code int
}

type simViolation struct {
	inv   string
	at    time.Duration
	msg   string
	class string // known finding class (simClasses), "" = unclassified
}

func (v simViolation) String() string {
	c := ""
	if v.class != "" {
		c = " {" + v.class + "}"
	}
	return fmt.Sprintf("[%s @%s]%s %s", v.inv, fmtDur(v.at), c, v.msg)
}

// simClasses are the violation shapes found on the base code (see
// sim-report.md). K2_SIM_ALLOW=FA,FB,M3 downgrades them to a log line; by
// default every violation fails the test.
var simClasses = map[string]string{
	"M3": "retry/hold tick trusts passive direct evidence: Phase-B rounds burnt while offline, accept-all captive shown as 503 (fix wave item M3)",
	"FA": "a round mixing code-less answers (Center outage / node-local Center failure) with transport-dead candidates counts toward the 10-min cap",
	"PI": "Pause does not stop a tick already in flight: its remaining Phase A/B attempts and netchecks run while paused (16e6bebda)",
	"PE": "a queued echo re-race signal is taken and raced while paused: the replace loop's reRaceChan case is not pause-gated (16e6bebda)",
	"RW": "recoveredWake ticks a scheduled code-less target on every echo success (Center outage, node alive): race + active netcheck every echo interval, defeating the 60s long-pending cadence (16e6bebda)",
	"FB": "a current node answering code-less pins the target to Phase A only: live candidates are never tried, 503 shown indefinitely",
}

type simNodeRT struct {
	st    simNodeState
	epoch uint64
}

type simResult struct {
	violations []simViolation
	log        []string
	failCode   int
	failAt     time.Duration
	failovers  int
	caps       int
	lpPairs    int // long-pending tick pairs the I4 cadence check judged
	holdRecOK  int // recovered wakes a hold took (8a57c0740 D2)
	holdRecDrp int // recovered wakes a hold dropped (gate closed)
	steps      int
	i2Lat      []time.Duration // P2 start → failover
	hash       uint64
	wall       time.Duration
}

type simRun struct {
	sc    *simScenario
	limit time.Duration // horizon actually simulated

	e      *Engine
	target *outboundTransport
	hm     *healthMonitor
	est    *echoLoopState
	static *staticProvider
	shell  *simShell
	comp   *compositeProvider
	urls   []string
	hosts  map[string]int

	clkMu sync.Mutex
	nowT  time.Time
	t0    time.Time

	blk      chan *simReq
	dead     chan struct{}
	loopDone chan struct{}
	ctx      context.Context

	tmu    sync.Mutex
	timers []*simTimer

	// world
	nodes     []*simNodeRT
	net       simNet
	center    simCenter
	paused    bool
	lastDirOK time.Time
	evIdx     int
	nextDial  time.Time
	nextProbe time.Time
	call      *simCall
	stolen    *outboundTransport
	stolenRec *outboundTransport // a recoveredWake token held back (see readyNow)
	// atIdle: the loop is parked at an idle point (outer select / hold).
	// pauseMidTick: Pause arrived while a tick was in flight and the loop has
	// not reached an idle point since (class PI).
	atIdle       bool
	pauseMidTick bool

	// oracle
	statusMu           sync.Mutex
	pushes             []Status
	fatals             int
	diagMu             sync.Mutex
	diags              []simDiag
	diagSeen           int
	chainPhaseA        []simAttempt // since the last idle point
	chainSetups        []simAttempt
	lastVerdict        netVerdict
	haveVerdict        bool
	okSinceVerd        bool
	cOKSince           time.Time     // I1 503 window (zero = closed)
	cOKPauseBase       time.Duration // pausedNow() when the window opened
	cOKSaved           time.Time     // the window a short break interrupted
	cOKSavedBase       time.Duration
	cOKSavedCount      time.Duration // its counted length at the break
	cOKBreakAt         time.Time
	cOKLateFlagged     bool
	simPausedTotal     time.Duration // finished pauses
	simPauseStart      time.Time
	p2Since            time.Time // I2
	p2Flagged          bool
	recSince           time.Time // I3c
	recFlagged         bool
	downSince          time.Time // I3 network-down
	downFlagged        bool
	downNet            simNet
	s503Since          time.Time
	lastCodeLessAt     time.Time
	lastCodeLessPaused time.Duration // pausedNow() at lastCodeLessAt
	// lastRaceCodeLessAt: the latest Phase-A race answered code-less (zero
	// once a race answers otherwise) — FB's signature.
	lastRaceCodeLessAt time.Time
	attempts           []time.Time // races + setups
	netchecks          []time.Time
	waitTicks          []time.Time // netchecks with trigger=wait-tick: the schedule's own cadence
	lpFlagged          bool        // long-pending cadence flagged for this reconnecting stretch
	lpLastJudged       time.Time
	recTicks           []time.Time // netchecks with trigger=recovered (recoveredWake ticks)
	lastReRaceReason   string
	lastWakeAt         time.Time
	// Mirror of holdUntilOnline's recovered-wake gate (holdTakesRecovered).
	wasHeld        bool
	hLastTry       time.Time
	hMuted         bool
	hCodeLessA     bool
	hRecReleased   bool      // the last release of a hold was for a recovered wake
	netOnlineSince time.Time // the network has been online since
	offlineEpoch   int       // incremented on each transition into a non-online network
	roundEpoch     int       // offlineEpoch of the last Phase-B round seen offline
	roundsOff      int       // Phase-B rounds in the current offline episode
	inRound        bool      // a setup was seen since the last idle point
	constSince     time.Time
	reconnSince    time.Time
	maxBackoff     time.Duration
	sameInstant    int
	lastStepAt     time.Time
	lastURL        string
	logBuf         []string
	violations     []simViolation
	res            simResult
	stalled        bool
	pauseEnabled   bool
	h              *simHandler
	resume         chan simResp
	stallTimer     *time.Timer
}

func (s *simRun) now() time.Time {
	s.clkMu.Lock()
	defer s.clkMu.Unlock()
	return s.nowT
}

func (s *simRun) at() time.Duration { return s.now().Sub(s.t0) }

func (s *simRun) logf(format string, args ...any) {
	s.logBuf = append(s.logBuf, fmtDur(s.at())+" "+fmt.Sprintf(format, args...))
}

func (s *simRun) violate(inv, format string, args ...any) {
	v := simViolation{inv: inv, at: s.at(), msg: fmt.Sprintf(format, args...)}
	v.class = s.classify(v)
	s.violations = append(s.violations, v)
	s.logf("VIOLATION %s", v)
}

// block is the loop goroutine's side of a handshake.
func (s *simRun) block(r *simReq) simResp {
	r.resume = s.resume // one handshake at a time: the loop is a single goroutine
	select {
	case s.blk <- r:
	case <-s.dead:
		return simResp{dead: true}
	}
	select {
	case resp := <-r.resume:
		return resp
	case <-s.dead:
		return simResp{dead: true}
	}
}

func callerHas(name string) bool {
	pcs := make([]uintptr, 8)
	n := runtime.Callers(3, pcs)
	frames := runtime.CallersFrames(pcs[:n])
	for i := 0; i < 3; i++ {
		f, more := frames.Next()
		if strings.Contains(f.Function, name) {
			return true
		}
		if !more {
			break
		}
	}
	return false
}

func (s *simRun) newTimer(d time.Duration) (<-chan time.Time, func()) {
	tm := &simTimer{d: d, at: s.now().Add(d), ch: make(chan time.Time, 1), hold: callerHas("holdUntilOnline")}
	s.tmu.Lock()
	s.timers = append(s.timers, tm)
	s.tmu.Unlock()
	if tm.hold {
		s.block(&simReq{kind: bkHold, timer: tm})
	}
	return tm.ch, func() {
		s.tmu.Lock()
		tm.stopped = true
		s.tmu.Unlock()
	}
}

// attempt is the outcome of one connection attempt to node i right now
// (race, setup, or a user dial needing a handshake).
func (s *simRun) attempt(i int) simAttempt {
	host := s.hostOf(i)
	switch s.net {
	case snIfaceDown:
		return simAttempt{err: fmt.Errorf("dial %s: connect: network is unreachable", host), dur: simDurIface, what: "net"}
	case snNoInternet:
		return simAttempt{err: fmt.Errorf("dial %s: i/o timeout", host), dur: simDurTimeout, what: "net"}
	case snCaptive, snAcceptAll:
		return simAttempt{err: fmt.Errorf("dial %s: tls: handshake failure", host), dur: simDurOK, what: "net"}
	}
	st := s.nodes[i].st
	if st.kind == skDead {
		if st.refused {
			return simAttempt{err: fmt.Errorf("dial %s: connection refused", host), dur: simDurRefused, what: "dead"}
		}
		return simAttempt{err: fmt.Errorf("dial %s: i/o timeout", host), dur: simDurTimeout, what: "dead"}
	}
	codeLess := &wire.ProxyLayerError{Err: fmt.Errorf("%w: unauthorized", wire.ErrMetadataRejected)}
	if st.kind == skCodeLess {
		return simAttempt{err: codeLess, dur: simDurOK, what: "code-less"}
	}
	switch s.center {
	case scRefusing:
		return simAttempt{err: codeLess, dur: simDurOK, what: "code-less"}
	case scHanging:
		return simAttempt{err: fmt.Errorf("metadata: %w", wire.ErrMetadataTimeout), dur: simDurHang, what: "hang"}
	}
	if st.kind == skCoded {
		return simAttempt{err: &wire.ProxyLayerError{Err: fmt.Errorf("%w: %w", wire.ErrMetadataRejected,
			&wire.StreamError{Code: st.code, Message: "center verdict"})}, dur: simDurOK, what: "coded-" + strconv.Itoa(st.code), code: st.code}
	}
	return simAttempt{dur: simDurOK, what: "ok"}
}

func (s *simRun) hostOf(i int) string { return fmt.Sprintf("n%d.example", i) }

// classify maps a violation to a known finding class from world facts.
func (s *simRun) classify(v simViolation) string {
	switch {
	case v.inv == "I4" && strings.Contains(v.msg, "while paused") && s.pauseMidTick:
		return "PI"
	case v.inv == "I4" && strings.HasPrefix(v.msg, "race ") && strings.Contains(v.msg, "while paused") &&
		s.lastReRaceReason == "echo-consecutive-fail":
		return "PE"
	case v.inv == "I4" && strings.Contains(v.msg, "steady state") &&
		countSince(s.recTicks, s.now().Add(-10*time.Minute)) > int(10*time.Minute/simSpecLongTick)+1:
		return "RW"
	case v.inv == "I4" && strings.Contains(v.msg, "Phase-B round while the network"):
		return "M3"
	case v.inv == "I3" && strings.Contains(v.msg, "network accept-all-captive"):
		return "M3"
	case v.inv == "I4" && strings.Contains(v.msg, "steady state") && s.net == snAcceptAll:
		return "M3"
	case v.inv == "I1" && (strings.Contains(v.msg, "failed 503 before") || strings.Contains(v.msg, "without that verdict")) &&
		!s.lastCodeLessAt.IsZero() && s.now().Sub(s.lastCodeLessAt)-(s.pausedNow()-s.lastCodeLessPaused) < simSpecCap+s.tickBound()+s.maxChain():
		return "FA"
	case v.inv == "I3" && strings.Contains(v.msg, "stale 503") &&
		(s.nodes[s.curNode()].st.kind == skCodeLess || !s.lastRaceCodeLessAt.IsZero()):
		return "FB"
	}
	return ""
}

func (s *simRun) curURL() string {
	s.e.mu.Lock()
	defer s.e.mu.Unlock()
	return s.target.url
}

func (s *simRun) curNode() int { return s.hosts[hostOf(s.curURL())] }

// candidates is the set of nodes the provider could offer from url.
func (s *simRun) candidates(url string) []int {
	cur := s.hosts[hostOf(url)]
	var out []int
	switch s.sc.mode {
	case "static":
		for i := 0; i < s.sc.nodes; i++ {
			if i != cur {
				out = append(out, i)
			}
		}
	case "shell":
		s.shell.mu.Lock()
		dropped := s.shell.dropped
		if !dropped[url] {
			for i := 0; i < s.sc.nodes; i++ {
				if i != cur && !dropped[s.urls[i]] {
					out = append(out, i)
				}
			}
		}
		s.shell.mu.Unlock()
	}
	return out
}

func (s *simRun) newDialer(i int, connected bool) *simNodeDialer {
	d := &simNodeDialer{s: s, node: i}
	if connected {
		d.conn, d.epoch = true, s.nodes[i].epoch
	}
	return d
}

// startCall answers a fake call from the world as it is now; the reply is
// delivered after the call's fake duration.
func (s *simRun) startCall(r *simReq) {
	c := &simCall{req: r}
	switch r.kind {
	case bkRace, bkSetup:
		i, ok := s.hosts[r.host]
		if !ok {
			c.resp.err = fmt.Errorf("sim: unknown host %q", r.host)
			c.at = s.now()
			break
		}
		a := s.attempt(i)
		c.at = s.now().Add(a.dur)
		kind := "race"
		if r.kind == bkSetup {
			kind = "setup"
			s.chainSetups = append(s.chainSetups, a)
			s.inRound = true
			s.checkOfflineRound()
		} else {
			s.chainPhaseA = append(s.chainPhaseA, a)
			s.lastRaceCodeLessAt = time.Time{}
			if a.what == "code-less" || a.what == "hang" {
				s.lastRaceCodeLessAt = s.now()
			}
			if s.target.held.Load() {
				s.hCodeLessA = a.what == "code-less" || a.what == "hang"
			}
		}
		s.attempts = append(s.attempts, s.now())
		if a.what == "code-less" || a.what == "hang" {
			s.lastCodeLessAt, s.lastCodeLessPaused = s.now(), s.pausedNow()
		}
		if s.paused && s.pauseEnabled {
			s.violate("I4", "%s %s while paused", kind, r.host)
		}
		s.logf("%s %s → %s", kind, r.host, a.what)
		if a.err != nil {
			c.resp.err = a.err
			break
		}
		s.okSinceVerd = true
		d := s.newDialer(i, true)
		if r.kind == bkRace {
			c.resp.dialer = d
		} else {
			tm := wire.NewTransportManager()
			tm.SetConnFailClock(s.now)
			tm.SetPrimary(d, "tcpws")
			c.resp.ot = &outboundTransport{
				tm:        tm,
				wireCfg:   wire.Config{Host: r.host, Port: 443, DialAddr: r.host + ":443"},
				dialAddr:  r.host + ":443",
				name:      "k2v5-" + r.host,
				url:       r.url,
				swappable: wire.NewSwappableDialer(tm),
			}
		}
	case bkNetcheck:
		var rs []checkResult
		dur := simDurNCFast
		if !s.sc.noTargets {
			switch s.net {
			case snOnline:
				rs = []checkResult{checkMatched}
			case snCaptive, snAcceptAll:
				rs = []checkResult{checkOtherResponse}
			default:
				rs, dur = []checkResult{checkNoResponse}, simDurNCNoResp
			}
		}
		c.resp.nc = rs
		c.at = s.now().Add(dur)
		s.netchecks = append(s.netchecks, s.now())
		if s.paused && s.pauseEnabled {
			s.violate("I4", "netcheck while paused")
		}
	}
	s.call = c
}

// checkOfflineRound: I4 — candidates are never burnt while the user's
// network is down (with netcheck targets), except the one round that
// discovers it.
func (s *simRun) checkOfflineRound() {
	if s.net == snOnline || s.sc.noTargets || s.inRoundCounted() {
		return
	}
	if s.roundEpoch != s.offlineEpoch {
		s.roundEpoch, s.roundsOff = s.offlineEpoch, 0
	}
	s.roundsOff++
	if s.roundsOff == 2 {
		s.violate("I4", "a second Phase-B round while the network is %s (candidates burnt offline)", s.net)
	}
}

// inRoundCounted: only the first setup of a round counts toward roundsOff.
func (s *simRun) inRoundCounted() bool { return len(s.chainSetups) > 1 }

// applyEvent mutates the world.
func (s *simRun) applyEvent(ev simEvent) {
	s.logf("WORLD %s", strings.SplitN(ev.String(), " ", 2)[1])
	s.constSince = s.now()
	switch ev.kind {
	case evNode:
		n := s.nodes[ev.node]
		n.epoch++ // the node restarted / changed: cached connections die
		n.st = ev.st
	case evNet:
		prev := s.net
		s.net = ev.net
		if prev == snOnline && ev.net != snOnline {
			s.offlineEpoch++
		}
		if (prev == snOnline || prev == snAcceptAll) && !(ev.net == snOnline || ev.net == snAcceptAll) {
			s.lastDirOK = s.now()
		}
		for _, n := range s.nodes {
			n.epoch++ // the path changed: every cached connection is dead
		}
		s.e.mu.Lock()
		nc := s.e.netCoord
		s.e.mu.Unlock()
		if nc == nil {
			return
		}
		nc.mu.Lock()
		nc.lastFire = time.Time{} // keep every reconnect a synchronous leading fire (no AfterFunc)
		nc.mu.Unlock()
		switch {
		case prev.ifaceUp() && !ev.net.ifaceUp():
			nc.handleEvent(&NetEvent{Signal: SignalUnavailable, Source: "sim"})
		case !prev.ifaceUp() && ev.net.ifaceUp():
			nc.handleEvent(&NetEvent{Signal: SignalAvailable, Source: "sim"})
		case ev.changed:
			nc.handleEvent(&NetEvent{Signal: SignalChanged, Source: "sim"})
		}
	case evCenter:
		s.center = ev.center
	case evShellDrop:
		u := s.curURL()
		s.shell.mu.Lock()
		s.shell.dropped[u] = true
		s.shell.mu.Unlock()
		s.logf("WORLD shell drops %s", u)
	case evShellRestore:
		s.shell.mu.Lock()
		s.shell.dropped = map[string]bool{}
		s.shell.mu.Unlock()
	case evPause:
		s.paused = true
		s.simPauseStart = s.now()
		s.pauseMidTick = !s.atIdle
		s.e.Pause()
	case evWake:
		if s.paused {
			s.simPausedTotal += s.now().Sub(s.simPauseStart)
		}
		s.paused = false
		s.lastWakeAt = s.now()
		s.e.Wake()
	}
}

func (s *simRun) userDial() {
	s.e.mu.Lock()
	tm := s.target.tm
	s.e.mu.Unlock()
	if c, err := tm.DialTCP(context.Background(), "example.com:443", nil); err == nil {
		c.Close()
	}
}

func (s *simRun) probe() {
	s.hm.probeOnce(s.target, s.est, time.Now())
}

func (s *simRun) hasActiveRetryTimer() bool {
	s.tmu.Lock()
	defer s.tmu.Unlock()
	for _, t := range s.timers {
		if !t.hold && !t.stopped && !t.fired {
			return true
		}
	}
	return false
}

// listenedTimer is the timer the parked select listens to: the hold's own
// (bkHold) or the retry set's (bkIdle).
func (s *simRun) listenedTimer(r *simReq) *simTimer {
	s.tmu.Lock()
	defer s.tmu.Unlock()
	if r.kind == bkHold {
		if !r.timer.stopped && !r.timer.fired {
			return r.timer
		}
		return nil
	}
	var best *simTimer
	for _, t := range s.timers {
		if !t.hold && !t.stopped && !t.fired && (best == nil || t.at.Before(best.at)) {
			best = t
		}
	}
	return best
}

func (s *simRun) pruneTimers() {
	s.tmu.Lock()
	out := s.timers[:0]
	for _, t := range s.timers {
		if !t.stopped && !t.fired {
			out = append(out, t)
		}
	}
	s.timers = out
	s.tmu.Unlock()
}

// readyNow: does the parked select (r) have a ready channel? At bkIdle it
// listens to reRaceChan, recoveredWake and — only while the retry set is
// non-empty, which is exactly while it has an armed timer — holdWake. Go's
// select picks at random among ready cases, so when several are ready the
// driver keeps exactly one: holdWake first, then recoveredWake, then the
// re-race signal; the others are held back and re-queued at the loop's next
// handshake (the loop touches neither channel before that).
func (s *simRun) readyNow(r *simReq) bool {
	switch r.kind {
	case bkHold:
		wake := len(s.e.holdWake) > 0
		rec := len(s.e.recoveredWake) > 0
		if wake && rec {
			s.stolenRec = <-s.e.recoveredWake
			s.logf("recovered wake held back behind holdWake, re-queued at the next handshake")
			rec = false
		}
		if rec && !s.holdTakesRecovered() {
			// The hold would drop this token and select again without a
			// handshake (8a57c0740 holdUntilOnline): the driver drops it
			// itself so the loop stays parked in the handshake.
			<-s.e.recoveredWake
			s.res.holdRecDrp++
			s.logf("hold drops a recovered wake (code-less=%v muted=%v since-last-try=%v)", s.hCodeLessA, s.hMuted, s.now().Sub(s.hLastTry))
			rec = false
		}
		if wake {
			s.hLastTry, s.hMuted = s.now(), false
		} else if rec {
			s.hLastTry, s.hMuted = s.now(), true
			s.hRecReleased = true
			s.res.holdRecOK++
		}
		return wake || rec
	case bkIdle:
		wake := len(s.e.holdWake) > 0 && s.hasActiveRetryTimer()
		rec := len(s.e.recoveredWake) > 0
		sig := len(s.e.reRaceChan) > 0
		if (wake || rec) && sig {
			s.stolen = <-s.e.reRaceChan
			s.logf("re-race signal held back behind a wake, re-queued at the next handshake")
			sig = false
		}
		if wake && rec {
			s.stolenRec = <-s.e.recoveredWake
			s.logf("recovered wake held back behind holdWake, re-queued at the next handshake")
			rec = false
		}
		return wake || rec || sig
	}
	return false
}

// holdTakesRecovered mirrors holdUntilOnline's recovered-wake gate
// (8a57c0740): the held target's token is taken unless its latest Phase A
// (lastErr at entry, then the hold's own) was code-less, the previous tick
// was a recovered one that failed (muted), or the last attempt is younger
// than holdBackoff[0]. A wrong mirror is not silent: a released token the
// hold drops leaves the loop selecting without a handshake → I5 stall.
func (s *simRun) holdTakesRecovered() bool {
	return !s.hCodeLessA && !s.hMuted && s.now().Sub(s.hLastTry) >= holdBackoff[0]
}

func (s *simRun) waitBlock() *simReq {
	if s.stallTimer == nil {
		s.stallTimer = time.NewTimer(30 * time.Second)
	} else {
		s.stallTimer.Reset(30 * time.Second)
	}
	defer s.stallTimer.Stop()
	select {
	case r := <-s.blk:
		return r
	case <-s.loopDone:
		return nil
	case <-s.stallTimer.C:
		s.stalled = true
		if s.hRecReleased {
			s.violate("I5", "replace loop stalled after the driver released a recovered wake to a hold: the hold dropped it — the driver's mirror of the hold's recovered gate (holdTakesRecovered) disagrees with the product")
			return nil
		}
		s.violate("I5", "replace loop neither blocked at a known point nor exited for 30s wall (deadlock or unknown wait)")
		return nil
	}
}

func (s *simRun) release(r *simReq, resp simResp) *simReq {
	r.resume <- resp
	return s.waitBlock()
}

// passive keeps directTrack.LastOK in step with the fake world: fresh (real
// now, judgeNet compares it to time.Now) while the fake age ≤ passiveFreshness.
func (s *simRun) passive() {
	if !s.sc.direct {
		return
	}
	if s.net == snOnline || s.net == snAcceptAll {
		s.lastDirOK = s.now()
	}
	if !s.lastDirOK.IsZero() && s.now().Sub(s.lastDirOK) <= passiveFreshness {
		s.e.directTrack.lastOK.Store(time.Now().UnixNano())
	} else {
		s.e.directTrack.lastOK.Store(0)
	}
}

// onBlock runs at every handshake: re-queue a held-back signal, sync the
// passive evidence, read the new DIAG lines, and at idle points run the
// status oracles.
func (s *simRun) onBlock(r *simReq) {
	s.res.steps++
	s.atIdle = r.kind == bkIdle || r.kind == bkHold
	s.hRecReleased = false
	// A new network hold (target.held turns on at its first timer arm):
	// lastTry = entry, code-less = the escalating race's result.
	if held := s.target.held.Load(); held && !s.wasHeld {
		s.hLastTry, s.hMuted, s.hCodeLessA = s.now(), false, !s.lastRaceCodeLessAt.IsZero()
		s.wasHeld = true
	} else if !held {
		s.wasHeld = false
	}
	if s.atIdle {
		s.pauseMidTick = false
	}
	if s.stolen != nil {
		select {
		case s.e.reRaceChan <- s.stolen:
		default:
		}
		s.stolen = nil
	}
	if s.stolenRec != nil {
		select {
		case s.e.recoveredWake <- s.stolenRec:
		default:
		}
		s.stolenRec = nil
	}
	s.passive()
	s.readDiags()
	if r.kind == bkHold && r.timer.d > s.maxBackoff {
		s.maxBackoff = r.timer.d
	}
	if t := s.listenedTimer(r); r.kind == bkIdle && t != nil && t.d > s.maxBackoff {
		s.maxBackoff = t.d
	}
	if r.kind == bkIdle || r.kind == bkHold {
		s.observe(true)
		s.chainPhaseA, s.chainSetups, s.inRound = nil, nil, false
	} else {
		s.observe(false)
	}
}

// ---------------------------------------------------------------------------
// Oracles

type simDiag struct {
	at    time.Time
	msg   string
	attrs map[string]any
}

type simHandler struct {
	mu  sync.Mutex
	run atomic.Pointer[simRun]
}

func (h *simHandler) Enabled(_ context.Context, l slog.Level) bool { return l >= slog.LevelInfo }
func (h *simHandler) Handle(_ context.Context, r slog.Record) error {
	s := h.run.Load()
	if s == nil {
		return nil
	}
	if !strings.HasPrefix(r.Message, "DIAG: ") && r.Message != "engine: fail" {
		return nil
	}
	d := simDiag{at: s.now(), msg: r.Message, attrs: map[string]any{}}
	r.Attrs(func(a slog.Attr) bool {
		d.attrs[a.Key] = a.Value.Any()
		return true
	})
	s.diagMu.Lock()
	s.diags = append(s.diags, d)
	s.diagMu.Unlock()
	return nil
}
func (h *simHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *simHandler) WithGroup(string) slog.Handler      { return h }

type simStatusHandler struct{ s *simRun }

func (h simStatusHandler) OnStatus(st Status) {
	h.s.statusMu.Lock()
	h.s.pushes = append(h.s.pushes, st)
	h.s.statusMu.Unlock()
}
func (h simStatusHandler) OnStats(int64, int64) {}
func (h simStatusHandler) OnOutboundFatal(string, error) {
	h.s.statusMu.Lock()
	h.s.fatals++
	h.s.statusMu.Unlock()
}

func verdictFromString(v string) netVerdict {
	switch v {
	case "iface_down":
		return netIfaceDown
	case "no_internet":
		return netNoInternet
	case "captive":
		return netCaptive
	case "online":
		return netOnline
	}
	return netUnknown
}

func (s *simRun) readDiags() {
	s.diagMu.Lock()
	ds := s.diags[s.diagSeen:]
	s.diagSeen = len(s.diags)
	s.diagMu.Unlock()
	for _, d := range ds {
		switch d.msg {
		case "DIAG: netcheck":
			v := verdictFromString(fmt.Sprint(d.attrs["verdict"]))
			s.lastVerdict, s.haveVerdict, s.okSinceVerd = v, true, false
			s.logf("engine netcheck trigger=%v verdict=%v evidence=%v", d.attrs["trigger"], d.attrs["verdict"], d.attrs["evidence"])
			switch d.attrs["trigger"] {
			case "wait-tick":
				s.waitTicks = append(s.waitTicks, s.now())
			case "recovered":
				s.recTicks = append(s.recTicks, s.now())
			}
		case "DIAG: outbound-failover":
			s.res.failovers++
			s.cOKSaved = time.Time{} // the engine saw the break: no bridge
			s.okSinceVerd = true
			s.logf("engine FAILOVER %v → %v", d.attrs["from"], d.attrs["to"])
		case "DIAG: outbound-exhausted", "DIAG: outbound-exhausted-recovered", "DIAG: outbound-exhausted-fatal":
			if d.msg == "DIAG: outbound-exhausted-fatal" {
				s.res.caps++
			}
			if d.msg == "DIAG: outbound-exhausted-recovered" {
				s.cOKSaved = time.Time{} // the engine saw the break: no bridge
			}
			s.logf("engine %s %v", strings.TrimPrefix(d.msg, "DIAG: "), d.attrs)
		case "DIAG: transport-rerace":
			s.logf("engine re-race reason=%v", d.attrs["reason"])
			s.lastReRaceReason = fmt.Sprint(d.attrs["reason"])
		case "engine: fail":
			s.logf("engine FAIL code=%v %v", d.attrs["code"], d.attrs["message"])
		}
	}
}

// capWindowOK (I1 503): the network works, nothing the engine can reach
// answers code-less (a live node during a Center outage, or a code-less
// node), and every node of the set is transport-dead or a coded reject.
func (s *simRun) capWindowOK() bool {
	netOK := s.net == snOnline || (s.sc.noTargets && s.net != snIfaceDown)
	if !netOK {
		return false
	}
	if s.net != snOnline {
		return true // no targets: the engine cannot tell; every node is unreachable
	}
	u := s.curURL()
	set := append([]int{s.hosts[hostOf(u)]}, s.candidates(u)...)
	for _, i := range set {
		st := s.nodes[i].st
		switch {
		case st.kind == skDead:
		case st.kind == skCoded && s.center == scOK:
		default:
			return false
		}
	}
	return true
}

// p2 (I2): online, Center ok, the current node transport-dead and a live
// candidate the provider can offer, with user traffic to detect it.
func (s *simRun) p2() bool {
	if !s.sc.traffic || s.paused || s.net != snOnline || s.center != scOK {
		return false
	}
	u := s.curURL()
	if s.nodes[s.hosts[hostOf(u)]].st.kind != skDead {
		return false
	}
	for _, i := range s.candidates(u) {
		if s.nodes[i].st.kind == skAlive {
			return true
		}
	}
	return false
}

// recovered (I3c): a working node is available to the target.
func (s *simRun) recovered() bool {
	if s.paused || s.net != snOnline || s.center != scOK {
		return false
	}
	u := s.curURL()
	if s.nodes[s.hosts[hostOf(u)]].st.kind == skAlive {
		return true
	}
	for _, i := range s.candidates(u) {
		if s.nodes[i].st.kind == skAlive {
			return true
		}
	}
	return false
}

// simMaxChain: the longest uninterrupted stretch of loop work (one tick):
// netcheck + Phase A + every candidate's setup + pre-exhaust netcheck.
func (s *simRun) maxChain() time.Duration {
	return 2*simDurNCNoResp + simDurHang + time.Duration(s.sc.nodes-1)*simDurHang
}

func (s *simRun) tickBound() time.Duration {
	b := s.maxBackoff
	if b < holdBackoff[len(holdBackoff)-1] {
		b = holdBackoff[len(holdBackoff)-1]
	}
	return b
}

// i2Bound: echo detection (two re-races, each ≤ 3 strikes at the 16s
// not-probed ceiling, each followed by a dead Phase-A) + escalate (netcheck +
// the dead candidates' setups + the live one), or — when the engine is mid
// tick / scheduled / holding — the in-flight tick, one retry interval and one
// more tick.
func (s *simRun) i2Bound() time.Duration {
	sched := 2*s.maxChain() + s.tickBound()
	return max(s.i2Detect(), sched)
}

// i2Detect: echo detection + the escalation round (see i2Bound).
func (s *simRun) i2Detect() time.Duration {
	ceil := echoIntervals[len(echoIntervals)-1]
	detect := s.sc.dialEvery + 2*(time.Duration(echoReraceThreshold)*ceil+simDurTimeout)
	escal := simDurNCNoResp + time.Duration(s.sc.nodes-1)*simDurTimeout + simDurOK
	return detect + escal
}

func (s *simRun) statusCode(st Status) int {
	if st.Error == nil {
		return 0
	}
	return st.Error.Code
}

// codedSeen: a coded verdict X was answered in the current chain.
func (s *simRun) codedSeen(x int) (phaseA, allSetups, anySetup bool) {
	for _, a := range s.chainPhaseA {
		if a.code == x {
			phaseA = true
		}
	}
	if n := len(s.chainSetups); n > 0 {
		all := true
		last := 0
		for _, a := range s.chainSetups {
			if a.code == 0 {
				all = false
			} else {
				last = a.code
			}
			if a.code == x {
				anySetup = true
			}
		}
		allSetups = all && last == x
	}
	return
}

// checkFail: I1 / I6 at the moment the engine ended the session.
func (s *simRun) checkFail(code int) {
	s.res.failCode, s.res.failAt = code, s.at()
	switch code {
	case ErrCodeAuthRejected, ErrCodePaymentRequired, ErrCodeForbidden:
		pa, all, any := s.codedSeen(code)
		atCap := s.capHeld()
		if !(pa || all || (atCap && any)) {
			s.violate("I1", "failed %d without that verdict justifying it (phaseA=%v allSetupsCoded=%v capWindow=%v anySetup=%v)", code, pa, all, atCap, any)
		}
		return
	}
	if s.sc.mode == "manual" {
		s.violate("I6", "manual mode (no candidates) failed with %d", code)
		return
	}
	if code != ErrCodeServerUnreachable {
		s.violate("I1", "failed with unexpected code %d", code)
		return
	}
	if !s.capHeld() {
		since := "never"
		if !s.cOKSince.IsZero() {
			since = fmtDur(s.cOKSince.Sub(s.t0))
		}
		s.violate("I1", "failed 503 before %v of continuous online, non-code-less, transport-dead exhaustion (window open since %s)", simSpecCap, since)
	}
}

// pausedNow: total paused time so far (finished pauses + the current one),
// the oracle's copy of the engine's pausedTotal.
func (s *simRun) pausedNow() time.Duration {
	d := s.simPausedTotal
	if s.paused && s.pauseEnabled {
		d += s.now().Sub(s.simPauseStart)
	}
	return d
}

// capCounted: how long the I1 window has been open, paused time excluded.
func (s *simRun) capCounted() time.Duration {
	if s.cOKSince.IsZero() {
		return 0
	}
	return s.now().Sub(s.cOKSince) - (s.pausedNow() - s.cOKPauseBase)
}

// capDebounce: a window break shorter than one tick (the in-flight tick plus
// one retry interval) may go unseen by the engine — a node that comes back
// 1s after its race failed, a shell restore between two ticks — so it does
// not reset the window.
func (s *simRun) capDebounce() time.Duration { return s.tickBound() + s.maxChain() }

// capHeld: the window has lasted simSpecCap, or did until a break the
// engine could not yet have observed.
func (s *simRun) capHeld() bool {
	if s.capCounted() >= simSpecCap {
		return true
	}
	return s.cOKSince.IsZero() && !s.cOKSaved.IsZero() &&
		s.now().Sub(s.cOKBreakAt) <= s.capDebounce() && s.cOKSavedCount >= simSpecCap
}

// updateCapWindow: the I1 window. While paused it is frozen (paused time
// neither counts nor breaks it: the engine subtracts it and observes nothing
// while paused); a break shorter than capDebounce is bridged (the gap
// counts, as it does for the engine, which never saw it).
func (s *simRun) updateCapWindow(now time.Time) {
	if s.paused && s.pauseEnabled {
		return
	}
	if s.capWindowOK() {
		if s.cOKSince.IsZero() {
			if !s.cOKSaved.IsZero() && now.Sub(s.cOKBreakAt) <= s.capDebounce() {
				s.cOKSince, s.cOKPauseBase = s.cOKSaved, s.cOKSavedBase
			} else {
				s.cOKSince, s.cOKPauseBase, s.cOKLateFlagged = now, s.pausedNow(), false
			}
			s.cOKSaved = time.Time{}
		}
		s.checkCapLate()
		return
	}
	if !s.cOKSince.IsZero() {
		s.cOKSaved, s.cOKSavedBase, s.cOKSavedCount, s.cOKBreakAt = s.cOKSince, s.cOKPauseBase, s.capCounted(), now
		s.cOKSince = time.Time{}
	}
}

// checkCapLate (I1 liveness): once the window is open the cap must fire
// within simSpecCap plus detection (the engine has to notice the dead
// node and run its first counted round) plus one tick spacing and one tick.
// Only where the engine can see the failure: user traffic, a real network
// verdict, candidates mode.
func (s *simRun) checkCapLate() {
	if s.cOKLateFlagged || !s.sc.traffic || s.sc.noTargets || s.sc.mode == "manual" || s.net != snOnline {
		return
	}
	b := simSpecCap + s.i2Detect() + 2*(s.tickBound()+s.maxChain())
	if c := s.capCounted(); c > b {
		s.cOKLateFlagged = true
		s.violate("I1", "cap late: online transport-dead exhaustion for %v and the session still not ended (bound %v)", c.Round(time.Second), b)
	}
}

// observe updates the time-window oracles; idle=true at the loop's idle
// points (outer select / hold select), where the shown status is checked.
func (s *simRun) observe(idle bool) {
	now := s.now()
	if u := s.curURL(); u != s.lastURL {
		if s.lastURL != "" {
			if !s.p2Since.IsZero() {
				s.res.i2Lat = append(s.res.i2Lat, now.Sub(s.p2Since))
			}
			s.p2Since, s.p2Flagged = time.Time{}, false
		}
		s.lastURL = u
	}
	// I1 window.
	s.updateCapWindow(now)
	if s.net != snOnline {
		s.netOnlineSince = time.Time{}
	} else if s.netOnlineSince.IsZero() {
		s.netOnlineSince = now
	}
	// I2.
	if s.p2() {
		if s.p2Since.IsZero() {
			s.p2Since = now
		}
		if b := s.i2Bound(); !s.p2Flagged && now.Sub(s.p2Since) > b {
			s.p2Flagged = true
			s.violate("I2", "current node dead, live candidate available (online, Center ok) for %v without a failover (bound %v)", now.Sub(s.p2Since).Round(time.Second), b)
		}
	} else {
		s.p2Since, s.p2Flagged = time.Time{}, false
	}
	// I3 down.
	if s.net != snOnline {
		if s.downSince.IsZero() || s.downNet != s.net {
			s.downSince, s.downNet, s.downFlagged = now, s.net, false
		}
	} else {
		s.downSince, s.downFlagged = time.Time{}, false
	}
	if s.recovered() {
		if s.recSince.IsZero() {
			s.recSince = now
		}
	} else {
		s.recSince, s.recFlagged = time.Time{}, false
	}

	st := s.e.GetStatus()
	for _, p := range s.takePushes() {
		if c := s.statusCode(p); c == ErrCodeAuthRejected || c == ErrCodePaymentRequired || c == ErrCodeForbidden {
			pa, all, any := s.codedSeen(c)
			if !(pa || all || any) {
				s.violate("I3", "status pushed %d with no node answering %d in this chain", c, c)
			}
		}
	}
	if !idle || st.State == StateDisconnected {
		return
	}
	code := s.statusCode(st)
	inSchedule := st.State == StateReconnecting
	if inSchedule {
		if s.reconnSince.IsZero() {
			s.reconnSince = now
		}
	} else {
		s.reconnSince, s.lpFlagged = time.Time{}, false
	}
	// I3a: the shown hold follows the latest network verdict.
	if s.haveVerdict && s.lastVerdict.blocksFailover() && !s.okSinceVerd && st.State != StatePaused {
		want := s.lastVerdict.holdError().Code
		if !(st.State == StateReconnecting && code == want) {
			s.violate("I3", "latest netcheck verdict %s but status %s/%d at idle (want reconnecting/%d)", s.lastVerdict, st.State, code, want)
			s.okSinceVerd = true // once per verdict
		}
	}
	// I3 down: while holding / scheduled, a down network is shown within one tick.
	if inSchedule && !s.downSince.IsZero() && !s.downFlagged && !(s.sc.noTargets && s.net != snIfaceDown) {
		b := passiveFreshness + s.tickBound() + s.maxChain()
		from := s.downSince // paused time does not count: the engine observes nothing then
		if s.lastWakeAt.After(from) {
			from = s.lastWakeAt
		}
		if now.Sub(from) > b && code != s.net.code() {
			s.downFlagged = true
			s.violate("I3", "network %s for %v while reconnecting, status still %d (bound %v)", s.net, now.Sub(from).Round(time.Second), code, b)
		}
	}
	// I3c: no stale 503 once a working node is available: the clock runs
	// from the later of "a working node is available" and "503 shown".
	if code == ErrCodeServerUnreachable {
		if s.s503Since.IsZero() {
			s.s503Since = now
		}
	} else {
		s.s503Since = time.Time{}
	}
	if code == ErrCodeServerUnreachable && !s.recSince.IsZero() && !s.recFlagged {
		from := s.recSince
		if s.s503Since.After(from) {
			from = s.s503Since
		}
		b := 2*s.maxChain() + s.tickBound()
		if !s.lastRaceCodeLessAt.IsZero() {
			// The current node answers code-less: Phase B only every
			// simSpecCodeLessPhaseB (FB ruling).
			b = max(b, simSpecCodeLessPhaseB+s.tickBound()+s.maxChain())
		}
		if now.Sub(from) > b {
			s.recFlagged = true
			s.violate("I3", "stale 503: shown for %v while a working node is available (bound %v)", now.Sub(from).Round(time.Second), b)
		}
	}
	s.checkBudgets(now)
}

func (s *simRun) takePushes() []Status {
	s.statusMu.Lock()
	defer s.statusMu.Unlock()
	p := s.pushes
	s.pushes = nil
	return p
}

func countSince(ts []time.Time, from time.Time) int {
	n := 0
	for i := len(ts) - 1; i >= 0 && !ts[i].Before(from); i-- {
		n++
	}
	return n
}

// checkBudgets: I4.
//   - storm guard: races + setups in any 60s ≤ (1+N)·⌈60s/holdBackoff[0]⌉
//     (one full tick per shortest backoff step) + one in-flight tick;
//   - steady state: over 10 min of an unchanged world in which the engine was
//     reconnecting the whole time (after a 45s warm-up = 5+10+30s backoff),
//     netchecks ≤ 2·(⌈10m/30s⌉+1) and races+setups ≤ (1+N)·(⌈10m/30s⌉+1).
func (s *simRun) checkBudgets(now time.Time) {
	per := s.sc.nodes // 1 Phase-A + (nodes-1) setups per tick
	minStep := holdBackoff[0]
	storm := per*int((time.Minute+minStep-1)/minStep) + per
	if n := countSince(s.attempts, now.Add(-time.Minute)); n > storm {
		s.violate("I4", "%d races+setups in the last 60s (limit %d)", n, storm)
		s.attempts = s.attempts[:0]
	}
	if s.reconnSince.IsZero() {
		return
	}
	from := s.constSince
	if s.reconnSince.After(from) {
		from = s.reconnSince
	}
	warm := holdBackoff[0] + holdBackoff[1] + holdBackoff[2]
	win := 10 * time.Minute
	if now.Sub(from) < win+warm {
		return
	}
	last := holdBackoff[len(holdBackoff)-1]
	ticks := int(win/last) + 1
	if n := countSince(s.netchecks, now.Add(-win)); n > 2*ticks {
		s.violate("I4", "steady state: %d netchecks in 10 min (limit %d)", n, 2*ticks)
		s.constSince = now
	}
	if n := countSince(s.attempts, now.Add(-win)); n > per*ticks {
		s.violate("I4", "steady state: %d races+setups in 10 min (limit %d)", n, per*ticks)
		s.constSince = now
	}
	// Long pending (final-review-2 M4): a target on the schedule for
	// simSpecLongPending ticks every simSpecLongTick. The engine's
	// pendingSince is at most one tick after the status turned reconnecting,
	// so once a wait-tick lands past reconnSince + simSpecLongPending + one
	// tick + maxChain the next one is ≥ simSpecLongTick later (less the
	// netcheck-duration jitter of the DIAG timestamps). Wakes (net-event,
	// wake, recovered) only push the next tick further out. On an online
	// network there is no hold, so every wait-tick is a schedule tick.
	if s.net == snOnline && len(s.waitTicks) >= 2 && !s.lpFlagged {
		t1, t2 := s.waitTicks[len(s.waitTicks)-2], s.waitTicks[len(s.waitTicks)-1]
		// A hold (network down) is not the schedule: the target joins it
		// when the network is back, so pending runs from the later of the two.
		base := s.reconnSince
		if s.netOnlineSince.After(base) {
			base = s.netOnlineSince
		}
		thr := base.Add(simSpecLongPending + s.tickBound() + s.maxChain())
		if t2 != s.lpLastJudged && !t1.Before(thr) && !t1.Before(s.netOnlineSince) {
			s.lpLastJudged = t2
			s.res.lpPairs++
		}
		if !t1.Before(thr) && !t1.Before(s.netOnlineSince) && t2.Sub(t1) < simSpecLongTick-2*simDurNCNoResp+simDurNCFast {
			s.lpFlagged = true
			s.violate("I4", "long pending: schedule ticks %v apart after %v reconnecting (want ≥ %v)", t2.Sub(t1).Round(100*time.Millisecond), now.Sub(s.reconnSince).Round(time.Second), simSpecLongTick)
		}
	}
}

// simCountGoroutines: goroutine records in a runtime.Stack(all) dump.
func simCountGoroutines(dump []byte) int {
	n := 0
	for _, ln := range strings.Split(string(dump), "\n") {
		if strings.HasPrefix(ln, "goroutine ") && strings.HasSuffix(ln, ":") {
			n++
		}
	}
	return n
}

// simGoroutineSummary: "func×count" of each goroutine's top k2 frame, for
// the I5 message (the full dump goes to the scenario log).
func simGoroutineSummary(dump []byte) string {
	counts := map[string]int{}
	for _, g := range strings.Split(string(dump), "\n\n") {
		top := "?"
		for _, ln := range strings.Split(g, "\n") {
			if strings.HasPrefix(ln, "github.com/kaitu-io/") && !strings.Contains(ln, "sim_failover_test") && !strings.Contains(ln, "TestSim") {
				top = strings.TrimPrefix(ln, "github.com/kaitu-io/k2/")
				if k := strings.IndexByte(top, '('); k > 0 {
					top = top[:k]
				}
				break
			}
		}
		counts[top]++
	}
	var parts []string
	for f, c := range counts {
		parts = append(parts, fmt.Sprintf("%s×%d", f, c))
	}
	sort.Strings(parts)
	return strings.Join(parts, " ")
}

// ---------------------------------------------------------------------------
// Run

var simHandlerSingleton = &simHandler{}

func newSimRun(sc *simScenario, pauseEnabled bool) *simRun {
	s := &simRun{
		sc: sc, limit: sc.horizon, pauseEnabled: pauseEnabled,
		blk: make(chan *simReq), dead: make(chan struct{}), loopDone: make(chan struct{}),
		resume: make(chan simResp, 1),
		hosts:  map[string]int{},
	}
	s.t0 = time.Unix(1_000_000, 0)
	s.nowT = s.t0
	s.constSince = s.t0
	for i := 0; i < sc.nodes; i++ {
		s.urls = append(s.urls, fmt.Sprintf("k2v5://%s:443", s.hostOf(i)))
		s.hosts[s.hostOf(i)] = i
		s.nodes = append(s.nodes, &simNodeRT{st: simNodeState{kind: skAlive}})
	}
	return s
}

func (s *simRun) run() simResult {
	wallStart := time.Now()
	sc := s.sc

	origRace, origSetup := raceTransportFn, setupOutboundTransportFn
	defer func() { raceTransportFn, setupOutboundTransportFn = origRace, origSetup }()
	raceTransportFn = func(ctx context.Context, cfg wire.Config, _ string, _ wire.DirectDialer, _ *wire.CookieJar) (*wire.RaceResult, error) {
		resp := s.block(&simReq{kind: bkRace, host: cfg.Host})
		if resp.dead {
			return nil, errors.New("sim: ended")
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if resp.err != nil {
			return nil, resp.err
		}
		return &wire.RaceResult{Dialer: resp.dialer, TransportType: "tcpws"}, nil
	}
	setupOutboundTransportFn = func(ctx context.Context, via string, _ wire.DirectDialer, _ string, _ string) (*outboundTransport, wire.Dialer, error) {
		resp := s.block(&simReq{kind: bkSetup, host: hostOf(via), url: via})
		if resp.dead {
			return nil, nil, errors.New("sim: ended")
		}
		if resp.err != nil {
			return nil, nil, resp.err
		}
		return resp.ot, nil, nil
	}

	// Providers, as Start builds them.
	var fallbacks map[string][]string
	var shell OutboundProvider
	switch sc.mode {
	case "static":
		fallbacks = map[string][]string{s.urls[0]: append([]string(nil), s.urls[1:]...)}
	case "shell":
		s.shell = &simShell{urls: append([]string(nil), s.urls...), dropped: map[string]bool{}, excluded: map[string]bool{}}
		shell = s.shell
	}
	if s.shell == nil {
		s.shell = &simShell{dropped: map[string]bool{}, excluded: map[string]bool{}}
	}
	s.static = newStaticProvider(fallbacks, s.now)
	s.comp = newCompositeProvider(s.static, shell)

	// The target, connected to node 0.
	tm := wire.NewTransportManager()
	tm.SetConnFailClock(s.now)
	tm.SetPrimary(s.newDialer(0, true), "tcpws")
	s.target = &outboundTransport{
		tm:        tm,
		wireCfg:   wire.Config{Host: s.hostOf(0), Port: 443, DialAddr: s.hostOf(0) + ":443"},
		dialAddr:  s.hostOf(0) + ":443",
		name:      "k2v5-" + s.hostOf(0),
		url:       s.urls[0],
		swappable: wire.NewSwappableDialer(tm),
	}
	s.target.fromShell = shellOrigins(s.comp, []*outboundTransport{s.target})[s.target]

	e := New()
	s.e = e
	h := &simStatusHandler{s: s}
	e.mu.Lock()
	e.state = StateConnected
	e.connectedAt = time.Now()
	e.handler = h
	e.outboundTMs = []*outboundTransport{s.target}
	e.outboundProvider = s.comp
	e.reRaceChan = make(chan *outboundTransport, 1)
	e.netCoord = newNetCoordinator(func() { e.doNetworkReconnect() }, func() {})
	e.directTrack = newTrackingDialer(nil)
	e.netcheck = func(ctx context.Context) ([]checkResult, []time.Duration) {
		resp := s.block(&simReq{kind: bkNetcheck})
		return resp.nc, make([]time.Duration, len(resp.nc))
	}
	if !sc.noTargets {
		e.netcheckTargets = []config.NetcheckTarget{{URL: "http://check.example/generate_204", Status: 204}}
	}
	e.holdNow = s.now
	e.holdTimerFn = s.newTimer
	ctx, cancel := context.WithCancel(context.Background())
	s.ctx = ctx
	e.reRaceCancel = cancel
	e.replaceLoopIdle = func() { s.block(&simReq{kind: bkIdle}) }
	e.mu.Unlock()

	// The real detector on the same fake clock.
	s.hm = newHealthMonitor(s.target, []*outboundTransport{s.target}, nil, nil, nil, time.Now())
	s.hm.clock = s.now
	s.hm.reRaceChan = e.reRaceChan
	e.wireHealthRecovered(s.hm)
	e.wireHealthNetUp(s.hm)
	s.est = &echoLoopState{trustIdx: int32(len(echoIntervals) - 1)}
	s.nextProbe = s.t0.Add(sc.firstProbe)
	s.nextDial = s.t0.Add(sc.dialEvery)

	goroutinesBefore := runtime.NumGoroutine()
	go func() {
		defer close(s.loopDone)
		e.outboundReplaceLoop(ctx)
	}()

	s.drive()

	// End of scenario: Stop must return promptly and the loop must exit (I5).
	stopDone := make(chan struct{})
	go func() { _ = e.Stop(); close(stopDone) }()
	select {
	case <-stopDone:
	case <-time.After(10 * time.Second):
		s.violate("I5", "Stop did not return within 10s wall")
	}
	close(s.dead)
	select {
	case <-s.loopDone:
	case <-time.After(10 * time.Second):
		s.violate("I5", "outboundReplaceLoop did not exit within 10s wall after Stop")
	}
	cancel()
	// Goroutine leak: the TransportManagers spawn nothing that outlives Close.
	// 5s grace: on a loaded host a goroutine on its way out can take seconds.
	deadline := time.Now().Add(5 * time.Second)
	for runtime.NumGoroutine() > goroutinesBefore && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if n := runtime.NumGoroutine(); n > goroutinesBefore {
		buf := make([]byte, 1<<20)
		buf = buf[:runtime.Stack(buf, true)]
		// Judge by the dump itself: a goroutine that exits between the
		// count and the dump was slow, not leaked (loaded host).
		if n = simCountGoroutines(buf); n > goroutinesBefore {
			s.logf("goroutine dump:\n%s", buf)
			s.violate("I5", "goroutines %d after the scenario, %d before: %s", n, goroutinesBefore, simGoroutineSummary(buf))
		}
	}
	s.res.wall = time.Since(wallStart)
	if s.res.wall > 60*time.Second {
		s.violate("I5", "scenario took %v wall", s.res.wall)
	}
	s.res.violations = s.violations
	s.res.log = s.logBuf
	hsh := fnv.New64a()
	for _, l := range s.logBuf {
		hsh.Write([]byte(l))
		hsh.Write([]byte{'\n'})
	}
	s.res.hash = hsh.Sum64()
	return s.res
}

// drive is the discrete-event loop. Invariant: it acts only while the replace
// loop is parked in a handshake (req).
func (s *simRun) drive() {
	req := s.waitBlock()
	var seen *simReq
	for req != nil {
		if req != seen {
			seen = req
			s.onBlock(req)
		}
		if len(s.violations) >= 5 {
			break
		}
		switch req.kind {
		case bkRace, bkSetup, bkNetcheck:
			if s.call == nil || s.call.req != req {
				s.startCall(req)
			}
		case bkIdle, bkHold:
			s.pruneTimers()
			if s.readyNow(req) {
				req = s.release(req, simResp{})
				continue
			}
		}
		// Advance to the next event.
		next := s.t0.Add(s.limit)
		if s.evIdx < len(s.sc.events) {
			if t := s.t0.Add(s.sc.events[s.evIdx].at); t.Before(next) {
				next = t
			}
		}
		if s.sc.traffic && !s.paused && s.nextDial.Before(next) {
			next = s.nextDial
		}
		if s.nextProbe.Before(next) {
			next = s.nextProbe
		}
		if s.call != nil && s.call.at.Before(next) {
			next = s.call.at
		}
		var tmr *simTimer
		if req.kind == bkIdle || req.kind == bkHold {
			if tmr = s.listenedTimer(req); tmr != nil && tmr.at.Before(next) {
				next = tmr.at
			}
		}
		if !next.Before(s.t0.Add(s.limit)) {
			break
		}
		if next.Equal(s.lastStepAt) {
			s.sameInstant++
			if s.sameInstant > 20000 {
				s.violate("I4", "replace loop spinning: 20000 steps without fake time advancing")
				break
			}
		} else {
			s.sameInstant, s.lastStepAt = 0, next
		}
		if next.After(s.now()) {
			s.clkMu.Lock()
			s.nowT = next
			s.clkMu.Unlock()
		}
		now := s.now()
		// World events, then the user's traffic, then the echo probe.
		for s.evIdx < len(s.sc.events) && !s.t0.Add(s.sc.events[s.evIdx].at).After(now) {
			s.applyEvent(s.sc.events[s.evIdx])
			s.evIdx++
		}
		if s.sc.traffic && !s.paused && !s.nextDial.After(now) {
			s.userDial()
			s.nextDial = now.Add(s.sc.dialEvery)
		}
		if !s.nextProbe.After(now) {
			s.probe()
			s.nextProbe = now.Add(echoIntervals[s.est.trustIdx])
		}
		s.passive()
		s.observe(false)
		switch req.kind {
		case bkRace, bkSetup, bkNetcheck:
			if !s.call.at.After(now) {
				resp := s.call.resp
				s.call = nil
				req = s.release(req, resp)
			}
		case bkIdle, bkHold:
			if s.readyNow(req) {
				req = s.release(req, simResp{})
				continue
			}
			if tmr != nil && !tmr.at.After(now) {
				if req.kind == bkHold {
					s.hLastTry, s.hMuted = now, false // a backoff tick
				}
				s.tmu.Lock()
				tmr.fired = true
				s.tmu.Unlock()
				tmr.ch <- time.Time{}
				req = s.release(req, simResp{})
			}
		}
	}
	// The loop exited (fail) or the horizon was reached.
	s.readDiags()
	s.e.mu.Lock()
	state, lastErr := s.e.state, s.e.lastError
	s.e.mu.Unlock()
	if req == nil && !s.stalled && state != StateConnected {
		code := 0
		if lastErr != nil {
			code = lastErr.Code
		}
		s.logf("engine ended the session: %s/%d", state, code)
		s.checkFail(code)
	}
}

// ---------------------------------------------------------------------------
// Minimization and the test

func simRunScenario(sc *simScenario, pause bool) simResult {
	s := newSimRun(sc, pause)
	simHandlerSingleton.run.Store(s)
	defer simHandlerSingleton.run.Store(nil)
	return s.run()
}

// simMinimize greedily drops world events (and shortens the horizon) while
// the same invariant still fails.
func simMinimize(sc *simScenario, inv string, pause bool, budget int) (*simScenario, simResult) {
	cur := *sc
	cur.events = append([]simEvent(nil), sc.events...)
	best := simRunScenario(&cur, pause)
	same := func(r simResult) bool { return len(r.violations) > 0 && r.violations[0].inv == inv }
	if !same(best) {
		return sc, best
	}
	// Shorten the horizon to just past the violation.
	if h := best.violations[0].at + time.Minute; h < cur.horizon {
		try := cur
		try.horizon = h
		if r := simRunScenario(&try, pause); same(r) {
			cur, best = try, r
		}
	}
	runs := 0
	for changed := true; changed && runs < budget; {
		changed = false
		for i := len(cur.events) - 1; i >= 0 && runs < budget; i-- {
			try := cur
			try.events = append(append([]simEvent(nil), cur.events[:i]...), cur.events[i+1:]...)
			runs++
			if r := simRunScenario(&try, pause); same(r) {
				cur, best, changed = try, r, true
			}
		}
	}
	return &cur, best
}

func simEnvInt(name string, def int64) int64 {
	if v := os.Getenv(name); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	}
	return def
}

func simReport(sc *simScenario, r simResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\nviolations:\n", sc.header())
	for _, v := range r.violations {
		fmt.Fprintf(&b, "  %s\n", v)
	}
	fmt.Fprintf(&b, "world events:\n")
	for _, ev := range sc.events {
		fmt.Fprintf(&b, "  %s\n", ev)
	}
	fmt.Fprintf(&b, "event log (last 120 lines):\n")
	lines := r.log
	if len(lines) > 120 {
		lines = lines[len(lines)-120:]
	}
	for _, l := range lines {
		fmt.Fprintf(&b, "  %s\n", l)
	}
	fmt.Fprintf(&b, "repro: K2_SIM_SEED=%d go test -run 'TestSimFailover$' ./engine/\n", sc.seed)
	return b.String()
}

func TestSimFailover(t *testing.T) {
	prev := slog.Default()
	slog.SetDefault(slog.New(simHandlerSingleton))
	t.Cleanup(func() { slog.SetDefault(prev) })

	pause := os.Getenv("K2_SIM_PAUSE") != "0" // the fix wave honours Pause (final-review-2 I1)
	allow := map[string]bool{}
	for _, c := range strings.Split(os.Getenv("K2_SIM_ALLOW"), ",") {
		if c = strings.TrimSpace(c); c != "" {
			allow[c] = true
		}
	}
	accepted := map[string]int{}
	var seeds []int64
	if one := os.Getenv("K2_SIM_SEED"); one != "" {
		n, _ := strconv.ParseInt(one, 10, 64)
		seeds = []int64{n}
	} else {
		base := simEnvInt("K2_SIM_SEED_BASE", 1)
		count := simEnvInt("K2_SIM_SEEDS", simDefaultSeeds)
		for i := int64(0); i < count; i++ {
			seeds = append(seeds, base+i)
		}
	}
	start := time.Now()
	var (
		failed    int
		byInv     = map[string]int{}
		fails     = map[int]int{}
		failovers int
		caps      int
		lpPairs   int
		holdRec   [2]int
		steps     int
		lat       []time.Duration
		modes     = map[string]int{}
		reported  int
	)
	for _, seed := range seeds {
		sc := simGen(seed, pause)
		modes[sc.mode]++
		r := simRunScenario(sc, pause)
		steps += r.steps
		failovers += r.failovers
		caps += r.caps
		lpPairs += r.lpPairs
		holdRec[0] += r.holdRecOK
		holdRec[1] += r.holdRecDrp
		lat = append(lat, r.i2Lat...)
		if r.failCode != 0 {
			fails[r.failCode]++
		}
		var real []simViolation
		for _, v := range r.violations {
			if allow[v.class] {
				accepted[v.class]++
				continue
			}
			real = append(real, v)
		}
		for _, v := range r.violations {
			t.Logf("SIMFAIL seed=%d %s", seed, v)
		}
		if len(real) == 0 {
			continue
		}
		r.violations = real
		failed++
		byInv[r.violations[0].inv]++
		if reported >= 1 {
			t.Errorf("seed %d: %s (further report suppressed)", seed, r.violations[0])
			continue
		}
		reported++
		msc, mr := sc, r
		if os.Getenv("K2_SIM_NOMIN") != "1" {
			msc, mr = simMinimize(sc, r.violations[0].inv, pause, int(simEnvInt("K2_SIM_MIN_RUNS", 60)))
		}
		t.Errorf("seed %d violates %s\n--- minimized ---\n%s", seed, r.violations[0].inv, simReport(msc, mr))
	}
	el := time.Since(start)
	sort.Slice(lat, func(i, j int) bool { return lat[i] < lat[j] })
	pct := func(p float64) time.Duration {
		if len(lat) == 0 {
			return 0
		}
		return lat[int(p*float64(len(lat)-1))]
	}
	t.Logf("sim: %d seeds in %v (%.1f seeds/s), %d failing (%v), accepted findings %v; modes %v; steps %d; failovers %d; caps %d; long-pending tick pairs %d; hold recovered wakes taken/dropped %d/%d; session ends by code %v; I2 latency n=%d p50=%v p99=%v max=%v",
		len(seeds), el.Round(time.Millisecond), float64(len(seeds))/el.Seconds(), failed, byInv, accepted, modes, steps, failovers, caps, lpPairs, holdRec[0], holdRec[1], fails,
		len(lat), pct(0.5).Round(time.Second), pct(0.99).Round(time.Second), pct(1).Round(time.Second))
	if os.Getenv("K2_SIM_SEED") != "" {
		r := simRunScenario(simGen(seeds[0], pause), pause)
		t.Logf("seed %d full log:\n%s", seeds[0], strings.Join(r.log, "\n"))
	}
}

// TestSimFailoverDeterministic: a seed replays identically (event-log hash).
func TestSimFailoverDeterministic(t *testing.T) {
	prev := slog.Default()
	slog.SetDefault(slog.New(simHandlerSingleton))
	t.Cleanup(func() { slog.SetDefault(prev) })
	for _, seed := range []int64{3, 17, 42} {
		a := simRunScenario(simGen(seed, false), false)
		b := simRunScenario(simGen(seed, false), false)
		if a.hash != b.hash || len(a.log) != len(b.log) {
			t.Fatalf("seed %d is not reproducible: %d vs %d log lines", seed, len(a.log), len(b.log))
		}
	}
}
