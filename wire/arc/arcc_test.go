// K2CC Vivace tests
package arc

import (
	"testing"
	"time"

	"github.com/apernet/quic-go/congestion"
)

// ============================================================================
// Tests for K2CCSender - K2CC Vivace Congestion Control
// ============================================================================

func TestNewK2CCSender(t *testing.T) {
	maxRate := int64(100_000_000) // 100 Mbps
	sender := NewK2CCSender(maxRate)

	if sender == nil {
		t.Fatal("sender should not be nil")
	}

	if sender.maxRate != maxRate {
		t.Errorf("expected maxRate=%d, got %d", maxRate, sender.maxRate)
	}

	// Verify default config
	if sender.config.Alpha != DefaultK2CCAlpha {
		t.Errorf("expected default alpha=%f, got %f", DefaultK2CCAlpha, sender.config.Alpha)
	}
}

func TestNewCCSenderGFWMode(t *testing.T) {
	maxRate := int64(100_000_000)
	sender := NewCCSenderGFWMode(maxRate)

	if sender == nil {
		t.Fatal("sender should not be nil")
	}

	// GFW mode should have low alpha
	if sender.config.Alpha > GFWK2CCAlpha*2 {
		t.Errorf("expected GFW alpha around %f, got %f", GFWK2CCAlpha, sender.config.Alpha)
	}

	if !sender.config.AggressiveMode {
		t.Error("GFW mode should be aggressive")
	}
}

// ============================================================================
// Tests for MonitorInterval
// ============================================================================

func TestMonitorInterval_New(t *testing.T) {
	mi := NewMonitorInterval(1, 10_000_000.0)

	if mi.ID != 1 {
		t.Errorf("expected ID=1, got %d", mi.ID)
	}
	if mi.TargetRate != 10_000_000.0 {
		t.Errorf("expected TargetRate=10_000_000, got %f", mi.TargetRate)
	}
	if mi.BytesSent != 0 {
		t.Errorf("expected BytesSent=0, got %d", mi.BytesSent)
	}
	if mi.IsComplete() {
		t.Error("new MI should not be complete")
	}
}

func TestMonitorInterval_IsComplete(t *testing.T) {
	mi := NewMonitorInterval(1, 10_000_000.0)

	// Not complete initially
	if mi.IsComplete() {
		t.Error("should not be complete initially")
	}

	// Add enough data
	mi.PacketsAcked = MinMIPackets
	mi.RTTSamples = append(mi.RTTSamples, 50*time.Millisecond)

	if !mi.IsComplete() {
		t.Error("should be complete with enough packets and RTT samples")
	}
}

func TestMonitorInterval_LossRate(t *testing.T) {
	mi := NewMonitorInterval(1, 10_000_000.0)
	mi.BytesSent = 10000
	mi.BytesLost = 2000

	lossRate := mi.LossRate()
	expected := 0.2 // 20%

	if lossRate < expected-0.01 || lossRate > expected+0.01 {
		t.Errorf("expected lossRate=%f, got %f", expected, lossRate)
	}
}

func TestMonitorInterval_Throughput(t *testing.T) {
	mi := NewMonitorInterval(1, 10_000_000.0)
	mi.BytesAcked = 125000 // 1 Mbps for 1 second in bytes

	// Simulate 1 second duration
	mi.StartTime = time.Now().Add(-time.Second)
	mi.Complete()

	throughput := mi.Throughput()
	// Expected: 125000 bytes * 8 bits / 1 second = 1,000,000 bps
	expectedMin := 900_000.0 // Allow some tolerance
	expectedMax := 1_100_000.0

	if throughput < expectedMin || throughput > expectedMax {
		t.Errorf("expected throughput around 1Mbps, got %f bps", throughput)
	}
}

func TestMonitorInterval_AvgRTT(t *testing.T) {
	mi := NewMonitorInterval(1, 10_000_000.0)

	// No samples
	if mi.AvgRTT() != 0 {
		t.Error("expected 0 with no RTT samples")
	}

	// Add samples
	mi.RTTSamples = append(mi.RTTSamples, 50*time.Millisecond)
	mi.RTTSamples = append(mi.RTTSamples, 60*time.Millisecond)

	avgRTT := mi.AvgRTT()
	expected := 55 * time.Millisecond

	if avgRTT != expected {
		t.Errorf("expected avgRTT=%v, got %v", expected, avgRTT)
	}
}

// ============================================================================
// Tests for Utility Function
// ============================================================================

func TestUtilityFunction_Calculate(t *testing.T) {
	uf := NewUtilityFunction(DefaultK2CCAlpha, DefaultK2CCBeta)

	mi := NewMonitorInterval(1, 10_000_000.0)
	mi.BytesSent = 125000
	mi.BytesAcked = 125000 // No loss
	mi.BytesLost = 0
	mi.RTTSamples = append(mi.RTTSamples, 50*time.Millisecond)
	mi.StartTime = time.Now().Add(-time.Second)
	mi.Complete()

	u := uf.Calculate(mi)

	// With no loss and minimal latency, utility should be positive
	if u <= 0 {
		t.Errorf("expected positive utility, got %f", u)
	}
}

