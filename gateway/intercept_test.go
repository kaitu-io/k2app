//go:build linux

package gateway

import (
	"fmt"
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
	if !strings.Contains(script, "add chain inet k2r dnsredir { type nat hook prerouting priority dstnat") {
		t.Error("DNS redirect should use a NAT chain, not filter chain")
	}
	if strings.Contains(script, "add rule inet k2r prerouting") && strings.Contains(script, "redirect") {
		t.Error("redirect rules should NOT be in the filter chain")
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

// fakeIptablesState simulates iptables/ip6tables semantics tightly enough to
// catch the two bugs we care about:
//   - `-N CHAIN` fails with "Chain already exists" when the chain is present
//   - `-X CHAIN` fails when references to the chain still exist
//
// Used as a runCmd / listChainRules backend for unit tests so we don't need
// a real Linux kernel + iptables.
type fakeIptablesState struct {
	calls         []string
	chains        map[string]bool   // "iptables:mangle:K2R" → exists
	refs          map[string]int    // "iptables:mangle:K2R" → reference count from PREROUTING jumps
	failPredicate func(name string, args []string) error
}

func newFakeIptables() *fakeIptablesState {
	return &fakeIptablesState{
		chains: map[string]bool{},
		refs:   map[string]int{},
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
		return strings.Join(lines, "\n"), nil
	}
}

// simulate models the iptables behaviors that matter for our tests.
func (f *fakeIptablesState) simulate(name string, args []string) error {
	// Skip non-iptables binaries (ip, ip6) — succeed silently.
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
		if f.refs[key] > 0 {
			return fmt.Errorf("%s -t %s -X %s: chain still referenced", name, table, chain)
		}
		delete(f.chains, key)
	case "-F":
		// Flush — leaves chain alive but empties rules. We don't model the rules; OK.
	case "-A":
		// -A PREROUTING ... -j K2R → bump ref count for K2R chain
		if chain == "PREROUTING" {
			if targetIdx := indexOf(args, "-j"); targetIdx >= 0 && targetIdx+1 < len(args) {
				target := args[targetIdx+1]
				if strings.HasPrefix(target, "K2R") {
					f.refs[name+":"+table+":"+target]++
				}
			}
		}
	case "-D":
		if chain == "PREROUTING" {
			if targetIdx := indexOf(args, "-j"); targetIdx >= 0 && targetIdx+1 < len(args) {
				target := args[targetIdx+1]
				refKey := name + ":" + table + ":" + target
				if f.refs[refKey] > 0 {
					f.refs[refKey]--
				}
			}
		}
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
	f.seedChain("iptables", "mangle", "K2R", 2)     // chain + 2 PREROUTING refs
	f.seedChain("iptables", "nat", "K2R_DNS", 1)    // DNS chain + 1 ref
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
	if !f.hasCallExact("iptables -t mangle -F K2R") {
		t.Errorf("must issue -F K2R despite earlier failure; calls:\n%s",
			strings.Join(f.calls, "\n"))
	}
	if !f.hasCallExact("iptables -t mangle -X K2R") {
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
