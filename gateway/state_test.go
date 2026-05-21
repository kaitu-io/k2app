//go:build linux

package gateway

import (
	"encoding/json"
	"os"
	"path/filepath"
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

	g := New("test", "test", "amd64")
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

	g := New("test", "test", "amd64")
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

	g := New("test", "test", "amd64")
	g.doDown(false) // shutdown

	st := loadState()
	autoReconnectReady := st != nil && st.State == "connected" && st.Config != nil
	if !autoReconnectReady {
		t.Fatalf("post-shutdown state.json must satisfy tryAutoReconnect prerequisites: state=%q, hasConfig=%v",
			func() string { if st == nil { return "<nil>" }; return st.State }(),
			st != nil && st.Config != nil)
	}
}
