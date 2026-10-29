package k2cc

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	tls "github.com/metacubex/utls"
	"crypto/x509"
	"fmt"
	"io"
	"math/big"
	mrand "math/rand"
	"net"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kaitu-io/kuic"
)

// median returns the median of a float64 slice. Returns 0 for empty input.
func median(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := make([]float64, len(values))
	copy(sorted, values)
	slices.Sort(sorted)
	n := len(sorted)
	if n%2 == 0 {
		return (sorted[n/2-1] + sorted[n/2]) / 2
	}
	return sorted[n/2]
}

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
// ~100ms, but packets within the same segment get the same delay, producing
// realistic RTT variation over time.
//
// Segments alone do NOT prevent reordering: when the delay steps DOWN at a
// segment boundary, a per-packet-timer relay lets the first packets of the new
// segment overtake the tail of the old one. Order is preserved by the FIFO
// relay (monotonic release times, see newUDPRelayFIFO), where a downward step
// shows up as a short delivery burst instead.
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
//
// Optional bandwidth cap (bandwidthCapBps > 0) simulates a network bottleneck
// using a token bucket. Excess packets are tail-dropped, creating congestion-induced
// loss that increases with sending rate — unlike random lossRate which is constant.
type udpRelay struct {
	conn       *net.UDPConn  // relay's listening socket
	serverAddr *net.UDPAddr
	delay      delayFunc     // per-packet delay function
	lossRate   float64       // 0.0 - 1.0, applied to both directions
	rng        *mrand.Rand

	// Token bucket for bandwidth cap (server→client direction only).
	// Zero = unlimited. Tokens refill at bandwidthCapBps/8 bytes/sec.
	bandwidthCapBps float64
	tokens          float64
	maxTokens       float64
	lastRefill      time.Time
	tokenMu         sync.Mutex

	clientAddr atomic.Pointer[net.UDPAddr] // learned from first client packet
	closed     chan struct{}

	// Ground-truth counters (datagrams). s2c = server→client (the data
	// direction in bulk tests), c2s = client→server (mostly ACKs).
	s2cForwarded, s2cDropped atomic.Uint64
	c2sForwarded, c2sDropped atomic.Uint64

	// Order-preserving delivery (newUDPRelayFIFO): [0]=s2c, [1]=c2s.
	fifoQ [2]chan delayedPkt
	wg    sync.WaitGroup

	// Delivery-order ground truth, per direction ([0]=s2c, [1]=c2s). Every
	// forwarded datagram gets its direction's next sequence number on receive
	// (relayLoop only) and reports it just before it is written out; a
	// datagram written after a higher-numbered one counts as out of order.
	// Must stay 0 on the FIFO relay — runBulkTransferFull asserts it — and is
	// non-zero on the per-packet relay, which is what makes it a discriminator.
	nextSeq       [2]uint64
	lastDelivered [2]atomic.Uint64
	outOfOrder    [2]atomic.Uint64

	// FIFO only: last scheduled release time per direction (relayLoop only),
	// and how many datagrams had their release pushed back to it because the
	// delay function stepped down (jitterDelay segment boundary).
	lastRelease [2]time.Time
	held        [2]atomic.Uint64
}

const (
	dirS2C = 0
	dirC2S = 1
)

// noteDelivered records that the datagram with per-direction sequence seq is
// about to be written. Safe for concurrent use (per-packet relay goroutines).
func (r *udpRelay) noteDelivered(dir int, seq uint64) {
	for {
		last := r.lastDelivered[dir].Load()
		if seq < last {
			r.outOfOrder[dir].Add(1)
			return
		}
		if r.lastDelivered[dir].CompareAndSwap(last, seq) {
			return
		}
	}
}

// newUDPRelay is the REORDERING relay (one goroutine+timer per packet). It is
// kept only so TestIntegration_LossAttributionAccuracy's reorder_* scenarios
// and TestRelayFIFO_NoReorder_NoSpuriousLoss's control can show the artifact.
// Benchmarks must use the FIFO relay (runBulkTransfer* do).
func newUDPRelay(t *testing.T, serverAddr *net.UDPAddr, delay delayFunc, lossRate float64) *udpRelay {
	t.Helper()
	return newUDPRelayOpts(t, serverAddr, delay, lossRate, 0, false)
}

// newUDPRelayFIFO is newUDPRelay with order-preserving delivery: each
// direction is a single FIFO queue released at receive-time + delay, instead
// of one goroutine+timer per packet. The per-packet mode reorders packets
// under scheduler pressure (timers created microseconds apart fire in
// arbitrary goroutine order), and QUIC's packet-threshold (3) loss detection
// turns that reordering into spurious loss — see
// TestIntegration_LossAttributionAccuracy.
//
// Order is preserved for ANY delay function, including jitterDelay: release
// times are forced monotonic per direction (releaseAt = max(receive+delay,
// previous releaseAt)), so when the delay steps down a packet waits behind
// its predecessor instead of overtaking it. The effective delay of such a
// packet is therefore larger than what the delay function returned; `held`
// counts them.
func newUDPRelayFIFO(t *testing.T, serverAddr *net.UDPAddr, delay delayFunc, lossRate float64) *udpRelay {
	t.Helper()
	return newUDPRelayOpts(t, serverAddr, delay, lossRate, 0, true)
}

