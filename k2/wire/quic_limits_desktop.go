//go:build (darwin && !ios) || (linux && !android) || windows

package wire

// Desktop/server QUIC flow control — large windows for high throughput.
const (
	quicStreamWindow     = 8 << 20  // 8 MB per stream
	quicConnectionWindow = 20 << 20 // 20 MB aggregate
	quicMaxStreams        = 256
)

// Windows the fingerprinted client leg advertises (buildChromeH3Spec). kuic
// makes the spec authoritative on the wire, so these are the credit the peer
// believes it has. They MUST be <= the local windows above, otherwise a peer
// that uses the credit it was granted trips our own flow-control check
// (FLOW_CONTROL_ERROR (local)) and the whole connection dies. Desktop keeps
// Chrome's 6 MB / 15 MB, which fit under 8 MB / 20 MB.
const (
	quicSpecStreamWindow     = 6291456  // Chrome: 6 MB per stream
	quicSpecConnectionWindow = 15728640 // Chrome: 15 MB aggregate
)
