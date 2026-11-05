package engine

// Tests for outboundReplaceLoop's network-verdict gating, hold (wait for the
// user's network) and silent failover. Everything time-related is injected:
// netcheck results, interface state, passive evidence, Phase-A re-race,
// Phase-B setup and the hold backoff timer — no test sleeps for seconds.

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	mdns "github.com/miekg/dns"
	"go.uber.org/goleak"

	"github.com/kaitu-io/k2/config"
	"github.com/kaitu-io/k2/provider"
	"github.com/kaitu-io/k2/rule"
	"github.com/kaitu-io/k2/wire"
)

const (
	foURLA = "k2v5://a.example:443"
	foURLB = "k2v5://b.example:443"
	foURLC = "k2v5://c.example:443"
)

// foHandler records every status push and OnOutboundFatal call.
type foHandler struct {
	mu       sync.Mutex
	statuses []Status
	fatal    int
}

func (h *foHandler) OnStatus(s Status) {
	h.mu.Lock()
	h.statuses = append(h.statuses, s)
	h.mu.Unlock()
}
func (h *foHandler) OnStats(int64, int64) {}
func (h *foHandler) OnOutboundFatal(string, error) {
	h.mu.Lock()
	h.fatal++
	h.mu.Unlock()
}

func (h *foHandler) last() (Status, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.statuses) == 0 {
		return Status{}, false
	}
	return h.statuses[len(h.statuses)-1], true
}

func (h *foHandler) sawState(state string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, s := range h.statuses {
		if s.State == state {
			return true
		}
	}
	return false
}

// sawCode reports whether any pushed status carried error code.
func (h *foHandler) sawCode(code int) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, s := range h.statuses {
		if s.Error != nil && s.Error.Code == code {
			return true
		}
	}
	return false
}

func (h *foHandler) fatalCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.fatal
}

// foNet is a programmable netcheck: run returns the current results.
type foNet struct {
	mu    sync.Mutex
	rs    []checkResult
	calls int
}

func (n *foNet) set(rs ...checkResult) {
	n.mu.Lock()
	n.rs = append([]checkResult(nil), rs...)
	n.mu.Unlock()
}

func (n *foNet) run(context.Context) ([]checkResult, []time.Duration) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.calls++
	return append([]checkResult(nil), n.rs...), make([]time.Duration, len(n.rs))
}

func (n *foNet) callCount() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.calls
}

// foTimer is one hold backoff timer the loop asked for.
type foTimer struct {
	d     time.Duration
	ch    chan time.Time
	at    time.Time // fake-clock deadline
	clock *foClock
}

// fire advances the fake clock to the timer's deadline, then delivers.
func (t foTimer) fire() {
	t.clock.advanceTo(t.at)
	t.ch <- time.Time{}
}

// foClock is the fake clock behind e.holdNow; it moves only when a timer fires.
type foClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *foClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *foClock) advanceTo(t time.Time) {
	c.mu.Lock()
	if t.After(c.now) {
		c.now = t
	}
	c.mu.Unlock()
}

// foServers decides which hosts answer Phase-A races and Phase-B setups.
type foServers struct {
	mu        sync.Mutex
	alive     map[string]bool
	races     map[string]int
	setups    map[string]int
	setupErrs map[string]error                       // host → error its setup fails with (overrides alive)
	onSetup   func(host string)                      // called (outside mu) on each setup, before the verdict
	onRace    func(ctx context.Context, host string) // called (outside mu) on each race, before the verdict
	setupDone chan string
}

func (s *foServers) setAlive(host string, alive bool) {
	s.mu.Lock()
	s.alive[host] = alive
	s.mu.Unlock()
}

func (s *foServers) isAlive(host string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.alive[host]
}

func (s *foServers) raceCount(host string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.races[host]
}

// countingProvider wraps an OutboundProvider and counts NextURL calls. It
// forwards OwnsURL and Reset so it is transparent to the loop.
type countingProvider struct {
	inner  OutboundProvider
	mu     sync.Mutex
	calls  int
	resets int
}

func (p *countingProvider) NextURL(cur string, reason error) (string, error) {
	p.mu.Lock()
	p.calls++
	p.mu.Unlock()
	return p.inner.NextURL(cur, reason)
}

func (p *countingProvider) Reset() {
	p.mu.Lock()
	p.resets++
	p.mu.Unlock()
	if r, ok := p.inner.(interface{ Reset() }); ok {
		r.Reset()
	}
}

// shellOwns forwards the compositeProvider's origin query (fromShell).
func (p *countingProvider) shellOwns(u string) bool { return providerShellOwns(p.inner, u) }

func (p *countingProvider) resetCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.resets
}

func (p *countingProvider) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

// shellOwner is a fake shell provider (desktop subscription manager shape)
// that owns a fixed URL set and counts NextURL calls.
type shellOwner struct {
	owns  map[string]bool
	mu    sync.Mutex
	calls int
}

func (s *shellOwner) OwnsURL(u string) bool { return s.owns[u] }
func (s *shellOwner) NextURL(string, error) (string, error) {
	s.mu.Lock()
	s.calls++
	s.mu.Unlock()
	return "", errors.New("shell: exhausted")
}
func (s *shellOwner) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

type foHarness struct {
	t        *testing.T
	e        *Engine
	target   *outboundTransport
	h        *foHandler
	net      *foNet
	servers  *foServers
	provider *countingProvider
	static   *staticProvider
	timers   chan foTimer
	clock    *foClock // behind e.holdNow
	cancel   context.CancelFunc
	done     chan struct{}

	drainOnce sync.Once // exhaust_cap_test.go drainSetups
}

type foOpts struct {
	fallbacks map[string][]string
	shell     OutboundProvider
	net       []checkResult
	idle      func() // e.replaceLoopIdle, installed before the loop starts
	targetURL string // the target's starting URL; default foURLA
}

func hostOf(u string) string {
	p, err := url.Parse(u)
	if err != nil {
		return u
	}
	return p.Hostname()
}

