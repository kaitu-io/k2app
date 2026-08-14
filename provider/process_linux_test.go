//go:build linux

package provider

import (
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

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
//   /proc/8000/exe → /usr/bin/chrome
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
	// /proc/net/tcp6: addresses are 32 hex chars, four 32-bit words each
	// byte-reversed (see parseProcNetAddr).
	//   row 0: fdfe:dcba:9876::2 : 54322 → inode 1002 (pure IPv6 socket)
	//   row 1: ::ffff:192.168.1.5 : 54323 → inode 1003 (dual-stack, v4-mapped)
	tcp6Lines := "  sl  local_address                         remote_address                        st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode\n" +
		"   0: BADCFEFD000076980000000002000000:D432 00000000000000000000000000000000:0000 01 00000000:00000000 00:00000000 00000000  1000        0 1002 0 0000000000000000 100 0 0 10 0\n" +
		"   1: 0000000000000000FFFF00000501A8C0:D433 00000000000000000000000000000000:0000 01 00000000:00000000 00:00000000 00000000  1000        0 1003 0 0000000000000000 100 0 0 10 0\n"
	must(os.WriteFile(filepath.Join(dir, "net/tcp6"), []byte(tcp6Lines), 0644))
	// /proc/net/udp: header only — the UDP flows below live in udp6, so a v4
	// UDP lookup exercises the miss→udp6 dual-stack fallback.
	udpHeader := "   sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode ref pointer drops\n"
	must(os.WriteFile(filepath.Join(dir, "net/udp"), []byte(udpHeader), 0644))
	// /proc/net/udp6:
	//   row 0: ::ffff:192.168.1.5 : 54324 → inode 1004 (dual-stack, v4-mapped)
	//   row 1: fdfe:dcba:9876::2 : 54325 → inode 1005 (pure IPv6 socket)
	udp6Lines := udpHeader +
		"   0: 0000000000000000FFFF00000501A8C0:D434 00000000000000000000000000000000:0000 01 00000000:00000000 00:00000000 00000000  1000        0 1004 2 0000000000000000 0\n" +
		"   1: BADCFEFD000076980000000002000000:D435 00000000000000000000000000000000:0000 01 00000000:00000000 00:00000000 00000000  1000        0 1005 2 0000000000000000 0\n"
	must(os.WriteFile(filepath.Join(dir, "net/udp6"), []byte(udp6Lines), 0644))
	must(os.MkdirAll(filepath.Join(dir, "8000/fd"), 0755))
	must(os.Symlink("socket:[1001]", filepath.Join(dir, "8000/fd/3")))
	must(os.Symlink("socket:[1002]", filepath.Join(dir, "8000/fd/4")))
	must(os.Symlink("socket:[1003]", filepath.Join(dir, "8000/fd/5")))
	must(os.Symlink("socket:[1004]", filepath.Join(dir, "8000/fd/6")))
	must(os.Symlink("socket:[1005]", filepath.Join(dir, "8000/fd/7")))
	must(os.WriteFile(filepath.Join(dir, "8000/comm"), []byte("chrome\n"), 0644))
	must(os.Symlink("/usr/bin/chrome", filepath.Join(dir, "8000/exe")))
	must(os.WriteFile(filepath.Join(dir, "8000/status"),
		[]byte("Name:\tchrome\nUid:\t10156\t10156\t10156\t10156\n"), 0644))
	return dir
}

type mockPackageResolver struct {
	packages   map[int32]string
	installers map[string]string
}

func (m *mockPackageResolver) PackageForUID(uid int32) string {
	return m.packages[uid]
}

