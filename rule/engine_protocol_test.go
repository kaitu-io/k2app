package rule

import "testing"

func TestMatchConn_Protocol(t *testing.T) {
	e := NewEngine([]RouteEntry{
		{Target: Target(0), Protocols: []string{"stun"}},
	}, Target(2), nil, false)

	if got := e.MatchConn("5.5.5.5:3478", ConnMeta{Network: "udp", Protocol: "stun"}); got != Target(0) {
		t.Errorf("stun = %v, want direct(0)", got)
	}
	if got := e.MatchConn("5.5.5.5:3478", ConnMeta{Network: "udp", Protocol: "quic"}); got != Target(2) {
		t.Errorf("quic = %v, want fallback(2)", got)
	}
	// Protocol unknown → route with protocol criteria must not match.
	if got := e.MatchConn("5.5.5.5:3478", ConnMeta{Network: "udp"}); got != Target(2) {
		t.Errorf("unknown = %v, want fallback(2)", got)
	}
}

// TestMatchConn_Protocol_EmptyRuleEntryNeverMatchesUnsniffedConn pins the
// fail-closed guard in matchMeta: even if a route's compiled Protocols list
// somehow contains an empty string (e.g. a malformed `protocol: [""]`
// config entry surviving strings.TrimSpace), an unsniffed connection
// (meta.Protocol == "") must never match it. Without the explicit
// meta.Protocol == "" guard, "" == "" in the OR-loop would silently make
// every not-yet-sniffed connection match a protocol rule that was never
// meant to be a wildcard.
func TestMatchConn_Protocol_EmptyRuleEntryNeverMatchesUnsniffedConn(t *testing.T) {
	e := NewEngine([]RouteEntry{
		{Target: Target(0), Protocols: []string{""}},
	}, Target(2), nil, false)

	if got := e.MatchConn("5.5.5.5:3478", ConnMeta{Network: "udp"}); got != Target(2) {
		t.Errorf("unsniffed conn against Protocols=[\"\"] = %v, want fallback(2) (fail-closed)", got)
	}
}
