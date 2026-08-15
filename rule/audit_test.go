//go:build audit

package rule_test

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/kaitu-io/k2-rules/krs"
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
// domain coverage with the published rule bundles. Generates a report, not
// assertions.
//
// Usage: go test -tags audit -run TestAudit -v -timeout 120s ./rule/
//
// Guarded by the `audit` build tag, which is also how it rotted unnoticed:
// it stopped compiling entirely (krs was used but never imported) and no
// normal build touches it, so nothing reported that. It also still fetched
// loose cn-direct.k2b / overseas.k2b — two format generations behind what
// k2-rules publishes. Both are fixed here; the bundles now arrive through
// EnsureBundles like everywhere else.
func TestAudit_GeositeCrossCheck(t *testing.T) {
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

	// Without this an empty index reports 0% coverage and a "missing" list
	// containing every domain v2fly knows — a spectacular-looking finding that
	// is purely an artifact of having loaded nothing.
	if len(bundles) == 0 || len(idx) == 0 {
		t.Fatalf("loaded %d bundles / %d sets — refusing to audit coverage against "+
			"an empty rule set, everything would read as missing", len(bundles), len(idx))
	}
	t.Logf("loaded %d bundles, %d sets", len(bundles), len(idx))

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
