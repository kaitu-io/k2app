//go:build !short

package rule_test

import (
	"flag"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kaitu-io/k2/rule"
)

var diagnoseHost = flag.String("host", "", "host (domain or IP) to diagnose routing for")

// TestDiagnose is a diagnostic tool for investigating routing decisions.
//
// Usage:
//
//	go test -run TestDiagnose -v -args -host=mmtcdn.cn
//	go test -run TestDiagnose -v -args -host=203.205.147.224
func TestDiagnose(t *testing.T) {
	if *diagnoseHost == "" {
		t.Skip("no -host flag provided; usage: go test -run TestDiagnose -v -args -host=<domain-or-ip>")
	}
	host := strings.ToLower(*diagnoseHost)

	// Download all country bundles.
	dir := t.TempDir()
	allBundles := []string{"overseas.k2b"}
	countries := []string{"ae", "bd", "by", "cn", "eg", "id", "ir", "mm", "pk", "ru", "sa", "th", "tr", "vn"}
	for _, cc := range countries {
		allBundles = append(allBundles, cc+"-direct.k2b")
	}
	for _, name := range allBundles {
		url := releaseBase + "/" + name
		path := filepath.Join(dir, name)
		if err := downloadFile(url, path); err != nil {
			t.Logf("WARN: failed to download %s: %v", name, err)
		}
	}

	bundles, err := rule.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	idx := rule.Index(bundles)

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
	fmt.Fprintf(os.Stderr, "\n--- Per-country route decisions ---\n")
	presets := map[string]string{
		"cn": "cn-access", "ir": "ir-access", "ru": "ru-access", "tr": "tr-access",
		"pk": "pk-access", "vn": "vn-access", "mm": "mm-access", "eg": "eg-access",
		"id": "id-access", "sa": "sa-access", "ae": "ae-access", "th": "th-access",
		"bd": "bd-access", "by": "by-access",
	}
	for _, cc := range countries {
		preset := presets[cc]
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

func sortedKeys(m map[string]*rule.BundleSet) []string {
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
