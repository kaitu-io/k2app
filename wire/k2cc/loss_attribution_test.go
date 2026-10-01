package k2cc

import (
	"context"
	"fmt"
	"io"
	"net"
	"slices"
	"sync"
	"testing"
	"time"

	tls "github.com/metacubex/utls"

	"github.com/kaitu-io/kuic"
	"github.com/kaitu-io/kuic/congestion"
)

// Loss attribution accuracy — how much of the loss QUIC declares actually
// reaches MonitorInterval.BytesLost (and therefore LossRate(), the utility's
// loss term, and the cc-summary loss histogram).
//
// Mechanism under test: an MI lives MinDuration = 2×RTT from its first send
// and only rotates on an ACK after that. QUIC declares a packet lost roughly
// one RTT after it was sent (packet threshold at bulk rates, 9/8·RTT time
// threshold otherwise). A packet sent in the tail of an MI is therefore
// usually declared lost after the MI has rotated, and OnCongestionEvent's
// stale-loss filter (`number < mi.FirstPacketNumber` → return) discards it —
// it is attributed to neither its own MI nor the next. Expected steady-state
// visibility ≈ (MI duration − detection delay) / MI duration ≈ ½.
//
// Fleet evidence that prompted this: over 2853 connections ≥ 100 MB,
// Σ(lossAgg×sent)/(sent−acked) = 0.47, stable across regions and loss bands.
//
// Measured (2026-10-01, FIFO relay, stableDelay): QUIC-declared loss equals
// relay drops (no spurious loss), K2CC sees 0.48–0.59 of it at RTT 50/200 ms
// × 5/10/20 % loss; median detection delay 1.00×RTT, median MI 2.0×RTT.
// Mutation (remove the early return) → ratio 1.00 in every scenario.
//
// Second finding: the per-packet relay (newUDPRelay, one goroutine+timer per
// packet) reorders packets, and QUIC's packet-threshold detection turns that
// into ~45 % SPURIOUS loss even at 0 % configured loss. Until 2026-10-01 it
// was the shared harness relay, so every benchmark built on runBulkTransfer
// ran at far more declared loss than its nominal lossRate; the harness now
// uses the FIFO relay. The reorder_* scenarios below keep the artifact
// visible and deliberately stay on the per-packet relay.

// lossProbeCC wraps the real sender and records every loss QUIC declares,
// independently of K2CC's own attribution. Embedding promotes all methods
// (including OnCongestionEventEx, so quic-go still sees a CongestionControlEx);
// only OnPacketSent and OnCongestionEvent are intercepted. Test-only.
type lossProbeCC struct {
	*K2CCSender

	mu            sync.Mutex
	sentPkts      uint64
	sentBytes     uint64
	sentAt        map[congestion.PacketNumber]time.Time
	sentMI        map[congestion.PacketNumber]uint64
	declaredPkts  uint64
	declaredBytes uint64
	// Classification at the moment QUIC declares the loss, using exactly the
	// filter predicate OnCongestionEvent applies.
	acceptedBytes uint64 // passes the filter → lands in some MI's BytesLost
	filteredBytes uint64 // stale → discarded
	sameMIBytes   uint64 // declared while the packet's own MI was still current
	delayOverRTT  []float64
}

func newLossProbeCC(inner *K2CCSender) *lossProbeCC {
	return &lossProbeCC{
		K2CCSender: inner,
		sentAt:     make(map[congestion.PacketNumber]time.Time),
		sentMI:     make(map[congestion.PacketNumber]uint64),
	}
}

func (c *lossProbeCC) OnPacketSent(sentTime congestion.Time, bytesInFlight congestion.ByteCount, pn congestion.PacketNumber, bytes congestion.ByteCount, retransmittable bool) {
	c.K2CCSender.OnPacketSent(sentTime, bytesInFlight, pn, bytes, retransmittable)
	c.K2CCSender.mu.RLock()
	miID := c.K2CCSender.state.CurrentMI.ID
	c.K2CCSender.mu.RUnlock()

	c.mu.Lock()
	c.sentPkts++
	c.sentBytes += uint64(bytes)
	c.sentAt[pn] = time.Now()
	c.sentMI[pn] = miID
	c.mu.Unlock()
}

