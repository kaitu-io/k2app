package daemon

import (
	"context"
	"os"
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
