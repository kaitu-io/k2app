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
