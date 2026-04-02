# Rule Engine v2 Enhancements — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extend rule/ package with inline rules (user-defined domains/IPs in config), process name matching, Android package name matching, ip_is_private, and network (tcp/udp) matching.

**Architecture:** Add `ConnMeta` type carrying connection metadata (process, package, network, port). Add `MatchConn(host, meta)` to Engine for connection-time routing. Inline rules are compiled from config strings into in-memory matchers at engine build time. `ConnectionHandler` interface is NOT changed — the tunnel extracts source addr from conn and looks up process info internally via an injected `ProcessSearcher`.

**Tech Stack:** Go 1.25, deadlock.RWMutex, sing-tun (gVisor stack)

---

## File Structure

| File | Responsibility | Action |
|------|---------------|--------|
| `rule/conn.go` | ConnMeta type | Create |
| `rule/conn_test.go` | ConnMeta tests | Create |
| `rule/target.go` | MatchConfig extended fields | Modify |
| `rule/target_test.go` | MatchConfig tests | Modify |
| `rule/inline.go` | Compile config strings → in-memory matchers | Create |
| `rule/inline_test.go` | InlineSet tests | Create |
| `rule/engine.go` | MatchConn method, RouteEntry extension | Modify |
| `rule/engine_test.go` | MatchConn + inline + meta tests | Modify |
| `core/router.go` | RouteConn method | Modify |
| `core/tunnel.go` | Build ConnMeta, call RouteConn | Modify |
| `engine/engine.go` | buildRuleEngine compiles inline rules | Modify |
| `engine/engine_test.go` | buildRuleEngine inline test | Modify |
| `provider/process.go` | ProcessSearcher interface + nop impl | Create |
| `core/tunnel.go` | Accept ProcessSearcher in config | Modify |
| `CLAUDE.md` | Update MatchConfig docs | Modify |

---

### Task 1: ConnMeta type

**Files:**
- Create: `rule/conn.go`
- Create: `rule/conn_test.go`

- [ ] **Step 1: Write the test**

```go
// rule/conn_test.go
package rule

import "testing"

func TestConnMeta_Zero(t *testing.T) {
	var m ConnMeta
	if m.ProcessName != "" {
		t.Error("zero ConnMeta should have empty ProcessName")
	}
	if m.Network != "" {
		t.Error("zero ConnMeta should have empty Network")
	}
}
```

- [ ] **Step 2: Run test — expect FAIL (ConnMeta not defined)**

Run: `go test -run TestConnMeta_Zero -count=1 ./rule/`
Expected: FAIL — `ConnMeta` undefined

- [ ] **Step 3: Write implementation**

```go
// rule/conn.go
package rule

// ConnMeta carries connection metadata for rule matching.
// Populated by the tunnel from connection source addr + process lookup.
// Zero value = no metadata available (all criteria pass).
type ConnMeta struct {
	ProcessName string // OS process name (empty = unknown)
	PackageName string // Android package name (empty = not Android or unknown)
	Network     string // "tcp" or "udp" (empty = unknown)
	Port        uint16 // destination port (0 = unknown)
}
```

- [ ] **Step 4: Run test — expect PASS**

Run: `go test -run TestConnMeta_Zero -count=1 ./rule/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add rule/conn.go rule/conn_test.go
git commit -m "feat(rule): add ConnMeta type for connection metadata matching"
```

---

### Task 2: Extend MatchConfig with inline + meta fields

**Files:**
- Modify: `rule/target.go`
- Modify: `rule/target_test.go`

- [ ] **Step 1: Write tests for new fields**

Add to `rule/target_test.go`:

```go
func TestMatchConfig_HasCriteria_InlineFields(t *testing.T) {
	// domain_suffix alone
	m := MatchConfig{DomainSuffix: []string{"google.com"}}
	if !m.HasCriteria() {
		t.Error("DomainSuffix should count as criteria")
	}

	// ip_cidr alone
	m = MatchConfig{IPCIDR: []string{"8.8.8.0/24"}}
	if !m.HasCriteria() {
		t.Error("IPCIDR should count as criteria")
	}

	// process_name alone
	m = MatchConfig{ProcessName: []string{"Chrome"}}
	if !m.HasCriteria() {
		t.Error("ProcessName should count as criteria")
	}

	// package_name alone
	m = MatchConfig{PackageName: []string{"com.android.chrome"}}
	if !m.HasCriteria() {
		t.Error("PackageName should count as criteria")
	}

	// network alone
	m = MatchConfig{Network: "tcp"}
	if !m.HasCriteria() {
		t.Error("Network should count as criteria")
	}

	// ip_is_private alone
	m = MatchConfig{IPIsPrivate: true}
	if !m.HasCriteria() {
		t.Error("IPIsPrivate should count as criteria")
	}

	// empty still false
	m = MatchConfig{}
	if m.HasCriteria() {
		t.Error("empty MatchConfig should not have criteria")
	}
}

func TestMatchConfig_HasHostCriteria(t *testing.T) {
	// preset = host criteria
	m := MatchConfig{Preset: "overseas"}
	if !m.HasHostCriteria() {
		t.Error("Preset should be host criteria")
	}

	// domain_suffix = host criteria
	m = MatchConfig{DomainSuffix: []string{"google.com"}}
	if !m.HasHostCriteria() {
		t.Error("DomainSuffix should be host criteria")
	}

	// ip_cidr = host criteria
	m = MatchConfig{IPCIDR: []string{"8.8.8.0/24"}}
	if !m.HasHostCriteria() {
		t.Error("IPCIDR should be host criteria")
	}

	// ip_is_private = host criteria
	m = MatchConfig{IPIsPrivate: true}
	if !m.HasHostCriteria() {
		t.Error("IPIsPrivate should be host criteria")
	}

	// process_name only = NOT host criteria
	m = MatchConfig{ProcessName: []string{"ssh"}}
	if m.HasHostCriteria() {
		t.Error("ProcessName alone should not be host criteria")
	}

	// network only = NOT host criteria
	m = MatchConfig{Network: "udp"}
	if m.HasHostCriteria() {
		t.Error("Network alone should not be host criteria")
	}
}
```

- [ ] **Step 2: Run tests — expect FAIL**

Run: `go test -run TestMatchConfig_Has -count=1 ./rule/`
Expected: FAIL — new fields and methods not defined

- [ ] **Step 3: Update MatchConfig in target.go**

Replace `MatchConfig` and `HasCriteria` in `rule/target.go`:

```go
type MatchConfig struct {
	// Bundle-based (from k2b files).
	Preset  string   `yaml:"preset,omitempty"  json:"preset,omitempty"`
	Names   []string `yaml:"names,omitempty"   json:"names,omitempty"`
	Exclude []string `yaml:"exclude,omitempty" json:"exclude,omitempty"`

	// Inline host matching (compiled at engine build time).
	DomainSuffix []string `yaml:"domain_suffix,omitempty" json:"domain_suffix,omitempty"`
	IPCIDR       []string `yaml:"ip_cidr,omitempty"       json:"ip_cidr,omitempty"`

	// Connection metadata matching.
	ProcessName []string `yaml:"process_name,omitempty" json:"process_name,omitempty"`
	PackageName []string `yaml:"package_name,omitempty" json:"package_name,omitempty"`
	Network     string   `yaml:"network,omitempty"      json:"network,omitempty"` // "tcp" or "udp"
	IPIsPrivate bool     `yaml:"ip_is_private,omitempty" json:"ip_is_private,omitempty"`

	// Catch-all.
	All bool `yaml:"all,omitempty" json:"all,omitempty"`
}

// HasCriteria reports whether any matching criterion is set.
func (m MatchConfig) HasCriteria() bool {
	return m.Preset != "" || len(m.Names) > 0 || len(m.Exclude) > 0 || m.All ||
		len(m.DomainSuffix) > 0 || len(m.IPCIDR) > 0 ||
		len(m.ProcessName) > 0 || len(m.PackageName) > 0 ||
		m.Network != "" || m.IPIsPrivate
}

// HasHostCriteria reports whether any host-based (domain/IP) criterion is set.
// Used by DNS-time matching: routes with only meta criteria (process, network)
// should not affect DNS routing decisions.
func (m MatchConfig) HasHostCriteria() bool {
	return m.Preset != "" || len(m.Names) > 0 || len(m.Exclude) > 0 ||
		len(m.DomainSuffix) > 0 || len(m.IPCIDR) > 0 || m.IPIsPrivate
}
```

- [ ] **Step 4: Run tests — expect PASS**

Run: `go test -run TestMatchConfig_Has -count=1 ./rule/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add rule/target.go rule/target_test.go
git commit -m "feat(rule): extend MatchConfig with inline + meta fields"
```

---

### Task 3: InlineSet — compile config strings to in-memory matchers

**Files:**
- Create: `rule/inline.go`
- Create: `rule/inline_test.go`

- [ ] **Step 1: Write tests**