func newFoHarness(t *testing.T, o foOpts) *foHarness {
	t.Helper()
	servers := &foServers{
		alive:     map[string]bool{},
		races:     map[string]int{},
		setups:    map[string]int{},
		setupErrs: map[string]error{},
		setupDone: make(chan string, 16),
	}

	origRace := raceTransportFn
	raceTransportFn = func(ctx context.Context, cfg wire.Config, _ string, _ wire.DirectDialer, _ *wire.CookieJar) (*wire.RaceResult, error) {
		servers.mu.Lock()
		servers.races[cfg.Host]++
		hook := servers.onRace
		servers.mu.Unlock()
		if hook != nil {
			hook(ctx, cfg.Host)
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if !servers.isAlive(cfg.Host) {
			return nil, fmt.Errorf("dial %s: connection refused", cfg.Host)
		}
		return &wire.RaceResult{Dialer: &stubDialer{}, TransportType: "quic-443"}, nil
	}
	origSetup := setupOutboundTransportFn
	setupOutboundTransportFn = func(_ context.Context, via string, _ wire.DirectDialer, _ string, _ string) (*outboundTransport, wire.Dialer, error) {
		host := hostOf(via)
		servers.mu.Lock()
		servers.setups[host]++
		hook := servers.onSetup
		servers.mu.Unlock()
		if hook != nil {
			hook(host)
		}
		defer func() { servers.setupDone <- host }()
		servers.mu.Lock()
		forced := servers.setupErrs[host]
		servers.mu.Unlock()
		if forced != nil {
			return nil, nil, forced
		}
		if !servers.isAlive(host) {
			return nil, nil, fmt.Errorf("setup %s: connection refused", host)
		}
		tm := wire.NewTransportManager()
		return &outboundTransport{
			tm:        tm,
			wireCfg:   wire.Config{Host: host, Port: 443, DialAddr: host + ":443"},
			dialAddr:  host + ":443",
			name:      "k2v5-" + host,
			url:       via,
			swappable: wire.NewSwappableDialer(tm),
		}, nil, nil
	}
	t.Cleanup(func() {
		raceTransportFn = origRace
		setupOutboundTransportFn = origSetup
	})

	startURL := o.targetURL
	if startURL == "" {
		startURL = foURLA
	}
	tm := wire.NewTransportManager()
	target := &outboundTransport{
		tm:        tm,
		wireCfg:   wire.Config{Host: hostOf(startURL), Port: 443, DialAddr: hostOf(startURL) + ":443"},
		dialAddr:  hostOf(startURL) + ":443",
		name:      "k2v5-" + hostOf(startURL),
		url:       startURL,
		swappable: wire.NewSwappableDialer(tm),
	}

	static := newStaticProvider(o.fallbacks, time.Now)
	provider := &countingProvider{inner: newCompositeProvider(static, o.shell)}
	fnet := &foNet{}
	fnet.set(o.net...)
	h := &foHandler{}
	timers := make(chan foTimer, 64)

	e := New()
	e.mu.Lock()
	e.state = StateConnected
	e.connectedAt = time.Now()
	e.handler = h
	e.outboundTMs = []*outboundTransport{target}
	e.outboundProvider = provider
	e.reRaceChan = make(chan *outboundTransport, 1)
	e.netCoord = newNetCoordinator(func() { e.doNetworkReconnect() }, func() {})
	e.directTrack = newTrackingDialer(nil)
	e.netcheck = fnet.run
	e.netcheckTargets = []config.NetcheckTarget{{URL: "http://check.example/generate_204", Status: 204}}
	clock := &foClock{now: time.Unix(1_000_000, 0)}
	e.holdNow = clock.Now
	e.holdTimerFn = func(d time.Duration) (<-chan time.Time, func()) {
		ch := make(chan time.Time, 1)
		timers <- foTimer{d: d, ch: ch, at: clock.Now().Add(d), clock: clock}
		return ch, func() {}
	}
	ctx, cancel := context.WithCancel(context.Background())
	e.reRaceCancel = cancel
	e.replaceLoopIdle = o.idle
	e.mu.Unlock()

	hs := &foHarness{
		t: t, e: e, target: target, h: h, net: fnet, servers: servers,
		provider: provider, static: static, timers: timers, clock: clock,
		cancel: cancel, done: make(chan struct{}),
	}
	go func() {
		e.outboundReplaceLoop(ctx)
		close(hs.done)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-hs.done:
		case <-time.After(5 * time.Second):
			t.Error("outboundReplaceLoop did not exit after cancel")
		}
	})
	return hs
}

// signal delivers one echo-failure signal (one Phase-A attempt).
func (hs *foHarness) signal() {
	hs.t.Helper()
	select {
	case hs.e.reRaceChan <- hs.target:
	case <-time.After(5 * time.Second):
		hs.t.Fatal("reRaceChan send blocked: loop not consuming")
	}
}

// escalate sends enough signals to reach the Phase-B threshold. The second
// send only lands once the loop has consumed the first, so both are
// processed in order.
func (hs *foHarness) escalate() {
	hs.t.Helper()
	for i := 0; i < reRaceFailThresholdForReplace; i++ {
		hs.signal()
	}
}

func (hs *foHarness) nextTimer() foTimer {
	hs.t.Helper()
	select {
	case tm := <-hs.timers:
		return tm
	case <-time.After(5 * time.Second):
		hs.t.Fatal("loop never armed a hold timer")
		return foTimer{}
	}
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for: %s", what)
}

func (hs *foHarness) waitStatus(what string, ok func(Status) bool) {
	hs.t.Helper()
	eventually(hs.t, what, func() bool {
		s, have := hs.h.last()
		return have && ok(s)
	})
}

func isReconnectingCode(code int) func(Status) bool {
	return func(s Status) bool {
		return s.State == StateReconnecting && s.Error != nil && s.Error.Code == code
	}
}

func (hs *foHarness) targetURL() string {
	hs.e.mu.Lock()
	defer hs.e.mu.Unlock()
	return hs.target.url
}

func (hs *foHarness) engineState() (string, *EngineError) {
	hs.e.mu.Lock()
	defer hs.e.mu.Unlock()
	return hs.e.state, hs.e.lastError
}

func TestReplaceLoop_OnlineFailsOverToFallback(t *testing.T) {
	hs := newFoHarness(t, foOpts{
		fallbacks: map[string][]string{foURLA: {foURLB, foURLC}},
		net:       []checkResult{checkMatched},
	})
	hs.servers.setAlive("b.example", true)

	hs.escalate()
	eventually(t, "target switched to b", func() bool { return hs.targetURL() == foURLB })

	if st, le := hs.engineState(); st != StateConnected || le != nil {
		t.Fatalf("engine = %s / %v, want connected / nil", st, le)
	}
	if hs.h.sawState(StateDisconnected) {
		t.Fatal("status went through disconnected during a silent failover")
	}
	if hs.h.fatalCount() != 0 {
		t.Fatalf("OnOutboundFatal = %d, want 0", hs.h.fatalCount())
	}
}

// R1: an exhaustion on a healthy network is scheduled (reconnecting 503);
// only exhaustFailAfter of it fails the engine with 503.
func TestReplaceLoop_AllDeadOnlineFails503AtCap(t *testing.T) {
	hs := newFoHarness(t, foOpts{
		fallbacks: map[string][]string{foURLA: {foURLB, foURLC}},
		net:       []checkResult{checkMatched},
	})

	hs.escalate()
	hs.waitStatus("reconnecting 503", isReconnectingCode(ErrCodeServerUnreachable))
	if s, _ := hs.h.last(); s.Error == nil || !strings.Contains(s.Error.Message, "all replacement candidates failed; retrying") {
		t.Fatalf("hold = %+v, want the exhaustion reason", s.Error)
	}
	hs.assertExhaustedScheduled("after escalate")
	hs.startTicks().runUntilCapFails(0, 20*time.Minute)
	st, le := hs.engineState()
	if st != StateDisconnected || le == nil || le.Code != ErrCodeServerUnreachable {
		t.Fatalf("engine = %s / %v, want disconnected / 503", st, le)
	}
	if n := hs.h.fatalCount(); n != 1 {
		t.Fatalf("OnOutboundFatal = %d, want 1", n)
	}
	s, _ := hs.h.last()
	if s.State != StateDisconnected || s.Error == nil || s.Error.Code != ErrCodeServerUnreachable {
		t.Fatalf("last status = %+v, want disconnected/503", s)
	}
}

