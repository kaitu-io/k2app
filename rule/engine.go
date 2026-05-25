package rule

import (
	"net/netip"
	"strings"
	"sync/atomic"

	"github.com/sasha-s/go-deadlock"
)

// RouteEntry represents one route in the routing table.
type RouteEntry struct {
	Target Target
	Sets   []*BundleSet // from k2b files

	// Inline matchers (compiled from config domain_suffix/ip_cidr).
	Inline *InlineSet

	// Connection metadata matchers.
	ProcessNames []string // match any (OR within list)
	PackageNames []string // match any (OR within list)

	// v2 (app-bypass) — prefix + installer.
	// ProcessNamePrefixes entries are PRE-LOWERCASED at compile time so the
	// per-flow matcher can do a single strings.ToLower on the observed name.
	// PackageNamePrefixes are case-preserved (Android package names are
	// case-sensitive identifiers per Android convention).
	ProcessNamePrefixes []string
	PackageNamePrefixes []string
	InstallerPackages   []string

	NetworkMatch string // "tcp" or "udp" (empty = any)
	IPIsPrivate  bool   // match private/loopback IPs
}

// hasHostCriteria reports whether this route has domain/IP matching criteria.
func (r *RouteEntry) hasHostCriteria() bool {
	if len(r.Sets) > 0 {
		return true
	}
	if r.Inline != nil && !r.Inline.IsEmpty() {
		return true
	}
	return r.IPIsPrivate
}

// hasMetaCriteria reports whether this route has connection metadata criteria.
func (r *RouteEntry) hasMetaCriteria() bool {
	return len(r.ProcessNames) > 0 || len(r.PackageNames) > 0 ||
		len(r.ProcessNamePrefixes) > 0 || len(r.PackageNamePrefixes) > 0 ||
		len(r.InstallerPackages) > 0 ||
		r.NetworkMatch != ""
}

// Engine is the multi-ruleset matching engine.
// Thread-safe: Match/MatchConn use RLock, SetTmpRule uses WLock.
// Lock ordering: Engine.mu is standalone — never nested inside any other lock.
type Engine struct {
	mu       deadlock.RWMutex
	tmp      map[netip.Addr]Target
	routes   []RouteEntry
	fallback Target
	allProxy bool      // match:all mode (disables sniffing)
	bundles  []*Bundle // lifecycle: Close releases all

	// missReporter is an optional hook invoked when a domain lookup
	// reaches the fallback target (no route matched). Phase 1 rule-miss
	// telemetry — nil by default, zero overhead when unset.
	//
	// Stored as an atomic.Pointer so the HOT PATH can do a single
	// atomic load outside the RLock critical section. When nil (the
	// Phase 1 default) the match path short-circuits before any
	// fallback bookkeeping — the compiler inlines matchHostLocked's
	// single-return form and the overhead is a single nil-check.
	missReporter atomic.Pointer[reporterSlot]
}

// reporterSlot is a wrapper so atomic.Pointer can hold an interface
// value (atomic.Pointer[MissReporter] would lose identity on nil).
type reporterSlot struct{ r MissReporter }

// NewEngine creates a routing engine.
func NewEngine(routes []RouteEntry, fallback Target, bundles []*Bundle, allProxy bool) *Engine {
	return &Engine{
		tmp:      make(map[netip.Addr]Target),
		routes:   routes,
		fallback: fallback,
		bundles:  bundles,
		allProxy: allProxy,
	}
}

// Match returns the routing target for a host (IP or domain).
// Used at DNS time — skips routes that have ONLY meta criteria (no host criteria).
// Priority: TmpRule > route entries (first-match-wins) > fallback.
//
// Hot path: when no MissReporter is installed (Phase 1 default) the only
// added cost vs the un-instrumented version is a single atomic.Pointer
// Load + nil check — performed OUTSIDE the RLock critical section.
func (e *Engine) Match(host string) Target {
	e.mu.RLock()
	t := e.matchHostLocked(host)
	e.mu.RUnlock()
	// Check hook outside the lock — atomic.Pointer.Load is lock-free
	// and cheap. When nil (the Phase 1 default) this returns immediately.
	slot := e.missReporter.Load()
	if slot == nil {
		return t
	}
	// Slow path: reporter installed. Detect the fallback case by checking
	// target equality. Safe: fallback was captured under the lock above
	// (e.fallback is set once at construction and doesn't change).
	if t == e.fallback && isFQDN(host) {
		slot.r.Report(host, "")
	}
	return t
}

// MatchConn returns the routing target for a connection with full metadata.
// Used at connection time — checks both host and meta criteria (AND semantics).
// Priority: TmpRule > route entries (first-match-wins) > fallback.
func (e *Engine) MatchConn(host string, meta ConnMeta) Target {
	e.mu.RLock()
	t := e.matchConnLocked(host, meta)
	e.mu.RUnlock()
	slot := e.missReporter.Load()
	if slot == nil {
		return t
	}
	if t == e.fallback && isFQDN(host) {
		slot.r.Report(host, meta.Network)
	}
	return t
}

// SetMissReporter installs a rule-miss telemetry sink. Pass nil to clear.
// Thread-safe: uses an atomic.Pointer swap so the hot path stays lock-free.
// Callers should set this immediately after NewEngine, before the engine
// is used on the hot path.
func (e *Engine) SetMissReporter(r MissReporter) {
	if r == nil {
		e.missReporter.Store(nil)
		return
	}
	e.missReporter.Store(&reporterSlot{r: r})
}

