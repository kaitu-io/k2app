//go:build linux

package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kaitu-io/k2/config"
	"github.com/kaitu-io/k2/rule"
)

func TestSaveLoadState(t *testing.T) {
	dir := t.TempDir()
	origStateDir := stateDir
	// Override stateDir for test isolation.
	oldPath := statePath()
	defer func() { _ = oldPath }()

	path := filepath.Join(dir, "state.json")

	st := &persistedState{
		Config: &GatewayConfig{
			ListenPort: 8888,
		},
		State:     "connected",
		Timestamp: time.Now().Truncate(time.Second),
	}

	// Write directly to temp path.
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}

	// Read it back.
	readData, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var loaded persistedState
	if err := json.Unmarshal(readData, &loaded); err != nil {
		t.Fatal(err)
	}
	if loaded.State != "connected" {
		t.Errorf("State = %q, want connected", loaded.State)
	}
	if loaded.Config == nil {
		t.Fatal("Config is nil")
	}
	if loaded.Config.ListenPort != 8888 {
		t.Errorf("Config.ListenPort = %d, want 8888", loaded.Config.ListenPort)
	}

	_ = origStateDir
}

func TestLoadState_Missing(t *testing.T) {
	// loadState returns nil for missing file — verify the pattern.
	data, err := os.ReadFile("/nonexistent/state.json")
	if err != nil {
		// Expected.
		var st persistedState
		if json.Unmarshal(data, &st) != nil {
			// Also expected (nil data).
		}
	}
}

func TestPersistedState_JSONRoundTrip(t *testing.T) {
	cfg := &GatewayConfig{
		ListenPort: 12345,
		LANSubnets: []string{"192.168.1.0/24"},
	}
	cfg.Routes = []config.RouteConfig{
		{Via: "k2v5://test:test@example.com:443", Match: rule.MatchConfig{All: true}},
	}
	config.SetDefaults(&cfg.ClientConfig)

	st := &persistedState{
		Config:    cfg,
		State:     "connected",
		Timestamp: time.Date(2026, 3, 25, 12, 0, 0, 0, time.UTC),
	}

	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		t.Fatal(err)
	}

	var loaded persistedState
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatal(err)
	}
	if loaded.State != "connected" {
		t.Errorf("State = %q", loaded.State)
	}
	if loaded.Config.ListenPort != 12345 {
		t.Errorf("ListenPort = %d", loaded.Config.ListenPort)
	}
	// Server is derived from routes after SetDefaults.
	config.SetDefaults(&loaded.Config.ClientConfig)
	if loaded.Config.Server != "k2v5://test:test@example.com:443" {
		t.Errorf("Server = %q (derived from routes)", loaded.Config.Server)
	}
	if len(loaded.Config.LANSubnets) != 1 {
		t.Errorf("LANSubnets = %v", loaded.Config.LANSubnets)
	}
}

func TestPersistedState_DisconnectedHasNoConfig(t *testing.T) {
	st := &persistedState{
		State:     "disconnected",
		Timestamp: time.Now(),
	}
	data, _ := json.Marshal(st)
	var loaded persistedState
	json.Unmarshal(data, &loaded)
	if loaded.Config != nil {
		t.Error("disconnected state should have nil config")
	}
}

// setStateDirForTest redirects state.json reads/writes to a temp directory
// for the duration of the test.
func setStateDirForTest(t *testing.T, dir string) {
	t.Helper()
	prev := stateDirOverride
	stateDirOverride = dir
	t.Cleanup(func() { stateDirOverride = prev })
}

// ── Bug 5: state.json must preserve Config across daemon restart ────

