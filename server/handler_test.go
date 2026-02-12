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

	// Simulate the wire stream with a pipe.
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		h.HandleTCP(serverConn, targetLn.Addr().String())
	}()

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

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		h.HandleTCP(serverConn, "127.0.0.1:1") // port 1 should fail
	}()

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
