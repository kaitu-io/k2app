package k2cc

import (
	"fmt"
	"math"
	"sync/atomic"
	"time"
)

// ============================================================================
// CCSummary — cumulative per-sender observability counters
// ============================================================================
//
// Everything here is cumulative since the sender was created and is fed once
// per completed Monitor Interval from processCompletedMI (already under p.mu,
// so no extra locking on the hot path). Consumers keep their own previous copy
// and call Sub() to get a window; the accumulator itself never resets.
//
// The struct holds only fixed-size arrays and scalars — a value copy is a
// complete snapshot, safe to hand across goroutines. ~450 B per sender.

const (
	lossHistBuckets = 10
	rttHistBuckets  = 12
	rateHistBuckets = 12
)

// Histogram bucket edges. A sample lands in the FIRST bucket whose edge is
// >= the value, so a percentile reads as "at most this edge".
var (
	// lossHistEdges is in percent. LossRate() is clamped to [0,1], so the last
	// edge (100) always catches — the loss histogram has no overflow bucket.
	lossHistEdges = [lossHistBuckets]float64{0, 0.5, 1, 2, 5, 10, 20, 35, 50, 100}
	// rttHistEdges is in milliseconds; the last bucket is open.
	rttHistEdges = [rttHistBuckets]float64{10, 20, 35, 50, 75, 100, 150, 200, 300, 500, 1000, math.Inf(1)}
	// rateHistEdges is in Mbps, shared by RateHist (TargetRate) and TputHist
	// (acked throughput); the last bucket is open.
	rateHistEdges = [rateHistBuckets]float64{1, 2, 5, 10, 20, 30, 50, 100, 200, 300, 500, math.Inf(1)}
)

// senderSeq hands out CCSummary.SenderID epochs. 0 is reserved for "no
// sender" (TCP-WS fallback, connection not yet established).
var senderSeq atomic.Uint64

// CCSummary is the cumulative observability record of one K2CCSender.
type CCSummary struct {
	// SenderID is the epoch: a new sender (client reconnect, new server
	// connection) gets a new ID, and Sub() treats a prev with a different
	// SenderID as zero. 0 = no sender.
	SenderID uint64

	// MI counters. SlowStartMIs + DecisionMIs + RateAdjMIs == MIs always
	// (mode residency is sampled BEFORE the MI is processed).
	MIs           uint64 // completed MIs, including app-limited ones
	AppLimitedMIs uint64 // MIs that took the app-limited anchor path
	SlowStartMIs  uint64
	DecisionMIs   uint64
	RateAdjMIs    uint64
	BWAnchoredMIs uint64 // MIs handled by processBWAnchored
	FloorMIs      uint64 // MIs after which CurrentRate sat on a floor (1M anchored / 5M PCC)

	// Event counters.
	BWDrops              uint64 // bw-anchor "BW drop detected"
	BWRises              uint64 // bw-anchor "BW rise detected"
	SlowStartSaturations uint64 // slow start exit condition 3
	RateProbes           uint64 // periodic +10% probes actually applied

	// Byte totals over completed MIs. BytesLost/BytesSent is the aggregate
	// loss; BytesAcked can slightly exceed BytesSent inside a short window
	// because ACKs are not packet-number filtered at MI boundaries (spec §12.3).
	BytesSent  uint64
	BytesAcked uint64
	BytesLost  uint64

	// Per-MI histograms (see the edge tables above).
	LossHist [lossHistBuckets]uint64 // LossRate() in percent
	RTTHist  [rttHistBuckets]uint64  // AvgRTT() in ms; MIs with no RTT sample are skipped
	RateHist [rateHistBuckets]uint64 // TargetRate in Mbps
	TputHist [rateHistBuckets]uint64 // Throughput() in Mbps (0 lands in the first bucket)

	// Exact maxima (Sub keeps the receiver's value).
	LossMax float64 // fraction [0,1]
	TputMax float64 // bps

	// Current values, filled by Summary() from live state (Sub keeps the
	// receiver's value).
	BWEst   float64 // bps, background-loss corrected; 0 when the estimator is not valid
	BGLoss  float64 // fraction
	Epsilon float64
	MinRTT  time.Duration
	Mode    K2CCMode
}

func satSub(a, b uint64) uint64 {
	if a < b {
		return 0
	}
	return a - b
}

func subHist(cur, prev []uint64) {
	for i := range cur {
		cur[i] = satSub(cur[i], prev[i])
	}
}

