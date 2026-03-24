package k2p2p

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/apernet/quic-go"
	"github.com/kaitu-io/k2/wire"
)

func TestAway_Dial_LoopbackIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}

	// Mock STUN servers.
	_, stunAddr := startTestSTUNServer(t)
	_, stunAddr2 := startTestSTUNServer(t)

	// Generate self-signed cert for home QUIC server.
	certPair, err := wire.GenerateCertPair()
	if err != nil {
		t.Fatal(err)
	}

	// Home side: QUIC server on loopback.
	// quic.Transport reads from homeConn but discards non-QUIC packets,
	// so we can WriteToUDP punch magic through homeConn while Transport is active.
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

	// Accept one QUIC connection in background.
	quicAccepted := make(chan struct{})
	go func() {
		conn, err := homeLn.Accept(context.Background())
		if err != nil {
			return
		}
		defer conn.CloseWithError(0, "test done")
		close(quicAccepted)
		time.Sleep(2 * time.Second)
	}()

	// Mock signal server.
	mux := http.NewServeMux()
	mux.HandleFunc("GET /p2p/lookup/{udid}", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(PeerInfo{
			UDID:    "test-home",
			Addr:    homeAddr.String(),
			NATType: "easy",
		})
	})
	mux.HandleFunc("POST /p2p/connect", func(w http.ResponseWriter, r *http.Request) {
		// Simulate home side: when away signals connect, home starts
		// sending punch magic from homeConn to away's reflexive address.
		var req ConnectRequest
		json.NewDecoder(r.Body).Decode(&req)

		go func() {
			addr, err := net.ResolveUDPAddr("udp4", req.AwayAddr)
			if err != nil {
				return
			}
			for i := 0; i < 50; i++ {
				homeConn.WriteToUDP([]byte(punchMagic), addr)
				time.Sleep(100 * time.Millisecond)
			}
		}()

		w.WriteHeader(200)
	})
	signalSrv := httptest.NewServer(mux)
	defer signalSrv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	wireCfg := wire.Config{Host: "localhost", Port: homeAddr.Port, Insecure: true}
	result, err := Dial(ctx, Config{
		HomeUDID:     "test-home",
		SignalServer: signalSrv.URL,
		Token:        "test-token",
		WireCfg:      wireCfg,
		STUNServers:  []string{stunAddr, stunAddr2},
	})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	if result.TransportType != "p2p-direct" {
		t.Errorf("expected p2p-direct, got %s", result.TransportType)
	}
	if result.Dialer == nil {
		t.Fatal("expected non-nil Dialer")
	}
	defer result.Dialer.Close()

	select {
	case <-quicAccepted:
		// Success — full punch + QUIC flow works.
	case <-time.After(5 * time.Second):
		t.Fatal("home did not accept QUIC connection")
	}
}
