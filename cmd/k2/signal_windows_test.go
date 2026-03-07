//go:build windows

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRunSignal_RejectReload verifies that "reload" is rejected on Windows.
func TestRunSignal_RejectReload(t *testing.T) {
	var buf bytes.Buffer
	err := runSignal("reload", "/nonexistent/pid", &buf)
	if err == nil {
		t.Fatal("expected error for reload on Windows, got nil")
	}
	if !strings.Contains(err.Error(), "not supported on Windows") {
		t.Errorf("expected error to mention 'not supported on Windows', got: %q", err.Error())
	}
}

// TestRunSignal_UnknownSignal verifies that an unrecognized signal name is rejected.
func TestRunSignal_UnknownSignal(t *testing.T) {
	var buf bytes.Buffer
	err := runSignal("hup", "/nonexistent/pid", &buf)
	if err == nil {
		t.Fatal("expected error for unknown signal, got nil")
	}
	if !strings.Contains(err.Error(), "unknown signal") {
		t.Errorf("expected error to mention 'unknown signal', got: %q", err.Error())
	}
}

// TestRunSignal_MissingPIDFile verifies that a missing PID file produces an error
// mentioning "pid".
func TestRunSignal_MissingPIDFile(t *testing.T) {
	var buf bytes.Buffer
	pidPath := filepath.Join(t.TempDir(), "nonexistent.pid")
	err := runSignal("stop", pidPath, &buf)
	if err == nil {
		t.Fatal("expected error for missing PID file, got nil")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "pid") {
		t.Errorf("expected error to mention 'pid', got: %q", err.Error())
	}
}

// TestRunSignal_StalePID_Removed verifies that a stale PID (dead process) causes
// an error containing "not running" and that the PID file is cleaned up.
func TestRunSignal_StalePID_Removed(t *testing.T) {
	dir := t.TempDir()
	pidPath := filepath.Join(dir, "stale.pid")

	// Write a PID that is almost certainly not running.
	if err := os.WriteFile(pidPath, []byte("99999999\n"), 0644); err != nil {
		t.Fatalf("write pid file: %v", err)
	}

	var buf bytes.Buffer
	err := runSignal("stop", pidPath, &buf)
	if err == nil {
		t.Fatal("expected error for stale PID, got nil")
	}
	if !strings.Contains(err.Error(), "not running") {
		t.Errorf("expected error to mention 'not running', got: %q", err.Error())
	}

	// Verify PID file was removed.
	if _, statErr := os.Stat(pidPath); statErr == nil {
		t.Error("expected stale PID file to be removed, but it still exists")
	}
}
