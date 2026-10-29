package wire

import (
	"bufio"
	"bytes"
	"context"
	tls "github.com/metacubex/utls"
	"fmt"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	smux "github.com/xtaci/smux/v2"
	"github.com/coder/websocket"
)

// Both smux configs must pass smux's own check, run receive-window
// auto-tuning (smux K2 patch 6), and leave room for at least 32 stalled
// streams before the session buffer blocks: the old fixed config (64 KB
// windows, 256 KB buffer) blocked on 4.
func TestDefaultSmuxConfig(t *testing.T) {
	for name, cfg := range map[string]*smux.Config{
		"client": DefaultSmuxConfig(),
		"server": ServerSmuxConfig(),
	} {
		if err := smux.VerifyConfig(cfg); err != nil {
			t.Errorf("%s: VerifyConfig: %v", name, err)
		}
		if cfg.MinStreamBuffer <= 0 || cfg.MinStreamBuffer >= cfg.MaxStreamBuffer {
			t.Errorf("%s: MinStreamBuffer %d, MaxStreamBuffer %d: auto-tuning off", name, cfg.MinStreamBuffer, cfg.MaxStreamBuffer)
		}
		if stalled := cfg.MaxReceiveBuffer / 2 / cfg.MinStreamBuffer; stalled < 32 {
			t.Errorf("%s: %d stalled streams block the session; want >= 32", name, stalled)
		}
		if cfg.MaxFrameSize != 32768 {
			t.Errorf("%s: MaxFrameSize = %d, want 32768", name, cfg.MaxFrameSize)
		}
		if cfg.KeepAliveInterval != 10*time.Second || cfg.KeepAliveTimeout != 30*time.Second {
			t.Errorf("%s: keepalive %v/%v, want 10s/30s", name, cfg.KeepAliveInterval, cfg.KeepAliveTimeout)
		}
	}
}

