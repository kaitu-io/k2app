package utils

import "sync/atomic"

// ConnectionStats stores stats for the connection. See the public
// ConnectionStats struct in connection.go for more information
type ConnectionStats struct {
	BytesSent       atomic.Uint64
	PacketsSent     atomic.Uint64
	BytesReceived   atomic.Uint64
	PacketsReceived atomic.Uint64
	BytesLost       atomic.Uint64
	PacketsLost     atomic.Uint64

	// kuic: receive-side 1-RTT packet number accounting (u_recv_pn_stats.go).
	PacketsReceived1RTT  atomic.Uint64
	PacketNumberSpan1RTT atomic.Uint64
}