func TestReplaceLoop_OfflineHoldsAndDoesNotConsumeCandidates(t *testing.T) {
	hs := newFoHarness(t, foOpts{
		fallbacks: map[string][]string{foURLA: {foURLB, foURLC}},
		net:       []checkResult{checkNoResponse, checkNoResponse},
	})

	hs.escalate()
	hs.waitStatus("reconnecting 106", isReconnectingCode(ErrCodeNoInternet))
	tm := hs.nextTimer()
	if tm.d != 5*time.Second {
		t.Fatalf("first hold timer = %v, want 5s", tm.d)
	}
	if n := hs.provider.count(); n != 0 {
		t.Fatalf("provider.NextURL called %d times while offline, want 0", n)
	}
	if st, _ := hs.engineState(); st != StateConnected {
		t.Fatalf("engine state = %s, want connected (hold keeps the tunnel)", st)
	}

	racesBefore := hs.servers.raceCount("a.example")
	hs.net.set(checkMatched)
	hs.servers.setAlive("a.example", true)
	tm.fire()

	hs.waitStatus("connected, hold cleared", func(s Status) bool {
		return s.State == StateConnected && s.Error == nil
	})
	eventually(t, "Phase-A on the primary after the hold", func() bool {
		return hs.servers.raceCount("a.example") == racesBefore+1
	})
	if n := hs.provider.count(); n != 0 {
		t.Fatalf("provider.NextURL called %d times, want 0 (Phase-A recovered the primary)", n)
	}
	if u := hs.targetURL(); u != foURLA {
		t.Fatalf("target = %s, want primary a", u)
	}
}

func TestReplaceLoop_NetworkDropsMidFailover_WaitsNotFails(t *testing.T) {
	hs := newFoHarness(t, foOpts{
		fallbacks: map[string][]string{foURLA: {foURLB, foURLC}},
		net:       []checkResult{checkMatched},
	})
	hs.servers.mu.Lock()
	hs.servers.onSetup = func(host string) {
		if host == "b.example" {
			hs.net.set(checkNoResponse) // the user's network dies mid-failover
		}
	}
	hs.servers.mu.Unlock()

	hs.escalate()
	hs.waitStatus("reconnecting 106 after exhaustion", isReconnectingCode(ErrCodeNoInternet))
	tm := hs.nextTimer()
	if st, _ := hs.engineState(); st != StateConnected {
		t.Fatalf("engine state = %s, want connected", st)
	}
	if hs.h.sawState(StateDisconnected) {
		t.Fatal("went disconnected although the network, not the nodes, failed")
	}
	if hs.h.fatalCount() != 0 {
		t.Fatalf("OnOutboundFatal = %d, want 0", hs.h.fatalCount())
	}
	// The exclusions gathered while the network was dying were reset: the
	// static provider offers b again for a.
	if u, err := hs.static.NextURL(foURLA, nil); err != nil || u != foURLB {
		t.Fatalf("after Reset NextURL(a) = %q, %v; want b", u, err)
	}
	hs.static.Reset() // undo the probe's own exclusion of a

	// Recovery: network back, primary back → Phase-A on the primary first.
	racesBefore := hs.servers.raceCount("a.example")
	hs.net.set(checkMatched)
	hs.servers.setAlive("a.example", true)
	tm.fire()
	hs.waitStatus("connected after recovery", func(s Status) bool {
		return s.State == StateConnected && s.Error == nil
	})
	eventually(t, "Phase-A on the primary", func() bool {
		return hs.servers.raceCount("a.example") == racesBefore+1
	})
	if u := hs.targetURL(); u != foURLA {
		t.Fatalf("target = %s, want primary a", u)
	}
}

func TestReplaceLoop_CaptiveReports107(t *testing.T) {
	hs := newFoHarness(t, foOpts{
		fallbacks: map[string][]string{foURLA: {foURLB}},
		net:       []checkResult{checkOtherResponse},
	})
	hs.escalate()
	hs.waitStatus("reconnecting 107", isReconnectingCode(ErrCodeCaptivePortal))
	if n := hs.provider.count(); n != 0 {
		t.Fatalf("provider.NextURL = %d, want 0", n)
	}
}

func TestReplaceLoop_IfaceDownReports101WithoutProbing(t *testing.T) {
	hs := newFoHarness(t, foOpts{
		fallbacks: map[string][]string{foURLA: {foURLB}},
		net:       []checkResult{checkMatched}, // would say online if consulted
	})
	hs.e.mu.Lock()
	nc := hs.e.netCoord
	hs.e.mu.Unlock()
	nc.handleEvent(&NetEvent{Signal: SignalUnavailable, Source: "test"})

	hs.escalate()
	hs.waitStatus("reconnecting 101", isReconnectingCode(ErrCodeNetworkUnavailable))
	if n := hs.net.callCount(); n != 0 {
		t.Fatalf("active netcheck ran %d times with the interface down, want 0", n)
	}
	if n := hs.provider.count(); n != 0 {
		t.Fatalf("provider.NextURL = %d, want 0", n)
	}
}

func TestReplaceLoop_PassiveEvidenceSkipsActiveCheck(t *testing.T) {
	hs := newFoHarness(t, foOpts{
		fallbacks: map[string][]string{foURLA: {foURLB}},
		net:       []checkResult{checkNoResponse}, // would say offline if consulted
	})
	hs.servers.setAlive("b.example", true)
	hs.e.mu.Lock()
	dt := hs.e.directTrack
	hs.e.mu.Unlock()
	dt.setLastOKForTest(time.Now())

	hs.escalate()
	eventually(t, "target switched to b", func() bool { return hs.targetURL() == foURLB })
	if n := hs.net.callCount(); n != 0 {
		t.Fatalf("active netcheck ran %d times with fresh passive evidence, want 0", n)
	}
}

func TestReplaceLoop_NoCandidatesKeepsReRacing(t *testing.T) {
	hs := newFoHarness(t, foOpts{net: []checkResult{checkMatched}})

	hs.escalate()
	hs.waitStatus("reconnecting 503", isReconnectingCode(ErrCodeServerUnreachable))
	if st, _ := hs.engineState(); st != StateConnected {
		t.Fatalf("engine state = %s, want connected (no candidates must not fail)", st)
	}
	if hs.h.fatalCount() != 0 {
		t.Fatalf("OnOutboundFatal = %d, want 0", hs.h.fatalCount())
	}

	// Each wait tick re-races the current node; a failure keeps 503.
	tm := hs.nextTimer()
	if tm.d != 5*time.Second {
		t.Fatalf("first no-candidates timer = %v, want 5s", tm.d)
	}
	before := hs.servers.raceCount("a.example")
	tm.fire()
	if d := hs.nextTimer().d; d != 10*time.Second {
		t.Fatalf("second no-candidates timer = %v, want 10s", d)
	}
	if n := hs.servers.raceCount("a.example"); n != before+1 {
		t.Fatalf("races on a after a wait tick = %d, want %d", n, before+1)
	}
	if s, _ := hs.h.last(); !isReconnectingCode(ErrCodeServerUnreachable)(s) {
		t.Fatalf("status after a failed tick = %+v, want reconnecting/503", s)
	}
	if hs.h.sawState(StateDisconnected) {
		t.Fatal("went disconnected")
	}
}

