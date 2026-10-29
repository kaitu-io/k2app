# Rule Module Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace `k2rule` with an in-tree `rule/` package. New `routes`-based config format. Per-service IP-CIDR + domain rule sets in mmap-indexed k2b format.

**Architecture:** New `rule/` package: domain binary search (k2rule v3 algorithm), IP range binary search (O(log n)), k2b format, Engine (Match + SetTmpRule), loader (cache_dir). Config changes from `server` + `rule` fields to `routes` array. Integration replaces all `k2rule.*` references. Phase 1: single server. Phase 2 (future): multi-server routing.

**Tech Stack:** Go stdlib only for rule/. `deadlock.RWMutex` for concurrency.

**Spec:** `docs/superpowers/specs/2026-04-02-rule-module-design.md`

---

## File Structure

### New files (rule/ package)

| File | Responsibility |
|------|---------------|
| `rule/target.go` | `Target` type (Direct/Proxy/Reject), preset expansion, match expansion, set merging |
| `rule/target_test.go` | Target, preset, merge tests |
| `rule/domain.go` | `domainSection` — binary search on reversed sorted domains in `[]byte` slice |
| `rule/domain_test.go` | Domain matching tests |
| `rule/ip.go` | `ipv4Section`, `ipv6Section` — binary search on sorted IP ranges in `[]byte` slice |
| `rule/ip_test.go` | IP matching tests |
| `rule/format.go` | k2b v1 reader/writer: `ReadBundle([]byte)`, `WriteBundle(io.Writer, []BundleSet)` |
| `rule/format_test.go` | Round-trip tests |
| `rule/engine.go` | `Engine` — Match, SetTmpRule, Close; holds bound routes + mmap'd bundles |
| `rule/engine_test.go` | Engine integration tests |
| `rule/loader.go` | `Load(cacheDir)` — read .k2b files, build name→BundleSet index |
| `rule/loader_test.go` | Loader tests (empty dir, valid bundles, corrupt files) |

### Modified files (integration)

| File | Change |
|------|--------|
| `config/config.go` | Delete `Server`+`Rule`, add `Routes []RouteConfig` |
| `core/router.go` | Hold `*rule.Engine`, delegate Match, add SetTmpRule |
| `core/tunnel.go` | `k2rule.Target` → `rule.Target`, SetTmpRule via router (5 sites) |
| `core/dns/middleware.go` | Inject matchRule/setTmpRule functions (7 sites) |
| `core/dns/middleware_test.go` | Replace `initK2Rule` |
| `core/tunnel_test.go` | Update Router construction |
| `engine/engine.go` | Extract server URL from routes, build rule.Engine, close in Stop() |
| `engine/engine_test.go` | Replace k2rule setup |
| `go.mod` | Remove `github.com/kaitu-io/k2rule` |

---

## Task 1: Target type + presets + match expansion

**Files:**
- Create: `rule/target.go`
- Create: `rule/target_test.go`

- [ ] **Step 1: Write target_test.go**

```go
package rule

import "testing"

func TestTargetString(t *testing.T) {
	tests := []struct {
		target Target
		want   string
	}{
		{TargetDirect, "direct"},
		{TargetProxy, "proxy"},
		{TargetReject, "reject"},
		{Target(99), "unknown(99)"},
	}
	for _, tt := range tests {
		if got := tt.target.String(); got != tt.want {
			t.Errorf("Target(%d).String() = %q, want %q", tt.target, got, tt.want)
		}
	}
}

func TestParseVia(t *testing.T) {
	tests := []struct {
		via  string
		want Target
	}{
		{"direct", TargetDirect},
		{"reject", TargetReject},
		{"k2v5://server:443", TargetProxy},
		{"k2v5://user:pass@host:443?ech=abc", TargetProxy},
	}
	for _, tt := range tests {
		if got := ParseVia(tt.via); got != tt.want {
			t.Errorf("ParseVia(%q) = %v, want %v", tt.via, got, tt.want)
		}
	}
}

func TestExpandPreset(t *testing.T) {
	names := ExpandPreset("overseas")
	if len(names) == 0 {
		t.Fatal("overseas preset should be non-empty")
	}
	found := false
	for _, n := range names {
		if n == "google" {
			found = true
		}
	}
	if !found {
		t.Error("overseas preset must include google")
	}
}

func TestExpandPresetUnknown(t *testing.T) {
	if names := ExpandPreset("nonexistent"); names != nil {
		t.Errorf("unknown preset should return nil, got %v", names)
	}
}

func TestExpandMatch(t *testing.T) {
	// Preset + names + exclude
	m := MatchConfig{
		Preset:  "overseas",
		Names:   []string{"my-corp"},
		Exclude: []string{"openai"},
	}
	names := ExpandMatch(m)

	// google should be present (from preset)
	has := func(name string) bool {
		for _, n := range names {
			if n == name {
				return true
			}
		}
		return false
	}
	if !has("google") {
		t.Error("should include google from preset")
	}
	if !has("my-corp") {
		t.Error("should include my-corp from names")
	}
	if has("openai") {
		t.Error("openai should be excluded")
	}
}

func TestExpandMatchAll(t *testing.T) {
	m := MatchConfig{All: true}
	names := ExpandMatch(m)
	if names != nil {
		t.Errorf("match all should return nil names, got %v", names)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/david/projects/kaitu-io/k2 && go test ./rule/ -v -run 'TestTarget|TestParse|TestExpand' 2>&1 | head -5`
Expected: compilation error — package `rule` does not exist

- [ ] **Step 3: Write target.go**

