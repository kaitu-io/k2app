package credential

import (
	"sync"
	"sync/atomic"
	"time"
)

// DefaultMaxReplayEntries bounds the replay cache's memory. At ~56 bytes per
// entry this is a few megabytes, and it holds a node sustaining ~150 new
// authenticated connections per second across the whole retention period.
const DefaultMaxReplayEntries = 200_000

// replayCache remembers credentials that have already been used, for exactly as
// long as they would still be accepted.
//
// Each entry expires at its own credential's deadline rather than at a fixed
// TTL from admission. That keeps the invariant — a credential is never
// simultaneously acceptable and forgotten — inside this file, instead of
// splitting it across an acceptance check here and a retention constant there
// where the two can drift apart by a boundary.
//
// Retention is by time, not by capacity. A capacity-evicting cache would drop
// an entry while its credential was still inside the acceptance window, which
// is precisely when replaying it works — so capacity here is a memory backstop
// that refuses new entries rather than an eviction policy that silently makes
// room.
//
// Refusing is the safe direction. A full cache means the node stops taking the
// authenticated path and relays everyone to the front: it becomes useless while
// staying perfectly camouflaged. Evicting instead would keep it useful while
// making it confirmable by an on-path attacker who captured a credential and
// replays it to see whether this node treats it specially. Unobservability is
// the property worth keeping when only one can be had.
//
// Deadlines arrive in near-insertion order (they are the client's timestamp
// plus a constant window, and a client whose clock is far off is rejected
// outright), so a FIFO swept from the front is enough and no heap is needed.
// Order is only an optimisation: a late-expiring entry at the head merely
// delays the sweep of ones behind it, which errs toward remembering longer.
type replayCache struct {
	max int
	now func() time.Time

	mu    sync.Mutex
	seen  map[[SealedLen]byte]struct{}
	order []entry

	// overflows counts admissions refused for want of capacity. It is the
	// signal that MaxReplayEntries is undersized for this node's load; a node
	// silently sitting at capacity would look like a node nobody can connect to.
	overflows atomic.Uint64
}

type entry struct {
	key     [SealedLen]byte
	expires time.Time
}

func newReplayCache(max int, now func() time.Time) *replayCache {
	if max <= 0 {
		max = DefaultMaxReplayEntries
	}
	return &replayCache{
		max:  max,
		now:  now,
		seen: make(map[[SealedLen]byte]struct{}),
	}
}

// admit records a credential and reports whether it is being seen for the first
// time. acceptableUntil is the last instant this credential would pass the
// timestamp check; the entry is kept through that instant inclusive, because
// that is the last moment a replay of it could succeed.
//
// A false return means either a replay or a full cache; both lead to the
// borrowed-shell path, so the caller needs no distinction.
func (r *replayCache) admit(sealed []byte, acceptableUntil time.Time) bool {
	var key [SealedLen]byte
	copy(key[:], sealed)

	r.mu.Lock()
	defer r.mu.Unlock()

	now := r.now()
	r.expire(now)

	if _, dup := r.seen[key]; dup {
		return false
	}
	if len(r.order) >= r.max {
		r.overflows.Add(1)
		return false
	}
	r.seen[key] = struct{}{}
	r.order = append(r.order, entry{key: key, expires: acceptableUntil})
	return true
}

// expire drops entries whose credentials can no longer be accepted anyway.
// The comparison is strict: an entry whose deadline is exactly now is still
// live, because the acceptance window is inclusive at both ends and a replay
// landing on that instant would otherwise pass.
// Callers hold r.mu.
func (r *replayCache) expire(now time.Time) {
	i := 0
	for i < len(r.order) && now.After(r.order[i].expires) {
		delete(r.seen, r.order[i].key)
		i++
	}
	if i == 0 {
		return
	}
	r.order = append(r.order[:0], r.order[i:]...)
}

// Overflows reports how many admissions this node refused for want of replay
// cache capacity. Non-zero means MaxReplayEntries is too small for the load.
func (o *Opener) Overflows() uint64 { return o.replay.overflows.Load() }