func (c *lossProbeCC) OnCongestionEvent(pn congestion.PacketNumber, lostBytes congestion.ByteCount, priorInFlight congestion.ByteCount) {
	if lostBytes > 0 {
		c.K2CCSender.mu.RLock()
		mi := c.K2CCSender.state.CurrentMI
		stale := mi == nil || mi.PacketsSent == 0 || int64(pn) < mi.FirstPacketNumber
		var curID uint64
		if mi != nil {
			curID = mi.ID
		}
		rtt := c.K2CCSender.rtt
		c.K2CCSender.mu.RUnlock()

		c.mu.Lock()
		c.declaredPkts++
		c.declaredBytes += uint64(lostBytes)
		if stale {
			c.filteredBytes += uint64(lostBytes)
		} else {
			c.acceptedBytes += uint64(lostBytes)
		}
		if id, ok := c.sentMI[pn]; ok && mi != nil && id == curID {
			c.sameMIBytes += uint64(lostBytes)
		}
		if at, ok := c.sentAt[pn]; ok && rtt > 0 {
			c.delayOverRTT = append(c.delayOverRTT, float64(time.Since(at))/float64(rtt))
		}
		c.mu.Unlock()
	}
	c.K2CCSender.OnCongestionEvent(pn, lostBytes, priorInFlight)
}

type lossAttrResult struct {
	relayS2CDropped, relayS2CForwarded uint64
	relayC2SDropped, relayC2SForwarded uint64
	relayOutOfOrder, relayHeld         [2]uint64 // [dirS2C, dirC2S]

	probeSentPkts, probeSentBytes uint64
	declaredPkts, declaredBytes   uint64
	acceptedBytes, filteredBytes  uint64
	sameMIBytes                   uint64
	medianDelayOverRTT            float64

	records   []MIRecord
	totalRead int64
	finalRTT  time.Duration
}

