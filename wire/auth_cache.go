package wire

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"
)

// CachingValidator wraps a JWTValidator with a TTL cache of SUCCESSFUL results.
//
// Opening the QUIC metadata path to every normal client turns Center's
// /slave/device-check-auth from ~0 QPS into one request per new connection; on a
// busy node an uncached validator would hammer Center. Only positive results are
// cached: a rejected token (expired / invalid / membership lapsed) is re-checked
// every time, so a user who just renewed isn't locked out for the TTL and the
// cache never keeps serving a device Center has since revoked.
//
// ttl <= 0 disables caching (every entry is already expired on read).
func CachingValidator(inner JWTValidator, ttl time.Duration) JWTValidator {
	c := &authCache{inner: inner, ttl: ttl, now: time.Now, entries: map[string]int64{}}
	return c.validate
}

type authCache struct {
	inner JWTValidator
	ttl   time.Duration
	now   func() time.Time

	mu        sync.Mutex
	entries   map[string]int64 // key → expiry (unix nanos)
	lastSweep int64            // unix nanos of last opportunistic sweep
}

func (c *authCache) validate(jwt, udid, mode string) bool {
	key := authCacheKey(jwt, udid, mode)
	nowNS := c.now().UnixNano()

	c.mu.Lock()
	if exp, ok := c.entries[key]; ok {
		if nowNS < exp {
			c.mu.Unlock()
			return true
		}
		delete(c.entries, key)
	}
	c.mu.Unlock()

	ok := c.inner(jwt, udid, mode)
	if ok && c.ttl > 0 {
		c.mu.Lock()
		c.entries[key] = nowNS + c.ttl.Nanoseconds()
		c.sweepLocked(nowNS)
		c.mu.Unlock()
	}
	return ok
}

// sweepLocked opportunistically evicts expired entries so the map can't grow
// unboundedly on a long-lived node. Runs at most once per ttl window. Caller
// holds c.mu.
func (c *authCache) sweepLocked(nowNS int64) {
	if c.ttl <= 0 || nowNS-c.lastSweep < c.ttl.Nanoseconds() {
		return
	}
	c.lastSweep = nowNS
	for k, exp := range c.entries {
		if nowNS >= exp {
			delete(c.entries, k)
		}
	}
}

func authCacheKey(jwt, udid, mode string) string {
	h := sha256.New()
	h.Write([]byte(udid))
	h.Write([]byte{0})
	h.Write([]byte(jwt))
	h.Write([]byte{0})
	h.Write([]byte(mode))
	return hex.EncodeToString(h.Sum(nil))
}
