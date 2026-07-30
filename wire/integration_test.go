package wire

import (
	"bytes"
	"context"
	"crypto/tls"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/apernet/quic-go"
)

// ============================================================================
// Integration: TransportManager with mock transports
// ============================================================================

func TestIntegration_TransportManager_Failover(t *testing.T) {
	callLog := make([]string, 0)
	var mu sync.Mutex

	quic := &mockTransport{
		available: true,
		dialTCPFn: func(ctx context.Context, addr string, earlyData []byte) (net.Conn, error) {
			mu.Lock()
			callLog = append(callLog, "quic:"+addr)
			mu.Unlock()
			c, _ := net.Pipe()
			return c, nil
		},
	}
	tcpws := &mockTransport{
		available: true,
		dialTCPFn: func(ctx context.Context, addr string, earlyData []byte) (net.Conn, error) {
			mu.Lock()
			callLog = append(callLog, "tcpws:"+addr)
			mu.Unlock()
			c, _ := net.Pipe()
			return c, nil
		},
	}

	tm := NewTransportManager()
	tm.SetPrimary(quic, "quic-hop")
	tm.SetTCPWS(tcpws)

	// First call: QUIC preferred
	conn1, err := tm.DialTCP(context.Background(), "1.1.1.1:443", nil)
	if err != nil {
		t.Fatal(err)
	}
	conn1.Close()

	// Simulate QUIC going down
	quic.available = false

	// Second call: falls back to TCP-WS
	conn2, err := tm.DialTCP(context.Background(), "8.8.8.8:53", nil)
	if err != nil {
		t.Fatal(err)
	}
	conn2.Close()

	// Simulate QUIC recovery + network change (clears backoff)
	quic.available = true
	tm.Reset()

	// Third call: QUIC again
	conn3, err := tm.DialTCP(context.Background(), "9.9.9.9:443", nil)
	if err != nil {
		t.Fatal(err)
	}
	conn3.Close()

	mu.Lock()
	defer mu.Unlock()

	if len(callLog) != 3 {
		t.Fatalf("expected 3 calls, got %d: %v", len(callLog), callLog)
	}
	if callLog[0] != "quic:1.1.1.1:443" {
		t.Errorf("call[0] = %q, want quic:1.1.1.1:443", callLog[0])
	}
	if callLog[1] != "tcpws:8.8.8.8:53" {
		t.Errorf("call[1] = %q, want tcpws:8.8.8.8:53", callLog[1])
	}
	if callLog[2] != "quic:9.9.9.9:443" {
		t.Errorf("call[2] = %q, want quic:9.9.9.9:443", callLog[2])
	}
}

// ============================================================================
// Integration: QUIC datagram with UDP framing
// ============================================================================

func TestIntegration_QUIC_UDPDatagram(t *testing.T) {
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

	serverDone := make(chan error, 1)
	go func() {
		conn, err := ln.Accept(context.Background())
		if err != nil {
			serverDone <- err
			return
		}
		// Receive datagram with UDP frame
		data, err := conn.ReceiveDatagram(context.Background())
		if err != nil {
			serverDone <- err
			return
		}

		// Parse as UDP frame
		frame, err := ReadUDPFrame(bytes.NewReader(data))
		if err != nil {
			serverDone <- err
			return
		}

		// Build response frame and echo back
		var resp bytes.Buffer
		WriteUDPFrame(&resp, frame.SessionID, frame.Addr, frame.Payload)
		err = conn.SendDatagram(resp.Bytes())
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

	// Send UDP frame via datagram
	alloc := NewSessionIDAllocator()
	sid := alloc.Next()

	var sendBuf bytes.Buffer
	WriteUDPFrame(&sendBuf, sid, "8.8.8.8:53", []byte("dns-query"))
	if err := conn.SendDatagram(sendBuf.Bytes()); err != nil {
		t.Fatal(err)
	}

	// Receive echo
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	data, err := conn.ReceiveDatagram(ctx)
	if err != nil {
		t.Fatalf("receive datagram: %v", err)
	}

	frame, err := ReadUDPFrame(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("parse response: %v", err)
	}
	if frame.SessionID != sid {
		t.Errorf("session_id = %d, want %d", frame.SessionID, sid)
	}
	if frame.Addr != "8.8.8.8:53" {
		t.Errorf("addr = %q", frame.Addr)
	}
	if !bytes.Equal(frame.Payload, []byte("dns-query")) {
		t.Errorf("payload = %q", frame.Payload)
	}

	if err := <-serverDone; err != nil {
		t.Fatalf("server error: %v", err)
	}
}

// ============================================================================
// Integration: ECH config parsing + fingerprint selection
// ============================================================================

func TestIntegration_ECHConfigPipeline(t *testing.T) {
	// Simulate the full ECH config pipeline from URL to TLS config
	cfg := Config{
		Host:        "real-server.example.com",
		Port:        443,
		ECH:         "dGVzdGVjaGNvbmZpZw==", // "testechconfig" in base64
		Fingerprint: "chrome",
	}

	// Step 1: Parse ECH config
	echBytes, err := ParseECHConfig(cfg.ECH)
	if err != nil {
		t.Fatal(err)
	}
	if len(echBytes) == 0 {
		t.Fatal("ECH bytes should not be empty")
	}

	// Step 2: Parse fingerprint
	fp := ParseFingerprint(cfg.Fingerprint)
	if fp.Client != "Chrome" {
		t.Errorf("fingerprint client = %q, want Chrome", fp.Client)
	}

	// Step 3: Build TLS config (should succeed with ECH present)
	tlsCfg, clientHello, err := BuildClientTLSConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if tlsCfg.ServerName != cfg.Host {
		t.Errorf("ServerName = %q, want %q", tlsCfg.ServerName, cfg.Host)
	}
	if clientHello.Client != "Chrome" {
		t.Errorf("ClientHelloID client = %q, want Chrome", clientHello.Client)
	}
}

// ============================================================================
// Integration: URL parse → Config → QUIC client creation
// ============================================================================

func TestIntegration_URLToClient(t *testing.T) {
	raw := "k2v5://mydevice:jwt-token-here@1.2.3.4:443?ech=dGVzdA==&fp=chrome&hop=10000-20000"

	cfg, err := ParseURL(raw)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.UDID != "mydevice" {
		t.Errorf("UDID = %q", cfg.UDID)
	}
	if cfg.Token != "jwt-token-here" {
		t.Errorf("Token = %q", cfg.Token)
	}

	// Create QUIC client from config
	qc := NewQUICClient(cfg, nil)
	if qc.serverAddr != "1.2.3.4:443" {
		t.Errorf("serverAddr = %q", qc.serverAddr)
	}
	if !qc.hopEnabled() {
		t.Error("port hopping should be enabled")
	}

	// Create TCP-WS client from same config
	tc := NewTCPWSClient(cfg, nil)
	if tc.serverAddr != "1.2.3.4:443" {
		t.Errorf("serverAddr = %q", tc.serverAddr)
	}

	// TransportManager wires them together
	tm := NewTransportManager()
	// In real code: tm.SetPrimary(qc, "quic-hop"), tm.SetTCPWS(tc)
	// Here we verify the creation pipeline works
	_ = tm
}