func TestUtilityFunction_LossPenalty(t *testing.T) {
	uf := NewUtilityFunction(DefaultK2CCAlpha, DefaultK2CCBeta)

	// Create two MIs: one with loss, one without
	miNoLoss := NewMonitorInterval(1, 10_000_000.0)
	miNoLoss.BytesSent = 125000
	miNoLoss.BytesAcked = 125000
	miNoLoss.BytesLost = 0
	miNoLoss.RTTSamples = append(miNoLoss.RTTSamples, 50*time.Millisecond)
	miNoLoss.StartTime = time.Now().Add(-time.Second)
	miNoLoss.Complete()

	miWithLoss := NewMonitorInterval(2, 10_000_000.0)
	miWithLoss.BytesSent = 125000
	miWithLoss.BytesAcked = 100000
	miWithLoss.BytesLost = 25000 // 20% loss
	miWithLoss.RTTSamples = append(miWithLoss.RTTSamples, 50*time.Millisecond)
	miWithLoss.StartTime = time.Now().Add(-time.Second)
	miWithLoss.Complete()

	uNoLoss := uf.Calculate(miNoLoss)
	uWithLoss := uf.Calculate(miWithLoss)

	// Utility with loss should be lower
	if uWithLoss >= uNoLoss {
		t.Errorf("utility with loss (%f) should be less than without loss (%f)", uWithLoss, uNoLoss)
	}
}

func TestUtilityFunction_GFWModeToleratesLoss(t *testing.T) {
	standard := NewUtilityFunction(DefaultK2CCAlpha, DefaultK2CCBeta)
	gfw := NewUtilityFunction(GFWK2CCAlpha, DefaultK2CCBeta)

	mi := NewMonitorInterval(1, 10_000_000.0)
	mi.BytesSent = 125000
	mi.BytesAcked = 100000
	mi.BytesLost = 25000 // 20% loss
	mi.RTTSamples = append(mi.RTTSamples, 50*time.Millisecond)
	mi.StartTime = time.Now().Add(-time.Second)
	mi.Complete()

	uStandard := standard.Calculate(mi)
	uGFW := gfw.Calculate(mi)

	// GFW mode should be less affected by loss (higher utility)
	if uGFW <= uStandard {
		t.Errorf("GFW utility (%f) should be greater than standard (%f) with loss", uGFW, uStandard)
	}
}

// ============================================================================
// Tests for K2CCSender Congestion Window
// ============================================================================

func TestK2CCSender_GetCongestionWindow(t *testing.T) {
	sender := NewK2CCSender(100_000_000) // 100 Mbps max

	// Set up mock RTT provider
	mockRTT := &mockRTTStatsProvider{smoothedRTT: 100 * time.Millisecond}
	sender.SetRTTStatsProvider(mockRTT)

	cwnd := sender.GetCongestionWindow()

	// Window should be positive and reasonable
	if cwnd == 0 {
		t.Error("congestion window should not be 0")
	}

	// Should be at least minimum window
	if cwnd < arcMinWindow {
		t.Errorf("cwnd %d should be >= minWindow %d", cwnd, arcMinWindow)
	}
}

func TestK2CCSender_CanSend(t *testing.T) {
	sender := NewK2CCSender(100_000_000)
	mockRTT := &mockRTTStatsProvider{smoothedRTT: 100 * time.Millisecond}
	sender.SetRTTStatsProvider(mockRTT)

	// With 0 bytes in flight, should be able to send
	if !sender.CanSend(0) {
		t.Error("should be able to send with 0 bytes in flight")
	}

	// With window full, should not be able to send
	cwnd := sender.GetCongestionWindow()
	if sender.CanSend(cwnd + 10000) {
		t.Error("should not be able to send when over congestion window")
	}
}

func TestK2CCSender_InSlowStart(t *testing.T) {
	sender := NewK2CCSender(100_000_000)

	// Should start in slow start mode
	if !sender.InSlowStart() {
		t.Error("should start in slow start mode")
	}
}

// ============================================================================
// Tests for Rate Adjustment (Gradient Ascent)
// ============================================================================

func TestK2CCSender_RateToCwnd(t *testing.T) {
	// Test rate to cwnd conversion
	// cwnd = (rate_bps / 8) * RTT_seconds * multiplier
	rate := 10_000_000.0 // 10 Mbps
	rtt := 100 * time.Millisecond

	cwnd := rateToCwnd(rate, rtt)

	// Expected: (10_000_000 / 8) * 0.1 * 1.5 = 187,500 bytes
	expectedMin := congestion.ByteCount(150_000)
	expectedMax := congestion.ByteCount(250_000)

	if cwnd < expectedMin || cwnd > expectedMax {
		t.Errorf("expected cwnd between %d and %d, got %d", expectedMin, expectedMax, cwnd)
	}
}

func TestK2CCSender_OnPacketAcked_UpdatesState(t *testing.T) {
	sender := NewK2CCSender(100_000_000)
	mockRTT := &mockRTTStatsProvider{smoothedRTT: 100 * time.Millisecond}
	sender.SetRTTStatsProvider(mockRTT)

	// Simulate sending some packets
	now := time.Now()
	sent := now.Add(-50 * time.Millisecond)

	sender.OnPacketSent(congestion.Time(sent.UnixMicro()), 0, 1, 1472, true)
	sender.OnPacketAcked(1, 1472, 1472, congestion.Time(now.UnixMicro()))

	// Check that MI is tracking data
	if sender.state.CurrentMI == nil {
		t.Error("current MI should not be nil after packet acked")
	}
	if sender.state.CurrentMI.BytesAcked == 0 {
		t.Error("bytes acked should be > 0")
	}
}

func TestK2CCSender_OnCongestionEvent_RecordsLoss(t *testing.T) {
	sender := NewK2CCSender(100_000_000)
	mockRTT := &mockRTTStatsProvider{smoothedRTT: 100 * time.Millisecond}
	sender.SetRTTStatsProvider(mockRTT)

	// Ensure there's a current MI
	sender.state.ensureCurrentMI(sender.state.CurrentRate, 0)

	// Simulate loss
	sender.OnCongestionEvent(1, 1472, 10000)

	if sender.state.CurrentMI.BytesLost == 0 {
		t.Error("bytes lost should be recorded")
	}
}

// ============================================================================
// Tests for K2CC Config
// ============================================================================

