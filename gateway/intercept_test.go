//go:build linux

package gateway

import (
	"strings"
	"testing"
)

// ── nftables rule generation ─────────────────────────────────────

func TestNftRuleGeneration(t *testing.T) {
	nft := &nftShellInterceptor{}
	script := nft.buildScript(InterceptConfig{
		ListenPort: 12345,
		LANSubnets: []string{"192.168.1.0/24", "10.0.0.0/8"},
		ExcludeIPs: []string{"1.2.3.4", "8.8.8.8"},
	})
	if !strings.Contains(script, "table inet k2r") {
		t.Error("missing table inet k2r")
	}
	if !strings.Contains(script, "ip saddr 192.168.1.0/24") {
		t.Error("missing first LAN subnet")
	}
	if !strings.Contains(script, "ip saddr 10.0.0.0/8") {
		t.Error("missing second LAN subnet")
	}
	if !strings.Contains(script, "1.2.3.4") {
		t.Error("missing exclude IP")
	}
	if !strings.Contains(script, ":12345") {
		t.Error("missing TPROXY port")
	}
	if !strings.Contains(script, "meta mark set 1") {
		t.Error("missing mark set")
	}
}

func TestNftRuleGeneration_IPv6(t *testing.T) {
	nft := &nftShellInterceptor{}
	script := nft.buildScript(InterceptConfig{
		ListenPort: 12345,
		LANSubnets: []string{"fd00::/64"},
		ExcludeIPs: []string{"2001:db8::1"},
	})
	if !strings.Contains(script, "ip6 saddr fd00::/64") {
		t.Error("missing IPv6 LAN subnet")
	}
	if !strings.Contains(script, "2001:db8::1") {
		t.Error("missing IPv6 exclude IP")
	}
}

func TestNftRuleGeneration_EmptyExclude(t *testing.T) {
	nft := &nftShellInterceptor{}
	script := nft.buildScript(InterceptConfig{
		ListenPort: 12345,
		LANSubnets: []string{"192.168.1.0/24"},
		ExcludeIPs: nil,
	})
	if strings.Contains(script, "exclude_v4") {
		t.Error("should not have exclude_v4 set with no excludes")
	}
}

func TestNftRuleGeneration_DualStack(t *testing.T) {
	nft := &nftShellInterceptor{}
	script := nft.buildScript(InterceptConfig{
		ListenPort: 8080,
		LANSubnets: []string{"192.168.1.0/24", "fd00::/64"},
		ExcludeIPs: []string{"1.2.3.4", "2001:db8::1"},
	})
	// IPv4 rules.
	if !strings.Contains(script, "ip saddr 192.168.1.0/24") {
		t.Error("missing IPv4 subnet rule")
	}
	if !strings.Contains(script, "tproxy ip to :8080") {
		t.Error("missing tproxy ip to :port for IPv4")
	}
	// IPv6 rules.
	if !strings.Contains(script, "ip6 saddr fd00::/64") {
		t.Error("missing IPv6 subnet rule")
	}
	if !strings.Contains(script, "tproxy ip6 to :8080") {
		t.Error("missing tproxy ip6 to :port for IPv6")
	}
	// Exclude sets for both families.
	if !strings.Contains(script, "exclude_v4") {
		t.Error("missing exclude_v4 set")
	}
	if !strings.Contains(script, "exclude_v6") {
		t.Error("missing exclude_v6 set")
	}
	// Loopback bypass.
	if !strings.Contains(script, `iifname "lo" return`) {
		t.Error("missing loopback return rule")
	}
	// Both TCP and UDP.
	if strings.Count(script, "l4proto tcp") < 2 {
		t.Error("should have TCP rules for both v4 and v6")
	}
	if strings.Count(script, "l4proto udp") < 2 {
		t.Error("should have UDP rules for both v4 and v6")
	}
}

func TestNftRuleGeneration_TPROXYSyntax(t *testing.T) {
	nft := &nftShellInterceptor{}
	// Verify nft uses "tproxy ip to" not bare "tproxy to" (inet family requires it).
	script := nft.buildScript(InterceptConfig{
		ListenPort: 1234,
		LANSubnets: []string{"10.0.0.0/8"},
	})
	if strings.Contains(script, "tproxy to :") {
		t.Error("bare 'tproxy to :port' is invalid in inet family — must use 'tproxy ip to' or 'tproxy ip6 to'")
	}
}

func TestNftRuleGeneration_MultipleSubnets(t *testing.T) {
	nft := &nftShellInterceptor{}
	script := nft.buildScript(InterceptConfig{
		ListenPort: 12345,
		LANSubnets: []string{"192.168.1.0/24", "192.168.2.0/24", "10.0.0.0/8"},
	})
	// Each subnet gets TCP + UDP = 2 rules.
	if strings.Count(script, "ip saddr 192.168.1.0/24") != 2 {
		t.Error("192.168.1.0/24 should appear in TCP + UDP rules")
	}
	if strings.Count(script, "ip saddr 192.168.2.0/24") != 2 {
		t.Error("192.168.2.0/24 should appear in TCP + UDP rules")
	}
	if strings.Count(script, "ip saddr 10.0.0.0/8") != 2 {
		t.Error("10.0.0.0/8 should appear in TCP + UDP rules")
	}
}

