//go:build darwin && !ios

package provider

import (
	"context"
	"log/slog"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/kaitu-io/k2/safego"
	"github.com/sasha-s/go-deadlock"
	"golang.org/x/sync/singleflight"
)

const (
	// lsofTimeout caps the lsof subprocess that runs on the connection-setup
	// critical path. See buildConnIndex for the rationale.
	lsofTimeout = 2 * time.Second

	// connIndexTTLDefault is how long one lsof sweep stays authoritative.
	// Mirrors LinuxProcessSearcher's inodeIndexTTL.
	connIndexTTLDefault = 5 * time.Second

	// minRebuildIntervalDefault throttles miss-triggered sweeps.
	//
	// This is the difference between an index and a per-connection shell-out.
	// A brand-new connection is by definition absent from an index built
	// before it existed, so "rebuild on every miss" — which is what the Linux
	// searcher does, because reading /proc is cheap — would fire one lsof per
	// connection here and reproduce the exact bug this index removes. lsof on
	// macOS walks the whole process/fd table and was measured at 100-1226ms in
	// production, so it must be rate-limited, not demand-driven.
	//
	// Within the throttle window a miss returns "" (fail-open: the connection
	// takes the region/default route, i.e. the tunnel). That is the same
	// outcome the old code produced whenever lsof timed out, and it is the
	// safe direction: a missed attribution costs an App-Bypass direct route,
	// never a leak.
	minRebuildIntervalDefault = 250 * time.Millisecond
)

// DarwinProcessSearcher resolves process names for local sockets from a
// periodically-rebuilt snapshot of `lsof -i` output.
//
// Design note — why a snapshot instead of a per-connection query:
// core/tunnel.go's routeConn calls FindProcess exactly once per connection,
// and processCache is keyed by (network, srcIP, srcPort). Every new
// connection therefore carries a fresh source port, a fresh cache key, and a
// guaranteed cache miss — the cache could never absorb anything on the only
// path that matters. The old implementation answered each of those misses
// with its own `lsof -i <proto>@<ip>:<port>` subprocess. Under browser-level
// concurrency those serialize behind each other; a 9s production window was
// measured accumulating 9.8s of lookup latency.
//
// One sweep now serves every connection in the TTL window.
type DarwinProcessSearcher struct {
	cache *processCache

	indexMu      deadlock.RWMutex
	connIndex    map[string]string // cacheKey(network, srcIP, srcPort) → COMMAND
	indexExpires time.Time         // zero = never built
	lastRebuild  time.Time

	sf               singleflight.Group    // collapses concurrent sweeps
	buildConnIndexFn func() map[string]string // seam for tests

	connIndexTTL time.Duration
	// minRebuildInterval throttles miss-triggered sweeps.
	// Zero means "unset" (ensureInit fills the default); negative disables
	// throttling entirely and is used by tests that assert sweep counts.
	minRebuildInterval time.Duration
}

// NewDarwinProcessSearcher constructs a Darwin process searcher with the
// project-standard cache (2s TTL, 4096 soft cap) and index defaults.
func NewDarwinProcessSearcher() *DarwinProcessSearcher {
	s := &DarwinProcessSearcher{
		cache:              newProcessCache(processCacheCap, processCacheTTL),
		connIndexTTL:       connIndexTTLDefault,
		minRebuildInterval: minRebuildIntervalDefault,
	}
	s.buildConnIndexFn = s.buildConnIndex
	return s
}

// ensureInit backfills defaults for zero-value struct callers (e.g. a daemon
// factory that did `&DarwinProcessSearcher{}`). Fields already set — by
// NewDarwinProcessSearcher or by a test — are left alone.
func (s *DarwinProcessSearcher) ensureInit() {
	s.indexMu.Lock()
	if s.cache == nil {
		s.cache = newProcessCache(processCacheCap, processCacheTTL)
	}
	if s.buildConnIndexFn == nil {
		s.buildConnIndexFn = s.buildConnIndex
	}
	if s.connIndexTTL == 0 {
		s.connIndexTTL = connIndexTTLDefault
	}
	if s.minRebuildInterval == 0 {
		s.minRebuildInterval = minRebuildIntervalDefault
	}
	s.indexMu.Unlock()
}

// FindProcess returns (processName, "", "") — Darwin has no packageName /
// installerPackage; those are Android-only.
func (s *DarwinProcessSearcher) FindProcess(network string, srcIP netip.Addr, srcPort uint16) (string, string, string) {
	if !srcIP.IsValid() {
		return "", "", ""
	}
	// Canonicalize locally instead of trusting every caller to have done it
	// (core/tunnel.go routeConn does): the index is keyed on the unmapped
	// form, so both spellings of a v4 address must land on the same entry.
	srcIP = srcIP.Unmap()
	s.ensureInit()

	key := cacheKey(network, srcIP, srcPort)
	if name, pkg, installer, ok := s.cache.get(key); ok {
		return name, pkg, installer
	}

	start := time.Now()
	name := s.lookupIndexed(key)
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		// macOS is the only platform that shells out (lsof) for attribution,
		// so it's the most stall-prone — mirror the Linux/Windows slow-lookup
		// DIAG (k2/CLAUDE.md reserved event) so a slow or hung sweep is visible
		// in logs instead of presenting as an unexplained connection stall.
		slog.Info("DIAG: process-lookup-slow",
			"platform", "darwin", "network", network, "latencyMs", elapsed.Milliseconds())
	}
	// Negative results are cached too: a socket lsof cannot see (already
	// closed, or opened after the sweep) must not re-trigger a sweep on every
	// packet for the same 4-tuple.
	s.cache.set(key, name, "", "")
	return name, "", ""
}

