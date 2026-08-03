package test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"math/big"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/apernet/quic-go"
	"github.com/kaitu-io/k2/provider"
	"github.com/kaitu-io/k2/server"
	"github.com/kaitu-io/k2/wire"
)

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

// testEnv holds the full E2E test environment.
type testEnv struct {
	t *testing.T

	// Server side
	quicSrv  *wire.QUICServer
	tcpwsSrv *wire.TCPWSServer
	handler  *server.ProxyHandler

	// Client transports
	quicClient  *wire.QUICClient
	tcpwsClient *wire.TCPWSClient

	// TCP-WS server listener (for feeding connections to tcpwsSrv)
	tcpwsLn net.Listener

	// Network addresses
	quicAddr  string
	tcpwsAddr string

	// Cleanup
	cancel  context.CancelFunc
	cleanup []func()
	wg      sync.WaitGroup
}

// setupQUICEnv creates a QUIC-based E2E test environment.
// Returns the test env with server accepting QUIC connections and client ready to dial.
func setupQUICEnv(t *testing.T) *testEnv {
	t.Helper()
	return setupQUICEnvUDP(t, nil)
}

// setupQUICEnvUDP is setupQUICEnv with an overridable server-side UDP handler.
// Passing nil keeps the production server.ProxyHandler, which is what every
// ordinary E2E test wants. Tests that need the server to behave like a
// *different* implementation — e.g. a pre-multi-dest server that replies with
// an empty frame addr — supply their own handler here rather than standing up
// a second, divergent QUIC fixture.
func setupQUICEnvUDP(t *testing.T, udp wire.UDPHandler) *testEnv {
	t.Helper()
	cert := generateTestCert(t)
	ctx, cancel := context.WithCancel(context.Background())

	env := &testEnv{
		t:      t,
		cancel: cancel,
	}

	// QUIC server
	serverUDP, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	env.cleanup = append(env.cleanup, func() { serverUDP.Close() })

	serverTransport := &quic.Transport{Conn: serverUDP}
	env.cleanup = append(env.cleanup, func() { serverTransport.Close() })

	serverTLS := &tls.Config{
		Certificates: []tls.Certificate{cert},
		NextProtos:   []string{"h3"},
	}
	ln, err := serverTransport.Listen(serverTLS, wire.DefaultQUICConfig())
	if err != nil {
		t.Fatal(err)
	}
	env.cleanup = append(env.cleanup, func() { ln.Close() })

	env.quicSrv = wire.NewQUICServer()
	env.quicSrv.Listen(ln)
	env.handler = server.NewProxyHandler()
	env.handler.AllowPrivateIP = true // echo servers run on localhost

	env.quicAddr = serverUDP.LocalAddr().String()

	// Start server accept loops
	env.wg.Add(1)
	go func() {
		defer env.wg.Done()
		for {
			conn, addr, err := env.quicSrv.AcceptTCP(ctx)
			if err != nil {
				return
			}
			go env.handler.HandleTCP(conn, addr)
		}
	}()

	if udp == nil {
		udp = wire.UDPHandlerFunc(env.handler.HandleUDP)
	}
	env.wg.Add(1)
	go func() {
		defer env.wg.Done()
		env.quicSrv.ServeUDP(ctx, udp)
	}()

	// QUIC client
	serverAddr := serverUDP.LocalAddr().(*net.UDPAddr)
	clientCfg := wire.Config{
		Host: "127.0.0.1",
		Port: serverAddr.Port,
	}
	env.quicClient = wire.NewQUICClient(clientCfg, nil)
	// Bypass ECH/fingerprint for testing
	env.quicClient.SetTLSConfig(&tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{"h3"},
	})

	return env
}

// setupTCPWSEnv creates a TCP-WS based E2E test environment.
func setupTCPWSEnv(t *testing.T) *testEnv {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())

	env := &testEnv{
		t:      t,
		cancel: cancel,
	}

	// TCP listener for smux connections
	tcpLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	env.tcpwsLn = tcpLn
	env.cleanup = append(env.cleanup, func() { tcpLn.Close() })
	env.tcpwsAddr = tcpLn.Addr().String()

	env.tcpwsSrv = wire.NewTCPWSServer()
	env.handler = server.NewProxyHandler()
	env.handler.AllowPrivateIP = true // echo servers run on localhost

	// Accept TCP connections and feed to TCP-WS server
	env.wg.Add(1)
	go func() {
		defer env.wg.Done()
		for {
			conn, err := tcpLn.Accept()
			if err != nil {
				return
			}
			env.tcpwsSrv.HandleConn(conn)
		}
	}()

	// Start server accept loops
	env.wg.Add(1)
	go func() {
		defer env.wg.Done()
		for {
			conn, addr, err := env.tcpwsSrv.AcceptTCP(ctx)
			if err != nil {
				return
			}
			go env.handler.HandleTCP(conn, addr)
		}
	}()

	env.wg.Add(1)
	go func() {
		defer env.wg.Done()
		env.tcpwsSrv.ServeUDP(ctx, wire.UDPHandlerFunc(env.handler.HandleUDP))
	}()

	// TCP-WS client (bypass TLS for testing)
	clientCfg := wire.Config{Host: "127.0.0.1", Port: 0}
	env.tcpwsClient = wire.NewTCPWSClient(clientCfg, nil)
	// Connect directly via net.Pipe to avoid TLS for unit test
	clientConn, serverConn := net.Pipe()
	env.tcpwsSrv.HandleConn(serverConn)
	env.tcpwsClient.SetTestConn(clientConn)

	return env
}

func (e *testEnv) Close() {
	e.cancel()
	if e.quicSrv != nil {
		e.quicSrv.Close()
	}
	if e.tcpwsSrv != nil {
		e.tcpwsSrv.Close()
	}
	if e.quicClient != nil {
		e.quicClient.Close()
	}
	if e.tcpwsClient != nil {
		e.tcpwsClient.Close()
	}
	for i := len(e.cleanup) - 1; i >= 0; i-- {
		e.cleanup[i]()
	}
	e.wg.Wait()
}

// startTCPEchoServer starts a TCP echo server and returns its address.
func startTCPEchoServer(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				buf := make([]byte, 4096)
				for {
					n, err := conn.Read(buf)
					if err != nil {
						return
					}
					if _, err := conn.Write(buf[:n]); err != nil {
						return
					}
				}
			}()
		}
	}()

	return ln.Addr().String()
}

// startUDPEchoServer starts a UDP echo server and returns its address.
func startUDPEchoServer(t *testing.T) string {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pc.Close() })

	go func() {
		buf := make([]byte, 4096)
		for {
			n, addr, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			pc.WriteTo(buf[:n], addr)
		}
	}()

	return pc.LocalAddr().String()
}

// mockProvider is a test provider that directly injects connections into the handler.
type mockProvider struct {
	handler provider.ConnectionHandler
	mu      sync.Mutex
}

func (p *mockProvider) Start(_ context.Context, handler provider.ConnectionHandler) error {
	p.mu.Lock()
	p.handler = handler
	p.mu.Unlock()
	return nil
}

func (p *mockProvider) Close() error { return nil }
func (p *mockProvider) Name() string { return "mock" }

func (p *mockProvider) InjectTCP(conn net.Conn, dest string) {
	p.mu.Lock()
	h := p.handler
	p.mu.Unlock()
	if h != nil {
		h.HandleTCP(conn, dest)
	}
}

func (p *mockProvider) InjectUDP(conn net.PacketConn, dest string) {
	p.mu.Lock()
	h := p.handler
	p.mu.Unlock()
	if h != nil {
		h.HandleUDP(conn, dest)
	}
}
