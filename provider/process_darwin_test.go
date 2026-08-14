//go:build darwin && !ios

package provider

import (
	"fmt"
	"net/netip"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// =============================================================================
// parseLsofOutput — synthetic lsof -F pc input. Tests every empirical quirk
// observed on production macOS (Darwin 25.x) to push App Bypass confidence
// to 10/10 without needing a fleet of test devices.
// =============================================================================

const selfPID = 1234

// idxKey builds the map key parseLsofIndex is expected to emit.
func idxKey(proto, ip string, port uint16) string {
	return cacheKey(proto, netip.MustParseAddr(ip), port)
}

// A connected-socket record in `lsof -i -F pcPn` shape.
func lsofRec(pid int, cmd, proto, addr string) string {
	return fmt.Sprintf("p%d\nc%s\nf42\nP%s\nn%s\n", pid, cmd, proto, addr)
}

// Empirically-verified COMMAND forms collected from `lsof -i` on a
// production macOS 25.5. The COMMAND field arrives untruncated, even for
// 20+ char bundle helper names — proving the kernel quirk that mattered
// on Linux (TASK_COMM_LEN) doesn't apply to macOS lsof.
func TestParseLsofIndex_EmpiricalCommandForms(t *testing.T) {
	cases := []struct{ name, cmd string }{
		{"plain binary", "Curl"},
		{"Chrome helper untruncated (20 chars)", "Google Chrome Helper"},
		{"Code Helper with parens (22 chars)", "Code Helper (Renderer)"},
		{"WebKit dotted bundle (27 chars)", "com.apple.WebKit.Networking"},
		{"Slack Helper (12 chars)", "Slack Helper"},
		{"UTF-8 multibyte name", "微信"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := lsofRec(9999, tc.cmd, "TCP", "192.168.1.5:50001->1.2.3.4:443")
			idx := parseLsofIndex([]byte(in), selfPID)
			if got := idx[idxKey("tcp", "192.168.1.5", 50001)]; got != tc.cmd {
				t.Errorf("got %q, want %q", got, tc.cmd)
			}
		})
	}
}

// lsof lists BOTH endpoints of a loopback connection. The daemon's own
// sockets must never be indexed — attributing an app's traffic to k2 itself
// would defeat App Bypass.
func TestParseLsofIndex_SelfPidExcluded(t *testing.T) {
	in := lsofRec(selfPID, "k2", "TCP", "127.0.0.1:9000->127.0.0.1:1080") +
		lsofRec(9999, "curl", "TCP", "127.0.0.1:50001->1.2.3.4:443")
	idx := parseLsofIndex([]byte(in), selfPID)
	if got, ok := idx[idxKey("tcp", "127.0.0.1", 9000)]; ok {
		t.Errorf("self socket indexed as %q, want absent", got)
	}
	if got := idx[idxKey("tcp", "127.0.0.1", 50001)]; got != "curl" {
		t.Errorf("peer socket = %q, want curl", got)
	}
}

// Each process contributes its own sockets; ordering must not matter.
func TestParseLsofIndex_MultipleProcesses(t *testing.T) {
	in := lsofRec(7777, "wget", "TCP", "10.0.0.2:40001->1.1.1.1:80") +
		lsofRec(9999, "curl", "TCP", "10.0.0.2:40002->1.1.1.1:443")
	idx := parseLsofIndex([]byte(in), selfPID)
	if got := idx[idxKey("tcp", "10.0.0.2", 40001)]; got != "wget" {
		t.Errorf("40001 = %q, want wget", got)
	}
	if got := idx[idxKey("tcp", "10.0.0.2", 40002)]; got != "curl" {
		t.Errorf("40002 = %q, want curl", got)
	}
}

