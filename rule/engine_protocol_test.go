package rule

import (
	"testing"

	"github.com/kaitu-io/k2-rules/krs"
)

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

// TestRouteConnProto_ProtocolHitHostMiss_NoMatch pins RouteConnProto's host
// branch (rule/engine.go: `if r.hasHostCriteria() && !matchRouteHost(r,
// host) { continue }`) with the negative case a protocol-hit-alone test can
// never distinguish: a route combining {protocol, domain_suffix} where the
// PROTOCOL matches but the HOST does not. Without the host branch, protocol
// alone would be enough to return a match — silently turning
// {protocol:["quic"], domain_suffix:["example.org"]} into "any QUIC traffic
// routes here", with the domain_suffix restriction never enforced and no
// error anywhere.
func TestRouteConnProto_ProtocolHitHostMiss_NoMatch(t *testing.T) {
	ns, err := krs.BuildSet(krs.NamedSet{DomainSuffixes: []string{"example.org"}})
	if err != nil {
		t.Fatal(err)
	}
	e := NewEngine([]RouteEntry{
		{Target: Target(0), Sets: []krs.Matcher{ns}, Protocols: []string{"quic"}},
	}, Target(2), nil, false)

	// Sanity check: the positive case (protocol hit + host hit) does match —
	// establishes the rule itself is well-formed before proving the negative.
	if target, matched := e.RouteConnProto("example.org", ConnMeta{Network: "udp", Protocol: "quic"}); !matched || target != Target(0) {
		t.Fatalf("positive control: RouteConnProto(example.org, quic) = (%v, %v), want (direct(0), true)", target, matched)
	}

	// Protocol hits ("quic" == "quic"), but host does NOT satisfy the
	// domain_suffix criterion — must not match.
	if target, matched := e.RouteConnProto("not-example.com", ConnMeta{Network: "udp", Protocol: "quic"}); matched {
		t.Errorf("RouteConnProto(not-example.com, quic) = (%v, %v), want (_, false) — protocol hit must not bypass an unmet host criterion", target, matched)
	}

	// Same, with an IP host that trivially can't satisfy a domain matcher.
	if target, matched := e.RouteConnProto("9.9.9.9", ConnMeta{Network: "udp", Protocol: "quic"}); matched {
		t.Errorf("RouteConnProto(9.9.9.9, quic) = (%v, %v), want (_, false)", target, matched)
	}
}
