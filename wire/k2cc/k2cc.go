// K2CC Vivace - Performance-oriented Congestion Control
package k2cc

import (
	"fmt"
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/kaitu-io/kuic/congestion"
)

// ============================================================================
// K2CC Vivace - Performance-oriented Congestion Control
//
// K2CC uses gradient ascent on a utility function to find optimal sending rate:
// U = Throughput - α * LossRate * Throughput - β * ΔRtt
//
// Key features:
// - Rate-based (not window-based like Cubic)
// - Uses Monitor Intervals (MI) to probe network capacity
// - GFW mode: low α to tolerate non-congestion packet loss
//
// References:
// - K2CC Vivace (NSDI '18): https://www.usenix.org/conference/nsdi18/presentation/dong
// ============================================================================

// Constants for K2CC algorithm
const (
	// DefaultMaxRate is the upper bound for CC target rate (500 Mbps).
	DefaultMaxRate = int64(500_000_000)

	// Congestion window bounds
	arcMinWindow = congestion.ByteCount(14720)      // 10 × 1472 (typical MTU)
	arcMaxWindow = congestion.ByteCount(1_000_000_000) // 1 GB

	// Rate boundaries — two floors for different purposes:
	// arcMinRate: CC hard floor. Gradient descent can go this low. Must be low enough
	// for K2CC to converge when actual bandwidth < 30M (GFW-throttled links).
	// arcMinDownloadRate: app-limited anchor floor. Prevents rate collapse from transient
	// low throughput during idle/ramp-up. Higher to ensure fast recovery.
	arcMinRate         = 5_000_000.0  // 5 Mbps — CC convergence floor
	arcMinDownloadRate = 30_000_000.0 // 30 Mbps — app-limited anchor floor

	// Algorithm parameters
	defaultAlpha       = 10.0 // Loss penalty — standard mode
	gfwAlpha           = 4.0  // Loss penalty — GFW mode
	// gfwAlpha theoretical tolerance: PCC gradient ∝ 1 - L(1+α).
	// At L > 1/(1+α) = 20%, gradient is permanently negative → rate descends to floor.
	// BW-anchored mode bypasses gradient and can sustain higher rates above 20%,
	// but convergence depends on early bwEst quality (stochastic).
	defaultBeta        = 1.0  // Latency penalty (throughput-prioritized for tunnel)
	defaultEpsilon     = 0.12 // Probing step size
	arcRateEWMAAlpha   = 0.9  // EWMA smoothing for rate updates
	arcSlowStartFactor = 2.0  // Slow start growth multiplier
	arcCwndMultiplier  = 1.2  // BDP multiplier for cwnd (burst headroom only; K2CC paces via TimeUntilSend)

	// Rate probing: periodic recovery by probing higher rate
	rateProbingInterval = uint64(20) // Probe every N MIs
	rateProbingIncrease = 0.10       // +10% probe

	// MI completion conditions
	minMIPackets          = uint64(10)        // Minimum ACKs for standard completion
	maxMIDuration         = 60 * time.Second  // Force-complete timeout
	minMIDurationRelaxed  = 30 * time.Second  // Relaxed completion minimum duration
	minMIPacketsRelaxed   = uint64(3)         // Relaxed completion minimum ACKs

	// Slow start thresholds
	slowStartExitRatio   = 0.99 // Exit slow start when rate >= maxRate × this
	slowStartDropRatio   = 0.95 // Exit slow start when utility drops below previous × this

	// App-limited detection: BytesSent < TargetBytes × this → app-limited
	appLimitedThreshold = 0.5

	// Initial rate = maxRate × this
	initialRateFactor = 0.3

	// Initial RTT assumption (before rttStats provides real measurements)
	initialRTT = 100 * time.Millisecond

	// AIMD-ε: Adaptive Exploration Step Size
	//
	// Adapts epsilon based on utility gradient signal quality.
	// Rules:
	//   ① Direction confirmed (counter ≥ 2):  ε += δ       [additive increase]
	//   ② Weak signal reversal:               ε *= decay    [multiplicative decrease]
	//   ③ Strong signal reversal:             ε = ε₀        [fast recovery]
	//   ④ Building (counter = 1):             no change     [hold]
	epsilonMin        = 0.03  // ε₀ / 4 (convergence precision)
	epsilonMax        = 0.25  // ε₀ × 2 (exploration ceiling)
	epsilonDelta      = 0.03  // AI step
	epsilonDecay      = 0.5   // MD halving
	epsilonZThreshold = 1.645 // 95% one-sided z-score
	noiseEWMAAlpha    = 0.1   // Noise baseline smoothing (10% new)
	coldStartNoise    = 1.2   // Conservative initial noise (10× ε₀)

	// App-limited anchor: max loss rate for bandwidth recovery formula T/(1-L)
	appLimitedMaxLoss = 0.5

	// ackRate cwnd compensation: floor to prevent cwnd explosion (same as Brutal)
	minAckRate = 0.95

	// Slow start throughput saturation detection
	// Throughput growth < rate growth × this → link saturated, exit slow start.
	// With slowStartFactor=2.0: threshold=1.4, so throughput must grow 40%+
	// to continue. Flat throughput (growth=1.0) correctly triggers exit.
	slowStartSaturationRatio = 0.7

	// BW Estimator: BBR-style bandwidth anchoring.
	// When bwEst is valid, rate probes around measured BW (Cruise/ProbeUp/Drain cycle)
	// instead of PCC gradient ascent. PCC logic preserved as fallback.
	bwWindowSize      = 8                  // Sliding window = 1 full probe cycle
	bwCruiseGain      = 0.95               // 5% below BW: zero congestion loss
	bwProbeUpGain     = 1.10               // 10% above BW: detect BW increase
	bwDrainGain       = 0.75               // Drain queue + measure BG loss
	bwCruiseLength    = 6                  // Cruise MIs per cycle (+ 1 ProbeUp + 1 Drain = 8)
	bwMinValidCount   = 2                  // Minimum samples for valid estimate
	bwDropThreshold   = 0.5                // throughput < expected × this → BW dropped
	bwRiseThreshold   = 1.05               // ProbeUp throughput > corrected BW × this → BW rose
	bgLossAlpha       = 0.2                // BG loss EWMA smoothing
	bwAnchoredMinRate = 1_000_000.0        // 1 Mbps floor for BW-anchored mode
)

// K2CCMode represents the K2CC operating mode
type K2CCMode int

const (
	// K2CCModeSlowStart - exponential growth until utility drops
	K2CCModeSlowStart K2CCMode = iota
	// K2CCModeDecision - probing to determine rate direction
	K2CCModeDecision
	// K2CCModeRateAdjusting - applying gradient updates
	K2CCModeRateAdjusting
)

// bwProbePhase represents the BW-anchored probing phase.
type bwProbePhase int

const (
	bwPhaseCruise  bwProbePhase = iota
	bwPhaseProbeUp
	bwPhaseDrain
)

// String returns a human-readable name for the K2CC mode.
func (m K2CCMode) String() string {
	switch m {
	case K2CCModeSlowStart:
		return "slow-start"
	case K2CCModeDecision:
		return "decision"
	case K2CCModeRateAdjusting:
		return "rate-adjusting"
	default:
		return "unknown"
	}
}

// ============================================================================
// K2CC Configuration
// ============================================================================

// K2CCConfig holds configuration for K2CC algorithm.
type K2CCConfig struct {
	Alpha   float64 // Loss penalty coefficient
	Beta    float64 // Latency penalty coefficient
	Epsilon float64 // Probing rate change step size

	// LossAttribution selects how an MI's loss rate is computed (see
	// MonitorInterval.LossRate). Zero value = legacy.
	LossAttribution LossAttribution
	// AlignBWPhases evaluates each BW-anchored MI under the phase it actually
	// RAN at instead of the phase label of the rate about to be set (see
	// processBWAnchored). Zero value = legacy (off by one MI).
	AlignBWPhases bool
	// Variant is the A/B arm name, surfaced in cc-summary. "" = control.
	Variant string
}

