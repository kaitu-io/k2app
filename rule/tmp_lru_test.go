package rule

import (
	"fmt"
	"net/netip"
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

// TestReload_ResetsLRUStructuresInSync verifies that reload() clears
// tmpOrder/tmpIndex in lockstep with e.tmp, not just the map. If only the map
// were cleared, tmpOrder/tmpIndex would retain stale pre-reload entries;
// eventually tmpLRUEvictOldest would pop one of those stale entries and its
// `delete(e.tmp, entry.addr)` would be a no-op, letting e.tmp silently grow
// past tmpRuleCap. Mutation check: removing the tmpLRUReset() call from
// reload() (leaving only `e.tmp = make(...)`) must turn this red.
func TestReload_ResetsLRUStructuresInSync(t *testing.T) {
	route := []RouteEntry{{Target: Target(0), NetworkMatch: "tcp"}}
	e := NewEngine(route, Target(2), nil, false)

	// Fill to exactly cap under the pre-reload rule set.
	for i := 0; i < tmpRuleCap; i++ {
		e.SetTmpRule(tmpTestIP(10, i), Target(1))
	}
	if got := e.tmpLen(); got != tmpRuleCap {
		t.Fatalf("setup: tmp size = %d, want %d", got, tmpRuleCap)
	}

	// Reload with a fresh route set. Existing (tested elsewhere) behavior:
	// old pins are untrustworthy under new routes. This test's concern: the
	// LRU order/index must be reset in lockstep, not just the tmp map.
	if err := e.Reload(route, nil); err != nil {
		t.Fatalf("Reload: %v", err)
	}

	// Refill with a disjoint address range (172.x, vs 10.x pre-reload) up to
	// cap again. Nothing here should evict yet — the live map started at 0
	// after reload.
	for i := 0; i < tmpRuleCap; i++ {
		e.SetTmpRule(tmpTestIP(172, i), Target(1))
	}
	if got := e.tmpLen(); got != tmpRuleCap {
		t.Fatalf("after post-reload refill: tmp size = %d, want %d", got, tmpRuleCap)
	}

	// One more distinct pin must evict exactly one LIVE entry, keeping size
	// at cap.
	e.SetTmpRule("203.0.113.10", Target(1))
	if got := e.tmpLen(); got != tmpRuleCap {
		t.Fatalf("tmp size after post-reload overflow = %d, want %d "+
			"(LRU structures must reset in lockstep with e.tmp on reload)", got, tmpRuleCap)
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
	e.mu.Lock()
	before := e.tmpOrder.Len()
	e.mu.Unlock()
	if before != n {
		t.Fatalf("setup: tmpOrder.Len() = %d, want %d", before, n)
	}

	e.Close()

	e.mu.Lock()
	orderLen := e.tmpOrder.Len()
	indexLen := len(e.tmpIndex)
	e.mu.Unlock()
	if orderLen != 0 {
		t.Errorf("Close: tmpOrder.Len() = %d, want 0", orderLen)
	}
	if indexLen != 0 {
		t.Errorf("Close: len(tmpIndex) = %d, want 0", indexLen)
	}
}
