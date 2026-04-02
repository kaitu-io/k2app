# Rule Module Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace `k2rule` with an in-tree `rule/` package that supports per-service IP-CIDR + domain rule sets in a mmap-indexed binary format (k2b).

**Architecture:** New `rule/` package with 6 files: types (Target, Config), domain binary search (k2rule v3 algorithm), IP range binary search (O(log n) on sorted ranges), k2b format reader/writer, Engine (Match + SetTmpRule), and loader (manifest + HTTP download + mmap). Integration replaces all `k2rule.*` references across engine/, core/, core/dns/, config/.

**Tech Stack:** Go stdlib only for the rule package. `go4.org/netipx` (BSD-3) for the k2b compiler tool only (IP range merging). `deadlock.RWMutex` for concurrency.

**Spec:** `docs/superpowers/specs/2026-04-02-rule-module-design.md`

---

## File Structure

### New files (rule/ package)

| File | Responsibility |
|------|---------------|
| `rule/target.go` | `Target` type (Direct/Proxy/Reject), `Config` struct, preset expansion |
| `rule/domain.go` | `domainSection` — binary search on reversed sorted domains in `[]byte` slice |
| `rule/domain_test.go` | Domain matching tests (suffix, exact, edge cases) |
| `rule/ip.go` | `ipv4Section`, `ipv6Section` — binary search on sorted IP ranges in `[]byte` slice |
| `rule/ip_test.go` | IP matching tests (v4, v6, boundary, miss) |
| `rule/format.go` | k2b v1 reader/writer: `ReadBundle([]byte)`, `WriteBundle(io.Writer, []BundleSet)` |
| `rule/format_test.go` | Round-trip tests: write k2b → read → verify rule sets |
| `rule/engine.go` | `Engine` — Match, SetTmpRule, Close; holds mmap'd bundles + bound sets |
| `rule/engine_test.go` | Engine integration tests: priority chain, SetTmpRule override, concurrent access |

### Modified files (integration)

| File | Change |
|------|--------|
| `config/config.go` | Replace `RuleConfig` fields |
| `core/router.go` | Hold `*rule.Engine`, delegate to `engine.Match()` |
| `core/tunnel.go` | `k2rule.Target` → `rule.Target`, `k2rule.SetTmpRule` → method call (5 sites) |
| `core/dns/middleware.go` | `k2rule.Match` → injected func, `k2rule.SetTmpRule` → injected func (7 sites) |
| `core/dns/middleware_test.go` | Replace `initK2Rule` with `rule.Engine` setup |
| `core/tunnel_test.go` | Update Router construction |
| `engine/engine.go` | Build `rule.Engine` in Start(), Close in Stop(), remove `k2rule` import |
| `engine/engine_test.go` | Replace `k2rule.Init` with `rule.Engine` setup |
| `go.mod` | Remove `github.com/kaitu-io/k2rule` |

---

## Task 1: Target type + Config struct

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

func TestParseTarget(t *testing.T) {
	tests := []struct {
		input string
		want  Target
		ok    bool
	}{
		{"direct", TargetDirect, true},
		{"proxy", TargetProxy, true},
		{"reject", TargetReject, true},
		{"PROXY", TargetProxy, true},
		{"bogus", 0, false},
	}
	for _, tt := range tests {
		got, ok := ParseTarget(tt.input)
		if ok != tt.ok || (ok && got != tt.want) {
			t.Errorf("ParseTarget(%q) = (%v, %v), want (%v, %v)", tt.input, got, ok, tt.want, tt.ok)
		}
	}
}

func TestExpandPreset(t *testing.T) {
	sets := ExpandPreset("overseas")
	if len(sets) == 0 {
		t.Fatal("overseas preset should expand to non-empty set list")
	}
	// All overseas entries default to proxy
	for _, s := range sets {
		if s.Action != TargetProxy {
			t.Errorf("overseas preset %q should be proxy, got %v", s.Name, s.Action)
		}
	}
	// google must be present
	found := false
	for _, s := range sets {
		if s.Name == "google" {
			found = true
		}
	}
	if !found {
		t.Error("overseas preset must include google")
	}
}

func TestExpandPresetUnknown(t *testing.T) {
	sets := ExpandPreset("nonexistent")
	if sets != nil {
		t.Errorf("unknown preset should return nil, got %v", sets)
	}
}

