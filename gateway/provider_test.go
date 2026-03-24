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
