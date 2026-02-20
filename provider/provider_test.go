package provider

import (
	"context"
	"net"
	"testing"
)

func TestProvider_InterfaceCompliance(t *testing.T) {
	var _ Provider = (*ProxyProvider)(nil)
	var _ Provider = (*TUNProvider)(nil)
}

func TestProviderConfig_SetDefaults(t *testing.T) {
	var cfg ProviderConfig
	cfg.SetDefaults()

	if cfg.Mode != "tun" {
		t.Errorf("Mode = %q, want tun", cfg.Mode)
	}
	if cfg.MTU != DefaultMTU {
		t.Errorf("MTU = %d, want %d", cfg.MTU, DefaultMTU)
	}
	if cfg.ProxyListen != "127.0.0.1:1080" {
		t.Errorf("ProxyListen = %q, want 127.0.0.1:1080", cfg.ProxyListen)
	}
}

func TestProviderConfig_PreservesValues(t *testing.T) {
	cfg := ProviderConfig{
		Mode:        "proxy",
		MTU:         1500,
		ProxyListen: "0.0.0.0:8080",
	}
	cfg.SetDefaults()

	if cfg.Mode != "proxy" {
		t.Errorf("Mode = %q, want proxy", cfg.Mode)
	}
	if cfg.MTU != 1500 {
		t.Errorf("MTU = %d, want 1500", cfg.MTU)
	}
	if cfg.ProxyListen != "0.0.0.0:8080" {
		t.Errorf("ProxyListen = %q, want 0.0.0.0:8080", cfg.ProxyListen)
	}
}

func TestProviderConfig_TunAddresses_Preserved(t *testing.T) {
	cfg := ProviderConfig{
		IPv4Address: "10.0.0.2/24",
		IPv6Address: "fd00::2/128",
	}
	cfg.SetDefaults()

	if cfg.IPv4Address != "10.0.0.2/24" {
		t.Errorf("IPv4Address = %q, want 10.0.0.2/24", cfg.IPv4Address)
	}
	if cfg.IPv6Address != "fd00::2/128" {
		t.Errorf("IPv6Address = %q, want fd00::2/128", cfg.IPv6Address)
	}
}

func TestProviderConfig_TunAddresses_Empty(t *testing.T) {
	cfg := ProviderConfig{}
	cfg.SetDefaults()

	// Empty TUN addresses stay empty — provider uses hardcoded defaults
	if cfg.IPv4Address != "" {
		t.Errorf("IPv4Address should be empty, got %q", cfg.IPv4Address)
	}
	if cfg.IPv6Address != "" {
		t.Errorf("IPv6Address should be empty, got %q", cfg.IPv6Address)
	}
}

// testHandler records calls from providers.
type testHandler struct {
	tcpCalls chan tcpCall
	udpCalls chan udpCall
}

type tcpCall struct {
	conn net.Conn
	dest string
}

type udpCall struct {
	conn net.PacketConn
	dest string
}

func newTestHandler() *testHandler {
	return &testHandler{
		tcpCalls: make(chan tcpCall, 16),
		udpCalls: make(chan udpCall, 16),
	}
}

func (h *testHandler) HandleTCP(conn net.Conn, dest string) {
	h.tcpCalls <- tcpCall{conn: conn, dest: dest}
}

func (h *testHandler) HandleUDP(conn net.PacketConn, dest string) {
	h.udpCalls <- udpCall{conn: conn, dest: dest}
}

func TestProxyProvider_Name(t *testing.T) {
	p := NewProxyProvider(ProviderConfig{})
	if p.Name() != "proxy" {
		t.Errorf("Name = %q, want proxy", p.Name())
	}
}

func TestProxyProvider_StartClose(t *testing.T) {
	p := NewProxyProvider(ProviderConfig{ProxyListen: "127.0.0.1:0"})
	handler := newTestHandler()

	if err := p.Start(context.Background(), handler); err != nil {
		t.Fatal(err)
	}
	defer p.Close()

	if p.listener == nil {
		t.Fatal("listener is nil after Start")
	}

	addr := p.listener.Addr().String()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()
}