func TestMergeSets(t *testing.T) {
	preset := []SetConfig{
		{Name: "google", Action: TargetProxy},
		{Name: "openai", Action: TargetProxy},
	}
	user := []SetConfig{
		{Name: "openai", Action: TargetReject},
		{Name: "my-corp", Action: TargetDirect},
	}
	merged := MergeSets(preset, user)
	// openai should be overridden to reject
	for _, s := range merged {
		if s.Name == "openai" && s.Action != TargetReject {
			t.Errorf("openai should be reject after merge, got %v", s.Action)
		}
	}
	// my-corp should be appended
	found := false
	for _, s := range merged {
		if s.Name == "my-corp" {
			found = true
		}
	}
	if !found {
		t.Error("user-added my-corp should be in merged result")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/david/projects/kaitu-io/k2 && go test ./rule/ -v -run 'TestTarget|TestParse|TestExpand|TestMerge' 2>&1 | head -5`
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

// ParseTarget converts a string to Target. Case-insensitive.
func ParseTarget(s string) (Target, bool) {
	switch strings.ToLower(s) {
	case "direct":
		return TargetDirect, true
	case "proxy":
		return TargetProxy, true
	case "reject":
		return TargetReject, true
	default:
		return 0, false
	}
}

// SetConfig binds a rule set name to a routing action.
type SetConfig struct {
	Name   string `yaml:"name"   json:"name"`
	Action Target `yaml:"-"      json:"-"`
	// ActionStr is the YAML/JSON serialization field for Action.
	ActionStr string `yaml:"action" json:"action"`
	URL        string `yaml:"url"    json:"url,omitempty"` // custom rule set URL (optional)
}

// Config is the rule engine configuration (from client YAML).
type Config struct {
	Preset   string      `yaml:"preset"    json:"preset"`
	CnDirect bool        `yaml:"cn_direct" json:"cn_direct"`
	Default  string      `yaml:"default"   json:"default"` // "direct" or "proxy"
	Sets     []SetConfig `yaml:"sets"      json:"sets"`
	CacheDir string      `yaml:"cache_dir" json:"cache_dir"`
}

// presets maps preset name to the set list it expands to.
var presets = map[string][]SetConfig{
	"overseas": {
		{Name: "google", Action: TargetProxy},
		{Name: "facebook", Action: TargetProxy},
		{Name: "telegram", Action: TargetProxy},
		{Name: "twitter", Action: TargetProxy},
		{Name: "netflix", Action: TargetProxy},
		{Name: "discord", Action: TargetProxy},
		{Name: "spotify", Action: TargetProxy},
		{Name: "tiktok", Action: TargetProxy},
		{Name: "openai", Action: TargetProxy},
		{Name: "github", Action: TargetProxy},
		{Name: "cloudflare", Action: TargetProxy},
		{Name: "fastly", Action: TargetProxy},
	},
	"cn-access": {
		{Name: "cn-sites", Action: TargetProxy},
		{Name: "bilibili", Action: TargetProxy},
		{Name: "geoip-cn", Action: TargetProxy},
	},
}

// ExpandPreset returns the set list for a preset name, or nil if unknown.
func ExpandPreset(name string) []SetConfig {
	sets, ok := presets[strings.ToLower(name)]
	if !ok {
		return nil
	}
	// Return a copy to avoid mutation.
	out := make([]SetConfig, len(sets))
	copy(out, sets)
	return out
}

// MergeSets merges preset sets with user sets. User entries with the same
// name override the preset entry. User entries with new names are appended.
func MergeSets(preset, user []SetConfig) []SetConfig {
	result := make([]SetConfig, len(preset))
	copy(result, preset)

	for _, u := range user {
		found := false
		for i := range result {
			if result[i].Name == u.Name {
				result[i] = u
				found = true
				break
			}
		}
		if !found {
			result = append(result, u)
		}
	}
	return result
}
```

- [ ] **Step 4: Run tests**

Run: `cd /Users/david/projects/kaitu-io/k2 && go test ./rule/ -v -run 'TestTarget|TestParse|TestExpand|TestMerge'`
Expected: all PASS

- [ ] **Step 5: Commit**

```bash
git add rule/target.go rule/target_test.go
git commit -m "feat(rule): add Target type, Config, preset expansion, set merging"
```

---

## Task 2: Domain binary search (mmap-compatible)

**Files:**
- Create: `rule/domain.go`
- Create: `rule/domain_test.go`

**Algorithm:** Reuses k2rule v3's approach. Domains stored as reversed, lowercased, dot-prefixed strings, sorted lexicographically. Query generates all suffixes, reverses each, binary searches for exact match. All operations work directly on a `[]byte` slice (mmap-friendly, zero heap allocation in the hot path).

- [ ] **Step 1: Write domain_test.go**

```go
package rule

import (
	"encoding/binary"
	"testing"
)

// buildDomainSection builds a domain section byte slice from a list of suffix domains.
// This is the test helper that constructs the binary format the mmap reader expects.
func buildDomainSection(domains []string) []byte {
	reversed := make([]string, len(domains))
	for i, d := range domains {
		reversed[i] = reverseString("." + d)
	}
	// Sort — import sort in test
	sortStrings(reversed)

	// Build: count(4) + offsets(count+1)*4 + strings
	var strings []byte
	offsets := make([]uint32, len(reversed)+1)
	for i, s := range reversed {
		offsets[i] = uint32(len(strings))
		strings = append(strings, []byte(s)...)
	}
	offsets[len(reversed)] = uint32(len(strings)) // sentinel

	buf := make([]byte, 4+(len(reversed)+1)*4+len(strings))
	binary.LittleEndian.PutUint32(buf[0:4], uint32(len(reversed)))
	for i, off := range offsets {
		binary.LittleEndian.PutUint32(buf[4+i*4:4+i*4+4], off)
	}
	copy(buf[4+(len(reversed)+1)*4:], strings)
	return buf
}

func TestDomainMatchSuffix(t *testing.T) {
	data := buildDomainSection([]string{"google.com", "youtube.com"})
	ds := domainSection{data: data, count: 2}

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
	ds := domainSection{data: nil, count: 0}
	if ds.Match("anything.com") {
		t.Error("empty domain section should not match anything")
	}
}

func TestDomainMatchCaseInsensitive(t *testing.T) {
	data := buildDomainSection([]string{"google.com"})
	ds := domainSection{data: data, count: 2}
	// Count is wrong — recalculate from data
	ds.count = int(binary.LittleEndian.Uint32(data[0:4]))

	if !ds.Match("GOOGLE.COM") {
		t.Error("domain match should be case-insensitive")
	}
	if !ds.Match("Google.Com") {
		t.Error("domain match should be case-insensitive (mixed)")
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
// The byte slice layout matches k2rule v3:
//
//	count      (4 bytes LE)
//	offsets[0..count] (4 bytes LE each, count+1 entries including sentinel)
//	strings area (reversed, lowercased, dot-prefixed domains, sorted)
//
// All operations work directly on the slice — suitable for mmap.
type domainSection struct {
	data  []byte
	count int
}

// Match returns true if domain matches any entry (suffix match).
// Case-insensitive. Zero heap allocation in the hot path (uses stack buffer).
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

// bsearch performs binary search for exact match of target in the sorted strings area.
func (ds *domainSection) bsearch(target string) bool {
	idx := sort.Search(ds.count, func(j int) bool {
		return ds.getDomainAt(j) >= target
	})
	return idx < ds.count && ds.getDomainAt(idx) == target
}

// getDomainAt returns the domain string at index i from the sorted strings area.
func (ds *domainSection) getDomainAt(i int) string {
	offsetsStart := 4 // after count
	stringsStart := offsetsStart + (ds.count+1)*4

	off := int(binary.LittleEndian.Uint32(ds.data[offsetsStart+i*4 : offsetsStart+i*4+4]))
	nextOff := int(binary.LittleEndian.Uint32(ds.data[offsetsStart+(i+1)*4 : offsetsStart+(i+1)*4+4]))

	return string(ds.data[stringsStart+off : stringsStart+nextOff])
}

// reverseString reverses a string byte-by-byte (ASCII-safe for domain names).
func reverseString(s string) string {
	b := []byte(s)
	for i, j := 0, len(b)-1; i < j; i, j = i+1, j-1 {
		b[i], b[j] = b[j], b[i]
	}
	return string(b)
}

// sortStrings sorts a string slice in place (used by test helper and writer).
func sortStrings(ss []string) {
	sort.Strings(ss)
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

## Task 3: IP range binary search (mmap-compatible)

**Files:**
- Create: `rule/ip.go`
- Create: `rule/ip_test.go`

**Algorithm:** IPv4 ranges stored as `(from[4], to[4])` pairs, sorted by `from`. Binary search finds largest `from <= target`, then checks `target <= to`. O(log n), zero allocation.

- [ ] **Step 1: Write ip_test.go**

```go
package rule

import (
	"net/netip"
	"testing"
)

// buildIPv4Section builds an IPv4 section from CIDR strings.
// Merges overlapping ranges and sorts.
func buildIPv4Section(cidrs []string) []byte {
	ranges := cidrsToRanges4(cidrs)
	buf := make([]byte, len(ranges)*8)
	for i, r := range ranges {
		copy(buf[i*8:i*8+4], r.from[:])
		copy(buf[i*8+4:i*8+8], r.to[:])
	}
	return buf
}

func TestIPv4Match(t *testing.T) {
	// 8.8.8.0/24 and 1.1.1.0/24
	data := buildIPv4Section([]string{"8.8.8.0/24", "1.1.1.0/24"})
	sec := ipv4Section{data: data, count: 2}

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

func TestIPv4MatchEmpty(t *testing.T) {
	sec := ipv4Section{data: nil, count: 0}
	addr := netip.MustParseAddr("8.8.8.8")
	if sec.Contains(addr) {
		t.Error("empty section should not match")
	}
}

func TestIPv4AdjacentRanges(t *testing.T) {
	// Two adjacent /24s: 10.0.0.0/24 and 10.0.1.0/24
	data := buildIPv4Section([]string{"10.0.0.0/24", "10.0.1.0/24"})
	sec := ipv4Section{data: data, count: 2}

	if !sec.Contains(netip.MustParseAddr("10.0.0.128")) {
		t.Error("should match first range")
	}
	if !sec.Contains(netip.MustParseAddr("10.0.1.128")) {
		t.Error("should match second range")
	}
	if sec.Contains(netip.MustParseAddr("10.0.2.0")) {
		t.Error("should not match outside ranges")
	}
}

func TestIPv6Match(t *testing.T) {
	data := buildIPv6Section([]string{"2001:4860::/32"})
	sec := ipv6Section{data: data, count: 1}

	if !sec.Contains(netip.MustParseAddr("2001:4860::8888")) {
		t.Error("should match inside /32")
	}
	if !sec.Contains(netip.MustParseAddr("2001:4860:4860::8888")) {
		t.Error("should match inside /32")
	}
	if sec.Contains(netip.MustParseAddr("2001:4861::1")) {
		t.Error("should not match outside /32")
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `cd /Users/david/projects/kaitu-io/k2 && go test ./rule/ -v -run TestIPv 2>&1 | head -5`
Expected: compilation error — `ipv4Section` undefined

- [ ] **Step 3: Write ip.go**

```go
package rule

import (
	"bytes"
	"net/netip"
	"sort"
)

// ipv4Range is a from-to pair used during construction.
type ipv4Range struct {
	from [4]byte
	to   [4]byte
}

// ipv6Range is a from-to pair used during construction.
type ipv6Range struct {
	from [16]byte
	to   [16]byte
}

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

	// Binary search: find rightmost range where from <= raw.
	idx := sort.Search(s.count, func(i int) bool {
		from := s.data[i*8 : i*8+4]
		return bytes.Compare(from, raw[:]) > 0
	}) - 1

	if idx < 0 {
		return false
	}
	to := s.data[idx*8+4 : idx*8+8]
	return bytes.Compare(raw[:], to) <= 0
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
		from := s.data[i*32 : i*32+16]
		return bytes.Compare(from, raw[:]) > 0
	}) - 1

	if idx < 0 {
		return false
	}
	to := s.data[idx*32+16 : idx*32+32]
	return bytes.Compare(raw[:], to) <= 0
}

// cidrsToRanges4 converts CIDR strings to sorted, non-overlapping IPv4 ranges.
func cidrsToRanges4(cidrs []string) []ipv4Range {
	var ranges []ipv4Range
	for _, cidr := range cidrs {
		prefix, err := netip.ParsePrefix(cidr)
		if err != nil || !prefix.Addr().Is4() {
			continue
		}
		from := prefix.Masked().Addr().As4()
		// Compute "to" address: set host bits to 1
		to := from
		bits := prefix.Bits()
		for i := bits; i < 32; i++ {
			to[i/8] |= 1 << (7 - i%8)
		}
		ranges = append(ranges, ipv4Range{from: from, to: to})
	}
	// Sort by from
	sort.Slice(ranges, func(i, j int) bool {
		return bytes.Compare(ranges[i].from[:], ranges[j].from[:]) < 0
	})
	// Merge overlapping
	if len(ranges) == 0 {
		return ranges
	}
	merged := []ipv4Range{ranges[0]}
	for _, r := range ranges[1:] {
		last := &merged[len(merged)-1]
		// Overlapping or adjacent: last.to+1 >= r.from
		if bytes.Compare(r.from[:], last.to[:]) <= 0 || adjacent4(last.to, r.from) {
			if bytes.Compare(r.to[:], last.to[:]) > 0 {
				last.to = r.to
			}
		} else {
			merged = append(merged, r)
		}
	}
	return merged
}

func adjacent4(a, b [4]byte) bool {
	// a+1 == b
	carry := uint16(1)
	for i := 3; i >= 0; i-- {
		sum := uint16(a[i]) + carry
		if byte(sum) != b[i] {
			return false
		}
		carry = sum >> 8
	}
	return true
}

// cidrsToRanges6 converts CIDR strings to sorted, non-overlapping IPv6 ranges.
func cidrsToRanges6(cidrs []string) []ipv6Range {
	var ranges []ipv6Range
	for _, cidr := range cidrs {
		prefix, err := netip.ParsePrefix(cidr)
		if err != nil || !prefix.Addr().Is6() {
			continue
		}
		from := prefix.Masked().Addr().As16()
		to := from
		bits := prefix.Bits()
		for i := bits; i < 128; i++ {
			to[i/8] |= 1 << (7 - i%8)
		}
		ranges = append(ranges, ipv6Range{from: from, to: to})
	}
	sort.Slice(ranges, func(i, j int) bool {
		return bytes.Compare(ranges[i].from[:], ranges[j].from[:]) < 0
	})
	if len(ranges) == 0 {
		return ranges
	}
	merged := []ipv6Range{ranges[0]}
	for _, r := range ranges[1:] {
		last := &merged[len(merged)-1]
		if bytes.Compare(r.from[:], last.to[:]) <= 0 {
			if bytes.Compare(r.to[:], last.to[:]) > 0 {
				last.to = r.to
			}
		} else {
			merged = append(merged, r)
		}
	}
	return merged
}

// buildIPv6Section builds an IPv6 section from CIDR strings (used by tests and writer).
func buildIPv6Section(cidrs []string) []byte {
	ranges := cidrsToRanges6(cidrs)
	buf := make([]byte, len(ranges)*32)
	for i, r := range ranges {
		copy(buf[i*32:i*32+16], r.from[:])
		copy(buf[i*32+16:i*32+32], r.to[:])
	}
	return buf
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

**Format:** See spec — 32-byte header + 28-byte index per set + data sections (names, domains, ipv4, ipv6). All little-endian, uint32-aligned for mmap.

- [ ] **Step 1: Write format_test.go**

```go
package rule

import (
	"bytes"
	"testing"
)

func TestFormatRoundTrip(t *testing.T) {
	sets := []BundleSet{
		{
			Name:          "google",
			DomainSuffixes: []string{"google.com", "youtube.com"},
			CIDRs:          []string{"8.8.8.0/24", "2001:4860::/32"},
		},
		{
			Name:          "telegram",
			DomainSuffixes: []string{"telegram.org", "t.me"},
			CIDRs:          []string{"149.154.160.0/20"},
		},
	}

	var buf bytes.Buffer
	if err := WriteBundle(&buf, sets); err != nil {
		t.Fatalf("WriteBundle: %v", err)
	}

	data := buf.Bytes()

	// Verify magic
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

	// Verify google domains
	g := bundle.Sets[0]
	if g.Name != "google" {
		t.Errorf("set 0 name = %q, want google", g.Name)
	}
	if !g.MatchDomain("www.google.com") {
		t.Error("google set should match www.google.com")
	}
	if !g.MatchDomain("youtube.com") {
		t.Error("google set should match youtube.com")
	}
	if g.MatchDomain("example.com") {
		t.Error("google set should not match example.com")
	}

	// Verify google IPs
	if !g.MatchIP(mustParseAddr("8.8.8.8")) {
		t.Error("google set should match 8.8.8.8")
	}
	if g.MatchIP(mustParseAddr("1.1.1.1")) {
		t.Error("google set should not match 1.1.1.1")
	}
	if !g.MatchIP(mustParseAddr("2001:4860::8888")) {
		t.Error("google set should match 2001:4860::8888")
	}

	// Verify telegram
	tg := bundle.Sets[1]
	if tg.Name != "telegram" {
		t.Errorf("set 1 name = %q, want telegram", tg.Name)
	}
	if !tg.MatchDomain("t.me") {
		t.Error("telegram set should match t.me")
	}
	if !tg.MatchIP(mustParseAddr("149.154.167.50")) {
		t.Error("telegram set should match 149.154.167.50")
	}
}

func TestFormatEmpty(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteBundle(&buf, nil); err != nil {
		t.Fatalf("WriteBundle empty: %v", err)
	}
	bundle, err := ReadBundle(buf.Bytes())
	if err != nil {
		t.Fatalf("ReadBundle empty: %v", err)
	}
	if len(bundle.Sets) != 0 {
		t.Errorf("expected 0 sets, got %d", len(bundle.Sets))
	}
}

func TestFormatDomainsOnly(t *testing.T) {
	sets := []BundleSet{
		{Name: "discord", DomainSuffixes: []string{"discord.com"}},
	}
	var buf bytes.Buffer
	if err := WriteBundle(&buf, sets); err != nil {
		t.Fatal(err)
	}
	bundle, err := ReadBundle(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if !bundle.Sets[0].MatchDomain("discord.com") {
		t.Error("should match domain")
	}
	if bundle.Sets[0].MatchIP(mustParseAddr("1.1.1.1")) {
		t.Error("should not match any IP (no CIDRs)")
	}
}

func mustParseAddr(s string) netip.Addr {
	a, _ := netip.ParseAddr(s)
	return a
}
```

Note: `mustParseAddr` needs `import "net/netip"` — add it.

- [ ] **Step 2: Run to verify failure**

Run: `cd /Users/david/projects/kaitu-io/k2 && go test ./rule/ -v -run TestFormat 2>&1 | head -5`
Expected: compilation error — `BundleSet`, `WriteBundle`, `ReadBundle` undefined

- [ ] **Step 3: Write format.go**

```go
package rule

import (
	"encoding/binary"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
)

const (
	magic       = "K2RB"
	version     = 1
	headerSize  = 32
	indexSize   = 28 // per set
)

// BundleSet is the input for WriteBundle and the output of ReadBundle.
// For write: populate Name, DomainSuffixes, CIDRs.
// For read: use MatchDomain/MatchIP methods (backed by mmap-compatible sections).
type BundleSet struct {
	Name           string
	DomainSuffixes []string // write input: raw domains (e.g. "google.com")
	CIDRs          []string // write input: CIDR strings (e.g. "8.8.8.0/24")

	// Read-side fields (populated by ReadBundle, backed by data slice).
	domains domainSection
	ipv4    ipv4Section
	ipv6    ipv6Section
}

// MatchDomain tests if domain matches this set's domain rules.
func (s *BundleSet) MatchDomain(domain string) bool {
	return s.domains.Match(domain)
}

// MatchIP tests if addr matches this set's IP rules.
func (s *BundleSet) MatchIP(addr netip.Addr) bool {
	if addr.Is4() {
		return s.ipv4.Contains(addr)
	}
	return s.ipv6.Contains(addr)
}

// Bundle is the result of reading a k2b file.
type Bundle struct {
	Sets []BundleSet
	data []byte // the raw k2b bytes (mmap backing)
}

// WriteBundle writes a k2b v1 file. Sets are written in the order given.
func WriteBundle(w io.Writer, sets []BundleSet) error {
	// 1. Build all section data.
	type setData struct {
		name    []byte
		domains []byte
		ipv4    []byte
		ipv6    []byte
		ipv4Count int
		ipv6Count int
		domainCount int
	}
	built := make([]setData, len(sets))
	for i, s := range sets {
		built[i].name = []byte(s.Name)
		built[i].domains = buildDomainData(s.DomainSuffixes)
		built[i].domainCount = len(s.DomainSuffixes)

		v4ranges := cidrsToRanges4(s.CIDRs)
		built[i].ipv4 = rangesTo4Bytes(v4ranges)
		built[i].ipv4Count = len(v4ranges)

		v6ranges := cidrsToRanges6(s.CIDRs)
		built[i].ipv6 = rangesTo6Bytes(v6ranges)
		built[i].ipv6Count = len(v6ranges)
	}

	// 2. Compute offsets.
	dataStart := headerSize + indexSize*len(sets)
	offset := uint32(dataStart)

	// Names section
	type indexEntry struct {
		nameOff, nameLen                    uint32
		domainOff, domainCount              uint32
		ipv4Off, ipv4Count, ipv6Off, ipv6Count uint32
	}
	entries := make([]indexEntry, len(sets))
	for i := range built {
		entries[i].nameOff = offset
		entries[i].nameLen = uint32(len(built[i].name))
		offset += uint32(len(built[i].name))
	}
	// Domain sections
	for i := range built {
		entries[i].domainOff = offset
		entries[i].domainCount = uint32(built[i].domainCount)
		offset += uint32(len(built[i].domains))
	}
	// IPv4 sections
	for i := range built {
		entries[i].ipv4Off = offset
		entries[i].ipv4Count = uint32(built[i].ipv4Count)
		offset += uint32(len(built[i].ipv4))
	}
	// IPv6 sections
	for i := range built {
		entries[i].ipv6Off = offset
		entries[i].ipv6Count = uint32(built[i].ipv6Count)
		offset += uint32(len(built[i].ipv6))
	}

	// 3. Write header.
	hdr := make([]byte, headerSize)
	copy(hdr[0:4], magic)
	binary.LittleEndian.PutUint32(hdr[4:8], version)
	binary.LittleEndian.PutUint32(hdr[8:12], uint32(len(sets)))
	binary.LittleEndian.PutUint64(hdr[12:20], uint64(time.Now().Unix()))
	// Checksum placeholder (bytes 20-28), reserved (28-32): zero
	if _, err := w.Write(hdr); err != nil {
		return err
	}

	// 4. Write index.
	for i := range entries {
		idx := make([]byte, indexSize)
		binary.LittleEndian.PutUint32(idx[0:4], entries[i].nameOff)
		binary.LittleEndian.PutUint16(idx[4:6], uint16(entries[i].nameLen))
		// idx[6:8] reserved
		binary.LittleEndian.PutUint32(idx[8:12], entries[i].domainOff)
		binary.LittleEndian.PutUint32(idx[12:16], entries[i].domainCount)
		binary.LittleEndian.PutUint32(idx[16:20], entries[i].ipv4Off)
		binary.LittleEndian.PutUint16(idx[20:22], uint16(entries[i].ipv4Count))
		binary.LittleEndian.PutUint32(idx[22:26], entries[i].ipv6Off)
		binary.LittleEndian.PutUint16(idx[26:28], uint16(entries[i].ipv6Count))
		if _, err := w.Write(idx); err != nil {
			return err
		}
	}

	// 5. Write data sections.
	for i := range built {
		if _, err := w.Write(built[i].name); err != nil {
			return err
		}
	}
	for i := range built {
		if _, err := w.Write(built[i].domains); err != nil {
			return err
		}
	}
	for i := range built {
		if _, err := w.Write(built[i].ipv4); err != nil {
			return err
		}
	}
	for i := range built {
		if _, err := w.Write(built[i].ipv6); err != nil {
			return err
		}
	}

	return nil
}

// ReadBundle parses a k2b v1 file from raw bytes (typically mmap'd).
func ReadBundle(data []byte) (*Bundle, error) {
	if len(data) < headerSize {
		return nil, fmt.Errorf("rule: k2b too short: %d bytes", len(data))
	}
	if string(data[0:4]) != magic {
		return nil, fmt.Errorf("rule: bad magic: %q", data[0:4])
	}
	ver := binary.LittleEndian.Uint32(data[4:8])
	if ver > version {
		return nil, fmt.Errorf("rule: unsupported version %d (max %d)", ver, version)
	}
	setCount := int(binary.LittleEndian.Uint32(data[8:12]))

	if len(data) < headerSize+indexSize*setCount {
		return nil, fmt.Errorf("rule: truncated index")
	}

	bundle := &Bundle{
		Sets: make([]BundleSet, setCount),
		data: data,
	}

	for i := 0; i < setCount; i++ {
		idx := data[headerSize+i*indexSize : headerSize+(i+1)*indexSize]

		nameOff := int(binary.LittleEndian.Uint32(idx[0:4]))
		nameLen := int(binary.LittleEndian.Uint16(idx[4:6]))
		domainOff := int(binary.LittleEndian.Uint32(idx[8:12]))
		domainCount := int(binary.LittleEndian.Uint32(idx[12:16]))
		ipv4Off := int(binary.LittleEndian.Uint32(idx[16:20]))
		ipv4Count := int(binary.LittleEndian.Uint16(idx[20:22]))
		ipv6Off := int(binary.LittleEndian.Uint32(idx[22:26]))
		ipv6Count := int(binary.LittleEndian.Uint16(idx[26:28]))

		if nameOff+nameLen > len(data) {
			return nil, fmt.Errorf("rule: set %d name out of bounds", i)
		}
		bundle.Sets[i].Name = string(data[nameOff : nameOff+nameLen])

		// Domain section: domainOff points to the domain section data
		if domainCount > 0 {
			// domainSection data starts at domainOff, extends to the domain area
			// The domainSection struct reads count from the data itself.
			bundle.Sets[i].domains = domainSection{
				data:  data[domainOff:],
				count: domainCount,
			}
		}

		if ipv4Count > 0 {
			bundle.Sets[i].ipv4 = ipv4Section{
				data:  data[ipv4Off : ipv4Off+ipv4Count*8],
				count: ipv4Count,
			}
		}

		if ipv6Count > 0 {
			bundle.Sets[i].ipv6 = ipv6Section{
				data:  data[ipv6Off : ipv6Off+ipv6Count*32],
				count: ipv6Count,
			}
		}
	}

	return bundle, nil
}

// buildDomainData builds the domain section binary for a set of suffix domains.
func buildDomainData(domains []string) []byte {
	if len(domains) == 0 {
		return nil
	}
	reversed := make([]string, len(domains))
	for i, d := range domains {
		reversed[i] = reverseString("." + strings.ToLower(d))
	}
	sort.Strings(reversed)

	// Build: count(4) + offsets(count+1)*4 + strings
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

func rangesTo4Bytes(ranges []ipv4Range) []byte {
	buf := make([]byte, len(ranges)*8)
	for i, r := range ranges {
		copy(buf[i*8:i*8+4], r.from[:])
		copy(buf[i*8+4:i*8+8], r.to[:])
	}
	return buf
}

func rangesTo6Bytes(ranges []ipv6Range) []byte {
	buf := make([]byte, len(ranges)*32)
	for i, r := range ranges {
		copy(buf[i*32:i*32+16], r.from[:])
		copy(buf[i*32+16:i*32+32], r.to[:])
	}
	return buf
}
```

Add `import "net/netip"` to format.go.

- [ ] **Step 4: Run tests**

Run: `cd /Users/david/projects/kaitu-io/k2 && go test ./rule/ -v -run TestFormat`
Expected: all PASS

- [ ] **Step 5: Commit**

```bash
git add rule/format.go rule/format_test.go
git commit -m "feat(rule): k2b v1 format reader/writer with mmap-indexed layout"
```

---

## Task 5: Engine (Match + SetTmpRule + Close)

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

func buildTestBundle(t *testing.T) []byte {
	t.Helper()
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
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestEngineMatchDomain(t *testing.T) {
	data := buildTestBundle(t)
	bundle, err := ReadBundle(data)
	if err != nil {
		t.Fatal(err)
	}

	eng := NewEngineFromBundle(bundle, []BoundSet{
		{Set: &bundle.Sets[0], Target: TargetProxy},  // google
		{Set: &bundle.Sets[1], Target: TargetProxy},  // telegram
	}, nil, TargetDirect)

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
	data := buildTestBundle(t)
	bundle, err := ReadBundle(data)
	if err != nil {
		t.Fatal(err)
	}

	eng := NewEngineFromBundle(bundle, []BoundSet{
		{Set: &bundle.Sets[0], Target: TargetProxy},
		{Set: &bundle.Sets[1], Target: TargetProxy},
	}, nil, TargetDirect)

	tests := []struct {
		host string
		want Target
	}{
		{"8.8.8.8", TargetProxy},         // google IPv4
		{"149.154.167.50", TargetProxy},   // telegram IPv4
		{"2001:4860::1", TargetProxy},     // google IPv6
		{"192.168.1.1", TargetDirect},     // no match → fallback
	}
	for _, tt := range tests {
		if got := eng.Match(tt.host); got != tt.want {
			t.Errorf("Match(%q) = %v, want %v", tt.host, got, tt.want)
		}
	}
}

func TestEngineSetTmpRule(t *testing.T) {
	data := buildTestBundle(t)
	bundle, err := ReadBundle(data)
	if err != nil {
		t.Fatal(err)
	}

	eng := NewEngineFromBundle(bundle, []BoundSet{
		{Set: &bundle.Sets[0], Target: TargetProxy},
	}, nil, TargetDirect)

	// 192.168.1.1 is not in any rule set → should be direct
	if got := eng.Match("192.168.1.1"); got != TargetDirect {
		t.Fatalf("before SetTmpRule: got %v, want direct", got)
	}

	// DNS learns this IP → mark as proxy
	eng.SetTmpRule("192.168.1.1", TargetProxy)

	// Now it should match as proxy (TmpRule has highest priority)
	if got := eng.Match("192.168.1.1"); got != TargetProxy {
		t.Fatalf("after SetTmpRule: got %v, want proxy", got)
	}
}

func TestEngineTmpRulePriority(t *testing.T) {
	data := buildTestBundle(t)
	bundle, err := ReadBundle(data)
	if err != nil {
		t.Fatal(err)
	}

	// google 8.8.8.8 is set to proxy via static rule
	eng := NewEngineFromBundle(bundle, []BoundSet{
		{Set: &bundle.Sets[0], Target: TargetProxy},
	}, nil, TargetDirect)

	// Override with TmpRule → direct
	eng.SetTmpRule("8.8.8.8", TargetDirect)

	// TmpRule should win
	if got := eng.Match("8.8.8.8"); got != TargetDirect {
		t.Fatalf("TmpRule should override static: got %v, want direct", got)
	}
}

func TestEngineConcurrency(t *testing.T) {
	data := buildTestBundle(t)
	bundle, err := ReadBundle(data)
	if err != nil {
		t.Fatal(err)
	}

	eng := NewEngineFromBundle(bundle, []BoundSet{
		{Set: &bundle.Sets[0], Target: TargetProxy},
	}, nil, TargetDirect)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			eng.Match("www.google.com")
		}()
		go func() {
			defer wg.Done()
			eng.SetTmpRule("10.0.0.1", TargetProxy)
		}()
	}
	wg.Wait()
}

func TestEngineGeoIPLayer(t *testing.T) {
	// Build a "geoip-cn" bundle with Chinese IPs
	cnSets := []BundleSet{
		{Name: "geoip-cn", CIDRs: []string{"114.114.114.0/24"}},
	}
	var buf bytes.Buffer
	WriteBundle(&buf, cnSets)
	cnBundle, _ := ReadBundle(buf.Bytes())

	// Build service bundle
	data := buildTestBundle(t)
	bundle, _ := ReadBundle(data)

	eng := NewEngineFromBundle(bundle,
		[]BoundSet{{Set: &bundle.Sets[0], Target: TargetProxy}}, // google=proxy
		[]BoundSet{{Set: &cnBundle.Sets[0], Target: TargetDirect}}, // geoip-cn=direct
		TargetDirect,
	)

	// Google IP → service layer → proxy
	if got := eng.Match("8.8.8.8"); got != TargetProxy {
		t.Errorf("google IP should be proxy, got %v", got)
	}
	// Chinese IP → geoIP layer → direct
	if got := eng.Match("114.114.114.114"); got != TargetDirect {
		t.Errorf("Chinese IP should be direct, got %v", got)
	}
	// Unknown IP → fallback → direct
	if got := eng.Match("192.168.1.1"); got != TargetDirect {
		t.Errorf("unknown IP should be direct, got %v", got)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `cd /Users/david/projects/kaitu-io/k2 && go test ./rule/ -v -run TestEngine 2>&1 | head -5`
Expected: compilation error — `NewEngineFromBundle`, `BoundSet` undefined

- [ ] **Step 3: Write engine.go**

```go
package rule

import (
	"net/netip"
	"strings"

	"github.com/sasha-s/go-deadlock"
)

// BoundSet binds a rule set to a routing action.
type BoundSet struct {
	Set    *BundleSet
	Target Target
}

// Engine is the multi-ruleset matching engine.
// Thread-safe: Match uses RLock, SetTmpRule uses WLock.
// Lock ordering: Engine.mu is standalone — never nested inside any other lock.
type Engine struct {
	mu       deadlock.RWMutex
	tmp      map[netip.Addr]Target
	sets     []BoundSet
	geoIP    []BoundSet
	fallback Target
	bundles  []*Bundle // kept for Close (munmap)
}

// NewEngineFromBundle creates an Engine from pre-loaded bundles.
// sets are checked in order (first-match-wins), geoIP is the lower-priority layer.
func NewEngineFromBundle(bundle *Bundle, sets []BoundSet, geoIP []BoundSet, fallback Target) *Engine {
	e := &Engine{
		tmp:      make(map[netip.Addr]Target),
		sets:     sets,
		geoIP:    geoIP,
		fallback: fallback,
	}
	if bundle != nil {
		e.bundles = append(e.bundles, bundle)
	}
	return e
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
		for i := range e.sets {
			if e.sets[i].Set.MatchIP(addr) {
				return e.sets[i].Target
			}
		}
		for i := range e.geoIP {
			if e.geoIP[i].Set.MatchIP(addr) {
				return e.geoIP[i].Target
			}
		}
	} else {
		// Domain path
		domain := strings.ToLower(host)
		for i := range e.sets {
			if e.sets[i].Set.MatchDomain(domain) {
				return e.sets[i].Target
			}
		}
	}
	return e.fallback
}

// SetTmpRule adds a DNS-learned temporary routing rule.
// TmpRules have the highest priority in IP matching.
func (e *Engine) SetTmpRule(ip string, target Target) {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return
	}
	e.mu.Lock()
	e.tmp[addr] = target
	e.mu.Unlock()
}

// Close releases all mmap'd bundles.
func (e *Engine) Close() {
	// Future: munmap bundle data. For now, bundles hold []byte slices
	// which will be GC'd. When mmap is added (loader.go), this will
	// call syscall.Munmap on each bundle's backing data.
	e.mu.Lock()
	e.bundles = nil
	e.sets = nil
	e.geoIP = nil
	e.tmp = nil
	e.mu.Unlock()
}
```

- [ ] **Step 4: Run tests**

Run: `cd /Users/david/projects/kaitu-io/k2 && go test ./rule/ -v -run TestEngine`
Expected: all PASS

- [ ] **Step 5: Run all rule/ tests together**

Run: `cd /Users/david/projects/kaitu-io/k2 && go test ./rule/ -v -race -count=1`
Expected: all PASS (race detector catches concurrency issues)

- [ ] **Step 6: Commit**

```bash
git add rule/engine.go rule/engine_test.go
git commit -m "feat(rule): Engine with Match, SetTmpRule, priority chain, concurrency"
```

---

## Task 6: Integration — config/config.go

**Files:**
- Modify: `config/config.go:91-99`
- Modify: `config/config_test.go` (if references old fields)

- [ ] **Step 1: Replace RuleConfig**

In `config/config.go`, replace lines 91-99:

Old:
```go
// RuleConfig contains k2rule routing rule configuration.
type RuleConfig struct {
	RuleURL  string `yaml:"rule_url"  json:"rule_url"`
	GeoIPURL string `yaml:"geoip_url" json:"geoip_url"`
	Antiporn bool   `yaml:"antiporn"  json:"antiporn"`
	PornURL  string `yaml:"porn_url"  json:"porn_url"`
	CacheDir string `yaml:"cache_dir" json:"cache_dir"`
	Global   bool   `yaml:"global"    json:"global"`
}
```

New:
```go
// RuleConfig contains routing rule configuration.
type RuleConfig struct {
	Preset   string          `yaml:"preset"    json:"preset"`    // "overseas", "cn-access"
	CnDirect bool            `yaml:"cn_direct" json:"cn_direct"` // add geoip-cn + cn-sites as direct
	Default  string          `yaml:"default"   json:"default"`   // "direct" or "proxy" (fallback)
	Sets     []RuleSetConfig `yaml:"sets"      json:"sets"`      // explicit rule set bindings
	CacheDir string          `yaml:"cache_dir" json:"cache_dir"` // platform shell sets this
	Global   bool            `yaml:"global"    json:"global"`    // true = all proxy (legacy compat)
}

// RuleSetConfig binds a rule set name to a routing action.
type RuleSetConfig struct {
	Name   string `yaml:"name"   json:"name"`
	Action string `yaml:"action" json:"action"` // "proxy", "direct", "reject"
	URL    string `yaml:"url"    json:"url,omitempty"`
}
```

- [ ] **Step 2: Fix config_test.go if it references old fields**

Search `config/config_test.go` for `RuleURL`, `GeoIPURL`, `Antiporn`, `PornURL` and replace with new fields. The test at line 255 (`Rule: RuleConfig{Global: true, CacheDir: "/tmp/k2rule"}`) becomes:
```go
Rule: RuleConfig{Global: true, CacheDir: "/tmp/k2rule"},
```
This still compiles since `Global` and `CacheDir` are preserved.

- [ ] **Step 3: Run config tests**

Run: `cd /Users/david/projects/kaitu-io/k2 && go test ./config/ -v`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add config/config.go config/config_test.go
git commit -m "refactor(config): replace RuleConfig for multi-ruleset engine"
```

---

## Task 7: Integration — core/router.go + core/tunnel.go

**Files:**
- Modify: `core/router.go` (full rewrite, 25 lines → ~25 lines)
- Modify: `core/tunnel.go` (type replacement, ~15 sites)
- Modify: `core/tunnel_test.go`

- [ ] **Step 1: Rewrite core/router.go**

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
func (r *Router) RouteAddr(host string) rule.Target {
	return r.engine.Match(host)
}
```

- [ ] **Step 2: Update core/tunnel.go — replace all k2rule references**

Replace import: `"github.com/kaitu-io/k2rule"` → `"github.com/kaitu-io/k2/rule"`

Replace all occurrences (use find/replace):
- `k2rule.TargetDirect` → `rule.TargetDirect`
- `k2rule.TargetProxy` → `rule.TargetProxy`
- `k2rule.TargetReject` → `rule.TargetReject`
- `k2rule.Target` → `rule.Target`

Replace `k2rule.SetTmpRule(host, ...)` calls (3 sites at lines 246, 388, and the route function) with a method call via the router's engine. Add a `SetTmpRule` method to Router:

Add to `core/router.go`:
```go
// SetTmpRule adds a DNS-learned temporary routing rule.
func (r *Router) SetTmpRule(ip string, target rule.Target) {
	r.engine.SetTmpRule(ip, target)
}
```

Then in tunnel.go, change `k2rule.SetTmpRule(host, k2rule.TargetProxy)` to `t.router.SetTmpRule(host, rule.TargetProxy)` at all 3 sites.

Also update `route()` method:
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

- [ ] **Step 3: Update core/tunnel_test.go**

Replace `&Router{}` with `NewRouter(eng)` where `eng` is a minimal rule engine. For the existing test that creates `Router{}` directly (line 187), create a fallback engine:

```go
eng := rule.NewEngineFromBundle(nil, nil, nil, rule.TargetDirect)
tunnel := NewClientTunnel(ClientTunnelConfig{
	Provider: prov,
	Dialer:   newMockDialer(),
	Router:   NewRouter(eng),
})
```

- [ ] **Step 4: Verify core/ compiles and tests pass**

Run: `cd /Users/david/projects/kaitu-io/k2 && go test ./core/ -v -short`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add core/router.go core/tunnel.go core/tunnel_test.go
git commit -m "refactor(core): replace k2rule with rule.Engine in router and tunnel"
```

---

## Task 8: Integration — core/dns/middleware.go

**Files:**
- Modify: `core/dns/middleware.go` (7 k2rule sites)
- Modify: `core/dns/middleware_test.go`

The DNS middleware currently calls global `k2rule.Match(domain)` and `k2rule.SetTmpRule(ip, action)`. These need to become injected functions so the middleware doesn't import k2rule.

- [ ] **Step 1: Add match/setTmpRule fields to Middleware**

In `core/dns/middleware.go`, add to the `Middleware` struct:

```go
// Rule matching functions (injected by engine, avoids direct k2rule import)
matchRule  func(host string) rule.Target
setTmpRule func(ip string, target rule.Target)
```

Wait — middleware is in `core/dns/`, and `rule` is in `rule/`. That's fine — `core/dns/` can import `rule/` (lower layer can import lower layer).

Actually, the spec says `core/dns/middleware.go` should import `rule` for the `Target` type. But to keep middleware decoupled, inject the functions via config. Use function types that match rule.Target:

Add to `MiddlewareConfig`:
```go
MatchRule  func(string) uint8         // returns rule.Target as uint8
SetTmpRule func(string, uint8)        // sets temporary rule
```

Actually, cleaner to just use `rule.Target` directly. The dns package already depends on the core package, and rule is a leaf package with no upward dependencies.

Replace import `"github.com/kaitu-io/k2rule"` with `"github.com/kaitu-io/k2/rule"`.

Add fields to `Middleware` struct and `MiddlewareConfig`:
```go
// In MiddlewareConfig:
MatchRule  func(string) rule.Target
SetTmpRule func(string, rule.Target)
```

In `NewMiddleware`:
```go
matchRule:  cfg.MatchRule,
setTmpRule: cfg.SetTmpRule,
```

Replace all `k2rule.Match(domain)` → `m.matchRule(domain)`
Replace all `k2rule.SetTmpRule(ip, action)` → `m.setTmpRule(ip, action)`
Replace all `k2rule.Target` → `rule.Target`
Replace all `k2rule.TargetReject` → `rule.TargetReject`
Replace all `k2rule.TargetDirect` → `rule.TargetDirect`
Replace all `k2rule.TargetProxy` → `rule.TargetProxy`

- [ ] **Step 2: Update middleware_test.go**

Replace `initK2Rule` helper with a simpler approach. Instead of initializing a global k2rule, create a rule.Engine and pass its methods:

```go
func testMatchFunc(target rule.Target) func(string) rule.Target {
	return func(string) rule.Target { return target }
}

func testSetTmpFunc() func(string, rule.Target) {
	return func(string, rule.Target) {} // no-op for tests
}
```

Then in tests that call `initK2Rule(t, k2rule.TargetProxy)`, replace with:
```go
mw := NewMiddleware(MiddlewareConfig{
	...
	MatchRule:  testMatchFunc(rule.TargetProxy),
	SetTmpRule: testSetTmpFunc(),
})
```

- [ ] **Step 3: Verify dns tests pass**

Run: `cd /Users/david/projects/kaitu-io/k2 && go test ./core/dns/ -v -short`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add core/dns/middleware.go core/dns/middleware_test.go
git commit -m "refactor(dns): inject rule matching functions, remove k2rule import"
```

---

## Task 9: Integration — engine/engine.go

**Files:**
- Modify: `engine/engine.go` (import, Start step 1, Stop, buildRuleConfig)
- Modify: `engine/engine_test.go`

- [ ] **Step 1: Replace k2rule in engine.go**

Remove import `"github.com/kaitu-io/k2rule"`, add `"github.com/kaitu-io/k2/rule"`.

Add field to `Engine` struct:
```go
ruleEngine *rule.Engine
```

Replace step 1 in Start() (lines 113-118):

Old:
```go
// 1. Rule config — built directly from client.Rule.
ruleCfg := buildRuleConfig(client)
if err := k2rule.Init(ruleCfg); err != nil {
	slog.Warn("engine: k2rule init failed, continuing with defaults", "err", err)
}
slog.Info("engine: step 1 - k2rule init done", "elapsed", time.Since(startTime))
```

New:
```go
// 1. Rule engine — load rule sets from config.
ruleEngine := buildRuleEngine(client)
slog.Info("engine: step 1 - rule engine init done", "elapsed", time.Since(startTime))
```

Replace step 5 (line 205) `router := core.NewRouter()` with:
```go
router := core.NewRouter(ruleEngine)
```

Update step 7 sniff flags (line 255-256):
```go
SniffBT:   !isGlobalProxy(client),
SniffSNI:  !isGlobalProxy(client),
```

Add helper:
```go
func isGlobalProxy(client *config.ClientConfig) bool {
	return client.Rule.Global || strings.ToLower(client.Rule.Default) == "proxy"
}
```

Wait — `Global` is legacy compat. If `Global: true`, it means all traffic is proxied. The new equivalent is `default: proxy` with no rule sets. Let me handle both.

Replace `buildRuleConfig` with `buildRuleEngine`:
```go
func buildRuleEngine(client *config.ClientConfig) *rule.Engine {
	// Legacy: global mode = all proxy, no rule matching.
	if client.Rule.Global {
		return rule.NewEngineFromBundle(nil, nil, nil, rule.TargetProxy)
	}

	// Parse fallback target.
	fallback := rule.TargetDirect
	if t, ok := rule.ParseTarget(client.Rule.Default); ok {
		fallback = t
	}

	// Expand preset + merge user sets.
	var setCfgs []rule.SetConfig
	if client.Rule.Preset != "" {
		setCfgs = rule.ExpandPreset(client.Rule.Preset)
	}
	// Convert config.RuleSetConfig to rule.SetConfig
	var userSets []rule.SetConfig
	for _, s := range client.Rule.Sets {
		t, ok := rule.ParseTarget(s.Action)
		if !ok {
			t = rule.TargetProxy
		}
		userSets = append(userSets, rule.SetConfig{Name: s.Name, Action: t, URL: s.URL})
	}
	if len(userSets) > 0 {
		setCfgs = rule.MergeSets(setCfgs, userSets)
	}

	// For now, return engine with fallback only.
	// Bundle loading (from cache_dir) will be added in the loader task.
	// This keeps the integration working — Match returns fallback for all hosts.
	return rule.NewEngineFromBundle(nil, nil, nil, fallback)
}
```

In Stop(), add cleanup after extracting resources (in the extract-nil-release-close block):
```go
re := e.ruleEngine
e.ruleEngine = nil
```
And after the teardown section:
```go
if re != nil {
	re.Close()
}
```

Store ruleEngine in the commit block (step 9):
```go
e.ruleEngine = ruleEngine
```

Wire DNS middleware with rule engine functions. In the TUN mode DNS setup section, update MiddlewareConfig:
```go
dnsMW := dns.NewMiddleware(dns.MiddlewareConfig{
	ProxyDNS:   proxyDNS,
	DirectDNS:  dns.NewDirectDNSClient(client.DNS.Direct, dnsOpts...),
	DNSCache:   dns.NewDNSCache(dns.DNSCacheConfig{}),
	Filter:     dns.NewFilter(dns.FilterConfig{}),
	EngineCtx:  ctx,
	MatchRule:  ruleEngine.Match,
	SetTmpRule: ruleEngine.SetTmpRule,
})
```

- [ ] **Step 2: Update engine_test.go**

Remove `"github.com/kaitu-io/k2rule"` import. Replace `k2rule.Init`, `k2rule.TargetProxy` etc. with `rule.*` equivalents. The `initK2Rule` calls in engine_test.go can be replaced with direct `rule.NewEngineFromBundle` usage, or simply removed if the test doesn't need rule matching.

- [ ] **Step 3: Remove k2rule from go.mod**

Run: `cd /Users/david/projects/kaitu-io/k2 && go mod tidy`

This should remove `github.com/kaitu-io/k2rule` from go.mod since no file imports it anymore.

- [ ] **Step 4: Full test gate**

Run: `cd /Users/david/projects/kaitu-io/k2 && go test -short -race ./...`
Expected: all PASS, no k2rule references remain.

- [ ] **Step 5: Verify no k2rule references remain**

Run: `cd /Users/david/projects/kaitu-io/k2 && grep -r "k2rule" --include="*.go" .`
Expected: no output (zero matches)

- [ ] **Step 6: Commit**

```bash
git add engine/engine.go engine/engine_test.go go.mod go.sum
git commit -m "refactor(engine): replace k2rule with rule.Engine, wire DNS middleware"
```

---

## Task 10: Final test gate + CLAUDE.md update

**Files:**
- Modify: `CLAUDE.md` (update dependency list, lock ordering, routing docs)

- [ ] **Step 1: Run full test suite**

Run: `cd /Users/david/projects/kaitu-io/k2 && go test -short -race -timeout 120s ./...`
Expected: all PASS

- [ ] **Step 2: Run go vet + build**

Run: `cd /Users/david/projects/kaitu-io/k2 && go vet ./... && go build ./cmd/k2 && go build ./cmd/k2s`
Expected: no errors

- [ ] **Step 3: Update CLAUDE.md**

Update the Dependencies section — replace k2rule entry:

Old: `- github.com/kaitu-io/k2rule v0.6.4 — domain/IP routing rules...`
New: `- rule/ (in-tree) — multi-ruleset routing engine (domain suffix binary search + IP range binary search, k2b mmap-indexed format)`

Update Lock Ordering Graph — add:
```
rule.Engine.mu (state, RWMutex, standalone — no nesting with engine locks)
```

Update the router description in Core section to reference `rule.Engine` instead of `k2rule`.

- [ ] **Step 4: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: update CLAUDE.md for rule module replacement"
```

---

## Post-implementation: Loader + mmap (separate plan)

The following are **deferred** to a follow-up plan (they depend on the k2-rules data pipeline being built):

1. `rule/loader.go` — manifest.json download, bundle HTTP fetch, gzip decompress, mmap
2. `rule/mmap_unix.go` / `rule/mmap_windows.go` — platform-specific mmap
3. Integration with `buildRuleEngine` — load bundles from cache_dir
4. `cmd/k2/rule_compile.go` — `k2 rule compile` subcommand for JSON → k2b conversion
5. `kaitu-io/k2-rules` repository — data pipeline (GitHub Actions, compiler)

The current plan delivers a fully functional rule engine that works with in-memory bundles. The loader adds mmap + HTTP download on top without changing any interfaces.
