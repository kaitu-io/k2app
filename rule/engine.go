package rule

import (
	"log/slog"
	"net/netip"
	"strings"
	"sync/atomic"

	"github.com/kaitu-io/k2-rules/krs"
	"github.com/sasha-s/go-deadlock"
)

// RouteEntry represents one route in the routing table.
type RouteEntry struct {
	Target Target
	Sets   []krs.Matcher // resolved by name from loaded bundles (heap *NamedSet or mmap *diskSet); inline config rules are compiled via krs.BuildSet and appended here too

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

	// SrcCIDRs: compiled from MatchConfig.SrcIPCIDR. Connection source
	// must fall in one of these prefixes (AND with other groups).
	SrcCIDRs []netip.Prefix

	// PortRanges: compiled from MatchConfig.Port. Destination port must fall
	// in one of these ranges (OR within the list, AND with other groups).
	PortRanges []PortRange
}

// hasHostCriteria reports whether this route has domain/IP matching criteria.
func (r *RouteEntry) hasHostCriteria() bool {
	if len(r.Sets) > 0 {
		return true
	}
	return r.IPIsPrivate
}

// hasMetaCriteria reports whether this route has connection metadata criteria.
func (r *RouteEntry) hasMetaCriteria() bool {
	return len(r.ProcessNames) > 0 || len(r.PackageNames) > 0 ||
		len(r.ProcessNamePrefixes) > 0 || len(r.PackageNamePrefixes) > 0 ||
		len(r.InstallerPackages) > 0 ||
		r.NetworkMatch != "" || len(r.SrcCIDRs) > 0 || len(r.PortRanges) > 0
}

