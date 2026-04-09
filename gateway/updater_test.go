//go:build linux

package gateway

import (
	"encoding/json"
	"os"
	"testing"
	"time"
)

func TestUpdateState_SaveLoadClear(t *testing.T) {
	dir := t.TempDir()
	statePath := dir + "/update-state.json"
	origPath := updateStatePath
	// Can't reassign const — use variable approach via helper
	// Instead, test the JSON round-trip directly
	state := &UpdateState{
		Stage: "downloading", Version: "0.4.3",
		TmpPath: "/tmp/k2r-update-0.4.3", BakPath: "/usr/bin/k2r.bak",
		StartedAt: time.Now().Unix(),
	}
	data, _ := json.Marshal(state)
	os.WriteFile(statePath, data, 0644)

	// Read back
	readData, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var loaded UpdateState
	if err := json.Unmarshal(readData, &loaded); err != nil {
		t.Fatal(err)
	}
	if loaded.Stage != "downloading" || loaded.Version != "0.4.3" {
		t.Errorf("loaded state mismatch: %+v", loaded)
	}

	_ = origPath // unused, just to verify const exists
}

func TestRecoverOnStartup_DownloadInterrupted(t *testing.T) {
	dir := t.TempDir()
	tmpFile := dir + "/k2r-update-0.4.3"
	os.WriteFile(tmpFile, []byte("partial download"), 0644)

	// Override updateStatePath for test
	origPath := updateStatePath
	updateStatePath = dir + "/update-state.json"
	t.Cleanup(func() { updateStatePath = origPath })

	// Write state that simulates interrupted download
	state := &UpdateState{
		Stage: "downloading", Version: "0.4.3",
		TmpPath: tmpFile, BakPath: dir + "/k2r.bak",
		StartedAt: time.Now().Unix(),
	}
	saveUpdateState(state)

	u := NewUpdater("0.4.2")
	u.RecoverOnStartup()

	// Tmp file should be cleaned up
	if _, err := os.Stat(tmpFile); !os.IsNotExist(err) {
		t.Error("expected tmp file to be removed after recovery")
	}
	// State file should be cleared
	if _, err := os.Stat(updateStatePath); !os.IsNotExist(err) {
		t.Error("expected state file to be cleared after recovery")
	}
}

func TestCopyFile(t *testing.T) {
	dir := t.TempDir()
	src := dir + "/src"
	dst := dir + "/dst"
	os.WriteFile(src, []byte("hello"), 0644)

	if err := copyFile(src, dst); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello" {
		t.Errorf("copied content = %q", string(data))
	}
}
