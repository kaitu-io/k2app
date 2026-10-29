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
	m.IncrStaleCacheHit()
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
	if m.StaleCacheHits != 1 {
		t.Errorf("StaleCacheHits = %d, want 1", m.StaleCacheHits)
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

func TestMetrics_NilReceiver(t *testing.T) {
	// All methods must be safe to call on nil *Metrics.
	var m *Metrics
	m.IncrTotal()
	m.IncrDirect()
	m.IncrProxy()
	m.IncrHostsHit()
	m.IncrSpecialUseHit()
	m.IncrLocalDomainHit()
	m.IncrNoiseFiltered()
	m.IncrMessageCacheHit()
	m.IncrStaleCacheHit()
	m.IncrError()
	m.Reset()

	if r := m.FilterRate(); r != 0 {
		t.Errorf("FilterRate() on nil = %f, want 0", r)
	}
	if r := m.CacheHitRate(); r != 0 {
		t.Errorf("CacheHitRate() on nil = %f, want 0", r)
	}
	if r := m.UpstreamRate(); r != 0 {
		t.Errorf("UpstreamRate() on nil = %f, want 0", r)
	}
	if s := m.String(); s != "DNS Metrics: <nil>" {
		t.Errorf("String() on nil = %q, want %q", s, "DNS Metrics: <nil>")
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