// TestReplaceLoop_NoCandidatesRecoversWithoutSignal: the node comes back on
// its own and no further echo signal arrives (wire healed lazily, or no
// cached connection to strike on) — the wait's own Phase-A must still bring
// status back to connected.
func TestReplaceLoop_NoCandidatesRecoversWithoutSignal(t *testing.T) {
	hs := newFoHarness(t, foOpts{net: []checkResult{checkMatched}})
	hs.escalate()
	hs.waitStatus("reconnecting 503", isReconnectingCode(ErrCodeServerUnreachable))
	tm := hs.nextTimer()

	hs.servers.setAlive("a.example", true)
	tm.fire() // no hs.signal()
	hs.waitStatus("connected without another echo signal", func(s Status) bool {
		return s.State == StateConnected && s.Error == nil
	})
	if gs := hs.e.GetStatus(); gs.State != StateConnected || gs.Error != nil {
		t.Fatalf("GetStatus = %+v, want connected", gs)
	}
	if n := ReRaceFailStreak(hs.e, hs.target); n != 0 {
		t.Fatalf("reRaceFailStreak = %d, want 0", n)
	}
}

// TestReplaceLoop_NoCandidatesShowsNetworkReason: while retrying a node with
// no candidates, a network verdict that is not online is the reason shown.
func TestReplaceLoop_NoCandidatesShowsNetworkReason(t *testing.T) {
	hs := newFoHarness(t, foOpts{net: []checkResult{checkMatched}})
	hs.escalate()
	hs.waitStatus("reconnecting 503", isReconnectingCode(ErrCodeServerUnreachable))
	hs.net.set(checkNoResponse)
	hs.nextTimer().fire()
	hs.waitStatus("reconnecting 106", isReconnectingCode(ErrCodeNoInternet))
	hs.net.set(checkMatched)
	hs.nextTimer().fire()
	hs.waitStatus("back to reconnecting 503", isReconnectingCode(ErrCodeServerUnreachable))
}

// TestReplaceLoop_PhaseBSuccessClearsHold: a hold left over (e.g. from a
// no-candidates wait) must not outlive a successful node switch.
func TestReplaceLoop_PhaseBSuccessClearsHold(t *testing.T) {
	hs := newFoHarness(t, foOpts{
		fallbacks: map[string][]string{foURLA: {foURLB}},
		net:       []checkResult{checkMatched},
	})
	hs.servers.setAlive("b.example", true)
	hs.e.SetHoldForTest(noCandidatesError("k2v5-a.example"))
	hs.waitStatus("pre-existing 503 hold", isReconnectingCode(ErrCodeServerUnreachable))

	hs.escalate()
	eventually(t, "target switched to b", func() bool { return hs.targetURL() == foURLB })
	hs.waitStatus("connected after Phase-B success", func(s Status) bool {
		return s.State == StateConnected && s.Error == nil
	})
	if gs := hs.e.GetStatus(); gs.State != StateConnected || gs.Error != nil {
		t.Fatalf("GetStatus = %+v, want connected", gs)
	}
}

// TestReplaceLoop_StaleWakeDoesNotSkipFirstStep: a network event from before
// the pre-phaseB verdict must not wake the hold at once — the first re-judge
// waits for the 5s timer.
func TestReplaceLoop_StaleWakeDoesNotSkipFirstStep(t *testing.T) {
	hs := newFoHarness(t, foOpts{
		fallbacks: map[string][]string{foURLA: {foURLB}},
		net:       []checkResult{checkNoResponse},
	})
	hs.e.wakeHold() // e.g. a routine "changed" event earlier in the session

	hs.escalate()
	tm := hs.nextTimer()
	if tm.d != 5*time.Second {
		t.Fatalf("first timer = %v, want 5s", tm.d)
	}
	select {
	case extra := <-hs.timers:
		t.Fatalf("hold re-armed (%v) without its timer firing: a stale wake skipped the first step", extra.d)
	case <-time.After(300 * time.Millisecond):
	}
	if n := hs.net.callCount(); n != 1 {
		t.Fatalf("netcheck ran %d times before the first 5s timer, want 1 (pre-phaseB only)", n)
	}
	// A wake sent after the verdict is genuine and still wakes the hold.
	hs.e.wakeHold()
	eventually(t, "post-verdict wake re-judges", func() bool { return hs.net.callCount() == 2 })
}

// TestReplaceLoop_QueuedSignalDroppedAfterRecovery: echo signals queued while
// waiting must not trigger an extra Phase-A right after the wait recovers.
func TestReplaceLoop_QueuedSignalDroppedAfterRecovery(t *testing.T) {
	hs := newFoHarness(t, foOpts{
		fallbacks: map[string][]string{foURLA: {foURLB}},
		net:       []checkResult{checkNoResponse},
	})
	hs.escalate()
	hs.waitStatus("reconnecting 106", isReconnectingCode(ErrCodeNoInternet))
	tm := hs.nextTimer()

	hs.signal() // echo strike during the hold; cap-1 chan, the loop is waiting
	before := hs.servers.raceCount("a.example")
	hs.servers.setAlive("a.example", true)
	tm.fire()
	hs.waitStatus("connected via wait-tick Phase-A", func(s Status) bool {
		return s.State == StateConnected && s.Error == nil
	})
	time.Sleep(300 * time.Millisecond) // window for a stale signal to be consumed
	if n := hs.servers.raceCount("a.example"); n != before+1 {
		t.Fatalf("races on a = %d, want %d (queued signal must be dropped)", n, before+1)
	}
	if n := len(hs.e.reRaceChan); n != 0 {
		t.Fatalf("reRaceChan len = %d, want 0", n)
	}
}

// TestDropQueuedSignal_KeepsOtherOutbound: only the recovered target's signal
// is dropped; another outbound's queued signal survives.
func TestDropQueuedSignal_KeepsOtherOutbound(t *testing.T) {
	e := New()
	e.reRaceChan = make(chan *outboundTransport, 1)
	a, b := &outboundTransport{name: "a"}, &outboundTransport{name: "b"}
	e.reRaceChan <- b
	e.dropQueuedSignal(a)
	select {
	case got := <-e.reRaceChan:
		if got != b {
			t.Fatalf("queued signal = %s, want b", got.name)
		}
	default:
		t.Fatal("other outbound's signal lost")
	}
	e.reRaceChan <- a
	e.dropQueuedSignal(a)
	if len(e.reRaceChan) != 0 {
		t.Fatal("recovered target's signal not dropped")
	}
	e.dropQueuedSignal(a) // empty: no block
}