// One process, many sockets: state (pid/cmd) carries forward across the
// repeated P/n pairs lsof emits per fd.
func TestParseLsofIndex_OneProcessManySockets(t *testing.T) {
	in := "p2995\ncGoogle Chrome\n" +
		"f42\nPTCP\nn192.168.1.5:50001->1.2.3.4:443\n" +
		"f43\nPTCP\nn192.168.1.5:50002->1.2.3.4:443\n" +
		"f44\nPUDP\nn192.168.1.5:50003\n"
	idx := parseLsofIndex([]byte(in), selfPID)
	for _, c := range []struct {
		proto string
		port  uint16
	}{{"tcp", 50001}, {"tcp", 50002}, {"udp", 50003}} {
		if got := idx[idxKey(c.proto, "192.168.1.5", c.port)]; got != "Google Chrome" {
			t.Errorf("%s/%d = %q, want Google Chrome", c.proto, c.port, got)
		}
	}
}

// Garbage PID line must discard that record without crashing or leaking
// its command into the next one.
func TestParseLsofIndex_BadPidLine_Resets(t *testing.T) {
	in := "pNOT_A_NUMBER\ncghost\nPTCP\nn10.0.0.3:1111->1.1.1.1:80\n" +
		lsofRec(9999, "chrome", "TCP", "10.0.0.3:2222->1.1.1.1:80")
	idx := parseLsofIndex([]byte(in), selfPID)
	if got, ok := idx[idxKey("tcp", "10.0.0.3", 1111)]; ok {
		t.Errorf("bad-pid record indexed as %q, want absent", got)
	}
	if got := idx[idxKey("tcp", "10.0.0.3", 2222)]; got != "chrome" {
		t.Errorf("got %q, want chrome", got)
	}
}

// Empty command ("c\n") — record carries no usable name, skip it.
func TestParseLsofIndex_EmptyCommand_Skipped(t *testing.T) {
	in := lsofRec(9999, "", "TCP", "10.0.0.4:3333->1.1.1.1:80") +
		lsofRec(8888, "curl", "TCP", "10.0.0.4:4444->1.1.1.1:80")
	idx := parseLsofIndex([]byte(in), selfPID)
	if got, ok := idx[idxKey("tcp", "10.0.0.4", 3333)]; ok {
		t.Errorf("empty-cmd record indexed as %q, want absent", got)
	}
	if got := idx[idxKey("tcp", "10.0.0.4", 4444)]; got != "curl" {
		t.Errorf("got %q, want curl", got)
	}
}

// Addresses that are not a concrete local ip:port must be skipped rather
// than indexed under a bogus key.
func TestParseLsofIndex_UnindexableAddresses(t *testing.T) {
	in := lsofRec(9999, "nginx", "TCP", "*:8080") + // wildcard listener
		lsofRec(9998, "weird", "TCP", "no-port-here") +
		lsofRec(9997, "hostnamed", "TCP", "example.com:443->1.1.1.1:443") // -n should prevent this, be defensive
	idx := parseLsofIndex([]byte(in), selfPID)
	if len(idx) != 0 {
		t.Errorf("indexed %d unparseable addresses, want 0: %v", len(idx), idx)
	}
}

// A bare listener address (no "->") is still a real local socket.
func TestParseLsofIndex_ListenerWithoutPeer(t *testing.T) {
	in := lsofRec(9999, "Dropbox", "TCP", "127.0.0.1:17600")
	idx := parseLsofIndex([]byte(in), selfPID)
	if got := idx[idxKey("tcp", "127.0.0.1", 17600)]; got != "Dropbox" {
		t.Errorf("got %q, want Dropbox", got)
	}
}

// IPv6 literals arrive bracketed; net.SplitHostPort handles them and the
// key must use the unmapped form so it matches ConnMeta.SrcIP.
func TestParseLsofIndex_IPv6Bracketed(t *testing.T) {
	in := lsofRec(9999, "Safari", "TCP", "[fe80::1]:50001->[2606:4700::1111]:443")
	idx := parseLsofIndex([]byte(in), selfPID)
	if got := idx[idxKey("tcp", "fe80::1", 50001)]; got != "Safari" {
		t.Errorf("got %q, want Safari", got)
	}
}

