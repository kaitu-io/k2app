package k2p

import (
	"context"
	tls "github.com/metacubex/utls"
	"fmt"
	"log/slog"
	"time"

	"github.com/kaitu-io/kuic"
	"github.com/kaitu-io/k2/safego"
	"github.com/kaitu-io/k2/wire"
	"github.com/pion/ice/v4"
)

// Listen starts the home peer: registers with signal server, waits for
// away peers, performs ICE connectivity checks, and accepts QUIC connections.
//
// Returns a channel of wire.Listener -- one per successfully connected peer.
// Channel is closed when ctx is cancelled.
func Listen(ctx context.Context, cfg Config) (<-chan wire.Listener, error) {
	// 1. Register metadata.
	signal := NewSignalClient(cfg.SignalServer, cfg.RegToken)
	err := signal.Register(ctx, RegisterRequest{
		UDID:     cfg.HomeUDID,
		Networks: cfg.Networks,
		DNS:      cfg.DNS,
	})
	if err != nil {
		return nil, fmt.Errorf("k2p: home: register: %w", err)
	}

	// 2. Keepalive: re-register every 25s.
	safego.Go(func() { keepaliveLoop(ctx, signal, cfg) })

	// 3. Listen for Away sessions via WS.
	sessionCh, err := signal.ListenHome(ctx, cfg.HomeUDID)
	if err != nil {
		return nil, fmt.Errorf("k2p: home: listen: %w", err)
	}

	listenerCh := make(chan wire.Listener, 4)

	safego.Go(func() {
		defer close(listenerCh)
		for {
			select {
			case <-ctx.Done():
				return
			case exchange, ok := <-sessionCh:
				if !ok {
					return
				}
				safego.Go(func() {
					listener, err := handleAwaySession(ctx, cfg, exchange)
					if err != nil {
						slog.Warn("DIAG: ice-failed", "session", exchange.Session(), "err", err)
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

// keepaliveLoop re-registers with the signal server every 25 seconds.
func keepaliveLoop(ctx context.Context, signal *SignalClient, cfg Config) {
	ticker := time.NewTicker(25 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			signal.Register(ctx, RegisterRequest{
				UDID:     cfg.HomeUDID,
				Networks: cfg.Networks,
				DNS:      cfg.DNS,
			})
		}
	}
}

// handleAwaySession handles a single Away peer connection attempt:
// ICE candidate exchange, QUIC listen, and optional peer auth.
func handleAwaySession(ctx context.Context, cfg Config, exchange *CandidateExchange) (wire.Listener, error) {
	defer exchange.Close()

	// 1. Create ICE agent.
	agent, err := NewICEAgent(cfg)
	if err != nil {
		return nil, fmt.Errorf("k2p: home: create ICE agent: %w", err)
	}

	// 2. Wire candidates.
	agent.OnCandidate(func(c ice.Candidate) {
		if c != nil {
			exchange.SendCandidate(c.Marshal())
		}
	})

	safego.Go(func() {
		for msg := range exchange.Incoming() {
			if msg.Type == "candidate" {
				c, parseErr := ice.UnmarshalCandidate(msg.Candidate)
				if parseErr == nil {
					agent.AddRemoteCandidate(c)
				}
			}
		}
	})

	// 3. Send credentials + gather.
	ufrag, pwd, _ := agent.GetLocalUserCredentials()
	exchange.SendCredentials(ufrag, pwd)
	if gatherErr := agent.GatherCandidates(); gatherErr != nil {
		agent.Close()
		return nil, fmt.Errorf("k2p: home: gather candidates: %w", gatherErr)
	}

	// 4. Wait for Away's credentials, then ICE Accept.
	awayUfrag, awayPwd, err := waitForCredentials(ctx, exchange)
	if err != nil {
		agent.Close()
		return nil, fmt.Errorf("k2p: home: wait for credentials: %w", err)
	}
	iceConn, err := agent.Accept(ctx, awayUfrag, awayPwd)
	if err != nil {
		agent.Close()
		return nil, fmt.Errorf("k2p: home: ICE accept: %w", err)
	}

	// 5. QUIC Listen over ICE.
	// Agent ownership transfers to icePacketConn — closed when transport closes.
	pc := newICEPacketConn(iceConn, agent)
	tr := &quic.Transport{Conn: pc}

	var tlsCert tls.Certificate
	if cfg.TLSCert != nil {
		tlsCert = *cfg.TLSCert
	} else {
		certPair, certErr := wire.GenerateCertPair()
		if certErr != nil {
			tr.Close() // closes pc → closes agent
			return nil, fmt.Errorf("k2p: home: generate cert: %w", certErr)
		}
		tlsCert = certPair.EC
	}

	tlsCfg := &tls.Config{
		NextProtos:   []string{"h3"},
		Certificates: []tls.Certificate{tlsCert},
	}
	quicLn, err := tr.Listen(tlsCfg, wire.DefaultQUICConfig())
	if err != nil {
		tr.Close() // closes pc → closes agent
		return nil, fmt.Errorf("k2p: home: QUIC listen: %w", err)
	}

	transportType := detectTransportType(agent)
	slog.Info("DIAG: ice-connected", "session", exchange.Session(), "method", transportType)

	// 6. Peer auth if configured.
	// Auth is done at the raw QUIC stream level (not through QUICServer.AcceptTCP
	// which expects k2v5 framing). Accept the first QUIC connection, verify auth
	// on a dedicated stream, then hand the connection to QUICServer for data plane.
	if cfg.SecretHash != "" {
		qconn, qErr := quicLn.Accept(ctx)
		if qErr != nil {
			quicLn.Close()
			tr.Close()
			return nil, fmt.Errorf("k2p: home: accept QUIC for auth: %w", qErr)
		}
		if authErr := verifyPeerAuth(ctx, qconn, cfg.SecretHash); authErr != nil {
			qconn.CloseWithError(1, "auth failed")
			quicLn.Close()
			tr.Close()
			return nil, authErr
		}
		return wire.NewQUICServerFromConn(qconn, quicLn), nil
	}

	return wire.NewQUICServerFromListener(quicLn), nil
}