func TestReplaceLoop_HoldSurvivesNetworkReconnectPush(t *testing.T) {
	hs := newFoHarness(t, foOpts{
		fallbacks: map[string][]string{foURLA: {foURLB}},
		net:       []checkResult{checkNoResponse},
	})
	hs.escalate()
	hs.waitStatus("reconnecting 106", isReconnectingCode(ErrCodeNoInternet))
	hs.nextTimer()

	calls := hs.net.callCount()
	hs.e.doNetworkReconnect() // pushes reconnecting + a rebuilt status, wakes the hold
	eventually(t, "hold re-judged after the net event", func() bool { return hs.net.callCount() > calls })

	s, _ := hs.h.last()
	if !isReconnectingCode(ErrCodeNoInternet)(s) {
		t.Fatalf("last status after network reconnect = %+v, want reconnecting/106", s)
	}
	if gs := hs.e.GetStatus(); !isReconnectingCode(ErrCodeNoInternet)(gs) {
		t.Fatalf("GetStatus = %+v, want reconnecting/106", gs)
	}
}

func TestReplaceLoop_NetEventWakesHold(t *testing.T) {
	hs := newFoHarness(t, foOpts{
		fallbacks: map[string][]string{foURLA: {foURLB}},
		net:       []checkResult{checkNoResponse},
	})
	hs.escalate()
	hs.waitStatus("reconnecting 106", isReconnectingCode(ErrCodeNoInternet))
	tm := hs.nextTimer() // armed, never fired

	hs.net.set(checkMatched)
	hs.servers.setAlive("a.example", true)
	hs.e.doNetworkReconnect()

	hs.waitStatus("hold cleared by the net event before the 5s timer", func(s Status) bool {
		return s.State == StateConnected && s.Error == nil
	})
	select {
	case <-tm.ch:
		t.Fatal("test bug: the timer channel was drained")
	default:
	}
}

func TestReplaceLoop_HoldBackoffSchedule(t *testing.T) {
	hs := newFoHarness(t, foOpts{
		fallbacks: map[string][]string{foURLA: {foURLB}},
		net:       []checkResult{checkNoResponse},
	})
	hs.escalate()
	want := []time.Duration{5 * time.Second, 10 * time.Second, 30 * time.Second, 30 * time.Second, 30 * time.Second}
	for i, w := range want {
		tm := hs.nextTimer()
		if tm.d != w {
			t.Fatalf("hold timer #%d = %v, want %v", i, tm.d, w)
		}
		tm.fire()
	}
	if n := hs.provider.count(); n != 0 {
		t.Fatalf("provider.NextURL = %d, want 0", n)
	}
}

func TestReplaceLoop_HoldReasonFollowsVerdict(t *testing.T) {
	hs := newFoHarness(t, foOpts{
		fallbacks: map[string][]string{foURLA: {foURLB}},
		net:       []checkResult{checkNoResponse},
	})
	hs.escalate()
	hs.waitStatus("reconnecting 106", isReconnectingCode(ErrCodeNoInternet))
	hs.net.set(checkOtherResponse)
	hs.nextTimer().fire()
	hs.waitStatus("reconnecting 107", isReconnectingCode(ErrCodeCaptivePortal))
}

func TestReplaceLoop_StopDuringHoldReturnsPromptly(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())

	hs := newFoHarness(t, foOpts{
		fallbacks: map[string][]string{foURLA: {foURLB}},
		net:       []checkResult{checkNoResponse},
	})
	hs.escalate()
	hs.waitStatus("reconnecting 106", isReconnectingCode(ErrCodeNoInternet))
	hs.nextTimer()

	start := time.Now()
	if err := hs.e.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	select {
	case <-hs.done:
	case <-time.After(time.Second):
		t.Fatal("outboundReplaceLoop still running 1s after Stop")
	}
	if d := time.Since(start); d > time.Second {
		t.Fatalf("Stop + loop exit took %v, want < 1s", d)
	}
	s, _ := hs.h.last()
	if s.State != StateDisconnected || s.Error != nil {
		t.Fatalf("last status = %+v, want disconnected without error", s)
	}
	if hs.provider.count() != 0 {
		t.Fatalf("provider.NextURL = %d, want 0", hs.provider.count())
	}
}

func TestReplaceLoop_ShellProviderAlsoGated(t *testing.T) {
	shell := &shellOwner{owns: map[string]bool{foURLA: true}}
	hs := newFoHarness(t, foOpts{shell: shell, net: []checkResult{checkNoResponse}})
	hs.escalate()
	hs.waitStatus("reconnecting 106", isReconnectingCode(ErrCodeNoInternet))
	if n := shell.count(); n != 0 {
		t.Fatalf("shell NextURL = %d while offline, want 0", n)
	}
}

func TestReplaceLoop_UserActionErrorOutranksHold(t *testing.T) {
	hs := newFoHarness(t, foOpts{
		fallbacks: map[string][]string{foURLA: {foURLB}},
		net:       []checkResult{checkNoResponse},
	})
	hs.escalate()
	hs.waitStatus("reconnecting 106", isReconnectingCode(ErrCodeNoInternet))

	// Membership lapses mid-hold: the 402 must be what the user sees.
	hs.e.ReportWireError(fmt.Errorf("%w: %w", wire.ErrMetadataRejected,
		&wire.StreamError{Code: 402, Message: "membership expired"}))

	s, _ := hs.h.last()
	if s.Error == nil || s.Error.Code != ErrCodePaymentRequired {
		t.Fatalf("last status = %+v, want error 402 over the hold", s)
	}
	if gs := hs.e.GetStatus(); gs.Error == nil || gs.Error.Code != ErrCodePaymentRequired {
		t.Fatalf("GetStatus = %+v, want error 402 over the hold", gs)
	}
	// A further hold re-judgement must not push the hold reason over it.
	hs.net.set(checkOtherResponse)
	calls := hs.net.callCount()
	hs.nextTimer().fire()
	eventually(t, "hold re-judged", func() bool { return hs.net.callCount() > calls })
	if gs := hs.e.GetStatus(); gs.Error == nil || gs.Error.Code != ErrCodePaymentRequired {
		t.Fatalf("GetStatus after verdict change = %+v, want 402", gs)
	}
}

// setLastOKForTest records a passive-evidence timestamp (test-only method on
// the owning type).
func (d *trackingDialer) setLastOKForTest(at time.Time) {
	d.lastOK.Store(at.UnixNano())
}

