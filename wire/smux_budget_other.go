//go:build !ios

package wire

// Client smux receive budget off iOS (see DefaultSmuxConfig): 8 MB per
// session, 64 stalled streams to block it, ~160 Mbps per stream at 200 ms
// RTT.
const (
	clientRecvBuffer      = 8 << 20
	clientMinStreamWindow = 64 << 10
	clientMaxStreamWindow = 4 << 20
)
