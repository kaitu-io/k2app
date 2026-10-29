//go:build linux

package gateway

import (
	"errors"
	"net"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kaitu-io/k2/config"
)

// recordingInterceptor counts Install / Remove. When block is non-nil, Install
// signals started and then waits for block to be closed.
type recordingInterceptor struct {
	installs atomic.Int32
	removes  atomic.Int32
	started  chan struct{}
	block    chan struct{}
}

func (r *recordingInterceptor) Install(InterceptConfig) error {
	if r.block != nil {
		close(r.started)
		<-r.block
	}
	r.installs.Add(1)
	return nil
}
func (r *recordingInterceptor) Remove()      { r.removes.Add(1) }
func (r *recordingInterceptor) Name() string { return "recording" }

// stubRemoveInterception replaces the firewall cleanup shutdown performs and
// returns the counter of how often it ran.
func stubRemoveInterception(t *testing.T) *atomic.Int32 {
	t.Helper()
	var calls atomic.Int32
	orig := removeInterception
	removeInterception = func() { calls.Add(1) }
	t.Cleanup(func() { removeInterception = orig })
	return &calls
}

func setShutdownGrace(t *testing.T, d time.Duration) {
	t.Helper()
	orig := shutdownGrace
	shutdownGrace = d
	t.Cleanup(func() { shutdownGrace = orig })
}

// ── install gate ────────────────────────────────────────────────────

func TestInstallGate_InstallsWhileOpen(t *testing.T) {
	var gate installGate
	i := &recordingInterceptor{}
	if err := gate.install(i, InterceptConfig{}); err != nil {
		t.Fatalf("install through an open gate: %v", err)
	}
	if got := i.installs.Load(); got != 1 {
		t.Errorf("installs = %d, want 1", got)
	}
}

func TestInstallGate_RefusesAfterClose(t *testing.T) {
	var gate installGate
	gate.close()
	i := &recordingInterceptor{}
	err := gate.install(i, InterceptConfig{})
	if !errors.Is(err, errGatewayStopping) {
		t.Errorf("err = %v, want errGatewayStopping", err)
	}
	if got := i.installs.Load(); got != 0 {
		t.Errorf("Install ran %d times through a closed gate", got)
	}
}

// close() must not return while an Install is still running: shutdown removes
// the rules right after, and an Install finishing later would put them back.
func TestInstallGate_CloseWaitsForInFlightInstall(t *testing.T) {
	var gate installGate
	i := &recordingInterceptor{started: make(chan struct{}), block: make(chan struct{})}
	go gate.install(i, InterceptConfig{})
	<-i.started

	closed := make(chan struct{})
	go func() {
		gate.close()
		close(closed)
	}()
	select {
	case <-closed:
		t.Fatal("close() returned while Install was still running")
	case <-time.After(50 * time.Millisecond):
	}

	close(i.block)
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("close() did not return after Install finished")
	}
	if got := i.installs.Load(); got != 1 {
		t.Errorf("installs = %d, want 1", got)
	}
}

// Providers built outside a Gateway (tests) have no gate.
func TestInstallGate_NilGateInstalls(t *testing.T) {
	var gate *installGate
	i := &recordingInterceptor{}
	if err := gate.install(i, InterceptConfig{}); err != nil {
		t.Fatalf("nil gate: %v", err)
	}
	if got := i.installs.Load(); got != 1 {
		t.Errorf("installs = %d, want 1", got)
	}
}

// ── provider ────────────────────────────────────────────────────────

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

// A doUp that was still dialing when shutdown began reaches provider.Start
// after the rules were removed. It must not install them.
func TestGatewayProvider_Start_RefusedOnceGateIsClosed(t *testing.T) {
	port := freePort(t)
	i := &recordingInterceptor{}
	gate := &installGate{}
	gate.close()
	p := newGatewayProvider(i, InterceptConfig{ListenPort: port}, gate)

	err := p.Start(t.Context(), &mockHandler{})
	if !errors.Is(err, errGatewayStopping) {
		t.Fatalf("Start err = %v, want errGatewayStopping", err)
	}
	if got := i.installs.Load(); got != 0 {
		t.Errorf("Install ran %d times", got)
	}
	// The listeners Start opened before the refusal must be closed again.
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		t.Fatalf("TPROXY port still bound after a refused Start: %v", err)
	}
	ln.Close()
}

