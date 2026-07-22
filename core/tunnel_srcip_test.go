package core

import (
	"net"
	"testing"

	"github.com/kaitu-io/k2/rule"
)

func TestRouteConn_PopulatesSrcIP(t *testing.T) {
	r := NewRouter(rule.NewEngine(nil, rule.Target(0), nil, false))
	ct := &ClientTunnel{router: r}
	src := &net.UDPAddr{IP: net.ParseIP("10.81.3.7"), Port: 40000}
	_, meta := ct.routeConn("93.184.216.34:443", "tcp", src)
	if !meta.SrcIP.IsValid() || meta.SrcIP.String() != "10.81.3.7" {
		t.Fatalf("want SrcIP 10.81.3.7 (unmapped), got %v", meta.SrcIP)
	}
	_, meta = ct.routeConn("93.184.216.34:443", "udp", nil)
	if meta.SrcIP.IsValid() {
		t.Fatalf("nil srcAddr must leave SrcIP zero, got %v", meta.SrcIP)
	}
}
