//go:build audit

package rule_test

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kaitu-io/k2/rule"
)

const geositeBase = "https://raw.githubusercontent.com/v2fly/domain-list-community/master/data/"

// geositeCategories maps k2b set names to v2fly geosite category names.
var geositeCategories = map[string][]string{
	"cn-sites": {
		"tencent", "alibaba", "baidu", "bilibili", "bytedance",
		"jd", "netease", "sina", "xiaomi",
	},
}

// TestAudit_GeositeCrossCheck downloads v2fly geosite data and compares
// domain coverage with k2b bundles. Generates a report, not assertions.
//
// Usage: go test -tags audit -run TestAudit -v -timeout 120s ./rule/
func TestAudit_GeositeCrossCheck(t *testing.T) {
	dir := t.TempDir()
	bundleFiles := []string{"cn-direct.k2b", "overseas.k2b"}
	for _, name := range bundleFiles {
		url := releaseBase + "/" + name
		path := filepath.Join(dir, name)
		if err := downloadFile(url, path); err != nil {
			t.Fatalf("download %s: %v", name, err)
		}
	}

	bundles, err := rule.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	idx := rule.Index(bundles)

	for setName, categories := range geositeCategories {
		bs, ok := idx[setName]
		if !ok {
			t.Errorf("set %q not found in bundles", setName)
			continue
		}

		t.Run(setName, func(t *testing.T) {
			var totalDomains, matchCount, missCount int
			var missingDomains []string

			for _, category := range categories {
				domains, err := fetchGeositeDomains(category)
				if err != nil {
					t.Logf("WARN: fetch geosite %q: %v", category, err)
					continue
				}

				for _, domain := range domains {
					totalDomains++
					if bs.MatchDomain(domain) {
						matchCount++
					} else {
						missCount++
						missingDomains = append(missingDomains, fmt.Sprintf("%s (geosite:%s)", domain, category))
					}
				}
			}

			coveragePct := float64(0)
			if totalDomains > 0 {
				coveragePct = float64(matchCount) / float64(totalDomains) * 100
			}
			fmt.Fprintf(os.Stderr, "\n=== Audit: %s ===\n", setName)
			fmt.Fprintf(os.Stderr, "  Total domains checked: %d\n", totalDomains)
			fmt.Fprintf(os.Stderr, "  Matched in k2b:        %d (%.1f%%)\n", matchCount, coveragePct)
			fmt.Fprintf(os.Stderr, "  Missing from k2b:      %d\n", missCount)

			if len(missingDomains) > 0 {
				fmt.Fprintf(os.Stderr, "\n  Missing domains:\n")
				for _, d := range missingDomains {
					fmt.Fprintf(os.Stderr, "    - %s\n", d)
				}
			}
			fmt.Fprintf(os.Stderr, "\n")
		})
	}
}

func fetchGeositeDomains(category string) ([]string, error) {
	url := geositeBase + category
	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d for %s", resp.StatusCode, url)
	}

	reader := io.LimitReader(resp.Body, 5<<20)
	scanner := bufio.NewScanner(reader)

	var domains []string
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "include:") || strings.HasPrefix(line, "regexp:") {
			continue
		}
		if idx := strings.IndexByte(line, ' '); idx > 0 {
			line = line[:idx]
		}
		for _, prefix := range []string{"domain:", "full:", "keyword:"} {
			line = strings.TrimPrefix(line, prefix)
		}
		if line == "" {
			continue
		}
		domains = append(domains, strings.ToLower(line))
	}
	return domains, scanner.Err()
}
