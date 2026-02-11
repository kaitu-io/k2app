// PCC Vivace tests
package pcc

import (
	"testing"
	"time"

	"github.com/apernet/quic-go/congestion"
)

// ============================================================================
// Tests for PCCSender - PCC Vivace Congestion Control
// ============================================================================

func TestPCCSender_Interface(t *testing.T) {
	// PCCSender must implement CongestionControl interface
	var _ congestion.CongestionControl = (*PCCSender)(nil)
}

func TestNewPCCSender(t *testing.T) {
	maxRate := int64(100_000_000) // 100 Mbps
	sender := NewPCCSender(maxRate)

	if sender == nil {
		t.Fatal("sender should not be nil")
	}

	if sender.maxRate != maxRate {
		t.Errorf("expected maxRate=%d, got %d", maxRate, sender.maxRate)
	}

	// Verify default config
	if sender.config.Alpha != DefaultPCCAlpha {
		t.Errorf("expected default alpha=%f, got %f", DefaultPCCAlpha, sender.config.Alpha)
	}
}

func TestNewPCCSenderGFWMode(t *testing.T) {
	maxRate := int64(100_000_000)
	sender := NewPCCSenderGFWMode(maxRate)

	if sender == nil {
		t.Fatal("sender should not be nil")
	}

	// GFW mode should have low alpha
	if sender.config.Alpha > GFWPCCAlpha*2 {
		t.Errorf("expected GFW alpha around %f, got %f", GFWPCCAlpha, sender.config.Alpha)
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
	expectedMin := 900_000.0  // Allow some tolerance
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
	uf := NewUtilityFunction(DefaultPCCAlpha, DefaultPCCBeta)

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
	uf := NewUtilityFunction(DefaultPCCAlpha, DefaultPCCBeta)

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
	standard := NewUtilityFunction(DefaultPCCAlpha, DefaultPCCBeta)
	gfw := NewUtilityFunction(GFWPCCAlpha, DefaultPCCBeta)

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
// Tests for PCCSender Congestion Window
// ============================================================================

func TestPCCSender_GetCongestionWindow(t *testing.T) {
	sender := NewPCCSender(100_000_000) // 100 Mbps max

	// Set up mock RTT provider
	mockRTT := &mockRTTStatsProvider{smoothedRTT: 100 * time.Millisecond}
	sender.SetRTTStatsProvider(mockRTT)

	cwnd := sender.GetCongestionWindow()

	// Window should be positive and reasonable
	if cwnd == 0 {
		t.Error("congestion window should not be 0")
	}

	// Should be at least minimum window
	if cwnd < pccMinWindow {
		t.Errorf("cwnd %d should be >= minWindow %d", cwnd, pccMinWindow)
	}
}

func TestPCCSender_CanSend(t *testing.T) {
	sender := NewPCCSender(100_000_000)
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

func TestPCCSender_InSlowStart(t *testing.T) {
	sender := NewPCCSender(100_000_000)

	// Should start in slow start mode
	if !sender.InSlowStart() {
		t.Error("should start in slow start mode")
	}
}

// ============================================================================
// Tests for Rate Adjustment (Gradient Ascent)
// ============================================================================

func TestPCCSender_RateToCwnd(t *testing.T) {
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

func TestPCCSender_OnPacketAcked_UpdatesState(t *testing.T) {
	sender := NewPCCSender(100_000_000)
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

func TestPCCSender_OnCongestionEvent_RecordsLoss(t *testing.T) {
	sender := NewPCCSender(100_000_000)
	mockRTT := &mockRTTStatsProvider{smoothedRTT: 100 * time.Millisecond}
	sender.SetRTTStatsProvider(mockRTT)

	// Ensure there's a current MI
	sender.state.ensureCurrentMI(sender.state.CurrentRate)

	// Simulate loss
	sender.OnCongestionEvent(1, 1472, 10000)

	if sender.state.CurrentMI.BytesLost == 0 {
		t.Error("bytes lost should be recorded")
	}
}

// ============================================================================
// Tests for PCC Config
// ============================================================================

func TestPCCConfig_Default(t *testing.T) {
	config := DefaultPCCConfig()

	if config.Alpha != DefaultPCCAlpha {
		t.Errorf("expected alpha=%f, got %f", DefaultPCCAlpha, config.Alpha)
	}
	if config.Beta != DefaultPCCBeta {
		t.Errorf("expected beta=%f, got %f", DefaultPCCBeta, config.Beta)
	}
	if config.AggressiveMode {
		t.Error("default should not be aggressive mode")
	}
}

func TestPCCConfig_GFWMode(t *testing.T) {
	config := GFWPCCConfig()

	if config.Alpha != GFWPCCAlpha {
		t.Errorf("expected alpha=%f, got %f", GFWPCCAlpha, config.Alpha)
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
	utility := NewUtilityFunctionAdaptive(InitialAlpha, DefaultPCCBeta)

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
	utility := NewUtilityFunctionAdaptive(InitialAlpha, DefaultPCCBeta)

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
	utility := NewUtilityFunctionAdaptive(InitialAlpha, DefaultPCCBeta)

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
	utility := NewUtilityFunctionAdaptive(InitialAlpha, DefaultPCCBeta)

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
	// Test NewPCCConfigAdaptive() constructor
	config := NewPCCConfigAdaptive()

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

func TestPCCSenderAdaptiveMode(t *testing.T) {
	sender := NewPCCSenderAdaptive(100_000_000) // 100 Mbps

	if !sender.IsAdaptive() {
		t.Error("Sender should be in adaptive mode")
	}
}

func TestPCCSenderAdaptiveIntegration(t *testing.T) {
	sender := NewPCCSenderAdaptive(100_000_000) // 100 Mbps
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
