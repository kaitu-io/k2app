package provider

import (
	"net/netip"
	"sync/atomic"
	"testing"
)

type fakeOwner struct {
	uid   int32
	calls atomic.Int32
	// captured last query for assertion
	lastNetwork          string
	lastSrcIP, lastDstIP string
	lastSrcPt, lastDstPt int32
}

func (f *fakeOwner) OwnerUID(network, srcIP string, srcPort int32, dstIP string, dstPort int32) int32 {
	f.calls.Add(1)
	f.lastNetwork, f.lastSrcIP, f.lastDstIP = network, srcIP, dstIP
	f.lastSrcPt, f.lastDstPt = srcPort, dstPort
	return f.uid
}

type fakePackages struct {
	byUID      map[int32]string // newline-joined
	installers map[string]string
	uidCalls   atomic.Int32
}

func (f *fakePackages) PackageForUID(uid int32) string {
	joined := f.byUID[uid]
	for i := range joined {
		if joined[i] == '\n' {
			return joined[:i]
		}
	}
	return joined
}

func (f *fakePackages) PackagesForUID(uid int32) string {
	f.uidCalls.Add(1)
	return f.byUID[uid]
}

func (f *fakePackages) InstallerForPackage(pkg string) string {
	return f.installers[pkg]
}

type fakeFallback struct {
	name, pkg, installer string
	calls                atomic.Int32
}

func (f *fakeFallback) FindProcess(string, netip.Addr, uint16) (string, string, string) {
	f.calls.Add(1)
	return f.name, f.pkg, f.installer
}

func TestOwnerSearcher_ResolvesUIDToAllSharedPackages(t *testing.T) {
	owner := &fakeOwner{uid: 10123}
	pkgs := &fakePackages{
		byUID:      map[int32]string{10123: "com.google.android.gms\ncom.android.vending"},
		installers: map[string]string{"com.google.android.gms": "com.android.vending"},
	}
	s := NewOwnerProcessSearcher(owner, pkgs, nil)

	name, got, installer := s.FindProcessConn("tcp",
		netip.MustParseAddr("10.0.0.2"), 40000,
		netip.MustParseAddr("142.250.0.1"), 443)

	if name != "" {
		t.Errorf("processName should stay empty on owner path, got %q", name)
	}
	if len(got) != 2 || got[0] != "com.google.android.gms" || got[1] != "com.android.vending" {
		t.Errorf("packages = %v, want both shared-UID packages", got)
	}
	if installer != "com.android.vending" {
		t.Errorf("installer = %q, want primary package's installer", installer)
	}
	if owner.lastNetwork != "tcp" || owner.lastSrcIP != "10.0.0.2" || owner.lastSrcPt != 40000 ||
		owner.lastDstIP != "142.250.0.1" || owner.lastDstPt != 443 {
		t.Errorf("owner queried with %s %s:%d→%s:%d",
			owner.lastNetwork, owner.lastSrcIP, owner.lastSrcPt, owner.lastDstIP, owner.lastDstPt)
	}
}

func TestOwnerSearcher_NegativeUIDFallsBackToProcPath(t *testing.T) {
	owner := &fakeOwner{uid: -1}
	fb := &fakeFallback{name: "comm", pkg: "com.example.app", installer: "com.xiaomi.market"}
	s := NewOwnerProcessSearcher(owner, &fakePackages{}, fb)

	name, pkgs, installer := s.FindProcessConn("udp",
		netip.MustParseAddr("10.0.0.2"), 40001,
		netip.MustParseAddr("1.2.3.4"), 53)

	if fb.calls.Load() != 1 {
		t.Fatalf("fallback calls = %d, want 1", fb.calls.Load())
	}
	if name != "comm" || len(pkgs) != 1 || pkgs[0] != "com.example.app" || installer != "com.xiaomi.market" {
		t.Errorf("got (%q, %v, %q), want fallback identity", name, pkgs, installer)
	}
}

func TestOwnerSearcher_InvalidDstUsesFallbackNotOwner(t *testing.T) {
	owner := &fakeOwner{uid: 10001}
	fb := &fakeFallback{}
	s := NewOwnerProcessSearcher(owner, &fakePackages{}, fb)

	s.FindProcessConn("tcp", netip.MustParseAddr("10.0.0.2"), 40002, netip.Addr{}, 0)

	if owner.calls.Load() != 0 {
		t.Errorf("owner must not be queried without a valid dst; calls = %d", owner.calls.Load())
	}
	if fb.calls.Load() != 1 {
		t.Errorf("fallback calls = %d, want 1", fb.calls.Load())
	}
}

