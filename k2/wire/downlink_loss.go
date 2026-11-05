package wire

import (
	"time"

	"github.com/sasha-s/go-deadlock"
)

// Downlink (server→client) loss estimation for the QUIC client.
//
// Why: k2cc's LossRate is measured by the client's own SENDER, i.e. it is
// uplink loss, and it only moves when a monitor interval completes. A client
// that mostly downloads sends nearly nothing, so under a path that drops 60%
// of the server's packets no MI completes and LossRate keeps reading 0 — the
// health grade never leaves healthy and the selector's quality path never
// starts (PVE netem, 2026-11-05: 60% UDP loss, bulk download, 0 MIs completed
// after the fault; the only escape was the echo re-race at +24s, and a
// light-traffic user was never moved off QUIC in 120s).
//
// OBSERVABILITY ONLY (heartbeat downLoss). It is deliberately not a health
// or channel-selection input: production cc-summary (2026-11-05) showed users
// at 27-37% sustained downlink loss rating every session good — k2cc serves
// that loss fine — so "high loss ⇒ switch" would force reconnects and a
// TCP-WS move on working sessions. The field data this produces is what a
// future, outcome-based switch rule should be designed from.
//
// kuic counts what the server sent in the 1-RTT packet-number space against
// what arrived (ConnectionStats.PacketNumberSpan1RTT / PacketsReceived1RTT,
// see third_party/kuic/u_recv_pn_stats.go). This window turns those
// cumulative per-connection counters into a recent rate.
const (
	// downlinkLossMinWindow is the shortest window a rate is computed over.
	downlinkLossMinWindow = 2 * time.Second
	// downlinkLossMinSpan is the minimum number of server packet numbers a
	// window must cover before its rate is trusted. A window that has not
	// gathered it keeps growing, up to downlinkLossMaxWindow.
	downlinkLossMinSpan = 20
	// downlinkLossMaxWindow bounds how long a sparse window may grow. Past it
	// with fewer than downlinkLossMinSpan packets there is no evidence either
	// way and the rate reads 0 ("no evidence" is never "lossy").
	downlinkLossMaxWindow = 10 * time.Second
)

// downlinkLossWindow is a field-only state holder (all methods are
// microsecond critical sections, no I/O). The caller reads the counters from
// the connection OUTSIDE any lock and passes them in with an identity for the
// connection they came from.
type downlinkLossWindow struct {
	mu deadlock.Mutex

	connID   any // identity of the connection the base belongs to
	baseAt   time.Time
	baseRcv  uint64
	baseSpan uint64
	rate     float64 // last computed rate, valid while rateOK
	rateOK   bool
}

// observe feeds one reading of the cumulative counters of connection connID
// and returns the current downlink loss estimate in [0,1] (0 = lossless or
// no evidence).
func (w *downlinkLossWindow) observe(connID any, received, span uint64, now time.Time) float64 {
	w.mu.Lock()
	r := w.stepLocked(connID, received, span, now)
	w.mu.Unlock()
	return r
}

// stepLocked is observe's body; caller holds w.mu. Field-only.
func (w *downlinkLossWindow) stepLocked(connID any, received, span uint64, now time.Time) float64 {
	// New connection (or counters went backwards, which only a new
	// connection can do): its counters start from zero, so the old base
	// means nothing. The previous connection's rate does not describe the
	// new one either.
	if connID == nil || connID != w.connID || received < w.baseRcv || span < w.baseSpan {
		w.connID = connID
		w.baseAt, w.baseRcv, w.baseSpan = now, received, span
		w.rate, w.rateOK = 0, false
		return 0
	}

	elapsed := now.Sub(w.baseAt)
	dSpan := span - w.baseSpan
	dRcv := received - w.baseRcv
	switch {
	case elapsed >= downlinkLossMinWindow && dSpan >= downlinkLossMinSpan:
		r := 0.0
		if dRcv < dSpan {
			r = 1 - float64(dRcv)/float64(dSpan)
		}
		w.rate, w.rateOK = r, true
		w.baseAt, w.baseRcv, w.baseSpan = now, received, span
	case elapsed >= downlinkLossMaxWindow:
		// Too little traffic to judge: no evidence.
		w.rate, w.rateOK = 0, false
		w.baseAt, w.baseRcv, w.baseSpan = now, received, span
	}
	if !w.rateOK {
		return 0
	}
	return w.rate
}
