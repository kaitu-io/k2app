//go:build darwin && !ios

package provider

import (
	"net"
	"net/netip"
	"testing"
)

// TestProcessSearcher_SkipsSelf asserts the searcher does NOT return our own
// process name when only our own socket is bound to the (ip,port).
// lsof -i tcp@<ip>:<port> on a listening socket returns our own PID; the
// searcher must skip self (os.Getpid()) and return empty.
//
// Real-world attribution is tested in integration smoke (curl + daemon),
// where the daemon and peer are distinct processes.
func TestProcessSearcher_SkipsSelf(t *testing.T) {
	// Create a TCP listener so there's a known socket on a known port,
	// owned by THIS test process (which is self).
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	addr := ln.Addr().(*net.TCPAddr)
	srcIP := netip.MustParseAddr("127.0.0.1")
	srcPort := uint16(addr.Port)

	searcher := DarwinProcessSearcher{}
	name, _, _ := searcher.FindProcess("tcp", srcIP, srcPort)
	if name != "" {
		t.Errorf("FindProcess(127.0.0.1:%d) = %q, want empty (self-PID must be skipped)", srcPort, name)
	}
}

func TestNopProcessSearcher_ReturnsThreeStrings(t *testing.T) {
	var s ProcessSearcher = NopProcessSearcher{}
	proc, pkg, installer := s.FindProcess("tcp", netip.MustParseAddr("127.0.0.1"), 0)
	if proc != "" || pkg != "" || installer != "" {
		t.Errorf("Nop should return three empty strings, got proc=%q pkg=%q installer=%q",
			proc, pkg, installer)
	}
}
