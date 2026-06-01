//go:build !short

package rule_test

import (
	"context"
	"net/netip"
	"os"
	"testing"
	"time"

	"github.com/kaitu-io/k2-rules/krs"
	"github.com/kaitu-io/k2/rule"
	"gopkg.in/yaml.v3"
)

// --- YAML schema ---

type goldenFile struct {
	Countries []goldenCountry `yaml:"countries"`
}

type goldenCountry struct {
	Code   string      `yaml:"code"`
	Preset string      `yaml:"preset"`
	Apps   []goldenApp `yaml:"apps"`
}

type goldenApp struct {
	Name          string   `yaml:"name"`
	DomainsDirect []string `yaml:"domains_direct"`
	DomainsProxy  []string `yaml:"domains_proxy"`
	IPsDirect     []string `yaml:"ips_direct"`
	IPsProxy      []string `yaml:"ips_proxy"`
}

// --- helpers ---

const goldenPath = "testdata/golden_routes.yaml"

func loadGolden(t *testing.T) goldenFile {
	t.Helper()
	data, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden file: %v", err)
	}
	var gf goldenFile
	if err := yaml.Unmarshal(data, &gf); err != nil {
		t.Fatalf("parse golden file: %v", err)
	}
	if len(gf.Countries) == 0 {
		t.Fatal("golden file has no countries")
	}
	return gf
}

func downloadBundles(t *testing.T, _ goldenFile) (string, []*krs.Bundle, map[string]*krs.NamedSet) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping golden test that downloads from CDN; use -run without -short to run")
	}
	dir := t.TempDir()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if err := rule.EnsureBundles(ctx, dir, nil); err != nil {
		t.Fatalf("EnsureBundles: %v", err)
	}

	bundles, err := krs.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	idx := krs.Index(bundles)
	return dir, bundles, idx
}

func firstIPInCIDR(cidr string) (netip.Addr, error) {
	prefix, err := netip.ParsePrefix(cidr)
	if err != nil {
		return netip.Addr{}, err
	}
	return prefix.Addr().Next(), nil
}

func buildEngine(t *testing.T, preset string, bundles []*krs.Bundle, idx map[string]*krs.NamedSet) *rule.Engine {
	t.Helper()
	presetNames := rule.ExpandPreset(preset)
	var directSets []krs.Matcher
	for _, name := range presetNames {
		if s, ok := idx[name]; ok {
			directSets = append(directSets, s)
		}
	}
	var proxySets []krs.Matcher
	if s, ok := idx["overseas"]; ok {
		proxySets = append(proxySets, s)
	}
	routes := []rule.RouteEntry{
		{Target: rule.Target(0), Sets: directSets},
		{Target: rule.Target(2), Sets: proxySets},
	}
	return rule.NewEngine(routes, rule.Target(0), bundles, false)
}

// --- L1: Data Accuracy ---

func TestGolden_L1_DataAccuracy(t *testing.T) {
	gf := loadGolden(t)
	_, _, idx := downloadBundles(t, gf)

	for _, country := range gf.Countries {
		presetNames := rule.ExpandPreset(country.Preset)

		for _, app := range country.Apps {
			for _, domain := range app.DomainsDirect {
				name := country.Code + "/" + app.Name + "/" + domain + "=direct"
				t.Run(name, func(t *testing.T) {
					for _, setName := range presetNames {
						if s, ok := idx[setName]; ok && s.MatchDomain(domain) {
							return
						}
					}
					t.Errorf("domain %q not found in any set of preset %s (sets: %v)", domain, country.Preset, presetNames)
				})
			}

			for _, cidr := range app.IPsDirect {
				addr, err := firstIPInCIDR(cidr)
				if err != nil {
					t.Errorf("bad CIDR %q in %s/%s: %v", cidr, country.Code, app.Name, err)
					continue
				}
				name := country.Code + "/" + app.Name + "/" + cidr + "=direct"
				t.Run(name, func(t *testing.T) {
					for _, setName := range presetNames {
						if s, ok := idx[setName]; ok && s.MatchIP(addr) {
							return
						}
					}
					t.Errorf("IP %s (from %s) not found in any set of preset %s", addr, cidr, country.Preset)
				})
			}

			for _, domain := range app.DomainsProxy {
				name := country.Code + "/" + app.Name + "/" + domain + "=proxy"
				t.Run(name, func(t *testing.T) {
					for _, setName := range presetNames {
						if s, ok := idx[setName]; ok && s.MatchDomain(domain) {
							t.Errorf("domain %q unexpectedly matched set %q in preset %s (should NOT be direct)", domain, setName, country.Preset)
							return
						}
					}
				})
			}

			for _, cidr := range app.IPsProxy {
				addr, err := firstIPInCIDR(cidr)
				if err != nil {
					t.Errorf("bad CIDR %q in %s/%s: %v", cidr, country.Code, app.Name, err)
					continue
				}
				name := country.Code + "/" + app.Name + "/" + cidr + "=proxy"
				t.Run(name, func(t *testing.T) {
					for _, setName := range presetNames {
						if s, ok := idx[setName]; ok && s.MatchIP(addr) {
							t.Errorf("IP %s (from %s) unexpectedly matched set %q in preset %s", addr, cidr, setName, country.Preset)
							return
						}
					}
				})
			}
		}
	}
}

