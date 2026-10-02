package k2cc

import (
	"sync"
	"testing"
	"time"

	"github.com/kaitu-io/kuic/congestion"
)

// TestLossAttribution_LateDeclaredLossIsCountedNowhere is the deterministic
// (no network, no randomness) half of loss_attribution_test.go: it pins the
// code-level fact that a loss QUIC declares after the packet's MI has rotated
// is dropped by the stale filter and lands in NO MonitorInterval — not its
// own (already completed) and not the current one.
//
// Each MI sends N packets; every 10th packet (pn%10 == 3) is lost, two in the
// MI's first half and two in its second half. Timing model (measured by the
// integration test: detection ≈ 1×RTT after send, MI ≈ 2×RTT): first-half
// losses are declared while their MI is still current; second-half losses are
// declared after rotation, together with the ACKs of the other second-half
// packets. With the filter as written, every completed MI reports exactly the
// first-half losses: 2/N instead of 4/N.
func TestLossAttribution_LateDeclaredLossIsCountedNowhere(t *testing.T) {
	sender := NewCCSenderGFWMode(100_000_000)
	recs, declared, declaredLate := runLateLossConstruction(t, sender)

	var sumLost uint64
	for i, r := range recs {
		sumLost += r.lost
		// PINNED LEGACY BEHAVIOUR (control arm): only the two in-time losses
		// are counted, so LossRate = 5 % where 10 % of the MI's packets were lost.
		if want := uint64(2 * lateLossPktSize); r.lost != want {
			t.Errorf("MI %d BytesLost = %d, want %d (2 in-time losses; the 2 late ones are dropped)", i, r.lost, want)
		}
		if r.lossRate < 0.049 || r.lossRate > 0.051 {
			t.Errorf("MI %d legacy LossRate = %.3f, want 0.050", i, r.lossRate)
		}
	}
	t.Logf("declared=%d B (late %d B), counted in any MI=%d B → visibility %.2f",
		declared, declaredLate, sumLost, float64(sumLost)/float64(declared))
	if sumLost+declaredLate != declared {
		t.Errorf("counted %d + late %d != declared %d: a late loss was counted somewhere", sumLost, declaredLate, declared)
	}
	// The event-time counters see every declared loss, late or not.
	sum := sender.Summary()
	if sum.DeclaredLostBytes != declared || sum.StaleLostBytes != declaredLate {
		t.Errorf("summary declared=%d stale=%d, want %d / %d", sum.DeclaredLostBytes, sum.StaleLostBytes, declared, declaredLate)
	}
}

// Same construction, resolved attribution: the late losses are still dropped
// (the stale filter stays), but the denominator is the MI's own RESOLVED
// packets — 18 acked + 2 lost in the first half — so every MI reports the
// true 10 %.
func TestLossAttribution_ResolvedDenominatorSeesTrueRate(t *testing.T) {
	sender := NewCCSenderVariant(VariantFixA3, 100_000_000)
	recs, _, _ := runLateLossConstruction(t, sender)
	for i, r := range recs {
		if r.lossRate < 0.099 || r.lossRate > 0.101 {
			t.Errorf("MI %d resolved LossRate = %.3f, want 0.100", i, r.lossRate)
		}
		if want := uint64(20 * lateLossPktSize); r.resolved != want {
			t.Errorf("MI %d resolved bytes = %d, want %d (first half of the MI)", i, r.resolved, want)
		}
	}
}

const lateLossPktSize = 1472

type lateLossRec struct {
	id, sent, lost, resolved uint64
	lossRate                 float64
}