// newUDPRelayFIFOWithBandwidth is newUDPRelayFIFO plus the server→client
// token-bucket bandwidth cap. This is the relay every benchmark uses.
func newUDPRelayFIFOWithBandwidth(t *testing.T, serverAddr *net.UDPAddr, delay delayFunc, lossRate float64, bandwidthCapBps float64) *udpRelay {
	t.Helper()
	return newUDPRelayOpts(t, serverAddr, delay, lossRate, bandwidthCapBps, true)
}

type delayedPkt struct {
	data      []byte
	dst       *net.UDPAddr
	releaseAt time.Time
	dir       int
	seq       uint64
}

func newUDPRelayOpts(t *testing.T, serverAddr *net.UDPAddr, delay delayFunc, lossRate float64, bandwidthCapBps float64, fifo bool) *udpRelay {
	t.Helper()
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	// Buffer = 20ms worth of data (small queue, realistic for internet links)
	var maxTokens float64
	if bandwidthCapBps > 0 {
		maxTokens = bandwidthCapBps / 8 * 0.020 // 20ms buffer
	}
	r := &udpRelay{
		conn:            conn,
		serverAddr:      serverAddr,
		delay:           delay,
		lossRate:        lossRate,
		rng:             mrand.New(mrand.NewSource(42)),
		closed:          make(chan struct{}),
		bandwidthCapBps: bandwidthCapBps,
		tokens:          maxTokens, // start full
		maxTokens:       maxTokens,
		lastRefill:      time.Now(),
	}
	if fifo {
		r.fifoQ = [2]chan delayedPkt{make(chan delayedPkt, 1<<16), make(chan delayedPkt, 1<<16)}
		for _, q := range r.fifoQ {
			r.wg.Add(1)
			go r.fifoLoop(q)
		}
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

		// Apply random loss (both directions)
		if r.lossRate > 0 && r.rng.Float64() < r.lossRate {
			r.countDrop(isFromServer)
			continue // drop
		}

		// Apply bandwidth cap (server→client direction only — download bottleneck).
		// Token bucket: refill at bandwidthCapBps, consume packet size, drop if empty.
		if isFromServer && r.bandwidthCapBps > 0 {
			r.tokenMu.Lock()
			now := time.Now()
			elapsed := now.Sub(r.lastRefill).Seconds()
			r.lastRefill = now
			r.tokens += r.bandwidthCapBps / 8 * elapsed
			if r.tokens > r.maxTokens {
				r.tokens = r.maxTokens
			}
			if r.tokens < float64(n) {
				r.tokenMu.Unlock()
				r.countDrop(isFromServer)
				continue // tail-drop: congestion loss
			}
			r.tokens -= float64(n)
			r.tokenMu.Unlock()
		}

		if isFromServer {
			r.s2cForwarded.Add(1)
		} else {
			r.c2sForwarded.Add(1)
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

		dir := dirC2S
		if isFromServer {
			dir = dirS2C
		}
		r.nextSeq[dir]++
		seq := r.nextSeq[dir]

		// Send with delay — use timer+select so goroutines exit on close
		d := r.delay(r.rng)
		if r.fifoQ[0] != nil {
			// Monotonic release times: never schedule a packet before its
			// predecessor in the same direction (delay stepped down).
			releaseAt := time.Now().Add(d)
			if releaseAt.Before(r.lastRelease[dir]) {
				releaseAt = r.lastRelease[dir]
				r.held[dir].Add(1)
			}
			select {
			case r.fifoQ[dir] <- delayedPkt{data: pkt, dst: dstAddr, releaseAt: releaseAt, dir: dir, seq: seq}:
				r.lastRelease[dir] = releaseAt
			default: // queue overflow: reclassify forwarded → dropped, keep ground truth honest
				if isFromServer {
					r.s2cForwarded.Add(^uint64(0))
				} else {
					r.c2sForwarded.Add(^uint64(0))
				}
				r.countDrop(isFromServer)
			}
			continue
		}
		if d > 0 {
			dst := dstAddr // capture for goroutine
			r.wg.Add(1)
			go func() {
				defer r.wg.Done()
				t := time.NewTimer(d)
				defer t.Stop()
				select {
				case <-t.C:
					r.noteDelivered(dir, seq)
					r.conn.WriteToUDP(pkt, dst)
				case <-r.closed:
				}
			}()
		} else {
			r.noteDelivered(dir, seq)
			r.conn.WriteToUDP(pkt, dstAddr)
		}
	}
}

func (r *udpRelay) fifoLoop(q chan delayedPkt) {
	defer r.wg.Done()
	for {
		select {
		case <-r.closed:
			return
		case p := <-q:
			if d := time.Until(p.releaseAt); d > 0 {
				t := time.NewTimer(d)
				select {
				case <-t.C:
				case <-r.closed:
					t.Stop()
					return
				}
			}
			r.noteDelivered(p.dir, p.seq)
			r.conn.WriteToUDP(p.data, p.dst)
		}
	}
}

func (r *udpRelay) countDrop(isFromServer bool) {
	if isFromServer {
		r.s2cDropped.Add(1)
	} else {
		r.c2sDropped.Add(1)
	}
}

// ccTestEnv holds a QUIC client+server test environment with CC instrumentation.
type ccTestEnv struct {
	serverCC  *K2CCSender
	relay     *udpRelay // counters stay readable after the transfer (relay is closed)
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

// transferSizeForMIs computes the transfer size (bytes) needed to run at least
// minMIs Monitor Intervals. MI duration ≈ RTT, so transfer_time = minMIs × RTT.
// transfer_size = transfer_time × writeRate. Minimum 2MB to avoid degenerate tests.
func transferSizeForMIs(minMIs int, oneWayDelayMs int, writeRateBytesPerSec int) int {
	rttSec := float64(oneWayDelayMs) * 2.0 / 1000.0 // RTT ≈ 2 × one-way delay
	miDuration := rttSec * 2.0                       // MI MinDuration = 2×RTT
	transferTime := float64(minMIs) * miDuration
	size := int(transferTime * float64(writeRateBytesPerSec))
	if size < 4*1024*1024 {
		size = 4 * 1024 * 1024 // 4MB minimum (up from 2MB for 2×RTT MIs)
	}
	return size
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

// runBulkTransfer creates QUIC client+server via the order-preserving (FIFO)
// UDP relay with configurable delay and loss, transfers data, and returns MI
// diagnostics.
// writeRateBytesPerSec limits server write speed (0 = unlimited).
func runBulkTransfer(t *testing.T, transferSize int, delay delayFunc, lossRate float64, writeRateBytesPerSec int) (*ccTestEnv, int64, time.Duration) {
	t.Helper()
	return runBulkTransferFull(t, transferSize, delay, lossRate, writeRateBytesPerSec, 0)
}

// runBulkTransferFull is like runBulkTransfer but also accepts a network bandwidth
// cap (bps). When bandwidthCapBps > 0, the relay drops packets exceeding the cap
// using a token bucket — simulating a congested bottleneck link where loss increases
// with sending rate (unlike random lossRate which is constant).
//
// The relay is the FIFO one, and the helper fails the test if the relay
// delivered anything out of order: with the per-packet relay used until
// 2026-10-01, QUIC declared ~35–45 % spurious loss at 0 % configured loss, so
// every benchmark ran far above its nominal lossRate.
func runBulkTransferFull(t *testing.T, transferSize int, delay delayFunc, lossRate float64, writeRateBytesPerSec int, bandwidthCapBps float64) (*ccTestEnv, int64, time.Duration) {
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
	relay := newUDPRelayFIFOWithBandwidth(t, serverUDP.LocalAddr().(*net.UDPAddr), delay, lossRate, bandwidthCapBps)
	defer relay.Close()
	env.relay = relay
	defer func() {
		if s2c, c2s := relay.outOfOrder[dirS2C].Load(), relay.outOfOrder[dirC2S].Load(); s2c != 0 || c2s != 0 {
			t.Errorf("harness relay reordered packets (s2c=%d c2s=%d out of order) — results are polluted by spurious QUIC loss", s2c, c2s)
		}
	}()

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

	// PCC gradient ∝ 1 - L(1+α). With α=4.0, gradient turns negative at L > 1/(1+α) = 20%.
	// Above this boundary, PCC correctly descends to arcMinRate. BW-anchored mode can
	// sustain higher rates but depends on early bwEst quality (stochastic).
	// Below boundary: assert rate >= 3M (stable convergence expected).
	// Above boundary: assert rate >= bwAnchoredMinRate 1M (drain phase floor).
	tests := []struct {
		name     string
		lossRate float64
		// Expected behavior
		expectTransferComplete bool
		expectAlphaClamped     bool    // α×L > 0.8 → α clamped
		minRate                float64 // minimum acceptable instantaneous rate (bps)
	}{
		{"loss_0pct", 0.00, true, false, 3_000_000},
		{"loss_5pct", 0.05, true, false, 3_000_000},
		{"loss_10pct", 0.10, true, false, 3_000_000},
		{"loss_15pct", 0.15, true, false, 3_000_000},
		{"loss_20pct", 0.20, true, true, 3_000_000},   // boundary: L=1/(1+α), still stable
		{"loss_25pct", 0.25, true, true, 1_000_000},   // above boundary: rate may collapse
		{"loss_30pct", 0.30, true, true, 1_000_000},
		{"loss_35pct", 0.35, true, true, 1_000_000},
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

			// Check that CC didn't completely collapse.
			snap := env.serverCC.Snapshot()
			t.Logf("Final: rate=%.1fM mode=%s epsilon=%.4f",
				snap.SendingRate/1e6, snap.Mode, snap.Epsilon)

			if snap.SendingRate < tc.minRate {
				t.Errorf("rate collapsed to %.1f Mbps (expected >= %.0f Mbps at %.0f%% loss)",
					snap.SendingRate/1e6, tc.minRate/1e6, tc.lossRate*100)
			}

			// Phase 1 observability: the cumulative summary must agree with the
			// per-MI hook and with the injected loss.
			sum := env.serverCC.Summary()
			if sum.SenderID == 0 {
				t.Error("Summary().SenderID must be non-zero")
			}
			if sum.MIs != uint64(len(records)) {
				t.Errorf("Summary().MIs=%d want %d (one per MI hook call)", sum.MIs, len(records))
			}
			var lossN, tputN, rateN uint64
			for i := range sum.LossHist {
				lossN += sum.LossHist[i]
			}
			for i := range sum.TputHist {
				tputN += sum.TputHist[i]
				rateN += sum.RateHist[i]
			}
			if lossN != sum.MIs || tputN != sum.MIs || rateN != sum.MIs {
				t.Errorf("histogram totals (loss=%d tput=%d rate=%d) != MIs=%d", lossN, tputN, rateN, sum.MIs)
			}
			if sum.SlowStartMIs+sum.DecisionMIs+sum.RateAdjMIs != sum.MIs {
				t.Errorf("mode residency sum != MIs: %+v", sum)
			}
			// Byte totals must equal the per-MI hook's sums exactly — that is
			// the discriminator between "accumulator wrong" and "harness noise"
			// (this 3 MB, 4–7 MI transfer sees relay/burst loss well above the
			// injected rate even at 0%, so lossAgg is logged, not banded).
			var hookSent, hookAcked, hookLost uint64
			for _, r := range records {
				hookSent += r.BytesSent
				hookAcked += r.BytesAcked
				hookLost += r.BytesLost
			}
			if sum.BytesSent != hookSent || sum.BytesAcked != hookAcked || sum.BytesLost != hookLost {
				t.Errorf("Summary bytes (sent=%d acked=%d lost=%d) != hook sums (sent=%d acked=%d lost=%d)",
					sum.BytesSent, sum.BytesAcked, sum.BytesLost, hookSent, hookAcked, hookLost)
			}
			lossAgg := 0.0
			if sum.BytesSent > 0 {
				lossAgg = float64(sum.BytesLost) / float64(sum.BytesSent)
			}
			// Stale-ACK boundary artifact (spec §12.3): allow one BDP at the
			// peak throughput plus the handshake bytes sent before CC was attached.
			slack := uint64(sum.TputMax*0.03/8) + 65536
			if sum.BytesAcked > sum.BytesSent+slack {
				t.Errorf("BytesAcked=%d exceeds BytesSent=%d by more than one BDP (%d)", sum.BytesAcked, sum.BytesSent, slack)
			}
			if e, of := sum.RTTPercentile(0.5); e == 0 || of || e > 150 {
				t.Errorf("rttP50=(%v,%v) implausible for a 30 ms relay", e, of)
			}
			t.Logf("Summary: mis=%d appLimited=%d bwAnchored=%d floor=%d bwDrop=%d bwRise=%d ssSat=%d probes=%d lossAgg=%.2f lossMax=%.2f bwEst=%.1fM",
				sum.MIs, sum.AppLimitedMIs, sum.BWAnchoredMIs, sum.FloorMIs, sum.BWDrops, sum.BWRises, sum.SlowStartSaturations, sum.RateProbes, lossAgg, sum.LossMax, sum.BWEst/1e6)
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
	name         string
	lossRate     float64
	delayBase    time.Duration // one-way base delay
	delayJitter  time.Duration // ±jitter range
	transferSize int           // bytes to transfer (scaled by RTT)
	rttLabel     string
	goodput      float64 // bps: writeRate × (1-loss)
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
	if testing.Short() {
		t.Skip("convergence benchmark requires real QUIC transfers")
	}

	const (
		writeRate    = 80_000_000 / 8   // 80 Mbps in bytes/sec
		writeRateBps = 80_000_000.0     // 80 Mbps
		minMIs       = 60              // 30 converge + 30 steady state
	)

	// RTT profiles: base ± 20% jitter (segment-based to avoid reordering).
	// Transfer size computed from minMIs × RTT × writeRate — guarantees enough
	// MIs for convergence regardless of RTT (was fixed MB, starved mid/far).
	near := [2]time.Duration{15 * time.Millisecond, 3 * time.Millisecond}   // HK: RTT 24-36ms
	mid  := [2]time.Duration{75 * time.Millisecond, 15 * time.Millisecond}  // JP/SG: RTT 120-180ms
	far  := [2]time.Duration{150 * time.Millisecond, 30 * time.Millisecond} // US/EU: RTT 240-360ms

	nearSize := transferSizeForMIs(minMIs, 15, writeRate)  // ~14 MB
	midSize  := transferSizeForMIs(minMIs, 75, writeRate)  // ~72 MB
	farSize  := transferSizeForMIs(minMIs, 150, writeRate) // ~144 MB

	scenarios := []convergenceExpect{
		// ── Near (Hong Kong, 30ms RTT ±20%) ──
		{"near_loss0",  0.00, near[0], near[1], nearSize, "30ms",  writeRateBps * 1.00},
		{"near_loss5",  0.05, near[0], near[1], nearSize, "30ms",  writeRateBps * 0.95},
		{"near_loss10", 0.10, near[0], near[1], nearSize, "30ms",  writeRateBps * 0.90},
		{"near_loss20", 0.20, near[0], near[1], nearSize, "30ms",  writeRateBps * 0.80},
		{"near_loss30", 0.30, near[0], near[1], nearSize, "30ms",  writeRateBps * 0.70},
		{"near_loss35", 0.35, near[0], near[1], nearSize, "30ms",  writeRateBps * 0.65},

		// ── Mid (Japan/Singapore, 150ms RTT ±20%) ──
		{"mid_loss0",   0.00, mid[0], mid[1], midSize, "150ms", writeRateBps * 1.00},
		{"mid_loss5",   0.05, mid[0], mid[1], midSize, "150ms", writeRateBps * 0.95},
		{"mid_loss10",  0.10, mid[0], mid[1], midSize, "150ms", writeRateBps * 0.90},
		{"mid_loss20",  0.20, mid[0], mid[1], midSize, "150ms", writeRateBps * 0.80},
		{"mid_loss30",  0.30, mid[0], mid[1], midSize, "150ms", writeRateBps * 0.70},

		// ── Far (US-West/Europe, 300ms RTT ±20%) ──
		{"far_loss0",   0.00, far[0], far[1], farSize, "300ms", writeRateBps * 1.00},
		{"far_loss5",   0.05, far[0], far[1], farSize, "300ms", writeRateBps * 0.95},
		{"far_loss10",  0.10, far[0], far[1], farSize, "300ms", writeRateBps * 0.90},
		{"far_loss20",  0.20, far[0], far[1], farSize, "300ms", writeRateBps * 0.80},
		{"far_loss30",  0.30, far[0], far[1], farSize, "300ms", writeRateBps * 0.70},
	}

	type convergenceScenarioResult struct {
		name       string
		rttLabel   string
		efficiency float64
	}
	var (
		convergenceResults   []convergenceScenarioResult
		convergenceResultsMu sync.Mutex
	)

	// Run all scenarios in parallel inside a group subtest.
	// The group subtest blocks until all parallel children finish.
	t.Run("scenarios", func(t *testing.T) {
		for _, sc := range scenarios {
			t.Run(sc.name, func(t *testing.T) {
				t.Parallel()
				delay := jitterDelay(sc.delayBase, sc.delayJitter)
				env, totalRead, elapsed := runBulkTransfer(t, sc.transferSize, delay, sc.lossRate, writeRate)

				aggTP := float64(totalRead) * 8.0 / elapsed.Seconds()
				efficiency := aggTP / sc.goodput
				records := env.getRecords()
				cr := analyzeConvergence(records, sc.goodput)

				convergeStr := fmt.Sprintf("%d", cr.convergeMI)
				if cr.convergeMI < 0 {
					convergeStr = "NEVER"
				}
				t.Logf("%-12s │ %4s │ %4.0f%% │ %7.1f │ %4s │ %10.1f │ %5.1f%% │ %8.1f │ MIs=%d",
					sc.name, sc.rttLabel, sc.lossRate*100,
					sc.goodput/1e6,
					convergeStr,
					aggTP/1e6, efficiency*100,
					cr.steadyThroughput/1e6, len(records))

				if totalRead != int64(sc.transferSize) {
					t.Errorf("incomplete transfer: %d/%d bytes", totalRead, sc.transferSize)
				}
				if len(records) == 0 {
					t.Error("no MI completions")
					return
				}

				convergenceResultsMu.Lock()
				convergenceResults = append(convergenceResults, convergenceScenarioResult{
					name:       sc.name,
					rttLabel:   sc.rttLabel,
					efficiency: efficiency,
				})
				convergenceResultsMu.Unlock()
			})
		}
	})

	// Gates: all parallel subtests are now complete.
	//
	// NOTE (2026-10-01): these thresholds — and those of the Descent and
	// Bottleneck gates below — were recorded on the old reordering relay, i.e.
	// at ~35–45 % spurious declared loss. The harness now uses the FIFO relay;
	// the thresholds are deliberately UNCHANGED (not loosened, not yet
	// tightened) because no run on a quiet machine (load < 12) was possible
	// that day. A single FIFO run at load ~170 gave near/mid/far medians
	// 90/65/38 % and min 24.9 %, so they are expected to pass with a wide
	// margin; re-record as (min across ≥2 quiet runs × 0.8).
	t.Run("gates", func(t *testing.T) {
		results := convergenceResults

		var nearEffs, midEffs, farEffs, allEffs []float64
		var minEff float64 = 1.0
		var worstName string
		for _, r := range results {
			allEffs = append(allEffs, r.efficiency)
			if r.efficiency < minEff {
				minEff = r.efficiency
				worstName = r.name
			}
			switch r.rttLabel {
			case "30ms":
				nearEffs = append(nearEffs, r.efficiency)
			case "150ms":
				midEffs = append(midEffs, r.efficiency)
			case "300ms":
				farEffs = append(farEffs, r.efficiency)
			}
		}

		nearMed := median(nearEffs)
		midMed := median(midEffs)
		farMed := median(farEffs)
		overallMed := median(allEffs)

		t.Logf("=== Convergence Gates ===")
		t.Logf("  near median:    %5.1f%% (threshold %5.1f%%)", nearMed*100, 45.0)
		t.Logf("  mid median:     %5.1f%% (threshold %5.1f%%)", midMed*100, 25.0)
		t.Logf("  far median:     %5.1f%% (threshold %5.1f%%)", farMed*100, 18.0)
		t.Logf("  overall median: %5.1f%% (threshold %5.1f%%)", overallMed*100, 22.0)
		t.Logf("  catastrophic:   min %5.1f%% (floor 5.0%%)", minEff*100)

		if minEff < 0.05 {
			t.Errorf("CATASTROPHIC: scenario %s efficiency %.1f%% < 5%%", worstName, minEff*100)
		}
		if nearMed < 0.45 {
			t.Errorf("near RTT median %.1f%% < 45%%", nearMed*100)
		}
		if midMed < 0.20 {
			t.Errorf("mid RTT median %.1f%% < 20%%", midMed*100)
		}
		if farMed < 0.15 {
			t.Errorf("far RTT median %.1f%% < 15%%", farMed*100)
		}
		if overallMed < 0.22 {
			t.Errorf("overall median %.1f%% < 22%%", overallMed*100)
		}
	})
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
	if testing.Short() {
		t.Skip("descent benchmark requires real QUIC transfers")
	}

	const minMIs = 40 // 10 descent + 30 steady state

	// Bandwidth tiers (server write rate in bytes/sec)
	bw10  := 10_000_000 / 8
	bw30  := 30_000_000 / 8
	bw50  := 50_000_000 / 8
	bw80  := 80_000_000 / 8
	bw100 := 100_000_000 / 8

	near := [2]time.Duration{15 * time.Millisecond, 3 * time.Millisecond}
	mid  := [2]time.Duration{75 * time.Millisecond, 15 * time.Millisecond}

	// Transfer sizes computed from MI count × RTT × writeRate.
	// Each scenario gets enough MIs for descent + steady state.
	scenarios := []descentExpect{
		// ── Near RTT (30ms) — different bandwidths ──
		{"near_bw10",     bw10,  10e6,  0.00, near[0], near[1], transferSizeForMIs(minMIs, 15, bw10),  "30ms"},
		{"near_bw30",     bw30,  30e6,  0.00, near[0], near[1], transferSizeForMIs(minMIs, 15, bw30),  "30ms"},
		{"near_bw50",     bw50,  50e6,  0.00, near[0], near[1], transferSizeForMIs(minMIs, 15, bw50),  "30ms"},
		{"near_bw80",     bw80,  80e6,  0.00, near[0], near[1], transferSizeForMIs(minMIs, 15, bw80),  "30ms"},
		{"near_bw100",    bw100, 100e6, 0.00, near[0], near[1], transferSizeForMIs(minMIs, 15, bw100), "30ms"},

		// ── Near RTT + 5% loss ──
		{"near_bw30_l5",  bw30,  30e6,  0.05, near[0], near[1], transferSizeForMIs(minMIs, 15, bw30),  "30ms"},
		{"near_bw50_l5",  bw50,  50e6,  0.05, near[0], near[1], transferSizeForMIs(minMIs, 15, bw50),  "30ms"},
		{"near_bw100_l5", bw100, 100e6, 0.05, near[0], near[1], transferSizeForMIs(minMIs, 15, bw100), "30ms"},

		// ── Mid RTT (150ms) — descent takes longer ──
		{"mid_bw30",      bw30,  30e6,  0.00, mid[0],  mid[1],  transferSizeForMIs(minMIs, 75, bw30),  "150ms"},
		{"mid_bw50",      bw50,  50e6,  0.00, mid[0],  mid[1],  transferSizeForMIs(minMIs, 75, bw50),  "150ms"},
		{"mid_bw100",     bw100, 100e6, 0.00, mid[0],  mid[1],  transferSizeForMIs(minMIs, 75, bw100), "150ms"},

		// ── Mid RTT + 5% loss ──
		{"mid_bw50_l5",   bw50,  50e6,  0.05, mid[0],  mid[1],  transferSizeForMIs(minMIs, 75, bw50),  "150ms"},
		{"mid_bw100_l5",  bw100, 100e6, 0.05, mid[0],  mid[1],  transferSizeForMIs(minMIs, 75, bw100), "150ms"},
	}

	type descentScenarioResult struct {
		name            string
		efficiency      float64
		descentAchieved bool // TargetRate reached ≤ 2× bandwidth
	}
	var (
		descentResults   []descentScenarioResult
		descentResultsMu sync.Mutex
	)

	t.Run("scenarios", func(t *testing.T) {
		for _, sc := range scenarios {
			t.Run(sc.name, func(t *testing.T) {
				t.Parallel()
				delay := jitterDelay(sc.delayBase, sc.delayJitter)
				env, totalRead, elapsed := runBulkTransfer(t, sc.transferSize, delay, sc.lossRate, sc.bandwidth)

				aggTP := float64(totalRead) * 8.0 / elapsed.Seconds()
				efficiency := aggTP / sc.bandwidthBps
				records := env.getRecords()
				dr := analyzeDescent(records, sc.bandwidthBps)

				descentStr := fmt.Sprintf("%d", dr.descentMI)
				if dr.descentMI < 0 {
					descentStr = "NEVER"
				}
				t.Logf("%-13s │ %4s │ %4.0f%% │ %8.1f │ %4s │ %10.1f │ %5.1f%% │ %8.1f │ %7.2fx │ MIs=%d",
					sc.name, sc.rttLabel, sc.lossRate*100,
					sc.bandwidthBps/1e6,
					descentStr,
					aggTP/1e6, efficiency*100,
					dr.steadyRate/1e6, dr.rateAccuracy, len(records))

				if totalRead != int64(sc.transferSize) {
					t.Errorf("incomplete transfer: %d/%d bytes", totalRead, sc.transferSize)
				}
				if len(records) == 0 {
					t.Error("no MI completions")
					return
				}

				descentResultsMu.Lock()
				descentResults = append(descentResults, descentScenarioResult{
					name:            sc.name,
					efficiency:      efficiency,
					descentAchieved: dr.descentMI >= 0,
				})
				descentResultsMu.Unlock()
			})
		}
	})

	t.Run("gates", func(t *testing.T) {
		var allEffs []float64
		var minEff float64 = 1.0
		var worstName string
		descentCount := 0
		for _, r := range descentResults {
			allEffs = append(allEffs, r.efficiency)
			if r.efficiency < minEff {
				minEff = r.efficiency
				worstName = r.name
			}
			if r.descentAchieved {
				descentCount++
			}
		}

		overallMed := median(allEffs)
		descentPct := float64(descentCount) / float64(max(len(descentResults), 1)) * 100

		t.Logf("=== Descent Gates ===")
		t.Logf("  descent achieved: %d/%d (%.0f%%, threshold 80%%)", descentCount, len(descentResults), descentPct)
		t.Logf("  overall median:   %5.1f%% (threshold %5.1f%%)", overallMed*100, 50.0)
		t.Logf("  catastrophic:     min %5.1f%% (floor 15.0%%)", minEff*100)

		if minEff < 0.15 {
			t.Errorf("CATASTROPHIC: scenario %s efficiency %.1f%% < 15%%", worstName, minEff*100)
		}
		if descentPct < 80.0 {
			t.Errorf("descent achieved in only %d/%d scenarios (%.0f%% < 80%%)", descentCount, len(descentResults), descentPct)
		}
		if overallMed < 0.50 {
			t.Errorf("overall median efficiency %.1f%% < 50%%", overallMed*100)
		}
	})
}

// ============================================================================
// Bottleneck Benchmark: network bandwidth cap < CC rate → congestion-induced loss
// ============================================================================
//
// Simulates the production case: server has plenty of data (80M write rate),
// but the network path has a congestion bottleneck (e.g., GFW throttling to 5M).
// The relay's token bucket drops excess packets, creating loss that INCREASES
// with sending rate — unlike random loss which is constant.
//
// This is the case that broke in production: K2CC sends at 30M+ (floor),
// bottleneck only passes 5M, 83%+ of packets are dropped, datagrams die,
// DNS becomes unreliable, user sees 3-5M throughput with massive jitter.
//
// Key metrics:
//   - Steady rate: should converge near bottleneck capacity (not floor)
//   - Loss rate: should stabilize at low level once rate converges
//   - Throughput efficiency: aggregate throughput / bottleneck capacity

type bottleneckExpect struct {
	name          string
	bottleneckBps float64       // network bandwidth cap (bits/sec)
	gfwLoss       float64       // additional random loss (GFW baseline)
	delayBase     time.Duration // one-way delay
	delayJitter   time.Duration
	transferSize  int
	rttLabel      string
	writeRate     int // server write rate (bytes/sec), should be >> bottleneck
}

type bottleneckResult struct {
	totalMIs   int
	appLimited int
	steadyRate float64 // average TargetRate in last 50% of non-app-limited MIs
	steadyLoss float64 // average loss in last 50% of non-app-limited MIs
}

func analyzeBottleneck(records []MIRecord) bottleneckResult {
	var res bottleneckResult
	res.totalMIs = len(records)

	if len(records) == 0 {
		return res
	}

	for _, r := range records {
		if r.AppLimited {
			res.appLimited++
		}
	}

	// Steady-state: last 50% of non-app-limited MIs
	midpoint := len(records) / 2
	var sumRate, sumLoss float64
	var count int
	for i := midpoint; i < len(records); i++ {
		if !records[i].AppLimited && records[i].BytesSent > 0 {
			sumRate += records[i].TargetRate
			sumLoss += float64(records[i].BytesLost) / float64(records[i].BytesSent)
			count++
		}
	}
	if count > 0 {
		res.steadyRate = sumRate / float64(count)
		res.steadyLoss = sumLoss / float64(count)
	}

	return res
}

func TestBottleneck_Benchmark(t *testing.T) {
	if testing.Short() {
		t.Skip("bottleneck benchmark requires real QUIC transfers")
	}

	const (
		writeRate80M = 80_000_000 / 8 // 80 Mbps server write rate (plenty of data)
	)

	// RTT profiles (one-way delay ± jitter)
	veryNear := [2]time.Duration{8 * time.Millisecond, 2 * time.Millisecond}   // HK: RTT 12-20ms
	near     := [2]time.Duration{15 * time.Millisecond, 3 * time.Millisecond}  // HK: RTT 24-36ms
	mid      := [2]time.Duration{75 * time.Millisecond, 15 * time.Millisecond} // JP/SG: RTT 120-180ms
	far      := [2]time.Duration{130 * time.Millisecond, 26 * time.Millisecond} // US: RTT 208-312ms (~260ms)

	// Transfer sizes: larger for low bandwidth (need more time for CC to stabilize)
	// Doubled from original for 2×RTT MinDuration (MI takes 2× longer)
	smallSize := 10 * 1024 * 1024 // 10 MB — enough for 5M bottleneck (~16s)
	medSize   := 20 * 1024 * 1024 // 20 MB

	scenarios := []bottleneckExpect{
		// ── Production case: GFW throttles to 5M, very low RTT ──
		{"hk_bw5",       5e6,  0.00, veryNear[0], veryNear[1], smallSize, "16ms",  writeRate80M},
		{"hk_bw5_gfw5",  5e6,  0.05, veryNear[0], veryNear[1], smallSize, "16ms",  writeRate80M},

		// ── Moderate throttling: 15M bottleneck ──
		{"hk_bw15",      15e6, 0.00, veryNear[0], veryNear[1], medSize,   "16ms",  writeRate80M},
		{"hk_bw15_gfw5", 15e6, 0.05, veryNear[0], veryNear[1], medSize,   "16ms",  writeRate80M},

		// ── Bottleneck at floor boundary: 30M ──
		{"hk_bw30",      30e6, 0.00, near[0],     near[1],     medSize,   "30ms",  writeRate80M},
		{"hk_bw30_gfw5", 30e6, 0.05, near[0],     near[1],     medSize,   "30ms",  writeRate80M},

		// ── Bottleneck above floor: 50M (should converge normally) ──
		{"hk_bw50",      50e6, 0.00, near[0],     near[1],     medSize,   "30ms",  writeRate80M},
		{"hk_bw50_gfw5", 50e6, 0.05, near[0],     near[1],     medSize,   "30ms",  writeRate80M},

		// ── Mid RTT (150ms) with low bottleneck ──
		{"jp_bw5",       5e6,  0.00, mid[0],      mid[1],      smallSize, "150ms", writeRate80M},
		{"jp_bw15",      15e6, 0.00, mid[0],      mid[1],      medSize,   "150ms", writeRate80M},
		{"jp_bw30",      30e6, 0.00, mid[0],      mid[1],      medSize,   "150ms", writeRate80M},
		{"jp_bw15_gfw5", 15e6, 0.05, mid[0],      mid[1],      medSize,   "150ms", writeRate80M},

		// ── Far RTT (260ms) with low bottleneck ──
		{"us_bw5",       5e6,  0.00, far[0],      far[1],      smallSize, "260ms", writeRate80M},
		{"us_bw15",      15e6, 0.00, far[0],      far[1],      medSize,   "260ms", writeRate80M},
		{"us_bw30",      30e6, 0.00, far[0],      far[1],      medSize,   "260ms", writeRate80M},
		{"us_bw15_gfw5", 15e6, 0.05, far[0],      far[1],      medSize,   "260ms", writeRate80M},
	}

	type bottleneckScenarioResult struct {
		name       string
		rttLabel   string
		efficiency float64
		rateRatio  float64 // steadyRate / bottleneckBps
	}
	var (
		bottleneckResults   []bottleneckScenarioResult
		bottleneckResultsMu sync.Mutex
	)

	t.Run("scenarios", func(t *testing.T) {
		for _, sc := range scenarios {
			t.Run(sc.name, func(t *testing.T) {
				t.Parallel()
				delay := jitterDelay(sc.delayBase, sc.delayJitter)
				env, totalRead, elapsed := runBulkTransferFull(
					t, sc.transferSize, delay, sc.gfwLoss, sc.writeRate, sc.bottleneckBps)

				aggTP := float64(totalRead) * 8.0 / elapsed.Seconds()
				efficiency := aggTP / sc.bottleneckBps
				records := env.getRecords()
				br := analyzeBottleneck(records)

				rateRatio := 0.0
				if sc.bottleneckBps > 0 {
					rateRatio = br.steadyRate / sc.bottleneckBps
				}

				t.Logf("%-12s │ %4s │ %4.0f%% │ %8.1f │ %4d │ %9.1f │ %5.1f%% │ %8.1f │ %7.1f%%",
					sc.name, sc.rttLabel, sc.gfwLoss*100,
					sc.bottleneckBps/1e6,
					br.totalMIs,
					aggTP/1e6, efficiency*100,
					br.steadyRate/1e6, br.steadyLoss*100)

				if totalRead != int64(sc.transferSize) {
					t.Errorf("incomplete transfer: %d/%d bytes", totalRead, sc.transferSize)
				}
				if len(records) == 0 {
					t.Error("no MI completions")
					return
				}

				bottleneckResultsMu.Lock()
				bottleneckResults = append(bottleneckResults, bottleneckScenarioResult{
					name:       sc.name,
					rttLabel:   sc.rttLabel,
					efficiency: efficiency,
					rateRatio:  rateRatio,
				})
				bottleneckResultsMu.Unlock()
			})
		}
	})

	t.Run("gates", func(t *testing.T) {
		var hkEffs, jpEffs, usEffs, allEffs []float64
		var minEff float64 = 1.0
		var worstEffName string
		var maxRatio float64
		var worstRatioName string
		for _, r := range bottleneckResults {
			allEffs = append(allEffs, r.efficiency)
			if r.efficiency < minEff {
				minEff = r.efficiency
				worstEffName = r.name
			}
			if r.rateRatio > maxRatio {
				maxRatio = r.rateRatio
				worstRatioName = r.name
			}
			switch {
			case r.rttLabel == "16ms" || r.rttLabel == "30ms":
				hkEffs = append(hkEffs, r.efficiency)
			case r.rttLabel == "150ms":
				jpEffs = append(jpEffs, r.efficiency)
			case r.rttLabel == "260ms":
				usEffs = append(usEffs, r.efficiency)
			}
		}

		hkMed := median(hkEffs)
		jpMed := median(jpEffs)
		usMed := median(usEffs)
		overallMed := median(allEffs)

		t.Logf("=== Bottleneck Gates ===")
		t.Logf("  HK median:      %5.1f%% (threshold %5.1f%%)", hkMed*100, 55.0)
		t.Logf("  JP median:      %5.1f%% (threshold %5.1f%%)", jpMed*100, 30.0)
		t.Logf("  US median:      %5.1f%% (threshold %5.1f%%)", usMed*100, 20.0)
		t.Logf("  overall median: %5.1f%% (threshold %5.1f%%)", overallMed*100, 35.0)
		t.Logf("  catastrophic:   min %5.1f%% (floor 10.0%%)", minEff*100)
		t.Logf("  rate ratio:     max %5.1fx (ceiling  5.0x)", maxRatio)

		if minEff < 0.10 {
			t.Errorf("CATASTROPHIC: scenario %s efficiency %.1f%% < 10%%", worstEffName, minEff*100)
		}
		if maxRatio > 5.0 {
			t.Errorf("CATASTROPHIC: scenario %s rate ratio %.1fx > 5x (over-sending)", worstRatioName, maxRatio)
		}
		if hkMed < 0.55 {
			t.Errorf("HK median efficiency %.1f%% < 55%%", hkMed*100)
		}
		if jpMed < 0.30 {
			t.Errorf("JP median efficiency %.1f%% < 30%%", jpMed*100)
		}
		if usMed < 0.20 {
			t.Errorf("US median efficiency %.1f%% < 20%%", usMed*100)
		}
		if overallMed < 0.35 {
			t.Errorf("overall median efficiency %.1f%% < 35%%", overallMed*100)
		}
	})
}
