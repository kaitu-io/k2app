//go:build windows

package provider

import (
	"encoding/binary"
	"fmt"
	"log/slog"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// tcpRowOwnerPID mirrors MIB_TCPROW_OWNER_PID (iphlpapi.h).
// Fields stored in network byte order; use ntohs / uint32ToIPv4 to decode.
type tcpRowOwnerPID struct {
	State      uint32
	LocalAddr  uint32
	LocalPort  uint32
	RemoteAddr uint32
	RemotePort uint32
	OwningPID  uint32
}

// udpRowOwnerPID mirrors MIB_UDPROW_OWNER_PID. No remote endpoint.
type udpRowOwnerPID struct {
	LocalAddr uint32
	LocalPort uint32
	OwningPID uint32
}

// Constants from iphlpapi.h.
const (
	AF_INET                 = 2
	TCP_TABLE_OWNER_PID_ALL = 5
	UDP_TABLE_OWNER_PID     = 1
)

// ntohs converts the low 16 bits of n from network byte order to host order.
//
// Windows TCP/UDP table fields (dwLocalPort, dwRemotePort) store the port
// in the low 16 bits of a uint32, in network byte order. Example: port
// 54321 (0xD431 NBO bytes [D4,31]) is stored as low 16 bits of dwLocalPort
// = 0x31D4. ntohs(0x31D4) = 0xD431 = 54321.
func ntohs(n uint32) uint16 {
	v := uint16(n & 0xFFFF)
	return (v >> 8) | (v << 8)
}

// uint32ToIPv4 extracts 4 IPv4 bytes from a uint32 stored in Windows
// network byte order. Example: dwLocalAddr 0x0501A8C0 (LE uint32) means
// IP 192.168.1.5 — PutUint32(LittleEndian) writes [0xC0, 0xA8, 0x01, 0x05].
func uint32ToIPv4(n uint32) [4]byte {
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], n)
	return b
}

var (
	iphlpapi                = windows.NewLazySystemDLL("iphlpapi.dll")
	procGetExtendedTcpTable = iphlpapi.NewProc("GetExtendedTcpTable")
	procGetExtendedUdpTable = iphlpapi.NewProc("GetExtendedUdpTable")
)

// tcpTableFetcher is the seam between the searcher's matching logic and the
// underlying iphlpapi syscall. Tests inject mocks.
type tcpTableFetcher interface {
	Fetch() ([]tcpRowOwnerPID, error)
}

type udpTableFetcher interface {
	Fetch() ([]udpRowOwnerPID, error)
}

type processNamer interface {
	NameForPID(pid uint32) string
}

type realTCPFetcher struct{}

func (realTCPFetcher) Fetch() ([]tcpRowOwnerPID, error) {
	var size uint32
	// First call: probe for required buffer size.
	procGetExtendedTcpTable.Call(
		0, uintptr(unsafe.Pointer(&size)),
		0, AF_INET, TCP_TABLE_OWNER_PID_ALL, 0,
	)
	if size == 0 {
		return nil, nil
	}
	buf := make([]byte, size)
	ret, _, _ := procGetExtendedTcpTable.Call(
		uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)),
		0, AF_INET, TCP_TABLE_OWNER_PID_ALL, 0,
	)
	if ret != 0 {
		return nil, fmt.Errorf("GetExtendedTcpTable: %d", ret)
	}
	// buf layout: [uint32 dwNumEntries] [tcpRowOwnerPID × N]
	n := *(*uint32)(unsafe.Pointer(&buf[0]))
	if n == 0 {
		return nil, nil
	}
	raw := unsafe.Slice(
		(*tcpRowOwnerPID)(unsafe.Pointer(&buf[4])),
		int(n),
	)
	out := make([]tcpRowOwnerPID, n)
	copy(out, raw) // copy out of buf — buf is GC'd after return
	return out, nil
}

type realUDPFetcher struct{}

func (realUDPFetcher) Fetch() ([]udpRowOwnerPID, error) {
	var size uint32
	procGetExtendedUdpTable.Call(
		0, uintptr(unsafe.Pointer(&size)),
		0, AF_INET, UDP_TABLE_OWNER_PID, 0,
	)
	if size == 0 {
		return nil, nil
	}
	buf := make([]byte, size)
	ret, _, _ := procGetExtendedUdpTable.Call(
		uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)),
		0, AF_INET, UDP_TABLE_OWNER_PID, 0,
	)
	if ret != 0 {
		return nil, fmt.Errorf("GetExtendedUdpTable: %d", ret)
	}
	n := *(*uint32)(unsafe.Pointer(&buf[0]))
	if n == 0 {
		return nil, nil
	}
	raw := unsafe.Slice(
		(*udpRowOwnerPID)(unsafe.Pointer(&buf[4])),
		int(n),
	)
	out := make([]udpRowOwnerPID, n)
	copy(out, raw)
	return out, nil
}

