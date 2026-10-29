package k2cc

import (
	"sync"
	"testing"
	"time"

	"github.com/kaitu-io/kuic/congestion"
)

// Snapshot() is called at 1 Hz by the health sampler. Before the fix,
// mi.Throughput() inside it set IsAppLimited=true on the LIVE MI whenever the
// in-progress MI had sent less than 50% of target so far — i.e. always, early
// in an MI — and the flag is sticky, so the observer flipped the controller
// into the app-limited anchor path.
func TestSnapshot_DoesNotMarkLiveMIAppLimited(t *testing.T) {
	sender := NewCCSenderGFWMode(DefaultMaxRate)
	mi := NewMonitorInterval(7, 150_000_000)
	mi.StartTime = time.Now().Add(-time.Second)
	mi.BytesSent = 150_000 // >= minSnapshotBytes, far below 50% of 150M×1s/8
	mi.BytesAcked = 150_000
	sender.state.CurrentMI = mi

	for range 3 {
		_ = sender.Snapshot()
	}
	if mi.IsAppLimited {
		t.Fatal("Snapshot() mutated the live MI's IsAppLimited — the observer must be pure")
	}
}

func TestMonitorInterval_AppLimitedIsExplicit(t *testing.T) {
	mi := NewMonitorInterval(1, 100_000_000)
	mi.StartTime = time.Now().Add(-100 * time.Millisecond)
	mi.EndTime = time.Now()
	mi.BytesSent = 100_000 // expected 1.25 MB → 8% → app-limited
	if !mi.appLimited() {
		t.Fatal("8% of expected bytes must be app-limited")
	}
	mi.BytesSent = 1_000_000 // 80%
	if mi.appLimited() {
		t.Fatal("80% of expected bytes must not be app-limited")
	}
	_ = mi.Throughput()
	if mi.IsAppLimited {
		t.Fatal("Throughput() must not write IsAppLimited")
	}
}

// Run with -race. Before the fix OnCongestionEventEx wrote the ackRate tracker
// outside p.mu while Snapshot()/Summary() read it under RLock.
func TestObservers_RaceFreeAgainstControlPath(t *testing.T) {
	sender := NewCCSenderGFWMode(DefaultMaxRate)
	sender.SetRTTStatsProvider(&mockRTTStatsProvider{smoothedRTT: 20 * time.Millisecond})

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			_ = sender.Snapshot()
			_ = sender.Summary()
		}
	}()

	start := time.Now()
	var pn congestion.PacketNumber
	for time.Since(start) < 50*time.Millisecond {
		now := congestion.Time(time.Since(start))
		acked := make([]congestion.AckedPacketInfo, 0, 20)
		for range 20 {
			sender.OnPacketSent(now, 0, pn, 1400, true)
			acked = append(acked, congestion.AckedPacketInfo{PacketNumber: pn, BytesAcked: 1400})
			pn++
		}
		for _, a := range acked {
			sender.OnPacketAcked(a.PacketNumber, a.BytesAcked, 0, now)
		}
		sender.OnCongestionEventEx(0, now, acked, nil)
	}
	close(stop)
	wg.Wait()
}

// ============================================================================
// Summary() accumulator paths — each mirrors an existing control-path test
// so the counters are checked against behaviour the suite already pins.
// ============================================================================

func TestSummary_SenderIDUniqueAndNonZero(t *testing.T) {
	a, b := NewCCSenderGFWMode(DefaultMaxRate), NewCCSenderGFWMode(DefaultMaxRate)
	sa, sb := a.Summary(), b.Summary()
	if sa.SenderID == 0 || sb.SenderID == 0 || sa.SenderID == sb.SenderID {
		t.Fatalf("SenderID must be unique and non-zero: %d %d", sa.SenderID, sb.SenderID)
	}
	if sa.MIs != 0 || sa.BWEst != 0 {
		t.Fatalf("fresh sender must have zero counters: %+v", sa)
	}
}

