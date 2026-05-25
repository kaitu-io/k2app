//go:build linux

package provider

import (
	"testing"
)

func TestLinuxProcessSearcher_WithPackageResolver(t *testing.T) {
	mock := &mockPackageResolver{packages: map[int32]string{
		10156: "com.android.chrome",
	}}
	searcher := NewLinuxProcessSearcher(mock)
	if searcher.packageResolver == nil {
		t.Error("packageResolver should be set")
	}
}

type mockPackageResolver struct {
	packages   map[int32]string
	installers map[string]string
}

func (m *mockPackageResolver) PackageForUID(uid int32) string {
	return m.packages[uid]
}

func (m *mockPackageResolver) InstallerForPackage(packageName string) string {
	if m.installers == nil {
		return ""
	}
	return m.installers[packageName]
}