func TestK2CCConfig_Default(t *testing.T) {
	config := DefaultK2CCConfig()

	if config.Alpha != DefaultK2CCAlpha {
		t.Errorf("expected alpha=%f, got %f", DefaultK2CCAlpha, config.Alpha)
	}
	if config.Beta != DefaultK2CCBeta {
		t.Errorf("expected beta=%f, got %f", DefaultK2CCBeta, config.Beta)
	}
	if config.AggressiveMode {
		t.Error("default should not be aggressive mode")
	}
}

func TestK2CCConfig_GFWMode(t *testing.T) {
	config := GFWK2CCConfig()

	if config.Alpha != GFWK2CCAlpha {
		t.Errorf("expected alpha=%f, got %f", GFWK2CCAlpha, config.Alpha)
	}
	if !config.AggressiveMode {
		t.Error("GFW mode should be aggressive")
	}
}

// ============================================================================
// Adaptive Alpha Tests (TDD - these should fail until implementation)
// ============================================================================

func TestAdaptiveAlphaCalculation(t *testing.T) {
	// Formula: α = BASE_ALPHA / (1 + SENSITIVITY × loss_rate)
	// With AdaptiveBaseAlpha = 10.0, AdaptiveSensitivity = 50.0
	utility := NewUtilityFunctionAdaptive(InitialAlpha, DefaultK2CCBeta)

	// 0% loss -> alpha = 10.0 / (1 + 50 * 0) = 10.0
	alpha0 := utility.computeAdaptiveAlpha(0.0)
	if diff := alpha0 - 10.0; diff < -0.01 || diff > 0.01 {
		t.Errorf("0%% loss: expected 10.0, got %f", alpha0)
	}

	// 10% loss -> alpha = 10.0 / (1 + 50 * 0.1) = 10.0 / 6.0 = 1.67
	alpha10 := utility.computeAdaptiveAlpha(0.1)
	if diff := alpha10 - 1.67; diff < -0.1 || diff > 0.1 {
		t.Errorf("10%% loss: expected 1.67, got %f", alpha10)
	}

	// 20% loss -> alpha = 10.0 / (1 + 50 * 0.2) = 10.0 / 11.0 = 0.91
	alpha20 := utility.computeAdaptiveAlpha(0.2)
	if diff := alpha20 - 0.91; diff < -0.1 || diff > 0.1 {
		t.Errorf("20%% loss: expected 0.91, got %f", alpha20)
	}

	// 30% loss -> alpha = 10.0 / (1 + 50 * 0.3) = 10.0 / 16.0 = 0.625
	alpha30 := utility.computeAdaptiveAlpha(0.3)
	if diff := alpha30 - 0.625; diff < -0.1 || diff > 0.1 {
		t.Errorf("30%% loss: expected 0.625, got %f", alpha30)
	}
}

func TestAdaptiveAlphaBounds(t *testing.T) {
	utility := NewUtilityFunctionAdaptive(InitialAlpha, DefaultK2CCBeta)

	// Very high loss should be clamped to MinAlpha (0.1)
	alphaExtreme := utility.computeAdaptiveAlpha(1.0) // 100% loss
	if alphaExtreme < MinAlpha {
		t.Errorf("Expected alpha >= %f (MinAlpha), got %f", MinAlpha, alphaExtreme)
	}

	// Zero loss should be clamped to MaxAlpha (10.0)
	alphaZero := utility.computeAdaptiveAlpha(0.0)
	if alphaZero > MaxAlpha {
		t.Errorf("Expected alpha <= %f (MaxAlpha), got %f", MaxAlpha, alphaZero)
	}
}

func TestAdaptiveAlphaEWMASmoothing(t *testing.T) {
	utility := NewUtilityFunctionAdaptive(InitialAlpha, DefaultK2CCBeta)

	// Initial alpha should be InitialAlpha (2.0)
	if diff := utility.Alpha - InitialAlpha; diff < -0.01 || diff > 0.01 {
		t.Errorf("Initial alpha should be %f, got %f", InitialAlpha, utility.Alpha)
	}

	// Create MI with 10% loss
	mi := NewMonitorInterval(1, 10_000_000.0)
	mi.BytesSent = 10000
	mi.BytesAcked = 9000
	mi.BytesLost = 1000 // 10% loss
	mi.PacketsAcked = MinMIPackets
	mi.RTTSamples = append(mi.RTTSamples, 50*time.Millisecond)
	mi.EndTime = mi.StartTime.Add(time.Second)

	// Update alpha with MI
	utility.UpdateAdaptiveAlpha(mi)

	// Alpha should move toward computed value but not jump there
	// EWMA with 0.3 weight: new_alpha = 2.0 * 0.7 + 1.67 * 0.3 ≈ 1.9
	if utility.Alpha <= 1.5 || utility.Alpha >= 2.5 {
		t.Errorf("Alpha should be smoothed (between 1.5 and 2.5), got %f", utility.Alpha)
	}

	// After several updates with same loss, alpha should converge
	for i := 0; i < 10; i++ {
		utility.UpdateAdaptiveAlpha(mi)
	}

	// Should converge toward 1.67 (the computed value for 10% loss)
	if diff := utility.Alpha - 1.67; diff < -0.3 || diff > 0.3 {
		t.Errorf("Alpha should converge to ~1.67, got %f", utility.Alpha)
	}
}

