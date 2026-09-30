//go:build linux

package gateway

import (
	"net"
	"reflect"
	"strings"
	"testing"

	"github.com/google/nftables/expr"
	"golang.org/x/sys/unix"
)

// Multicast, the limited broadcast and each LAN subnet's directed broadcast
// stay on the LAN. Measured on OpenWrt 25.12 (2026-09-30): a LAN client
// sending to 255.255.255.255:9999 and 10.30.0.255:137 every 10 s opened one
// tunnel session per destination per minute, each ending as a relay timeout —
// the datagram relay was marked unhealthy 64 s in and new UDP moved to TCP-WS.

func TestLinkScopeDests(t *testing.T) {
	tests := []struct {
		name    string
		subnets []string
		v4, v6  []string
	}{
		{
			name:    "dual stack, interface addresses as DiscoverLANSubnets returns them",
			subnets: []string{"10.30.0.1/24", "192.168.1.77/24", "fd1a:5aef:40b0::1/60"},
			v4:      []string{"224.0.0.0/4", "255.255.255.255/32", "10.30.0.255/32", "192.168.1.255/32"},
			v6:      []string{"ff00::/8"},
		},
		{
			name:    "directed broadcast follows the mask, not the octet",
			subnets: []string{"172.16.4.0/22"},
			v4:      []string{"224.0.0.0/4", "255.255.255.255/32", "172.16.7.255/32"},
		},
		{
			name:    "point-to-point subnets have no broadcast address",
			subnets: []string{"10.0.0.0/31", "10.0.0.9/32"},
			v4:      []string{"224.0.0.0/4", "255.255.255.255/32"},
		},
		{
			name:    "IPv6 only: no IPv4 rules",
			subnets: []string{"fd00::/64"},
			v6:      []string{"ff00::/8"},
		},
		{name: "nothing intercepted, nothing exempted"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v4, v6 := linkScopeDests(tt.subnets)
			if !reflect.DeepEqual(v4, tt.v4) {
				t.Errorf("v4 = %v, want %v", v4, tt.v4)
			}
			if !reflect.DeepEqual(v6, tt.v6) {
				t.Errorf("v6 = %v, want %v", v6, tt.v6)
			}
		})
	}
}

// lineIndex returns the index of the first line containing every one of
// parts, or -1.
func lineIndex(lines []string, parts ...string) int {
	for i, l := range lines {
		all := true
		for _, p := range parts {
			if !strings.Contains(l, p) {
				all = false
				break
			}
		}
		if all {
			return i
		}
	}
	return -1
}

func TestNftRuleGeneration_LinkScopeExemptPrecedesTPROXY(t *testing.T) {
	n := &nftShellInterceptor{}
	script := n.buildScript(InterceptConfig{
		ListenPort: 12345,
		LANSubnets: []string{"10.30.0.1/24", "fd00::1/64"},
	})
	lines := strings.Split(script, "\n")
	firstTProxy := lineIndex(lines, "tproxy")
	if firstTProxy < 0 {
		t.Fatalf("no tproxy rule in script:\n%s", script)
	}
	for _, want := range []string{
		"add rule inet k2r prerouting ip daddr 224.0.0.0/4 return",
		"add rule inet k2r prerouting ip daddr 255.255.255.255/32 return",
		"add rule inet k2r prerouting ip daddr 10.30.0.255/32 return",
		"add rule inet k2r prerouting ip6 daddr ff00::/8 return",
	} {
		at := lineIndex(lines, want)
		if at < 0 {
			t.Errorf("missing %q in script:\n%s", want, script)
			continue
		}
		if at > firstTProxy {
			t.Errorf("%q is at line %d, after the first tproxy rule (line %d): it would never be reached", want, at, firstTProxy)
		}
	}
}

// The allowlist decides who may talk at all; the exemption must not let a
// device that is not on it reach the router by multicast or broadcast.
func TestNftRuleGeneration_LinkScopeExemptFollowsMACAllowlist(t *testing.T) {
	n := &nftShellInterceptor{}
	script := n.buildScript(InterceptConfig{
		ListenPort:    12345,
		LANSubnets:    []string{"10.30.0.1/24"},
		AllowlistMode: true,
		AllowedMACs:   []string{"aa:bb:cc:dd:ee:ff"},
	})
	lines := strings.Split(script, "\n")
	drop := lineIndex(lines, "@allowed_macs drop")
	exempt := lineIndex(lines, "ip daddr 224.0.0.0/4 return")
	if drop < 0 || exempt < 0 {
		t.Fatalf("drop=%d exempt=%d in script:\n%s", drop, exempt, script)
	}
	if exempt < drop {
		t.Errorf("link-scope exemption (line %d) precedes the MAC allowlist drop (line %d)", exempt, drop)
	}
}

