//go:build linux

package gateway

import (
	"strconv"
	"strings"
	"testing"

	"github.com/kaitu-io/k2/config"
)

// ── anchor DNAT rule text-level assertions ──────────────────────────
//
// The anchor DNAT rule redirects forwarded traffic addressed to the
// well-known anchor IP:port (config.RouterAnchorIP:RouterAnchorPort) to the
// local control API port, regardless of user config (LAN subnets, DNS
// redirect, MAC allowlist). It is the sole path an app behind arbitrary NAT
// layers uses to discover/control k2r. See config/anchor.go.

func TestAnchorIptCommands_ContainsAnchorAddrPortAndRedirect(t *testing.T) {
	cmds := anchorIptCommands()
	joined := flattenCmds(cmds)

	if !strings.Contains(joined, config.RouterAnchorIP) {
		t.Errorf("missing anchor IP %s in: %s", config.RouterAnchorIP, joined)
	}
	if !strings.Contains(joined, strconv.Itoa(config.RouterAnchorPort)) {
		t.Errorf("missing anchor port %d in: %s", config.RouterAnchorPort, joined)
	}
	if !strings.Contains(joined, "-j REDIRECT") {
		t.Errorf("expected REDIRECT target, got: %s", joined)
	}
	if !strings.Contains(joined, "-p tcp") {
		t.Errorf("anchor rule must match tcp only, got: %s", joined)
	}
}

// The anchor chain must be unconditional — present regardless of
// LANSubnets/DNSRedirect/AllowlistMode — since it doesn't depend on any of
// that config.
func TestBuildIptCommands_IncludesAnchorRedirect_Unconditional(t *testing.T) {
	cmds, _, _ := buildIptCommands(InterceptConfig{
		ListenPort: 12345,
		// No LANSubnets, no DNSRedirect, no AllowlistMode.
	})
	joined := flattenCmds(cmds)
	if !strings.Contains(joined, config.RouterAnchorIP) {
		t.Errorf("anchor rule must be present even with a bare InterceptConfig; got: %s", joined)
	}
	if !strings.Contains(joined, "-j REDIRECT --to-ports "+strconv.Itoa(config.RouterAnchorPort)) {
		t.Errorf("missing anchor REDIRECT --to-ports %d; got: %s", config.RouterAnchorPort, joined)
	}
}

func TestAnchorNftLines_ContainsAnchorAddrPortAndRedirect(t *testing.T) {
	lines := anchorNftLines()
	joined := strings.Join(lines, "\n")

	if !strings.Contains(joined, config.RouterAnchorIP) {
		t.Errorf("missing anchor IP in: %s", joined)
	}
	if !strings.Contains(joined, "dport "+strconv.Itoa(config.RouterAnchorPort)) {
		t.Errorf("missing anchor dport in: %s", joined)
	}
	if !strings.Contains(joined, "redirect to :"+strconv.Itoa(config.RouterAnchorPort)) {
		t.Errorf("missing redirect target in: %s", joined)
	}
	if !strings.Contains(joined, "tcp") {
		t.Errorf("anchor rule must match tcp, got: %s", joined)
	}
}

// The anchor NAT chain must be created via a dedicated nat-hook chain, not
// stuffed into the mangle-table `prerouting` (filter) chain — REDIRECT is a
// NAT action, same constraint as dnsredir (see TestNftRuleGeneration_DNSRedirect).
func TestNftRuleGeneration_IncludesAnchorRedirect_Unconditional(t *testing.T) {
	nft := &nftShellInterceptor{}
	script := nft.buildScript(InterceptConfig{
		ListenPort: 12345,
		// No LANSubnets, no DNSRedirect.
	})
	if !strings.Contains(script, config.RouterAnchorIP) {
		t.Errorf("anchor rule must be present even with a bare InterceptConfig; got script:\n%s", script)
	}
	for _, line := range strings.Split(script, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "add rule inet k2r prerouting") && strings.Contains(trimmed, "redirect") {
			t.Errorf("anchor redirect rule must not be in the filter (prerouting) chain: %q", trimmed)
		}
	}
}

// ── idempotency: back-to-back Install() must not duplicate the anchor rule ──

func TestInstall_AnchorIdempotentBackToBack(t *testing.T) {
	f := newFakeIptables()
	f.install(t)
	i := &iptInterceptor{}
	cfg := InterceptConfig{ListenPort: 12345, LANSubnets: []string{"192.168.1.0/24"}}

	if err := i.Install(cfg); err != nil {
		t.Fatalf("first Install: %v", err)
	}
	if err := i.Install(cfg); err != nil {
		t.Fatalf("second Install must succeed, got: %v", err)
	}

	// Anchor chain must exist exactly once (K2R-family idempotent cleanup via
	// cleanK2RFamily's prefix scan), with exactly one PREROUTING jump ref —
	// not two, despite two Install() calls.
	if !f.chains["iptables:nat:K2R_ANCHOR"] {
		t.Fatal("K2R_ANCHOR chain must exist after Install")
	}
	if got := f.refs["iptables:nat:K2R_ANCHOR"]; got != 1 {
		t.Errorf("K2R_ANCHOR PREROUTING refs = %d, want 1 (must not accumulate across repeated Install)", got)
	}
}

// A crashed daemon that left the K2R_ANCHOR chain + jump behind must not
// block the next Install (mirrors TestInstall_RecoverFromStaleOrphanChain).
func TestInstall_AnchorRecoverFromStaleOrphanChain(t *testing.T) {
	f := newFakeIptables()
	f.seedChain("iptables", "nat", "K2R_ANCHOR", 1)
	f.install(t)
	i := &iptInterceptor{}
	cfg := InterceptConfig{ListenPort: 12345, LANSubnets: []string{"192.168.1.0/24"}}

	if err := i.Install(cfg); err != nil {
		t.Fatalf("Install over stale anchor orphan must succeed, got: %v", err)
	}
	if got := f.refs["iptables:nat:K2R_ANCHOR"]; got != 1 {
		t.Errorf("K2R_ANCHOR refs = %d, want 1 (stale refs must not survive)", got)
	}
}
