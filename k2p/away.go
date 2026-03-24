package k2p

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"
	"time"

	"github.com/apernet/quic-go"
	"github.com/kaitu-io/k2/wire"
)

// ConnectResult is the output of a successful Dial().
type ConnectResult struct {
	Dialer        wire.Dialer
	TransportType string // "p2p-direct" or "p2p-relay"
}

// Dial connects to a home peer via NAT traversal.
// Steps: STUN probe → classify NAT → lookup home → signal connect → punch → QUIC dial.
// Falls back to relay if punch fails and SignalServer is set.
func Dial(ctx context.Context, cfg Config) (*ConnectResult, error) {
	startTime := time.Now()

	stunServers := cfg.STUNServers
	if len(stunServers) == 0 {
		stunServers = DefaultSTUNServers()
	}

	// 1. STUN probe + NAT classification.
	natResult, err := ClassifyNAT(ctx, stunServers)
	if err != nil {
		return nil, fmt.Errorf("k2p: away: NAT classification: %w", err)
	}
	slog.Info("DIAG: nat-classify",
		"type", natResult.Type.String(),
		"reflexive", natResult.Reflexive.String(),
		"serversUsed", 2)

	// 2. Lookup home peer.
	signal := NewSignalClient(cfg.SignalServer, cfg.Token)
	homeInfo, err := signal.Lookup(ctx, cfg.HomeUDID)
	if err != nil {
		return nil, fmt.Errorf("k2p: away: lookup home: %w", err)
	}

	homeAddr, err := net.ResolveUDPAddr("udp4", homeInfo.Addr)
	if err != nil {
		return nil, fmt.Errorf("k2p: away: resolve home addr: %w", err)
	}

	// 3. Signal connect request (so API notifies home with our address).
	err = signal.RequestConnect(ctx, ConnectRequest{
		HomeUDID: cfg.HomeUDID,
		AwayAddr: natResult.Reflexive.String(),
		NATType:  natResult.Type.String(),
	})
	if err != nil {
		slog.Warn("k2p: away: connect request failed (proceeding anyway)", "err", err)
	}

	// 4. Punch.
	remoteNAT := parseNATType(homeInfo.NATType)
	punchResult, punchErr := Punch(ctx, natResult.Type, remoteNAT, natResult.Local, homeAddr)
	if punchErr != nil {
		// 4b. Relay fallback.
		if cfg.SignalServer == "" {
			return nil, fmt.Errorf("k2p: away: punch failed, no relay: %w", punchErr)
		}
		slog.Warn("DIAG: p2p-relay-fallback",
			"peer", cfg.HomeUDID,
			"relayServer", cfg.SignalServer,
			"err", punchErr)

		relayURL := strings.Replace(cfg.SignalServer, "https://", "wss://", 1) +
			"/k2p/relay?udid=" + cfg.HomeUDID + "&role=away"
		relayConn, err := DialRelay(relayURL)
		if err != nil {
			return nil, fmt.Errorf("k2p: relay dial: %w (punch: %v)", err, punchErr)
		}
		dialer, err := dialQUICOverRelay(ctx, relayConn, cfg)
		if err != nil {
			relayConn.Close()
			return nil, fmt.Errorf("k2p: QUIC over relay: %w (punch: %v)", err, punchErr)
		}

		slog.Info("DIAG: p2p-connected",
			"peer", cfg.HomeUDID,
			"method", "relay",
			"dialMs", time.Since(startTime).Milliseconds())

		return &ConnectResult{Dialer: dialer, TransportType: "p2p-relay"}, nil
	}

	// 5. QUIC dial over punched hole.
	dialer, err := dialQUICOverPunch(ctx, punchResult, cfg)
	if err != nil {
		punchResult.Conn.Close()
		return nil, fmt.Errorf("k2p: away: QUIC dial: %w", err)
	}

	slog.Info("DIAG: p2p-connected",
		"peer", punchResult.PeerAddr.String(),
		"method", punchResult.Strategy,
		"dialMs", time.Since(startTime).Milliseconds())

	return &ConnectResult{
		Dialer:        dialer,
		TransportType: "p2p-direct",
	}, nil
}

