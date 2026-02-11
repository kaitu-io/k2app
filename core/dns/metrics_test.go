package dns

import (
	"sync"
	"testing"
)

func TestMetrics_Counters(t *testing.T) {
	m := NewMetrics()

	// Test increment methods
	m.IncrTotal()
	m.IncrTotal()
	m.IncrDirect()
	m.IncrProxy()
	m.IncrHostsHit()
	m.IncrSpecialUseHit()
	m.IncrLocalDomainHit()
	m.IncrNoiseFiltered()
	m.IncrMessageCacheHit()
	m.IncrError()

	if m.TotalQueries != 2 {
		t.Errorf("TotalQueries = %d, want 2", m.TotalQueries)
	}
	if m.DirectQueries != 1 {
		t.Errorf("DirectQueries = %d, want 1", m.DirectQueries)
	}
	if m.ProxyQueries != 1 {
		t.Errorf("ProxyQueries = %d, want 1", m.ProxyQueries)
	}
	if m.HostsHits != 1 {
		t.Errorf("HostsHits = %d, want 1", m.HostsHits)
	}
	if m.SpecialUseHits != 1 {
		t.Errorf("SpecialUseHits = %d, want 1", m.SpecialUseHits)
	}
	if m.LocalDomainHits != 1 {
		t.Errorf("LocalDomainHits = %d, want 1", m.LocalDomainHits)
	}
	if m.NoiseFiltered != 1 {
		t.Errorf("NoiseFiltered = %d, want 1", m.NoiseFiltered)
	}
	if m.MessageCacheHits != 1 {
		t.Errorf("MessageCacheHits = %d, want 1", m.MessageCacheHits)
	}
	if m.Errors != 1 {
		t.Errorf("Errors = %d, want 1", m.Errors)
	}
}

func TestMetrics_FilterRate(t *testing.T) {
	m := NewMetrics()

	// 10 total queries
	for i := 0; i < 10; i++ {
		m.IncrTotal()
	}

	// 5 filtered (hosts=1, special=1, local=1, noise=1, cache=1)
	m.IncrHostsHit()
	m.IncrSpecialUseHit()
	m.IncrLocalDomainHit()
	m.IncrNoiseFiltered()
	m.IncrMessageCacheHit()

	// Filter rate should be 5/10 = 50%
	rate := m.FilterRate()
	if rate != 50.0 {
		t.Errorf("FilterRate() = %.1f, want 50.0", rate)
	}
}

func TestMetrics_CacheHitRate(t *testing.T) {
	m := NewMetrics()

	// 10 total queries
	for i := 0; i < 10; i++ {
		m.IncrTotal()
	}

	// 3 cache hits
	m.IncrMessageCacheHit()
	m.IncrMessageCacheHit()
	m.IncrMessageCacheHit()

	// Cache hit rate should be 3/10 = 30%
	rate := m.CacheHitRate()
	if rate != 30.0 {
		t.Errorf("CacheHitRate() = %.1f, want 30.0", rate)
	}
}

func TestMetrics_UpstreamRate(t *testing.T) {
	m := NewMetrics()

	// 10 total queries
	for i := 0; i < 10; i++ {
		m.IncrTotal()
	}

	// 2 direct + 3 proxy = 5 upstream
	m.IncrDirect()
	m.IncrDirect()
	m.IncrProxy()
	m.IncrProxy()
	m.IncrProxy()

	// Upstream rate should be 5/10 = 50%
	rate := m.UpstreamRate()
	if rate != 50.0 {
		t.Errorf("UpstreamRate() = %.1f, want 50.0", rate)
	}
}

func TestMetrics_Reset(t *testing.T) {
	m := NewMetrics()

	// Add some metrics
	m.IncrTotal()
	m.IncrDirect()
	m.IncrProxy()
	m.IncrHostsHit()
	m.IncrError()

	// Reset
	m.Reset()

	// All should be zero
	if m.TotalQueries != 0 {
		t.Errorf("After reset, TotalQueries = %d, want 0", m.TotalQueries)
	}
	if m.DirectQueries != 0 {
		t.Errorf("After reset, DirectQueries = %d, want 0", m.DirectQueries)
	}
	if m.ProxyQueries != 0 {
		t.Errorf("After reset, ProxyQueries = %d, want 0", m.ProxyQueries)
	}
	if m.HostsHits != 0 {
		t.Errorf("After reset, HostsHits = %d, want 0", m.HostsHits)
	}
	if m.Errors != 0 {
		t.Errorf("After reset, Errors = %d, want 0", m.Errors)
	}
}

func TestMetrics_String(t *testing.T) {
	m := NewMetrics()

	// Add some metrics
	for i := 0; i < 100; i++ {
		m.IncrTotal()
	}
	m.IncrDirect()         // 1%
	m.IncrProxy()          // 1%
	m.IncrHostsHit()       // 1%
	m.IncrSpecialUseHit()  // 1%
	m.IncrLocalDomainHit() // 1%
	m.IncrNoiseFiltered()  // 1%

	// Total upstream = 2/100 = 2%
	// Total filtered = 4/100 = 4%

	str := m.String()
	if str == "" {
		t.Error("String() returned empty string")
	}

	// Should contain key metrics
	expectedSubstrings := []string{
		"Total=100",
		"Direct=1",
		"Proxy=1",
		"Hosts=1",
		"Special=1",
		"Local=1",
		"Noise=1",
	}

	for _, substr := range expectedSubstrings {
		if !contains(str, substr) {
			t.Errorf("String() missing %q: %s", substr, str)
		}
	}
}

func TestMetrics_Concurrent(t *testing.T) {
	m := NewMetrics()

	// Concurrent increments
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.IncrTotal()
			m.IncrDirect()
			m.IncrProxy()
			m.IncrHostsHit()
		}()
	}

	wg.Wait()

	// All counters should be 100
	if m.TotalQueries != 100 {
		t.Errorf("TotalQueries = %d, want 100", m.TotalQueries)
	}
	if m.DirectQueries != 100 {
		t.Errorf("DirectQueries = %d, want 100", m.DirectQueries)
	}
	if m.ProxyQueries != 100 {
		t.Errorf("ProxyQueries = %d, want 100", m.ProxyQueries)
	}
	if m.HostsHits != 100 {
		t.Errorf("HostsHits = %d, want 100", m.HostsHits)
	}
}

func TestMetrics_ZeroTotal(t *testing.T) {
	m := NewMetrics()

	// With zero total queries, rates should be 0
	if m.FilterRate() != 0 {
		t.Errorf("FilterRate() with zero total = %.1f, want 0", m.FilterRate())
	}
	if m.CacheHitRate() != 0 {
		t.Errorf("CacheHitRate() with zero total = %.1f, want 0", m.CacheHitRate())
	}
	if m.UpstreamRate() != 0 {
		t.Errorf("UpstreamRate() with zero total = %.1f, want 0", m.UpstreamRate())
	}
}

// Helper function
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
