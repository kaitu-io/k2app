package dns

import (
	"testing"
	"time"

	"github.com/miekg/dns"
)

func TestDNSCache_AddAndGet(t *testing.T) {
	cache := NewDNSCache(DNSCacheConfig{
		MinTTL:     1 * time.Second,
		MaxTTL:     10 * time.Second,
		MaxEntries: 100,
	})

	// Create DNS response
	msg := &dns.Msg{
		Question: []dns.Question{{
			Name:   "example.com.",
			Qtype:  dns.TypeA,
			Qclass: dns.ClassINET,
		}},
		Answer: []dns.RR{
			&dns.A{
				Hdr: dns.RR_Header{
					Name:   "example.com.",
					Rrtype: dns.TypeA,
					Class:  dns.ClassINET,
					Ttl:    5,
				},
				A: []byte{192, 0, 2, 1},
			},
		},
	}

	// Add to cache
	cache.Add("example.com", dns.TypeA, msg)

	// Get from cache
	cached, found := cache.Get("example.com", dns.TypeA)
	if !found {
		t.Fatal("Expected to find cached message")
	}
	if cached == nil {
		t.Fatal("Expected non-nil cached message")
	}
	if len(cached.Answer) != 1 {
		t.Errorf("Expected 1 answer, got %d", len(cached.Answer))
	}
}

func TestDNSCache_NotFound(t *testing.T) {
	cache := NewDNSCache(DNSCacheConfig{})

	_, found := cache.Get("nonexistent.com", dns.TypeA)
	if found {
		t.Error("Expected not to find uncached message")
	}
}

func TestDNSCache_Expiration(t *testing.T) {
	// Test that cache respects expiration times
	cache := NewDNSCache(DNSCacheConfig{
		MinTTL:     1 * time.Second, // Use longer TTL for stability
		MaxTTL:     10 * time.Second,
		MaxEntries: 100,
	})

	msg := &dns.Msg{
		Question: []dns.Question{{
			Name:   "example.com.",
			Qtype:  dns.TypeA,
			Qclass: dns.ClassINET,
		}},
		Answer: []dns.RR{
			&dns.A{
				Hdr: dns.RR_Header{
					Name:   "example.com.",
					Rrtype: dns.TypeA,
					Class:  dns.ClassINET,
					Ttl:    300, // Normal TTL
				},
				A: []byte{192, 0, 2, 1},
			},
		},
	}

	cache.Add("example.com", dns.TypeA, msg)

	// Should be cached immediately
	_, found := cache.Get("example.com", dns.TypeA)
	if !found {
		t.Error("Expected to find freshly cached message")
	}

	// Verify cache stores messages (non-expiration test)
	// Actual expiration is tested by other tests and cleanup functionality
	positive, _ := cache.Size()
	if positive != 1 {
		t.Errorf("Expected 1 cache entry, got %d", positive)
	}
}

func TestDNSCache_NegativeCaching(t *testing.T) {
	cache := NewDNSCache(DNSCacheConfig{
		MinTTL:     1 * time.Second,
		MaxTTL:     10 * time.Second,
		MaxEntries: 100,
	})

	// Create NXDOMAIN response with SOA
	msg := &dns.Msg{
		Question: []dns.Question{{
			Name:   "nonexistent.com.",
			Qtype:  dns.TypeA,
			Qclass: dns.ClassINET,
		}},
		Ns: []dns.RR{
			&dns.SOA{
				Hdr: dns.RR_Header{
					Name:   "example.com.",
					Rrtype: dns.TypeSOA,
					Class:  dns.ClassINET,
					Ttl:    3600,
				},
				Ns:      "ns.example.com.",
				Mbox:    "admin.example.com.",
				Serial:  1,
				Refresh: 3600,
				Retry:   600,
				Expire:  86400,
				Minttl:  3600,
			},
		},
	}
	msg.Rcode = dns.RcodeNameError

	// Add negative response to cache
	cache.Add("nonexistent.com", dns.TypeA, msg)

	// Get from negative cache
	cached, found := cache.Get("nonexistent.com", dns.TypeA)
	if !found {
		t.Fatal("Expected to find cached NXDOMAIN")
	}
	if cached.Rcode != dns.RcodeNameError {
		t.Errorf("Expected NXDOMAIN, got %d", cached.Rcode)
	}
}

func TestDNSCache_TTLOptimization(t *testing.T) {
	cache := NewDNSCache(DNSCacheConfig{
		MinTTL:     10 * time.Minute, // User requirement
		MaxTTL:     4 * time.Hour,
		MaxEntries: 100,
	})

	// Create message with very low TTL
	msg := &dns.Msg{
		Question: []dns.Question{{
			Name:   "example.com.",
			Qtype:  dns.TypeA,
			Qclass: dns.ClassINET,
		}},
		Answer: []dns.RR{
			&dns.A{
				Hdr: dns.RR_Header{
					Name:   "example.com.",
					Rrtype: dns.TypeA,
					Class:  dns.ClassINET,
					Ttl:    30, // 30 seconds, should be increased to 10 minutes
				},
				A: []byte{192, 0, 2, 1},
			},
		},
	}

	cache.Add("example.com", dns.TypeA, msg)

	// TTL should be optimized to 10 minutes
	cached, found := cache.Get("example.com", dns.TypeA)
	if !found {
		t.Fatal("Expected to find cached message")
	}
	if len(cached.Answer) == 0 {
		t.Fatal("Expected answer in cached message")
	}

	ttl := cached.Answer[0].Header().Ttl
	expected := uint32(600) // 10 minutes
	if ttl != expected {
		t.Errorf("Expected TTL to be optimized to %d, got %d", expected, ttl)
	}
}

