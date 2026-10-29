//go:build darwin && !ios

package provider

import (
	"fmt"
	"net"
	"net/netip"
	"os/exec"
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

// TestProcessSearcher_IPv6Attribution_RealProcess is the end-to-end proof for
// the 2026-08-14 IPv6 fix: a real second process (curl) connects over IPv6
// loopback and a real lsof lookup must attribute it. Before the fix,
// FindProcess early-returned empty for any non-IPv4 srcIP, so every IPv6
// connection silently bypassed `match.apps` / `process_name` rules.
func TestProcessSearcher_IPv6Attribution_RealProcess(t *testing.T) {
	ln, err := net.Listen("tcp", "[::1]:0")
	if err != nil {
		t.Skipf("IPv6 loopback unavailable: %v", err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port

	// curl blocks waiting for an HTTP response we never send, keeping the
	// connection open while lsof runs. --max-time bounds the worst case.
	cmd := exec.Command("curl", "-s", "--max-time", "10",
		fmt.Sprintf("http://[::1]:%d/", port))
	if err := cmd.Start(); err != nil {
		t.Skipf("curl unavailable: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	conn, err := ln.Accept()
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	defer conn.Close()
	remote := conn.RemoteAddr().(*net.TCPAddr)
	srcIP, ok := netip.AddrFromSlice(remote.IP)
	if !ok {
		t.Fatalf("bad remote addr %v", remote)
	}
	srcIP = srcIP.Unmap() // mirror core/tunnel.go routeConn
	if !srcIP.Is6() || srcIP.Is4In6() {
		t.Fatalf("expected pure IPv6 source, got %v", srcIP)
	}

	s := NewDarwinProcessSearcher()
	name, _, _ := s.FindProcess("tcp", srcIP, uint16(remote.Port))
	if name != "curl" {
		t.Errorf("FindProcess([::1]:%d) = %q, want curl", remote.Port, name)
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
