package wire

// Race + Echo Contract Tests
//
// These tests verify end-to-end transport race behavior with real QUIC/TCP-WS
// servers, focusing on echo probe edge cases and hot-swap correctness.

import (
	"bufio"
	"context"
	tls "github.com/metacubex/utls"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/kaitu-io/kuic"
	"github.com/coder/websocket"
	smux "github.com/xtaci/smux/v2"
)

// TestContract_EchoDetectsBlockedShortHeader verifies that when a QUIC server
// accepts handshake (Long Header) but never processes streams (Short Header
// blocked by middlebox), the echo probe times out and RaceTransport falls
// back to TCP-WS.
//
// User scenario: airport/hotel WiFi DPI that passes QUIC Initial packets
// (Long Header) but drops all Short Header (1-RTT) data packets.
func TestContract_EchoDetectsBlockedShortHeader(t *testing.T) {
	cert := generateTestCert(t)

	// Find a free port usable for both UDP (QUIC) and TCP (HTTP/WS).
	// UDP and TCP can share the same port number.
	udpConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	port := udpConn.LocalAddr().(*net.UDPAddr).Port

	// QUIC listener: accepts connections (handshake completes) but NEVER
	// accepts streams — simulates middlebox that drops Short Header packets.
	transport := &quic.Transport{Conn: udpConn}
	serverTLS := &tls.Config{
		Certificates: []tls.Certificate{cert},
		NextProtos:   []string{"h3"},
	}
	ln, err := transport.Listen(serverTLS, DefaultQUICConfig())
	if err != nil {
		udpConn.Close()
		t.Fatal(err)
	}

	// Accept connections to complete handshake, but never call AcceptStream.
	go func() {
		for {
			conn, err := ln.Accept(context.Background())
			if err != nil {
				return
			}
			// Hold the connection open — never process streams.
			// This means OpenStreamSync on the client side will succeed
			// (QUIC handshake is done), but writing to the stream will
			// either block or the echo read will time out because the
			// server never reads/responds.
			_ = conn
		}
	}()

	// TCP-WS server on same port number (TCP, different protocol from UDP).
	tcpLn, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		ln.Close()
		transport.Close()
		t.Fatal(err)
	}

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

	srv := &http.Server{Handler: mux}
	srvTLS := &tls.Config{Certificates: []tls.Certificate{cert}}
	go srv.Serve(tls.NewListener(tcpLn, srvTLS))

	defer func() {
		srv.Close()
		tcpLn.Close()
		ln.Close()
		transport.Close()
	}()

	cfg := Config{
		Host:        "127.0.0.1",
		Port:        port,
		Insecure:    true,
		Fingerprint: "chrome",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	result, err := RaceTransport(ctx, cfg, "", nil, nil)
	if err != nil {
		t.Fatalf("RaceTransport failed: %v", err)
	}
	defer result.Dialer.Close()

	if result.TransportType != "tcpws" {
		t.Errorf("TransportType = %q, want tcpws (QUIC echo should have timed out due to blocked Short Header)", result.TransportType)
	}
}

