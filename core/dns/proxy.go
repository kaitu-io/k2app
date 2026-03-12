package dns

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sasha-s/go-deadlock"

	"github.com/kaitu-io/k2/wire"
	"github.com/miekg/dns"
)

// ProxyDNSClient queries DNS through the wire tunnel using persistent UDP
// connections with message-ID-based multiplexing (sing-box pattern).
// A single UDP connection per server handles all queries, avoiding per-query
// DialUDP overhead (expensive: QUIC stream or smux session creation).
type ProxyDNSClient struct {
	resolver *racingResolver
	dialer   wire.Dialer
	timeout  time.Duration

	// Connection pool: server → persistent connection.
	mu    deadlock.Mutex
	conns map[string]*proxyConn
}

// proxyConn is a persistent UDP connection to a single DNS server.
// Multiple queries share the connection via message ID multiplexing.
type proxyConn struct {
	conn net.PacketConn

	mu        deadlock.RWMutex
	queryID   atomic.Uint32 // monotonic query ID counter
	callbacks map[uint16]*dnsCallback

	done      chan struct{}
	closeOnce sync.Once
	err       error
}

// dnsCallback receives the response for a specific DNS query ID.
type dnsCallback struct {
	message *dns.Msg
	done    chan struct{}
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
		// Defense-in-depth defaults; normally populated by config.SetDefaults().
		// Keep in sync with config.SetDefaults() and direct.go defaults.
		servers = []string{
			"8.8.8.8:53", "8.8.4.4:53",
			"1.1.1.1:53", "1.0.0.1:53",
			"9.9.9.9:53", "149.112.112.112:53",
		}
	}

	c := &ProxyDNSClient{
		dialer:  cfg.Dialer,
		timeout: 5 * time.Second,
		conns:   make(map[string]*proxyConn),
	}
	c.resolver = newRacingResolver(servers, c.queryServer, defaultRaceN, c.timeout, "proxy")
	return c
}

// Query sends a DNS query through the tunnel via persistent connections.
// Races the healthiest servers in parallel and returns the first success.
func (c *ProxyDNSClient) Query(ctx context.Context, query *dns.Msg) (*dns.Msg, error) {
	return c.resolver.Query(ctx, query)
}

// queryServer sends a query to a single server using a persistent connection.
func (c *ProxyDNSClient) queryServer(ctx context.Context, server string, query *dns.Msg) (*dns.Msg, error) {
	pc, err := c.open(ctx, server)
	if err != nil {
		return nil, fmt.Errorf("open connection to %s: %w", server, err)
	}

	// Assign unique query ID and register callback.
	assignedID := uint16(pc.queryID.Add(1))
	cb := &dnsCallback{done: make(chan struct{})}

	pc.mu.Lock()
	pc.callbacks[assignedID] = cb
	pc.mu.Unlock()

	// Cleanup callback on exit.
	defer func() {
		pc.mu.Lock()
		delete(pc.callbacks, assignedID)
		pc.mu.Unlock()
	}()

	// Rewrite query ID for multiplexing.
	outQuery := query.Copy()
	outQuery.Id = assignedID

	queryBytes, err := outQuery.Pack()
	if err != nil {
		return nil, fmt.Errorf("pack DNS query: %w", err)
	}

	if _, err := pc.conn.WriteTo(queryBytes, nil); err != nil {
		// Connection broken — close and remove so next query redials.
		c.closeConn(server, pc)
		return nil, fmt.Errorf("write query to %s: %w", server, err)
	}

	// Wait for response, connection death, or context cancellation.
	select {
	case <-cb.done:
		if cb.message == nil {
			return nil, fmt.Errorf("nil response from %s", server)
		}
		// Restore original query ID.
		cb.message.Id = query.Id
		slog.Debug("proxy DNS query ok", "domain", extractDomain(query), "server", server)
		return cb.message, nil
	case <-pc.done:
		return nil, fmt.Errorf("connection to %s closed: %w", server, pc.err)
	case <-ctx.Done():
		// Fail-fast on genuine timeout only — connection is likely a zombie.
		// Don't close on Canceled (race loser or engine shutdown — Close() handles that).
		if ctx.Err() == context.DeadlineExceeded {
			c.closeConn(server, pc)
		}
		return nil, ctx.Err()
	}
}