// lookupIndexed answers from the snapshot, rebuilding when the snapshot is
// absent, stale, or does not contain the key — the last case subject to
// minRebuildInterval.
//
//   - Cold start (no index yet): synchronous sweep, then look up.
//   - Hit on a fresh index: return it.
//   - Miss on a fresh index: the socket is newer than the snapshot. Sweep
//     synchronously if the throttle allows, otherwise return "" (fail-open).
//   - Stale index: serve the current value and kick an async sweep.
func (s *DarwinProcessSearcher) lookupIndexed(key string) string {
	s.indexMu.RLock()
	built := !s.indexExpires.IsZero()
	cmd := s.connIndex[key]
	now := time.Now()
	expired := built && now.After(s.indexExpires)
	throttled := s.minRebuildInterval >= 0 && now.Sub(s.lastRebuild) < s.minRebuildInterval
	s.indexMu.RUnlock()

	if cmd != "" && !expired {
		return cmd
	}

	if !built || !throttled {
		// Cold start, or a miss/staleness we're allowed to resolve now.
		s.sf.Do("rebuild", s.doRebuild)
		s.indexMu.RLock()
		cmd = s.connIndex[key]
		s.indexMu.RUnlock()
		return cmd
	}

	if expired {
		safego.Go(func() { s.sf.Do("rebuild", s.doRebuild) })
	}
	return cmd
}

func (s *DarwinProcessSearcher) doRebuild() (any, error) {
	idx := s.buildConnIndexFn()
	now := time.Now()
	s.indexMu.Lock()
	s.connIndex = idx
	s.indexExpires = now.Add(s.connIndexTTL)
	s.lastRebuild = now
	s.indexMu.Unlock()
	return nil, nil
}

// buildConnIndex runs one `lsof -i` sweep and indexes every local socket by
// (network, ip, port).
//
// Hard timeout: lsof normally returns in tens of ms, but a stuck fd or a
// stalled process-table walk can hang it indefinitely. A sweep can run
// synchronously on the connection-setup path while the caller holds a
// conn-slot, so an unbounded hang would block that connection forever and,
// under systemic slowness, exhaust the conn-slot semaphore. Cap it; on
// timeout (or any error) return an empty index → those connections fall back
// to the region/default route.
func (s *DarwinProcessSearcher) buildConnIndex() map[string]string {
	ctx, cancel := context.WithTimeout(context.Background(), lsofTimeout)
	defer cancel()
	// -P/-n: no port- or host-name resolution (both would issue lookups).
	// -F pcPn: machine-readable pid / command / protocol / address.
	out, err := exec.CommandContext(ctx, "lsof", "-i", "-P", "-n", "-F", "pcPn").Output()
	if err != nil || len(out) == 0 {
		return map[string]string{}
	}
	return parseLsofIndex(out, os.Getpid())
}

// parseLsofIndex turns `lsof -i -F pcPn` output into a
// cacheKey(network, ip, port) → COMMAND map.
//
// Output shape: a "p<pid>" line opens a process record, "c<command>" names
// it, then each socket contributes "P<proto>" and "n<address>" lines. State
// carries forward across sockets within a record, which is why proto is not
// reset per socket — lsof emits P before each n.
//
// Quirks handled (same empirical set the per-connection parser handled):
//   - COMMAND is not TASK_COMM_LEN-truncated on macOS; lsof prefers pbi_name
//     and proc_pidpath basename. Spaces, parens and dotted bundle ids appear
//     verbatim ("Google Chrome Helper", "Code Helper (Renderer)"), so the
//     line after 'c' is taken as-is with no field splitting.
//   - Addresses arrive as "laddr->raddr" for connected sockets and bare
//     "laddr" for listeners. Only the local half is indexed.
//   - Wildcard listeners ("*:8080") and any address netip cannot parse are
//     skipped rather than indexed under a bogus key.
//   - selfPid is skipped: the daemon holds its own proxied sockets, and
//     attributing an app's traffic to the daemon would defeat App Bypass.
//   - First writer wins. With a loopback connection both endpoints are local
//     and share one (proto, ip, port); the earlier record is kept for
//     determinism.
func parseLsofIndex(out []byte, selfPid int) map[string]string {
	idx := make(map[string]string, 512)
	var pid int
	var cmd, proto string

	for _, line := range strings.Split(string(out), "\n") {
		if line == "" {
			continue
		}
		switch line[0] {
		case 'p':
			v, err := strconv.Atoi(line[1:])
			if err != nil {
				pid, cmd, proto = 0, "", ""
				continue
			}
			pid, cmd, proto = v, "", ""
		case 'c':
			cmd = line[1:]
		case 'P':
			proto = strings.ToLower(line[1:])
		case 'n':
			if pid == 0 || pid == selfPid || cmd == "" || proto == "" {
				continue
			}
			key, ok := lsofAddrToKey(proto, line[1:])
			if !ok {
				continue
			}
			if _, exists := idx[key]; !exists {
				idx[key] = cmd
			}
		}
	}
	return idx
}

// lsofAddrToKey converts one lsof address field into a processCache key.
// Returns ok=false for anything that is not a concrete local ip:port.
func lsofAddrToKey(proto, addr string) (string, bool) {
	if proto != "tcp" && proto != "udp" {
		return "", false
	}
	local := addr
	if i := strings.Index(addr, "->"); i >= 0 {
		local = addr[:i]
	}
	host, portStr, err := net.SplitHostPort(local)
	if err != nil {
		return "", false
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		// "*" wildcard listeners and malformed hosts land here.
		return "", false
	}
	port, err := strconv.ParseUint(portStr, 10, 16)
	if err != nil {
		return "", false
	}
	return cacheKey(proto, ip.Unmap(), uint16(port)), true
}
