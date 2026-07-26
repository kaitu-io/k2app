package wire

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/apernet/quic-go"
	"github.com/coder/websocket"
	smux "github.com/xtaci/smux/v2"
)

// TestRaceTransport_QUIC443Wins verifies that when a QUIC server is reachable
// on the primary port, "quic-443" wins the race with echo probe verification.
func TestRaceTransport_QUIC443Wins(t *testing.T) {
	cert := generateTestCert(t)

	// Set up QUIC server with echo support (via QUICServer.Listen + handleStream).
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
		serverUDP.Close()
		t.Fatal(err)
	}

	qs := NewQUICServer()
	qs.Listen(ln)

	serverAddr := serverUDP.LocalAddr().(*net.UDPAddr)

	defer func() {
		qs.Close()
		ln.Close()
		serverTransport.Close()
		serverUDP.Close()
	}()

	// Build config pointing to this server, no hop ports.
	cfg := Config{
		Host:     "127.0.0.1",
		Port:     serverAddr.Port,
		Insecure: true,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := RaceTransport(ctx, cfg, "", nil)
	if err != nil {
		t.Fatalf("RaceTransport failed: %v", err)
	}
	defer result.Dialer.Close()

	if result.TransportType != "quic-443" {
		t.Errorf("TransportType = %q, want %q", result.TransportType, "quic-443")
	}
	if result.Dialer == nil {
		t.Fatal("Dialer is nil")
	}
}

// TestRaceTransport_FallbackToTCPWS verifies that when QUIC is unreachable
// (no UDP listener), TCP-WS wins the race as the fallback candidate.
func TestRaceTransport_FallbackToTCPWS(t *testing.T) {
	cert := generateTestCert(t)

	// Start a TCP-WS server (HTTPS + WebSocket + smux).
	mux := http.NewServeMux()
	mux.HandleFunc("/k2v5/tunnel", func(w http.ResponseWriter, r *http.Request) {
		ws, wsErr := websocket.Accept(w, r, nil)
		if wsErr != nil {
			return
		}
		wsConn := websocket.NetConn(context.Background(), ws, websocket.MessageBinary)
		defer wsConn.Close()

		sess, smuxErr := smux.Server(wsConn, DefaultSmuxConfig())
		if smuxErr != nil {
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
				}
			}()
		}
	})

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	srv := &http.Server{
		Handler:   mux,
		TLSConfig: &tls.Config{Certificates: []tls.Certificate{cert}},
	}
	go srv.Serve(tls.NewListener(listener, srv.TLSConfig))
	defer srv.Close()

	_, portStr, _ := net.SplitHostPort(listener.Addr().String())
	var port int
	fmt.Sscanf(portStr, "%d", &port)

	// Config pointing to the TCP-WS server's port.
	// QUIC will fail (no UDP listener on this port), TCP-WS will succeed.
	cfg := Config{
		Host:        "127.0.0.1",
		Port:        port,
		Insecure:    true,
		Fingerprint: "chrome",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	result, err := RaceTransport(ctx, cfg, "", nil)
	if err != nil {
		t.Fatalf("RaceTransport failed: %v", err)
	}
	defer result.Dialer.Close()

	if result.TransportType != "tcpws" {
		t.Errorf("TransportType = %q, want %q", result.TransportType, "tcpws")
	}
}

// TestRaceTransport_AllFail verifies that when no transport is reachable,
// RaceTransport returns an error.
func TestRaceTransport_AllFail(t *testing.T) {
	// Use a port that nothing listens on.
	cfg := Config{
		Host:     "127.0.0.1",
		Port:     1, // unreachable
		Insecure: true,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := RaceTransport(ctx, cfg, "", nil)
	if err == nil {
		if result != nil && result.Dialer != nil {
			result.Dialer.Close()
		}
		t.Fatal("expected error, got nil")
	}
	if result != nil {
		t.Errorf("expected nil result, got %+v", result)
	}
}
