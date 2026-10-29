package server

import (
	"context"
	"fmt"
	"log/slog"
	"net"

	k2dns "github.com/kaitu-io/k2/core/dns"
	"github.com/miekg/dns"
)

// Resolver resolves domain names using the shared DNS cache and a DNS querier.
// Cache hit: unpack A records from cached response (no network I/O).
// Cache miss: query upstream via DNSQuerier (DirectDNSClient with health-aware
// racing, EWMA latency tracking, degradation recovery, and TC→TCP retry).
// Shared cache means handleDNSUDP and Resolver benefit from each other's queries.
type Resolver struct {
	cache  *k2dns.DNSCache
	client k2dns.DNSQuerier
}

// NewResolver creates a Resolver that shares the given DNS cache.
// The client is typically a DirectDNSClient configured with global DNS servers.
func NewResolver(cache *k2dns.DNSCache, client k2dns.DNSQuerier) *Resolver {
	return &Resolver{cache: cache, client: client}
}

// Client returns the underlying DNS querier. Used by handleDNSUDP for the
// packed-bytes hot path: cache hits return pre-packed bytes via DNSCache.GetPacked,
// misses call Client().Query(...) and cache the response. Exposing the client
// instead of wrapping every call avoids an unnecessary Pack/Unpack round trip.
func (r *Resolver) Client() k2dns.DNSQuerier {
	return r.client
}

// LookupHost resolves a hostname to IP addresses.
// If host is already an IP literal, returns it directly.
// Otherwise checks DNS cache, then queries upstream on miss.
func (r *Resolver) LookupHost(ctx context.Context, host string) ([]string, error) {
	// Fast path: IP literal.
	if net.ParseIP(host) != nil {
		return []string{host}, nil
	}

	fqdn := dns.Fqdn(host) // ensure trailing dot

	// Cache lookup: unpack A records from cached response.
	if msg, ok := r.cache.Get(fqdn, dns.TypeA); ok {
		ips := extractARecords(msg)
		if len(ips) > 0 {
			slog.Info("DIAG: dns-resolve-cache-hit", "host", host, "ips", len(ips))
			return ips, nil
		}
	}

	// Cache miss: query upstream via DNSQuerier.
	query := new(dns.Msg)
	query.SetQuestion(fqdn, dns.TypeA)
	query.RecursionDesired = true

	resp, err := r.client.Query(ctx, query)
	if err != nil {
		slog.Warn("DIAG: dns-resolve-fail", "host", host, "err", err)
		return nil, fmt.Errorf("resolve %s: %w", host, err)
	}

	if resp.Rcode != dns.RcodeSuccess {
		r.cache.Add(fqdn, dns.TypeA, resp)
		return nil, fmt.Errorf("resolve %s: %s", host, dns.RcodeToString[resp.Rcode])
	}

	// Cache successful response.
	r.cache.Add(fqdn, dns.TypeA, resp)

	ips := extractARecords(resp)
	if len(ips) == 0 {
		return nil, fmt.Errorf("resolve %s: no A records", host)
	}

	slog.Info("DIAG: dns-resolve-ok", "host", host, "ips", ips)
	return ips, nil
}

// extractARecords extracts IPv4 addresses from DNS A records.
func extractARecords(msg *dns.Msg) []string {
	var ips []string
	for _, rr := range msg.Answer {
		if a, ok := rr.(*dns.A); ok {
			ips = append(ips, a.A.String())
		}
	}
	return ips
}
