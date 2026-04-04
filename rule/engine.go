package rule

import (
	"net/netip"
	"strings"

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
	NetworkMatch string   // "tcp" or "udp" (empty = any)
	IPIsPrivate  bool     // match private/loopback IPs
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
	return len(r.ProcessNames) > 0 || len(r.PackageNames) > 0 || r.NetworkMatch != ""
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
}

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
func (e *Engine) Match(host string) Target {
	e.mu.RLock()
	t := e.matchHostLocked(host)
	e.mu.RUnlock()
	return t
}

// MatchConn returns the routing target for a connection with full metadata.
// Used at connection time — checks both host and meta criteria (AND semantics).
// Priority: TmpRule > route entries (first-match-wins) > fallback.
func (e *Engine) MatchConn(host string, meta ConnMeta) Target {
	e.mu.RLock()
	t := e.matchConnLocked(host, meta)
	e.mu.RUnlock()
	return t
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
