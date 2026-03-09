//go:build ios || android

package wire

// Mobile QUIC flow control — iOS NE has ~50 MB jetsam limit.
// Connection window (8 MB) is the hard cap on total buffered data.
// Stream window (2 MB) prevents a single stream from monopolizing.
// 8 MB supports ~4 concurrent video streams (YouTube Shorts prefetch)
// while staying under iOS NE jetsam (~50 MB total, Go runtime ~20-30 MB).
const (
	quicStreamWindow     = 2 << 20 // 2 MB per stream
	quicConnectionWindow = 8 << 20 // 8 MB aggregate (worst-case memory cap)
	quicMaxStreams        = 64
)
