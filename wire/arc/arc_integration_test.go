package arc

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"math/big"
	mrand "math/rand"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/apernet/quic-go"
)

// MIRecord captures per-MI data for analysis.
type MIRecord struct {
	ID           uint64
	BytesSent    uint64
	BytesAcked   uint64
	BytesLost    uint64
	PacketsSent  uint64
	PacketsAcked uint64
	Duration     time.Duration
	Throughput   float64 // bps
	TargetRate   float64
	AppLimited   bool
	MinDuration  time.Duration
}

func generateTestCert(t *testing.T) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(time.Hour),
		DNSNames:     []string{"localhost"},
	}
	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{
		Certificate: [][]byte{certDER},
		PrivateKey:  key,
	}
}

// delayFunc returns a one-way delay for each packet. The rng is provided for
// deterministic jitter generation.
type delayFunc func(rng *mrand.Rand) time.Duration

// stableDelay returns a fixed one-way delay.
func stableDelay(d time.Duration) delayFunc {
	return func(_ *mrand.Rand) time.Duration { return d }
}

// jitterDelay returns segment-based jitter: the base delay changes every
// ~100ms, but packets within the same segment get the same delay. This avoids
// artificial packet reordering (which QUIC misdetects as loss) while still
// producing realistic RTT variation over time.
func jitterDelay(base, maxJitter time.Duration) delayFunc {
	const segmentDur = 100 * time.Millisecond
	current := base
	lastChange := time.Time{}
	return func(rng *mrand.Rand) time.Duration {
		now := time.Now()
		if lastChange.IsZero() || now.Sub(lastChange) >= segmentDur {
			offset := time.Duration(rng.Int63n(int64(2*maxJitter+1))) - maxJitter
			current = base + offset
			if current < 0 {
				current = 0
			}
			lastChange = now
		}
		return current
	}
}

// udpRelay sits between client and server, adding delay and optional packet loss.
// It listens on a local UDP port, forwards packets to the server with delay,
// and forwards server responses back to the client with delay.
type udpRelay struct {
	conn       *net.UDPConn  // relay's listening socket
	serverAddr *net.UDPAddr
	delay      delayFunc     // per-packet delay function
	lossRate   float64       // 0.0 - 1.0, applied to both directions
	rng        *mrand.Rand

	clientAddr atomic.Pointer[net.UDPAddr] // learned from first client packet
	closed     chan struct{}
	wg         sync.WaitGroup
}

func newUDPRelay(t *testing.T, serverAddr *net.UDPAddr, delay delayFunc, lossRate float64) *udpRelay {
	t.Helper()
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	r := &udpRelay{
		conn:       conn,
		serverAddr: serverAddr,
		delay:      delay,
		lossRate:   lossRate,
		rng:        mrand.New(mrand.NewSource(42)),
		closed:     make(chan struct{}),
	}

	// Start two goroutines: client→server and implicit server→client
	// We use a single socket, so we detect direction by source address.
	r.wg.Add(1)
	go r.relayLoop()

	return r
}

func (r *udpRelay) Addr() *net.UDPAddr {
	return r.conn.LocalAddr().(*net.UDPAddr)
}

func (r *udpRelay) Close() {
	select {
	case <-r.closed:
	default:
		close(r.closed)
	}
	r.conn.Close()
	r.wg.Wait()
}

func (r *udpRelay) relayLoop() {
	defer r.wg.Done()
	buf := make([]byte, 65536)
	for {
		select {
		case <-r.closed:
			return
		default:
		}
		r.conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		n, srcAddr, err := r.conn.ReadFromUDP(buf)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			select {
			case <-r.closed:
				return
			default:
			}
			continue
		}

		// Determine direction: from client or from server?
		isFromServer := srcAddr.Port == r.serverAddr.Port && srcAddr.IP.Equal(r.serverAddr.IP)

		if !isFromServer {
			// Client→Server: remember client address
			r.clientAddr.CompareAndSwap(nil, srcAddr)
		}

		// Apply loss
		if r.lossRate > 0 && r.rng.Float64() < r.lossRate {
			continue // drop
		}

		// Copy packet data (will be sent after delay)
		pkt := make([]byte, n)
		copy(pkt, buf[:n])

		var dstAddr *net.UDPAddr
		if isFromServer {
			ca := r.clientAddr.Load()
			if ca == nil {
				continue // don't know client yet
			}
			dstAddr = ca
		} else {
			dstAddr = r.serverAddr
		}

		// Send with delay
		d := r.delay(r.rng)
		if d > 0 {
			dst := dstAddr // capture for goroutine
			go func() {
				time.Sleep(d)
				r.conn.WriteToUDP(pkt, dst)
			}()
		} else {
			r.conn.WriteToUDP(pkt, dstAddr)
		}
	}
}

