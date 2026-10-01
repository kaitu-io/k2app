//go:build windows

package provider

import (
	"testing"
	"time"
)

func newDNSClientTestSearcher(tb *dnsClientTable) *WindowsProcessSearcher {
	s := newTestSearcher(&mockTCPFetcher{}, &mockUDPFetcher{}, nil)
	s.procNamer = &identNamer{
		mockProcNamer: mockProcNamer{names: map[uint32]string{700: "doubao.exe", 800: "svchost.exe"}},
		paths:         map[uint32]string{700: `c:\users\u\appdata\local\doubao\application\doubao.exe`},
	}
	s.selfPID = 999
	if tb != nil {
		s.dnsWatch = &dnsClientWatch{table: tb}
	}
	return s
}

func TestFindDNSClient(t *testing.T) {
	tb := newDNSClientTable()
	s := newDNSClientTestSearcher(tb)

	go func() {
		time.Sleep(10 * time.Millisecond)
		tb.record("www.doubao.com", 700)
	}()
	name, path, ok := s.FindDNSClient("svchost.exe", "www.doubao.com")
	if !ok || name != "doubao.exe" || path != `c:\users\u\appdata\local\doubao\application\doubao.exe` {
		t.Fatalf("got (%q, %q, %v)", name, path, ok)
	}
}

// The socket owner is the app itself (Chromium's own resolver): no lookup,
// and above all no waiting.
func TestFindDNSClient_NotTheSystemResolver_ReturnsAtOnce(t *testing.T) {
	tb := newDNSClientTable()
	tb.record("www.doubao.com", 700)
	s := newDNSClientTestSearcher(tb)

	start := time.Now()
	if _, _, ok := s.FindDNSClient("chrome.exe", "www.doubao.com"); ok {
		t.Fatal("attributed a query whose socket is not the system resolver's")
	}
	if d := time.Since(start); d > dnsClientWait/2 {
		t.Fatalf("took %v — a non-resolver flow must not wait for an event", d)
	}
}

func TestFindDNSClient_WatchOff(t *testing.T) {
	s := newDNSClientTestSearcher(nil)
	start := time.Now()
	if _, _, ok := s.FindDNSClient("svchost.exe", "www.doubao.com"); ok {
		t.Fatal("attributed with the watch off")
	}
	if d := time.Since(start); d > dnsClientWait/2 {
		t.Fatalf("took %v with the watch off", d)
	}
}

// The daemon's own lookups and processes that have already exited must not
// produce an attribution.
func TestFindDNSClient_SelfAndGoneProcess(t *testing.T) {
	tb := newDNSClientTable()
	s := newDNSClientTestSearcher(tb)
	tb.record("self.example.com", 999)
	tb.record("gone.example.com", 4242)
	time.Sleep(2 * dnsClientSkew)
	for _, q := range []string{"self.example.com", "gone.example.com"} {
		if name, _, ok := s.FindDNSClient("svchost.exe", q); ok {
			t.Errorf("%s attributed to %q", q, name)
		}
	}
}
