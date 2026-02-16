package daemon

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/kaitu-io/k2/config"
)

func TestStatePersistence(t *testing.T) {
	// Clean up.
	os.Remove(stateFilePath())
	defer os.Remove(stateFilePath())

	cfg := &config.ClientConfig{Server: "k2v5://test:tok@host:443"}
	config.SetDefaults(cfg)

	// Save state.
	st := persistedState{
		Config:    cfg,
		State:     StateConnected,
		Timestamp: time.Now(),
	}
	saveState(st)

	// Load state.
	loaded, err := loadState()
	if err != nil {
		t.Fatalf("loadState: %v", err)
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

	cfg := &config.ClientConfig{Server: "k2v5://test:tok@host:443"}
	config.SetDefaults(cfg)

	// Save stale state (2 hours ago).
	saveState(persistedState{
		Config:    cfg,
		State:     StateConnected,
		Timestamp: time.Now().Add(-2 * time.Hour),
	})

	d := New()
	// tryAutoReconnect should skip because it's stale.
	d.tryAutoReconnect(context.Background())

	// State should remain stopped.
	if d.State() != StateStopped {
		t.Fatalf("expected stopped, got %s", d.State())
	}
}
