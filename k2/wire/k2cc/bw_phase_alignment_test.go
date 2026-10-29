package k2cc

import (
	"testing"
	"time"
)

// TestBWAnchored_PhaseLabelVsRateTheMIRanAt drives the real BW-anchored state
// machine through two full cycles and records, for every completed MI, the
// gain it actually RAN at (TargetRate / bw) next to the phase label it was
// EVALUATED under (p.bwPhase at processBWAnchored entry).
//
// processBWAnchored's phase label names the rate it is about to SET, so the
// MI evaluated under label X ran at the rate set one step earlier:
//
//	label Cruise  → MI ran at cruise (0.95) — except the first, which ran at drain (0.75)
//	label ProbeUp → MI ran at cruise (0.95)   ← BW-rise check never sees the 1.10 MI
//	label Drain   → MI ran at probe-up (1.10) ← bgLoss is sampled on the MOST congesting MI
func TestBWAnchored_PhaseLabelVsRateTheMIRanAt(t *testing.T) {
	const bw = 20_000_000.0
	sender := NewCCSenderGFWMode(500_000_000)
	sender.SetRTTStatsProvider(&mockRTTStatsProvider{smoothedRTT: 20 * time.Millisecond})
	sender.bwEst.addSample(bw)
	sender.bwEst.addSample(bw)
	sender.bwPhase = bwPhaseCruise
	sender.bwCruiseRemain = bwCruiseLength
	sender.state.Mode = K2CCModeDecision
	sender.state.CurrentRate = bw * bwCruiseGain

	type row struct {
		label bwProbePhase
		gain  float64
	}
	var rows []row
	for i := 0; i < 2*(bwCruiseLength+2)+2; i++ {
		rate := sender.state.CurrentRate // the rate this MI runs at
		dur := 40 * time.Millisecond
		sent := uint64(rate / 8 * dur.Seconds())
		// Delivered = min(rate, bw): a clean bottleneck, no loss reported, so
		// bwEst stays at bw and the gains are easy to read.
		acked := sent
		if rate > bw {
			acked = uint64(bw / 8 * dur.Seconds())
		}
		mi := &MonitorInterval{
			ID: uint64(i), TargetRate: rate,
			BytesSent: sent, BytesAcked: acked,
			PacketsSent: sent / 1400, PacketsAcked: acked / 1400,
			RTTSamples: []time.Duration{20 * time.Millisecond},
			StartTime:  time.Now().Add(-dur), EndTime: time.Now(),
			FirstPacketNumber: int64(i * 1000), LastPacketNumber: int64(i*1000 + 999),
		}
		rows = append(rows, row{sender.bwPhase, rate / bw})
		sender.processCompletedMI(mi)
	}

	name := []string{"cruise", "probeUp", "drain"}
	for i, r := range rows {
		t.Logf("MI %2d evaluated under label %-7s ran at %.2f×bw", i, name[r.label], r.gain)
	}
	near := func(a, b float64) bool { return a > b*0.97 && a < b*1.03 }
	var sawDrainLabel, sawProbeLabel bool
	for i, r := range rows {
		if i < bwCruiseLength+2 { // skip the first cycle (EWMA settling from the hand-set start)
			continue
		}
		switch r.label {
		case bwPhaseDrain:
			sawDrainLabel = true
			// PINNED CURRENT BEHAVIOUR (off by one): the MI whose loss feeds
			// bgLossEWMA ran at the probe-up rate, not the drain rate.
			if !near(r.gain, bwProbeUpGain) {
				t.Errorf("MI %d under label drain ran at %.2f×bw, pinned %.2f (probe-up rate)", i, r.gain, bwProbeUpGain)
			}
		case bwPhaseProbeUp:
			sawProbeLabel = true
			if !near(r.gain, bwCruiseGain) {
				t.Errorf("MI %d under label probeUp ran at %.2f×bw, pinned %.2f (cruise rate)", i, r.gain, bwCruiseGain)
			}
		}
	}
	if !sawDrainLabel || !sawProbeLabel {
		t.Fatalf("cycle did not reach both labels (drain=%v probeUp=%v)", sawDrainLabel, sawProbeLabel)
	}
}

// TestBWAnchored_AlignedPhasesSampleBGLossOnTheDrainMI runs the same cycle on
// a clean bottleneck where only the probe-up-rate MI loses packets (the 10 %
// overshoot) and the drain-rate MI loses none.
//
//	legacy  → that congestion loss is sampled as "background loss"
//	aligned → bgLoss is sampled on the MI that ran at the drain rate: stays 0
func TestBWAnchored_AlignedPhasesSampleBGLossOnTheDrainMI(t *testing.T) {
	run := func(variant string) (bg float64, drops uint64) {
		const bw = 20_000_000.0
		sender := NewCCSenderVariant(variant, 500_000_000)
		sender.SetRTTStatsProvider(&mockRTTStatsProvider{smoothedRTT: 20 * time.Millisecond})
		sender.bwEst.addSample(bw)
		sender.bwEst.addSample(bw)
		sender.bwPhase = bwPhaseCruise
		sender.bwCruiseRemain = bwCruiseLength
		sender.state.Mode = K2CCModeDecision
		sender.state.CurrentRate = bw * bwCruiseGain
		for i := 0; i < 4*(bwCruiseLength+2); i++ {
			rate := sender.state.CurrentRate
			dur := 40 * time.Millisecond
			sent := uint64(rate / 8 * dur.Seconds())
			acked, lost := sent, uint64(0)
			if rate > bw { // overshoot is dropped at the bottleneck
				acked = uint64(bw / 8 * dur.Seconds())
				lost = sent - acked
			}
			mi := &MonitorInterval{
				ID: uint64(i), TargetRate: rate,
				BytesSent: sent, BytesAcked: acked, BytesLost: lost,
				AckedOwnBytes: acked, ResolvedPkts: sent / 1400,
				PacketsSent: sent / 1400, PacketsAcked: acked / 1400,
				RTTSamples: []time.Duration{20 * time.Millisecond},
				StartTime:  time.Now().Add(-dur), EndTime: time.Now(),
				FirstPacketNumber: int64(i * 1000), LastPacketNumber: int64(i*1000 + 999),
				lossAttr: sender.config.LossAttribution,
				ranPhase: sender.state.nextRanPhase,
			}
			sender.processCompletedMI(mi)
		}
		return sender.bgLossEWMA, sender.Summary().BWDrops
	}

	legacyBG, _ := run(VariantControl)
	alignedBG, drops := run(VariantFixA3)
	t.Logf("bgLossEWMA after 4 cycles on a loss-free bottleneck: legacy %.3f, aligned %.3f", legacyBG, alignedBG)
	if legacyBG < 0.01 {
		t.Errorf("legacy bgLoss = %.4f: expected the probe-up overshoot to be mis-sampled as background loss (pinned)", legacyBG)
	}
	if alignedBG != 0 {
		t.Errorf("aligned bgLoss = %.4f, want 0: the drain-rate MI lost nothing", alignedBG)
	}
	if drops != 0 {
		t.Errorf("aligned: %d spurious BW drops — the drain-rate MI must not be judged as a cruise MI", drops)
	}
}