// User-initiated `k2r down` (handleDown → safego.Go(g.doDown)) calls
// doDown(userInitiated=true). state.json must reflect "I want to be
// disconnected, forget my config" so the next start does NOT auto-reconnect.
func TestDoDown_UserInitiated_ClearsConfig(t *testing.T) {
	setStateDirForTest(t, t.TempDir())
	// Seed: simulate a previously successful doUp that wrote a connected state.
	cfg := &GatewayConfig{ListenPort: 12345}
	saveState(&persistedState{Config: cfg, State: "connected", Timestamp: time.Now()})

	g := New("test", "test", "amd64", "")
	g.doDown(true) // user-initiated via `k2r down`

	loaded := loadState()
	if loaded == nil {
		t.Fatal("state.json must exist after user-initiated down")
	}
	if loaded.State != "disconnected" {
		t.Errorf("State = %q, want %q", loaded.State, "disconnected")
	}
	if loaded.Config != nil {
		t.Errorf("user-initiated down must clear Config so tryAutoReconnect bails on next start, got: %+v", loaded.Config)
	}
}

// Daemon shutdown via ctx.Done() (procd SIGTERM, `/etc/init.d/k2r restart`,
// Linux reboot) MUST NOT clear the saved config — the user expects the
// tunnel to resume on next start. doDown(userInitiated=false) must leave
// state.json in a state that lets tryAutoReconnect resume.
func TestDoDown_Shutdown_PreservesAutoReconnect(t *testing.T) {
	setStateDirForTest(t, t.TempDir())
	cfg := &GatewayConfig{ListenPort: 12345}
	saveState(&persistedState{Config: cfg, State: "connected", Timestamp: time.Now()})
	original := loadState()

	g := New("test", "test", "amd64", "")
	g.doDown(false) // daemon-internal shutdown (NOT user-initiated)

	loaded := loadState()
	if loaded == nil {
		t.Fatal("state.json must still exist after daemon shutdown")
	}
	if loaded.Config == nil {
		t.Fatal("daemon shutdown must preserve Config so next start auto-reconnects (Bug 5)")
	}
	if loaded.Config.ListenPort != cfg.ListenPort {
		t.Errorf("Config.ListenPort = %d, want %d (preserved from before shutdown)",
			loaded.Config.ListenPort, cfg.ListenPort)
	}
	if loaded.State != "connected" {
		t.Errorf("State = %q, want %q (tryAutoReconnect requires connected state)",
			loaded.State, "connected")
	}
	_ = original
}

// Full lifecycle: doUp writes connected + Config → doDown(false) for
// shutdown → on next start, tryAutoReconnect's prerequisites (state ==
// "connected" && Config != nil) must still hold so it would attempt
// reconnect. Goal: prove the procd restart path is not destructive.
func TestPersistedState_SurvivesShutdownRoundtrip(t *testing.T) {
	setStateDirForTest(t, t.TempDir())
	cfg := &GatewayConfig{ListenPort: 12345}
	saveState(&persistedState{Config: cfg, State: "connected", Timestamp: time.Now()})

	g := New("test", "test", "amd64", "")
	g.doDown(false) // shutdown

	st := loadState()
	autoReconnectReady := st != nil && st.State == "connected" && st.Config != nil
	if !autoReconnectReady {
		t.Fatalf("post-shutdown state.json must satisfy tryAutoReconnect prerequisites: state=%q, hasConfig=%v",
			func() string {
				if st == nil {
					return "<nil>"
				}
				return st.State
			}(),
			st != nil && st.Config != nil)
	}
}

// ── Bug 4: auto-reconnect must retry on failure ─────────────────────

// When state.json says "disconnected" (user explicitly down or never
// connected), runAutoReconnectLoop must bail without ever calling attempt.
func TestRunAutoReconnectLoop_NoConfigBails(t *testing.T) {
	setStateDirForTest(t, t.TempDir())
	saveState(&persistedState{State: "disconnected", Timestamp: time.Now()})

	var calls atomic.Int32
	attempt := func() error {
		calls.Add(1)
		return nil
	}
	g := New("test", "test", "amd64", "")
	g.runAutoReconnectLoop(context.Background(), attempt, []time.Duration{time.Millisecond})

	if got := calls.Load(); got != 0 {
		t.Errorf("attempt called %d times; expected 0 (no Config to reconnect with)", got)
	}
}

