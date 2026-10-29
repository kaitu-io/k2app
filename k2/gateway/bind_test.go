//go:build linux

package gateway

import (
	"net"
	"testing"
)

func TestResolveLANBindAddrsExcludesPublic(t *testing.T) {
	ifaces := []bindIfaceStub{
		{name: "br-lan", ips: []string{"192.168.8.1/24"}},
		{name: "eth0", ips: []string{"203.0.113.5/24"}}, // WAN 公网
		{name: "lo", ips: []string{"127.0.0.1/8"}},
	}
	got := resolveLANBindAddrs(stubIfaces(ifaces), 1779)
	if !contains(got, "192.168.8.1:1779") {
		t.Fatalf("missing LAN bind, got %v", got)
	}
	for _, a := range got {
		if a == "203.0.113.5:1779" {
			t.Fatalf("WAN address must be excluded, got %v", got)
		}
	}
	if !contains(got, "127.0.0.1:1779") {
		t.Fatalf("loopback must remain, got %v", got)
	}
}

// IPv6 LAN addresses must be bracketed — fmt "%s:%d" yields an unparseable
// "fd1a::1:1779" that net.Listen rejects, so the ULA bind silently failed.
func TestResolveLANBindAddrsBracketsIPv6(t *testing.T) {
	ifaces := []bindIfaceStub{
		{name: "br-lan", ips: []string{"10.30.0.1/24", "fd1a:5aef:40b0::1/60"}},
	}
	got := resolveLANBindAddrs(stubIfaces(ifaces), 1779)
	if !contains(got, "[fd1a:5aef:40b0::1]:1779") {
		t.Fatalf("IPv6 bind must be bracketed, got %v", got)
	}
	for _, a := range got {
		if _, _, err := net.SplitHostPort(a); err != nil {
			t.Fatalf("unparseable bind addr %q: %v", a, err)
		}
	}
}

type bindIfaceStub struct {
	name string
	ips  []string
}

func stubIfaces(s []bindIfaceStub) []ifaceAddrs {
	var out []ifaceAddrs
	for _, x := range s {
		var addrs []net.Addr
		for _, c := range x.ips {
			ip, ipnet, _ := net.ParseCIDR(c)
			ipnet.IP = ip
			addrs = append(addrs, ipnet)
		}
		out = append(out, ifaceAddrs{Name: x.name, Addrs: addrs})
	}
	return out
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}