type realProcNamer struct{}

func (realProcNamer) NameForPID(pid uint32) string {
	// PROCESS_QUERY_LIMITED_INFORMATION works on system-protected processes
	// (smss/csrss/etc.) where PROCESS_QUERY_INFORMATION would be denied.
	h, err := windows.OpenProcess(
		windows.PROCESS_QUERY_LIMITED_INFORMATION,
		false, pid,
	)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(h)

	var size uint32 = windows.MAX_PATH
	buf := make([]uint16, size)
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &size); err != nil {
		return ""
	}
	// Lowercase to match the rule side: app-bypass rules are built with the
	// process names lowercased on Windows (engine/region_expand.go), and
	// rule.processNameMatches is a case-sensitive exact compare. Without this,
	// any app whose on-disk exe basename is not already lowercase
	// (Discord.exe, Code.exe, WeChat.exe, Telegram.exe) would never match its
	// override — a silent App Bypass failure. Windows paths are
	// case-insensitive, so lowercasing the observed name is always safe.
	return strings.ToLower(filepath.Base(windows.UTF16ToString(buf[:size])))
}

type WindowsProcessSearcher struct {
	cache      *processCache
	tcpFetcher tcpTableFetcher
	udpFetcher udpTableFetcher
	procNamer  processNamer
	selfPID    uint32
}

func NewWindowsProcessSearcher() *WindowsProcessSearcher {
	return &WindowsProcessSearcher{
		cache:      newProcessCache(processCacheCap, processCacheTTL),
		tcpFetcher: realTCPFetcher{},
		udpFetcher: realUDPFetcher{},
		procNamer:  realProcNamer{},
		selfPID:    uint32(os.Getpid()),
	}
}

// FindProcess returns (processName, "", "") — Windows has no packageName /
// installerPackage (Android-only); the cache key + entry carry empty strings
// for those fields.
func (s *WindowsProcessSearcher) FindProcess(network string, srcIP netip.Addr, srcPort uint16) (string, string, string) {
	if !srcIP.Is4() {
		return "", "", ""
	}
	key := cacheKey(network, srcIP, srcPort)
	if name, pkg, installer, ok := s.cache.get(key); ok {
		return name, pkg, installer
	}

	start := time.Now()
	name := s.lookupUncached(network, srcIP, srcPort)
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		slog.Info("DIAG: process-lookup-slow",
			"platform", "windows",
			"network", network,
			"latencyMs", elapsed.Milliseconds())
	}

	s.cache.set(key, name, "", "")
	return name, "", ""
}

func (s *WindowsProcessSearcher) lookupUncached(network string, srcIP netip.Addr, srcPort uint16) string {
	srcBytes := srcIP.As4()
	switch network {
	case "tcp":
		return s.findInTCP(srcBytes, srcPort)
	case "udp":
		return s.findInUDP(srcBytes, srcPort)
	}
	return ""
}

func (s *WindowsProcessSearcher) findInTCP(srcAddr [4]byte, srcPort uint16) string {
	rows, err := s.tcpFetcher.Fetch()
	if err != nil {
		return ""
	}
	for i := range rows {
		r := &rows[i]
		if r.OwningPID == 0 || r.OwningPID == s.selfPID {
			continue
		}
		if ntohs(r.LocalPort) != srcPort {
			continue
		}
		if uint32ToIPv4(r.LocalAddr) != srcAddr {
			continue
		}
		return s.procNamer.NameForPID(r.OwningPID)
	}
	return ""
}

func (s *WindowsProcessSearcher) findInUDP(srcAddr [4]byte, srcPort uint16) string {
	rows, err := s.udpFetcher.Fetch()
	if err != nil {
		return ""
	}
	// Windows registers UDP sockets created via socket()+sendto() (the default
	// for net.Dial("udp"), most app SDKs, browser QUIC, online games, etc.)
	// with dwLocalAddr=0.0.0.0 even when the OS later routes outbound packets
	// through a specific interface. Treat 0.0.0.0 as a wildcard and match
	// on port alone. Collisions are bounded by the kernel: at most one
	// socket can own (0.0.0.0:port) absent SO_REUSEADDR, and ephemeral ports
	// are unique per active flow.
	zeroAddr := [4]byte{0, 0, 0, 0}
	for i := range rows {
		r := &rows[i]
		if r.OwningPID == 0 || r.OwningPID == s.selfPID {
			continue
		}
		if ntohs(r.LocalPort) != srcPort {
			continue
		}
		local := uint32ToIPv4(r.LocalAddr)
		if local != srcAddr && local != zeroAddr {
			continue
		}
		return s.procNamer.NameForPID(r.OwningPID)
	}
	return ""
}
