package engine

// Built-in anchor-direct route: k2r's discovery/control anchor address
// (config.RouterAnchorIP) must always route direct, even under a Global
// (match:all) proxy catch-all — otherwise a tunneled connection to a
// LAN-only address gets rejected by k2s's private-ip-guard. Not
// user-configurable; see config/anchor.go and engine.go's buildRuleEngine.
//
// Reuses cpTDirect/cpTProxy/cpServerURL/cpUnrelatedIP test fixtures from
// controlplane_route_test.go (same package).

import (
	"testing"

	"github.com/kaitu-io/k2/config"
	"github.com/kaitu-io/k2/rule"
)

// TestAnchorRoute_GlobalAllProxy_FastPath exercises the buildRuleEngine fast
// path (client.IsAllProxy() && len(finalRoutes)==1 -> rule.NewEngine(nil, ...))
// used by config.ClientFromURL and gateway.GatewayFromURL for a plain Global
// config. This is the path the 2026-07-18 spec revision flagged as broken:
// entries was nil, so the anchor address fell through to the proxy fallback.
func TestAnchorRoute_GlobalAllProxy_FastPath(t *testing.T) {
	client := config.ClientFromURL(cpServerURL)
	if !client.IsAllProxy() || len(client.Routes) != 1 {
		t.Fatal("precondition: expected the single-route Global fast path")
	}
	re := buildRuleEngine(client, buildOutboundMap(client.Routes), false)
	defer re.Close()

	if got := re.Match(config.RouterAnchorIP); got != cpTDirect {
		t.Fatalf("Match(anchor)=%v, want direct even in Global mode (fast path)", got)
	}
	// Regression: builtin rule must not widen direct beyond the anchor /32 —
	// an ordinary public IP must still tunnel.
	if got := re.Match(cpUnrelatedIP); got != cpTProxy {
		t.Fatalf("Match(%s)=%v, want proxy (builtin anchor rule must not leak to other IPs)", cpUnrelatedIP, got)
	}
}

// TestAnchorRoute_GlobalAllProxy_WithExceptionRoutes covers the non-fast-path
// Global case: match:all catch-all plus an explicit exception route (so
// buildRouteEntries's loop runs instead of the len==1 shortcut).
func TestAnchorRoute_GlobalAllProxy_WithExceptionRoutes(t *testing.T) {
	client := &config.ClientConfig{
		Routes: []config.RouteConfig{
			cpDirectRoute("k2.52j.me"),
			{Via: cpServerURL, Match: rule.MatchConfig{All: true}},
		},
	}
	if !client.IsAllProxy() {
		t.Fatal("precondition: expected IsAllProxy=true")
	}
	re := buildRuleEngine(client, buildOutboundMap(client.Routes), false)
	defer re.Close()

	if got := re.Match(config.RouterAnchorIP); got != cpTDirect {
		t.Fatalf("Match(anchor)=%v, want direct in Global mode with exception routes", got)
	}
	if got := re.Match(cpUnrelatedIP); got != cpTProxy {
		t.Fatalf("Match(%s)=%v, want proxy (fallback intact)", cpUnrelatedIP, got)
	}
}

// TestAnchorRoute_DefaultMode pins the "no behavior change outside Global
// mode" requirement: default fallback is already direct, so the anchor
// address must route direct exactly as before.
func TestAnchorRoute_DefaultMode(t *testing.T) {
	client := &config.ClientConfig{
		Routes: []config.RouteConfig{
			{Via: cpServerURL, Match: rule.MatchConfig{DomainSuffix: []string{"example.com"}}},
		},
	}
	if client.IsAllProxy() {
		t.Fatal("precondition: expected a non-Global (partial-match) config")
	}
	re := buildRuleEngine(client, buildOutboundMap(client.Routes), false)
	defer re.Close()

	if got := re.Match(config.RouterAnchorIP); got != cpTDirect {
		t.Fatalf("Match(anchor)=%v, want direct (unchanged default fallback)", got)
	}
}

// TestAnchorRoute_DiskReaderPath covers the NetworkExtension (iOS NE) disk
// path (buildRuleEngineDisk), which constructs its rule.Engine independently
// of the heap path.
func TestAnchorRoute_DiskReaderPath(t *testing.T) {
	client := config.ClientFromURL(cpServerURL)
	// Force past the len==1 fast path so buildRuleEngineDisk actually runs.
	client.Routes = append(client.Routes, config.RouteConfig{Via: "direct", Match: rule.MatchConfig{DomainSuffix: []string{"example.com"}}})
	re := buildRuleEngine(client, buildOutboundMap(client.Routes), true) // useDiskReader
	defer re.Close()

	if got := re.Match(config.RouterAnchorIP); got != cpTDirect {
		t.Fatalf("Match(anchor)=%v, want direct on the disk-reader path", got)
	}
}
