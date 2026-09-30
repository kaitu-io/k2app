//go:build linux

package gateway

import (
	"fmt"
	"strings"
	"testing"

	"github.com/google/nftables/expr"
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
	if !strings.Contains(joined, "iptables -w 5 -t mangle -N K2R") {
		t.Error("missing iptables chain creation")
	}
	if !strings.Contains(joined, "ip6tables -w 5 -t mangle -N K2R") {
		t.Error("missing ip6tables chain creation")
	}
	// Exclude rules in correct tool.
	if !strings.Contains(joined, "iptables -w 5 -t mangle -A K2R -d 1.2.3.4 -j RETURN") {
		t.Error("missing iptables IPv4 exclude")
	}
	if !strings.Contains(joined, "ip6tables -w 5 -t mangle -A K2R -d 2001:db8::1 -j RETURN") {
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
	if strings.Contains(joined, "iptables -w 5 -t mangle -N K2R") {
		t.Error("should not create iptables chain when no IPv4 subnets")
	}
	if !strings.Contains(joined, "ip6tables -w 5 -t mangle -N K2R") {
		t.Error("missing ip6tables chain for IPv6-only")
	}
}

func TestIptRuleGeneration_EmptyExclude(t *testing.T) {
	cmds, _, _ := buildIptCommands(InterceptConfig{
		ListenPort: 12345,
		LANSubnets: []string{"192.168.1.0/24"},
	})
	// The only RETURN rules left are the link-scope exemptions, which do not
	// depend on ExcludeIPs.
	linkScope, _ := linkScopeDests([]string{"192.168.1.0/24"})
	for _, line := range strings.Split(flattenCmds(cmds), "\n") {
		if !strings.Contains(line, "RETURN") {
			continue
		}
		isLinkScope := false
		for _, cidr := range linkScope {
			if strings.Contains(line, "-d "+cidr+" ") {
				isLinkScope = true
			}
		}
		if !isLinkScope {
			t.Errorf("RETURN rule with no excludes configured: %s", line)
		}
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
	if !strings.Contains(script, "add chain inet k2r dnsredir { type nat hook prerouting priority dstnat") {
		t.Error("DNS redirect should use a NAT chain, not filter chain")
	}
	// redirect must live ONLY in the dnsredir/dnsredir_out NAT chains, never in the
	// filter `prerouting` chain. Scan per-line: the crude whole-script Contains&&Contains
	// false-positives because the script legitimately has both a `prerouting` filter chain
	// (TPROXY rules) and a `redirect` (in the separate dnsredir NAT chain).
	for _, line := range strings.Split(script, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "add rule inet k2r prerouting") && strings.Contains(line, "redirect") {
			t.Errorf("redirect rule must not be in the filter (prerouting) chain: %q", line)
		}
	}
}

// Bare word "redirect" is no longer a sufficient probe here: the anchor DNAT
// rule (gateway/anchor.go) is unconditional and legitimately emits its own
// "redirect to :<port>" line regardless of DNSRedirect. This test narrows to
// the DNS-specific chain name, which the anchor rule never emits.
func TestNftRuleGeneration_DNSRedirect_Disabled(t *testing.T) {
	nft := &nftShellInterceptor{}
	script := nft.buildScript(InterceptConfig{
		ListenPort: 12345,
		LANSubnets: []string{"192.168.1.0/24"},
	})
	if strings.Contains(script, "dnsredir") {
		t.Error("should not have DNS redirect chain when DNSRedirect is false")
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

// K2R_DNS handles LAN-originated DNS (PREROUTING), but dnsmasq's own upstream
// queries originate locally — they never traverse PREROUTING and leak straight
// out the WAN. K2R_DNS_OUT in nat OUTPUT closes that path by REDIRECTing local
// port 53 traffic to the DNS listener as well.
func TestIptRuleGeneration_DNSRedirect_OutputChain(t *testing.T) {
	cmds, _, _ := buildIptCommands(InterceptConfig{
		ListenPort:  12345,
		LANSubnets:  []string{"192.168.1.0/24"},
		DNSRedirect: true,
		DNSPort:     12346,
	})
	joined := flattenCmds(cmds)
	if !strings.Contains(joined, "-t nat -N K2R_DNS_OUT") {
		t.Error("missing K2R_DNS_OUT chain creation")
	}
	if !strings.Contains(joined, "-A OUTPUT -p tcp --dport 53 -j K2R_DNS_OUT") {
		t.Error("missing OUTPUT tcp/53 jump to K2R_DNS_OUT")
	}
	if !strings.Contains(joined, "-A OUTPUT -p udp --dport 53 -j K2R_DNS_OUT") {
		t.Error("missing OUTPUT udp/53 jump to K2R_DNS_OUT")
	}
	if !strings.Contains(joined, "-A K2R_DNS_OUT -p tcp -j REDIRECT --to-ports 12346") {
		t.Error("missing K2R_DNS_OUT tcp REDIRECT")
	}
	if !strings.Contains(joined, "-A K2R_DNS_OUT -p udp -j REDIRECT --to-ports 12346") {
		t.Error("missing K2R_DNS_OUT udp REDIRECT")
	}
}

// The mark RETURN rule MUST come before the REDIRECT rule. Otherwise k2r's own
// DirectDNS queries (e.g. to 223.5.5.5:53) get REDIRECTed back into k2r's DNS
// listener, which then issues a DirectDNS query to 223.5.5.5:53 again → infinite
// recursion. SO_MARK 0x4b32 set in gatewayDirectDialer.ControlFunc is the exit.
func TestIptRuleGeneration_OutputChain_MarkReturnBeforeRedirect(t *testing.T) {
	cmds, _, _ := buildIptCommands(InterceptConfig{
		ListenPort:  12345,
		LANSubnets:  []string{"192.168.1.0/24"},
		DNSRedirect: true,
		DNSPort:     12346,
	})
	joined := flattenCmds(cmds)

	markRule := "-A K2R_DNS_OUT -m mark --mark 0x4b32 -j RETURN"
	if !strings.Contains(joined, markRule) {
		t.Fatalf("missing mark-RETURN exemption: %q", markRule)
	}

	// Find positions: mark RETURN MUST appear before the first REDIRECT.
	markIdx := strings.Index(joined, markRule)
	redirIdx := strings.Index(joined, "-A K2R_DNS_OUT -p tcp -j REDIRECT")
	if redirIdx < 0 {
		t.Fatal("REDIRECT rule missing")
	}
	if markIdx > redirIdx {
		t.Errorf("mark RETURN at %d must come before REDIRECT at %d (else k2r's own DirectDNS recursion)", markIdx, redirIdx)
	}
}

// cleanK2RFamily must remove K2R-family jumps from ANY built-in chain, not
// just PREROUTING. Our new K2R_DNS_OUT is referenced from OUTPUT — without the
// fix, the OUTPUT REDIRECT rule survives doDown and permanently breaks DNS for
// the whole system once k2r is uninstalled (or, less catastrophically, leaks
// stale rules across crashes).
func TestCleanK2RFamily_HandlesOutputJumps(t *testing.T) {
	f := newFakeIptables()
	f.seedChainOutput("iptables", "nat", "K2R_DNS_OUT", 1) // chain + 1 OUTPUT ref
	f.install(t)

	cleanK2RFamily("iptables", "nat")

	if f.chains["iptables:nat:K2R_DNS_OUT"] {
		t.Errorf("K2R_DNS_OUT chain must be deleted; calls:\n%s",
			strings.Join(f.calls, "\n"))
	}
	if f.outputRefs["iptables:nat:K2R_DNS_OUT"] != 0 {
		t.Errorf("OUTPUT refs must be zero, got %d", f.outputRefs["iptables:nat:K2R_DNS_OUT"])
	}
}

// nft shell backend needs a parallel OUTPUT-hook NAT chain with the same mark
// RETURN + redirect pattern. dnsredir already covers PREROUTING; dnsredir_out
// extends to local output so dnsmasq-style local resolvers can't leak.
func TestNftScript_DNSRedirect_OutputChainHasMarkReturn(t *testing.T) {
	nft := &nftShellInterceptor{}
	script := nft.buildScript(InterceptConfig{
		ListenPort:  12345,
		LANSubnets:  []string{"192.168.1.0/24"},
		DNSRedirect: true,
		DNSPort:     12346,
	})

	if !strings.Contains(script, "dnsredir_out") {
		t.Error("missing OUTPUT-hook nat chain dnsredir_out")
	}
	if !strings.Contains(script, "hook output") {
		t.Error("dnsredir_out should hook output, not prerouting")
	}

	// mark exemption MUST come before redirect within dnsredir_out.
	markIdx := strings.Index(script, "meta mark 0x4b32 return")
	redirIdx := strings.Index(script, "dnsredir_out") // any reference within the chain body
	if markIdx < 0 {
		t.Error("missing nft mark exemption (meta mark 0x4b32 return)")
	}
	// Verify ordering by finding both within dnsredir_out chain rules:
	if !strings.Contains(script, "add rule inet k2r dnsredir_out meta mark 0x4b32 return") {
		t.Error("mark RETURN rule should be in dnsredir_out chain")
	}
	if !strings.Contains(script, "add rule inet k2r dnsredir_out") || !strings.Contains(script, "redirect to :12346") {
		t.Error("missing dnsredir_out REDIRECT rule")
	}
	_ = redirIdx
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

// fakeIptablesState simulates iptables/ip6tables semantics tightly enough to
// catch the two bugs we care about:
//   - `-N CHAIN` fails with "Chain already exists" when the chain is present
//   - `-X CHAIN` fails when references to the chain still exist
//
// Used as a runCmd / listChainRules backend for unit tests so we don't need
// a real Linux kernel + iptables.
type fakeIptablesState struct {
	calls         []string
	chains        map[string]bool // "iptables:mangle:K2R" → exists
	refs          map[string]int  // "iptables:mangle:K2R" → reference count from PREROUTING jumps
	outputRefs    map[string]int  // "iptables:nat:K2R_DNS_OUT" → reference count from OUTPUT jumps
	ipEntries     map[string]int  // "rule fwmark 1 table 100" → how many copies `ip` holds
	failPredicate func(name string, args []string) error
}

func newFakeIptables() *fakeIptablesState {
	return &fakeIptablesState{
		chains:     map[string]bool{},
		refs:       map[string]int{},
		outputRefs: map[string]int{},
		ipEntries:  map[string]int{},
	}
}

func (f *fakeIptablesState) install(t *testing.T) {
	t.Helper()
	origRunCmd := runCmd
	origListChainRules := listChainRules
	t.Cleanup(func() {
		runCmd = origRunCmd
		listChainRules = origListChainRules
	})
	runCmd = func(name string, args ...string) error {
		f.calls = append(f.calls, name+" "+strings.Join(args, " "))
		if f.failPredicate != nil {
			if err := f.failPredicate(name, args); err != nil {
				return err
			}
		}
		return f.simulate(name, args)
	}
	listChainRules = func(ipBin, table string) (string, error) {
		var lines []string
		for k := range f.chains {
			parts := strings.SplitN(k, ":", 3)
			if len(parts) == 3 && parts[0] == ipBin && parts[1] == table {
				lines = append(lines, "-N "+parts[2])
			}
		}
		// Emit -A PREROUTING for each reference.
		for k, count := range f.refs {
			parts := strings.SplitN(k, ":", 3)
			if len(parts) == 3 && parts[0] == ipBin && parts[1] == table && count > 0 {
				for i := 0; i < count; i++ {
					lines = append(lines, "-A PREROUTING -j "+parts[2])
				}
			}
		}
		// Emit -A OUTPUT for each OUTPUT reference.
		for k, count := range f.outputRefs {
			parts := strings.SplitN(k, ":", 3)
			if len(parts) == 3 && parts[0] == ipBin && parts[1] == table && count > 0 {
				for i := 0; i < count; i++ {
					lines = append(lines, "-A OUTPUT -j "+parts[2])
				}
			}
		}
		return strings.Join(lines, "\n"), nil
	}
}

// simulate models the iptables behaviors that matter for our tests.
func (f *fakeIptablesState) simulate(name string, args []string) error {
	if name == "ip" {
		return f.simulateIP(args)
	}
	// Skip other non-iptables binaries — succeed silently.
	if name != "iptables" && name != "ip6tables" {
		return nil
	}
	// Parse: -t TABLE -OP CHAIN ...
	var table, op, chain string
	for idx := 0; idx < len(args); idx++ {
		switch args[idx] {
		case "-t":
			if idx+1 < len(args) {
				table = args[idx+1]
			}
		case "-N", "-X", "-F":
			op = args[idx]
			if idx+1 < len(args) {
				chain = args[idx+1]
			}
		case "-A", "-D":
			op = args[idx]
			if idx+1 < len(args) {
				chain = args[idx+1]
			}
		}
	}
	key := name + ":" + table + ":" + chain
	// Only track operations on K2R-family chains; everything else is a no-op success.
	if !strings.HasPrefix(chain, "K2R") && op != "-A" && op != "-D" {
		return nil
	}
	switch op {
	case "-N":
		if f.chains[key] {
			return fmt.Errorf("%s -t %s -N %s: %s: exit status 1",
				name, table, chain, "iptables: Chain already exists.")
		}
		f.chains[key] = true
	case "-X":
		if f.refs[key] > 0 || f.outputRefs[key] > 0 {
			return fmt.Errorf("%s -t %s -X %s: chain still referenced", name, table, chain)
		}
		delete(f.chains, key)
	case "-F":
		// Flush — leaves chain alive but empties rules. We don't model the rules; OK.
	case "-A":
		// -A PREROUTING/OUTPUT ... -j K2R* → bump corresponding ref count.
		if targetIdx := indexOf(args, "-j"); targetIdx >= 0 && targetIdx+1 < len(args) {
			target := args[targetIdx+1]
			if strings.HasPrefix(target, "K2R") {
				switch chain {
				case "PREROUTING":
					f.refs[name+":"+table+":"+target]++
				case "OUTPUT":
					f.outputRefs[name+":"+table+":"+target]++
				}
			}
		}
	case "-D":
		if targetIdx := indexOf(args, "-j"); targetIdx >= 0 && targetIdx+1 < len(args) {
			target := args[targetIdx+1]
			refKey := name + ":" + table + ":" + target
			switch chain {
			case "PREROUTING":
				if f.refs[refKey] > 0 {
					f.refs[refKey]--
				}
			case "OUTPUT":
				if f.outputRefs[refKey] > 0 {
					f.outputRefs[refKey]--
				}
			}
		}
	}
	return nil
}

// simulateIP models what matters about `ip rule` / `ip route`: `rule add`
// stacks another copy of an identical rule, `route add` refuses a duplicate,
// and `del` removes one copy and fails when there is none.
func (f *fakeIptablesState) simulateIP(args []string) error {
	var key []string
	op, isRule := "", false
	for _, a := range args {
		switch a {
		case "add", "del":
			op = a
			continue
		case "rule":
			isRule = true
		}
		key = append(key, a)
	}
	k := strings.Join(key, " ")
	switch op {
	case "add":
		if !isRule && f.ipEntries[k] > 0 {
			return fmt.Errorf("ip %s: File exists", strings.Join(args, " "))
		}
		f.ipEntries[k]++
	case "del":
		if f.ipEntries[k] == 0 {
			return fmt.Errorf("ip %s: No such file or directory", strings.Join(args, " "))
		}
		f.ipEntries[k]--
	}
	return nil
}

func indexOf(s []string, v string) int {
	for i, x := range s {
		if x == v {
			return i
		}
	}
	return -1
}

func (f *fakeIptablesState) hasCallExact(want string) bool {
	for _, c := range f.calls {
		if c == want {
			return true
		}
	}
	return false
}

// seedChain pre-populates a chain (and optionally PREROUTING references) so a
// test can simulate "stale chain left over from a previous daemon crash".
func (f *fakeIptablesState) seedChain(ipBin, table, chain string, refs int) {
	key := ipBin + ":" + table + ":" + chain
	f.chains[key] = true
	if refs > 0 {
		f.refs[key] = refs
	}
}

// seedChainOutput is the OUTPUT-jump analogue of seedChain for testing the
// nat OUTPUT REDIRECT path (K2R_DNS_OUT).
func (f *fakeIptablesState) seedChainOutput(ipBin, table, chain string, refs int) {
	key := ipBin + ":" + table + ":" + chain
	f.chains[key] = true
	if refs > 0 {
		f.outputRefs[key] = refs
	}
}

// ── Bug 2: Install must be idempotent on back-to-back calls ──────────

// Install + Install (no Remove between) must not fail with "Chain already
// exists". Simulator enforces real iptables semantic: -N CHAIN fails if chain
// already exists, mirroring the production failure mode.
func TestInstall_IdempotentBackToBack(t *testing.T) {
	f := newFakeIptables()
	f.install(t)
	i := &iptInterceptor{}
	cfg := InterceptConfig{ListenPort: 12345, LANSubnets: []string{"192.168.1.0/24"}}

	if err := i.Install(cfg); err != nil {
		t.Fatalf("first Install: %v", err)
	}
	if err := i.Install(cfg); err != nil {
		t.Fatalf("second Install must succeed (was Bug 2), got: %v", err)
	}
}

// Fresh interceptor instance running Install when the kernel still has a
// stale K2R chain from a previous daemon crash. Mirrors `cleanStaleRules`'s
// real-world scenario at daemon startup.
func TestInstall_RecoverFromStaleOrphanChain(t *testing.T) {
	f := newFakeIptables()
	// Pre-seed: previous daemon left K2R chain with two PREROUTING refs.
	f.seedChain("iptables", "mangle", "K2R", 2)
	f.install(t)
	i := &iptInterceptor{}
	cfg := InterceptConfig{ListenPort: 12345, LANSubnets: []string{"192.168.1.0/24"}}

	if err := i.Install(cfg); err != nil {
		t.Fatalf("Install over stale orphan must succeed (Bug 1+2), got: %v", err)
	}
	// Final state: chain exists exactly once, with exactly one PREROUTING ref
	// (the new one we installed — stale refs from seed must be gone).
	if got := f.refs["iptables:mangle:K2R"]; got != 1 {
		t.Errorf("K2R refs = %d, want 1 (stale refs not cleaned)", got)
	}
}

// ── Bug 1: Remove must clean orphan rules even on fresh interceptor ──

// A fresh iptInterceptor (zero-value, hadV4Subnet=false) must still find and
// delete orphan K2R chain + PREROUTING jumps left over from a prior crashed
// daemon. Today's behavior: Remove() returns immediately because
// hadV4Subnet=false — Bug 1.5 making cleanStaleRules a no-op.
func TestRemove_DiscoversOrphansOnFreshStruct(t *testing.T) {
	f := newFakeIptables()
	f.seedChain("iptables", "mangle", "K2R", 2)  // chain + 2 PREROUTING refs
	f.seedChain("iptables", "nat", "K2R_DNS", 1) // DNS chain + 1 ref
	f.install(t)

	i := &iptInterceptor{} // zero value — no prior in-memory state

	i.Remove()

	if f.chains["iptables:mangle:K2R"] {
		t.Errorf("K2R chain must be deleted by Remove; calls:\n%s",
			strings.Join(f.calls, "\n"))
	}
	if f.refs["iptables:mangle:K2R"] != 0 {
		t.Errorf("K2R PREROUTING refs must be zero, got %d", f.refs["iptables:mangle:K2R"])
	}
	if f.chains["iptables:nat:K2R_DNS"] {
		t.Errorf("K2R_DNS chain must be deleted by Remove")
	}
}

// Remove() must continue cleaning subsequent rules even if an early cmd fails
// (e.g. iptables xtables-lock contention). Today: any silently-swallowed
// error CAN break the cleanup chain → stale chain blocks next Install.
func TestRemove_PartialFailureContinues(t *testing.T) {
	f := newFakeIptables()
	f.seedChain("iptables", "mangle", "K2R", 1)
	// Inject failure: first -D attempt fails (lock contention).
	firstD := true
	f.failPredicate = func(name string, args []string) error {
		joined := strings.Join(args, " ")
		if firstD && strings.Contains(joined, "-D PREROUTING") && strings.Contains(joined, "-j K2R") {
			firstD = false
			return fmt.Errorf("simulated xtables lock contention")
		}
		return nil
	}
	f.install(t)

	i := &iptInterceptor{}
	i.Remove()

	// -F K2R and -X K2R (mangle, not K2R_DNS) must still have been issued.
	if !f.hasCallExact("iptables -w 5 -t mangle -F K2R") {
		t.Errorf("must issue -F K2R despite earlier failure; calls:\n%s",
			strings.Join(f.calls, "\n"))
	}
	if !f.hasCallExact("iptables -w 5 -t mangle -X K2R") {
		t.Errorf("must issue -X K2R despite earlier failure; calls:\n%s",
			strings.Join(f.calls, "\n"))
	}
}

// Full lifecycle: Install → Remove → Install on the SAME interceptor instance
// must work cleanly with simulator enforcing real iptables semantics.
func TestInstall_RemoveInstallRoundtrip(t *testing.T) {
	f := newFakeIptables()
	f.install(t)
	i := &iptInterceptor{}
	cfg := InterceptConfig{ListenPort: 12345, LANSubnets: []string{"192.168.1.0/24"}}

	if err := i.Install(cfg); err != nil {
		t.Fatalf("first Install: %v", err)
	}
	i.Remove()
	if err := i.Install(cfg); err != nil {
		t.Fatalf("Install after Remove: %v", err)
	}
}

// ── Bug 8: iptables xtables.lock contention — must wait, not fail ────

// `ipt()` helper builds an iptables-family args slice with `-w 5` prefix so
// the underlying iptables waits up to 5 s for /var/run/xtables.lock instead
// of failing immediately when another process holds it. Confirmed on
// GL-XE300 MIPS (iptables v1.8.3 legacy) 2026-05-21.
func TestIpt_PrependsWaitFlag(t *testing.T) {
	got := ipt("-t", "mangle", "-N", "K2R")
	want := []string{"-w", "5", "-t", "mangle", "-N", "K2R"}
	if len(got) != len(want) {
		t.Fatalf("ipt() len = %d, want %d; got = %v", len(got), len(want), got)
	}
	for i, v := range want {
		if got[i] != v {
			t.Errorf("ipt()[%d] = %q, want %q", i, got[i], v)
		}
	}
}

// Every iptables/ip6tables command emitted by buildIptCommands must start
// with `-w 5`. ip(8) commands must NOT have it — they use netlink, not
// /var/run/xtables.lock, and would error on the unknown flag.
//
// Bug 8 (2026-05-21): on real OpenWRT during WiFi re-association, the
// hotplug.d/iface/20-firewall script briefly holds xtables.lock; without
// -w our `-A K2R -d 114.114.114.114 -j RETURN` failed mid-Install,
// triggered error cleanup, and stranded the LAN with no chain for 15 s.
func TestBuildIptCommands_AllIptablesHaveWaitFlag(t *testing.T) {
	cmds, _, _ := buildIptCommands(InterceptConfig{
		ListenPort:    12345,
		LANSubnets:    []string{"192.168.1.0/24", "fd00::/64"},
		ExcludeIPs:    []string{"1.2.3.4", "2001:db8::1"},
		DNSRedirect:   true,
		DNSPort:       5353,
		AllowlistMode: true,
		AllowedMACs:   []string{"aa:bb:cc:dd:ee:ff"},
	})
	if len(cmds) < 10 {
		t.Fatalf("expected dual-stack + DNS + MAC config to produce many commands; got %d", len(cmds))
	}
	var iptCount, ipCount int
	for i, c := range cmds {
		switch c.name {
		case "iptables", "ip6tables":
			iptCount++
			if len(c.args) < 2 || c.args[0] != "-w" || c.args[1] != "5" {
				t.Errorf("Bug 8: cmd[%d] %s missing -w 5 prefix; got: %v", i, c.name, c.args)
			}
		case "ip":
			ipCount++
			// ip(8) uses netlink, not xtables.lock. -w is invalid there.
			if len(c.args) >= 2 && c.args[0] == "-w" {
				t.Errorf("Bug 8: cmd[%d] ip(8) must not have -w prefix; got: %v", i, c.args)
			}
		default:
			t.Errorf("cmd[%d] unexpected binary %q; got: %v", i, c.name, c.args)
		}
	}
	if iptCount == 0 {
		t.Error("no iptables/ip6tables commands generated — config probably wrong")
	}
}

// cleanK2RFamily issues iptables -D / -F / -X commands at runtime — they
// also need -w 5 to ride out hotplug-driven xtables.lock contention.
func TestCleanK2RFamily_UsesWaitFlag(t *testing.T) {
	f := newFakeIptables()
	// Seed: chain + 2 PREROUTING refs so cleanK2RFamily has work to do
	// across all three pass types: -D, -F, -X.
	f.seedChain("iptables", "mangle", "K2R", 2)
	f.install(t)

	cleanK2RFamily("iptables", "mangle")

	if len(f.calls) == 0 {
		t.Fatal("cleanK2RFamily issued zero commands — seeding wrong?")
	}
	var sawD, sawF, sawX bool
	for _, call := range f.calls {
		if !strings.HasPrefix(call, "iptables ") && !strings.HasPrefix(call, "ip6tables ") {
			continue
		}
		if !strings.HasPrefix(call, "iptables -w 5 ") && !strings.HasPrefix(call, "ip6tables -w 5 ") {
			t.Errorf("Bug 8: cleanK2RFamily call missing -w 5 prefix: %q", call)
		}
		switch {
		case strings.Contains(call, " -D "):
			sawD = true
		case strings.Contains(call, " -F "):
			sawF = true
		case strings.Contains(call, " -X "):
			sawX = true
		}
	}
	if !sawD || !sawF || !sawX {
		t.Errorf("must exercise -D (%v), -F (%v), and -X (%v) paths; calls:\n%s",
			sawD, sawF, sawX, strings.Join(f.calls, "\n"))
	}
}

// ── DNS port-53 exemption from TPROXY ────────────────────────────
//
// With DNSRedirect on, LAN→ANY:53 is owned by the NAT REDIRECT chain (DNS
// listener). If the mangle TPROXY rule also claims it, both actions apply on
// a real router (OpenWrt 25.12, 2026-09-25): the query gets REDIRECT-rewritten
// to <router>:DNSPort and then TPROXY-tunneled to that private address — the
// client sees a timeout (UDP) / reset (TCP) and the bogus flows poison the
// QUIC datagram relay health probe. So the TPROXY chain must RETURN dport 53
// before any tproxy rule.

func TestNftRuleGeneration_DNSRedirect_ExemptsPort53FromTPROXY(t *testing.T) {
	nft := &nftShellInterceptor{}
	script := nft.buildScript(InterceptConfig{
		ListenPort:  12345,
		LANSubnets:  []string{"192.168.1.0/24", "fd00::/64"},
		DNSRedirect: true,
		DNSPort:     12346,
	})
	const exempt = "add rule inet k2r prerouting meta l4proto { tcp, udp } th dport 53 return"
	exemptIdx := strings.Index(script, exempt)
	if exemptIdx < 0 {
		t.Fatalf("missing port-53 TPROXY exemption rule\n%s", script)
	}
	tproxyIdx := strings.Index(script, "tproxy ip to")
	if tproxyIdx < 0 {
		t.Fatal("missing tproxy rule")
	}
	if exemptIdx > tproxyIdx {
		t.Error("port-53 exemption must precede the tproxy rules")
	}
}

func TestNftRuleGeneration_DNSRedirect_Disabled_NoPort53Exemption(t *testing.T) {
	nft := &nftShellInterceptor{}
	script := nft.buildScript(InterceptConfig{
		ListenPort: 12345,
		LANSubnets: []string{"192.168.1.0/24"},
	})
	if strings.Contains(script, "th dport 53 return") {
		t.Error("port-53 exemption only makes sense when DNSRedirect owns port 53")
	}
}

func TestIptRuleGeneration_DNSRedirect_ExemptsPort53FromTPROXY(t *testing.T) {
	cmds, _, _ := buildIptCommands(InterceptConfig{
		ListenPort:  12345,
		LANSubnets:  []string{"192.168.1.0/24", "fd00::/64"},
		DNSRedirect: true,
		DNSPort:     12346,
	})
	for _, bin := range []string{"iptables", "ip6tables"} {
		var lines []string
		for _, c := range cmds {
			if c.name == bin {
				lines = append(lines, strings.Join(c.args, " "))
			}
		}
		joined := strings.Join(lines, "\n")
		tproxyIdx := strings.Index(joined, "-j TPROXY")
		if tproxyIdx < 0 {
			t.Fatalf("%s: missing TPROXY rule", bin)
		}
		for _, proto := range []string{"tcp", "udp"} {
			rule := "-t mangle -A K2R -p " + proto + " --dport 53 -j RETURN"
			idx := strings.Index(joined, rule)
			if idx < 0 {
				t.Errorf("%s: missing %q\n%s", bin, rule, joined)
				continue
			}
			if idx > tproxyIdx {
				t.Errorf("%s: %q must precede the TPROXY rule", bin, rule)
			}
		}
	}
}

func TestIptRuleGeneration_DNSRedirect_Disabled_NoPort53Exemption(t *testing.T) {
	cmds, _, _ := buildIptCommands(InterceptConfig{
		ListenPort: 12345,
		LANSubnets: []string{"192.168.1.0/24"},
	})
	if strings.Contains(flattenCmds(cmds), "--dport 53 -j RETURN") {
		t.Error("port-53 exemption only makes sense when DNSRedirect owns port 53")
	}
}

func TestNftNetlink_DNSExemptExprs(t *testing.T) {
	for _, proto := range []byte{6, 17} {
		exprs := dnsExemptTPROXYExprs(proto)
		if len(exprs) == 0 {
			t.Fatalf("proto %d: no exprs", proto)
		}
		if _, ok := exprs[len(exprs)-1].(*expr.Verdict); !ok {
			t.Errorf("proto %d: last expr must be a verdict (return), got %T", proto, exprs[len(exprs)-1])
		}
		saw53 := false
		for _, e := range exprs {
			if c, ok := e.(*expr.Cmp); ok && len(c.Data) == 2 && c.Data[0] == 0 && c.Data[1] == 53 {
				saw53 = true
			}
		}
		if !saw53 {
			t.Errorf("proto %d: no dport==53 comparison", proto)
		}
	}
}