// --- L2: Engine Match ---

func TestGolden_L2_EngineMatch(t *testing.T) {
	gf := loadGolden(t)
	_, bundles, idx := downloadBundles(t, gf)

	for _, country := range gf.Countries {
		eng := buildEngine(t, country.Preset, bundles, idx)
		defer eng.Close()

		for _, app := range country.Apps {
			for _, domain := range app.DomainsDirect {
				name := country.Code + "/" + app.Name + "/" + domain + "=direct"
				t.Run(name, func(t *testing.T) {
					if got := eng.Match(domain); got != rule.Target(0) {
						t.Errorf("Engine.Match(%q) = %v, want direct (Target 0)", domain, got)
					}
				})
			}
			for _, domain := range app.DomainsProxy {
				name := country.Code + "/" + app.Name + "/" + domain + "=proxy"
				t.Run(name, func(t *testing.T) {
					if got := eng.Match(domain); got != rule.Target(2) {
						t.Errorf("Engine.Match(%q) = %v, want proxy (Target 2)", domain, got)
					}
				})
			}
			for _, cidr := range app.IPsDirect {
				addr, err := firstIPInCIDR(cidr)
				if err != nil {
					continue
				}
				name := country.Code + "/" + app.Name + "/" + cidr + "=direct"
				t.Run(name, func(t *testing.T) {
					if got := eng.Match(addr.String()); got != rule.Target(0) {
						t.Errorf("Engine.Match(%q) = %v, want direct (Target 0)", addr, got)
					}
				})
			}
			for _, cidr := range app.IPsProxy {
				addr, err := firstIPInCIDR(cidr)
				if err != nil {
					continue
				}
				name := country.Code + "/" + app.Name + "/" + cidr + "=proxy"
				t.Run(name, func(t *testing.T) {
					if got := eng.Match(addr.String()); got != rule.Target(2) {
						t.Errorf("Engine.Match(%q) = %v, want proxy (Target 2)", addr, got)
					}
				})
			}
		}
	}
}

// --- L3: End-to-End ---

func TestGolden_L3_EndToEnd(t *testing.T) {
	gf := loadGolden(t)
	_, bundles, idx := downloadBundles(t, gf)

	for _, country := range gf.Countries {
		t.Run(country.Code, func(t *testing.T) {
			presetNames := rule.ExpandPreset(country.Preset)
			if presetNames == nil {
				t.Fatalf("ExpandPreset(%q) returned nil", country.Preset)
			}
			var directSets []krs.Matcher
			for _, name := range presetNames {
				s, ok := idx[name]
				if !ok {
					t.Fatalf("set %q from preset %q not found", name, country.Preset)
				}
				directSets = append(directSets, s)
			}
			overseasSet, ok := idx["overseas"]
			if !ok {
				t.Fatal("overseas set not found")
			}
			routes := []rule.RouteEntry{
				{Target: rule.Target(0), Sets: directSets},
				{Target: rule.Target(2), Sets: []krs.Matcher{overseasSet}},
			}
			eng := rule.NewEngine(routes, rule.Target(0), bundles, false)
			defer eng.Close()

			for _, app := range country.Apps {
				for _, domain := range app.DomainsDirect {
					if got := eng.Match(domain); got != rule.Target(0) {
						t.Errorf("%s/%s: Match(%q) = %v, want direct", app.Name, domain, domain, got)
					}
				}
				for _, domain := range app.DomainsProxy {
					if got := eng.Match(domain); got != rule.Target(2) {
						t.Errorf("%s/%s: Match(%q) = %v, want proxy", app.Name, domain, domain, got)
					}
				}
				for _, cidr := range app.IPsDirect {
					addr, err := firstIPInCIDR(cidr)
					if err != nil {
						continue
					}
					if got := eng.Match(addr.String()); got != rule.Target(0) {
						t.Errorf("%s/%s: Match(%q) = %v, want direct", app.Name, cidr, addr, got)
					}
				}
				for _, cidr := range app.IPsProxy {
					addr, err := firstIPInCIDR(cidr)
					if err != nil {
						continue
					}
					if got := eng.Match(addr.String()); got != rule.Target(2) {
						t.Errorf("%s/%s: Match(%q) = %v, want proxy", app.Name, cidr, addr, got)
					}
				}
			}
		})
	}
}
