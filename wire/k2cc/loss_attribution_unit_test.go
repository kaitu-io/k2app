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
	const (
		N       = 40
		pktSize = 1472
		mis     = 12
	)
	lost := func(pn int) bool { return pn%10 == 3 }

	sender := NewCCSenderGFWMode(100_000_000)
	sender.SetRTTStatsProvider(&mockRTTStatsProvider{smoothedRTT: 50 * time.Millisecond})

	type rec struct{ id, sent, lost uint64 }
	var (
		mu   sync.Mutex
		recs []rec
	)
	sender.SetMICompletedHook(func(mi *MonitorInterval, _ float64, _ bool) {
		mu.Lock()
		recs = append(recs, rec{mi.ID, mi.BytesSent, mi.BytesLost})
		mu.Unlock()
	})

	var declared, declaredLate uint64
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
		// 4. MinDuration elapses; the first-half ACKs complete the MI.
		sender.mu.Lock()
		sender.state.CurrentMI.StartTime = time.Now().Add(-200 * time.Millisecond)
		sender.mu.Unlock()
		for pn := base; pn < base+N/2; pn++ {
			if !lost(pn) {
				sender.OnPacketAcked(congestion.PacketNumber(pn), pktSize, 0, 0)
			}
		}
	}

	mu.Lock()
	defer mu.Unlock()
	if len(recs) != mis {
		t.Fatalf("completed MIs = %d, want %d (harness did not rotate once per round)", len(recs), mis)
	}
	var sumLost uint64
	for i, r := range recs {
		if r.sent != N*pktSize {
			t.Fatalf("MI %d BytesSent = %d, want %d (MI boundaries drifted from the construction)", i, r.sent, N*pktSize)
		}
		sumLost += r.lost
		// PINNED CURRENT BEHAVIOUR: only the two in-time losses are counted,
		// so LossRate = 5 % where 10 % of the MI's packets were lost.
		if want := uint64(2 * pktSize); r.lost != want {
			t.Errorf("MI %d BytesLost = %d, want %d (2 in-time losses; the 2 late ones are dropped)", i, r.lost, want)
		}
	}
	t.Logf("declared=%d B (late %d B), counted in any MI=%d B → visibility %.2f",
		declared, declaredLate, sumLost, float64(sumLost)/float64(declared))
	if sumLost+declaredLate != declared {
		t.Errorf("counted %d + late %d != declared %d: a late loss was counted somewhere", sumLost, declaredLate, declared)
	}
}
