package core

import (
	"net"
	"net/netip"
	"testing"

	"github.com/kaitu-io/k2/rule"
)

// connSearcher records which entry point routeConn used and returns a
// shared-UID package set from the 4-tuple path.
type connSearcher struct {
	connCalls, plainCalls int
	gotDstIP              netip.Addr
	gotDstPort            uint16
}

func (s *connSearcher) FindProcess(string, netip.Addr, uint16) (string, string, string) {
	s.plainCalls++
	return "plain", "com.plain", ""
}

func (s *connSearcher) FindProcessConn(_ string, _ netip.Addr, _ uint16, dstIP netip.Addr, dstPort uint16) (string, []string, string) {
	s.connCalls++
	s.gotDstIP, s.gotDstPort = dstIP, dstPort
	return "", []string{"com.google.android.gms", "com.android.vending"}, ""
}

func TestRouteConn_PrefersConnSearcherAndFillsPackageNames(t *testing.T) {
	r := NewRouter(rule.NewEngine(nil, rule.Target(0), nil, false))
	s := &connSearcher{}
	ct := &ClientTunnel{router: r, processSearcher: s}
	src := &net.UDPAddr{IP: net.ParseIP("10.81.3.7"), Port: 40000}

	_, meta := ct.routeConn("93.184.216.34:443", "tcp", src)

	if s.connCalls != 1 || s.plainCalls != 0 {
		t.Fatalf("calls = (conn %d, plain %d), want the 4-tuple path", s.connCalls, s.plainCalls)
	}
	if s.gotDstIP.String() != "93.184.216.34" || s.gotDstPort != 443 {
		t.Errorf("dst seen = %v:%d, want 93.184.216.34:443", s.gotDstIP, s.gotDstPort)
	}
	if len(meta.PackageNames) != 2 || meta.PackageName != "com.google.android.gms" {
		t.Errorf("meta = (%q, %v), want primary + full shared set", meta.PackageName, meta.PackageNames)
	}
}

// Non-IP destinations (proxy-mode domain dials) cannot feed
// getConnectionOwnerUid — routeConn must fall back to the plain searcher.
func TestRouteConn_DomainDestFallsBackToPlainSearcher(t *testing.T) {
	r := NewRouter(rule.NewEngine(nil, rule.Target(0), nil, false))
	s := &connSearcher{}
	ct := &ClientTunnel{router: r, processSearcher: s}
	src := &net.UDPAddr{IP: net.ParseIP("10.81.3.7"), Port: 40001}

	_, meta := ct.routeConn("example.com:443", "tcp", src)

	if s.connCalls != 0 || s.plainCalls != 1 {
		t.Fatalf("calls = (conn %d, plain %d), want the plain path", s.connCalls, s.plainCalls)
	}
	if meta.PackageName != "com.plain" || len(meta.PackageNames) != 0 {
		t.Errorf("meta = (%q, %v), want plain single-package identity", meta.PackageName, meta.PackageNames)
	}
}