// Sub returns the window between prev and s: counters and histograms are
// subtracted (saturating), maxima and current values are the receiver's.
// A prev from a different SenderID (or the zero value) is treated as zero,
// so the first window after a reconnect is the new sender's whole life.
func (s CCSummary) Sub(prev CCSummary) CCSummary {
	if prev.SenderID != s.SenderID {
		prev = CCSummary{}
	}
	out := s
	out.MIs = satSub(s.MIs, prev.MIs)
	out.AppLimitedMIs = satSub(s.AppLimitedMIs, prev.AppLimitedMIs)
	out.SlowStartMIs = satSub(s.SlowStartMIs, prev.SlowStartMIs)
	out.DecisionMIs = satSub(s.DecisionMIs, prev.DecisionMIs)
	out.RateAdjMIs = satSub(s.RateAdjMIs, prev.RateAdjMIs)
	out.BWAnchoredMIs = satSub(s.BWAnchoredMIs, prev.BWAnchoredMIs)
	out.FloorMIs = satSub(s.FloorMIs, prev.FloorMIs)
	out.BWDrops = satSub(s.BWDrops, prev.BWDrops)
	out.BWRises = satSub(s.BWRises, prev.BWRises)
	out.SlowStartSaturations = satSub(s.SlowStartSaturations, prev.SlowStartSaturations)
	out.RateProbes = satSub(s.RateProbes, prev.RateProbes)
	out.BytesSent = satSub(s.BytesSent, prev.BytesSent)
	out.BytesAcked = satSub(s.BytesAcked, prev.BytesAcked)
	out.BytesLost = satSub(s.BytesLost, prev.BytesLost)
	subHist(out.LossHist[:], prev.LossHist[:])
	subHist(out.RTTHist[:], prev.RTTHist[:])
	subHist(out.RateHist[:], prev.RateHist[:])
	subHist(out.TputHist[:], prev.TputHist[:])
	return out
}

// histAdd counts v into the first bucket whose edge is >= v.
func histAdd(counts []uint64, edges []float64, v float64) {
	for i, e := range edges {
		if v <= e {
			counts[i]++
			return
		}
	}
	counts[len(counts)-1]++
}

// histPercentile returns the upper edge of the bucket holding the ⌈p·n⌉-th
// sample — a conservative "p-quantile is at most edge". overflow=true means
// that sample sits in the open +Inf bucket; edge is then the previous edge
// and should be rendered as ">edge". An empty histogram returns (0,false).
func histPercentile(counts []uint64, edges []float64, p float64) (edge float64, overflow bool) {
	var n uint64
	for _, c := range counts {
		n += c
	}
	if n == 0 {
		return 0, false
	}
	target := uint64(math.Ceil(p * float64(n)))
	if target < 1 {
		target = 1
	}
	var cum uint64
	for i, c := range counts {
		cum += c
		if cum >= target {
			if math.IsInf(edges[i], 1) {
				return edges[i-1], true
			}
			return edges[i], false
		}
	}
	return edges[len(edges)-1], false // unreachable: cum == n >= target
}

// LossPercentile returns the upper edge (percent) of the bucket holding the
// p-quantile of per-MI loss.
func (s CCSummary) LossPercentile(p float64) (float64, bool) {
	return histPercentile(s.LossHist[:], lossHistEdges[:], p)
}

// RTTPercentile returns the upper edge (ms) of the bucket holding the
// p-quantile of per-MI average RTT.
func (s CCSummary) RTTPercentile(p float64) (float64, bool) {
	return histPercentile(s.RTTHist[:], rttHistEdges[:], p)
}

// RatePercentile returns the upper edge (Mbps) of the bucket holding the
// p-quantile of per-MI target rate.
func (s CCSummary) RatePercentile(p float64) (float64, bool) {
	return histPercentile(s.RateHist[:], rateHistEdges[:], p)
}

// TputPercentile returns the upper edge (Mbps) of the bucket holding the
// p-quantile of per-MI acked throughput.
func (s CCSummary) TputPercentile(p float64) (float64, bool) {
	return histPercentile(s.TputHist[:], rateHistEdges[:], p)
}

// FormatBucket renders a percentile result for a log line: "10", "0.5", ">500".
func FormatBucket(edge float64, overflow bool) string {
	if overflow {
		return fmt.Sprintf(">%.0f", edge)
	}
	if edge != math.Trunc(edge) {
		return fmt.Sprintf("%.1f", edge)
	}
	return fmt.Sprintf("%.0f", edge)
}