// open returns a persistent connection to the server, dialing if needed.
func (c *ProxyDNSClient) open(ctx context.Context, server string) (*proxyConn, error) {
	c.mu.Lock()

	// Check for existing healthy connection.
	if pc, ok := c.conns[server]; ok {
		select {
		case <-pc.done:
			// Dead — remove and redial below.
			delete(c.conns, server)
		default:
			c.mu.Unlock()
			return pc, nil
		}
	}

	c.mu.Unlock()

	// Dial outside the lock to avoid blocking other servers.
	dialCtx, dialCancel := context.WithTimeout(ctx, c.timeout)
	defer dialCancel()

	conn, err := c.dialer.DialUDP(dialCtx, server)
	if err != nil {
		return nil, fmt.Errorf("dial UDP %s: %w", server, err)
	}

	pc := &proxyConn{
		conn:      conn,
		callbacks: make(map[uint16]*dnsCallback),
		done:      make(chan struct{}),
	}

	// concurrency:allow — Close() below is after Unlock (inside select/default branch)
	c.mu.Lock()
	// Double-check: another goroutine may have dialed concurrently.
	if existing, ok := c.conns[server]; ok {
		select {
		case <-existing.done:
			// Still dead — use our new connection.
		default:
			// Healthy connection appeared — discard ours.
			c.mu.Unlock()
			conn.Close()
			return existing, nil
		}
	}
	c.conns[server] = pc
	c.mu.Unlock()

	// Start recv loop.
	go c.recvLoop(server, pc)

	return pc, nil
}

// recvLoop reads DNS responses from the persistent connection and dispatches
// to waiting callbacks via message ID.
func (c *ProxyDNSClient) recvLoop(server string, pc *proxyConn) {
	buf := make([]byte, 4096)
	for {
		n, _, err := pc.conn.ReadFrom(buf)
		if err != nil {
			pc.closeWithErr(err)
			c.removeConn(server, pc)
			return
		}

		var msg dns.Msg
		if err := msg.Unpack(buf[:n]); err != nil {
			continue // malformed response — skip
		}

		pc.mu.RLock()
		cb, ok := pc.callbacks[msg.Id]
		pc.mu.RUnlock()

		if ok {
			select {
			case <-cb.done:
				// Already resolved (timeout raced recv) — skip.
			default:
				cb.message = &msg
				close(cb.done)
			}
		}
	}
}

// closeConn closes a specific connection and removes it from the pool.
func (c *ProxyDNSClient) closeConn(server string, pc *proxyConn) {
	pc.closeWithErr(fmt.Errorf("write error"))
	c.removeConn(server, pc)
}

// removeConn removes a connection from the pool if it matches.
func (c *ProxyDNSClient) removeConn(server string, pc *proxyConn) {
	c.mu.Lock()
	if c.conns[server] == pc {
		delete(c.conns, server)
	}
	c.mu.Unlock()
}

// closeWithErr closes the connection and signals all waiting callbacks.
func (pc *proxyConn) closeWithErr(err error) {
	pc.closeOnce.Do(func() {
		pc.err = err
		pc.conn.Close()
		close(pc.done)
	})
}

// Close closes all persistent connections. Called when the wire dialer is torn down.
func (c *ProxyDNSClient) Close() {
	c.mu.Lock()
	conns := c.conns
	c.conns = make(map[string]*proxyConn)
	c.mu.Unlock()

	for _, pc := range conns {
		pc.closeWithErr(fmt.Errorf("client closed"))
	}
}