// LossAttribution is the per-MI loss accounting mode.
type LossAttribution int

const (
	// LossAttrLegacy: BytesLost / BytesSent. Losses declared after the MI
	// rotated are dropped, and the denominator includes packets whose fate is
	// not known yet, so it reports roughly half of the real loss
	// (loss_attribution_test.go).
	LossAttrLegacy LossAttribution = iota
	// LossAttrResolved: BytesLost / (AckedOwnBytes + BytesLost) — only the
	// MI's own packets whose fate is known when the MI completes.
	LossAttrResolved
)

// minResolvedPkts is the fewest resolved own packets for LossAttrResolved to
// trust its sample; below it LossRate falls back to the legacy formula.
const minResolvedPkts = uint64(5)

// miPhase is the BW-anchored phase an MI actually ran at (the gain that set
// its TargetRate). miPhaseNone: not a BW-anchored rate (PCC, slow start,
// app-limited anchor, first anchored MI).
type miPhase int8

const (
	miPhaseNone miPhase = iota
	miPhaseCruise
	miPhaseProbeUp
	miPhaseDrain
)

// ============================================================================
// Monitor Interval
// ============================================================================

// MonitorInterval tracks statistics for a probing period
type MonitorInterval struct {
	ID           uint64
	TargetRate   float64
	BytesSent    uint64
	BytesAcked   uint64
	BytesLost    uint64
	RTTSamples   []time.Duration
	StartTime    time.Time
	EndTime      time.Time
	PacketsSent  uint64
	PacketsAcked uint64

	// Packet number range for stale loss detection
	FirstPacketNumber int64 // set on first OnPacketSent
	LastPacketNumber  int64 // updated on every OnPacketSent

	// MinDuration is the minimum time this MI must survive before completing.
	// Set to smoothed RTT so that QUIC loss detection (which takes ~1 RTT)
	// has time to report losses for packets sent in this MI.
	// Without this, high-rate MIs complete in a few ms (10 ACKs) and all
	// loss events arrive after MI rotation → filtered as "stale" → loss=0%.
	MinDuration time.Duration

	// IsAppLimited indicates if this MI was application-limited
	// (application not sending at full rate, e.g., idle period)
	IsAppLimited bool

	// Own-packet resolution (packets with number >= FirstPacketNumber whose
	// fate is known). BytesLost is already own-only (stale filter), so
	// AckedOwnBytes + BytesLost is the resolved byte count.
	AckedOwnBytes uint64
	ResolvedPkts  uint64

	lossAttr LossAttribution
	// ranPhase is the BW-anchored phase whose gain set this MI's rate.
	ranPhase miPhase
}

// NewMonitorInterval creates a new Monitor Interval
func NewMonitorInterval(id uint64, targetRate float64) *MonitorInterval {
	return &MonitorInterval{
		ID:         id,
		TargetRate: targetRate,
		StartTime:  time.Now(),
		RTTSamples: make([]time.Duration, 0, 20),
	}
}

// IsComplete checks if this MI has enough data.
// The MinDuration constraint ensures the MI survives at least 1 RTT so that
// QUIC loss detection can report losses for packets sent in this MI.
func (mi *MonitorInterval) IsComplete() bool {
	// Timeout protection: force complete after 60 seconds (always overrides)
	if mi.Duration() >= maxMIDuration {
		return true
	}

	// MinDuration gate: MI must live at least 1 RTT for loss visibility.
	// At high rates, 10 ACKs arrive in ~1ms but loss detection takes ~1 RTT.
	if mi.MinDuration > 0 && mi.Duration() < mi.MinDuration {
		return false
	}

	// Standard condition: >= 10 ACKs + RTT samples
	standardCondition := mi.PacketsAcked >= minMIPackets && len(mi.RTTSamples) > 0

	// Relaxed condition: >= 3 ACKs + >= 30 seconds (for low-rate scenarios)
	relaxedCondition := mi.PacketsAcked >= minMIPacketsRelaxed &&
		len(mi.RTTSamples) > 0 &&
		mi.Duration() >= minMIDurationRelaxed

	return standardCondition || relaxedCondition
}

// Complete marks this MI as complete
func (mi *MonitorInterval) Complete() {
	if mi.EndTime.IsZero() {
		mi.EndTime = time.Now()
	}
}

// Duration returns the duration of this MI
func (mi *MonitorInterval) Duration() time.Duration {
	end := mi.EndTime
	if end.IsZero() {
		end = time.Now()
	}
	return end.Sub(mi.StartTime)
}

// SendRate returns actual sending rate in bps: BytesSent × 8 / duration.
// This may exceed TargetRate when cwnd ackRate compensation inflates cwnd.
func (mi *MonitorInterval) SendRate() float64 {
	durationSecs := mi.Duration().Seconds()
	if durationSecs == 0 {
		return mi.TargetRate
	}
	return float64(mi.BytesSent) * 8.0 / durationSecs
}

// Throughput returns acked throughput in bps. Pure — safe to call from
// Snapshot(); app-limited detection lives in appLimited().
func (mi *MonitorInterval) Throughput() float64 {
	durationSecs := mi.Duration().Seconds()
	if durationSecs == 0 {
		return 0.0
	}
	return float64(mi.BytesAcked) * 8.0 / durationSecs
}

// appLimited reports whether the application sent less than
// appLimitedThreshold of what TargetRate allowed over this MI. Evaluated
// exactly once, at processCompletedMI entry, and stored in IsAppLimited —
// never from an observer, because the flag is sticky and flips the
// controller into the anchor path.
func (mi *MonitorInterval) appLimited() bool {
	durationSecs := mi.Duration().Seconds()
	if durationSecs == 0 {
		return false
	}
	expectedBytes := mi.TargetRate * durationSecs / 8.0
	return float64(mi.BytesSent) < expectedBytes*appLimitedThreshold
}

// LossRate calculates loss rate, clamped to [0.0, 1.0].
// MI boundary artifacts can produce BytesLost > BytesSent when stale loss
// events from a previous MI are attributed to a new MI with few bytes sent.
func (mi *MonitorInterval) LossRate() float64 {
	if mi.lossAttr == LossAttrResolved && mi.ResolvedPkts >= minResolvedPkts {
		// Own packets with a known fate only: no unresolved tail in the
		// denominator, no non-ack-eliciting packets, bounded by construction.
		return float64(mi.BytesLost) / float64(mi.AckedOwnBytes+mi.BytesLost)
	}
	if mi.BytesSent == 0 {
		return 0.0
	}
	loss := float64(mi.BytesLost) / float64(mi.BytesSent)
	if loss > 1.0 {
		loss = 1.0
	}
	return loss
}

// AvgRTT calculates average RTT
func (mi *MonitorInterval) AvgRTT() time.Duration {
	if len(mi.RTTSamples) == 0 {
		return 0
	}
	var sum time.Duration
	for _, rtt := range mi.RTTSamples {
		sum += rtt
	}
	return sum / time.Duration(len(mi.RTTSamples))
}

// RTTCV returns the coefficient of variation (stddev/mean) of RTT samples.
// Used as a UDP-native noise estimator for AIMD-ε signal classification.
// Returns 0 if fewer than 2 samples (insufficient data for variance).
// Single-pass: computes mean and variance together to avoid double iteration.
func (mi *MonitorInterval) RTTCV() float64 {
	n := len(mi.RTTSamples)
	if n < 2 {
		return 0
	}
	// Single pass: sum for mean, then variance
	var sum int64
	for _, rtt := range mi.RTTSamples {
		sum += int64(rtt)
	}
	mean := float64(sum) / float64(n)
	if mean == 0 {
		return 0
	}
	var sumSq float64
	for _, rtt := range mi.RTTSamples {
		d := float64(rtt) - mean
		sumSq += d * d
	}
	return math.Sqrt(sumSq/float64(n)) / mean
}