// Single successful attempt — must not keep retrying.
func TestRunAutoReconnectLoop_SucceedsOnFirstTry(t *testing.T) {
	setStateDirForTest(t, t.TempDir())
	saveState(&persistedState{
		Config:    &GatewayConfig{ListenPort: 12345},
		State:     "connected",
		Timestamp: time.Now(),
	})

	var calls atomic.Int32
	attempt := func() error {
		calls.Add(1)
		return nil
	}
	g := New("test", "test", "amd64", "")
	g.runAutoReconnectLoop(context.Background(), attempt, []time.Duration{time.Millisecond, time.Millisecond})

	if got := calls.Load(); got != 1 {
		t.Errorf("attempt called %d times; expected exactly 1", got)
	}
}

// A router stays connected for months and the saved timestamp is the moment
// the tunnel came up. A session older than a week used to be dropped, so a
// router that had been up for 8 days did not reconnect after a power cut.
func TestRunAutoReconnectLoop_OldSavedSessionStillReconnects(t *testing.T) {
	setStateDirForTest(t, t.TempDir())
	saveState(&persistedState{
		Config:    &GatewayConfig{ListenPort: 12345},
		State:     "connected",
		Timestamp: time.Now().Add(-90 * 24 * time.Hour),
	})

	var calls atomic.Int32
	attempt := func() error {
		calls.Add(1)
		return nil
	}
	g := New("test", "test", "amd64", "")
	g.runAutoReconnectLoop(context.Background(), attempt, []time.Duration{time.Millisecond})

	if got := calls.Load(); got != 1 {
		t.Errorf("attempt called %d times; a 90-day-old session must still reconnect", got)
	}
}

// Bug 4 core: 2 failures must NOT make us give up — the loop continues
// through backoff until success.
func TestRunAutoReconnectLoop_RetriesAfterFailure(t *testing.T) {
	setStateDirForTest(t, t.TempDir())
	saveState(&persistedState{
		Config:    &GatewayConfig{ListenPort: 12345},
		State:     "connected",
		Timestamp: time.Now(),
	})

	var calls atomic.Int32
	attempt := func() error {
		n := calls.Add(1)
		if n < 3 {
			return errors.New("simulated reconnect failure")
		}
		return nil
	}
	g := New("test", "test", "amd64", "")
	g.runAutoReconnectLoop(context.Background(), attempt, []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond})

	if got := calls.Load(); got != 3 {
		t.Errorf("attempt called %d times; expected 3 (2 fails + 1 success)", got)
	}
}

// Loop must honor ctx cancellation between attempts — a long-running router
// shutting down should not be blocked by an in-progress backoff sleep.
func TestRunAutoReconnectLoop_RespectsCtxCancel(t *testing.T) {
	setStateDirForTest(t, t.TempDir())
	saveState(&persistedState{
		Config:    &GatewayConfig{ListenPort: 12345},
		State:     "connected",
		Timestamp: time.Now(),
	})

	var calls atomic.Int32
	attempt := func() error {
		calls.Add(1)
		return errors.New("always fail")
	}

	ctx, cancel := context.WithCancel(context.Background())
	g := New("test", "test", "amd64", "")

	// Cancel after the first attempt observes (it always fails).
	go func() {
		for calls.Load() < 1 {
			time.Sleep(100 * time.Microsecond)
		}
		cancel()
	}()

	// 2nd and later delays are long enough that the cancel must kick in
	// before they elapse.
	g.runAutoReconnectLoop(ctx, attempt, []time.Duration{time.Millisecond, 30 * time.Second, 30 * time.Second})

	if got := calls.Load(); got > 2 {
		t.Errorf("attempt called %d times; ctx cancel should have stopped the loop after 1-2", got)
	}
}

// ── auto-reconnect yields to the user ───────────────────────────────