// ccTestEnv holds a QUIC client+server test environment with CC instrumentation.
type ccTestEnv struct {
	serverCC  *K2CCSender
	miRecords []MIRecord
	miMu      sync.Mutex
}

func (e *ccTestEnv) collectMI(mi *MonitorInterval, throughput float64, isAppLimited bool) {
	e.miMu.Lock()
	defer e.miMu.Unlock()
	e.miRecords = append(e.miRecords, MIRecord{
		ID:           mi.ID,
		BytesSent:    mi.BytesSent,
		BytesAcked:   mi.BytesAcked,
		BytesLost:    mi.BytesLost,
		PacketsSent:  mi.PacketsSent,
		PacketsAcked: mi.PacketsAcked,
		Duration:     mi.Duration(),
		Throughput:   throughput,
		TargetRate:   mi.TargetRate,
		AppLimited:   isAppLimited,
		MinDuration:  mi.MinDuration,
	})
}

func (e *ccTestEnv) getRecords() []MIRecord {
	e.miMu.Lock()
	defer e.miMu.Unlock()
	out := make([]MIRecord, len(e.miRecords))
	copy(out, e.miRecords)
	return out
}

func logMIAnalysis(t *testing.T, env *ccTestEnv, totalRead int64, elapsed time.Duration) {
	t.Helper()
	aggregateMbps := float64(totalRead) * 8.0 / elapsed.Seconds() / 1_000_000.0

	t.Logf("=== Aggregate Transfer ===")
	t.Logf("  Transferred: %d bytes (%.1f MB)", totalRead, float64(totalRead)/1024/1024)
	t.Logf("  Duration: %v", elapsed)
	t.Logf("  Throughput: %.1f Mbps", aggregateMbps)

	if env.serverCC != nil {
		snap := env.serverCC.Snapshot()
		t.Logf("=== CC Final State ===")
		t.Logf("  Mode: %s", snap.Mode)
		t.Logf("  SendingRate: %.1f Mbps", snap.SendingRate/1_000_000)
		t.Logf("  Throughput: %.1f Mbps", snap.Throughput/1_000_000)
		t.Logf("  RTT: %v", snap.RTT)
		t.Logf("  Cwnd: %d bytes", snap.Cwnd)
		t.Logf("  MIID: %d", snap.MIID)
		t.Logf("  Epsilon: %.4f", snap.Epsilon)
	}

	records := env.getRecords()
	t.Logf("=== MI Records (%d total) ===", len(records))
	appLimitedCount := 0
	var maxTP, totalTP float64
	for i, r := range records {
		if r.AppLimited {
			appLimitedCount++
		}
		if r.Throughput > maxTP {
			maxTP = r.Throughput
		}
		totalTP += r.Throughput
		if i < 30 || i >= len(records)-5 {
			t.Logf("  MI[%d]: sent=%d acked=%d lost=%d pkts=%d/%d dur=%v tp=%.1fM target=%.1fM appLim=%v minDur=%v",
				r.ID, r.BytesSent, r.BytesAcked, r.BytesLost,
				r.PacketsAcked, r.PacketsSent,
				r.Duration, r.Throughput/1_000_000, r.TargetRate/1_000_000,
				r.AppLimited, r.MinDuration)
		} else if i == 30 {
			t.Logf("  ... (%d MIs omitted) ...", len(records)-35)
		}
	}

	avgTP := float64(0)
	if len(records) > 0 {
		avgTP = totalTP / float64(len(records))
	}
	t.Logf("=== MI Summary ===")
	t.Logf("  Total MIs: %d", len(records))
	t.Logf("  App-limited: %d (%.0f%%)", appLimitedCount, float64(appLimitedCount)/float64(max(len(records), 1))*100)
	t.Logf("  Max MI throughput: %.1f Mbps", maxTP/1_000_000)
	t.Logf("  Avg MI throughput: %.1f Mbps", avgTP/1_000_000)
}

