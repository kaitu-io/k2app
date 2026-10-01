//go:build windows

package provider

import (
	"fmt"
	"log/slog"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Row structs (tcpRowOwnerPID etc.), the layout guards, and parseOwnerTable
// live in process_wintable.go — kept build-tag-free so the literal-offset
// layout tests run on every platform.

// Constants from iphlpapi.h / winsock2.h.
const (
	AF_INET                 = 2
	AF_INET6                = 23
	TCP_TABLE_OWNER_PID_ALL = 5
	UDP_TABLE_OWNER_PID     = 1
)

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

type tcp6TableFetcher interface {
	Fetch() ([]tcp6RowOwnerPID, error)
}

type udp6TableFetcher interface {
	Fetch() ([]udp6RowOwnerPID, error)
}

type processNamer interface {
	NameForPID(pid uint32) string
}

// processIdenter is the optional richer form of processNamer: name and full
// image path from one OpenProcess. A namer without it (test fakes) yields
// name-only attribution.
type processIdenter interface {
	IdentForPID(pid uint32) (name, path string)
}

// fetchOwnerTable is the shared GetExtended{Tcp,Udp}Table call pattern:
// size-probe, fetch, decode. All four (af × proto) fetchers route through it.
func fetchOwnerTable[T any](proc *windows.LazyProc, af, tableClass uintptr) ([]T, error) {
	var size uint32
	// First call: probe for required buffer size.
	proc.Call(
		0, uintptr(unsafe.Pointer(&size)),
		0, af, tableClass, 0,
	)
	if size == 0 {
		return nil, nil
	}
	buf := make([]byte, size)
	ret, _, _ := proc.Call(
		uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)),
		0, af, tableClass, 0,
	)
	if ret != 0 {
		return nil, fmt.Errorf("%s(af=%d): %d", proc.Name, af, ret)
	}
	// The second call may report fewer bytes written than probed; decode
	// only what the kernel filled in.
	if int(size) < len(buf) {
		buf = buf[:size]
	}
	return parseOwnerTable[T](buf)
}

type realTCPFetcher struct{}

func (realTCPFetcher) Fetch() ([]tcpRowOwnerPID, error) {
	return fetchOwnerTable[tcpRowOwnerPID](procGetExtendedTcpTable, AF_INET, TCP_TABLE_OWNER_PID_ALL)
}

type realUDPFetcher struct{}

func (realUDPFetcher) Fetch() ([]udpRowOwnerPID, error) {
	return fetchOwnerTable[udpRowOwnerPID](procGetExtendedUdpTable, AF_INET, UDP_TABLE_OWNER_PID)
}

type realTCP6Fetcher struct{}

func (realTCP6Fetcher) Fetch() ([]tcp6RowOwnerPID, error) {
	return fetchOwnerTable[tcp6RowOwnerPID](procGetExtendedTcpTable, AF_INET6, TCP_TABLE_OWNER_PID_ALL)
}

type realUDP6Fetcher struct{}

func (realUDP6Fetcher) Fetch() ([]udp6RowOwnerPID, error) {
	return fetchOwnerTable[udp6RowOwnerPID](procGetExtendedUdpTable, AF_INET6, UDP_TABLE_OWNER_PID)
}

type realProcNamer struct{}

func (n realProcNamer) NameForPID(pid uint32) string {
	name, _ := n.IdentForPID(pid)
	return name
}

// IdentForPID implements processIdenter: the lowercased image basename plus
// the full image path in rule.NormalizeProcessPath form (lowercased,
// backslashes — QueryFullProcessImageName already returns a Win32 path).
func (realProcNamer) IdentForPID(pid uint32) (name, path string) {
	// PROCESS_QUERY_LIMITED_INFORMATION works on system-protected processes
	// (smss/csrss/etc.) where PROCESS_QUERY_INFORMATION would be denied.
	h, err := windows.OpenProcess(
		windows.PROCESS_QUERY_LIMITED_INFORMATION,
		false, pid,
	)
	if err != nil {
		return "", ""
	}
	defer windows.CloseHandle(h)

	// 32767 = the NT path limit: an image under a long-path install dir must
	// not be reported as "unknown process" just because it exceeds MAX_PATH.
	var size uint32 = 32767
	buf := make([]uint16, size)
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &size); err != nil {
		return "", ""
	}
	// Lowercase to match the rule side: app-bypass rules are built with the
	// process names lowercased on Windows (engine/region_expand.go), and
	// rule.processNameMatches is a case-sensitive exact compare. Without this,
	// any app whose on-disk exe basename is not already lowercase
	// (Discord.exe, Code.exe, WeChat.exe, Telegram.exe) would never match its
	// override — a silent App Bypass failure. Windows paths are
	// case-insensitive, so lowercasing the observed name is always safe.
	full := strings.ToLower(windows.UTF16ToString(buf[:size]))
	return filepath.Base(full), full
}