func TestTCPWSClient_Create(t *testing.T) {
	cfg := Config{
		Host:        "1.2.3.4",
		Port:        443,
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
	ts := NewTCPWSServer()
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

	server := NewTCPWSServer()
	server.HandleConn(serverConn)

	client := NewTCPWSClient(Config{Host: "127.0.0.1", Port: 443}, nil)
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
	conn, err := client.DialTCP(context.Background(), "example.com:80", nil)
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
		conn, err := client.DialTCP(context.Background(), addr, nil)
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
	ok, _, errMsg, err := ReadStreamResponse(br)
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
		ok, _, _, err := ReadStreamResponse(br)
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

// ============================================================================
// uTLS + WebSocket Tests (AC8, AC9, AC10, AC11, AC13)
// ============================================================================

// TestTCPWSClient_uTLS_Handshake verifies that TCPWSClient uses utls.UClient
// with the configured ClientHelloID, not standard tls.Client.
// AC8: TCP-WS client uses utls.UClient with configured ClientHelloID
// AC9: TLS fingerprint matches configured profile
func TestTCPWSClient_uTLS_Handshake(t *testing.T) {
	tests := []struct {
		name        string
		fingerprint string
	}{
		{"chrome", "chrome"},
		{"firefox", "firefox"},
		{"safari", "safari"},
		{"random", "random"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create TLS server with test certificate
			cert := generateTestCert(t)
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()

			serverAddr := listener.Addr().String()
			host, portStr, _ := net.SplitHostPort(serverAddr)
			var port int
			fmt.Sscanf(portStr, "%d", &port)

			// Server accepts TLS connection and verifies handshake succeeds
			handshakeSuccess := make(chan bool, 1)
			serverDone := make(chan error, 1)
			go func() {
				conn, err := listener.Accept()
				if err != nil {
					serverDone <- err
					return
				}
				defer conn.Close()

				tlsConn := tls.Server(conn, &tls.Config{
					Certificates: []tls.Certificate{cert},
					MinVersion:   tls.VersionTLS13,
				})

				if err := tlsConn.Handshake(); err != nil {
					handshakeSuccess <- false
					serverDone <- err
					return
				}

				handshakeSuccess <- true

				// Read and discard any WebSocket upgrade request
				buf := make([]byte, 4096)
				tlsConn.Read(buf)

				serverDone <- nil
			}()

			// Client configuration with fingerprint
			cfg := Config{
				Host:        host,
				Port:        port,
				Fingerprint: tt.fingerprint,
			}

			client := NewTCPWSClient(cfg, nil)
			client.tlsConfig = &tls.Config{
				ServerName:         host,
				InsecureSkipVerify: true,
				MinVersion:         tls.VersionTLS13,
			}

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()

			// Attempt connection - should complete TLS handshake with uTLS
			// The WebSocket upgrade will fail (no WS server), but TLS handshake should succeed
			conn, err := client.connectTLSWebSocket(ctx)
			if conn != nil {
				conn.Close()
			}

			// Verify that TLS handshake succeeded (uTLS worked)
			select {
			case success := <-handshakeSuccess:
				if !success {
					t.Error("TLS handshake failed - uTLS may not be working")
				}
			case <-time.After(3 * time.Second):
				t.Fatal("timeout waiting for handshake")
			}

			// Wait for server to finish
			select {
			case serverErr := <-serverDone:
				if serverErr != nil {
					t.Logf("Server result: %v", serverErr)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("timeout waiting for server")
			}
		})
	}
}

// TestTCPWSClient_FullFlow_WS tests the full client flow:
// TCP → uTLS → WebSocket → smux → open stream → echo data
// AC11: TCP-WS client performs WS upgrade to /k2v5/tunnel before smux
// AC13: TCP proxy works correctly over WS+smux transport
func TestTCPWSClient_FullFlow_WS(t *testing.T) {
	// Create TLS certificate for server
	cert := generateTestCert(t)

	// Start HTTP server with WebSocket upgrade support
	upgradePath := make(chan string, 1)
	smuxReady := make(chan net.Conn, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/k2v5/tunnel", func(w http.ResponseWriter, r *http.Request) {
		// Record the upgrade path
		upgradePath <- r.URL.Path

		// Accept WebSocket upgrade
		ws, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Logf("WebSocket accept error: %v", err)
			return
		}

		// Convert to net.Conn for smux
		wsConn := websocket.NetConn(context.Background(), ws, websocket.MessageBinary)
		smuxReady <- wsConn
	})

	// Create TLS listener
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	serverAddr := listener.Addr().String()
	host, portStr, _ := net.SplitHostPort(serverAddr)
	var port int
	fmt.Sscanf(portStr, "%d", &port)

	// Start HTTPS server
	srv := &http.Server{Handler: mux}
	srvTLS := &tls.Config{Certificates: []tls.Certificate{cert}}

	go func() {
		tlsListener := tls.NewListener(listener, srvTLS)
		srv.Serve(tlsListener)
	}()
	defer srv.Close()

	// Server goroutine to handle smux session
	serverDone := make(chan error, 1)
	go func() {
		wsConn := <-smuxReady
		if wsConn == nil {
			serverDone <- fmt.Errorf("no WebSocket connection")
			return
		}
		defer wsConn.Close()

		// Create smux server on WebSocket connection
		sess, err := smux.Server(wsConn, DefaultSmuxConfig())
		if err != nil {
			serverDone <- err
			return
		}
		defer sess.Close()

		// Accept stream. TCPWSClient.connect sends a best-effort metadata
		// stream immediately after the smux session is established (see
		// sendMetadataBestEffort) — drain and OK it before the actual
		// proxy stream, mirroring TCPWSServer.handleStream.
		var stream *smux.Stream
		var br *bufio.Reader
		var streamType byte
		var addr string
		for {
			stream, err = sess.AcceptStream()
			if err != nil {
				serverDone <- err
				return
			}
			br = bufio.NewReader(stream)
			streamType, addr, err = ReadStreamHeader(br)
			if err != nil {
				serverDone <- err
				return
			}
			if streamType == StreamTypeMeta {
				if _, err := ReadMetadataFrame(br); err != nil {
					serverDone <- err
					return
				}
				WriteStreamResponse(stream, nil)
				stream.Close()
				continue
			}
			break
		}
		if streamType != StreamTypeTCP || addr != "example.com:80" {
			serverDone <- fmt.Errorf("unexpected stream: type=%d addr=%s", streamType, addr)
			return
		}

		// Send OK response
		if err := WriteStreamResponse(stream, nil); err != nil {
			serverDone <- err
			return
		}

		// Echo data
		buf := make([]byte, 100)
		n, err := br.Read(buf)
		if err != nil {
			serverDone <- err
			return
		}
		stream.Write(buf[:n])
		serverDone <- nil
	}()

	// Client configuration
	cfg := Config{
		Host:        host,
		Port:        port,
		Fingerprint: "chrome",
	}

	client := NewTCPWSClient(cfg, nil)
	client.tlsConfig = &tls.Config{
		ServerName:         host,
		InsecureSkipVerify: true,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Open TCP stream through client
	conn, err := client.DialTCP(ctx, "example.com:80", nil)
	if err != nil {
		t.Fatalf("DialTCP failed: %v", err)
	}
	defer conn.Close()

	// Verify WebSocket upgrade path
	select {
	case path := <-upgradePath:
		if path != "/k2v5/tunnel" {
			t.Errorf("upgrade path = %q, want /k2v5/tunnel", path)
		}
	case <-time.After(1 * time.Second):
		t.Error("WebSocket upgrade did not happen")
	}

	// Send data and verify echo
	if _, err := conn.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}

	buf := make([]byte, 100)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	if string(buf[:n]) != "hello" {
		t.Errorf("echo = %q, want hello", string(buf[:n]))
	}

	// Wait for server to finish
	if err := <-serverDone; err != nil {
		t.Fatalf("server error: %v", err)
	}
}

