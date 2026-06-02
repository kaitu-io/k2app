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

func TestNtohs_KnownValues(t *testing.T) {
	cases := []struct {
		in   uint32
		want uint16
	}{
		{0x000031D4, 54321}, // 0xD431 in NBO
		{0x00005000, 80},    // 0x0050 in NBO
		{0x0000BB01, 443},   // 0x01BB in NBO
		{0x00000000, 0},
		{0x0000FFFF, 0xFFFF},
	}
	for _, c := range cases {
		got := ntohs(c.in)
		if got != c.want {
			t.Errorf("ntohs(0x%08X) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestUint32ToIPv4_KnownValues(t *testing.T) {
	cases := []struct {
		in   uint32
		want [4]byte
	}{
		{0x0501A8C0, [4]byte{192, 168, 1, 5}},
		{0x0100007F, [4]byte{127, 0, 0, 1}},
		{0x00000000, [4]byte{0, 0, 0, 0}},
		{0x010101D5, [4]byte{213, 1, 1, 1}}, // 213.1.1.1 ← 0xD5010101 NBO
	}
	for _, c := range cases {
		got := uint32ToIPv4(c.in)
		if got != c.want {
			t.Errorf("uint32ToIPv4(0x%08X) = %v, want %v", c.in, got, c.want)
		}
	}
}

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

type mockProcNamer struct {
	names map[uint32]string
}

func (m *mockProcNamer) NameForPID(pid uint32) string {
	return m.names[pid]
}

func newTestSearcher(tcp *mockTCPFetcher, udp *mockUDPFetcher, namer *mockProcNamer) *WindowsProcessSearcher {
	return &WindowsProcessSearcher{
		cache:      newProcessCache(processCacheCap, processCacheTTL),
		tcpFetcher: tcp,
		udpFetcher: udp,
		procNamer:  namer,
		selfPID:    uint32(os.Getpid()),
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

func TestWindowsFindProcess_IPv6Early(t *testing.T) {
	tcp := &mockTCPFetcher{}
	s := newTestSearcher(tcp, &mockUDPFetcher{}, &mockProcNamer{})
	name, _, _ := s.FindProcess("tcp", netip.MustParseAddr("2001:db8::1"), 54321)
	if name != "" {
		t.Errorf("got %q, want empty", name)
	}
	if tcp.calls.Load() != 0 {
		t.Errorf("fetcher called %d times for IPv6, want 0", tcp.calls.Load())
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
