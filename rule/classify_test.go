package rule

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/kaitu-io/k2-rules/krs"
)

func writeAppBundleFile(t *testing.T, path string, apps *krs.AppPatterns) {
	t.Helper()
	var buf bytes.Buffer
	if err := krs.WriteBundle(&buf, &krs.Bundle{Apps: apps}); err != nil {
		t.Fatalf("krs.WriteBundle: %v", err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("WriteFile %s: %v", path, err)
	}
}

func resultsByID(rs []ClassifyResult) map[string]ClassifyResult {
	m := make(map[string]ClassifyResult, len(rs))
	for _, r := range rs {
		m[r.ID] = r
	}
	return m
}

// The core fix: an app matching the region bundle classifies "direct", a
// non-matching app stays "proxy". This is exactly what was silently failing on
// mobile (every app fell through to "proxy" because classify-apps was unwired).
func TestClassifyInstalled_RegionMatch(t *testing.T) {
	dir := t.TempDir()
	writeAppBundleFile(t, filepath.Join(dir, "cn.krs"), &krs.AppPatterns{
		Android: krs.AndroidPatterns{Apps: []string{"com.tencent.mm"}},
	})

	apps := []krs.MatchableApp{
		{ID: "com.tencent.mm", Label: "WeChat"},
		{ID: "com.foreign.app", Label: "Foreign"},
	}
	got := resultsByID(ClassifyInstalled(dir, "cn", "android", apps))

	if got["com.tencent.mm"].Default != "direct" {
		t.Errorf("WeChat default = %q, want direct (hit=%+v)", got["com.tencent.mm"].Default, got["com.tencent.mm"])
	}
	if got["com.foreign.app"].Default != "proxy" {
		t.Errorf("Foreign default = %q, want proxy", got["com.foreign.app"].Default)
	}
}

// Wrong region selected → that region's bundle is absent → all proxy.
func TestClassifyInstalled_RegionMismatch(t *testing.T) {
	dir := t.TempDir()
	writeAppBundleFile(t, filepath.Join(dir, "cn.krs"), &krs.AppPatterns{
		Android: krs.AndroidPatterns{Apps: []string{"com.tencent.mm"}},
	})
	apps := []krs.MatchableApp{{ID: "com.tencent.mm", Label: "WeChat"}}
	got := resultsByID(ClassifyInstalled(dir, "th", "android", apps))
	if got["com.tencent.mm"].Default != "proxy" {
		t.Errorf("region=th default = %q, want proxy (no th bundle here)", got["com.tencent.mm"].Default)
	}
}

func TestClassifyInstalled_FailSoft(t *testing.T) {
	apps := []krs.MatchableApp{{ID: "a"}, {ID: "b"}}
	cases := []struct{ name, dir, region string }{
		{"empty region", t.TempDir(), ""},
		{"empty cacheDir", "", "cn"},
		{"missing bundle dir", filepath.Join(t.TempDir(), "nope"), "cn"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ClassifyInstalled(tc.dir, tc.region, "android", apps)
			if len(got) != 2 {
				t.Fatalf("len(got) = %d, want 2", len(got))
			}
			for _, r := range got {
				if r.Default != "proxy" {
					t.Errorf("%s default = %q, want proxy", r.ID, r.Default)
				}
			}
		})
	}
}