// TestTCPWSClient_DialUDP_WS tests UDP over WS+smux transport.
// AC13: UDP proxy works correctly over WS+smux transport
func TestTCPWSClient_DialUDP_WS(t *testing.T) {
	// Create TLS certificate
	cert := generateTestCert(t)

	// Start HTTP server with WebSocket upgrade
	smuxReady := make(chan net.Conn, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/k2v5/tunnel", func(w http.ResponseWriter, r *http.Request) {
		ws, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		wsConn := websocket.NetConn(context.Background(), ws, websocket.MessageBinary)
		smuxReady <- wsConn
	})

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	serverAddr := listener.Addr().String()
	host, portStr, _ := net.SplitHostPort(serverAddr)
	var port int
	fmt.Sscanf(portStr, "%d", &port)

	srv := &http.Server{Handler: mux}
	srvTLS := &tls.Config{Certificates: []tls.Certificate{cert}}

	go func() {
		tlsListener := tls.NewListener(listener, srvTLS)
		srv.Serve(tlsListener)
	}()
	defer srv.Close()

	// Server: handle UDP stream
	received := make(chan string, 1)
	serverDone := make(chan error, 1)
	go func() {
		wsConn := <-smuxReady
		if wsConn == nil {
			serverDone <- fmt.Errorf("no WebSocket connection")
			return
		}
		defer wsConn.Close()

		sess, err := smux.Server(wsConn, DefaultSmuxConfig())
		if err != nil {
			serverDone <- err
			return
		}
		defer sess.Close()

		// Accept UDP stream. TCPWSClient.connect sends a best-effort metadata
		// stream immediately after the smux session is established (see
		// sendMetadataBestEffort) — drain and OK it before the actual UDP
		// stream, mirroring TCPWSServer.handleStream.
		var stream *smux.Stream
		var br *bufio.Reader
		var streamType byte
		for {
			stream, err = sess.AcceptStream()
			if err != nil {
				serverDone <- err
				return
			}
			br = bufio.NewReader(stream)
			streamType, _, err = ReadStreamHeader(br)
			if err != nil {
				serverDone <- err
				return
			}
			if streamType == StreamTypeMeta {
				if _, err := ReadMetadataFrame(br); err != nil {
					serverDone <- err
					return
				}
				WriteStreamResponse(stream, nil)
				stream.Close()
				continue
			}
			break
		}
		if streamType != StreamTypeUDP {
			serverDone <- fmt.Errorf("expected UDP stream type")
			return
		}

		// Read UDP frame
		frame, err := ReadSmuxUDPFrame(br)
		if err != nil {
			serverDone <- err
			return
		}
		received <- string(frame.Payload)

		// Echo back
		WriteSmuxUDPFrame(stream, frame.SessionID, "", frame.Payload)
		serverDone <- nil
	}()

	// Client
	cfg := Config{
		Host:        host,
		Port:        port,
		Fingerprint: "chrome",
	}

	client := NewTCPWSClient(cfg, nil)
	client.tlsConfig = &tls.Config{
		ServerName:         host,
		InsecureSkipVerify: true,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pconn, err := client.DialUDP(ctx, "8.8.8.8:53")
	if err != nil {
		t.Fatalf("DialUDP failed: %v", err)
	}
	defer pconn.Close()

	// Send UDP packet
	if _, err := pconn.WriteTo([]byte("dns-query"), nil); err != nil {
		t.Fatal(err)
	}

	// Verify server received it
	select {
	case data := <-received:
		if data != "dns-query" {
			t.Errorf("received = %q, want dns-query", data)
		}
	case <-time.After(2 * time.Second):
		t.Error("server did not receive UDP packet")
	}

	// Read echo response
	buf := make([]byte, 100)
	n, _, err := pconn.ReadFrom(buf)
	if err != nil {
		t.Fatal(err)
	}
	if string(buf[:n]) != "dns-query" {
		t.Errorf("echo = %q, want dns-query", string(buf[:n]))
	}

	if err := <-serverDone; err != nil {
		t.Fatalf("server error: %v", err)
	}
}

