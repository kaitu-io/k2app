package core

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/kaitu-io/k2/provider"
	"github.com/kaitu-io/k2/sniff"
)

// mockDialer records DialTCP/DialUDP calls and returns mock connections.
type mockDialer struct {
	mu       sync.Mutex
	tcpCalls []string
	udpCalls []string

	tcpConns chan net.Conn
	udpConns chan net.PacketConn
	dialErr  error
}

func newMockDialer() *mockDialer {
	return &mockDialer{
		tcpConns: make(chan net.Conn, 8),
		udpConns: make(chan net.PacketConn, 8),
	}
}

func (d *mockDialer) DialTCP(ctx context.Context, addr string) (net.Conn, error) {
	d.mu.Lock()
	d.tcpCalls = append(d.tcpCalls, addr)
	d.mu.Unlock()

	if d.dialErr != nil {
		return nil, d.dialErr
	}

	select {
	case conn := <-d.tcpConns:
		return conn, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (d *mockDialer) DialUDP(ctx context.Context, addr string) (net.PacketConn, error) {
	d.mu.Lock()
	d.udpCalls = append(d.udpCalls, addr)
	d.mu.Unlock()

	if d.dialErr != nil {
		return nil, d.dialErr
	}

	select {
	case conn := <-d.udpConns:
		return conn, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (d *mockDialer) Close() error { return nil }

// mockProvider calls handler methods directly.
type mockProvider struct {
	handler provider.ConnectionHandler
	started bool
}

func (p *mockProvider) Start(ctx context.Context, handler provider.ConnectionHandler) error {
	p.handler = handler
	p.started = true
	return nil
}

func (p *mockProvider) Close() error { return nil }
func (p *mockProvider) Name() string { return "mock" }

func TestClientTunnel_HandleTCP_Proxy(t *testing.T) {
	dialer := newMockDialer()
	prov := &mockProvider{}

	tunnel := NewClientTunnel(ClientTunnelConfig{
		Provider: prov,
		Dialer:   dialer,
		// Router nil → defaults to proxy
	})

	if err := tunnel.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	// Provide a mock remote connection
	clientConn, remoteConn := net.Pipe()
	dialer.tcpConns <- remoteConn

	// Create a local connection from the "provider"
	localConn, providerConn := net.Pipe()

	// Inject a TCP connection
	done := make(chan struct{})
	go func() {
		prov.handler.HandleTCP(localConn, "example.com:80")
		close(done)
	}()

	// Write from provider side → should arrive at remote
	providerConn.Write([]byte("hello"))

	buf := make([]byte, 100)
	n, err := clientConn.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	if string(buf[:n]) != "hello" {
		t.Errorf("got %q, want hello", string(buf[:n]))
	}

	// Write from remote → should arrive at provider
	clientConn.Write([]byte("world"))
	n, err = providerConn.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	if string(buf[:n]) != "world" {
		t.Errorf("got %q, want world", string(buf[:n]))
	}

	// Close connections to unblock pipe
	providerConn.Close()
	clientConn.Close()
	<-done

	// Verify DialTCP was called with correct addr
	dialer.mu.Lock()
	defer dialer.mu.Unlock()
	if len(dialer.tcpCalls) != 1 || dialer.tcpCalls[0] != "example.com:80" {
		t.Errorf("tcpCalls = %v, want [example.com:80]", dialer.tcpCalls)
	}

	tunnel.Close()
}

func TestClientTunnel_HandleTCP_Direct(t *testing.T) {
	// Start a local echo server to verify direct connections
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		io.Copy(conn, conn) // echo
	}()

	prov := &mockProvider{}
	// Create tunnel with nil dialer (shouldn't be used for direct)
	// Use a router that always returns direct
	tunnel := NewClientTunnel(ClientTunnelConfig{
		Provider: prov,
		Dialer:   newMockDialer(),
		Router:   &Router{}, // No cache → falls through to k2rule.Match which returns direct for localhost
	})

	if err := tunnel.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	// Create a pipe for the "provider" side
	localConn, providerConn := net.Pipe()

	done := make(chan struct{})
	go func() {
		prov.handler.HandleTCP(localConn, ln.Addr().String())
		close(done)
	}()

	// Give the goroutine time to dial
	time.Sleep(50 * time.Millisecond)

	providerConn.Write([]byte("direct-test"))
	buf := make([]byte, 100)
	n, err := providerConn.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	if string(buf[:n]) != "direct-test" {
		t.Errorf("got %q, want direct-test", string(buf[:n]))
	}

	providerConn.Close()
	<-done
	tunnel.Close()
}

func TestClientTunnel_HandleUDP_Proxy(t *testing.T) {
	dialer := newMockDialer()
	prov := &mockProvider{}

	tunnel := NewClientTunnel(ClientTunnelConfig{
		Provider: prov,
		Dialer:   dialer,
	})

	if err := tunnel.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	// Create mock remote PacketConn
	remotePC := newMockPacketConn()
	dialer.udpConns <- remotePC

	// Create a mock local PacketConn from the "provider"
	localPC := newMockPacketConn()

	done := make(chan struct{})
	go func() {
		prov.handler.HandleUDP(localPC, "8.8.8.8:53")
		close(done)
	}()

	// Send a packet from local → should arrive at remote
	localPC.injectRead([]byte("dns-query"), nil)

	pkt := remotePC.waitWrite(t, time.Second)
	if !bytes.Equal(pkt, []byte("dns-query")) {
		t.Errorf("remote got %q, want dns-query", pkt)
	}

	// Send response from remote → should arrive at local
	remotePC.injectRead([]byte("dns-response"), nil)
	pkt = localPC.waitWrite(t, time.Second)
	if !bytes.Equal(pkt, []byte("dns-response")) {
		t.Errorf("local got %q, want dns-response", pkt)
	}

	// Close to unblock
	localPC.closeConn()
	remotePC.closeConn()
	<-done

	dialer.mu.Lock()
	defer dialer.mu.Unlock()
	if len(dialer.udpCalls) != 1 || dialer.udpCalls[0] != "8.8.8.8:53" {
		t.Errorf("udpCalls = %v, want [8.8.8.8:53]", dialer.udpCalls)
	}

	tunnel.Close()
}

func TestClientTunnel_Start_SetsHandler(t *testing.T) {
	prov := &mockProvider{}
	tunnel := NewClientTunnel(ClientTunnelConfig{
		Provider: prov,
		Dialer:   newMockDialer(),
	})

	if err := tunnel.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	if !prov.started {
		t.Error("provider not started")
	}
	if prov.handler == nil {
		t.Error("handler not set")
	}

	tunnel.Close()
}

// mockPacketConn for testing UDP piping.
type mockPacketConn struct {
	readCh  chan packetData
	writeCh chan packetData
	done    chan struct{}
	once    sync.Once
}

type packetData struct {
	data []byte
	addr net.Addr
}

func newMockPacketConn() *mockPacketConn {
	return &mockPacketConn{
		readCh:  make(chan packetData, 16),
		writeCh: make(chan packetData, 16),
		done:    make(chan struct{}),
	}
}

func (c *mockPacketConn) ReadFrom(b []byte) (int, net.Addr, error) {
	select {
	case pkt := <-c.readCh:
		n := copy(b, pkt.data)
		return n, pkt.addr, nil
	case <-c.done:
		return 0, nil, errors.New("closed")
	}
}

func (c *mockPacketConn) WriteTo(b []byte, addr net.Addr) (int, error) {
	data := make([]byte, len(b))
	copy(data, b)
	select {
	case c.writeCh <- packetData{data: data, addr: addr}:
		return len(b), nil
	case <-c.done:
		return 0, errors.New("closed")
	}
}

func (c *mockPacketConn) injectRead(data []byte, addr net.Addr) {
	c.readCh <- packetData{data: data, addr: addr}
}

func (c *mockPacketConn) waitWrite(t *testing.T, timeout time.Duration) []byte {
	t.Helper()
	select {
	case pkt := <-c.writeCh:
		return pkt.data
	case <-time.After(timeout):
		t.Fatal("timeout waiting for write")
		return nil
	}
}

func (c *mockPacketConn) closeConn() {
	c.once.Do(func() { close(c.done) })
}

func (c *mockPacketConn) Close() error {
	c.closeConn()
	return nil
}

func (c *mockPacketConn) LocalAddr() net.Addr                { return nil }
func (c *mockPacketConn) SetDeadline(time.Time) error        { return nil }
func (c *mockPacketConn) SetReadDeadline(time.Time) error    { return nil }
func (c *mockPacketConn) SetWriteDeadline(time.Time) error   { return nil }

// --- Close unblocking tests ---

// TestClientTunnel_Close_UnblocksStuckTCP verifies that Close() forcefully
// interrupts handler goroutines stuck in pipe() by closing tracked connections.
// This reproduces the TUN mode disconnect hang (I5).
func TestClientTunnel_Close_UnblocksStuckTCP(t *testing.T) {
	dialer := newMockDialer()
	prov := &mockProvider{}

	tunnel := NewClientTunnel(ClientTunnelConfig{
		Provider: prov,
		Dialer:   dialer,
	})

	if err := tunnel.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	// net.Pipe() connections block Read() until explicitly closed —
	// simulates gVisor connections that don't error on stack.Close().
	localConn, _ := net.Pipe()
	remoteConn, _ := net.Pipe()
	dialer.tcpConns <- remoteConn

	handlerDone := make(chan struct{})
	go func() {
		prov.handler.HandleTCP(localConn, "stuck.example.com:80")
		close(handlerDone)
	}()

	// Let handler enter pipe()
	time.Sleep(50 * time.Millisecond)

	// Close MUST complete within timeout (not hang forever).
	closeDone := make(chan struct{})
	go func() {
		tunnel.Close()
		close(closeDone)
	}()

	select {
	case <-closeDone:
	case <-time.After(10 * time.Second):
		t.Fatal("tunnel.Close() blocked — connTracker fix not working")
	}

	select {
	case <-handlerDone:
	case <-time.After(time.Second):
		t.Fatal("handler goroutine still running after Close")
	}
}

// TestClientTunnel_Close_UnblocksStuckUDP verifies Close() unblocks
// handler goroutines stuck in pipePacket().
func TestClientTunnel_Close_UnblocksStuckUDP(t *testing.T) {
	dialer := newMockDialer()
	prov := &mockProvider{}

	tunnel := NewClientTunnel(ClientTunnelConfig{
		Provider: prov,
		Dialer:   dialer,
	})

	if err := tunnel.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	// mockPacketConn with no data injected — ReadFrom blocks until Close.
	localPC := newMockPacketConn()
	remotePC := newMockPacketConn()
	dialer.udpConns <- remotePC

	handlerDone := make(chan struct{})
	go func() {
		prov.handler.HandleUDP(localPC, "8.8.8.8:53")
		close(handlerDone)
	}()

	time.Sleep(50 * time.Millisecond)

	closeDone := make(chan struct{})
	go func() {
		tunnel.Close()
		close(closeDone)
	}()

	select {
	case <-closeDone:
	case <-time.After(10 * time.Second):
		t.Fatal("tunnel.Close() blocked for UDP — connTracker fix not working")
	}

	select {
	case <-handlerDone:
	case <-time.After(time.Second):
		t.Fatal("UDP handler goroutine still running after Close")
	}
}

// TestConnTracker_ConcurrentOps verifies connTracker is safe under
// concurrent track/untrack/closeAll.
func TestConnTracker_ConcurrentOps(t *testing.T) {
	ct := newConnTracker()
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			a, b := net.Pipe()
			defer b.Close()
			untrack := ct.track(a)
			time.Sleep(time.Millisecond)
			untrack()
			a.Close()
		}()
	}

	// Concurrent closeAll
	go ct.closeAll()

	wg.Wait()
}

func TestClientTunnel_HandleTCP_BTForcedDirect(t *testing.T) {
	// Start a local server to accept the direct connection
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	serverGot := make(chan string, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		buf := make([]byte, 256)
		n, _ := conn.Read(buf)
		serverGot <- string(buf[:n])
	}()

	dialer := newMockDialer()
	prov := &mockProvider{}

	tunnel := NewClientTunnel(ClientTunnelConfig{
		Provider: prov,
		Dialer:   dialer,
		SniffBT:  true,
		// Router nil → defaults to proxy → sniff will check for BT
	})

	if err := tunnel.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	// Build a BT handshake (20 bytes header + 48 bytes rest)
	btHandshake, _ := hex.DecodeString(
		"13426974546f7272656e742070726f746f636f6c" +
			"0000000000100000e21ea9569b69bab33c97851d0298bdfa89bc90922d5554313631302dea812fcd6a3563e3be40c1d1")

	// Create connection that sends BT handshake
	localConn, providerConn := net.Pipe()

	done := make(chan struct{})
	go func() {
		// Use ln.Addr() so direct dial succeeds to our local server
		prov.handler.HandleTCP(localConn, ln.Addr().String())
		close(done)
	}()

	// Send BT handshake from provider side
	providerConn.Write(btHandshake)

	// The server should receive the full BT handshake (sniff must prepend peeked bytes)
	select {
	case got := <-serverGot:
		if len(got) < 20 {
			t.Fatalf("server got only %d bytes, expected >= 20", len(got))
		}
		if got[:20] != "\x13BitTorrent protocol" {
			t.Errorf("server didn't receive BT handshake, got %q", got[:20])
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for server to receive data")
	}

	providerConn.Close()
	<-done

	// Verify DialTCP was NOT called (should have gone direct, not proxy)
	dialer.mu.Lock()
	defer dialer.mu.Unlock()
	if len(dialer.tcpCalls) != 0 {
		t.Errorf("DialTCP called %d times, expected 0 (BT should force direct)", len(dialer.tcpCalls))
	}

	tunnel.Close()
}

func TestClientTunnel_HandleTCP_NonBTStillProxied(t *testing.T) {
	dialer := newMockDialer()
	prov := &mockProvider{}

	tunnel := NewClientTunnel(ClientTunnelConfig{
		Provider: prov,
		Dialer:   dialer,
		SniffBT:  true,
		// Router nil → defaults to proxy
	})

	if err := tunnel.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	remoteConn, clientConn := net.Pipe()
	dialer.tcpConns <- remoteConn

	localConn, providerConn := net.Pipe()

	done := make(chan struct{})
	go func() {
		prov.handler.HandleTCP(localConn, "example.com:443")
		close(done)
	}()

	// Send TLS ClientHello (not BT) — starts with 0x16
	tlsHello := []byte{0x16, 0x03, 0x01, 0x00, 0x05, 0x01, 0x00, 0x00, 0x01, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
	providerConn.Write(tlsHello)

	// Should be proxied — read from remote side
	buf := make([]byte, 256)
	n, err := clientConn.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	// Verify TLS hello arrived intact (sniffed bytes prepended correctly)
	if buf[0] != 0x16 {
		t.Errorf("first byte = 0x%02x, want 0x16 (TLS)", buf[0])
	}
	if n < 20 {
		t.Errorf("got %d bytes, want >= 20", n)
	}

	providerConn.Close()
	clientConn.Close()
	<-done

	// Verify DialTCP WAS called (non-BT goes to proxy)
	dialer.mu.Lock()
	defer dialer.mu.Unlock()
	if len(dialer.tcpCalls) != 1 {
		t.Errorf("DialTCP called %d times, expected 1", len(dialer.tcpCalls))
	}

	tunnel.Close()
}

func TestClientTunnel_HandleTCP_BTNotSniffedInGlobalMode(t *testing.T) {
	dialer := newMockDialer()
	prov := &mockProvider{}

	tunnel := NewClientTunnel(ClientTunnelConfig{
		Provider: prov,
		Dialer:   dialer,
		SniffBT:  false, // global mode — no sniffing
	})

	if err := tunnel.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	remoteConn, clientConn := net.Pipe()
	dialer.tcpConns <- remoteConn

	localConn, providerConn := net.Pipe()

	// Build a BT handshake
	btHandshake, _ := hex.DecodeString(
		"13426974546f7272656e742070726f746f636f6c" +
			"0000000000100000e21ea9569b69bab33c97851d0298bdfa89bc90922d5554313631302dea812fcd6a3563e3be40c1d1")

	done := make(chan struct{})
	go func() {
		prov.handler.HandleTCP(localConn, "example.com:443")
		close(done)
	}()

	providerConn.Write(btHandshake)

	// In global mode, BT should still be proxied — read from remote side
	buf := make([]byte, 256)
	n, err := clientConn.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	if n < 20 {
		t.Fatalf("got %d bytes, want >= 20", n)
	}
	// Verify BT handshake arrived on proxy side (not forced to direct)
	if buf[0] != 0x13 {
		t.Errorf("first byte = 0x%02x, want 0x13 (BT)", buf[0])
	}

	providerConn.Close()
	clientConn.Close()
	<-done

	// Verify DialTCP WAS called (global mode → proxy, no sniff override)
	dialer.mu.Lock()
	defer dialer.mu.Unlock()
	if len(dialer.tcpCalls) != 1 {
		t.Errorf("DialTCP called %d times, expected 1 (global mode should proxy BT)", len(dialer.tcpCalls))
	}

	tunnel.Close()
}

func TestClientTunnel_HandleUDP_UTPForcedDirect(t *testing.T) {
	// Start a real UDP listener as the direct target
	directListener, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer directListener.Close()

	serverGot := make(chan []byte, 1)
	go func() {
		buf := make([]byte, 4096)
		n, _, err := directListener.ReadFrom(buf)
		if err != nil {
			return
		}
		pkt := make([]byte, n)
		copy(pkt, buf[:n])
		serverGot <- pkt
	}()

	dialer := newMockDialer()
	prov := &mockProvider{}

	tunnel := NewClientTunnel(ClientTunnelConfig{
		Provider: prov,
		Dialer:   dialer,
		SniffBT:  true,
		// Router nil → defaults to proxy → sniff will check for uTP
	})

	if err := tunnel.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	// uTP packet fixture (from sniff/bittorrent_test.go)
	utpData, _ := hex.DecodeString("21001ecb6817f2805d044fd700100000dbd03029")

	localPC := newMockPacketConn()

	done := make(chan struct{})
	go func() {
		prov.handler.HandleUDP(localPC, directListener.LocalAddr().String())
		close(done)
	}()

	// Inject uTP packet as first read
	localPC.injectRead(utpData, nil)

	// The direct listener should receive the uTP packet
	select {
	case got := <-serverGot:
		if !bytes.Equal(got, utpData) {
			t.Errorf("direct listener got %x, want %x", got, utpData)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for direct listener to receive uTP packet")
	}

	localPC.closeConn()
	<-done

	// Verify DialUDP was NOT called (uTP should force direct)
	dialer.mu.Lock()
	defer dialer.mu.Unlock()
	if len(dialer.udpCalls) != 0 {
		t.Errorf("DialUDP called %d times, expected 0 (uTP should force direct)", len(dialer.udpCalls))
	}

	tunnel.Close()
}

func TestClientTunnel_HandleUDP_NonBTStillProxied(t *testing.T) {
	dialer := newMockDialer()
	prov := &mockProvider{}

	tunnel := NewClientTunnel(ClientTunnelConfig{
		Provider: prov,
		Dialer:   dialer,
		SniffBT:  true,
		// Router nil → defaults to proxy
	})

	if err := tunnel.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	remotePC := newMockPacketConn()
	dialer.udpConns <- remotePC

	localPC := newMockPacketConn()

	done := make(chan struct{})
	go func() {
		prov.handler.HandleUDP(localPC, "8.8.8.8:53")
		close(done)
	}()

	// Inject non-BT UDP packet (plain DNS-like bytes)
	plainUDP := []byte{0x00, 0x01, 0x01, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x03, 0x77, 0x77, 0x77, 0x06, 0x67, 0x6f, 0x6f,
		0x67, 0x6c, 0x65, 0x03, 0x63, 0x6f, 0x6d, 0x00, 0x00, 0x01, 0x00, 0x01}
	localPC.injectRead(plainUDP, nil)

	// Should be proxied — packet arrives at mock remote
	pkt := remotePC.waitWrite(t, 3*time.Second)
	if !bytes.Equal(pkt, plainUDP) {
		t.Errorf("remote got %x, want %x", pkt, plainUDP)
	}

	localPC.closeConn()
	remotePC.closeConn()
	<-done

	// Verify DialUDP WAS called (non-BT goes to proxy)
	dialer.mu.Lock()
	defer dialer.mu.Unlock()
	if len(dialer.udpCalls) != 1 {
		t.Errorf("DialUDP called %d times, expected 1", len(dialer.udpCalls))
	}

	tunnel.Close()
}

func TestClientTunnel_HandleUDP_STUNForcedDirect(t *testing.T) {
	// Start a real UDP listener as the direct target
	directListener, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer directListener.Close()

	serverGot := make(chan []byte, 1)
	go func() {
		buf := make([]byte, 4096)
		n, _, err := directListener.ReadFrom(buf)
		if err != nil {
			return
		}
		pkt := make([]byte, n)
		copy(pkt, buf[:n])
		serverGot <- pkt
	}()

	dialer := newMockDialer()
	prov := &mockProvider{}

	tunnel := NewClientTunnel(ClientTunnelConfig{
		Provider: prov,
		Dialer:   dialer,
		SniffBT:  true,
		// Router nil → defaults to proxy → sniff will check for STUN
	})

	if err := tunnel.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	// STUN Binding Request: type=0x0001, length=0, magic=0x2112A442, txn_id=12 bytes
	stunPacket := make([]byte, 20)
	stunPacket[0], stunPacket[1] = 0x00, 0x01 // Binding Request
	stunPacket[2], stunPacket[3] = 0x00, 0x00 // Length = 0
	stunPacket[4], stunPacket[5], stunPacket[6], stunPacket[7] = 0x21, 0x12, 0xA4, 0x42 // Magic cookie

	localPC := newMockPacketConn()

	done := make(chan struct{})
	go func() {
		prov.handler.HandleUDP(localPC, directListener.LocalAddr().String())
		close(done)
	}()

	localPC.injectRead(stunPacket, nil)

	select {
	case got := <-serverGot:
		if !bytes.Equal(got, stunPacket) {
			t.Errorf("direct listener got %x, want %x", got, stunPacket)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for direct listener to receive STUN packet")
	}

	localPC.closeConn()
	<-done

	dialer.mu.Lock()
	defer dialer.mu.Unlock()
	if len(dialer.udpCalls) != 0 {
		t.Errorf("DialUDP called %d times, expected 0 (STUN should force direct)", len(dialer.udpCalls))
	}

	tunnel.Close()
}

func TestClientTunnel_HandleUDP_DTLSForcedDirect(t *testing.T) {
	directListener, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer directListener.Close()

	serverGot := make(chan []byte, 1)
	go func() {
		buf := make([]byte, 4096)
		n, _, err := directListener.ReadFrom(buf)
		if err != nil {
			return
		}
		pkt := make([]byte, n)
		copy(pkt, buf[:n])
		serverGot <- pkt
	}()

	dialer := newMockDialer()
	prov := &mockProvider{}

	tunnel := NewClientTunnel(ClientTunnelConfig{
		Provider: prov,
		Dialer:   dialer,
		SniffBT:  true,
	})

	if err := tunnel.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	// DTLS ClientHello: content_type=22(handshake), version=0xFEFD(DTLS 1.2), + padding
	dtlsPacket := make([]byte, 13)
	dtlsPacket[0] = 22   // handshake
	dtlsPacket[1] = 0xFE // version major
	dtlsPacket[2] = 0xFD // version minor (DTLS 1.2)

	localPC := newMockPacketConn()

	done := make(chan struct{})
	go func() {
		prov.handler.HandleUDP(localPC, directListener.LocalAddr().String())
		close(done)
	}()

	localPC.injectRead(dtlsPacket, nil)

	select {
	case got := <-serverGot:
		if !bytes.Equal(got, dtlsPacket) {
			t.Errorf("direct listener got %x, want %x", got, dtlsPacket)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for direct listener to receive DTLS packet")
	}

	localPC.closeConn()
	<-done

	dialer.mu.Lock()
	defer dialer.mu.Unlock()
	if len(dialer.udpCalls) != 0 {
		t.Errorf("DialUDP called %d times, expected 0 (DTLS should force direct)", len(dialer.udpCalls))
	}

	tunnel.Close()
}

func TestClientTunnel_HandleTCP_HTTPSniffReplay(t *testing.T) {
	dialer := newMockDialer()
	prov := &mockProvider{}

	tunnel := NewClientTunnel(ClientTunnelConfig{
		Provider: prov,
		Dialer:   dialer,
		SniffSNI: true,
		// Router nil → proxy, HTTP sniff won't upgrade but peeked data must replay
	})

	if err := tunnel.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	remoteConn, clientConn := net.Pipe()
	dialer.tcpConns <- remoteConn

	localConn, providerConn := net.Pipe()

	done := make(chan struct{})
	go func() {
		prov.handler.HandleTCP(localConn, "93.184.216.34:80")
		close(done)
	}()

	httpReq := []byte("GET / HTTP/1.1\r\nHost: example.com\r\nConnection: close\r\n\r\n")
	providerConn.Write(httpReq)

	buf := make([]byte, 256)
	n, err := clientConn.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(buf[:n], []byte("GET / HTTP/1.1")) {
		t.Errorf("proxy didn't receive HTTP request, got %q", string(buf[:n]))
	}

	providerConn.Close()
	clientConn.Close()
	<-done
	tunnel.Close()
}

func TestClientTunnel_HandleUDP_QUICBlocked(t *testing.T) {
	dialer := newMockDialer()
	prov := &mockProvider{}

	tunnel := NewClientTunnel(ClientTunnelConfig{
		Provider:  prov,
		Dialer:    dialer,
		BlockQUIC: true,
		// Router nil → defaults to proxy
	})

	if err := tunnel.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	// Build a minimal QUIC Initial long header (version 1)
	quicPacket := make([]byte, 32)
	quicPacket[0] = 0xC3 // long header + fixed bit + Initial type
	binary.BigEndian.PutUint32(quicPacket[1:5], 0x00000001) // QUIC v1

	localPC := newMockPacketConn()
	done := make(chan struct{})
	go func() {
		prov.handler.HandleUDP(localPC, "142.250.197.99:443")
		close(done)
	}()

	localPC.injectRead(quicPacket, nil)

	select {
	case <-done:
		// HandleUDP returned — QUIC was blocked
	case <-time.After(3 * time.Second):
		t.Fatal("HandleUDP did not return — QUIC block not working")
	}

	dialer.mu.Lock()
	defer dialer.mu.Unlock()
	if len(dialer.udpCalls) != 0 {
		t.Errorf("DialUDP called %d times, want 0 (QUIC should be blocked)", len(dialer.udpCalls))
	}

	tunnel.Close()
}
// TestSniffQUICClient_IsBrowser_Integration verifies the sniff package's
// IsBrowser classification works correctly end-to-end.
func TestSniffQUICClient_IsBrowser_Integration(t *testing.T) {
	// Verify browser clients are identified correctly
	if !sniff.QUICClientChromium.IsBrowser() {
		t.Error("Chromium should be browser")
	}
	if !sniff.QUICClientFirefox.IsBrowser() {
		t.Error("Firefox should be browser")
	}
	if !sniff.QUICClientSafari.IsBrowser() {
		t.Error("Safari should be browser")
	}
	if sniff.QUICClientQUICGo.IsBrowser() {
		t.Error("quic-go should NOT be browser")
	}
	if sniff.QUICClientUnknown.IsBrowser() {
		t.Error("unknown should NOT be browser")
	}
}

func TestClientTunnel_HandleUDP_QUICPassthrough(t *testing.T) {
	dialer := newMockDialer()
	prov := &mockProvider{}

	tunnel := NewClientTunnel(ClientTunnelConfig{
		Provider:  prov,
		Dialer:    dialer,
		BlockQUIC: false, // Allow QUIC through
		// Router nil → defaults to proxy
	})

	if err := tunnel.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	// Build a minimal QUIC Initial long header (version 1)
	quicPacket := make([]byte, 32)
	quicPacket[0] = 0xC3 // long header + fixed bit + Initial type
	binary.BigEndian.PutUint32(quicPacket[1:5], 0x00000001) // QUIC v1

	// Provide a mock UDP conn for DialUDP to return
	mockRemote := newMockPacketConn()
	dialer.udpConns <- mockRemote

	localPC := newMockPacketConn()
	done := make(chan struct{})
	go func() {
		prov.handler.HandleUDP(localPC, "142.250.197.99:443")
		close(done)
	}()

	localPC.injectRead(quicPacket, nil)

	// DialUDP should be called (QUIC forwarded, not blocked)
	deadline := time.After(3 * time.Second)
	for {
		dialer.mu.Lock()
		n := len(dialer.udpCalls)
		dialer.mu.Unlock()
		if n > 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("DialUDP not called — QUIC was blocked instead of forwarded")
		case <-time.After(10 * time.Millisecond):
		}
	}

	// Clean up
	localPC.Close()
	mockRemote.Close()
	<-done
	tunnel.Close()
}
