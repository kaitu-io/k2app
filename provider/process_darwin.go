//go:build darwin && !ios

package provider

import (
	"fmt"
	"net/netip"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// DarwinProcessSearcher uses lsof to look up process names by source (IP, port).
// Results cached via shared processCache helper.
type DarwinProcessSearcher struct {
	cache *processCache
}

// NewDarwinProcessSearcher constructs a Darwin process searcher with the
// project-standard cache (2s TTL, 4096 soft cap).
func NewDarwinProcessSearcher() *DarwinProcessSearcher {
	return &DarwinProcessSearcher{
		cache: newProcessCache(processCacheCap, processCacheTTL),
	}
}

func (s *DarwinProcessSearcher) FindProcess(network string, srcIP netip.Addr, srcPort uint16) (string, string) {
	if !srcIP.Is4() {
		return "", ""
	}
	// Lazy-init cache for zero-value struct callers (e.g. daemon factory
	// pre-shared-cache callers that did `&DarwinProcessSearcher{}`).
	if s.cache == nil {
		s.cache = newProcessCache(processCacheCap, processCacheTTL)
	}
	key := cacheKey(network, srcIP, srcPort)
	if name, pkg, ok := s.cache.get(key); ok {
		return name, pkg
	}
	name := lsofLookup(network, srcIP, srcPort)
	s.cache.set(key, name, "")
	return name, ""
}

// lsofLookup calls lsof to find the process name for a socket.
// lsof -i <proto>@<ip>:<port> returns BOTH endpoints (client + server) when
// a connection is established. We must exclude our own PID (the daemon) so
// we return the actual peer (e.g., curl, browser).
//
// Output format (-F pc): one "p<pid>" line per process, followed by a "c<command>"
// line, then "f<fd>" lines per matching socket. We scan line-by-line, tracking
// the current PID, and return the first command whose PID is not our own.
func lsofLookup(network string, srcIP netip.Addr, srcPort uint16) string {
	proto := "tcp"
	if network == "udp" {
		proto = "udp"
	}
	spec := fmt.Sprintf("%s@%s:%d", proto, srcIP, srcPort)

	out, err := exec.Command("lsof", "-i", spec, "-P", "-n", "-F", "pc").Output()
	if err != nil || len(out) == 0 {
		return ""
	}

	selfPid := os.Getpid()
	var currentPid int
	var currentCmd string
	for _, line := range strings.Split(string(out), "\n") {
		if len(line) < 1 {
			continue
		}
		switch line[0] {
		case 'p':
			if currentPid != 0 && currentPid != selfPid && currentCmd != "" {
				return currentCmd
			}
			pid, err := strconv.Atoi(line[1:])
			if err != nil {
				currentPid = 0
				currentCmd = ""
				continue
			}
			currentPid = pid
			currentCmd = ""
		case 'c':
			currentCmd = line[1:]
		}
	}
	if currentPid != 0 && currentPid != selfPid && currentCmd != "" {
		return currentCmd
	}
	return ""
}
