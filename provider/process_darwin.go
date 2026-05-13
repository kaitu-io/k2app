//go:build darwin && !ios

package provider

import (
	"fmt"
	"net/netip"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DarwinProcessSearcher uses lsof to look up process names by source (IP, port).
// Results are cached briefly to amortize the cost of exec.
type DarwinProcessSearcher struct {
	mu    sync.Mutex
	cache map[string]cacheEntry
}

type cacheEntry struct {
	name    string
	expires time.Time
}

const cacheTTL = 2 * time.Second

func (s *DarwinProcessSearcher) FindProcess(network string, srcIP netip.Addr, srcPort uint16) (string, string) {
	if !srcIP.Is4() {
		return "", ""
	}

	key := fmt.Sprintf("%s:%s:%d", network, srcIP, srcPort)

	s.mu.Lock()
	if s.cache != nil {
		if e, ok := s.cache[key]; ok && time.Now().Before(e.expires) {
			s.mu.Unlock()
			return e.name, ""
		}
	}
	s.mu.Unlock()

	name := lsofLookup(network, srcIP, srcPort)

	s.mu.Lock()
	if s.cache == nil {
		s.cache = make(map[string]cacheEntry)
	}
	s.cache[key] = cacheEntry{name: name, expires: time.Now().Add(cacheTTL)}
	s.mu.Unlock()

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
			// Emit previous (pid, cmd) if it was not self.
			if currentPid != 0 && currentPid != selfPid && currentCmd != "" {
				return currentCmd
			}
			// New PID block.
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
	// Tail: last block not yet emitted.
	if currentPid != 0 && currentPid != selfPid && currentCmd != "" {
		return currentCmd
	}
	return ""
}
