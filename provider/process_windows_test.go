//go:build windows

package provider

import (
	"net/netip"
	"os"
	"sync/atomic"
	"testing"
)

// errFetcher is a sentinel error returned by mock fetchers to simulate
// GetExtendedTcpTable/GetExtendedUdpTable failures.
type errFetcher string

func (e errFetcher) Error() string { return string(e) }

type mockTCPFetcher struct {
	rows  []tcpRowOwnerPID
	err   error
	calls atomic.Int32
}

func (m *mockTCPFetcher) Fetch() ([]tcpRowOwnerPID, error) {
	m.calls.Add(1)
	return m.rows, m.err
}

type mockUDPFetcher struct {
	rows  []udpRowOwnerPID
	err   error
	calls atomic.Int32
}

func (m *mockUDPFetcher) Fetch() ([]udpRowOwnerPID, error) {
	m.calls.Add(1)
	return m.rows, m.err
}

type mockTCP6Fetcher struct {
	rows  []tcp6RowOwnerPID
	err   error
	calls atomic.Int32
}

func (m *mockTCP6Fetcher) Fetch() ([]tcp6RowOwnerPID, error) {
	m.calls.Add(1)
	return m.rows, m.err
}

type mockUDP6Fetcher struct {
	rows  []udp6RowOwnerPID
	err   error
	calls atomic.Int32
}

func (m *mockUDP6Fetcher) Fetch() ([]udp6RowOwnerPID, error) {
	m.calls.Add(1)
	return m.rows, m.err
}

type mockProcNamer struct {
	names map[uint32]string
}

func (m *mockProcNamer) NameForPID(pid uint32) string {
	return m.names[pid]
}

func newTestSearcher(tcp *mockTCPFetcher, udp *mockUDPFetcher, namer *mockProcNamer) *WindowsProcessSearcher {
	return &WindowsProcessSearcher{
		cache:       newProcessCache(processCacheCap, processCacheTTL),
		tcpFetcher:  tcp,
		udpFetcher:  udp,
		tcp6Fetcher: &mockTCP6Fetcher{},
		udp6Fetcher: &mockUDP6Fetcher{},
		procNamer:   namer,
		selfPID:     uint32(os.Getpid()),
	}
}

// 192.168.1.5:54321 → app PID 8000
//   dwLocalAddr = 0x0501A8C0 (LE uint32 of NBO [C0,A8,01,05])
//   dwLocalPort low 16 = 0x31D4 (NBO of 0xD431)
var chromeRow = tcpRowOwnerPID{
	State:     5,
	LocalAddr: 0x0501A8C0,
	LocalPort: 0x000031D4,
	OwningPID: 8000,
}

// =============================================================================
// IPv6 attribution — added 2026-08-14. Before this, FindProcess early-returned
// empty for any non-IPv4 srcIP, so every IPv6 connection silently fell back to
// the global route and `match.apps` / `process_name` rules never fired on v6.
// =============================================================================

// TUN IPv6 address form (config.DefaultTunIPv6 network) + port 54321 in the
// same NBO low-16-bits layout as the v4 rows.
var wechatV6Row = tcp6RowOwnerPID{
	LocalAddr: netip.MustParseAddr("fdfe:dcba:9876::2").As16(),
	LocalPort: 0x000031D4, // port 54321
	State:     5,
	OwningPID: 8000,
}

func TestWindowsFindProcess_IPv6TCPMatch(t *testing.T) {
	tcp := &mockTCPFetcher{}
	s := newTestSearcher(tcp, &mockUDPFetcher{}, &mockProcNamer{names: map[uint32]string{8000: "wechat.exe"}})
	s.tcp6Fetcher = &mockTCP6Fetcher{rows: []tcp6RowOwnerPID{wechatV6Row}}
	name, _, _ := s.FindProcess("tcp", netip.MustParseAddr("fdfe:dcba:9876::2"), 54321)
	if name != "wechat.exe" {
		t.Errorf("got %q, want wechat.exe", name)
	}
	if tcp.calls.Load() != 0 {
		t.Errorf("v4 fetcher called %d times for a pure-IPv6 lookup, want 0", tcp.calls.Load())
	}
}

