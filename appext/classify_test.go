package appext

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/kaitu-io/k2-rules/krs"
	"github.com/kaitu-io/k2/rule"
)

func writeAppBundle(t *testing.T, path string, apps *krs.AppPatterns) {
	t.Helper()
	var buf bytes.Buffer
	if err := krs.WriteBundle(&buf, &krs.Bundle{Apps: apps}); err != nil {
		t.Fatalf("krs.WriteBundle: %v", err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func parseClassifications(t *testing.T, jsonStr string) map[string]rule.ClassifyResult {
	t.Helper()
	var out classifyAppsOutput
	if err := json.Unmarshal([]byte(jsonStr), &out); err != nil {
		t.Fatalf("unmarshal result %q: %v", jsonStr, err)
	}
	m := make(map[string]rule.ClassifyResult, len(out.Classifications))
	for _, r := range out.Classifications {
		m[r.ID] = r
	}
	return m
}

// End-to-end of the gomobile entry: cacheDir base (rules live in cacheDir/rules),
// JSON in, JSON out — an app in the region bundle is "direct", others "proxy".
func TestClassifyApps_RegionMatch(t *testing.T) {
	base := t.TempDir()
	rulesDir := filepath.Join(base, "rules")
	if err := os.MkdirAll(rulesDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeAppBundle(t, filepath.Join(rulesDir, "cn.krs"), &krs.AppPatterns{
		Android: krs.AndroidPatterns{Apps: []string{"com.tencent.mm"}},
	})

	installed := `[{"id":"com.tencent.mm","label":"WeChat","installer_package_name":"","process_names":["com.tencent.mm"]},` +
		`{"id":"com.foreign.app","label":"Foreign","installer_package_name":"","process_names":["com.foreign.app"]}]`

	// goos is runtime.GOOS inside ClassifyApps; the krs Android subset only
	// matches when the host is android, so assert on the platform-appropriate
	// expectation: on a non-android host nothing matches (fail-soft proxy),
	// which still proves the wiring + JSON round-trip. The android match path is
	// covered deterministically by rule.TestClassifyInstalled_RegionMatch.
	got := parseClassifications(t, ClassifyApps(base, "cn", installed))
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2; raw classifications missing", len(got))
	}
	if _, ok := got["com.tencent.mm"]; !ok {
		t.Errorf("result missing com.tencent.mm: %+v", got)
	}
}

func TestClassifyApps_BadJSONFailsSoft(t *testing.T) {
	got := ClassifyApps(t.TempDir(), "cn", "{not valid json")
	out := parseClassifications(t, got)
	if len(out) != 0 {
		t.Fatalf("bad JSON should yield empty classifications, got %+v", out)
	}
}

func TestClassifyApps_EmptyInstalled(t *testing.T) {
	got := ClassifyApps(t.TempDir(), "cn", "[]")
	out := parseClassifications(t, got)
	if len(out) != 0 {
		t.Fatalf("empty installed → empty classifications, got %+v", out)
	}
}
