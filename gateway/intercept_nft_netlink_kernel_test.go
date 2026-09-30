//go:build linux

package gateway

import (
	"bytes"
	"errors"
	"net"
	"os/exec"
	"runtime"
	"testing"

	"github.com/google/nftables"
	"github.com/google/nftables/expr"
	"golang.org/x/sys/unix"
)

// enterEmptyNetNS moves the test's thread into a fresh network namespace, so a
// real ruleset can be loaded into the kernel and read back without touching
// the host's. The thread stays locked: it ends with the test goroutine.
func enterEmptyNetNS(t *testing.T) {
	t.Helper()
	runtime.LockOSThread()
	if err := unix.Unshare(unix.CLONE_NEWNET); err != nil {
		runtime.UnlockOSThread()
		t.Skipf("new network namespace: %v (needs root)", err)
	}
}

// daddrReturnIndex finds the rule that returns on destination ipNet.
func daddrReturnIndex(rules []*nftables.Rule, ipNet *net.IPNet) int {
	addr, offset := []byte(ipNet.IP.To4()), uint32(16)
	if addr == nil {
		addr, offset = ipNet.IP.To16(), 24
	}
	for i, r := range rules {
		var daddr, match, ret bool
		for _, e := range r.Exprs {
			switch x := e.(type) {
			case *expr.Payload:
				daddr = daddr || (x.Base == expr.PayloadBaseNetworkHeader && x.Offset == offset)
			case *expr.Cmp:
				match = match || bytes.Equal(x.Data, addr)
			case *expr.Verdict:
				ret = ret || x.Kind == expr.VerdictReturn
			}
		}
		if daddr && match && ret {
			return i
		}
	}
	return -1
}

// The netlink backend is the one OpenWrt runs. Its chain is built by a
// sequence of add* calls whose order is the rule order, and only the kernel
// can say what that order came out as.
func TestNftNetlink_Install_LinkScopeExemptSitsBetweenAllowlistAndTPROXY(t *testing.T) {
	enterEmptyNetNS(t)
	origRunCmd := runCmd
	runCmd = func(string, ...string) error { return errors.New("no ip(8) in this test") }
	t.Cleanup(func() { runCmd = origRunCmd })

	conn, err := nftables.New()
	if err != nil {
		t.Fatalf("nftables: %v", err)
	}
	n := &nftNetlinkInterceptor{conn: conn}
	cfg := InterceptConfig{
		ListenPort:    12345,
		LANSubnets:    []string{"192.168.8.1/24", "fd00::1/64"},
		ExcludeIPs:    []string{"203.0.113.7"},
		DNSRedirect:   true,
		DNSPort:       12346,
		AllowlistMode: true,
		AllowedMACs:   []string{"aa:bb:cc:dd:ee:ff"},
	}
	if err := n.Install(cfg); err != nil {
		t.Fatalf("Install: %v", err)
	}

	rules, err := conn.GetRules(n.table, n.chain)
	if err != nil {
		t.Fatalf("read the chain back: %v", err)
	}
	firstTProxy, allowlistDrop := -1, -1
	for i, r := range rules {
		for _, e := range r.Exprs {
			switch x := e.(type) {
			case *expr.Meta:
				// `meta mark set`: only the tproxy rules write the mark.
				// (The library does not decode the tproxy expression itself.)
				if x.Key == expr.MetaKeyMARK && x.SourceRegister && firstTProxy < 0 {
					firstTProxy = i
				}
			case *expr.Verdict:
				if x.Kind == expr.VerdictDrop {
					allowlistDrop = i
				}
			}
		}
	}
	if firstTProxy < 0 || allowlistDrop < 0 {
		t.Fatalf("chain read back without its landmarks: first tproxy rule %d, allowlist drop %d (of %d rules)",
			firstTProxy, allowlistDrop, len(rules))
	}

	v4, v6 := linkScopeDests(cfg.LANSubnets)
	for _, cidr := range append(v4, v6...) {
		_, ipNet, _ := net.ParseCIDR(cidr)
		at := daddrReturnIndex(rules, ipNet)
		switch {
		case at < 0:
			t.Errorf("no return rule for %s in the installed chain", cidr)
		case at > firstTProxy:
			t.Errorf("return for %s is rule %d, after the first tproxy rule (%d)", cidr, at, firstTProxy)
		case at < allowlistDrop:
			t.Errorf("return for %s is rule %d, ahead of the MAC allowlist drop (%d)", cidr, at, allowlistDrop)
		}
	}
}

// The nft shell backend (used when netlink is unavailable) after a daemon was
// killed: its table is still there when the next daemon installs.
func TestNftShell_Install_ReplacesAStaleTable(t *testing.T) {
	if _, err := exec.LookPath("nft"); err != nil {
		t.Skip("no nft binary")
	}
	enterEmptyNetNS(t)
	origRunCmd := runCmd
	runCmd = func(string, ...string) error { return errors.New("no ip(8) in this test") }
	t.Cleanup(func() { runCmd = origRunCmd })

	cfg := InterceptConfig{ListenPort: 12345, LANSubnets: []string{"192.168.8.1/24", "fd00::1/64"}, DNSRedirect: true, DNSPort: 12346}
	countRules := func() int {
		conn, err := nftables.New()
		if err != nil {
			t.Fatalf("nftables: %v", err)
		}
		table := &nftables.Table{Family: nftables.TableFamilyINet, Name: "k2r"}
		rules, err := conn.GetRules(table, &nftables.Chain{Name: "prerouting", Table: table})
		if err != nil {
			t.Fatalf("read the chain back: %v", err)
		}
		return len(rules)
	}

	if err := (&nftShellInterceptor{}).Install(cfg); err != nil {
		t.Fatalf("first Install: %v", err)
	}
	first := countRules()
	if first == 0 {
		t.Fatal("first Install left an empty chain")
	}
	// The daemon dies here: no Remove. Its successor installs.
	if err := (&nftShellInterceptor{}).Install(cfg); err != nil {
		t.Fatalf("Install over the stale table: %v", err)
	}
	if second := countRules(); second != first {
		t.Errorf("prerouting has %d rules after the second Install, %d after the first", second, first)
	}
}