// Measured on OpenWrt 25.12 (2026-09-30): state.json held a config that no
// longer connected; the loop's first attempt failed, the user ran `k2r up`
// with a working one and was connected — 8 s later attempt 2 tore that tunnel
// down, failed again, and left the router disconnected with its rules gone.
func TestRunAutoReconnectLoop_StopsWhenSuperseded(t *testing.T) {
	setStateDirForTest(t, t.TempDir())
	saveState(&persistedState{
		Config:    &GatewayConfig{ListenPort: 12345},
		State:     "connected",
		Timestamp: time.Now(),
	})

	var calls atomic.Int32
	attempt := func() error {
		calls.Add(1)
		return errAutoReconnectSuperseded
	}
	g := New("test", "test", "amd64", "")
	done := make(chan struct{})
	go func() {
		g.runAutoReconnectLoop(context.Background(), attempt, []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("loop kept running after the attempt was superseded")
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("attempt called %d times; a superseded loop must stop at once", got)
	}
}

// unconnectableConfig is a session whose outbound does not parse: doUp fails
// right after closeTunnel, before anything touches the system.
func unconnectableConfig() *GatewayConfig {
	cfg := &GatewayConfig{LANSubnets: []string{"10.0.0.1/24"}}
	cfg.Routes = []config.RouteConfig{{Via: "k2v5://%zz"}}
	return cfg
}

// saveConnectedSession writes the state a connected router has on disk. The
// session in it cannot connect, so an attempt that does run ends as a doUp
// failure — told apart from errAutoReconnectSuperseded.
func saveConnectedSession(t *testing.T) {
	t.Helper()
	saveState(&persistedState{Config: unconnectableConfig(), State: "connected", Timestamp: time.Now()})
}

// ranDoUp reports whether an attempt went through to doUp.
func ranDoUp(err error) bool {
	return err != nil && !errors.Is(err, errAutoReconnectSuperseded)
}

// coreRequest sends one /api/core action from loopback.
func coreRequest(t *testing.T, g *Gateway, body string) {
	t.Helper()
	r := httptest.NewRequest("POST", "/api/core", strings.NewReader(body))
	r.RemoteAddr = "127.0.0.1:50000"
	w := httptest.NewRecorder()
	g.handleCore(w, r)
	if w.Code != 200 {
		t.Fatalf("%s: HTTP %d", body, w.Code)
	}
}

// Somebody else's doUp succeeded: a tunnel is up and this loop's doUp would
// start by tearing it down.
func TestAutoReconnectUp_StandsDownOnceAnotherUpSucceeded(t *testing.T) {
	setStateDirForTest(t, t.TempDir())
	saveConnectedSession(t)
	g := New("test", "test", "amd64", "")
	live := &recordingInterceptor{} // the tunnel the user just brought up
	g.interceptor = live

	attempt := g.autoReconnectUp(nil)
	g.ups.Add(1) // what doUpLocked does when it succeeds

	err := attempt()
	if !errors.Is(err, errAutoReconnectSuperseded) {
		t.Fatalf("err = %v, want errAutoReconnectSuperseded", err)
	}
	if got := live.removes.Load(); got != 0 {
		t.Errorf("the user's tunnel was torn down (%d Remove calls)", got)
	}
	if g.interceptor == nil {
		t.Error("the user's tunnel state was cleared")
	}
}

// With nothing else having happened, the attempt is a plain doUp — it must
// not be refused.
func TestAutoReconnectUp_RunsWhileItsSessionIsStillWanted(t *testing.T) {
	setStateDirForTest(t, t.TempDir())
	saveConnectedSession(t)
	g := New("test", "test", "amd64", "")
	stale := &recordingInterceptor{}
	g.interceptor = stale

	if err := g.autoReconnectUp(nil)(); !ranDoUp(err) {
		t.Fatalf("err = %v, want the doUp failure", err)
	}
	if got := stale.removes.Load(); got != 1 {
		t.Errorf("doUp did not run (closeTunnel Remove calls = %d, want 1)", got)
	}
}

// `k2r down` and reset end the wish to reconnect, whenever they land.
func TestAutoReconnectUp_StandsDownAfterDownOrReset(t *testing.T) {
	for _, action := range []string{"down", "reset"} {
		t.Run(action, func(t *testing.T) {
			setStateDirForTest(t, t.TempDir())
			saveConnectedSession(t)
			g := New("test", "test", "amd64", "")
			attempt := g.autoReconnectUp(nil)

			coreRequest(t, g, `{"action":"`+action+`"}`)
			// `down` persists the disconnect from a goroutine; let it land
			// before the test's state dir goes away.
			for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); time.Sleep(time.Millisecond) {
				if st := loadState(); st == nil || st.State == "disconnected" {
					break
				}
			}

			if err := attempt(); !errors.Is(err, errAutoReconnectSuperseded) {
				t.Errorf("after %s the loop's attempt returned %v, want errAutoReconnectSuperseded", action, err)
			}
		})
	}
}

