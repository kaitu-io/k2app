package core

import (
	"context"
	"net"
)

// Outbound dials a remote endpoint for a routed connection.
// Implementations: directOutbound (net.Dial), wireOutbound (wire.Dialer wrapper).
// Reject = nil entry in outbound table (tunnel drops silently).
type Outbound interface {
	// DialTCP dials a TCP connection to dest. earlyData is sent alongside the
	// wire stream header (wireOutbound) or ignored (directOutbound).
	DialTCP(ctx context.Context, dest string, earlyData []byte) (net.Conn, error)

	// DialUDP dials a UDP path to dest. Returns destAddr for unconnected
	// PacketConns (direct: WriteTo target) or nil for pre-connected ones (wire).
	DialUDP(ctx context.Context, dest string) (conn net.PacketConn, destAddr net.Addr, err error)

	// Name returns a human-readable label for logging (e.g. "direct", "k2v5", "k2p-home").
	Name() string

	// Close releases resources owned by this outbound.
	Close() error
}
