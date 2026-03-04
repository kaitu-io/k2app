package dns

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/kaitu-io/k2/wire"
	"github.com/miekg/dns"
)

// proxyBufPool pools 4096-byte buffers for DNS UDP response reads.
// Reduces GC pressure under high concurrency, especially on iOS NE (~15MB limit).
var proxyBufPool = sync.Pool{New: func() any { b := make([]byte, 4096); return &b }}

// ProxyDNSClient queries DNS through the wire tunnel using DialUDP.
// Each query opens a fresh UDP session to the DNS server.
type ProxyDNSClient struct {
	dialer  wire.Dialer
	servers []string
	timeout time.Duration
}

// ProxyDNSClientConfig contains configuration for proxy DNS client.
type ProxyDNSClientConfig struct {
	Dialer  wire.Dialer
	Servers []string
}

// NewProxyDNSClient creates a new proxy DNS client.
func NewProxyDNSClient(cfg ProxyDNSClientConfig) *ProxyDNSClient {
	servers := cfg.Servers
	if len(servers) == 0 {
		servers = []string{"8.8.8.8:53", "1.1.1.1:53"}
	}

	return &ProxyDNSClient{
		dialer:  cfg.Dialer,
		servers: servers,
		timeout: 5 * time.Second,
	}
}

// Query sends a DNS query through the tunnel via wire.Dialer.DialUDP.
// Races all configured servers in parallel and returns the first success.
func (c *ProxyDNSClient) Query(query *dns.Msg) (*dns.Msg, error) {
	queryBytes, err := query.Pack()
	if err != nil {
		return nil, fmt.Errorf("pack DNS query: %w", err)
	}

	if len(c.servers) == 1 {
		return c.queryServer(c.servers[0], queryBytes, query)
	}

	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()

	type result struct {
		resp *dns.Msg
		err  error
	}
	ch := make(chan result, len(c.servers))

	for _, server := range c.servers {
		go func(s string) {
			resp, err := c.queryServer(s, queryBytes, query)
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
				return nil, fmt.Errorf("all proxy DNS servers failed: %w", lastErr)
			}
			return nil, fmt.Errorf("all proxy DNS servers timeout")
		}
	}

	return nil, fmt.Errorf("all proxy DNS servers failed: %w", lastErr)
}

func (c *ProxyDNSClient) queryServer(server string, queryBytes []byte, query *dns.Msg) (*dns.Msg, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()

	conn, err := c.dialer.DialUDP(ctx, server)
	if err != nil {
		return nil, fmt.Errorf("dial UDP %s: %w", server, err)
	}
	defer conn.Close()

	if _, err := conn.WriteTo(queryBytes, nil); err != nil {
		return nil, fmt.Errorf("write query: %w", err)
	}

	bp := proxyBufPool.Get().(*[]byte)
	defer proxyBufPool.Put(bp)
	buf := *bp
	conn.SetReadDeadline(time.Now().Add(c.timeout))
	n, _, err := conn.ReadFrom(buf)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	var resp dns.Msg
	if err := resp.Unpack(buf[:n]); err != nil {
		return nil, fmt.Errorf("unpack response: %w", err)
	}

	slog.Debug("proxy DNS query ok", "domain", extractDomain(query), "server", server)
	return &resp, nil
}