func TestWindowsFindProcess_IPv6TCPNoMatch(t *testing.T) {
	s := newTestSearcher(&mockTCPFetcher{}, &mockUDPFetcher{}, &mockProcNamer{names: map[uint32]string{8000: "wechat.exe"}})
	s.tcp6Fetcher = &mockTCP6Fetcher{rows: []tcp6RowOwnerPID{wechatV6Row}}
	// Different v6 srcIP, same port — TCP has no wildcard, must NOT match.
	name, _, _ := s.FindProcess("tcp", netip.MustParseAddr("2001:db8::9"), 54321)
	if name != "" {
		t.Errorf("got %q, want empty (TCP6 requires exact local addr)", name)
	}
}

func TestWindowsFindProcess_IPv6UDPMatch_WildcardLocalAddr(t *testing.T) {
	udp6 := &mockUDP6Fetcher{rows: []udp6RowOwnerPID{{
		LocalAddr: [16]byte{}, // :: — kernel wildcard, same quirk as 0.0.0.0
		LocalPort: 0x000031D4,
		OwningPID: 9000,
	}}}
	s := newTestSearcher(&mockTCPFetcher{}, &mockUDPFetcher{}, &mockProcNamer{names: map[uint32]string{9000: "douyin.exe"}})
	s.udp6Fetcher = udp6
	name, _, _ := s.FindProcess("udp", netip.MustParseAddr("fdfe:dcba:9876::2"), 54321)
	if name != "douyin.exe" {
		t.Errorf("got %q, want douyin.exe (:: wildcard must match any v6 srcIP)", name)
	}
}

// Dual-stack coverage: an AF_INET6 socket without IPV6_V6ONLY carrying an
// IPv4 flow appears ONLY in the AF_INET6 table, local addr ::ffff:a.b.c.d.
// A v4 lookup that misses the v4 table must fall through and find it there.
func TestWindowsFindProcess_DualStackV4Mapped_TCPFallback(t *testing.T) {
	tcp := &mockTCPFetcher{} // v4 table empty — dual-stack socket not listed
	tcp6 := &mockTCP6Fetcher{rows: []tcp6RowOwnerPID{{
		LocalAddr: netip.MustParseAddr("::ffff:192.168.1.5").As16(),
		LocalPort: 0x000031D4,
		State:     5,
		OwningPID: 8000,
	}}}
	s := newTestSearcher(tcp, &mockUDPFetcher{}, &mockProcNamer{names: map[uint32]string{8000: "javaw.exe"}})
	s.tcp6Fetcher = tcp6
	name, _, _ := s.FindProcess("tcp", netip.MustParseAddr("192.168.1.5"), 54321)
	if name != "javaw.exe" {
		t.Errorf("got %q, want javaw.exe (v4 miss must fall through to v4-mapped row in TCP6 table)", name)
	}
	if tcp.calls.Load() != 1 {
		t.Errorf("v4 fetcher called %d times, want 1 (tried before fallback)", tcp.calls.Load())
	}
}

func TestWindowsFindProcess_DualStackV4_UDPWildcardFallback(t *testing.T) {
	udp6 := &mockUDP6Fetcher{rows: []udp6RowOwnerPID{{
		LocalAddr: [16]byte{}, // dual-stack UDP socket bound to ::
		LocalPort: 0x000031D4,
		OwningPID: 9000,
	}}}
	s := newTestSearcher(&mockTCPFetcher{}, &mockUDPFetcher{}, &mockProcNamer{names: map[uint32]string{9000: "chrome.exe"}})
	s.udp6Fetcher = udp6
	name, _, _ := s.FindProcess("udp", netip.MustParseAddr("198.18.0.2"), 54321)
	if name != "chrome.exe" {
		t.Errorf("got %q, want chrome.exe (v4 UDP miss must fall through to :: wildcard in UDP6 table)", name)
	}
}

// v4 hit must NOT consult the v6 tables — the fallback is miss-only, keeping
// the hot path identical to the pre-IPv6 behavior.
func TestWindowsFindProcess_V4Hit_NoV6Fallback(t *testing.T) {
	tcp6 := &mockTCP6Fetcher{}
	namer := &mockProcNamer{names: map[uint32]string{8000: "chrome.exe"}}
	s := newTestSearcher(&mockTCPFetcher{rows: []tcpRowOwnerPID{chromeRow}}, &mockUDPFetcher{}, namer)
	s.tcp6Fetcher = tcp6
	name, _, _ := s.FindProcess("tcp", netip.MustParseAddr("192.168.1.5"), 54321)
	if name != "chrome.exe" {
		t.Fatalf("got %q, want chrome.exe", name)
	}
	if tcp6.calls.Load() != 0 {
		t.Errorf("v6 fetcher called %d times on a v4 hit, want 0", tcp6.calls.Load())
	}
}

