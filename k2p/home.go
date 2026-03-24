package k2p

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"time"

	"github.com/apernet/quic-go"
	"github.com/sasha-s/go-deadlock"

	"github.com/kaitu-io/k2/safego"
	"github.com/kaitu-io/k2/wire"
)

// Listen starts the home peer: registers with signal server, waits for
// away peers, punches holes, and accepts QUIC connections.
//
// Returns a channel of wire.Listener — one per successfully punched connection.
// Channel is closed when ctx is cancelled.
func Listen(ctx context.Context, cfg Config) (<-chan wire.Listener, error) {
	stunServers := cfg.STUNServers
	if len(stunServers) == 0 {
		stunServers = DefaultSTUNServers()
	}

	// 1. STUN probe + NAT classification.
	natResult, err := ClassifyNAT(ctx, stunServers)
	if err != nil {
		return nil, fmt.Errorf("k2p: home: NAT classification: %w", err)
	}
	slog.Info("DIAG: nat-classify",
		"type", natResult.Type.String(),
		"reflexive", natResult.Reflexive.String(),
		"serversUsed", 2)

	// 2. Register with signal server.
	signal := NewSignalClient(cfg.SignalServer, cfg.RegToken)
	err = signal.Register(ctx, RegisterRequest{
		UDID:     cfg.HomeUDID,
		Addr:     natResult.Reflexive.String(),
		NATType:  natResult.Type.String(),
		Networks: cfg.Networks,
		DNS:      cfg.DNS,
	})
	if err != nil {
		return nil, fmt.Errorf("k2p: home: register: %w", err)
	}

	// Protect natResult from concurrent access.
	var natMu deadlock.Mutex
	getNAT := func() NATResult {
		natMu.Lock()
		defer natMu.Unlock()
		return natResult
	}

	// 3. Keepalive: re-register every 25s.
	safego.Go(func() {
		ticker := time.NewTicker(25 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if nr, err := ClassifyNAT(ctx, stunServers); err == nil {
					natMu.Lock()
					natResult = nr
					natMu.Unlock()
				}
				nr := getNAT()
				_ = signal.Register(ctx, RegisterRequest{
					UDID:     cfg.HomeUDID,
					Addr:     nr.Reflexive.String(),
					NATType:  nr.Type.String(),
					Networks: cfg.Networks,
					DNS:      cfg.DNS,
				})
			}
		}
	})

	// 4. Listen for punch notifications.
	notifyCh, err := signal.ListenNotify(ctx, cfg.HomeUDID)
	if err != nil {
		return nil, fmt.Errorf("k2p: home: listen notify: %w", err)
	}

	listenerCh := make(chan wire.Listener, 4)

	safego.Go(func() {
		defer close(listenerCh)
		for {
			select {
			case <-ctx.Done():
				return
			case notify, ok := <-notifyCh:
				if !ok {
					return
				}
				safego.Go(func() {
					listener, err := handleAwayConnection(ctx, cfg, getNAT(), notify)
					if err != nil {
						slog.Warn("DIAG: punch-fail", "away", notify.AwayAddr, "err", err)
						return
					}
					select {
					case listenerCh <- listener:
					case <-ctx.Done():
						listener.Close()
					}
				})
			}
		}
	})

	return listenerCh, nil
}

// handleAwayConnection processes a single away peer: punch -> QUIC accept -> wire.Listener.
func handleAwayConnection(ctx context.Context, cfg Config, localNAT NATResult, notify PunchNotify) (wire.Listener, error) {
	awayAddr, err := net.ResolveUDPAddr("udp4", notify.AwayAddr)
	if err != nil {
		return nil, fmt.Errorf("k2p: resolve away addr: %w", err)
	}

	remoteNAT := parseNATType(notify.NATType)
	punchResult, err := Punch(ctx, localNAT.Type, remoteNAT, localNAT.Local, awayAddr)
	if err != nil {
		return nil, fmt.Errorf("k2p: punch: %w", err)
	}

	// Create QUIC listener on the punched UDP socket.
	tr := &quic.Transport{Conn: punchResult.Conn}

	var tlsCert tls.Certificate
	if cfg.TLSCert != nil {
		tlsCert = *cfg.TLSCert
	} else {
		certPair, certErr := wire.GenerateCertPair()
		if certErr != nil {
			punchResult.Conn.Close()
			return nil, fmt.Errorf("k2p: generate cert: %w", certErr)
		}
		tlsCert = certPair.EC
	}

	tlsCfg := &tls.Config{
		NextProtos:   []string{"h3"},
		Certificates: []tls.Certificate{tlsCert},
	}

	quicLn, err := tr.Listen(tlsCfg, wire.DefaultQUICConfig())
	if err != nil {
		punchResult.Conn.Close()
		return nil, fmt.Errorf("k2p: QUIC listen: %w", err)
	}

	slog.Info("DIAG: p2p-connected",
		"peer", punchResult.PeerAddr.String(),
		"method", punchResult.Strategy)

	// Peer auth must happen at the QUIC connection level, before wrapping in
	// QUICServer. QUICServer expects k2v5 protocol streams (H3 frame + StreamHeader),
	// but the auth protocol uses its own frame format. We accept the QUIC
	// connection, verify auth on the first stream, then hand the authenticated
	// connection to QUICServer for k2v5 stream processing.
	if cfg.SecretHash != "" {
		conn, err := quicLn.Accept(ctx)
		if err != nil {
			quicLn.Close()
			return nil, fmt.Errorf("k2p: accept QUIC for auth: %w", err)
		}
		stream, err := conn.AcceptStream(ctx)
		if err != nil {
			conn.CloseWithError(1, "auth stream failed")
			quicLn.Close()
			return nil, fmt.Errorf("k2p: accept auth stream: %w", err)
		}
		secret, err := ReadAuthFrame(stream)
		if err != nil {
			stream.Close()
			conn.CloseWithError(1, "auth read failed")
			quicLn.Close()
			return nil, fmt.Errorf("k2p: read auth: %w", err)
		}
		if !VerifySecret(secret, cfg.SecretHash) {
			stream.Write([]byte{AuthRejected}) //nolint:errcheck
			stream.Close()
			conn.CloseWithError(1, "auth rejected")
			quicLn.Close()
			return nil, fmt.Errorf("k2p: peer auth rejected")
		}
		stream.Write([]byte{AuthOK}) //nolint:errcheck
		stream.Close()
		// Auth passed — hand the authenticated connection to QUICServer.
		// QUICServer will handle subsequent k2v5 streams on this connection
		// and also accept new connections from the listener.
		return wire.NewQUICServerFromConn(conn, quicLn, cfg.TicketStore), nil
	}

	return wire.NewQUICServerFromListener(quicLn, cfg.TicketStore), nil
}
