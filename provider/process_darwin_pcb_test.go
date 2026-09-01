//go:build darwin && !ios

package provider

import (
	"encoding/binary"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// =============================================================================
// Real-kernel tests: create actual sockets and attribute them through the
// live pcblist_n sysctl. These are cheap (~250µs per fetch) and run
// unconditionally, unlike the opt-in lsof real-world test.
// =============================================================================

func lookupOwn(t *testing.T, network string, la netip.Addr, port uint16) (int32, uint32) {
	t.Helper()
	buf, err := unix.SysctlRaw(pcbSysctlName(network))
	if err != nil {
		t.Fatalf("sysctl %s: %v", pcbSysctlName(network), err)
	}
	pid, uid, ok := pcbLookupPid(buf, la, port)
	if !ok {
		t.Fatalf("own %s socket %s:%d not found in live pcb table", network, la, port)
	}
	return pid, uid
}

// TestPCBLayout_OwnSocketFields is the struct-layout pin: it extracts pid AND
// uid from a socket we fully control. If Apple ever moves so_last_pid/so_uid
// or the inpcb address fields, this fails before the change ships (and the
// runtime pcbSelfCheck would fail in the field, flipping to lsof fallback).
func TestPCBLayout_OwnSocketFields(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	la := conn.LocalAddr().(*net.TCPAddr)
	pid, uid := lookupOwn(t, "tcp", netip.MustParseAddr("127.0.0.1"), uint16(la.Port))
	if pid != int32(os.Getpid()) {
		t.Fatalf("so_last_pid = %d, want own pid %d (layout drift?)", pid, os.Getpid())
	}
	if uid != uint32(os.Getuid()) {
		t.Fatalf("so_uid = %d, want own uid %d (layout drift?)", uid, os.Getuid())
	}
	if name := pcbProcName(pid); name == "" {
		t.Fatal("pcbProcName(self) returned empty")
	}
}

func TestPCB_TCP6_OwnSocket(t *testing.T) {
	ln, err := net.Listen("tcp", "[::1]:0")
	if err != nil {
		t.Skipf("no IPv6 loopback: %v", err)
	}
	defer ln.Close()
	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	la := conn.LocalAddr().(*net.TCPAddr)
	pid, _ := lookupOwn(t, "tcp", netip.MustParseAddr("::1"), uint16(la.Port))
	if pid != int32(os.Getpid()) {
		t.Fatalf("v6 socket attributed to pid %d, want %d", pid, os.Getpid())
	}
}

func TestPCB_UDP4_Connected(t *testing.T) {
	// Connected UDP (Chromium's QUIC pattern): laddr is concrete.
	conn, err := net.Dial("udp", "127.0.0.1:9") // discard port; no traffic sent
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	la := conn.LocalAddr().(*net.UDPAddr)
	pid, _ := lookupOwn(t, "udp", netip.MustParseAddr("127.0.0.1"), uint16(la.Port))
	if pid != int32(os.Getpid()) {
		t.Fatalf("connected UDP attributed to pid %d, want %d", pid, os.Getpid())
	}
}

// TestPCB_UDP_WildcardFallback: an unconnected sendto()-style socket has an
// unspecified laddr in the kernel table; the lookup source IP is whatever
// interface the packet left through. Wildcard matching must attribute it —
// Windows-parity semantics, and a strict improvement over the lsof index
// which skipped wildcard sockets entirely.
func TestPCB_UDP_WildcardFallback(t *testing.T) {
	uc, err := net.ListenUDP("udp4", &net.UDPAddr{}) // 0.0.0.0:ephemeral
	if err != nil {
		t.Fatal(err)
	}
	defer uc.Close()
	port := uint16(uc.LocalAddr().(*net.UDPAddr).Port)
	// 192.0.2.1 (TEST-NET) is guaranteed not to be a concrete laddr of any
	// real socket, so only the wildcard path can produce this match.
	buf, err := unix.SysctlRaw("net.inet.udp.pcblist_n")
	if err != nil {
		t.Fatal(err)
	}
	pid, _, ok := pcbLookupPid(buf, netip.MustParseAddr("192.0.2.1"), port)
	if !ok {
		t.Fatal("wildcard UDP socket not attributed")
	}
	if pid != int32(os.Getpid()) {
		t.Fatalf("wildcard UDP attributed to pid %d, want %d", pid, os.Getpid())
	}
}

func TestPCB_UnknownNetworkMisses(t *testing.T) {
	if got := pcbFindProcess("icmp", netip.MustParseAddr("127.0.0.1"), 1); got != "" {
		t.Fatalf("unknown network returned %q, want empty", got)
	}
}

func TestPCB_SelfCheckPasses(t *testing.T) {
	if !pcbSelfCheck() {
		t.Fatal("pcbSelfCheck failed on a real darwin kernel")
	}
}

// TestFindProcess_PCB_SelfSkip: the daemon's own sockets must never be
// attributed (it holds the proxied upper half of every flow).
func TestFindProcess_PCB_SelfSkip(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	s := NewDarwinProcessSearcher()
	s.attribBackend = backendPCB
	la := conn.LocalAddr().(*net.TCPAddr)
	name, _, _ := s.FindProcess("tcp", netip.MustParseAddr("127.0.0.1"), uint16(la.Port))
	if name != "" {
		t.Fatalf("own socket attributed as %q, want empty (self skip)", name)
	}
}

// TestFindProcess_PCB_OtherProcess is the end-to-end assertion that matters:
// attribute a socket owned by a DIFFERENT process, at the moment it connects
// (the exact timing the lsof snapshot could not serve). The helper is this
// same test binary re-exec'd (standard TestHelperProcess pattern), so the
// expected name equals our own pbi_name.
func TestFindProcess_PCB_OtherProcess(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	cmd := exec.Command(os.Args[0], "-test.run", "TestHelperProcessDialer")
	cmd.Env = append(os.Environ(),
		"K2_PCB_HELPER=1",
		"K2_PCB_DIAL="+ln.Addr().String())
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()

	acceptCh := make(chan net.Conn, 1)
	go func() {
		c, aerr := ln.Accept()
		if aerr == nil {
			acceptCh <- c
		}
	}()
	var peer net.Conn
	select {
	case peer = <-acceptCh:
		defer peer.Close()
	case <-time.After(5 * time.Second):
		t.Fatal("helper never connected")
	}

	// The helper's local half is our accepted conn's remote half. Attribute
	// it IMMEDIATELY — this is the SYN-time lookup routeConn performs.
	ra := peer.RemoteAddr().(*net.TCPAddr)
	s := NewDarwinProcessSearcher()
	s.attribBackend = backendPCB
	name, _, _ := s.FindProcess("tcp", netip.MustParseAddr("127.0.0.1"), uint16(ra.Port))

	want := pcbProcName(int32(cmd.Process.Pid))
	if want == "" {
		t.Fatal("could not resolve helper name for comparison")
	}
	if name != want {
		t.Fatalf("helper socket attributed as %q, want %q", name, want)
	}
}

// TestHelperProcessDialer is not a real test: re-exec'd by
// TestFindProcess_PCB_OtherProcess to own a socket from a distinct pid.
func TestHelperProcessDialer(t *testing.T) {
	if os.Getenv("K2_PCB_HELPER") != "1" {
		t.Skip("helper process entry point")
	}
	conn, err := net.Dial("tcp", os.Getenv("K2_PCB_DIAL"))
	if err != nil {
		os.Exit(1)
	}
	defer conn.Close()
	time.Sleep(10 * time.Second) // hold the socket until the parent kills us
}

func TestPCB_LookupLatencyBudget(t *testing.T) {
	conn, err := net.Dial("udp", "127.0.0.1:9")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	la := conn.LocalAddr().(*net.UDPAddr)

	const n = 50
	start := time.Now()
	for i := 0; i < n; i++ {
		buf, serr := unix.SysctlRaw("net.inet.udp.pcblist_n")
		if serr != nil {
			t.Fatal(serr)
		}
		if _, _, ok := pcbLookupPid(buf, netip.MustParseAddr("127.0.0.1"), uint16(la.Port)); !ok {
			t.Fatal("own socket vanished mid-benchmark")
		}
	}
	avg := time.Since(start) / n
	// lsof was 100-1226ms per sweep; the whole point is being orders of
	// magnitude under that. 20ms is ~100x headroom over the measured ~230µs
	// so a loaded CI machine doesn't flake.
	if avg > 20*time.Millisecond {
		t.Fatalf("avg lookup %v exceeds 20ms budget", avg)
	}
	t.Logf("avg fetch+lookup: %v", avg)
}

// =============================================================================
// Synthetic-buffer parser tests: deterministic, kernel-independent coverage
// of pcbLookupPid's matching and robustness rules.
// =============================================================================

type synthSock struct {
	lport uint16
	vflag byte
	laddr netip.Addr
	pid   int32
	uid   uint32
}

func buildSynthBuf(t *testing.T, socks []synthSock) []byte {
	t.Helper()
	var buf []byte
	// xinpgen header: only xig_len (first u32) matters to the parser.
	hdr := make([]byte, 24)
	binary.LittleEndian.PutUint32(hdr, 24)
	buf = append(buf, hdr...)
	for _, s := range socks {
		inp := make([]byte, 104)
		binary.LittleEndian.PutUint32(inp[0:], 104)
		binary.LittleEndian.PutUint32(inp[4:], pcbKindInpcb)
		binary.BigEndian.PutUint16(inp[pcbInpLportOff:], s.lport)
		inp[pcbInpVflagOff] = s.vflag
		if s.vflag&pcbInpIPv4 != 0 {
			a4 := s.laddr.As4()
			copy(inp[pcbInpLaddr4:], a4[:])
		} else {
			a16 := s.laddr.As16()
			copy(inp[pcbInpLaddrOff:], a16[:])
		}
		so := make([]byte, 104)
		binary.LittleEndian.PutUint32(so[0:], 104)
		binary.LittleEndian.PutUint32(so[4:], pcbKindSocket)
		binary.LittleEndian.PutUint32(so[pcbSoUIDOff:], s.uid)
		binary.LittleEndian.PutUint32(so[pcbSoLastPidOff:], uint32(s.pid))
		buf = append(buf, inp...)
		buf = append(buf, so...)
	}
	return buf
}

func TestPCBLookup_Synthetic_ExactBeatsWildcard(t *testing.T) {
	ip := netip.MustParseAddr("198.18.0.7")
	buf := buildSynthBuf(t, []synthSock{
		{lport: 5000, vflag: pcbInpIPv4, laddr: netip.MustParseAddr("0.0.0.0"), pid: 111, uid: 1},
		{lport: 5000, vflag: pcbInpIPv4, laddr: ip, pid: 222, uid: 2},
	})
	pid, uid, ok := pcbLookupPid(buf, ip, 5000)
	if !ok || pid != 222 || uid != 2 {
		t.Fatalf("got (%d,%d,%v), want exact match (222,2,true) over wildcard", pid, uid, ok)
	}
}

func TestPCBLookup_Synthetic_WildcardV6MatchesV4Source(t *testing.T) {
	// Dual-stack socket without V6ONLY: kernel lists it as v6 :: — a v4
	// source must still match it via the wildcard rule.
	buf := buildSynthBuf(t, []synthSock{
		{lport: 6000, vflag: pcbInpIPv6, laddr: netip.MustParseAddr("::"), pid: 333, uid: 3},
	})
	pid, _, ok := pcbLookupPid(buf, netip.MustParseAddr("10.0.0.1"), 6000)
	if !ok || pid != 333 {
		t.Fatalf("got (%d,%v), want (333,true)", pid, ok)
	}
}

func TestPCBLookup_Synthetic_V4MappedV6EqualsV4(t *testing.T) {
	// A v6-listed socket with a v4-mapped laddr must match the plain-v4
	// source (both sides Unmap before comparing).
	buf := buildSynthBuf(t, []synthSock{
		{lport: 7000, vflag: pcbInpIPv6, laddr: netip.MustParseAddr("::ffff:192.168.1.5"), pid: 444, uid: 4},
	})
	pid, _, ok := pcbLookupPid(buf, netip.MustParseAddr("192.168.1.5"), 7000)
	if !ok || pid != 444 {
		t.Fatalf("got (%d,%v), want (444,true)", pid, ok)
	}
}

func TestPCBLookup_Synthetic_PortMismatchMisses(t *testing.T) {
	buf := buildSynthBuf(t, []synthSock{
		{lport: 8000, vflag: pcbInpIPv4, laddr: netip.MustParseAddr("127.0.0.1"), pid: 555, uid: 5},
	})
	if _, _, ok := pcbLookupPid(buf, netip.MustParseAddr("127.0.0.1"), 8001); ok {
		t.Fatal("port mismatch produced a match")
	}
}

func TestPCBLookup_Synthetic_MalformedBuffers(t *testing.T) {
	ip := netip.MustParseAddr("127.0.0.1")
	cases := map[string][]byte{
		"empty":         nil,
		"short":         {1, 2, 3},
		"zero-xig-len":  make([]byte, 64),
		"huge-xig-len":  func() []byte { b := make([]byte, 16); binary.LittleEndian.PutUint32(b, 4096); return b }(),
		"truncated-blk": func() []byte { b := buildSynthBuf(t, []synthSock{{lport: 1, vflag: pcbInpIPv4, laddr: ip, pid: 1}}); return b[:len(b)-60] }(),
	}
	for name, buf := range cases {
		if _, _, ok := pcbLookupPid(buf, ip, 1); ok && name != "truncated-blk" {
			t.Fatalf("%s: malformed buffer produced a match", name)
		}
	}
}

func TestPCBSysctlName(t *testing.T) {
	for in, want := range map[string]string{
		"tcp": "net.inet.tcp.pcblist_n",
		"udp": "net.inet.udp.pcblist_n",
		"":    "",
		"foo": "",
	} {
		if got := pcbSysctlName(in); got != want {
			t.Fatalf("pcbSysctlName(%q) = %q, want %q", in, got, want)
		}
	}
}

// Guard against silent contract drift between the constants used by the
// builder above and the parser: a change to one without the other should
// break loudly, not pass vacuously.
func TestPCBLookup_Synthetic_RoundTripPidUid(t *testing.T) {
	ip := netip.MustParseAddr("172.16.9.9")
	want := synthSock{lport: 40000, vflag: pcbInpIPv4, laddr: ip, pid: 987654, uid: 20}
	buf := buildSynthBuf(t, []synthSock{want})
	pid, uid, ok := pcbLookupPid(buf, ip, want.lport)
	if !ok || pid != want.pid || uid != want.uid {
		t.Fatalf("round trip got (%d,%d,%v), want (%d,%d,true)", pid, uid, ok, want.pid, want.uid)
	}
}
