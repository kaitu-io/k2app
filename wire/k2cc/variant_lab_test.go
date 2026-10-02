package k2cc

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	tls "github.com/metacubex/utls"

	"github.com/kaitu-io/kuic"
	"github.com/kaitu-io/kuic/congestion"
)

// Lab comparison of the loss-attribution A/B arms on the order-preserving
// relay (spec docs/superpowers/specs/2026-10-01-k2cc-loss-attribution-design.md §6.2).
// Long and timing-sensitive, so it only runs with K2CC_LAB=1:
//
//	K2CC_LAB=1 go test ./wire/k2cc/ -run TestLab_VariantMatrix -v -count=1 -timeout 3600s
//	K2CC_LAB_ARMS=control,fix-a3  K2CC_LAB_SCEN=cap20_rtt200  K2CC_LAB_SECS=20   (optional filters)

// labProbeCC wraps the real sender and keeps per-MI ground truth: for every
// MI, the ack-eliciting bytes it sent and the bytes of those QUIC eventually
// declared lost — however late. Test-only; all sender methods are promoted.
type labProbeCC struct {
	*K2CCSender

	mu       sync.Mutex
	sentMI   map[congestion.PacketNumber]uint64
	sentByMI map[uint64]uint64
	lostByMI map[uint64]uint64
	seen     map[uint64]labMISeen // what the controller saw when it judged the MI
}

type labMISeen struct {
	loss       float64
	ranPhase   miPhase
	appLimited bool
	resolved   uint64
	sent       uint64
}

func newLabProbeCC(inner *K2CCSender) *labProbeCC {
	p := &labProbeCC{
		K2CCSender: inner,
		sentMI:     make(map[congestion.PacketNumber]uint64),
		sentByMI:   make(map[uint64]uint64),
		lostByMI:   make(map[uint64]uint64),
		seen:       make(map[uint64]labMISeen),
	}
	inner.SetMICompletedHook(func(mi *MonitorInterval, _ float64, appLimited bool) {
		p.mu.Lock()
		p.seen[mi.ID] = labMISeen{mi.LossRate(), mi.ranPhase, appLimited, mi.AckedOwnBytes + mi.BytesLost, mi.BytesSent}
		p.mu.Unlock()
	})
	return p
}

func (c *labProbeCC) OnPacketSent(sentTime congestion.Time, bytesInFlight congestion.ByteCount, pn congestion.PacketNumber, bytes congestion.ByteCount, retransmittable bool) {
	c.K2CCSender.OnPacketSent(sentTime, bytesInFlight, pn, bytes, retransmittable)
	if !retransmittable {
		return // never acked or declared lost — has no fate to compare against
	}
	c.K2CCSender.mu.RLock()
	id := c.K2CCSender.state.CurrentMI.ID
	c.K2CCSender.mu.RUnlock()
	c.mu.Lock()
	c.sentMI[pn] = id
	c.sentByMI[id] += uint64(bytes)
	c.mu.Unlock()
}

func (c *labProbeCC) OnCongestionEvent(pn congestion.PacketNumber, lostBytes congestion.ByteCount, priorInFlight congestion.ByteCount) {
	if lostBytes > 0 {
		c.mu.Lock()
		if id, ok := c.sentMI[pn]; ok {
			c.lostByMI[id] += uint64(lostBytes)
		}
		c.mu.Unlock()
	}
	c.K2CCSender.OnCongestionEvent(pn, lostBytes, priorInFlight)
}

type labResult struct {
	goodputMbps      float64
	relayLoss        float64 // s2c dropped / offered (random + policer)
	sum              CCSummary
	rttInflation     float64    // final smoothed RTT / path RTT
	phaseSeen        [4]float64 // byte-weighted loss the controller saw, per ran-phase
	phaseTrue        [4]float64 // true final loss of the same MIs
	phaseMIs         [4]int
	seenAll, trueAll float64 // non-app-limited MIs
}

