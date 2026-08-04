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

// TestRouteConnProto_IgnoresRoutesWithoutProtocols pins matchProtoRoute's
// first guard — `if len(r.Protocols) == 0 { return false }` — which is the
// ONLY enforcement point for the design promise that the protocol dimension
// never disturbs a route that does not mention a protocol.
//
// Without it, RouteConnProto degrades into "return the first route whose host
// and meta criteria happen to match", which is a completely different
// function: step 6 in core/tunnel.go would then hand any sniffed connection
// back to an ordinary route. Concretely, with a plain
// {domain_suffix:["x.com"], via: proxy} route in the table, a BitTorrent flow
// whose sniffed SNI resolved to x.com would be re-routed onto the tunnel —
// BitTorrent on the proxy, silently, with the "forced direct" default
// apparently honored right up until step 6 undid it.
//
// The route below deliberately has host + meta criteria that DO match the
// probe, so the ONLY reason it must stay invisible is its empty Protocols
// list. The positive control that follows proves the probe itself is capable
// of matching, so a green result cannot come from the probe simply missing.
//
// Mutation check: delete the len(r.Protocols) == 0 guard and this must fail.
// (Verified — before this test existed, deleting that guard left all of
// ./rule/ and ./core/ green.)
func TestRouteConnProto_IgnoresRoutesWithoutProtocols(t *testing.T) {
	ns, err := krs.BuildSet(krs.NamedSet{DomainSuffixes: []string{"example.org"}})
	if err != nil {
		t.Fatal(err)
	}

	meta := ConnMeta{Network: "udp", Port: 443, Protocol: "quic"}

	// Route carries host criteria AND meta criteria, both satisfied by `meta`
	// below — but no Protocols. RouteConnProto must not see it.
	noProto := RouteEntry{
		Target:       Target(3),
		Sets:         []krs.Matcher{ns},
		NetworkMatch: "udp",
		PortRanges:   []PortRange{{Lo: 443, Hi: 443}},
	}

	e := NewEngine([]RouteEntry{noProto}, Target(2), nil, false)
	if target, matched := e.RouteConnProto("example.org", meta); matched {
		t.Errorf("RouteConnProto matched a route with no Protocols criteria: (%v, %v), want (_, false).\n"+
			"A route that never mentions a protocol must be invisible to the post-sniff "+
			"re-route — otherwise any sniffed connection can be handed to an ordinary route.",
			target, matched)
	}

	// Positive control: the identical route, plus a matching Protocols list,
	// MUST match. This proves the negative above is caused by the missing
	// Protocols criteria and not by the host/meta criteria failing to match.
	withProto := noProto
	withProto.Protocols = []string{"quic"}
	e2 := NewEngine([]RouteEntry{withProto}, Target(2), nil, false)
	target, matched := e2.RouteConnProto("example.org", meta)
	if !matched || target != Target(3) {
		t.Fatalf("positive control: RouteConnProto with Protocols=[quic] = (%v, %v), want (outbound(3), true) — "+
			"the probe cannot match this route at all, so the negative case above proves nothing",
			target, matched)
	}
}