// Engine is the multi-ruleset matching engine.
// Thread-safe: Match/MatchConn use RLock, SetTmpRule uses WLock.
// Lock ordering: Engine.mu is standalone — never nested inside any other lock.
type Engine struct {
	mu       deadlock.RWMutex
	tmp      map[netip.Addr]Target
	routes   []RouteEntry
	fallback Target
	// disableSniff: the bulk of traffic is proxied (a match:all proxy catch-all),
	// so BT/SNI sniffing is turned off. ⚠️ This is NOT "everything proxies, no
	// exceptions" — a direct/reject exception route can precede the catch-all and
	// still leave this true. To test for the presence of exception routes, use
	// len(routes), never this flag. See DisableSniff.
	disableSniff bool
	bundles      []*krs.Bundle // heap lifecycle: GC'd on Close (no explicit free)

	// closers releases non-GC resources held by the current rule set —
	// specifically mmap unmaps for the disk-backed reader (krs.DiskBundle.Close).
	// Run by Close and swapped+run by reload, always OUTSIDE the state lock
	// (constitution rule 4). nil for the heap path.
	closers []func() error

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

// NewEngine creates a routing engine. disableSniff turns off BT/SNI sniffing for
// bulk-proxied configs (see the Engine.disableSniff field doc) — it does not gate
// DNS-pin learning, which keys off route presence (see SetTmpRule).
func NewEngine(routes []RouteEntry, fallback Target, bundles []*krs.Bundle, disableSniff bool) *Engine {
	return &Engine{
		tmp:          make(map[netip.Addr]Target),
		routes:       routes,
		fallback:     fallback,
		bundles:      bundles,
		disableSniff: disableSniff,
	}
}

// NewEngineNamed creates an engine from NamedBundles. Thin wrapper over
// NewEngine that flattens []NamedBundle to the []*krs.Bundle lifecycle slice.
func NewEngineNamed(routes []RouteEntry, fallback Target, named []NamedBundle, disableSniff bool) *Engine {
	bundles := make([]*krs.Bundle, 0, len(named))
	for _, nb := range named {
		if nb.Bundle == nil {
			continue
		}
		bundles = append(bundles, nb.Bundle)
	}
	return NewEngine(routes, fallback, bundles, disableSniff)
}

// NewEngineWithClosers creates an engine whose rule sets are backed by
// resources requiring explicit release (mmap unmaps). closers are run by Close
// / reload outside the state lock. Used by the disk-backed (NE) loader; the
// matchers themselves live in routes[].Sets as krs.Matcher (mmap *diskSet).
func NewEngineWithClosers(routes []RouteEntry, fallback Target, closers []func() error, disableSniff bool) *Engine {
	e := NewEngine(routes, fallback, nil, disableSniff)
	e.closers = closers
	return e
}

// runClosers invokes each non-nil closer, ignoring errors (best-effort munmap).
// MUST be called outside the engine state lock (constitution rule 4).
func runClosers(cs []func() error) {
	for _, c := range cs {
		if c != nil {
			_ = c()
		}
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
	// Hoist: normalize (IDNA + lowercase + reverse) once per lookup, not once
	// per set (constitution rule 5). Both heap *NamedSet and mmap *diskSet match
	// on these pre-reversed parents.
	parents := krs.ReversedParents(host)
	for i := range e.routes {
		r := &e.routes[i]
		if !r.hasHostCriteria() {
			continue
		}
		if matchRouteDomain(r, parents) {
			return r.Target
		}
	}
	return e.fallback
}

// matchConnLocked matches host + meta, AND semantics within each route.
//
// Meta-only rules (process_name / package_name / network without host criteria)
// are checked BEFORE the DNS-learned tmp cache, because per-connection identity
// supersedes IP-level caching: a process bypass must fire regardless of which
// route the destination's domain happened to resolve to via DNS.
func (e *Engine) matchConnLocked(host string, meta ConnMeta) Target {
	hasAnyMeta := meta.ProcessName != "" || meta.PackageName != "" || meta.Network != ""
	if hasAnyMeta {
		for i := range e.routes {
			r := &e.routes[i]
			if r.hasHostCriteria() || !r.hasMetaCriteria() {
				continue // need a meta-only route here
			}
			if matchMeta(r, meta) {
				return r.Target
			}
		}
	}

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
	parents := krs.ReversedParents(host)
	for i := range e.routes {
		r := &e.routes[i]
		hasHost := r.hasHostCriteria()
		hasMeta := r.hasMetaCriteria()
		if !hasHost && !hasMeta {
			continue
		}
		hostOK := !hasHost || matchRouteDomain(r, parents)
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
	return false
}

// matchRouteDomain checks if a domain matches any host criteria in the route.
// parents are the pre-reversed parent suffixes from krs.ReversedParents,
// computed once by the caller (constitution rule 5).
func matchRouteDomain(r *RouteEntry, parents []string) bool {
	for _, s := range r.Sets {
		if s.MatchDomainReversed(parents) {
			return true
		}
	}
	return false
}

// commTruncLen is the kernel-imposed cap on what process searchers can
// observe via /proc/PID/comm (Linux TASK_COMM_LEN) or lsof COMMAND (macOS
// MAXCOMLEN) — 15 visible characters before the trailing nul. UI-supplied
// rule names come from full executable basenames (e.g. "Google Chrome
// Helper"), so a literal == match would silently miss every rule whose
// process is >15 chars. Windows is unaffected (full image name).
const commTruncLen = 15

// processNameMatches reports whether the observed process name (as seen by
// the daemon-side ProcessSearcher) matches a rule entry. Accepts both:
//   - exact equality (Windows full name, or short Linux/macOS names)
//   - rule-prefix match when the observed value is exactly the 15-char
//     kernel truncation of the rule (Linux /proc/PID/comm, macOS lsof)
func processNameMatches(rule, observed string) bool {
	if rule == observed {
		return true
	}
	if len(rule) > commTruncLen && len(observed) == commTruncLen && rule[:commTruncLen] == observed {
		return true
	}
	return false
}

// matchMeta checks connection metadata against route criteria.
//
// Semantics:
//   - Process group   = ProcessNames ∪ ProcessNamePrefixes (any hit)
//   - Package group   = PackageNames ∪ PackageNamePrefixes ∪ InstallerPackages (any hit)
//   - Network         = exact match if set
//
// Groups are AND'd: a route with both process and package criteria must satisfy both.
//
// Empty observed field where the route has criteria for that group = no match.
func matchMeta(r *RouteEntry, meta ConnMeta) bool {
	if len(r.SrcCIDRs) > 0 {
		if !meta.SrcIP.IsValid() {
			return false
		}
		src := meta.SrcIP.Unmap()
		hit := false
		for _, p := range r.SrcCIDRs {
			if p.Contains(src) {
				hit = true
				break
			}
		}
		if !hit {
			return false
		}
	}

	hasProcess := len(r.ProcessNames) > 0 || len(r.ProcessNamePrefixes) > 0
	if hasProcess {
		if meta.ProcessName == "" {
			return false
		}
		hit := false
		for _, p := range r.ProcessNames {
			if processNameMatches(p, meta.ProcessName) {
				hit = true
				break
			}
		}
		if !hit && len(r.ProcessNamePrefixes) > 0 {
			lower := strings.ToLower(meta.ProcessName)
			for _, p := range r.ProcessNamePrefixes {
				// Entries pre-lowercased at compile time.
				if strings.HasPrefix(lower, p) {
					hit = true
					break
				}
			}
		}
		if !hit {
			return false
		}
	}

	hasPackage := len(r.PackageNames) > 0 || len(r.PackageNamePrefixes) > 0 || len(r.InstallerPackages) > 0
	if hasPackage {
		hit := false
		if meta.PackageName != "" {
			for _, p := range r.PackageNames {
				if meta.PackageName == p {
					hit = true
					break
				}
			}
			if !hit {
				for _, p := range r.PackageNamePrefixes {
					if strings.HasPrefix(meta.PackageName, p) {
						hit = true
						break
					}
				}
			}
		}
		if !hit && meta.InstallerPackage != "" {
			for _, p := range r.InstallerPackages {
				if meta.InstallerPackage == p {
					hit = true
					break
				}
			}
		}
		if !hit {
			return false
		}
	}

	if len(r.PortRanges) > 0 {
		hit := false
		for _, pr := range r.PortRanges {
			if pr.Contains(meta.Port) {
				hit = true
				break
			}
		}
		if !hit {
			return false
		}
	}

	if r.NetworkMatch != "" && meta.Network != r.NetworkMatch {
		return false
	}
	return true
}

// SetTmpRule adds a DNS-learned temporary routing rule.
//
// Threshold-gated DIAG: emits `DIAG: rule-tmp-pin` only on first pin of an
// IP, or when target changes for an already-pinned IP. Duplicate calls
// (same ip + same target) are silent — DNS pipeline re-pins on every cache
// hit and we'd flood the log otherwise.
func (e *Engine) SetTmpRule(ip string, target Target) {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return
	}
	e.mu.Lock()
	if e.tmp == nil {
		e.mu.Unlock()
		return
	}
	// No exception routes → every host takes the fallback, so a per-IP pin can
	// never change a routing decision. Skip it purely to bound the loaded-engine
	// dirty heap (constitution). When exception routes DO exist (e.g. a
	// control-plane domain→direct route prepended to a global config), DNS-learned
	// pins are the ONLY bridge from the DNS-time domain decision
	// (`Match(domain)`→direct) to the IP-routed connection (`Match(ip)`), so they
	// must be kept — even when the fallback is a proxy. Gated on route presence,
	// NOT the sniff flag: those answer different questions (see DisableSniff).
	if len(e.routes) == 0 {
		e.mu.Unlock()
		return
	}
	prev, existed := e.tmp[addr]
	evicted := 0
	if !existed && len(e.tmp) >= tmpRuleCap {
		// Bounded cache: clear and let DNS re-pin live IPs on their next lookup.
		// Cheaper and constitution-safe vs an LRU; the pin cache is an
		// optimization, not a source of truth (routes still match correctly).
		evicted = len(e.tmp)
		e.tmp = make(map[netip.Addr]Target)
		prev, existed = 0, false
	}
	e.tmp[addr] = target
	tmpSize := len(e.tmp)
	e.mu.Unlock()

	if evicted > 0 {
		slog.Info("DIAG: rule-tmp-evict", "evicted", evicted, "cap", tmpRuleCap)
	}
	switch {
	case !existed:
		slog.Info("DIAG: rule-tmp-pin",
			"ip", ip, "target", uint8(target), "action", "add", "tmpSize", tmpSize)
	case prev != target:
		slog.Info("DIAG: rule-tmp-pin",
			"ip", ip, "target", uint8(target), "action", "change",
			"prevTarget", uint8(prev), "tmpSize", tmpSize)
	}
}

// tmpRuleCap bounds the DNS-learned pin cache so a long-lived tunnel under heavy
// DNS churn cannot grow e.tmp without limit (iOS NE jetsam budget).
const tmpRuleCap = 4096

// tmpLen returns the number of DNS-learned pins. Test/diagnostic accessor.
func (e *Engine) tmpLen() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return len(e.tmp)
}

// DisableSniff reports whether BT/SNI sniffing should be off because the bulk of
// traffic is proxied. Sniffing only refines routing (BT→direct, SNI direct→proxy
// upgrade); it is wasted hot-path work when most traffic tunnels, and enabling it
// in global mode would force BitTorrent direct (tunnel leak).
//
// ⚠️ This does NOT mean "no exception routes exist." A direct/reject exception
// (e.g. a control-plane domain→direct route) can precede the proxy catch-all and
// still leave this true. To test for exceptions, inspect the route entries — never
// this flag. Conflating the two is what silently dropped DNS-learned pins for
// exception routes (see SetTmpRule).
func (e *Engine) DisableSniff() bool {
	return e.disableSniff
}

// HasMetaCriteria reports whether any loaded route matches on connection
// metadata (process_name / package_name / network). The tunnel uses this to
// short-circuit per-connection ProcessSearcher invocation when no rule needs
// it — without any bypass list, calling lsof/equivalent per connection is
// pure waste.
func (e *Engine) HasMetaCriteria() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	for i := range e.routes {
		if e.routes[i].hasMetaCriteria() {
			return true
		}
	}
	return false
}