```go
// rule/inline_test.go
package rule

import (
	"net/netip"
	"testing"
)

func TestInlineSet_DomainMatch(t *testing.T) {
	s := BuildInlineSet([]string{"google.com", "youtube.com"}, nil)

	tests := []struct {
		domain string
		want   bool
	}{
		{"google.com", true},
		{"www.google.com", true},
		{"youtube.com", true},
		{"example.com", false},
		{"notgoogle.com", false},
	}
	for _, tt := range tests {
		if got := s.MatchDomain(tt.domain); got != tt.want {
			t.Errorf("MatchDomain(%q) = %v, want %v", tt.domain, got, tt.want)
		}
	}
}

func TestInlineSet_IPMatch(t *testing.T) {
	s := BuildInlineSet(nil, []string{"8.8.8.0/24", "2001:4860::/32"})

	tests := []struct {
		ip   string
		want bool
	}{
		{"8.8.8.8", true},
		{"8.8.8.1", true},
		{"8.8.4.4", false},
		{"2001:4860::1", true},
		{"2001:4861::1", false},
	}
	for _, tt := range tests {
		addr := netip.MustParseAddr(tt.ip)
		if got := s.MatchIP(addr); got != tt.want {
			t.Errorf("MatchIP(%q) = %v, want %v", tt.ip, got, tt.want)
		}
	}
}

func TestInlineSet_Empty(t *testing.T) {
	s := BuildInlineSet(nil, nil)
	if s.MatchDomain("anything.com") {
		t.Error("empty InlineSet should not match domains")
	}
	if s.MatchIP(netip.MustParseAddr("1.2.3.4")) {
		t.Error("empty InlineSet should not match IPs")
	}
}

func TestInlineSet_Mixed(t *testing.T) {
	s := BuildInlineSet(
		[]string{"example.com"},
		[]string{"10.0.0.0/8"},
	)
	if !s.MatchDomain("example.com") {
		t.Error("should match domain")
	}
	if !s.MatchIP(netip.MustParseAddr("10.1.2.3")) {
		t.Error("should match IP")
	}
	if s.MatchDomain("other.com") {
		t.Error("should not match unrelated domain")
	}
	if s.MatchIP(netip.MustParseAddr("192.168.1.1")) {
		t.Error("should not match unrelated IP")
	}
}
```

- [ ] **Step 2: Run tests — expect FAIL**

Run: `go test -run TestInlineSet -count=1 ./rule/`
Expected: FAIL — `BuildInlineSet` undefined

- [ ] **Step 3: Write implementation**

```go
// rule/inline.go
package rule

import "net/netip"

// InlineSet is an in-memory rule set compiled from config domain_suffix and
// ip_cidr strings. Unlike BundleSet (backed by k2b file bytes), InlineSet
// owns its data — allocated at engine build time, freed on Close.
type InlineSet struct {
	domains domainSection
	ipv4    ipv4Section
	ipv6    ipv6Section
	data    []byte // backing store for domainSection
}

// BuildInlineSet compiles domain suffixes and CIDR strings into an InlineSet.
// Returns an InlineSet where MatchDomain/MatchIP return false for nil inputs.
func BuildInlineSet(domainSuffixes []string, cidrs []string) InlineSet {
	var s InlineSet

	if len(domainSuffixes) > 0 {
		s.data = buildDomainData(domainSuffixes)
		if len(s.data) >= 4 {
			count := int(le.Uint32(s.data[0:4]))
			s.domains = domainSection{data: s.data, count: count}
		}
	}

	if len(cidrs) > 0 {
		v4data, v4count := buildIPv4Data(cidrs)
		if v4count > 0 {
			s.ipv4 = ipv4Section{data: v4data, count: v4count}
		}
		v6data, v6count := buildIPv6Data(cidrs)
		if v6count > 0 {
			s.ipv6 = ipv6Section{data: v6data, count: v6count}
		}
	}

	return s
}

// MatchDomain reports whether domain matches any suffix in this inline set.
func (s *InlineSet) MatchDomain(domain string) bool {
	return s.domains.Match(domain)
}

// MatchIP reports whether addr falls within any CIDR in this inline set.
func (s *InlineSet) MatchIP(addr netip.Addr) bool {
	if addr.Is4() {
		return s.ipv4.Contains(addr)
	}
	return s.ipv6.Contains(addr)
}

// IsEmpty reports whether this inline set has no rules.
func (s *InlineSet) IsEmpty() bool {
	return s.domains.count == 0 && s.ipv4.count == 0 && s.ipv6.count == 0
}
```

Also add to `rule/inline.go` a package-level alias for binary.LittleEndian to keep it clean:

```go
import (
	"encoding/binary"
	"net/netip"
)

var le = binary.LittleEndian
```

- [ ] **Step 4: Run tests — expect PASS**

Run: `go test -run TestInlineSet -count=1 ./rule/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add rule/inline.go rule/inline_test.go
git commit -m "feat(rule): InlineSet — compile config strings to in-memory matchers"
```

---

### Task 4: RouteEntry extension + MatchConn in Engine

**Files:**
- Modify: `rule/engine.go`
- Modify: `rule/engine_test.go`

- [ ] **Step 1: Write tests for MatchConn**

Add to `rule/engine_test.go`:

