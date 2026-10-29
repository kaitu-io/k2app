//go:build linux

package main

import (
	"strings"
	"testing"
)

// Bug 7 regression: bare `procd_set_param respawn` inherits procd's default
// retry=5 — after 5 crashes within 3600 s, procd marks the service dead and
// never respawns it. For a VPN gateway this is fail-OPEN: no daemon → no
// K2R chain → LAN traffic escapes to WAN.
//
// Verified 2026-05-21 on GL-XE300 MIPS: 5x `kill -9` left the service in
// `running=false` state with no further respawn. Conversely, retry=0 ran 15
// respawns in 15 seconds (procd's documented "infinite" sentinel).
//
// Fix: emit `procd_set_param respawn 3600 5 0` (threshold 1 h, term grace 5 s,
// retry count 0 = infinite).
func TestInstallOpenWRT_GeneratesInfiniteRetryRespawn(t *testing.T) {
	script := generateOpenWRTInitScript("/usr/bin/k2r")

	want := "procd_set_param respawn 3600 5 0"
	if !strings.Contains(script, want) {
		t.Errorf("init.d script must contain %q (Bug 7 fix); got:\n%s", want, script)
	}

	// Negative check: bare `procd_set_param respawn` with no args silently
	// re-enables the default retry=5 behavior.
	for _, line := range strings.Split(script, "\n") {
		if strings.TrimSpace(line) == "procd_set_param respawn" {
			t.Errorf("Bug 7 regression: bare 'procd_set_param respawn' present (defaults retry=5):\n%s", script)
		}
	}
}

// The init.d script must reference the exact binary path passed in — k2r
// upgrades rewrite this and the wrong path would silently start a stale
// binary.
func TestGenerateOpenWRTInitScript_UsesProvidedBinaryPath(t *testing.T) {
	script := generateOpenWRTInitScript("/opt/custom/k2r")
	if !strings.Contains(script, "/opt/custom/k2r run") {
		t.Errorf("script must use provided binary path; got:\n%s", script)
	}
}
