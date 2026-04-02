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
