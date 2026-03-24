//go:build linux

package gateway

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

type persistedState struct {
	Config    *GatewayConfig `json:"config,omitempty"`
	State     string         `json:"state"`
	Timestamp time.Time      `json:"timestamp"`
}

func stateDir() string {
	return "/etc/k2r"
}

func statePath() string {
	return filepath.Join(stateDir(), "state.json")
}

func saveState(st *persistedState) {
	os.MkdirAll(stateDir(), 0700)
	data, _ := json.MarshalIndent(st, "", "  ")
	os.WriteFile(statePath(), data, 0600)
}

func loadState() *persistedState {
	data, err := os.ReadFile(statePath())
	if err != nil {
		return nil
	}
	var st persistedState
	if json.Unmarshal(data, &st) != nil {
		return nil
	}
	return &st
}