func TestProxyProvider_SOCKS5Connect(t *testing.T) {
	p := NewProxyProvider(ProviderConfig{ProxyListen: "127.0.0.1:0"})
	handler := newTestHandler()

	if err := p.Start(context.Background(), handler); err != nil {
		t.Fatal(err)
	}
	defer p.Close()

	addr := p.listener.Addr().String()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	// SOCKS5 handshake: version 5, 1 method (no auth)
	conn.Write([]byte{0x05, 0x01, 0x00})

	// Read auth reply
	reply := make([]byte, 2)
	if _, err := conn.Read(reply); err != nil {
		t.Fatal(err)
	}
	if reply[0] != 0x05 || reply[1] != 0x00 {
		t.Fatalf("auth reply = %v, want [5 0]", reply)
	}

	// CONNECT to example.com:80
	// VER=5 CMD=CONNECT RSV=0 ATYP=DOMAIN
	conn.Write([]byte{
		0x05, 0x01, 0x00, 0x03,
		11, 'e', 'x', 'a', 'm', 'p', 'l', 'e', '.', 'c', 'o', 'm',
		0x00, 0x50, // port 80
	})

	// Read connect reply
	connectReply := make([]byte, 10)
	if _, err := conn.Read(connectReply); err != nil {
		t.Fatal(err)
	}
	if connectReply[1] != 0x00 {
		t.Fatalf("connect reply status = %d, want 0 (success)", connectReply[1])
	}

	// Verify handler was called
	select {
	case call := <-handler.tcpCalls:
		if call.dest != "example.com:80" {
			t.Errorf("dest = %q, want example.com:80", call.dest)
		}
	default:
		t.Fatal("handler.HandleTCP not called")
	}
}

func TestProxyProvider_SOCKS5Connect_IPv4(t *testing.T) {
	p := NewProxyProvider(ProviderConfig{ProxyListen: "127.0.0.1:0"})
	handler := newTestHandler()

	if err := p.Start(context.Background(), handler); err != nil {
		t.Fatal(err)
	}
	defer p.Close()

	addr := p.listener.Addr().String()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	// Auth
	conn.Write([]byte{0x05, 0x01, 0x00})
	reply := make([]byte, 2)
	conn.Read(reply)

	// CONNECT to 93.184.216.34:443 (ATYP=IPv4)
	conn.Write([]byte{
		0x05, 0x01, 0x00, 0x01,
		93, 184, 216, 34,
		0x01, 0xBB, // port 443
	})

	connectReply := make([]byte, 10)
	conn.Read(connectReply)

	select {
	case call := <-handler.tcpCalls:
		if call.dest != "93.184.216.34:443" {
			t.Errorf("dest = %q, want 93.184.216.34:443", call.dest)
		}
	default:
		t.Fatal("handler.HandleTCP not called")
	}
}

func TestProxyProvider_HTTPConnect(t *testing.T) {
	p := NewProxyProvider(ProviderConfig{ProxyListen: "127.0.0.1:0"})
	handler := newTestHandler()

	if err := p.Start(context.Background(), handler); err != nil {
		t.Fatal(err)
	}
	defer p.Close()

	addr := p.listener.Addr().String()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	// HTTP CONNECT
	conn.Write([]byte("CONNECT example.com:443 HTTP/1.1\r\nHost: example.com:443\r\n\r\n"))

	// Read response
	buf := make([]byte, 256)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	resp := string(buf[:n])
	if resp != "HTTP/1.1 200 Connection Established\r\n\r\n" {
		t.Errorf("response = %q", resp)
	}

	select {
	case call := <-handler.tcpCalls:
		if call.dest != "example.com:443" {
			t.Errorf("dest = %q, want example.com:443", call.dest)
		}
	default:
		t.Fatal("handler.HandleTCP not called")
	}
}

func TestProxyProvider_HTTPConnect_NoPort(t *testing.T) {
	p := NewProxyProvider(ProviderConfig{ProxyListen: "127.0.0.1:0"})
	handler := newTestHandler()

	if err := p.Start(context.Background(), handler); err != nil {
		t.Fatal(err)
	}
	defer p.Close()

	addr := p.listener.Addr().String()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	// HTTP CONNECT without port
	conn.Write([]byte("CONNECT example.com HTTP/1.1\r\nHost: example.com\r\n\r\n"))

	buf := make([]byte, 256)
	conn.Read(buf)

	select {
	case call := <-handler.tcpCalls:
		if call.dest != "example.com:443" {
			t.Errorf("dest = %q, want example.com:443", call.dest)
		}
	default:
		t.Fatal("handler.HandleTCP not called")
	}
}