func TestIptRuleGeneration_LinkScopeExemptPrecedesTPROXY(t *testing.T) {
	cmds, _, _ := buildIptCommands(InterceptConfig{
		ListenPort:    12345,
		LANSubnets:    []string{"10.30.0.1/24", "fd00::1/64"},
		AllowlistMode: true,
		AllowedMACs:   []string{"aa:bb:cc:dd:ee:ff"},
	})
	lines := strings.Split(flattenCmds(cmds), "\n")
	for _, tt := range []struct{ bin, dest string }{
		{"iptables", "224.0.0.0/4"},
		{"iptables", "255.255.255.255/32"},
		{"iptables", "10.30.0.255/32"},
		{"ip6tables", "ff00::/8"},
	} {
		exempt := lineIndex(lines, tt.bin+" ", "-A K2R -d "+tt.dest+" -j RETURN")
		tproxy := lineIndex(lines, tt.bin+" ", "-A K2R ", "-j TPROXY")
		mac := lineIndex(lines, tt.bin+" ", "-A K2R -j K2R_MAC")
		if exempt < 0 || tproxy < 0 || mac < 0 {
			t.Errorf("%s %s: exempt=%d tproxy=%d mac=%d in:\n%s", tt.bin, tt.dest, exempt, tproxy, mac, strings.Join(lines, "\n"))
			continue
		}
		if exempt > tproxy {
			t.Errorf("%s %s: exemption (line %d) is after the TPROXY rule (line %d)", tt.bin, tt.dest, exempt, tproxy)
		}
		if exempt < mac {
			t.Errorf("%s %s: exemption (line %d) precedes the MAC allowlist jump (line %d)", tt.bin, tt.dest, exempt, mac)
		}
	}
	// IPv6 destinations must not leak into the IPv4 chain or the reverse.
	if at := lineIndex(lines, "iptables ", "ff00::/8"); at >= 0 {
		t.Errorf("IPv6 multicast in an iptables command: %s", lines[at])
	}
	if at := lineIndex(lines, "ip6tables ", "224.0.0.0/4"); at >= 0 {
		t.Errorf("IPv4 multicast in an ip6tables command: %s", lines[at])
	}
}

// In an inet table a raw network-header offset is read from whatever packet
// arrives. Without the nfproto match the IPv4 multicast rule would test bytes
// 16-19 of an IPv6 header — part of the source address — and let one IPv6
// source in sixteen bypass the proxy.
func TestNftNetlink_DaddrReturnExprs(t *testing.T) {
	tests := []struct {
		cidr   string
		family byte
		offset uint32
		length uint32
	}{
		{"224.0.0.0/4", unix.NFPROTO_IPV4, 16, 4},
		{"10.30.0.255/32", unix.NFPROTO_IPV4, 16, 4},
		{"ff00::/8", unix.NFPROTO_IPV6, 24, 16},
	}
	for _, tt := range tests {
		t.Run(tt.cidr, func(t *testing.T) {
			_, ipNet, err := net.ParseCIDR(tt.cidr)
			if err != nil {
				t.Fatal(err)
			}
			exprs := daddrReturnExprs(ipNet)
			if len(exprs) != 6 {
				t.Fatalf("got %d exprs, want 6: %#v", len(exprs), exprs)
			}
			meta, ok := exprs[0].(*expr.Meta)
			if !ok || meta.Key != expr.MetaKeyNFPROTO {
				t.Fatalf("expr 0 must load meta nfproto, got %#v", exprs[0])
			}
			if fam, ok := exprs[1].(*expr.Cmp); !ok || fam.Op != expr.CmpOpEq || !reflect.DeepEqual(fam.Data, []byte{tt.family}) {
				t.Errorf("expr 1 must compare nfproto == %d, got %#v", tt.family, exprs[1])
			}
			payload, ok := exprs[2].(*expr.Payload)
			if !ok || payload.Base != expr.PayloadBaseNetworkHeader || payload.Offset != tt.offset || payload.Len != tt.length {
				t.Errorf("expr 2 must load the destination address (offset %d, len %d), got %#v", tt.offset, tt.length, exprs[2])
			}
			mask, ok := exprs[3].(*expr.Bitwise)
			if !ok || !reflect.DeepEqual(mask.Mask, []byte(ipNet.Mask)) || mask.Len != tt.length {
				t.Errorf("expr 3 must apply the prefix mask %v, got %#v", ipNet.Mask, exprs[3])
			}
			cmp, ok := exprs[4].(*expr.Cmp)
			if !ok || cmp.Op != expr.CmpOpEq || uint32(len(cmp.Data)) != tt.length || !net.IP(cmp.Data).Equal(ipNet.IP) {
				t.Errorf("expr 4 must compare against the network address %v, got %#v", ipNet.IP, exprs[4])
			}
			if v, ok := exprs[5].(*expr.Verdict); !ok || v.Kind != expr.VerdictReturn {
				t.Errorf("expr 5 must be a return verdict, got %#v", exprs[5])
			}
		})
	}
}

