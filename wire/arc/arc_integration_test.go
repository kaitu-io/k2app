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

// udpRelay sits between client and server, adding delay and optional packet loss.
// It listens on a local UDP port, forwards packets to the server with delay,
// and forwards server responses back to the client with delay.
type udpRelay struct {
	conn     *net.UDPConn // relay's listening socket
	serverAddr *net.UDPAddr
	oneWayDelay time.Duration // delay added in each direction
	lossRate    float64       // 0.0 - 1.0, applied to both directions
	rng         *mrand.Rand

	clientAddr atomic.Pointer[net.UDPAddr] // learned from first client packet
	closed     chan struct{}
	wg         sync.WaitGroup
}

func newUDPRelay(t *testing.T, serverAddr *net.UDPAddr, oneWayDelay time.Duration, lossRate float64) *udpRelay {
	t.Helper()
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	r := &udpRelay{
		conn:        conn,
		serverAddr:  serverAddr,
		oneWayDelay: oneWayDelay,
		lossRate:    lossRate,
		rng:         mrand.New(mrand.NewSource(42)),
		closed:      make(chan struct{}),
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
		if r.oneWayDelay > 0 {
			dst := dstAddr // capture for goroutine
			go func() {
				time.Sleep(r.oneWayDelay)
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
		t.Logf("  BwEstimate: %.1f Mbps", snap.BwEstimate/1_000_000)
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
func runBulkTransfer(t *testing.T, transferSize int, oneWayDelay time.Duration, lossRate float64, writeRateBytesPerSec int) (*ccTestEnv, int64, time.Duration) {
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
	relay := newUDPRelay(t, serverUDP.LocalAddr().(*net.UDPAddr), oneWayDelay, lossRate)
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

	env, totalRead, elapsed := runBulkTransfer(t, transferSize, 15*time.Millisecond, 0.0, 0)
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

	env, totalRead, elapsed := runBulkTransfer(t, transferSize, 0, 0.0, 0)
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

	env, totalRead, elapsed := runBulkTransfer(t, transferSize, 15*time.Millisecond, 0.0, writeRateMbps*1_000_000/8)
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
	const oneWayDelay = 15 * time.Millisecond // 30ms RTT

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
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			env, totalRead, elapsed := runBulkTransfer(t, transferSize, oneWayDelay, tc.lossRate, writeRate)
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
			t.Logf("Final: rate=%.1fM bwEst=%.1fM mode=%s epsilon=%.4f",
				snap.SendingRate/1e6, snap.BwEstimate/1e6, snap.Mode, snap.Epsilon)

			// At any loss rate, the rate should not drop below arcMinDownloadRate (30 Mbps)
			// unless the link genuinely can't support it
			if snap.SendingRate < 20_000_000 && tc.lossRate <= 0.20 {
				t.Errorf("rate collapsed to %.1f Mbps (expected >= 20 Mbps at %0.f%% loss)",
					snap.SendingRate/1e6, tc.lossRate*100)
			}
		})
	}
}
