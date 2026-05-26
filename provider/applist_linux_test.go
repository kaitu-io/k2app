//go:build linux && !android

package provider

import (
	"context"
	"strings"
	"testing"
)

func TestLinuxAppList_ParsesDesktopFiles(t *testing.T) {
	p := &linuxAppList{scanRoots: []string{"testdata/applist_linux"}}
	apps, err := p.ListInstalled(context.Background())
	if err != nil {
		t.Fatalf("ListInstalled: %v", err)
	}
	if len(apps) != 1 {
		t.Fatalf("want 1 (firefox; hidden skipped), got %d: %+v", len(apps), apps)
	}
	if apps[0].Label != "Firefox" {
		t.Errorf("Label %q", apps[0].Label)
	}
	if len(apps[0].ProcessNames) != 1 || apps[0].ProcessNames[0] != "firefox" {
		t.Errorf("ProcessNames %v want [firefox]", apps[0].ProcessNames)
	}
	if !strings.HasSuffix(apps[0].ID, "firefox.desktop") {
		t.Errorf("ID %q should end in firefox.desktop", apps[0].ID)
	}
}

func TestLinuxAppList_NoDir(t *testing.T) {
	p := &linuxAppList{scanRoots: []string{"testdata/nonexistent"}}
	apps, err := p.ListInstalled(context.Background())
	if err != nil {
		t.Fatalf("nonexistent dir should not error: %v", err)
	}
	if len(apps) != 0 {
		t.Fatalf("want empty, got %+v", apps)
	}
}

func TestFirstExecToken(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"/usr/lib/firefox/firefox %u", "/usr/lib/firefox/firefox"},
		{"/usr/bin/foo", "/usr/bin/foo"},
		{`"/path with space/bin" --arg`, "/path with space/bin"},
		{"firefox %F", "firefox"},
	}
	for _, c := range cases {
		got := firstExecToken(c.in)
		if got != c.want {
			t.Errorf("firstExecToken(%q) = %q want %q", c.in, got, c.want)
		}
	}
}