func TestAdaptiveAlphaLossHistory(t *testing.T) {
	utility := NewUtilityFunctionAdaptive(InitialAlpha, DefaultK2CCBeta)

	// Create MIs with varying loss rates
	lossRates := []float64{0.05, 0.10, 0.15, 0.20, 0.25}

	for i, lossRate := range lossRates {
		mi := NewMonitorInterval(uint64(i), 10_000_000.0)
		lost := uint64(10000.0 * lossRate)
		mi.BytesSent = 10000
		mi.BytesAcked = 10000 - lost
		mi.BytesLost = lost
		mi.PacketsAcked = MinMIPackets
		mi.RTTSamples = append(mi.RTTSamples, 50*time.Millisecond)
		mi.EndTime = mi.StartTime.Add(time.Second)

		utility.UpdateAdaptiveAlpha(mi)
	}

	// History should be limited to 4 entries
	if utility.LossHistoryLen() > LossHistorySize {
		t.Errorf("Loss history should be limited to %d, got %d", LossHistorySize, utility.LossHistoryLen())
	}
}

func TestAdaptiveConfig(t *testing.T) {
	// Test NewK2CCConfigAdaptive() constructor
	config := NewK2CCConfigAdaptive()

	if !config.AdaptiveAlpha {
		t.Error("AdaptiveAlpha should be true")
	}
	if diff := config.InitialAlpha - InitialAlpha; diff < -0.01 || diff > 0.01 {
		t.Errorf("InitialAlpha should be %f, got %f", InitialAlpha, config.InitialAlpha)
	}
	if diff := config.Alpha - InitialAlpha; diff < -0.01 || diff > 0.01 {
		t.Errorf("Alpha should start at %f, got %f", InitialAlpha, config.Alpha)
	}
}

func TestK2CCSenderAdaptiveMode(t *testing.T) {
	sender := NewK2CCSenderAdaptive(100_000_000) // 100 Mbps

	if !sender.IsAdaptive() {
		t.Error("Sender should be in adaptive mode")
	}
}

// ============================================================================
// Tests for LossRate Clamp + Snapshot MI Boundary Fallback
// ============================================================================

func TestMonitorInterval_LossRate_Clamped(t *testing.T) {
	// Bug: MI boundary can produce BytesLost > BytesSent (stale loss events
	// from previous MI attributed to new MI with few bytes sent).
	// LossRate must never exceed 1.0.
	mi := NewMonitorInterval(1, 10_000_000.0)
	mi.BytesSent = 1000
	mi.BytesLost = 5000 // Impossible in theory, but happens at MI boundary

	loss := mi.LossRate()
	if loss > 1.0 {
		t.Errorf("LossRate should be clamped to 1.0, got %f", loss)
	}
	if loss != 1.0 {
		t.Errorf("LossRate with BytesLost > BytesSent should be exactly 1.0, got %f", loss)
	}

	// Normal case: unchanged behavior
	mi2 := NewMonitorInterval(2, 10_000_000.0)
	mi2.BytesSent = 10000
	mi2.BytesLost = 2000
	if diff := mi2.LossRate() - 0.2; diff < -0.01 || diff > 0.01 {
		t.Errorf("Normal loss rate should be 0.2, got %f", mi2.LossRate())
	}

	// Zero sent: 0.0
	mi3 := NewMonitorInterval(3, 10_000_000.0)
	if mi3.LossRate() != 0.0 {
		t.Errorf("Zero BytesSent should return 0.0, got %f", mi3.LossRate())
	}
}

func TestK2CCSender_Snapshot_UsesLastCompletedOnBoundary(t *testing.T) {
	// When CurrentMI has insufficient data (BytesSent < minSnapshotBytes),
	// Snapshot() should return the last completed MI's loss/throughput.
	sender := NewK2CCSender(100_000_000)
	mockRTT := &mockRTTStatsProvider{smoothedRTT: 50 * time.Millisecond}
	sender.SetRTTStatsProvider(mockRTT)

	// Simulate a completed MI with known stats
	sender.lastCompletedLoss = 0.02
	sender.lastCompletedThroughput = 5_000_000.0

	// Set up CurrentMI with very few bytes (MI boundary state)
	sender.state.ensureCurrentMI(sender.state.CurrentRate, 0)
	sender.state.CurrentMI.BytesSent = 500 // << minSnapshotBytes (100000)
	sender.state.CurrentMI.BytesLost = 300 // Would produce 60% loss — garbage
	sender.state.CurrentMI.BytesAcked = 200

	snap := sender.Snapshot()

	// Should use lastCompleted values, not the garbage from young MI
	if snap.LossRate != 0.02 {
		t.Errorf("Expected lastCompleted LossRate 0.02, got %f", snap.LossRate)
	}
	if snap.Throughput != 5_000_000.0 {
		t.Errorf("Expected lastCompleted Throughput 5000000, got %f", snap.Throughput)
	}

	// Now simulate MI with enough data
	sender.state.CurrentMI.BytesSent = 200000 // > minSnapshotBytes (100000)
	sender.state.CurrentMI.BytesLost = 20000  // 10% real loss
	sender.state.CurrentMI.BytesAcked = 180000
	sender.state.CurrentMI.StartTime = time.Now().Add(-time.Second)
	sender.state.CurrentMI.Complete()

	snap2 := sender.Snapshot()

	// Should use CurrentMI values now
	if snap2.LossRate < 0.09 || snap2.LossRate > 0.11 {
		t.Errorf("Expected CurrentMI LossRate ~0.10, got %f", snap2.LossRate)
	}
	if snap2.Throughput <= 0 {
		t.Errorf("Expected positive Throughput from CurrentMI, got %f", snap2.Throughput)
	}
}

