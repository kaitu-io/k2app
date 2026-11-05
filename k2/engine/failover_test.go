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

func (n *foNet) run(context.Context) []checkResult {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.calls++
	return append([]checkResult(nil), n.rs...)
}

func (n *foNet) callCount() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.calls
}

// foTimer is one hold backoff timer the loop asked for.
type foTimer struct {
	d  time.Duration
	ch chan time.Time
}

func (t foTimer) fire() { t.ch <- time.Time{} }

// foServers decides which hosts answer Phase-A races and Phase-B setups.
type foServers struct {
	mu        sync.Mutex
	alive     map[string]bool
	races     map[string]int
	setups    map[string]int
	onSetup   func(host string) // called (outside mu) on each setup, before the verdict
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
	inner OutboundProvider
	mu    sync.Mutex
	calls int
}

func (p *countingProvider) NextURL(cur string, reason error) (string, error) {
	p.mu.Lock()
	p.calls++
	p.mu.Unlock()
	return p.inner.NextURL(cur, reason)
}

func (p *countingProvider) Reset() {
	if r, ok := p.inner.(interface{ Reset() }); ok {
		r.Reset()
	}
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
	cancel   context.CancelFunc
	done     chan struct{}
}

type foOpts struct {
	fallbacks map[string][]string
	shell     OutboundProvider
	net       []checkResult
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
		setupDone: make(chan string, 16),
	}

	origRace := raceTransportFn
	raceTransportFn = func(_ context.Context, cfg wire.Config, _ string, _ wire.DirectDialer, _ *wire.CookieJar) (*wire.RaceResult, error) {
		servers.mu.Lock()
		servers.races[cfg.Host]++
		alive := servers.alive[cfg.Host]
		servers.mu.Unlock()
		if !alive {
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

	tm := wire.NewTransportManager()
	target := &outboundTransport{
		tm:        tm,
		wireCfg:   wire.Config{Host: hostOf(foURLA), Port: 443, DialAddr: hostOf(foURLA) + ":443"},
		dialAddr:  hostOf(foURLA) + ":443",
		name:      "k2v5-" + hostOf(foURLA),
		url:       foURLA,
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
	e.holdTimerFn = func(d time.Duration) (<-chan time.Time, func()) {
		ch := make(chan time.Time, 1)
		timers <- foTimer{d: d, ch: ch}
		return ch, func() {}
	}
	ctx, cancel := context.WithCancel(context.Background())
	e.reRaceCancel = cancel
	e.mu.Unlock()

	hs := &foHarness{
		t: t, e: e, target: target, h: h, net: fnet, servers: servers,
		provider: provider, static: static, timers: timers,
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

func TestReplaceLoop_AllDeadOnlineFails503(t *testing.T) {
	hs := newFoHarness(t, foOpts{
		fallbacks: map[string][]string{foURLA: {foURLB, foURLC}},
		net:       []checkResult{checkMatched},
	})

	hs.escalate()
	select {
	case <-hs.done:
	case <-time.After(5 * time.Second):
		t.Fatal("loop did not exit after exhaustion on a healthy network")
	}
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

	hs.servers.setAlive("a.example", true)
	hs.signal()
	hs.waitStatus("connected after Phase-A success", func(s Status) bool {
		return s.State == StateConnected && s.Error == nil
	})
	if hs.h.sawState(StateDisconnected) {
		t.Fatal("went disconnected")
	}
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
	if rs := run(context.Background()); len(rs) != 1 || rs[0] != checkMatched {
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
