//go:build linux

package provider

import (
	"net/netip"
	"os"
	"path/filepath"
	"testing"
)

type mockPackageResolver struct {
	packages map[int32]string
}

func (m *mockPackageResolver) PackageForUID(uid int32) string {
	return m.packages[uid]
}

func TestLinuxProcessSearcher_PreservesResolverSignature(t *testing.T) {
	mock := &mockPackageResolver{packages: map[int32]string{
		10156: "com.android.chrome",
	}}
	searcher := NewLinuxProcessSearcher(mock)
	if searcher.packageResolver == nil {
		t.Error("packageResolver should be set")
	}
}

// setupFakeProcfs builds a minimal /proc tree:
//   /proc/net/tcp: socket inode 1001 owned by 192.168.1.5:54321 (state 01)
//   /proc/8000/fd/3 → socket:[1001]
//   /proc/8000/comm: "chrome\n"
//   /proc/8000/status: includes "Uid:\t10156\t..."
func setupFakeProcfs(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("setup: %v", err)
		}
	}
	must(os.MkdirAll(filepath.Join(dir, "net"), 0755))
	// /proc/net/tcp line: local_address 0501A8C0:D431 = 192.168.1.5:54321
	//   (LE uint32 0x0501A8C0 written as hex chars in proc format)
	tcpLine := "  sl  local_address rem_address   st tx_queue:rx_queue tr:tm->when retrnsmt   uid  timeout inode\n" +
		"   0: 0501A8C0:D431 00000000:0000 01 00000000:00000000 00:00000000 00000000  1000        0 1001 0 0000000000000000 100 0 0 10 0\n"
	must(os.WriteFile(filepath.Join(dir, "net/tcp"), []byte(tcpLine), 0644))
	must(os.MkdirAll(filepath.Join(dir, "8000/fd"), 0755))
	must(os.Symlink("socket:[1001]", filepath.Join(dir, "8000/fd/3")))
	must(os.WriteFile(filepath.Join(dir, "8000/comm"), []byte("chrome\n"), 0644))
	must(os.WriteFile(filepath.Join(dir, "8000/status"),
		[]byte("Name:\tchrome\nUid:\t10156\t10156\t10156\t10156\n"), 0644))
	return dir
}

func TestFindProcess_IPv6Early(t *testing.T) {
	s := NewLinuxProcessSearcher(nil)
	s.procRoot = "/nonexistent" // would fail if reached
	name, pkg := s.FindProcess("tcp", netip.MustParseAddr("2001:db8::1"), 54321)
	if name != "" || pkg != "" {
		t.Errorf("got (%q,%q), want empty", name, pkg)
	}
}

func TestFindProcess_FakeProcfs_TCPMatch(t *testing.T) {
	s := NewLinuxProcessSearcher(nil)
	s.procRoot = setupFakeProcfs(t)
	name, _ := s.FindProcess("tcp", netip.MustParseAddr("192.168.1.5"), 54321)
	if name != "chrome" {
		t.Errorf("name = %q, want chrome", name)
	}
}

func TestFindProcess_CacheHit_NoProcfsRescan(t *testing.T) {
	s := NewLinuxProcessSearcher(nil)
	s.procRoot = setupFakeProcfs(t)
	// First lookup populates cache.
	s.FindProcess("tcp", netip.MustParseAddr("192.168.1.5"), 54321)
	// Now delete procfs — second lookup must hit cache, not procfs.
	if err := os.RemoveAll(s.procRoot); err != nil {
		t.Fatal(err)
	}
	name, _ := s.FindProcess("tcp", netip.MustParseAddr("192.168.1.5"), 54321)
	if name != "chrome" {
		t.Errorf("expected cache hit returning chrome, got %q", name)
	}
}

func TestFindProcess_NoMatch_NegativeCache(t *testing.T) {
	s := NewLinuxProcessSearcher(nil)
	s.procRoot = setupFakeProcfs(t)
	// Port that doesn't exist in fake procfs.
	name, _ := s.FindProcess("tcp", netip.MustParseAddr("192.168.1.5"), 9999)
	if name != "" {
		t.Errorf("got %q, want empty", name)
	}
}

func TestFindProcess_PackageResolution(t *testing.T) {
	mock := &mockPackageResolver{packages: map[int32]string{
		10156: "com.android.chrome",
	}}
	s := NewLinuxProcessSearcher(mock)
	s.procRoot = setupFakeProcfs(t)
	name, pkg := s.FindProcess("tcp", netip.MustParseAddr("192.168.1.5"), 54321)
	if name != "chrome" || pkg != "com.android.chrome" {
		t.Errorf("got (%q,%q), want (chrome, com.android.chrome)", name, pkg)
	}
}