// TestStart_WiresFailoverProviderAndNetcheck drives the real Start and checks
// the replace loop's inputs: a composite provider over ClientConfig.Fallbacks
// (static first, then the shell's URLOwner provider) and a netcheck that
// probes the configured targets through cfg.DirectDialer.
func TestStart_WiresFailoverProviderAndNetcheck(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	shell := &shellOwner{owns: map[string]bool{"k2v5://shell.example:443": true}}
	raw := &countingDialer{}
	e := New()
	if err := e.Start(Config{
		Client: &config.ClientConfig{
			Mode:      "proxy",
			Routes:    []config.RouteConfig{{Via: "direct", Match: rule.MatchConfig{All: true}}},
			Fallbacks: map[string][]string{foURLA: {foURLB}},
			Netcheck:  []config.NetcheckTarget{{URL: srv.URL + "/generate_204", Status: 204}},
		},
		FileDescriptor:   -1,
		Provider:         &capturingProvider{h: make(chan provider.ConnectionHandler, 1)},
		DirectDialer:     raw,
		OutboundProvider: shell,
	}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer e.Stop()

	e.mu.Lock()
	p := e.outboundProvider
	run := e.netcheck
	targets := e.netcheckTargets
	e.mu.Unlock()

	if u, err := p.NextURL(foURLA, nil); err != nil || u != foURLB {
		t.Fatalf("NextURL(a) = %q, %v; want fallback b", u, err)
	}
	if _, err := p.NextURL("k2v5://shell.example:443", nil); err == nil || shell.count() != 1 {
		t.Fatalf("shell-owned URL not delegated to the shell provider (calls=%d, err=%v)", shell.count(), err)
	}
	if _, err := p.NextURL("k2v5://nobody.example:443", nil); !errors.Is(err, ErrNoCandidates) {
		t.Fatalf("unowned URL: err = %v, want ErrNoCandidates", err)
	}
	if run == nil || len(targets) != 1 {
		t.Fatalf("netcheck not wired (run=%v, targets=%d)", run != nil, len(targets))
	}
	before := raw.n.Load()
	if rs, ds := run(context.Background()); len(rs) != 1 || rs[0] != checkMatched || len(ds) != 1 {
		t.Fatalf("netcheck = %v, want [matched]", rs)
	}
	if raw.n.Load() == before {
		t.Fatal("netcheck bypassed cfg.DirectDialer")
	}
}

// TestDirectResolve_IPv4First: netcheck dials resolved addresses in order in
// one 3s budget, so A records must precede AAAA even when AAAA answers first.
func TestDirectResolve_IPv4First(t *testing.T) {
	aaaaDone := make(chan struct{})
	query := func(_ context.Context, m *mdns.Msg) (*mdns.Msg, error) {
		resp := new(mdns.Msg)
		resp.SetReply(m)
		q := m.Question[0]
		switch q.Qtype {
		case mdns.TypeA:
			<-aaaaDone // answer A strictly after AAAA
			resp.Answer = []mdns.RR{
				&mdns.CNAME{Hdr: mdns.RR_Header{Name: q.Name, Rrtype: mdns.TypeCNAME, Class: mdns.ClassINET}, Target: "edge.example."},
				&mdns.A{Hdr: mdns.RR_Header{Name: "edge.example.", Rrtype: mdns.TypeA, Class: mdns.ClassINET}, A: net.ParseIP("203.0.113.7")},
			}
		case mdns.TypeAAAA:
			resp.Answer = []mdns.RR{
				&mdns.AAAA{Hdr: mdns.RR_Header{Name: q.Name, Rrtype: mdns.TypeAAAA, Class: mdns.ClassINET}, AAAA: net.ParseIP("2001:db8::7")},
			}
			close(aaaaDone)
		}
		return resp, nil
	}
	addrs, err := directResolve(query)(context.Background(), "check.example")
	if err != nil {
		t.Fatal(err)
	}
	want := []netip.Addr{netip.MustParseAddr("203.0.113.7"), netip.MustParseAddr("2001:db8::7")}
	if len(addrs) != 2 || addrs[0] != want[0] || addrs[1] != want[1] {
		t.Fatalf("addrs = %v, want %v (IPv4 first)", addrs, want)
	}

	// Both lookups failing surfaces an error; one failing does not.
	failA := func(_ context.Context, m *mdns.Msg) (*mdns.Msg, error) {
		if m.Question[0].Qtype == mdns.TypeA {
			return nil, errors.New("a: timeout")
		}
		resp := new(mdns.Msg)
		resp.SetReply(m)
		return resp, nil // empty AAAA
	}
	if _, err := directResolve(failA)(context.Background(), "check.example"); err == nil {
		t.Fatal("no addresses at all: want an error")
	}
}

// TestReplaceLoop_NetEventRestartsBackoff: a network change during a hold
// re-judges at once and restarts the backoff at 5s — a new network often
// needs a few seconds (DHCP, portal) before it answers.
func TestReplaceLoop_NetEventRestartsBackoff(t *testing.T) {
	hs := newFoHarness(t, foOpts{
		fallbacks: map[string][]string{foURLA: {foURLB}},
		net:       []checkResult{checkNoResponse},
	})
	hs.escalate()
	hs.nextTimer().fire()                           // 5s
	if d := hs.nextTimer().d; d != 10*time.Second { // armed, not fired
		t.Fatalf("second timer = %v, want 10s", d)
	}
	hs.e.doNetworkReconnect() // still offline after the event
	if d := hs.nextTimer().d; d != 5*time.Second {
		t.Fatalf("timer after a net event = %v, want 5s (backoff restarted)", d)
	}
}

// TestReplaceLoop_HoldTickRetriesCurrentNode: the netcheck targets never
// answer on this network (blocked / filtered) while the node is fine, so the
// verdict stays "no internet" — yet a hold tick's Phase-A on the current node
// must still bring the session back, no network event needed.
func TestReplaceLoop_HoldTickRetriesCurrentNode(t *testing.T) {
	hs := newFoHarness(t, foOpts{
		fallbacks: map[string][]string{foURLA: {foURLB}},
		net:       []checkResult{checkNoResponse},
	})

	hs.escalate()
	hs.waitStatus("reconnecting 106", isReconnectingCode(ErrCodeNoInternet))
	tm := hs.nextTimer()
	before := hs.servers.raceCount("a.example")

	// A failing tick keeps the hold.
	tm.fire()
	tm = hs.nextTimer()
	if n := hs.servers.raceCount("a.example"); n != before+1 {
		t.Fatalf("races on a after a hold tick = %d, want %d", n, before+1)
	}
	if s, _ := hs.h.last(); !isReconnectingCode(ErrCodeNoInternet)(s) {
		t.Fatalf("status after a failed tick re-race = %+v, want reconnecting/106", s)
	}

	hs.servers.setAlive("a.example", true)
	tm.fire()
	hs.waitStatus("connected via wait-tick Phase-A", func(s Status) bool {
		return s.State == StateConnected && s.Error == nil
	})
	if gs := hs.e.GetStatus(); gs.State != StateConnected || gs.Error != nil {
		t.Fatalf("GetStatus = %+v, want connected", gs)
	}
	if n := ReRaceFailStreak(hs.e, hs.target); n != 0 {
		t.Fatalf("reRaceFailStreak = %d, want 0", n)
	}
	if n := hs.provider.count(); n != 0 {
		t.Fatalf("provider.NextURL = %d, want 0", n)
	}
	if u := hs.targetURL(); u != foURLA {
		t.Fatalf("target = %s, want a", u)
	}
	// Back in the loop: the next echo failure is an ordinary Phase-A.
	hs.signal()
	eventually(t, "loop consumed a new signal", func() bool {
		return hs.servers.raceCount("a.example") == before+3
	})
}

// TestReplaceLoop_StopDuringHoldWithInflightPhaseA: Stop while a hold tick's
// Phase-A dial hangs must still return within 1s, leaking nothing.
func TestReplaceLoop_StopDuringHoldWithInflightPhaseA(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())

	hs := newFoHarness(t, foOpts{
		fallbacks: map[string][]string{foURLA: {foURLB}},
		net:       []checkResult{checkNoResponse},
	})
	hs.escalate()
	hs.waitStatus("reconnecting 106", isReconnectingCode(ErrCodeNoInternet))
	tm := hs.nextTimer()

	inflight := make(chan struct{})
	hs.servers.mu.Lock()
	hs.servers.onRace = func(ctx context.Context, _ string) {
		close(inflight)
		<-ctx.Done() // a dial that never answers on its own
	}
	hs.servers.mu.Unlock()
	tm.fire()
	select {
	case <-inflight:
	case <-time.After(5 * time.Second):
		t.Fatal("hold tick never started a Phase-A")
	}

	start := time.Now()
	if err := hs.e.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	select {
	case <-hs.done:
	case <-time.After(time.Second):
		t.Fatal("outboundReplaceLoop still running 1s after Stop with a Phase-A in flight")
	}
	if d := time.Since(start); d > time.Second {
		t.Fatalf("Stop + loop exit took %v, want < 1s", d)
	}
	if s, _ := hs.h.last(); s.State != StateDisconnected || s.Error != nil {
		t.Fatalf("last status = %+v, want disconnected without error", s)
	}
}