```go
package rule

import (
	"fmt"
	"strings"
)

// Target is the routing decision for a matched host.
type Target uint8

const (
	TargetDirect Target = iota
	TargetProxy
	TargetReject
)

func (t Target) String() string {
	switch t {
	case TargetDirect:
		return "direct"
	case TargetProxy:
		return "proxy"
	case TargetReject:
		return "reject"
	default:
		return fmt.Sprintf("unknown(%d)", t)
	}
}

// ParseVia converts a route via string to a Target.
// k2v5:// URLs → TargetProxy; "direct" → TargetDirect; "reject" → TargetReject.
func ParseVia(via string) Target {
	if strings.HasPrefix(via, "k2v5://") {
		return TargetProxy
	}
	switch strings.ToLower(via) {
	case "direct":
		return TargetDirect
	case "reject":
		return TargetReject
	default:
		return TargetDirect
	}
}

// MatchConfig describes which rule sets a route matches.
type MatchConfig struct {
	Preset  string   `yaml:"preset,omitempty"  json:"preset,omitempty"`
	Names   []string `yaml:"names,omitempty"   json:"names,omitempty"`
	Exclude []string `yaml:"exclude,omitempty" json:"exclude,omitempty"`
	All     bool     `yaml:"all,omitempty"     json:"all,omitempty"`
}

// HasCriteria returns true if any match criteria are set.
func (m MatchConfig) HasCriteria() bool {
	return m.All || m.Preset != "" || len(m.Names) > 0
}

// presets maps preset name to the rule set names it includes.
var presets = map[string][]string{
	"overseas": {
		"google", "facebook", "telegram", "twitter", "netflix",
		"discord", "spotify", "tiktok", "openai", "github",
		"cloudflare", "fastly",
	},
	"cn-access": {
		"cn-sites", "bilibili", "geoip-cn",
	},
}

// ExpandPreset returns the rule set names for a preset, or nil if unknown.
func ExpandPreset(name string) []string {
	names, ok := presets[strings.ToLower(name)]
	if !ok {
		return nil
	}
	out := make([]string, len(names))
	copy(out, names)
	return out
}

// ExpandMatch expands a MatchConfig into a list of rule set names.
// Returns nil for match-all (caller should treat as fallback).
func ExpandMatch(m MatchConfig) []string {
	if m.All {
		return nil
	}

	var names []string
	if m.Preset != "" {
		names = append(names, ExpandPreset(m.Preset)...)
	}
	names = append(names, m.Names...)

	// Apply excludes
	if len(m.Exclude) > 0 {
		excl := make(map[string]bool, len(m.Exclude))
		for _, e := range m.Exclude {
			excl[e] = true
		}
		filtered := names[:0]
		for _, n := range names {
			if !excl[n] {
				filtered = append(filtered, n)
			}
		}
		names = filtered
	}

	return names
}
```

- [ ] **Step 4: Run tests**

Run: `cd /Users/david/projects/kaitu-io/k2 && go test ./rule/ -v -run 'TestTarget|TestParse|TestExpand'`
Expected: all PASS

- [ ] **Step 5: Commit**

```bash
git add rule/target.go rule/target_test.go
git commit -m "feat(rule): Target type, ParseVia, preset expansion, match expansion"
```

---

## Task 2: Domain binary search

**Files:**
- Create: `rule/domain.go`
- Create: `rule/domain_test.go`

- [ ] **Step 1: Write domain_test.go**

```go
package rule

import (
	"encoding/binary"
	"sort"
	"testing"
)

// buildTestDomainSection builds a domain section from suffix domains for testing.
func buildTestDomainSection(domains []string) domainSection {
	data := buildDomainData(domains)
	if len(data) == 0 {
		return domainSection{}
	}
	count := int(binary.LittleEndian.Uint32(data[0:4]))
	return domainSection{data: data, count: count}
}

func TestDomainMatchSuffix(t *testing.T) {
	ds := buildTestDomainSection([]string{"google.com", "youtube.com"})

	tests := []struct {
		domain string
		want   bool
	}{
		{"google.com", true},
		{"www.google.com", true},
		{"mail.google.com", true},
		{"youtube.com", true},
		{"m.youtube.com", true},
		{"example.com", false},
		{"notgoogle.com", false},
		{"google.com.evil.com", false},
	}
	for _, tt := range tests {
		if got := ds.Match(tt.domain); got != tt.want {
			t.Errorf("Match(%q) = %v, want %v", tt.domain, got, tt.want)
		}
	}
}

func TestDomainMatchEmpty(t *testing.T) {
	ds := domainSection{}
	if ds.Match("anything.com") {
		t.Error("empty domain section should not match anything")
	}
}

func TestDomainMatchCaseInsensitive(t *testing.T) {
	ds := buildTestDomainSection([]string{"google.com"})

	if !ds.Match("GOOGLE.COM") {
		t.Error("should be case-insensitive")
	}
	if !ds.Match("Google.Com") {
		t.Error("should be case-insensitive (mixed)")
	}
}

func TestDomainNoFalsePrefix(t *testing.T) {
	// "oogle.com" should NOT match the "google.com" suffix entry
	ds := buildTestDomainSection([]string{"google.com"})
	if ds.Match("oogle.com") {
		t.Error("oogle.com should not match google.com suffix")
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `cd /Users/david/projects/kaitu-io/k2 && go test ./rule/ -v -run TestDomain 2>&1 | head -5`
Expected: compilation error — `domainSection` undefined

- [ ] **Step 3: Write domain.go**

```go
package rule

import (
	"encoding/binary"
	"sort"
	"strings"
)

// domainSection provides zero-allocation domain matching on a byte slice.
// Layout: count(4) + offsets(count+1)*4 + strings area.
// Strings are reversed, lowercased, dot-prefixed domains, sorted lexicographically.
// Reuses k2rule v3 algorithm.
type domainSection struct {
	data  []byte
	count int
}

// Match returns true if domain matches any entry (suffix match).
func (ds *domainSection) Match(domain string) bool {
	if ds.count == 0 || len(ds.data) == 0 {
		return false
	}
	domain = strings.ToLower(domain)

	parts := strings.Split(domain, ".")
	for i := 0; i < len(parts); i++ {
		suffix := strings.Join(parts[i:], ".")
		if suffix == "" {
			continue
		}
		target := reverseString("." + suffix)
		if ds.bsearch(target) {
			return true
		}
	}
	return false
}

func (ds *domainSection) bsearch(target string) bool {
	idx := sort.Search(ds.count, func(j int) bool {
		return ds.entryAt(j) >= target
	})
	return idx < ds.count && ds.entryAt(idx) == target
}

