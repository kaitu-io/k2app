package main

import (
	"strings"
	"testing"
)

func TestDaemonNotRunningHint_ContainsSetup(t *testing.T) {
	hint := daemonNotRunningHint()
	if !strings.Contains(hint, "k2 setup") {
		t.Errorf("expected hint to contain %q, got: %s", "k2 setup", hint)
	}
}

func TestDaemonNotRunningHint_ContainsPlatformCmd(t *testing.T) {
	hint := daemonNotRunningHint()
	platformCmds := []string{"launchctl", "systemctl", "sc start"}
	found := false
	for _, cmd := range platformCmds {
		if strings.Contains(hint, cmd) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected hint to contain a platform service command (one of %v), got: %s", platformCmds, hint)
	}
}
