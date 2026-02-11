package dns

import (
	"log"
	"sync"
	"time"

	"github.com/kaitu-io/k2rule"
)

// IPRule represents a cached IP routing rule with TTL
type IPRule struct {
	Domain   string         // Original domain name
	Action   k2rule.Target  // Routing action (direct/proxy/reject)
	ExpireAt time.Time      // TTL expiration timestamp
}

// DynamicIPCache stores IP→Action mappings from DNS responses with TTL expiration
type DynamicIPCache struct {
	mu    sync.RWMutex
	rules map[string]IPRule // IP string → Rule
}

// NewDynamicIPCache creates a new dynamic IP cache
func NewDynamicIPCache() *DynamicIPCache {
	return &DynamicIPCache{
		rules: make(map[string]IPRule),
	}
}

// Add adds or updates an IP rule in the cache
func (c *DynamicIPCache) Add(ip string, rule IPRule) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.rules[ip] = rule
	log.Printf("Dynamic cache: add %s → %s (domain: %s, TTL: %v)",
		ip, rule.Action, rule.Domain, time.Until(rule.ExpireAt))
}

// GetRule retrieves an IP rule from the cache (internal use)
// Returns (rule, true) if found and not expired
// Returns (empty, false) if not found or expired
func (c *DynamicIPCache) GetRule(ip string) (IPRule, bool) {
	c.mu.RLock()
	rule, found := c.rules[ip]
	c.mu.RUnlock()

	if !found {
		return IPRule{}, false
	}

	// Check TTL expiration
	if time.Now().After(rule.ExpireAt) {
		// Expired - remove and return not found
		c.mu.Lock()
		delete(c.rules, ip)
		c.mu.Unlock()
		return IPRule{}, false
	}

	return rule, true
}

// Get retrieves a cached rule for router use
// Returns (domain, action, found)
func (c *DynamicIPCache) Get(ip string) (string, k2rule.Target, bool) {
	rule, found := c.GetRule(ip)
	if !found {
		return "", k2rule.TargetProxy, false
	}
	return rule.Domain, rule.Action, true
}

// Delete removes an IP from the cache
func (c *DynamicIPCache) Delete(ip string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.rules, ip)
}

// Size returns the current number of cached rules
func (c *DynamicIPCache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.rules)
}

// StartCleanup starts a background goroutine to periodically clean up expired entries
func (c *DynamicIPCache) StartCleanup(interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		for range ticker.C {
			c.cleanup()
		}
	}()
}

// cleanup removes all expired entries from the cache
func (c *DynamicIPCache) cleanup() {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	removed := 0

	for ip, rule := range c.rules {
		if now.After(rule.ExpireAt) {
			delete(c.rules, ip)
			removed++
		}
	}

	if removed > 0 {
		log.Printf("Dynamic cache cleanup: removed %d expired entries, %d remaining",
			removed, len(c.rules))
	}
}
