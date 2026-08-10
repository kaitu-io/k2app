package test

import (
	"context"
	"crypto/sha256"
	tls "github.com/metacubex/utls"
	"crypto/x509"
	"fmt"
	"io"
	"net"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kaitu-io/kuic"

	"github.com/kaitu-io/k2/k2p"
	"github.com/kaitu-io/k2/server"
	"github.com/kaitu-io/k2/signal"
	"github.com/kaitu-io/k2/wire"
)

// TestK2P_E2E_ICEDirect verifies the full k2p ICE-based flow on loopback:
// signal server + Home.Listen + Away.Dial + ICE host candidates + QUIC + peer auth.
func TestK2P_E2E_ICEDirect(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// 1. Signal server.
	store := signal.NewStore()
	handler := signal.NewHandler(store, "", nil)
	signalSrv := httptest.NewServer(handler)
	t.Cleanup(signalSrv.Close)

	// 2. Generate Home TLS cert.
	certPair, err := wire.GenerateCertPair()
	if err != nil {
		t.Fatalf("generate cert pair: %v", err)
	}

	// 3. Claim UDID via signal client.
	homeUDID := "test-home-e2e"
	signalClient := k2p.NewSignalClient(signalSrv.URL, "")
	regToken, err := signalClient.Claim(ctx, homeUDID)
	if err != nil {
		t.Fatalf("claim UDID: %v", err)
	}

	// 4. Home: Listen (registers, opens WS, waits for Away ICE sessions).
	// Peer auth is exercised end-to-end: Home verifies Away's secret at QUIC level.
	peerSecret := "test-peer-secret-e2e"
	secretHash, err := k2p.HashSecret(peerSecret)
	if err != nil {
		t.Fatalf("hash secret: %v", err)
	}
	homeCfg := k2p.Config{
		HomeUDID:     homeUDID,
		SignalServer: signalSrv.URL,
		RegToken:     regToken,
		TLSCert:      &certPair.EC,
		SecretHash:   secretHash,
	}
	listenerCh, err := k2p.Listen(ctx, homeCfg)
	if err != nil {
		t.Fatalf("Home Listen: %v", err)
	}

	// Consume the listener from the channel in a goroutine.
	homeAccepted := make(chan wire.Listener, 1)
	go func() {
		listener, ok := <-listenerCh
		if !ok {
			return
		}
		homeAccepted <- listener
	}()

	// Wait for Home's WS connection to be established so Away's /connect works.
	waitForHomeOnline(t, ctx, store, homeUDID)

	// 5. Away: Dial (lookup -> WS -> ICE candidate exchange -> QUIC).
	awayCfg := k2p.Config{
		HomeUDID:     homeUDID,
		SignalServer: signalSrv.URL,
		Token:        "any-token", // Away uses Token (not RegToken) for lookup/connect.
		Pin:          certPair.FormatPins(),
		PeerSecret:   peerSecret,
		WireCfg:      wire.Config{Host: "localhost", Insecure: true},
	}
	result, err := k2p.Dial(ctx, awayCfg)
	if err != nil {
		t.Fatalf("Away Dial: %v", err)
	}
	t.Cleanup(func() {
		if result.Dialer != nil {
			result.Dialer.Close()
		}
	})

	// 6. Verify transport type (host candidates on loopback = p2p-lan).
	if result.TransportType != "p2p-lan" {
		t.Errorf("expected transport type p2p-lan, got %s", result.TransportType)
	}
	if result.Dialer == nil {
		t.Fatal("expected non-nil Dialer")
	}

	// 7. Verify Home received the connection (ICE + QUIC + listener creation).
	var homeListener wire.Listener
	select {
	case homeListener = <-homeAccepted:
		t.Cleanup(func() { homeListener.Close() })
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for Home to accept connection")
	}

	// 8. Start TCP echo server (simulates Home LAN service, e.g. NAS).
	echoAddr := startTCPEchoServer(t)

	// 9. Home: serve incoming TCP from Away using ProxyHandler (same as peer_run.go).
	// ProxyHandler handles k2v5 stream response + upstream dial + pipe.
	ph := server.NewProxyHandler()
	ph.AllowPrivateIP = true // echo server is on localhost
	go func() {
		for {
			conn, addr, err := homeListener.AcceptTCP(ctx)
			if err != nil {
				return
			}
			go ph.HandleTCP(conn, addr)
		}
	}()

	// 10. Away: DialTCP through k2p dialer → echo server via Home.
	// This validates the full outbound data path: Away → QUIC/ICE → Home → echo.
	awayConn, err := result.Dialer.DialTCP(ctx, echoAddr, nil)
	if err != nil {
		t.Fatalf("Away DialTCP: %v", err)
	}
	defer awayConn.Close()

	testData := "hello from away via k2p outbound"
	if _, err := awayConn.Write([]byte(testData)); err != nil {
		t.Fatalf("write: %v", err)
	}
	buf := make([]byte, len(testData))
	if _, err := io.ReadFull(awayConn, buf); err != nil {
		t.Fatalf("read echo: %v", err)
	}
	if string(buf) != testData {
		t.Fatalf("echo mismatch: got %q, want %q", string(buf), testData)
	}
	t.Logf("k2p outbound TCP data flow verified: %d bytes round-trip", len(testData))
}

