package daemon

import (
	"os"
	"runtime"
	"testing"
)

func TestProcessExists(t *testing.T) {
	// Current process should exist.
	if !processExists(os.Getpid()) {
		t.Fatal("expected current process to exist")
	}

	if runtime.GOOS == "windows" {
		// Windows stub always returns true (PID monitoring disabled).
		return
	}

	// PID 99999999 should not exist.
	if processExists(99999999) {
		t.Fatal("expected non-existent PID to return false")
	}
}

func TestShouldMonitorPID(t *testing.T) {
	got := shouldMonitorPID()
	if runtime.GOOS == "windows" {
		if got {
			t.Fatal("shouldMonitorPID should be false on Windows")
		}
	} else {
		if !got {
			t.Fatal("shouldMonitorPID should be true on Unix")
		}
	}
}
