//go:build darwin && !ios

package provider

import (
	"context"
	"strings"
	"testing"
)

func TestDarwinAppList_ScansBundles(t *testing.T) {
	// Use the testdata dir as the only scan root to isolate from host state.
	p := &darwinAppList{scanRoots: []string{"testdata/applist_darwin"}}
	apps, err := p.ListInstalled(context.Background())
	if err != nil {
		t.Fatalf("ListInstalled: %v", err)
	}
	if len(apps) != 1 {
		t.Fatalf("want 1 app, got %d: %+v", len(apps), apps)
	}
	a := apps[0]
	if !strings.HasSuffix(a.ID, "TestApp.app") {
		t.Errorf("ID mismatch: %q", a.ID)
	}
	if a.Label != "Test App" { // DisplayName wins over Name
		t.Errorf("Label %q want %q", a.Label, "Test App")
	}
	if len(a.ProcessNames) != 1 || a.ProcessNames[0] != "TestApp" {
		t.Errorf("ProcessNames %v; want [TestApp] (case preserved for krs case-sensitive Darwin matcher)", a.ProcessNames)
	}
}

func TestDarwinAppList_NoBundles(t *testing.T) {
	p := &darwinAppList{scanRoots: []string{"testdata/nonexistent_dir"}}
	apps, err := p.ListInstalled(context.Background())
	if err != nil {
		t.Fatalf("nonexistent dir should not error: %v", err)
	}
	if len(apps) != 0 {
		t.Fatalf("want empty, got %+v", apps)
	}
}
