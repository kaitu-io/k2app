//go:build darwin && !ios

package provider

import (
	"context"
	"fmt"
	"log/slog"
	"net/netip"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// lsofTimeout caps the lsof subprocess that runs on the connection-setup
// critical path. See lsofLookup for the rationale.
const lsofTimeout = 2 * time.Second

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

// FindProcess returns (processName, "", "") — Darwin has no packageName /
// installerPackage; those are Android-only. lsof handles macOS process
// attribution; the cache key + entry carry empty strings for the latter two.
func (s *DarwinProcessSearcher) FindProcess(network string, srcIP netip.Addr, srcPort uint16) (string, string, string) {
	if !srcIP.IsValid() {
		return "", "", ""
	}
	// Canonicalize locally instead of trusting every caller to have done it
	// (core/tunnel.go routeConn does): lsof expects the bare v4 form, and the
	// cache key must be stable across both forms.
	srcIP = srcIP.Unmap()
	// Lazy-init cache for zero-value struct callers (e.g. daemon factory
	// pre-shared-cache callers that did `&DarwinProcessSearcher{}`).
	if s.cache == nil {
		s.cache = newProcessCache(processCacheCap, processCacheTTL)
	}
	key := cacheKey(network, srcIP, srcPort)
	if name, pkg, installer, ok := s.cache.get(key); ok {
		return name, pkg, installer
	}
	start := time.Now()
	name := lsofLookup(network, srcIP, srcPort)
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		// macOS is the only platform that shells out (lsof) for attribution,
		// so it's the most stall-prone — mirror the Linux/Windows slow-lookup
		// DIAG (k2/CLAUDE.md reserved event) so a slow or hung lsof is visible
		// in logs instead of presenting as an unexplained connection stall.
		slog.Info("DIAG: process-lookup-slow",
			"platform", "darwin", "network", network, "latencyMs", elapsed.Milliseconds())
	}
	s.cache.set(key, name, "", "")
	return name, "", ""
}

// lsofLookup calls lsof to find the process name for a socket.
// lsof -i <proto>@<ip>:<port> returns BOTH endpoints (client + server) when
// a connection is established. We must exclude our own PID (the daemon) so
// we return the actual peer (e.g., curl, browser).
func lsofLookup(network string, srcIP netip.Addr, srcPort uint16) string {
	spec := lsofSpec(network, srcIP, srcPort)

	// Hard timeout: lsof normally returns in <30ms, but a stuck fd or a stalled
	// process-table walk can hang it indefinitely. Attribution runs synchronously
	// on the connection-setup path while the caller holds a conn-slot, so an
	// unbounded hang would block that connection forever and, under systemic
	// slowness, exhaust the conn-slot semaphore. Cap it; on timeout (or any
	// error) return "" → the connection falls back to the region/default route.
	ctx, cancel := context.WithTimeout(context.Background(), lsofTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "lsof", "-i", spec, "-P", "-n", "-F", "pc").Output()
	if err != nil || len(out) == 0 {
		return ""
	}
	return parseLsofOutput(out, os.Getpid())
}

// lsofSpec builds the `-i` address selector. lsof's grammar requires IPv6
// addresses in brackets (`tcp@[fdfe::2]:443`); a bare colon-hex address
// would be mis-split at the first colon and silently match nothing —
// exactly the kind of no-log attribution miss this package must avoid.
func lsofSpec(network string, srcIP netip.Addr, srcPort uint16) string {
	proto := "tcp"
	if network == "udp" {
		proto = "udp"
	}
	if srcIP.Is4() {
		return fmt.Sprintf("%s@%s:%d", proto, srcIP, srcPort)
	}
	return fmt.Sprintf("%s@[%s]:%d", proto, srcIP, srcPort)
}

// parseLsofOutput scans `lsof -F pc` machine-readable output and returns
// the COMMAND for the first record whose PID is not selfPid.
//
// Output format (-F pc): one "p<pid>" line opens a record, followed by a
// "c<command>" line, then "f<fd>" lines per matching socket. Unknown
// prefixes are skipped. We track the current (pid, cmd) and emit at each
// new "p" line, or at EOF.
//
// Quirks handled by this parser:
//   - The COMMAND field is **not** TASK_COMM_LEN-truncated on macOS
//     (verified empirically up to 27 chars: "com.apple.WebKit.Networking",
//     "Google Chrome Helper", "Code Helper (Plugin)"). The kernel does
//     truncate `pbi_comm` to MAXCOMLEN+1 but lsof prefers `pbi_name`
//     (2*MAXCOMLEN) and `proc_pidpath` basename. No truncation tolerance
//     is needed here; the value matches what the UI's app-list provider
//     reports.
//   - Spaces, parens, and dotted bundle names appear verbatim in
//     COMMAND (e.g. "Code Helper (Renderer)"). The line after `c` is
//     consumed as-is, no field splitting.
//   - Multi-process records: lsof outputs every PID that has a matching
//     socket. With a connected TCP/UDP pair, that's two records (client
//   - server) sharing one (proto, ip, port). The daemon is one of
//     them; we skip self and return the peer.
//   - Empty cmd ("c\n"): treated as no command, record skipped.
//   - PID 0: should not appear (kernel sockets), but if it does the
//     selfPid filter naturally rejects it as 0 != os.Getpid().
func parseLsofOutput(out []byte, selfPid int) string {
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