func TestWindowsFindProcess_IPv6FetcherError(t *testing.T) {
	s := newTestSearcher(&mockTCPFetcher{}, &mockUDPFetcher{}, &mockProcNamer{})
	s.tcp6Fetcher = &mockTCP6Fetcher{err: errFetcher("simulated GetExtendedTcpTable(AF_INET6) failure")}
	name, _, _ := s.FindProcess("tcp", netip.MustParseAddr("fdfe:dcba:9876::2"), 54321)
	if name != "" {
		t.Errorf("got %q, want empty (fetcher error must surface as empty)", name)
	}
}

func TestWindowsFindProcess_TCPMatch(t *testing.T) {
	tcp := &mockTCPFetcher{rows: []tcpRowOwnerPID{chromeRow}}
	namer := &mockProcNamer{names: map[uint32]string{8000: "chrome.exe"}}
	s := newTestSearcher(tcp, &mockUDPFetcher{}, namer)
	name, _, _ := s.FindProcess("tcp", netip.MustParseAddr("192.168.1.5"), 54321)
	if name != "chrome.exe" {
		t.Errorf("got %q, want chrome.exe", name)
	}
}

func TestWindowsFindProcess_PIDZeroSkipped(t *testing.T) {
	row := chromeRow
	row.OwningPID = 0 // TIME_WAIT zombie
	tcp := &mockTCPFetcher{rows: []tcpRowOwnerPID{row}}
	s := newTestSearcher(tcp, &mockUDPFetcher{}, &mockProcNamer{names: map[uint32]string{}})
	name, _, _ := s.FindProcess("tcp", netip.MustParseAddr("192.168.1.5"), 54321)
	if name != "" {
		t.Errorf("got %q, want empty (PID=0 should be skipped)", name)
	}
}

func TestWindowsFindProcess_SelfPIDSkipped(t *testing.T) {
	row := chromeRow
	row.OwningPID = uint32(os.Getpid())
	tcp := &mockTCPFetcher{rows: []tcpRowOwnerPID{row}}
	s := newTestSearcher(tcp, &mockUDPFetcher{}, &mockProcNamer{names: map[uint32]string{
		uint32(os.Getpid()): "k2.exe",
	}})
	name, _, _ := s.FindProcess("tcp", netip.MustParseAddr("192.168.1.5"), 54321)
	if name != "" {
		t.Errorf("got %q, want empty (self-PID should be skipped)", name)
	}
}

func TestWindowsFindProcess_CacheHit(t *testing.T) {
	tcp := &mockTCPFetcher{rows: []tcpRowOwnerPID{chromeRow}}
	namer := &mockProcNamer{names: map[uint32]string{8000: "chrome.exe"}}
	s := newTestSearcher(tcp, &mockUDPFetcher{}, namer)
	addr := netip.MustParseAddr("192.168.1.5")
	s.FindProcess("tcp", addr, 54321)
	s.FindProcess("tcp", addr, 54321) // should hit cache
	if tcp.calls.Load() != 1 {
		t.Errorf("fetcher called %d times, want 1 (cache hit on second call)", tcp.calls.Load())
	}
}

func TestWindowsFindProcess_NegativeCache(t *testing.T) {
	tcp := &mockTCPFetcher{rows: nil} // empty table
	s := newTestSearcher(tcp, &mockUDPFetcher{}, &mockProcNamer{})
	addr := netip.MustParseAddr("192.168.1.5")
	s.FindProcess("tcp", addr, 54321)
	s.FindProcess("tcp", addr, 54321) // negative cache hit
	if tcp.calls.Load() != 1 {
		t.Errorf("fetcher called %d times, want 1 (negative cache hit)", tcp.calls.Load())
	}
}

