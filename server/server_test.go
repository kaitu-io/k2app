package server

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"io"
	"math/big"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/kaitu-io/k2/config"
	"github.com/kaitu-io/k2/wire"
	smux "github.com/xtaci/smux/v2"
)

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

func TestNewServer(t *testing.T) {
	cfg := &config.ServerConfig{
		Listen: ":0",
	}

	s, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if s.handler == nil {
		t.Error("handler should not be nil")
	}
	if s.quicSrv == nil {
		t.Error("quicSrv should not be nil")
	}
	if s.tcpwsSrv == nil {
		t.Error("tcpwsSrv should not be nil")
	}
}

func TestServer_TCPWSProxy(t *testing.T) {
	cert := generateTestCert(t)

	// Start a target TCP echo server.
	echoLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer echoLn.Close()

	go func() {
		for {
			conn, err := echoLn.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				io.Copy(conn, conn)
			}()
		}
	}()

	// Create server.
	cfg := &config.ServerConfig{Listen: "127.0.0.1:0"}
	srv, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	srv.TLSConfig = &tls.Config{
		Certificates: []tls.Certificate{cert},
		NextProtos:   []string{"h3"},
		MinVersion:   tls.VersionTLS13,
	}

	// Start TCP listener for the server.
	tcpLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer tcpLn.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Run TCP-WS accept loop.
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		srv.acceptTCPLoop(ctx, srv.tcpwsSrv)
	}()

	// Accept one raw TCP connection as if it were already TLS-terminated.
	wg.Add(1)
	go func() {
		defer wg.Done()
		conn, err := tcpLn.Accept()
		if err != nil {
			return
		}
		// Simulate TLS-terminated connection: feed directly to TCP-WS server.
		srv.tcpwsSrv.HandleConn(conn)
	}()

	// Client: connect and create smux session.
	clientConn, err := net.Dial("tcp", tcpLn.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer clientConn.Close()

	sess, err := smux.Client(clientConn, wire.DefaultSmuxConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()

	// Open TCP proxy stream.
	stream, err := sess.OpenStream()
	if err != nil {
		t.Fatal(err)
	}

	if err := wire.WriteStreamHeader(stream, wire.StreamTypeTCP, echoLn.Addr().String()); err != nil {
		t.Fatal(err)
	}

	br := bufio.NewReader(stream)
	ok, errMsg, err := wire.ReadStreamResponse(br)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatalf("expected success, got: %s", errMsg)
	}

	// Send data, read echo.
	if _, err := stream.Write([]byte("hello server")); err != nil {
		t.Fatal(err)
	}

	buf := make([]byte, 12)
	if _, err := io.ReadFull(br, buf); err != nil {
		t.Fatal(err)
	}
	if string(buf) != "hello server" {
		t.Fatalf("expected 'hello server', got %q", buf)
	}

	stream.Close()
	cancel()
	wg.Wait()
}

func TestServer_IsHTTPMethod(t *testing.T) {
	tests := []struct {
		input []byte
		want  bool
	}{
		{[]byte("GET "), true},
		{[]byte("POST"), true},
		{[]byte("PUT "), true},
		{[]byte("DELE"), true},
		{[]byte("HEAD"), true},
		{[]byte("OPTI"), true},
		{[]byte("PATC"), true},
		{[]byte{0x02, 0x00, 0x00, 0x00}, false}, // smux frame
		{[]byte{0x16, 0x03}, false},               // TLS
		{[]byte("ab"), false},                      // too short
	}

	for _, tc := range tests {
		got := isHTTPMethod(tc.input)
		if got != tc.want {
			t.Errorf("isHTTPMethod(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}