// ============================================================================
// Sliding-Window Ack Rate Tracker
// ============================================================================
//
// Tracks global ack/loss ratio over a 5-second sliding window using 1-second
// buckets. Inspired by Hysteria's Brutal CC. Independent of the per-MI loss
// attribution system — immune to MI boundary artifacts and stale loss filtering.
//
// Fed by OnCongestionEventEx (batched ACK+loss callback from quic-go).
// Exposed via CCSnapshot.AckRate for observability (debug logging only).

const (
	// ackRateSlotCount is the number of 1-second slots in the sliding window.
	ackRateSlotCount = 5
)

// ackRateSlot holds packet counts for a 1-second time slot.
type ackRateSlot struct {
	Timestamp int64  // Unix-ish second (from monotonic time)
	AckCount  uint64
	LossCount uint64
}

// ackRateTracker maintains a sliding-window ack rate.
type ackRateTracker struct {
	slots [ackRateSlotCount]ackRateSlot
}

// newAckRateTracker creates a tracker. Initial ackRate is 1.0 (all slots empty).
func newAckRateTracker() *ackRateTracker {
	return &ackRateTracker{}
}

// record adds ack and loss counts for the given monotonic time.
// Called from OnCongestionEventEx under the sender's mutex.
func (tr *ackRateTracker) record(eventTime time.Duration, acks, losses uint64) {
	ts := int64(eventTime / time.Second)
	slot := ts % ackRateSlotCount
	if tr.slots[slot].Timestamp == ts {
		tr.slots[slot].AckCount += acks
		tr.slots[slot].LossCount += losses
	} else {
		tr.slots[slot] = ackRateSlot{
			Timestamp: ts,
			AckCount:  acks,
			LossCount: losses,
		}
	}
}

// AckRate returns the ack rate over the sliding window.
// Returns 1.0 if no data.
func (tr *ackRateTracker) AckRate() float64 {
	var totalAck, totalLoss uint64
	for i := range tr.slots {
		totalAck += tr.slots[i].AckCount
		totalLoss += tr.slots[i].LossCount
	}
	total := totalAck + totalLoss
	if total == 0 {
		return 1.0
	}
	rate := float64(totalAck) / float64(total)
	if rate > 1.0 {
		return 1.0
	}
	return rate
}

// bwEstimator maintains a sliding-window max of throughput samples.
// Similar to BBR's BtlBw: captures bottleneck bandwidth when CC rate >= BW.
type bwEstimator struct {
	samples [bwWindowSize]float64
	head    int
	count   int
}

// addSample adds a throughput measurement to the sliding window.
func (e *bwEstimator) addSample(throughput float64) {
	e.samples[e.head] = throughput
	e.head = (e.head + 1) % bwWindowSize
	if e.count < bwWindowSize {
		e.count++
	}
}

// estimate returns the maximum throughput in the sliding window.
func (e *bwEstimator) estimate() float64 {
	if e.count == 0 {
		return 0
	}
	maxVal := 0.0
	for i := range e.count {
		if e.samples[i] > maxVal {
			maxVal = e.samples[i]
		}
	}
	return maxVal
}

// valid returns true when enough samples exist for a reliable estimate.
func (e *bwEstimator) valid() bool {
	return e.count >= bwMinValidCount
}

// reset replaces all samples with a new value (BW drop recovery).
func (e *bwEstimator) reset(throughput float64) {
	for i := range e.samples {
		e.samples[i] = throughput
	}
	e.head = 1
	if e.count < bwMinValidCount {
		e.count = bwMinValidCount
	}
}

// ============================================================================
// Utility Function
// ============================================================================

// UtilityFunction calculates K2CC utility: U = T(1-αL) - β·ΔRtt
type UtilityFunction struct {
	Alpha  float64
	Beta   float64
	MinRTT time.Duration
}

// NewUtilityFunction creates a new utility function.
func NewUtilityFunction(alpha, beta float64) *UtilityFunction {
	return &UtilityFunction{
		Alpha:  alpha,
		Beta:   beta,
		MinRTT: time.Second, // Updated as real RTT samples arrive
	}
}

// UpdateMinRTT updates minimum RTT if sample is lower.
func (uf *UtilityFunction) UpdateMinRTT(rtt time.Duration) {
	if rtt < uf.MinRTT && rtt > 0 {
		uf.MinRTT = rtt
	}
}

// Calculate computes utility for a Monitor Interval.
// U = T - α·L·R - β·ΔRtt
//
// sendRate is the actual sending rate in bps (BytesSent × 8 / duration).
// Using actual rate (not TargetRate) ensures the penalty reflects what the network
// experienced. cwnd ackRate compensation can inflate actual sending beyond the
// CC target rate — using TargetRate for R would under-penalize this over-sending.
func (uf *UtilityFunction) Calculate(mi *MonitorInterval, sendRate float64) float64 {
	throughput := mi.Throughput()
	lossRate := mi.LossRate()

	// RTT gradient: deviation from minimum
	var deltaRTT float64
	if avgRTT := mi.AvgRTT(); avgRTT > 0 {
		deltaRTT = max(avgRTT-uf.MinRTT, 0).Seconds() * 1000.0
	}

	throughputMbps := throughput / 1_000_000.0

	// Rate-based loss penalty: U = T - α·L·R - β·ΔRtt
	// Using actual sending rate R gives O(R) gradient signal when rate >> BW,
	// and correctly penalizes cwnd-inflated over-sending.
	rateMbps := sendRate / 1_000_000.0
	utility := throughputMbps - uf.Alpha*lossRate*rateMbps - uf.Beta*deltaRTT
	return utility
}

// ============================================================================
// K2CC State
// ============================================================================

// K2CCState holds the K2CC algorithm state
type K2CCState struct {
	CurrentRate      float64
	Epsilon          float64
	Mode             K2CCMode
	CurrentMI        *MonitorInterval
	NextMIID         uint64
	lastUtility      float64
	hasLastUtility   bool
	UtilityIncreases uint32
	UtilityDecreases uint32

	// Slow start: throughput from previous MI for saturation detection
	lastSlowStartThroughput float64

	// Stamped onto every MI this state creates.
	lossAttr LossAttribution
	// nextRanPhase is the BW-anchored phase whose gain set CurrentRate — it
	// becomes the next MI's ranPhase.
	nextRanPhase miPhase
}

// ensureCurrentMI ensures there's a current MI.
// rtt sets MinDuration on newly created MIs. Uses 2×RTT to give QUIC loss
// detection enough time — at high rates, packets are sent in <1ms but loss
// detection takes ~1 RTT. With 1×RTT MinDuration, MIs complete before most
// losses are reported → loss is under-reported → gradient signal is wrong.
func (s *K2CCState) ensureCurrentMI(rate float64, rtt time.Duration) *MonitorInterval {
	if s.CurrentMI == nil {
		s.CurrentMI = NewMonitorInterval(s.NextMIID, rate)
		s.CurrentMI.MinDuration = 2 * rtt
		s.CurrentMI.lossAttr = s.lossAttr
		s.CurrentMI.ranPhase = s.nextRanPhase
		s.NextMIID++
	}
	return s.CurrentMI
}

// startNewMI starts a new Monitor Interval, reusing the previous MI's
// RTTSamples backing array to avoid a 160-byte allocation per MI rotation.
// rtt is the current smoothed RTT. MinDuration is set to 2×RTT for loss visibility.
func (s *K2CCState) startNewMI(rate float64, rtt time.Duration) *MonitorInterval {
	var rttBuf []time.Duration
	if s.CurrentMI != nil {
		s.CurrentMI.Complete()
		// Reuse RTTSamples backing array (processCompletedMI has finished reading it)
		if cap(s.CurrentMI.RTTSamples) > 0 {
			rttBuf = s.CurrentMI.RTTSamples[:0]
		}
	}
	if rttBuf == nil {
		rttBuf = make([]time.Duration, 0, 20)
	}
	s.CurrentMI = &MonitorInterval{
		ID:          s.NextMIID,
		TargetRate:  rate,
		StartTime:   time.Now(),
		RTTSamples:  rttBuf,
		MinDuration: 2 * rtt,
		lossAttr:    s.lossAttr,
		ranPhase:    s.nextRanPhase,
	}
	s.NextMIID++
	return s.CurrentMI
}

