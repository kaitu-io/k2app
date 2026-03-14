package dns

import (
	"net"
	"sync"
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

func TestDNSCache_GetStalePacked_ReturnsExpiredEntry(t *testing.T) {
	cache := NewDNSCache(DNSCacheConfig{
		MinTTL:     1 * time.Second,
		MaxTTL:     10 * time.Second,
		MaxEntries: 100,
		StaleFor:   5 * time.Minute,
	})

	msg := &dns.Msg{
		Question: []dns.Question{{Name: "example.com.", Qtype: dns.TypeA, Qclass: dns.ClassINET}},
		Answer: []dns.RR{&dns.A{
			Hdr: dns.RR_Header{Name: "example.com.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 1},
			A:   []byte{192, 0, 2, 1},
		}},
	}
	cache.Add("example.com", dns.TypeA, msg)

	// Wait for entry to expire
	time.Sleep(1500 * time.Millisecond)

	// Fresh lookup should miss
	_, found := cache.GetPacked("example.com", dns.TypeA, 1234)
	if found {
		t.Error("Expected GetPacked to miss for expired entry")
	}

	// Stale lookup should hit
	packed, found := cache.GetStalePacked("example.com", dns.TypeA, 1234)
	if !found {
		t.Fatal("Expected GetStalePacked to return expired entry within stale window")
	}
	if packed == nil {
		t.Fatal("Expected non-nil packed bytes")
	}
}

func TestDNSCache_GetStalePacked_NotForFreshEntry(t *testing.T) {
	cache := NewDNSCache(DNSCacheConfig{
		MinTTL:     10 * time.Minute,
		MaxEntries: 100,
		StaleFor:   5 * time.Minute,
	})

	msg := &dns.Msg{
		Question: []dns.Question{{Name: "fresh.com.", Qtype: dns.TypeA, Qclass: dns.ClassINET}},
		Answer: []dns.RR{&dns.A{
			Hdr: dns.RR_Header{Name: "fresh.com.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 600},
			A:   []byte{1, 2, 3, 4},
		}},
	}
	cache.Add("fresh.com", dns.TypeA, msg)

	// Entry is fresh — stale should return false
	_, found := cache.GetStalePacked("fresh.com", dns.TypeA, 1234)
	if found {
		t.Error("Expected GetStalePacked to return false for fresh entry")
	}
}

func TestDNSCache_GetStalePacked_NotPastStaleWindow(t *testing.T) {
	cache := NewDNSCache(DNSCacheConfig{
		MinTTL:     1 * time.Second,
		MaxTTL:     10 * time.Second,
		MaxEntries: 100,
		StaleFor:   1 * time.Millisecond, // Tiny stale window
	})

	msg := &dns.Msg{
		Question: []dns.Question{{Name: "old.com.", Qtype: dns.TypeA, Qclass: dns.ClassINET}},
		Answer: []dns.RR{&dns.A{
			Hdr: dns.RR_Header{Name: "old.com.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 1},
			A:   []byte{1, 2, 3, 4},
		}},
	}
	cache.Add("old.com", dns.TypeA, msg)

	// Wait for both TTL and stale window to expire
	time.Sleep(1500 * time.Millisecond)

	_, found := cache.GetStalePacked("old.com", dns.TypeA, 1234)
	if found {
		t.Error("Expected GetStalePacked to return false past stale window")
	}
}

func TestDNSCache_GetStalePacked_NeverReturnsNXDOMAIN(t *testing.T) {
	cache := NewDNSCache(DNSCacheConfig{
		MinTTL:     1 * time.Second,
		MaxTTL:     10 * time.Second,
		MaxEntries: 100,
		StaleFor:   5 * time.Minute,
	})

	// Add NXDOMAIN to negative cache
	nxMsg := &dns.Msg{
		Question: []dns.Question{{Name: "gone.com.", Qtype: dns.TypeA, Qclass: dns.ClassINET}},
		Ns: []dns.RR{&dns.SOA{
			Hdr:    dns.RR_Header{Name: "com.", Rrtype: dns.TypeSOA, Class: dns.ClassINET, Ttl: 1},
			Ns:     "ns.com.", Mbox: "admin.com.",
			Serial: 1, Refresh: 3600, Retry: 600, Expire: 86400, Minttl: 1,
		}},
	}
	nxMsg.Rcode = dns.RcodeNameError
	cache.Add("gone.com", dns.TypeA, nxMsg)

	// Wait for negative cache entry to expire
	time.Sleep(1500 * time.Millisecond)

	// Stale should never return NXDOMAIN entries
	_, found := cache.GetStalePacked("gone.com", dns.TypeA, 1234)
	if found {
		t.Error("Expected GetStalePacked to never return NXDOMAIN entries")
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

func buildAResponseForCache(domain string, ip net.IP) *dns.Msg {
	resp := new(dns.Msg)
	q := new(dns.Msg)
	q.SetQuestion(dns.Fqdn(domain), dns.TypeA)
	resp.SetReply(q)
	resp.Answer = append(resp.Answer, &dns.A{
		Hdr: dns.RR_Header{
			Name:   dns.Fqdn(domain),
			Rrtype: dns.TypeA,
			Class:  dns.ClassINET,
			Ttl:    300,
		},
		A: ip.To4(),
	})
	return resp
}

func TestDNSCache_GetPackedNearExpiry_ReturnsFreshAndAge(t *testing.T) {
	cache := NewDNSCache(DNSCacheConfig{
		MinTTL: 4 * time.Second,
		MaxTTL: 4 * time.Second,
	})

	resp := buildAResponseForCache("age.example.com", net.ParseIP("1.2.3.4"))
	cache.Add("age.example.com", dns.TypeA, resp)

	// Immediately: should be fresh, not near expiry
	packed, nearExpiry := cache.GetPackedNearExpiry("age.example.com", dns.TypeA, 1234, 0.25)
	if packed == nil {
		t.Fatal("expected cached entry")
	}
	if nearExpiry {
		t.Error("should NOT be near expiry immediately after add")
	}

	// Wait until ~80% of TTL elapsed (3.2s of 4s → 0.8s left = 20% < 25%)
	time.Sleep(3300 * time.Millisecond)

	packed, nearExpiry = cache.GetPackedNearExpiry("age.example.com", dns.TypeA, 1234, 0.25)
	if packed == nil {
		t.Fatal("expected cached entry still fresh")
	}
	if !nearExpiry {
		t.Error("should be near expiry with < 25% TTL remaining")
	}
}

func TestDNSCache_GetPackedNearExpiry_NegativeCache_NeverNearExpiry(t *testing.T) {
	cache := NewDNSCache(DNSCacheConfig{
		MinTTL: 4 * time.Second,
		MaxTTL: 4 * time.Second,
	})

	// Add NXDOMAIN response
	nxResp := new(dns.Msg)
	q := new(dns.Msg)
	q.SetQuestion(dns.Fqdn("gone.example.com"), dns.TypeA)
	nxResp.SetReply(q)
	nxResp.Rcode = dns.RcodeNameError
	nxResp.Ns = []dns.RR{&dns.SOA{
		Hdr:    dns.RR_Header{Name: "example.com.", Rrtype: dns.TypeSOA, Class: dns.ClassINET, Ttl: 4},
		Ns:     "ns.example.com.", Mbox: "admin.example.com.",
		Serial: 1, Refresh: 3600, Retry: 600, Expire: 86400, Minttl: 4,
	}}
	cache.Add("gone.example.com", dns.TypeA, nxResp)

	// NXDOMAIN should return packed but never nearExpiry (no prefresh for negative cache)
	packed, nearExpiry := cache.GetPackedNearExpiry("gone.example.com", dns.TypeA, 1234, 0.25)
	if packed == nil {
		t.Fatal("expected negative cache entry")
	}
	if nearExpiry {
		t.Error("NXDOMAIN should never be marked near-expiry")
	}
}

func TestDNSCache_ConcurrentAccess(t *testing.T) {
	cache := NewDNSCache(DNSCacheConfig{
		MinTTL:     1 * time.Second,
		MaxTTL:     10 * time.Second,
		MaxEntries: 1000,
		StaleFor:   5 * time.Minute,
	})

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			msg := &dns.Msg{
				Question: []dns.Question{{Name: "example.com.", Qtype: dns.TypeA, Qclass: dns.ClassINET}},
				Answer: []dns.RR{&dns.A{
					Hdr: dns.RR_Header{Name: "example.com.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 300},
					A:   []byte{192, 0, 2, byte(idx % 256)},
				}},
			}
			cache.Add("example.com", dns.TypeA+uint16(idx%10), msg)
			cache.GetPacked("example.com", dns.TypeA+uint16(idx%10), uint16(idx))
			cache.GetStalePacked("example.com", dns.TypeA+uint16(idx%10), uint16(idx))
			cache.Size()
		}(i)
	}
	wg.Wait()
}
