package dns

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/miekg/dns"
)

// DirectDNSClient queries DNS via raw UDP socket (bypasses TUN device)
// This prevents DNS query loops where queries get routed back through the TUN
type DirectDNSClient struct {
	servers []string // DNS server addresses (e.g., "114.114.114.114:53")
	timeout time.Duration
}

// NewDirectDNSClient creates a new direct DNS client
func NewDirectDNSClient(servers []string) *DirectDNSClient {
	if len(servers) == 0 {
		// Default to common CN DNS servers
		servers = []string{
			"114.114.114.114:53",
			"223.5.5.5:53",
		}
	}

	return &DirectDNSClient{
		servers: servers,
		timeout: 5 * time.Second,
	}
}

// Query sends a DNS query via direct UDP socket (bypassing TUN)
// Tries all configured servers until one succeeds
func (c *DirectDNSClient) Query(query *dns.Msg) (*dns.Msg, error) {
	client := &dns.Client{
		Net:     "udp",
		Timeout: c.timeout,
	}

	var lastErr error

	// Try each server until one succeeds
	for _, server := range c.servers {
		resp, rtt, err := client.Exchange(query, server)
		if err != nil {
			slog.Warn("direct DNS query failed", "server", server, "err", err)
			lastErr = err
			continue
		}

		if resp.Rcode != dns.RcodeSuccess {
			slog.Warn("direct DNS query returned error code", "server", server, "rcode", dns.RcodeToString[resp.Rcode])
			lastErr = fmt.Errorf("DNS error: %s", dns.RcodeToString[resp.Rcode])
			continue
		}

		slog.Debug("direct DNS query ok", "domain", extractDomain(query), "server", server, "rtt", rtt)

		return resp, nil
	}

	if lastErr != nil {
		return nil, fmt.Errorf("all direct DNS servers failed: %w", lastErr)
	}

	return nil, fmt.Errorf("no direct DNS servers configured")
}

// extractDomain extracts the domain name from a DNS query
func extractDomain(query *dns.Msg) string {
	if len(query.Question) > 0 {
		return query.Question[0].Name
	}
	return "<unknown>"
}
