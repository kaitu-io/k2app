//go:build windows

package provider

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestWindowsCollectExeNames_Shallow(t *testing.T) {
	// Use the test's own temp dir as install location stand-in.
	dir := t.TempDir()
	exe1 := filepath.Join(dir, "Game.exe")
	exe2 := filepath.Join(dir, "subdir", "Helper.exe")
	if err := os.WriteFile(exe1, []byte{}, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(exe2), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe2, []byte{}, 0644); err != nil {
		t.Fatal(err)
	}

	got := collectWindowsExeNames(dir, 2)
	sort.Strings(got)
	want := []string{"game.exe", "helper.exe"}
	if !equalStringSlice(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestWindowsCollectExeNames_DepthLimit(t *testing.T) {
	dir := t.TempDir()
	// nested depth 3: dir/a/b/c/Deep.exe — should NOT be returned with depth=2.
	deep := filepath.Join(dir, "a", "b", "c", "Deep.exe")
	if err := os.MkdirAll(filepath.Dir(deep), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(deep, []byte{}, 0644); err != nil {
		t.Fatal(err)
	}
	got := collectWindowsExeNames(dir, 2)
	if len(got) != 0 {
		t.Fatalf("depth=2 should not reach depth-3 file, got %v", got)
	}
}

func TestWindowsCollectExeNames_EmptyRoot(t *testing.T) {
	if got := collectWindowsExeNames("", 2); got != nil {
		t.Fatalf("empty root must return nil, got %v", got)
	}
}

func TestWindowsSkipsSystemComponent(t *testing.T) {
	cases := []struct {
		name         string
		displayName  string
		sysComp      bool
		parentKey    string
		wantSkipped  bool
	}{
		{"explicit_sys_component", "Whatever", true, "", true},
		{"parent_key_set", "Whatever", false, "{abc}", true},
		{"security_update_kb50", "KB5000123 Security Update", false, "", true},
		{"redistributable", "Visual C++ 2015 Redistributable", false, "", true},
		{"normal_app", "WeChat", false, "", false},
		{"firefox", "Mozilla Firefox", false, "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := isSystemComponent(c.displayName, "{guid}", c.sysComp, c.parentKey)
			if got != c.wantSkipped {
				t.Errorf("isSystemComponent(%q, sysComp=%v, parent=%q) = %v, want %v",
					c.displayName, c.sysComp, c.parentKey, got, c.wantSkipped)
			}
		})
	}
}

func TestWindowsExtractExeFromUninstallString(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{`"C:\Program Files\X\unins.exe" /S`, "unins.exe"},
		{`C:\Program Files\X\unins.exe /S`, "unins.exe"},
		{`C:\X\foo.exe`, "foo.exe"},
		{`"C:\X\foo.exe"`, "foo.exe"},
	}
	for _, c := range cases {
		got := extractExeFromUninstallString(c.in)
		if got != c.want {
			t.Errorf("extractExeFromUninstallString(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func equalStringSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