// ============================================================================
// K2CC Sender (Congestion Controller)
// ============================================================================

// K2CCSender implements K2CC Vivace congestion control
type K2CCSender struct {
	config   *K2CCConfig
	state    *K2CCState
	utility  *UtilityFunction
	maxRate  int64
	cwnd     congestion.ByteCount
	maxSize  congestion.ByteCount
	rtt      time.Duration
	rttStats congestion.RTTStatsProvider

	// Pacing support
	lastSendTime   congestion.Time
	pacingBudget   congestion.ByteCount
	lastUpdateTime congestion.Time

	// Snapshot fallback: when CurrentMI is too young (MI boundary), Snapshot()
	// returns these values instead of the in-progress MI's unreliable stats.
	lastCompletedLoss       float64
	lastCompletedThroughput float64

	// AIMD-ε: noise baseline for strong/weak signal classification.
	// EWMA of RTT CV during inconsistent (building) periods.
	noiseEWMA float64

	// Stable probing: base rate around which undecided probes alternate.
	// Prevents EWMA-induced drift during probing (R+ε, R-ε cycles).
	// Updated when direction is confirmed (counter ≥ 2).
	probeBaseRate float64

	// Sliding-window ack rate tracker — fed by OnCongestionEventEx.
	// Used for cwnd ackRate compensation (Brutal-style) + debug logging.
	ackRate *ackRateTracker

	// BW Estimator: BBR-style bandwidth anchoring.
	// When bwEst.valid(), rate probes around measured BW instead of PCC gradient.
	bwEst          bwEstimator
	bgLossEWMA     float64      // Background loss from Drain phases
	bwPhase        bwProbePhase // Cruise / ProbeUp / Drain
	bwCruiseRemain int          // Remaining Cruise MIs in current cycle

	// stats is the cumulative observability record (wire/k2cc/summary.go).
	// Written only from processCompletedMI (under mu), read by Summary().
	stats CCSummary

	// Test-only hook: called when an MI completes (inside mu lock).
	miCompletedHook func(mi *MonitorInterval, throughput float64, isAppLimited bool)

	mu sync.RWMutex
}

// NewK2CCSender creates a new K2CC sender with default (standard) config.
func NewK2CCSender(maxRate int64) *K2CCSender {
	return NewK2CCSenderWithConfig(maxRate, &K2CCConfig{
		Alpha: defaultAlpha, Beta: defaultBeta, Epsilon: defaultEpsilon,
	})
}

// NewCCSenderGFWMode creates a new K2CC sender optimized for GFW/lossy networks.
func NewCCSenderGFWMode(maxRate int64) *K2CCSender {
	return NewK2CCSenderWithConfig(maxRate, &K2CCConfig{
		Alpha: gfwAlpha, Beta: defaultBeta, Epsilon: defaultEpsilon,
	})
}

// NewK2CCSenderWithConfig creates a new K2CC sender with custom config.
func NewK2CCSenderWithConfig(maxRate int64, config *K2CCConfig) *K2CCSender {
	rate := float64(maxRate) * initialRateFactor

	return &K2CCSender{
		config:  config,
		state: &K2CCState{
			CurrentRate: rate,
			Epsilon:     config.Epsilon,
			Mode:        K2CCModeSlowStart,
			lossAttr:    config.LossAttribution,
		},
		utility:   NewUtilityFunction(config.Alpha, config.Beta),
		maxRate:   maxRate,
		cwnd:      rateToCwnd(rate, initialRTT, 1.0),
		maxSize:   congestion.ByteCount(1350),
		rtt:       initialRTT,
		noiseEWMA: coldStartNoise,
		ackRate:   newAckRateTracker(),
		stats:     CCSummary{SenderID: senderSeq.Add(1), Variant: config.Variant},
	}
}

// clampedAckRate returns the ack rate clamped to [minAckRate, 1.0].
// Prevents cwnd explosion when ack rate is very low.
func (p *K2CCSender) clampedAckRate() float64 {
	rate := p.ackRate.AckRate()
	if rate < minAckRate {
		return minAckRate
	}
	return rate
}

// rateToCwnd converts sending rate to congestion window with ack rate compensation.
// ackRate < 1.0 enlarges cwnd to compensate for packet loss (Brutal-style).
func rateToCwnd(rateBps float64, rtt time.Duration, ackRate float64) congestion.ByteCount {
	// cwnd = (rate_bps / 8) * rtt_seconds * multiplier / ackRate
	bytesPerSec := rateBps / 8.0
	rttSecs := rtt.Seconds()
	cwnd := congestion.ByteCount(bytesPerSec * rttSecs * arcCwndMultiplier / ackRate)

	cwnd = max(cwnd, arcMinWindow)
	cwnd = min(cwnd, arcMaxWindow)
	return cwnd
}

// SetMICompletedHook sets a callback invoked when each MI completes.
// Test use only. The hook is called under the sender's lock.
func (p *K2CCSender) SetMICompletedHook(hook func(mi *MonitorInterval, throughput float64, isAppLimited bool)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.miCompletedHook = hook
}

// SetRTTStatsProvider sets the RTT statistics provider
func (p *K2CCSender) SetRTTStatsProvider(provider congestion.RTTStatsProvider) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.rttStats = provider
}

// updateRate updates the sending rate with EWMA smoothing.
// Floor is always arcMinRate (5M) — allows gradient descent on bottleneck links.
// The higher arcMinDownloadRate (30M) is used ONLY by app-limited anchor for
// fast recovery from idle periods (see processCompletedMI).
func (p *K2CCSender) updateRate(newRate float64) {
	// Clamp new rate
	if newRate < arcMinRate {
		newRate = arcMinRate
	}
	if newRate > float64(p.maxRate) {
		newRate = float64(p.maxRate)
	}

	// EWMA smoothing
	p.state.CurrentRate = p.state.CurrentRate*(1.0-arcRateEWMAAlpha) + newRate*arcRateEWMAAlpha

	// Strict floor protection AFTER EWMA
	if p.state.CurrentRate < arcMinRate {
		p.state.CurrentRate = arcMinRate
	}

	// Update cwnd
	p.cwnd = rateToCwnd(p.state.CurrentRate, p.rtt, p.clampedAckRate())
}

// setRateAnchored sets rate for BW-anchored mode with lower floor than PCC.
// Drain at 0.75×BW can go below the PCC 5M floor on low-bandwidth links.
func (p *K2CCSender) setRateAnchored(newRate float64) {
	if newRate < bwAnchoredMinRate {
		newRate = bwAnchoredMinRate
	}
	if newRate > float64(p.maxRate) {
		newRate = float64(p.maxRate)
	}
	p.state.CurrentRate = p.state.CurrentRate*(1.0-arcRateEWMAAlpha) + newRate*arcRateEWMAAlpha
	if p.state.CurrentRate < bwAnchoredMinRate {
		p.state.CurrentRate = bwAnchoredMinRate
	}
	p.cwnd = rateToCwnd(p.state.CurrentRate, p.rtt, p.clampedAckRate())
}