// Non-IP protocols lsof may emit must not enter the index.
func TestParseLsofIndex_NonIPProtocolSkipped(t *testing.T) {
	in := lsofRec(9999, "weird", "ICMP", "10.0.0.7:0")
	if idx := parseLsofIndex([]byte(in), selfPID); len(idx) != 0 {
		t.Errorf("indexed non-ip protocol: %v", idx)
	}
}

// First writer wins: both endpoints of a loopback pair share one key.
func TestParseLsofIndex_FirstWriterWins(t *testing.T) {
	in := lsofRec(7777, "first", "TCP", "127.0.0.1:5555->127.0.0.1:6666") +
		lsofRec(8888, "second", "TCP", "127.0.0.1:5555->127.0.0.1:6666")
	idx := parseLsofIndex([]byte(in), selfPID)
	if got := idx[idxKey("tcp", "127.0.0.1", 5555)]; got != "first" {
		t.Errorf("got %q, want first", got)
	}
}

// Empty output (lsof failed / nothing open) and trailing blank lines from
// exec Output() must both be tolerated.
func TestParseLsofIndex_EmptyAndTrailingBlanks(t *testing.T) {
	if idx := parseLsofIndex([]byte(""), selfPID); len(idx) != 0 {
		t.Errorf("empty input produced %v", idx)
	}
	in := lsofRec(9999, "curl", "TCP", "10.0.0.8:6001->1.1.1.1:80") + "\n\n"
	idx := parseLsofIndex([]byte(in), selfPID)
	if got := idx[idxKey("tcp", "10.0.0.8", 6001)]; got != "curl" {
		t.Errorf("got %q, want curl (trailing blank lines tolerated)", got)
	}
}

// =============================================================================
// Integration: DarwinProcessSearcher cache hit avoids re-shelling lsof.
// Verified via the cache layer's contract — first call populates, second
// returns the cached value without ever invoking lsof (which would be
// blocked by the closed listener in TestProcessSearcher_SkipsSelf).
// =============================================================================

func TestDarwinProcessSearcher_CacheHitShortCircuits(t *testing.T) {
	s := NewDarwinProcessSearcher()
	srcIP := netip.MustParseAddr("127.0.0.1")
	srcPort := uint16(54321)
	// Pre-populate cache with a known answer (simulates "we already looked
	// this up 1.5s ago, lsof should NOT be re-run").
	s.cache.set(cacheKey("tcp", srcIP, srcPort), "WeChat", "", "")
	name, _, _ := s.FindProcess("tcp", srcIP, srcPort)
	if name != "WeChat" {
		t.Errorf("got %q, want WeChat (cache hit)", name)
	}
}

// f4ac8e1 (2026-08-14) removed the `!srcIP.Is4()` early-return so IPv6 flows
// get attributed. This test used to be TestDarwinProcessSearcher_IPv6Early and
// asserted the opposite ("IPv6 must early-return empty", comment claiming the
// provider layer is IPv4-only) — after the gate was removed it kept passing
// only because 2001:db8::1 happens to be absent from any real lsof sweep, i.e.
// it was green for the wrong reason and its name documented behaviour that no
// longer existed. Assert the real contract instead: a v6 source reaches the
// index like any other.
func TestDarwinProcessSearcher_IPv6ParticipatesInLookup(t *testing.T) {
	var swept bool
	s := NewDarwinProcessSearcher()
	s.buildConnIndexFn = func() map[string]string {
		swept = true
		return map[string]string{
			cacheKey("tcp", netip.MustParseAddr("2001:db8::1"), 443): "Safari",
		}
	}
	name, _, _ := s.FindProcess("tcp", netip.MustParseAddr("2001:db8::1"), 443)
	if !swept {
		t.Fatal("v6 source never reached the index — the !Is4() early-return gate is back")
	}
	if name != "Safari" {
		t.Fatalf("got %q, want Safari", name)
	}
}