// runLateLossConstruction: 12 MIs × 40 packets, every 10th packet lost (two
// per half); first-half fates arrive in time, second-half fates after the MI
// rotated. Returns one record per completed MI plus declared / late-declared
// loss bytes.
func runLateLossConstruction(t *testing.T, sender *K2CCSender) (recs []lateLossRec, declared, declaredLate uint64) {
	t.Helper()
	const (
		N       = 40
		pktSize = lateLossPktSize
		mis     = 12
	)
	lost := func(pn int) bool { return pn%10 == 3 }
	sender.SetRTTStatsProvider(&mockRTTStatsProvider{smoothedRTT: 50 * time.Millisecond})

	var mu sync.Mutex
	sender.SetMICompletedHook(func(mi *MonitorInterval, _ float64, _ bool) {
		mu.Lock()
		recs = append(recs, lateLossRec{mi.ID, mi.BytesSent, mi.BytesLost, mi.AckedOwnBytes + mi.BytesLost, mi.LossRate()})
		mu.Unlock()
	})

	for k := 0; k < mis; k++ {
		base := k * N
		// 1. The MI sends its packets.
		for pn := base; pn < base+N; pn++ {
			sender.OnPacketSent(0, 0, congestion.PacketNumber(pn), pktSize, true)
		}
		// 2. Late events for the PREVIOUS MI's second half arrive now: its
		//    losses (stale by the time they are declared) and its ACKs. MI k
		//    is too young to complete here (MinDuration = 2×RTT not reached).
		if k > 0 {
			prev := base - N
			for pn := prev + N/2; pn < base; pn++ {
				if lost(pn) {
					sender.OnCongestionEvent(congestion.PacketNumber(pn), pktSize, 0)
					declared += pktSize
					declaredLate += pktSize
				} else {
					sender.OnPacketAcked(congestion.PacketNumber(pn), pktSize, 0, 0)
				}
			}
		}
		// 3. In-time losses for this MI's first half.
		for pn := base; pn < base+N/2; pn++ {
			if lost(pn) {
				sender.OnCongestionEvent(congestion.PacketNumber(pn), pktSize, 0)
				declared += pktSize
			}
		}
		// 4. The first-half ACKs arrive during the MI's second RTT; the last
		//    of them lands once MinDuration has elapsed and completes the MI.
		var own []int
		for pn := base; pn < base+N/2; pn++ {
			if !lost(pn) {
				own = append(own, pn)
			}
		}
		for _, pn := range own[:len(own)-1] {
			sender.OnPacketAcked(congestion.PacketNumber(pn), pktSize, 0, 0)
		}
		sender.mu.Lock()
		sender.state.CurrentMI.StartTime = time.Now().Add(-200 * time.Millisecond)
		sender.mu.Unlock()
		sender.OnPacketAcked(congestion.PacketNumber(own[len(own)-1]), pktSize, 0, 0)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(recs) != mis {
		t.Fatalf("completed MIs = %d, want %d (harness did not rotate once per round)", len(recs), mis)
	}
	for i, r := range recs {
		if r.sent != N*pktSize {
			t.Fatalf("MI %d BytesSent = %d, want %d (MI boundaries drifted from the construction)", i, r.sent, N*pktSize)
		}
	}
	return recs, declared, declaredLate
}

// Below minResolvedPkts the resolved sample is too small to trust; LossRate
// falls back to the legacy formula instead of reporting 1 lost / 2 resolved = 50 %.
func TestLossAttribution_ResolvedFallsBackOnTinySample(t *testing.T) {
	mi := &MonitorInterval{lossAttr: LossAttrResolved, BytesSent: 100_000, BytesLost: 1000, AckedOwnBytes: 1000, ResolvedPkts: 2}
	if got := mi.LossRate(); got != 0.01 {
		t.Errorf("tiny sample LossRate = %v, want legacy 0.01", got)
	}
	mi.ResolvedPkts = minResolvedPkts
	if got := mi.LossRate(); got != 0.5 {
		t.Errorf("at minResolvedPkts LossRate = %v, want resolved 0.5", got)
	}
}

// kuic reports an ECN congestion signal as OnCongestionEvent(pn, 0, …): it is
// not a lost packet and must not count as a declared loss or a resolved packet.
func TestLossAttribution_ECNEventIsNotALoss(t *testing.T) {
	for _, v := range []string{VariantControl, VariantFixA3} {
		sender := NewCCSenderVariant(v, 100_000_000)
		sender.OnPacketSent(0, 0, 0, 1472, true)
		sender.OnCongestionEvent(0, 0, 0)
		sum := sender.Summary()
		sender.mu.RLock()
		resolved := sender.state.CurrentMI.ResolvedPkts
		sender.mu.RUnlock()
		if sum.DeclaredLostPkts != 0 || sum.DeclaredLostBytes != 0 || resolved != 0 {
			t.Errorf("%s: ECN event counted (declaredPkts=%d bytes=%d resolvedPkts=%d)", v, sum.DeclaredLostPkts, sum.DeclaredLostBytes, resolved)
		}
	}
}