// ── ip rule hygiene ─────────────────────────────────────────────────

// `ip rule add` does not dedupe. Measured on OpenWrt 25.12: every start that
// followed a killed daemon left one more `fwmark 0x1 lookup 100` rule, and
// Remove deleted exactly one of them.
func TestSetupIPRules_DoesNotStackCopies(t *testing.T) {
	f := newFakeIptables()
	f.install(t)

	setupIPRules()
	setupIPRules() // the daemon before us died without a Remove
	setupIPRules()

	for key, n := range f.ipEntries {
		if n != 1 {
			t.Errorf("%q: %d copies after three setups, want 1", key, n)
		}
	}
	if len(f.ipEntries) != 4 {
		t.Errorf("want the 4 policy-route entries, got %v", f.ipEntries)
	}
}

func TestCleanupIPRules_RemovesEveryStackedCopy(t *testing.T) {
	f := newFakeIptables()
	f.install(t)
	f.ipEntries["rule fwmark 1 table 100"] = 5 // five unclean restarts
	f.ipEntries["-6 rule fwmark 1 table 100"] = 2
	f.ipEntries["route local 0.0.0.0/0 dev lo table 100"] = 1

	cleanupIPRules()

	for key, n := range f.ipEntries {
		if n != 0 {
			t.Errorf("%q: %d copies left", key, n)
		}
	}
}

// A misbehaving `ip` that never reports "no such rule" must not hang Remove.
func TestCleanupIPRules_Bounded(t *testing.T) {
	orig := runCmd
	t.Cleanup(func() { runCmd = orig })
	calls := 0
	runCmd = func(string, ...string) error { calls++; return nil }

	cleanupIPRules()

	if want := 4 * maxStaleIPRules; calls != want {
		t.Errorf("%d calls, want %d (4 entries × the bound)", calls, want)
	}
}

// All three backends leave nothing behind after Remove, however many copies
// were stacked before.
func TestRemove_ClearsStackedIPRules(t *testing.T) {
	for _, tt := range []struct {
		name string
		i    Interceptor
	}{
		{"nft shell", &nftShellInterceptor{}},
		{"iptables", &iptInterceptor{}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := newFakeIptables()
			f.install(t)
			f.ipEntries["rule fwmark 1 table 100"] = 4

			tt.i.Remove()

			if n := f.ipEntries["rule fwmark 1 table 100"]; n != 0 {
				t.Errorf("%d copies of the fwmark rule left after Remove", n)
			}
		})
	}
}

// A killed daemon leaves its table for the next one to take over. The script
// must replace it, not add a second copy of every rule to it.
func TestNftRuleGeneration_StartsFromAnEmptyTable(t *testing.T) {
	n := &nftShellInterceptor{}
	script := n.buildScript(InterceptConfig{ListenPort: 12345, LANSubnets: []string{"192.168.8.1/24"}})

	lines := strings.Split(script, "\n")
	want := []string{"add table inet k2r", "delete table inet k2r", "add table inet k2r"}
	if len(lines) < len(want) {
		t.Fatalf("script has %d lines", len(lines))
	}
	for i, w := range want {
		if lines[i] != w {
			t.Fatalf("script line %d = %q, want %q", i+1, lines[i], w)
		}
	}
}
