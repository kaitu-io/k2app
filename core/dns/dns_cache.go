package dns

import (
	"strconv"
	"sync"
	"time"

	"github.com/miekg/dns"
)

// cachedEntry stores a pre-packed DNS response with expiration.
// Storing packed bytes instead of *dns.Msg eliminates dns.Msg.Copy() on
// every cache hit and lets callers skip resp.Pack() entirely.
type cachedEntry struct {
	packed   []byte
	expireAt time.Time
}

// DNSCache implements DNS message caching with negative caching (RFC 2308)
type DNSCache struct {
	mu sync.RWMutex

	// Positive cache: domain+qtype → pre-packed DNS response
	messages map[string]cachedEntry

	// Negative cache: domain+qtype → pre-packed NXDOMAIN response
	negative map[string]cachedEntry

	// Configuration
	minTTL     time.Duration // Minimum TTL (user requirement: 10 minutes)
	maxTTL     time.Duration // Maximum TTL
	maxEntries int           // Max cache entries (LRU eviction)
	staleFor   time.Duration // Stale-while-revalidate window (RFC 8767)

	// Lifecycle
	stopCleanup chan struct{}
}

// DNSCacheConfig contains DNS cache configuration
type DNSCacheConfig struct {
	MinTTL     time.Duration // Minimum TTL (default: 10 minutes)
	MaxTTL     time.Duration // Maximum TTL (default: 4 hours)
	MaxEntries int           // Max cache entries (default: 5000)
	StaleFor   time.Duration // Serve stale entries for this long after expiry (default: 5 minutes, 0 disables)
}

// NewDNSCache creates a new DNS message cache
func NewDNSCache(cfg DNSCacheConfig) *DNSCache {
	if cfg.MinTTL == 0 {
		cfg.MinTTL = 10 * time.Minute // User requirement
	}
	if cfg.MaxTTL == 0 {
		cfg.MaxTTL = 4 * time.Hour
	}
	if cfg.MaxEntries == 0 {
		cfg.MaxEntries = 5000
	}
	if cfg.StaleFor == 0 {
		cfg.StaleFor = 5 * time.Minute
	}

	return &DNSCache{
		messages:    make(map[string]cachedEntry, cfg.MaxEntries),
		negative:    make(map[string]cachedEntry, 256),
		minTTL:      cfg.MinTTL,
		maxTTL:      cfg.MaxTTL,
		maxEntries:  cfg.MaxEntries,
		staleFor:    cfg.StaleFor,
		stopCleanup: make(chan struct{}),
	}
}

// GetPacked retrieves a pre-packed DNS response from cache.
// Returns a copy of the packed bytes with the query ID patched to queryID.
// This is the fast path: pure memcpy, no dns.Msg.Copy() or Pack().
func (c *DNSCache) GetPacked(domain string, qtype uint16, queryID uint16) ([]byte, bool) {
	key := cacheKey(domain, qtype)

	c.mu.RLock()
	defer c.mu.RUnlock()

	now := time.Now()

	// Check negative cache first
	if entry, found := c.negative[key]; found {
		if now.Before(entry.expireAt) {
			return cloneAndPatchID(entry.packed, queryID), true
		}
	}

	// Check positive cache
	entry, found := c.messages[key]
	if !found {
		return nil, false
	}
	if now.After(entry.expireAt) {
		return nil, false
	}

	return cloneAndPatchID(entry.packed, queryID), true
}

// GetStalePacked returns an expired-but-within-stale-window entry from positive
// cache only. Negative cache (NXDOMAIN) entries are never returned stale because
// the domain may have recovered. Returns packed bytes with queryID patched.
func (c *DNSCache) GetStalePacked(domain string, qtype uint16, queryID uint16) ([]byte, bool) {
	key := cacheKey(domain, qtype)

	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, found := c.messages[key]
	if !found {
		return nil, false
	}

	now := time.Now()
	// Must be expired but within stale window
	if now.After(entry.expireAt) && now.Before(entry.expireAt.Add(c.staleFor)) {
		return cloneAndPatchID(entry.packed, queryID), true
	}

	return nil, false
}

// Get retrieves a cached DNS message (convenience wrapper, unpacks internally).
// Production hot paths should use GetPacked instead.
func (c *DNSCache) Get(domain string, qtype uint16) (*dns.Msg, bool) {
	packed, found := c.GetPacked(domain, qtype, 0)
	if !found {
		return nil, false
	}
	var msg dns.Msg
	if err := msg.Unpack(packed); err != nil {
		return nil, false
	}
	return &msg, true
}

// cloneAndPatchID copies packed DNS bytes and overwrites the query ID (bytes 0-1).
func cloneAndPatchID(packed []byte, queryID uint16) []byte {
	out := make([]byte, len(packed))
	copy(out, packed)
	out[0] = byte(queryID >> 8)
	out[1] = byte(queryID)
	return out
}

