//go:build windows

package provider

import (
	"bytes"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Real-kernel-table integration tests for Windows process attribution.
//
// What these cover that nothing else in the package does: the layer the mock
// fetchers replace. process_windows_test.go injects mock tables, so it proves
// the match rules. process_wintable_test.go decodes hand-built buffers at
// literal offsets, so it proves the row layout — portably, on any OS. Neither
// ever calls iphlpapi. Everything in between is untested anywhere else: the
// AF_INET6 / TCP_TABLE_OWNER_PID_ALL constants handed to GetExtendedTcpTable,
// fetchOwnerTable's grow-and-retry buffer loop, which fetcher
// NewWindowsProcessSearcher actually wires into each field, and
// QueryFullProcessImageName against a live PID. Swap AF_INET6 for AF_INET in
// realTCP6Fetcher and every other test in this package still passes.
//
// They also pin an assumption no unit test can state, because the mocks are
// where that assumption gets encoded: that Windows really does list a
// connected IPv6 socket in the AF_INET6 table under the exact local address
// the matcher looks up.
//
// Historical note, so nobody re-derives this wrong: the R1 bug (a single
// `if !srcIP.Is4() { return "", "", "" }` short-circuiting every IPv6 lookup)
// shipped with a green suite because the IPv6 mock tests did not exist yet —
// they arrived with its fix. Re-introducing that line today turns
// process_windows_test.go red, so it does NOT demonstrate what this file
// adds. Use the AF_INET6 swap above for that.
//
// These tests spawn a child process holding a live loopback connection and
// run the PRODUCTION searcher against it — real GetExtended{Tcp,Udp}Table,
// real QueryFullProcessImageName.
//
// The child process is mandatory, not incidental: WindowsProcessSearcher
// skips rows owned by its own PID (process_windows.go, `r.OwningPID ==
// s.selfPID`), so a socket opened by the test process itself is invisible by
// design.
//
// DO NOT add a testing.Short() guard here. CI runs the k2 Windows suite with
// -short; a Short skip would make these tests silently vanish from the only
// environment that can run them at all.

// realKernelChildEnv carries "<network>|<addr>" to the re-executed child.
const realKernelChildEnv = "K2_PROVIDER_REALKERNEL_CHILD"

// TestRealKernelChildHelper is not a test. It is the body of the child
// process re-executed by the real-kernel tests below, and a no-op skip in a
// normal `go test` pass.
func TestRealKernelChildHelper(t *testing.T) {
	spec := os.Getenv(realKernelChildEnv)
	if spec == "" {
		t.Skip("not the re-executed child process")
	}
	network, target, ok := strings.Cut(spec, "|")
	if !ok {
		t.Fatalf("malformed child spec %q", spec)
	}

	conn, err := net.Dial(network, target)
	if err != nil {
		t.Fatalf("child dial %s %s: %v", network, target, err)
	}
	defer conn.Close()

	if network == "udp" {
		// A UDP socket does not appear in the kernel table until it sends,
		// and the parent needs a datagram to learn our ephemeral port.
		if _, err := conn.Write([]byte("k2-realkernel")); err != nil {
			t.Fatalf("child udp write: %v", err)
		}
	}

	// Hold the socket open while the parent queries the kernel table. The
	// parent releases us by closing (TCP: EOF) or exiting; the deadline is a
	// backstop against an orphaned child.
	_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	_, _ = conn.Read(make([]byte, 1))
}

// spawnRealKernelChild re-executes this test binary as a child process that
// connects to target and holds the connection open.
func spawnRealKernelChild(t *testing.T, network, target string) {
	t.Helper()

	cmd := exec.Command(os.Args[0], "-test.run=^TestRealKernelChildHelper$")
	cmd.Env = append(os.Environ(), realKernelChildEnv+"="+network+"|"+target)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out

	if err := cmd.Start(); err != nil {
		t.Fatalf("spawn child %s: %v", os.Args[0], err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		if t.Failed() {
			t.Logf("child process output:\n%s", out.String())
		}
	})
}

// wantChildProcessName is what NameForPID reports for the re-executed test
// binary: lowercased basename (process_windows.go realProcNamer).
func wantChildProcessName() string {
	return strings.ToLower(filepath.Base(os.Args[0]))
}

func TestWindowsFindProcess_RealKernelTable_IPv6TCP(t *testing.T) {
	ln, err := net.Listen("tcp", "[::1]:0")
	if err != nil {
		// Deliberately fatal rather than skipped: a silent skip here would
		// restore exactly the blind spot this test exists to close.
		t.Fatalf("IPv6 loopback listen failed; this test requires a working "+
			"IPv6 stack and must not be skipped: %v", err)
	}
	defer ln.Close()

	spawnRealKernelChild(t, "tcp", ln.Addr().String())

	_ = ln.(*net.TCPListener).SetDeadline(time.Now().Add(30 * time.Second))
	conn, err := ln.Accept()
	if err != nil {
		t.Fatalf("accept from child: %v", err)
	}
	defer conn.Close()

	// The child's LOCAL addr:port is our peer addr — that is the tuple the
	// tunnel would hand FindProcess for an outbound connection.
	raddr, ok := conn.RemoteAddr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("remote addr %T is not *net.TCPAddr", conn.RemoteAddr())
	}
	srcIP, ok := netip.AddrFromSlice(raddr.IP)
	if !ok {
		t.Fatalf("cannot convert child local IP %v", raddr.IP)
	}
	if srcIP.Is4() || srcIP.Is4In6() {
		t.Fatalf("child connected over IPv4 (%v); this test must exercise the "+
			"AF_INET6 kernel table", srcIP)
	}

	name, pkg, installer := NewWindowsProcessSearcher().
		FindProcess("tcp", srcIP, uint16(raddr.Port))

	if want := wantChildProcessName(); name != want {
		t.Fatalf("FindProcess(tcp, %v, %d) = %q, want %q — a child process is "+
			"holding a live ESTABLISHED IPv6 connection from that exact "+
			"local addr:port. An empty result means the AF_INET6 TCP path is "+
			"broken: wrong family/class constant, fetchOwnerTable buffer "+
			"handling, tcp6Fetcher wiring, or QueryFullProcessImageName",
			srcIP, raddr.Port, name, want)
	}
	if pkg != "" || installer != "" {
		t.Errorf("Windows carries no package/installer fields, got (%q, %q)", pkg, installer)
	}
}

