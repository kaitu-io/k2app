package core

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/kaitu-io/k2/provider"
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
