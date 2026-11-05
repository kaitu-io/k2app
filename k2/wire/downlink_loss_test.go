package wire

import (
	"context"
	"io"
	"math/rand"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/kaitu-io/kuic"
	tls "github.com/metacubex/utls"
)

// ---- downlinkLossWindow (pure) ----

func TestDownlinkLossWindow_FirstReadingIsNoEvidence(t *testing.T) {
	var w downlinkLossWindow
	now := time.Now()
	if got := w.observe("c1", 100, 200, now); got != 0 {
		t.Fatalf("first reading = %v, want 0 (base only)", got)
	}
}

func TestDownlinkLossWindow_RateAfterMinWindow(t *testing.T) {
	var w downlinkLossWindow
	t0 := time.Now()
	w.observe("c1", 0, 0, t0)
	// 1s later: window too short, no rate yet.
	if got := w.observe("c1", 40, 100, t0.Add(time.Second)); got != 0 {
		t.Fatalf("before min window = %v, want 0", got)
	}
	// 2s: 100 packet numbers spanned, 40 arrived → 60% loss.
	got := w.observe("c1", 40, 100, t0.Add(downlinkLossMinWindow))
	if got < 0.599 || got > 0.601 {
		t.Fatalf("rate = %v, want 0.6", got)
	}
	// Between windows the last rate is held, not dropped to 0.
	if got := w.observe("c1", 50, 110, t0.Add(downlinkLossMinWindow+time.Second)); got < 0.599 || got > 0.601 {
		t.Fatalf("held rate = %v, want 0.6", got)
	}
	// Next window is computed from the new base: 100 more spanned, all arrived.
	if got := w.observe("c1", 140, 200, t0.Add(2*downlinkLossMinWindow)); got != 0 {
		t.Fatalf("clean window = %v, want 0", got)
	}
}

// A sparse window keeps growing until it has downlinkLossMinSpan packets, so
// a light-traffic user still gets a rate — just over a longer window.
func TestDownlinkLossWindow_SparseWindowGrowsUntilEnoughPackets(t *testing.T) {
	var w downlinkLossWindow
	t0 := time.Now()
	w.observe("c1", 0, 0, t0)
	if got := w.observe("c1", 4, 10, t0.Add(3*time.Second)); got != 0 {
		t.Fatalf("10 packets after 3s = %v, want 0 (not enough evidence yet)", got)
	}
	got := w.observe("c1", 8, 20, t0.Add(6*time.Second))
	if got < 0.599 || got > 0.601 {
		t.Fatalf("20 packets after 6s = %v, want 0.6 over the grown window", got)
	}
}

// Fewer than downlinkLossMinSpan packets in downlinkLossMaxWindow: no
// evidence, and an earlier lossy rate must not linger as if it were current.
func TestDownlinkLossWindow_IdleWindowExpiresOldRate(t *testing.T) {
	var w downlinkLossWindow
	t0 := time.Now()
	w.observe("c1", 0, 0, t0)
	if got := w.observe("c1", 40, 100, t0.Add(2*time.Second)); got < 0.59 {
		t.Fatalf("setup rate = %v, want 0.6", got)
	}
	if got := w.observe("c1", 41, 102, t0.Add(2*time.Second+downlinkLossMaxWindow)); got != 0 {
		t.Fatalf("after idle max window = %v, want 0 (no evidence)", got)
	}
}

// A new connection starts its counters from zero; the old base and the old
// connection's rate say nothing about it.
func TestDownlinkLossWindow_NewConnectionResets(t *testing.T) {
	var w downlinkLossWindow
	t0 := time.Now()
	w.observe("c1", 0, 0, t0)
	if got := w.observe("c1", 40, 100, t0.Add(2*time.Second)); got < 0.59 {
		t.Fatalf("setup rate = %v, want 0.6", got)
	}
	if got := w.observe("c2", 5, 5, t0.Add(3*time.Second)); got != 0 {
		t.Fatalf("new connection = %v, want 0", got)
	}
	// Even with the same identity, counters going backwards mean a new conn.
	var w2 downlinkLossWindow
	w2.observe("c1", 100, 100, t0)
	if got := w2.observe("c1", 10, 50, t0.Add(3*time.Second)); got != 0 {
		t.Fatalf("counters backwards = %v, want 0", got)
	}
}

