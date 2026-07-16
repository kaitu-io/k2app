package core

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kaitu-io/k2/provider"
	"github.com/kaitu-io/k2/rule"
	"github.com/kaitu-io/k2/sniff"
	"github.com/kaitu-io/k2/wire"
)

// testOutbounds builds an outbound table for tests: [0]=direct, [1]=nil(reject), [2]=wire.
func testOutbounds(dialer wire.Dialer) []Outbound {
	return []Outbound{NewDirectOutbound(nil), nil, NewWireOutbound("test", dialer, nil)}
}

// mockDialer records DialTCP/DialUDP calls and returns mock connections.
type mockDialer struct {
	mu            sync.Mutex
	tcpCalls      []string
	udpCalls      []string
	tcpEarlyDatas [][]byte // earlyData passed to each DialTCP call

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

func (d *mockDialer) DialTCP(ctx context.Context, addr string, earlyData []byte) (net.Conn, error) {
	d.mu.Lock()
	d.tcpCalls = append(d.tcpCalls, addr)
	d.tcpEarlyDatas = append(d.tcpEarlyDatas, earlyData)
	d.mu.Unlock()

	if d.dialErr != nil {
		return nil, d.dialErr
	}

	select {
	case conn := <-d.tcpConns:
		// Write early data into the conn to simulate server-side behavior:
		// server receives earlyData via wire, then pipes it to upstream target.
		if len(earlyData) > 0 {
			if _, err := conn.Write(earlyData); err != nil {
				conn.Close()
				return nil, err
			}
		}
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

func (d *mockDialer) DialStreamUDP(ctx context.Context, addr string) (net.PacketConn, error) {
	return d.DialUDP(ctx, addr)
}

func (d *mockDialer) Reset() {}

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
		Provider:     prov,
		Outbounds:    testOutbounds(dialer),
		DirectTarget: rule.Target(0),
		// Router nil → defaults to proxy
		DefaultTarget: rule.Target(2),
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
	// Use a router with a rule engine that defaults to direct for localhost.
	ruleEngine := rule.NewEngine(nil, rule.Target(0), nil, false)
	tunnel := NewClientTunnel(ClientTunnelConfig{
		Provider:     prov,
		Outbounds:    testOutbounds(newMockDialer()),
		DirectTarget: rule.Target(0),
		Router:       NewRouter(ruleEngine),
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
		Provider:      prov,
		Outbounds:     testOutbounds(dialer),
		DirectTarget:  rule.Target(0),
		DefaultTarget: rule.Target(2),
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
		Provider:     prov,
		Outbounds:    testOutbounds(newMockDialer()),
		DirectTarget: rule.Target(0),
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

// mockRejectingPacketConn implements provider.UDPRejecter on top of mockPacketConn
// so type assertions in HandleUDP succeed and we can count RejectHandshake calls.
type mockRejectingPacketConn struct {
	*mockPacketConn
	rejectCount atomic.Int32
}

func newMockRejectingPacketConn() *mockRejectingPacketConn {
	return &mockRejectingPacketConn{mockPacketConn: newMockPacketConn()}
}

func (c *mockRejectingPacketConn) RejectHandshake() error {
	c.rejectCount.Add(1)
	return nil
}

// TestClientTunnel_HandleUDP_IPv6Drop verifies IPv6 UDP destinations are
// silently dropped without invoking RejectHandshake (which routes through
// gVisor's gWriteUnreachable and races against buffer chunk refcounts).
func TestClientTunnel_HandleUDP_IPv6Drop(t *testing.T) {
	dialer := newMockDialer()
	prov := &mockProvider{}

	tunnel := NewClientTunnel(ClientTunnelConfig{
		Provider:      prov,
		Outbounds:     testOutbounds(dialer),
		DirectTarget:  rule.Target(0),
		DefaultTarget: rule.Target(2),
	})
	if err := tunnel.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer tunnel.Close()

	localPC := newMockRejectingPacketConn()
	done := make(chan struct{})
	go func() {
		prov.handler.HandleUDP(localPC, "[2001:db8::1]:443")
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("HandleUDP did not return for IPv6 dest")
	}

	if got := localPC.rejectCount.Load(); got != 0 {
		t.Errorf("RejectHandshake called %d times for IPv6 dest, want 0 (panic-prone path)", got)
	}
	dialer.mu.Lock()
	defer dialer.mu.Unlock()
	if len(dialer.udpCalls) != 0 {
		t.Errorf("DialUDP called %d times, want 0 (IPv6 should be silently dropped)", len(dialer.udpCalls))
	}
}

// TestClientTunnel_HandleUDP_QUICBlockedDrop verifies the blockQUIC paths
// also drop silently without invoking RejectHandshake (same gVisor concern).
func TestClientTunnel_HandleUDP_QUICBlockedDrop(t *testing.T) {
	dialer := newMockDialer()
	prov := &mockProvider{}

	tunnel := NewClientTunnel(ClientTunnelConfig{
		Provider:      prov,
		Outbounds:     testOutbounds(dialer),
		DirectTarget:  rule.Target(0),
		BlockQUIC:     true,
		DefaultTarget: rule.Target(2),
	})
	if err := tunnel.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer tunnel.Close()

	quicPacket := make([]byte, 32)
	quicPacket[0] = 0xC3
	binary.BigEndian.PutUint32(quicPacket[1:5], 0x00000001)

	localPC := newMockRejectingPacketConn()
	done := make(chan struct{})
	go func() {
		prov.handler.HandleUDP(localPC, "142.250.197.99:443")
		close(done)
	}()
	localPC.injectRead(quicPacket, nil)

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("HandleUDP did not return — QUIC block not working")
	}

	if got := localPC.rejectCount.Load(); got != 0 {
		t.Errorf("RejectHandshake called %d times for blocked QUIC, want 0", got)
	}
}

// --- Close unblocking tests ---

// TestClientTunnel_Close_UnblocksStuckTCP verifies that Close() forcefully
// interrupts handler goroutines stuck in pipe() by closing tracked connections.
// This reproduces the TUN mode disconnect hang (I5).
func TestClientTunnel_Close_UnblocksStuckTCP(t *testing.T) {
	dialer := newMockDialer()
	prov := &mockProvider{}

	tunnel := NewClientTunnel(ClientTunnelConfig{
		Provider:      prov,
		Outbounds:     testOutbounds(dialer),
		DirectTarget:  rule.Target(0),
		DefaultTarget: rule.Target(2),
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
		Provider:      prov,
		Outbounds:     testOutbounds(dialer),
		DirectTarget:  rule.Target(0),
		DefaultTarget: rule.Target(2),
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
		Provider:     prov,
		Outbounds:    testOutbounds(dialer),
		DirectTarget:  rule.Target(0),
		DefaultTarget: rule.Target(2),
		SniffBT:      true,
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
		Provider:     prov,
		Outbounds:    testOutbounds(dialer),
		DirectTarget: rule.Target(0),
		SniffBT:      true,
		// Router nil → defaults to proxy
		DefaultTarget: rule.Target(2),
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
		Provider:      prov,
		Outbounds:     testOutbounds(dialer),
		DirectTarget:  rule.Target(0),
		DefaultTarget: rule.Target(2),
		SniffBT:      false, // global mode — no sniffing
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

// TestClientTunnel_HandleTCP_EarlyData verifies the early data optimization:
// peeked bytes (TLS ClientHello) are sent as earlyData to DialTCP instead of
// being replayed through pipe(), preventing double-send.
func TestClientTunnel_HandleTCP_EarlyData(t *testing.T) {
	dialer := newMockDialer()
	prov := &mockProvider{}

	tunnel := NewClientTunnel(ClientTunnelConfig{
		Provider:      prov,
		Outbounds:     testOutbounds(dialer),
		DirectTarget:  rule.Target(0),
		DefaultTarget: rule.Target(2),
		SniffBT:      true,
		SniffSNI:     true,
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

	// Send TLS ClientHello — should be captured as earlyData for PROXY path
	tlsHello := []byte{0x16, 0x03, 0x01, 0x00, 0x05, 0x01, 0x00, 0x00, 0x01, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
	providerConn.Write(tlsHello)

	// Read earlyData from remote side (mock dialer writes it into conn)
	buf := make([]byte, 256)
	n, err := clientConn.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(buf[:n], tlsHello) {
		t.Errorf("earlyData mismatch: got %d bytes starting with 0x%02x, want %d bytes starting with 0x16",
			n, buf[0], len(tlsHello))
	}

	// Send follow-up data through providerConn — this goes through pipe(), not earlyData
	followUp := []byte("GET / HTTP/1.1\r\nHost: example.com\r\n\r\n")
	providerConn.Write(followUp)

	n, err = clientConn.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(buf[:n], followUp) {
		t.Errorf("follow-up data mismatch: got %q, want %q", buf[:n], followUp)
	}

	providerConn.Close()
	clientConn.Close()
	<-done

	// Verify earlyData was passed to DialTCP (not nil)
	dialer.mu.Lock()
	defer dialer.mu.Unlock()
	if len(dialer.tcpEarlyDatas) != 1 {
		t.Fatalf("DialTCP called %d times, expected 1", len(dialer.tcpEarlyDatas))
	}
	if !bytes.Equal(dialer.tcpEarlyDatas[0], tlsHello) {
		t.Errorf("earlyData passed to DialTCP = %v, want TLS hello", dialer.tcpEarlyDatas[0])
	}

	tunnel.Close()
}

// TestClientTunnel_HandleTCP_EarlyDataGlobalMode verifies early data works in
// global mode (sniffBT=false, sniffSNI=false). The PROXY path should still peek
// and send earlyData even without sniffing.
func TestClientTunnel_HandleTCP_EarlyDataGlobalMode(t *testing.T) {
	dialer := newMockDialer()
	prov := &mockProvider{}

	tunnel := NewClientTunnel(ClientTunnelConfig{
		Provider:      prov,
		Outbounds:     testOutbounds(dialer),
		DirectTarget:  rule.Target(0),
		DefaultTarget: rule.Target(2),
		SniffBT:      false, // global mode
		SniffSNI:     false,
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

	// Send TLS ClientHello
	tlsHello := []byte{0x16, 0x03, 0x01, 0x00, 0x05, 0x01, 0x00, 0x00, 0x01, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
	providerConn.Write(tlsHello)

	// Read earlyData from remote side
	buf := make([]byte, 256)
	n, err := clientConn.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(buf[:n], tlsHello) {
		t.Errorf("earlyData mismatch: got %d bytes, want %d", n, len(tlsHello))
	}

	providerConn.Close()
	clientConn.Close()
	<-done

	// Verify earlyData was passed to DialTCP
	dialer.mu.Lock()
	defer dialer.mu.Unlock()
	if len(dialer.tcpEarlyDatas) != 1 {
		t.Fatalf("DialTCP called %d times, expected 1", len(dialer.tcpEarlyDatas))
	}
	if !bytes.Equal(dialer.tcpEarlyDatas[0], tlsHello) {
		t.Errorf("earlyData in global mode = %v, want TLS hello", dialer.tcpEarlyDatas[0])
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
		Provider:     prov,
		Outbounds:    testOutbounds(dialer),
		DirectTarget:  rule.Target(0),
		DefaultTarget: rule.Target(2),
		SniffBT:      true,
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
		Provider:     prov,
		Outbounds:    testOutbounds(dialer),
		DirectTarget: rule.Target(0),
		SniffBT:      true,
		// Router nil → defaults to proxy
		DefaultTarget: rule.Target(2),
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

// STUN (WebRTC call setup) must be RELAYED THROUGH THE PROXY, not forced
// direct. Forcing it direct broke Telegram/KakaoTalk voice/video calls for
// users behind the GFW (support ticket #3051). The detection still runs (so
// the QUIC sniff is skipped for a WebRTC packet), but the proxy route is kept.
func TestClientTunnel_HandleUDP_STUNKeepsProxyRoute(t *testing.T) {
	// A direct listener that must NOT receive the STUN packet.
	directListener, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer directListener.Close()

	wentDirect := make(chan struct{}, 1)
	go func() {
		buf := make([]byte, 4096)
		if _, _, err := directListener.ReadFrom(buf); err == nil {
			wentDirect <- struct{}{}
		}
	}()

	dialer := newMockDialer()
	// Pre-load the proxy-side conn so DialUDP returns; what the tunnel writes
	// into it is the "relayed through proxy" evidence.
	remotePC := newMockPacketConn()
	dialer.udpConns <- remotePC

	prov := &mockProvider{}
	tunnel := NewClientTunnel(ClientTunnelConfig{
		Provider:      prov,
		Outbounds:     testOutbounds(dialer),
		DirectTarget:  rule.Target(0),
		DefaultTarget: rule.Target(2),
		SniffBT:       true,
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
	case got := <-remotePC.writeCh:
		if !bytes.Equal(got.data, stunPacket) {
			t.Errorf("proxy conn got %x, want %x", got.data, stunPacket)
		}
	case <-wentDirect:
		t.Fatal("STUN egressed DIRECT — regression of #3051")
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for STUN to reach the proxy conn")
	}

	localPC.closeConn()
	remotePC.closeConn()
	<-done

	dialer.mu.Lock()
	defer dialer.mu.Unlock()
	if len(dialer.udpCalls) != 1 {
		t.Errorf("DialUDP called %d times, expected 1 (STUN relayed through proxy)", len(dialer.udpCalls))
	}

	tunnel.Close()
}

// DTLS (WebRTC media key exchange) must be RELAYED THROUGH THE PROXY, not
// forced direct — same #3051 root cause and contract as the STUN case above.
func TestClientTunnel_HandleUDP_DTLSKeepsProxyRoute(t *testing.T) {
	directListener, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer directListener.Close()

	wentDirect := make(chan struct{}, 1)
	go func() {
		buf := make([]byte, 4096)
		if _, _, err := directListener.ReadFrom(buf); err == nil {
			wentDirect <- struct{}{}
		}
	}()

	dialer := newMockDialer()
	remotePC := newMockPacketConn()
	dialer.udpConns <- remotePC

	prov := &mockProvider{}
	tunnel := NewClientTunnel(ClientTunnelConfig{
		Provider:      prov,
		Outbounds:     testOutbounds(dialer),
		DirectTarget:  rule.Target(0),
		DefaultTarget: rule.Target(2),
		SniffBT:       true,
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
	case got := <-remotePC.writeCh:
		if !bytes.Equal(got.data, dtlsPacket) {
			t.Errorf("proxy conn got %x, want %x", got.data, dtlsPacket)
		}
	case <-wentDirect:
		t.Fatal("DTLS egressed DIRECT — regression of #3051")
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for DTLS to reach the proxy conn")
	}

	localPC.closeConn()
	remotePC.closeConn()
	<-done

	dialer.mu.Lock()
	defer dialer.mu.Unlock()
	if len(dialer.udpCalls) != 1 {
		t.Errorf("DialUDP called %d times, expected 1 (DTLS relayed through proxy)", len(dialer.udpCalls))
	}

	tunnel.Close()
}

func TestClientTunnel_HandleTCP_HTTPSniffReplay(t *testing.T) {
	dialer := newMockDialer()
	prov := &mockProvider{}

	tunnel := NewClientTunnel(ClientTunnelConfig{
		Provider:     prov,
		Outbounds:    testOutbounds(dialer),
		DirectTarget:  rule.Target(0),
		DefaultTarget: rule.Target(2),
		SniffSNI:     true,
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
		Provider:     prov,
		Outbounds:    testOutbounds(dialer),
		DirectTarget: rule.Target(0),
		BlockQUIC:    true,
		// Router nil → defaults to proxy
		DefaultTarget: rule.Target(2),
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

// waitForGoroutineCount polls until runtime.NumGoroutine() drops to maxCount
// or below, returning true on success. Returns false if timeout expires.
func waitForGoroutineCount(t *testing.T, maxCount int, timeout time.Duration) bool {
	t.Helper()
	deadline := time.After(timeout)
	for {
		if runtime.NumGoroutine() <= maxCount {
			return true
		}
		select {
		case <-deadline:
			return false
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// TestTunnelClose_Timeout_GoroutineEventuallyExits verifies that the wg.Wait
// goroutine spawned by Close() eventually exits after the slow handler
// completes — even though Close() itself returned early due to closeTimeout.
func TestTunnelClose_Timeout_GoroutineEventuallyExits(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tunnel := NewClientTunnel(ClientTunnelConfig{
		// Minimal config — we only need wg and conns for Close()
	})
	tunnel.ctx, tunnel.cancel = ctx, cancel

	// Simulate a slow handler by adding to wg without completing yet.
	handlerDone := make(chan struct{})
	tunnel.wg.Add(1)
	go func() {
		<-handlerDone
		tunnel.wg.Done()
	}()

	baseline := runtime.NumGoroutine()

	// Close should return after closeTimeout (~5s) even though handler is stuck.
	start := time.Now()
	tunnel.Close()
	elapsed := time.Since(start)

	// Verify Close returned via timeout, not handler completion.
	if elapsed < 4*time.Second {
		t.Fatalf("Close returned too quickly (%v), handler wasn't blocking", elapsed)
	}

	// wg.Wait goroutine is still alive. Release the handler.
	close(handlerDone)

	// Wait for the orphaned goroutine to drain.
	// Allow baseline+3 for runtime jitter.
	if !waitForGoroutineCount(t, baseline+3, 3*time.Second) {
		t.Fatalf("wg.Wait goroutine leaked: baseline=%d, current=%d",
			baseline, runtime.NumGoroutine())
	}
}

func TestClientTunnel_HandleUDP_QUICPassthrough(t *testing.T) {
	dialer := newMockDialer()
	prov := &mockProvider{}

	tunnel := NewClientTunnel(ClientTunnelConfig{
		Provider:     prov,
		Outbounds:    testOutbounds(dialer),
		DirectTarget: rule.Target(0),
		BlockQUIC:    false, // Allow QUIC through
		// Router nil → defaults to proxy
		DefaultTarget: rule.Target(2),
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

// makeTCPPair returns a connected *net.TCPConn pair on loopback.
// Used by pipe() regression tests to exercise the real closeWriter type
// assertion path (stdlib *net.TCPConn implements CloseWrite).
func makeTCPPair(t *testing.T) (client, server net.Conn) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	type result struct {
		c net.Conn
		e error
	}
	ch := make(chan result, 1)
	go func() {
		c, e := ln.Accept()
		ch <- result{c, e}
	}()
	client, err = net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	r := <-ch
	if r.e != nil {
		client.Close()
		t.Fatal(r.e)
	}
	return client, r.c
}

// errOnReadConn wraps a net.Conn and makes Read return a specific error on
// demand (after a channel is closed). Used to test pipe()'s error-exit path
// deterministically without depending on kernel TCP RST semantics.
type errOnReadConn struct {
	net.Conn
	trigger chan struct{} // close to activate
	readErr error
}

func (c *errOnReadConn) Read(b []byte) (int, error) {
	select {
	case <-c.trigger:
		return 0, c.readErr
	default:
	}
	// Block until either data arrives on the inner conn or trigger fires.
	type result struct {
		n   int
		err error
	}
	resCh := make(chan result, 1)
	go func() {
		n, err := c.Conn.Read(b)
		resCh <- result{n, err}
	}()
	select {
	case r := <-resCh:
		return r.n, r.err
	case <-c.trigger:
		// Unblock the inner Read by closing the conn, drain the goroutine.
		c.Conn.SetReadDeadline(time.Now())
		<-resCh
		return 0, c.readErr
	}
}

// TestPipe_ErrorPath_UnblocksPeer is the core regression test for the
// slot-leak introduced by commit 2068d24 (see ticket #97). When the remote
// wire stream returns a non-EOF error (simulating tm.Reset()
// closing an in-flight smux/QUIC stream), the RX goroutine must full-close
// BOTH conns so the TX goroutine's blocked Read on the local peer returns
// immediately. Before the fix, the RX goroutine only half-closed the local
// conn via CloseWrite() and the TX goroutine leaked indefinitely.
func TestPipe_ErrorPath_UnblocksPeer(t *testing.T) {
	// Keep the watchdog at production length so this test can only pass via
	// the error-path fast fix, not via watchdog rescue.
	orig := pipeWatchdogGrace
	pipeWatchdogGrace = 30 * time.Second
	defer func() { pipeWatchdogGrace = orig }()

	localClient, localApp := makeTCPPair(t)
	defer localClient.Close()
	defer localApp.Close()

	innerRemote, remoteServer := makeTCPPair(t)
	defer innerRemote.Close()
	defer remoteServer.Close()

	// Sanity: stdlib *net.TCPConn implements closeWriter — the old buggy
	// path would take the half-close branch. errOnReadConn intentionally
	// does NOT expose CloseWrite so the pipe falls back to Close() on
	// clean exits, but the error-path branch is what we're testing here.
	if _, ok := localClient.(closeWriter); !ok {
		t.Fatalf("local: expected *net.TCPConn to implement closeWriter, got %T", localClient)
	}

	trigger := make(chan struct{})
	remoteClient := &errOnReadConn{
		Conn:    innerRemote,
		trigger: trigger,
		readErr: errors.New("simulated wire reset"),
	}

	done := make(chan struct{})
	var tx, rx atomic.Int64
	go func() {
		defer close(done)
		pipe(localClient, remoteClient, &tx, &rx)
	}()

	// Let both directions enter their blocking Reads.
	time.Sleep(100 * time.Millisecond)

	// Simulate tm.Reset(): remote stream Read returns a
	// non-EOF transport error.
	close(trigger)

	// localApp is the quiet peer: never sends, never closes. With the fix,
	// G2's error exit full-closes localClient, unblocking G1's Read.
	// Without the fix, pipe() hangs until the 30s watchdog (disabled here).
	select {
	case <-done:
		// success
	case <-time.After(2 * time.Second):
		t.Fatal("pipe() did not return after remote error — error path fix not working")
	}
}

// TestPipe_Watchdog_ForceCloseOnStuckCleanExit covers the residual leak path
// that the error-exit fix alone does not handle: one direction exits with a
// clean EOF (half-closing the peer per the 2068d24 download-correctness
// intent), but the other direction's Read stays blocked indefinitely because
// the remote peer never sends FIN. The watchdog must force-close both conns
// after pipeWatchdogGrace.
func TestPipe_Watchdog_ForceCloseOnStuckCleanExit(t *testing.T) {
	// Shorten watchdog for test runtime.
	orig := pipeWatchdogGrace
	pipeWatchdogGrace = 300 * time.Millisecond
	defer func() { pipeWatchdogGrace = orig }()

	localClient, localApp := makeTCPPair(t)
	defer localClient.Close()
	defer localApp.Close()

	remoteClient, remoteServer := makeTCPPair(t)
	defer remoteClient.Close()
	defer remoteServer.Close()

	done := make(chan struct{})
	var tx, rx atomic.Int64
	start := time.Now()
	go func() {
		defer close(done)
		pipe(localClient, remoteClient, &tx, &rx)
	}()

	// Let both directions enter their blocking Reads.
	time.Sleep(50 * time.Millisecond)

	// Server half-closes its write direction, sending FIN to remoteClient.
	// G2's io.CopyBuffer(a, b) reads EOF cleanly (err == nil) → G2 calls
	// localClient.CloseWrite(). localApp (the quiet peer) never reacts to
	// the FIN, so G1 stays blocked in localClient.Read().
	if tcp, ok := remoteServer.(*net.TCPConn); ok {
		if err := tcp.CloseWrite(); err != nil {
			t.Fatalf("remoteServer.CloseWrite: %v", err)
		}
	} else {
		t.Fatalf("remoteServer: expected *net.TCPConn, got %T", remoteServer)
	}

	// Watchdog must fire within ~grace + scheduling slack.
	select {
	case <-done:
		elapsed := time.Since(start)
		if elapsed < 200*time.Millisecond {
			t.Fatalf("pipe() returned too quickly (%v) — watchdog may not have triggered", elapsed)
		}
		if elapsed > 2*time.Second {
			t.Fatalf("pipe() returned too slowly (%v) — watchdog grace not honored", elapsed)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("pipe() never returned — watchdog did not force-close stuck pipe")
	}
}

// TestPipe_CleanBidirectionalExit verifies the fast-path: both directions
// complete normally (EOF on Read) without the watchdog firing. Validates that
// the fix doesn't cause spurious force-closes on well-behaved pipes.
func TestPipe_CleanBidirectionalExit(t *testing.T) {
	// pipeWatchdogGrace is package-level; explicitly set a generous value so
	// this test does not depend on whichever value an earlier test left
	// behind. Windows GitHub runners need >300ms for two TCP loopback
	// directions to flush before the watchdog force-closes them.
	orig := pipeWatchdogGrace
	pipeWatchdogGrace = 30 * time.Second
	defer func() { pipeWatchdogGrace = orig }()

	localClient, localApp := makeTCPPair(t)
	remoteClient, remoteServer := makeTCPPair(t)
	// Cleanup: pipe() / pipeDirExit half-close one side per conn; full-close
	// the remaining halves so the test does not leak descriptors.
	defer localClient.Close()
	defer localApp.Close()
	defer remoteClient.Close()
	defer remoteServer.Close()

	done := make(chan struct{})
	var tx, rx atomic.Int64
	start := time.Now()
	go func() {
		defer close(done)
		pipe(localClient, remoteClient, &tx, &rx)
	}()

	// Both peers send a small payload and half-close their write direction.
	// pipe() should complete quickly via natural EOF on both sides.
	//
	// CloseWrite (not full Close) on purpose: a full Close on one peer races
	// the other direction's Write. On Windows loopback the recently-closed
	// peer kernel RSTs the in-flight Write fast enough that io.CopyBuffer
	// returns (0, ECONNRESET) and the byte counter never bumps. The
	// implementation is correct in production (real wire RTTs > the RST
	// window); only this synthetic-loopback test trips the race. Half-close
	// keeps the peer's read side open so Writes drain cleanly to EOF.
	go func() {
		localApp.Write([]byte("hello"))
		localApp.(*net.TCPConn).CloseWrite()
	}()
	go func() {
		remoteServer.Write([]byte("world"))
		remoteServer.(*net.TCPConn).CloseWrite()
	}()

	select {
	case <-done:
		elapsed := time.Since(start)
		if elapsed > 500*time.Millisecond {
			t.Errorf("clean pipe exit took too long: %v", elapsed)
		}
		if tx.Load() == 0 || rx.Load() == 0 {
			t.Errorf("counters not updated: tx=%d rx=%d", tx.Load(), rx.Load())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("clean bidirectional pipe() did not complete")
	}
}
