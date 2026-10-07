package rule

import (
	"fmt"
	"net/netip"
	"strings"
	"testing"
)

// tmpLen returns the number of DNS-learned pins. Test/diagnostic accessor —
// moved here (out of engine.go) because nothing outside _test.go needs it;
// Engine's fields are already visible to same-package test files.
func (e *Engine) tmpLen() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return len(e.tmp)
}

// hasTmpRule reports whether ip currently has a live DNS-learned pin.
// Test/diagnostic accessor, sibling to tmpLen — see its comment.
func (e *Engine) hasTmpRule(ip string) bool {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return false
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	_, ok := e.tmp[addr]
	return ok
}

// tmpTestIP builds a distinct dotted-quad from a small integer, spreading
// across the last three octets so tests can generate tmpRuleCap+ distinct
// addresses without collisions. prefix picks the first octet so two ranges
// (e.g. pre-/post-reload) never collide with each other.
func tmpTestIP(prefix, i int) string {
	return fmt.Sprintf("%d.%d.%d.%d", prefix, i/65536%256, i/256%256, i%256)
}

func TestSetTmpRule_EvictsOldestNotWholeTable(t *testing.T) {
	e := NewEngine([]RouteEntry{{Target: Target(0), NetworkMatch: "tcp"}}, Target(2), nil, false)

	// Fill to capacity.
	for i := 0; i < tmpRuleCap; i++ {
		e.SetTmpRule(tmpTestIP(10, i), Target(1))
	}
	// One more push must evict exactly one entry, not the whole map.
	e.SetTmpRule("203.0.113.7", Target(1))

	if got := e.tmpLen(); got != tmpRuleCap {
		t.Fatalf("tmp size after overflow = %d, want %d (evict one, keep the rest)", got, tmpRuleCap)
	}
	// The most recent pin must still be there.
	if !e.hasTmpRule("203.0.113.7") {
		t.Error("newest pin was evicted")
	}
}

// TestSetTmpRule_EvictsFrontNotBack pins down WHICH entry gets evicted: the
// least-recently-used one (list Front), not the most-recently-used one (list
// Back). Mutation check: swapping tmpLRUEvictOldest to pop Back() instead of
// Front() must turn this red.
func TestSetTmpRule_EvictsFrontNotBack(t *testing.T) {
	e := NewEngine([]RouteEntry{{Target: Target(0), NetworkMatch: "tcp"}}, Target(2), nil, false)

	for i := 0; i < tmpRuleCap; i++ {
		e.SetTmpRule(tmpTestIP(10, i), Target(1))
	}
	// ip(0) is the oldest (pushed first, never touched again).
	// ip(cap-1) is the most recently inserted pin before this overflow push.
	e.SetTmpRule("203.0.113.8", Target(1))

	if e.hasTmpRule(tmpTestIP(10, 0)) {
		t.Error("oldest pin (list front) should have been evicted, but is still present")
	}
	if !e.hasTmpRule(tmpTestIP(10, tmpRuleCap-1)) {
		t.Error("most-recently-inserted pre-overflow pin should NOT have been evicted")
	}
	if !e.hasTmpRule("203.0.113.8") {
		t.Error("newest pin was evicted")
	}
}

// TestSetTmpRule_TouchRefreshesFreshness verifies that re-pinning an existing
// key moves it to the back of the LRU (fresh), so it survives the next
// eviction that would otherwise have claimed it. This matters because the
// DNS pipeline re-pins on every cache hit (see SetTmpRule's doc comment),
// not just on target changes. Mutation check: dropping the MoveToBack call
// in tmpLRUTouch's existing-key branch must turn this red.
func TestSetTmpRule_TouchRefreshesFreshness(t *testing.T) {
	e := NewEngine([]RouteEntry{{Target: Target(0), NetworkMatch: "tcp"}}, Target(2), nil, false)

	for i := 0; i < tmpRuleCap; i++ {
		e.SetTmpRule(tmpTestIP(10, i), Target(1))
	}
	// Re-pin ip(0) with the SAME target — a duplicate call, which still must
	// refresh its LRU position (only the DIAG log is threshold-gated, the LRU
	// touch is not).
	e.SetTmpRule(tmpTestIP(10, 0), Target(1))

	// One more distinct pin must now evict ip(1) — the new oldest — not
	// ip(0), which was just refreshed to the back.
	e.SetTmpRule("203.0.113.9", Target(1))

	if !e.hasTmpRule(tmpTestIP(10, 0)) {
		t.Error("ip(0) was refreshed via touch and should not have been evicted")
	}
	if e.hasTmpRule(tmpTestIP(10, 1)) {
		t.Error("ip(1) is now the oldest untouched pin and should have been evicted")
	}
}