// sendSignal delivers one echo-failure signal for t.
func (hs *foHarness) sendSignal(t *outboundTransport) {
	hs.t.Helper()
	select {
	case hs.e.reRaceChan <- t:
	case <-time.After(5 * time.Second):
		hs.t.Fatal("reRaceChan send blocked: loop not consuming")
	}
}

// TestReplaceLoop_NoCandidatesDoesNotBlockOtherOutbounds: outbound A has no
// candidates and stays dead (on the retry schedule); outbound B's node dies
// and B has a fallback — B must fail over at once, not after A recovers.
func TestReplaceLoop_NoCandidatesDoesNotBlockOtherOutbounds(t *testing.T) {
	const urlD, urlE = "k2v5://d.example:443", "k2v5://e.example:443"
	hs := newFoHarness(t, foOpts{
		fallbacks: map[string][]string{urlD: {urlE}}, // only B (d) has a fallback
		net:       []checkResult{checkMatched},
	})
	hs.servers.setAlive("e.example", true)
	tmB := wire.NewTransportManager()
	targetB := &outboundTransport{
		tm:        tmB,
		wireCfg:   wire.Config{Host: "d.example", Port: 443, DialAddr: "d.example:443"},
		dialAddr:  "d.example:443",
		name:      "k2v5-d.example",
		url:       urlD,
		swappable: wire.NewSwappableDialer(tmB),
	}
	hs.e.mu.Lock()
	hs.e.outboundTMs = append(hs.e.outboundTMs, targetB)
	hs.e.mu.Unlock()

	hs.escalate() // A → no candidates → retry schedule
	hs.waitStatus("reconnecting 503 for A", isReconnectingCode(ErrCodeServerUnreachable))
	tmA := hs.nextTimer()
	if tmA.d != 5*time.Second {
		t.Fatalf("A's first retry timer = %v, want 5s", tmA.d)
	}

	for i := 0; i < reRaceFailThresholdForReplace; i++ {
		hs.sendSignal(targetB)
	}
	eventually(t, "B failed over to e while A is pending", func() bool {
		hs.e.mu.Lock()
		defer hs.e.mu.Unlock()
		return targetB.url == urlE
	})
	// A is still pending, so the hold stays.
	if gs := hs.e.GetStatus(); !isReconnectingCode(ErrCodeServerUnreachable)(gs) {
		t.Fatalf("GetStatus after B's failover = %+v, want reconnecting/503 (A still down)", gs)
	}

	// A recovers on its scheduled retry → hold clears.
	hs.servers.setAlive("a.example", true)
	tmA.fire()
	hs.waitStatus("connected once A answers", func(s Status) bool {
		return s.State == StateConnected && s.Error == nil
	})
}

// TestReplaceLoop_NoCandidatesEchoSignalIgnored (review C ruling): an echo
// signal for a pending outbound does NOT attempt it — only the retry timer
// and network events do, so the 5/10/30s backoff holds even when failing
// traffic keeps the probe signalling.
func TestReplaceLoop_NoCandidatesEchoSignalIgnored(t *testing.T) {
	hs := newFoHarness(t, foOpts{net: []checkResult{checkMatched}})
	hs.escalate()
	hs.waitStatus("reconnecting 503", isReconnectingCode(ErrCodeServerUnreachable))
	tm := hs.nextTimer() // armed, not fired yet

	hs.servers.setAlive("a.example", true)
	before := hs.servers.raceCount("a.example")
	netBefore := hs.net.callCount()
	for i := 0; i < 5; i++ {
		hs.signal()
	}
	time.Sleep(50 * time.Millisecond)
	if n := hs.servers.raceCount("a.example"); n != before {
		t.Fatalf("echo signals ran %d Phase-A on a pending target, want 0", n-before)
	}
	if n := hs.net.callCount(); n != netBefore {
		t.Fatalf("echo signals ran %d netchecks on a pending target, want 0", n-netBefore)
	}
	if gs := hs.e.GetStatus(); !isReconnectingCode(ErrCodeServerUnreachable)(gs) {
		t.Fatalf("status = %+v, want still reconnecting/503", gs)
	}
	tm.fire()
	hs.waitStatus("connected via the scheduled retry", func(s Status) bool {
		return s.State == StateConnected && s.Error == nil
	})
}

// TestReplaceLoop_NoCandidatesNetEventRetriesNow: a network event retries
// every pending outbound immediately.
func TestReplaceLoop_NoCandidatesNetEventRetriesNow(t *testing.T) {
	hs := newFoHarness(t, foOpts{net: []checkResult{checkMatched}})
	hs.escalate()
	hs.waitStatus("reconnecting 503", isReconnectingCode(ErrCodeServerUnreachable))
	hs.nextTimer()

	hs.servers.setAlive("a.example", true)
	hs.e.doNetworkReconnect()
	hs.waitStatus("connected via the net-event attempt", func(s Status) bool {
		return s.State == StateConnected && s.Error == nil
	})
}

