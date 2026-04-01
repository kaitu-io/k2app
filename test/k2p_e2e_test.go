package test

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/apernet/quic-go"
	"github.com/pion/stun/v3"

	"github.com/kaitu-io/k2/k2p"
	"github.com/kaitu-io/k2/signal"
	"github.com/kaitu-io/k2/wire"
)

// startTestSTUNServer runs a minimal STUN server on loopback that responds
// to Binding Requests with the client's source address as XOR-MAPPED-ADDRESS.
// Duplicated from k2p/stun_test.go (package-internal, cannot import).
func startTestSTUNServer(t *testing.T) string {
	t.Helper()
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("listen STUN server: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	go func() {
		buf := make([]byte, 1500)
		for {
			n, from, err := conn.ReadFromUDP(buf)
			if err != nil {
				return
			}
			req := new(stun.Message)
			req.Raw = buf[:n]
			if err := req.Decode(); err != nil {
				continue
			}
			resp, err := stun.Build(
				stun.NewTransactionIDSetter(req.TransactionID),
				stun.BindingSuccess,
				&stun.XORMappedAddress{IP: from.IP, Port: from.Port},
				stun.Fingerprint,
			)
			if err != nil {
				continue
			}
			conn.WriteToUDP(resp.Raw, from)
		}
	}()

	return conn.LocalAddr().String()
}

// TestK2P_E2E_DirectPunch verifies the full k2p loopback flow:
// signal server + STUN + Home.Listen + Away.Dial + hole punch + QUIC.
func TestK2P_E2E_DirectPunch(t *testing.T) {
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

	// 2. STUN servers (need 2 for NAT classification).
	stunAddr1 := startTestSTUNServer(t)
	stunAddr2 := startTestSTUNServer(t)
	stunServers := []string{stunAddr1, stunAddr2}

	// 3. Generate Home TLS cert.
	certPair, err := wire.GenerateCertPair()
	if err != nil {
		t.Fatalf("generate cert pair: %v", err)
	}

	// 4. Hash a test secret for peer auth.
	peerSecret := "test-secret-42"
	secretHash, err := k2p.HashSecret(peerSecret)
	if err != nil {
		t.Fatalf("hash secret: %v", err)
	}

	// 5. Claim UDID via signal client.
	homeUDID := "test-home-e2e"
	signalClient := k2p.NewSignalClient(signalSrv.URL, "")
	regToken, err := signalClient.Claim(ctx, homeUDID)
	if err != nil {
		t.Fatalf("claim UDID: %v", err)
	}

	// 6. Home: Listen (registers, opens WS, waits for punch notifications).
	homeCfg := k2p.Config{
		HomeUDID:     homeUDID,
		SignalServer: signalSrv.URL,
		RegToken:     regToken,
		SecretHash:   secretHash,
		TLSCert:      &certPair.EC,
		STUNServers:  stunServers,
	}
	listenerCh, err := k2p.Listen(ctx, homeCfg)
	if err != nil {
		t.Fatalf("Home Listen: %v", err)
	}

	// Consume the listener from the channel in a goroutine. Auth is now
	// handled at the QUIC connection level inside handleAwayConnection,
	// so the listener is ready for k2v5 streams when it arrives.
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

	// 7. Away: Dial (STUN → lookup → connect → punch → QUIC).
	awayCfg := k2p.Config{
		HomeUDID:     homeUDID,
		SignalServer: signalSrv.URL,
		Token:        "any-token", // Away uses Token (not RegToken) for lookup/connect.
		PeerSecret:   peerSecret,
		Pin:          certPair.FormatPins(),
		STUNServers:  stunServers,
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

	// 8. Verify transport type.
	if result.TransportType != "p2p-direct" {
		t.Errorf("expected transport type p2p-direct, got %s", result.TransportType)
	}
	if result.Dialer == nil {
		t.Fatal("expected non-nil Dialer")
	}

	// 9. Verify Home received the connection (auth + listener creation).
	select {
	case listener := <-homeAccepted:
		// Success — full signal + punch + auth + QUIC flow works.
		t.Cleanup(func() { listener.Close() })
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for Home to accept connection")
	}
}

// TestK2P_E2E_AuthRejection verifies that a wrong PeerSecret is rejected.
// Tests at the QUIC connection level to avoid loopback punch timing issues.
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
// Tests at the QUIC connection level (no full punch needed — same pattern as
// TestK2P_E2E_AuthRejection) to avoid loopback punch timing issues.
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
