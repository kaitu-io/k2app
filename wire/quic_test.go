package wire

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"math/big"
	"net"
	"testing"
	"time"

	"github.com/apernet/quic-go"
)

func TestDefaultQUICConfig(t *testing.T) {
	cfg := DefaultQUICConfig()

	if !cfg.EnableDatagrams {
		t.Error("datagrams should be enabled")
	}
	if cfg.MaxIdleTimeout != 30*time.Second {
		t.Errorf("MaxIdleTimeout = %v, want 30s", cfg.MaxIdleTimeout)
	}
	if cfg.KeepAlivePeriod != 10*time.Second {
		t.Errorf("KeepAlivePeriod = %v, want 10s", cfg.KeepAlivePeriod)
	}
	if cfg.MaxIncomingStreams != 256 {
		t.Errorf("MaxIncomingStreams = %d, want 256", cfg.MaxIncomingStreams)
	}
}

func TestQUICStreamConn(t *testing.T) {
	// quicStreamConn should satisfy net.Conn at compile time
	var _ interface {
		Read([]byte) (int, error)
		Write([]byte) (int, error)
		Close() error
	} = (*quicStreamConn)(nil)
}

func TestQUICClientConfig(t *testing.T) {
	cfg := Config{
		Host:        "1.2.3.4",
		Port:        443,
		ECH:         "dGVzdA==", // "test" in base64
		Fingerprint: "chrome",
		HopStart:    10000,
		HopEnd:      20000,
	}

	qc := NewQUICClient(cfg, nil)
	if qc == nil {
		t.Fatal("NewQUICClient returned nil")
	}
	if qc.serverAddr != "1.2.3.4:443" {
		t.Errorf("serverAddr = %q, want %q", qc.serverAddr, "1.2.3.4:443")
	}
	if !qc.hopEnabled() {
		t.Error("port hopping should be enabled")
	}
}

func TestQUICClientConfig_NoHop(t *testing.T) {
	cfg := Config{
		Host:        "1.2.3.4",
		Port:        443,
		ECH:         "dGVzdA==",
		Fingerprint: "chrome",
	}

	qc := NewQUICClient(cfg, nil)
	if qc.hopEnabled() {
		t.Error("port hopping should be disabled when hop range not set")
	}
}

// Verify quic.Config is properly built
func TestQUICConfigType(t *testing.T) {
	cfg := DefaultQUICConfig()
	// Verify it's the right type for quic-go
	_ = &quic.Config{
		EnableDatagrams:   cfg.EnableDatagrams,
		MaxIdleTimeout:    cfg.MaxIdleTimeout,
		KeepAlivePeriod:   cfg.KeepAlivePeriod,
		MaxIncomingStreams: cfg.MaxIncomingStreams,
	}
}

func TestQUICServer_Create(t *testing.T) {
	store := NewTicketStore()
	qs := NewQUICServer(store)
	if qs == nil {
		t.Fatal("NewQUICServer returned nil")
	}
}

func TestQUICClient_HopPort(t *testing.T) {
	qc := NewQUICClient(Config{
		Host:     "1.2.3.4",
		Port:     443,
		HopStart: 10000,
		HopEnd:   20000,
	}, nil)

	seen := make(map[int]bool)
	for i := 0; i < 100; i++ {
		port := qc.hopPort()
		if port < 10000 || port >= 20000 {
			t.Fatalf("hopPort() = %d, out of range [10000, 20000)", port)
		}
		seen[port] = true
	}
	if len(seen) < 5 {
		t.Errorf("hopPort() lacks randomness, only %d distinct ports in 100 calls", len(seen))
	}
}

// generateTestCert creates a self-signed TLS certificate for testing.
func generateTestCert(t *testing.T) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(time.Hour),
		DNSNames:     []string{"localhost"},
	}
	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{
		Certificate: [][]byte{certDER},
		PrivateKey:  key,
	}
}