// runLossAttributionTransfer is runBulkTransferFull reduced to what this test
// needs, plus: the server CC is wrapped in lossProbeCC, the relay's drop
// counters are returned, and the server writes for a fixed wall-clock
// duration (instead of a fixed size) so every scenario costs the same time
// regardless of where the controller converges.
func runLossAttributionTransfer(t *testing.T, delay delayFunc, lossRate float64, fifo bool, writeRateBytesPerSec int, sendFor time.Duration) lossAttrResult {
	t.Helper()
	cert := generateTestCert(t)
	ctx, cancel := context.WithTimeout(context.Background(), sendFor+90*time.Second)
	defer cancel()

	env := &ccTestEnv{}
	var probe *lossProbeCC

	serverUDP, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer serverUDP.Close()
	serverTransport := &quic.Transport{Conn: serverUDP}
	defer serverTransport.Close()

	qcfg := &quic.Config{
		MaxIdleTimeout:                 30 * time.Second,
		EnableDatagrams:                true,
		InitialStreamReceiveWindow:     8 * 1024 * 1024,
		MaxStreamReceiveWindow:         8 * 1024 * 1024,
		InitialConnectionReceiveWindow: 20 * 1024 * 1024,
		MaxConnectionReceiveWindow:     20 * 1024 * 1024,
	}
	ln, err := serverTransport.Listen(&tls.Config{
		Certificates: []tls.Certificate{cert},
		NextProtos:   []string{"h3"},
	}, qcfg)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	var relay *udpRelay
	if fifo {
		relay = newUDPRelayFIFO(t, serverUDP.LocalAddr().(*net.UDPAddr), delay, lossRate)
	} else {
		relay = newUDPRelay(t, serverUDP.LocalAddr().(*net.UDPAddr), delay, lossRate)
	}
	defer relay.Close()

	probeReady := make(chan *lossProbeCC, 1)
	serverDone := make(chan error, 1)
	go func() {
		conn, err := ln.Accept(ctx)
		if err != nil {
			serverDone <- fmt.Errorf("accept: %w", err)
			return
		}
		inner := NewCCSenderGFWMode(DefaultMaxRate)
		inner.SetMICompletedHook(env.collectMI)
		p := newLossProbeCC(inner)
		conn.SetCongestionControl(p)
		probeReady <- p

		stream, err := conn.AcceptStream(ctx)
		if err != nil {
			serverDone <- fmt.Errorf("accept stream: %w", err)
			return
		}
		if _, err := stream.Read(make([]byte, 1)); err != nil {
			serverDone <- fmt.Errorf("read request: %w", err)
			return
		}
		const chunkSize = 32 * 1024
		data := make([]byte, chunkSize)
		interval := time.Duration(float64(time.Second) * chunkSize / float64(writeRateBytesPerSec))
		deadline := time.Now().Add(sendFor)
		next := time.Now()
		for time.Now().Before(deadline) {
			if _, err := stream.Write(data); err != nil {
				serverDone <- fmt.Errorf("write: %w", err)
				return
			}
			// Absolute schedule: sleep overshoot on a loaded machine does not
			// accumulate into a lower write rate.
			next = next.Add(interval)
			if d := time.Until(next); d > 0 {
				time.Sleep(d)
			}
		}
		stream.Close()
		serverDone <- nil
	}()

	clientUDP, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer clientUDP.Close()
	clientTransport := &quic.Transport{Conn: clientUDP}
	defer clientTransport.Close()

	conn, err := clientTransport.Dial(ctx, relay.Addr(), &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{"h3"},
	}, qcfg)
	if err != nil {
		t.Fatal("client dial:", err)
	}
	stream, err := conn.OpenStreamSync(ctx)
	if err != nil {
		t.Fatal("open stream:", err)
	}
	if _, err := stream.Write([]byte{0x01}); err != nil {
		t.Fatal("write request:", err)
	}
	totalRead, err := io.Copy(io.Discard, stream)
	if err != nil {
		t.Fatal("read:", err)
	}
	if err := <-serverDone; err != nil {
		t.Fatal("server:", err)
	}
	probe = <-probeReady

	res := lossAttrResult{
		relayS2CDropped:   relay.s2cDropped.Load(),
		relayS2CForwarded: relay.s2cForwarded.Load(),
		relayC2SDropped:   relay.c2sDropped.Load(),
		relayC2SForwarded: relay.c2sForwarded.Load(),
		records:           env.getRecords(),
		totalRead:         totalRead,
	}
	for dir := range res.relayOutOfOrder {
		res.relayOutOfOrder[dir] = relay.outOfOrder[dir].Load()
		res.relayHeld[dir] = relay.held[dir].Load()
	}
	probe.mu.Lock()
	res.probeSentPkts, res.probeSentBytes = probe.sentPkts, probe.sentBytes
	res.declaredPkts, res.declaredBytes = probe.declaredPkts, probe.declaredBytes
	res.acceptedBytes, res.filteredBytes = probe.acceptedBytes, probe.filteredBytes
	res.sameMIBytes = probe.sameMIBytes
	if n := len(probe.delayOverRTT); n > 0 {
		d := slices.Clone(probe.delayOverRTT)
		slices.Sort(d)
		res.medianDelayOverRTT = d[n/2]
	}
	probe.mu.Unlock()
	probe.K2CCSender.mu.RLock()
	res.finalRTT = probe.K2CCSender.rtt
	probe.K2CCSender.mu.RUnlock()
	return res
}

