package dns

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/miekg/dns"
)

// mockDialer for testing ProxyDNSClient.
type mockDialer struct {
	mu       sync.Mutex
	udpCalls []string
	conns    chan net.PacketConn
	dialErr  error
}

func (d *mockDialer) DialTCP(ctx context.Context, addr string, earlyData []byte) (net.Conn, error) {
	return nil, nil
}

func (d *mockDialer) DialUDP(ctx context.Context, addr string) (net.PacketConn, error) {
	d.mu.Lock()
	d.udpCalls = append(d.udpCalls, addr)
	d.mu.Unlock()

	if d.dialErr != nil {
		return nil, d.dialErr
	}
	select {
	case c := <-d.conns:
		return c, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (d *mockDialer) DialStreamUDP(ctx context.Context, addr string) (net.PacketConn, error) {
	return d.DialUDP(ctx, addr)
}

func (d *mockDialer) Reset() {}

func (d *mockDialer) Close() error { return nil }

// mockPacketConn that returns a pre-set DNS response.
type mockPacketConn struct {
	writeCh   chan []byte
	readBuf   chan []byte // channel of response bytes to deliver
	closed    chan struct{}
}

func newMockPacketConn() *mockPacketConn {
	return &mockPacketConn{
		writeCh: make(chan []byte, 10),
		readBuf: make(chan []byte, 10),
		closed:  make(chan struct{}),
	}
}

func (c *mockPacketConn) ReadFrom(b []byte) (int, net.Addr, error) {
	select {
	case data := <-c.readBuf:
		n := copy(b, data)
		return n, nil, nil
	case <-c.closed:
		return 0, nil, net.ErrClosed
	}
}

func (c *mockPacketConn) WriteTo(b []byte, addr net.Addr) (int, error) {
	data := make([]byte, len(b))
	copy(data, b)
	select {
	case c.writeCh <- data:
	default:
	}
	return len(b), nil
}

func (c *mockPacketConn) enqueueResponse(data []byte) {
	c.readBuf <- data
}

func (c *mockPacketConn) Close() error {
	select {
	case <-c.closed:
	default:
		close(c.closed)
	}
	return nil
}

func (c *mockPacketConn) LocalAddr() net.Addr                { return nil }
func (c *mockPacketConn) SetDeadline(time.Time) error        { return nil }
func (c *mockPacketConn) SetReadDeadline(time.Time) error    { return nil }
func (c *mockPacketConn) SetWriteDeadline(time.Time) error   { return nil }

func TestProxyDNSClient_Query(t *testing.T) {
	pc := newMockPacketConn()
	dialer := &mockDialer{conns: make(chan net.PacketConn, 1)}
	dialer.conns <- pc

	client := NewProxyDNSClient(ProxyDNSClientConfig{
		Dialer:  dialer,
		Servers: []string{"8.8.8.8:53"},
	})

	// Build a DNS query
	query := new(dns.Msg)
	query.SetQuestion("example.com.", dns.TypeA)

	// Build a mock DNS response
	resp := new(dns.Msg)
	resp.SetReply(query)
	resp.Answer = append(resp.Answer, &dns.A{
		Hdr: dns.RR_Header{Name: "example.com.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 300},
		A:   net.ParseIP("93.184.216.34"),
	})

	// When the client writes the query, read it and send back response with matching ID.
	go func() {
		queryBytes := <-pc.writeCh
		// Parse the query to get the assigned ID.
		var outQuery dns.Msg
		outQuery.Unpack(queryBytes)
		// Build response with matching ID.
		respCopy := resp.Copy()
		respCopy.Id = outQuery.Id
		respBytes, _ := respCopy.Pack()
		pc.enqueueResponse(respBytes)
	}()

	got, err := client.Query(context.Background(), query)
	if err != nil {
		t.Fatal(err)
	}

	if got.Rcode != dns.RcodeSuccess {
		t.Errorf("Rcode = %d, want 0", got.Rcode)
	}
	if len(got.Answer) != 1 {
		t.Fatalf("answers = %d, want 1", len(got.Answer))
	}

	a, ok := got.Answer[0].(*dns.A)
	if !ok {
		t.Fatal("answer is not A record")
	}
	if a.A.String() != "93.184.216.34" {
		t.Errorf("A = %s, want 93.184.216.34", a.A.String())
	}

	// Verify DialUDP was called (once for persistent connection).
	dialer.mu.Lock()
	defer dialer.mu.Unlock()
	if len(dialer.udpCalls) != 1 || dialer.udpCalls[0] != "8.8.8.8:53" {
		t.Errorf("udpCalls = %v, want [8.8.8.8:53]", dialer.udpCalls)
	}

	// Cleanup.
	client.Close()
}

func TestProxyDNSClient_ConnectionReuse(t *testing.T) {
	pc := newMockPacketConn()
	dialer := &mockDialer{conns: make(chan net.PacketConn, 1)}
	dialer.conns <- pc

	client := NewProxyDNSClient(ProxyDNSClientConfig{
		Dialer:  dialer,
		Servers: []string{"8.8.8.8:53"},
	})

	// Respond to queries in background.
	go func() {
		for {
			select {
			case queryBytes := <-pc.writeCh:
				var outQuery dns.Msg
				outQuery.Unpack(queryBytes)
				resp := new(dns.Msg)
				resp.SetReply(&outQuery)
				resp.Answer = append(resp.Answer, &dns.A{
					Hdr: dns.RR_Header{Name: outQuery.Question[0].Name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 300},
					A:   net.ParseIP("1.2.3.4"),
				})
				respBytes, _ := resp.Pack()
				pc.enqueueResponse(respBytes)
			case <-pc.closed:
				return
			}
		}
	}()

	ctx := context.Background()

	// Multiple queries should reuse the same connection.
	for i := 0; i < 3; i++ {
		query := new(dns.Msg)
		query.SetQuestion("example.com.", dns.TypeA)
		_, err := client.Query(ctx, query)
		if err != nil {
			t.Fatalf("query %d: %v", i, err)
		}
	}

	// Should have only dialed once.
	dialer.mu.Lock()
	calls := len(dialer.udpCalls)
	dialer.mu.Unlock()
	if calls != 1 {
		t.Errorf("expected 1 DialUDP call (connection reuse), got %d", calls)
	}

	client.Close()
}

func TestProxyDNSClient_ResetConnections(t *testing.T) {
	dialer := &mockDialer{conns: make(chan net.PacketConn, 3)}
	pc1 := newMockPacketConn()
	pc2 := newMockPacketConn()
	dialer.conns <- pc1
	dialer.conns <- pc2

	client := NewProxyDNSClient(ProxyDNSClientConfig{
		Dialer:  dialer,
		Servers: []string{"8.8.8.8:53"},
	})

	// Warm the connection pool.
	conn1, err := client.open(context.Background(), "8.8.8.8:53")
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	// Verify reuse.
	conn1b, err := client.open(context.Background(), "8.8.8.8:53")
	if err != nil {
		t.Fatalf("open reuse: %v", err)
	}
	if conn1b != conn1 {
		t.Fatal("expected same connection on reuse")
	}

	dialer.mu.Lock()
	if len(dialer.udpCalls) != 1 {
		t.Fatalf("expected 1 dial, got %d", len(dialer.udpCalls))
	}
	dialer.mu.Unlock()

	// Reset.
	client.ResetConnections()

	// Old connection's done channel must be closed.
	select {
	case <-conn1.done:
	default:
		t.Fatal("old connection's done channel should be closed after reset")
	}

	// Reset must inject errConnPoolReset so queryServer can suppress WARN-level
	// dns-proxy-conn-dead for these by-design closures (log noise reduction).
	if !errors.Is(conn1.err, errConnPoolReset) {
		t.Fatalf("expected pc.err = errConnPoolReset, got %v", conn1.err)
	}

	// Next open must dial fresh.
	conn2, err := client.open(context.Background(), "8.8.8.8:53")
	if err != nil {
		t.Fatalf("open after reset: %v", err)
	}
	if conn2 == conn1 {
		t.Fatal("expected new connection after reset")
	}

	dialer.mu.Lock()
	if len(dialer.udpCalls) != 2 {
		t.Fatalf("expected 2 dials after reset, got %d", len(dialer.udpCalls))
	}
	dialer.mu.Unlock()

	client.Close()
}

func TestProxyDNSClient_ResetConnections_Empty(t *testing.T) {
	// ResetConnections on empty pool must not panic.
	client := NewProxyDNSClient(ProxyDNSClientConfig{
		Dialer:  &mockDialer{conns: make(chan net.PacketConn, 1)},
		Servers: []string{"8.8.8.8:53"},
	})
	client.ResetConnections() // should be a no-op
	client.ResetConnections() // idempotent
}

func TestProxyDNSClient_ContextCancellation(t *testing.T) {
	pc := newMockPacketConn()
	dialer := &mockDialer{conns: make(chan net.PacketConn, 1)}
	dialer.conns <- pc

	client := NewProxyDNSClient(ProxyDNSClientConfig{
		Dialer:  dialer,
		Servers: []string{"8.8.8.8:53"},
	})

	// Don't send any response — let context cancel.
	query := new(dns.Msg)
	query.SetQuestion("slow.example.com.", dns.TypeA)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := client.Query(ctx, query)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error from context cancellation")
	}
	if elapsed > 2*time.Second {
		t.Errorf("query took too long: %v", elapsed)
	}

	client.Close()
}

// --- Transport racing (Happy Eyeballs) tests ---

// autoRespondConn creates a mockPacketConn that automatically responds to DNS
// queries after the given delay. The responder goroutine exits when the conn is closed.
func autoRespondConn(delay time.Duration) *mockPacketConn {
	pc := newMockPacketConn()
	go func() {
		for {
			select {
			case queryBytes := <-pc.writeCh:
				if delay < 0 {
					continue // swallow, never respond
				}
				if delay > 0 {
					select {
					case <-time.After(delay):
					case <-pc.closed:
						return
					}
				}
				var outQuery dns.Msg
				if err := outQuery.Unpack(queryBytes); err != nil {
					continue
				}
				resp := new(dns.Msg)
				resp.SetReply(&outQuery)
				resp.Answer = append(resp.Answer, &dns.A{
					Hdr: dns.RR_Header{Name: outQuery.Question[0].Name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 300},
					A:   net.ParseIP("1.2.3.4"),
				})
				respBytes, _ := resp.Pack()
				pc.enqueueResponse(respBytes)
			case <-pc.closed:
				return
			}
		}
	}()
	return pc
}

// testDialer wraps autoRespondDialer with cleanup tracking.
// Call cleanup() after the test to close all connections (used and unused).
type testDialer struct {
	*mockDialer
	allConns []*mockPacketConn
}

func newTestDialer(delay time.Duration) *testDialer {
	pc := autoRespondConn(delay)
	d := &mockDialer{conns: make(chan net.PacketConn, 1)}
	d.conns <- pc
	return &testDialer{mockDialer: d, allConns: []*mockPacketConn{pc}}
}

func (td *testDialer) cleanup() {
	for _, pc := range td.allConns {
		pc.Close()
	}
}

func newFailingDialer() *testDialer {
	return &testDialer{
		mockDialer: &mockDialer{
			conns:   make(chan net.PacketConn, 1),
			dialErr: net.ErrClosed,
		},
	}
}

func TestQueryWithRace_FastPrimary(t *testing.T) {
	primary := newTestDialer(10 * time.Millisecond)
	fallback := newTestDialer(10 * time.Millisecond)
	defer primary.cleanup()
	defer fallback.cleanup()

	client := NewProxyDNSClient(ProxyDNSClientConfig{
		Dialer:         primary,
		FallbackDialer: fallback,
		Servers:        []string{"8.8.8.8:53"},
	})
	defer client.Close()

	query := new(dns.Msg)
	query.SetQuestion("fast.example.com.", dns.TypeA)

	start := time.Now()
	resp, err := client.Query(context.Background(), query)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil || resp.Rcode != dns.RcodeSuccess {
		t.Fatal("expected success response")
	}
	// Should resolve well within the 300ms stagger — fallback should not start.
	// Use 500ms threshold for CI resilience (goroutine scheduling jitter).
	if elapsed > 500*time.Millisecond {
		t.Errorf("expected fast resolution, took %v", elapsed)
	}

	// Fallback dialer should not have been used (primary won within stagger).
	fallback.mu.Lock()
	fbCalls := len(fallback.udpCalls)
	fallback.mu.Unlock()
	if fbCalls > 0 {
		t.Errorf("fallback dialed %d times, expected 0 (primary should win within stagger)", fbCalls)
	}
}

func TestQueryWithRace_SlowPrimary(t *testing.T) {
	primary := newTestDialer(2 * time.Second)        // slow
	fallback := newTestDialer(10 * time.Millisecond) // fast
	defer primary.cleanup()
	defer fallback.cleanup()

	client := NewProxyDNSClient(ProxyDNSClientConfig{
		Dialer:         primary,
		FallbackDialer: fallback,
		Servers:        []string{"8.8.8.8:53"},
	})
	defer client.Close()

	query := new(dns.Msg)
	query.SetQuestion("slow.example.com.", dns.TypeA)

	start := time.Now()
	resp, err := client.Query(context.Background(), query)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil || resp.Rcode != dns.RcodeSuccess {
		t.Fatal("expected success response")
	}
	// Fallback should win: stagger (300ms) + fallback dial + response (~50ms).
	// Total should be well under 1s, definitely under 2s (primary's delay).
	if elapsed > 1*time.Second {
		t.Errorf("expected fallback to win quickly, took %v", elapsed)
	}
}

func TestQueryWithRace_PrimaryFail_FallbackWins(t *testing.T) {
	primary := newFailingDialer()
	fallback := newTestDialer(10 * time.Millisecond)
	defer fallback.cleanup()

	client := NewProxyDNSClient(ProxyDNSClientConfig{
		Dialer:         primary,
		FallbackDialer: fallback,
		Servers:        []string{"8.8.8.8:53"},
	})
	defer client.Close()

	query := new(dns.Msg)
	query.SetQuestion("fail.example.com.", dns.TypeA)

	resp, err := client.Query(context.Background(), query)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil || resp.Rcode != dns.RcodeSuccess {
		t.Fatal("expected success from fallback")
	}

	// primaryConsecFail should have incremented.
	if client.primaryConsecFail.Load() < 1 {
		t.Error("expected primaryConsecFail >= 1")
	}
}

func TestQueryWithRace_StaggerBypass(t *testing.T) {
	primary := newTestDialer(2 * time.Second)
	fallback := newTestDialer(10 * time.Millisecond)
	defer primary.cleanup()
	defer fallback.cleanup()

	client := NewProxyDNSClient(ProxyDNSClientConfig{
		Dialer:         primary,
		FallbackDialer: fallback,
		Servers:        []string{"8.8.8.8:53"},
	})
	defer client.Close()

	// Simulate primary degradation.
	client.primaryConsecFail.Store(consecFailThreshold)

	query := new(dns.Msg)
	query.SetQuestion("bypass.example.com.", dns.TypeA)

	start := time.Now()
	resp, err := client.Query(context.Background(), query)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected response")
	}
	// Without stagger (bypassed), fallback starts immediately.
	// Use 400ms threshold for CI resilience (goroutine scheduling jitter).
	if elapsed > 400*time.Millisecond {
		t.Errorf("expected immediate fallback (stagger bypassed), took %v", elapsed)
	}
}

func TestQueryWithRace_BothFail(t *testing.T) {
	primary := newFailingDialer()
	fallback := newFailingDialer()

	client := NewProxyDNSClient(ProxyDNSClientConfig{
		Dialer:         primary,
		FallbackDialer: fallback,
		Servers:        []string{"8.8.8.8:53"},
	})
	defer client.Close()

	query := new(dns.Msg)
	query.SetQuestion("both-fail.example.com.", dns.TypeA)

	_, err := client.Query(context.Background(), query)
	if err == nil {
		t.Fatal("expected error when both transports fail")
	}
}

func TestQueryWithRace_ResetClearsConsecFail(t *testing.T) {
	client := NewProxyDNSClient(ProxyDNSClientConfig{
		Dialer:         &mockDialer{conns: make(chan net.PacketConn, 1)},
		FallbackDialer: &mockDialer{conns: make(chan net.PacketConn, 1)},
		Servers:        []string{"8.8.8.8:53"},
	})

	client.primaryConsecFail.Store(5)
	client.ResetConnections()

	if client.primaryConsecFail.Load() != 0 {
		t.Errorf("expected primaryConsecFail=0 after reset, got %d", client.primaryConsecFail.Load())
	}
}

func TestQuery_NoFallback_BackwardCompat(t *testing.T) {
	dialer := newTestDialer(10 * time.Millisecond)
	defer dialer.cleanup()

	// No FallbackDialer — single transport mode.
	client := NewProxyDNSClient(ProxyDNSClientConfig{
		Dialer:  dialer,
		Servers: []string{"8.8.8.8:53"},
	})
	defer client.Close()

	if client.fallback != nil {
		t.Fatal("expected nil fallback when FallbackDialer not set")
	}

	query := new(dns.Msg)
	query.SetQuestion("compat.example.com.", dns.TypeA)

	resp, err := client.Query(context.Background(), query)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil || resp.Rcode != dns.RcodeSuccess {
		t.Fatal("expected success response")
	}
}

func TestQueryWithRace_ContextCancelled(t *testing.T) {
	// Both transports are slow. Parent context cancelled during the race.
	// Covers the ctx.Done() branch in queryWithRace's result-collection loop.
	primary := newTestDialer(-1)  // never responds
	fallback := newTestDialer(-1) // never responds
	defer primary.cleanup()
	defer fallback.cleanup()

	client := NewProxyDNSClient(ProxyDNSClientConfig{
		Dialer:         primary,
		FallbackDialer: fallback,
		Servers:        []string{"8.8.8.8:53"},
	})
	defer client.Close()

	query := new(dns.Msg)
	query.SetQuestion("cancelled.example.com.", dns.TypeA)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := client.Query(ctx, query)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error from context cancellation")
	}
	// Should fail within ~200ms (context timeout), not 5s (DNS timeout).
	if elapsed > 1*time.Second {
		t.Errorf("expected fast cancellation, took %v", elapsed)
	}
}

