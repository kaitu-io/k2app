//go:build linux

package gateway

import (
	"errors"
	"testing"
	"time"

	"github.com/kaitu-io/k2/engine"
)

// gatewayWithTunnel returns a gateway that looks connected through eng.
func gatewayWithTunnel(eng *engine.Engine) (*Gateway, *recordingInterceptor) {
	g := New("test", "test", "amd64", "")
	rules := &recordingInterceptor{}
	g.eng = eng
	g.interceptor = rules
	return g, rules
}

// A plain k2v5:// route has no subscription behind it. Handing the engine the
// manager anyway makes it ask for a replacement that cannot exist, and the
// refusal is fatal; with no provider it keeps re-racing the same server.
func TestOutboundProviderFor(t *testing.T) {
	mgr := New("test", "test", "amd64", "").subsMgr
	if mgr == nil {
		t.Fatal("New() built no subscription manager")
	}

	if got := outboundProviderFor(mgr, 0, false); got != nil {
		t.Errorf("no subscription session: provider = %v, want nil", got)
	}
	if got := outboundProviderFor(mgr, 1, true); got != nil {
		t.Errorf("multi-slot: provider = %v, want nil", got)
	}
	if got := outboundProviderFor(mgr, 1, false); got != engine.OutboundProvider(mgr) {
		t.Errorf("one subscription session: provider = %v, want the manager", got)
	}
}

// ── the engine gave up on its outbound ──────────────────────────────
//
// Measured on OpenWrt 25.12 (2026-09-30): the node was unreachable for 15 s
// on an idle router. The engine ran out of servers to try, shut itself down
// and never came back; LAN clients could not resolve a name until someone ran
// `k2r up` by hand.

func TestAutoReconnectUp_AfterFatal_ReplacesTheDeadEngine(t *testing.T) {
	setStateDirForTest(t, t.TempDir())
	saveConnectedSession(t)
	dead := engine.New()
	g, rules := gatewayWithTunnel(dead)

	err := g.autoReconnectUp(dead)()

	if !ranDoUp(err) {
		t.Fatalf("err = %v, want the doUp failure", err)
	}
	if got := rules.removes.Load(); got != 1 {
		t.Errorf("the dead tunnel's rules were removed %d times, want 1", got)
	}
	if g.eng != nil {
		t.Error("the dead engine is still the gateway's engine")
	}
}

// The first attempt takes the dead engine away; the second must not read its
// absence as "someone else took over".
func TestAutoReconnectUp_AfterFatal_KeepsRetrying(t *testing.T) {
	setStateDirForTest(t, t.TempDir())
	saveConnectedSession(t)
	dead := engine.New()
	g, _ := gatewayWithTunnel(dead)
	attempt := g.autoReconnectUp(dead)

	for n := 1; n <= 3; n++ {
		if err := attempt(); !ranDoUp(err) {
			t.Fatalf("attempt %d: err = %v, want the doUp failure", n, err)
		}
	}
}

// Engine.Stop does not join the replace loop, so a fatal can arrive from an
// engine the gateway has already replaced. The live tunnel must survive it.
func TestAutoReconnectUp_AfterFatal_LateEventLeavesLiveTunnel(t *testing.T) {
	setStateDirForTest(t, t.TempDir())
	saveConnectedSession(t)
	live := engine.New()
	g, rules := gatewayWithTunnel(live)

	err := g.autoReconnectUp(engine.New())()

	if !errors.Is(err, errAutoReconnectSuperseded) {
		t.Fatalf("err = %v, want errAutoReconnectSuperseded", err)
	}
	if got := rules.removes.Load(); got != 0 {
		t.Errorf("the live tunnel was torn down (%d Remove calls)", got)
	}
	if g.eng != live {
		t.Error("the live engine was replaced")
	}
}

// `k2r down` between the fatal and the first attempt: nothing to bring back.
func TestAutoReconnectUp_AfterFatal_UserDisconnected(t *testing.T) {
	setStateDirForTest(t, t.TempDir())
	saveState(&persistedState{State: "disconnected", Timestamp: time.Now()})
	g := New("test", "test", "amd64", "") // doDown left no engine behind

	err := g.autoReconnectUp(engine.New())()

	if !errors.Is(err, errAutoReconnectSuperseded) {
		t.Fatalf("err = %v, want errAutoReconnectSuperseded", err)
	}
}

