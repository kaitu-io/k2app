package wire

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	smux "github.com/xtaci/smux/v2"
)

func TestDefaultSmuxConfig(t *testing.T) {
	cfg := DefaultSmuxConfig()
	if cfg == nil {
		t.Fatal("DefaultSmuxConfig returned nil")
	}
	if cfg.MaxFrameSize != 32768 {
		t.Errorf("MaxFrameSize = %d, want 32768", cfg.MaxFrameSize)
	}
	if cfg.MaxReceiveBuffer != 262144 {
		t.Errorf("MaxReceiveBuffer = %d, want 262144", cfg.MaxReceiveBuffer)
	}
	if cfg.MaxStreamBuffer != 65536 {
		t.Errorf("MaxStreamBuffer = %d, want 65536", cfg.MaxStreamBuffer)
	}
	if cfg.KeepAliveInterval != 10*time.Second {
		t.Errorf("KeepAliveInterval = %v, want 10s", cfg.KeepAliveInterval)
	}
	if cfg.KeepAliveTimeout != 30*time.Second {
		t.Errorf("KeepAliveTimeout = %v, want 30s", cfg.KeepAliveTimeout)
	}
}

func TestTCPWSClient_Create(t *testing.T) {
	cfg := Config{
		Host:        "1.2.3.4",
		Port:        443,
		SNI:         "www.microsoft.com",
		ECH:         "dGVzdA==",
		Fingerprint: "chrome",
	}
	tc := NewTCPWSClient(cfg, nil)
	if tc == nil {
		t.Fatal("NewTCPWSClient returned nil")
	}
	if tc.serverAddr != "1.2.3.4:443" {
		t.Errorf("serverAddr = %q, want %q", tc.serverAddr, "1.2.3.4:443")
	}
}

func TestTCPWSServer_Create(t *testing.T) {
	store := NewTicketStore()
	ts := NewTCPWSServer(store)
	if ts == nil {
		t.Fatal("NewTCPWSServer returned nil")
	}
}

func TestTCPWSClient_InterfaceCompliance(t *testing.T) {
	var _ Dialer = (*TCPWSClient)(nil)
	var _ Listener = (*TCPWSServer)(nil)
}

// setupTCPWSPair creates a connected TCPWSClient + TCPWSServer pair using net.Pipe.
func setupTCPWSPair(t *testing.T) (*TCPWSClient, *TCPWSServer, func()) {
	t.Helper()
	clientConn, serverConn := net.Pipe()

	store := NewTicketStore()
	server := NewTCPWSServer(store)
	server.HandleConn(serverConn)

	client := NewTCPWSClient(Config{Host: "127.0.0.1", Port: 443}, store)
	client.testConn = clientConn

	cleanup := func() {
		client.Close()
		server.Close()
		clientConn.Close()
		serverConn.Close()
	}
	return client, server, cleanup
}

func TestTCPWSClient_DialTCP(t *testing.T) {
	client, server, cleanup := setupTCPWSPair(t)
	defer cleanup()

	// Server: accept stream, write OK response, echo data
	serverDone := make(chan error, 1)
	go func() {
		conn, addr, err := server.AcceptTCP(context.Background())
		if err != nil {
			serverDone <- err
			return
		}
		if addr != "example.com:80" {
			serverDone <- fmt.Errorf("addr = %q, want example.com:80", addr)
			return
		}

		if err := WriteStreamResponse(conn, nil); err != nil {
			serverDone <- err
			return
		}

		buf := make([]byte, 100)
		n, err := conn.Read(buf)
		if err != nil {
			serverDone <- err
			return
		}
		if _, err := conn.Write(buf[:n]); err != nil {
			serverDone <- err
			return
		}
		serverDone <- nil
	}()

	// Client: dial TCP
	conn, err := client.DialTCP(context.Background(), "example.com:80")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	if _, err := conn.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}

	buf := make([]byte, 100)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	if string(buf[:n]) != "hello" {
		t.Errorf("echo = %q, want %q", string(buf[:n]), "hello")
	}

	if err := <-serverDone; err != nil {
		t.Fatalf("server error: %v", err)
	}
}