```go
func TestEngineMatchConn_InlineDomain(t *testing.T) {
	inline := BuildInlineSet([]string{"custom-blocked.com"}, nil)
	routes := []RouteEntry{
		{Target: TargetProxy, Inline: &inline},
	}
	eng := NewEngine(routes, TargetDirect, nil, false)
	defer eng.Close()

	meta := ConnMeta{Network: "tcp"}
	if got := eng.MatchConn("custom-blocked.com", meta); got != TargetProxy {
		t.Errorf("MatchConn(custom-blocked.com) = %v, want proxy", got)
	}
	if got := eng.MatchConn("www.custom-blocked.com", meta); got != TargetProxy {
		t.Errorf("MatchConn(www.custom-blocked.com) = %v, want proxy", got)
	}
	if got := eng.MatchConn("other.com", meta); got != TargetDirect {
		t.Errorf("MatchConn(other.com) = %v, want direct", got)
	}
}

func TestEngineMatchConn_InlineIP(t *testing.T) {
	inline := BuildInlineSet(nil, []string{"10.0.0.0/8"})
	routes := []RouteEntry{
		{Target: TargetDirect, Inline: &inline},
	}
	eng := NewEngine(routes, TargetProxy, nil, false)
	defer eng.Close()

	meta := ConnMeta{Network: "tcp"}
	if got := eng.MatchConn("10.1.2.3", meta); got != TargetDirect {
		t.Errorf("MatchConn(10.1.2.3) = %v, want direct", got)
	}
	if got := eng.MatchConn("8.8.8.8", meta); got != TargetProxy {
		t.Errorf("MatchConn(8.8.8.8) = %v, want proxy (fallback)", got)
	}
}

func TestEngineMatchConn_ProcessName(t *testing.T) {
	routes := []RouteEntry{
		{Target: TargetDirect, ProcessNames: []string{"ssh", "git"}},
	}
	eng := NewEngine(routes, TargetProxy, nil, false)
	defer eng.Close()

	// ssh matches → direct
	if got := eng.MatchConn("8.8.8.8", ConnMeta{ProcessName: "ssh"}); got != TargetDirect {
		t.Errorf("ssh = %v, want direct", got)
	}
	// git matches → direct
	if got := eng.MatchConn("8.8.8.8", ConnMeta{ProcessName: "git"}); got != TargetDirect {
		t.Errorf("git = %v, want direct", got)
	}
	// chrome doesn't match → fallback proxy
	if got := eng.MatchConn("8.8.8.8", ConnMeta{ProcessName: "chrome"}); got != TargetProxy {
		t.Errorf("chrome = %v, want proxy", got)
	}
	// empty process → no match (meta-only routes require meta)
	if got := eng.MatchConn("8.8.8.8", ConnMeta{}); got != TargetProxy {
		t.Errorf("empty = %v, want proxy", got)
	}
}

func TestEngineMatchConn_PackageName(t *testing.T) {
	routes := []RouteEntry{
		{Target: TargetProxy, PackageNames: []string{"com.android.chrome"}},
	}
	eng := NewEngine(routes, TargetDirect, nil, false)
	defer eng.Close()

	if got := eng.MatchConn("1.2.3.4", ConnMeta{PackageName: "com.android.chrome"}); got != TargetProxy {
		t.Errorf("chrome = %v, want proxy", got)
	}
	if got := eng.MatchConn("1.2.3.4", ConnMeta{PackageName: "com.tencent.mm"}); got != TargetDirect {
		t.Errorf("wechat = %v, want direct", got)
	}
}

func TestEngineMatchConn_Network(t *testing.T) {
	routes := []RouteEntry{
		{Target: TargetDirect, NetworkMatch: "udp"},
	}
	eng := NewEngine(routes, TargetProxy, nil, false)
	defer eng.Close()

	if got := eng.MatchConn("1.2.3.4", ConnMeta{Network: "udp"}); got != TargetDirect {
		t.Errorf("udp = %v, want direct", got)
	}
	if got := eng.MatchConn("1.2.3.4", ConnMeta{Network: "tcp"}); got != TargetProxy {
		t.Errorf("tcp = %v, want proxy", got)
	}
}

func TestEngineMatchConn_IPIsPrivate(t *testing.T) {
	routes := []RouteEntry{
		{Target: TargetDirect, IPIsPrivate: true},
	}
	eng := NewEngine(routes, TargetProxy, nil, false)
	defer eng.Close()

	meta := ConnMeta{Network: "tcp"}
	if got := eng.MatchConn("192.168.1.1", meta); got != TargetDirect {
		t.Errorf("192.168.1.1 = %v, want direct", got)
	}
	if got := eng.MatchConn("10.0.0.1", meta); got != TargetDirect {
		t.Errorf("10.0.0.1 = %v, want direct", got)
	}
	if got := eng.MatchConn("127.0.0.1", meta); got != TargetDirect {
		t.Errorf("127.0.0.1 = %v, want direct", got)
	}
	if got := eng.MatchConn("8.8.8.8", meta); got != TargetProxy {
		t.Errorf("8.8.8.8 = %v, want proxy", got)
	}
	// domain host: ip_is_private does not apply
	if got := eng.MatchConn("google.com", meta); got != TargetProxy {
		t.Errorf("google.com = %v, want proxy", got)
	}
}

func TestEngineMatchConn_AND_Semantics(t *testing.T) {
	// Route: proxy overseas domains from Chrome only
	b := mustBundle(t, []BundleSet{
		{Name: "google", DomainSuffixes: []string{"google.com"}},
	})
	idx := Index([]*Bundle{b})
	routes := []RouteEntry{
		{
			Target:       TargetProxy,
			Sets:         bindSets(idx, "google"),
			ProcessNames: []string{"Google Chrome"},
		},
	}
	eng := NewEngine(routes, TargetDirect, []*Bundle{b}, false)
	defer eng.Close()

	// google.com from Chrome → proxy (both criteria match)
	if got := eng.MatchConn("google.com", ConnMeta{ProcessName: "Google Chrome"}); got != TargetProxy {
		t.Errorf("chrome+google = %v, want proxy", got)
	}
	// google.com from ssh → direct (process doesn't match)
	if got := eng.MatchConn("google.com", ConnMeta{ProcessName: "ssh"}); got != TargetDirect {
		t.Errorf("ssh+google = %v, want direct", got)
	}
	// example.com from Chrome → direct (domain doesn't match)
	if got := eng.MatchConn("example.com", ConnMeta{ProcessName: "Google Chrome"}); got != TargetDirect {
		t.Errorf("chrome+example = %v, want direct", got)
	}
}

func TestEngineMatch_IgnoresMetaOnlyRoutes(t *testing.T) {
	// Route with only process criteria should NOT affect DNS-time Match()
	routes := []RouteEntry{
		{Target: TargetDirect, ProcessNames: []string{"ssh"}},
	}
	eng := NewEngine(routes, TargetProxy, nil, false)
	defer eng.Close()

	// Match (DNS-time) should skip meta-only routes → fallback
	if got := eng.Match("anything.com"); got != TargetProxy {
		t.Errorf("Match(anything.com) = %v, want proxy (fallback)", got)
	}
}
```