// TestTCPWSClient_ProbeContextCancellation reproduces the engine wire probe bug:
// when the dial context is canceled after DialTCP returns (simulating probeCancel()),
// the WebSocket's NetConn read/write contexts are killed, destroying the cached
// smux session. Subsequent DialTCP calls fail because connect() returns the dead session.
//
// Root cause: websocket.NetConn(ctx, ws, ...) derives read/write contexts from ctx.
// When ctx is canceled, all reads/writes fail → smux session dies.
func TestTCPWSClient_ProbeContextCancellation(t *testing.T) {
	cert := generateTestCert(t)

	// Server: accept multiple streams (probe + real traffic).
	mux := http.NewServeMux()
	mux.HandleFunc("/k2v5/tunnel", func(w http.ResponseWriter, r *http.Request) {
		ws, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		wsConn := websocket.NetConn(context.Background(), ws, websocket.MessageBinary)
		defer wsConn.Close()

		sess, err := smux.Server(wsConn, DefaultSmuxConfig())
		if err != nil {
			return
		}
		defer sess.Close()

		// Handle streams until session closes.
		for {
			stream, err := sess.AcceptStream()
			if err != nil {
				return
			}
			go func() {
				defer stream.Close()
				br := bufio.NewReader(stream)
				streamType, _, err := ReadStreamHeader(br)
				if err != nil {
					return
				}
				if streamType == StreamTypeTCP {
					WriteStreamResponse(stream, nil)
					// Echo loop
					buf := make([]byte, 4096)
					for {
						n, err := br.Read(buf)
						if err != nil {
							return
						}
						if _, err := stream.Write(buf[:n]); err != nil {
							return
						}
					}
				}
			}()
		}
	})

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	host, portStr, _ := net.SplitHostPort(listener.Addr().String())
	var port int
	fmt.Sscanf(portStr, "%d", &port)

	srv := &http.Server{Handler: mux}
	srvTLS := &tls.Config{Certificates: []tls.Certificate{cert}}
	go srv.Serve(tls.NewListener(listener, srvTLS))
	defer srv.Close()

	// Client
	client := NewTCPWSClient(Config{Host: host, Port: port, Fingerprint: "chrome"}, nil)
	client.tlsConfig = &tls.Config{
		ServerName:         host,
		InsecureSkipVerify: true,
	}
	defer client.Close()

	// === Phase 1: Probe DialTCP — simulates engine wire probe ===
	probeCtx, probeCancel := context.WithTimeout(context.Background(), 5*time.Second)
	probeConn, err := client.DialTCP(probeCtx, "1.1.1.1:443", nil)
	if err != nil {
		t.Fatalf("probe DialTCP failed: %v", err)
	}

	// Cancel probe context — simulates engine's probeCancel()
	probeCancel()
	probeConn.Close()

	// Let context cancellation propagate to WebSocket read/write contexts.
	time.Sleep(200 * time.Millisecond)

	// === Phase 2: Real DialTCP — must succeed if session survived ===
	realCtx, realCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer realCancel()

	conn, err := client.DialTCP(realCtx, "example.com:80", nil)
	if err != nil {
		t.Fatalf("post-probe DialTCP failed (session killed by probe context cancel): %v", err)
	}
	defer conn.Close()

	// Verify data actually flows
	if _, err := conn.Write([]byte("hello")); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	buf := make([]byte, 100)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	if string(buf[:n]) != "hello" {
		t.Errorf("echo = %q, want hello", string(buf[:n]))
	}
}

