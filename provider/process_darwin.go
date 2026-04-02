//go:build darwin && !ios

package provider

import (
	"fmt"
	"net/netip"
	"os/exec"
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
// lsof -i <proto>@<ip>:<port> -P -n -F c
// Output: "p<pid>\nc<command>\n"
func lsofLookup(network string, srcIP netip.Addr, srcPort uint16) string {
	proto := "tcp"
	if network == "udp" {
		proto = "udp"
	}
	spec := fmt.Sprintf("%s@%s:%d", proto, srcIP, srcPort)

	out, err := exec.Command("lsof", "-i", spec, "-P", "-n", "-F", "c").Output()
	if err != nil || len(out) == 0 {
		return ""
	}

	// Parse -F c output: lines starting with 'c' have the command name.
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "c") {
			return line[1:]
		}
	}
	return ""
}
