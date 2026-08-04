package rule

import (
	"container/list"
	"net/netip"
)

// tmpLRUEntry is the container/list payload for one DNS-learned pin.
// addr is carried alongside target so eviction (tmpLRUEvictOldest, which only
// has the list element to start from) can find the e.tmp map key without a
// reverse index.
type tmpLRUEntry struct {
	addr   netip.Addr
	target Target
}

// tmpLRUTouch records or refreshes addr's LRU position: existing keys move to
// the back (most-recently-used) with their target updated in place; new keys
// are pushed to the back. Capacity eviction is the caller's responsibility
// (see tmpLRUEvictOldest) — this method never evicts.
//
// MUST be called only while holding e.mu for writing. Engine.mu is never
// nested inside any other lock (see the Engine struct's lock-ordering
// comment) — this method takes no lock of its own, by design.
func (e *Engine) tmpLRUTouch(addr netip.Addr, target Target) {
	if el, ok := e.tmpIndex[addr]; ok {
		el.Value.(*tmpLRUEntry).target = target
		e.tmpOrder.MoveToBack(el)
		return
	}
	el := e.tmpOrder.PushBack(&tmpLRUEntry{addr: addr, target: target})
	e.tmpIndex[addr] = el
}

// tmpLRUEvictOldest removes the single least-recently-used pin from
// e.tmp/e.tmpOrder/e.tmpIndex together and returns its address. Reports
// false if the LRU is empty (defensive — SetTmpRule only calls this when
// len(e.tmp) >= tmpRuleCap, so the list should never be empty there).
//
// MUST be called only while holding e.mu for writing (see tmpLRUTouch).
func (e *Engine) tmpLRUEvictOldest() (netip.Addr, bool) {
	front := e.tmpOrder.Front()
	if front == nil {
		return netip.Addr{}, false
	}
	entry := front.Value.(*tmpLRUEntry)
	e.tmpOrder.Remove(front)
	delete(e.tmpIndex, entry.addr)
	delete(e.tmp, entry.addr)
	return entry.addr, true
}

// tmpLRUReset reinitializes tmpOrder/tmpIndex to empty. MUST be called
// together with (never instead of, never separately from) `e.tmp = make(...)`
// — the three structures are one logical unit. A clear that touches only
// e.tmp leaves stale elements in tmpOrder/tmpIndex; the next eviction can
// then pick a stale (already-gone-from-tmp) entry, so tmpLRUEvictOldest's
// `delete(e.tmp, entry.addr)` becomes a no-op and e.tmp silently grows past
// tmpRuleCap.
//
// MUST be called only while holding e.mu for writing (see tmpLRUTouch).
func (e *Engine) tmpLRUReset() {
	e.tmpOrder = list.New()
	e.tmpIndex = make(map[netip.Addr]*list.Element)
}