func TestWindowsFindProcess_NoMatchEmptyTable(t *testing.T) {
	tcp := &mockTCPFetcher{rows: nil}
	s := newTestSearcher(tcp, &mockUDPFetcher{}, &mockProcNamer{})
	name, _, _ := s.FindProcess("tcp", netip.MustParseAddr("192.168.1.5"), 54321)
	if name != "" {
		t.Errorf("got %q, want empty", name)
	}
}

func TestWindowsFindProcess_UDPMatch(t *testing.T) {
	udp := &mockUDPFetcher{rows: []udpRowOwnerPID{{
		LocalAddr: 0x0501A8C0,
		LocalPort: 0x000031D4,
		OwningPID: 9000,
	}}}
	namer := &mockProcNamer{names: map[uint32]string{9000: "dns-app.exe"}}
	s := newTestSearcher(&mockTCPFetcher{}, udp, namer)
	name, _, _ := s.FindProcess("udp", netip.MustParseAddr("192.168.1.5"), 54321)
	if name != "dns-app.exe" {
		t.Errorf("got %q, want dns-app.exe", name)
	}
}

// Windows kernel registers most UDP sockets with dwLocalAddr=0.0.0.0 (the
// default for socket()+sendto() workflows used by net.Dial("udp"), browser
// QUIC stacks, online games, etc.) regardless of where outbound packets
// actually route. findInUDP must match port-only when the row's LocalAddr
// is 0.0.0.0, otherwise UDP App Bypass attribution silently breaks on
// Windows for any app that doesn't bind() to a specific local interface.
func TestWindowsFindProcess_UDPMatch_WildcardLocalAddr(t *testing.T) {
	udp := &mockUDPFetcher{rows: []udpRowOwnerPID{{
		LocalAddr: 0x00000000, // 0.0.0.0 — kernel wildcard
		LocalPort: 0x000031D4, // port 54321
		OwningPID: 9000,
	}}}
	namer := &mockProcNamer{names: map[uint32]string{9000: "chrome.exe"}}
	s := newTestSearcher(&mockTCPFetcher{}, udp, namer)
	// Tunnel observed packet src = TUN interface IP; kernel UDP table
	// has 0.0.0.0. Must still attribute to chrome.exe.
	name, _, _ := s.FindProcess("udp", netip.MustParseAddr("198.18.0.2"), 54321)
	if name != "chrome.exe" {
		t.Errorf("got %q, want chrome.exe (wildcard 0.0.0.0 must match any srcIP)", name)
	}
}

// =============================================================================
// OS-specific quirk coverage — added 2026-05-22 to push App Bypass
// confidence to 10/10. Each test reproduces a Windows-specific behavior
// that could silently break process attribution.
// =============================================================================

// Process name with embedded space (e.g. "Microsoft Edge.exe"). The
// kernel's QueryFullProcessImageName preserves spaces; the searcher
// must pass them through to the rule engine unchanged. Otherwise
// `process_name: ["Microsoft Edge.exe"]` rules silently miss.
func TestWindowsFindProcess_NameWithSpaces(t *testing.T) {
	tcp := &mockTCPFetcher{rows: []tcpRowOwnerPID{chromeRow}}
	namer := &mockProcNamer{names: map[uint32]string{8000: "Microsoft Edge.exe"}}
	s := newTestSearcher(tcp, &mockUDPFetcher{}, namer)
	name, _, _ := s.FindProcess("tcp", netip.MustParseAddr("192.168.1.5"), 54321)
	if name != "Microsoft Edge.exe" {
		t.Errorf("got %q, want %q (spaces preserved)", name, "Microsoft Edge.exe")
	}
}

// UWP / Microsoft Store apps run inside ApplicationFrameHost.exe. The
// kernel registers the host's PID as the socket owner, not the inner
// package. Users who bypass UWP apps add "ApplicationFrameHost.exe" to
// their list. Verify the attribution returns exactly that name.
func TestWindowsFindProcess_UWPHostAttribution(t *testing.T) {
	tcp := &mockTCPFetcher{rows: []tcpRowOwnerPID{chromeRow}}
	namer := &mockProcNamer{names: map[uint32]string{8000: "ApplicationFrameHost.exe"}}
	s := newTestSearcher(tcp, &mockUDPFetcher{}, namer)
	name, _, _ := s.FindProcess("tcp", netip.MustParseAddr("192.168.1.5"), 54321)
	if name != "ApplicationFrameHost.exe" {
		t.Errorf("got %q, want UWP host name", name)
	}
}

