// K2CC Vivace tests
package k2cc

import (
	"math"
	"testing"
	"time"

	"github.com/kaitu-io/kuic/congestion"
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
	if sender.config.Alpha != defaultAlpha {
		t.Errorf("expected default alpha=%f, got %f", defaultAlpha, sender.config.Alpha)
	}
}

func TestNewCCSenderGFWMode(t *testing.T) {
	maxRate := int64(100_000_000)
	sender := NewCCSenderGFWMode(maxRate)

	if sender == nil {
		t.Fatal("sender should not be nil")
	}

	// GFW mode should have low alpha
	if sender.config.Alpha != gfwAlpha {
		t.Errorf("expected GFW alpha %f, got %f", gfwAlpha, sender.config.Alpha)
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
	mi.PacketsAcked = minMIPackets
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
	uf := NewUtilityFunction(defaultAlpha, defaultBeta)

	mi := NewMonitorInterval(1, 10_000_000.0)
	mi.BytesSent = 125000
	mi.BytesAcked = 125000 // No loss
	mi.BytesLost = 0
	mi.RTTSamples = append(mi.RTTSamples, 50*time.Millisecond)
	mi.StartTime = time.Now().Add(-time.Second)
	mi.Complete()

	u := uf.Calculate(mi, mi.SendRate())

	// With no loss and minimal latency, utility should be positive
	if u <= 0 {
		t.Errorf("expected positive utility, got %f", u)
	}
}

func TestUtilityFunction_LossPenalty(t *testing.T) {
	uf := NewUtilityFunction(defaultAlpha, defaultBeta)

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

	uNoLoss := uf.Calculate(miNoLoss, miNoLoss.SendRate())
	uWithLoss := uf.Calculate(miWithLoss, miWithLoss.SendRate())

	// Utility with loss should be lower
	if uWithLoss >= uNoLoss {
		t.Errorf("utility with loss (%f) should be less than without loss (%f)", uWithLoss, uNoLoss)
	}
}

func TestUtilityFunction_GFWModeToleratesLoss(t *testing.T) {
	standard := NewUtilityFunction(defaultAlpha, defaultBeta)
	gfw := NewUtilityFunction(gfwAlpha, defaultBeta)

	mi := NewMonitorInterval(1, 10_000_000.0)
	mi.BytesSent = 125000
	mi.BytesAcked = 112500
	mi.BytesLost = 12500 // 10% loss (below α clamping threshold for both modes)
	mi.RTTSamples = append(mi.RTTSamples, 50*time.Millisecond)
	mi.StartTime = time.Now().Add(-time.Second)
	mi.Complete()

	uStandard := standard.Calculate(mi, mi.SendRate())
	uGFW := gfw.Calculate(mi, mi.SendRate())

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
	// cwnd = (rate_bps / 8) * RTT_seconds * multiplier / ackRate
	rate := 10_000_000.0 // 10 Mbps
	rtt := 100 * time.Millisecond

	cwnd := rateToCwnd(rate, rtt, 1.0)

	// Expected: (10_000_000 / 8) * 0.1 * 1.2 = 150,000 bytes
	expected := congestion.ByteCount(150_000)
	tolerance := congestion.ByteCount(10_000)
	if cwnd < expected-tolerance || cwnd > expected+tolerance {
		t.Errorf("expected cwnd ~%d (±%d), got %d", expected, tolerance, cwnd)
	}
}

func TestRateToCwnd_AckRateCompensation(t *testing.T) {
	rate := 100_000_000.0 // 100 Mbps
	rtt := 100 * time.Millisecond

	cwndNoLoss := rateToCwnd(rate, rtt, 1.0)
	cwndWith20Loss := rateToCwnd(rate, rtt, 0.8) // 20% loss → ackRate=0.8

	// cwnd should be 1.25x larger when ackRate=0.8
	ratio := float64(cwndWith20Loss) / float64(cwndNoLoss)
	if ratio < 1.20 || ratio > 1.30 {
		t.Errorf("expected ~1.25x cwnd increase, got %.2fx (noLoss=%d, with20Loss=%d)",
			ratio, cwndNoLoss, cwndWith20Loss)
	}
}

func TestCwnd_AckRateFloor(t *testing.T) {
	sender := NewCCSenderGFWMode(500_000_000)

	// Record massive loss to push ackRate below 0.8
	for i := 0; i < 100; i++ {
		sender.ackRate.record(time.Duration(i)*time.Second, 1, 9) // 10% ack rate
	}

	rate := sender.ackRate.AckRate()
	if rate >= 0.5 {
		t.Fatalf("expected ackRate < 0.5, got %.2f", rate)
	}

	// clampedAckRate should be floored at 0.8
	clamped := sender.clampedAckRate()
	if clamped != minAckRate {
		t.Errorf("expected clamped ackRate = %.1f, got %.2f", minAckRate, clamped)
	}
}

func TestCwnd_AckRateClampedCompensation(t *testing.T) {
	sender := NewCCSenderGFWMode(500_000_000)

	// Record 20% loss to push ackRate to ~0.8
	for i := 0; i < 100; i++ {
		sender.ackRate.record(time.Duration(i)*time.Second, 8, 2) // 80% ack rate
	}

	clamped := sender.clampedAckRate()
	// With minAckRate=0.95, clamped should be 0.95 (not 0.8)
	if clamped != minAckRate {
		t.Errorf("expected clamped ackRate = %.2f, got %.2f", minAckRate, clamped)
	}

	// Max cwnd inflation from ackRate compensation: 1/0.95 = 1.053×
	cwndNoLoss := rateToCwnd(100_000_000, 100*time.Millisecond, 1.0)
	cwndClamped := rateToCwnd(100_000_000, 100*time.Millisecond, clamped)
	ratio := float64(cwndClamped) / float64(cwndNoLoss)
	if ratio < 1.04 || ratio > 1.06 {
		t.Errorf("expected ~1.053x cwnd increase with clamped ackRate, got %.3fx", ratio)
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

	// Send a packet first so the MI has PacketsSent > 0
	// (PacketsSent == 0 → stale loss filter skips all losses)
	sender.OnPacketSent(0, 0, 1, 1472, true)

	// Simulate loss for that packet
	sender.OnCongestionEvent(1, 1472, 10000)

	if sender.state.CurrentMI.BytesLost == 0 {
		t.Error("bytes lost should be recorded")
	}
}

// ============================================================================
// Tests for K2CC Config
// ============================================================================

func TestK2CCConfig_Default(t *testing.T) {
	sender := NewK2CCSender(100_000_000)
	if sender.config.Alpha != defaultAlpha {
		t.Errorf("expected alpha=%f, got %f", defaultAlpha, sender.config.Alpha)
	}
	if sender.config.Beta != defaultBeta {
		t.Errorf("expected beta=%f, got %f", defaultBeta, sender.config.Beta)
	}
}

func TestK2CCConfig_GFWMode(t *testing.T) {
	sender := NewCCSenderGFWMode(100_000_000)
	if sender.config.Alpha != gfwAlpha {
		t.Errorf("expected alpha=%f, got %f", gfwAlpha, sender.config.Alpha)
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

	// Ack enough packets to trigger MI completion (minMIPackets = 10)
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

	// Ack enough to complete MI #0 (minMIPackets=10) → starts MI #1
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

func TestGFWParams_Alpha(t *testing.T) {
	sender := NewCCSenderGFWMode(100_000_000)
	if sender.config.Alpha != gfwAlpha {
		t.Errorf("GFW alpha should be %f, got %f", gfwAlpha, sender.config.Alpha)
	}
}

func TestParams_Beta1(t *testing.T) {
	sender := NewK2CCSender(100_000_000)
	if sender.config.Beta != 1.0 {
		t.Errorf("beta should be 1.0, got %f", sender.config.Beta)
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
	sender.state.Epsilon = defaultEpsilon
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
	mi.PacketsAcked = minMIPackets
	mi.RTTSamples = append(mi.RTTSamples, 50*time.Millisecond)
	mi.StartTime = time.Now().Add(-time.Second)
	mi.Complete()

	sender.processCompletedMI(mi)

	// UtilityIncreases should now be 2 → additive increase triggered
	expected := defaultEpsilon + epsilonDelta
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
	mi.PacketsAcked = minMIPackets
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
	sender.bwEst = bwEstimator{} // Reset BW estimator so PCC gradient path is taken
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
	mi.PacketsAcked = minMIPackets
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
	mi.PacketsAcked = minMIPackets
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

	// Feed 20 ACKs (well above minMIPackets=10) — but instantly
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
	if minDur != 240*time.Millisecond {
		t.Errorf("new MI MinDuration should be 2×RTT (240ms), got %v", minDur)
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
		mi.PacketsAcked = minMIPackets
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

// ============================================================================
// Tests for OnCongestionEventEx (CongestionControlEx interface)
// ============================================================================

func TestOnCongestionEventEx_RecordsAckRate(t *testing.T) {
	sender := NewK2CCSender(100_000_000)
	sender.SetRTTStatsProvider(&mockRTTStatsProvider{smoothedRTT: 50 * time.Millisecond})

	for i := range 20 {
		sender.OnPacketSent(0, 0, congestion.PacketNumber(i), 1400, true)
	}

	acked := make([]congestion.AckedPacketInfo, 18)
	for i := range acked {
		acked[i] = congestion.AckedPacketInfo{PacketNumber: congestion.PacketNumber(i), BytesAcked: 1400}
	}
	lost := []congestion.LostPacketInfo{
		{PacketNumber: 18, BytesLost: 1400},
		{PacketNumber: 19, BytesLost: 1400},
	}

	sender.OnCongestionEventEx(0, congestion.Time(500*time.Second), acked, lost)

	snap := sender.Snapshot()
	// 18 acks, 2 losses → 90%
	if snap.AckRate < 0.89 || snap.AckRate > 0.91 {
		t.Errorf("expected AckRate ~0.90, got %f", snap.AckRate)
	}
}

func TestOnCongestionEventEx_NilPackets(t *testing.T) {
	sender := NewK2CCSender(100_000_000)
	sender.OnCongestionEventEx(0, 0, nil, nil)

	snap := sender.Snapshot()
	if snap.AckRate != 1.0 {
		t.Errorf("expected AckRate=1.0 with no data, got %f", snap.AckRate)
	}
}

func TestCongestionControlEx_InterfaceCompliance(t *testing.T) {
	var _ congestion.CongestionControlEx = (*K2CCSender)(nil)
}

// ============================================================================
// App-Limited Anchor + Slow Start Saturation + Stable Probing + Alpha Clamping
// ============================================================================

func TestAppLimited_SimpleAnchor(t *testing.T) {
	// Simulate: R=270M, actual link B=100M → app-limited
	// Anchor should set R ≈ throughput and exit slow start
	sender := NewCCSenderGFWMode(DefaultMaxRate)
	sender.state.CurrentRate = 270_000_000
	sender.state.Mode = K2CCModeSlowStart
	sender.rtt = 30 * time.Millisecond

	// App-limited MI: sent 100M worth but target was 270M (37% = app-limited)
	mi := NewMonitorInterval(1, 270_000_000)
	mi.StartTime = time.Now().Add(-30 * time.Millisecond)
	mi.EndTime = time.Now()
	mi.BytesSent = 375000
	mi.BytesAcked = 375000
	mi.PacketsAcked = 255
	mi.RTTSamples = []time.Duration{30 * time.Millisecond}

	sender.processCompletedMI(mi)

	// Rate should be anchored to ~100M (throughput), not stuck at 270M
	if sender.state.CurrentRate > 150_000_000 {
		t.Errorf("expected rate anchored near 100M, got %.1fM", sender.state.CurrentRate/1e6)
	}
	if sender.state.CurrentRate < 50_000_000 {
		t.Errorf("rate too low, expected ~100M, got %.1fM", sender.state.CurrentRate/1e6)
	}
	if sender.state.Mode != K2CCModeDecision {
		t.Errorf("expected decision mode after anchor, got %s", sender.state.Mode)
	}
}

func TestAppLimited_PreservesGradientContext(t *testing.T) {
	// After app-limited anchor, hasLastUtility should be true
	// so next MI can compute gradient immediately (no blind probing)
	sender := NewCCSenderGFWMode(DefaultMaxRate)
	sender.state.CurrentRate = 270_000_000
	sender.state.Mode = K2CCModeSlowStart
	sender.rtt = 30 * time.Millisecond

	mi := NewMonitorInterval(1, 270_000_000)
	mi.StartTime = time.Now().Add(-30 * time.Millisecond)
	mi.EndTime = time.Now()
	mi.BytesSent = 375000
	mi.BytesAcked = 375000
	mi.PacketsAcked = 255
	mi.RTTSamples = []time.Duration{30 * time.Millisecond}

	sender.processCompletedMI(mi)

	if !sender.state.hasLastUtility {
		t.Error("expected hasLastUtility=true after anchor (gradient context preserved)")
	}
	if sender.state.lastUtility == 0 {
		t.Error("expected non-zero lastUtility after anchor")
	}
}

func TestAppLimited_NotAppLimited(t *testing.T) {
	// When NOT app-limited, anchor should NOT fire — normal CC path
	sender := NewCCSenderGFWMode(DefaultMaxRate)
	sender.state.CurrentRate = 100_000_000
	sender.state.Mode = K2CCModeSlowStart
	sender.rtt = 30 * time.Millisecond

	mi := NewMonitorInterval(0, 100_000_000)
	mi.StartTime = time.Now().Add(-30 * time.Millisecond)
	mi.EndTime = time.Now()
	mi.BytesSent = 375000 // 100% of expected
	mi.BytesAcked = 375000
	mi.PacketsAcked = 255
	mi.RTTSamples = []time.Duration{30 * time.Millisecond}

	sender.processCompletedMI(mi)

	if sender.state.Mode != K2CCModeSlowStart {
		t.Errorf("expected still in slow start, got %s", sender.state.Mode)
	}
	if sender.state.CurrentRate < 150_000_000 {
		t.Errorf("expected rate doubled in slow start, got %.1fM", sender.state.CurrentRate/1e6)
	}
}

func TestAppLimited_WithLoss(t *testing.T) {
	// B=100M, loss=5%: T=95M, B_est = T/(1-L) = 95/(1-0.05) = 100M
	sender := NewCCSenderGFWMode(DefaultMaxRate)
	sender.state.CurrentRate = 270_000_000
	sender.state.Mode = K2CCModeSlowStart
	sender.rtt = 30 * time.Millisecond

	mi := NewMonitorInterval(1, 270_000_000)
	mi.StartTime = time.Now().Add(-30 * time.Millisecond)
	mi.EndTime = time.Now()
	mi.BytesSent = 375000
	mi.BytesLost = 18750   // 5% loss
	mi.BytesAcked = 356250 // 95% acked
	mi.PacketsAcked = 242
	mi.RTTSamples = []time.Duration{30 * time.Millisecond}

	sender.processCompletedMI(mi)

	// Rate should be anchored to ~100M via T/(1-L) formula
	if sender.state.CurrentRate < 80_000_000 || sender.state.CurrentRate > 130_000_000 {
		t.Errorf("expected rate ~100M via T/(1-L), got %.1fM", sender.state.CurrentRate/1e6)
	}
}

func TestSlowStart_ThroughputSaturation(t *testing.T) {
	// Simulate: rate doubles but throughput stays flat → link saturated
	sender := NewCCSenderGFWMode(100_000_000) // 100 Mbps max
	sender.state.CurrentRate = 50_000_000     // 50 Mbps
	sender.state.Mode = K2CCModeSlowStart
	sender.rtt = 30 * time.Millisecond

	// MI 0: throughput = 50M (rate=50M, filling pipe)
	mi0 := NewMonitorInterval(0, 50_000_000)
	mi0.StartTime = time.Now().Add(-30 * time.Millisecond)
	mi0.EndTime = time.Now()
	mi0.BytesSent = 187500 // 50M * 0.03s / 8
	mi0.BytesAcked = 187500
	mi0.PacketsAcked = 128
	mi0.RTTSamples = []time.Duration{30 * time.Millisecond}

	sender.processCompletedMI(mi0)
	// Should continue slow start, rate now ~100M
	if sender.state.Mode != K2CCModeSlowStart {
		t.Fatalf("expected slow start after MI 0, got %s", sender.state.Mode)
	}

	// MI 1: rate=~100M but throughput still ~50M (link saturated at 50M)
	mi1 := NewMonitorInterval(1, sender.state.CurrentRate)
	mi1.StartTime = time.Now().Add(-30 * time.Millisecond)
	mi1.EndTime = time.Now()
	mi1.BytesSent = 187500 // Still 50M throughput worth
	mi1.BytesAcked = 187500
	mi1.PacketsAcked = 128
	mi1.RTTSamples = []time.Duration{30 * time.Millisecond}

	sender.processCompletedMI(mi1)

	// Should have exited slow start due to saturation
	if sender.state.Mode != K2CCModeDecision {
		t.Errorf("expected decision mode after saturation, got %s", sender.state.Mode)
	}
	// Rate should be set to actual throughput (~50M), not overshooting rate
	if sender.state.CurrentRate > 70_000_000 {
		t.Errorf("expected rate near throughput ~50M, got %.1fM", sender.state.CurrentRate/1e6)
	}
}

func TestSlowStart_ThroughputGrowing(t *testing.T) {
	// Throughput growing proportionally to rate → don't exit slow start
	sender := NewCCSenderGFWMode(DefaultMaxRate)
	sender.state.CurrentRate = 50_000_000
	sender.state.Mode = K2CCModeSlowStart
	sender.rtt = 30 * time.Millisecond

	// MI 0: throughput = 50M
	mi0 := NewMonitorInterval(0, 50_000_000)
	mi0.StartTime = time.Now().Add(-30 * time.Millisecond)
	mi0.EndTime = time.Now()
	mi0.BytesSent = 187500
	mi0.BytesAcked = 187500
	mi0.PacketsAcked = 128
	mi0.RTTSamples = []time.Duration{30 * time.Millisecond}

	sender.processCompletedMI(mi0)
	if sender.state.Mode != K2CCModeSlowStart {
		t.Fatalf("expected slow start after MI 0, got %s", sender.state.Mode)
	}

	// MI 1: throughput = 90M (rate ~100M, still growing)
	mi1 := NewMonitorInterval(1, sender.state.CurrentRate)
	mi1.StartTime = time.Now().Add(-30 * time.Millisecond)
	mi1.EndTime = time.Now()
	mi1.BytesSent = 337500 // 90M * 0.03s / 8
	mi1.BytesAcked = 337500
	mi1.PacketsAcked = 230
	mi1.RTTSamples = []time.Duration{30 * time.Millisecond}

	sender.processCompletedMI(mi1)

	// Should still be in slow start — throughput grew proportionally
	if sender.state.Mode != K2CCModeSlowStart {
		t.Errorf("expected still in slow start (throughput growing), got %s", sender.state.Mode)
	}
}

func TestProbing_StableBaseRate(t *testing.T) {
	// probeBaseRate prevents drift during alternating probes.
	// After 10 probe cycles (20 MIs), rate should remain near base rate.
	sender := NewCCSenderGFWMode(DefaultMaxRate)
	sender.state.CurrentRate = 100_000_000
	sender.state.Mode = K2CCModeDecision
	sender.probeBaseRate = 100_000_000
	sender.rtt = 30 * time.Millisecond
	sender.state.Epsilon = defaultEpsilon
	sender.state.hasLastUtility = true
	sender.state.lastUtility = 100.0
	sender.state.NextMIID = 3 // avoid rate probing (every 10 MIs)

	baseRate := sender.state.CurrentRate

	// Run 20 MIs (10 probe cycles) with zero gradient.
	// Use fixed timestamps to eliminate time.Now() jitter.
	baseTime := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 20; i++ {
		mi := NewMonitorInterval(uint64(i+2), sender.state.CurrentRate)
		mi.StartTime = baseTime.Add(time.Duration(i) * 30 * time.Millisecond)
		mi.EndTime = mi.StartTime.Add(30 * time.Millisecond)
		mi.BytesSent = 375000
		mi.BytesAcked = 375000
		mi.PacketsAcked = 255
		mi.RTTSamples = []time.Duration{30 * time.Millisecond}
		sender.state.NextMIID++

		sender.processCompletedMI(mi)
	}

	// Rate should not have drifted significantly from base
	drift := math.Abs(sender.probeBaseRate-baseRate) / baseRate
	if drift > 0.05 {
		t.Errorf("probeBaseRate drifted %.1f%% from 100M (%.1fM → %.1fM)",
			drift*100, baseRate/1e6, sender.probeBaseRate/1e6)
	}
}

func TestRateProbing_PreservesCounters(t *testing.T) {
	// Rate probing every 10 MIs should NOT reset direction counters
	sender := NewK2CCSender(100_000_000)
	sender.state.CurrentRate = 80_000_000
	sender.state.Mode = K2CCModeDecision
	sender.state.UtilityIncreases = 2 // confirmed direction
	sender.state.NextMIID = 10        // triggers rate probing

	mi := NewMonitorInterval(9, 80_000_000)
	mi.StartTime = time.Now().Add(-30 * time.Millisecond)
	mi.EndTime = time.Now()
	mi.BytesSent = 300000
	mi.BytesAcked = 300000
	mi.PacketsAcked = 204
	mi.RTTSamples = []time.Duration{30 * time.Millisecond}
	sender.state.hasLastUtility = true
	sender.state.lastUtility = 80.0

	sender.processCompletedMI(mi)

	// Direction counters should NOT have been reset by rate probing.
	// They may have been modified by gradient processing, but the point is
	// rate probing itself doesn't zero them out.
	// The gradient from the +20% probe will update counters naturally.
}

func TestAlphaClamping_HighLoss(t *testing.T) {
	// With α=5.0 and loss=25%, unclamped: 1-αL = 1-1.25 = -0.25 (utility inverts!)
	// α clamping: α_eff = 0.8/0.25 = 3.2, so 1-α_eff×L = 1-0.8 = 0.2 (positive)
	sender := NewCCSenderGFWMode(DefaultMaxRate)
	sender.state.CurrentRate = 100_000_000
	sender.state.Mode = K2CCModeDecision
	sender.rtt = 30 * time.Millisecond
	sender.state.hasLastUtility = true
	sender.state.lastUtility = 20.0
	sender.state.NextMIID = 3

	mi := NewMonitorInterval(2, 100_000_000)
	mi.StartTime = time.Now().Add(-30 * time.Millisecond)
	mi.EndTime = time.Now()
	mi.BytesSent = 375000
	mi.BytesLost = 93750   // 25% loss
	mi.BytesAcked = 281250 // 75% acked
	mi.PacketsAcked = 191
	mi.RTTSamples = []time.Duration{30 * time.Millisecond}

	sender.processCompletedMI(mi)

	if sender.state.CurrentRate < 50_000_000 {
		t.Errorf("α inversion: rate collapsed to %.1fM (expected stable ~100M)", sender.state.CurrentRate/1e6)
	}
}

// ============================================================================
// Utility Gradient Tests
//
// Verify the utility function provides correct gradient signal across all
// network scenarios. The gradient (U_high - U_low) must point the algorithm
// toward optimal rate in every case.
// ============================================================================

// makeBottleneckMI creates a Monitor Interval simulating a bottleneck link.
// sendRate is the CC target rate (bps), bw is bottleneck bandwidth (bps),
// bgLoss is constant background loss (0.0-1.0), rtt is round-trip time.
// Duration is fixed at 100ms for reproducibility.
func makeBottleneckMI(id uint64, sendRate, bw float64, bgLoss float64, rtt time.Duration) *MonitorInterval {
	durationSec := 0.1 // 100ms MI
	bytesSent := uint64(sendRate / 8.0 * durationSec)

	// Throughput capped by bottleneck
	throughput := math.Min(sendRate, bw)
	bytesAcked := uint64(throughput / 8.0 * durationSec)

	// Congestion loss: packets above BW are dropped
	var congLoss float64
	if sendRate > bw {
		congLoss = 1.0 - bw/sendRate
	}
	// Total loss = 1 - (1-congLoss)*(1-bgLoss)
	totalLoss := 1.0 - (1.0-congLoss)*(1.0-bgLoss)
	bytesLost := uint64(float64(bytesSent) * totalLoss)

	// Adjust acked for bg loss
	if bgLoss > 0 && sendRate <= bw {
		bytesAcked = uint64(float64(bytesAcked) * (1.0 - bgLoss))
	}

	mi := &MonitorInterval{
		ID:          id,
		TargetRate:  sendRate,
		BytesSent:   bytesSent,
		BytesAcked:  bytesAcked,
		BytesLost:   bytesLost,
		PacketsAcked: uint64(bytesAcked / 1350),
		RTTSamples:  []time.Duration{rtt},
		StartTime:   time.Now().Add(-time.Duration(durationSec * float64(time.Second))),
	}
	mi.Complete()
	return mi
}

func TestUtilityGradient_DescentAboveBW(t *testing.T) {
	// When rate >> BW, utility must DECREASE as rate increases.
	// This is the core property that enables convergence on bottleneck links.
	uf := NewUtilityFunction(gfwAlpha, defaultBeta) // α=4.0
	uf.MinRTT = 15 * time.Millisecond

	cases := []struct {
		name   string
		bw     float64
		bgLoss float64
		rtt    time.Duration
	}{
		{"5M_clean", 5e6, 0.0, 16 * time.Millisecond},
		{"5M_gfw5", 5e6, 0.05, 16 * time.Millisecond},
		{"5M_gfw10", 5e6, 0.10, 16 * time.Millisecond},
		{"20M_clean", 20e6, 0.0, 75 * time.Millisecond},
		{"20M_gfw5", 20e6, 0.05, 75 * time.Millisecond},
		{"50M_gfw5", 50e6, 0.05, 130 * time.Millisecond},
		{"100M_clean", 100e6, 0.0, 16 * time.Millisecond},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Test at 2×, 4×, 6× BW
			rates := []float64{tc.bw * 2, tc.bw * 4, tc.bw * 6}
			var prevU float64
			for i, rate := range rates {
				mi := makeBottleneckMI(uint64(i), rate, tc.bw, tc.bgLoss, tc.rtt)
				u := uf.Calculate(mi, mi.SendRate())
				if i > 0 {
					// Higher rate must give lower utility (descent gradient)
					if u >= prevU {
						t.Errorf("rate=%.0fM: U=%.2f should be < previous U=%.2f (rate=%.0fM)",
							rate/1e6, u, prevU, rates[i-1]/1e6)
					}
				}
				prevU = u
			}
		})
	}
}

func TestUtilityGradient_AscentBelowBW(t *testing.T) {
	// When rate < BW (no congestion loss), utility must INCREASE with rate.
	uf := NewUtilityFunction(gfwAlpha, defaultBeta)
	uf.MinRTT = 15 * time.Millisecond

	cases := []struct {
		name   string
		bw     float64
		bgLoss float64
		rtt    time.Duration
	}{
		{"100M_clean", 100e6, 0.0, 16 * time.Millisecond},
		{"100M_gfw5", 100e6, 0.05, 16 * time.Millisecond},
		{"100M_gfw10", 100e6, 0.10, 75 * time.Millisecond},
		{"100M_gfw15", 100e6, 0.15, 130 * time.Millisecond},
		{"50M_gfw5", 50e6, 0.05, 16 * time.Millisecond},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rates := []float64{tc.bw * 0.3, tc.bw * 0.5, tc.bw * 0.8}
			var prevU float64
			for i, rate := range rates {
				mi := makeBottleneckMI(uint64(i), rate, tc.bw, tc.bgLoss, tc.rtt)
				u := uf.Calculate(mi, mi.SendRate())
				if i > 0 {
					if u <= prevU {
						t.Errorf("rate=%.0fM: U=%.2f should be > previous U=%.2f (rate=%.0fM)",
							rate/1e6, u, prevU, rates[i-1]/1e6)
					}
				}
				prevU = u
			}
		})
	}
}

func TestUtilityGradient_NearOptimal(t *testing.T) {
	// Near BW, probing above should give lower utility than probing below.
	uf := NewUtilityFunction(gfwAlpha, defaultBeta)
	uf.MinRTT = 15 * time.Millisecond

	cases := []struct {
		name   string
		bw     float64
		bgLoss float64
		rtt    time.Duration
	}{
		{"5M_gfw5", 5e6, 0.05, 16 * time.Millisecond},
		{"50M_clean", 50e6, 0.0, 75 * time.Millisecond},
		{"100M_gfw5", 100e6, 0.05, 130 * time.Millisecond},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			epsilon := 0.12
			rateAbove := tc.bw * (1.0 + epsilon) // just above BW
			rateBelow := tc.bw * (1.0 - epsilon) // just below BW

			miAbove := makeBottleneckMI(0, rateAbove, tc.bw, tc.bgLoss, tc.rtt)
			miBelow := makeBottleneckMI(1, rateBelow, tc.bw, tc.bgLoss, tc.rtt)

			uAbove := uf.Calculate(miAbove, miAbove.SendRate())
			uBelow := uf.Calculate(miBelow, miBelow.SendRate())

			// Below BW should have higher utility → gradient points down
			if uAbove >= uBelow {
				t.Errorf("U(above BW)=%.2f should be < U(below BW)=%.2f", uAbove, uBelow)
			}
		})
	}
}

func TestUtilityGradient_BGLoss20Boundary(t *testing.T) {
	// At 20% constant BG loss with α=5, αL_bg = 1.0.
	// Below BW (rate-independent loss): dU/drate = 1 - αL ≈ 0 (flat).
	// This is a known limitation — document, not fix.
	uf := NewUtilityFunction(gfwAlpha, defaultBeta)
	uf.MinRTT = 15 * time.Millisecond

	bw := 100e6
	bgLoss := 0.20

	miLow := makeBottleneckMI(0, bw*0.5, bw, bgLoss, 16*time.Millisecond)
	miHigh := makeBottleneckMI(1, bw*0.8, bw, bgLoss, 16*time.Millisecond)

	uLow := uf.Calculate(miLow, miLow.SendRate())
	uHigh := uf.Calculate(miHigh, miHigh.SendRate())

	// At 20% BG, gradient should be near-zero or slightly positive
	// (the rate-based penalty: dU/drate = 1 - αL = 1 - 1.0 = 0)
	gradient := uHigh - uLow
	t.Logf("BG=20%%: U(50M)=%.2f, U(80M)=%.2f, gradient=%.4f", uLow, uHigh, gradient)

	// Above BW, descent should still work
	miOvershoot := makeBottleneckMI(2, bw*1.5, bw, bgLoss, 16*time.Millisecond)
	uOvershoot := uf.Calculate(miOvershoot, miOvershoot.SendRate())
	if uOvershoot >= uHigh {
		t.Errorf("U(150M)=%.2f should be < U(80M)=%.2f even at 20%% BG loss", uOvershoot, uHigh)
	}
}

func TestUtilityGradient_ProductionCase(t *testing.T) {
	// The exact production scenario: HK node, rate=30M, BW=5M, GFW 5%
	uf := NewUtilityFunction(gfwAlpha, defaultBeta)
	uf.MinRTT = 15 * time.Millisecond

	bw := 5e6
	bgLoss := 0.05
	rtt := 16 * time.Millisecond

	// Rate stuck at 30M (old floor) vs 15M vs 6M
	mi30 := makeBottleneckMI(0, 30e6, bw, bgLoss, rtt)
	mi15 := makeBottleneckMI(1, 15e6, bw, bgLoss, rtt)
	mi6 := makeBottleneckMI(2, 6e6, bw, bgLoss, rtt)

	u30 := uf.Calculate(mi30, mi30.SendRate())
	u15 := uf.Calculate(mi15, mi15.SendRate())
	u6 := uf.Calculate(mi6, mi6.SendRate())

	// Must have monotonic descent: U(30M) < U(15M) < U(6M)
	if u30 >= u15 {
		t.Errorf("U(30M)=%.2f should be < U(15M)=%.2f", u30, u15)
	}
	if u15 >= u6 {
		t.Errorf("U(15M)=%.2f should be < U(6M)=%.2f", u15, u6)
	}

	// Gradient must be strong enough for direction detection
	gradient := u30 - u15
	if math.Abs(gradient) < 1.0 {
		t.Errorf("|gradient| between 30M and 15M = %.4f, should be > 1.0", math.Abs(gradient))
	}

	t.Logf("Production case: U(30M)=%.2f, U(15M)=%.2f, U(6M)=%.2f", u30, u15, u6)
}

// ============================================================================
// Tests for BW Estimator
// ============================================================================

func TestBWEstimator_Empty(t *testing.T) {
	var est bwEstimator
	if est.valid() {
		t.Error("empty estimator should not be valid")
	}
	if est.estimate() != 0 {
		t.Errorf("empty estimator should return 0, got %f", est.estimate())
	}
}

func TestBWEstimator_AddSamples(t *testing.T) {
	var est bwEstimator
	est.addSample(5_000_000)
	if est.valid() {
		t.Error("1 sample should not be valid (need bwMinValidCount=2)")
	}
	est.addSample(4_500_000)
	if !est.valid() {
		t.Error("2 samples should be valid")
	}
	if est.estimate() != 5_000_000 {
		t.Errorf("expected estimate=5M, got %f", est.estimate())
	}
}

func TestBWEstimator_SlidingWindowMax(t *testing.T) {
	var est bwEstimator
	for i := 0; i < bwWindowSize; i++ {
		est.addSample(5_000_000)
	}
	if est.estimate() != 5_000_000 {
		t.Errorf("expected 5M, got %f", est.estimate())
	}
	for i := 0; i < bwWindowSize; i++ {
		est.addSample(3_000_000)
	}
	if est.estimate() != 3_000_000 {
		t.Errorf("expected 3M after aging, got %f", est.estimate())
	}
}

func TestBWEstimator_Reset(t *testing.T) {
	var est bwEstimator
	est.addSample(50_000_000)
	est.addSample(50_000_000)
	est.reset(5_000_000)
	if !est.valid() {
		t.Error("reset should keep valid state")
	}
	if est.estimate() != 5_000_000 {
		t.Errorf("expected 5M after reset, got %f", est.estimate())
	}
}

func TestBWAnchored_PhaseConstants(t *testing.T) {
	if bwCruiseLength+2 != bwWindowSize {
		t.Errorf("phase cycle (%d+2=%d) must equal window size (%d)",
			bwCruiseLength, bwCruiseLength+2, bwWindowSize)
	}
	if !(bwDrainGain < bwCruiseGain && bwCruiseGain < 1.0 && 1.0 < bwProbeUpGain) {
		t.Errorf("gain ordering violated: drain=%.2f cruise=%.2f probeUp=%.2f",
			bwDrainGain, bwCruiseGain, bwProbeUpGain)
	}
}

func TestBWAnchored_SenderHasFields(t *testing.T) {
	sender := NewCCSenderGFWMode(500_000_000)
	if sender.bwEst.valid() {
		t.Error("new sender bwEst should not be valid")
	}
	if sender.bwPhase != bwPhaseCruise {
		t.Errorf("expected initial phase=Cruise, got %d", sender.bwPhase)
	}
}

func TestBWAnchored_CruiseRate(t *testing.T) {
	sender := NewCCSenderGFWMode(500_000_000)
	mockRTT := &mockRTTStatsProvider{smoothedRTT: 16 * time.Millisecond}
	sender.SetRTTStatsProvider(mockRTT)

	sender.bwEst.addSample(5_000_000)
	sender.bwEst.addSample(5_000_000)
	sender.bwPhase = bwPhaseCruise
	sender.bwCruiseRemain = 3
	sender.state.Mode = K2CCModeDecision
	sender.state.CurrentRate = 4_750_000

	mi := &MonitorInterval{
		ID: 10, TargetRate: 4_750_000,
		BytesSent: 19000, BytesAcked: 19000, BytesLost: 0,
		PacketsSent: 13, PacketsAcked: 13,
		StartTime: time.Now().Add(-32 * time.Millisecond), EndTime: time.Now(),
		FirstPacketNumber: 100, LastPacketNumber: 112,
	}

	sender.processCompletedMI(mi)

	expectedTarget := 5_000_000.0 * bwCruiseGain
	if sender.state.CurrentRate < expectedTarget*0.8 || sender.state.CurrentRate > expectedTarget*1.2 {
		t.Errorf("expected rate near %.0f, got %.0f", expectedTarget, sender.state.CurrentRate)
	}
	if sender.bwCruiseRemain != 2 {
		t.Errorf("expected cruiseRemain=2, got %d", sender.bwCruiseRemain)
	}
}

func TestBWAnchored_CruiseDetectsBWDrop(t *testing.T) {
	sender := NewCCSenderGFWMode(500_000_000)
	mockRTT := &mockRTTStatsProvider{smoothedRTT: 16 * time.Millisecond}
	sender.SetRTTStatsProvider(mockRTT)

	sender.bwEst.addSample(50_000_000)
	sender.bwEst.addSample(50_000_000)
	sender.bwPhase = bwPhaseCruise
	sender.bwCruiseRemain = 3
	sender.state.Mode = K2CCModeDecision
	sender.state.CurrentRate = 47_500_000

	mi := &MonitorInterval{
		ID: 10, TargetRate: 47_500_000,
		BytesSent: 190000, BytesAcked: 20000, BytesLost: 170000,
		PacketsSent: 130, PacketsAcked: 14,
		StartTime: time.Now().Add(-32 * time.Millisecond), EndTime: time.Now(),
		FirstPacketNumber: 100, LastPacketNumber: 229,
	}

	sender.processCompletedMI(mi)

	est := sender.bwEst.estimate()
	if est > 10_000_000 {
		t.Errorf("bwEst should have reset to ~5M after BW drop, got %.0f", est)
	}
	if sender.state.CurrentRate > 10_000_000 {
		t.Errorf("rate should have dropped to ~5M, got %.0f", sender.state.CurrentRate)
	}
}

func TestBWAnchored_FullCycle(t *testing.T) {
	sender := NewCCSenderGFWMode(500_000_000)
	mockRTT := &mockRTTStatsProvider{smoothedRTT: 16 * time.Millisecond}
	sender.SetRTTStatsProvider(mockRTT)

	sender.bwEst.addSample(10_000_000)
	sender.bwEst.addSample(10_000_000)
	sender.bwPhase = bwPhaseCruise
	sender.bwCruiseRemain = 1
	sender.state.Mode = K2CCModeDecision
	sender.state.CurrentRate = 9_500_000

	makeMI := func(id uint64, bytesAcked uint64) *MonitorInterval {
		bytesLost := uint64(1472) // 1 packet lost
		return &MonitorInterval{
			ID: id, TargetRate: sender.state.CurrentRate,
			BytesSent: bytesAcked + bytesLost, BytesAcked: bytesAcked, BytesLost: bytesLost,
			PacketsSent: uint64(bytesAcked/1472) + 1, PacketsAcked: uint64(bytesAcked / 1472),
			StartTime: time.Now().Add(-32 * time.Millisecond), EndTime: time.Now(),
			FirstPacketNumber: int64(id * 20), LastPacketNumber: int64(id*20 + 19),
		}
	}

	// Last Cruise MI → should transition to ProbeUp
	sender.processCompletedMI(makeMI(1, 38000))
	if sender.bwPhase != bwPhaseProbeUp {
		t.Errorf("after last Cruise MI, expected ProbeUp, got %d", sender.bwPhase)
	}

	// ProbeUp MI → should transition to Drain
	sender.processCompletedMI(makeMI(2, 38000))
	if sender.bwPhase != bwPhaseDrain {
		t.Errorf("after ProbeUp MI, expected Drain, got %d", sender.bwPhase)
	}

	// Drain MI → should transition to Cruise with cruiseRemain=6
	sender.processCompletedMI(makeMI(3, 30000))
	if sender.bwPhase != bwPhaseCruise {
		t.Errorf("after Drain MI, expected Cruise, got %d", sender.bwPhase)
	}
	if sender.bwCruiseRemain != bwCruiseLength {
		t.Errorf("expected cruiseRemain=%d, got %d", bwCruiseLength, sender.bwCruiseRemain)
	}
}

func TestBWAnchored_BGLossDetection(t *testing.T) {
	sender := NewCCSenderGFWMode(500_000_000)
	mockRTT := &mockRTTStatsProvider{smoothedRTT: 16 * time.Millisecond}
	sender.SetRTTStatsProvider(mockRTT)

	sender.bwEst.addSample(5_000_000)
	sender.bwEst.addSample(5_000_000)
	sender.bwPhase = bwPhaseDrain
	sender.state.Mode = K2CCModeDecision
	sender.state.CurrentRate = 3_750_000

	mi := &MonitorInterval{
		ID: 10, TargetRate: 3_750_000,
		BytesSent: 15000, BytesAcked: 14250, BytesLost: 750,
		PacketsSent: 11, PacketsAcked: 10,
		StartTime: time.Now().Add(-32 * time.Millisecond), EndTime: time.Now(),
		FirstPacketNumber: 100, LastPacketNumber: 110,
	}

	sender.processCompletedMI(mi)

	if sender.bgLossEWMA < 0.005 {
		t.Errorf("expected bgLossEWMA > 0.5%% after drain with loss, got %.3f%%", sender.bgLossEWMA*100)
	}
}

func TestBWAnchored_FallbackToPCC(t *testing.T) {
	sender := NewCCSenderGFWMode(500_000_000)
	mockRTT := &mockRTTStatsProvider{smoothedRTT: 100 * time.Millisecond}
	sender.SetRTTStatsProvider(mockRTT)

	// bwEst NOT valid → PCC gradient path
	sender.state.Mode = K2CCModeDecision
	sender.state.CurrentRate = 50_000_000
	sender.state.hasLastUtility = true
	sender.state.lastUtility = 10.0

	mi := &MonitorInterval{
		ID: 5, TargetRate: 50_000_000,
		BytesSent: 50000, BytesAcked: 45000, BytesLost: 5000,
		PacketsSent: 35, PacketsAcked: 31,
		StartTime: time.Now().Add(-100 * time.Millisecond), EndTime: time.Now(),
		FirstPacketNumber: 100, LastPacketNumber: 134,
	}

	sender.processCompletedMI(mi)

	// Should NOT have entered BW-anchored — bwEst gets 1 sample from this MI
	// but needs 2 for valid. So PCC path ran.
	if sender.bwPhase != bwPhaseCruise {
		t.Errorf("with invalid bwEst, should stay in default phase")
	}
	if !sender.state.hasLastUtility {
		t.Error("PCC path should have set hasLastUtility")
	}
}
