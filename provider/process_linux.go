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

func (s *LinuxProcessSearcher) FindProcess(network string, srcIP netip.Addr, srcPort uint16) (string, string, string) {
	if !srcIP.IsValid() {
		return "", "", ""
	}
	// Canonicalize locally instead of trusting every caller to have done it
	// (core/tunnel.go routeConn does): a 4-in-6 ::ffff: addr must hit the
	// IPv4 tables first, and the cache key must be stable across both forms.
	srcIP = srcIP.Unmap()
	key := cacheKey(network, srcIP, srcPort)
	if name, pkg, installer, ok := s.cache.get(key); ok {
		return name, pkg, installer
	}

	start := time.Now()
	name, pkg, installer := s.lookupUncached(network, srcIP, srcPort)
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		slog.Info("DIAG: process-lookup-slow",
			"platform", "linux",
			"network", network,
			"latencyMs", elapsed.Milliseconds())
	}

	s.cache.set(key, name, pkg, installer)
	return name, pkg, installer
}

func (s *LinuxProcessSearcher) lookupUncached(network string, srcIP netip.Addr, srcPort uint16) (string, string, string) {
	inode := s.findSocketInode(network, srcIP, srcPort)
	if inode == 0 {
		return "", "", ""
	}
	pid := s.lookupPID(inode)
	if pid == 0 || pid == os.Getpid() {
		return "", "", ""
	}
	name := s.readProcessName(pid)

	// On Android, resolve UID → package name via callback, then installer
	// source via PackageManager.getInstallSourceInfo (App Bypass v2 signal).
	var packageName, installerPackage string
	if s.packageResolver != nil {
		if uid := s.readProcessUID(pid); uid >= 10000 {
			packageName = s.packageResolver.PackageForUID(int32(uid))
			if packageName != "" {
				installerPackage = s.packageResolver.InstallerForPackage(packageName)
			}
		}
	}
	return name, packageName, installerPackage
}

func (s *LinuxProcessSearcher) findSocketInode(network string, srcIP netip.Addr, srcPort uint16) uint64 {
	proto := "tcp"
	if network == "udp" {
		proto = "udp"
	}
	if !srcIP.Is4() {
		return searchProcNet(filepath.Join(s.procRoot, "net", proto+"6"), srcIP, srcPort)
	}
	if inode := searchProcNet(filepath.Join(s.procRoot, "net", proto), srcIP, srcPort); inode != 0 {
		return inode
	}
	// Dual-stack fallback: an AF_INET6 socket without IPV6_V6ONLY (Go's
	// net.Dial default, Chromium's QUIC stack, JVM) carries IPv4 flows too,
	// and the kernel lists those sockets ONLY in /proc/net/{tcp6,udp6} with a
	// v4-mapped local address (::ffff:a.b.c.d). The caller unmapped srcIP, so
	// re-map it for the v6-table comparison.
	mapped := netip.AddrFrom16(srcIP.As16())
	return searchProcNet(filepath.Join(s.procRoot, "net", proto+"6"), mapped, srcPort)
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
//
//   - Cold start (no index yet): synchronous build, then look up.
//   - Fresh hit: return cached PID.
//   - Fresh miss: process likely spawned after last build (e.g. a newly-launched
//     browser making its first connection). Synchronous rebuild via
//     singleflight, then re-look-up. Negative lookups are cached at the
//     FindProcess layer (2s TTL by 4-tuple) so a ghost inode triggers at most
//     one rebuild per source 4-tuple.
//   - Stale hit: return cached PID immediately, kick async rebuild.
func (s *LinuxProcessSearcher) lookupPID(inode uint64) int {
	s.indexMu.RLock()
	indexBuilt := !s.indexExpires.IsZero()
	pid := s.inodeIndex[inode]
	expired := indexBuilt && time.Now().After(s.indexExpires)
	s.indexMu.RUnlock()

	if !indexBuilt || pid == 0 {
		// Cold start, or fresh-index miss: synchronous rebuild.
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

// readProcessName resolves the process name as the basename of
// procRoot/PID/exe. Falls back to procRoot/PID/comm only if the exe
// symlink is unreadable.
//
// Why exe, not comm: /proc/PID/comm reflects whatever the app last set
// via prctl(PR_SET_NAME) or pthread_setname_np on the main thread.
// Every Electron app (Chrome, VS Code, Slack, Discord, Signal, Element)
// and most browsers (Firefox) rename their workers to internal labels
// like "Chrome_ChildIOT" or "DOM Worker", which the UI never sees —
// so a comm-based match silently misses every modern multi-process app.
// /proc/PID/exe is the kernel's record of the executable image and is
// unaffected by app-level renaming. It also matches what the UI's
// app-list provider reports (executable basename), so the user-visible
// name and the daemon-observed name agree.
//
// `os.Readlink` returns the resolved path with " (deleted)" suffix when
// the binary has been replaced (e.g. mid-upgrade). Strip it so a
// post-upgrade flow still matches the original name.
func (s *LinuxProcessSearcher) readProcessName(pid int) string {
	exePath, err := os.Readlink(filepath.Join(s.procRoot, strconv.Itoa(pid), "exe"))
	if err == nil && exePath != "" {
		exePath = strings.TrimSuffix(exePath, " (deleted)")
		if name := filepath.Base(exePath); name != "" && name != "." && name != "/" {
			return name
		}
	}
	// Fallback: /proc/PID/comm. Subject to TASK_COMM_LEN=16 truncation and
	// prctl override, but the rule engine's processNameMatches() tolerates
	// the truncation case. This path catches processes whose exe symlink
	// can't be read (rare: post-exit race, restricted Yama ptrace_scope).
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