func TestTCPWSClient_DialTCP_Multiple(t *testing.T) {
	client, server, cleanup := setupTCPWSPair(t)
	defer cleanup()

	type accepted struct {
		addr string
		err  error
	}
	results := make(chan accepted, 3)
	go func() {
		for i := 0; i < 3; i++ {
			conn, addr, err := server.AcceptTCP(context.Background())
			if err != nil {
				results <- accepted{err: err}
				continue
			}
			WriteStreamResponse(conn, nil)
			results <- accepted{addr: addr}
		}
	}()

	addrs := []string{"a.com:80", "b.com:443", "c.com:8080"}
	for _, addr := range addrs {
		conn, err := client.DialTCP(context.Background(), addr)
		if err != nil {
			t.Fatalf("DialTCP(%s): %v", addr, err)
		}
		conn.Close()
	}

	got := make(map[string]bool)
	for i := 0; i < 3; i++ {
		select {
		case r := <-results:
			if r.err != nil {
				t.Fatalf("AcceptTCP: %v", r.err)
			}
			got[r.addr] = true
		case <-time.After(5 * time.Second):
			t.Fatal("timeout")
		}
	}
	for _, addr := range addrs {
		if !got[addr] {
			t.Errorf("missing addr %q", addr)
		}
	}
}