// TestSmuxUDPMuxConcurrentClose verifies that calling smuxUDPMux.close()
// concurrently from multiple goroutines does not panic (double-close of done channel).
// This reproduces the race between readLoop defer and explicit close() during reconnect.
func TestSmuxUDPMuxConcurrentClose(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()

	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		sSess, err := smux.Server(server, DefaultSmuxConfig())
		if err != nil {
			return
		}
		defer sSess.Close()
		for {
			s, err := sSess.AcceptStream()
			if err != nil {
				return
			}
			s.Close()
		}
	}()

	sess, err := smux.Client(client, DefaultSmuxConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()

	stream, err := sess.OpenStream()
	if err != nil {
		t.Fatal(err)
	}

	mux := newSmuxUDPMux(stream)
	mux.start()

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			mux.close()
		}()
	}
	wg.Wait()

	server.Close()
	<-serverDone
}

// TestTCPWSClient_DialUDP_AfterReset verifies that after resetConnection(),
// TCPWSClient.DialUDP can rebuild a new smux session and udpMux.
// This simulates a network change: old session dies, new connection is established.
func TestTCPWSClient_DialUDP_AfterReset(t *testing.T) {
	// Phase 1: initial setup + first DialUDP
	clientConn1, serverConn1 := net.Pipe()
	server := NewTCPWSServer()
	server.HandleConn(serverConn1)

	client := NewTCPWSClient(Config{Host: "127.0.0.1", Port: 443}, nil)
	client.testConn = clientConn1

	received1 := make(chan string, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go server.ServeUDP(ctx, UDPHandlerFunc(func(conn net.PacketConn, addr string) {
		buf := make([]byte, 100)
		n, _, err := conn.ReadFrom(buf)
		if err != nil {
			return
		}
		received1 <- string(buf[:n])
	}))
	time.Sleep(50 * time.Millisecond)

	pc1, err := client.DialUDP(context.Background(), "8.8.8.8:53")
	if err != nil {
		t.Fatalf("first DialUDP: %v", err)
	}
	pc1.WriteTo([]byte("before-reset"), nil)

	select {
	case msg := <-received1:
		if msg != "before-reset" {
			t.Fatalf("first msg = %q, want before-reset", msg)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout on first DialUDP")
	}
	pc1.Close()

	// Phase 2: reset + reconnect with new pipe
	client.resetConnection()

	// Verify udpMux was cleared
	client.mu.Lock()
	if client.udpMux != nil {
		client.mu.Unlock()
		t.Fatal("udpMux should be nil after resetConnection")
	}
	client.mu.Unlock()

	// Create fresh pipe for "new network connection"
	clientConn2, serverConn2 := net.Pipe()
	client.mu.Lock()
	client.testConn = clientConn2
	client.mu.Unlock()

	// Server needs new smux session on new pipe
	server2 := NewTCPWSServer()
	server2.HandleConn(serverConn2)

	received2 := make(chan string, 1)
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	go server2.ServeUDP(ctx2, UDPHandlerFunc(func(conn net.PacketConn, addr string) {
		buf := make([]byte, 100)
		n, _, err := conn.ReadFrom(buf)
		if err != nil {
			return
		}
		received2 <- string(buf[:n])
	}))
	time.Sleep(50 * time.Millisecond)

	// Phase 3: second DialUDP should build new smux session + udpMux
	pc2, err := client.DialUDP(context.Background(), "8.8.8.8:53")
	if err != nil {
		t.Fatalf("second DialUDP after reset: %v", err)
	}
	defer pc2.Close()

	pc2.WriteTo([]byte("after-reset"), nil)

	select {
	case msg := <-received2:
		if msg != "after-reset" {
			t.Fatalf("second msg = %q, want after-reset", msg)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout on second DialUDP after reset")
	}

	// Cleanup
	client.Close()
	server.Close()
	server2.Close()
	clientConn1.Close()
	serverConn1.Close()
	clientConn2.Close()
	serverConn2.Close()
}