// dialQUICOverPunch establishes a QUIC connection over a punched UDP socket.
func dialQUICOverPunch(ctx context.Context, punch *PunchResult, cfg Config) (wire.Dialer, error) {
	tr := &quic.Transport{Conn: punch.Conn}

	tlsCfg := buildPinTLSConfig(cfg)
	quicCfg := wire.DefaultQUICConfig()
	conn, err := tr.Dial(ctx, punch.PeerAddr, tlsCfg, quicCfg)
	if err != nil {
		tr.Close()
		return nil, fmt.Errorf("k2p: QUIC dial: %w", err)
	}

	if err := doPeerAuth(ctx, conn, cfg.PeerSecret); err != nil {
		conn.CloseWithError(1, "auth failed")
		tr.Close()
		return nil, err
	}

	return wire.NewQUICClientFromConn(conn, tr, cfg.WireCfg, cfg.TicketStore), nil
}

// dialQUICOverRelay establishes a QUIC connection over a relay WebSocket.
func dialQUICOverRelay(ctx context.Context, relay *RelayConn, cfg Config) (wire.Dialer, error) {
	tr := &quic.Transport{Conn: relay}

	tlsCfg := buildPinTLSConfig(cfg)
	quicCfg := wire.DefaultQUICConfig()

	// Synthetic peerAddr — RelayConn.WriteTo ignores addr.
	peerAddr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1}
	conn, err := tr.Dial(ctx, peerAddr, tlsCfg, quicCfg)
	if err != nil {
		tr.Close()
		return nil, fmt.Errorf("k2p: QUIC dial over relay: %w", err)
	}

	if err := doPeerAuth(ctx, conn, cfg.PeerSecret); err != nil {
		conn.CloseWithError(1, "auth failed")
		tr.Close()
		return nil, err
	}

	return wire.NewQUICClientFromConn(conn, tr, cfg.WireCfg, cfg.TicketStore), nil
}

// buildPinTLSConfig creates a TLS config with optional certificate pin verification.
// If cfg.Pin is empty, InsecureSkipVerify is true (self-signed Home cert accepted).
// If cfg.Pin is set, the peer certificate's SPKI hash must match one of the pins.
func buildPinTLSConfig(cfg Config) *tls.Config {
	tlsCfg := &tls.Config{
		NextProtos:         []string{"h3"},
		InsecureSkipVerify: true, // skip CA chain (self-signed Home cert)
	}
	if cfg.Pin != "" {
		expectedPins, err := wire.ParsePins(cfg.Pin)
		if err == nil && len(expectedPins) > 0 {
			tlsCfg.VerifyPeerCertificate = func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
				return verifyPins(rawCerts, expectedPins)
			}
		}
	}
	return tlsCfg
}

// verifyPins checks that at least one certificate's SPKI hash matches an expected pin.
func verifyPins(rawCerts [][]byte, expected [][32]byte) error {
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
		for _, exp := range expected {
			if hash == exp {
				return nil
			}
		}
	}
	return fmt.Errorf("k2p: no certificate matched pin")
}

// doPeerAuth sends a peer auth frame on a new QUIC stream and reads the response.
// No-op if secret is empty.
func doPeerAuth(ctx context.Context, conn *quic.Conn, secret string) error {
	if secret == "" {
		return nil
	}

	authStream, err := conn.OpenStreamSync(ctx)
	if err != nil {
		return fmt.Errorf("k2p: open auth stream: %w", err)
	}
	if err := WriteAuthFrame(authStream, secret); err != nil {
		authStream.Close()
		return fmt.Errorf("k2p: write auth: %w", err)
	}
	resp := make([]byte, 1)
	if _, err := io.ReadFull(authStream, resp); err != nil {
		authStream.Close()
		return fmt.Errorf("k2p: read auth response: %w", err)
	}
	authStream.Close()
	if resp[0] != AuthOK {
		return fmt.Errorf("k2p: peer auth rejected by Home")
	}
	return nil
}