func TestK2CCSender_Snapshot_UpdatesLastCompletedOnMICompletion(t *testing.T) {
	// When MI completes via OnPacketAcked, lastCompleted fields should update.
	sender := NewK2CCSender(100_000_000)
	mockRTT := &mockRTTStatsProvider{smoothedRTT: 50 * time.Millisecond}
	sender.SetRTTStatsProvider(mockRTT)

	// Initially zero
	if sender.lastCompletedLoss != 0 {
		t.Errorf("Initial lastCompletedLoss should be 0, got %f", sender.lastCompletedLoss)
	}

	// Send packets to build up MI
	for i := 0; i < 20; i++ {
		sender.OnPacketSent(0, 0, congestion.PacketNumber(i), 1472, true)
	}
	// Add some loss
	sender.OnCongestionEvent(15, 1472*2, 0)

	// Backdate MI start so MinDuration gate doesn't block completion
	sender.mu.Lock()
	sender.state.CurrentMI.StartTime = time.Now().Add(-200 * time.Millisecond)
	sender.mu.Unlock()

	// Ack enough packets to trigger MI completion (MinMIPackets = 10)
	for i := 0; i < 12; i++ {
		sender.OnPacketAcked(congestion.PacketNumber(i), 1472, 0, 0)
	}

	// After MI completes, lastCompleted should be non-zero
	sender.mu.RLock()
	completedLoss := sender.lastCompletedLoss
	completedThroughput := sender.lastCompletedThroughput
	sender.mu.RUnlock()

	// We can't predict exact values, but they should be set
	if completedLoss == 0 && completedThroughput == 0 {
		t.Error("After MI completion, lastCompletedLoss and lastCompletedThroughput should be updated")
	}
}

func TestOnCongestionEvent_StaleLossFiltering(t *testing.T) {
	// Confirmed root cause: OnCongestionEvent receives loss events for packets
	// sent in the PREVIOUS MI, inflating the new MI's BytesLost (e.g., 947% LossRate).
	// 86 stale events in 3 minutes observed in production diagnostic run.
	sender := NewK2CCSender(100_000_000)
	mockRTT := &mockRTTStatsProvider{smoothedRTT: 50 * time.Millisecond}
	sender.SetRTTStatsProvider(mockRTT)

	// Send packets 0-19 in MI #0
	for i := 0; i < 20; i++ {
		sender.OnPacketSent(0, 0, congestion.PacketNumber(i), 1472, true)
	}

	// Backdate MI start so MinDuration gate doesn't block completion
	sender.mu.Lock()
	sender.state.CurrentMI.StartTime = time.Now().Add(-200 * time.Millisecond)
	sender.mu.Unlock()

	// Ack enough to complete MI #0 (MinMIPackets=10) → starts MI #1
	for i := 0; i < 12; i++ {
		sender.OnPacketAcked(congestion.PacketNumber(i), 1472, 0, 0)
	}

	// Send packets 20-29 in MI #1
	for i := 20; i < 30; i++ {
		sender.OnPacketSent(0, 0, congestion.PacketNumber(i), 1472, true)
	}

	// Get MI #1's BytesLost before stale loss
	sender.mu.RLock()
	lostBefore := sender.state.CurrentMI.BytesLost
	miFirstPkt := sender.state.CurrentMI.FirstPacketNumber
	sender.mu.RUnlock()

	if miFirstPkt <= 0 {
		t.Fatalf("MI #1 should have FirstPacketNumber > 0, got %d", miFirstPkt)
	}

	// Loss event for packet 15 (belongs to MI #0, not MI #1!)
	sender.OnCongestionEvent(congestion.PacketNumber(15), 1472, 0)

	// MI #1's BytesLost should NOT increase from stale loss
	sender.mu.RLock()
	lostAfter := sender.state.CurrentMI.BytesLost
	sender.mu.RUnlock()

	if lostAfter != lostBefore {
		t.Errorf("Stale loss (pkt 15 < firstPkt %d) should not increase MI #1 BytesLost: before=%d, after=%d",
			miFirstPkt, lostBefore, lostAfter)
	}

	// Loss event for packet 25 (belongs to MI #1) SHOULD be counted
	sender.OnCongestionEvent(congestion.PacketNumber(25), 1472, 0)

	sender.mu.RLock()
	lostAfterValid := sender.state.CurrentMI.BytesLost
	sender.mu.RUnlock()

	if lostAfterValid == lostBefore {
		t.Error("Loss from current MI's packet (pkt 25) should be counted")
	}
}

func TestGFWParams_Alpha5(t *testing.T) {
	sender := NewCCSenderGFWMode(100_000_000)
	if sender.config.Alpha != 5.0 {
		t.Errorf("GFW alpha should be 5.0, got %f", sender.config.Alpha)
	}
}

func TestParams_Beta1(t *testing.T) {
	sender := NewK2CCSender(100_000_000)
	if sender.config.Beta != 1.0 {
		t.Errorf("beta should be 1.0, got %f", sender.config.Beta)
	}
}

func TestK2CCSenderAdaptiveIntegration(t *testing.T) {
	sender := NewK2CCSenderAdaptive(100_000_000) // 100 Mbps
	mockRTT := &mockRTTStatsProvider{smoothedRTT: 100 * time.Millisecond}
	sender.SetRTTStatsProvider(mockRTT)

	// Simulate sending packets
	for i := 0; i < 20; i++ {
		sender.OnPacketSent(0, 0, congestion.PacketNumber(i), 1472, true)
	}

	// Simulate 15 acks (25% loss implicit)
	for i := 0; i < 15; i++ {
		sender.OnPacketAcked(congestion.PacketNumber(i), 1472, 0, 0)
	}

	// Simulate 5 lost packets
	sender.OnCongestionEvent(15, 1472*5, 0)

	// After processing, verify sender is still functional
	cwnd := sender.GetCongestionWindow()
	if cwnd <= 0 {
		t.Error("Congestion window should be positive")
	}
}

// ============================================================================
// Tests for AIMD-ε (Adaptive Epsilon)
// ============================================================================