type WindowsProcessSearcher struct {
	cache       *processCache
	tcpFetcher  tcpTableFetcher
	udpFetcher  udpTableFetcher
	tcp6Fetcher tcp6TableFetcher
	udp6Fetcher udp6TableFetcher
	procNamer   processNamer
	selfPID     uint32

	// System-resolver attribution (dnsclient_etw_windows.go). nil while off.
	dnsMu    sync.Mutex
	dnsWatch *dnsClientWatch
	dnsHolds int
}

func NewWindowsProcessSearcher() *WindowsProcessSearcher {
	return &WindowsProcessSearcher{
		cache:       newProcessCache(processCacheCap, processCacheTTL),
		tcpFetcher:  realTCPFetcher{},
		udpFetcher:  realUDPFetcher{},
		tcp6Fetcher: realTCP6Fetcher{},
		udp6Fetcher: realUDP6Fetcher{},
		procNamer:   realProcNamer{},
		selfPID:     uint32(os.Getpid()),
	}
}

// FindProcess returns (processName, "", "") — Windows has no packageName /
// installerPackage (Android-only); the cache key + entry carry empty strings
// for those fields.
func (s *WindowsProcessSearcher) FindProcess(network string, srcIP netip.Addr, srcPort uint16) (string, string, string) {
	name, _ := s.FindProcessPath(network, srcIP, srcPort)
	return name, "", ""
}

// FindProcessPath implements ProcessPathSearcher.
func (s *WindowsProcessSearcher) FindProcessPath(network string, srcIP netip.Addr, srcPort uint16) (string, string) {
	if !srcIP.IsValid() {
		return "", ""
	}
	// Canonicalize locally instead of trusting every caller to have done it
	// (core/tunnel.go routeConn does): a 4-in-6 ::ffff: addr must hit the
	// IPv4 tables first, and the cache key must be stable across both forms.
	srcIP = srcIP.Unmap()
	key := cacheKey(network, srcIP, srcPort)
	if name, path, ok := s.cache.getIdent(key); ok {
		return name, path
	}

	start := time.Now()
	name, path := s.lookupUncached(network, srcIP, srcPort)
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		slog.Info("DIAG: process-lookup-slow",
			"platform", "windows",
			"network", network,
			"latencyMs", elapsed.Milliseconds())
	}

	s.cache.setIdent(key, name, path)
	return name, path
}

// identForPID resolves a table row's owning pid. pid 0 = no row matched.
func (s *WindowsProcessSearcher) identForPID(pid uint32) (name, path string) {
	if pid == 0 {
		return "", ""
	}
	if id, ok := s.procNamer.(processIdenter); ok {
		return id.IdentForPID(pid)
	}
	return s.procNamer.NameForPID(pid), ""
}

func (s *WindowsProcessSearcher) lookupUncached(network string, srcIP netip.Addr, srcPort uint16) (name, path string) {
	if srcIP.Is4() {
		srcBytes := srcIP.As4()
		var pid uint32
		switch network {
		case "tcp":
			pid = s.findInTCP(srcBytes, srcPort)
		case "udp":
			pid = s.findInUDP(srcBytes, srcPort)
		default:
			return "", ""
		}
		if name, path := s.identForPID(pid); name != "" {
			return name, path
		}
		// Fall through: an AF_INET6 socket without IPV6_V6ONLY (dual-stack —
		// Go's net.Dial default, Chromium's QUIC stack, the JVM) carries IPv4
		// flows too, but Windows lists it ONLY in the AF_INET6 tables, with a
		// v4-mapped local address. srcIP.As16() of an IPv4 addr is exactly
		// that ::ffff: form, so the v6-table search below covers both cases.
	}
	switch network {
	case "tcp":
		return s.identForPID(s.findInTCP6(srcIP.As16(), srcPort))
	case "udp":
		return s.identForPID(s.findInUDP6(srcIP.As16(), srcPort))
	}
	return "", ""
}