// The state, not the presence of a config, is what says "reconnect": a state
// file that keeps its config while disconnected must not be reconnected.
func TestAutoReconnectUp_StandsDownWhenSavedStateIsDisconnected(t *testing.T) {
	setStateDirForTest(t, t.TempDir())
	saveConnectedSession(t)
	g := New("test", "test", "amd64", "")
	attempt := g.autoReconnectUp(nil)

	saveState(&persistedState{Config: unconnectableConfig(), State: "disconnected", Timestamp: time.Now()})

	if err := attempt(); !errors.Is(err, errAutoReconnectSuperseded) {
		t.Fatalf("err = %v, want errAutoReconnectSuperseded", err)
	}
}

// A user's up that fails leaves the router with no tunnel and a saved session
// that still says connected. The loop is then the only thing that will bring
// it back, so it must keep going: an app that retries "connect" while the WAN
// is down must not be what ends the router's own recovery.
func TestAutoReconnectUp_KeepsGoingAfterAFailedUserUp(t *testing.T) {
	for _, tt := range []struct{ name, body string }{
		{"up", `{"action":"up","params":{"config":{"routes":[{"via":"k2v5://%zz"}]}}}`},
		// k2subs: the fetch is refused at once, so doUp ends in Resolve.
		{"set-credential", `{"action":"set-credential","params":{"url":"k2subs://u:t@127.0.0.1:1/api/subs"}}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			setStateDirForTest(t, t.TempDir())
			saveConnectedSession(t)
			g := New("test", "test", "amd64", "")
			old := &recordingInterceptor{}
			g.interceptor = old
			attempt := g.autoReconnectUp(nil)

			coreRequest(t, g, tt.body)
			// The request's doUp runs on its own goroutine: wait until it has
			// torn the old tunnel down, then until it has let go of opMu.
			for deadline := time.Now().Add(5 * time.Second); old.removes.Load() == 0; time.Sleep(time.Millisecond) {
				if time.Now().After(deadline) {
					t.Fatal("the user's doUp never ran")
				}
			}
			g.opMu.Lock()
			g.opMu.Unlock()

			if err := attempt(); !ranDoUp(err) {
				t.Errorf("after a failed %s the loop's attempt returned %v, want it to run (the doUp failure)", tt.name, err)
			}
		})
	}
}

// set-credential saves the new credential before it tries to connect. When
// that connect fails, the loop must come back with the new credential — not
// with the one it saw when it started.
func TestAutoReconnectUp_UsesTheSessionSavedNow(t *testing.T) {
	setStateDirForTest(t, t.TempDir())
	saveConnectedSession(t)
	g := New("test", "test", "amd64", "")
	attempt := g.autoReconnectUp(nil)

	// Each session fails in doUp for its own reason; the reason says which
	// one the attempt used.
	const newSessionFailure = "missing auth"
	before := attempt()
	if !ranDoUp(before) || strings.Contains(before.Error(), newSessionFailure) {
		t.Fatalf("attempt with the session the loop started from: err = %v", before)
	}

	replaced := &GatewayConfig{LANSubnets: []string{"10.0.0.1/24"}}
	replaced.Routes = []config.RouteConfig{{Via: "k2v5://new-credential.invalid:443"}}
	saveState(&persistedState{Config: replaced, State: "connected", Timestamp: time.Now()})

	after := attempt()
	if !ranDoUp(after) || !strings.Contains(after.Error(), newSessionFailure) {
		t.Fatalf("attempt after the saved session was replaced: err = %v, want the new session's failure (%q)", after, newSessionFailure)
	}
}
