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

// TestAnchorRoute_SurvivesUpdaterReload is the regression test for the
// Critical gap found in review (2026-07-18 follow-up): Start() wires a
// rebuild closure into rule.StartUpdaterNamed for the 24h background
// rule-bundle updater (desktop daemon + Android — client.CacheDir != "" &&
// !cfg.NetworkExtension; NE/iOS skips it). StartUpdaterNamed fires an
// "immediate first check" within seconds of connect, which calls the
// rebuild function and hot-swaps the live engine's routes via
// Engine.ReloadNamed — a wholesale e.routes replace (rule/engine.go reload).
// The rebuild function previously called buildRouteEntries directly,
// bypassing withAnchorFirst, so the anchor entry present right after
// buildRuleEngine construction was silently stripped by the first updater
// tick: anchor traffic worked immediately after connect, then broke minutes
// later. rebuildEntriesForUpdater is the exact function Start() wires into
// the updater — this test drives it directly (per reviewer guidance) rather
// than standing up a full Start() integration harness.
func TestAnchorRoute_SurvivesUpdaterReload(t *testing.T) {
	client := config.ClientFromURL(cpServerURL)
	// The updater only runs for CacheDir != "" configs, which always take
	// buildRuleEngine's heap path (never the bare match:all fast path in
	// production — daemon/appext always set CacheDir). Push past the
	// len==1 fast path here so this test's construction matches that shape.
	client.Routes = append(client.Routes, config.RouteConfig{Via: "direct", Match: rule.MatchConfig{DomainSuffix: []string{"example.com"}}})
	client.CacheDir = t.TempDir() // empty dir — rule.LoadNamed returns (nil, nil), no bundles needed for this test

	outboundMap := buildOutboundMap(client.Routes)
	re := buildRuleEngine(client, outboundMap, false)
	defer re.Close()

	// Precondition: anchor is already direct right after construction (the
	// three-construction-site fix already covers this).
	if got := re.Match(config.RouterAnchorIP); got != cpTDirect {
		t.Fatalf("precondition: Match(anchor)=%v, want direct before reload", got)
	}

	// Drive the exact function Start() wires into rule.StartUpdaterNamed —
	// what the immediate first updater tick calls within seconds of connect.
	entries, named, err := rebuildEntriesForUpdater(client.Routes, outboundMap, client.CacheDir)
	if err != nil {
		t.Fatalf("rebuildEntriesForUpdater: %v", err)
	}
	if err := re.ReloadNamed(entries, named); err != nil {
		t.Fatalf("ReloadNamed: %v", err)
	}

	if got := re.Match(config.RouterAnchorIP); got != cpTDirect {
		t.Fatalf("Match(anchor)=%v, want direct AFTER updater reload (Critical gap regression)", got)
	}
	// Regression: builtin rule must not widen direct beyond the anchor /32.
	if got := re.Match(cpUnrelatedIP); got != cpTProxy {
		t.Fatalf("Match(%s)=%v, want proxy after reload (fallback intact)", cpUnrelatedIP, got)
	}
}
