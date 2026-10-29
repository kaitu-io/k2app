//go:build ios || android

package wire

// Mobile QUIC flow control — iOS NE has ~50 MB jetsam limit.
// Connection window (8 MB) is the hard cap on total buffered data.
// Stream window (2 MB) prevents a single stream from monopolizing.
// 8 MB supports ~4 concurrent video streams (YouTube Shorts prefetch)
// while staying under iOS NE jetsam (~50 MB total, Go runtime ~20-30 MB).
//
// THESE NO LONGER APPLY TO THE FINGERPRINTED CLIENT QUIC LEG. kuic makes the
// ClientHelloSpec's transport parameters authoritative for the wire, so what
// that leg advertises is what buildChromeH3Spec sends — Chrome's 6 MB stream /
// 15 MB connection. The values here still govern the server side and any
// unfingerprinted dial.
//
// That roughly doubles the worst-case buffered-data cap on iOS NE against a
// jetsam limit these numbers were chosen for. It was accepted deliberately, to
// keep the transport parameters indistinguishable from Chrome's, and it is the
// first thing to measure on a real device. If jetsam kills show up, the
// narrowest fix is to give buildChromeH3Spec these windows instead of Chrome's
// — losing fidelity on three parameters while keeping the rest.
const (
	quicStreamWindow     = 2 << 20 // 2 MB per stream
	quicConnectionWindow = 8 << 20 // 8 MB aggregate (worst-case memory cap)
	quicMaxStreams        = 64
)