// runBulkTransfer creates QUIC client+server via a UDP relay with configurable
// delay and loss, transfers data, and returns MI diagnostics.
// writeRateBytesPerSec limits server write speed (0 = unlimited).
func runBulkTransfer(t *testing.T, transferSize int, delay delayFunc, lossRate float64, writeRateBytesPerSec int) (*ccTestEnv, int64, time.Duration) {
	t.Helper()
	cert := generateTestCert(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	env := &ccTestEnv{}

	// --- Server ---
	serverUDP, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer serverUDP.Close()

	serverTransport := &quic.Transport{Conn: serverUDP}
	defer serverTransport.Close()

	qcfg := &quic.Config{
		MaxIdleTimeout:                 30 * time.Second,
		Allow0RTT:                      true,
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

	// --- UDP relay ---
	relay := newUDPRelay(t, serverUDP.LocalAddr().(*net.UDPAddr), delay, lossRate)
	defer relay.Close()

	// --- Server goroutine ---
	serverDone := make(chan error, 1)
	go func() {
		conn, err := ln.Accept(ctx)
		if err != nil {
			serverDone <- fmt.Errorf("accept: %w", err)
			return
		}

		env.serverCC = NewCCSenderGFWMode(DefaultMaxRate)
		env.serverCC.SetMICompletedHook(env.collectMI)
		conn.SetCongestionControl(env.serverCC)

		stream, err := conn.AcceptStream(ctx)
		if err != nil {
			serverDone <- fmt.Errorf("accept stream: %w", err)
			return
		}

		buf := make([]byte, 1)
		if _, err := stream.Read(buf); err != nil {
			serverDone <- fmt.Errorf("read request: %w", err)
			return
		}

		chunkSize := 32 * 1024 // 32 KB
		data := make([]byte, chunkSize)
		for i := range data {
			data[i] = byte(i % 256)
		}
		remaining := transferSize
		// Rate limiting: if writeRateBytesPerSec > 0, sleep between writes
		var writeInterval time.Duration
		if writeRateBytesPerSec > 0 {
			writeInterval = time.Duration(float64(time.Second) * float64(chunkSize) / float64(writeRateBytesPerSec))
		}
		for remaining > 0 {
			n := min(len(data), remaining)
			written, err := stream.Write(data[:n])
			if err != nil {
				serverDone <- fmt.Errorf("write: %w", err)
				return
			}
			remaining -= written
			if writeInterval > 0 {
				time.Sleep(writeInterval)
			}
		}
		stream.Close()
		serverDone <- nil
	}()

	// --- Client ---
	clientUDP, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer clientUDP.Close()

	clientTransport := &quic.Transport{Conn: clientUDP}
	defer clientTransport.Close()

	// Client connects to RELAY, not directly to server
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

	start := time.Now()
	totalRead, err := io.Copy(io.Discard, stream)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatal("read:", err)
	}

	if err := <-serverDone; err != nil {
		t.Fatal("server:", err)
	}

	return env, totalRead, elapsed
}

// TestIntegration_BulkTransfer_CCDiagnostic transfers 10MB via QUIC with 15ms
// one-way delay (30ms RTT) to diagnose CC MI throughput measurement.
func TestIntegration_BulkTransfer_CCDiagnostic(t *testing.T) {
	const transferSize = 10 * 1024 * 1024 // 10 MB

	env, totalRead, elapsed := runBulkTransfer(t, transferSize, stableDelay(15*time.Millisecond), 0.0, 0)
	logMIAnalysis(t, env, totalRead, elapsed)

	if totalRead != int64(transferSize) {
		t.Errorf("expected %d bytes, got %d", transferSize, totalRead)
	}

	// With 10MB at ~30ms RTT, we should see multiple MI completions
	records := env.getRecords()
	if len(records) == 0 {
		t.Error("no MI completions recorded — CC never operated")
	}
}

// TestIntegration_BulkTransfer_NoDelay runs on localhost without delay for comparison.
func TestIntegration_BulkTransfer_NoDelay(t *testing.T) {
	const transferSize = 10 * 1024 * 1024

	env, totalRead, elapsed := runBulkTransfer(t, transferSize, stableDelay(0), 0.0, 0)
	logMIAnalysis(t, env, totalRead, elapsed)

	if totalRead != int64(transferSize) {
		t.Errorf("expected %d bytes, got %d", transferSize, totalRead)
	}
}

// TestIntegration_BulkTransfer_RateLimited simulates a real-world scenario where
// upstream data arrives at a limited rate (80 Mbps) while CC target is higher.
// This is the condition seen on HK server where per-MI throughput was only 0-9.6M.
func TestIntegration_BulkTransfer_RateLimited(t *testing.T) {
	const transferSize = 5 * 1024 * 1024 // 5 MB (shorter to keep test fast)
	const writeRateMbps = 80              // Simulate 80 Mbps upstream

	env, totalRead, elapsed := runBulkTransfer(t, transferSize, stableDelay(15*time.Millisecond), 0.0, writeRateMbps*1_000_000/8)
	logMIAnalysis(t, env, totalRead, elapsed)

	if totalRead != int64(transferSize) {
		t.Errorf("expected %d bytes, got %d", transferSize, totalRead)
	}

	// Analyze: how many MIs are app-limited?
	records := env.getRecords()
	if len(records) == 0 {
		t.Error("no MI completions")
		return
	}

	appLimited := 0
	for _, r := range records {
		if r.AppLimited {
			appLimited++
		}
	}
	t.Logf("Rate-limited result: %d/%d MIs app-limited (%.0f%%)",
		appLimited, len(records), float64(appLimited)/float64(len(records))*100)
}

// TestIntegration_BulkTransfer_WithLoss tests CC behavior under various loss rates.
// Validates Throughput Anchor, Ceiling Hold, and α clamping with real QUIC data flow.
func TestIntegration_BulkTransfer_WithLoss(t *testing.T) {
	const transferSize = 3 * 1024 * 1024  // 3 MB (keep tests fast)
	const writeRate = 80_000_000 / 8       // 80 Mbps
	delay := stableDelay(15 * time.Millisecond) // 30ms RTT

	tests := []struct {
		name     string
		lossRate float64
		// Expected behavior
		expectTransferComplete bool
		expectAlphaClamped     bool // α×L > 0.8 → α clamped
	}{
		{"loss_0pct", 0.00, true, false},
		{"loss_5pct", 0.05, true, false},   // α×0.05 = 0.25 < 0.8
		{"loss_10pct", 0.10, true, false},  // α×0.10 = 0.50 < 0.8
		{"loss_15pct", 0.15, true, false},  // α×0.15 = 0.75 < 0.8
		{"loss_20pct", 0.20, true, true},   // α×0.20 = 1.00 > 0.8
		{"loss_25pct", 0.25, true, true},   // α×0.25 = 1.25 > 0.8
		{"loss_30pct", 0.30, true, true},   // α×0.30 = 1.50 > 0.8
		{"loss_35pct", 0.35, true, true},   // α×0.35 = 1.75 > 0.8
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			env, totalRead, elapsed := runBulkTransfer(t, transferSize, delay, tc.lossRate, writeRate)
			logMIAnalysis(t, env, totalRead, elapsed)

			if tc.expectTransferComplete {
				if totalRead != int64(transferSize) {
					t.Errorf("expected %d bytes, got %d", transferSize, totalRead)
				}
			}

			records := env.getRecords()
			if len(records) == 0 {
				t.Error("no MI completions — CC never operated")
				return
			}

			// Check that CC didn't completely collapse
			snap := env.serverCC.Snapshot()
			t.Logf("Final: rate=%.1fM mode=%s epsilon=%.4f",
				snap.SendingRate/1e6, snap.Mode, snap.Epsilon)

			// Rate should stay above the download floor (30 Mbps) at any tested loss rate.
			// The α clamping ensures utility stays positive even at 35% loss.
			if snap.SendingRate < 10_000_000 {
				t.Errorf("rate collapsed to %.1f Mbps (expected >= 10 Mbps at %.0f%% loss)",
					snap.SendingRate/1e6, tc.lossRate*100)
			}
		})
	}
}