// TestK2P_E2E_AuthRejection verifies that a wrong PeerSecret is rejected.
// Tests at the QUIC connection level to avoid loopback ICE timing issues.
func TestK2P_E2E_AuthRejection(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Generate Home cert.
	certPair, err := wire.GenerateCertPair()
	if err != nil {
		t.Fatalf("generate cert pair: %v", err)
	}
	secretHash, err := k2p.HashSecret("correct-secret")
	if err != nil {
		t.Fatalf("hash secret: %v", err)
	}

	// Home side: QUIC listener on loopback.
	homeConn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	homeAddr := homeConn.LocalAddr().(*net.UDPAddr)
	homeTr := &quic.Transport{Conn: homeConn}
	t.Cleanup(func() { homeTr.Close() })

	homeTLSCfg := &tls.Config{
		NextProtos:   []string{"h3"},
		Certificates: []tls.Certificate{certPair.EC},
	}
	homeLn, err := homeTr.Listen(homeTLSCfg, wire.DefaultQUICConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { homeLn.Close() })

	// Home: accept connection and verify auth (same logic as handleAwayConnection).
	authResult := make(chan error, 1)
	go func() {
		conn, err := homeLn.Accept(ctx)
		if err != nil {
			authResult <- err
			return
		}
		stream, err := conn.AcceptStream(ctx)
		if err != nil {
			conn.CloseWithError(1, "auth stream failed")
			authResult <- err
			return
		}
		secret, err := k2p.ReadAuthFrame(stream)
		if err != nil {
			stream.Close()
			conn.CloseWithError(1, "auth read failed")
			authResult <- err
			return
		}
		if !k2p.VerifySecret(secret, secretHash) {
			stream.Write([]byte{k2p.AuthRejected}) //nolint:errcheck
			stream.Close()
			conn.CloseWithError(1, "auth rejected")
			authResult <- nil // auth rejection is expected
			return
		}
		stream.Write([]byte{k2p.AuthOK}) //nolint:errcheck
		stream.Close()
		authResult <- fmt.Errorf("auth should have been rejected")
	}()

	// Away side: QUIC dial with wrong secret.
	awayConn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	awayTr := &quic.Transport{Conn: awayConn}
	t.Cleanup(func() { awayTr.Close() })

	awayTLSCfg := &tls.Config{
		NextProtos:         []string{"h3"},
		InsecureSkipVerify: true,
	}
	qConn, err := awayTr.Dial(ctx, homeAddr, awayTLSCfg, wire.DefaultQUICConfig())
	if err != nil {
		t.Fatalf("QUIC dial: %v", err)
	}

	// Send wrong auth.
	authStream, err := qConn.OpenStreamSync(ctx)
	if err != nil {
		t.Fatalf("open auth stream: %v", err)
	}
	if err := k2p.WriteAuthFrame(authStream, "wrong-secret"); err != nil {
		t.Fatalf("write auth frame: %v", err)
	}
	resp := make([]byte, 1)
	if _, err := io.ReadFull(authStream, resp); err != nil {
		// Connection closed before response — also means auth rejected.
		t.Logf("auth read error (expected): %v", err)
	} else if resp[0] != k2p.AuthRejected {
		t.Fatalf("expected AuthRejected (0x01), got 0x%02x", resp[0])
	}
	authStream.Close()
	qConn.CloseWithError(0, "test done")

	// Verify Home side processed auth correctly.
	select {
	case err := <-authResult:
		if err != nil {
			t.Fatalf("Home auth error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for Home auth result")
	}
}

// TestK2P_E2E_PinRejection verifies that Away is rejected when the provided pin
// does not match the Home peer's actual certificate.
// Tests at the QUIC connection level (no full ICE needed — same pattern as
// TestK2P_E2E_AuthRejection).
func TestK2P_E2E_PinRejection(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Generate two distinct cert pairs: one for Home, one to provide the wrong pin.
	homeCertPair, err := wire.GenerateCertPair()
	if err != nil {
		t.Fatalf("generate home cert pair: %v", err)
	}
	wrongCertPair, err := wire.GenerateCertPair()
	if err != nil {
		t.Fatalf("generate wrong cert pair: %v", err)
	}

	// Start a QUIC listener using the Home cert.
	homeConn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	homeAddr := homeConn.LocalAddr().(*net.UDPAddr)
	homeTr := &quic.Transport{Conn: homeConn}
	t.Cleanup(func() { homeTr.Close() })

	homeTLSCfg := &tls.Config{
		NextProtos:   []string{"h3"},
		Certificates: []tls.Certificate{homeCertPair.EC},
	}
	homeLn, err := homeTr.Listen(homeTLSCfg, wire.DefaultQUICConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { homeLn.Close() })

	// Home accepts connections in background (just accept and close).
	go func() {
		for {
			conn, err := homeLn.Accept(ctx)
			if err != nil {
				return
			}
			conn.CloseWithError(0, "test done")
		}
	}()

	// Away dials with the WRONG pin (from wrongCertPair, not homeCertPair).
	// The TLS VerifyPeerCertificate callback should reject Home's certificate.
	awayConn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	awayTr := &quic.Transport{Conn: awayConn}
	t.Cleanup(func() { awayTr.Close() })

	wrongPin := wrongCertPair.FormatPins()
	awayTLSCfg := &tls.Config{
		NextProtos:         []string{"h3"},
		InsecureSkipVerify: true, // skip CA; pin check replaces CA validation
	}
	expectedPins, err := wire.ParsePins(wrongPin)
	if err != nil {
		t.Fatalf("ParsePins: %v", err)
	}
	awayTLSCfg.VerifyPeerCertificate = func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
		for _, raw := range rawCerts {
			cert, err := x509.ParseCertificate(raw)
			if err != nil {
				continue
			}
			pubDER, err := x509.MarshalPKIXPublicKey(cert.PublicKey)
			if err != nil {
				continue
			}
			hash := sha256.Sum256(pubDER)
			for _, exp := range expectedPins {
				if hash == exp {
					return nil
				}
			}
		}
		return fmt.Errorf("k2p: no certificate matched pin")
	}

	_, err = awayTr.Dial(ctx, homeAddr, awayTLSCfg, wire.DefaultQUICConfig())
	if err == nil {
		t.Fatal("expected QUIC dial to fail with pin mismatch, got nil")
	}
	if !strings.Contains(err.Error(), "pin") && !strings.Contains(err.Error(), "certificate") && !strings.Contains(err.Error(), "tls") {
		t.Errorf("expected pin/certificate/tls error, got: %v", err)
	}
	t.Logf("pin rejection error (expected): %v", err)
}

// waitForHomeOnline polls the signal store until the Home peer's WS is connected.
func waitForHomeOnline(t *testing.T, ctx context.Context, store *signal.Store, udid string) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		if store.IsOnline(udid) {
			return
		}
		select {
		case <-deadline:
			t.Fatal("timeout waiting for Home to connect WS")
		case <-ctx.Done():
			t.Fatal("context cancelled waiting for Home WS")
		case <-time.After(50 * time.Millisecond):
		}
	}
}
