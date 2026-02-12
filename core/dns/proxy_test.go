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

func (d *mockDialer) DialTCP(ctx context.Context, addr string) (net.Conn, error) {
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

func (d *mockDialer) Close() error { return nil }

// mockPacketConn that returns a pre-set DNS response.
type mockPacketConn struct {
	writeCh   chan []byte
	readResp  []byte
	readOnce  sync.Once
	readReady chan struct{}
	closed    chan struct{}
}

func newMockPacketConn() *mockPacketConn {
	return &mockPacketConn{
		writeCh:   make(chan []byte, 1),
		readReady: make(chan struct{}),
		closed:    make(chan struct{}),
	}
}

func (c *mockPacketConn) ReadFrom(b []byte) (int, net.Addr, error) {
	select {
	case <-c.readReady:
		n := copy(b, c.readResp)
		return n, nil, nil
	case <-c.closed:
		return 0, nil, net.ErrClosed
	}
}

func (c *mockPacketConn) WriteTo(b []byte, addr net.Addr) (int, error) {
	data := make([]byte, len(b))
	copy(data, b)
	c.writeCh <- data
	return len(b), nil
}

func (c *mockPacketConn) setResponse(data []byte) {
	c.readResp = data
	c.readOnce.Do(func() { close(c.readReady) })
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
	respBytes, _ := resp.Pack()

	// When the client writes the query, set the response
	go func() {
		<-pc.writeCh // Wait for query to be written
		pc.setResponse(respBytes)
	}()

	got, err := client.Query(query)
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

	// Verify DialUDP was called
	dialer.mu.Lock()
	defer dialer.mu.Unlock()
	if len(dialer.udpCalls) != 1 || dialer.udpCalls[0] != "8.8.8.8:53" {
		t.Errorf("udpCalls = %v, want [8.8.8.8:53]", dialer.udpCalls)
	}
}

func TestProxyDNSClient_Defaults(t *testing.T) {
	client := NewProxyDNSClient(ProxyDNSClientConfig{})

	if client.dialer != nil {
		t.Error("expected nil dialer when not provided")
	}
	if len(client.servers) != 2 {
		t.Errorf("servers = %d, want 2", len(client.servers))
	}
	if client.servers[0] != "8.8.8.8:53" {
		t.Errorf("server[0] = %q, want 8.8.8.8:53", client.servers[0])
	}
}