// ── iptables rule generation ─────────────────────────────────────

func TestIptRuleGeneration(t *testing.T) {
	cmds, _, _ := buildIptCommands(InterceptConfig{
		ListenPort: 12345,
		LANSubnets: []string{"192.168.1.0/24"},
		ExcludeIPs: []string{"1.2.3.4", "8.8.8.8"},
	})
	joined := flattenCmds(cmds)
	if !strings.Contains(joined, "-N K2R") {
		t.Error("missing chain creation")
	}
	if !strings.Contains(joined, "-d 1.2.3.4 -j RETURN") {
		t.Error("missing exclude rule")
	}
	if !strings.Contains(joined, "--on-port 12345 --tproxy-mark 1") {
		t.Error("missing TPROXY rule")
	}
	if !strings.Contains(joined, "-A PREROUTING -s 192.168.1.0/24 -j K2R") {
		t.Error("missing PREROUTING jump")
	}
}

func TestIptRuleGeneration_IPv6(t *testing.T) {
	cmds, _, _ := buildIptCommands(InterceptConfig{
		ListenPort: 12345,
		LANSubnets: []string{"fd00::/64"},
		ExcludeIPs: []string{"2001:db8::1"},
	})
	joined := flattenCmds(cmds)
	if !strings.Contains(joined, "ip6tables") {
		t.Error("missing ip6tables commands")
	}
	if !strings.Contains(joined, "fd00::/64") {
		t.Error("missing IPv6 subnet")
	}
}

func TestIptRuleGeneration_DualStack(t *testing.T) {
	cmds, v4Sub, v6Sub := buildIptCommands(InterceptConfig{
		ListenPort: 8080,
		LANSubnets: []string{"192.168.1.0/24", "fd00::/64"},
		ExcludeIPs: []string{"1.2.3.4", "2001:db8::1"},
	})
	if len(v4Sub) != 1 || v4Sub[0] != "192.168.1.0/24" {
		t.Errorf("v4Subnets = %v, want [192.168.1.0/24]", v4Sub)
	}
	if len(v6Sub) != 1 || v6Sub[0] != "fd00::/64" {
		t.Errorf("v6Subnets = %v, want [fd00::/64]", v6Sub)
	}
	joined := flattenCmds(cmds)
	// Both iptables and ip6tables chains.
	if !strings.Contains(joined, "iptables -t mangle -N K2R") {
		t.Error("missing iptables chain creation")
	}
	if !strings.Contains(joined, "ip6tables -t mangle -N K2R") {
		t.Error("missing ip6tables chain creation")
	}
	// Exclude rules in correct tool.
	if !strings.Contains(joined, "iptables -t mangle -A K2R -d 1.2.3.4 -j RETURN") {
		t.Error("missing iptables IPv4 exclude")
	}
	if !strings.Contains(joined, "ip6tables -t mangle -A K2R -d 2001:db8::1 -j RETURN") {
		t.Error("missing ip6tables IPv6 exclude")
	}
	// ip rule + route for both families.
	if !strings.Contains(joined, "ip rule add fwmark 1 table 100") {
		t.Error("missing IPv4 ip rule")
	}
	if !strings.Contains(joined, "ip -6 rule add fwmark 1 table 100") {
		t.Error("missing IPv6 ip rule")
	}
}

func TestIptRuleGeneration_NoIPv4(t *testing.T) {
	cmds, v4Sub, v6Sub := buildIptCommands(InterceptConfig{
		ListenPort: 12345,
		LANSubnets: []string{"fd00::/64"},
	})
	if len(v4Sub) != 0 {
		t.Errorf("v4Subnets should be empty, got %v", v4Sub)
	}
	if len(v6Sub) != 1 {
		t.Errorf("v6Subnets should have 1, got %v", v6Sub)
	}
	joined := flattenCmds(cmds)
	if strings.Contains(joined, "iptables -t mangle -N K2R") {
		t.Error("should not create iptables chain when no IPv4 subnets")
	}
	if !strings.Contains(joined, "ip6tables -t mangle -N K2R") {
		t.Error("missing ip6tables chain for IPv6-only")
	}
}

func TestIptRuleGeneration_EmptyExclude(t *testing.T) {
	cmds, _, _ := buildIptCommands(InterceptConfig{
		ListenPort: 12345,
		LANSubnets: []string{"192.168.1.0/24"},
	})
	joined := flattenCmds(cmds)
	if strings.Contains(joined, "RETURN") {
		t.Error("should not have RETURN rules with no excludes")
	}
}

// ── IP/subnet family splitting ───────────────────────────────────

