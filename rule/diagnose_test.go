//go:build !short

package rule_test

import (
	"context"
	"flag"
	"fmt"
	"net/netip"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/kaitu-io/k2-rules/krs"
	"github.com/kaitu-io/k2/rule"
)

var diagnoseHost = flag.String("host", "", "host (domain or IP) to diagnose routing for")

// TestDiagnose is a diagnostic tool for investigating routing decisions.
//
// Usage:
//
//	go test -run TestDiagnose -v -args -host=mmtcdn.cn
//	go test -run TestDiagnose -v -args -host=203.205.147.224
//
// Read the failure modes below before trusting a report from an older build.
//
// This tool used to fetch loose per-country .k2b files and merely t.Logf a
// download failure. Those files stopped existing (format renamed to .krs,
// then folded into a single tarball), so every fetch 404'd, krs.Load read an
// empty directory, and the tool printed a complete, confident report in which
// EVERY host routed DIRECT in EVERY country — and passed. Measured on
// 2026-08-15: `-host=google.com` reported "cn-access: DIRECT". A tool that
// answers a routing question wrongly is worse than one that does not run, so
// the bundle count is now asserted before anything is printed.
func TestDiagnose(t *testing.T) {
	if *diagnoseHost == "" {
		t.Skip("no -host flag provided; usage: go test -run TestDiagnose -v -args -host=<domain-or-ip>")
	}
	host := strings.ToLower(*diagnoseHost)

	// Fetch through the production path — one tarball, every bundle, the same
	// call daemon.go and appext.go make. No per-country file list to drift.
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := rule.EnsureBundles(ctx, dir, rule.DefaultSources); err != nil {
		t.Fatalf("EnsureBundles: %v", err)
	}

	bundles, err := krs.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	idx := krs.Index(bundles)

	// The discriminator. Without it an empty index still produces a full,
	// plausible report — every set silently absent, every country "DIRECT".
	if len(bundles) == 0 || len(idx) == 0 {
		t.Fatalf("loaded %d bundles / %d sets — refusing to report routing decisions "+
			"against an empty rule set, every answer would read DIRECT", len(bundles), len(idx))
	}
	t.Logf("loaded %d bundles, %d sets", len(bundles), len(idx))

	isIP := false
	var addr netip.Addr
	if a, err := netip.ParseAddr(host); err == nil {
		isIP = true
		addr = a
	}

	// Section 1: Per-set match results.
	fmt.Fprintf(os.Stderr, "\n=== Diagnose: %s ===\n\n", host)
	fmt.Fprintf(os.Stderr, "--- Per-set match results ---\n")

	setNames := sortedKeys(idx)
	for _, name := range setNames {
		s := idx[name]
		if isIP {
			if strings.HasPrefix(name, "geoip-") || name == "overseas" {
				if s.MatchIP(addr) {
					fmt.Fprintf(os.Stderr, "  %-20s MATCH\n", name+":")
				} else {
					fmt.Fprintf(os.Stderr, "  %-20s NO MATCH\n", name+":")
				}
			} else {
				fmt.Fprintf(os.Stderr, "  %-20s n/a (IP query, domain-only set)\n", name+":")
			}
		} else {
			if strings.HasPrefix(name, "geoip-") {
				fmt.Fprintf(os.Stderr, "  %-20s n/a (domain query, IP-only set)\n", name+":")
			} else {
				if s.MatchDomain(host) {
					fmt.Fprintf(os.Stderr, "  %-20s MATCH\n", name+":")
				} else {
					fmt.Fprintf(os.Stderr, "  %-20s NO MATCH\n", name+":")
				}
			}
		}
	}

	// Section 2: Per-country route decisions.
	//
	// The country list is derived, never written down here. It used to be a
	// hardcoded 14-entry slice plus a hand-copied cc→preset map duplicating
	// rule.presets; production had already grown to 17 (tm, kz, uz), so those
	// three countries were simply undiagnosable and nothing said so. Now every
	// "<cc>-sites" set present in the real bundles proposes a country, and
	// rule.ExpandPreset — the production registry itself — decides whether the
	// matching preset exists.
	countries := discoverCountries(idx)
	fmt.Fprintf(os.Stderr, "\n--- Per-country route decisions (%d countries) ---\n", len(countries))
	for _, cc := range countries {
		preset := cc + "-access"
		eng := buildEngine(t, preset, bundles, idx)
		got := eng.Match(host)
		decision := "FALLBACK (direct)"
		switch got {
		case rule.Target(0):
			decision = "DIRECT"
		case rule.Target(2):
			decision = "PROXY"
		}
		fmt.Fprintf(os.Stderr, "  %-5s (%-12s): %s\n", cc, preset, decision)
		eng.Close()
	}
	fmt.Fprintf(os.Stderr, "\n")
}

// discoverCountries returns the country codes that have both a "<cc>-sites"
// set in the loaded bundles and a registered "<cc>-access" preset, sorted.
//
// Deriving it means a new country becomes diagnosable the moment k2-rules
// ships its bundle and rule/target.go registers its preset — no third edit
// here, and no silent gap when someone forgets one.
func discoverCountries(idx map[string]*krs.NamedSet) []string {
	var out []string
	for _, name := range sortedKeys(idx) {
		cc, ok := strings.CutSuffix(name, "-sites")
		if !ok {
			continue
		}
		if rule.ExpandPreset(cc+"-access") == nil {
			continue // bundle exists but no preset registers it
		}
		out = append(out, cc)
	}
	return out
}

func sortedKeys(m map[string]*krs.NamedSet) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}