func TestMonitorInterval_RTTCV(t *testing.T) {
	mi := NewMonitorInterval(1, 10_000_000.0)

	// No samples → 0
	if mi.RTTCV() != 0 {
		t.Error("RTTCV should be 0 with no samples")
	}

	// Single sample → 0 (need ≥2 for variance)
	mi.RTTSamples = append(mi.RTTSamples, 50*time.Millisecond)
	if mi.RTTCV() != 0 {
		t.Error("RTTCV should be 0 with single sample")
	}

	// Identical samples → CV = 0
	mi.RTTSamples = append(mi.RTTSamples, 50*time.Millisecond)
	if mi.RTTCV() != 0 {
		t.Errorf("RTTCV should be 0 for identical samples, got %f", mi.RTTCV())
	}

	// Varied samples: 40ms, 50ms, 60ms → mean=50ms, stddev≈8.16ms, CV≈0.163
	mi.RTTSamples = []time.Duration{40 * time.Millisecond, 50 * time.Millisecond, 60 * time.Millisecond}
	cv := mi.RTTCV()
	if cv < 0.1 || cv > 0.25 {
		t.Errorf("RTTCV for [40,50,60]ms should be ~0.163, got %f", cv)
	}
}

func TestAIMDEpsilon_AdditiveIncrease(t *testing.T) {
	// When direction is confirmed (counter ≥ 2), ε should increase by epsilonDelta.
	// Setup: put sender in Decision mode with counter=1 and a low LastUtility,
	// then call processCompletedMI with an MI that produces higher utility → counter=2 → AI.
	sender := NewK2CCSender(100_000_000)
	sender.state.Mode = K2CCModeDecision
	sender.state.Epsilon = DefaultK2CCEpsilon
	sender.state.NextMIID = 1 // Avoid rate probing (0%10==0)
	sender.state.UtilityIncreases = 1
	sender.state.UtilityDecreases = 0

	// Set LastUtility very low so any computed utility produces positive gradient
	lowUtility := -100.0
	sender.state.hasLastUtility = true; sender.state.lastUtility =lowUtility

	// MI with enough data to not be app-limited (BytesSent ≈ rate * duration / 8)
	mi := NewMonitorInterval(1, sender.state.CurrentRate)
	mi.BytesSent = 5_000_000 // 5 MB
	mi.BytesAcked = 5_000_000
	mi.PacketsAcked = MinMIPackets
	mi.RTTSamples = append(mi.RTTSamples, 50*time.Millisecond)
	mi.StartTime = time.Now().Add(-time.Second)
	mi.Complete()

	sender.processCompletedMI(mi)

	// UtilityIncreases should now be 2 → additive increase triggered
	expected := DefaultK2CCEpsilon + epsilonDelta
	if sender.state.Epsilon < expected-0.001 || sender.state.Epsilon > expected+0.001 {
		t.Errorf("ε should be %f after additive increase, got %f", expected, sender.state.Epsilon)
	}
}

func TestAIMDEpsilon_Bounds(t *testing.T) {
	sender := NewK2CCSender(100_000_000)
	sender.state.Mode = K2CCModeDecision
	sender.state.NextMIID = 1 // Avoid rate probing

	mi := NewMonitorInterval(1, sender.state.CurrentRate)
	mi.BytesSent = 5_000_000
	mi.BytesAcked = 5_000_000
	mi.PacketsAcked = MinMIPackets
	mi.RTTSamples = append(mi.RTTSamples, 50*time.Millisecond)
	mi.StartTime = time.Now().Add(-time.Second)
	mi.Complete()

	// Test upper bound: ε above max + confirmed direction → clamp
	sender.state.Epsilon = epsilonMax + 0.1
	sender.state.UtilityIncreases = 1 // will reach 2 (confirmed) → AI
	sender.state.UtilityDecreases = 0
	lowU := -100.0
	sender.state.hasLastUtility = true; sender.state.lastUtility =lowU
	sender.processCompletedMI(mi)

	if sender.state.Epsilon > epsilonMax {
		t.Errorf("ε should be clamped to max %f, got %f", epsilonMax, sender.state.Epsilon)
	}

	// Test lower bound: ε below min, set up reversal with weak signal → MD → clamp
	sender.state.Epsilon = epsilonMin * 0.5 // 0.01, below min
	sender.state.UtilityIncreases = 3       // confirmed
	sender.state.UtilityDecreases = 0
	highU := 100.0
	sender.state.hasLastUtility = true; sender.state.lastUtility =highU
	// Process MI with utility < highU → gradient negative → reversal
	// Cold-start noise is high → weak signal → MD → 0.01 * 0.5 = 0.005 → clamp to min
	sender.processCompletedMI(mi)
	if sender.state.Epsilon < epsilonMin {
		t.Errorf("ε should be clamped to min %f, got %f", epsilonMin, sender.state.Epsilon)
	}
}

func TestAIMDEpsilon_ColdStart(t *testing.T) {
	sender := NewK2CCSender(100_000_000)

	// noiseEWMA should start at coldStartNoise (conservative)
	if sender.noiseEWMA != coldStartNoise {
		t.Errorf("Initial noiseEWMA should be %f, got %f", coldStartNoise, sender.noiseEWMA)
	}
}

func TestAIMDEpsilon_NoiseUpdate(t *testing.T) {
	sender := NewK2CCSender(100_000_000)
	sender.state.Mode = K2CCModeDecision
	sender.state.NextMIID = 1 // Avoid rate probing
	initialNoise := sender.noiseEWMA

	// Set low LastUtility so gradient is positive → counter goes 0→1 (building)
	lowU := -100.0
	sender.state.hasLastUtility = true; sender.state.lastUtility =lowU
	sender.state.UtilityIncreases = 0
	sender.state.UtilityDecreases = 0

	mi := NewMonitorInterval(1, sender.state.CurrentRate)
	mi.BytesSent = 5_000_000
	mi.BytesAcked = 5_000_000
	mi.PacketsAcked = MinMIPackets
	// Add varied RTT samples to produce non-zero CV
	mi.RTTSamples = []time.Duration{
		40 * time.Millisecond, 50 * time.Millisecond,
		60 * time.Millisecond, 45 * time.Millisecond,
	}
	mi.StartTime = time.Now().Add(-time.Second)
	mi.Complete()

	sender.processCompletedMI(mi)

	// Counter goes 0→1 (building, nowConfirmed=false) → noise update runs
	if sender.noiseEWMA == initialNoise {
		t.Error("noiseEWMA should update during building phase")
	}
	// Should move DOWN from coldStartNoise toward real RTT CV (~0.16)
	if sender.noiseEWMA >= initialNoise {
		t.Errorf("noiseEWMA should decrease from cold start %f, got %f",
			initialNoise, sender.noiseEWMA)
	}
}