func (ds *domainSection) entryAt(i int) string {
	base := 4 // skip count
	strStart := base + (ds.count+1)*4

	off := int(binary.LittleEndian.Uint32(ds.data[base+i*4 : base+i*4+4]))
	nextOff := int(binary.LittleEndian.Uint32(ds.data[base+(i+1)*4 : base+(i+1)*4+4]))
	return string(ds.data[strStart+off : strStart+nextOff])
}

func reverseString(s string) string {
	b := []byte(s)
	for i, j := 0, len(b)-1; i < j; i, j = i+1, j-1 {
		b[i], b[j] = b[j], b[i]
	}
	return string(b)
}

// buildDomainData builds the domain section binary for suffix domains.
// Used by WriteBundle and tests.
func buildDomainData(domains []string) []byte {
	if len(domains) == 0 {
		return nil
	}
	reversed := make([]string, len(domains))
	for i, d := range domains {
		reversed[i] = reverseString("." + strings.ToLower(d))
	}
	sort.Strings(reversed)

	var strBuf []byte
	offsets := make([]uint32, len(reversed)+1)
	for i, s := range reversed {
		offsets[i] = uint32(len(strBuf))
		strBuf = append(strBuf, []byte(s)...)
	}
	offsets[len(reversed)] = uint32(len(strBuf))

	buf := make([]byte, 4+(len(reversed)+1)*4+len(strBuf))
	binary.LittleEndian.PutUint32(buf[0:4], uint32(len(reversed)))
	for i, off := range offsets {
		binary.LittleEndian.PutUint32(buf[4+i*4:4+i*4+4], off)
	}
	copy(buf[4+(len(reversed)+1)*4:], strBuf)
	return buf
}
```

- [ ] **Step 4: Run tests**

Run: `cd /Users/david/projects/kaitu-io/k2 && go test ./rule/ -v -run TestDomain`
Expected: all PASS

- [ ] **Step 5: Commit**

```bash
git add rule/domain.go rule/domain_test.go
git commit -m "feat(rule): domain binary search — suffix matching on byte slice"
```

---

## Task 3: IP range binary search

**Files:**
- Create: `rule/ip.go`
- Create: `rule/ip_test.go`

- [ ] **Step 1: Write ip_test.go**

```go
package rule

import (
	"net/netip"
	"testing"
)

func TestIPv4Match(t *testing.T) {
	data, count := buildIPv4Data([]string{"8.8.8.0/24", "1.1.1.0/24"})
	sec := ipv4Section{data: data, count: count}

	tests := []struct {
		ip   string
		want bool
	}{
		{"8.8.8.8", true},
		{"8.8.8.0", true},
		{"8.8.8.255", true},
		{"8.8.9.0", false},
		{"1.1.1.1", true},
		{"1.1.2.0", false},
		{"192.168.1.1", false},
	}
	for _, tt := range tests {
		addr := netip.MustParseAddr(tt.ip)
		if got := sec.Contains(addr); got != tt.want {
			t.Errorf("Contains(%s) = %v, want %v", tt.ip, got, tt.want)
		}
	}
}

func TestIPv4Empty(t *testing.T) {
	sec := ipv4Section{}
	if sec.Contains(netip.MustParseAddr("8.8.8.8")) {
		t.Error("empty section should not match")
	}
}

func TestIPv4AdjacentRanges(t *testing.T) {
	data, count := buildIPv4Data([]string{"10.0.0.0/24", "10.0.1.0/24"})
	sec := ipv4Section{data: data, count: count}

	if !sec.Contains(netip.MustParseAddr("10.0.0.128")) {
		t.Error("should match first range")
	}
	if !sec.Contains(netip.MustParseAddr("10.0.1.128")) {
		t.Error("should match second range")
	}
	if sec.Contains(netip.MustParseAddr("10.0.2.0")) {
		t.Error("should not match outside")
	}
}

func TestIPv6Match(t *testing.T) {
	data, count := buildIPv6Data([]string{"2001:4860::/32"})
	sec := ipv6Section{data: data, count: count}

	if !sec.Contains(netip.MustParseAddr("2001:4860::8888")) {
		t.Error("should match inside /32")
	}
	if sec.Contains(netip.MustParseAddr("2001:4861::1")) {
		t.Error("should not match outside /32")
	}
}

