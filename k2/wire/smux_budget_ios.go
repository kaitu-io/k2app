//go:build ios

package wire

// Client smux receive budget on iOS (see DefaultSmuxConfig). The Network
// Extension runs under a ~50 MB jetsam limit with GOMEMLIMIT at 35 MB and
// most of that already budgeted (core/limits_ios.go), so the session
// buffer stays at 1 MB: 32 stalled streams to block it; a stream's window
// tops out at a quarter of it, so two fast streams grow at once: ~20 Mbps
// per stream at 100 ms RTT, ~10 Mbps at 200 ms.
const (
	clientRecvBuffer      = 1 << 20
	clientMinStreamWindow = 16 << 10
	clientMaxStreamWindow = 256 << 10
)