func TestOwnerSearcher_CachesBySourceSocket(t *testing.T) {
	owner := &fakeOwner{uid: 10200}
	pkgs := &fakePackages{byUID: map[int32]string{10200: "com.example.one"}}
	s := NewOwnerProcessSearcher(owner, pkgs, nil)

	src := netip.MustParseAddr("10.0.0.2")
	dst := netip.MustParseAddr("8.8.8.8")
	s.FindProcessConn("tcp", src, 41000, dst, 443)
	_, got, _ := s.FindProcessConn("tcp", src, 41000, dst, 443)

	if owner.calls.Load() != 1 {
		t.Errorf("owner calls = %d, want 1 (second lookup must hit cache)", owner.calls.Load())
	}
	if len(got) != 1 || got[0] != "com.example.one" {
		t.Errorf("cached packages = %v", got)
	}
}

func TestOwnerSearcher_UIDCacheAvoidsRepeatPackageLookups(t *testing.T) {
	owner := &fakeOwner{uid: 10300}
	pkgs := &fakePackages{byUID: map[int32]string{10300: "com.example.two"}}
	s := NewOwnerProcessSearcher(owner, pkgs, nil)

	dst := netip.MustParseAddr("8.8.8.8")
	// Two DIFFERENT source ports → two owner queries, but one shared UID.
	s.FindProcessConn("tcp", netip.MustParseAddr("10.0.0.2"), 42000, dst, 443)
	s.FindProcessConn("tcp", netip.MustParseAddr("10.0.0.2"), 42001, dst, 443)

	if owner.calls.Load() != 2 {
		t.Fatalf("owner calls = %d, want 2 (distinct sockets)", owner.calls.Load())
	}
	if pkgs.uidCalls.Load() != 1 {
		t.Errorf("PackagesForUID calls = %d, want 1 (uid cache)", pkgs.uidCalls.Load())
	}
}

func TestOwnerSearcher_UnmapsMappedV4BeforeQuery(t *testing.T) {
	owner := &fakeOwner{uid: 10400}
	pkgs := &fakePackages{byUID: map[int32]string{10400: "com.example.v4"}}
	s := NewOwnerProcessSearcher(owner, pkgs, nil)

	src := netip.MustParseAddr("::ffff:10.0.0.2")
	dst := netip.MustParseAddr("::ffff:1.2.3.4")
	s.FindProcessConn("tcp", src, 43000, dst, 443)

	if owner.lastSrcIP != "10.0.0.2" || owner.lastDstIP != "1.2.3.4" {
		t.Errorf("owner saw (%s → %s), want unmapped IPv4 literals", owner.lastSrcIP, owner.lastDstIP)
	}
}

func TestOwnerSearcher_EmptyPackagesForUIDFallsBackToSingle(t *testing.T) {
	owner := &fakeOwner{uid: 10500}
	pkgs := &fakePackages{byUID: map[int32]string{}} // PackagesForUID → ""
	s := NewOwnerProcessSearcher(owner, singleOnlyResolver{pkgs}, nil)

	_, got, _ := s.FindProcessConn("tcp",
		netip.MustParseAddr("10.0.0.2"), 44000, netip.MustParseAddr("8.8.8.8"), 443)
	if len(got) != 1 || got[0] != "com.single.only" {
		t.Errorf("packages = %v, want PackageForUID fallback", got)
	}
}

// singleOnlyResolver simulates a resolver whose PackagesForUID yields nothing
// but whose singular PackageForUID still answers.
type singleOnlyResolver struct{ *fakePackages }

func (singleOnlyResolver) PackageForUID(int32) string { return "com.single.only" }

func TestOwnerSearcher_FindProcessWithoutDstDelegatesToFallback(t *testing.T) {
	owner := &fakeOwner{uid: 10600}
	fb := &fakeFallback{name: "n", pkg: "p", installer: "i"}
	s := NewOwnerProcessSearcher(owner, &fakePackages{}, fb)

	name, pkg, installer := s.FindProcess("tcp", netip.MustParseAddr("10.0.0.2"), 45000)

	if owner.calls.Load() != 0 {
		t.Errorf("owner must not be queried from FindProcess")
	}
	if name != "n" || pkg != "p" || installer != "i" {
		t.Errorf("got (%q,%q,%q), want fallback passthrough", name, pkg, installer)
	}
}

func TestSplitPackages(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"", 0},
		{"a", 1},
		{"a\nb", 2},
		{"a\n\n b \n", 2},
		{"\n\n", 0},
	}
	for _, c := range cases {
		if got := splitPackages(c.in); len(got) != c.want {
			t.Errorf("splitPackages(%q) = %v, want %d entries", c.in, got, c.want)
		}
	}
}