// TestIntegration_LossAttributionAccuracy measures, per scenario:
//
//	cfg       relay's configured random drop rate (applied independently to
//	          BOTH directions; only server→client drops lose data — c2s drops
//	          hit ACKs, which QUIC's cumulative ACK ranges absorb)
//	relay     ground truth: s2c dropped / s2c offered datagrams
//	quic      QUIC-declared loss: lost packets / sent packets at the CC hook
//	seenAll   Σ MI.BytesLost / Σ MI.BytesSent, every completed MI
//	seenNAL   same, non-app-limited MIs only (what drives the gradient)
//	ratio     seenAll / quic — the attribution accuracy under test
//
// The assertion pins CURRENT behaviour (K2CC sees roughly half of the loss),
// following this package's convention of pinning known structural facts
// (CLAUDE.md "Three facts the counters expose") rather than skipping: a
// t.Skip'd desired-state test would stop measuring anything, while the band
// fails loudly the moment attribution changes in either direction. A fix to
// the stale-loss filter must flip it to ratio ≥ 0.85.
func TestIntegration_LossAttributionAccuracy(t *testing.T) {
	if testing.Short() {
		t.Skip("real QUIC transfers through a delaying relay")
	}
	const (
		writeRate = 40_000_000 / 8 // 40 Mbps writer: keeps the relay's per-packet goroutines bounded
		sendFor   = 8 * time.Second
	)
	// fifo=true: order-preserving relay — relay drops are the only loss, so
	// the ratio isolates the MI filter. fifo=false: the per-packet-goroutine
	// relay (the harness default until 2026-10-01), which reorders and makes QUIC declare
	// spurious loss; logged (not asserted) so the harness artifact stays
	// visible next to the real measurement.
	scenarios := []struct {
		name     string
		oneWay   time.Duration
		lossRate float64
		fifo     bool
	}{
		{"rtt50_loss5", 25 * time.Millisecond, 0.05, true},
		{"rtt50_loss10", 25 * time.Millisecond, 0.10, true},
		{"rtt50_loss20", 25 * time.Millisecond, 0.20, true},
		{"rtt200_loss5", 100 * time.Millisecond, 0.05, true},
		{"rtt200_loss10", 100 * time.Millisecond, 0.10, true},
		{"rtt200_loss20", 100 * time.Millisecond, 0.20, true},
		{"reorder_rtt50_loss0", 25 * time.Millisecond, 0.00, false},
		{"reorder_rtt50_loss5", 25 * time.Millisecond, 0.05, false},
	}

	type row struct {
		name                                      string
		cfg, relay, quic, seenAll, seenNAL, ratio float64
		filteredFrac, sameMIFrac, delayRTT        float64
		miDurRTT                                  float64
		mis, nal                                  int
	}
	var rows []row

	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			r := runLossAttributionTransfer(t, stableDelay(sc.oneWay), sc.lossRate, sc.fifo, writeRate, sendFor)

			var sentAll, lostAll, sentNAL, lostNAL, ackedAll uint64
			var durs []float64
			nal := 0
			for _, m := range r.records {
				sentAll += m.BytesSent
				lostAll += m.BytesLost
				ackedAll += m.BytesAcked
				if !m.AppLimited {
					nal++
					sentNAL += m.BytesSent
					lostNAL += m.BytesLost
				}
				if r.finalRTT > 0 {
					durs = append(durs, float64(m.Duration)/float64(r.finalRTT))
				}
			}
			ratioOf := func(a, b uint64) float64 {
				if b == 0 {
					return 0
				}
				return float64(a) / float64(b)
			}
			rw := row{
				name:         sc.name,
				cfg:          sc.lossRate,
				relay:        ratioOf(r.relayS2CDropped, r.relayS2CDropped+r.relayS2CForwarded),
				quic:         ratioOf(r.declaredPkts, r.probeSentPkts),
				seenAll:      ratioOf(lostAll, sentAll),
				seenNAL:      ratioOf(lostNAL, sentNAL),
				filteredFrac: ratioOf(r.filteredBytes, r.declaredBytes),
				sameMIFrac:   ratioOf(r.sameMIBytes, r.declaredBytes),
				delayRTT:     r.medianDelayOverRTT,
				miDurRTT:     median(durs),
				mis:          len(r.records),
				nal:          nal,
			}
			if rw.quic > 0 {
				rw.ratio = rw.seenAll / rw.quic
			}
			rows = append(rows, rw)

			t.Logf("read=%.1fMB rtt=%v MIs=%d (non-app-limited %d)", float64(r.totalRead)/1e6, r.finalRTT, rw.mis, nal)
			t.Logf("relay s2c: dropped=%d forwarded=%d | c2s: dropped=%d forwarded=%d",
				r.relayS2CDropped, r.relayS2CForwarded, r.relayC2SDropped, r.relayC2SForwarded)
			t.Logf("quic: sent=%d pkts / %d B, declared lost=%d pkts / %d B; filter accepted=%d B, filtered(stale)=%d B",
				r.probeSentPkts, r.probeSentBytes, r.declaredPkts, r.declaredBytes, r.acceptedBytes, r.filteredBytes)
			t.Logf("MI sums: sent=%d acked=%d lost=%d; fleet-style Σlost/(sent−acked)=%.3f",
				sentAll, ackedAll, lostAll, ratioOf(lostAll, sentAll-min(ackedAll, sentAll)))
			t.Logf("median loss-detection delay = %.2f×RTT, median MI duration = %.2f×RTT, declared while own MI current = %.0f%%",
				rw.delayRTT, rw.miDurRTT, rw.sameMIFrac*100)

			if !sc.fifo {
				t.Logf("per-packet relay: QUIC declared %.3f vs relay %.3f (spurious ≈ %.3f) — harness artifact, not asserted",
					rw.quic, rw.relay, rw.quic-rw.relay)
				return
			}
			if r.declaredPkts < 200 {
				t.Fatalf("only %d QUIC-declared losses — too few to measure attribution", r.declaredPkts)
			}
			if nal == 0 {
				t.Fatalf("every MI app-limited — scenario did not exercise the controller")
			}
			// Sanity: QUIC declares (nearly) what the relay dropped. Spurious
			// declarations or undetected tail drops would show up here and
			// would mean the gap is not the MI filter.
			if rw.quic < rw.relay*0.8 || rw.quic > rw.relay*1.25 {
				t.Errorf("QUIC-declared loss %.3f far from relay ground truth %.3f", rw.quic, rw.relay)
			}
			// Filter bookkeeping must reconcile with MI sums (the probe
			// replicates the filter predicate exactly; MIs still open at the
			// end make ΣBytesLost ≤ accepted).
			if lostAll > r.acceptedBytes {
				t.Errorf("Σ MI.BytesLost %d > filter-accepted %d — probe predicate out of sync with OnCongestionEvent", lostAll, r.acceptedBytes)
			}

			// PINNED CURRENT BEHAVIOUR: K2CC sees roughly half of the loss.
			// When the stale filter is fixed (attribute each loss to the MI
			// that SENT the packet, and delay MI evaluation until its losses
			// are resolvable), replace this band with `rw.ratio >= 0.85`.
			if rw.ratio < 0.25 || rw.ratio > 0.75 {
				t.Errorf("attribution ratio seen/declared = %.2f, outside the pinned current band [0.25, 0.75] — "+
					"if the stale-loss filter was fixed, update this test to assert ≥ 0.85", rw.ratio)
			}
		})
	}

	t.Logf("=== Loss attribution summary ===")
	t.Logf("%-20s %5s %6s %6s %7s %7s %6s %8s %7s %7s %7s", "scenario", "cfg", "relay", "quic", "seenAll", "seenNAL", "ratio", "filtered", "sameMI", "det/RTT", "MI/RTT")
	for _, r := range rows {
		t.Logf("%-20s %5.2f %6.3f %6.3f %7.3f %7.3f %6.2f %8.2f %7.2f %7.2f %7.2f",
			r.name, r.cfg, r.relay, r.quic, r.seenAll, r.seenNAL, r.ratio, r.filteredFrac, r.sameMIFrac, r.delayRTT, r.miDurRTT)
	}
}