func TestAIMDEpsilon_HoldDuringBuilding(t *testing.T) {
	sender := NewK2CCSender(100_000_000)
	sender.state.Mode = K2CCModeDecision
	sender.state.NextMIID = 1 // Avoid rate probing
	sender.state.Epsilon = 0.08 // non-default value

	// Set up: counter = 0, first gradient (not a reversal since prev wasn't confirmed)
	lowU := -100.0
	sender.state.hasLastUtility = true; sender.state.lastUtility =lowU
	sender.state.UtilityIncreases = 0
	sender.state.UtilityDecreases = 0

	epsBefore := sender.state.Epsilon

	mi := NewMonitorInterval(1, sender.state.CurrentRate)
	mi.BytesSent = 5_000_000
	mi.BytesAcked = 5_000_000
	mi.PacketsAcked = MinMIPackets
	mi.RTTSamples = append(mi.RTTSamples, 50*time.Millisecond)
	mi.StartTime = time.Now().Add(-time.Second)
	mi.Complete()

	sender.processCompletedMI(mi)

	// Counter goes from 0 → 1 (building, not reversal)
	// ε should be unchanged (hold)
	if sender.state.Epsilon != epsBefore {
		t.Errorf("ε should hold during building phase, was %f now %f",
			epsBefore, sender.state.Epsilon)
	}
}

func TestAIMDEpsilon_Snapshot(t *testing.T) {
	sender := NewK2CCSender(100_000_000)
	mockRTT := &mockRTTStatsProvider{smoothedRTT: 50 * time.Millisecond}
	sender.SetRTTStatsProvider(mockRTT)

	snap := sender.Snapshot()
	if snap.Epsilon != sender.state.Epsilon {
		t.Errorf("Snapshot Epsilon should match state, got %f vs %f",
			snap.Epsilon, sender.state.Epsilon)
	}
}

// ============================================================================
// Tests for MI minimum duration (loss visibility fix)
// ============================================================================

func TestMI_NotCompleteBeforeMinDuration(t *testing.T) {
	// At high rates, MI completes after just 10 ACKs (a few ms).
	// QUIC loss detection takes ~1 RTT. If MI completes before that,
	// all loss events land in the next MI and get filtered as "stale".
	//
	// Fix: MI must last at least 1 smoothed RTT before completing.

	mi := NewMonitorInterval(1, 400_000_000.0) // 400 Mbps
	mi.MinDuration = 100 * time.Millisecond     // simulate 100ms RTT

	// Feed 20 ACKs (well above MinMIPackets=10) — but instantly
	mi.PacketsAcked = 20
	mi.RTTSamples = make([]time.Duration, 20)

	// MI should NOT be complete because MinDuration hasn't elapsed
	if mi.IsComplete() {
		t.Error("MI should not complete before MinDuration, even with enough ACKs")
	}
}

func TestMI_CompletesAfterMinDuration(t *testing.T) {
	// After MinDuration has passed and ACK threshold met, MI should complete.
	mi := NewMonitorInterval(1, 400_000_000.0)
	mi.MinDuration = 50 * time.Millisecond

	mi.PacketsAcked = 15
	mi.RTTSamples = make([]time.Duration, 15)
	// Backdate start so duration > MinDuration
	mi.StartTime = time.Now().Add(-100 * time.Millisecond)

	if !mi.IsComplete() {
		t.Error("MI should complete when both ACK threshold and MinDuration are met")
	}
}

func TestMI_TimeoutOverridesMinDuration(t *testing.T) {
	// The 60s timeout should still force-complete even if MinDuration
	// somehow isn't met (safety valve).
	mi := NewMonitorInterval(1, 400_000_000.0)
	mi.MinDuration = 200 * time.Second // absurdly high

	mi.StartTime = time.Now().Add(-61 * time.Second)

	if !mi.IsComplete() {
		t.Error("60s timeout should override MinDuration")
	}
}

func TestSender_SetsMinDurationFromRTT(t *testing.T) {
	// When K2CCSender creates a new MI via startNewMI, it should set
	// MinDuration = current smoothed RTT.
	sender := NewK2CCSender(500_000_000) // 500 Mbps
	mockRTT := &mockRTTStatsProvider{smoothedRTT: 120 * time.Millisecond}
	sender.SetRTTStatsProvider(mockRTT)

	// MI #0: created by ensureCurrentMI in OnPacketSent with initial rtt (100ms).
	// Send and ACK enough packets to complete MI #0 and trigger startNewMI.
	// Backdate MI start so MinDuration gate doesn't block completion.
	for i := 0; i < 20; i++ {
		sender.OnPacketSent(0, 0, congestion.PacketNumber(i), 1472, true)
	}
	sender.mu.Lock()
	sender.state.CurrentMI.StartTime = time.Now().Add(-200 * time.Millisecond)
	sender.mu.Unlock()

	for i := 0; i < 15; i++ {
		sender.OnPacketAcked(congestion.PacketNumber(i), 1472, 0, 0)
	}

	// MI #1: created by startNewMI with p.rtt = 120ms (from mockRTT).
	sender.mu.RLock()
	mi := sender.state.CurrentMI
	minDur := mi.MinDuration
	miID := mi.ID
	sender.mu.RUnlock()

	if miID == 0 {
		t.Fatal("expected MI #1 to be current (MI #0 should have completed)")
	}
	if minDur != 120*time.Millisecond {
		t.Errorf("new MI MinDuration should be smoothed RTT (120ms), got %v", minDur)
	}
}

