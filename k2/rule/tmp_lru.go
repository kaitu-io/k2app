package rule

import (
	"container/list"
	"net/netip"
)

// tmpLRUTouch records or refreshes addr's LRU position: an existing key
// moves to the back (most-recently-used); a new key is pushed to the back.
// The target itself is never stored in the list — routing and eviction both
// only ever need the address (e.tmp[addr] is the sole source of truth for
// target; storing a second copy here would just be an unread, unverified
// duplicate of it). Capacity eviction is the caller's responsibility (see
// tmpLRUEvictOldest) — this method never evicts.
//
// MUST be called only while holding e.mu for writing. Engine.mu is never
// nested inside any other lock (see the Engine struct's lock-ordering
// comment) — this method takes no lock of its own, by design.
func (e *Engine) tmpLRUTouch(addr netip.Addr) {
	if el, ok := e.tmpIndex[addr]; ok {
		e.tmpOrder.MoveToBack(el)
		return
	}
	el := e.tmpOrder.PushBack(addr)
	e.tmpIndex[addr] = el
}

// tmpLRUEvictOldest removes the single least-recently-used pin from
// e.tmp/e.tmpOrder/e.tmpIndex together and returns its address. Reports
// false if the LRU is empty (defensive — SetTmpRule only calls this when
// len(e.tmp) >= tmpRuleCap, so the list should never be empty there; see
// SetTmpRule's DIAG: rule-tmp-lru-desync handling for what a false here
// would mean).
//
// MUST be called only while holding e.mu for writing (see tmpLRUTouch).
func (e *Engine) tmpLRUEvictOldest() (netip.Addr, bool) {
	front := e.tmpOrder.Front()
	if front == nil {
		return netip.Addr{}, false
	}
	addr := front.Value.(netip.Addr)
	e.tmpOrder.Remove(front)
	delete(e.tmpIndex, addr)
	delete(e.tmp, addr)
	return addr, true
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