// Multi-byte UTF-8 names (e.g. localized exe filenames "微信.exe" on
// Chinese Windows). realProcNamer uses UTF-16 wide-API + UTF16ToString;
// mock-level test verifies the matcher doesn't garble UTF-8 bytes.
func TestWindowsFindProcess_UTF8Name(t *testing.T) {
	tcp := &mockTCPFetcher{rows: []tcpRowOwnerPID{chromeRow}}
	namer := &mockProcNamer{names: map[uint32]string{8000: "微信.exe"}}
	s := newTestSearcher(tcp, &mockUDPFetcher{}, namer)
	name, _, _ := s.FindProcess("tcp", netip.MustParseAddr("192.168.1.5"), 54321)
	if name != "微信.exe" {
		t.Errorf("got %q, want %q", name, "微信.exe")
	}
}

// GetExtendedTcpTable / GetExtendedUdpTable failure path. The fetcher
// returns err != nil; FindProcess must return empty gracefully (no
// panic, no garbage value).
func TestWindowsFindProcess_TCPFetcherError(t *testing.T) {
	tcp := &mockTCPFetcher{err: errFetcher("simulated GetExtendedTcpTable failure")}
	s := newTestSearcher(tcp, &mockUDPFetcher{}, &mockProcNamer{})
	name, _, _ := s.FindProcess("tcp", netip.MustParseAddr("192.168.1.5"), 54321)
	if name != "" {
		t.Errorf("got %q, want empty (fetcher error must surface as empty)", name)
	}
}

func TestWindowsFindProcess_UDPFetcherError(t *testing.T) {
	udp := &mockUDPFetcher{err: errFetcher("simulated GetExtendedUdpTable failure")}
	s := newTestSearcher(&mockTCPFetcher{}, udp, &mockProcNamer{})
	name, _, _ := s.FindProcess("udp", netip.MustParseAddr("192.168.1.5"), 54321)
	if name != "" {
		t.Errorf("got %q, want empty", name)
	}
}

// Multiple non-self rows matching the same (ip, port) — kernel anomaly
// (e.g. SO_REUSEPORT, stale TIME_WAIT). Parser returns the FIRST
// non-self match in fetch order. Deterministic; rule engine treats it
// as authoritative.
func TestWindowsFindProcess_MultipleMatches_ReturnsFirstNonSelf(t *testing.T) {
	rows := []tcpRowOwnerPID{
		{State: 1, LocalAddr: 0x0501A8C0, LocalPort: 0x000031D4, OwningPID: uint32(os.Getpid())}, // self
		{State: 5, LocalAddr: 0x0501A8C0, LocalPort: 0x000031D4, OwningPID: 7777},                 // first non-self
		{State: 5, LocalAddr: 0x0501A8C0, LocalPort: 0x000031D4, OwningPID: 8888},                 // shadowed
	}
	tcp := &mockTCPFetcher{rows: rows}
	namer := &mockProcNamer{names: map[uint32]string{
		7777: "wget.exe",
		8888: "curl.exe",
	}}
	s := newTestSearcher(tcp, &mockUDPFetcher{}, namer)
	name, _, _ := s.FindProcess("tcp", netip.MustParseAddr("192.168.1.5"), 54321)
	if name != "wget.exe" {
		t.Errorf("got %q, want wget.exe (first non-self match)", name)
	}
}

// NameForPID returning empty string (PPL / protected process / race
// after fetch). The matcher returns empty rather than a placeholder —
// silent miss is preferable to wrong attribution.
func TestWindowsFindProcess_NamerReturnsEmpty(t *testing.T) {
	tcp := &mockTCPFetcher{rows: []tcpRowOwnerPID{chromeRow}}
	namer := &mockProcNamer{names: map[uint32]string{}} // PID 8000 not registered → returns ""
	s := newTestSearcher(tcp, &mockUDPFetcher{}, namer)
	name, _, _ := s.FindProcess("tcp", netip.MustParseAddr("192.168.1.5"), 54321)
	if name != "" {
		t.Errorf("got %q, want empty (PPL/protected → namer returns empty)", name)
	}
}