// Add adds a DNS message to the cache. The message is packed (serialized) once
// and stored as raw bytes, eliminating dns.Msg.Copy() and allowing cache hits
// to return pre-packed bytes via GetPacked.
func (c *DNSCache) Add(domain string, qtype uint16, msg *dns.Msg) {
	if msg == nil {
		return
	}

	key := cacheKey(domain, qtype)

	c.mu.Lock()
	defer c.mu.Unlock()

	// Handle NXDOMAIN (negative caching per RFC 2308)
	if msg.Rcode == dns.RcodeNameError {
		ttl := c.extractNegativeTTL(msg)
		// Pre-pack NXDOMAIN response with SOA for negative cache.
		nxResp := createNXDOMAINWithSOA(&dns.Msg{Question: msg.Question})
		packed, err := nxResp.Pack()
		if err != nil {
			return
		}
		c.negative[key] = cachedEntry{packed: packed, expireAt: time.Now().Add(ttl)}
		return
	}

	// Skip caching if no answers
	if len(msg.Answer) == 0 {
		return
	}

	// Optimize TTL (10 minute minimum per user requirement)
	c.optimizeTTL(msg)

	// Pack after TTL optimization so stored bytes have correct TTLs
	packed, err := msg.Pack()
	if err != nil {
		return
	}

	// Calculate expiration based on minimum answer TTL
	ttl := c.extractTTL(msg)

	// Evict a random entry if cache is full
	if len(c.messages) >= c.maxEntries {
		c.evictOne()
	}

	// Store pre-packed response
	c.messages[key] = cachedEntry{packed: packed, expireAt: time.Now().Add(ttl)}
}

// optimizeTTL enforces minimum and maximum TTL on all RRs
func (c *DNSCache) optimizeTTL(msg *dns.Msg) {
	minTTL := uint32(c.minTTL.Seconds())
	maxTTL := uint32(c.maxTTL.Seconds())

	for _, rr := range msg.Answer {
		hdr := rr.Header()
		if hdr.Ttl < minTTL {
			hdr.Ttl = minTTL
		}
		if hdr.Ttl > maxTTL {
			hdr.Ttl = maxTTL
		}
	}

	for _, rr := range msg.Ns {
		hdr := rr.Header()
		if hdr.Ttl < minTTL {
			hdr.Ttl = minTTL
		}
		if hdr.Ttl > maxTTL {
			hdr.Ttl = maxTTL
		}
	}

	for _, rr := range msg.Extra {
		hdr := rr.Header()
		if hdr.Ttl < minTTL {
			hdr.Ttl = minTTL
		}
		if hdr.Ttl > maxTTL {
			hdr.Ttl = maxTTL
		}
	}
}

// extractTTL extracts the minimum TTL from DNS response
func (c *DNSCache) extractTTL(msg *dns.Msg) time.Duration {
	if len(msg.Answer) == 0 {
		return c.minTTL
	}

	minTTL := msg.Answer[0].Header().Ttl
	for _, rr := range msg.Answer {
		ttl := rr.Header().Ttl
		if ttl < minTTL {
			minTTL = ttl
		}
	}

	duration := time.Duration(minTTL) * time.Second

	// Enforce minimum
	if duration < c.minTTL {
		return c.minTTL
	}

	// Enforce maximum
	if duration > c.maxTTL {
		return c.maxTTL
	}

	return duration
}

// extractNegativeTTL extracts negative cache TTL from SOA record (RFC 2308)
func (c *DNSCache) extractNegativeTTL(msg *dns.Msg) time.Duration {
	// Look for SOA record in Authority section
	for _, rr := range msg.Ns {
		if soa, ok := rr.(*dns.SOA); ok {
			// RFC 2308: use SOA MINIMUM field for negative caching
			ttl := time.Duration(soa.Minttl) * time.Second
			if ttl < c.minTTL {
				return c.minTTL
			}
			if ttl > c.maxTTL {
				return c.maxTTL
			}
			return ttl
		}
	}

	// Default negative cache TTL
	return c.minTTL
}

// evictOne removes a random cache entry. Go map iteration order is randomized,
// so deleting the first iterated key gives O(1) random eviction. This is
// sufficient for DNS caches where most entries have similar TTLs (minTTL=10min)
// and expired entries are cleaned up by the background goroutine.
func (c *DNSCache) evictOne() {
	for key := range c.messages {
		delete(c.messages, key)
		return
	}
}

// StartCleanup starts background cleanup of expired entries.
// The goroutine stops when Close() is called.
func (c *DNSCache) StartCleanup(interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				c.cleanup()
			case <-c.stopCleanup:
				return
			}
		}
	}()
}

// Close stops the cleanup goroutine and releases cached entries.
func (c *DNSCache) Close() {
	select {
	case <-c.stopCleanup:
	default:
		close(c.stopCleanup)
	}

	c.mu.Lock()
	clear(c.messages)
	clear(c.negative)
	c.mu.Unlock()
}

// cleanup removes all expired entries
func (c *DNSCache) cleanup() {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()

	// Clean positive cache — preserve entries within stale window
	for key, entry := range c.messages {
		if now.After(entry.expireAt.Add(c.staleFor)) {
			delete(c.messages, key)
		}
	}

	// Clean negative cache
	for key, entry := range c.negative {
		if now.After(entry.expireAt) {
			delete(c.negative, key)
		}
	}
}

// Size returns current cache size
func (c *DNSCache) Size() (positive, negative int) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.messages), len(c.negative)
}

// cacheKey creates a cache key from domain and query type
func cacheKey(domain string, qtype uint16) string {
	return domain + ":" + strconv.FormatUint(uint64(qtype), 10)
}
