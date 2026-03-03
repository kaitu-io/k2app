//go:build ios || android

package wire

// Mobile QUIC flow control — iOS NE has ~50 MB jetsam limit.
// Connection window (4 MB) is the hard cap on total buffered data.
// Stream window (2 MB) prevents a single stream from monopolizing.
const (
	quicStreamWindow     = 2 << 20 // 2 MB per stream
	quicConnectionWindow = 4 << 20 // 4 MB aggregate (worst-case memory cap)
	quicMaxStreams        = 64
)