// TestReload_KeepsLRUStructuresInSync verifies that reload() leaves e.tmp,
// tmpOrder and tmpIndex untouched together: pins survive a hot reload (see
// TestEngineReload_KeepsTmpRules) and the LRU keeps evicting the oldest
// pre-reload pin first. Mutation check: resetting only one of the three
// structures in reload() must turn this red.
func TestReload_KeepsLRUStructuresInSync(t *testing.T) {
	route := []RouteEntry{{Target: Target(0), NetworkMatch: "tcp"}}
	e := NewEngine(route, Target(2), nil, false)

	for i := 0; i < tmpRuleCap; i++ {
		e.SetTmpRule(tmpTestIP(10, i), Target(1))
	}
	if err := e.Reload(route, nil); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if got := e.tmpLen(); got != tmpRuleCap {
		t.Fatalf("tmp size after reload = %d, want %d (pins kept)", got, tmpRuleCap)
	}

	// One more distinct pin evicts exactly the oldest pre-reload entry.
	e.SetTmpRule("203.0.113.10", Target(1))
	if got := e.tmpLen(); got != tmpRuleCap {
		t.Fatalf("tmp size after overflow = %d, want %d", got, tmpRuleCap)
	}
	if e.hasTmpRule(tmpTestIP(10, 0)) {
		t.Error("oldest pre-reload pin should have been evicted first")
	}
	if !e.hasTmpRule(tmpTestIP(10, 1)) {
		t.Error("second-oldest pre-reload pin should still be present")
	}
}

// TestClose_ResetsLRUStructuresInSync verifies that Close() resets
// tmpOrder/tmpIndex alongside e.tmp. Direct field inspection (legitimate
// here: the invariant under test IS "these three fields stay in sync",
// which SetTmpRule's black-box behavior can't observe post-Close since
// Close() also nils e.routes, permanently short-circuiting SetTmpRule).
// Mutation check: removing the tmpLRUReset() call from Close() must turn
// this red.
func TestClose_ResetsLRUStructuresInSync(t *testing.T) {
	route := []RouteEntry{{Target: Target(0), NetworkMatch: "tcp"}}
	e := NewEngine(route, Target(2), nil, false)

	const n = 100
	for i := 0; i < n; i++ {
		e.SetTmpRule(tmpTestIP(10, i), Target(1))
	}
	e.mu.RLock()
	before := e.tmpOrder.Len()
	e.mu.RUnlock()
	if before != n {
		t.Fatalf("setup: tmpOrder.Len() = %d, want %d", before, n)
	}

	e.Close()

	e.mu.RLock()
	orderLen := e.tmpOrder.Len()
	indexLen := len(e.tmpIndex)
	e.mu.RUnlock()
	if orderLen != 0 {
		t.Errorf("Close: tmpOrder.Len() = %d, want 0", orderLen)
	}
	if indexLen != 0 {
		t.Errorf("Close: len(tmpIndex) = %d, want 0", indexLen)
	}
}

// TestSetTmpRule_DIAGEvictedIPField verifies the DIAG payload contract for
// eviction: when SetTmpRule evicts to make room, the evicted IP rides along
// on the same `rule-tmp-pin` add line (see SetTmpRule's doc comment for why
// there's no separate rule-tmp-evict line) in an `evictedIP` field — and it
// must be the EVICTED (oldest) IP, not the new pin's own IP. On-call triage
// greps this field to see which pin got kicked out; the two addresses are
// easy to transpose by accident since both are in scope at the log call
// site. Mutation check: swapping evictedAddr for addr (the new pin's own
// address) in the log call must turn this red.
func TestSetTmpRule_DIAGEvictedIPField(t *testing.T) {
	buf := captureSlog(t)
	e := NewEngine([]RouteEntry{{Target: Target(0), NetworkMatch: "tcp"}}, Target(2), nil, false)
	defer e.Close()

	for i := 0; i < tmpRuleCap; i++ {
		e.SetTmpRule(tmpTestIP(10, i), Target(1))
	}
	buf.Reset() // discard the cap-filling pin logs

	newIP := "203.0.113.11"
	oldestIP := tmpTestIP(10, 0)
	e.SetTmpRule(newIP, Target(1))

	out := buf.String()
	if !strings.Contains(out, "DIAG: rule-tmp-pin") {
		t.Fatalf("missing rule-tmp-pin log on overflow:\n%s", out)
	}
	wantEvicted := fmt.Sprintf("evictedIP=%s", oldestIP)
	if !strings.Contains(out, wantEvicted) {
		t.Errorf("evictedIP field wrong or missing; want %q in:\n%s", wantEvicted, out)
	}
	if strings.Contains(out, fmt.Sprintf("evictedIP=%s", newIP)) {
		t.Errorf("evictedIP must be the EVICTED (oldest) ip %q, not the new pin's own ip %q:\n%s",
			oldestIP, newIP, out)
	}
}
