package dns

import (
	"context"
	"errors"
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

// errConnPoolReset is the sentinel error injected into in-flight queries when
// ResetConnections() runs on a network change / wake event. Distinguishing it
// from real connection death keeps WARN-level DIAG events focused on actual
// failures rather than expected by-design closures. See dns-proxy-conn-dead.
var errConnPoolReset = errors.New("dns connection pool reset (by-design)")

// ProxyDNSClient queries DNS through the wire tunnel using persistent UDP
// connections with message-ID-based multiplexing (sing-box pattern).
// A single UDP connection per server handles all queries, avoiding per-query
// DialUDP overhead (expensive: QUIC stream or smux session creation).
//
// When FallbackDialer is configured, Query() races two transports with stagger
// (Happy Eyeballs for DNS transports): primary starts immediately, fallback
// starts after stagger. First success wins. This eliminates TCP HOL blocking
// on the TCP-WS path by preferring QUIC stream (no HOL) when available.
type ProxyDNSClient struct {
	resolver *racingResolver
	dialer   wire.Dialer
	timeout  time.Duration

	// Connection pool: server → persistent connection.
	mu    deadlock.Mutex
	conns map[string]*proxyConn

	// Transport racing: primary (QUIC preferred) vs fallback (TCP-WS direct).
	// fallback is nil when FallbackDialer not configured (single transport mode).
	fallback          *ProxyDNSClient
	stagger           time.Duration
	primaryConsecFail atomic.Int32
}

// proxyConn is a persistent UDP connection to a single DNS server.
// Multiple queries share the connection via message ID multiplexing.
type proxyConn struct {
	conn      net.PacketConn
	createdAt time.Time // for DIAG: connection age tracking

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
	// Dialer is the primary transport for DNS queries.
	// Typically TransportManager (QUIC stream preferred, TCP-WS fallback within TM).
	Dialer wire.Dialer

	// FallbackDialer is the fallback transport, raced against Dialer with stagger.
	// Typically the TCP-WS dialer directly.
	// nil = single transport mode (backward compatible, no racing).
	FallbackDialer wire.Dialer

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
		stagger: 300 * time.Millisecond,
	}
	c.resolver = newRacingResolver(servers, c.queryServer, defaultRaceN, c.timeout, "proxy")

	if cfg.FallbackDialer != nil {
		c.fallback = NewProxyDNSClient(ProxyDNSClientConfig{
			Dialer:  cfg.FallbackDialer,
			Servers: servers,
			// FallbackDialer deliberately nil — prevents recursive nesting.
		})
	}

	return c
}

// Query sends a DNS query through the tunnel via persistent connections.
// When fallback is configured, races primary and fallback transports with stagger.
// Otherwise races the healthiest servers on a single transport.
func (c *ProxyDNSClient) Query(ctx context.Context, query *dns.Msg) (*dns.Msg, error) {
	if c.fallback != nil {
		return c.queryWithRace(ctx, query)
	}
	return c.resolver.Query(ctx, query)
}

// consecFailThreshold is the number of consecutive primary failures before
// the stagger is bypassed and both transports race immediately.
const consecFailThreshold = 3

// queryWithRace races primary and fallback transports with stagger.
// Primary (QUIC preferred) starts immediately. Fallback (TCP-WS direct) starts
// after stagger delay, or immediately if primary has consecutive failures.
// First success cancels the other via shared context.
func (c *ProxyDNSClient) queryWithRace(ctx context.Context, query *dns.Msg) (*dns.Msg, error) {
	type raceResult struct {
		resp      *dns.Msg
		err       error
		transport string
	}

	raceCtx, raceCancel := context.WithCancel(ctx)
	defer raceCancel()
	ch := make(chan raceResult, 2)

	// Primary (TransportManager: QUIC stream preferred) starts immediately.
	go func() {
		resp, err := c.resolver.Query(raceCtx, query)
		ch <- raceResult{resp, err, "primary"}
	}()

	// Fallback (TCP-WS direct) starts after stagger.
	stagger := c.stagger
	staggerBypassed := false
	if c.primaryConsecFail.Load() >= consecFailThreshold {
		stagger = 0 // primary broken, race immediately
		staggerBypassed = true
	}
	if staggerBypassed {
		slog.Debug("dns transport race: stagger bypassed",
			"domain", extractDomain(query),
			"primaryConsecFail", c.primaryConsecFail.Load(),
		)
	}
	go func() {
		if stagger > 0 {
			timer := time.NewTimer(stagger)
			defer timer.Stop()
			select {
			case <-timer.C:
			case <-raceCtx.Done():
				ch <- raceResult{nil, raceCtx.Err(), "fallback"}
				return
			}
		}
		resp, err := c.fallback.resolver.Query(raceCtx, query)
		ch <- raceResult{resp, err, "fallback"}
	}()

	// First success wins.
	var lastErr error
	for i := 0; i < 2; i++ {
		select {
		case res := <-ch:
			if res.err == nil {
				raceCancel()
				if res.transport == "primary" {
					c.primaryConsecFail.Store(0)
				}
				slog.Debug("dns transport race: winner",
					"winner", res.transport,
					"domain", extractDomain(query),
					"staggerBypassed", staggerBypassed,
				)
				return res.resp, nil
			}
			if res.transport == "primary" {
				c.primaryConsecFail.Add(1)
			}
			lastErr = res.err
		case <-ctx.Done():
			if lastErr != nil {
				return nil, lastErr
			}
			return nil, ctx.Err()
		}
	}
	return nil, fmt.Errorf("all DNS transports failed: %w", lastErr)
}