- [ ] **Step 2: Run tests — expect FAIL**

Run: `go test -run "TestEngineMatchConn|TestEngineMatch_Ignores" -count=1 ./rule/`
Expected: FAIL — `MatchConn`, `Inline`, `ProcessNames`, etc. undefined

- [ ] **Step 3: Update RouteEntry and Engine**

Modify `rule/engine.go`:

```go
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
	if r.IPIsPrivate {
		return true
	}
	return false
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
// Used at DNS time — ignores routes that have ONLY meta criteria (no host criteria).
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
				continue // skip meta-only routes at DNS time
			}
			if e.matchRouteHost(r, "", addr) {
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
		if e.matchRouteDomain(r, domain) {
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
			hostMatch := !r.hasHostCriteria() || e.matchRouteHost(r, "", addr)
			metaMatch := !r.hasMetaCriteria() || matchMeta(r, meta)
			// At least one dimension must have criteria.
			if !r.hasHostCriteria() && !r.hasMetaCriteria() {
				continue
			}
			if hostMatch && metaMatch {
				return r.Target
			}
		}
		return e.fallback
	}
	domain := strings.ToLower(host)
	for i := range e.routes {
		r := &e.routes[i]
		hostMatch := !r.hasHostCriteria() || e.matchRouteDomain(r, domain)
		metaMatch := !r.hasMetaCriteria() || matchMeta(r, meta)
		if !r.hasHostCriteria() && !r.hasMetaCriteria() {
			continue
		}
		if hostMatch && metaMatch {
			return r.Target
		}
	}
	return e.fallback
}

// matchRouteHost checks if an IP matches any host criteria in the route.
func (e *Engine) matchRouteHost(r *RouteEntry, _ string, addr netip.Addr) bool {
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
func (e *Engine) matchRouteDomain(r *RouteEntry, domain string) bool {
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
func matchMeta(r *RouteEntry, meta ConnMeta) bool {
	if len(r.ProcessNames) > 0 {
		if meta.ProcessName == "" {
			return false
		}
		found := false
		for _, p := range r.ProcessNames {
			if strings.EqualFold(meta.ProcessName, p) {
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
	if r.NetworkMatch != "" {
		if meta.Network == "" || meta.Network != r.NetworkMatch {
			return false
		}
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
```

- [ ] **Step 4: Run all rule tests — expect PASS**

Run: `go test -count=1 ./rule/`
Expected: PASS (existing tests + new tests)

- [ ] **Step 5: Commit**

```bash
git add rule/engine.go rule/engine_test.go
git commit -m "feat(rule): MatchConn with inline, process, package, network, ip_is_private matching"
```

