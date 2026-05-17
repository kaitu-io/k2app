package provider

import (
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/sasha-s/go-deadlock"
)

const (
	processCacheCap = 4096
	processCacheTTL = 2 * time.Second
)

// cacheKey builds a stable string key for (network, srcIP, srcPort).
// Pre-allocated builder avoids fmt.Sprintf allocations on hot path.
func cacheKey(network string, srcIP netip.Addr, srcPort uint16) string {
	var b strings.Builder
	b.Grow(len(network) + 1 + 15 + 1 + 5)
	b.WriteString(network)
	b.WriteByte(':')
	b.WriteString(srcIP.String())
	b.WriteByte(':')
	b.WriteString(strconv.FormatUint(uint64(srcPort), 10))
	return b.String()
}

type processCache struct {
	mu      deadlock.Mutex
	entries map[string]processCacheEntry
	cap     int
	ttl     time.Duration
}

type processCacheEntry struct {
	name    string
	pkg     string
	expires time.Time
}

func newProcessCache(cap int, ttl time.Duration) *processCache {
	return &processCache{
		entries: make(map[string]processCacheEntry),
		cap:     cap,
		ttl:     ttl,
	}
}

func (c *processCache) get(key string) (name, pkg string, hit bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok || time.Now().After(e.expires) {
		return "", "", false
	}
	return e.name, e.pkg, true
}

func (c *processCache) set(key, name, pkg string) {
	now := time.Now()
	entry := processCacheEntry{
		name:    name,
		pkg:     pkg,
		expires: now.Add(c.ttl),
	}
	c.mu.Lock()
	if len(c.entries) >= c.cap {
		// Lazy sweep: amortized O(1), worst-case O(cap) every cap inserts.
		for k, e := range c.entries {
			if now.After(e.expires) {
				delete(c.entries, k)
			}
		}
	}
	c.entries[key] = entry
	c.mu.Unlock()
}