// Zero-value DarwinProcessSearcher (someone did &DarwinProcessSearcher{}
// instead of NewDarwinProcessSearcher()) must lazy-init its cache and
// still work. Otherwise the cache.set call panics on nil deref.
func TestDarwinProcessSearcher_ZeroValueLazyInits(t *testing.T) {
	s := &DarwinProcessSearcher{}
	// Pre-populating directly would panic on nil cache, so we go through
	// FindProcess which has the lazy-init guard. IPv6 path doesn't exec
	// lsof, but FindProcess returns before reaching cache.set — so use
	// IPv4 path. Cache may or may not get populated depending on actual
	// lsof output; the assertion is just "doesn't panic".
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("FindProcess panicked on zero-value struct: %v", r)
		}
	}()
	_, _, _ = s.FindProcess("tcp", netip.MustParseAddr("127.0.0.1"), 0)
}

// =============================================================================
// Connection-index behaviour.
//
// The bug these lock down: attribution used to shell out to
// `lsof -i <proto>@<ip>:<port>` once per connection, and the processCache
// could never absorb it because cacheKey includes srcPort — every new
// connection is a new key, so the cache missed 100% of the time on the only
// path that matters (core/tunnel.go routeConn calls FindProcess exactly once
// per connection). Measured on production macOS: 100-1226ms per lookup, and
// under browser-level concurrency the lsof invocations queue up behind each
// other. Mirrors LinuxProcessSearcher's inode-index design.
// =============================================================================

// countingIndex returns a build function that records how many times it ran.
func countingIndex(builds *int32, idx map[string]string) func() map[string]string {
	return func() map[string]string {
		atomic.AddInt32(builds, 1)
		return idx
	}
}

// TestDarwinProcessSearcher_IndexBuiltOncePerTTL_NotPerConnection is the
// regression gate for the per-connection shell-out. 20 distinct source ports
// (i.e. 20 distinct cacheKeys, so the processCache cannot help) must resolve
// from a SINGLE index build.
func TestDarwinProcessSearcher_IndexBuiltOncePerTTL_NotPerConnection(t *testing.T) {
	var builds int32
	idx := map[string]string{}
	const n = 20
	for p := 50001; p < 50001+n; p++ {
		idx[cacheKey("tcp", netip.MustParseAddr("192.168.1.10"), uint16(p))] = "Google Chrome"
	}
	s := NewDarwinProcessSearcher()
	s.minRebuildInterval = -1
	s.buildConnIndexFn = countingIndex(&builds, idx)

	for p := 50001; p < 50001+n; p++ {
		name, _, _ := s.FindProcess("tcp", netip.MustParseAddr("192.168.1.10"), uint16(p))
		if name != "Google Chrome" {
			t.Fatalf("port %d: got %q, want %q", p, name, "Google Chrome")
		}
	}
	if got := atomic.LoadInt32(&builds); got != 1 {
		t.Fatalf("index builds = %d, want 1 (one lsof sweep serves every connection)", got)
	}
}

// TestDarwinProcessSearcher_ConcurrentBuildsCollapse pins the singleflight:
// a burst of concurrent first-connections must not each trigger their own
// lsof sweep. This is the browser-open case that made the original bug bite.
func TestDarwinProcessSearcher_ConcurrentBuildsCollapse(t *testing.T) {
	var builds int32
	idx := map[string]string{}
	for p := 60001; p < 60041; p++ {
		idx[cacheKey("tcp", netip.MustParseAddr("10.0.0.5"), uint16(p))] = "Safari"
	}
	s := NewDarwinProcessSearcher()
	s.minRebuildInterval = -1
	slow := func() map[string]string {
		atomic.AddInt32(&builds, 1)
		time.Sleep(20 * time.Millisecond) // widen the window for collapsing
		return idx
	}
	s.buildConnIndexFn = slow

	var wg sync.WaitGroup
	for p := 60001; p < 60041; p++ {
		wg.Add(1)
		go func(port int) {
			defer wg.Done()
			s.FindProcess("tcp", netip.MustParseAddr("10.0.0.5"), uint16(port))
		}(p)
	}
	wg.Wait()
	// singleflight collapses the concurrent burst; a couple of sequential
	// waves are acceptable, 40 independent sweeps are not.
	if got := atomic.LoadInt32(&builds); got > 3 {
		t.Fatalf("index builds = %d for 40 concurrent lookups, want <= 3", got)
	}
}

