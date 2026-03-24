package k2p2p

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"time"

	"github.com/kaitu-io/k2/safego"
)

const (
	punchMagic    = "k2p2p-punch"
	punchInterval = 100 * time.Millisecond
	punchTimeout  = 5 * time.Second
)

// punchStandard sends UDP punch packets to peerAddr from conn at regular
// intervals while simultaneously listening for incoming punch packets.
// Returns the confirmed peer address on success.
func punchStandard(ctx context.Context, conn *net.UDPConn, peerAddr *net.UDPAddr) (*net.UDPAddr, error) {
	ctx, cancel := context.WithTimeout(ctx, punchTimeout)
	defer cancel()

	// Send punch packets in background.
	safego.Go(func() {
		ticker := time.NewTicker(punchInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				conn.WriteToUDP([]byte(punchMagic), peerAddr)
			}
		}
	})

	buf := make([]byte, 64)
	for {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("k2p2p: punch timeout: %w", ctx.Err())
		}
		conn.SetReadDeadline(time.Now().Add(punchInterval * 2))
		n, from, err := conn.ReadFromUDP(buf)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			return nil, fmt.Errorf("k2p2p: punch read: %w", err)
		}
		if n >= len(punchMagic) && string(buf[:len(punchMagic)]) == punchMagic {
			slog.Info("DIAG: punch-ok", "from", from.String(), "method", "standard")
			return from, nil
		}
	}
}
