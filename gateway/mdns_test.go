//go:build linux

package gateway

import (
	"net"
	"testing"
)

func TestMDNSAnswerIPPicksFirstPrivateV4(t *testing.T) {
	ips := []net.IP{
		net.ParseIP("203.0.113.9"), // 公网,跳过
		net.ParseIP("fe80::1"),     // 链路本地 v6,跳过
		net.ParseIP("192.168.8.1"), // ✓
	}
	got := mdnsAnswerIP(ips)
	if got == nil || got.String() != "192.168.8.1" {
		t.Fatalf("want 192.168.8.1, got %v", got)
	}
}

func TestMDNSAnswerIPNoneWhenNoPrivateV4(t *testing.T) {
	if mdnsAnswerIP([]net.IP{net.ParseIP("203.0.113.9")}) != nil {
		t.Fatal("want nil when no private v4")
	}
}
