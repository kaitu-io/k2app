package dns

import (
	"bufio"
	"net"
	"os"
	"runtime"
	"strings"
	"sync"
)

// HostsFile manages /etc/hosts lookups
type HostsFile struct {
	mu      sync.RWMutex
	entries map[string][]net.IP // domain -> IPs
}

// NewHostsFile creates a new hosts file manager
func NewHostsFile() *HostsFile {
	return &HostsFile{
		entries: make(map[string][]net.IP),
	}
}

// Load parses the hosts file and loads entries into memory
func (h *HostsFile) Load(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	h.mu.Lock()
	// concurrency:allow — file I/O only at startup, no concurrent mutation risk
	defer h.mu.Unlock()

	h.entries = make(map[string][]net.IP)

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()

		// Remove comments
		if idx := strings.Index(line, "#"); idx >= 0 {
			line = line[:idx]
		}

		// Trim whitespace
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Parse: IP hostname [aliases...]
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}

		ip := net.ParseIP(fields[0])
		if ip == nil {
			continue
		}

		// Add entry for each hostname/alias
		for _, hostname := range fields[1:] {
			hostname = strings.ToLower(hostname)
			h.entries[hostname] = append(h.entries[hostname], ip)
		}
	}

	return scanner.Err()
}

// LoadDefault loads the system's default hosts file
func (h *HostsFile) LoadDefault() error {
	path := "/etc/hosts"
	if runtime.GOOS == "windows" {
		path = `C:\Windows\System32\Drivers\etc\hosts`
	}
	return h.Load(path)
}

// Lookup returns IP addresses for a domain from the hosts file
// Returns (IPs, true) if found, (nil, false) if not found
func (h *HostsFile) Lookup(domain string) ([]net.IP, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	domain = strings.ToLower(domain)

	// Direct lookup
	if ips, found := h.entries[domain]; found {
		return ips, true
	}

	return nil, false
}

// Size returns the number of hosts entries
func (h *HostsFile) Size() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.entries)
}
