package dns

import (
	"strconv"
	"time"

	"github.com/miekg/dns"
	"github.com/sagernet/sing/contrab/freelru"
	"github.com/sagernet/sing/contrab/maphash"
)

// cachedEntry stores a pre-packed DNS response with original expiration.
// Storing packed bytes instead of *dns.Msg eliminates dns.Msg.Copy() on
// every cache hit and lets callers skip resp.Pack() entirely.
type cachedEntry struct {
	packed      []byte
	expireAt    time.Time     // original TTL expiry (for fresh/stale distinction)
	originalTTL time.Duration // original TTL duration (for prefresh threshold calc)
}

// DNSCache implements DNS message caching with negative caching (RFC 2308).
// Uses freelru (sharded LRU with per-entry TTL) for lock-free concurrent access.
type DNSCache struct {
	// Positive cache: domain:qtype → pre-packed DNS response
	messages freelru.Cache[string, *cachedEntry]

	// Negative cache: domain:qtype → pre-packed NXDOMAIN response
	negative freelru.Cache[string, *cachedEntry]

	// Configuration
	minTTL   time.Duration
	maxTTL   time.Duration
	staleFor time.Duration
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

	hasher := maphash.NewHasher[string]()
	hashFn := func(key string) uint32 { return hasher.Hash32(key) }

	messages, _ := freelru.NewSharded[string, *cachedEntry](uint32(cfg.MaxEntries), hashFn)
	negative, _ := freelru.NewSharded[string, *cachedEntry](256, hashFn)

	return &DNSCache{
		messages: messages,
		negative: negative,
		minTTL:   cfg.MinTTL,
		maxTTL:   cfg.MaxTTL,
		staleFor: cfg.StaleFor,
	}
}

// GetPacked retrieves a pre-packed DNS response from cache.
// Returns a copy of the packed bytes with the query ID patched to queryID.
// This is the fast path: pure memcpy, no dns.Msg.Copy() or Pack().
func (c *DNSCache) GetPacked(domain string, qtype uint16, queryID uint16) ([]byte, bool) {
	key := cacheKey(domain, qtype)
	// Check negative cache first.
	// freelru auto-expires, so if we get it, it's within TTL.
	if entry, ok := c.negative.Get(key); ok {
		return cloneAndPatchID(entry.packed, queryID), true
	}

	// Check positive cache — stored with lifetime = ttl + staleFor,
	// so Get() returns the entry during both fresh and stale windows.
	// We check entry.expireAt to distinguish fresh from stale.
	entry, ok := c.messages.Get(key)
	if !ok {
		return nil, false
	}
	if time.Now().After(entry.expireAt) {
		// Expired original TTL — stale, not fresh.
		return nil, false
	}

	return cloneAndPatchID(entry.packed, queryID), true
}

// GetStalePacked returns an expired-but-within-stale-window entry from positive
// cache only. Negative cache (NXDOMAIN) entries are never returned stale because
// the domain may have recovered. Returns packed bytes with queryID patched.
func (c *DNSCache) GetStalePacked(domain string, qtype uint16, queryID uint16) ([]byte, bool) {
	key := cacheKey(domain, qtype)

	// Entry is stored with lifetime = ttl + staleFor. Get() returns the entry
	// if it's within that extended lifetime. We check entry.expireAt to see
	// if the original TTL has expired (making it stale but still servable).
	entry, ok := c.messages.Get(key)
	if !ok {
		return nil, false
	}

	// Must be past original TTL expiry (stale) but still within freelru lifetime.
	if time.Now().After(entry.expireAt) {
		return cloneAndPatchID(entry.packed, queryID), true
	}

	return nil, false
}

// GetPackedNearExpiry retrieves a fresh cached entry and reports whether it is
// near expiry (remaining TTL < threshold fraction of original TTL).
// Returns (nil, false) on cache miss or expired entry.
// Returns (packed, false) for fresh entries with plenty of TTL.
// Returns (packed, true) for fresh entries nearing expiry.
// Negative cache (NXDOMAIN) never returns nearExpiry=true.
func (c *DNSCache) GetPackedNearExpiry(domain string, qtype uint16, queryID uint16, threshold float64) ([]byte, bool) {
	key := cacheKey(domain, qtype)

	// Check negative cache first — never triggers prefresh.
	if entry, ok := c.negative.Get(key); ok {
		return cloneAndPatchID(entry.packed, queryID), false
	}

	entry, ok := c.messages.Get(key)
	if !ok {
		return nil, false
	}

	now := time.Now()
	if now.After(entry.expireAt) {
		return nil, false // expired — not fresh
	}

	// Check if near expiry.
	remaining := entry.expireAt.Sub(now)
	if entry.originalTTL <= 0 {
		// Defensive: no originalTTL → treat as not near expiry.
		return cloneAndPatchID(entry.packed, queryID), false
	}

	nearExpiry := float64(remaining)/float64(entry.originalTTL) < threshold
	return cloneAndPatchID(entry.packed, queryID), nearExpiry
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

	// Handle NXDOMAIN (negative caching per RFC 2308)
	if msg.Rcode == dns.RcodeNameError {
		ttl := c.extractNegativeTTL(msg)
		// Pre-pack NXDOMAIN response with SOA for negative cache.
		nxResp := createNXDOMAINWithSOA(&dns.Msg{Question: msg.Question})
		packed, err := nxResp.Pack()
		if err != nil {
			return
		}
		c.negative.AddWithLifetime(key, &cachedEntry{
			packed:   packed,
			expireAt: time.Now().Add(ttl),
		}, ttl)
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

	// Store with extended lifetime (ttl + staleFor) so entry survives
	// in freelru during the stale-while-revalidate window.
	c.messages.AddWithLifetime(key, &cachedEntry{
		packed:      packed,
		expireAt:    time.Now().Add(ttl),
		originalTTL: ttl,
	}, ttl+c.staleFor)
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

// Close releases cached entries.
func (c *DNSCache) Close() {
	c.messages.Purge()
	c.negative.Purge()
}

// Size returns current cache size
func (c *DNSCache) Size() (positive, negative int) {
	return c.messages.Len(), c.negative.Len()
}

// cacheKey creates a cache key from domain and query type
func cacheKey(domain string, qtype uint16) string {
	return domain + ":" + strconv.FormatUint(uint64(qtype), 10)
}
