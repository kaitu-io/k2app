//go:build linux

package gateway

import (
	"net"
	"sync"
	"testing"
)

type mockHandler struct {
	mu       sync.Mutex
	tcpDests []string
	udpDests []string
}

func (m *mockHandler) HandleTCP(conn net.Conn, dest string) {
	m.mu.Lock()
	m.tcpDests = append(m.tcpDests, dest)
	m.mu.Unlock()
	conn.Close()
}

func (m *mockHandler) HandleUDP(conn net.PacketConn, dest string) {
	m.mu.Lock()
	m.udpDests = append(m.udpDests, dest)
	m.mu.Unlock()
	conn.Close()
}

func TestGatewayProviderName(t *testing.T) {
	p := &gatewayProvider{done: make(chan struct{})}
	if p.Name() != "gateway" {
		t.Errorf("Name() = %q, want %q", p.Name(), "gateway")
	}
}

func TestGatewayProviderClose_Idempotent(t *testing.T) {
	p := &gatewayProvider{done: make(chan struct{})}
	if err := p.Close(); err != nil {
		t.Errorf("first Close: %v", err)
	}
	if err := p.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
}

func TestTProxyUDPConn_ReadOnce(t *testing.T) {
	// Verify that tproxyUDPConn delivers the initial packet exactly once.
	src := &net.UDPAddr{IP: net.IPv4(192, 168, 1, 100), Port: 54321}
	dst := &net.UDPAddr{IP: net.IPv4(8, 8, 8, 8), Port: 53}
	payload := []byte("hello DNS")

	// We can't use a real UDPConn, but we can test the read path.
	conn := newTProxyUDPConn(nil, src, dst, payload)
	defer conn.Close()

	// First read returns initial packet.
	buf := make([]byte, 1024)
	n, addr, err := conn.ReadFrom(buf)
	if err != nil {
		t.Fatal(err)
	}
	if n != len(payload) {
		t.Errorf("n = %d, want %d", n, len(payload))
	}
	if string(buf[:n]) != "hello DNS" {
		t.Errorf("data = %q", buf[:n])
	}
	if addr.String() != src.String() {
		t.Errorf("addr = %v, want %v", addr, src)
	}

	// Second read blocks until Close.
	go func() {
		conn.Close()
	}()
	_, _, err = conn.ReadFrom(buf)
	if err != net.ErrClosed {
		t.Errorf("second ReadFrom err = %v, want net.ErrClosed", err)
	}
}

func TestTProxyUDPConn_LocalAddr(t *testing.T) {
	dst := &net.UDPAddr{IP: net.IPv4(93, 184, 216, 34), Port: 443}
	conn := newTProxyUDPConn(nil, nil, dst, nil)
	if conn.LocalAddr().String() != dst.String() {
		t.Errorf("LocalAddr() = %v, want %v", conn.LocalAddr(), dst)
	}
}

func TestTProxyUDPConn_InitialDataIsCopied(t *testing.T) {
	payload := []byte("original")
	conn := newTProxyUDPConn(nil, &net.UDPAddr{}, &net.UDPAddr{}, payload)

	// Mutate original — conn should be unaffected.
	payload[0] = 'X'

	buf := make([]byte, 1024)
	n, _, _ := conn.ReadFrom(buf)
	if string(buf[:n]) != "original" {
		t.Errorf("data = %q, want 'original' (initial data should be copied)", buf[:n])
	}
	conn.Close()
}