// ── shutdown ────────────────────────────────────────────────────────

// `/etc/init.d/k2r stop` on OpenWrt 25.12 (2026-09-30), before shutdown
// removed the rules: `table inet k2r` stayed, every LAN DNS query and the
// router's own were redirected to a port nobody listened on.
func TestShutdown_RemovesRulesAndClosesGate(t *testing.T) {
	setStateDirForTest(t, t.TempDir())
	removed := stubRemoveInterception(t)
	g := New("test", "test", "amd64", "")

	g.shutdown()

	if got := removed.Load(); got != 1 {
		t.Errorf("rules removed %d times, want 1", got)
	}
	if err := g.gate.install(&recordingInterceptor{}, InterceptConfig{}); !errors.Is(err, errGatewayStopping) {
		t.Errorf("install after shutdown: err = %v, want errGatewayStopping", err)
	}
}

// The order is the point: had the gate closed after the removal, an Install
// slipping in between would survive the shutdown.
func TestShutdown_ClosesGateBeforeRemovingRules(t *testing.T) {
	setStateDirForTest(t, t.TempDir())
	g := New("test", "test", "amd64", "")
	var gateClosedAtRemoval bool
	orig := removeInterception
	removeInterception = func() {
		gateClosedAtRemoval = errors.Is(g.gate.install(&recordingInterceptor{}, InterceptConfig{}), errGatewayStopping)
	}
	t.Cleanup(func() { removeInterception = orig })

	g.shutdown()

	if !gateClosedAtRemoval {
		t.Error("rules were removed while the install gate was still open")
	}
}