// processCompletedMI processes a completed Monitor Interval.
func (p *K2CCSender) processCompletedMI(mi *MonitorInterval) {
	// Decide app-limited exactly once, here — Throughput() is pure.
	mi.IsAppLimited = mi.appLimited()
	throughput := mi.Throughput()
	// Only processBWAnchored sets a BW-anchored rate; every other exit leaves
	// the next MI un-phased.
	p.state.nextRanPhase = miPhaseNone

	// Observability: record what the controller SAW before it acts (mode
	// residency is the mode this MI ran under). recordMIOutcome runs on every
	// exit — the app-limited branch returns early.
	p.recordCompletedMI(mi, throughput)
	defer p.recordMIOutcome()

	// Feed BW estimator (always, except app-limited)
	if !mi.IsAppLimited && throughput > 0 {
		p.bwEst.addSample(throughput)
	}

	// Rate probing: every N MIs, probe higher rate to recover from local minima.
	// Do NOT reset direction counters — let AIMD-ε evaluate the probe result
	// through normal gradient processing.
	// Skip probe when loss is high — probing into congestion just wastes bandwidth
	// and prevents convergence on bottleneck-limited links.
	// Skip when BW-anchored mode is active (ProbeUp serves this purpose).
	if !p.bwEst.valid() && p.state.NextMIID%rateProbingInterval == 0 && mi.LossRate() < 0.30 {
		probeRate := p.state.CurrentRate * (1.0 + rateProbingIncrease)
		if probeRate <= float64(p.maxRate) {
			p.stats.RateProbes++
			p.updateRate(probeRate)
		}
	}

	// Test observability hook
	if p.miCompletedHook != nil {
		p.miCompletedHook(mi, throughput, mi.IsAppLimited)
	}

	// App-limited: rate exceeds link capacity. Anchor to observed throughput.
	// Stateless: computes B_est = T/(1-L) inline, no persistent EWMA needed.
	if mi.IsAppLimited {
		if throughput > 0 {
			anchorRate := throughput
			if lossRate := mi.LossRate(); lossRate > 0 && lossRate < appLimitedMaxLoss {
				anchorRate = throughput / (1.0 - lossRate)
			}
			// Anchor floor: prevents rate collapse from transient low throughput.
			// - Transitional (post-slow-start, tiny BytesSent): use arcMinRate (5M)
			// - BW known (bwEst valid): use bgLoss-corrected bwEst (actual link capacity)
			//   Without this, the 30M floor causes 6x overshoot on 5M links → 85% loss
			//   → DATAGRAM delivery dies → UDP relay fails → "巨慢"
			// - Cold start (no bwEst): use arcMinDownloadRate (30M) for fast ramp-up
			minRate := arcMinRate
			if float64(p.maxRate) >= arcMinDownloadRate && mi.BytesSent >= 10000 {
				if p.bwEst.valid() {
					minRate = p.bwEst.estimate()
					if p.bgLossEWMA > 0.01 {
						minRate = minRate / (1.0 - p.bgLossEWMA)
					}
				} else {
					minRate = arcMinDownloadRate
				}
			}
			if anchorRate < minRate {
				anchorRate = minRate
			}
			p.state.CurrentRate = anchorRate
			p.cwnd = rateToCwnd(p.state.CurrentRate, p.rtt, p.clampedAckRate())
		}
		if p.state.Mode == K2CCModeSlowStart {
			p.state.Mode = K2CCModeDecision
		}
		// Reset direction counters (rate jumped), but preserve gradient context
		// so next MI can compute gradient immediately (no blind probing period).
		p.state.UtilityIncreases = 0
		p.state.UtilityDecreases = 0
		p.state.lastUtility = p.utility.Calculate(mi, mi.SendRate())
		p.state.hasLastUtility = true
		p.probeBaseRate = p.state.CurrentRate

		p.lastCompletedLoss = mi.LossRate()
		p.lastCompletedThroughput = throughput

		slog.Debug("k2cc: app-limited anchor",
			"mi", mi.ID,
			"rate", fmt.Sprintf("%.1fM", p.state.CurrentRate/1_000_000),
			"throughput", fmt.Sprintf("%.1fM", throughput/1_000_000),
			"loss", fmt.Sprintf("%.1f%%", mi.LossRate()*100),
			"mode", p.state.Mode.String(),
		)
		return
	}

	// Update min RTT
	avgRTT := mi.AvgRTT()
	if avgRTT > 0 {
		p.utility.UpdateMinRTT(avgRTT)
	}

	// Calculate utility using actual sending rate (not TargetRate).
	// cwnd ackRate compensation may inflate actual bytes beyond pacing rate.
	utility := p.utility.Calculate(mi, mi.SendRate())

	switch p.state.Mode {
	case K2CCModeSlowStart:
		// Exit condition 1: rate has reached maxRate — no room to grow.
		if p.state.CurrentRate >= float64(p.maxRate)*slowStartExitRatio {
			p.state.Mode = K2CCModeDecision
			p.state.UtilityIncreases = 0
			p.state.UtilityDecreases = 0
			// Initialize BW-anchored probing if estimator is ready
			if p.bwEst.valid() {
				p.bwPhase = bwPhaseCruise
				p.bwCruiseRemain = bwCruiseLength
			}
			break
		}

		// Exit condition 2: utility dropped significantly.
		// Anchor rate to throughput — rate has exceeded link capacity.
		// Without this, rate stays at the slow-start overshoot level and the
		// algorithm takes hundreds of MIs to descend via gradient.
		if p.state.hasLastUtility && utility < p.state.lastUtility*slowStartDropRatio {
			if throughput > 0 {
				p.state.CurrentRate = throughput
				p.cwnd = rateToCwnd(p.state.CurrentRate, p.rtt, p.clampedAckRate())
				p.probeBaseRate = p.state.CurrentRate
			}
			p.state.Mode = K2CCModeDecision
			p.state.UtilityIncreases = 0
			p.state.UtilityDecreases = 0
			// Initialize BW-anchored probing if estimator is ready
			if p.bwEst.valid() {
				p.bwPhase = bwPhaseCruise
				p.bwCruiseRemain = bwCruiseLength
			}
			break
		}

		// Exit condition 3: throughput saturation.
		// Rate doubled but throughput didn't grow proportionally → link is full.
		// Set rate to actual throughput so gradient ascent starts near optimum.
		if p.state.lastSlowStartThroughput > 0 && throughput > 0 {
			tputGrowth := throughput / p.state.lastSlowStartThroughput
			if tputGrowth < arcSlowStartFactor*slowStartSaturationRatio {
				p.state.CurrentRate = throughput
				p.cwnd = rateToCwnd(p.state.CurrentRate, p.rtt, p.clampedAckRate())
				p.state.Mode = K2CCModeDecision
				p.state.UtilityIncreases = 0
				p.state.UtilityDecreases = 0
				p.probeBaseRate = p.state.CurrentRate
				// Initialize BW-anchored probing if estimator is ready
				if p.bwEst.valid() {
					p.bwPhase = bwPhaseCruise
					p.bwCruiseRemain = bwCruiseLength
				}
				p.stats.SlowStartSaturations++
				slog.Debug("k2cc: slow start saturated",
					"mi", mi.ID,
					"throughput", fmt.Sprintf("%.1fM", throughput/1_000_000),
					"prevThroughput", fmt.Sprintf("%.1fM", p.state.lastSlowStartThroughput/1_000_000),
					"growth", fmt.Sprintf("%.2f", tputGrowth),
				)
				break
			}
		}
		p.state.lastSlowStartThroughput = throughput

		// Continue slow start: multiply by growth factor
		newRate := p.state.CurrentRate * arcSlowStartFactor
		p.updateRate(newRate)

	case K2CCModeDecision, K2CCModeRateAdjusting:
		// BW-anchored mode: when bandwidth estimate is valid, probe around
		// measured BW instead of using PCC gradient ascent.
		if p.bwEst.valid() {
			p.processBWAnchored(mi, throughput)
		} else if p.state.hasLastUtility {
			gradient := utility - p.state.lastUtility

			// Capture previous state for AIMD-ε reversal detection
			prevConfirmed := p.state.UtilityIncreases >= 2 || p.state.UtilityDecreases >= 2

			// Update direction counters.
			// Zero gradient = no signal (utility is flat) — don't update counters.
			// Only clear signals drive direction confirmation.
			if gradient > 0 {
				p.state.UtilityIncreases++
				p.state.UtilityDecreases = 0
			} else if gradient < 0 {
				p.state.UtilityDecreases++
				p.state.UtilityIncreases = 0
			}
			// gradient == 0: ambiguous, keep current counter state

			// AIMD-ε: adapt exploration step size
			nowConfirmed := p.state.UtilityIncreases >= 2 || p.state.UtilityDecreases >= 2

			// Update noise baseline during inconsistent (building) periods
			if !nowConfirmed {
				if rttCV := mi.RTTCV(); rttCV > 0 {
					p.noiseEWMA = p.noiseEWMA*(1-noiseEWMAAlpha) + rttCV*noiseEWMAAlpha
				}
			}

			if nowConfirmed {
				// ① Direction confirmed (counter ≥ 2): additive increase
				p.state.Epsilon += epsilonDelta
			} else if prevConfirmed {
				// Direction just reversed — classify signal strength
				absGrad := math.Abs(gradient)
				utilityScale := p.state.CurrentRate / 1_000_000.0
				if utilityScale < 1.0 {
					utilityScale = 1.0
				}
				threshold := epsilonZThreshold * p.noiseEWMA * utilityScale

				if absGrad > threshold {
					// ③ Strong signal: genuine regime change → fast recovery
					p.state.Epsilon = defaultEpsilon
				} else {
					// ② Weak signal: noise-induced reversal → multiplicative decrease
					p.state.Epsilon *= epsilonDecay
				}
			}
			// ④ Building (counter = 1, not reversal): no change (hold)

			// Clamp epsilon
			if p.state.Epsilon < epsilonMin {
				p.state.Epsilon = epsilonMin
			}
			if p.state.Epsilon > epsilonMax {
				p.state.Epsilon = epsilonMax
			}

			// Apply gradient ascent with stable probing.
			// Confirmed direction: move base rate via EWMA.
			// Undecided probing: alternate around fixed probeBaseRate (no drift).
			if p.state.UtilityIncreases >= 2 {
				p.updateRate(p.state.CurrentRate * (1.0 + p.state.Epsilon))
				p.probeBaseRate = p.state.CurrentRate
			} else if p.state.UtilityDecreases >= 2 {
				p.updateRate(p.state.CurrentRate * (1.0 - p.state.Epsilon))
				p.probeBaseRate = p.state.CurrentRate
			} else {
				// Probing: alternate around probeBaseRate to avoid EWMA drift
				if p.probeBaseRate == 0 {
					p.probeBaseRate = p.state.CurrentRate
				}
				sign := 1.0
				if p.state.NextMIID%2 == 1 {
					sign = -1.0
				}
				probeRate := p.probeBaseRate * (1.0 + sign*p.state.Epsilon)
				// Clamp without EWMA — probing doesn't shift the base rate
				if probeRate < arcMinRate {
					probeRate = arcMinRate
				}
				if probeRate > float64(p.maxRate) {
					probeRate = float64(p.maxRate)
				}
				p.state.CurrentRate = probeRate
				p.cwnd = rateToCwnd(probeRate, p.rtt, p.clampedAckRate())
			}
		}
	}

	p.state.lastUtility = utility
	p.state.hasLastUtility = true

	// Log completed MI for observability.
	dir := "probe"
	if p.state.UtilityIncreases >= 2 {
		dir = "+1"
	} else if p.state.UtilityDecreases >= 2 {
		dir = "-1"
	}
	slog.Debug("k2cc: MI complete",
		"mi", mi.ID,
		"rate", fmt.Sprintf("%.1fM", p.state.CurrentRate/1_000_000),
		"throughput", fmt.Sprintf("%.1fM", throughput/1_000_000),
		"loss", fmt.Sprintf("%.1f%%", mi.LossRate()*100),
		"rtt", mi.AvgRTT(),
		"epsilon", fmt.Sprintf("%.3f", p.state.Epsilon),
		"mode", p.state.Mode.String(),
		"dir", dir,
		"ackRate", fmt.Sprintf("%.3f", p.ackRate.AckRate()),
	)
}

