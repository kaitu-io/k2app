package main

import (
	"strings"
	"testing"
)

// TestDaemonNotRunningHint_ContainsDaemonNotRunning verifies the hint contains
// a clear "daemon not running" message.
func TestDaemonNotRunningHint_ContainsDaemonNotRunning(t *testing.T) {
	hint := daemonNotRunningHint()
	if !strings.Contains(hint, "daemon not running") {
		t.Errorf("expected hint to contain %q, got:\n%s", "daemon not running", hint)
	}
}

// TestDaemonNotRunningHint_ContainsSetup verifies the hint mentions the k2 setup command
// so first-time users know how to get started.
func TestDaemonNotRunningHint_ContainsSetup(t *testing.T) {
	hint := daemonNotRunningHint()
	if !strings.Contains(hint, "k2 setup") {
		t.Errorf("expected hint to contain %q, got:\n%s", "k2 setup", hint)
	}
}

// TestDaemonNotRunningHint_ContainsPlatformCmd verifies the hint contains the
// platform-specific service start command. Each platform's build-tag file provides
// the relevant command (launchctl on darwin, systemctl on linux, sc start on windows).
func TestDaemonNotRunningHint_ContainsPlatformCmd(t *testing.T) {
	hint := daemonNotRunningHint()

	// Each platform file defines a different keyword; exactly one will be compiled in.
	platformCmds := []string{"launchctl", "systemctl", "sc start"}
	found := false
	for _, cmd := range platformCmds {
		if strings.Contains(hint, cmd) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected hint to contain a platform service command (one of %v), got:\n%s", platformCmds, hint)
	}
}