func TestDownlinkLossWindow_NilConnIsNoEvidence(t *testing.T) {
	var w downlinkLossWindow
	t0 := time.Now()
	w.observe(nil, 0, 0, t0)
	if got := w.observe(nil, 40, 100, t0.Add(3*time.Second)); got != 0 {
		t.Fatalf("nil conn = %v, want 0", got)
	}
}

// ---- real QUICClient ↔ QUICServer through a lossy relay ----

// lossyUDPRelay forwards one client's datagrams to target and back, dropping
// 1-RTT (short header) server→client packets with probability downRate. The
// handshake (long header) is never dropped so the test measures steady-state
// loss, not handshake retries.
type lossyUDPRelay struct {
	pc       *net.UDPConn
	target   *net.UDPAddr
	downRate float64

	mu       sync.Mutex
	rng      *rand.Rand
	client   *net.UDPAddr
	upstream *net.UDPConn
	seen     int
	dropped  int
}

func newLossyUDPRelay(t *testing.T, target *net.UDPAddr, downRate float64) *lossyUDPRelay {
	t.Helper()
	pc, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	up, err := net.DialUDP("udp", nil, target)
	if err != nil {
		t.Fatal(err)
	}
	r := &lossyUDPRelay{pc: pc, target: target, downRate: downRate, rng: rand.New(rand.NewSource(7)), upstream: up}
	go r.clientToServer()
	go r.serverToClient()
	t.Cleanup(func() { pc.Close(); up.Close() })
	return r
}

func (r *lossyUDPRelay) addr() *net.UDPAddr { return r.pc.LocalAddr().(*net.UDPAddr) }

func (r *lossyUDPRelay) clientToServer() {
	buf := make([]byte, 65535)
	for {
		n, from, err := r.pc.ReadFromUDP(buf)
		if err != nil {
			return
		}
		r.mu.Lock()
		r.client = from
		r.mu.Unlock()
		_, _ = r.upstream.Write(buf[:n])
	}
}

func (r *lossyUDPRelay) serverToClient() {
	buf := make([]byte, 65535)
	for {
		n, err := r.upstream.Read(buf)
		if err != nil {
			return
		}
		r.mu.Lock()
		client := r.client
		drop := false
		if n > 0 && buf[0]&0x80 == 0 {
			r.seen++
			if r.rng.Float64() < r.downRate {
				drop = true
				r.dropped++
			}
		}
		r.mu.Unlock()
		if !drop && client != nil {
			_, _ = r.pc.WriteToUDP(buf[:n], client)
		}
	}
}

func (r *lossyUDPRelay) actualDownRate() float64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.seen == 0 {
		return 0
	}
	return float64(r.dropped) / float64(r.seen)
}

