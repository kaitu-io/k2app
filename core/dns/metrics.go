package dns

import (
	"fmt"
	"sync/atomic"
)

// Metrics tracks DNS middleware statistics
type Metrics struct {
	// Query counts
	TotalQueries   uint64
	DirectQueries  uint64
	ProxyQueries   uint64

	// Filter hits
	HostsHits       uint64
	SpecialUseHits  uint64
	LocalDomainHits uint64
	NoiseFiltered   uint64

	// Cache hits
	MessageCacheHits uint64
	IPCacheHits      uint64

	// Errors
	Errors uint64
}

// NewMetrics creates a new metrics tracker
func NewMetrics() *Metrics {
	return &Metrics{}
}

// IncrTotal increments total query count
func (m *Metrics) IncrTotal() {
	atomic.AddUint64(&m.TotalQueries, 1)
}

// IncrDirect increments direct DNS query count
func (m *Metrics) IncrDirect() {
	atomic.AddUint64(&m.DirectQueries, 1)
}

// IncrProxy increments proxy DNS query count
func (m *Metrics) IncrProxy() {
	atomic.AddUint64(&m.ProxyQueries, 1)
}

// IncrHostsHit increments hosts file hit count
func (m *Metrics) IncrHostsHit() {
	atomic.AddUint64(&m.HostsHits, 1)
}

// IncrSpecialUseHit increments RFC 6761 special-use hit count
func (m *Metrics) IncrSpecialUseHit() {
	atomic.AddUint64(&m.SpecialUseHits, 1)
}

// IncrLocalDomainHit increments local domain hit count
func (m *Metrics) IncrLocalDomainHit() {
	atomic.AddUint64(&m.LocalDomainHits, 1)
}

// IncrNoiseFiltered increments noise filter count
func (m *Metrics) IncrNoiseFiltered() {
	atomic.AddUint64(&m.NoiseFiltered, 1)
}

// IncrMessageCacheHit increments DNS message cache hit count
func (m *Metrics) IncrMessageCacheHit() {
	atomic.AddUint64(&m.MessageCacheHits, 1)
}

// IncrIPCacheHit increments IP cache hit count (for router metrics)
func (m *Metrics) IncrIPCacheHit() {
	atomic.AddUint64(&m.IPCacheHits, 1)
}

// IncrError increments error count
func (m *Metrics) IncrError() {
	atomic.AddUint64(&m.Errors, 1)
}

// FilterRate calculates the percentage of queries handled by filters
// (hosts, special-use, local, noise, cache) vs total queries
func (m *Metrics) FilterRate() float64 {
	total := atomic.LoadUint64(&m.TotalQueries)
	if total == 0 {
		return 0
	}

	filtered := atomic.LoadUint64(&m.HostsHits) +
		atomic.LoadUint64(&m.SpecialUseHits) +
		atomic.LoadUint64(&m.LocalDomainHits) +
		atomic.LoadUint64(&m.NoiseFiltered) +
		atomic.LoadUint64(&m.MessageCacheHits)

	return float64(filtered) / float64(total) * 100.0
}

// CacheHitRate calculates the cache hit rate
func (m *Metrics) CacheHitRate() float64 {
	total := atomic.LoadUint64(&m.TotalQueries)
	if total == 0 {
		return 0
	}

	hits := atomic.LoadUint64(&m.MessageCacheHits)
	return float64(hits) / float64(total) * 100.0
}

// UpstreamRate calculates the percentage of queries sent to upstream DNS
func (m *Metrics) UpstreamRate() float64 {
	total := atomic.LoadUint64(&m.TotalQueries)
	if total == 0 {
		return 0
	}

	upstream := atomic.LoadUint64(&m.DirectQueries) +
		atomic.LoadUint64(&m.ProxyQueries)

	return float64(upstream) / float64(total) * 100.0
}

// String returns a human-readable metrics summary
func (m *Metrics) String() string {
	total := atomic.LoadUint64(&m.TotalQueries)
	direct := atomic.LoadUint64(&m.DirectQueries)
	proxy := atomic.LoadUint64(&m.ProxyQueries)
	hosts := atomic.LoadUint64(&m.HostsHits)
	special := atomic.LoadUint64(&m.SpecialUseHits)
	local := atomic.LoadUint64(&m.LocalDomainHits)
	noise := atomic.LoadUint64(&m.NoiseFiltered)
	msgCache := atomic.LoadUint64(&m.MessageCacheHits)
	errors := atomic.LoadUint64(&m.Errors)

	return fmt.Sprintf(
		"DNS Metrics: Total=%d Direct=%d Proxy=%d | "+
			"Filters: Hosts=%d Special=%d Local=%d Noise=%d Cache=%d | "+
			"Rates: Filter=%.1f%% Upstream=%.1f%% Errors=%d",
		total, direct, proxy,
		hosts, special, local, noise, msgCache,
		m.FilterRate(), m.UpstreamRate(), errors,
	)
}

// Reset resets all metrics to zero
func (m *Metrics) Reset() {
	atomic.StoreUint64(&m.TotalQueries, 0)
	atomic.StoreUint64(&m.DirectQueries, 0)
	atomic.StoreUint64(&m.ProxyQueries, 0)
	atomic.StoreUint64(&m.HostsHits, 0)
	atomic.StoreUint64(&m.SpecialUseHits, 0)
	atomic.StoreUint64(&m.LocalDomainHits, 0)
	atomic.StoreUint64(&m.NoiseFiltered, 0)
	atomic.StoreUint64(&m.MessageCacheHits, 0)
	atomic.StoreUint64(&m.IPCacheHits, 0)
	atomic.StoreUint64(&m.Errors, 0)
}
