//go:build linux

package provider

import (
	"bufio"
	"encoding/hex"
	"log/slog"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/kaitu-io/k2/safego"
	"github.com/sasha-s/go-deadlock"
	"golang.org/x/sync/singleflight"
)

const inodeIndexTTL = 5 * time.Second

// LinuxProcessSearcher looks up process names by reading /proc/net/tcp{,6}
// to find the socket inode, then resolving inode→PID via a cached reverse
// index (stale-while-revalidate, singleflight-guarded rebuild), and finally
// reading /proc/PID/comm for the process name.
type LinuxProcessSearcher struct {
	packageResolver PackageResolver
	cache           *processCache
	procRoot        string // default "/proc", overridable for tests

	indexMu      deadlock.RWMutex
	inodeIndex   map[uint64]int // inode → PID snapshot
	indexExpires time.Time      // zero = never built

	sf                singleflight.Group    // dedup concurrent rebuilds
	buildInodeIndexFn func() map[uint64]int // seam for tests
}

// NewLinuxProcessSearcher creates a LinuxProcessSearcher with an optional
// PackageResolver for Android UID→package name mapping.
// Pass nil on non-Android Linux.
func NewLinuxProcessSearcher(pr PackageResolver) *LinuxProcessSearcher {
	s := &LinuxProcessSearcher{
		packageResolver: pr,
		cache:           newProcessCache(processCacheCap, processCacheTTL),
		procRoot:        "/proc",
	}
	s.buildInodeIndexFn = s.buildInodeIndex
	return s
}

func (s *LinuxProcessSearcher) FindProcess(network string, srcIP netip.Addr, srcPort uint16) (string, string) {
	if !srcIP.Is4() {
		return "", ""
	}
	key := cacheKey(network, srcIP, srcPort)
	if name, pkg, ok := s.cache.get(key); ok {
		return name, pkg
	}

	start := time.Now()
	name, pkg := s.lookupUncached(network, srcIP, srcPort)
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		slog.Info("DIAG: process-lookup-slow",
			"platform", "linux",
			"network", network,
			"latencyMs", elapsed.Milliseconds())
	}

	s.cache.set(key, name, pkg)
	return name, pkg
}

func (s *LinuxProcessSearcher) lookupUncached(network string, srcIP netip.Addr, srcPort uint16) (string, string) {
	inode := s.findSocketInode(network, srcIP, srcPort)
	if inode == 0 {
		return "", ""
	}
	pid := s.lookupPID(inode)
	if pid == 0 || pid == os.Getpid() {
		return "", ""
	}
	name := s.readProcessName(pid)

	var pkg string
	if s.packageResolver != nil {
		if uid := s.readProcessUID(pid); uid >= 10000 {
			pkg = s.packageResolver.PackageForUID(int32(uid))
		}
	}
	return name, pkg
}

func (s *LinuxProcessSearcher) findSocketInode(network string, srcIP netip.Addr, srcPort uint16) uint64 {
	var path string
	switch {
	case network == "udp" && srcIP.Is4():
		path = filepath.Join(s.procRoot, "net/udp")
	case network == "udp":
		path = filepath.Join(s.procRoot, "net/udp6")
	case srcIP.Is4():
		path = filepath.Join(s.procRoot, "net/tcp")
	default:
		path = filepath.Join(s.procRoot, "net/tcp6")
	}
	return searchProcNet(path, srcIP, srcPort)
}

// searchProcNet parses a /proc/net/tcp{,6} or /proc/net/udp{,6} file.
// Format: sl local_address rem_address st tx_queue:rx_queue tr:tm->when retrnsmt uid timeout inode ...
func searchProcNet(path string, srcIP netip.Addr, srcPort uint16) uint64 {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Scan() // skip header line
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 10 {
			continue
		}
		ip, port, ok := parseProcNetAddr(fields[1])
		if !ok || port != srcPort {
			continue
		}
		if ip != srcIP {
			continue
		}
		inode, err := strconv.ParseUint(fields[9], 10, 64)
		if err != nil {
			continue
		}
		return inode
	}
	return 0
}