func TestSlowStart_ExitsAtMaxRate(t *testing.T) {
	maxRate := int64(100_000_000) // 100 Mbps
	sender := NewCCSenderGFWMode(maxRate)
	rttStats := &mockRTTStatsProvider{smoothedRTT: 50 * time.Millisecond}
	sender.SetRTTStatsProvider(rttStats)

	if !sender.InSlowStart() {
		t.Fatal("should start in slow start")
	}

	// Directly drive processCompletedMI with well-formed MIs to avoid
	// app-limited detection and MinDuration issues in test harness.
	// Initial rate is 30% of maxRate = 30 Mbps.
	for i := 0; i < 20; i++ {
		rate := sender.state.CurrentRate
		mi := NewMonitorInterval(uint64(i), rate)
		mi.BytesSent = 5_000_000 // 5 MB — well above app-limited threshold
		mi.BytesAcked = 5_000_000
		mi.PacketsAcked = MinMIPackets
		mi.RTTSamples = append(mi.RTTSamples, 50*time.Millisecond)
		mi.StartTime = time.Now().Add(-200 * time.Millisecond)
		mi.Complete()

		sender.mu.Lock()
		sender.processCompletedMI(mi)
		sender.state.NextMIID++
		sender.mu.Unlock()

		if !sender.InSlowStart() {
			return // exited — test passes
		}
	}

	t.Error("slow start should exit when rate reaches maxRate")
}

func TestHighRate_LossVisibleWithMinDuration(t *testing.T) {
	// Integration test: at high rate with RTT-based MinDuration,
	// loss events should be recorded in the MI (not filtered as stale).
	sender := NewK2CCSender(500_000_000) // 500 Mbps
	mockRTT := &mockRTTStatsProvider{smoothedRTT: 100 * time.Millisecond}
	sender.SetRTTStatsProvider(mockRTT)

	// Send 100 packets (simulating high-rate burst)
	for i := 0; i < 100; i++ {
		sender.OnPacketSent(0, 0, congestion.PacketNumber(i), 1472, true)
	}

	// ACK 15 packets — would complete MI under old logic (≥10 ACKs),
	// but with MinDuration=100ms the MI should stay open.
	for i := 0; i < 15; i++ {
		sender.OnPacketAcked(congestion.PacketNumber(i), 1472, 0, 0)
	}

	// Loss event for packet 50 — should land in CURRENT MI, not be stale
	sender.OnCongestionEvent(congestion.PacketNumber(50), 1472, 0)

	sender.mu.RLock()
	lost := sender.state.CurrentMI.BytesLost
	miID := sender.state.CurrentMI.ID
	sender.mu.RUnlock()

	if miID != 0 {
		t.Errorf("MI should still be #0 (not rotated), got MI #%d", miID)
	}
	if lost == 0 {
		t.Error("loss for packet 50 should be recorded in current MI, not filtered as stale")
	}
}

// ============================================================================
// Tests for ackRateTracker (sliding-window loss rate)
// ============================================================================

func TestAckRateTracker_NewTracker(t *testing.T) {
	tr := newAckRateTracker()
	if tr.AckRate() != 1.0 {
		t.Errorf("new tracker should return ackRate=1.0, got %f", tr.AckRate())
	}
}

func TestAckRateTracker_SingleSlot(t *testing.T) {
	tr := newAckRateTracker()
	now := time.Duration(1000) * time.Second
	tr.record(now, 90, 10)

	rate := tr.AckRate()
	if rate < 0.89 || rate > 0.91 {
		t.Errorf("expected ackRate ~0.90, got %f", rate)
	}
}

func TestAckRateTracker_SlidingWindow(t *testing.T) {
	tr := newAckRateTracker()

	for sec := int64(100); sec < 105; sec++ {
		tr.record(time.Duration(sec)*time.Second, 100, 0)
	}
	if tr.AckRate() != 1.0 {
		t.Errorf("expected ackRate=1.0 with no loss, got %f", tr.AckRate())
	}

	// Slide: second 105 has heavy loss
	tr.record(time.Duration(105)*time.Second, 50, 50)

	// Window: seconds 101-105. Acks: 4*100+50=450, Losses: 50, Total=500
	rate := tr.AckRate()
	expected := 450.0 / 500.0 // 0.90
	if rate < expected-0.01 || rate > expected+0.01 {
		t.Errorf("expected ackRate ~%.2f, got %f", expected, rate)
	}
}

func TestAckRateTracker_SlotOverwrite(t *testing.T) {
	tr := newAckRateTracker()
	now := time.Duration(200) * time.Second
	tr.record(now, 10, 2)
	tr.record(now, 20, 3)

	rate := tr.AckRate()
	expected := 30.0 / 35.0 // 0.857
	if rate < expected-0.01 || rate > expected+0.01 {
		t.Errorf("expected ackRate ~%.3f, got %f", expected, rate)
	}
}

func TestAckRateTracker_EmptySlots(t *testing.T) {
	tr := newAckRateTracker()
	tr.record(time.Duration(400)*time.Second, 100, 0)
	tr.record(time.Duration(410)*time.Second, 50, 50)

	// Only second 410 in window (400 expired). 50/100 = 0.50.
	rate := tr.AckRate()
	if rate != 0.5 {
		t.Errorf("expected ackRate 0.50, got %f", rate)
	}
}
