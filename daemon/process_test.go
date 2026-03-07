package daemon

import (
	"os"
	"testing"
)

func TestProcessExists(t *testing.T) {
	// Current process should exist.
	if !processExists(os.Getpid()) {
		t.Fatal("expected current process to exist")
	}

	// PID 99999999 should not exist.
	if processExists(99999999) {
		t.Fatal("expected non-existent PID to return false")
	}
}

func TestShouldMonitorPID(t *testing.T) {
	// PID monitoring is supported on all platforms:
	// Unix uses Signal(0), Windows uses OpenProcess.
	if !shouldMonitorPID() {
		t.Fatal("shouldMonitorPID should be true on all platforms")
	}
}