// TestContract_OldServerNoEcho verifies backward compatibility: a server that
// does not implement echo (old k2s version) should still work. RaceTransport
// degrades to handshake-only acceptance when isEchoUnsupported returns true.
//
// User scenario: client upgraded, server hasn't been updated yet.
func TestContract_OldServerNoEcho(t *testing.T) {
	cert := generateTestCert(t)

	udpConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	port := udpConn.LocalAddr().(*net.UDPAddr).Port

	transport := &quic.Transport{Conn: udpConn}
	serverTLS := &tls.Config{
		Certificates: []tls.Certificate{cert},
		NextProtos:   []string{"h3"},
	}
	ln, err := transport.Listen(serverTLS, DefaultQUICConfig())
	if err != nil {
		udpConn.Close()
		t.Fatal(err)
	}

	// Old server: accepts connections and streams, but for unknown stream
	// types (echo), resets/closes the stream immediately. This is what a
	// pre-echo server would do when it encounters StreamTypeEcho.
	go func() {
		for {
			conn, err := ln.Accept(context.Background())
			if err != nil {
				return
			}
			go func(c *quic.Conn) {
				for {
					stream, err := c.AcceptStream(context.Background())
					if err != nil {
						return
					}
					go func(s *quic.Stream) {
						// Read enough to identify the stream type, then reject.
						br := bufio.NewReader(s)
						// Read H3 frame type marker.
						ReadVarint(br)
						// Read stream header to see the type.
						streamType, _, _ := ReadStreamHeader(br)
						if streamType == StreamTypeEcho {
							// Old server: doesn't know echo, reset the stream.
							s.CancelRead(0)
							s.Close()
							return
						}
						// For other types, handle normally (not needed for this test).
						s.CancelRead(0)
						s.Close()
					}(stream)
				}
			}(conn)
		}
	}()

	defer func() {
		ln.Close()
		transport.Close()
	}()

	cfg := Config{
		Host:     "127.0.0.1",
		Port:     port,
		Insecure: true,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := RaceTransport(ctx, cfg, "", nil, nil)
	if err != nil {
		t.Fatalf("RaceTransport should degrade gracefully for old server: %v", err)
	}
	defer result.Dialer.Close()

	if result.TransportType != "quic-443" {
		t.Errorf("TransportType = %q, want quic-443 (old server without echo should still be accepted)", result.TransportType)
	}
}

// TestContract_ReplacePrimary_ClosesOld verifies that ReplacePrimary closes the
// old primary transport but preserves the TCP-WS transport. This is the
// hot-swap mechanism used by re-race when echo probes detect degradation.
//
// User scenario: tunnel running on QUIC-443, network change causes echo
// failures, re-race establishes a fresh QUIC connection, hot-swaps without
// dropping TCP-WS fallback.
func TestContract_ReplacePrimary_ClosesOld(t *testing.T) {
	old := &mockTransport{available: true}
	tcpws := &mockTransport{available: true}

	tm := NewTransportManager()
	tm.SetPrimary(old, "quic-443")
	tm.SetTCPWS(tcpws)

	// Activate fallback to verify ReplacePrimary clears it.
	tm.SetFallbackActive(true)
	if !tm.FallbackActive() {
		t.Fatal("precondition: fallback should be active")
	}

	newPrimary := &mockTransport{available: true}
	tm.ReplacePrimary(newPrimary, "quic-443")

	// Old primary must be closed.
	if !old.closed {
		t.Error("contract: old primary must be closed after ReplacePrimary")
	}

	// TCP-WS must NOT be closed — it's the shared fallback.
	if tcpws.closed {
		t.Error("contract: TCP-WS must NOT be closed by ReplacePrimary")
	}

	// New primary must NOT be closed.
	if newPrimary.closed {
		t.Error("contract: new primary must NOT be closed by ReplacePrimary")
	}

	// Type label must be updated.
	if got := tm.PrimaryType(); got != "quic-443" {
		t.Errorf("PrimaryType = %q, want quic-443", got)
	}

	// Fallback must be cleared (new primary is fresh).
	if tm.FallbackActive() {
		t.Error("contract: fallback must be cleared after ReplacePrimary")
	}

	// Verify new primary is actually used for traffic.
	newPrimaryCalled := false
	newPrimary.dialTCPFn = func(ctx context.Context, addr string, earlyData []byte) (net.Conn, error) {
		newPrimaryCalled = true
		c, _ := net.Pipe()
		return c, nil
	}

	conn, err := tm.DialTCP(context.Background(), "8.8.8.8:53", nil)
	if err != nil {
		t.Fatalf("DialTCP after ReplacePrimary: %v", err)
	}
	conn.Close()

	if !newPrimaryCalled {
		t.Error("contract: new primary must be used for traffic after ReplacePrimary")
	}
}