// setupQUICPair creates a connected QUICClient + QUICServer pair for testing.
func setupQUICPair(t *testing.T) (*QUICClient, *QUICServer, func()) {
	t.Helper()
	cert := generateTestCert(t)

	// Server QUIC listener
	serverUDP, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	serverTransport := &quic.Transport{Conn: serverUDP}
	serverTLS := &tls.Config{
		Certificates: []tls.Certificate{cert},
		NextProtos:   []string{"h3"},
	}
	ln, err := serverTransport.Listen(serverTLS, DefaultQUICConfig())
	if err != nil {
		t.Fatal(err)
	}

	store := NewTicketStore()
	server := NewQUICServer(store)
	server.Listen(ln)

	// Client
	serverAddr := serverUDP.LocalAddr().(*net.UDPAddr)
	clientCfg := Config{
		Host: "127.0.0.1",
		Port: serverAddr.Port,
	}
	client := NewQUICClient(clientCfg, store)
	client.tlsConfig = &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{"h3"},
	}

	cleanup := func() {
		client.Close()
		server.Close()
		serverTransport.Close()
	}

	return client, server, cleanup
}

func TestQUICClient_DialTCP(t *testing.T) {
	client, server, cleanup := setupQUICPair(t)
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

		// Write OK response
		if err := WriteStreamResponse(conn, nil); err != nil {
			serverDone <- err
			return
		}

		// Echo data
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

	// Send data
	if _, err := conn.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}

	// Read echo
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

func TestQUICClient_DialUDP(t *testing.T) {
	client, server, cleanup := setupQUICPair(t)
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
		// Send response
		conn.WriteTo([]byte("response"), nil)
	}))

	// Small delay for handler registration
	time.Sleep(50 * time.Millisecond)

	// Client: dial UDP
	pconn, err := client.DialUDP(context.Background(), "8.8.8.8:53")
	if err != nil {
		t.Fatal(err)
	}
	defer pconn.Close()

	// Send data
	if _, err := pconn.WriteTo([]byte("dns-query"), nil); err != nil {
		t.Fatal(err)
	}

	// Verify server received
	select {
	case data := <-received:
		if data != "dns-query" {
			t.Errorf("received = %q, want dns-query", data)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for server to receive")
	}

	// Read response
	buf := make([]byte, 100)
	n, _, err := pconn.ReadFrom(buf)
	if err != nil {
		t.Fatal(err)
	}
	if string(buf[:n]) != "response" {
		t.Errorf("response = %q, want response", string(buf[:n]))
	}
}

func TestQUICServer_AcceptTCP_MultipleStreams(t *testing.T) {
	client, server, cleanup := setupQUICPair(t)
	defer cleanup()

	// Server: accept 2 streams
	type accepted struct {
		addr string
		err  error
	}
	results := make(chan accepted, 2)
	go func() {
		for i := 0; i < 2; i++ {
			conn, addr, err := server.AcceptTCP(context.Background())
			if err != nil {
				results <- accepted{err: err}
				continue
			}
			WriteStreamResponse(conn, nil)
			results <- accepted{addr: addr}
		}
	}()

	// Client: open 2 TCP connections
	for _, addr := range []string{"a.com:80", "b.com:443"} {
		conn, err := client.DialTCP(context.Background(), addr)
		if err != nil {
			t.Fatalf("DialTCP(%s): %v", addr, err)
		}
		conn.Close()
	}

	// Verify both accepted
	addrs := make(map[string]bool)
	for i := 0; i < 2; i++ {
		select {
		case r := <-results:
			if r.err != nil {
				t.Fatalf("AcceptTCP: %v", r.err)
			}
			addrs[r.addr] = true
		case <-time.After(5 * time.Second):
			t.Fatal("timeout waiting for AcceptTCP")
		}
	}
	if !addrs["a.com:80"] || !addrs["b.com:443"] {
		t.Errorf("accepted addrs = %v, want a.com:80 and b.com:443", addrs)
	}
}

func TestQUICClient_InterfaceCompliance(t *testing.T) {
	var _ Dialer = (*QUICClient)(nil)
	var _ Listener = (*QUICServer)(nil)
}

// ============================================================================
// T4: QUIC UDP Stream Fallback Tests
// ============================================================================

