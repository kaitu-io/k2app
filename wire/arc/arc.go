// K2CC Vivace - Performance-oriented Congestion Control
package arc

import (
	"fmt"
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/apernet/quic-go/congestion"
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
	// Default max rate for K2CC (500 Mbps — generous upper bound)
	DefaultMaxRate = int64(500_000_000)

	// Minimum congestion window (10 packets worth)
	arcMinWindow = congestion.ByteCount(14720) // 10 × 1472 (typical MTU)

	// Maximum congestion window (1GB)
	arcMaxWindow = congestion.ByteCount(1_000_000_000)

	// ============================================================================
	// Rate Boundaries - Aligned with realistic bandwidth constraints
	// ============================================================================

	// Minimum upload rate (10 Mbps - realistic home upload minimum)
	arcMinRate = 10_000_000.0 // 10 Mbps

	// Minimum download rate (30 Mbps - realistic home download minimum)
	// Used when maxRate >= 30 Mbps (download scenarios)
	arcMinDownloadRate = 30_000_000.0 // 30 Mbps

	// ============================================================================
	// Performance Tuning - Optimized for fast convergence
	// ============================================================================

	// Probing epsilon: exploration step size
	// History: 0.05 (initial) → 0.15 (aggressive for double CC) → 0.10 (browser QUIC blocked)
	DefaultK2CCEpsilon = 0.10

	// Beta: latency penalty coefficient
	// Low: throughput-prioritized for tunnel use
	DefaultK2CCBeta = 1.0

	// EWMA smoothing factor for rate updates
	// History: 0.7 (initial) → 0.9 (faster response for double CC) → 0.8 (browser QUIC blocked)
	arcRateEWMAAlpha = 0.8

	// Slow start growth factor
	// History: 2.0 (initial) → 2.5 (faster ramp for double CC) → 2.0 (browser QUIC blocked)
	arcSlowStartFactor = 2.0

	// ============================================================================
	// Recovery Mechanisms
	// ============================================================================

	// Rate probing interval: probe higher rate every N MIs
	// Frequency: every 10 MIs (was 20 - now probe 2x more often)
	RateProbingInterval = uint64(10)

	// Rate probing increase percentage
	// Old: 10%, New: 20% (more aggressive recovery)
	RateProbingIncrease = 0.2

	// ============================================================================
	// MI Completion Conditions
	// ============================================================================

	// Minimum packets to complete an MI
	MinMIPackets = uint64(10)

	// Maximum MI duration before forced completion (prevent long-running MIs)
	MaxMIDuration = 60 * time.Second

	// Minimum MI duration for relaxed completion
	MinMIDurationForRelaxed = 30 * time.Second

	// Minimum packets for relaxed completion (when MI is long-running)
	MinMIPacketsRelaxed = uint64(3)

	// Default alpha (loss penalty) - standard mode
	DefaultK2CCAlpha = 10.0

	// Alpha for GFW/aggressive mode (low loss penalty)
	GFWK2CCAlpha = 5.0

	// Congestion window multiplier (for rate to cwnd conversion)
	// 2.0× BDP matches BBR's cwnd gain, provides headroom for bursty ACK patterns
	arcCwndMultiplier = 2.0

	// ============================================================================
	// Adaptive Alpha Constants
	// ============================================================================

	// AdaptiveBaseAlpha is the base value for adaptive alpha calculation
	AdaptiveBaseAlpha = 10.0

	// AdaptiveSensitivity is the loss sensitivity coefficient
	AdaptiveSensitivity = 50.0

	// InitialAlpha is the starting alpha value for adaptive mode
	InitialAlpha = 2.0

	// AlphaEWMA is the EWMA coefficient for alpha updates (30% new value weight)
	AlphaEWMA = 0.3

	// MinAlpha is the minimum alpha value (lower bound)
	MinAlpha = 0.1

	// MaxAlpha is the maximum alpha value (upper bound)
	MaxAlpha = 10.0

	// LossHistorySize is the number of loss rate samples to keep
	LossHistorySize = 4

	// ============================================================================
	// AIMD-ε: Adaptive Exploration Step Size
	// ============================================================================
	//
	// Adapts epsilon (rate probing step size) based on utility gradient signal
	// quality. Derived from SGD optimal step theory: ε* ∝ |R-R*|^(1/3).
	// AIMD naturally approximates this schedule with zero new user parameters.
	//
	// Rules:
	//   ① Direction confirmed (counter ≥ 2):   ε += δ          [additive increase]
	//   ② Weak signal reversal:                ε *= decay       [multiplicative decrease]
	//   ③ Strong signal reversal:              ε = ε_default    [fast recovery]
	//   ④ Building (counter = 1):              no change        [hold]

	// epsilonMin is the lower bound for ε (convergence precision).
	// = DefaultK2CCEpsilon / 5
	epsilonMin = 0.02

	// epsilonMax is the upper bound for ε (exploration ceiling).
	// = DefaultK2CCEpsilon × 2
	epsilonMax = 0.20

	// epsilonDelta is the additive increase step for ε when direction is confirmed.
	epsilonDelta = 0.02

	// epsilonDecay is the multiplicative decrease factor on weak signal reversal.
	epsilonDecay = 0.5

	// epsilonZThreshold is the z-score for strong signal detection (95% confidence).
	epsilonZThreshold = 1.645

	// noiseEWMAAlpha is the smoothing factor for noise baseline updates.
	// Slow-moving (10% new) to provide stable noise floor estimate.
	noiseEWMAAlpha = 0.1

	// coldStartNoise is the initial noiseEWMA value.
	// Conservative (high) → early reversals classified as weak → starts with small ε.
	coldStartNoise = DefaultK2CCEpsilon * 10.0 // = 1.0
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

// K2CCConfig holds configuration for K2CC algorithm
type K2CCConfig struct {
	// Alpha is the loss penalty coefficient
	// Standard: 10.0-100.0 (loss-sensitive)
	// GFW mode: 0.1-2.0 (ignore non-congestion loss)
	// Adaptive: starts at InitialAlpha, adjusts based on loss
	Alpha float64

	// Beta is the latency penalty coefficient
	Beta float64

	// Epsilon is the probing rate change percentage
	Epsilon float64

	// AggressiveMode tolerates packet loss (for GFW/lossy networks)
	AggressiveMode bool

	// AdaptiveAlpha enables adaptive alpha mode
	// When true, alpha is automatically adjusted based on observed loss rate
	AdaptiveAlpha bool

	// InitialAlpha is the starting alpha value for adaptive mode
	InitialAlpha float64
}

// DefaultK2CCConfig returns default K2CC configuration
func DefaultK2CCConfig() *K2CCConfig {
	return &K2CCConfig{
		Alpha:          DefaultK2CCAlpha,
		Beta:           DefaultK2CCBeta,
		Epsilon:        DefaultK2CCEpsilon,
		AggressiveMode: false,
	}
}

// GFWK2CCConfig returns K2CC configuration optimized for GFW/lossy networks
func GFWK2CCConfig() *K2CCConfig {
	return &K2CCConfig{
		Alpha:          GFWK2CCAlpha,
		Beta:           DefaultK2CCBeta,
		Epsilon:        DefaultK2CCEpsilon,
		AggressiveMode: true,
		AdaptiveAlpha:  false,
		InitialAlpha:   InitialAlpha,
	}
}

// NewK2CCConfigAdaptive returns K2CC configuration with adaptive alpha mode
// Alpha is automatically adjusted based on observed loss rate
func NewK2CCConfigAdaptive() *K2CCConfig {
	return &K2CCConfig{
		Alpha:          InitialAlpha,
		Beta:           DefaultK2CCBeta,
		Epsilon:        DefaultK2CCEpsilon,
		AggressiveMode: false,
		AdaptiveAlpha:  true,
		InitialAlpha:   InitialAlpha,
	}
}

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
	if mi.Duration() >= MaxMIDuration {
		return true
	}

	// MinDuration gate: MI must live at least 1 RTT for loss visibility.
	// At high rates, 10 ACKs arrive in ~1ms but loss detection takes ~1 RTT.
	if mi.MinDuration > 0 && mi.Duration() < mi.MinDuration {
		return false
	}

	// Standard condition: >= 10 ACKs + RTT samples
	standardCondition := mi.PacketsAcked >= MinMIPackets && len(mi.RTTSamples) > 0

	// Relaxed condition: >= 3 ACKs + >= 30 seconds (for low-rate scenarios)
	relaxedCondition := mi.PacketsAcked >= MinMIPacketsRelaxed &&
		len(mi.RTTSamples) > 0 &&
		mi.Duration() >= MinMIDurationForRelaxed

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

// Throughput calculates throughput in bps and detects app-limited state
func (mi *MonitorInterval) Throughput() float64 {
	durationSecs := mi.Duration().Seconds()
	if durationSecs == 0 {
		return 0.0
	}
	throughput := float64(mi.BytesAcked) * 8.0 / durationSecs

	// Detect app-limited state: if actual sending is < 50% of target rate
	expectedBytes := mi.TargetRate * durationSecs / 8.0
	actualBytes := float64(mi.BytesSent)
	if actualBytes < expectedBytes*0.5 {
		mi.IsAppLimited = true
	}

	return throughput
}

// LossRate calculates loss rate, clamped to [0.0, 1.0].
// MI boundary artifacts can produce BytesLost > BytesSent when stale loss
// events from a previous MI are attributed to a new MI with few bytes sent.
func (mi *MonitorInterval) LossRate() float64 {
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
// Utility Function
// ============================================================================

// UtilityFunction calculates K2CC utility
type UtilityFunction struct {
	Alpha        float64
	Beta         float64
	MinRTT       time.Duration
	adaptiveMode bool
	lossHistory  []float64
}

// NewUtilityFunction creates a new utility function
func NewUtilityFunction(alpha, beta float64) *UtilityFunction {
	return &UtilityFunction{
		Alpha:        alpha,
		Beta:         beta,
		MinRTT:       time.Second, // Will be updated
		adaptiveMode: false,
		lossHistory:  nil,
	}
}

// NewUtilityFunctionAdaptive creates a new adaptive utility function
func NewUtilityFunctionAdaptive(initialAlpha, beta float64) *UtilityFunction {
	return &UtilityFunction{
		Alpha:        initialAlpha,
		Beta:         beta,
		MinRTT:       time.Second,
		adaptiveMode: true,
		lossHistory:  make([]float64, 0, LossHistorySize),
	}
}

// LossHistoryLen returns the number of loss rate samples in history
func (uf *UtilityFunction) LossHistoryLen() int {
	return len(uf.lossHistory)
}

// computeAdaptiveAlpha calculates adaptive alpha based on loss rate
// Formula: α = BASE_ALPHA / (1 + SENSITIVITY × avg_loss_rate)
func (uf *UtilityFunction) computeAdaptiveAlpha(currentLossRate float64) float64 {
	// Use average of historical loss rates for stability
	avgLoss := currentLossRate
	if len(uf.lossHistory) > 0 {
		sum := currentLossRate
		for _, l := range uf.lossHistory {
			sum += l
		}
		avgLoss = sum / float64(len(uf.lossHistory)+1)
	}

	// Inverse formula: higher loss -> lower alpha
	computed := AdaptiveBaseAlpha / (1.0 + AdaptiveSensitivity*avgLoss)

	// Clamp to bounds
	if computed < MinAlpha {
		return MinAlpha
	}
	if computed > MaxAlpha {
		return MaxAlpha
	}
	return computed
}

// UpdateAdaptiveAlpha updates alpha based on observed loss rate from a completed MI
func (uf *UtilityFunction) UpdateAdaptiveAlpha(mi *MonitorInterval) {
	if !uf.adaptiveMode {
		return
	}

	lossRate := mi.LossRate()

	// Update history
	uf.lossHistory = append(uf.lossHistory, lossRate)
	if len(uf.lossHistory) > LossHistorySize {
		uf.lossHistory = uf.lossHistory[1:]
	}

	// Compute new alpha
	newAlpha := uf.computeAdaptiveAlpha(lossRate)

	// EWMA smoothing: alpha = alpha * (1 - EWMA) + new_alpha * EWMA
	uf.Alpha = uf.Alpha*(1.0-AlphaEWMA) + newAlpha*AlphaEWMA
}

// UpdateMinRTT updates minimum RTT if sample is lower
func (uf *UtilityFunction) UpdateMinRTT(rtt time.Duration) {
	if rtt < uf.MinRTT && rtt > 0 {
		uf.MinRTT = rtt
	}
}

// Calculate computes utility for a Monitor Interval
// U = Throughput - α * LossRate * Throughput - β * ΔRtt
func (uf *UtilityFunction) Calculate(mi *MonitorInterval) float64 {
	throughput := mi.Throughput()
	lossRate := mi.LossRate()

	// Calculate RTT gradient (deviation from min)
	var deltaRTT float64
	avgRTT := mi.AvgRTT()
	if avgRTT > 0 {
		delta := max(avgRTT-uf.MinRTT, 0)
		deltaRTT = delta.Seconds() * 1000.0 // Convert to ms for scaling
	}

	// U = Throughput - α * LossRate * Throughput - β * ΔRtt
	// Scale throughput to Mbps for reasonable utility values
	throughputMbps := throughput / 1_000_000.0
	lossPenalty := uf.Alpha * lossRate * throughputMbps
	latencyPenalty := uf.Beta * deltaRTT

	utility := throughputMbps - lossPenalty - latencyPenalty

	// Clamp utility to prevent extreme negative values
	if utility < -10.0 {
		utility = -10.0
	}

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
}

// ensureCurrentMI ensures there's a current MI.
// rtt sets MinDuration on newly created MIs for loss visibility.
func (s *K2CCState) ensureCurrentMI(rate float64, rtt time.Duration) *MonitorInterval {
	if s.CurrentMI == nil {
		s.CurrentMI = NewMonitorInterval(s.NextMIID, rate)
		s.CurrentMI.MinDuration = rtt
		s.NextMIID++
	}
	return s.CurrentMI
}

// startNewMI starts a new Monitor Interval, reusing the previous MI's
// RTTSamples backing array to avoid a 160-byte allocation per MI rotation.
// rtt is the current smoothed RTT, used as MinDuration for loss visibility.
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
		MinDuration: rtt,
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

	mu sync.RWMutex
}

// NewK2CCSender creates a new K2CC sender with default config
func NewK2CCSender(maxRate int64) *K2CCSender {
	return NewK2CCSenderWithConfig(maxRate, DefaultK2CCConfig())
}

// NewCCSenderGFWMode creates a new K2CC sender optimized for GFW/lossy networks
func NewCCSenderGFWMode(maxRate int64) *K2CCSender {
	return NewK2CCSenderWithConfig(maxRate, GFWK2CCConfig())
}

// NewK2CCSenderAdaptive creates a new K2CC sender with adaptive alpha mode
func NewK2CCSenderAdaptive(maxRate int64) *K2CCSender {
	return NewK2CCSenderWithConfig(maxRate, NewK2CCConfigAdaptive())
}

// NewK2CCSenderWithConfig creates a new K2CC sender with custom config
func NewK2CCSenderWithConfig(maxRate int64, config *K2CCConfig) *K2CCSender {
	// Start at 30% of max (fast ramp via slow start)
	initialRate := float64(maxRate) * 0.3

	state := &K2CCState{
		CurrentRate: initialRate,
		Epsilon:     config.Epsilon,
		Mode:        K2CCModeSlowStart,
	}

	// Create utility function based on mode
	var utility *UtilityFunction
	if config.AdaptiveAlpha {
		utility = NewUtilityFunctionAdaptive(config.InitialAlpha, config.Beta)
	} else {
		utility = NewUtilityFunction(config.Alpha, config.Beta)
	}

	return &K2CCSender{
		config:         config,
		state:          state,
		utility:        utility,
		maxRate:        maxRate,
		cwnd:           rateToCwnd(initialRate, 100*time.Millisecond),
		maxSize:        congestion.ByteCount(1350),
		rtt:            100 * time.Millisecond,
		lastSendTime:   0,
		pacingBudget:   0,
		lastUpdateTime: 0,
		noiseEWMA:      coldStartNoise,
	}
}

// rateToCwnd converts sending rate to congestion window
func rateToCwnd(rateBps float64, rtt time.Duration) congestion.ByteCount {
	// cwnd = (rate_bps / 8) * rtt_seconds * multiplier
	bytesPerSec := rateBps / 8.0
	rttSecs := rtt.Seconds()
	cwnd := congestion.ByteCount(bytesPerSec * rttSecs * arcCwndMultiplier)

	cwnd = max(cwnd, arcMinWindow)
	cwnd = min(cwnd, arcMaxWindow)
	return cwnd
}

// SetRTTStatsProvider sets the RTT statistics provider
func (p *K2CCSender) SetRTTStatsProvider(provider congestion.RTTStatsProvider) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.rttStats = provider
}

