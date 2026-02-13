// PCC Vivace - Performance-oriented Congestion Control
package pcc

import (
	"sync"
	"time"

	"github.com/apernet/quic-go/congestion"
)

// ============================================================================
// PCC Vivace - Performance-oriented Congestion Control
//
// PCC uses gradient ascent on a utility function to find optimal sending rate:
// U = Throughput - α * LossRate * Throughput - β * ΔRtt
//
// Key features:
// - Rate-based (not window-based like Cubic)
// - Uses Monitor Intervals (MI) to probe network capacity
// - GFW mode: low α to tolerate non-congestion packet loss
//
// References:
// - PCC Vivace (NSDI '18): https://www.usenix.org/conference/nsdi18/presentation/dong
// ============================================================================

// Constants for PCC algorithm
const (
	// Default max rate for PCC (500 Mbps — generous upper bound)
	DefaultMaxRate = int64(500_000_000)

	// Minimum congestion window (10 packets worth)
	pccMinWindow = congestion.ByteCount(14720) // 10 × 1472 (typical MTU)

	// Maximum congestion window (1GB)
	pccMaxWindow = congestion.ByteCount(1_000_000_000)

	// ============================================================================
	// Rate Boundaries - Aligned with realistic bandwidth constraints
	// ============================================================================

	// Minimum upload rate (10 Mbps - realistic home upload minimum)
	pccMinRate = 10_000_000.0 // 10 Mbps

	// Minimum download rate (30 Mbps - realistic home download minimum)
	// Used when maxRate >= 30 Mbps (download scenarios)
	pccMinDownloadRate = 30_000_000.0 // 30 Mbps

	// ============================================================================
	// Performance Tuning - Optimized for fast convergence
	// ============================================================================

	// Probing epsilon: exploration step size
	// Old: 0.05 (5%, conservative), New: 0.15 (15%, aggressive)
	DefaultPCCEpsilon = 0.15

	// Beta: latency penalty coefficient
	// Low: throughput-prioritized for tunnel use
	DefaultPCCBeta = 0.1

	// EWMA smoothing factor for rate updates
	// Old: 0.7 (70% new value), New: 0.9 (90% new value, faster response)
	pccRateEWMAAlpha = 0.9

	// Slow start growth factor
	// Old: 2.0 (double each MI), New: 2.5 (2.5x each MI, faster ramp-up)
	pccSlowStartFactor = 2.5

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
	DefaultPCCAlpha = 10.0

	// Alpha for GFW/aggressive mode (low loss penalty)
	GFWPCCAlpha = 0.5

	// Congestion window multiplier (for rate to cwnd conversion)
	// 2.0× BDP matches BBR's cwnd gain, provides headroom for bursty ACK patterns
	pccCwndMultiplier = 2.0

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
)

// PCCMode represents the PCC operating mode
type PCCMode int

const (
	// PCCModeSlowStart - exponential growth until utility drops
	PCCModeSlowStart PCCMode = iota
	// PCCModeDecision - probing to determine rate direction
	PCCModeDecision
	// PCCModeRateAdjusting - applying gradient updates
	PCCModeRateAdjusting
)

// ============================================================================
// PCC Configuration
// ============================================================================

