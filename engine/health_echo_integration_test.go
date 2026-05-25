package engine

import (
	"context"
	"errors"
	"net"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kaitu-io/k2/core"
	"github.com/kaitu-io/k2/wire"
)

// Integration tests for echoProbeLoop / echoLoop.
//
// Unit tests in health_test.go cover the pure echoStep state machine. These
// tests are *attack-style*: they spin up the real goroutines, inject failure
// modes, and stress the cap-1 reRaceChan, the per-transport state, shutdown
// races, panic recovery, and goroutine leaks. Every test runs cleanly under
// `go test -race` and `-tags=deadlock_disable` is NOT set, so go-deadlock is
// active across the suite.

// ---------- mock transports ---------------------------------------------

// stubDialer satisfies wire.Dialer with errors on every dial. Echo loop never
// touches DialTCP/UDP — only Primary() and PrimaryType() / RunEchoProbe.
type stubDialer struct{}

func (*stubDialer) DialTCP(ctx context.Context, addr string, ed []byte) (net.Conn, error) {
	return nil, errors.New("stub: no dial")
}
func (*stubDialer) DialUDP(ctx context.Context, addr string) (net.PacketConn, error) {
	return nil, errors.New("stub: no dial")
}
func (*stubDialer) DialStreamUDP(ctx context.Context, addr string) (net.PacketConn, error) {
	return nil, errors.New("stub: no dial")
}
func (*stubDialer) Reset()       {}
func (*stubDialer) Close() error { return nil }

// echoStubDialer additionally implements wire.EchoProber. The probe function
// is goroutine-safe — callers can swap fn under mu while probes are running.
type echoStubDialer struct {
	stubDialer
	mu    sync.Mutex
	fn    func(ctx context.Context) error
	calls atomic.Int64
}

func (e *echoStubDialer) RunEchoProbe(ctx context.Context) error {
	e.calls.Add(1)
	e.mu.Lock()
	fn := e.fn
	e.mu.Unlock()
	if fn == nil {
		return nil
	}
	return fn(ctx)
}

func (e *echoStubDialer) setFn(fn func(ctx context.Context) error) {
	e.mu.Lock()
	e.fn = fn
	e.mu.Unlock()
}

// newEchoTransport builds a TransportManager wired with an echoStubDialer
// installed as primary (type "quic"). Returns the ot for echoProbeLoop and
// the stub for behavior injection.
func newEchoTransport(name string, fn func(ctx context.Context) error) (*outboundTransport, *echoStubDialer) {
	stub := &echoStubDialer{fn: fn}
	tm := wire.NewTransportManager()
	tm.SetPrimary(stub, "quic")
	return &outboundTransport{tm: tm, name: name}, stub
}

// startEchoLoopOnly wires hm.wg/done so we can run JUST echoLoop without the
// 1Hz sample goroutine. Caller MUST call hm.stop(). Sample loop is irrelevant
// for echo machinery and would introduce a 30s heartbeat side effect.
func startEchoLoopOnly(hm *healthMonitor) {
	hm.wg.Add(1)
	go hm.echoLoop()
}

// drainReraceChan starts a goroutine that counts reRace signals until stop is
// closed. Returns the atomic counter to assert against.
func drainReraceChan(reRaceChan <-chan *outboundTransport, stop <-chan struct{}) *atomic.Int64 {
	var n atomic.Int64
	go func() {
		for {
			select {
			case <-stop:
				return
			case _, ok := <-reRaceChan:
				if !ok {
					return
				}
				n.Add(1)
			}
		}
	}()
	return &n
}

// newHMForEcho constructs a healthMonitor wired with the given transports and
// a buffered (cap=1, matching production) reRaceChan ready to receive signals.
func newHMForEcho(t *testing.T, tms []*outboundTransport) (*healthMonitor, chan *outboundTransport) {
	t.Helper()
	tunnel := core.NewClientTunnel(core.ClientTunnelConfig{})
	hm := newHealthMonitor(tms[0], tms, tunnel, nil, nil, time.Now())
	ch := make(chan *outboundTransport, 1)
	hm.reRaceChan = ch
	return hm, ch
}

// ---------- T1: real failure fires rerace within bounded time -----------