---

### Task 5: ProcessSearcher interface

**Files:**
- Create: `provider/process.go`

- [ ] **Step 1: Write implementation**

```go
// provider/process.go
package provider

import "net/netip"

// ProcessSearcher finds the process that owns a network connection.
// Platform-specific implementations look up the process by source address.
// The nop implementation (default) returns empty strings.
type ProcessSearcher interface {
	FindProcess(network string, srcIP netip.Addr, srcPort uint16) (processName string, packageName string)
}

// NopProcessSearcher returns empty strings for all lookups.
// Used when platform doesn't support process detection.
type NopProcessSearcher struct{}

func (NopProcessSearcher) FindProcess(string, netip.Addr, uint16) (string, string) {
	return "", ""
}
```

- [ ] **Step 2: Commit**

```bash
git add provider/process.go
git commit -m "feat(provider): ProcessSearcher interface for process name lookup"
```

---

### Task 6: Router.RouteConn + Tunnel integration

**Files:**
- Modify: `core/router.go`
- Modify: `core/tunnel.go`

- [ ] **Step 1: Add RouteConn to Router**

Update `core/router.go`:

```go
package core

import (
	"github.com/kaitu-io/k2/core/packet"
	"github.com/kaitu-io/k2/rule"
)

// Router implements routing decisions via the rule engine.
type Router struct {
	engine *rule.Engine
}

// NewRouter creates a new router backed by the given rule engine.
func NewRouter(engine *rule.Engine) *Router {
	return &Router{engine: engine}
}

// Route makes a routing decision for a packet.
func (r *Router) Route(info packet.Info) rule.Target {
	return r.engine.Match(info.DstIP)
}

// RouteAddr makes a routing decision for a host address (IP or domain).
// Used by DNS middleware and SNI-based routing.
func (r *Router) RouteAddr(host string) rule.Target {
	return r.engine.Match(host)
}

// RouteConn makes a routing decision with full connection metadata.
// Used by tunnel for connection-time routing.
func (r *Router) RouteConn(host string, meta rule.ConnMeta) rule.Target {
	return r.engine.MatchConn(host, meta)
}

// SetTmpRule adds a DNS-learned temporary routing rule.
func (r *Router) SetTmpRule(ip string, target rule.Target) {
	r.engine.SetTmpRule(ip, target)
}
```

- [ ] **Step 2: Add ProcessSearcher to ClientTunnelConfig and update route()**

In `core/tunnel.go`, add field to config and struct:

Add to `ClientTunnelConfig`:
```go
ProcessSearcher provider.ProcessSearcher // optional: nil = no process matching
```

Add to `ClientTunnel` struct:
```go
processSearcher provider.ProcessSearcher
```

In `NewClientTunnel`, add:
```go
processSearcher: cfg.ProcessSearcher,
```

Replace the `route` method:

```go
func (t *ClientTunnel) route(dest string) rule.Target {
	if t.router == nil {
		return rule.TargetProxy
	}
	host, _, err := net.SplitHostPort(dest)
	if err != nil {
		return rule.TargetProxy
	}
	return t.router.RouteAddr(host)
}
```

With:

```go
func (t *ClientTunnel) routeConn(dest string, network string, conn interface{ RemoteAddr() net.Addr }) rule.Target {
	if t.router == nil {
		return rule.TargetProxy
	}
	host, _, err := net.SplitHostPort(dest)
	if err != nil {
		return rule.TargetProxy
	}

	meta := rule.ConnMeta{Network: network}

	// Look up process info from source address.
	if t.processSearcher != nil && conn != nil {
		if srcAddr, ok := conn.RemoteAddr().(*net.TCPAddr); ok {
			srcIP, _ := netip.AddrFromSlice(srcAddr.IP)
			meta.ProcessName, meta.PackageName = t.processSearcher.FindProcess(network, srcIP, uint16(srcAddr.Port))
		} else if srcAddr, ok := conn.RemoteAddr().(*net.UDPAddr); ok {
			srcIP, _ := netip.AddrFromSlice(srcAddr.IP)
			meta.ProcessName, meta.PackageName = t.processSearcher.FindProcess(network, srcIP, uint16(srcAddr.Port))
		}
	}

	return t.router.RouteConn(host, meta)
}
```

Then in `HandleTCP`, replace:
```go
action := t.route(dest)
```
with:
```go
action := t.routeConn(dest, "tcp", conn)
```

In `HandleUDP`, replace:
```go
action := t.route(dest)
```
with:
```go
action := t.routeConn(dest, "udp", nil) // UDP PacketConn doesn't have RemoteAddr
```

Note: For UDP, `net.PacketConn` doesn't implement `RemoteAddr()` the same way. We pass nil — process lookup for UDP is future work (requires source addr from the first packet).