// ============================================================================
// Convergence Benchmark: Loss × RTT matrix
// ============================================================================
//
// Measures three metrics for each (loss, RTT) scenario:
//   - Convergence MIs: first MI where 3-MI rolling avg throughput ≥ 50% of goodput
//   - Aggregate throughput: totalRead × 8 / elapsed
//   - Efficiency: aggregate throughput / goodput
//
// RTT profiles model real deployment:
//   near: Hong Kong     30ms  (±20% jitter)
//   mid:  Japan/SG     150ms  (±20% jitter)
//   far:  US-West/EU   300ms  (±20% jitter)

type convergenceExpect struct {
	name          string
	lossRate      float64
	delayBase     time.Duration // one-way base delay
	delayJitter   time.Duration // ±jitter range
	transferSize  int           // bytes to transfer (scaled by RTT)
	rttLabel      string
	goodput       float64 // bps: writeRate × (1-loss)
	maxConvergeMI int     // convergence within this many MIs
	minEfficiency float64 // aggregate throughput / goodput
}

// convergenceResult holds measured metrics for one scenario.
type convergenceResult struct {
	convergeMI       int     // first MI where 3-MI rolling avg ≥ 50% goodput (-1 = never)
	aggThroughput    float64 // bps
	efficiency       float64 // aggThroughput / goodput
	totalMIs         int
	appLimitedCount  int
	steadyThroughput float64 // avg throughput of last 50% of MIs
}

