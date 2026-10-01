package k2cc

import (
	"testing"
	"time"
)

// The benchmark harness must not manufacture loss. These tests are the
// harness's own proof: the FIFO relay delivers in order (so QUIC declares
// almost nothing at 0 % configured loss), and the out-of-order counter that
// proves it is itself able to see reordering.

// TestRelayOrderCounter_DetectsReordering pins the discriminator: a counter
// that could never be non-zero would make the "0 out of order" assertion in
// runBulkTransferFull worthless.
func TestRelayOrderCounter_DetectsReordering(t *testing.T) {
	r := &udpRelay{}
	for _, seq := range []uint64{1, 2, 4, 3, 5, 7, 6} { // 3 and 6 arrive late
		r.noteDelivered(dirS2C, seq)
	}
	for _, seq := range []uint64{1, 2, 3} {
		r.noteDelivered(dirC2S, seq)
	}
	if got := r.outOfOrder[dirS2C].Load(); got != 2 {
		t.Errorf("s2c out-of-order = %d, want 2", got)
	}
	if got := r.outOfOrder[dirC2S].Load(); got != 0 {
		t.Errorf("c2s out-of-order = %d, want 0 (in-order stream)", got)
	}
}

// TestRelayFIFO_NoReorder_NoSpuriousLoss runs short 0 %-loss transfers through
// the FIFO relay and asserts (a) zero out-of-order deliveries in both
// directions and (b) QUIC-declared loss < 1 %. The jitter scenarios are the
// ones the benchmarks actually use: jitterDelay steps the delay down at
// segment boundaries, which is exactly where a non-monotonic relay would let
// packets overtake.
//
// The reorder_control subtest runs the same transfer through the per-packet
// relay and logs what it does. It is not asserted (how much a scheduler
// reorders is machine-dependent), but it keeps the contrast in the log.
func TestRelayFIFO_NoReorder_NoSpuriousLoss(t *testing.T) {
	if testing.Short() {
		t.Skip("real QUIC transfers through a delaying relay")
	}
	const (
		writeRate = 40_000_000 / 8
		sendFor   = 3 * time.Second
	)
	scenarios := []struct {
		name  string
		delay delayFunc
		fifo  bool
	}{
		{"stable_rtt50", stableDelay(25 * time.Millisecond), true},
		{"jitter_rtt30", jitterDelay(15*time.Millisecond, 3*time.Millisecond), true},
		{"jitter_rtt150", jitterDelay(75*time.Millisecond, 15*time.Millisecond), true},
		{"reorder_control_rtt50", stableDelay(25 * time.Millisecond), false},
	}
	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			r := runLossAttributionTransfer(t, sc.delay, 0, sc.fifo, writeRate, sendFor)
			declared := 0.0
			if r.probeSentPkts > 0 {
				declared = float64(r.declaredPkts) / float64(r.probeSentPkts)
			}
			t.Logf("read=%.1fMB sent=%d pkts, QUIC-declared lost=%d (%.3f%%); relay forwarded s2c=%d c2s=%d, dropped s2c=%d c2s=%d, out-of-order s2c=%d c2s=%d, held s2c=%d c2s=%d",
				float64(r.totalRead)/1e6, r.probeSentPkts, r.declaredPkts, declared*100,
				r.relayS2CForwarded, r.relayC2SForwarded, r.relayS2CDropped, r.relayC2SDropped,
				r.relayOutOfOrder[dirS2C], r.relayOutOfOrder[dirC2S], r.relayHeld[dirS2C], r.relayHeld[dirC2S])
			if !sc.fifo {
				return // control: logged only
			}
			if r.probeSentPkts < 2000 {
				t.Fatalf("only %d packets sent — transfer too small to measure anything", r.probeSentPkts)
			}
			if r.relayS2CDropped != 0 || r.relayC2SDropped != 0 {
				t.Errorf("relay dropped packets at 0%% configured loss (s2c=%d c2s=%d) — FIFO queue overflow", r.relayS2CDropped, r.relayC2SDropped)
			}
			if r.relayOutOfOrder[dirS2C] != 0 || r.relayOutOfOrder[dirC2S] != 0 {
				t.Errorf("FIFO relay delivered out of order: s2c=%d c2s=%d", r.relayOutOfOrder[dirS2C], r.relayOutOfOrder[dirC2S])
			}
			if declared >= 0.01 {
				t.Errorf("QUIC declared %.2f%% loss at 0%% configured loss — the harness is manufacturing loss", declared*100)
			}
		})
	}
}