// isFQDN reports whether host looks like a public fully-qualified domain
// name worth reporting. Returns false for IP literals, bare hostnames
// (no dot), localhost, and private TLDs like .local/.internal/.lan/.home.
func isFQDN(host string) bool {
	if host == "" {
		return false
	}
	// Reject IP literals.
	if _, err := netip.ParseAddr(host); err == nil {
		return false
	}
	// Must contain at least one dot (rules out "localhost", bare hosts).
	idx := strings.LastIndexByte(host, '.')
	if idx <= 0 || idx == len(host)-1 {
		return false
	}
	// Reject trailing-dot FQDNs like "example.com." — strip once then re-check.
	tld := strings.ToLower(host[idx+1:])
	switch tld {
	case "local", "internal", "lan", "home", "localhost", "intranet", "private", "corp":
		return false
	}
	return true
}

// matchHostLocked matches host only, skipping meta-only routes.
func (e *Engine) matchHostLocked(host string) Target {
	if addr, err := netip.ParseAddr(host); err == nil {
		if t, ok := e.tmp[addr]; ok {
			return t
		}
		for i := range e.routes {
			r := &e.routes[i]
			if !r.hasHostCriteria() {
				continue
			}
			if matchRouteIP(r, addr) {
				return r.Target
			}
		}
		return e.fallback
	}
	domain := strings.ToLower(host)
	for i := range e.routes {
		r := &e.routes[i]
		if !r.hasHostCriteria() {
			continue
		}
		if matchRouteDomain(r, domain) {
			return r.Target
		}
	}
	return e.fallback
}

// matchConnLocked matches host + meta, AND semantics within each route.
func (e *Engine) matchConnLocked(host string, meta ConnMeta) Target {
	if addr, err := netip.ParseAddr(host); err == nil {
		if t, ok := e.tmp[addr]; ok {
			return t
		}
		for i := range e.routes {
			r := &e.routes[i]
			hasHost := r.hasHostCriteria()
			hasMeta := r.hasMetaCriteria()
			if !hasHost && !hasMeta {
				continue
			}
			hostOK := !hasHost || matchRouteIP(r, addr)
			metaOK := !hasMeta || matchMeta(r, meta)
			if hostOK && metaOK {
				return r.Target
			}
		}
		return e.fallback
	}
	domain := strings.ToLower(host)
	for i := range e.routes {
		r := &e.routes[i]
		hasHost := r.hasHostCriteria()
		hasMeta := r.hasMetaCriteria()
		if !hasHost && !hasMeta {
			continue
		}
		hostOK := !hasHost || matchRouteDomain(r, domain)
		metaOK := !hasMeta || matchMeta(r, meta)
		if hostOK && metaOK {
			return r.Target
		}
	}
	return e.fallback
}

// matchRouteIP checks if an IP matches any host criteria in the route.
func matchRouteIP(r *RouteEntry, addr netip.Addr) bool {
	if r.IPIsPrivate && (addr.IsPrivate() || addr.IsLoopback() || addr.IsLinkLocalUnicast()) {
		return true
	}
	for _, s := range r.Sets {
		if s.MatchIP(addr) {
			return true
		}
	}
	if r.Inline != nil && r.Inline.MatchIP(addr) {
		return true
	}
	return false
}

// matchRouteDomain checks if a domain matches any host criteria in the route.
func matchRouteDomain(r *RouteEntry, domain string) bool {
	for _, s := range r.Sets {
		if s.MatchDomain(domain) {
			return true
		}
	}
	if r.Inline != nil && r.Inline.MatchDomain(domain) {
		return true
	}
	return false
}

// matchMeta checks connection metadata against route criteria.
// All criteria within a route AND together. Empty meta field = no match.
func matchMeta(r *RouteEntry, meta ConnMeta) bool {
	if len(r.ProcessNames) > 0 {
		if meta.ProcessName == "" {
			return false
		}
		found := false
		for _, p := range r.ProcessNames {
			if meta.ProcessName == p {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	if len(r.PackageNames) > 0 {
		if meta.PackageName == "" {
			return false
		}
		found := false
		for _, p := range r.PackageNames {
			if meta.PackageName == p {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	if r.NetworkMatch != "" && meta.Network != r.NetworkMatch {
		return false
	}
	return true
}

// SetTmpRule adds a DNS-learned temporary routing rule.
func (e *Engine) SetTmpRule(ip string, target Target) {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return
	}
	e.mu.Lock()
	if e.tmp != nil {
		e.tmp[addr] = target
	}
	e.mu.Unlock()
}

// IsAllProxy returns true if engine is in match-all proxy mode.
func (e *Engine) IsAllProxy() bool {
	return e.allProxy
}

// RouteCount returns the number of configured route entries. Used by tests
// and diagnostics — zero indicates a misconfigured or failed rule build.
func (e *Engine) RouteCount() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return len(e.routes)
}

// Close releases all bundle resources.
func (e *Engine) Close() {
	e.mu.Lock()
	e.bundles = nil
	e.routes = nil
	e.tmp = make(map[netip.Addr]Target)
	e.mu.Unlock()
}

// Reload atomically replaces the engine's routes and bundles.
// TmpRules are cleared — DNS queries will re-learn them against the new rules.
// Thread-safe: callers of Match/MatchConn block briefly during swap.
//
// Concurrency: builds new data outside lock, swaps under WLock, closes old
// bundles outside lock (Rule #4: no Close under state lock).
func (e *Engine) Reload(routes []RouteEntry, bundles []*Bundle) error {
	e.mu.Lock()
	oldBundles := e.bundles
	e.bundles = bundles
	e.routes = routes
	e.tmp = make(map[netip.Addr]Target)
	e.mu.Unlock()

	_ = oldBundles // Bundle has no explicit Close — data is GC'd.
	return nil
}