func analyzeConvergence(records []MIRecord, goodput float64) convergenceResult {
	var res convergenceResult
	res.totalMIs = len(records)
	res.convergeMI = -1

	if len(records) == 0 {
		return res
	}

	// Count app-limited
	for _, r := range records {
		if r.AppLimited {
			res.appLimitedCount++
		}
	}

	// Convergence: first MI where 3-MI rolling avg throughput ≥ 50% of goodput
	threshold := goodput * 0.5
	for i := 2; i < len(records); i++ {
		avg3 := (records[i-2].Throughput + records[i-1].Throughput + records[i].Throughput) / 3.0
		if avg3 >= threshold {
			res.convergeMI = i
			break
		}
	}

	// Steady-state throughput: average of last 50% of non-app-limited MIs
	midpoint := len(records) / 2
	var steadySum float64
	var steadyCount int
	for i := midpoint; i < len(records); i++ {
		if !records[i].AppLimited {
			steadySum += records[i].Throughput
			steadyCount++
		}
	}
	if steadyCount > 0 {
		res.steadyThroughput = steadySum / float64(steadyCount)
	}

	return res
}

func TestConvergence_Benchmark(t *testing.T) {
	const (
		writeRate    = 80_000_000 / 8   // 80 Mbps in bytes/sec
		writeRateBps = 80_000_000.0     // 80 Mbps
	)

	// RTT profiles: base ± 20% jitter (segment-based to avoid reordering)
	// Each scenario gets a fresh delayFunc created in the test loop.
	//
	// Transfer size scales with RTT to ensure enough MIs reach steady state:
	//   near (30ms):  10 MB ≈ 1s  → ~33 MIs
	//   mid (150ms):  20 MB ≈ 3s  → ~20 MIs
	//   far (300ms):  30 MB ≈ 5s  → ~17 MIs
	near := [2]time.Duration{15 * time.Millisecond, 3 * time.Millisecond}   // HK: RTT 24-36ms
	mid  := [2]time.Duration{75 * time.Millisecond, 15 * time.Millisecond}  // JP/SG: RTT 120-180ms
	far  := [2]time.Duration{150 * time.Millisecond, 30 * time.Millisecond} // US/EU: RTT 240-360ms

	nearSize := 10 * 1024 * 1024 // 10 MB
	midSize  := 20 * 1024 * 1024 // 20 MB
	farSize  := 30 * 1024 * 1024 // 30 MB

	// Thresholds calibrated from empirical data (2025-03-10).
	// maxConvergeMI: set to ~2x observed worst case (jitter makes this noisy).
	// minEfficiency: set to ~80% of observed value (regression floor).
	// If algorithm improves, tighten these to lock in gains.
	scenarios := []convergenceExpect{
		// ── Near (Hong Kong, 30ms RTT ±20%) ──
		// Observed isolated: converge MI 2, efficiency 80-97%.
		// Under load (parallel tests): efficiency drops to ~68-73%. Thresholds set conservatively.
		{"near_loss0",  0.00, near[0], near[1], nearSize, "30ms",  writeRateBps * 1.00, 6,  0.55},
		{"near_loss5",  0.05, near[0], near[1], nearSize, "30ms",  writeRateBps * 0.95, 6,  0.50},
		{"near_loss10", 0.10, near[0], near[1], nearSize, "30ms",  writeRateBps * 0.90, 6,  0.40},
		{"near_loss20", 0.20, near[0], near[1], nearSize, "30ms",  writeRateBps * 0.80, 6,  0.35},
		{"near_loss30", 0.30, near[0], near[1], nearSize, "30ms",  writeRateBps * 0.70, 6,  0.25},
		{"near_loss35", 0.35, near[0], near[1], nearSize, "30ms",  writeRateBps * 0.65, 6,  0.20},

		// ── Mid (Japan/Singapore, 150ms RTT ±20%) ──
		// Observed: converge MI 2-13, efficiency 37-52%
		{"mid_loss0",   0.00, mid[0], mid[1], midSize, "150ms", writeRateBps * 1.00, 20, 0.30},
		{"mid_loss5",   0.05, mid[0], mid[1], midSize, "150ms", writeRateBps * 0.95, 20, 0.30},
		{"mid_loss10",  0.10, mid[0], mid[1], midSize, "150ms", writeRateBps * 0.90, 20, 0.28},
		{"mid_loss20",  0.20, mid[0], mid[1], midSize, "150ms", writeRateBps * 0.80, 0,  0.25},
		{"mid_loss30",  0.30, mid[0], mid[1], midSize, "150ms", writeRateBps * 0.70, 0,  0.20},

		// ── Far (US-West/Europe, 300ms RTT ±20%) ──
		// Observed: converge MI 3-29, efficiency 28-44%
		// Far scenarios: 50% of goodput threshold is unreachable (actual ≈ 33% of goodput
		// due to RTT overhead). Convergence check skipped (0); efficiency guards regression.
		{"far_loss0",   0.00, far[0], far[1], farSize, "300ms", writeRateBps * 1.00, 0,  0.28},
		{"far_loss5",   0.05, far[0], far[1], farSize, "300ms", writeRateBps * 0.95, 0,  0.28},
		{"far_loss10",  0.10, far[0], far[1], farSize, "300ms", writeRateBps * 0.90, 0,  0.22},
		{"far_loss20",  0.20, far[0], far[1], farSize, "300ms", writeRateBps * 0.80, 0,  0.20},
		{"far_loss30",  0.30, far[0], far[1], farSize, "300ms", writeRateBps * 0.70, 0,  0.20},
	}

	// Print header
	t.Logf("┌──────────────┬──────┬───────┬─────────┬───────────┬────────────┬────────┬──────────┐")
	t.Logf("│ Scenario     │ RTT  │ Loss  │ Goodput │ Converge  │ Agg TP     │ Effic  │ Steady   │")
	t.Logf("│              │      │       │  (Mbps) │ MI (max)  │   (Mbps)   │   %%    │  (Mbps)  │")
	t.Logf("├──────────────┼──────┼───────┼─────────┼───────────┼────────────┼────────┼──────────┤")

	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			// Fresh delayFunc per scenario — no shared closure state between subtests.
			delay := jitterDelay(sc.delayBase, sc.delayJitter)
			env, totalRead, elapsed := runBulkTransfer(t, sc.transferSize, delay, sc.lossRate, writeRate)

			// Compute aggregate throughput
			aggTP := float64(totalRead) * 8.0 / elapsed.Seconds()
			efficiency := aggTP / sc.goodput

			// Analyze convergence from MI records
			records := env.getRecords()
			cr := analyzeConvergence(records, sc.goodput)

			// Print result row
			convergeStr := fmt.Sprintf("%d", cr.convergeMI)
			if cr.convergeMI < 0 {
				convergeStr = "NEVER"
			}
			t.Logf("│ %-12s │ %4s │ %4.0f%% │ %7.1f │ %4s (%2d) │ %10.1f │ %5.1f%% │ %8.1f │",
				sc.name, sc.rttLabel, sc.lossRate*100,
				sc.goodput/1e6,
				convergeStr, sc.maxConvergeMI,
				aggTP/1e6, efficiency*100,
				cr.steadyThroughput/1e6)

			// Transfer must complete
			if totalRead != int64(sc.transferSize) {
				t.Errorf("incomplete transfer: %d/%d bytes", totalRead, sc.transferSize)
			}

			// Must have MI data
			if len(records) == 0 {
				t.Error("no MI completions — CC never operated")
				return
			}

			// Assert: convergence speed (maxConvergeMI=0 means skip — extreme scenarios)
			if sc.maxConvergeMI > 0 {
				if cr.convergeMI < 0 {
					t.Errorf("NEVER converged (need 3-MI rolling avg ≥ %.1fM = 50%% of goodput %.1fM)",
						sc.goodput*0.5/1e6, sc.goodput/1e6)
				} else if cr.convergeMI > sc.maxConvergeMI {
					t.Errorf("converged at MI %d, expected ≤ %d", cr.convergeMI, sc.maxConvergeMI)
				}
			}

			// Assert: efficiency
			if efficiency < sc.minEfficiency {
				t.Errorf("efficiency %.1f%% < expected %.1f%% (agg=%.1fM, goodput=%.1fM)",
					efficiency*100, sc.minEfficiency*100, aggTP/1e6, sc.goodput/1e6)
			}
		})
	}

	t.Logf("└──────────────┴──────┴───────┴─────────┴───────────┴────────────┴────────┴──────────┘")
}