// PCCConfig holds configuration for PCC algorithm
type PCCConfig struct {
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

// DefaultPCCConfig returns default PCC configuration
func DefaultPCCConfig() *PCCConfig {
	return &PCCConfig{
		Alpha:          DefaultPCCAlpha,
		Beta:           DefaultPCCBeta,
		Epsilon:        DefaultPCCEpsilon,
		AggressiveMode: false,
	}
}

// GFWPCCConfig returns PCC configuration optimized for GFW/lossy networks
func GFWPCCConfig() *PCCConfig {
	return &PCCConfig{
		Alpha:          GFWPCCAlpha,
		Beta:           DefaultPCCBeta,
		Epsilon:        DefaultPCCEpsilon,
		AggressiveMode: true,
		AdaptiveAlpha:  false,
		InitialAlpha:   InitialAlpha,
	}
}

// NewPCCConfigAdaptive returns PCC configuration with adaptive alpha mode
// Alpha is automatically adjusted based on observed loss rate
func NewPCCConfigAdaptive() *PCCConfig {
	return &PCCConfig{
		Alpha:          InitialAlpha,
		Beta:           DefaultPCCBeta,
		Epsilon:        DefaultPCCEpsilon,
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

// IsComplete checks if this MI has enough data
func (mi *MonitorInterval) IsComplete() bool {
	// Standard condition: >= 10 ACKs + RTT samples
	standardCondition := mi.PacketsAcked >= MinMIPackets && len(mi.RTTSamples) > 0

	// Timeout protection: force complete after 60 seconds
	timeoutCondition := mi.Duration() >= MaxMIDuration

	// Relaxed condition: >= 3 ACKs + >= 30 seconds (for low-rate scenarios)
	relaxedCondition := mi.PacketsAcked >= MinMIPacketsRelaxed &&
		len(mi.RTTSamples) > 0 &&
		mi.Duration() >= MinMIDurationForRelaxed

	return standardCondition || timeoutCondition || relaxedCondition
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

// LossRate calculates loss rate (0.0 to 1.0)
func (mi *MonitorInterval) LossRate() float64 {
	if mi.BytesSent == 0 {
		return 0.0
	}
	return float64(mi.BytesLost) / float64(mi.BytesSent)
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

// ============================================================================
// Utility Function
// ============================================================================

// UtilityFunction calculates PCC utility
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
// PCC State
// ============================================================================

// PCCState holds the PCC algorithm state
type PCCState struct {
	CurrentRate      float64
	Epsilon          float64
	Mode             PCCMode
	CurrentMI        *MonitorInterval
	NextMIID         uint64
	LastUtility      *float64
	UtilityIncreases uint32
	UtilityDecreases uint32
}

// ensureCurrentMI ensures there's a current MI
func (s *PCCState) ensureCurrentMI(rate float64) *MonitorInterval {
	if s.CurrentMI == nil {
		s.CurrentMI = NewMonitorInterval(s.NextMIID, rate)
		s.NextMIID++
	}
	return s.CurrentMI
}

// startNewMI starts a new Monitor Interval
func (s *PCCState) startNewMI(rate float64) *MonitorInterval {
	if s.CurrentMI != nil {
		s.CurrentMI.Complete()
	}
	s.CurrentMI = NewMonitorInterval(s.NextMIID, rate)
	s.NextMIID++
	return s.CurrentMI
}

// ============================================================================
// PCC Sender (Congestion Controller)
// ============================================================================

// PCCSender implements PCC Vivace congestion control
type PCCSender struct {
	config   *PCCConfig
	state    *PCCState
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

	mu sync.RWMutex
}

// NewPCCSender creates a new PCC sender with default config
func NewPCCSender(maxRate int64) *PCCSender {
	return NewPCCSenderWithConfig(maxRate, DefaultPCCConfig())
}

// NewPCCSenderGFWMode creates a new PCC sender optimized for GFW/lossy networks
func NewPCCSenderGFWMode(maxRate int64) *PCCSender {
	return NewPCCSenderWithConfig(maxRate, GFWPCCConfig())
}

// NewPCCSenderAdaptive creates a new PCC sender with adaptive alpha mode
func NewPCCSenderAdaptive(maxRate int64) *PCCSender {
	return NewPCCSenderWithConfig(maxRate, NewPCCConfigAdaptive())
}

// NewPCCSenderWithConfig creates a new PCC sender with custom config
func NewPCCSenderWithConfig(maxRate int64, config *PCCConfig) *PCCSender {
	// Start at 30% of max (fast ramp via slow start)
	initialRate := float64(maxRate) * 0.3

	state := &PCCState{
		CurrentRate: initialRate,
		Epsilon:     config.Epsilon,
		Mode:        PCCModeSlowStart,
	}

	// Create utility function based on mode
	var utility *UtilityFunction
	if config.AdaptiveAlpha {
		utility = NewUtilityFunctionAdaptive(config.InitialAlpha, config.Beta)
	} else {
		utility = NewUtilityFunction(config.Alpha, config.Beta)
	}

	return &PCCSender{
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
	}
}

// rateToCwnd converts sending rate to congestion window
func rateToCwnd(rateBps float64, rtt time.Duration) congestion.ByteCount {
	// cwnd = (rate_bps / 8) * rtt_seconds * multiplier
	bytesPerSec := rateBps / 8.0
	rttSecs := rtt.Seconds()
	cwnd := congestion.ByteCount(bytesPerSec * rttSecs * pccCwndMultiplier)

	cwnd = max(cwnd, pccMinWindow)
	cwnd = min(cwnd, pccMaxWindow)
	return cwnd
}

// SetRTTStatsProvider sets the RTT statistics provider
func (p *PCCSender) SetRTTStatsProvider(provider congestion.RTTStatsProvider) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.rttStats = provider
}

// updateRate updates the sending rate with EWMA smoothing
func (p *PCCSender) updateRate(newRate float64) {
	// Determine minimum rate based on scenario
	minRate := pccMinRate // 10 Mbps (upload)
	if float64(p.maxRate) >= pccMinDownloadRate {
		minRate = pccMinDownloadRate // 30 Mbps (download)
	}

	// Clamp new rate
	if newRate < minRate {
		newRate = minRate
	}
	if newRate > float64(p.maxRate) {
		newRate = float64(p.maxRate)
	}

	// EWMA smoothing
	p.state.CurrentRate = p.state.CurrentRate*(1.0-pccRateEWMAAlpha) + newRate*pccRateEWMAAlpha

	// Strict floor protection AFTER EWMA
	if p.state.CurrentRate < minRate {
		p.state.CurrentRate = minRate
	}

	// Update cwnd
	p.cwnd = rateToCwnd(p.state.CurrentRate, p.rtt)
}

// IsAdaptive returns whether the sender is in adaptive alpha mode
func (p *PCCSender) IsAdaptive() bool {
	return p.config.AdaptiveAlpha
}

// processCompletedMI processes a completed Monitor Interval
func (p *PCCSender) processCompletedMI(mi *MonitorInterval) {
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
	case PCCModeSlowStart:
		// Check if we should exit slow start
		if p.state.LastUtility != nil && utility < *p.state.LastUtility*0.95 {
			// Utility dropped, exit slow start
			p.state.Mode = PCCModeDecision
			p.state.UtilityIncreases = 0
			p.state.UtilityDecreases = 0
		} else {
			// Continue slow start: multiply by growth factor
			newRate := p.state.CurrentRate * pccSlowStartFactor
			p.updateRate(newRate)
		}

	case PCCModeDecision, PCCModeRateAdjusting:
		if p.state.LastUtility != nil {
			gradient := utility - *p.state.LastUtility

			if gradient > 0 {
				p.state.UtilityIncreases++
				p.state.UtilityDecreases = 0
			} else {
				p.state.UtilityDecreases++
				p.state.UtilityIncreases = 0
			}

			// Apply gradient ascent
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

	p.state.LastUtility = &utility
}

// ============================================================================
// congestion.CongestionControl Interface Implementation
// ============================================================================

// TimeUntilSend returns when the next packet can be sent
func (p *PCCSender) TimeUntilSend(bytesInFlight congestion.ByteCount) congestion.Time {
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
func (p *PCCSender) HasPacingBudget(now congestion.Time) bool {
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
func (p *PCCSender) OnPacketSent(
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

	mi := p.state.ensureCurrentMI(p.state.CurrentRate)
	mi.BytesSent += uint64(bytes)
	mi.PacketsSent++
}

// CanSend returns true if we can send more data
func (p *PCCSender) CanSend(bytesInFlight congestion.ByteCount) bool {
	return bytesInFlight < p.GetCongestionWindow()
}

// MaybeExitSlowStart is called to check if we should exit slow start
func (p *PCCSender) MaybeExitSlowStart() {
	// PCC handles this in processCompletedMI
}

// OnPacketAcked is called when a packet is acknowledged
func (p *PCCSender) OnPacketAcked(
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
	mi := p.state.ensureCurrentMI(p.state.CurrentRate)
	mi.BytesAcked += uint64(ackedBytes)
	mi.PacketsAcked++
	mi.RTTSamples = append(mi.RTTSamples, p.rtt)

	// Check if MI is complete
	if mi.IsComplete() {
		miCopy := *mi
		mi.Complete()

		// Process the completed MI
		p.processCompletedMI(&miCopy)

		// Start new MI
		p.state.startNewMI(p.state.CurrentRate)
	}

	// Update cwnd
	p.cwnd = rateToCwnd(p.state.CurrentRate, p.rtt)
}

// OnCongestionEvent is called when congestion is detected
func (p *PCCSender) OnCongestionEvent(
	number congestion.PacketNumber,
	lostBytes congestion.ByteCount,
	priorInFlight congestion.ByteCount,
) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Record loss in current MI
	mi := p.state.ensureCurrentMI(p.state.CurrentRate)
	mi.BytesLost += uint64(lostBytes)

	// In aggressive/GFW mode, we mostly ignore loss
	// The utility function handles this via low alpha
}

// OnRetransmissionTimeout is called when RTO expires
func (p *PCCSender) OnRetransmissionTimeout(packetsRetransmitted bool) {
	// PCC doesn't reduce cwnd on timeout
}

// SetMaxDatagramSize sets the maximum datagram size
func (p *PCCSender) SetMaxDatagramSize(size congestion.ByteCount) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.maxSize = size
}

// InSlowStart returns whether we're in slow start mode
func (p *PCCSender) InSlowStart() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.state.Mode == PCCModeSlowStart
}

// InRecovery returns whether we're in recovery mode
func (p *PCCSender) InRecovery() bool {
	return false // PCC doesn't use traditional recovery
}

// getCongestionWindowLocked computes cwnd without acquiring the lock.
// Caller must already hold p.mu (read or write).
func (p *PCCSender) getCongestionWindowLocked() congestion.ByteCount {
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
func (p *PCCSender) GetCongestionWindow() congestion.ByteCount {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.getCongestionWindowLocked()
}

// Verify PCCSender implements CongestionControl interface
var _ congestion.CongestionControl = (*PCCSender)(nil)
