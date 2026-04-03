//go:build linux

package provider

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// LinuxProcessSearcher looks up process names by reading /proc/net/tcp{,6}
// to find the socket inode, then scanning /proc/*/fd/ for the owning PID,
// and reading /proc/PID/comm for the process name.
type LinuxProcessSearcher struct {
	packageResolver PackageResolver
}

// NewLinuxProcessSearcher creates a LinuxProcessSearcher with an optional
// PackageResolver for Android UID→package name mapping.
// Pass nil on non-Android Linux.
func NewLinuxProcessSearcher(pr PackageResolver) *LinuxProcessSearcher {
	return &LinuxProcessSearcher{packageResolver: pr}
}

func (s *LinuxProcessSearcher) FindProcess(network string, srcIP netip.Addr, srcPort uint16) (string, string) {
	inode := findSocketInode(network, srcIP, srcPort)
	if inode == 0 {
		return "", ""
	}
	pid := findPIDByInode(inode)
	if pid == 0 {
		return "", ""
	}
	name := readProcessName(pid)

	// On Android, resolve UID → package name via callback.
	var packageName string
	if s.packageResolver != nil {
		if uid := readProcessUID(pid); uid >= 10000 {
			packageName = s.packageResolver.PackageForUID(int32(uid))
		}
	}

	return name, packageName
}

// findSocketInode searches /proc/net/tcp or /proc/net/tcp6 for a matching
// source (IP, port) and returns the socket inode.
func findSocketInode(network string, srcIP netip.Addr, srcPort uint16) uint64 {
	var paths []string
	if srcIP.Is4() {
		paths = []string{"/proc/net/tcp"}
	} else {
		paths = []string{"/proc/net/tcp6"}
	}
	if network == "udp" {
		if srcIP.Is4() {
			paths = []string{"/proc/net/udp"}
		} else {
			paths = []string{"/proc/net/udp6"}
		}
	}

	for _, path := range paths {
		if inode := searchProcNet(path, srcIP, srcPort); inode != 0 {
			return inode
		}
	}
	return 0
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

// findPIDByInode scans /proc/*/fd/ for a symlink pointing to the socket inode.
func findPIDByInode(inode uint64) int {
	target := fmt.Sprintf("socket:[%d]", inode)
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		fdDir := filepath.Join("/proc", entry.Name(), "fd")
		fds, err := os.ReadDir(fdDir)
		if err != nil {
			continue
		}
		for _, fd := range fds {
			link, err := os.Readlink(filepath.Join(fdDir, fd.Name()))
			if err != nil {
				continue
			}
			if link == target {
				return pid
			}
		}
	}
	return 0
}

// readProcessName reads the process name from /proc/PID/comm.
func readProcessName(pid int) string {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/comm", pid))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// readProcessUID reads the process UID from /proc/PID/status.
func readProcessUID(pid int) int {
	f, err := os.Open(fmt.Sprintf("/proc/%d/status", pid))
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
