//go:build !short

package rule_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"os"
	"testing"
	"time"

	"github.com/kaitu-io/k2-rules/krs"
	"github.com/kaitu-io/k2/rule"
)

const releaseBase = "https://github.com/kaitu-io/k2-rules/releases/latest/download"

// TestIntegration_RealBundles downloads the real rule archive from GitHub
// releases and verifies the full pipeline: upstream data → krs format → Load
// → Match. Skipped with -short (requires network).
//
// The bundles arrive through EnsureBundles — the same call daemon.go and
// appext.go make — rather than by fetching individual .krs files. k2-rules
// stopped publishing loose bundles: since v2026.08.14 a release carries only
// krs.tar.gz / k2-rules.tar.gz / manifest.json / VERSION, so the old
// per-file fetch had been a guaranteed HTTP 404. Going through the production
// path also means this test now covers manifest → download → extract, not just
// Load → Match.
func TestIntegration_RealBundles(t *testing.T) {
	if testing.Short() {
		t.Skip("requires network (downloads release bundles)")
	}
	dir := t.TempDir()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if err := rule.EnsureBundles(ctx, dir, rule.DefaultSources); err != nil {
		t.Fatalf("EnsureBundles: %v", err)
	}

	// Load all bundles.
	bundles, err := krs.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(bundles) < 2 {
		t.Fatalf("expected >=2 bundles, got %d", len(bundles))
	}

	idx := krs.Index(bundles)
	t.Logf("loaded %d bundles, %d sets indexed", len(bundles), len(idx))

	// Verify expected sets exist (v2 schema: consolidated).
	requiredSets := []string{"overseas", "cn-sites", "geoip-cn"}
	for _, name := range requiredSets {
		if _, ok := idx[name]; !ok {
			t.Errorf("missing expected set: %s", name)
		}
	}

	// Build engine with overseas → proxy, cn-direct → direct.
	overseasSets := resolveNames(idx, []string{"overseas"})
	cnSets := resolveNames(idx, []string{"geoip-cn", "cn-sites"})

	entries := []rule.RouteEntry{
		{Target: rule.Target(2), Sets: overseasSets},
		{Target: rule.Target(0), Sets: cnSets},
	}
	engine := rule.NewEngine(entries, rule.Target(0), bundles, false)
	defer engine.Close()

	// Domain matching tests.
	domainTests := []struct {
		domain string
		want   rule.Target
	}{
		{"google.com", rule.Target(2)},
		{"youtube.com", rule.Target(2)},
		{"facebook.com", rule.Target(2)},
		{"api.openai.com", rule.Target(2)},
		{"github.com", rule.Target(2)},
		{"t.me", rule.Target(2)},
		{"telegram.org", rule.Target(2)},
		{"bilibili.com", rule.Target(0)},
		{"www.bilibili.com", rule.Target(0)},
		{"example.com", rule.Target(0)},       // fallback
		{"randomsite.xyz", rule.Target(0)},     // fallback
	}
	for _, tc := range domainTests {
		got := engine.Match(tc.domain)
		if got != tc.want {
			t.Errorf("Match(%q) = %v, want %v", tc.domain, got, tc.want)
		}
	}

	// IP matching tests.
	ipTests := []struct {
		ip   string
		want rule.Target
		desc string
	}{
		{"8.8.8.8", rule.Target(2), "Google DNS"},
		{"8.8.4.4", rule.Target(2), "Google DNS secondary"},
		{"149.154.167.50", rule.Target(2), "Telegram DC2"},
		{"91.108.56.100", rule.Target(2), "Telegram"},
		{"104.16.0.1", rule.Target(2), "Cloudflare CDN"},
	}
	for _, tc := range ipTests {
		got := engine.Match(tc.ip)
		if got != tc.want {
			t.Errorf("Match(%q) [%s] = %v, want %v", tc.ip, tc.desc, got, tc.want)
		}
	}

	// CN IP matching (if geoip-cn set has data).
	if bs, ok := idx["geoip-cn"]; ok {
		// A well-known Chinese IP (Alibaba Cloud).
		cnAddr := netip.MustParseAddr("47.52.0.1")
		if bs.MatchIP(cnAddr) {
			t.Logf("geoip-cn correctly matches Chinese IP %s", cnAddr)
		} else {
			t.Logf("geoip-cn did not match %s (may vary by upstream data)", cnAddr)
		}
	}
}

func resolveNames(idx map[string]*krs.NamedSet, names []string) []krs.Matcher {
	var sets []krs.Matcher
	for _, name := range names {
		if bs, ok := idx[name]; ok {
			sets = append(sets, bs)
		}
	}
	return sets
}

func downloadFile(url, path string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, resp.Body)
	return err
}