// processBWAnchored implements BBR-style BW-anchored rate control.
// Called from processCompletedMI when bwEst is valid.
// Cycle: 6 × Cruise → 1 × ProbeUp → 1 × Drain → repeat.
func (p *K2CCSender) processBWAnchored(mi *MonitorInterval, throughput float64) {
	bw := p.bwEst.estimate()

	// Correct BW estimate for background loss
	if p.bgLossEWMA > 0.01 {
		bw = bw / (1.0 - p.bgLossEWMA)
	}

	if bw <= 0 {
		p.bwEst = bwEstimator{} // Invalid — fall back to PCC
		return
	}
	p.stats.BWAnchoredMIs++

	// Evaluation: which phase's measurement does this MI carry?
	// Legacy: the label of the rate about to be set — one MI ahead of the MI
	// that just completed (bw_phase_alignment_test.go): bgLoss is sampled on
	// the MI that ran at the probe-up rate, BW-rise on a cruise MI.
	// Aligned: the phase the MI actually ran at.
	evalPhase := miPhaseNone
	if p.config.AlignBWPhases {
		evalPhase = mi.ranPhase
	} else {
		switch p.bwPhase {
		case bwPhaseCruise:
			evalPhase = miPhaseCruise
		case bwPhaseProbeUp:
			evalPhase = miPhaseProbeUp
		case bwPhaseDrain:
			evalPhase = miPhaseDrain
		}
	}

	switch evalPhase {
	case miPhaseCruise:
		// BW decrease detection: throughput much lower than expected
		expectedThroughput := bw * bwCruiseGain
		if p.bgLossEWMA > 0.01 {
			expectedThroughput *= (1.0 - p.bgLossEWMA)
		}
		if throughput > 0 && throughput < expectedThroughput*bwDropThreshold {
			newBW := throughput
			if p.bgLossEWMA > 0.01 {
				newBW = throughput / (1.0 - p.bgLossEWMA)
			}
			p.bwEst.reset(newBW)
			bw = newBW
			p.stats.BWDrops++
			slog.Debug("k2cc: bw-anchor BW drop detected",
				"mi", mi.ID,
				"throughput", fmt.Sprintf("%.1fM", throughput/1e6),
				"newBW", fmt.Sprintf("%.1fM", bw/1e6),
			)
		}

	case miPhaseProbeUp:
		// BW increase detection
		correctedBW := bw
		if p.bgLossEWMA > 0.01 {
			correctedBW = bw * (1.0 - p.bgLossEWMA)
		}
		if throughput > correctedBW*bwRiseThreshold {
			p.bwEst.addSample(throughput)
			p.stats.BWRises++
			slog.Debug("k2cc: bw-anchor BW rise detected",
				"mi", mi.ID,
				"throughput", fmt.Sprintf("%.1fM", throughput/1e6),
				"prevBW", fmt.Sprintf("%.1fM", bw/1e6),
			)
		}

	case miPhaseDrain:
		// BG loss measurement: rate well below BW → any loss is background
		if lossRate := mi.LossRate(); lossRate > 0.005 {
			p.bgLossEWMA = p.bgLossEWMA*(1-bgLossAlpha) + lossRate*bgLossAlpha
		} else if p.bgLossEWMA > 0.001 {
			p.bgLossEWMA *= (1 - bgLossAlpha)
		}
	}

	// Scheduling: set the next MI's rate and advance the cycle. Unchanged
	// between modes — 6 × cruise, 1 × probe-up, 1 × drain.
	switch p.bwPhase {
	case bwPhaseCruise:
		p.bwCruiseRemain--
		if p.bwCruiseRemain <= 0 {
			p.bwPhase = bwPhaseProbeUp
		}
		p.setRateAnchored(bw * bwCruiseGain)
		p.state.nextRanPhase = miPhaseCruise

	case bwPhaseProbeUp:
		p.bwPhase = bwPhaseDrain
		p.setRateAnchored(bw * bwProbeUpGain)
		p.state.nextRanPhase = miPhaseProbeUp

	case bwPhaseDrain:
		p.bwPhase = bwPhaseCruise
		p.bwCruiseRemain = bwCruiseLength
		p.setRateAnchored(bw * bwDrainGain)
		p.state.nextRanPhase = miPhaseDrain
	}

	slog.Debug("k2cc: bw-anchor",
		"mi", mi.ID,
		"phase", []string{"cruise", "probeUp", "drain"}[p.bwPhase],
		"bw", fmt.Sprintf("%.1fM", bw/1e6),
		"rate", fmt.Sprintf("%.1fM", p.state.CurrentRate/1e6),
		"bgLoss", fmt.Sprintf("%.1f%%", p.bgLossEWMA*100),
	)
}