// queryServer sends a query to a single server using a persistent connection.
func (c *ProxyDNSClient) queryServer(ctx context.Context, server string, query *dns.Msg) (*dns.Msg, error) {
	isNewConn := false
	pc, err := c.open(ctx, server)
	if err != nil {
		// Race-loser cancellations (Happy Eyeballs winner cancels siblings)
		// are by-design and must not pollute WARN-level metrics. Genuine dial
		// failures (DNS timeout, TCP refused, TLS error) do not carry Canceled.
		if errors.Is(err, context.Canceled) {
			slog.Debug("dns-proxy-dial-canceled",
				"server", server,
				"domain", extractDomain(query),
			)
		} else {
			slog.Warn("DIAG: dns-proxy-dial-fail",
				"server", server,
				"domain", extractDomain(query),
				"err", err,
			)
		}
		return nil, fmt.Errorf("open connection to %s: %w", server, err)
	}
	connAgeS := int(time.Since(pc.createdAt).Seconds())
	// A connection created within this call's lifetime is "new".
	if connAgeS == 0 {
		isNewConn = true
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
		slog.Warn("DIAG: dns-proxy-write-fail",
			"server", server,
			"domain", extractDomain(query),
			"connAgeS", connAgeS,
			"isNewConn", isNewConn,
			"err", err,
		)
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
		// Pool-reset on wake/network-change is by-design — don't pollute WARN.
		// Genuine conn death (recvLoop read error, write failure) keeps WARN.
		if errors.Is(pc.err, errConnPoolReset) {
			slog.Debug("dns-proxy-conn-reset",
				"server", server,
				"domain", extractDomain(query),
				"connAgeS", connAgeS,
				"isNewConn", isNewConn,
			)
		} else {
			slog.Warn("DIAG: dns-proxy-conn-dead",
				"server", server,
				"domain", extractDomain(query),
				"connAgeS", connAgeS,
				"isNewConn", isNewConn,
				"err", pc.err,
			)
		}
		return nil, fmt.Errorf("connection to %s closed: %w", server, pc.err)
	case <-ctx.Done():
		// Fail-fast on genuine timeout only — connection is likely a zombie.
		// Don't close on Canceled (race loser or engine shutdown — Close() handles that).
		if ctx.Err() == context.DeadlineExceeded {
			slog.Warn("DIAG: dns-proxy-timeout",
				"server", server,
				"domain", extractDomain(query),
				"connAgeS", connAgeS,
				"isNewConn", isNewConn,
			)
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

	conn, err := c.dialer.DialStreamUDP(dialCtx, server)
	if err != nil {
		return nil, fmt.Errorf("dial UDP %s: %w", server, err)
	}

	pc := &proxyConn{
		conn:      conn,
		createdAt: time.Now(),
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
			slog.Debug("DIAG: dns-proxy-recvloop-exit", "server", server, "err", err)
			pc.closeWithErr(err)
			c.removeConn(server, pc)
			return
		}

		var msg dns.Msg
		if err := msg.Unpack(buf[:n]); err != nil {
			slog.Warn("DIAG: dns-proxy-recv-malformed", "server", server, "len", n)
			continue // malformed response — skip
		}

		pc.mu.RLock()
		cb, ok := pc.callbacks[msg.Id]
		numCallbacks := len(pc.callbacks)
		pc.mu.RUnlock()

		if ok {
			select {
			case <-cb.done:
				slog.Debug("DIAG: dns-proxy-recv-late", "server", server, "queryID", msg.Id, "domain", extractDomain(&msg))
			default:
				cb.message = &msg
				// Log before close(cb.done): queryServer writes cb.message.Id after
				// receiving from cb.done, which races with reading msg.Id here
				// (cb.message points to the same msg struct).
				slog.Debug("DIAG: dns-proxy-recv-ok", "server", server, "queryID", msg.Id, "domain", extractDomain(&msg))
				close(cb.done)
			}
		} else {
			slog.Warn("DIAG: dns-proxy-recv-no-callback", "server", server, "queryID", msg.Id, "domain", extractDomain(&msg), "numCallbacks", numCallbacks)
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

// ResetConnections closes all persistent connections so the next query
// dials fresh ones through the current wire transport. Safe to call
// concurrently — in-flight queries receive an error and retry with
// a new connection on the next attempt.
func (c *ProxyDNSClient) ResetConnections() {
	c.mu.Lock()
	conns := c.conns
	c.conns = make(map[string]*proxyConn)
	c.mu.Unlock()

	for _, pc := range conns {
		pc.closeWithErr(errConnPoolReset)
	}

	// Reset health tracking — fresh transport after reconnect.
	c.primaryConsecFail.Store(0)

	if c.fallback != nil {
		c.fallback.ResetConnections()
	}
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

	if c.fallback != nil {
		c.fallback.Close()
	}
}
