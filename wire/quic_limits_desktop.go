//go:build (darwin && !ios) || (linux && !android) || windows

package wire

// Desktop/server QUIC flow control — large windows for high throughput.
const (
	quicStreamWindow     = 8 << 20  // 8 MB per stream
	quicConnectionWindow = 20 << 20 // 20 MB aggregate
	quicMaxStreams        = 256
)