// recordCompletedMI feeds the cumulative CCSummary. Caller holds p.mu.
func (p *K2CCSender) recordCompletedMI(mi *MonitorInterval, throughput float64) {
	st := &p.stats
	st.MIs++
	switch p.state.Mode {
	case K2CCModeSlowStart:
		st.SlowStartMIs++
	case K2CCModeDecision:
		st.DecisionMIs++
	case K2CCModeRateAdjusting:
		st.RateAdjMIs++
	}
	if mi.IsAppLimited {
		st.AppLimitedMIs++
	}
	st.BytesSent += mi.BytesSent
	st.BytesAcked += mi.BytesAcked
	st.BytesLost += mi.BytesLost
	st.ResolvedBytes += mi.AckedOwnBytes + mi.BytesLost
	// Per ran-phase loss: what each BW-anchored gain actually costs.
	st.PhaseMIs[mi.ranPhase]++
	st.PhaseLostBytes[mi.ranPhase] += mi.BytesLost
	st.PhaseResolvedBytes[mi.ranPhase] += mi.AckedOwnBytes + mi.BytesLost

	loss := mi.LossRate()
	histAdd(st.LossHist[:], lossHistEdges[:], loss*100)
	if loss > st.LossMax {
		st.LossMax = loss
	}
	// A 60 s force-completed MI can carry no RTT sample; skip rather than
	// count a fake 0 ms.
	if rtt := mi.AvgRTT(); rtt > 0 {
		histAdd(st.RTTHist[:], rttHistEdges[:], float64(rtt)/float64(time.Millisecond))
	}
	histAdd(st.RateHist[:], rateHistEdges[:], mi.TargetRate/1e6)
	histAdd(st.TputHist[:], rateHistEdges[:], throughput/1e6)
	if throughput > st.TputMax {
		st.TputMax = throughput
	}
}

// recordMIOutcome runs (deferred) at every processCompletedMI exit and counts
// MIs that left the rate sitting on a floor: the 1 Mbps BW-anchored floor, or
// the 5 Mbps PCC floor when no bandwidth estimate is anchoring the rate.
// Caller holds p.mu.
func (p *K2CCSender) recordMIOutcome() {
	r := p.state.CurrentRate
	if r <= bwAnchoredMinRate*1.001 || (!p.bwEst.valid() && r <= arcMinRate*1.001) {
		p.stats.FloorMIs++
	}
}

// Summary returns a copy of the cumulative observability record with the
// current-value fields filled from live state. Pure read under RLock.
func (p *K2CCSender) Summary() CCSummary {
	p.mu.RLock()
	// concurrency:allow — pure copy of struct fields, no I/O or external calls
	defer p.mu.RUnlock()
	s := p.stats
	s.Epsilon = p.state.Epsilon
	s.Mode = p.state.Mode
	s.MinRTT = p.utility.MinRTT
	s.BGLoss = p.bgLossEWMA
	if p.bwEst.valid() {
		bw := p.bwEst.estimate()
		if p.bgLossEWMA > 0.01 {
			bw = bw / (1.0 - p.bgLossEWMA)
		}
		s.BWEst = bw
	}
	return s
}

// ============================================================================
// congestion.CongestionControl Interface Implementation
// ============================================================================

// TimeUntilSend returns when the next packet can be sent
func (p *K2CCSender) TimeUntilSend(bytesInFlight congestion.ByteCount) congestion.Time {
	p.mu.RLock()
	// concurrency:allow — pure arithmetic on struct fields, no I/O or external calls
	defer p.mu.RUnlock()

	// If we have budget, send immediately
	if p.pacingBudget > 0 {
		return 0
	}

	// Calculate pacing interval based on current rate
	packetSize := float64(p.maxSize)
	rateBytesPerSec := p.state.CurrentRate / 8.0
	if rateBytesPerSec <= 0 {
		return 0
	}

	pacingIntervalNs := int64((packetSize / rateBytesPerSec) * 1e9)
	return p.lastSendTime + congestion.Time(pacingIntervalNs)
}

// HasPacingBudget returns true if we have pacing budget to send
func (p *K2CCSender) HasPacingBudget(now congestion.Time) bool {
	p.mu.Lock()
	// concurrency:allow — pure arithmetic on struct fields, no I/O or external calls
	defer p.mu.Unlock()

	// Calculate time since last update
	elapsedSec := float64(now-p.lastUpdateTime) / 1e9
	if elapsedSec <= 0 {
		elapsedSec = 0.001
	}

	// Add budget based on current rate
	budgetIncrease := congestion.ByteCount((p.state.CurrentRate / 8.0) * elapsedSec)
	p.pacingBudget += budgetIncrease
	p.lastUpdateTime = now

	// Cap budget at cwnd to prevent bursts
	// Use lock-free version to avoid deadlock (we already hold p.mu)
	cwnd := p.getCongestionWindowLocked()
	if p.pacingBudget > cwnd {
		p.pacingBudget = cwnd
	}

	return p.pacingBudget > 0
}

// OnPacketSent is called when a packet is sent
func (p *K2CCSender) OnPacketSent(
	sentTime congestion.Time,
	bytesInFlight congestion.ByteCount,
	packetNumber congestion.PacketNumber,
	bytes congestion.ByteCount,
	isRetransmittable bool,
) {
	p.mu.Lock()
	// concurrency:allow — pure arithmetic on struct fields, no I/O or external calls
	defer p.mu.Unlock()

	// Update pacing state
	p.lastSendTime = sentTime
	if p.pacingBudget >= bytes {
		p.pacingBudget -= bytes
	} else {
		p.pacingBudget = 0
	}

	mi := p.state.ensureCurrentMI(p.state.CurrentRate, p.rtt)
	mi.BytesSent += uint64(bytes)
	mi.PacketsSent++
	if mi.PacketsSent == 1 {
		mi.FirstPacketNumber = int64(packetNumber)
	}
	mi.LastPacketNumber = int64(packetNumber)
}

// CanSend returns true if we can send more data
func (p *K2CCSender) CanSend(bytesInFlight congestion.ByteCount) bool {
	return bytesInFlight < p.GetCongestionWindow()
}

// MaybeExitSlowStart is called to check if we should exit slow start
func (p *K2CCSender) MaybeExitSlowStart() {
	// K2CC handles this in processCompletedMI
}

// OnPacketAcked is called when a packet is acknowledged
func (p *K2CCSender) OnPacketAcked(
	number congestion.PacketNumber,
	ackedBytes congestion.ByteCount,
	priorInFlight congestion.ByteCount,
	eventTime congestion.Time,
) {
	p.mu.Lock()
	// concurrency:allow — pure arithmetic on struct fields, no I/O or external calls
	defer p.mu.Unlock()

	// Update RTT from stats provider
	if p.rttStats != nil {
		smoothedRTT := p.rttStats.SmoothedRTT()
		if smoothedRTT > 0 {
			p.rtt = smoothedRTT
		}
	}

	// Record in current MI
	mi := p.state.ensureCurrentMI(p.state.CurrentRate, p.rtt)
	mi.BytesAcked += uint64(ackedBytes)
	mi.PacketsAcked++
	mi.RTTSamples = append(mi.RTTSamples, p.rtt)
	if mi.PacketsSent > 0 && int64(number) >= mi.FirstPacketNumber {
		mi.AckedOwnBytes += uint64(ackedBytes)
		mi.ResolvedPkts++
	}
	p.stats.EventAckedBytes += uint64(ackedBytes)

	// Check if MI is complete
	if mi.IsComplete() {
		miCopy := *mi
		mi.Complete()

		// Save completed MI stats for Snapshot() fallback
		p.lastCompletedLoss = miCopy.LossRate()
		p.lastCompletedThroughput = miCopy.Throughput()

		// Process the completed MI
		p.processCompletedMI(&miCopy)

		// Start new MI with MinDuration = current smoothed RTT
		p.state.startNewMI(p.state.CurrentRate, p.rtt)
	}

	// Update cwnd
	p.cwnd = rateToCwnd(p.state.CurrentRate, p.rtt, p.clampedAckRate())
}

