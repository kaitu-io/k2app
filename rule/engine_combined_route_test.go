package rule

import (
	"testing"

	"github.com/kaitu-io/k2-rules/krs"
)

// Regression pin for the two-phase matching trap documented in CLAUDE.md
// ("Two-Phase Matching") and warned about at compile time by
// engine.buildRouteEntries (dnsDroppedMetaFields): a route combining domain
// criteria with connection-time criteria has its meta half silently dropped
// at DNS time. These assertions PIN the current — surprising — semantics so
// any change to them is a conscious decision with this test in the diff,
// never an accident.
func TestCombinedDomainPortRoute_TwoPhaseSemantics(t *testing.T) {
	ns, err := krs.BuildSet(krs.NamedSet{DomainSuffixes: []string{"x.com"}})
	if err != nil {
		t.Fatal(err)
	}
	combined := Target(3)
	fallback := Target(2)
	e := NewEngine([]RouteEntry{
		{
			Target:     combined,
			Sets:       []krs.Matcher{ns},
			PortRanges: []PortRange{{Lo: 443, Hi: 443}},
		},
	}, fallback, nil, false)

	// DNS time: the port condition is DROPPED — the route fires on host
	// alone. core/dns/middleware.go then pins every IP the domain resolves
	// to onto this target for ALL ports, and a reject target NXDOMAINs the
	// whole domain.
	if got := e.Match("x.com"); got != combined {
		t.Errorf("Match(x.com) = %v, want %v (port condition dropped at DNS time)", got, combined)
	}
	if got := e.Match("a.x.com"); got != combined {
		t.Errorf("Match(a.x.com) = %v, want %v", got, combined)
	}

	// Connection time, IP-literal destination (the common case after DNS):
	// the domain half cannot match an IP, so the AND fails even though the
	// port matches — the combined route itself is unreachable here.
	if got := e.MatchConn("93.184.216.34", ConnMeta{Network: "tcp", Port: 443}); got != fallback {
		t.Errorf("MatchConn(ip, 443) = %v, want fallback %v (domain half cannot match an IP)", got, fallback)
	}

	// Connection time with a domain destination (rare: only when the dialer
	// still has the hostname) — the one path where the combination ANDs as
	// configured.
	if got := e.MatchConn("x.com", ConnMeta{Network: "tcp", Port: 443}); got != combined {
		t.Errorf("MatchConn(x.com, 443) = %v, want %v", got, combined)
	}
	if got := e.MatchConn("x.com", ConnMeta{Network: "tcp", Port: 80}); got != fallback {
		t.Errorf("MatchConn(x.com, 80) = %v, want fallback %v", got, fallback)
	}
}
