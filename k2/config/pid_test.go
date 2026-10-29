package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestPIDWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.pid")

	if err := WritePID(path); err != nil {
		t.Fatalf("WritePID: %v", err)
	}

	pid, err := ReadPID(path)
	if err != nil {
		t.Fatalf("ReadPID after write: %v", err)
	}
	if pid <= 0 {
		t.Errorf("PID = %d, want > 0", pid)
	}
}

func TestPIDRead(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.pid")

	// Write a known PID value directly
	if err := os.WriteFile(path, []byte("12345\n"), 0644); err != nil {
		t.Fatal(err)
	}

	pid, err := ReadPID(path)
	if err != nil {
		t.Fatalf("ReadPID: %v", err)
	}
	if pid != 12345 {
		t.Errorf("PID = %d, want 12345", pid)
	}
}

func TestPIDRead_Missing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nonexistent.pid")

	pid, err := ReadPID(path)
	if err != nil {
		t.Fatalf("ReadPID on missing file: expected nil error, got %v", err)
	}
	if pid != 0 {
		t.Errorf("PID = %d, want 0 for missing file", pid)
	}
}

func TestPIDStale(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stale.pid")

	// Write a PID that almost certainly does not exist (very large number)
	if err := os.WriteFile(path, []byte("999999999\n"), 0644); err != nil {
		t.Fatal(err)
	}

	pid, err := CheckStale(path)
	if err != nil {
		t.Fatalf("CheckStale: %v", err)
	}
	// Should return the stale PID
	if pid != 999999999 {
		t.Errorf("stale PID = %d, want 999999999", pid)
	}
	// File should be cleaned up
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Error("stale PID file should have been removed")
	}
}

func TestDefaultPIDPath(t *testing.T) {
	path := DefaultPIDPath("k2")
	if runtime.GOOS == "windows" {
		// Windows: PID file disabled, should return "".
		if path != "" {
			t.Errorf("DefaultPIDPath on Windows = %q, want empty", path)
		}
		return
	}
	// Unix: should return a non-empty path containing the name.
	if path == "" {
		t.Fatal("DefaultPIDPath returned empty string")
	}
	if !containsPIDName(path, "k2") {
		t.Errorf("DefaultPIDPath = %q, should contain 'k2'", path)
	}
}

// containsPIDName checks that path contains the given name component.
func containsPIDName(path, name string) bool {
	for i := 0; i <= len(path)-len(name); i++ {
		if path[i:i+len(name)] == name {
			return true
		}
	}
	return false
}

func TestIsProcessAlive_Self(t *testing.T) {
	// Current process should always be alive — works on all platforms.
	pid := os.Getpid()
	if !IsProcessAlive(pid) {
		t.Errorf("IsProcessAlive(%d) = false for current process", pid)
	}
}

func TestIsProcessAlive_Dead(t *testing.T) {
	// A very large PID should not exist on any platform.
	if IsProcessAlive(999999999) {
		t.Error("IsProcessAlive(999999999) = true, expected false")
	}
}

func TestIsProcessAlive_Invalid(t *testing.T) {
	if IsProcessAlive(0) {
		t.Error("IsProcessAlive(0) = true, expected false")
	}
	if IsProcessAlive(-1) {
		t.Error("IsProcessAlive(-1) = true, expected false")
	}
}

func TestPIDCleanup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.pid")

	if err := WritePID(path); err != nil {
		t.Fatalf("WritePID: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("PID file should exist after WritePID")
	}

	RemovePID(path)

	// Verify file is gone
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("PID file should be removed after RemovePID")
	}
}