func TestSplitIPsByFamily(t *testing.T) {
	v4, v6 := splitIPsByFamily([]string{"1.2.3.4", "::1", "8.8.8.8", "2001:db8::1", "not-an-ip"})
	if len(v4) != 2 {
		t.Errorf("v4 = %v, want 2 IPs", v4)
	}
	if len(v6) != 2 {
		t.Errorf("v6 = %v, want 2 IPs", v6)
	}
}

func TestSplitIPsByFamily_Empty(t *testing.T) {
	v4, v6 := splitIPsByFamily(nil)
	if len(v4) != 0 || len(v6) != 0 {
		t.Errorf("expected empty, got v4=%v v6=%v", v4, v6)
	}
}

func TestSplitSubnetsByFamily(t *testing.T) {
	v4, v6 := splitSubnetsByFamily([]string{"192.168.1.0/24", "fd00::/64", "10.0.0.0/8", "invalid"})
	if len(v4) != 2 {
		t.Errorf("v4 = %v, want 2 subnets", v4)
	}
	if len(v6) != 1 {
		t.Errorf("v6 = %v, want 1 subnet", v6)
	}
}

func TestSplitSubnetsByFamily_Empty(t *testing.T) {
	v4, v6 := splitSubnetsByFamily(nil)
	if len(v4) != 0 || len(v6) != 0 {
		t.Errorf("expected empty, got v4=%v v6=%v", v4, v6)
	}
}

// ── DNS redirect + MAC allowlist tests ──────────────────────────

func TestNftRuleGeneration_DNSRedirect(t *testing.T) {
	nft := &nftShellInterceptor{}
	script := nft.buildScript(InterceptConfig{
		ListenPort:  12345,
		LANSubnets:  []string{"192.168.1.0/24"},
		DNSRedirect: true,
		DNSPort:     12346,
	})
	if !strings.Contains(script, "th dport 53 redirect to :12346") {
		t.Error("missing DNS redirect rule")
	}
}

func TestNftRuleGeneration_DNSRedirect_Disabled(t *testing.T) {
	nft := &nftShellInterceptor{}
	script := nft.buildScript(InterceptConfig{
		ListenPort: 12345,
		LANSubnets: []string{"192.168.1.0/24"},
	})
	if strings.Contains(script, "redirect") {
		t.Error("should not have redirect rule when DNSRedirect is false")
	}
}

func TestNftRuleGeneration_MACAllowlist(t *testing.T) {
	nft := &nftShellInterceptor{}
	script := nft.buildScript(InterceptConfig{
		ListenPort:    12345,
		LANSubnets:    []string{"192.168.1.0/24"},
		AllowlistMode: true,
		AllowedMACs:   []string{"AA:BB:CC:DD:EE:FF", "11:22:33:44:55:66"},
	})
	if !strings.Contains(script, "allowed_macs") {
		t.Error("missing allowed_macs set")
	}
	if !strings.Contains(script, "AA:BB:CC:DD:EE:FF") {
		t.Error("missing MAC in set")
	}
	if !strings.Contains(script, "ether saddr != @allowed_macs drop") {
		t.Error("missing MAC drop rule")
	}
}

func TestNftRuleGeneration_MACAllowlist_Disabled(t *testing.T) {
	nft := &nftShellInterceptor{}
	script := nft.buildScript(InterceptConfig{
		ListenPort: 12345,
		LANSubnets: []string{"192.168.1.0/24"},
	})
	if strings.Contains(script, "allowed_macs") {
		t.Error("should not have MAC set when AllowlistMode is false")
	}
}

func TestIptRuleGeneration_DNSRedirect(t *testing.T) {
	cmds, _, _ := buildIptCommands(InterceptConfig{
		ListenPort:  12345,
		LANSubnets:  []string{"192.168.1.0/24"},
		DNSRedirect: true,
		DNSPort:     12346,
	})
	joined := flattenCmds(cmds)
	if !strings.Contains(joined, "-t nat -N K2R_DNS") {
		t.Error("missing DNS nat chain creation")
	}
	if !strings.Contains(joined, "--dport 53 -j REDIRECT --to-ports 12346") {
		t.Error("missing DNS redirect rule")
	}
}

func TestIptRuleGeneration_MACAllowlist(t *testing.T) {
	cmds, _, _ := buildIptCommands(InterceptConfig{
		ListenPort:    12345,
		LANSubnets:    []string{"192.168.1.0/24"},
		AllowlistMode: true,
		AllowedMACs:   []string{"AA:BB:CC:DD:EE:FF"},
	})
	joined := flattenCmds(cmds)
	if !strings.Contains(joined, "--mac-source AA:BB:CC:DD:EE:FF") {
		t.Error("missing MAC filter rule")
	}
}

// ── helpers ──────────────────────────────────────────────────────

func flattenCmds(cmds []cmdSpec) string {
	var flat []string
	for _, c := range cmds {
		flat = append(flat, c.name+" "+strings.Join(c.args, " "))
	}
	return strings.Join(flat, "\n")
}
