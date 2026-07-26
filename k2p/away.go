package k2p

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"time"

	"github.com/apernet/quic-go"
	"github.com/kaitu-io/k2/safego"
	"github.com/kaitu-io/k2/wire"
	"github.com/pion/ice/v4"
)

// ConnectResult is the output of a successful Dial().
type ConnectResult struct {
	Dialer        wire.Dialer
	TransportType string // "p2p-lan", "p2p-direct", "p2p-relay", or "p2p-unknown"
}

// Dial connects to a home peer via ICE connectivity checks.
// Steps: lookup home -> exchange ICE candidates via signal -> QUIC dial over ice.Conn.
func Dial(ctx context.Context, cfg Config) (*ConnectResult, error) {
	startTime := time.Now()

	// 1. Lookup Home.
	signal := NewSignalClient(cfg.SignalServer, cfg.Token)
	homeInfo, err := signal.Lookup(ctx, cfg.HomeUDID)
	if err != nil {
		return nil, fmt.Errorf("k2p: away: lookup: %w", err)
	}

	// 2. Apply TURN credentials from lookup.
	if homeInfo.TURN != nil {
		cfg.TURNURLs = homeInfo.TURN.URLs
		cfg.TURNUsername = homeInfo.TURN.Username
		cfg.TURNPassword = homeInfo.TURN.Password
	}

	// 3. Create ICE agent.
	agent, err := NewICEAgent(cfg)
	if err != nil {
		return nil, fmt.Errorf("k2p: away: create ICE agent: %w", err)
	}

	// 4. Open WS for candidate exchange.
	exchange, err := signal.ConnectHome(ctx, cfg.HomeUDID)
	if err != nil {
		agent.Close()
		return nil, fmt.Errorf("k2p: away: connect signal: %w", err)
	}
	defer exchange.Close() // idempotent via sync.Once — ensures cleanup on all error paths

	// 5. Wire ICE candidate callbacks to WS.
	agent.OnCandidate(func(c ice.Candidate) {
		if c != nil {
			exchange.SendCandidate(c.Marshal())
		}
	})

	// 6. Start receiving remote candidates from Home.
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

	// 7. Send our credentials.
	ufrag, pwd, _ := agent.GetLocalUserCredentials()
	exchange.SendCredentials(ufrag, pwd)

	// 8. Gather candidates.
	if gatherErr := agent.GatherCandidates(); gatherErr != nil {
		agent.Close()
		return nil, fmt.Errorf("k2p: away: gather candidates: %w", gatherErr)
	}

	// 9. Wait for Home's credentials, then ICE Dial.
	homeUfrag, homePwd, err := waitForCredentials(ctx, exchange)
	if err != nil {
		agent.Close()
		return nil, fmt.Errorf("k2p: away: wait for credentials: %w", err)
	}
	iceConn, err := agent.Dial(ctx, homeUfrag, homePwd)
	if err != nil {
		agent.Close()
		return nil, fmt.Errorf("k2p: away: ICE dial: %w", err)
	}

	// 10. QUIC over ICE.
	// Agent ownership transfers to icePacketConn — closed when transport closes.
	pc := newICEPacketConn(iceConn, agent)
	tr := &quic.Transport{Conn: pc}
	tlsCfg := buildPinTLSConfig(cfg)
	peerAddr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1} // synthetic — ICE handles routing
	quicConn, err := tr.Dial(ctx, peerAddr, tlsCfg, wire.DefaultQUICConfig())
	if err != nil {
		tr.Close() // closes pc → closes agent
		return nil, fmt.Errorf("k2p: away: QUIC dial: %w", err)
	}

	// 11. Peer auth.
	if err := doPeerAuth(ctx, quicConn, cfg.PeerSecret); err != nil {
		quicConn.CloseWithError(1, "auth failed")
		tr.Close()
		return nil, err
	}

	// 12. Detect transport type.
	transportType := detectTransportType(agent)

	slog.Info("DIAG: p2p-connected",
		"peer", cfg.HomeUDID,
		"method", transportType,
		"dialMs", time.Since(startTime).Milliseconds())

	return &ConnectResult{
		Dialer:        wire.NewQUICClientFromConn(quicConn, tr, cfg.WireCfg),
		TransportType: transportType,
	}, nil
}

// detectTransportType determines the connectivity method from the selected ICE candidate pair.
func detectTransportType(agent *ice.Agent) string {
	pair, err := agent.GetSelectedCandidatePair()
	if err != nil || pair == nil {
		return "p2p-unknown"
	}
	switch pair.Local.Type() {
	case ice.CandidateTypeHost:
		return "p2p-lan"
	case ice.CandidateTypeServerReflexive:
		return "p2p-direct"
	case ice.CandidateTypeRelay:
		return "p2p-relay"
	default:
		return "p2p-unknown"
	}
}