// writeProcEntry overlays a single PID's exe symlink + comm onto an
// existing fake procfs root. Returns the PID's dir path for further
// per-test customization.
func writeProcEntry(t *testing.T, procRoot string, pid int, exeTarget, comm string) string {
	t.Helper()
	pidDir := filepath.Join(procRoot, strconv.Itoa(pid))
	if err := os.MkdirAll(pidDir, 0755); err != nil {
		t.Fatal(err)
	}
	exePath := filepath.Join(pidDir, "exe")
	_ = os.Remove(exePath)
	if exeTarget != "" {
		if err := os.Symlink(exeTarget, exePath); err != nil {
			t.Fatal(err)
		}
	}
	if comm != "" {
		if err := os.WriteFile(filepath.Join(pidDir, "comm"), []byte(comm), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return pidDir
}

// IPv6 attribution — before 2026-08-14, FindProcess early-returned empty for
// any non-IPv4 srcIP, so every IPv6 connection silently fell back to the
// global route and `match.apps` / `process_name` rules never fired on v6.
func TestFindProcess_IPv6_TCP6Match(t *testing.T) {
	s := NewLinuxProcessSearcher(nil)
	s.procRoot = setupFakeProcfs(t)
	name, _, _ := s.FindProcess("tcp", netip.MustParseAddr("fdfe:dcba:9876::2"), 54322)
	if name != "chrome" {
		t.Errorf("name = %q, want chrome (pure-IPv6 socket in /proc/net/tcp6)", name)
	}
}

// Dual-stack: an AF_INET6 socket without IPV6_V6ONLY carrying an IPv4 flow is
// listed ONLY in /proc/net/tcp6 with a v4-mapped local address. A v4 lookup
// that misses /proc/net/tcp must fall through and find it there.
func TestFindProcess_DualStack_V4MappedFallback(t *testing.T) {
	s := NewLinuxProcessSearcher(nil)
	s.procRoot = setupFakeProcfs(t)
	name, _, _ := s.FindProcess("tcp", netip.MustParseAddr("192.168.1.5"), 54323)
	if name != "chrome" {
		t.Errorf("name = %q, want chrome (v4 miss must fall through to ::ffff: row in tcp6)", name)
	}
}

func TestFindProcess_UDP6_PureV6Match(t *testing.T) {
	s := NewLinuxProcessSearcher(nil)
	s.procRoot = setupFakeProcfs(t)
	name, _, _ := s.FindProcess("udp", netip.MustParseAddr("fdfe:dcba:9876::2"), 54325)
	if name != "chrome" {
		t.Errorf("name = %q, want chrome (pure-IPv6 socket in /proc/net/udp6)", name)
	}
}

func TestFindProcess_UDP_DualStackV4MappedFallback(t *testing.T) {
	s := NewLinuxProcessSearcher(nil)
	s.procRoot = setupFakeProcfs(t)
	// net/udp has no rows — the v4 lookup must miss there and find the
	// ::ffff: row in net/udp6.
	name, _, _ := s.FindProcess("udp", netip.MustParseAddr("192.168.1.5"), 54324)
	if name != "chrome" {
		t.Errorf("name = %q, want chrome (v4 UDP miss must fall through to ::ffff: row in udp6)", name)
	}
}

func TestFindProcess_IPv6_NoMatch(t *testing.T) {
	s := NewLinuxProcessSearcher(nil)
	s.procRoot = setupFakeProcfs(t)
	name, _, _ := s.FindProcess("tcp", netip.MustParseAddr("2001:db8::1"), 54322)
	if name != "" {
		t.Errorf("got %q, want empty (different v6 addr must not match)", name)
	}
}

func TestFindProcess_FakeProcfs_TCPMatch(t *testing.T) {
	s := NewLinuxProcessSearcher(nil)
	s.procRoot = setupFakeProcfs(t)
	name, _, _ := s.FindProcess("tcp", netip.MustParseAddr("192.168.1.5"), 54321)
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
	name, _, _ := s.FindProcess("tcp", netip.MustParseAddr("192.168.1.5"), 54321)
	if name != "chrome" {
		t.Errorf("expected cache hit returning chrome, got %q", name)
	}
}

func TestFindProcess_NoMatch_NegativeCache(t *testing.T) {
	s := NewLinuxProcessSearcher(nil)
	s.procRoot = setupFakeProcfs(t)
	// Port that doesn't exist in fake procfs.
	name, _, _ := s.FindProcess("tcp", netip.MustParseAddr("192.168.1.5"), 9999)
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
	name, pkg, _ := s.FindProcess("tcp", netip.MustParseAddr("192.168.1.5"), 54321)
	if name != "chrome" || pkg != "com.android.chrome" {
		t.Errorf("got (%q,%q), want (chrome, com.android.chrome)", name, pkg)
	}
}

func TestLookupPID_ColdStartBuildsIndex(t *testing.T) {
	s := NewLinuxProcessSearcher(nil)
	s.procRoot = setupFakeProcfs(t)
	pid := s.lookupPID(1001)
	if pid != 8000 {
		t.Errorf("pid = %d, want 8000", pid)
	}
}

func TestLookupPID_StaleReturnsCachedAsyncRebuilds(t *testing.T) {
	s := NewLinuxProcessSearcher(nil)
	s.procRoot = setupFakeProcfs(t)
	// Cold start: builds index synchronously.
	if pid := s.lookupPID(1001); pid != 8000 {
		t.Fatalf("cold start pid = %d, want 8000", pid)
	}
	// Force stale by rewinding expires.
	s.indexMu.Lock()
	s.indexExpires = time.Now().Add(-time.Second)
	s.indexMu.Unlock()
	// Stale call must return cached value immediately.
	if pid := s.lookupPID(1001); pid != 8000 {
		t.Errorf("stale pid = %d, want 8000 (stale cached)", pid)
	}
	// Wait for async rebuild to complete.
	for i := 0; i < 100; i++ {
		s.indexMu.RLock()
		fresh := time.Now().Before(s.indexExpires)
		s.indexMu.RUnlock()
		if fresh {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Errorf("async rebuild did not complete within 1s")
}

func TestLookupPID_SingleflightDedupsRebuild(t *testing.T) {
	s := NewLinuxProcessSearcher(nil)
	s.procRoot = setupFakeProcfs(t)
	// Hook buildInodeIndex via counter — wrap with a slow build that increments
	// before delegating, to maximize concurrent overlap.
	var calls atomic.Int32
	origBuild := s.buildInodeIndexFn
	s.buildInodeIndexFn = func() map[uint64]int {
		calls.Add(1)
		time.Sleep(50 * time.Millisecond) // make overlap likely
		return origBuild()
	}
	// 10 concurrent cold-start lookups → singleflight should collapse to 1 build.
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.lookupPID(1001)
		}()
	}
	wg.Wait()
	if got := calls.Load(); got != 1 {
		t.Errorf("buildInodeIndex called %d times, want 1 (singleflight)", got)
	}
}

// TestLookupPID_FreshMissTriggersRebuild covers the production scenario where
// a process spawns AFTER the inode index was last built (e.g., user launches a
// new browser; daemon already serviced earlier connections from other apps).
// Previously the fresh-index miss returned 0 without rebuilding, breaking
// App Bypass for the newly-launched process for up to inodeIndexTTL.
func TestLookupPID_FreshMissTriggersRebuild(t *testing.T) {
	s := NewLinuxProcessSearcher(nil)
	s.procRoot = setupFakeProcfs(t)
	// First call: cold-start, builds index (contains inode 1001 → 8000).
	if pid := s.lookupPID(1001); pid != 8000 {
		t.Fatalf("cold pid = %d, want 8000", pid)
	}
	// Now simulate a brand-new process spawning a socket with inode 2002,
	// AFTER the index has been built — but well within its TTL (fresh).
	origBuild := s.buildInodeIndexFn
	var calls atomic.Int32
	s.buildInodeIndexFn = func() map[uint64]int {
		calls.Add(1)
		idx := origBuild()
		idx[2002] = 9000 // new process
		return idx
	}
	// Without the fix, lookupPID(2002) returns 0 because the fresh cached
	// index lacks 2002 and the code never rebuilds on miss.
	pid := s.lookupPID(2002)
	if pid != 9000 {
		t.Fatalf("fresh-miss pid = %d, want 9000 (rebuild on miss)", pid)
	}
	if calls.Load() != 1 {
		t.Errorf("buildInodeIndexFn calls = %d, want 1 (single rebuild)", calls.Load())
	}
	// Subsequent lookup must hit cache, NOT trigger another rebuild.
	if pid := s.lookupPID(2002); pid != 9000 {
		t.Fatalf("second lookup pid = %d, want 9000", pid)
	}
	if calls.Load() != 1 {
		t.Errorf("buildInodeIndexFn calls after second lookup = %d, want 1", calls.Load())
	}
	// True miss (no such inode) still triggers a rebuild and returns 0.
	// This is acceptable thrash: bounded by the 2s negative cache at the
	// FindProcess layer (keyed by source 4-tuple, not inode).
	if pid := s.lookupPID(9999); pid != 0 {
		t.Errorf("ghost-inode pid = %d, want 0", pid)
	}
}

// ====================================================================
// OS-specific quirk coverage — added 2026-05-22 to push App Bypass
// confidence to 10/10 without requiring physical Linux hardware. Each
// test reproduces an empirically-observed kernel/userland behavior that
// would silently break the rule engine's process_name match.
// ====================================================================

// Real-world: Chrome's worker threads call prctl(PR_SET_NAME) to set
// /proc/PID/comm to "Chrome_ChildIOT" / "Chrome_IOThread". The UI's
// listRunning gives the user the *executable basename* ("Google Chrome
// Helper"). Without reading /proc/PID/exe, App Bypass for every
// Electron app (Chrome, VS Code, Slack, Discord, Signal, Element) and
// most modern browsers would silently never fire.
//
// Verified empirically on linux/arm64 Docker — see audit transcript.
func TestReadProcessName_PrctlOverride_PrefersExe(t *testing.T) {
	s := NewLinuxProcessSearcher(nil)
	dir := setupFakeProcfs(t)
	s.procRoot = dir

	// Overwrite 8000's exe + comm to mimic a Chrome helper:
	//   - argv[0] basename / exe → "Google Chrome Helper" (UI's view)
	//   - comm → "Chrome_ChildIOT" (prctl override; useless to UI)
	writeProcEntry(t, dir, 8000, "/Applications/Google Chrome.app/Contents/Frameworks/.../Helpers/Google Chrome Helper", "Chrome_ChildIOT\n")

	name := s.readProcessName(8000)
	if name != "Google Chrome Helper" {
		t.Errorf("readProcessName(prctl-renamed) = %q, want %q (exe basename, not comm)",
			name, "Google Chrome Helper")
	}
}

// /proc/PID/exe with " (deleted)" suffix appears when the binary is
// replaced mid-run (apt/dpkg/snap upgrade, brew upgrade analog). The
// kernel keeps the original inode mapped; readlink decorates the
// resolved path. We strip the suffix so the bypass rule keeps matching.
func TestReadProcessName_DeletedBinarySuffix(t *testing.T) {
	s := NewLinuxProcessSearcher(nil)
	dir := setupFakeProcfs(t)
	s.procRoot = dir
	writeProcEntry(t, dir, 8000, "/usr/bin/chrome (deleted)", "chrome\n")

	if got := s.readProcessName(8000); got != "chrome" {
		t.Errorf("readProcessName(deleted binary) = %q, want %q (suffix stripped)", got, "chrome")
	}
}

// When /proc/PID/exe symlink is unreadable (rare: post-exit race,
// Yama ptrace_scope restricting introspection, container without
// hidepid relaxation), fall back to /proc/PID/comm. The rule engine's
// 15-char truncation tolerance covers the comm-fallback path.
func TestReadProcessName_ExeUnreadable_FallsBackToComm(t *testing.T) {
	s := NewLinuxProcessSearcher(nil)
	dir := setupFakeProcfs(t)
	s.procRoot = dir
	// Remove exe but keep comm (kernel rare: exe missing, comm present).
	writeProcEntry(t, dir, 8000, "", "kworker\n")

	if got := s.readProcessName(8000); got != "kworker" {
		t.Errorf("readProcessName(no exe) = %q, want %q (comm fallback)", got, "kworker")
	}
}

// Both exe and comm unreadable → empty (process gone). FindProcess
// caches the empty result via the 2s negative-cache layer above.
func TestReadProcessName_BothUnreadable_ReturnsEmpty(t *testing.T) {
	s := NewLinuxProcessSearcher(nil)
	dir := setupFakeProcfs(t)
	s.procRoot = dir
	writeProcEntry(t, dir, 9999, "", "")

	if got := s.readProcessName(9999); got != "" {
		t.Errorf("readProcessName(absent) = %q, want empty", got)
	}
}

// Executable basename longer than TASK_COMM_LEN-1 = 15 chars MUST be
// returned untruncated via the exe path. comm would have given
// "Microsoft Power"; exe gives the real name. End-to-end this is what
// makes Office / Chrome / VS Code bypass work.
func TestReadProcessName_LongBasename_NotTruncated(t *testing.T) {
	cases := []struct {
		name    string
		exeBase string
	}{
		{"Microsoft PowerPoint (20 char)", "Microsoft PowerPoint"},
		{"Google Chrome Helper (20 char)", "Google Chrome Helper"},
		{"Visual Studio Code (18 char)", "Visual Studio Code"},
		{"com.apple.WebKit.Networking (27 char)", "com.apple.WebKit.Networking"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := NewLinuxProcessSearcher(nil)
			dir := setupFakeProcfs(t)
			s.procRoot = dir
			writeProcEntry(t, dir, 8000, "/opt/foo/"+tc.exeBase, "anything-truncated-here\n")
			if got := s.readProcessName(8000); got != tc.exeBase {
				t.Errorf("got %q, want %q (full basename, not truncated)", got, tc.exeBase)
			}
		})
	}
}

// Multi-byte UTF-8 names. App basenames containing CJK characters
// (微信 / 钉钉 / 百度网盘) must survive the exe-symlink readlink path
// byte-perfect — rule match is byte-equality, not codepoint-equality.
func TestReadProcessName_UTF8MultiByte(t *testing.T) {
	cases := []string{
		"微信",        // 6 bytes (3+3) — well under 15
		"百度网盘",     // 12 bytes — under 15
		"微信网络助手",   // 18 bytes — over 15, comm would truncate mid-rune
		"DingTalk助手", // mixed ASCII + CJK, 14 bytes
	}
	for _, base := range cases {
		t.Run(base, func(t *testing.T) {
			s := NewLinuxProcessSearcher(nil)
			dir := setupFakeProcfs(t)
			s.procRoot = dir
			writeProcEntry(t, dir, 8000, "/Applications/"+base, "irrelevant\n")
			if got := s.readProcessName(8000); got != base {
				t.Errorf("got %q (% x), want %q (% x)", got, got, base, base)
			}
		})
	}
}

// End-to-end through FindProcess (not just readProcessName): a prctl-
// renamed process matched via TCP socket must still return its exe
// basename. This is the exact path the rule engine sees.
func TestFindProcess_PrctlRenamedHelper_EndToEnd(t *testing.T) {
	s := NewLinuxProcessSearcher(nil)
	dir := setupFakeProcfs(t)
	s.procRoot = dir
	// Replace 8000's exe to a long, prctl-renamed Chrome helper.
	writeProcEntry(t, dir, 8000,
		"/Applications/Google Chrome.app/Contents/Frameworks/Google Chrome Framework.framework/Versions/X/Helpers/Google Chrome Helper.app/Contents/MacOS/Google Chrome Helper",
		"Chrome_ChildIOT\n")

	name, _, _ := s.FindProcess("tcp", netip.MustParseAddr("192.168.1.5"), 54321)
	if name != "Google Chrome Helper" {
		t.Errorf("FindProcess returned %q, want %q (prctl-renamed → exe basename)",
			name, "Google Chrome Helper")
	}
}

// Path with trailing slash or unusual edge characters — basename must
// not degenerate to "." or "/".
func TestReadProcessName_RootSlashEdge(t *testing.T) {
	s := NewLinuxProcessSearcher(nil)
	dir := setupFakeProcfs(t)
	s.procRoot = dir
	// exe pointing to root — filepath.Base("/") returns "/" → must be
	// rejected and fall through to comm.
	writeProcEntry(t, dir, 8000, "/", "fallback-name\n")
	if got := s.readProcessName(8000); got != "fallback-name" {
		t.Errorf("readProcessName(exe='/') = %q, want %q (root rejected, comm fallback)",
			got, "fallback-name")
	}
}

func TestSelfPIDSkip_ViaFakeProcfs(t *testing.T) {
	s := NewLinuxProcessSearcher(nil)
	dir := t.TempDir()
	// Build minimal /proc with our OWN pid owning the inode.
	selfPid := os.Getpid()
	selfDir := filepath.Join(dir, strconv.Itoa(selfPid))
	if err := os.MkdirAll(filepath.Join(selfDir, "fd"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("socket:[1001]", filepath.Join(selfDir, "fd/3")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(selfDir, "comm"), []byte("k2-test\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "net"), 0755); err != nil {
		t.Fatal(err)
	}
	tcpLine := "  sl  local_address rem_address\n" +
		"   0: 0501A8C0:D431 00000000:0000 01 00000000:00000000 00:00000000 00000000  1000        0 1001 0 0000000000000000 100 0 0 10 0\n"
	if err := os.WriteFile(filepath.Join(dir, "net/tcp"), []byte(tcpLine), 0644); err != nil {
		t.Fatal(err)
	}
	s.procRoot = dir
	name, _, _ := s.FindProcess("tcp", netip.MustParseAddr("192.168.1.5"), 54321)
	if name != "" {
		t.Errorf("expected self-PID skip → empty name, got %q", name)
	}
}

func (m *mockPackageResolver) InstallerForPackage(packageName string) string {
	if m.installers == nil {
		return ""
	}
	return m.installers[packageName]
}