// A user's up that failed between the fatal and the first attempt took the
// dead engine away and put nothing in its place. That is not "somebody else
// took over": the loop must still reconnect.
func TestAutoReconnectUp_AfterFatal_TunnelGoneButStillWanted(t *testing.T) {
	setStateDirForTest(t, t.TempDir())
	saveConnectedSession(t)
	g := New("test", "test", "amd64", "")

	if err := g.autoReconnectUp(engine.New())(); !ranDoUp(err) {
		t.Fatalf("err = %v, want the doUp failure", err)
	}
}

func TestAutoReconnectUp_AfterFatal_StandsDownOnceAnotherUpSucceeded(t *testing.T) {
	setStateDirForTest(t, t.TempDir())
	saveConnectedSession(t)
	dead := engine.New()
	g, rules := gatewayWithTunnel(dead)
	attempt := g.autoReconnectUp(dead)
	g.ups.Add(1)

	err := attempt()

	if !errors.Is(err, errAutoReconnectSuperseded) {
		t.Fatalf("err = %v, want errAutoReconnectSuperseded", err)
	}
	if got := rules.removes.Load(); got != 0 {
		t.Errorf("rules removed %d times, want 0", got)
	}
}

// Two loops must never alternate tearing each other's tunnel down: the boot
// loop may still be retrying when a tunnel the user brought up dies.
func TestAutoReconnectUp_NewerLoopSupersedesOlder(t *testing.T) {
	setStateDirForTest(t, t.TempDir())
	saveConnectedSession(t)
	g, rules := gatewayWithTunnel(engine.New())
	older := g.autoReconnectUp(nil)
	newer := g.autoReconnectUp(nil)

	if err := older(); !errors.Is(err, errAutoReconnectSuperseded) {
		t.Fatalf("older loop: err = %v, want errAutoReconnectSuperseded", err)
	}
	if got := rules.removes.Load(); got != 0 {
		t.Fatalf("the older loop tore the tunnel down (%d Remove calls)", got)
	}
	if err := newer(); !ranDoUp(err) {
		t.Fatalf("newer loop: err = %v, want the doUp failure", err)
	}
}

// The event itself must start the loop — nothing else will.
func TestEngineEvents_OutboundFatalReconnectsFromSavedSession(t *testing.T) {
	setStateDirForTest(t, t.TempDir())
	saveConnectedSession(t)
	origBackoff := autoReconnectBackoff
	autoReconnectBackoff = []time.Duration{time.Millisecond}
	t.Cleanup(func() { autoReconnectBackoff = origBackoff })

	dead := engine.New()
	g, rules := gatewayWithTunnel(dead)
	// The loop runs on a goroutine the test cannot join; see TestRun_StopShutsDown.
	defer func() {
		g.Stop()
		g.opMu.Lock()
		g.opMu.Unlock()
	}()

	engineEvents{g: g, eng: dead}.OnOutboundFatal("k2v5://node.example", errors.New("all subscribed tunnels failed"))

	for deadline := time.Now().Add(5 * time.Second); rules.removes.Load() == 0; time.Sleep(time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("no reconnect attempt after the outbound fatal")
		}
	}
}

// The loop the event starts is bound to the engine the event came from.
func TestReconnectAfterFatal_LateEventLeavesLiveTunnel(t *testing.T) {
	setStateDirForTest(t, t.TempDir())
	saveConnectedSession(t)
	origBackoff := autoReconnectBackoff
	autoReconnectBackoff = []time.Duration{time.Millisecond}
	t.Cleanup(func() { autoReconnectBackoff = origBackoff })

	live := engine.New()
	g, rules := gatewayWithTunnel(live)
	defer func() { // a loop that did not stand down would retry forever
		g.Stop()
		g.opMu.Lock()
		g.opMu.Unlock()
	}()

	done := make(chan struct{})
	go func() {
		g.reconnectAfterFatal(engine.New())
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Error("the loop did not stand down")
	}
	if got := rules.removes.Load(); got != 0 {
		t.Errorf("the live tunnel was torn down (%d Remove calls)", got)
	}
}

// `k2r down` is the only thing that ends the wish to reconnect: after it the
// saved state has no session, and a fatal from the old engine starts nothing.
func TestReconnectAfterFatal_NoSavedSessionDoesNothing(t *testing.T) {
	setStateDirForTest(t, t.TempDir())
	saveState(&persistedState{State: "disconnected", Timestamp: time.Now()})
	origBackoff := autoReconnectBackoff
	autoReconnectBackoff = []time.Duration{time.Millisecond}
	t.Cleanup(func() { autoReconnectBackoff = origBackoff })

	dead := engine.New()
	g, rules := gatewayWithTunnel(dead)

	g.reconnectAfterFatal(dead) // returns at once: nothing saved

	if got := rules.removes.Load(); got != 0 {
		t.Errorf("rules removed %d times, want 0", got)
	}
}