// parseProcNetAddr parses "0100007F:0035" or IPv6 hex format from /proc/net/tcp.
func parseProcNetAddr(s string) (netip.Addr, uint16, bool) {
	parts := strings.SplitN(s, ":", 2)
	if len(parts) != 2 {
		return netip.Addr{}, 0, false
	}
	port, err := strconv.ParseUint(parts[1], 16, 16)
	if err != nil {
		return netip.Addr{}, 0, false
	}
	hexIP := parts[0]
	ipBytes, err := hex.DecodeString(hexIP)
	if err != nil {
		return netip.Addr{}, 0, false
	}
	var addr netip.Addr
	switch len(ipBytes) {
	case 4:
		// /proc/net/tcp stores IPv4 in little-endian (host byte order on x86).
		var b [4]byte
		b[0], b[1], b[2], b[3] = ipBytes[3], ipBytes[2], ipBytes[1], ipBytes[0]
		addr = netip.AddrFrom4(b)
	case 16:
		// /proc/net/tcp6 stores IPv6 as four 32-bit words in host byte order.
		var b [16]byte
		for i := 0; i < 4; i++ {
			off := i * 4
			b[off], b[off+1], b[off+2], b[off+3] = ipBytes[off+3], ipBytes[off+2], ipBytes[off+1], ipBytes[off]
		}
		addr = netip.AddrFrom16(b)
	default:
		return netip.Addr{}, 0, false
	}
	return addr, uint16(port), true
}

// lookupPID resolves an inode → PID via the cached reverse index.
// Cold start: synchronous index build (singleflight-protected).
// Stale: returns last-known value, kicks async rebuild.
// Fresh: direct map lookup.
func (s *LinuxProcessSearcher) lookupPID(inode uint64) int {
	s.indexMu.RLock()
	indexBuilt := !s.indexExpires.IsZero()
	pid := s.inodeIndex[inode]
	expired := indexBuilt && time.Now().After(s.indexExpires)
	s.indexMu.RUnlock()

	if !indexBuilt {
		s.sf.Do("rebuild", s.doRebuild)
		s.indexMu.RLock()
		pid = s.inodeIndex[inode]
		s.indexMu.RUnlock()
		return pid
	}

	if expired {
		safego.Go(func() { s.sf.Do("rebuild", s.doRebuild) })
	}
	return pid
}

func (s *LinuxProcessSearcher) doRebuild() (any, error) {
	idx := s.buildInodeIndexFn()
	s.indexMu.Lock()
	s.inodeIndex = idx
	s.indexExpires = time.Now().Add(inodeIndexTTL)
	s.indexMu.Unlock()
	return nil, nil
}

// buildInodeIndex walks s.procRoot/*/fd/ once and returns inode→PID map.
// Replaces per-lookup O(N) walk with single-pass index. Singleflight
// (via doRebuild) ensures concurrent triggers collapse to one build.
func (s *LinuxProcessSearcher) buildInodeIndex() map[uint64]int {
	idx := make(map[uint64]int, 1024)
	entries, err := os.ReadDir(s.procRoot)
	if err != nil {
		return idx
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		fdDir := filepath.Join(s.procRoot, entry.Name(), "fd")
		fds, err := os.ReadDir(fdDir)
		if err != nil {
			continue
		}
		for _, fd := range fds {
			link, err := os.Readlink(filepath.Join(fdDir, fd.Name()))
			if err != nil || !strings.HasPrefix(link, "socket:[") {
				continue
			}
			end := strings.IndexByte(link, ']')
			if end < 0 {
				continue
			}
			inode, err := strconv.ParseUint(link[8:end], 10, 64)
			if err != nil {
				continue
			}
			idx[inode] = pid
		}
	}
	return idx
}

// readProcessName reads the process name from procRoot/PID/comm.
func (s *LinuxProcessSearcher) readProcessName(pid int) string {
	data, err := os.ReadFile(filepath.Join(s.procRoot, strconv.Itoa(pid), "comm"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// readProcessUID reads the process UID from procRoot/PID/status.
func (s *LinuxProcessSearcher) readProcessUID(pid int) int {
	f, err := os.Open(filepath.Join(s.procRoot, strconv.Itoa(pid), "status"))
	if err != nil {
		return -1
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "Uid:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				uid, _ := strconv.Atoi(fields[1])
				return uid
			}
		}
	}
	return -1
}