// Mirrors TestAppLimited_SimpleAnchor: R=270M, throughput 100M → anchor.
func TestSummary_AppLimitedMI(t *testing.T) {
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

	s := sender.Summary()
	if s.MIs != 1 || s.AppLimitedMIs != 1 || s.SlowStartMIs != 1 || s.DecisionMIs != 0 {
		t.Fatalf("counters: %+v", s)
	}
	if s.BytesSent != 375000 || s.BytesAcked != 375000 || s.BytesLost != 0 {
		t.Fatalf("bytes: %+v", s)
	}
	if e, of := s.LossPercentile(0.5); e != 0 || of {
		t.Fatalf("lossP50=(%v,%v) want (0,false)", e, of)
	}
	if e, _ := s.RTTPercentile(0.5); e != 35 {
		t.Fatalf("rttP50=%v want 35 (30ms → first edge >= 30)", e)
	}
	if e, _ := s.RatePercentile(0.5); e != 300 {
		t.Fatalf("rateP50=%v want 300 (270 Mbps)", e)
	}
	if e, _ := s.TputPercentile(0.5); e != 100 {
		t.Fatalf("tputP50=%v want 100 (≈100 Mbps)", e)
	}
	if s.TputMax < 90e6 || s.TputMax > 101e6 {
		t.Fatalf("TputMax=%v want ≈100M", s.TputMax)
	}
	if s.Mode != K2CCModeDecision {
		t.Fatalf("Mode=%v want decision (current value)", s.Mode)
	}
	if s.FloorMIs != 0 {
		t.Fatalf("anchored to ~100M is not a floor: %+v", s)
	}
}

// Mirrors TestSlowStart_ThroughputSaturation.
func TestSummary_SlowStartSaturation(t *testing.T) {
	sender := NewCCSenderGFWMode(100_000_000)
	sender.state.CurrentRate = 50_000_000
	sender.state.Mode = K2CCModeSlowStart
	sender.rtt = 30 * time.Millisecond

	mk := func(id uint64, rate float64) *MonitorInterval {
		mi := NewMonitorInterval(id, rate)
		mi.StartTime = time.Now().Add(-30 * time.Millisecond)
		mi.EndTime = time.Now()
		mi.BytesSent = 187500 // 50M worth over 30 ms
		mi.BytesAcked = 187500
		mi.PacketsAcked = 128
		mi.RTTSamples = []time.Duration{30 * time.Millisecond}
		return mi
	}
	sender.processCompletedMI(mk(0, 50_000_000))
	sender.processCompletedMI(mk(1, sender.state.CurrentRate))

	s := sender.Summary()
	if s.SlowStartSaturations != 1 {
		t.Fatalf("SlowStartSaturations=%d want 1", s.SlowStartSaturations)
	}
	if s.MIs != 2 || s.SlowStartMIs != 2 || s.DecisionMIs != 0 {
		t.Fatalf("mode residency sampled before processing: %+v", s)
	}
	if s.SlowStartMIs+s.DecisionMIs+s.RateAdjMIs != s.MIs {
		t.Fatalf("mode sum != MIs: %+v", s)
	}
}

// Mirrors TestBWAnchored_CruiseDetectsBWDrop.
func TestSummary_BWDropAndLossHistogram(t *testing.T) {
	sender := NewCCSenderGFWMode(500_000_000)
	sender.SetRTTStatsProvider(&mockRTTStatsProvider{smoothedRTT: 16 * time.Millisecond})
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

	s := sender.Summary()
	if s.BWDrops != 1 || s.BWAnchoredMIs != 1 || s.DecisionMIs != 1 {
		t.Fatalf("counters: %+v", s)
	}
	if s.LossMax < 0.89 || s.LossMax > 0.90 {
		t.Fatalf("LossMax=%v want 170000/190000", s.LossMax)
	}
	if e, _ := s.LossPercentile(0.5); e != 100 {
		t.Fatalf("lossP50=%v want 100 (89%% lands in the 100 bucket)", e)
	}
	var rttN uint64
	for _, c := range s.RTTHist {
		rttN += c
	}
	if rttN != 0 {
		t.Fatal("MI without RTT samples must not enter RTTHist")
	}
	if s.BWEst <= 0 || s.BWEst > 10_000_000 {
		t.Fatalf("BWEst=%v want reset to ~5M", s.BWEst)
	}
}

