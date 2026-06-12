//go:build linux

package gateway

import (
	"strings"
	"testing"
)

// Bug 6 regression: Gateway.Run must NOT eagerly remove pre-existing iptables
// rules at startup. Doing so creates a multi-second LAN-escape window between
// daemon startup and the first successful Install (auto-reconnect's 3 s wait
// + transport race ~3-5 s = ~8 s confirmed 2026-05-21 on GL-XE300 MIPS).
//
// Install's pre-flight Remove (intercept_ipt.go) handles stale state at the
// correct time (when a fresh chain is about to replace the old one), so the
// startup cleanup is both redundant AND harmful.
//
// Contract: startupRecovery() must NOT touch K2R-family rules. Anything that
// needs stale-state cleanup goes through the doUp / Install path so the new
// chain replaces the old one back-to-back (microsecond window) instead of
// leaving an empty interval.
func TestGateway_StartupRecovery_PreservesPreExistingRules(t *testing.T) {
	f := newFakeIptables()
	// Pre-seed: previous daemon installed a complete K2R chain
	// (chain + 2 PREROUTING refs, like our GL-XE300 baseline).
	f.seedChain("iptables", "mangle", "K2R", 2)
	f.install(t)

	g := New("test", "test", "amd64", "")
	g.startupRecovery()

	if !f.chains["iptables:mangle:K2R"] {
		t.Errorf("Bug 6 regression: startupRecovery removed pre-existing K2R chain; calls:\n%s",
			strings.Join(f.calls, "\n"))
	}
	if got := f.refs["iptables:mangle:K2R"]; got != 2 {
		t.Errorf("Bug 6 regression: startupRecovery dropped PREROUTING refs, got %d want 2; calls:\n%s",
			got, strings.Join(f.calls, "\n"))
	}
	// Stronger: no -D / -F / -X command of any kind should have been issued.
	for _, c := range f.calls {
		if strings.Contains(c, " -D ") || strings.Contains(c, " -F ") || strings.Contains(c, " -X ") {
			t.Errorf("Bug 6 regression: startupRecovery issued teardown command: %q", c)
		}
	}
}
