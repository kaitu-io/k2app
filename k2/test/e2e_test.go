package test

import (
	"context"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/kaitu-io/k2/core"
	"github.com/kaitu-io/k2/rule"
	"github.com/kaitu-io/k2/wire"
)

// TestE2E_QUIC_TCPProxy verifies the full TCP proxy path:
// mockProvider -> ClientTunnel -> QUICClient -> QUICServer -> ProxyHandler -> echo server
func TestE2E_QUIC_TCPProxy(t *testing.T) {
	env := setupQUICEnv(t)
	defer env.Close()

	echoAddr := startTCPEchoServer(t)

	// Create client tunnel with mock provider.
	prov := &mockProvider{}
	tunnel := core.NewClientTunnel(core.ClientTunnelConfig{
		Provider:     prov,
		Outbounds:    []core.Outbound{core.NewDirectOutbound(nil), nil, core.NewWireOutbound("quic", env.quicClient, nil)},
		DirectTarget: rule.Target(0),
		Router:       core.NewRouter(rule.NewEngine(nil, rule.Target(2), nil, true)),
	})
	if err := tunnel.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer tunnel.Close()

	// Inject a TCP connection through the mock provider.
	clientConn, appConn := net.Pipe()
	defer clientConn.Close()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		prov.InjectTCP(appConn, echoAddr)
	}()

	// Write data through the tunnel.
	testData := []byte("hello through QUIC tunnel")
	if _, err := clientConn.Write(testData); err != nil {
		t.Fatal(err)
	}

	// Read echoed data.
	buf := make([]byte, len(testData))
	clientConn.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := io.ReadFull(clientConn, buf); err != nil {
		t.Fatal(err)
	}
	if string(buf) != string(testData) {
		t.Fatalf("expected %q, got %q", testData, buf)
	}

	clientConn.Close()
	wg.Wait()
}

// TestE2E_QUIC_UDPProxy verifies the full UDP proxy path:
// mockProvider -> ClientTunnel -> QUICClient -> QUICServer -> ProxyHandler -> echo server
func TestE2E_QUIC_UDPProxy(t *testing.T) {
	env := setupQUICEnv(t)
	defer env.Close()

	echoAddr := startUDPEchoServer(t)

	// Create client tunnel with mock provider.
	prov := &mockProvider{}
	tunnel := core.NewClientTunnel(core.ClientTunnelConfig{
		Provider:     prov,
		Outbounds:    []core.Outbound{core.NewDirectOutbound(nil), nil, core.NewWireOutbound("quic", env.quicClient, nil)},
		DirectTarget: rule.Target(0),
		Router:       core.NewRouter(rule.NewEngine(nil, rule.Target(2), nil, true)),
	})
	if err := tunnel.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer tunnel.Close()

	// Create a channeled PacketConn pair.
	appPC := newChanPacketConn()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		prov.InjectUDP(appPC, echoAddr)
	}()

	// Send data.
	appPC.sendCh <- []byte("hello UDP")

	// Read echo.
	select {
	case data := <-appPC.recvCh:
		if string(data) != "hello UDP" {
			t.Fatalf("expected 'hello UDP', got %q", data)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for UDP echo")
	}

	appPC.Close()
	wg.Wait()
}

// TestE2E_TCPWS_TCPProxy verifies TCP proxy over TCP-WS (smux) transport.
func TestE2E_TCPWS_TCPProxy(t *testing.T) {
	env := setupTCPWSEnv(t)
	defer env.Close()

	echoAddr := startTCPEchoServer(t)

	// Create client tunnel with mock provider.
	prov := &mockProvider{}
	tunnel := core.NewClientTunnel(core.ClientTunnelConfig{
		Provider:     prov,
		Outbounds:    []core.Outbound{core.NewDirectOutbound(nil), nil, core.NewWireOutbound("tcpws", env.tcpwsClient, nil)},
		DirectTarget: rule.Target(0),
		Router:       core.NewRouter(rule.NewEngine(nil, rule.Target(2), nil, true)),
	})
	if err := tunnel.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer tunnel.Close()

	clientConn, appConn := net.Pipe()
	defer clientConn.Close()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		prov.InjectTCP(appConn, echoAddr)
	}()

	testData := []byte("hello through smux tunnel")
	if _, err := clientConn.Write(testData); err != nil {
		t.Fatal(err)
	}

	buf := make([]byte, len(testData))
	clientConn.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := io.ReadFull(clientConn, buf); err != nil {
		t.Fatal(err)
	}
	if string(buf) != string(testData) {
		t.Fatalf("expected %q, got %q", testData, buf)
	}

	clientConn.Close()
	wg.Wait()
}

