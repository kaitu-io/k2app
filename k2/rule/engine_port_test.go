package rule

import "testing"

func TestMatchConn_PortRange(t *testing.T) {
	e := NewEngine([]RouteEntry{
		{
			Target:       Target(0), // direct
			NetworkMatch: "udp",
			PortRanges:   []PortRange{{Lo: 27000, Hi: 28000}},
		},
	}, Target(2) /* fallback = proxy */, nil, false)

	// In-range UDP → direct
	if got := e.MatchConn("5.5.5.5:27015", ConnMeta{Network: "udp", Port: 27015}); got != Target(0) {
		t.Errorf("udp/27015 = %v, want direct(0)", got)
	}
	// Out-of-range UDP → fallback
	if got := e.MatchConn("5.5.5.5:443", ConnMeta{Network: "udp", Port: 443}); got != Target(2) {
		t.Errorf("udp/443 = %v, want fallback(2)", got)
	}
	// In-range but TCP → fallback (network is AND'd)
	if got := e.MatchConn("5.5.5.5:27015", ConnMeta{Network: "tcp", Port: 27015}); got != Target(2) {
		t.Errorf("tcp/27015 = %v, want fallback(2)", got)
	}
	// Port unknown (0) → route with port criteria must not match
	if got := e.MatchConn("5.5.5.5:27015", ConnMeta{Network: "udp", Port: 0}); got != Target(2) {
		t.Errorf("udp/unknown = %v, want fallback(2)", got)
	}
}
