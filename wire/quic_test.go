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
		SNI:         "www.microsoft.com",
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
		SNI:         "www.microsoft.com",
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
		EnableDatagrams:    cfg.EnableDatagrams,
		MaxIdleTimeout:     cfg.MaxIdleTimeout,
		KeepAlivePeriod:    cfg.KeepAlivePeriod,
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
	ok, errMsg, err := ReadStreamResponse(br)
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