Also update the SNI override sections in `HandleTCP` and `HandleUDP` that call `t.router.RouteAddr(sni)` — these stay as `RouteAddr` (DNS-time semantics, no meta).

- [ ] **Step 3: Run all core tests**

Run: `go test -count=1 ./core/...`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add core/router.go core/tunnel.go
git commit -m "feat(core): RouteConn with ConnMeta for connection-time routing"
```

---

### Task 7: Engine buildRuleEngine handles inline rules + meta

**Files:**
- Modify: `engine/engine.go`
- Modify: `engine/engine_test.go`

- [ ] **Step 1: Write test**

Add to `engine/engine_test.go`:

```go
func TestBuildRuleEngine_InlineRules(t *testing.T) {
	client := &config.ClientConfig{
		Routes: []config.RouteConfig{
			{
				Via: "reject",
				Match: rule.MatchConfig{
					DomainSuffix: []string{"ads.example.com"},
				},
			},
			{
				Via: "direct",
				Match: rule.MatchConfig{
					IPCIDR: []string{"10.0.0.0/8", "192.168.0.0/16"},
				},
			},
			{
				Via: "k2v5://server:443",
				Match: rule.MatchConfig{
					Preset:       "overseas",
					DomainSuffix: []string{"custom-blocked.com"},
				},
			},
		},
	}
	re := buildRuleEngine(client)
	defer re.Close()

	// Inline domain → reject
	if got := re.Match("ads.example.com"); got != rule.TargetReject {
		t.Errorf("ads.example.com = %v, want reject", got)
	}
	// Inline IP → direct
	if got := re.Match("10.1.2.3"); got != rule.TargetDirect {
		t.Errorf("10.1.2.3 = %v, want direct", got)
	}
	if got := re.Match("192.168.1.1"); got != rule.TargetDirect {
		t.Errorf("192.168.1.1 = %v, want direct", got)
	}
	// Custom inline domain in overseas route → proxy
	if got := re.Match("custom-blocked.com"); got != rule.TargetProxy {
		t.Errorf("custom-blocked.com = %v, want proxy", got)
	}
}

func TestBuildRuleEngine_ProcessRoute(t *testing.T) {
	client := &config.ClientConfig{
		Routes: []config.RouteConfig{
			{
				Via: "direct",
				Match: rule.MatchConfig{
					ProcessName: []string{"ssh", "git"},
				},
			},
			{
				Via: "k2v5://server:443",
				Match: rule.MatchConfig{All: true},
			},
		},
	}
	re := buildRuleEngine(client)
	defer re.Close()

	// DNS-time: process-only route should be skipped
	if got := re.Match("google.com"); got != rule.TargetProxy {
		t.Errorf("Match(google.com) = %v, want proxy", got)
	}

	// Connection-time: ssh → direct
	if got := re.MatchConn("google.com", rule.ConnMeta{ProcessName: "ssh"}); got != rule.TargetDirect {
		t.Errorf("MatchConn(ssh) = %v, want direct", got)
	}

	// Connection-time: chrome → proxy (all proxy fallback)
	if got := re.MatchConn("google.com", rule.ConnMeta{ProcessName: "chrome"}); got != rule.TargetProxy {
		t.Errorf("MatchConn(chrome) = %v, want proxy", got)
	}
}
```

- [ ] **Step 2: Run tests — expect FAIL**

Run: `go test -run "TestBuildRuleEngine_Inline|TestBuildRuleEngine_Process" -count=1 ./engine/`
Expected: FAIL

- [ ] **Step 3: Update buildRuleEngine**

In `engine/engine.go`, update `buildRuleEngine` to handle new MatchConfig fields:

```go
func buildRuleEngine(client *config.ClientConfig) *rule.Engine {
	if client.IsAllProxy() {
		return rule.NewEngine(nil, rule.TargetProxy, nil, true)
	}

	fallback := rule.TargetDirect

	bundles, err := rule.Load(client.CacheDir)
	if err != nil {
		slog.Warn("engine: rule load failed", "err", err)
	}
	idx := rule.Index(bundles)

	var entries []rule.RouteEntry
	allProxy := false

	for i, route := range client.Routes {
		target := rule.ParseVia(route.Via)

		if route.Match.All {
			allProxy = (target == rule.TargetProxy)
			fallback = target
			break
		}

		entry := rule.RouteEntry{Target: target}

		// Bundle-based sets.
		names := rule.ExpandMatch(route.Match)
		for _, name := range names {
			if bs, ok := idx[name]; ok {
				entry.Sets = append(entry.Sets, bs)
			}
		}

		// Inline rules.
		if len(route.Match.DomainSuffix) > 0 || len(route.Match.IPCIDR) > 0 {
			inline := rule.BuildInlineSet(route.Match.DomainSuffix, route.Match.IPCIDR)
			entry.Inline = &inline
		}

		// Meta criteria.
		entry.ProcessNames = route.Match.ProcessName
		entry.PackageNames = route.Match.PackageName
		entry.NetworkMatch = route.Match.Network
		entry.IPIsPrivate = route.Match.IPIsPrivate

		// Only add route if it has any criteria.
		if len(entry.Sets) > 0 || (entry.Inline != nil && !entry.Inline.IsEmpty()) ||
			len(entry.ProcessNames) > 0 || len(entry.PackageNames) > 0 ||
			entry.NetworkMatch != "" || entry.IPIsPrivate {
			entries = append(entries, entry)
		}

		if i == len(client.Routes)-1 && !route.Match.HasCriteria() {
			fallback = target
		}
	}

	if len(bundles) == 0 && !hasInlineRules(entries) {
		slog.Info("engine: no rule bundles or inline rules, using fallback only",
			"cacheDir", client.CacheDir, "fallback", fallback)
	}

	return rule.NewEngine(entries, fallback, bundles, allProxy)
}

