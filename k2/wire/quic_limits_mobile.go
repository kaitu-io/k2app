//go:build ios || android

package wire

// Mobile QUIC flow control — iOS NE has ~50 MB jetsam limit.
// Connection window (8 MB) is the hard cap on total buffered data.
// Stream window (2 MB) prevents a single stream from monopolizing.
// 8 MB supports ~4 concurrent video streams (YouTube Shorts prefetch)
// while staying under iOS NE jetsam (~50 MB total, Go runtime ~20-30 MB).
//
// The fingerprinted client QUIC leg advertises quicSpec* below, not the values
// above, because kuic makes the ClientHelloSpec's transport parameters
// authoritative for the wire. Chrome's 6 MB stream / 15 MB connection would
// hand the server more credit than our local flow controller accepts (2 MB /
// 8 MB), so a server burst past 2 MB unread was rejected locally with
// "FLOW_CONTROL_ERROR (local): received N bytes on stream S, allowed 2097152"
// and killed the whole connection (every proxy stream and the UDP mux).
// Mobile therefore advertises exactly what it enforces — this loses fidelity
// on three transport parameters (initial_max_stream_data_*, initial_max_data)
// and keeps the rest of the Chrome parrot.
const (
	quicSpecStreamWindow     = quicStreamWindow
	quicSpecConnectionWindow = quicConnectionWindow
)

const (
	quicStreamWindow     = 2 << 20 // 2 MB per stream
	quicConnectionWindow = 8 << 20 // 8 MB aggregate (worst-case memory cap)
	quicMaxStreams        = 64
)