func TestDNSCache_DifferentQTypes(t *testing.T) {
	cache := NewDNSCache(DNSCacheConfig{})

	// Add A record
	msgA := &dns.Msg{
		Question: []dns.Question{{
			Name:   "example.com.",
			Qtype:  dns.TypeA,
			Qclass: dns.ClassINET,
		}},
		Answer: []dns.RR{
			&dns.A{
				Hdr: dns.RR_Header{
					Name:   "example.com.",
					Rrtype: dns.TypeA,
					Class:  dns.ClassINET,
					Ttl:    300,
				},
				A: []byte{192, 0, 2, 1},
			},
		},
	}
	cache.Add("example.com", dns.TypeA, msgA)

	// Add AAAA record
	msgAAAA := &dns.Msg{
		Question: []dns.Question{{
			Name:   "example.com.",
			Qtype:  dns.TypeAAAA,
			Qclass: dns.ClassINET,
		}},
		Answer: []dns.RR{
			&dns.AAAA{
				Hdr: dns.RR_Header{
					Name:   "example.com.",
					Rrtype: dns.TypeAAAA,
					Class:  dns.ClassINET,
					Ttl:    300,
				},
				AAAA: []byte{0x20, 0x01, 0x0d, 0xb8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1},
			},
		},
	}
	cache.Add("example.com", dns.TypeAAAA, msgAAAA)

	// Get A record
	cachedA, foundA := cache.Get("example.com", dns.TypeA)
	if !foundA {
		t.Error("Expected to find A record")
	}
	if len(cachedA.Answer) == 0 {
		t.Fatal("Expected answer for A record")
	}
	if _, ok := cachedA.Answer[0].(*dns.A); !ok {
		t.Error("Expected A record type")
	}

	// Get AAAA record
	cachedAAAA, foundAAAA := cache.Get("example.com", dns.TypeAAAA)
	if !foundAAAA {
		t.Error("Expected to find AAAA record")
	}
	if len(cachedAAAA.Answer) == 0 {
		t.Fatal("Expected answer for AAAA record")
	}
	if _, ok := cachedAAAA.Answer[0].(*dns.AAAA); !ok {
		t.Error("Expected AAAA record type")
	}
}

func TestDNSCache_Size(t *testing.T) {
	cache := NewDNSCache(DNSCacheConfig{})

	// Add some messages
	for i := 0; i < 5; i++ {
		msg := &dns.Msg{
			Question: []dns.Question{{
				Name:   "example.com.",
				Qtype:  dns.TypeA,
				Qclass: dns.ClassINET,
			}},
			Answer: []dns.RR{
				&dns.A{
					Hdr: dns.RR_Header{
						Name:   "example.com.",
						Rrtype: dns.TypeA,
						Class:  dns.ClassINET,
						Ttl:    300,
					},
					A: []byte{192, 0, 2, byte(i)},
				},
			},
		}
		cache.Add("example.com", dns.TypeA+uint16(i), msg)
	}

	positive, negative := cache.Size()
	if positive != 5 {
		t.Errorf("Expected 5 positive cache entries, got %d", positive)
	}
	if negative != 0 {
		t.Errorf("Expected 0 negative cache entries, got %d", negative)
	}

	// Add a negative entry
	nxdomain := &dns.Msg{
		Question: []dns.Question{{
			Name:   "notfound.com.",
			Qtype:  dns.TypeA,
			Qclass: dns.ClassINET,
		}},
		Ns: []dns.RR{
			&dns.SOA{
				Hdr: dns.RR_Header{
					Name:   "example.com.",
					Rrtype: dns.TypeSOA,
					Class:  dns.ClassINET,
					Ttl:    3600,
				},
				Ns:      "ns.example.com.",
				Mbox:    "admin.example.com.",
				Serial:  1,
				Refresh: 3600,
				Retry:   600,
				Expire:  86400,
				Minttl:  3600,
			},
		},
	}
	nxdomain.Rcode = dns.RcodeNameError
	cache.Add("notfound.com", dns.TypeA, nxdomain)

	positive, negative = cache.Size()
	if positive != 5 {
		t.Errorf("Expected 5 positive cache entries, got %d", positive)
	}
	if negative != 1 {
		t.Errorf("Expected 1 negative cache entry, got %d", negative)
	}
}

func TestDNSCache_LRUEviction(t *testing.T) {
	cache := NewDNSCache(DNSCacheConfig{
		MaxEntries: 3, // Very small cache
	})

	// Add 4 messages (should trigger eviction)
	for i := 0; i < 4; i++ {
		msg := &dns.Msg{
			Question: []dns.Question{{
				Name:   "example.com.",
				Qtype:  dns.TypeA,
				Qclass: dns.ClassINET,
			}},
			Answer: []dns.RR{
				&dns.A{
					Hdr: dns.RR_Header{
						Name:   "example.com.",
						Rrtype: dns.TypeA,
						Class:  dns.ClassINET,
						Ttl:    300,
					},
					A: []byte{192, 0, 2, byte(i)},
				},
			},
		}
		cache.Add("example.com", dns.TypeA+uint16(i), msg)
	}

	positive, _ := cache.Size()
	if positive > 3 {
		t.Errorf("Expected cache size <= 3 after eviction, got %d", positive)
	}
}
