package wire

import (
	"crypto/sha256"
	"encoding/hex"
	"time"

	deadlock "github.com/sasha-s/go-deadlock"
	"golang.org/x/sync/singleflight"
)

// CachingValidator wraps a Validator with a TTL cache of SUCCESSFUL verdicts
// and a singleflight on cold misses.
//
// Only OK verdicts are cached. Code == -1 (Center unreachable) is NEVER
// cached — caching "can't reach" would freeze a transient blip into a
// TTL-long outage. Explicit rejections (401/402/403) are never cached either:
// a user who just renewed must not stay locked out for the TTL, and the cache
// must never keep serving a device Center has since revoked.
//
// ttl <= 0 disables caching (every entry is already expired on read); the
// singleflight still dedupes concurrent identical calls.
func CachingValidator(inner Validator, ttl time.Duration) Validator {
	c := &authCache{inner: inner, ttl: ttl, now: time.Now, entries: map[string]cachedVerdict{}}
	return c.validate
}

type cachedVerdict struct {
	verdict AuthVerdict
	expiry  int64 // unix nanos
}

type authCache struct {
	inner Validator
	ttl   time.Duration
	now   func() time.Time
	sf    singleflight.Group

	// mu is a state lock (standalone — see CLAUDE.md Lock Ordering Graph):
	// critical sections only read/write entries/lastSweep. The inner validator
	// (Center I/O) runs OUTSIDE the lock, inside the singleflight.
	mu        deadlock.Mutex
	entries   map[string]cachedVerdict
	lastSweep int64 // unix nanos of last opportunistic sweep
}

func (c *authCache) validate(token, udid, mode string) AuthVerdict {
	key := authCacheKey(token, udid, mode)
	nowNS := c.now().UnixNano()

	c.mu.Lock()
	if e, ok := c.entries[key]; ok {
		if nowNS < e.expiry {
			c.mu.Unlock()
			return e.verdict
		}
		delete(c.entries, key)
	}
	c.mu.Unlock()

	v, _, _ := c.sf.Do(key, func() (any, error) {
		verdict := c.inner(token, udid, mode)
		if verdict.OK && c.ttl > 0 {
			expNS := c.now().UnixNano() + c.ttl.Nanoseconds()
			c.mu.Lock()
			c.entries[key] = cachedVerdict{verdict: verdict, expiry: expNS}
			c.sweepLocked(c.now().UnixNano())
			c.mu.Unlock()
		}
		return verdict, nil
	})
	return v.(AuthVerdict)
}

// sweepLocked opportunistically evicts expired entries so the map can't grow
// unboundedly on a long-lived node. Runs at most once per ttl window. Caller
// holds c.mu.
func (c *authCache) sweepLocked(nowNS int64) {
	if c.ttl <= 0 || nowNS-c.lastSweep < c.ttl.Nanoseconds() {
		return
	}
	c.lastSweep = nowNS
	for k, e := range c.entries {
		if nowNS >= e.expiry {
			delete(c.entries, k)
		}
	}
}

func authCacheKey(token, udid, mode string) string {
	h := sha256.New()
	h.Write([]byte(udid))
	h.Write([]byte{0})
	h.Write([]byte(token))
	h.Write([]byte{0})
	h.Write([]byte(mode))
	return hex.EncodeToString(h.Sum(nil))
}
