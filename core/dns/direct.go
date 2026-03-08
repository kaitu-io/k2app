package dns

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"time"

	"github.com/miekg/dns"
)

// DirectDNSClient queries DNS via raw UDP socket (bypasses TUN device)
// This prevents DNS query loops where queries get routed back through the TUN
type DirectDNSClient struct {
	servers []string // DNS server addresses (e.g., "114.114.114.114:53")
	timeout time.Duration
	dialer  *net.Dialer // optional: protected dialer for mobile socket protection
}

// DirectDNSOption configures a DirectDNSClient.
type DirectDNSOption func(*DirectDNSClient)

// WithDialer sets a custom net.Dialer for DNS queries.
// Used on mobile to route DNS through a protected socket (VpnService.protect).
func WithDialer(d *net.Dialer) DirectDNSOption {
	return func(c *DirectDNSClient) { c.dialer = d }
}

// NewDirectDNSClient creates a new direct DNS client
func NewDirectDNSClient(servers []string, opts ...DirectDNSOption) *DirectDNSClient {
	if len(servers) == 0 {
		// Default to common CN DNS servers
		servers = []string{
			"114.114.114.114:53",
			"223.5.5.5:53",
		}
	}

	c := &DirectDNSClient{
		servers: servers,
		timeout: 5 * time.Second,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Query sends a DNS query via direct UDP socket (bypassing TUN).
// Races all configured servers in parallel and returns the first success.
// Any valid DNS response (including NXDOMAIN) is accepted — only
// transport-level errors (timeout, connection refused) are failures.
func (c *DirectDNSClient) Query(ctx context.Context, query *dns.Msg) (*dns.Msg, error) {
	if len(c.servers) == 1 {
		return c.queryServer(ctx, query, c.servers[0])
	}

	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	type result struct {
		resp *dns.Msg
		err  error
	}
	ch := make(chan result, len(c.servers))

	for _, server := range c.servers {
		go func(s string) {
			resp, err := c.queryServer(ctx, query, s)
			ch <- result{resp, err}
		}(server)
	}

	var lastErr error
	for range c.servers {
		select {
		case r := <-ch:
			if r.err == nil {
				return r.resp, nil
			}
			lastErr = r.err
		case <-ctx.Done():
			if lastErr != nil {
				return nil, fmt.Errorf("all direct DNS servers failed: %w", lastErr)
			}
			return nil, fmt.Errorf("all direct DNS servers timeout")
		}
	}

	return nil, fmt.Errorf("all direct DNS servers failed: %w", lastErr)
}

// queryServer queries a single DNS server via UDP.
// Retries over TCP if the response has the TC (truncated) flag set.
func (c *DirectDNSClient) queryServer(ctx context.Context, query *dns.Msg, server string) (*dns.Msg, error) {
	// Derive timeout from parent context, capped at c.timeout.
	timeout := c.timeout
	if deadline, ok := ctx.Deadline(); ok {
		if remaining := time.Until(deadline); remaining < timeout {
			timeout = remaining
		}
	}

	client := &dns.Client{
		Net:     "udp",
		Timeout: timeout,
		Dialer:  c.dialer,
	}

	resp, rtt, err := client.ExchangeContext(ctx, query, server)
	if err != nil {
		slog.Warn("direct DNS query failed", "server", server, "err", err)
		return nil, err
	}

	// RFC 1035 §4.2.1: TC flag means response was truncated.
	// Retry over TCP to get the full response.
	if resp.Truncated {
		tcpClient := &dns.Client{
			Net:     "tcp",
			Timeout: timeout,
			Dialer:  c.dialer,
		}
		resp, rtt, err = tcpClient.ExchangeContext(ctx, query, server)
		if err != nil {
			slog.Warn("direct DNS TCP retry failed", "server", server, "err", err)
			return nil, err
		}
	}

	slog.Debug("direct DNS query ok", "domain", extractDomain(query), "server", server, "rtt", rtt, "rcode", dns.RcodeToString[resp.Rcode])
	return resp, nil
}

// extractDomain extracts the domain name from a DNS query
func extractDomain(query *dns.Msg) string {
	if len(query.Question) > 0 {
		return query.Question[0].Name
	}
	return "<unknown>"
}
