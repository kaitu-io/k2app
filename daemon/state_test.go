package daemon

import (
	"context"
	"os"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/kaitu-io/k2/config"
	"github.com/kaitu-io/k2/engine"
)

func TestStateFilePath_NotInSystemTempDir(t *testing.T) {
	path := stateFilePath()

	if runtime.GOOS == "windows" {
		winTemp := os.Getenv("SystemRoot")
		if winTemp != "" {
			systemTemp := winTemp + `\Temp`
			if strings.HasPrefix(strings.ToLower(path), strings.ToLower(systemTemp)) {
				t.Errorf("stateFilePath() = %q, must not be under system temp %q", path, systemTemp)
			}
		}
	}

	if path == "" {
		t.Fatal("stateFilePath() returned empty string")
	}
	if !strings.Contains(path, "k2") {
		t.Errorf("stateFilePath() = %q, should contain 'k2'", path)
	}
}

func TestStatePersistence(t *testing.T) {
	// Clean up.
	os.Remove(stateFilePath())
	defer os.Remove(stateFilePath())

	cfg := config.ClientFromURL("k2v5://test:tok@host:443")

	// Save state.
	st := persistedState{
		Config:    cfg,
		State:     engine.StateConnected,
		Timestamp: time.Now(),
	}
	saveState(st)

	// Load state.
	loaded, err := loadState()
	if err != nil {
		t.Fatalf("loadState: %v", err)
	}

	// Server is derived (json:"-"), re-derive after load.
	if loaded.Config != nil {
		config.SetDefaults(loaded.Config)
	}
	if loaded.Config == nil || loaded.Config.Server != st.Config.Server {
		t.Fatalf("expected config server %s, got %v", st.Config.Server, loaded.Config)
	}
	if loaded.State != st.State {
		t.Fatalf("expected state %s, got %s", st.State, loaded.State)
	}
}

func TestLoadStateNotFound(t *testing.T) {
	os.Remove(stateFilePath())
	_, err := loadState()
	if err == nil {
		t.Fatal("expected error for missing state file")
	}
}

func TestAutoReconnectSkipsStale(t *testing.T) {
	os.Remove(stateFilePath())
	defer os.Remove(stateFilePath())

	cfg := config.ClientFromURL("k2v5://test:tok@host:443")

	// Save stale state (8 days ago — exceeds 7-day auto-reconnect window).
	saveState(persistedState{
		Config:    cfg,
		State:     engine.StateConnected,
		Timestamp: time.Now().Add(-8 * 24 * time.Hour),
	})

	d := New()
	// tryAutoReconnect should skip because it's stale.
	d.tryAutoReconnect(context.Background())

	// State should remain stopped.
	if d.State() != engine.StateDisconnected {
		t.Fatalf("expected stopped, got %s", d.State())
	}
}

func TestPersistedState_AppBypassRoundTrip(t *testing.T) {
	// Use a custom state dir for isolation
	origDir := stateDir
	tmpDir := t.TempDir()
	stateDir = func() string { return tmpDir }
	defer func() { stateDir = origDir }()

	want := persistedState{
		Config:    nil, // OK — AppBypass alone, no active connection yet
		State:     "disconnected",
		Timestamp: time.Now().UTC(),
		AppBypass: &AppBypassOverrides{
			Region:      "cn",
			ProcessAdds: []string{"Steam", "VSCode"},
			PackageAdds: []string{"com.user.app"},
		},
	}
	saveState(want)
	got, err := loadState()
	if err != nil {
		t.Fatalf("loadState: %v", err)
	}
	if got.AppBypass == nil {
		t.Fatal("AppBypass field nil after round-trip")
	}
	if got.AppBypass.Region != "cn" {
		t.Errorf("Region = %q, want cn", got.AppBypass.Region)
	}
	if !reflect.DeepEqual([]string{"Steam", "VSCode"}, got.AppBypass.ProcessAdds) {
		t.Errorf("ProcessAdds = %v", got.AppBypass.ProcessAdds)
	}
}

func TestPersistedState_AppBypassNilIsOmitted(t *testing.T) {
	// When AppBypass is nil, JSON should not contain the key (omitempty).
	origDir := stateDir
	tmpDir := t.TempDir()
	stateDir = func() string { return tmpDir }
	defer func() { stateDir = origDir }()

	saveState(persistedState{State: "disconnected", Timestamp: time.Now().UTC()})
	data, _ := os.ReadFile(stateFilePath())
	if strings.Contains(string(data), "app_bypass") {
		t.Errorf("nil AppBypass should be omitted from JSON, got: %s", string(data))
	}
}

// TestPersistedState_LoadOldFormat verifies that a state.json written by an
// older daemon (no AppBypass field) loads cleanly without panicking and
// yields a nil AppBypassOverrides. This is the v0.4.x → v0.4.5 upgrade path:
// user upgrades the daemon, the existing state.json predates app-bypass-v2,
// and tryAutoReconnect must not crash.
func TestPersistedState_LoadOldFormat(t *testing.T) {
	origDir := stateDir
	tmpDir := t.TempDir()
	stateDir = func() string { return tmpDir }
	defer func() { stateDir = origDir }()

	// Old-format state.json: no app_bypass field at all.
	oldJSON := `{
		"state": "connected",
		"timestamp": "2026-05-01T10:00:00Z"
	}`
	if err := os.WriteFile(stateFilePath(), []byte(oldJSON), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := loadState()
	if err != nil {
		t.Fatalf("loadState: %v", err)
	}
	if got.AppBypass != nil {
		t.Errorf("old-format state.json should yield nil AppBypass, got %+v", got.AppBypass)
	}
	if got.State != "connected" {
		t.Errorf("State = %q, want connected", got.State)
	}
}

// TestTryAutoReconnect_SeedsAppBypassFromState exercises the daemon-restart
// path: state.json has saved AppBypass overrides → tryAutoReconnect must
// load them into d.appBypassOverrides BEFORE deciding to reconnect, so
// the user's smart-bypass preferences survive even if the reconnect itself
// is skipped (e.g. state too old, config nil, etc.).
func TestTryAutoReconnect_SeedsAppBypassFromState(t *testing.T) {
	origDir := stateDir
	tmpDir := t.TempDir()
	stateDir = func() string { return tmpDir }
	defer func() { stateDir = origDir }()

	// Write state with AppBypass + stale timestamp (skips reconnect, but
	// AppBypass seeding should still happen — that's the contract).
	saveState(persistedState{
		Config:    nil,
		State:     "disconnected",
		Timestamp: time.Now().Add(-30 * 24 * time.Hour), // 30 days old
		AppBypass: &AppBypassOverrides{
			Region:      "ir",
			ProcessAdds: []string{"telegram"},
		},
	})

	d := New()
	d.tryAutoReconnect(context.Background())

	o := d.currentAppBypass()
	if o.Region != "ir" {
		t.Errorf("Region = %q, want ir (must seed regardless of stale timestamp)", o.Region)
	}
	if len(o.ProcessAdds) != 1 || o.ProcessAdds[0] != "telegram" {
		t.Errorf("ProcessAdds = %v", o.ProcessAdds)
	}
}
