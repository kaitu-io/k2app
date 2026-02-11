package dns

import (
	"fmt"
	"sync"
	"time"

	"github.com/miekg/dns"
)

// CachedMessage represents a cached DNS message with expiration
type CachedMessage struct {
	Message  *dns.Msg
	ExpireAt time.Time
}

// DNSCache implements DNS message caching with negative caching (RFC 2308)
type DNSCache struct {
	mu sync.RWMutex

	// Positive cache: domain+qtype → cached response
	messages map[string]CachedMessage

	// Negative cache: domain+qtype → NXDOMAIN expiration
	negative map[string]time.Time

	// Configuration
	minTTL     time.Duration // Minimum TTL (user requirement: 10 minutes)
	maxTTL     time.Duration // Maximum TTL
	maxEntries int           // Max cache entries (LRU eviction)
}

// DNSCacheConfig contains DNS cache configuration
type DNSCacheConfig struct {
	MinTTL     time.Duration // Minimum TTL (default: 10 minutes)
	MaxTTL     time.Duration // Maximum TTL (default: 4 hours)
	MaxEntries int           // Max cache entries (default: 5000)
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

	return &DNSCache{
		messages:   make(map[string]CachedMessage),
		negative:   make(map[string]time.Time),
		minTTL:     cfg.MinTTL,
		maxTTL:     cfg.MaxTTL,
		maxEntries: cfg.MaxEntries,
	}
}

// Get retrieves a cached DNS message
// Returns (message, true) if found and not expired
// Returns (nil, false) if not found or expired
func (c *DNSCache) Get(domain string, qtype uint16) (*dns.Msg, bool) {
	key := cacheKey(domain, qtype)

	c.mu.RLock()
	defer c.mu.RUnlock()

	// Check negative cache first
	if expireAt, found := c.negative[key]; found {
		if time.Now().Before(expireAt) {
			// Negative cache hit - create NXDOMAIN response
			return createNXDOMAINWithSOA(&dns.Msg{
				Question: []dns.Question{{
					Name:   dns.Fqdn(domain),
					Qtype:  qtype,
					Qclass: dns.ClassINET,
				}},
			}), true
		}
		// Expired negative entry - will be cleaned up later
	}

	// Check positive cache
	cached, found := c.messages[key]
	if !found {
		return nil, false
	}

	// Check expiration
	if time.Now().After(cached.ExpireAt) {
		return nil, false
	}

	// Clone message to avoid mutation
	msg := cached.Message.Copy()
	return msg, true
}

// Add adds a DNS message to the cache with TTL optimization
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
		c.negative[key] = time.Now().Add(ttl)
		return
	}

	// Skip caching if no answers
	if len(msg.Answer) == 0 {
		return
	}

	// Optimize TTL (10 minute minimum per user requirement)
	c.optimizeTTL(msg)

	// Calculate expiration based on minimum answer TTL
	ttl := c.extractTTL(msg)
	expireAt := time.Now().Add(ttl)

	// LRU eviction if cache is full
	if len(c.messages) >= c.maxEntries {
		c.evictOldest()
	}

	// Store cached message
	c.messages[key] = CachedMessage{
		Message:  msg.Copy(),
		ExpireAt: expireAt,
	}
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

// evictOldest removes the oldest cache entry (simple LRU)
func (c *DNSCache) evictOldest() {
	if len(c.messages) == 0 {
		return
	}

	// Find entry with earliest expiration
	var oldestKey string
	var oldestTime time.Time

	for key, cached := range c.messages {
		if oldestKey == "" || cached.ExpireAt.Before(oldestTime) {
			oldestKey = key
			oldestTime = cached.ExpireAt
		}
	}

	if oldestKey != "" {
		delete(c.messages, oldestKey)
	}
}

// StartCleanup starts background cleanup of expired entries
func (c *DNSCache) StartCleanup(interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		for range ticker.C {
			c.cleanup()
		}
	}()
}

// cleanup removes all expired entries
func (c *DNSCache) cleanup() {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	removed := 0

	// Clean positive cache
	for key, cached := range c.messages {
		if now.After(cached.ExpireAt) {
			delete(c.messages, key)
			removed++
		}
	}

	// Clean negative cache
	for key, expireAt := range c.negative {
		if now.After(expireAt) {
			delete(c.negative, key)
			removed++
		}
	}

	// No logging to avoid noise
}

// Size returns current cache size
func (c *DNSCache) Size() (positive, negative int) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.messages), len(c.negative)
}

// cacheKey creates a cache key from domain and query type
func cacheKey(domain string, qtype uint16) string {
	return fmt.Sprintf("%s:%d", domain, qtype)
}
