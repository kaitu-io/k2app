//go:build darwin && !ios

package provider

import (
	"net"
	"net/netip"
	"testing"
)

func TestProcessSearcher_FindSelf(t *testing.T) {
	// Create a TCP listener to have a known socket.
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
	if name == "" {
		t.Errorf("FindProcess(127.0.0.1:%d) returned empty process name", srcPort)
	} else {
		t.Logf("FindProcess(127.0.0.1:%d) = %q", srcPort, name)
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