// updateRate updates the sending rate with EWMA smoothing
func (p *K2CCSender) updateRate(newRate float64) {
	// Determine minimum rate based on scenario
	minRate := arcMinRate // 10 Mbps (upload)
	if float64(p.maxRate) >= arcMinDownloadRate {
		minRate = arcMinDownloadRate // 30 Mbps (download)
	}

	// Clamp new rate
	if newRate < minRate {
		newRate = minRate
	}
	if newRate > float64(p.maxRate) {
		newRate = float64(p.maxRate)
	}

	// EWMA smoothing
	p.state.CurrentRate = p.state.CurrentRate*(1.0-arcRateEWMAAlpha) + newRate*arcRateEWMAAlpha

	// Strict floor protection AFTER EWMA
	if p.state.CurrentRate < minRate {
		p.state.CurrentRate = minRate
	}

	// Update cwnd
	p.cwnd = rateToCwnd(p.state.CurrentRate, p.rtt)
}

// IsAdaptive returns whether the sender is in adaptive alpha mode
func (p *K2CCSender) IsAdaptive() bool {
	return p.config.AdaptiveAlpha
}

// processCompletedMI processes a completed Monitor Interval
func (p *K2CCSender) processCompletedMI(mi *MonitorInterval) {
	// Rate probing (BEFORE app-limited check)
	// Every N MIs, probe higher rate to recover from previous reductions
	if p.state.NextMIID%RateProbingInterval == 0 {
		probeRate := p.state.CurrentRate * (1.0 + RateProbingIncrease)
		if probeRate <= float64(p.maxRate) {
			p.updateRate(probeRate)
			// Reset utility counters to give new rate a fair chance
			p.state.UtilityIncreases = 0
			p.state.UtilityDecreases = 0
		}
	}

	// Check if app-limited (calculate throughput triggers detection)
	_ = mi.Throughput()
	if mi.IsAppLimited {
		return
	}

	// Update min RTT
	avgRTT := mi.AvgRTT()
	if avgRTT > 0 {
		p.utility.UpdateMinRTT(avgRTT)
	}

	// Update adaptive alpha if in adaptive mode
	p.utility.UpdateAdaptiveAlpha(mi)

	// Calculate utility (uses potentially updated alpha)
	utility := p.utility.Calculate(mi)

	switch p.state.Mode {
	case K2CCModeSlowStart:
		// Exit slow start if rate has reached maxRate — no room to grow.
		// Use 99% threshold because EWMA smoothing in updateRate means
		// CurrentRate asymptotically approaches but never equals maxRate.
		if p.state.CurrentRate >= float64(p.maxRate)*0.99 {
			p.state.Mode = K2CCModeDecision
			p.state.UtilityIncreases = 0
			p.state.UtilityDecreases = 0
			break
		}
		// Check if we should exit slow start
		if p.state.hasLastUtility && utility < p.state.lastUtility*0.95 {
			// Utility dropped, exit slow start
			p.state.Mode = K2CCModeDecision
			p.state.UtilityIncreases = 0
			p.state.UtilityDecreases = 0
		} else {
			// Continue slow start: multiply by growth factor
			newRate := p.state.CurrentRate * arcSlowStartFactor
			p.updateRate(newRate)
		}

	case K2CCModeDecision, K2CCModeRateAdjusting:
		if p.state.hasLastUtility {
			gradient := utility - p.state.lastUtility

			// Capture previous state for AIMD-ε reversal detection
			prevConfirmed := p.state.UtilityIncreases >= 2 || p.state.UtilityDecreases >= 2

			// Update direction counters
			if gradient > 0 {
				p.state.UtilityIncreases++
				p.state.UtilityDecreases = 0
			} else {
				p.state.UtilityDecreases++
				p.state.UtilityIncreases = 0
			}

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
				utilityScale := p.state.CurrentRate / 1_000_000.0 // Mbps (same as utility)
				if utilityScale < 1.0 {
					utilityScale = 1.0
				}
				threshold := epsilonZThreshold * p.noiseEWMA * utilityScale

				if absGrad > threshold {
					// ③ Strong signal: genuine regime change → fast recovery
					p.state.Epsilon = DefaultK2CCEpsilon
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

			// Apply gradient ascent (uses adapted epsilon)
			var adjustment float64
			if p.state.UtilityIncreases >= 2 {
				// Keep increasing
				adjustment = p.state.CurrentRate * p.state.Epsilon
			} else if p.state.UtilityDecreases >= 2 {
				// Reverse direction
				adjustment = -p.state.CurrentRate * p.state.Epsilon
			} else {
				// Probing: alternate high/low
				sign := 1.0
				if p.state.NextMIID%2 == 1 {
					sign = -1.0
				}
				adjustment = sign * p.state.CurrentRate * p.state.Epsilon
			}

			newRate := p.state.CurrentRate + adjustment
			p.updateRate(newRate)
		}
	}

	p.state.lastUtility = utility
	p.state.hasLastUtility = true

	// Log completed MI for K2CC observability (benchmarking, GFW analysis).
	// Direction: "+1" = rate increasing, "-1" = rate decreasing, "probe" = undecided.
	dir := "probe"
	if p.state.UtilityIncreases >= 2 {
		dir = "+1"
	} else if p.state.UtilityDecreases >= 2 {
		dir = "-1"
	}
	slog.Debug("k2cc: MI complete",
		"mi", mi.ID,
		"rate", fmt.Sprintf("%.1fM", p.state.CurrentRate/1_000_000),
		"throughput", fmt.Sprintf("%.1fM", mi.Throughput()/1_000_000),
		"loss", fmt.Sprintf("%.1f%%", mi.LossRate()*100),
		"rtt", mi.AvgRTT(),
		"epsilon", fmt.Sprintf("%.3f", p.state.Epsilon),
		"alpha", fmt.Sprintf("%.2f", p.utility.Alpha),
		"mode", p.state.Mode.String(),
		"dir", dir,
	)
}

// ============================================================================
// congestion.CongestionControl Interface Implementation
// ============================================================================

// TimeUntilSend returns when the next packet can be sent
func (p *K2CCSender) TimeUntilSend(bytesInFlight congestion.ByteCount) congestion.Time {
	p.mu.RLock()
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
	p.cwnd = rateToCwnd(p.state.CurrentRate, p.rtt)
}

// OnCongestionEvent is called when congestion is detected
func (p *K2CCSender) OnCongestionEvent(
	number congestion.PacketNumber,
	lostBytes congestion.ByteCount,
	priorInFlight congestion.ByteCount,
) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Record loss in current MI — but skip stale losses from previous MI.
	// QUIC reports loss for packets sent in old MI after new MI has started.
	// Without filtering: BytesLost >> BytesSent → LossRate 300-900% → false CRITICAL.
	mi := p.state.ensureCurrentMI(p.state.CurrentRate, p.rtt)

	if mi.PacketsSent > 0 && int64(number) < mi.FirstPacketNumber {
		// Stale loss: packet belongs to a previous MI. Don't pollute current MI.
		slog.Debug("arc: skipping stale loss",
			"lostPkt", number,
			"miFirstPkt", mi.FirstPacketNumber,
		)
		return
	}

	mi.BytesLost += uint64(lostBytes)

	// In aggressive/GFW mode, we mostly ignore loss
	// The utility function handles this via low alpha
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
		rtt = 100 * time.Millisecond
	}

	cwnd := rateToCwnd(p.state.CurrentRate, rtt)
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
	defer p.mu.RUnlock()
	s := CCSnapshot{
		SendingRate: p.state.CurrentRate,
		Epsilon:     p.state.Epsilon,
		RTT:         p.rtt,
		MinRTT:      p.utility.MinRTT,
		Cwnd:        int64(p.cwnd),
		Mode:        p.state.Mode,
	}
	if mi := p.state.CurrentMI; mi != nil {
		if mi.BytesSent >= minSnapshotBytes {
			s.LossRate = mi.LossRate()
			s.Throughput = mi.Throughput()
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
var _ congestion.CongestionControl = (*K2CCSender)(nil)