// TestReplaceLoop_NetworkDropsMidFailover_StalePassiveStillHolds: a direct
// dial succeeded just before the outage (fresh passive evidence), then the
// network dies and the candidates fail fast — all within passiveFreshness.
// The pre-exhaust judgement must probe, not trust that dial: hold 106, never
// 503.
func TestReplaceLoop_NetworkDropsMidFailover_StalePassiveStillHolds(t *testing.T) {
	hs := newFoHarness(t, foOpts{
		fallbacks: map[string][]string{foURLA: {foURLB, foURLC}},
		net:       []checkResult{checkMatched},
	})
	hs.e.mu.Lock()
	dt := hs.e.directTrack
	hs.e.mu.Unlock()
	dt.setLastOKForTest(time.Now()) // e.g. a CN direct dial under smart routing
	hs.servers.mu.Lock()
	hs.servers.onSetup = func(host string) {
		if host == "b.example" {
			hs.net.set(checkNoResponse) // the user's network dies mid-failover
		}
	}
	hs.servers.mu.Unlock()

	hs.escalate()
	hs.waitStatus("reconnecting 106 after exhaustion", isReconnectingCode(ErrCodeNoInternet))
	hs.nextTimer()
	if st, _ := hs.engineState(); st != StateConnected {
		t.Fatalf("engine state = %s, want connected", st)
	}
	if hs.h.sawState(StateDisconnected) || hs.h.fatalCount() != 0 {
		t.Fatalf("disconnected=%v fatal=%d: a dial from before the outage vouched for a dead network",
			hs.h.sawState(StateDisconnected), hs.h.fatalCount())
	}
	if n := hs.net.callCount(); n != 1 {
		t.Fatalf("active netcheck ran %d times, want 1 (pre-phaseB passive, pre-exhaust active)", n)
	}
}

// noTargetsHarness is a harness whose session configured no netcheck targets
// (k2r gateway, CLI, an old webapp): netcheck runs over an empty list.
func noTargetsHarness(t *testing.T, fallbacks map[string][]string) *foHarness {
	t.Helper()
	hs := newFoHarness(t, foOpts{fallbacks: fallbacks})
	hs.e.mu.Lock()
	hs.e.netcheckTargets = nil
	hs.e.mu.Unlock()
	return hs
}

// TestReplaceLoop_NoNetcheckTargetsBehavesLikeBefore: with no netcheck
// targets and no passive evidence the verdict is unknown and must not gate:
// a dead primary fails over to a live fallback, never showing 106.
func TestReplaceLoop_NoNetcheckTargetsBehavesLikeBefore(t *testing.T) {
	hs := noTargetsHarness(t, map[string][]string{foURLA: {foURLB}})
	hs.servers.setAlive("b.example", true)

	hs.escalate()
	eventually(t, "target switched to b", func() bool { return hs.targetURL() == foURLB })
	if hs.h.sawCode(ErrCodeNoInternet) {
		t.Fatal("showed 106 with no netcheck targets: a blind verdict gated failover")
	}
	if st, le := hs.engineState(); st != StateConnected || le != nil {
		t.Fatalf("engine = %s / %v, want connected / nil", st, le)
	}
	select {
	case tm := <-hs.timers:
		t.Fatalf("a hold timer (%v) was armed: no-targets must not hold", tm.d)
	default:
	}
}

// TestReplaceLoop_NoNetcheckTargetsExhaustionAndRetryLikeBefore: the other
// two pre-branch behaviours with no targets — exhaustion fails 503, and a
// node without candidates goes on the non-blocking retry schedule showing
// 503 (not 106).
func TestReplaceLoop_NoNetcheckTargetsExhaustionAndRetryLikeBefore(t *testing.T) {
	t.Run("exhausted retries showing 503, fails 503 at the cap", func(t *testing.T) {
		hs := noTargetsHarness(t, map[string][]string{foURLA: {foURLB, foURLC}})
		hs.escalate()
		// R1: unknown verdict = no gating, so the cap clock runs as online.
		hs.waitStatus("reconnecting 503", isReconnectingCode(ErrCodeServerUnreachable))
		hs.startTicks().runUntilCapFails(0, 20*time.Minute)
		if st, le := hs.engineState(); st != StateDisconnected || le == nil || le.Code != ErrCodeServerUnreachable {
			t.Fatalf("engine = %s / %v, want disconnected / 503", st, le)
		}
		if hs.h.sawCode(ErrCodeNoInternet) {
			t.Fatal("showed 106 with no netcheck targets")
		}
	})
	t.Run("no candidates retries showing 503", func(t *testing.T) {
		hs := noTargetsHarness(t, nil)
		hs.escalate()
		hs.waitStatus("reconnecting 503", isReconnectingCode(ErrCodeServerUnreachable))
		hs.nextTimer().fire()
		if d := hs.nextTimer().d; d != 10*time.Second {
			t.Fatalf("second retry timer = %v, want 10s", d)
		}
		if s, _ := hs.h.last(); !isReconnectingCode(ErrCodeServerUnreachable)(s) {
			t.Fatalf("status after a failed retry = %+v, want reconnecting/503", s)
		}
		if hs.h.sawCode(ErrCodeNoInternet) {
			t.Fatal("showed 106 with no netcheck targets")
		}
	})
}

// TestReplaceLoop_AllFallbacksReject402FailsWith402: the primary is dead and
// every fallback rejects the device with 402 at setup (membership lapsed):
// that is the true cause, reported as-is rather than 503 (spec §4.2).
func TestReplaceLoop_AllFallbacksReject402FailsWith402(t *testing.T) {
	hs := newFoHarness(t, foOpts{
		fallbacks: map[string][]string{foURLA: {foURLB, foURLC}},
		net:       []checkResult{checkMatched},
	})
	rejected := fmt.Errorf("%w: %w", wire.ErrMetadataRejected,
		&wire.StreamError{Code: 402, Message: "membership expired"})
	hs.servers.mu.Lock()
	hs.servers.setupErrs["b.example"] = rejected
	hs.servers.setupErrs["c.example"] = rejected
	hs.servers.mu.Unlock()

	hs.escalate()
	select {
	case <-hs.done:
	case <-time.After(5 * time.Second):
		t.Fatal("loop did not exit after exhaustion")
	}
	st, le := hs.engineState()
	if st != StateDisconnected || le == nil || le.Code != ErrCodePaymentRequired {
		t.Fatalf("engine = %s / %v, want disconnected / 402", st, le)
	}
	if s, _ := hs.h.last(); s.State != StateDisconnected || s.Error == nil || s.Error.Code != ErrCodePaymentRequired {
		t.Fatalf("last status = %+v, want disconnected/402", s)
	}
	hs.servers.mu.Lock()
	b, c := hs.servers.setups["b.example"], hs.servers.setups["c.example"]
	hs.servers.mu.Unlock()
	if b != 1 || c != 1 {
		t.Fatalf("setups b=%d c=%d, want 1 each (one 402 must not stop the round)", b, c)
	}
}

// TestRetryOnce_NotConnectedEndsLoop: a retry due after the engine left
// connected (Stop/fail window before ctx is cancelled) must end the loop, not
// leave the target due — that re-arms a 0s timer and busy-spins.
func TestRetryOnce_NotConnectedEndsLoop(t *testing.T) {
	e := New()
	e.mu.Lock()
	e.state = StateDisconnected
	e.mu.Unlock()
	target := &outboundTransport{name: "a"}
	r := newRetrySet(nil, nil)
	r.schedule(target, 0)
	if e.retryOnce(context.Background(), r, target, "wait-tick", "wait-tick") {
		t.Fatal("retryOnce = true with state disconnected, want false (loop must exit)")
	}
}