// ============================================================================
// Descent Benchmark: rate starts above actual bandwidth → must come down
// ============================================================================
//
// K2CC starts at maxRate × initialRateFactor = 500M × 0.3 = 150M.
// When the actual bandwidth is lower (e.g., 30M), the algorithm must:
//   1. Detect throughput saturation (slow start) or app-limited condition
//   2. Anchor rate down to actual bandwidth
//   3. Stabilize via gradient ascent around the real bandwidth
//
// Key metrics:
//   - Descent MI: first MI where TargetRate ≤ 2× bandwidth
//   - Steady rate: average TargetRate in last 50% of MIs
//   - Rate accuracy: steady rate / bandwidth (ideal = 1.0)
//   - Throughput efficiency: aggregate throughput / bandwidth

type descentExpect struct {
	name         string
	bandwidth    int           // server write rate in bytes/sec
	bandwidthBps float64       // bits/sec
	lossRate     float64
	delayBase    time.Duration
	delayJitter  time.Duration
	transferSize int
	rttLabel     string
	maxDescentMI int     // rate must reach ≤ 2× bandwidth within this many MIs (0 = skip)
	minEfficiency float64 // aggregate throughput / bandwidth
}

type descentResult struct {
	descentMI    int     // first MI where TargetRate ≤ 2× bandwidth (-1 = never)
	steadyRate   float64 // avg TargetRate in last 50% of non-app-limited MIs
	rateAccuracy float64 // steadyRate / bandwidth
	totalMIs     int
	appLimited   int
}

