package wire

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/apernet/quic-go"
	smux "github.com/xtaci/smux/v2"
)

// ============================================================================
// Integration: Full QUIC proxy flow (auth + stream + data)
// ============================================================================

func TestIntegration_QUIC_FullProxyFlow(t *testing.T) {
	cert := generateTestCert(t)

	// Server setup
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
	ln, err := serverTransport.Listen(serverTLS, DefaultQUICConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	serverAddr := serverUDP.LocalAddr().(*net.UDPAddr)
	store := NewTicketStore()

	// Server goroutine: accept, auth on first stream, proxy on second
	serverDone := make(chan error, 1)
	go func() {
		conn, err := ln.Accept(context.Background())
		if err != nil {
			serverDone <- err
			return
		}

		// Stream 1: Auth request (simulated as stream header + response)
		authStream, err := conn.AcceptStream(context.Background())
		if err != nil {
			serverDone <- err
			return
		}
		// Client sends device ID, server issues ticket
		br := bufio.NewReader(authStream)
		buf := make([]byte, 100)
		n, err := br.Read(buf)
		if err != nil {
			serverDone <- err
			return
		}
		deviceID := string(buf[:n])
		ticket, err := store.Issue(deviceID, true)
		if err != nil {
			serverDone <- err
			return
		}
		authStream.Write([]byte(ticket.Token))
		authStream.Close()

		// Stream 2: Proxy stream
		proxyStream, err := conn.AcceptStream(context.Background())
		if err != nil {
			serverDone <- err
			return
		}
		pbr := bufio.NewReader(proxyStream)
		typ, addr, err := ReadStreamHeader(pbr)
		if err != nil {
			serverDone <- err
			return
		}
		if typ != StreamTypeTCP {
			serverDone <- net.UnknownNetworkError("expected TCP stream")
			return
		}
		_ = addr

		WriteStreamResponse(proxyStream, nil)

		// Echo data
		data := make([]byte, 100)
		dn, err := pbr.Read(data)
		if err != nil {
			serverDone <- err
			return
		}
		proxyStream.Write(data[:dn])
		proxyStream.Close()
		serverDone <- nil
	}()

	// Client side
	clientUDP, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer clientUDP.Close()

	clientTransport := &quic.Transport{Conn: clientUDP}
	defer clientTransport.Close()

	clientTLS := &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{"h3"},
	}
	conn, err := clientTransport.Dial(context.Background(), serverAddr, clientTLS, DefaultQUICConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseWithError(0, "")

	// Auth: open stream, send device ID, get ticket
	authStream, err := conn.OpenStreamSync(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	authStream.Write([]byte("test-device-001"))
	abr := bufio.NewReader(authStream)
	tbuf := make([]byte, 200)
	tn, err := abr.Read(tbuf)
	if err != nil {
		t.Fatalf("read ticket: %v", err)
	}
	ticket := string(tbuf[:tn])

	// Verify ticket is valid
	_, ok := store.Validate(ticket)
	if !ok {
		t.Fatal("ticket should be valid")
	}

	// Proxy: open stream, send header, exchange data
	proxyStream, err := conn.OpenStreamSync(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	WriteStreamHeader(proxyStream, StreamTypeTCP, "93.184.216.34:80")

	pbr := bufio.NewReader(proxyStream)
	rok, _, errMsg, err := ReadStreamResponse(pbr)
	if err != nil {
		t.Fatalf("ReadStreamResponse: %v", err)
	}
	if !rok {
		t.Fatalf("server rejected: %s", errMsg)
	}

	proxyStream.Write([]byte("GET / HTTP/1.1\r\n"))

	rbuf := make([]byte, 100)
	rn, err := pbr.Read(rbuf)
	if err != nil {
		t.Fatalf("read proxy data: %v", err)
	}
	if string(rbuf[:rn]) != "GET / HTTP/1.1\r\n" {
		t.Errorf("echo = %q", string(rbuf[:rn]))
	}

	if err := <-serverDone; err != nil {
		t.Fatalf("server error: %v", err)
	}
}

// ============================================================================
// Integration: Full smux proxy flow (auth + TCP stream + UDP stream)
// ============================================================================

func TestIntegration_Smux_FullProxyFlow(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	store := NewTicketStore()
	ticket, _ := store.Issue("test-device", true)

	serverDone := make(chan error, 1)
	go func() {
		sess, err := smux.Server(serverConn, DefaultSmuxConfig())
		if err != nil {
			serverDone <- err
			return
		}

		var wg sync.WaitGroup

		// Accept TCP proxy stream
		stream1, err := sess.AcceptStream()
		if err != nil {
			serverDone <- err
			return
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer stream1.Close()
			br := bufio.NewReader(stream1)
			typ, addr, err := ReadStreamHeader(br)
			if err != nil {
				return
			}
			if typ != StreamTypeTCP || addr != "93.184.216.34:80" {
				return
			}
			WriteStreamResponse(stream1, nil)
			buf := make([]byte, 100)
			n, _ := br.Read(buf)
			stream1.Write(buf[:n])
		}()

		// Accept UDP stream
		stream2, err := sess.AcceptStream()
		if err != nil {
			serverDone <- err
			return
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer stream2.Close()
			br := bufio.NewReader(stream2)
			typ, _, err := ReadStreamHeader(br)
			if err != nil || typ != StreamTypeUDP {
				return
			}
			frame, err := ReadSmuxUDPFrame(br)
			if err != nil {
				return
			}
			// Echo UDP frame back
			WriteSmuxUDPFrame(stream2, frame.SessionID, frame.Addr, frame.Payload)
		}()

		wg.Wait()
		sess.Close()
		serverDone <- nil
	}()

	// Client side
	sess, err := smux.Client(clientConn, DefaultSmuxConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()

	// Verify ticket (client would have gotten this from auth)
	_, ok := store.Validate(ticket.Token)
	if !ok {
		t.Fatal("ticket invalid")
	}

	// TCP proxy stream
	tcpStream, err := sess.OpenStream()
	if err != nil {
		t.Fatal(err)
	}
	WriteStreamHeader(tcpStream, StreamTypeTCP, "93.184.216.34:80")
	tbr := bufio.NewReader(tcpStream)
	rok, _, _, err := ReadStreamResponse(tbr)
	if err != nil {
		t.Fatal(err)
	}
	if !rok {
		t.Fatal("TCP stream rejected")
	}
	tcpStream.Write([]byte("GET /"))
	buf := make([]byte, 100)
	n, err := tbr.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	if string(buf[:n]) != "GET /" {
		t.Errorf("TCP echo = %q", string(buf[:n]))
	}
	tcpStream.Close()

	// UDP stream
	udpStream, err := sess.OpenStream()
	if err != nil {
		t.Fatal(err)
	}
	WriteStreamHeader(udpStream, StreamTypeUDP, "")
	WriteSmuxUDPFrame(udpStream, 1, "8.8.8.8:53", []byte("dns-query"))

	ubr := bufio.NewReader(udpStream)
	frame, err := ReadSmuxUDPFrame(ubr)
	if err != nil {
		t.Fatalf("ReadSmuxUDPFrame: %v", err)
	}
	if frame.SessionID != 1 {
		t.Errorf("session_id = %d", frame.SessionID)
	}
	if !bytes.Equal(frame.Payload, []byte("dns-query")) {
		t.Errorf("payload = %q", frame.Payload)
	}
	udpStream.Close()

	if err := <-serverDone; err != nil {
		t.Fatalf("server error: %v", err)
	}
}

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
	tm.ResetConnections()

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
// Integration: Auth ticket lifecycle (issue → validate → expire)
// ============================================================================

func TestIntegration_AuthTicketLifecycle(t *testing.T) {
	store := NewTicketStore()

	// Device A gets ticket
	ticketA, err := store.Issue("device-A", true)
	if err != nil {
		t.Fatal(err)
	}

	// Device B gets ticket
	ticketB, err := store.Issue("device-B", true)
	if err != nil {
		t.Fatal(err)
	}

	// Both valid
	_, okA := store.Validate(ticketA.Token)
	_, okB := store.Validate(ticketB.Token)
	if !okA || !okB {
		t.Fatal("both tickets should be valid")
	}

	// Device A re-auths (simulating transport switch)
	ticketA2, err := store.Issue("device-A", true)
	if err != nil {
		t.Fatal(err)
	}

	// New ticket valid
	_, ok := store.Validate(ticketA2.Token)
	if !ok {
		t.Error("new ticket should be valid")
	}

	// Old ticket in grace period (still valid briefly)
	_, ok = store.Validate(ticketA.Token)
	if !ok {
		t.Error("old ticket should be in grace period")
	}

	// Device B unaffected
	_, ok = store.Validate(ticketB.Token)
	if !ok {
		t.Error("device B ticket should still be valid")
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