// OnCongestionEvent is called when congestion is detected
func (p *K2CCSender) OnCongestionEvent(
	number congestion.PacketNumber,
	lostBytes congestion.ByteCount,
	priorInFlight congestion.ByteCount,
) {
	p.mu.Lock()
	// concurrency:allow — pure arithmetic on struct fields, no I/O or external calls
	defer p.mu.Unlock()

	// ECN congestion signals arrive here with lostBytes == 0 (kuic
	// sent_packet_handler); they are not a lost packet.
	if lostBytes == 0 {
		return
	}
	// Variant-independent ground truth: every loss QUIC declares, before any
	// MI attribution. declLoss in cc-summary is built from these.
	p.stats.DeclaredLostBytes += uint64(lostBytes)
	p.stats.DeclaredLostPkts++

	// Record loss in current MI — but skip stale losses from previous MI.
	// QUIC reports loss for packets sent in old MI after new MI has started.
	// Without filtering: BytesLost >> BytesSent → LossRate 300-900% → false CRITICAL.
	mi := p.state.ensureCurrentMI(p.state.CurrentRate, p.rtt)

	// Stale loss filter: skip losses from previous MI.
	// PacketsSent == 0: no packets sent in this MI yet, ALL losses are stale.
	// number < FirstPacketNumber: packet belongs to a previous MI.
	if mi.PacketsSent == 0 || int64(number) < mi.FirstPacketNumber {
		p.stats.StaleLostBytes += uint64(lostBytes)
		return
	}

	mi.BytesLost += uint64(lostBytes)
	mi.ResolvedPkts++

	// In aggressive/GFW mode, we mostly ignore loss
	// The utility function handles this via low alpha
}

// OnCongestionEventEx is the batched congestion event callback (CongestionControlEx).
// Called by quic-go AFTER individual OnCongestionEvent + OnPacketAcked in the same
// ACK frame. Records global ack/loss counts in the sliding-window ackRate tracker
// for debug observability.
func (p *K2CCSender) OnCongestionEventEx(
	priorInFlight congestion.ByteCount,
	eventTime congestion.Time,
	ackedPackets []congestion.AckedPacketInfo,
	lostPackets []congestion.LostPacketInfo,
) {
	p.mu.Lock()
	// concurrency:allow — pure arithmetic on struct fields, no I/O or external calls
	defer p.mu.Unlock()

	// The ack-rate tracker is a time-window quantity with no MI ownership, so
	// the stale filter has no meaning for it; legacy keeps the filter only to
	// stay bit-identical for the control arm.
	filteredLosses := uint64(len(lostPackets))
	if p.config.LossAttribution == LossAttrLegacy {
		filteredLosses = p.countFreshLossesLocked(lostPackets)
	}
	acks := uint64(len(ackedPackets))
	if acks == 0 && filteredLosses == 0 {
		return
	}
	// Under p.mu: Snapshot()/Summary() read the tracker under RLock.
	p.ackRate.record(time.Duration(eventTime), acks, filteredLosses)
}

// countFreshLossesLocked filters stale losses — same logic as OnCongestionEvent.
// PacketsSent == 0: no sends in this MI yet, all losses are stale.
// Otherwise: only count losses with PacketNumber >= FirstPacketNumber.
// Caller must hold p.mu.
func (p *K2CCSender) countFreshLossesLocked(lostPackets []congestion.LostPacketInfo) uint64 {
	mi := p.state.CurrentMI
	if mi == nil || mi.PacketsSent == 0 {
		return 0
	}
	var count uint64
	for i := range lostPackets {
		if int64(lostPackets[i].PacketNumber) >= mi.FirstPacketNumber {
			count++
		}
	}
	return count
}

// OnRetransmissionTimeout is called when RTO expires
func (p *K2CCSender) OnRetransmissionTimeout(packetsRetransmitted bool) {
	// K2CC doesn't reduce cwnd on timeout
}

// SetMaxDatagramSize sets the maximum datagram size
func (p *K2CCSender) SetMaxDatagramSize(size congestion.ByteCount) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.maxSize = size
}

// InSlowStart returns whether we're in slow start mode
func (p *K2CCSender) InSlowStart() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.state.Mode == K2CCModeSlowStart
}

// InRecovery returns whether we're in recovery mode
func (p *K2CCSender) InRecovery() bool {
	return false // K2CC doesn't use traditional recovery
}

// getCongestionWindowLocked computes cwnd without acquiring the lock.
// Caller must already hold p.mu (read or write).
func (p *K2CCSender) getCongestionWindowLocked() congestion.ByteCount {
	rtt := p.rtt
	if p.rttStats != nil {
		smoothedRTT := p.rttStats.SmoothedRTT()
		if smoothedRTT > 0 {
			rtt = smoothedRTT
		}
	}
	if rtt == 0 {
		rtt = initialRTT
	}

	cwnd := rateToCwnd(p.state.CurrentRate, rtt, p.clampedAckRate())
	cwnd = max(cwnd, p.maxSize)
	return cwnd
}

// GetCongestionWindow returns the current congestion window size
func (p *K2CCSender) GetCongestionWindow() congestion.ByteCount {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.getCongestionWindowLocked()
}

// CCSnapshot is a point-in-time snapshot of K2CC internal state.
// All fields are copied under the read lock — safe for concurrent use after return.
type CCSnapshot struct {
	SendingRate float64       // Current sending rate in bps
	LossRate    float64       // Current MI loss rate (0.0-1.0)
	Throughput  float64       // Current MI throughput in bps
	RTT         time.Duration // Smoothed RTT
	MinRTT      time.Duration // Minimum observed RTT
	Cwnd        int64         // Congestion window in bytes
	Mode        K2CCMode     // SlowStart/Decision/RateAdjusting
	MIID        uint64        // Current Monitor Interval ID
	Epsilon     float64       // Current epsilon (AIMD-ε adaptive step size)
	AckRate     float64       // Global sliding-window ack rate (0.0-1.0), observability only
}

// minSnapshotBytes is the minimum BytesSent in the current MI before its stats
// are trusted for external consumption. Below this threshold, MI boundary
// artifacts (stale loss events, tiny duration) produce unreliable metrics.
// Set to 100KB to ensure stale loss from previous MI (up to ~BDP worth of
// packets) is adequately diluted before reporting.
const minSnapshotBytes = uint64(100000) // 100KB

// Snapshot returns a point-in-time copy of K2CC state.
func (p *K2CCSender) Snapshot() CCSnapshot {
	p.mu.RLock()
	// concurrency:allow — pure arithmetic on struct fields, no I/O or external calls
	defer p.mu.RUnlock()
	s := CCSnapshot{
		SendingRate: p.state.CurrentRate,
		Epsilon:     p.state.Epsilon,
		RTT:         p.rtt,
		MinRTT:      p.utility.MinRTT,
		Cwnd:        int64(p.cwnd),
		Mode:        p.state.Mode,
	}
	if p.ackRate != nil {
		s.AckRate = p.ackRate.AckRate()
	}
	if mi := p.state.CurrentMI; mi != nil {
		if mi.BytesSent >= minSnapshotBytes {
			s.LossRate = mi.LossRate()
			s.Throughput = mi.Throughput() // pure read — never touches IsAppLimited
		} else {
			// MI too young — use last completed MI's stats to avoid boundary artifacts.
			s.LossRate = p.lastCompletedLoss
			s.Throughput = p.lastCompletedThroughput
		}
		s.MIID = mi.ID
	}
	return s
}

// Verify K2CCSender implements CongestionControl interface
var _ congestion.CongestionControlEx = (*K2CCSender)(nil)
