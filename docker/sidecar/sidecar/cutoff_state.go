package sidecar

import (
	"encoding/json"
	"os"
)

// cutoffState is the persisted enforcer state, written to the sidecar's writable
// config volume (/etc/kaitu) so a restart re-applies an in-effect cut before
// contacting Center (zero leak window). See the traffic-cutoff design spec.
type cutoffState struct {
	EpochID int64 `json:"epoch_id"`
	Cut     bool  `json:"cut"`
}

// loadCutoffState reads the persisted state. A missing or corrupt file is treated
// as the zero value (no cut) — never an error that would block startup.
func loadCutoffState(path string) cutoffState {
	var s cutoffState
	data, err := os.ReadFile(path)
	if err != nil {
		return cutoffState{}
	}
	if err := json.Unmarshal(data, &s); err != nil {
		return cutoffState{}
	}
	return s
}

// saveCutoffState persists the state atomically (write temp + rename) so a crash
// mid-write never leaves a half-written file.
func saveCutoffState(path string, s cutoffState) error {
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