func hasInlineRules(entries []rule.RouteEntry) bool {
	for _, e := range entries {
		if e.Inline != nil && !e.Inline.IsEmpty() {
			return true
		}
		if len(e.ProcessNames) > 0 || len(e.PackageNames) > 0 {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Run tests — expect PASS**

Run: `go test -count=1 ./engine/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add engine/engine.go engine/engine_test.go
git commit -m "feat(engine): buildRuleEngine handles inline rules + meta criteria"
```

---

### Task 8: Wire ProcessSearcher into engine + tunnel

**Files:**
- Modify: `engine/engine.go` (Config struct)
- Modify: `engine/config.go` (if exists, or engine.go)

- [ ] **Step 1: Add ProcessSearcher to engine.Config**

In `engine/engine.go`, find the `Config` struct and add:

```go
ProcessSearcher provider.ProcessSearcher // optional: nil = no process matching
```

- [ ] **Step 2: Pass to ClientTunnelConfig**

In the `Start()` method where `ClientTunnelConfig` is built, add:

```go
tunnelCfg.ProcessSearcher = cfg.ProcessSearcher
```

Note: `provider` package already imported in engine.go.

- [ ] **Step 3: Run full test suite**

Run: `go test -short -count=1 ./...`
Expected: PASS (all packages compile, all tests pass)

- [ ] **Step 4: Commit**

```bash
git add engine/engine.go
git commit -m "feat(engine): wire ProcessSearcher through to tunnel"
```

---

### Task 9: Update CLAUDE.md

**Files:**
- Modify: `CLAUDE.md`

- [ ] **Step 1: Update MatchConfig documentation**

Update the `routes` config example and MatchConfig docs. Update the Dependencies section that describes rule/.

In the Dependencies section, update the `rule/` entry to:

```
- `rule/` (in-tree) — multi-ruleset routing engine: domain suffix binary search + IP range O(log n) binary search, k2b mmap-indexed format, routes-based config, per-service IP-CIDR + domain rule sets, inline rules (domain_suffix/ip_cidr from config), connection metadata matching (process_name, package_name, network, ip_is_private)
```

In the Data Flow section, update Router description to mention ConnMeta:

```
- Traffic: TUN → gVisor stack → ConnectionHandler → Router (RouteConn with ConnMeta) → Wire (QUIC/TCP-WS) → Server
```

- [ ] **Step 2: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: update CLAUDE.md with rule v2 inline + meta matching"
```

---

### Task 10: Full validation

- [ ] **Step 1: Run all tests**

```bash
go test -short -count=1 -race ./rule/... ./core/... ./engine/...
```
Expected: PASS

- [ ] **Step 2: Run full integration test**

```bash
go test -run TestIntegration_RealBundles -count=1 ./rule/
```
Expected: PASS

- [ ] **Step 3: Run make quick-check**

```bash
make quick-check
```
Expected: PASS

---

## Config Examples

After this plan, users can write:

```yaml
routes:
  # Block ads
  - via: reject
    match:
      domain_suffix: ["ads.example.com", "tracker.example.com"]

  # Private IPs always direct
  - via: direct
    match:
      ip_is_private: true

  # Terminal tools direct
  - via: direct
    match:
      process_name: ["ssh", "git", "scp"]

  # Android: only proxy Chrome and Telegram
  - via: "k2v5://server:443?ech=..."
    match:
      package_name: ["com.android.chrome", "org.telegram.messenger"]

  # Proxy overseas with custom additions
  - via: "k2v5://server:443?ech=..."
    match:
      preset: overseas
      domain_suffix: ["my-blocked-site.com"]

  # CN direct
  - via: direct
    match:
      preset: cn-access
      ip_cidr: ["10.0.0.0/8", "192.168.0.0/16"]

  # Everything else direct
  - via: direct
    match:
      all: true
```