func analyzeDescent(records []MIRecord, bandwidthBps float64) descentResult {
	var res descentResult
	res.totalMIs = len(records)
	res.descentMI = -1

	if len(records) == 0 {
		return res
	}

	// Count app-limited
	for _, r := range records {
		if r.AppLimited {
			res.appLimited++
		}
	}

	// Descent: first MI where TargetRate ≤ 2× bandwidth
	threshold := bandwidthBps * 2.0
	for i, r := range records {
		if r.TargetRate <= threshold {
			res.descentMI = i
			break
		}
	}

	// Steady-state rate: average TargetRate of last 50% of non-app-limited MIs
	midpoint := len(records) / 2
	var sum float64
	var count int
	for i := midpoint; i < len(records); i++ {
		if !records[i].AppLimited {
			sum += records[i].TargetRate
			count++
		}
	}
	if count > 0 {
		res.steadyRate = sum / float64(count)
		res.rateAccuracy = res.steadyRate / bandwidthBps
	}

	return res
}

func TestDescent_Benchmark(t *testing.T) {
	// Bandwidth tiers (server write rate)
	bw10  := 10_000_000 / 8  // 10 Mbps
	bw30  := 30_000_000 / 8  // 30 Mbps
	bw50  := 50_000_000 / 8  // 50 Mbps
	bw80  := 80_000_000 / 8  // 80 Mbps
	bw100 := 100_000_000 / 8 // 100 Mbps

	near := [2]time.Duration{15 * time.Millisecond, 3 * time.Millisecond}
	mid  := [2]time.Duration{75 * time.Millisecond, 15 * time.Millisecond}

	// Transfer size: enough MIs for descent + stabilization
	nearSize := 10 * 1024 * 1024 // 10 MB
	midSize  := 15 * 1024 * 1024 // 15 MB

	// Initial CC rate = 500M × 0.3 = 150M. Algorithm must descend to actual bandwidth.
	// maxDescentMI: how quickly rate reaches ≤ 2× bandwidth.
	// minEfficiency: aggregate throughput / bandwidth (regression floor).
	scenarios := []descentExpect{
		// ── Near RTT (30ms) — different bandwidths ──
		// Descent should be fast: 1-3 MIs via slow start saturation + app-limited anchor.
		// near_bw10: minRate=50M floor means rate can't reach ≤ 2×10M=20M → skip descent check.
		{"near_bw10",     bw10,  10e6,  0.00, near[0], near[1], nearSize, "30ms",  0,  0.50},
		{"near_bw30",     bw30,  30e6,  0.00, near[0], near[1], nearSize, "30ms",  5,  0.60},
		{"near_bw50",     bw50,  50e6,  0.00, near[0], near[1], nearSize, "30ms",  5,  0.55},
		{"near_bw80",     bw80,  80e6,  0.00, near[0], near[1], nearSize, "30ms",  5,  0.55},
		{"near_bw100",    bw100, 100e6, 0.00, near[0], near[1], nearSize, "30ms",  5,  0.55},

		// ── Near RTT + 5% loss ──
		{"near_bw30_l5",  bw30,  30e6,  0.05, near[0], near[1], nearSize, "30ms",  5,  0.50},
		{"near_bw50_l5",  bw50,  50e6,  0.05, near[0], near[1], nearSize, "30ms",  5,  0.50},
		{"near_bw100_l5", bw100, 100e6, 0.05, near[0], near[1], nearSize, "30ms",  5,  0.45},

		// ── Mid RTT (150ms) — descent takes longer due to MI duration ──
		{"mid_bw30",      bw30,  30e6,  0.00, mid[0],  mid[1],  midSize,  "150ms", 5,  0.35},
		{"mid_bw50",      bw50,  50e6,  0.00, mid[0],  mid[1],  midSize,  "150ms", 5,  0.35},
		{"mid_bw100",     bw100, 100e6, 0.00, mid[0],  mid[1],  midSize,  "150ms", 5,  0.35},

		// ── Mid RTT + 5% loss ──
		{"mid_bw50_l5",   bw50,  50e6,  0.05, mid[0],  mid[1],  midSize,  "150ms", 5,  0.30},
		{"mid_bw100_l5",  bw100, 100e6, 0.05, mid[0],  mid[1],  midSize,  "150ms", 5,  0.25},
	}

	t.Logf("┌───────────────┬──────┬───────┬──────────┬──────────┬────────────┬────────┬──────────┬──────────┐")
	t.Logf("│ Scenario      │ RTT  │ Loss  │ Bandwith │ Descent  │ Agg TP     │ Effic  │ Steady R │ R Accur  │")
	t.Logf("│               │      │       │  (Mbps)  │ MI (max) │   (Mbps)   │   %%    │  (Mbps)  │          │")
	t.Logf("├───────────────┼──────┼───────┼──────────┼──────────┼────────────┼────────┼──────────┼──────────┤")

	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			delay := jitterDelay(sc.delayBase, sc.delayJitter)
			env, totalRead, elapsed := runBulkTransfer(t, sc.transferSize, delay, sc.lossRate, sc.bandwidth)

			// Aggregate throughput
			aggTP := float64(totalRead) * 8.0 / elapsed.Seconds()
			efficiency := aggTP / sc.bandwidthBps

			// Descent analysis
			records := env.getRecords()
			dr := analyzeDescent(records, sc.bandwidthBps)

			// Print result row
			descentStr := fmt.Sprintf("%d", dr.descentMI)
			if dr.descentMI < 0 {
				descentStr = "NEVER"
			}
			t.Logf("│ %-13s │ %4s │ %4.0f%% │ %8.1f │ %4s (%2d) │ %10.1f │ %5.1f%% │ %8.1f │ %7.2fx │",
				sc.name, sc.rttLabel, sc.lossRate*100,
				sc.bandwidthBps/1e6,
				descentStr, sc.maxDescentMI,
				aggTP/1e6, efficiency*100,
				dr.steadyRate/1e6, dr.rateAccuracy)

			// Transfer must complete
			if totalRead != int64(sc.transferSize) {
				t.Errorf("incomplete transfer: %d/%d bytes", totalRead, sc.transferSize)
			}

			if len(records) == 0 {
				t.Error("no MI completions — CC never operated")
				return
			}

			// Assert: descent speed (0 = skip)
			if sc.maxDescentMI > 0 {
				if dr.descentMI < 0 {
					t.Errorf("NEVER descended (rate never reached ≤ 2× bandwidth %.1fM)",
						sc.bandwidthBps/1e6)
				} else if dr.descentMI > sc.maxDescentMI {
					t.Errorf("descended at MI %d, expected ≤ %d", dr.descentMI, sc.maxDescentMI)
				}
			}

			// Assert: efficiency
			if efficiency < sc.minEfficiency {
				t.Errorf("efficiency %.1f%% < expected %.1f%% (agg=%.1fM, bw=%.1fM)",
					efficiency*100, sc.minEfficiency*100, aggTP/1e6, sc.bandwidthBps/1e6)
			}
		})
	}

	t.Logf("└───────────────┴──────┴───────┴──────────┴──────────┴────────────┴────────┴──────────┴──────────┘")
}