// TestQUICServer_HandleStream_UDP tests server handling of StreamTypeUDP (AC22).
func TestQUICServer_HandleStream_UDP(t *testing.T) {
	client, server, cleanup := setupQUICPair(t)
	defer cleanup()

	// Server: set up UDP handler
	received := make(chan struct {
		addr    string
		payload string
	}, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go server.ServeUDP(ctx, UDPHandlerFunc(func(conn net.PacketConn, addr string) {
		buf := make([]byte, 100)
		n, _, err := conn.ReadFrom(buf)
		if err != nil {
			return
		}
		received <- struct {
			addr    string
			payload string
		}{addr: addr, payload: string(buf[:n])}
		// Send response
		conn.WriteTo([]byte("pong"), nil)
	}))

	// Small delay for handler registration
	time.Sleep(50 * time.Millisecond)

	// Client: manually open QUIC stream and send StreamTypeUDP header + smux-framed UDP
	qconn, err := client.connect(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	stream, err := qconn.OpenStreamSync(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()

	// Write H3 frame type + UDP stream header
	if err := WriteH3ProxyFrameType(stream); err != nil {
		t.Fatal(err)
	}
	if err := WriteStreamHeader(stream, StreamTypeUDP, ""); err != nil {
		t.Fatal(err)
	}

	// Write smux-framed UDP frame
	if err := WriteSmuxUDPFrame(stream, 1, "1.1.1.1:53", []byte("ping")); err != nil {
		t.Fatal(err)
	}

	// Verify server received
	select {
	case data := <-received:
		if data.addr != "1.1.1.1:53" {
			t.Errorf("addr = %q, want 1.1.1.1:53", data.addr)
		}
		if data.payload != "ping" {
			t.Errorf("payload = %q, want ping", data.payload)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for server to receive")
	}

	// Read response
	br := bufio.NewReader(stream)
	frame, err := ReadSmuxUDPFrame(br)
	if err != nil {
		t.Fatal(err)
	}
	if string(frame.Payload) != "pong" {
		t.Errorf("response = %q, want pong", string(frame.Payload))
	}
}

// TestQUICClient_DialUDP_StreamFallback tests stream-based UDP when datagrams disabled (AC21).
func TestQUICClient_DialUDP_StreamFallback(t *testing.T) {
	cert := generateTestCert(t)

	// Server with datagrams DISABLED
	serverUDP, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer serverUDP.Close()

	serverTransport := &quic.Transport{Conn: serverUDP}
	defer serverTransport.Close()

	serverTLS := &tls.Config{
		Certificates: []tls.Certificate{cert},
		NextProtos:   []string{"h3"},
	}
	quicCfg := DefaultQUICConfig()
	quicCfg.EnableDatagrams = false // Force stream fallback
	ln, err := serverTransport.Listen(serverTLS, quicCfg)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	store := NewTicketStore()
	server := NewQUICServer(store)
	server.Listen(ln)
	defer server.Close()

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
		// Send response
		conn.WriteTo([]byte("echo"), nil)
	}))

	time.Sleep(50 * time.Millisecond)

	// Client
	serverAddr := serverUDP.LocalAddr().(*net.UDPAddr)
	clientCfg := Config{
		Host: "127.0.0.1",
		Port: serverAddr.Port,
	}
	client := NewQUICClient(clientCfg, store)
	defer client.Close()
	client.tlsConfig = &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{"h3"},
	}

	// Client: dial UDP (should use stream fallback)
	pconn, err := client.DialUDP(context.Background(), "8.8.8.8:53")
	if err != nil {
		t.Fatal(err)
	}
	defer pconn.Close()

	// Send data
	if _, err := pconn.WriteTo([]byte("query"), nil); err != nil {
		t.Fatal(err)
	}

	// Verify server received
	select {
	case data := <-received:
		if data != "query" {
			t.Errorf("received = %q, want query", data)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for server to receive")
	}

	// Read response
	buf := make([]byte, 100)
	n, _, err := pconn.ReadFrom(buf)
	if err != nil {
		t.Fatal(err)
	}
	if string(buf[:n]) != "echo" {
		t.Errorf("response = %q, want echo", string(buf[:n]))
	}
}

// ============================================================================
// Existing loopback tests (raw QUIC, no QUICClient/QUICServer)
// ============================================================================

func TestQUICLoopback_ProxyStream(t *testing.T) {
	cert := generateTestCert(t)

	// Server-side QUIC listener
	serverUDP, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer serverUDP.Close()

	serverTransport := &quic.Transport{Conn: serverUDP}
	defer serverTransport.Close()

	serverTLS := &tls.Config{
		Certificates: []tls.Certificate{cert},
		NextProtos:   []string{"k2v5"},
	}
	ln, err := serverTransport.Listen(serverTLS, DefaultQUICConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	serverAddr := serverUDP.LocalAddr().(*net.UDPAddr)

	// Server goroutine
	serverDone := make(chan error, 1)
	go func() {
		conn, err := ln.Accept(context.Background())
		if err != nil {
			serverDone <- err
			return
		}
		stream, err := conn.AcceptStream(context.Background())
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
			serverDone <- fmt.Errorf("unexpected header: type=%d addr=%q", typ, addr)
			return
		}

		WriteStreamResponse(stream, nil)

		// Echo data back
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

	// Client-side QUIC connection
	clientUDP, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer clientUDP.Close()

	clientTransport := &quic.Transport{Conn: clientUDP}
	defer clientTransport.Close()

	clientTLS := &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{"k2v5"},
	}
	conn, err := clientTransport.Dial(context.Background(), serverAddr, clientTLS, DefaultQUICConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseWithError(0, "")

	stream, err := conn.OpenStreamSync(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	// Write proxy header + data
	if err := WriteStreamHeader(stream, StreamTypeTCP, "8.8.8.8:53"); err != nil {
		t.Fatal(err)
	}

	// Read response
	br := bufio.NewReader(stream)
	ok, _, errMsg, err := ReadStreamResponse(br)
	if err != nil {
		t.Fatalf("ReadStreamResponse: %v", err)
	}
	if !ok {
		t.Fatalf("server rejected: %s", errMsg)
	}

	// Send data
	if _, err := stream.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}

	// Read echo
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

func TestQUICLoopback_Datagram(t *testing.T) {
	cert := generateTestCert(t)

	// Server
	serverUDP, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer serverUDP.Close()

	serverTransport := &quic.Transport{Conn: serverUDP}
	defer serverTransport.Close()

	serverTLS := &tls.Config{
		Certificates: []tls.Certificate{cert},
		NextProtos:   []string{"k2v5"},
	}
	ln, err := serverTransport.Listen(serverTLS, DefaultQUICConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	serverAddr := serverUDP.LocalAddr().(*net.UDPAddr)

	// Server: receive datagram and echo back
	serverDone := make(chan error, 1)
	go func() {
		conn, err := ln.Accept(context.Background())
		if err != nil {
			serverDone <- err
			return
		}
		data, err := conn.ReceiveDatagram(context.Background())
		if err != nil {
			serverDone <- err
			return
		}
		err = conn.SendDatagram(data) // echo
		serverDone <- err
	}()

	// Client
	clientUDP, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer clientUDP.Close()

	clientTransport := &quic.Transport{Conn: clientUDP}
	defer clientTransport.Close()

	clientTLS := &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{"k2v5"},
	}
	conn, err := clientTransport.Dial(context.Background(), serverAddr, clientTLS, DefaultQUICConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseWithError(0, "")

	// Send datagram with UDP frame
	var frameBuf [64]byte
	copy(frameBuf[:], "test-datagram")
	if err := conn.SendDatagram(frameBuf[:13]); err != nil {
		t.Fatal(err)
	}

	// Receive echo
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	data, err := conn.ReceiveDatagram(ctx)
	if err != nil {
		t.Fatalf("receive datagram: %v", err)
	}
	if string(data) != "test-datagram" {
		t.Errorf("datagram = %q, want %q", string(data), "test-datagram")
	}

	if err := <-serverDone; err != nil {
		t.Fatalf("server error: %v", err)
	}
}

// TestQUICClient_DialTCP_ResetsOnStreamError verifies that DialTCP resets
// the connection when OpenStreamSync fails with a non-context error (e.g.,
// connection closed). This prevents the dead-connection retry loop where
// TransportManager backoff retries keep hitting the same dead conn.
func TestQUICClient_DialTCP_ResetsOnStreamError(t *testing.T) {
	client, server, cleanup := setupQUICPair(t)
	defer cleanup()

	// Establish a connection first.
	ctx := context.Background()
	serverDone := make(chan error, 1)
	go func() {
		conn, _, err := server.AcceptTCP(ctx)
		if err != nil {
			serverDone <- err
			return
		}
		WriteStreamResponse(conn, nil)
		conn.Close()
		serverDone <- nil
	}()

	conn, err := client.DialTCP(ctx, "example.com:80")
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()
	<-serverDone

	// Verify client has a cached connection.
	client.mu.Lock()
	hasConn := client.conn != nil
	client.mu.Unlock()
	if !hasConn {
		t.Fatal("expected cached connection before server close")
	}

	// Simulate a dead connection: close the underlying QUIC connection directly.
	// The client still has c.conn != nil (connect() will return it), but
	// OpenStreamSync will fail immediately on the closed connection.
	// This mirrors production: ISP kills the path, conn object is stale.
	client.mu.Lock()
	client.conn.CloseWithError(0, "test: simulate dead connection")
	client.mu.Unlock()

	// DialTCP should fail AND reset the dead connection.
	_, err = client.DialTCP(ctx, "example.com:80")
	if err == nil {
		t.Fatal("expected error from DialTCP on dead connection")
	}

	// The connection should have been reset (nil'd).
	client.mu.Lock()
	connAfter := client.conn
	client.mu.Unlock()
	if connAfter != nil {
		t.Fatal("expected conn to be nil after DialTCP reset")
	}
}

// TestQUICClient_DialTCP_NoResetOnContextTimeout verifies that DialTCP
// does NOT reset the connection when the failure is due to context timeout.
// The connection might still be healthy — just slow.
func TestQUICClient_DialTCP_NoResetOnContextTimeout(t *testing.T) {
	client, _, cleanup := setupQUICPair(t)
	defer cleanup()

	// Establish connection.
	ctx := context.Background()
	_, err := client.connect(ctx)
	if err != nil {
		t.Fatal(err)
	}

	// Use an already-expired context so OpenStreamSync fails with DeadlineExceeded.
	expiredCtx, cancel := context.WithTimeout(ctx, 0)
	cancel()

	_, err = client.DialTCP(expiredCtx, "example.com:80")
	if err == nil {
		t.Fatal("expected error from DialTCP with expired context")
	}

	// Connection should NOT be reset — context error, not connection error.
	client.mu.Lock()
	connAfter := client.conn
	client.mu.Unlock()
	if connAfter == nil {
		t.Fatal("conn should NOT be nil after context timeout")
	}
}

// TestQUICClient_dialUDPStream_ResetsOnStreamError verifies that dialUDPStream
// resets the connection when OpenStreamSync fails with a non-context error,
// matching the DialTCP behavior. This is a defensive fix for the datagram-
// disabled fallback path.
func TestQUICClient_dialUDPStream_ResetsOnStreamError(t *testing.T) {
	client, _, cleanup := setupQUICPair(t)
	defer cleanup()

	// Establish connection (sets c.conn, c.udpMux, c.alloc).
	ctx := context.Background()
	conn, err := client.connect(ctx)
	if err != nil {
		t.Fatal(err)
	}

	// Verify client has a cached connection.
	client.mu.Lock()
	hasConn := client.conn != nil
	client.mu.Unlock()
	if !hasConn {
		t.Fatal("expected cached connection after connect")
	}

	// Kill the QUIC connection so OpenStreamSync will fail.
	conn.CloseWithError(0, "test: simulate dead connection")

	// dialUDPStream should fail AND reset the dead connection.
	_, err = client.dialUDPStream(ctx, conn, "example.com:80")
	if err == nil {
		t.Fatal("expected error from dialUDPStream on dead connection")
	}

	// The connection should have been reset (nil'd).
	client.mu.Lock()
	connAfter := client.conn
	client.mu.Unlock()
	if connAfter != nil {
		t.Fatal("expected conn to be nil after dialUDPStream reset")
	}
}

func TestQUICClient_ConsecConnFails(t *testing.T) {
	// Test that connect() failures increment consecConnFails and success resets it.
	qc := NewQUICClient(Config{Host: "127.0.0.1", Port: 1}, nil)
	qc.SetTLSConfig(&tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{"h3"},
	})

	// connect() to unreachable port should fail (timeout) and increment counter.
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	_, err := qc.connect(ctx)
	cancel()
	if err == nil {
		t.Fatal("expected connect to fail")
	}
	if got := qc.ConsecConnFails(); got != 1 {
		t.Fatalf("consecConnFails = %d after 1 failure, want 1", got)
	}

	// Second failure increments again.
	ctx2, cancel2 := context.WithTimeout(context.Background(), 500*time.Millisecond)
	_, err = qc.connect(ctx2)
	cancel2()
	if err == nil {
		t.Fatal("expected connect to fail")
	}
	if got := qc.ConsecConnFails(); got != 2 {
		t.Fatalf("consecConnFails = %d after 2 failures, want 2", got)
	}

	// Manual reset (simulating success in a real scenario).
	qc.consecConnFails.Store(0)
	if got := qc.ConsecConnFails(); got != 0 {
		t.Fatalf("consecConnFails = %d after reset, want 0", got)
	}
}
