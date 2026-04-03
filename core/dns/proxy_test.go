package dns

import (
	"context"
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