func TestEchoProbeLoop_ReraceFiresOnConsecutiveFails(t *testing.T) {
	// 3 consecutive context.DeadlineExceeded ⇒ rerace. With echoIntervals[0]=1s
	// and trustIdx collapsing to 0 on each fail, three fails fit in ~3-5s.
	ot, _ := newEchoTransport("t-fail", func(ctx context.Context) error {
		return context.DeadlineExceeded
	})
	hm, ch := newHMForEcho(t, []*outboundTransport{ot})

	startEchoLoopOnly(hm)
	t.Cleanup(hm.stop)

	select {
	case got := <-ch:
		if got != ot {
			t.Fatalf("rerace signal targeted wrong outbound: got %p want %p", got, ot)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("no rerace signal after 8s of consecutive failures — expected within ~5s")
	}
}

// ---------- T2: pre-echo server NEVER triggers rerace -------------------

func TestEchoProbeLoop_PreEchoServerProducesZeroReraces(t *testing.T) {
	// A pre-echo server returns "stream reset" / EOF — non-DeadlineExceeded.
	// IsEchoUnsupported returns true ⇒ trust grows, strikes stay 0.
	// Over 5s we should see zero rerace signals; 1s probe storm scenario.
	ot, _ := newEchoTransport("t-old-server", func(ctx context.Context) error {
		return errors.New("stream reset by old server")
	})
	hm, ch := newHMForEcho(t, []*outboundTransport{ot})

	stop := make(chan struct{})
	n := drainReraceChan(ch, stop)

	startEchoLoopOnly(hm)
	defer close(stop)
	defer hm.stop()

	time.Sleep(5 * time.Second)

	if got := n.Load(); got != 0 {
		t.Fatalf("pre-echo server produced %d rerace signals (must be 0 — would storm 18×/min)", got)
	}
}

// ---------- T3: 50 transports failing in parallel — no deadlock ---------

func TestEchoProbeLoop_ConcurrentReraceStormStaysBounded(t *testing.T) {
	// Stress: 50 transports all failing simultaneously, cap=1 channel,
	// slow consumer. Verifies: (a) no deadlock between probe goroutines and
	// state lock, (b) cap=1 + select-default throttles the storm correctly,
	// (c) all goroutines exit cleanly on hm.stop().
	const N = 50

	tms := make([]*outboundTransport, N)
	for i := range N {
		ot, _ := newEchoTransport("t-stress", func(ctx context.Context) error {
			return context.DeadlineExceeded
		})
		tms[i] = ot
	}
	hm, ch := newHMForEcho(t, tms)

	// SLOW consumer: ~50ms per drain. Cap=1 means the producer must drop
	// most signals via select-default. If any goroutine blocks on send,
	// the whole storm wedges → hm.stop will time out below.
	stop := make(chan struct{})
	var consumed atomic.Int64
	go func() {
		for {
			select {
			case <-stop:
				return
			case <-ch:
				consumed.Add(1)
				time.Sleep(50 * time.Millisecond)
			}
		}
	}()

	startEchoLoopOnly(hm)

	// Let the storm rage for ~6s — long enough for several rerace rounds.
	time.Sleep(6 * time.Second)

	stopDone := make(chan struct{})
	go func() {
		hm.stop()
		close(stopDone)
	}()
	select {
	case <-stopDone:
		// success
	case <-time.After(10 * time.Second):
		t.Fatal("hm.stop() did not return within 10s — likely deadlock/goroutine wedged")
	}
	close(stop)

	// Sanity: at least one signal made it through the SLOW consumer.
	// We don't assert an upper bound — throttling is the *guarantee*, not
	// a specific count.
	if consumed.Load() == 0 {
		t.Fatal("no rerace signals consumed across 50 failing transports over 6s — channel wired wrong?")
	}
}

// ---------- T4: shutdown returns promptly even mid-probe ----------------

func TestEchoProbeLoop_ShutdownReturnsPromptlyMidProbe(t *testing.T) {
	// RunEchoProbe blocks until either (a) ctx (probe timeout=2s) fires, or
	// (b) the test signals it. hm.stop() must return within probeTimeout+grace
	// even when N probes are in flight. Validates that close(hm.done) is the
	// authoritative shutdown signal and the probe ctx is properly chained.
	const N = 10

	probeEntered := make(chan struct{}, N) // signals each probe entry
	tms := make([]*outboundTransport, N)
	for i := range N {
		ot, _ := newEchoTransport("t-blocking", func(ctx context.Context) error {
			select {
			case probeEntered <- struct{}{}:
			default:
			}
			// Block until probe ctx expires (2s) — simulates a stuck server.
			<-ctx.Done()
			return ctx.Err()
		})
		tms[i] = ot
	}
	hm, ch := newHMForEcho(t, tms)

	stop := make(chan struct{})
	drainReraceChan(ch, stop)
	defer close(stop)

	startEchoLoopOnly(hm)

	// Wait until at least one probe is actively blocking.
	select {
	case <-probeEntered:
	case <-time.After(3 * time.Second):
		t.Fatal("no probe entered within 3s — echo loop not running")
	}

	stopDone := make(chan struct{})
	t0 := time.Now()
	go func() {
		hm.stop()
		close(stopDone)
	}()

	// Worst case: probeTimeout (2s) for in-flight probe + small grace.
	// Anything beyond ~3s means the loop is wedged.
	select {
	case <-stopDone:
		if elapsed := time.Since(t0); elapsed > 3*time.Second {
			t.Fatalf("hm.stop() returned but took %v (>3s budget)", elapsed)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("hm.stop() did not return within 5s during in-flight probes")
	}
}

// ---------- T5: panic in one probe doesn't kill others ------------------

func TestEchoProbeLoop_PanicInOneProbeOthersContinue(t *testing.T) {
	// safego.Go recovers panics. The panicking goroutine exits (that
	// transport stops being probed — known cost), but other transports
	// must keep probing. Asserts that the wg accounting is correct so
	// hm.stop() still returns.
	const N = 3

	tms := make([]*outboundTransport, N)
	stubs := make([]*echoStubDialer, N)
	for i := range N {
		ot, stub := newEchoTransport("t-mixed", nil)
		tms[i] = ot
		stubs[i] = stub
	}
	// Healthy transports return ok; transport[1] panics every call.
	stubs[0].setFn(func(ctx context.Context) error { return nil })
	stubs[1].setFn(func(ctx context.Context) error { panic("boom") })
	stubs[2].setFn(func(ctx context.Context) error { return nil })

	hm, ch := newHMForEcho(t, tms)
	stop := make(chan struct{})
	drainReraceChan(ch, stop)
	defer close(stop)

	startEchoLoopOnly(hm)
	defer hm.stop()

	// Healthy transports start at 1s interval, grow exponentially.
	// After ~5s expect each healthy transport to have probed ≥ 2 times.
	time.Sleep(5 * time.Second)

	if c := stubs[0].calls.Load(); c < 2 {
		t.Fatalf("healthy transport[0] only probed %d times — panic in [1] may have wedged the loop", c)
	}
	if c := stubs[2].calls.Load(); c < 2 {
		t.Fatalf("healthy transport[2] only probed %d times — panic in [1] may have wedged the loop", c)
	}
	// Panicking transport: at least one call before it panicked out.
	if c := stubs[1].calls.Load(); c < 1 {
		t.Fatalf("panicking transport never even ran (%d calls)", c)
	}
}

// ---------- T6: no goroutine leak after stop ----------------------------

func TestEchoProbeLoop_NoGoroutineLeakAfterStop(t *testing.T) {
	// Start/stop 5 transports; the goroutine count must return to baseline
	// within a bounded grace window. Tolerates runtime jitter (+2).
	const N = 5

	// Quiesce any background test goroutines.
	runtime.GC()
	time.Sleep(50 * time.Millisecond)
	baseline := runtime.NumGoroutine()

	tms := make([]*outboundTransport, N)
	for i := range N {
		ot, _ := newEchoTransport("t-leak", func(ctx context.Context) error { return nil })
		tms[i] = ot
	}
	hm, ch := newHMForEcho(t, tms)
	stop := make(chan struct{})
	drainReraceChan(ch, stop)

	startEchoLoopOnly(hm)
	time.Sleep(1500 * time.Millisecond) // at least one probe round
	hm.stop()
	close(stop)

	// Poll for return-to-baseline. Allow up to 2s + small jitter tolerance.
	deadline := time.Now().Add(2 * time.Second)
	var final int
	for time.Now().Before(deadline) {
		runtime.GC()
		final = runtime.NumGoroutine()
		if final <= baseline+2 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("goroutine leak: baseline=%d final=%d (delta=%d, allowance=2)",
		baseline, final, final-baseline)
}

// ---------- T7: flapping ok/fail never triggers false rerace ------------

func TestEchoProbeLoop_FlappingResetsStrikesPreventsFalseRerace(t *testing.T) {
	// Alternating ok/fail: each ok resets strikes to 0, so strikes can never
	// reach the rerace threshold (3). Validates that the state machine in the
	// real goroutine (not just the pure echoStep function) behaves correctly.
	var flip atomic.Bool
	ot, _ := newEchoTransport("t-flap", func(ctx context.Context) error {
		if flip.Swap(!flip.Load()) {
			return context.DeadlineExceeded
		}
		return nil
	})
	hm, ch := newHMForEcho(t, []*outboundTransport{ot})

	stop := make(chan struct{})
	n := drainReraceChan(ch, stop)

	startEchoLoopOnly(hm)
	defer close(stop)
	defer hm.stop()

	// Run long enough for many flap cycles. 1s base interval ⇒ ~7-8 probes.
	time.Sleep(8 * time.Second)

	if got := n.Load(); got != 0 {
		t.Fatalf("flapping ok/fail produced %d rerace signals (must be 0 — ok resets strikes)", got)
	}
}

// ---------- T8: nil primary doesn't crash, resets state -----------------

func TestEchoProbeLoop_NilPrimaryResetsStateNoCrash(t *testing.T) {
	// When TransportManager.Primary() returns nil (e.g. mid-replacement),
	// the loop must reset trust/strikes and re-arm the timer at base
	// interval — NOT panic.
	tm := wire.NewTransportManager()
	// Intentionally NO SetPrimary call ⇒ Primary() returns nil.
	ot := &outboundTransport{tm: tm, name: "t-nil"}
	hm, ch := newHMForEcho(t, []*outboundTransport{ot})

	stop := make(chan struct{})
	n := drainReraceChan(ch, stop)

	startEchoLoopOnly(hm)
	defer close(stop)
	defer hm.stop()

	// Run for 4s. With nil primary every loop iteration short-circuits.
	// We don't get probes, we don't get reraces, and we don't crash.
	time.Sleep(4 * time.Second)

	if got := n.Load(); got != 0 {
		t.Fatalf("nil primary produced %d reraces — must produce 0", got)
	}
	// hm.stop() in defer is the real test: if the loop panicked, wg.Wait
	// would hang indefinitely → test timeout.
}

// ---------- T9: non-EchoProber primary paces at ceiling -----------------

func TestEchoProbeLoop_NonEchoProberPacesAtCeiling(t *testing.T) {
	// Primary is a plain wire.Dialer (no EchoProber) — represents TCP-WS
	// or a future transport. Loop must NOT crash, must NOT spin at 1s,
	// must reset to ceiling pacing. Validates the early-return branch.
	tm := wire.NewTransportManager()
	tm.SetPrimary(&stubDialer{}, "tcpws") // stubDialer has NO RunEchoProbe
	ot := &outboundTransport{tm: tm, name: "t-tcpws"}
	hm, ch := newHMForEcho(t, []*outboundTransport{ot})

	stop := make(chan struct{})
	n := drainReraceChan(ch, stop)

	startEchoLoopOnly(hm)
	defer close(stop)
	defer hm.stop()

	time.Sleep(4 * time.Second)

	// No EchoProber ⇒ no real probes, no failures, no reraces.
	if got := n.Load(); got != 0 {
		t.Fatalf("non-EchoProber primary produced %d reraces (must be 0)", got)
	}
}

// ---------- T10: multi-transport mixed-mode end-to-end ------------------

func TestEchoProbeLoop_MixedHealthAcrossTransportsScopedCorrectly(t *testing.T) {
	// 3 transports, distinct health: one healthy, one pre-echo, one dying.
	// Validates that per-transport state is isolated (no shared map) and
	// each fires (or doesn't) its own rerace independently.
	healthyOT, _ := newEchoTransport("t-healthy", func(ctx context.Context) error { return nil })
	preEchoOT, _ := newEchoTransport("t-pre-echo", func(ctx context.Context) error {
		return errors.New("stream reset")
	})
	dyingOT, _ := newEchoTransport("t-dying", func(ctx context.Context) error {
		return context.DeadlineExceeded
	})

	hm, ch := newHMForEcho(t, []*outboundTransport{healthyOT, preEchoOT, dyingOT})

	// Identity tracker: which outbounds fired reraces?
	var mu sync.Mutex
	fired := map[*outboundTransport]int{}
	stop := make(chan struct{})
	go func() {
		for {
			select {
			case <-stop:
				return
			case ot := <-ch:
				mu.Lock()
				fired[ot]++
				mu.Unlock()
			}
		}
	}()

	startEchoLoopOnly(hm)
	defer close(stop)
	defer hm.stop()

	// 6s ⇒ dying transport accumulates 3+ strikes; others must not.
	time.Sleep(6 * time.Second)

	mu.Lock()
	defer mu.Unlock()
	if fired[healthyOT] != 0 {
		t.Fatalf("healthy transport wrongly fired %d reraces", fired[healthyOT])
	}
	if fired[preEchoOT] != 0 {
		t.Fatalf("pre-echo transport wrongly fired %d reraces (backward-compat broken)", fired[preEchoOT])
	}
	if fired[dyingOT] < 1 {
		t.Fatalf("dying transport never fired rerace (expected ≥1 within 6s)")
	}
}
