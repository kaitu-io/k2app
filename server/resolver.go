package server

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"time"

	k2dns "github.com/kaitu-io/k2/core/dns"
	"github.com/miekg/dns"
)

// Resolver resolves domain names using the shared DNS cache.
// Cache hit: unpack A records from cached response (no network I/O).
// Cache miss: query upstream via raw UDP, cache response, return IPs.
// Shared cache means handleDNSUDP and Resolver benefit from each other's queries.
type Resolver struct {
	cache   *k2dns.DNSCache
	servers []string
	timeout time.Duration
}

// NewResolver creates a Resolver that shares the given DNS cache.
func NewResolver(cache *k2dns.DNSCache, servers []string) *Resolver {
	if len(servers) == 0 {
		servers = []string{"8.8.8.8:53", "1.1.1.1:53"}
	}
	return &Resolver{
		cache:   cache,
		servers: servers,
		timeout: 3 * time.Second,
	}
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

	// Cache lookup: unpack A records from cached packed bytes.
	if msg, ok := r.cache.Get(fqdn, dns.TypeA); ok {
		ips := extractARecords(msg)
		if len(ips) > 0 {
			slog.Info("DIAG: dns-resolve-cache-hit", "host", host, "ips", len(ips))
			return ips, nil
		}
	}

	// Cache miss: query upstream.
	query := new(dns.Msg)
	query.SetQuestion(fqdn, dns.TypeA)
	query.RecursionDesired = true

	resp, server, err := r.queryUpstream(ctx, query)
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

	slog.Info("DIAG: dns-resolve-ok", "host", host, "ips", ips, "server", server)
	return ips, nil
}

// queryUpstream races configured DNS servers, returns first successful response.
func (r *Resolver) queryUpstream(ctx context.Context, query *dns.Msg) (*dns.Msg, string, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	type result struct {
		resp   *dns.Msg
		server string
		err    error
	}

	// Race up to 2 servers.
	n := min(len(r.servers), 2)
	ch := make(chan result, n)

	for i := range n {
		go func(srv string) {
			c := new(dns.Client)
			c.Net = "udp"
			resp, _, err := c.ExchangeContext(ctx, query, srv)
			ch <- result{resp, srv, err}
		}(r.servers[i])
	}

	var lastErr error
	for range n {
		select {
		case res := <-ch:
			if res.err == nil {
				return res.resp, res.server, nil
			}
			lastErr = res.err
		case <-ctx.Done():
			if lastErr != nil {
				return nil, "", lastErr
			}
			return nil, "", ctx.Err()
		}
	}
	return nil, "", fmt.Errorf("all DNS servers failed: %w", lastErr)
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