// Different (ip,port) keys MUST NOT collide in the cache. Two distinct
// processes on adjacent ports must each get their correct attribution.
func TestWindowsFindProcess_CacheKeyIsolation(t *testing.T) {
	rows := []tcpRowOwnerPID{
		{State: 5, LocalAddr: 0x0501A8C0, LocalPort: 0x000031D4, OwningPID: 7777}, // 192.168.1.5:54321
		{State: 5, LocalAddr: 0x0501A8C0, LocalPort: 0x000032D4, OwningPID: 8888}, // 192.168.1.5:54322
	}
	tcp := &mockTCPFetcher{rows: rows}
	namer := &mockProcNamer{names: map[uint32]string{
		7777: "wget.exe",
		8888: "curl.exe",
	}}
	s := newTestSearcher(tcp, &mockUDPFetcher{}, namer)
	if name, _, _ := s.FindProcess("tcp", netip.MustParseAddr("192.168.1.5"), 54321); name != "wget.exe" {
		t.Errorf("port 54321 attributed to %q, want wget.exe", name)
	}
	if name, _, _ := s.FindProcess("tcp", netip.MustParseAddr("192.168.1.5"), 54322); name != "curl.exe" {
		t.Errorf("port 54322 attributed to %q, want curl.exe", name)
	}
}

// Exact LocalAddr must beat the wildcard regardless of kernel table order —
// otherwise attribution is nondeterministic when both a 0.0.0.0:P socket and
// the actual owner's a.b.c.d:P socket exist.
func TestWindowsFindProcess_UDPExactBeatsWildcard(t *testing.T) {
	udp := &mockUDPFetcher{rows: []udpRowOwnerPID{
		{LocalAddr: 0x00000000, LocalPort: 0x000031D4, OwningPID: 9001}, // wildcard FIRST
		{LocalAddr: 0x0501A8C0, LocalPort: 0x000031D4, OwningPID: 9002}, // exact owner
	}}
	namer := &mockProcNamer{names: map[uint32]string{9001: "other.exe", 9002: "owner.exe"}}
	s := newTestSearcher(&mockTCPFetcher{}, udp, namer)
	name, _, _ := s.FindProcess("udp", netip.MustParseAddr("192.168.1.5"), 54321)
	if name != "owner.exe" {
		t.Errorf("got %q, want owner.exe (exact match must beat wildcard row listed first)", name)
	}
}

func TestWindowsFindProcess_UDP6ExactBeatsWildcard(t *testing.T) {
	udp6 := &mockUDP6Fetcher{rows: []udp6RowOwnerPID{
		{LocalAddr: [16]byte{}, LocalPort: 0x000031D4, OwningPID: 9001}, // :: FIRST
		{LocalAddr: netip.MustParseAddr("fdfe:dcba:9876::2").As16(), LocalPort: 0x000031D4, OwningPID: 9002},
	}}
	namer := &mockProcNamer{names: map[uint32]string{9001: "other.exe", 9002: "owner.exe"}}
	s := newTestSearcher(&mockTCPFetcher{}, &mockUDPFetcher{}, namer)
	s.udp6Fetcher = udp6
	name, _, _ := s.FindProcess("udp", netip.MustParseAddr("fdfe:dcba:9876::2"), 54321)
	if name != "owner.exe" {
		t.Errorf("got %q, want owner.exe (exact match must beat :: wildcard listed first)", name)
	}
}

// Specific LocalAddr that doesn't match srcAddr must NOT match — only
// 0.0.0.0 is wildcard, not "any non-matching IP".
func TestWindowsFindProcess_UDPNoMatch_SpecificLocalAddrMismatch(t *testing.T) {
	udp := &mockUDPFetcher{rows: []udpRowOwnerPID{{
		LocalAddr: 0x0501A8C0, // 192.168.1.5 — specific, not wildcard
		LocalPort: 0x000031D4,
		OwningPID: 9000,
	}}}
	namer := &mockProcNamer{names: map[uint32]string{9000: "app.exe"}}
	s := newTestSearcher(&mockTCPFetcher{}, udp, namer)
	name, _, _ := s.FindProcess("udp", netip.MustParseAddr("10.0.0.1"), 54321)
	if name != "" {
		t.Errorf("got %q, want empty (specific LocalAddr 192.168.1.5 must NOT match 10.0.0.1)", name)
	}
}