// TestE2E_TransportManager_Failover verifies QUIC primary with TCP-WS fallback.
func TestE2E_TransportManager_Failover(t *testing.T) {
	// Set up TCP-WS environment (QUIC not available).
	env := setupTCPWSEnv(t)
	defer env.Close()

	echoAddr := startTCPEchoServer(t)

	// TransportManager with only TCP-WS (simulating QUIC failure).
	tm := wire.NewTransportManager()
	tm.SetTCPWS(env.tcpwsClient)
	// No QUIC set → falls back to TCP-WS

	prov := &mockProvider{}
	tunnel := core.NewClientTunnel(core.ClientTunnelConfig{
		Provider:     prov,
		Outbounds:    []core.Outbound{core.NewDirectOutbound(nil), nil, core.NewWireOutbound("tm", tm, nil)},
		DirectTarget: rule.Target(0),
		Router:       core.NewRouter(rule.NewEngine(nil, rule.Target(2), nil, true)),
	})
	if err := tunnel.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer tm.Close()
	defer tunnel.Close()

	clientConn, appConn := net.Pipe()
	defer clientConn.Close()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		prov.InjectTCP(appConn, echoAddr)
	}()

	testData := []byte("fallback to tcpws")
	if _, err := clientConn.Write(testData); err != nil {
		t.Fatal(err)
	}

	buf := make([]byte, len(testData))
	clientConn.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := io.ReadFull(clientConn, buf); err != nil {
		t.Fatal(err)
	}
	if string(buf) != string(testData) {
		t.Fatalf("expected %q, got %q", testData, buf)
	}

	clientConn.Close()
	wg.Wait()
}

// TestE2E_ClientServerConnect verifies basic handshake and tunnel creation.
func TestE2E_ClientServerConnect(t *testing.T) {
	env := setupQUICEnv(t)
	defer env.Close()

	// Use a local echo server — no external network dependency.
	echoAddr := startTCPEchoServer(t)

	conn, err := env.quicClient.DialTCP(context.Background(), echoAddr, nil)
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()
}

// chanPacketConn is a channel-backed PacketConn for E2E testing.
type chanPacketConn struct {
	sendCh chan []byte
	recvCh chan []byte
	closed chan struct{}
}

func newChanPacketConn() *chanPacketConn {
	return &chanPacketConn{
		sendCh: make(chan []byte, 16),
		recvCh: make(chan []byte, 16),
		closed: make(chan struct{}),
	}
}

func (c *chanPacketConn) ReadFrom(b []byte) (int, net.Addr, error) {
	select {
	case data := <-c.sendCh:
		n := copy(b, data)
		return n, nil, nil
	case <-c.closed:
		return 0, nil, net.ErrClosed
	}
}

func (c *chanPacketConn) WriteTo(b []byte, _ net.Addr) (int, error) {
	data := make([]byte, len(b))
	copy(data, b)
	select {
	case c.recvCh <- data:
		return len(b), nil
	case <-c.closed:
		return 0, net.ErrClosed
	}
}

func (c *chanPacketConn) Close() error {
	select {
	case <-c.closed:
	default:
		close(c.closed)
	}
	return nil
}

func (c *chanPacketConn) LocalAddr() net.Addr                { return nil }
func (c *chanPacketConn) SetDeadline(time.Time) error        { return nil }
func (c *chanPacketConn) SetReadDeadline(time.Time) error    { return nil }
func (c *chanPacketConn) SetWriteDeadline(time.Time) error   { return nil }
