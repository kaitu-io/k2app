//go:build windows

package provider

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDNSStateFilePath_UsesProgramData(t *testing.T) {
	programData := os.Getenv("ProgramData")
	if programData == "" {
		t.Skip("ProgramData not set on this system")
	}

	path := dnsStateFilePath()

	if !strings.Contains(path, programData) {
		t.Errorf("path should contain ProgramData %q, got %q", programData, path)
	}
	if !strings.HasSuffix(path, "dns-state.json") {
		t.Errorf("path should end with dns-state.json, got %q", path)
	}
	if !strings.Contains(path, `\k2\`) && !strings.Contains(path, `/k2/`) {
		t.Errorf("path should contain k2 directory component, got %q", path)
	}
}

func TestDNSStateFilePath_FallbackWhenProgramDataEmpty(t *testing.T) {
	original := os.Getenv("ProgramData")
	defer os.Setenv("ProgramData", original)

	os.Setenv("ProgramData", "")

	path := dnsStateFilePath()
	tmpDir := os.TempDir()

	if !strings.HasPrefix(path, tmpDir) {
		t.Errorf("path should start with TempDir %q, got %q", tmpDir, path)
	}
	if !strings.HasSuffix(path, "dns-state.json") {
		t.Errorf("path should end with dns-state.json, got %q", path)
	}
}

func TestSaveDNSState_WritesValidJSON(t *testing.T) {
	original := os.Getenv("ProgramData")
	defer os.Setenv("ProgramData", original)

	tmpDir := t.TempDir()
	os.Setenv("ProgramData", tmpDir)

	saveDNSState("sing-tun", "198.18.0.8")

	path := dnsStateFilePath()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read state file: %v", err)
	}

	var state windowsDNSState
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatalf("failed to unmarshal state: %v", err)
	}

	if state.InterfaceName != "sing-tun" {
		t.Errorf("InterfaceName = %q, want %q", state.InterfaceName, "sing-tun")
	}
	if state.DNSServer != "198.18.0.8" {
		t.Errorf("DNSServer = %q, want %q", state.DNSServer, "198.18.0.8")
	}
	if state.Timestamp.IsZero() {
		t.Error("Timestamp should not be zero")
	}
	if time.Since(state.Timestamp) > 5*time.Second {
		t.Errorf("Timestamp should be recent, got %v ago", time.Since(state.Timestamp))
	}
}

func TestRemoveDNSState_DeletesFile(t *testing.T) {
	original := os.Getenv("ProgramData")
	defer os.Setenv("ProgramData", original)

	tmpDir := t.TempDir()
	os.Setenv("ProgramData", tmpDir)

	saveDNSState("sing-tun", "198.18.0.8")

	path := dnsStateFilePath()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("state file should exist after save")
	}

	removeDNSState()

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("state file should not exist after remove")
	}
}

func TestCleanupStaleDNSOverride_NoMarker_NoOp(t *testing.T) {
	original := os.Getenv("ProgramData")
	defer os.Setenv("ProgramData", original)

	tmpDir := t.TempDir()
	os.Setenv("ProgramData", tmpDir)

	// No marker file exists — should not panic.
	CleanupStaleDNSOverride()
}

func TestCleanupStaleDNSOverride_OldMarker_Removed(t *testing.T) {
	original := os.Getenv("ProgramData")
	defer os.Setenv("ProgramData", original)

	tmpDir := t.TempDir()
	os.Setenv("ProgramData", tmpDir)

	state := windowsDNSState{
		InterfaceName: "sing-tun",
		DNSServer:     "198.18.0.8",
		Timestamp:     time.Now().Add(-25 * time.Hour),
	}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("failed to marshal state: %v", err)
	}

	path := dnsStateFilePath()
	os.MkdirAll(filepath.Dir(path), 0700)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatalf("failed to write marker: %v", err)
	}

	CleanupStaleDNSOverride()

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("old marker (>24h) should be removed after cleanup")
	}
}

func TestCleanupStaleDNSOverride_InvalidJSON_Removed(t *testing.T) {
	original := os.Getenv("ProgramData")
	defer os.Setenv("ProgramData", original)

	tmpDir := t.TempDir()
	os.Setenv("ProgramData", tmpDir)

	path := dnsStateFilePath()
	os.MkdirAll(filepath.Dir(path), 0700)
	if err := os.WriteFile(path, []byte("not json"), 0600); err != nil {
		t.Fatalf("failed to write invalid marker: %v", err)
	}

	CleanupStaleDNSOverride()

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("invalid JSON marker should be removed after cleanup")
	}
}

func TestCleanupStaleDNSOverride_FreshMarker_InterfaceGone_Removed(t *testing.T) {
	original := os.Getenv("ProgramData")
	defer os.Setenv("ProgramData", original)

	tmpDir := t.TempDir()
	os.Setenv("ProgramData", tmpDir)

	state := windowsDNSState{
		InterfaceName: "nonexistent-test-iface-12345",
		DNSServer:     "198.18.0.8",
		Timestamp:     time.Now(),
	}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("failed to marshal state: %v", err)
	}

	path := dnsStateFilePath()
	os.MkdirAll(filepath.Dir(path), 0700)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatalf("failed to write marker: %v", err)
	}

	CleanupStaleDNSOverride()

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("marker with nonexistent interface should be removed after cleanup")
	}
}
