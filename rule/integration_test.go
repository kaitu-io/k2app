//go:build !short

package rule_test

import (
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"testing"

	"github.com/kaitu-io/k2-rules/krs"
	"github.com/kaitu-io/k2/rule"
)

const releaseBase = "https://github.com/kaitu-io/k2-rules/releases/latest/download"

// TestIntegration_RealBundles downloads real .krs bundles from GitHub releases
// and verifies the full pipeline: upstream data → krs format → Load → Match.
// Skipped with -short (requires network).
func TestIntegration_RealBundles(t *testing.T) {
	dir := t.TempDir()

	// Download both bundles.
	for _, name := range []string{"overseas.krs", "cn.krs"} {
		url := releaseBase + "/" + name
		path := filepath.Join(dir, name)
		if err := downloadFile(url, path); err != nil {
			t.Fatalf("download %s: %v", name, err)
		}
		fi, _ := os.Stat(path)
		t.Logf("downloaded %s (%d bytes)", name, fi.Size())
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
