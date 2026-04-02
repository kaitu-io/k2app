package rule

import (
	"net/netip"
	"strings"

	"github.com/sasha-s/go-deadlock"
)

// RouteEntry represents one route in the routing table.
type RouteEntry struct {
	Target Target
	Sets   []*BundleSet
}

// Engine is the multi-ruleset matching engine.
// Thread-safe: Match uses RLock, SetTmpRule uses WLock.
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
// Priority: TmpRule > route entries (first-match-wins) > fallback.
func (e *Engine) Match(host string) Target {
	e.mu.RLock()
	t := e.matchLocked(host)
	e.mu.RUnlock()
	return t
}

func (e *Engine) matchLocked(host string) Target {
	if addr, err := netip.ParseAddr(host); err == nil {
		if t, ok := e.tmp[addr]; ok {
			return t
		}
		for i := range e.routes {
			for _, s := range e.routes[i].Sets {
				if s.MatchIP(addr) {
					return e.routes[i].Target
				}
			}
		}
		return e.fallback
	}
	domain := strings.ToLower(host)
	for i := range e.routes {
		for _, s := range e.routes[i].Sets {
			if s.MatchDomain(domain) {
				return e.routes[i].Target
			}
		}
	}
	return e.fallback
}

// SetTmpRule adds a DNS-learned temporary routing rule.
func (e *Engine) SetTmpRule(ip string, target Target) {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return
	}
	e.mu.Lock()
	e.tmp[addr] = target
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
	e.tmp = nil
	e.mu.Unlock()
}