func (s *WindowsProcessSearcher) findInTCP(srcAddr [4]byte, srcPort uint16) uint32 {
	rows, err := s.tcpFetcher.Fetch()
	if err != nil {
		return 0
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
		return r.OwningPID
	}
	return 0
}

func (s *WindowsProcessSearcher) findInTCP6(srcAddr [16]byte, srcPort uint16) uint32 {
	rows, err := s.tcp6Fetcher.Fetch()
	if err != nil {
		return 0
	}
	for i := range rows {
		r := &rows[i]
		if r.OwningPID == 0 || r.OwningPID == s.selfPID {
			continue
		}
		if ntohs(r.LocalPort) != srcPort {
			continue
		}
		if r.LocalAddr != srcAddr {
			continue
		}
		return r.OwningPID
	}
	return 0
}

func (s *WindowsProcessSearcher) findInUDP6(srcAddr [16]byte, srcPort uint16) uint32 {
	rows, err := s.udp6Fetcher.Fetch()
	if err != nil {
		return 0
	}
	// Same kernel behavior as the v4 table: socket()+sendto() sockets are
	// registered with an unspecified local address (:: here) regardless of
	// the interface outbound packets actually take. An exact LocalAddr match
	// always wins; :: is only a last resort so the answer never depends on
	// kernel table ordering. This also covers dual-stack UDP sockets whose
	// IPv4 flows fall through from findInUDP — with a residual ambiguity:
	// AF_INET and AF_INET6 are separate port spaces, so a fallen-through v4
	// flow can wildcard-match an unrelated IPV6_V6ONLY socket bound to
	// [::]:port. The table carries no V6ONLY bit, so that cannot be fully
	// resolved; it is bounded by the fallback only running after a v4-table
	// miss.
	var zeroAddr [16]byte
	var wildcardPID uint32
	for i := range rows {
		r := &rows[i]
		if r.OwningPID == 0 || r.OwningPID == s.selfPID {
			continue
		}
		if ntohs(r.LocalPort) != srcPort {
			continue
		}
		if r.LocalAddr == srcAddr {
			return r.OwningPID
		}
		if r.LocalAddr == zeroAddr && wildcardPID == 0 {
			wildcardPID = r.OwningPID
		}
	}
	if wildcardPID != 0 {
		return wildcardPID
	}
	return 0
}

func (s *WindowsProcessSearcher) findInUDP(srcAddr [4]byte, srcPort uint16) uint32 {
	rows, err := s.udpFetcher.Fetch()
	if err != nil {
		return 0
	}
	// Windows registers UDP sockets created via socket()+sendto() (the default
	// for net.Dial("udp"), most app SDKs, browser QUIC, online games, etc.)
	// with dwLocalAddr=0.0.0.0 even when the OS later routes outbound packets
	// through a specific interface. An exact LocalAddr match always wins;
	// 0.0.0.0 is only a last resort so the answer never depends on kernel
	// table ordering. Wildcard collisions are bounded by the kernel: at most
	// one socket can own (0.0.0.0:port) absent SO_REUSEADDR, and ephemeral
	// ports are unique per active flow.
	zeroAddr := [4]byte{0, 0, 0, 0}
	var wildcardPID uint32
	for i := range rows {
		r := &rows[i]
		if r.OwningPID == 0 || r.OwningPID == s.selfPID {
			continue
		}
		if ntohs(r.LocalPort) != srcPort {
			continue
		}
		local := uint32ToIPv4(r.LocalAddr)
		if local == srcAddr {
			return r.OwningPID
		}
		if local == zeroAddr && wildcardPID == 0 {
			wildcardPID = r.OwningPID
		}
	}
	if wildcardPID != 0 {
		return wildcardPID
	}
	return 0
}