// RouteCount returns the number of configured route entries. Used by tests
// and diagnostics — zero indicates a misconfigured or failed rule build.
func (e *Engine) RouteCount() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return len(e.routes)
}

// Close releases all bundle resources, including mmap unmaps (closers).
// Closers run outside the state lock (constitution rule 4).
func (e *Engine) Close() {
	e.mu.Lock()
	cs := e.closers
	e.closers = nil
	e.bundles = nil
	e.routes = nil
	e.tmp = make(map[netip.Addr]Target)
	e.mu.Unlock()
	runClosers(cs)
}

// Reload atomically replaces the engine's routes and bundles.
// TmpRules are cleared — DNS queries will re-learn them against the new rules.
// Thread-safe: callers of Match/MatchConn block briefly during swap.
//
// Concurrency: builds new data outside lock, swaps under WLock, closes old
// bundles outside lock (Rule #4: no Close under state lock).
func (e *Engine) Reload(routes []RouteEntry, bundles []*krs.Bundle) error {
	return e.reload(routes, bundles, nil)
}

// ReloadWithClosers is the disk-backed variant of Reload: it swaps in new mmap
// matchers (held in routes[].Sets) plus their unmap closers, and munmaps the
// previous set's mappings outside the state lock. Used by the NE hot-reload.
func (e *Engine) ReloadWithClosers(routes []RouteEntry, closers []func() error) error {
	return e.reload(routes, nil, closers)
}

