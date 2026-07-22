//go:build linux

package gateway

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// slotLocalState is router-local slot state that must survive manifest
// convergence: which slots' SSID names the customer has taken over.
// Passwords never touch this file (they live only in UCI wireless).
type slotLocalState struct {
	TakenOver map[int]bool `json:"takenOver"`
}

func slotLocalPath() string { return filepath.Join(stateDir(), "slots-local.json") }

func loadSlotLocal() *slotLocalState {
	s := &slotLocalState{TakenOver: map[int]bool{}}
	data, err := os.ReadFile(slotLocalPath())
	if err != nil {
		return s
	}
	_ = json.Unmarshal(data, s)
	if s.TakenOver == nil {
		s.TakenOver = map[int]bool{}
	}
	return s
}

func (s *slotLocalState) save() error {
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(stateDir(), 0700); err != nil {
		return err
	}
	return os.WriteFile(slotLocalPath(), data, 0600)
}