func TestIPv6Empty(t *testing.T) {
	sec := ipv6Section{}
	if sec.Contains(netip.MustParseAddr("::1")) {
		t.Error("empty section should not match")
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `cd /Users/david/projects/kaitu-io/k2 && go test ./rule/ -v -run TestIPv 2>&1 | head -5`
Expected: compilation error

- [ ] **Step 3: Write ip.go**

```go
package rule

import (
	"bytes"
	"net/netip"
	"sort"
)

// ipv4Section provides zero-allocation IPv4 matching on a byte slice.
// Layout: (from[4] + to[4]) × count, sorted by from.
type ipv4Section struct {
	data  []byte
	count int
}

// Contains returns true if addr falls within any range. O(log n).
func (s *ipv4Section) Contains(addr netip.Addr) bool {
	if s.count == 0 || !addr.Is4() {
		return false
	}
	raw := addr.As4()
	// Find rightmost range where from <= raw.
	idx := sort.Search(s.count, func(i int) bool {
		return bytes.Compare(s.data[i*8:i*8+4], raw[:]) > 0
	}) - 1
	if idx < 0 {
		return false
	}
	return bytes.Compare(raw[:], s.data[idx*8+4:idx*8+8]) <= 0
}

// ipv6Section provides zero-allocation IPv6 matching on a byte slice.
// Layout: (from[16] + to[16]) × count, sorted by from.
type ipv6Section struct {
	data  []byte
	count int
}

// Contains returns true if addr falls within any range. O(log n).
func (s *ipv6Section) Contains(addr netip.Addr) bool {
	if s.count == 0 || !addr.Is6() {
		return false
	}
	raw := addr.As16()
	idx := sort.Search(s.count, func(i int) bool {
		return bytes.Compare(s.data[i*32:i*32+16], raw[:]) > 0
	}) - 1
	if idx < 0 {
		return false
	}
	return bytes.Compare(raw[:], s.data[idx*32+16:idx*32+32]) <= 0
}

// buildIPv4Data converts CIDRs to sorted merged ranges, returns (data, count).
func buildIPv4Data(cidrs []string) ([]byte, int) {
	type r struct{ from, to [4]byte }
	var ranges []r
	for _, cidr := range cidrs {
		p, err := netip.ParsePrefix(cidr)
		if err != nil || !p.Addr().Is4() {
			continue
		}
		from := p.Masked().Addr().As4()
		to := from
		for i := p.Bits(); i < 32; i++ {
			to[i/8] |= 1 << (7 - i%8)
		}
		ranges = append(ranges, r{from, to})
	}
	sort.Slice(ranges, func(i, j int) bool {
		return bytes.Compare(ranges[i].from[:], ranges[j].from[:]) < 0
	})
	// Merge overlapping/adjacent
	if len(ranges) > 1 {
		merged := ranges[:1]
		for _, cur := range ranges[1:] {
			last := &merged[len(merged)-1]
			if bytes.Compare(cur.from[:], last.to[:]) <= 0 {
				if bytes.Compare(cur.to[:], last.to[:]) > 0 {
					last.to = cur.to
				}
			} else {
				merged = append(merged, cur)
			}
		}
		ranges = merged
	}
	buf := make([]byte, len(ranges)*8)
	for i, rng := range ranges {
		copy(buf[i*8:], rng.from[:])
		copy(buf[i*8+4:], rng.to[:])
	}
	return buf, len(ranges)
}

// buildIPv6Data converts CIDRs to sorted merged ranges, returns (data, count).
func buildIPv6Data(cidrs []string) ([]byte, int) {
	type r struct{ from, to [16]byte }
	var ranges []r
	for _, cidr := range cidrs {
		p, err := netip.ParsePrefix(cidr)
		if err != nil || !p.Addr().Is6() {
			continue
		}
		from := p.Masked().Addr().As16()
		to := from
		for i := p.Bits(); i < 128; i++ {
			to[i/8] |= 1 << (7 - i%8)
		}
		ranges = append(ranges, r{from, to})
	}
	sort.Slice(ranges, func(i, j int) bool {
		return bytes.Compare(ranges[i].from[:], ranges[j].from[:]) < 0
	})
	if len(ranges) > 1 {
		merged := ranges[:1]
		for _, cur := range ranges[1:] {
			last := &merged[len(merged)-1]
			if bytes.Compare(cur.from[:], last.to[:]) <= 0 {
				if bytes.Compare(cur.to[:], last.to[:]) > 0 {
					last.to = cur.to
				}
			} else {
				merged = append(merged, cur)
			}
		}
		ranges = merged
	}
	buf := make([]byte, len(ranges)*32)
	for i, rng := range ranges {
		copy(buf[i*32:], rng.from[:])
		copy(buf[i*32+16:], rng.to[:])
	}
	return buf, len(ranges)
}
```

- [ ] **Step 4: Run tests**

Run: `cd /Users/david/projects/kaitu-io/k2 && go test ./rule/ -v -run TestIPv`
Expected: all PASS

- [ ] **Step 5: Commit**

```bash
git add rule/ip.go rule/ip_test.go
git commit -m "feat(rule): IP range binary search — O(log n) on sorted from/to pairs"
```

---

## Task 4: k2b format reader/writer

**Files:**
- Create: `rule/format.go`
- Create: `rule/format_test.go`

- [ ] **Step 1: Write format_test.go**

```go
package rule

import (
	"bytes"
	"net/netip"
	"testing"
)

func TestFormatRoundTrip(t *testing.T) {
	sets := []BundleSet{
		{
			Name:           "google",
			DomainSuffixes: []string{"google.com", "youtube.com"},
			CIDRs:          []string{"8.8.8.0/24", "2001:4860::/32"},
		},
		{
			Name:           "telegram",
			DomainSuffixes: []string{"telegram.org", "t.me"},
			CIDRs:          []string{"149.154.160.0/20"},
		},
	}
	var buf bytes.Buffer
	if err := WriteBundle(&buf, sets); err != nil {
		t.Fatalf("WriteBundle: %v", err)
	}
	data := buf.Bytes()
	if string(data[0:4]) != "K2RB" {
		t.Fatalf("bad magic: %q", data[0:4])
	}

	bundle, err := ReadBundle(data)
	if err != nil {
		t.Fatalf("ReadBundle: %v", err)
	}
	if len(bundle.Sets) != 2 {
		t.Fatalf("expected 2 sets, got %d", len(bundle.Sets))
	}

	g := &bundle.Sets[0]
	if g.Name != "google" {
		t.Errorf("set 0 name = %q", g.Name)
	}
	if !g.MatchDomain("www.google.com") {
		t.Error("should match www.google.com")
	}
	if !g.MatchDomain("youtube.com") {
		t.Error("should match youtube.com")
	}
	if g.MatchDomain("example.com") {
		t.Error("should not match example.com")
	}
	if !g.MatchIP(netip.MustParseAddr("8.8.8.8")) {
		t.Error("should match 8.8.8.8")
	}
	if g.MatchIP(netip.MustParseAddr("1.1.1.1")) {
		t.Error("should not match 1.1.1.1")
	}
	if !g.MatchIP(netip.MustParseAddr("2001:4860::8888")) {
		t.Error("should match 2001:4860::8888")
	}

	tg := &bundle.Sets[1]
	if tg.Name != "telegram" {
		t.Errorf("set 1 name = %q", tg.Name)
	}
	if !tg.MatchDomain("t.me") {
		t.Error("should match t.me")
	}
	if !tg.MatchIP(netip.MustParseAddr("149.154.167.50")) {
		t.Error("should match 149.154.167.50")
	}
}

func TestFormatEmpty(t *testing.T) {
	var buf bytes.Buffer
	WriteBundle(&buf, nil)
	bundle, err := ReadBundle(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if len(bundle.Sets) != 0 {
		t.Errorf("expected 0 sets, got %d", len(bundle.Sets))
	}
}

func TestFormatDomainsOnly(t *testing.T) {
	sets := []BundleSet{{Name: "discord", DomainSuffixes: []string{"discord.com"}}}
	var buf bytes.Buffer
	WriteBundle(&buf, sets)
	bundle, _ := ReadBundle(buf.Bytes())
	if !bundle.Sets[0].MatchDomain("discord.com") {
		t.Error("should match domain")
	}
	if bundle.Sets[0].MatchIP(netip.MustParseAddr("1.1.1.1")) {
		t.Error("should not match IP")
	}
}

func TestFormatIPsOnly(t *testing.T) {
	sets := []BundleSet{{Name: "cloudflare", CIDRs: []string{"104.16.0.0/12"}}}
	var buf bytes.Buffer
	WriteBundle(&buf, sets)
	bundle, _ := ReadBundle(buf.Bytes())
	if !bundle.Sets[0].MatchIP(netip.MustParseAddr("104.20.1.1")) {
		t.Error("should match IP")
	}
	if bundle.Sets[0].MatchDomain("cloudflare.com") {
		t.Error("should not match domain")
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `cd /Users/david/projects/kaitu-io/k2 && go test ./rule/ -v -run TestFormat 2>&1 | head -5`
Expected: compilation error

- [ ] **Step 3: Write format.go**

See spec for the complete k2b v1 binary layout. Key types:

```go
// BundleSet is input for WriteBundle and output of ReadBundle.
type BundleSet struct {
	Name           string   // set name ("google", "telegram", ...)
	DomainSuffixes []string // write: raw domains; read: unused (match via sections)
	CIDRs          []string // write: CIDR strings; read: unused
	domains        domainSection
	ipv4           ipv4Section
	ipv6           ipv6Section
}

func (s *BundleSet) MatchDomain(domain string) bool { return s.domains.Match(domain) }
func (s *BundleSet) MatchIP(addr netip.Addr) bool {
	if addr.Is4() { return s.ipv4.Contains(addr) }
	return s.ipv6.Contains(addr)
}

type Bundle struct {
	Sets []BundleSet
	data []byte
}
```

Full implementation: WriteBundle builds header+index+data sections. ReadBundle parses the index and creates domainSection/ipv4Section/ipv6Section backed by the raw data slice.

*(The complete code follows the spec's binary layout. Implementation is straightforward but ~150 lines — see Task 4 of the original plan for the full WriteBundle/ReadBundle code. The key change: buildIPv4Data/buildIPv6Data now return (data, count) instead of just data.)*

- [ ] **Step 4: Run tests**

Run: `cd /Users/david/projects/kaitu-io/k2 && go test ./rule/ -v -run TestFormat`
Expected: all PASS

- [ ] **Step 5: Commit**

```bash
git add rule/format.go rule/format_test.go
git commit -m "feat(rule): k2b v1 format reader/writer"
```

---

## Task 5: Loader (cache_dir → bundles → index)

**Files:**
- Create: `rule/loader.go`
- Create: `rule/loader_test.go`

- [ ] **Step 1: Write loader_test.go**

```go
package rule

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func writeTestBundle(t *testing.T, dir, filename string, sets []BundleSet) {
	t.Helper()
	var buf bytes.Buffer
	if err := WriteBundle(&buf, sets); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, filename), buf.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadEmpty(t *testing.T) {
	dir := t.TempDir()
	bundles, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(bundles) != 0 {
		t.Errorf("expected 0 bundles, got %d", len(bundles))
	}
}

func TestLoadNonexistent(t *testing.T) {
	bundles, err := Load("/nonexistent/path")
	if err != nil {
		t.Fatal("should not error on nonexistent dir")
	}
	if bundles != nil {
		t.Error("should return nil")
	}
}

func TestLoadBundles(t *testing.T) {
	dir := t.TempDir()
	writeTestBundle(t, dir, "overseas.k2b", []BundleSet{
		{Name: "google", DomainSuffixes: []string{"google.com"}, CIDRs: []string{"8.8.8.0/24"}},
		{Name: "telegram", DomainSuffixes: []string{"t.me"}},
	})
	writeTestBundle(t, dir, "cn.k2b", []BundleSet{
		{Name: "geoip-cn", CIDRs: []string{"114.114.114.0/24"}},
	})
	// non-k2b file should be ignored
	os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("hi"), 0644)

	bundles, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(bundles) != 2 {
		t.Fatalf("expected 2 bundles, got %d", len(bundles))
	}

	idx := Index(bundles)
	if _, ok := idx["google"]; !ok {
		t.Error("index should contain google")
	}
	if _, ok := idx["telegram"]; !ok {
		t.Error("index should contain telegram")
	}
	if _, ok := idx["geoip-cn"]; !ok {
		t.Error("index should contain geoip-cn")
	}
	if _, ok := idx["nonexistent"]; ok {
		t.Error("index should not contain nonexistent")
	}
}

func TestLoadSkipsCorrupt(t *testing.T) {
	dir := t.TempDir()
	// Valid bundle
	writeTestBundle(t, dir, "good.k2b", []BundleSet{{Name: "google"}})
	// Corrupt bundle
	os.WriteFile(filepath.Join(dir, "bad.k2b"), []byte("not a k2b file"), 0644)

	bundles, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(bundles) != 1 {
		t.Fatalf("should load 1 good bundle, got %d", len(bundles))
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `cd /Users/david/projects/kaitu-io/k2 && go test ./rule/ -v -run TestLoad 2>&1 | head -5`
Expected: compilation error — `Load`, `Index` undefined

- [ ] **Step 3: Write loader.go**

```go
package rule

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// Load reads all .k2b bundle files from cacheDir.
// Returns nil (not error) if dir is empty or doesn't exist.
// Skips corrupt files with a warning log.
func Load(cacheDir string) ([]*Bundle, error) {
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var bundles []*Bundle
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".k2b") {
			continue
		}
		path := filepath.Join(cacheDir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			slog.Warn("rule: skip bundle", "file", e.Name(), "err", err)
			continue
		}
		b, err := ReadBundle(data)
		if err != nil {
			slog.Warn("rule: skip bundle", "file", e.Name(), "err", err)
			continue
		}
		bundles = append(bundles, b)
	}
	return bundles, nil
}

// Index builds a name→*BundleSet lookup from all loaded bundles.
// If the same name appears in multiple bundles, the last one wins.
func Index(bundles []*Bundle) map[string]*BundleSet {
	idx := make(map[string]*BundleSet)
	for _, b := range bundles {
		for i := range b.Sets {
			idx[b.Sets[i].Name] = &b.Sets[i]
		}
	}
	return idx
}
```

- [ ] **Step 4: Run tests**

Run: `cd /Users/david/projects/kaitu-io/k2 && go test ./rule/ -v -run TestLoad`
Expected: all PASS

- [ ] **Step 5: Commit**

```bash
git add rule/loader.go rule/loader_test.go
git commit -m "feat(rule): loader — read .k2b bundles from cache_dir, build index"
```

---

## Task 6: Engine (Match + SetTmpRule + routes)

**Files:**
- Create: `rule/engine.go`
- Create: `rule/engine_test.go`

- [ ] **Step 1: Write engine_test.go**

```go
package rule

import (
	"bytes"
	"net/netip"
	"sync"
	"testing"
)

func mustBundle(t *testing.T, sets []BundleSet) *Bundle {
	t.Helper()
	var buf bytes.Buffer
	if err := WriteBundle(&buf, sets); err != nil {
		t.Fatal(err)
	}
	b, err := ReadBundle(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestEngineMatchDomain(t *testing.T) {
	b := mustBundle(t, []BundleSet{
		{Name: "google", DomainSuffixes: []string{"google.com", "youtube.com"}, CIDRs: []string{"8.8.8.0/24"}},
		{Name: "telegram", DomainSuffixes: []string{"t.me"}, CIDRs: []string{"149.154.160.0/20"}},
	})
	idx := Index([]*Bundle{b})

	// Route 0: google+telegram → proxy
	eng := NewEngine([]RouteEntry{
		{Target: TargetProxy, Sets: bindSets(idx, "google", "telegram")},
	}, TargetDirect, []*Bundle{b}, false)

	tests := []struct {
		host string
		want Target
	}{
		{"www.google.com", TargetProxy},
		{"youtube.com", TargetProxy},
		{"t.me", TargetProxy},
		{"example.com", TargetDirect}, // fallback
	}
	for _, tt := range tests {
		if got := eng.Match(tt.host); got != tt.want {
			t.Errorf("Match(%q) = %v, want %v", tt.host, got, tt.want)
		}
	}
}

func TestEngineMatchIP(t *testing.T) {
	b := mustBundle(t, []BundleSet{
		{Name: "google", CIDRs: []string{"8.8.8.0/24", "2001:4860::/32"}},
		{Name: "geoip-cn", CIDRs: []string{"114.114.114.0/24"}},
	})
	idx := Index([]*Bundle{b})

	eng := NewEngine([]RouteEntry{
		{Target: TargetProxy, Sets: bindSets(idx, "google")},
		{Target: TargetDirect, Sets: bindSets(idx, "geoip-cn")},
	}, TargetDirect, []*Bundle{b}, false)

	tests := []struct {
		host string
		want Target
	}{
		{"8.8.8.8", TargetProxy},
		{"2001:4860::1", TargetProxy},
		{"114.114.114.114", TargetDirect},
		{"192.168.1.1", TargetDirect}, // fallback
	}
	for _, tt := range tests {
		if got := eng.Match(tt.host); got != tt.want {
			t.Errorf("Match(%q) = %v, want %v", tt.host, got, tt.want)
		}
	}
}

func TestEngineSetTmpRule(t *testing.T) {
	eng := NewEngine(nil, TargetDirect, nil, false)

	if got := eng.Match("192.168.1.1"); got != TargetDirect {
		t.Fatalf("before: got %v", got)
	}
	eng.SetTmpRule("192.168.1.1", TargetProxy)
	if got := eng.Match("192.168.1.1"); got != TargetProxy {
		t.Fatalf("after: got %v", got)
	}
}

func TestEngineTmpRulePriority(t *testing.T) {
	b := mustBundle(t, []BundleSet{
		{Name: "google", CIDRs: []string{"8.8.8.0/24"}},
	})
	idx := Index([]*Bundle{b})

	eng := NewEngine([]RouteEntry{
		{Target: TargetProxy, Sets: bindSets(idx, "google")},
	}, TargetDirect, []*Bundle{b}, false)

	// Static rule says proxy, TmpRule overrides to direct
	eng.SetTmpRule("8.8.8.8", TargetDirect)
	if got := eng.Match("8.8.8.8"); got != TargetDirect {
		t.Fatalf("TmpRule should win: got %v", got)
	}
}

func TestEngineRouteOrder(t *testing.T) {
	b := mustBundle(t, []BundleSet{
		{Name: "google", DomainSuffixes: []string{"google.com"}},
		{Name: "block", DomainSuffixes: []string{"google.com"}}, // same domain, different route
	})
	idx := Index([]*Bundle{b})

	// Route 0: google→proxy, Route 1: block→reject
	// First match wins, so google.com → proxy
	eng := NewEngine([]RouteEntry{
		{Target: TargetProxy, Sets: bindSets(idx, "google")},
		{Target: TargetReject, Sets: bindSets(idx, "block")},
	}, TargetDirect, []*Bundle{b}, false)

	if got := eng.Match("google.com"); got != TargetProxy {
		t.Errorf("first route should win: got %v", got)
	}
}

func TestEngineConcurrency(t *testing.T) {
	b := mustBundle(t, []BundleSet{
		{Name: "google", DomainSuffixes: []string{"google.com"}, CIDRs: []string{"8.8.8.0/24"}},
	})
	idx := Index([]*Bundle{b})

	eng := NewEngine([]RouteEntry{
		{Target: TargetProxy, Sets: bindSets(idx, "google")},
	}, TargetDirect, []*Bundle{b}, false)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); eng.Match("www.google.com") }()
		go func() { defer wg.Done(); eng.SetTmpRule("10.0.0.1", TargetProxy) }()
	}
	wg.Wait()
}

func TestEngineIsAllProxy(t *testing.T) {
	eng := NewEngine(nil, TargetProxy, nil, true)
	if !eng.IsAllProxy() {
		t.Error("should be all-proxy")
	}
	eng2 := NewEngine(nil, TargetDirect, nil, false)
	if eng2.IsAllProxy() {
		t.Error("should not be all-proxy")
	}
}

// bindSets is a test helper to look up BundleSets by name from an index.
func bindSets(idx map[string]*BundleSet, names ...string) []*BundleSet {
	var sets []*BundleSet
	for _, n := range names {
		if s, ok := idx[n]; ok {
			sets = append(sets, s)
		}
	}
	return sets
}
```

- [ ] **Step 2: Run to verify failure**

Run: `cd /Users/david/projects/kaitu-io/k2 && go test ./rule/ -v -run TestEngine 2>&1 | head -5`
Expected: compilation error

- [ ] **Step 3: Write engine.go**

```go
package rule

import (
	"net/netip"
	"strings"

	"github.com/sasha-s/go-deadlock"
)

// RouteEntry represents one route in the routing table.
// Each entry has a target and a set of rule sets to match against.
type RouteEntry struct {
	Target Target
	Sets   []*BundleSet
}

// Engine is the multi-ruleset matching engine.
// Thread-safe: Match uses RLock, SetTmpRule uses WLock.
// Lock ordering: Engine.mu is standalone — never nested inside any other lock.
type Engine struct {
	mu         deadlock.RWMutex
	tmp        map[netip.Addr]Target
	routes     []RouteEntry
	fallback   Target
	allProxy   bool     // match:all mode (disables sniffing)
	bundles    []*Bundle // lifecycle: Close munmaps all
}

// NewEngine creates a routing engine from route entries.
func NewEngine(routes []RouteEntry, fallback Target, bundles []*Bundle, allProxy bool) *Engine {
	return &Engine{
		tmp:      make(map[netip.Addr]Target),
		routes:   routes,
		fallback: fallback,
		allProxy: allProxy,
		bundles:  bundles,
	}
}

// Match returns the routing target for a host (IP or domain).
func (e *Engine) Match(host string) Target {
	e.mu.RLock()
	defer e.mu.RUnlock()

	addr, err := netip.ParseAddr(host)
	if err == nil {
		// IP path
		if t, ok := e.tmp[addr]; ok {
			return t
		}
		for i := range e.routes {
			for j := range e.routes[i].Sets {
				if e.routes[i].Sets[j].MatchIP(addr) {
					return e.routes[i].Target
				}
			}
		}
	} else {
		// Domain path
		domain := strings.ToLower(host)
		for i := range e.routes {
			for j := range e.routes[i].Sets {
				if e.routes[i].Sets[j].MatchDomain(domain) {
					return e.routes[i].Target
				}
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

// IsAllProxy returns true if the engine is in match-all proxy mode.
// Used by tunnel to disable SNI sniffing.
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

- [ ] **Step 4: Run all rule/ tests**

Run: `cd /Users/david/projects/kaitu-io/k2 && go test ./rule/ -v -race -count=1`
Expected: all PASS

- [ ] **Step 5: Commit**

```bash
git add rule/engine.go rule/engine_test.go
git commit -m "feat(rule): Engine with route-based matching, SetTmpRule, concurrency"
```

---

## Task 7: Integration — config/config.go

**Files:**
- Modify: `config/config.go`
- Modify: `config/config_test.go`

- [ ] **Step 1: Replace Server + RuleConfig with Routes**

In `config/config.go`:
- Delete `Server` field from ClientConfig
- Delete `RuleConfig` struct and `Rule` field
- Add `Routes` and `RouteConfig`/`MatchConfig` (from spec)
- Add `CacheDir` as top-level field (was inside RuleConfig)
- Add `ServerURL()` method to extract first k2v5:// from routes
- Update `SetDefaults` to handle routes
- Update `ClientFromURL` to create a single-route config

- [ ] **Step 2: Fix config_test.go**

Update all references to `cfg.Server` → `cfg.ServerURL()` and `cfg.Rule.Global` → route-based checks.

- [ ] **Step 3: Run config tests**

Run: `cd /Users/david/projects/kaitu-io/k2 && go test ./config/ -v`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add config/config.go config/config_test.go
git commit -m "refactor(config): replace Server+Rule with Routes"
```

---

## Task 8: Integration — core/ (router + tunnel)

**Files:**
- Modify: `core/router.go`
- Modify: `core/tunnel.go`
- Modify: `core/tunnel_test.go`

- [ ] **Step 1: Rewrite router.go**

```go
package core

import (
	"github.com/kaitu-io/k2/core/packet"
	"github.com/kaitu-io/k2/rule"
)

type Router struct {
	engine *rule.Engine
}

func NewRouter(engine *rule.Engine) *Router {
	return &Router{engine: engine}
}

func (r *Router) Route(info packet.Info) rule.Target {
	return r.engine.Match(info.DstIP)
}

func (r *Router) RouteAddr(host string) rule.Target {
	return r.engine.Match(host)
}

func (r *Router) SetTmpRule(ip string, target rule.Target) {
	r.engine.SetTmpRule(ip, target)
}
```

- [ ] **Step 2: Update tunnel.go**

Replace import `k2rule` → `rule`. Replace all `k2rule.Target*` → `rule.Target*`. Replace `k2rule.SetTmpRule(host, ...)` → `t.router.SetTmpRule(host, ...)` (3 sites). Update `route()` method return type.

- [ ] **Step 3: Update tunnel_test.go**

Replace `&Router{}` with `NewRouter(rule.NewEngine(nil, rule.TargetDirect, nil, false))`.

- [ ] **Step 4: Run core/ tests**

Run: `cd /Users/david/projects/kaitu-io/k2 && go test ./core/ -v -short`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add core/router.go core/tunnel.go core/tunnel_test.go
git commit -m "refactor(core): replace k2rule with rule.Engine"
```

---

## Task 9: Integration — core/dns/middleware.go

**Files:**
- Modify: `core/dns/middleware.go`
- Modify: `core/dns/middleware_test.go`

- [ ] **Step 1: Inject match/setTmpRule functions**

Replace import `k2rule` → `rule`. Add fields to Middleware and MiddlewareConfig:

```go
MatchRule  func(string) rule.Target
SetTmpRule func(string, rule.Target)
```

Replace all 7 call sites:
- `k2rule.Match(domain)` → `m.matchRule(domain)` (2 sites: line ~183, ~200)
- `k2rule.SetTmpRule(ip, action)` → `m.setTmpRule(ip, action)` (2 sites: line ~265, ~309)
- `k2rule.Target` → `rule.Target` (type references in signatures)
- `k2rule.TargetReject` → `rule.TargetReject`
- `k2rule.TargetDirect/Proxy` → `rule.TargetDirect/Proxy`

- [ ] **Step 2: Update middleware_test.go**

Delete `initK2Rule` function. Replace with injected functions:

```go
func alwaysProxy(string) rule.Target  { return rule.TargetProxy }
func alwaysDirect(string) rule.Target { return rule.TargetDirect }
func nopSetTmp(string, rule.Target)   {}
```

Update all test MiddlewareConfig to include `MatchRule` and `SetTmpRule`.

- [ ] **Step 3: Run dns tests**

Run: `cd /Users/david/projects/kaitu-io/k2 && go test ./core/dns/ -v -short`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add core/dns/middleware.go core/dns/middleware_test.go
git commit -m "refactor(dns): inject rule functions, remove k2rule import"
```

---

## Task 10: Integration — engine/engine.go

**Files:**
- Modify: `engine/engine.go`
- Modify: `engine/engine_test.go`
- Modify: `go.mod`

- [ ] **Step 1: Replace k2rule in engine.go**

Remove `k2rule` import, add `rule` import. Add `ruleEngine *rule.Engine` field.

Replace step 1 in Start():
```go
// 1. Rule engine.
serverURL := extractServerURL(client.Routes)
if serverURL == "" {
    return e.fail(fmt.Errorf("engine: no server URL in routes"))
}
ruleEngine := buildRuleEngine(client.Routes, client.CacheDir)
```

Add `extractServerURL`:
```go
func extractServerURL(routes []config.RouteConfig) string {
    for _, r := range routes {
        if strings.HasPrefix(r.Via, "k2v5://") {
            return r.Via
        }
    }
    return ""
}
```

Add `buildRuleEngine`:
```go
func buildRuleEngine(routes []config.RouteConfig, cacheDir string) *rule.Engine {
    bundles, err := rule.Load(cacheDir)
    if err != nil {
        slog.Warn("engine: rule load failed", "err", err)
    }
    idx := rule.Index(bundles)

    var entries []rule.RouteEntry
    var fallback rule.Target = rule.TargetDirect
    allProxy := false

    for i, route := range routes {
        target := rule.ParseVia(route.Via)

        if route.Match.All {
            allProxy = (target == rule.TargetProxy)
            fallback = target
            break
        }

        names := rule.ExpandMatch(route.Match)
        var sets []*rule.BundleSet
        for _, name := range names {
            if bs, ok := idx[name]; ok {
                sets = append(sets, bs)
            }
        }
        if len(sets) > 0 {
            entries = append(entries, rule.RouteEntry{Target: target, Sets: sets})
        }

        // Last route without criteria is fallback
        if i == len(routes)-1 && !route.Match.HasCriteria() {
            fallback = target
        }
    }

    return rule.NewEngine(entries, fallback, bundles, allProxy)
}
```

Replace `client.Server` → `serverURL` in wire.ParseURL and all logging.

Replace `client.Rule.Global` with `ruleEngine.IsAllProxy()` for sniff flags:
```go
SniffBT:  !ruleEngine.IsAllProxy(),
SniffSNI: !ruleEngine.IsAllProxy(),
```

Wire DNS middleware:
```go
MatchRule:  ruleEngine.Match,
SetTmpRule: ruleEngine.SetTmpRule,
```

In Stop(), extract-nil-close ruleEngine:
```go
re := e.ruleEngine
e.ruleEngine = nil
// ... (after other close calls)
if re != nil { re.Close() }
```

In commit block: `e.ruleEngine = ruleEngine`

- [ ] **Step 2: Update engine_test.go**

Remove k2rule import and usage. Update tests that reference `client.Server` or `client.Rule`.

- [ ] **Step 3: Remove k2rule from go.mod**

Run: `cd /Users/david/projects/kaitu-io/k2 && go mod tidy`

- [ ] **Step 4: Full test gate**

Run: `cd /Users/david/projects/kaitu-io/k2 && go test -short -race -timeout 120s ./...`
Expected: all PASS

- [ ] **Step 5: Verify no k2rule references**

Run: `grep -r "k2rule" --include="*.go" /Users/david/projects/kaitu-io/k2/`
Expected: no output

- [ ] **Step 6: Commit**

```bash
git add engine/ go.mod go.sum
git commit -m "refactor(engine): routes-based config, rule.Engine, remove k2rule"
```

---

## Task 11: Final test gate + CLAUDE.md

**Files:**
- Modify: `CLAUDE.md`

- [ ] **Step 1: Full test + vet + build**

Run: `cd /Users/david/projects/kaitu-io/k2 && go vet ./... && go build ./cmd/k2 && go build ./cmd/k2s && go test -short -race -timeout 120s ./...`
Expected: all clean

- [ ] **Step 2: Update CLAUDE.md**

- Replace k2rule dependency with `rule/ (in-tree)`
- Update Lock Ordering Graph: add `rule.Engine.mu (standalone RWMutex)`
- Update config format documentation to show routes
- Update `k2 setup` example

- [ ] **Step 3: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: update CLAUDE.md for rule module + routes config"
```

---

## Phase 2 (separate plan)

1. `rule/mmap_unix.go` / `rule/mmap_windows.go` — platform mmap (replace os.ReadFile)
2. `rule/download.go` — HTTP manifest + bundle download to cache_dir
3. Embedded fallback bundle via `go:embed`
4. `k2 rule update` CLI command
5. `kaitu-io/k2-rules` data pipeline repository
6. Multi-server routing (multiple wire.Dialer per route)