func (e *Engine) reload(routes []RouteEntry, bundles []*krs.Bundle, closers []func() error) error {
	e.mu.Lock()
	cleared := len(e.tmp)
	oldBundles := e.bundles
	oldClosers := e.closers
	e.bundles = bundles
	e.closers = closers
	e.routes = routes
	e.tmp = make(map[netip.Addr]Target)
	e.mu.Unlock()

	slog.Info("DIAG: rule-tmp-reload",
		"cleared", cleared, "routes", len(routes), "bundles", len(bundles))

	_ = oldBundles                // heap Bundle has no explicit Close — GC'd.
	runClosers(oldClosers)        // disk: munmap previous mappings (outside lock).
	return nil
}

// ReloadNamed is the NamedBundle variant of Reload: it flattens
// []NamedBundle to the []*krs.Bundle lifecycle slice and delegates the
// atomic swap to Reload. Kept (over calling Reload directly) so the
// background updater can keep passing the NamedBundle shape it builds.
func (e *Engine) ReloadNamed(routes []RouteEntry, named []NamedBundle) error {
	bundles := make([]*krs.Bundle, 0, len(named))
	for _, nb := range named {
		if nb.Bundle == nil {
			continue
		}
		bundles = append(bundles, nb.Bundle)
	}
	return e.Reload(routes, bundles)
}
