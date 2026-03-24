package k2p

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
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
	punchResult, err := Punch(ctx, natResult.Type, remoteNAT, natResult.Local, homeAddr)
	if err != nil {
		return nil, fmt.Errorf("k2p: away: punch failed: %w", err)
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

	tlsCfg := &tls.Config{
		NextProtos:         []string{"h3"},
		ServerName:         cfg.WireCfg.Host,
		InsecureSkipVerify: cfg.WireCfg.Insecure,
	}

	quicCfg := wire.DefaultQUICConfig()
	conn, err := tr.Dial(ctx, punch.PeerAddr, tlsCfg, quicCfg)
	if err != nil {
		tr.Close()
		return nil, fmt.Errorf("k2p: QUIC dial: %w", err)
	}

	return wire.NewQUICClientFromConn(conn, tr, cfg.WireCfg, cfg.TicketStore), nil
}
