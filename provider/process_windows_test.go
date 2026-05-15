//go:build windows

package provider

import (
	"net/netip"
	"os"
	"sync/atomic"
	"testing"
)

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
	name, _ := s.FindProcess("tcp", netip.MustParseAddr("2001:db8::1"), 54321)
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
	name, _ := s.FindProcess("tcp", netip.MustParseAddr("192.168.1.5"), 54321)
	if name != "chrome.exe" {
		t.Errorf("got %q, want chrome.exe", name)
	}
}

func TestWindowsFindProcess_PIDZeroSkipped(t *testing.T) {
	row := chromeRow
	row.OwningPID = 0 // TIME_WAIT zombie
	tcp := &mockTCPFetcher{rows: []tcpRowOwnerPID{row}}
	s := newTestSearcher(tcp, &mockUDPFetcher{}, &mockProcNamer{names: map[uint32]string{}})
	name, _ := s.FindProcess("tcp", netip.MustParseAddr("192.168.1.5"), 54321)
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
	name, _ := s.FindProcess("tcp", netip.MustParseAddr("192.168.1.5"), 54321)
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
	name, _ := s.FindProcess("tcp", netip.MustParseAddr("192.168.1.5"), 54321)
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
	name, _ := s.FindProcess("udp", netip.MustParseAddr("192.168.1.5"), 54321)
	if name != "dns-app.exe" {
		t.Errorf("got %q, want dns-app.exe", name)
	}
}