// downloadThroughRelay connects a QUICClient to a QUICServer via a relay that
// drops downRate of the server's 1-RTT packets, downloads payload bytes over
// one proxied TCP stream, and returns the client's WireStats after the
// download plus the drop rate the relay actually applied.
func downloadThroughRelay(t *testing.T, downRate float64, payload int) (WireStats, float64) {
	t.Helper()
	cert := generateTestCert(t)
	serverUDP, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	serverTransport := &quic.Transport{Conn: serverUDP}
	defer serverTransport.Close()
	ln, err := serverTransport.Listen(&tls.Config{Certificates: []tls.Certificate{cert}, NextProtos: []string{"h3"}}, DefaultQUICConfig())
	if err != nil {
		t.Fatal(err)
	}
	server := NewQUICServer()
	server.Listen(ln)
	defer server.Close()

	relay := newLossyUDPRelay(t, serverUDP.LocalAddr().(*net.UDPAddr), downRate)
	client := NewQUICClient(Config{Host: "127.0.0.1", Port: relay.addr().Port}, nil)
	client.tlsConfig = buildInsecureTLSConfig()
	defer client.Close()

	go func() {
		conn, _, err := server.AcceptTCP(context.Background())
		if err != nil {
			return
		}
		defer conn.Close()
		if err := WriteStreamResponse(conn, nil); err != nil {
			return
		}
		_, _ = conn.Write(make([]byte, payload))
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	conn, err := client.DialTCP(ctx, "example.com:80", nil)
	if err != nil {
		t.Fatalf("DialTCP: %v", err)
	}
	defer conn.Close()
	start := time.Now()
	client.WireStats() // base reading for the window

	_ = conn.SetReadDeadline(time.Now().Add(50 * time.Second))
	n, err := io.CopyN(io.Discard, conn, int64(payload))
	if err != nil {
		t.Fatalf("download: got %d/%d bytes: %v", n, payload, err)
	}
	if wait := downlinkLossMinWindow - time.Since(start); wait > 0 {
		time.Sleep(wait + 50*time.Millisecond)
	}
	return client.WireStats(), relay.actualDownRate()
}

// The blind spot this file makes visible: a downloading client whose
// server→client packets are dropped. The client's own (uplink) loss stays near
// zero; the receive-side estimate reports the applied loss.
func TestQUICClient_DownlinkLossVisibleWhileUplinkClean(t *testing.T) {
	ws, actual := downloadThroughRelay(t, 0.3, 4<<20)
	t.Logf("relay dropped %.3f of server 1-RTT packets; client DownlinkLossRate=%.3f PacketLossRate(uplink)=%.3f",
		actual, ws.DownlinkLossRate, ws.PacketLossRate)
	if ws.DownlinkLossRate < actual-0.08 || ws.DownlinkLossRate > actual+0.08 {
		t.Fatalf("DownlinkLossRate = %.3f, want ≈ applied %.3f", ws.DownlinkLossRate, actual)
	}
}

// Negative control: a clean path must read (nearly) lossless, or every
// healthy download would be graded degraded.
func TestQUICClient_DownlinkLossCleanPath(t *testing.T) {
	ws, _ := downloadThroughRelay(t, 0, 4<<20)
	t.Logf("clean: DownlinkLossRate=%.4f PacketLossRate=%.4f", ws.DownlinkLossRate, ws.PacketLossRate)
	if ws.DownlinkLossRate > 0.02 {
		t.Fatalf("clean path DownlinkLossRate = %.4f, want < 0.02", ws.DownlinkLossRate)
	}
}

// selectorWithQUICStats builds a quic-primary TM with a healthy, freshly
// measured tcpws backup, records ws as QUIC's stats and evaluates the
// selector at t0 and past the sustain window.
func selectorWithQUICStats(t *testing.T, ws WireStats) (*TransportManager, channelDecision) {
	t.Helper()
	tm := NewTransportManager()
	tm.SetPrimary(&mockTransport{available: true}, "quic-443")
	tm.SetTCPWS(&mockTransport{available: true})
	t0 := time.Now()
	for i := 0; i < 5; i++ {
		tm.RecordEchoResult("tcpws", 15*time.Millisecond, nil, t0)
		// QUIC is reachable (echoes pass) — only its loss differs per case.
		tm.RecordEchoResult("quic", 0, nil, t0)
	}
	tm.RecordQUICStats(ws, t0)
	if d := tm.evaluateSelector(t0); d != channelStay {
		t.Fatalf("evaluateSelector(t0) = %v, want channelStay (sustain window not elapsed)", d)
	}
	after := t0.Add(qualitySustainWindow + time.Second)
	tm.RecordQUICStats(ws, after)
	return tm, tm.evaluateSelector(after)
}

// Downlink loss is observability only: it must NOT drive the selector's
// quality path (k2cc serves ~30% downlink loss fine for real users; see
// downlink_loss.go). Wiring DownlinkLossRate into RecordQUICStats turns this
// red.
func TestSelector_DownlinkLossDoesNotDriveQualityPath(t *testing.T) {
	tm, d := selectorWithQUICStats(t, WireStats{DownlinkLossRate: 0.6, RTT: 20 * time.Millisecond, MinRTT: 20 * time.Millisecond})
	if d != channelStay || tm.ActiveChannel() != "quic" {
		t.Fatalf("decision=%v active=%q, want stay on quic — downlink loss must not feed the QUIC score", d, tm.ActiveChannel())
	}
}

// Positive control for the helper: the same setup with UPLINK loss 0.6 does
// switch, so the test above is not passing because the helper can never
// switch.
func TestSelector_UplinkLossStillDrivesQualityPath(t *testing.T) {
	tm, d := selectorWithQUICStats(t, WireStats{PacketLossRate: 0.6, RTT: 20 * time.Millisecond, MinRTT: 20 * time.Millisecond})
	if d != channelSwitchToTCPWS || tm.ActiveChannel() != "tcpws" {
		t.Fatalf("decision=%v active=%q, want switch on 60%% uplink loss", d, tm.ActiveChannel())
	}
}
