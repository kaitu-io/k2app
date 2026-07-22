package rule

import (
	"net/netip"
	"testing"
)

func TestMatchMeta_SrcCIDR(t *testing.T) {
	entry := &RouteEntry{
		Target:   Target(2),
		SrcCIDRs: []netip.Prefix{netip.MustParsePrefix("10.81.1.0/24")},
	}
	// 命中源子网
	if !matchMeta(entry, ConnMeta{SrcIP: netip.MustParseAddr("10.81.1.30")}) {
		t.Fatal("expected src 10.81.1.30 to match 10.81.1.0/24")
	}
	// 不同子网不命中
	if matchMeta(entry, ConnMeta{SrcIP: netip.MustParseAddr("10.81.2.30")}) {
		t.Fatal("expected src 10.81.2.30 NOT to match")
	}
	// 无源地址(zero value)时,有 SrcCIDRs 约束的路由不得命中(fail-closed)
	if matchMeta(entry, ConnMeta{}) {
		t.Fatal("expected empty SrcIP NOT to match src-constrained route")
	}
	// IPv4-mapped IPv6 源地址要 Unmap 后命中
	mapped := netip.AddrFrom16(netip.MustParseAddr("10.81.1.30").As16())
	if !matchMeta(entry, ConnMeta{SrcIP: mapped}) {
		t.Fatal("expected v4-mapped src to match after Unmap")
	}
}

func TestMatchConn_SrcCIDRRouting(t *testing.T) {
	// 两条 src 路由 + fallback:按源子网分流
	routes := []RouteEntry{
		{Target: Target(2), SrcCIDRs: []netip.Prefix{netip.MustParsePrefix("10.81.1.0/24")}},
		{Target: Target(3), SrcCIDRs: []netip.Prefix{netip.MustParsePrefix("10.81.2.0/24")}},
	}
	e := NewEngine(routes, Target(1) /* fallback = reject */, nil, false)
	if got := e.MatchConn("93.184.216.34", ConnMeta{SrcIP: netip.MustParseAddr("10.81.1.5"), Network: "tcp"}); got != Target(2) {
		t.Fatalf("slot1 src: got %v want outbound(2)", got)
	}
	if got := e.MatchConn("93.184.216.34", ConnMeta{SrcIP: netip.MustParseAddr("10.81.2.5"), Network: "tcp"}); got != Target(3) {
		t.Fatalf("slot2 src: got %v want outbound(3)", got)
	}
	if got := e.MatchConn("93.184.216.34", ConnMeta{SrcIP: netip.MustParseAddr("192.168.1.5"), Network: "tcp"}); got != Target(1) {
		t.Fatalf("unknown src: got %v want reject fallback", got)
	}
}

func TestMatchConfig_SrcIPCIDR_HasCriteria(t *testing.T) {
	m := MatchConfig{SrcIPCIDR: []string{"10.81.1.0/24"}}
	if !m.HasCriteria() {
		t.Fatal("SrcIPCIDR must count as criteria")
	}
	if m.HasHostCriteria() {
		t.Fatal("SrcIPCIDR is meta, not host criteria")
	}
}
