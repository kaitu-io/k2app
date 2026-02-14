package server

import (
	"bufio"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/kaitu-io/k2/wire"
)

func TestIsReservedIP(t *testing.T) {
	blocked := []string{
		"127.0.0.1", "127.255.255.255",
		"10.0.0.1", "10.255.255.255",
		"172.16.0.1", "172.31.255.255",
		"192.168.0.1", "192.168.255.255",
		"169.254.1.1",
		"100.64.0.1", "100.127.255.255",
		"0.0.0.0",
		"::1",
		"fe80::1",
		"fc00::1",
		"fd12::1",
		"::",
	}
	for _, s := range blocked {
		ip := net.ParseIP(s)
		if ip == nil {
			t.Fatalf("bad test IP: %s", s)
		}
		if !isReservedIP(ip) {
			t.Errorf("expected %s to be reserved", s)
		}
	}

	allowed := []string{
		"8.8.8.8", "1.1.1.1", "93.184.216.34",
		"172.32.0.1",  // just outside 172.16.0.0/12
		"100.128.0.1", // just outside 100.64.0.0/10
		"2001:4860:4860::8888",
	}
	for _, s := range allowed {
		ip := net.ParseIP(s)
		if ip == nil {
			t.Fatalf("bad test IP: %s", s)
		}
		if isReservedIP(ip) {
			t.Errorf("expected %s to be allowed", s)
		}
	}
}

func TestCheckAddr(t *testing.T) {
	// IP literals that should be blocked.
	blockedAddrs := []string{
		"127.0.0.1:80",
		"10.0.0.1:443",
		"192.168.1.1:22",
		"[::1]:53",
		"100.64.0.1:8080",
	}
	for _, addr := range blockedAddrs {
		if err := checkAddr(addr); err == nil {
			t.Errorf("expected %s to be blocked", addr)
		}
	}

	// Public IPs that should be allowed.
	allowedAddrs := []string{
		"8.8.8.8:53",
		"1.1.1.1:443",
		"[2001:4860:4860::8888]:53",
	}
	for _, addr := range allowedAddrs {
		if err := checkAddr(addr); err != nil {
			t.Errorf("expected %s to be allowed, got: %v", addr, err)
		}
	}

	// Invalid addresses.
	if err := checkAddr("no-port"); err == nil {
		t.Error("expected error for missing port")
	}
}

func TestProxyHandler_HandleTCP_PrivateIP(t *testing.T) {
	h := NewProxyHandler()
	// Default: AllowPrivateIP = false

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()

	var wg sync.WaitGroup
	wg.Go(func() {
		h.HandleTCP(serverConn, "192.168.1.1:80")
	})

	br := bufio.NewReader(clientConn)
	ok, _, err := wire.ReadStreamResponse(br)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected error response for private IP target")
	}

	wg.Wait()
}

func TestProxyHandler_HandleUDP_PrivateIP(t *testing.T) {
	h := NewProxyHandler()

	pc := newTestPacketConn()
	h.HandleUDP(pc, "10.0.0.1:53")

	// HandleUDP should close the conn immediately for private IP.
	select {
	case <-pc.closed:
		// expected
	case <-time.After(2 * time.Second):
		t.Fatal("expected conn to be closed for private IP target")
	}
}

func TestProxyHandler_HandleTCP(t *testing.T) {
	// Start a target TCP echo server.
	targetLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer targetLn.Close()

	go func() {
		conn, err := targetLn.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		io.Copy(conn, conn)
	}()

	h := NewProxyHandler()
	h.AllowPrivateIP = true // echo server is on localhost

	// Simulate the wire stream with a pipe.
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()

	var wg sync.WaitGroup
	wg.Go(func() {
		h.HandleTCP(serverConn, targetLn.Addr().String())
	})

	// Client: read stream response.
	br := bufio.NewReader(clientConn)
	ok, errMsg, err := wire.ReadStreamResponse(br)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatalf("expected success, got error: %s", errMsg)
	}

	// Send data, expect echo.
	testData := []byte("hello proxy")
	if _, err := clientConn.Write(testData); err != nil {
		t.Fatal(err)
	}

	buf := make([]byte, len(testData))
	if _, err := io.ReadFull(br, buf); err != nil {
		t.Fatal(err)
	}
	if string(buf) != "hello proxy" {
		t.Fatalf("expected 'hello proxy', got %q", buf)
	}

	clientConn.Close()
	wg.Wait()
}

func TestProxyHandler_HandleTCP_DialFail(t *testing.T) {
	h := NewProxyHandler()
	h.DialTimeout = 100 * time.Millisecond
	h.AllowPrivateIP = true // testing dial failure, not IP guard

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()

	var wg sync.WaitGroup
	wg.Go(func() {
		h.HandleTCP(serverConn, "127.0.0.1:1") // port 1 should fail
	})

	br := bufio.NewReader(clientConn)
	ok, _, err := wire.ReadStreamResponse(br)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected error response for bad target")
	}

	wg.Wait()
}

func TestProxyHandler_HandleUDP(t *testing.T) {
	// Start a target UDP echo server.
	targetConn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer targetConn.Close()

	go func() {
		buf := make([]byte, 4096)
		for {
			n, addr, err := targetConn.ReadFrom(buf)
			if err != nil {
				return
			}
			targetConn.WriteTo(buf[:n], addr)
		}
	}()

	h := NewProxyHandler()
	h.AllowPrivateIP = true // echo server is on localhost
	pc := newTestPacketConn()

	go h.HandleUDP(pc, targetConn.LocalAddr().String())

	// Send data through the mock client side.
	pc.readCh <- []byte("hello udp")

	// Read echoed response.
	select {
	case data := <-pc.writeCh:
		if string(data) != "hello udp" {
			t.Fatalf("expected 'hello udp', got %q", data)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for UDP echo")
	}

	pc.Close()
}

// testPacketConn is a channel-based PacketConn for testing.
type testPacketConn struct {
	readCh  chan []byte
	writeCh chan []byte
	closed  chan struct{}
}

func newTestPacketConn() *testPacketConn {
	return &testPacketConn{
		readCh:  make(chan []byte, 16),
		writeCh: make(chan []byte, 16),
		closed:  make(chan struct{}),
	}
}

func (c *testPacketConn) ReadFrom(b []byte) (int, net.Addr, error) {
	select {
	case data := <-c.readCh:
		n := copy(b, data)
		return n, nil, nil
	case <-c.closed:
		return 0, nil, net.ErrClosed
	}
}

func (c *testPacketConn) WriteTo(b []byte, _ net.Addr) (int, error) {
	data := make([]byte, len(b))
	copy(data, b)
	select {
	case c.writeCh <- data:
		return len(b), nil
	case <-c.closed:
		return 0, net.ErrClosed
	}
}

func (c *testPacketConn) Close() error {
	select {
	case <-c.closed:
	default:
		close(c.closed)
	}
	return nil
}

func (c *testPacketConn) LocalAddr() net.Addr                { return nil }
func (c *testPacketConn) SetDeadline(time.Time) error        { return nil }
func (c *testPacketConn) SetReadDeadline(time.Time) error    { return nil }
func (c *testPacketConn) SetWriteDeadline(time.Time) error   { return nil }