// Finding while writing this test (Phase 1, 2026-09-06): "BW rise detected"
// is structurally unreachable. processCompletedMI feeds every non-app-limited
// MI's throughput into the sliding-max estimator BEFORE processBWAnchored
// runs, so in ProbeUp `throughput > estimate()×1.05` can never hold — the
// estimator already contains this throughput. Bandwidth increases ARE tracked
// (via that entry addSample), only the event never fires. The counter stays
// in the contract (Phase 3 may rework the probe); this test pins the truth so
// a production bwRise=0 is read as "expected", not "broken".
func TestSummary_BWRiseIsTrackedByEstimatorNotEvent(t *testing.T) {
	sender := NewCCSenderGFWMode(500_000_000)
	sender.SetRTTStatsProvider(&mockRTTStatsProvider{smoothedRTT: 16 * time.Millisecond})
	sender.bwEst.addSample(10_000_000)
	sender.bwEst.addSample(10_000_000)
	sender.bwPhase = bwPhaseProbeUp
	sender.state.Mode = K2CCModeDecision
	sender.state.CurrentRate = 11_000_000

	mi := &MonitorInterval{
		ID: 11, TargetRate: 11_000_000,
		BytesSent: 48000, BytesAcked: 48000, // expected 44000 → not app-limited; ≈12 Mbps
		PacketsSent: 34, PacketsAcked: 34,
		StartTime: time.Now().Add(-32 * time.Millisecond), EndTime: time.Now(),
		FirstPacketNumber: 1, LastPacketNumber: 34,
		RTTSamples: []time.Duration{16 * time.Millisecond},
	}
	sender.processCompletedMI(mi)
	s := sender.Summary()
	if s.BWAnchoredMIs != 1 {
		t.Fatalf("ProbeUp MI must count as BW-anchored: %+v", s)
	}
	if s.BWEst < 11_500_000 {
		t.Fatalf("estimator must have picked up the ≈12 Mbps ProbeUp throughput, got %.1fM", s.BWEst/1e6)
	}
	if s.BWRises != 0 {
		t.Fatalf("BWRises=%d — the rise event became reachable; update the Observability docs and §13 before relying on it", s.BWRises)
	}
}

// Anchored floor: bwEst says 1 Mbps, cruise target 0.95M clamps to bwAnchoredMinRate.
func TestSummary_FloorAnchored(t *testing.T) {
	sender := NewCCSenderGFWMode(500_000_000)
	sender.SetRTTStatsProvider(&mockRTTStatsProvider{smoothedRTT: 16 * time.Millisecond})
	sender.bwEst.addSample(1_000_000)
	sender.bwEst.addSample(1_000_000)
	sender.bwPhase = bwPhaseCruise
	sender.bwCruiseRemain = 3
	sender.state.Mode = K2CCModeDecision
	sender.state.CurrentRate = 1_000_000

	mi := &MonitorInterval{
		ID: 12, TargetRate: 1_000_000,
		BytesSent: 4000, BytesAcked: 4000, // exactly expected → not app-limited; 1 Mbps
		PacketsSent: 3, PacketsAcked: 3,
		StartTime: time.Now().Add(-32 * time.Millisecond), EndTime: time.Now(),
		FirstPacketNumber: 1, LastPacketNumber: 3,
		RTTSamples: []time.Duration{16 * time.Millisecond},
	}
	sender.processCompletedMI(mi)
	s := sender.Summary()
	if s.FloorMIs != 1 {
		t.Fatalf("FloorMIs=%d want 1 (rate=%v)", s.FloorMIs, sender.state.CurrentRate)
	}
}

