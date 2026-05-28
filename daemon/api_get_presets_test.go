package daemon

import (
	"testing"
)

func TestAPI_GetPresets_EmptyCacheReturnsEmptyList(t *testing.T) {
	d := New()
	d.ruleCacheDir = t.TempDir()

	resp := callCore(t, d, "get-presets", nil)
	if resp.Code != 0 {
		t.Fatalf("code %d msg %q", resp.Code, resp.Message)
	}
	data := decodeData[getPresetsData](t, resp)
	if len(data.Regions) != 0 {
		t.Errorf("empty cache → empty list; got %+v", data.Regions)
	}
}

func TestAPI_GetPresets_BundleAppearsWithCounts(t *testing.T) {
	d := New()
	cacheDir := t.TempDir()
	d.ruleCacheDir = cacheDir
	writeCNBundle(t, cacheDir)

	resp := callCore(t, d, "get-presets", nil)
	if resp.Code != 0 {
		t.Fatalf("code %d", resp.Code)
	}
	data := decodeData[getPresetsData](t, resp)
	if len(data.Regions) != 1 {
		t.Fatalf("want 1 region; got %+v", data.Regions)
	}
	r := data.Regions[0]
	if r.ID != "cn" {
		t.Errorf("ID = %q, want cn", r.ID)
	}
	if r.SetCount != 1 {
		t.Errorf("SetCount = %d, want 1", r.SetCount)
	}
	if r.AppPatternCounts.Darwin == 0 && r.AppPatternCounts.Windows == 0 && r.AppPatternCounts.Android == 0 {
		t.Errorf("expected at least one app-pattern count > 0; got %+v", r.AppPatternCounts)
	}
}