// A transport race holds opMu for ~5 s and procd SIGKILLs 5 s after SIGTERM.
// The rules must be gone long before either.
func TestShutdown_RemovesRulesWithoutWaitingForInFlightUp(t *testing.T) {
	setStateDirForTest(t, t.TempDir())
	setShutdownGrace(t, 150*time.Millisecond)
	removed := stubRemoveInterception(t)
	g := New("test", "test", "amd64", "")

	g.opMu.Lock() // a doUp is in flight
	done := make(chan struct{})
	start := time.Now()
	go func() {
		g.shutdown()
		close(done)
	}()

	deadline := time.After(100 * time.Millisecond)
	for removed.Load() == 0 {
		select {
		case <-deadline:
			t.Fatal("rules not removed while a doUp holds opMu")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown did not return after its grace period")
	}
	if waited := time.Since(start); waited < 150*time.Millisecond {
		t.Errorf("shutdown returned after %v, before the grace period — it did not wait for the tunnel teardown at all", waited)
	}
	g.opMu.Unlock() // let the queued doDown finish before the test's state dir goes away
	time.Sleep(20 * time.Millisecond)
}

// A stop is not a disconnect: the next start must reconnect.
func TestShutdown_PreservesSavedSession(t *testing.T) {
	setStateDirForTest(t, t.TempDir())
	stubRemoveInterception(t)
	saveState(&persistedState{Config: &GatewayConfig{ListenPort: 12345}, State: "connected", Timestamp: time.Now()})
	g := New("test", "test", "amd64", "")

	g.shutdown()

	st := loadState()
	if st == nil || st.State != "connected" || st.Config == nil {
		t.Fatalf("saved session lost across shutdown: %+v", st)
	}
}

// An OTA upgrade restarts the service from inside the daemon. The successor's
// Install replaces the rules back-to-back; removing them here would hand LAN
// DNS to the ISP resolver for the seconds in between.
func TestShutdown_RestartHandoffLeavesRules(t *testing.T) {
	setStateDirForTest(t, t.TempDir())
	removed := stubRemoveInterception(t)
	g := New("test", "test", "amd64", "")
	g.restartHandoff.Store(true)

	g.shutdown()

	if got := removed.Load(); got != 0 {
		t.Errorf("rules removed %d times during a restart handoff, want 0", got)
	}
	if err := g.gate.install(&recordingInterceptor{}, InterceptConfig{}); err != nil {
		t.Errorf("gate closed during a restart handoff: %v", err)
	}
}

// shortHandoffLinger shrinks the time the handoff outlives the restart command.
func shortHandoffLinger(t *testing.T, d time.Duration) {
	t.Helper()
	orig := restartHandoffLinger
	restartHandoffLinger = d
	t.Cleanup(func() { restartHandoffLinger = orig })
}

func TestRestartService_HandoffWhileRestarting(t *testing.T) {
	setStateDirForTest(t, t.TempDir())
	shortHandoffLinger(t, time.Millisecond)
	g := New("test", "test", "amd64", "")
	var duringRestart bool
	orig := runServiceRestart
	runServiceRestart = func() { duringRestart = g.restartHandoff.Load() }
	t.Cleanup(func() { runServiceRestart = orig })

	g.restartService()

	if !duringRestart {
		t.Error("handoff flag not set while the restart command runs")
	}
	// Still running well after the command came back: nothing restarted us.
	// A later plain stop must remove the rules again.
	if g.restartHandoff.Load() {
		t.Error("handoff flag still set after restartService returned")
	}
}

// systemd ends `systemctl restart` — a child in the unit's cgroup — with the
// same SIGTERM it sends the daemon. The command is back a moment before
// shutdown reads the flag, and the flag must still be up.
func TestRestartService_HandoffOutlivesTheRestartCommand(t *testing.T) {
	setStateDirForTest(t, t.TempDir())
	removed := stubRemoveInterception(t)
	shortHandoffLinger(t, 500*time.Millisecond)
	g := New("test", "test", "amd64", "")
	commandReturned := make(chan struct{})
	orig := runServiceRestart
	runServiceRestart = func() { close(commandReturned) }
	t.Cleanup(func() { runServiceRestart = orig })

	done := make(chan struct{})
	go func() {
		g.restartService()
		close(done)
	}()
	<-commandReturned
	g.shutdown() // the SIGTERM that ended the command reaches the daemon
	<-done

	if got := removed.Load(); got != 0 {
		t.Errorf("rules removed %d times during a restart handoff", got)
	}
}

// The upgrader must restart through restartService, not around it.
func TestNew_UpgraderRestartsThroughHandoff(t *testing.T) {
	setStateDirForTest(t, t.TempDir())
	shortHandoffLinger(t, time.Millisecond)
	g := New("test", "test", "amd64", "")
	var sawHandoff bool
	orig := runServiceRestart
	runServiceRestart = func() { sawHandoff = g.restartHandoff.Load() }
	t.Cleanup(func() { runServiceRestart = orig })

	g.updater.Restart()

	if !sawHandoff {
		t.Error("upgrader restart did not go through the handoff")
	}
}

// Run must turn Stop (what cmd/k2r calls on SIGTERM) into a shutdown.
func TestRun_StopShutsDown(t *testing.T) {
	setStateDirForTest(t, t.TempDir())
	removed := stubRemoveInterception(t)

	// Run starts the auto-reconnect loop on a goroutine the test cannot
	// join. Give it a saved session whose outbound does not parse and a 1 ms
	// backoff: every attempt takes opMu, so taking opMu after Run returns
	// orders the loop's reads of the test-only globals before the cleanups
	// that restore them (the race detector needs that edge).
	bad := &GatewayConfig{LANSubnets: []string{"10.0.0.1/24"}}
	bad.Routes = []config.RouteConfig{{Via: "k2v5://%zz"}}
	saveState(&persistedState{Config: bad, State: "connected", Timestamp: time.Now()})
	origBackoff := autoReconnectBackoff
	autoReconnectBackoff = []time.Duration{time.Millisecond}
	t.Cleanup(func() { autoReconnectBackoff = origBackoff })

	g := New("test", "test", "amd64", "")
	attempted := &recordingInterceptor{} // closeTunnel removes it on the first attempt
	g.interceptor = attempted
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(freePort(t)))

	errCh := make(chan error, 1)
	go func() { errCh <- g.Run(addr) }()
	for deadline := time.Now().Add(5 * time.Second); attempted.removes.Load() == 0; time.Sleep(time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("auto-reconnect never attempted")
		}
	}
	g.Stop()
	defer func() {
		g.opMu.Lock()
		g.opMu.Unlock()
	}()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Run returned %v after Stop", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after Stop")
	}
	if got := removed.Load(); got != 1 {
		t.Errorf("rules removed %d times, want 1", got)
	}
}