// PCC floor: confirmed negative gradient at arcMinRate stays at arcMinRate.
func TestSummary_FloorPCC(t *testing.T) {
	sender := NewCCSenderGFWMode(500_000_000)
	sender.SetRTTStatsProvider(&mockRTTStatsProvider{smoothedRTT: 32 * time.Millisecond})
	sender.state.Mode = K2CCModeDecision
	sender.state.CurrentRate = arcMinRate
	sender.state.NextMIID = 5 // keep away from the every-20-MI probe
	sender.state.hasLastUtility = true
	sender.state.lastUtility = 1e12
	sender.state.UtilityDecreases = 1

	mi := &MonitorInterval{
		ID: 4, TargetRate: arcMinRate,
		BytesSent: 20000, BytesAcked: 10000, BytesLost: 10000, // 50% loss
		PacketsSent: 14, PacketsAcked: 7,
		StartTime: time.Now().Add(-32 * time.Millisecond), EndTime: time.Now(),
		FirstPacketNumber: 1, LastPacketNumber: 14,
		RTTSamples: []time.Duration{32 * time.Millisecond},
	}
	sender.processCompletedMI(mi)
	s := sender.Summary()
	if s.FloorMIs != 1 {
		t.Fatalf("FloorMIs=%d want 1 (rate=%v)", s.FloorMIs, sender.state.CurrentRate)
	}
	if s.RateProbes != 0 || s.BWAnchoredMIs != 0 {
		t.Fatalf("PCC path must not count probes/anchored: %+v", s)
	}
}

func TestSummary_RateProbeCounted(t *testing.T) {
	sender := NewCCSenderGFWMode(500_000_000)
	sender.SetRTTStatsProvider(&mockRTTStatsProvider{smoothedRTT: 32 * time.Millisecond})
	sender.state.Mode = K2CCModeDecision
	sender.state.CurrentRate = 50_000_000
	sender.state.NextMIID = 20 // NextMIID % 20 == 0 → probe

	mi := &MonitorInterval{
		ID: 19, TargetRate: 50_000_000,
		BytesSent: 200000, BytesAcked: 200000,
		PacketsSent: 143, PacketsAcked: 143,
		StartTime: time.Now().Add(-32 * time.Millisecond), EndTime: time.Now(),
		FirstPacketNumber: 1, LastPacketNumber: 143,
		RTTSamples: []time.Duration{32 * time.Millisecond},
	}
	sender.processCompletedMI(mi)
	s := sender.Summary()
	if s.RateProbes != 1 {
		t.Fatalf("RateProbes=%d want 1", s.RateProbes)
	}
	if sender.state.CurrentRate <= 50_000_000 {
		t.Fatalf("probe must raise the rate: %v", sender.state.CurrentRate)
	}
}

func TestSummary_CurrentValuesTrackLiveState(t *testing.T) {
	sender := NewCCSenderGFWMode(500_000_000)
	sender.state.Epsilon = 0.21
	sender.bgLossEWMA = 0.05
	sender.bwEst.addSample(20_000_000)
	sender.bwEst.addSample(20_000_000)
	sender.utility.MinRTT = 41 * time.Millisecond
	s := sender.Summary()
	if s.Epsilon != 0.21 || s.BGLoss != 0.05 || s.MinRTT != 41*time.Millisecond || s.Mode != K2CCModeSlowStart {
		t.Fatalf("current values: %+v", s)
	}
	want := 20_000_000 / (1 - 0.05)
	if s.BWEst < want*0.999 || s.BWEst > want*1.001 {
		t.Fatalf("BWEst=%v want bgLoss-corrected %v", s.BWEst, want)
	}
}
