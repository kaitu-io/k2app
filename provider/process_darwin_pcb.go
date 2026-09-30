//go:build darwin && !ios

package provider

/*
#include <libproc.h>
*/
import "C"

import (
	"bytes"
	"encoding/binary"
	"net"
	"net/netip"
	"os"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

// This file implements the primary Darwin attribution path: reading the
// kernel's live PCB (protocol control block) table via the same sysctls
// netstat uses — `net.inet.tcp.pcblist_n` / `net.inet.udp.pcblist_n` — and
// resolving the owning pid to a name with libproc.
//
// Why this replaces the lsof snapshot as the primary path: routeConn calls
// FindProcess exactly once per connection, at SYN time. A snapshot built
// before the connection existed can never contain it, so the snapshot design
// had to choose between rebuild-per-miss (the lsof cost problem) and a
// throttle window whose misses fail open (the App-Bypass-silently-routes-
// through-the-tunnel bug this file fixes — browser page loads arrive in
// waves, and every wave after the first landed inside the 250ms window).
// The PCB sysctl has neither horn: it IS the live kernel table (a brand-new
// socket is present at decision time) and one fetch+scan measures ~230µs
// p50 on a loaded desktop — ~300x cheaper than one lsof sweep — so it can
// simply run per lookup with no snapshot, TTL, or throttle at all.
//
// The lsof snapshot machinery is retained verbatim as a fallback: a one-time
// self-check attributes one of our own loopback sockets through the PCB path
// at first use, and any failure (sysctl EPERM in an exotic sandbox, a struct
// layout change in a future macOS) flips the searcher to the legacy path for
// the process lifetime, with a loud `DIAG: process-pcb-fallback`.
//
// Struct layout: the stream is self-describing — a `xinpgen` header, then
// (xgn_len, xgn_kind)-prefixed blocks, one group of blocks per socket, and a
// repeated kind marks the next group (mirrors Apple's netstat inet.c walk).
// Field offsets below are from xnu bsd/netinet/in_pcb.h (struct xinpcb_n)
// and bsd/sys/socketvar.h (struct xsocket_n) on LP64, and are pinned by
// TestPCBLayout_OwnSocketFields which extracts a known socket end-to-end and
// cross-checks ports, address, uid AND pid — layout drift fails tests before
// it ships, and fails the runtime self-check (→ lsof fallback) if it ever
// reaches the field.
const (
	pcbKindSocket = 0x001 // XSO_SOCKET
	pcbKindInpcb  = 0x010 // XSO_INPCB

	// struct xinpcb_n offsets. The _n structs are PACKED (xnu declares them
	// under #pragma pack), so u64 fields after inp_lport sit at unaligned
	// offsets — do not "correct" these to natural alignment. Pinned
	// empirically from a live kernel dump (inp_ip_ttl=64 lands at 45,
	// 127.0.0.1 lands at 60/76 for faddr/laddr) and enforced by
	// TestPCBLayout_OwnSocketFields + pcbSelfCheck at runtime.
	pcbInpFportOff = 16 // u16, network byte order
	pcbInpLportOff = 18 // u16, network byte order
	pcbInpVflagOff = 44 // u8: INP_IPV4=0x1, INP_IPV6=0x2 (ttl@45, ip_p@46)
	pcbInpLaddrOff = 64 // 16-byte union; IPv4 occupies the last 4 bytes
	pcbInpLaddr4   = 76 // = pcbInpLaddrOff + 12 (struct in_addr_4in6 padding)
	pcbInpcbMinLen = 80

	// struct xsocket_n offsets
	pcbSoUIDOff     = 64 // u32 so_uid
	pcbSoLastPidOff = 68 // i32 so_last_pid
	pcbSoEPidOff    = 72 // i32 so_e_pid — the app a delegated socket acts for
	pcbSocketMinLen = 72 // enough for so_last_pid; so_e_pid is read when present

	pcbInpIPv4 = 0x1 // INP_IPV4
	pcbInpIPv6 = 0x2 // INP_IPV6
)

func pcbSysctlName(network string) string {
	switch network {
	case "tcp":
		return "net.inet.tcp.pcblist_n"
	case "udp":
		return "net.inet.udp.pcblist_n"
	default:
		return ""
	}
}

func pcbRoundup8(n int) int { return (n + 7) &^ 7 }

// pcbOwner is what the kernel records about who a socket belongs to.
//
// pid is so_last_pid: the process that last touched the socket. ePid is
// so_e_pid, set only on DELEGATED sockets (SOF_DELEGATED) and 0 otherwise:
// system services that open sockets on an app's behalf stamp that app here.
// Measured on macOS 26: com.apple.WebKit.Networking (every WKWebView page an
// app loads) and mDNSResponder (every getaddrinfo from a native app) both
// delegate, so so_last_pid alone names the system service and a forced-direct
// app's embedded web pages and DNS silently fall through to the tunnel.
type pcbOwner struct {
	pid  int32
	ePid int32
	uid  uint32
}

// pcbLookupPid scans one pcblist_n buffer for the socket whose LOCAL half is
// (srcIP, srcPort) and returns its owner.
//
// Match semantics mirror the Windows searcher: an exact local-address match
// always beats a wildcard (unspecified laddr) match, and wildcard matching is
// what attributes unconnected UDP sendto() sockets — a strict improvement
// over the lsof index, which skipped wildcard sockets entirely. First match
// wins within each class, for determinism when TIME_WAIT ghosts share a port.
func pcbLookupPid(buf []byte, srcIP netip.Addr, srcPort uint16) (owner pcbOwner, ok bool) {
	srcIP = srcIP.Unmap()
	var exact, wildcard *pcbOwner
	pcbForEachGroup(buf, func(inp, so []byte) {
		if len(so) < pcbSocketMinLen {
			return
		}
		switch pcbMatchInpcb(inp, srcIP, srcPort) {
		case pcbMatchExact:
			if exact == nil {
				o := pcbSocketOwner(so)
				exact = &o
			}
		case pcbMatchWildcard:
			if wildcard == nil {
				o := pcbSocketOwner(so)
				wildcard = &o
			}
		}
	})
	if exact != nil {
		return *exact, true
	}
	if wildcard != nil {
		return *wildcard, true
	}
	return pcbOwner{}, false
}

// pcbForEachGroup walks one pcblist_n buffer and calls fn once per socket
// group with that group's xinpcb_n and xsocket_n blocks (either may be nil
// when the kernel omitted it). Malformed or truncated input ends the walk.
func pcbForEachGroup(buf []byte, fn func(inp, so []byte)) {
	if len(buf) < 8 {
		return
	}
	xigLen := int(binary.LittleEndian.Uint32(buf[0:4]))
	if xigLen < 8 || xigLen > len(buf) {
		return
	}

	var inp, so []byte
	var seen uint32
	flush := func() {
		if seen != 0 {
			fn(inp, so)
		}
		inp, so, seen = nil, nil, 0
	}

	off := pcbRoundup8(xigLen)
	for off+8 <= len(buf) {
		l := int(binary.LittleEndian.Uint32(buf[off:]))
		kind := binary.LittleEndian.Uint32(buf[off+4:])
		if l <= xigLen || off+l > len(buf) {
			break // trailing xinpgen terminator, or truncated buffer
		}
		blk := buf[off : off+l]

		var kindBit uint32
		switch kind {
		case pcbKindInpcb:
			kindBit = 1
		case pcbKindSocket:
			kindBit = 2
		}
		if kindBit != 0 && seen&kindBit != 0 {
			flush() // repeated kind = new socket group starts here
		}
		seen |= kindBit

		switch kind {
		case pcbKindInpcb:
			inp = blk
		case pcbKindSocket:
			so = blk
		}
		off += pcbRoundup8(l)
	}
	flush()
}

// pcbSocketOwner decodes one xsocket_n block (len >= pcbSocketMinLen). A block
// too short to carry so_e_pid reads as not delegated.
func pcbSocketOwner(so []byte) pcbOwner {
	o := pcbOwner{
		pid: int32(binary.LittleEndian.Uint32(so[pcbSoLastPidOff:])),
		uid: binary.LittleEndian.Uint32(so[pcbSoUIDOff:]),
	}
	if len(so) >= pcbSoEPidOff+4 {
		o.ePid = int32(binary.LittleEndian.Uint32(so[pcbSoEPidOff:]))
	}
	return o
}

const (
	pcbMatchNone = iota
	pcbMatchExact
	pcbMatchWildcard
)

// pcbMatchInpcb classifies one xinpcb_n block against (srcIP, srcPort).
func pcbMatchInpcb(blk []byte, srcIP netip.Addr, srcPort uint16) int {
	laddr, lport, ok := pcbInpLocal(blk)
	if !ok || lport != srcPort {
		return pcbMatchNone
	}
	switch {
	case laddr == srcIP:
		return pcbMatchExact
	case laddr.IsUnspecified():
		return pcbMatchWildcard
	default:
		return pcbMatchNone
	}
}

// pcbInpLocal decodes the local (address, port) half of one xinpcb_n block.
func pcbInpLocal(blk []byte) (netip.Addr, uint16, bool) {
	if len(blk) < pcbInpcbMinLen {
		return netip.Addr{}, 0, false
	}
	lport := binary.BigEndian.Uint16(blk[pcbInpLportOff:])
	vflag := blk[pcbInpVflagOff]
	switch {
	case vflag&pcbInpIPv4 != 0:
		return netip.AddrFrom4([4]byte(blk[pcbInpLaddr4 : pcbInpLaddr4+4])), lport, true
	case vflag&pcbInpIPv6 != 0:
		return netip.AddrFrom16([16]byte(blk[pcbInpLaddrOff : pcbInpLaddrOff+16])).Unmap(), lport, true
	default:
		return netip.Addr{}, 0, false
	}
}

// pcbProcName resolves a pid to the same name lsof's COMMAND column carries:
// proc_name (pbi_name — e.g. "Google Chrome Helper (Renderer)", untruncated
// at 2*MAXCOMLEN, and the same source that reports Claude Code as "2.1.232"),
// falling back to the proc_pidpath basename when proc_name is denied
// (cross-uid callers without root; the production daemon is root and never
// takes the fallback). Both are single syscalls — no caching needed.
func pcbProcName(pid int32) string {
	var nameBuf [128]byte
	if n := C.proc_name(C.int(pid), unsafe.Pointer(&nameBuf[0]), C.uint32_t(len(nameBuf))); n > 0 {
		return string(nameBuf[:n])
	}
	var pathBuf [4096]byte
	n := C.proc_pidpath(C.int(pid), unsafe.Pointer(&pathBuf[0]), C.uint32_t(len(pathBuf)))
	if n <= 0 {
		return ""
	}
	path := pathBuf[:n]
	if i := bytes.LastIndexByte(path, '/'); i >= 0 {
		path = path[i+1:]
	}
	return string(path)
}

// pcbFindProcess is the per-lookup pipeline: fetch the live table, find the
// socket, name its owner.
func pcbFindProcess(network string, srcIP netip.Addr, srcPort uint16) string {
	name := pcbSysctlName(network)
	if name == "" {
		return ""
	}
	buf, err := unix.SysctlRaw(name)
	if err != nil {
		return "" // fail-open, same directional choice as every miss path
	}
	o, ok := pcbLookupPid(buf, srcIP, srcPort)
	if !ok {
		return ""
	}
	return pcbOwnerName(o, int32(os.Getpid()))
}

// pcbOwnerName names the app a socket's traffic belongs to: the delegating
// app when the socket is delegated, else the socket's own process. Self
// sockets — ours, or opened on our behalf — return "": the daemon holds the
// proxied upper halves of every flow, and attributing an app's traffic to
// the daemon would defeat App Bypass (same rule parseLsofIndex applies).
// A delegator that already exited has no name; the socket's own process is
// still a truthful answer, so fall back to it rather than to "".
func pcbOwnerName(o pcbOwner, self int32) string {
	if o.pid == self || o.ePid == self {
		return ""
	}
	if o.ePid > 0 {
		if name := pcbProcName(o.ePid); name != "" {
			return name
		}
	}
	return pcbProcName(o.pid)
}

// pcbSelfCheck proves the whole pipeline on a real socket: dial our own
// loopback listener, then attribute the dialer's local half through the PCB
// path and require our own pid and a non-empty name. Any layout drift, sysctl
// restriction, or libproc failure surfaces here as false → the searcher runs
// on the legacy lsof snapshot instead (today's behavior, no worse).
func pcbSelfCheck() bool {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return false
	}
	defer ln.Close()
	conn, err := net.DialTimeout("tcp", ln.Addr().String(), 2*time.Second)
	if err != nil {
		return false
	}
	defer conn.Close()

	la := conn.LocalAddr().(*net.TCPAddr)
	ip, ok := netip.AddrFromSlice(la.IP)
	if !ok {
		return false
	}
	buf, err := unix.SysctlRaw("net.inet.tcp.pcblist_n")
	if err != nil {
		return false
	}
	o, found := pcbLookupPid(buf, ip.Unmap(), uint16(la.Port))
	if !found || o.pid != int32(os.Getpid()) {
		return false
	}
	return pcbProcName(o.pid) != ""
}