func TestWindowsFindProcess_RealKernelTable_IPv6UDP(t *testing.T) {
	pc, err := net.ListenPacket("udp", "[::1]:0")
	if err != nil {
		t.Fatalf("IPv6 loopback packet listen failed; this test requires a "+
			"working IPv6 stack and must not be skipped: %v", err)
	}
	defer pc.Close()

	spawnRealKernelChild(t, "udp", pc.LocalAddr().String())

	_ = pc.SetReadDeadline(time.Now().Add(30 * time.Second))
	_, addr, err := pc.ReadFrom(make([]byte, 64))
	if err != nil {
		t.Fatalf("read datagram from child: %v", err)
	}
	uaddr, ok := addr.(*net.UDPAddr)
	if !ok {
		t.Fatalf("source addr %T is not *net.UDPAddr", addr)
	}
	srcIP, ok := netip.AddrFromSlice(uaddr.IP)
	if !ok {
		t.Fatalf("cannot convert child local IP %v", uaddr.IP)
	}
	if srcIP.Is4() || srcIP.Is4In6() {
		t.Fatalf("child sent over IPv4 (%v); this test must exercise the "+
			"AF_INET6 kernel table", srcIP)
	}

	name, _, _ := NewWindowsProcessSearcher().
		FindProcess("udp", srcIP, uint16(uaddr.Port))

	if want := wantChildProcessName(); name != want {
		t.Fatalf("FindProcess(udp, %v, %d) = %q, want %q — a child process "+
			"just sent a datagram from that exact local addr:port. An empty "+
			"result means the AF_INET6 UDP path is broken: wrong family/class "+
			"constant, fetchOwnerTable buffer handling, udp6Fetcher wiring, "+
			"or QueryFullProcessImageName",
			srcIP, uaddr.Port, name, want)
	}
}