func TestTCPWSClient_DialUDP(t *testing.T) {
	client, server, cleanup := setupTCPWSPair(t)
	defer cleanup()

	// Server: set up UDP handler
	received := make(chan string, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go server.ServeUDP(ctx, UDPHandlerFunc(func(conn net.PacketConn, addr string) {
		buf := make([]byte, 100)
		n, _, err := conn.ReadFrom(buf)
		if err != nil {
			return
		}
		received <- string(buf[:n])
		conn.WriteTo([]byte("response"), nil)
	}))

	time.Sleep(50 * time.Millisecond)

	// Client: dial UDP
	pconn, err := client.DialUDP(context.Background(), "8.8.8.8:53")
	if err != nil {
		t.Fatal(err)
	}
	defer pconn.Close()

	if _, err := pconn.WriteTo([]byte("dns-query"), nil); err != nil {
		t.Fatal(err)
	}

	select {
	case data := <-received:
		if data != "dns-query" {
			t.Errorf("received = %q, want dns-query", data)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for server to receive")
	}

	buf := make([]byte, 100)
	n, _, err := pconn.ReadFrom(buf)
	if err != nil {
		t.Fatal(err)
	}
	if string(buf[:n]) != "response" {
		t.Errorf("response = %q, want response", string(buf[:n]))
	}
}

func TestTCPWSClient_DialUDP_MultipleSessions(t *testing.T) {
	client, server, cleanup := setupTCPWSPair(t)
	defer cleanup()

	received := make(chan string, 2)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go server.ServeUDP(ctx, UDPHandlerFunc(func(conn net.PacketConn, addr string) {
		buf := make([]byte, 100)
		n, _, err := conn.ReadFrom(buf)
		if err != nil {
			return
		}
		received <- addr + ":" + string(buf[:n])
		conn.WriteTo([]byte("reply-"+addr), nil)
	}))

	time.Sleep(50 * time.Millisecond)

	// Open two UDP sessions to different targets
	pconn1, err := client.DialUDP(context.Background(), "8.8.8.8:53")
	if err != nil {
		t.Fatal(err)
	}
	defer pconn1.Close()

	pconn2, err := client.DialUDP(context.Background(), "1.1.1.1:53")
	if err != nil {
		t.Fatal(err)
	}
	defer pconn2.Close()

	pconn1.WriteTo([]byte("q1"), nil)
	pconn2.WriteTo([]byte("q2"), nil)

	got := make(map[string]bool)
	for i := 0; i < 2; i++ {
		select {
		case r := <-received:
			got[r] = true
		case <-time.After(5 * time.Second):
			t.Fatal("timeout")
		}
	}
	if !got["8.8.8.8:53:q1"] {
		t.Error("missing 8.8.8.8:53:q1")
	}
	if !got["1.1.1.1:53:q2"] {
		t.Error("missing 1.1.1.1:53:q2")
	}
}

// ============================================================================
// Existing smux loopback tests (raw smux, no TCPWSClient/TCPWSServer)
// ============================================================================

func TestSmuxLoopback_ProxyStream(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	serverDone := make(chan error, 1)
	go func() {
		sess, err := smux.Server(serverConn, DefaultSmuxConfig())
		if err != nil {
			serverDone <- err
			return
		}
		defer sess.Close()

		stream, err := sess.AcceptStream()
		if err != nil {
			serverDone <- err
			return
		}

		br := bufio.NewReader(stream)
		typ, addr, err := ReadStreamHeader(br)
		if err != nil {
			serverDone <- err
			return
		}
		if typ != StreamTypeTCP || addr != "8.8.8.8:53" {
			serverDone <- net.UnknownNetworkError("unexpected header")
			return
		}

		WriteStreamResponse(stream, nil)

		buf := make([]byte, 100)
		n, err := br.Read(buf)
		if err != nil {
			serverDone <- err
			return
		}
		stream.Write(buf[:n])
		stream.Close()
		serverDone <- nil
	}()

	sess, err := smux.Client(clientConn, DefaultSmuxConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()

	stream, err := sess.OpenStream()
	if err != nil {
		t.Fatal(err)
	}

	WriteStreamHeader(stream, StreamTypeTCP, "8.8.8.8:53")

	br := bufio.NewReader(stream)
	ok, errMsg, err := ReadStreamResponse(br)
	if err != nil {
		t.Fatalf("ReadStreamResponse: %v", err)
	}
	if !ok {
		t.Fatalf("server rejected: %s", errMsg)
	}

	stream.Write([]byte("hello"))

	buf := make([]byte, 100)
	n, err := br.Read(buf)
	if err != nil {
		t.Fatalf("read echo: %v", err)
	}
	if string(buf[:n]) != "hello" {
		t.Errorf("echo = %q, want %q", string(buf[:n]), "hello")
	}

	if err := <-serverDone; err != nil {
		t.Fatalf("server error: %v", err)
	}
}

func TestSmuxLoopback_UDPStream(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	serverDone := make(chan error, 1)
	go func() {
		sess, err := smux.Server(serverConn, DefaultSmuxConfig())
		if err != nil {
			serverDone <- err
			return
		}
		defer sess.Close()

		stream, err := sess.AcceptStream()
		if err != nil {
			serverDone <- err
			return
		}

		br := bufio.NewReader(stream)
		typ, addr, err := ReadStreamHeader(br)
		if err != nil {
			serverDone <- err
			return
		}
		if typ != StreamTypeUDP {
			serverDone <- net.UnknownNetworkError("expected UDP stream type")
			return
		}
		_ = addr

		frame, err := ReadSmuxUDPFrame(br)
		if err != nil {
			serverDone <- err
			return
		}
		if frame.SessionID != 1 {
			serverDone <- net.UnknownNetworkError("wrong session ID")
			return
		}

		WriteSmuxUDPFrame(stream, frame.SessionID, frame.Addr, frame.Payload)
		stream.Close()
		serverDone <- nil
	}()

	sess, err := smux.Client(clientConn, DefaultSmuxConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()

	stream, err := sess.OpenStream()
	if err != nil {
		t.Fatal(err)
	}

	WriteStreamHeader(stream, StreamTypeUDP, "")
	WriteSmuxUDPFrame(stream, 1, "8.8.8.8:53", []byte("dns-query"))

	br := bufio.NewReader(stream)
	frame, err := ReadSmuxUDPFrame(br)
	if err != nil {
		t.Fatalf("ReadSmuxUDPFrame: %v", err)
	}
	if frame.SessionID != 1 {
		t.Errorf("session_id = %d, want 1", frame.SessionID)
	}
	if !bytes.Equal(frame.Payload, []byte("dns-query")) {
		t.Errorf("payload = %q, want dns-query", frame.Payload)
	}

	if err := <-serverDone; err != nil {
		t.Fatalf("server error: %v", err)
	}
}

func TestSmuxLoopback_MultipleStreams(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	const numStreams = 5

	serverDone := make(chan error, 1)
	go func() {
		sess, err := smux.Server(serverConn, DefaultSmuxConfig())
		if err != nil {
			serverDone <- err
			return
		}

		var wg sync.WaitGroup
		for i := 0; i < numStreams; i++ {
			stream, err := sess.AcceptStream()
			if err != nil {
				serverDone <- err
				return
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer stream.Close()
				br := bufio.NewReader(stream)
				_, _, _ = ReadStreamHeader(br)
				WriteStreamResponse(stream, nil)
				buf := make([]byte, 100)
				n, _ := br.Read(buf)
				stream.Write(buf[:n])
			}()
		}
		wg.Wait()
		sess.Close()
		serverDone <- nil
	}()

	sess, err := smux.Client(clientConn, DefaultSmuxConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()

	for i := 0; i < numStreams; i++ {
		stream, err := sess.OpenStream()
		if err != nil {
			t.Fatal(err)
		}
		WriteStreamHeader(stream, StreamTypeTCP, "10.0.0.1:80")

		br := bufio.NewReader(stream)
		ok, _, err := ReadStreamResponse(br)
		if err != nil {
			t.Fatalf("stream %d: ReadStreamResponse: %v", i, err)
		}
		if !ok {
			t.Fatalf("stream %d: rejected", i)
		}

		stream.Write([]byte("data"))
		buf := make([]byte, 100)
		n, err := br.Read(buf)
		if err != nil {
			t.Fatalf("stream %d: read echo: %v", i, err)
		}
		if string(buf[:n]) != "data" {
			t.Errorf("stream %d: echo = %q", i, string(buf[:n]))
		}
		stream.Close()
	}

	if err := <-serverDone; err != nil {
		t.Fatalf("server error: %v", err)
	}
}