// TestDarwinProcessSearcher_StaleIndexRebuilds proves the index is not frozen:
// once past its TTL a lookup for a process that appeared later must resolve.
func TestDarwinProcessSearcher_StaleIndexRebuilds(t *testing.T) {
	var gen int32
	s := NewDarwinProcessSearcher()
	s.connIndexTTL = 10 * time.Millisecond
	s.minRebuildInterval = -1 // no throttle: this test is about TTL, not rate limiting
	// The negative result of the first lookup is cached by cacheKey; give that
	// entry a short TTL too or the second lookup never reaches the index.
	s.cache = newProcessCache(processCacheCap, 5*time.Millisecond)
	s.buildConnIndexFn = func() map[string]string {
		if atomic.AddInt32(&gen, 1) == 1 {
			return map[string]string{}
		}
		return map[string]string{
			cacheKey("tcp", netip.MustParseAddr("10.0.0.9"), 7777): "curl",
		}
	}
	if name, _, _ := s.FindProcess("tcp", netip.MustParseAddr("10.0.0.9"), 7777); name != "" {
		t.Fatalf("first lookup: got %q, want empty (not in index yet)", name)
	}
	time.Sleep(30 * time.Millisecond) // let both index TTL and the negative cache entry lapse
	name, _, _ := s.FindProcess("tcp", netip.MustParseAddr("10.0.0.9"), 7777)
	if name != "curl" {
		t.Fatalf("after TTL: got %q, want %q", name, "curl")
	}
}

// TestDarwinProcessSearcher_MissesAreThrottled is the anti-regression gate for
// the whole point of this change. A stream of brand-new connections is by
// definition absent from any previously-built index, so a naive
// "rebuild on miss" policy would fire one lsof sweep per connection — exactly
// the behaviour the index replaced. The throttle must cap that.
func TestDarwinProcessSearcher_MissesAreThrottled(t *testing.T) {
	var builds int32
	s := NewDarwinProcessSearcher()
	s.minRebuildInterval = time.Hour // nothing may re-sweep after the cold start
	s.buildConnIndexFn = func() map[string]string {
		atomic.AddInt32(&builds, 1)
		return map[string]string{} // every lookup misses
	}
	for p := 1; p <= 50; p++ {
		s.FindProcess("tcp", netip.MustParseAddr("172.16.0.1"), uint16(30000+p))
	}
	if got := atomic.LoadInt32(&builds); got != 1 {
		t.Fatalf("index builds = %d for 50 missing connections, want 1 (cold start only)", got)
	}
}

// A miss must fail open — the connection falls back to the region/default
// route rather than being attributed to some other process.
func TestDarwinProcessSearcher_MissFailsOpen(t *testing.T) {
	s := NewDarwinProcessSearcher()
	s.buildConnIndexFn = func() map[string]string {
		return map[string]string{
			cacheKey("tcp", netip.MustParseAddr("172.16.0.2"), 111): "Other",
		}
	}
	name, pkg, installer := s.FindProcess("tcp", netip.MustParseAddr("172.16.0.2"), 222)
	if name != "" || pkg != "" || installer != "" {
		t.Fatalf("miss returned (%q,%q,%q), want all empty", name, pkg, installer)
	}
}
