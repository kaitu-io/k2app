package daemon

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/kaitu-io/k2/config"
)

// AppBypassOverrides is the persistent shape of daemon's app-bypass state.
// Lives inside persistedState so the existing saveState/loadState path
// carries it alongside the auto-reconnect Config. No separate file.
//
// Privacy: persistedState is written plaintext to stateFilePath() (0o600,
// dir 0o700). Entry names never appear in logs / feedback / Sentry per
// spec §13. Encryption deferred until daemon gains a shared encrypted KV.
type AppBypassOverrides struct {
	Region      string   `json:"region"`
	ProcessAdds []string `json:"process_adds,omitempty"`
	PackageAdds []string `json:"package_adds,omitempty"`
}

// persistedState is saved to disk for auto-reconnect across daemon restarts.
type persistedState struct {
	Config    *config.ClientConfig `json:"config,omitempty"`
	State     string               `json:"state"`
	Timestamp time.Time            `json:"timestamp"`
	AppBypass *AppBypassOverrides  `json:"app_bypass,omitempty"`
}

func stateFilePath() string {
	return filepath.Join(stateDir(), "state.json")
}

// stateDir returns a platform-secure directory for persisted state.
// Windows SYSTEM: %ProgramData%\k2 (admin-only ACL).
// Windows user: %LocalAppData%\k2 (per-user).
// macOS/Linux: os.TempDir()/k2 (per-user, 0700 enforced by OS).
//
// Declared as var (not func) so tests can inject a temporary directory
// via stateDir = func() string { return t.TempDir() } without side effects.
var stateDir = func() string {
	if runtime.GOOS == "windows" {
		if config.IsRoot() {
			programData := os.Getenv("ProgramData")
			if programData == "" {
				programData = `C:\ProgramData`
			}
			return filepath.Join(programData, "k2")
		}
		base, err := os.UserCacheDir()
		if err != nil {
			return filepath.Join(os.TempDir(), "k2")
		}
		return filepath.Join(base, "k2")
	}
	return filepath.Join(os.TempDir(), "k2")
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