func TestQueryWithRace_PrimaryRecoveryCycle(t *testing.T) {
	// Full cycle: primary degraded → primary success → counter reset → stagger restored.
	// Primary is faster than fallback to ensure primary wins the race.
	primary := newTestDialer(10 * time.Millisecond)
	fallback := newTestDialer(2 * time.Second)
	defer primary.cleanup()
	defer fallback.cleanup()

	client := NewProxyDNSClient(ProxyDNSClientConfig{
		Dialer:         primary,
		FallbackDialer: fallback,
		Servers:        []string{"8.8.8.8:53"},
	})
	defer client.Close()

	// Step 1: Simulate primary degradation.
	client.primaryConsecFail.Store(consecFailThreshold)
	if client.primaryConsecFail.Load() < consecFailThreshold {
		t.Fatal("setup: expected primaryConsecFail >= threshold")
	}

	// Step 2: Query succeeds (primary is fast, wins the race).
	query := new(dns.Msg)
	query.SetQuestion("recover.example.com.", dns.TypeA)

	resp, err := client.Query(context.Background(), query)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected response")
	}

	// Step 3: Verify recovery — counter should be back to 0.
	if client.primaryConsecFail.Load() != 0 {
		t.Errorf("expected primaryConsecFail=0 after primary success, got %d",
			client.primaryConsecFail.Load())
	}
}