func runLabTransfer(t *testing.T, variant string, oneWay time.Duration, lossRate, capBps float64, writeRateBytesPerSec int, sendFor time.Duration) labResult {
	t.Helper()
	cert := generateTestCert(t)
	ctx, cancel := context.WithTimeout(context.Background(), sendFor+90*time.Second)
	defer cancel()

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
	ln, err := serverTransport.Listen(&tls.Config{Certificates: []tls.Certificate{cert}, NextProtos: []string{"h3"}}, qcfg)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	relay := newUDPRelayOpts(t, serverUDP.LocalAddr().(*net.UDPAddr), stableDelay(oneWay), lossRate, capBps, true)
	defer relay.Close()

	probeReady := make(chan *labProbeCC, 1)
	serverDone := make(chan error, 1)
	go func() {
		conn, err := ln.Accept(ctx)
		if err != nil {
			serverDone <- fmt.Errorf("accept: %w", err)
			return
		}
		p := newLabProbeCC(NewCCSenderVariant(variant, DefaultMaxRate))
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
			next = next.Add(interval)
			if d := time.Until(next); d > 0 {
				time.Sleep(d)
			} else if d < -time.Second {
				next = time.Now() // CC-limited writer: don't bank an unbounded burst
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
	conn, err := clientTransport.Dial(ctx, relay.Addr(), &tls.Config{InsecureSkipVerify: true, NextProtos: []string{"h3"}}, qcfg)
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
	start := time.Now()
	totalRead, err := io.Copy(io.Discard, stream)
	if err != nil {
		t.Fatal("read:", err)
	}
	elapsed := time.Since(start)
	if err := <-serverDone; err != nil {
		t.Fatal("server:", err)
	}
	probe := <-probeReady

	res := labResult{
		goodputMbps: float64(totalRead) * 8 / elapsed.Seconds() / 1e6,
		sum:         probe.K2CCSender.Summary(),
	}
	if off := relay.s2cDropped.Load() + relay.s2cForwarded.Load(); off > 0 {
		res.relayLoss = float64(relay.s2cDropped.Load()) / float64(off)
	}
	probe.K2CCSender.mu.RLock()
	res.rttInflation = float64(probe.K2CCSender.rtt) / float64(2*oneWay)
	probe.K2CCSender.mu.RUnlock()

	// Per-MI bias: what the controller saw at decision time vs the MI's true
	// final loss, byte-weighted, grouped by the phase the MI ran at.
	probe.mu.Lock()
	defer probe.mu.Unlock()
	var seenW, trueL, sentW [4]float64
	var allSeenW, allTrueL, allSent float64
	for id, s := range probe.seen {
		sent := float64(probe.sentByMI[id])
		if sent == 0 || s.appLimited {
			continue
		}
		lost := float64(probe.lostByMI[id])
		ph := s.ranPhase
		seenW[ph] += s.loss * sent
		trueL[ph] += lost
		sentW[ph] += sent
		res.phaseMIs[ph]++
		allSeenW += s.loss * sent
		allTrueL += lost
		allSent += sent
	}
	for ph := range sentW {
		if sentW[ph] > 0 {
			res.phaseSeen[ph] = seenW[ph] / sentW[ph]
			res.phaseTrue[ph] = trueL[ph] / sentW[ph]
		}
	}
	if allSent > 0 {
		res.seenAll, res.trueAll = allSeenW/allSent, allTrueL/allSent
	}
	return res
}

func TestLab_VariantMatrix(t *testing.T) {
	if os.Getenv("K2CC_LAB") != "1" {
		t.Skip("lab comparison: set K2CC_LAB=1 (long, timing-sensitive)")
	}
	secs := 12
	if v := os.Getenv("K2CC_LAB_SECS"); v != "" {
		fmt.Sscanf(v, "%d", &secs)
	}
	arms := []string{VariantControl, VariantFixA4, VariantFixA3, VariantFixA15, VariantResolvedOnly, VariantAlignOnly}
	if v := os.Getenv("K2CC_LAB_ARMS"); v != "" {
		arms = strings.Split(v, ",")
	}
	type scen struct {
		name   string
		oneWay time.Duration
		loss   float64
		capBps float64
		write  int // bytes/s offered by the app
	}
	const mbps = 1_000_000 / 8
	scens := []scen{
		// Policed bottleneck (token bucket, 20 ms burst, tail-drop): congestion
		// loss rises with rate. App offers 3× the cap.
		{"cap20_rtt50_loss0", 25 * time.Millisecond, 0, 20e6, 60 * mbps},
		{"cap20_rtt50_loss5", 25 * time.Millisecond, 0.05, 20e6, 60 * mbps},
		{"cap20_rtt50_loss15", 25 * time.Millisecond, 0.15, 20e6, 60 * mbps},
		{"cap20_rtt200_loss0", 100 * time.Millisecond, 0, 20e6, 60 * mbps},
		{"cap20_rtt200_loss5", 100 * time.Millisecond, 0.05, 20e6, 60 * mbps},
		{"cap20_rtt200_loss15", 100 * time.Millisecond, 0.15, 20e6, 60 * mbps},
		// No bottleneck within reach: pure background loss, app-paced at 40 Mbps.
		{"nocap_rtt200_loss5", 100 * time.Millisecond, 0.05, 0, 40 * mbps},
		{"nocap_rtt200_loss20", 100 * time.Millisecond, 0.20, 0, 40 * mbps},
	}
	if v := os.Getenv("K2CC_LAB_SCEN"); v != "" {
		var keep []scen
		for _, s := range scens {
			for _, want := range strings.Split(v, ",") {
				if strings.Contains(s.name, want) {
					keep = append(keep, s)
					break
				}
			}
		}
		scens = keep
	}

	type row struct {
		scen, arm string
		r         labResult
	}
	var rows []row
	for _, sc := range scens {
		for _, arm := range arms {
			t.Run(sc.name+"/"+arm, func(t *testing.T) {
				r := runLabTransfer(t, arm, sc.oneWay, sc.loss, sc.capBps, sc.write, time.Duration(secs)*time.Second)
				rows = append(rows, row{sc.name, arm, r})
			})
		}
	}

	sort.SliceStable(rows, func(i, j int) bool { return rows[i].scen < rows[j].scen })
	pct := func(v float64) string { return fmt.Sprintf("%5.1f", v*100) }
	ph := func(r labResult, p miPhase) string {
		if r.phaseMIs[p] == 0 {
			return "     -     "
		}
		return fmt.Sprintf("%4.1f/%4.1f", r.phaseSeen[p]*100, r.phaseTrue[p]*100)
	}
	t.Logf("=== lab matrix (%ds per run; loss columns in %%; seen/true = controller-seen vs final true loss of the same MIs) ===", secs)
	t.Logf("%-20s %-17s %7s %6s %6s %6s %6s %5s %6s %6s %5s %5s %5s | %-11s %-11s %-11s %-11s",
		"scenario", "arm", "goodput", "relay", "decl", "lossAg", "bgLoss", "resv", "bwEst", "rttX", "floor", "drop", "MIs",
		"all s/t", "cruise s/t", "probe s/t", "drain s/t")
	for _, x := range rows {
		s := x.r.sum
		lossAgg := 0.0
		if s.BytesSent > 0 {
			lossAgg = float64(s.BytesLost) / float64(s.BytesSent)
		}
		resv := 0.0
		if s.BytesSent > 0 {
			resv = float64(s.ResolvedBytes) / float64(s.BytesSent)
		}
		t.Logf("%-20s %-17s %7.2f %s %s %s %s %5.0f %6.1f %6.2f %5d %5d %5d | %s %s %s %s",
			x.scen, x.arm, x.r.goodputMbps, pct(x.r.relayLoss), pct(s.DeclaredLoss()), pct(lossAgg), pct(s.BGLoss),
			resv*100, s.BWEst/1e6, x.r.rttInflation, s.FloorMIs, s.BWDrops, s.MIs,
			fmt.Sprintf("%4.1f/%4.1f", x.r.seenAll*100, x.r.trueAll*100),
			ph(x.r, miPhaseCruise), ph(x.r, miPhaseProbeUp), ph(x.r, miPhaseDrain))
	}
}
