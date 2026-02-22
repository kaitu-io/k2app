package daemon

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/kaitu-io/k2/config"
)

// persistedState is saved to disk for auto-reconnect across daemon restarts.
type persistedState struct {
	Config    *config.ClientConfig `json:"config,omitempty"`
	State     string               `json:"state"`
	Timestamp time.Time            `json:"timestamp"`
}

func stateFilePath() string {
	dir := os.TempDir()
	return filepath.Join(dir, "k2", "state.json")
}

func saveState(st persistedState) {
	path := stateFilePath()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		slog.Warn("cannot create state dir", "err", err)
		return
	}

	data, err := json.Marshal(st)
	if err != nil {
		slog.Warn("cannot marshal state", "err", err)
		return
	}

	if err := os.WriteFile(path, data, 0600); err != nil {
		slog.Warn("cannot write state file", "err", err)
	}
}

func loadState() (persistedState, error) {
	data, err := os.ReadFile(stateFilePath())
	if err != nil {
		return persistedState{}, err
	}

	var st persistedState
	if err := json.Unmarshal(data, &st); err != nil {
		return persistedState{}, err
	}
	return st, nil
}
