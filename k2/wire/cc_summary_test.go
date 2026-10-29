package wire

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/kaitu-io/k2/wire/k2cc"
	"github.com/kaitu-io/kuic"
	tls "github.com/metacubex/utls"
)

// attrPairs turns a key/value []any into a map of rendered values.
func attrPairs(t *testing.T, attrs []any) map[string]string {
	t.Helper()
	if len(attrs)%2 != 0 {
		t.Fatalf("odd attr list: %v", attrs)
	}
	out := map[string]string{}
	for i := 0; i < len(attrs); i += 2 {
		out[attrs[i].(string)] = fmt.Sprint(attrs[i+1])
	}
	return out
}

func TestCCSummaryAttrs_FieldSetAndFormatting(t *testing.T) {
	win := k2cc.CCSummary{
		SenderID: 3, MIs: 42, AppLimitedMIs: 31, BWAnchoredMIs: 8, FloorMIs: 1,
		SlowStartMIs: 2, DecisionMIs: 40, BWDrops: 1, BWRises: 2, SlowStartSaturations: 1, RateProbes: 3,
		BytesSent: 40_000_000, BytesAcked: 38_000_000, BytesLost: 1_360_000,
		LossMax: 0.142, TputMax: 48_700_000,
	}
	win.LossHist[3] = 21 // 2%
	win.LossHist[5] = 21 // 10%
	win.RTTHist[11] = 42 // all in the open bucket → ">1000"
	win.RateHist[5] = 42 // 30 Mbps
	win.TputHist[4] = 42 // 20 Mbps
	// Appended 2026-10: event-time loss, stale share, resolved share, per-phase loss.
	win.DeclaredLostBytes, win.EventAckedBytes = 3_000_000, 37_000_000 // 7.5 %
	win.StaleLostBytes = 1_500_000                                      // 50 % of declared
	win.ResolvedBytes = 22_000_000                                      // 55 % of sent
	win.PhaseLostBytes[1], win.PhaseResolvedBytes[1] = 30_000, 1_000_000 // cruise 3.0 %
	win.PhaseLostBytes[2], win.PhaseResolvedBytes[2] = 90_000, 1_000_000 // probe-up 9.0 %
	// drain: nothing resolved → "-"
	win.Variant = "fix-a3"
	cur := win
	cur.BWEst = 47_100_000
	cur.BGLoss = 0.012
	cur.Epsilon = 0.09
	cur.MinRTT = 41 * time.Millisecond
	cur.Mode = k2cc.K2CCModeDecision

	got := attrPairs(t, ccSummaryAttrs(win, cur, 3, false, 60, 181, "abc"))
	want := map[string]string{
		"udid": "abc", "seq": "3", "final": "false", "windowS": "60", "uptimeS": "181",
		"mis": "42", "appLimited": "31", "bwAnchored": "8", "floor": "1",
		"slowStart": "2", "decision": "40", "rateAdj": "0",
		"bwDrop": "1", "bwRise": "2", "ssSat": "1", "probes": "3",
		"sentMB": "38.1", "ackedMB": "36.2", "lossAgg": "3.4",
		"lossP50": "2", "lossP90": "10", "lossMax": "14.2",
		"rttP50": ">1000", "rttP90": ">1000", "minRttMs": "41",
		"rateP50": "30", "tputP50": "20", "tputMax": "48.7",
		"bwEst": "47.1", "bgLoss": "1.2", "epsilon": "0.090", "mode": "decision",
		"variant": "fix-a3", "declLoss": "7.5", "staleLoss": "50", "resolved": "55",
		"lossCruise": "3.0", "lossProbe": "9.0", "lossDrain": "-",
	}
	for k, w := range want {
		if g, ok := got[k]; !ok {
			t.Errorf("missing key %s", k)
		} else if g != w {
			t.Errorf("%s=%q want %q", k, g, w)
		}
	}
	for k := range got {
		if _, ok := want[k]; !ok {
			t.Errorf("unexpected key %s — the key set is the Phase 2/3 contract", k)
		}
	}
}

func TestCCSummaryAttrs_ZeroWindowDoesNotPanic(t *testing.T) {
	cur := k2cc.CCSummary{SenderID: 1, MIs: 5}
	got := attrPairs(t, ccSummaryAttrs(k2cc.CCSummary{SenderID: 1}, cur, 2, true, 0, 9, ""))
	if got["mis"] != "0" || got["lossAgg"] != "0.0" || got["lossP50"] != "0" || got["final"] != "true" || got["udid"] != "" {
		t.Fatalf("zero window rendering: %v", got)
	}
	// A sender with no arm name is the control arm; empty windows render zeros.
	if got["variant"] != "control" || got["declLoss"] != "0.0" || got["staleLoss"] != "0" || got["resolved"] != "0" || got["lossDrain"] != "-" {
		t.Fatalf("zero window rendering of appended keys: %v", got)
	}
}

func TestCCSummaryAttrs_LossAggClampedTo100(t *testing.T) {
	win := k2cc.CCSummary{SenderID: 1, BytesSent: 100, BytesLost: 250}
	if got := attrPairs(t, ccSummaryAttrs(win, win, 1, false, 60, 60, ""))["lossAgg"]; got != "100.0" {
		t.Fatalf("lossAgg=%q want clamped 100.0 (same semantics as LossRate())", got)
	}
}

// syncRecordCollector is a goroutine-safe slog.Handler for tests that log
// from connection goroutines (transport_selector_test.go's recordCollector
// appends without a lock).
type syncRecordCollector struct {
	mu      sync.Mutex
	records []slog.Record
}

func (c *syncRecordCollector) Enabled(context.Context, slog.Level) bool { return true }
func (c *syncRecordCollector) Handle(_ context.Context, r slog.Record) error {
	c.mu.Lock()
	c.records = append(c.records, r.Clone())
	c.mu.Unlock()
	return nil
}
func (c *syncRecordCollector) WithAttrs([]slog.Attr) slog.Handler { return c }
func (c *syncRecordCollector) WithGroup(string) slog.Handler      { return c }

func (c *syncRecordCollector) byMessage(msg string) []slog.Record {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []slog.Record
	for _, r := range c.records {
		if r.Message == msg {
			out = append(out, r)
		}
	}
	return out
}

// ============================================================================
// ccSummaryLoop — real QUIC client/server pair
// ============================================================================

// ccSummaryTestServer is an authenticated QUIC server whose AcceptTCP side
// streams `payload` bytes to every proxied stream (download direction = the
// server's k2cc sender), paced at one 32 KiB chunk per 5 ms so the transfer
// outlives the first MI. k2cc creates the first MI on the first OnPacketSent
// with MinDuration = 2×initialRTT = 200 ms, and completion is only evaluated
// inside OnPacketAcked — a loopback burst that finishes in 30 ms leaves no
// ACK after the 200 ms gate, so the MI never completes (MIs=0, no line).
// Returns the client config to dial it.
func ccSummaryTestServer(t *testing.T, payload int) (Config, func()) {
	t.Helper()
	cert := generateTestCert(t)
	serverUDP, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	serverTransport := &quic.Transport{Conn: serverUDP}
	ln, err := serverTransport.Listen(&tls.Config{
		Certificates: []tls.Certificate{cert},
		NextProtos:   []string{"h3"},
	}, DefaultQUICConfig())
	if err != nil {
		t.Fatal(err)
	}
	server := NewQUICServer()
	server.SetValidator(func(token, udid, mode string) AuthVerdict {
		if udid == "good-device" && token == "valid-token" {
			return AuthVerdict{OK: true}
		}
		return AuthVerdict{Code: 401}
	})
	server.Listen(ln)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		for {
			c, _, err := server.AcceptTCP(ctx)
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				if err := WriteStreamResponse(c, nil); err != nil {
					return
				}
				buf := make([]byte, 32*1024)
				for rem := payload; rem > 0; {
					n := min(len(buf), rem)
					if _, err := c.Write(buf[:n]); err != nil {
						return
					}
					rem -= n
					time.Sleep(5 * time.Millisecond)
				}
			}()
		}
	}()

	cfg := Config{
		Host:  "127.0.0.1",
		Port:  serverUDP.LocalAddr().(*net.UDPAddr).Port,
		UDID:  "good-device",
		Token: "valid-token",
	}
	return cfg, func() {
		cancel()
		server.Close()
		serverTransport.Close()
	}
}

func withShortCCSummaryInterval(t *testing.T, d time.Duration) *syncRecordCollector {
	t.Helper()
	prevInterval := ccSummaryInterval
	ccSummaryInterval = d
	col := &syncRecordCollector{}
	prevLog := slog.Default()
	slog.SetDefault(slog.New(col))
	t.Cleanup(func() {
		ccSummaryInterval = prevInterval
		slog.SetDefault(prevLog)
	})
	return col
}

func recAttr(r slog.Record, key string) (string, bool) {
	var v string
	var ok bool
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == key {
			v, ok = a.Value.String(), true
			return false
		}
		return true
	})
	return v, ok
}

func waitForFinal(t *testing.T, col *syncRecordCollector) []slog.Record {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		recs := col.byMessage("DIAG: cc-summary")
		for _, r := range recs {
			if f, _ := recAttr(r, "final"); f == "true" {
				return recs
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("no final cc-summary line within 3s; got %d lines", len(col.byMessage("DIAG: cc-summary")))
	return nil
}

func TestCCSummaryLoop_PeriodicThenFinal(t *testing.T) {
	col := withShortCCSummaryInterval(t, 50*time.Millisecond)
	const payload = 4 << 20 // 128 chunks × 5 ms ≈ 650 ms of paced ACK flow
	cfg, cleanup := ccSummaryTestServer(t, payload)
	defer cleanup()

	client := NewQUICClient(cfg, nil)
	client.tlsConfig = buildInsecureTLSConfig()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := client.DialTCP(ctx, "example.com:80", nil)
	if err != nil {
		t.Fatal(err)
	}
	n, err := io.Copy(io.Discard, conn)
	if err != nil || n != payload {
		t.Fatalf("download: n=%d err=%v", n, err)
	}
	conn.Close()
	// Keep the QUIC connection alive across several ticks so at least one
	// periodic line lands after the MIs completed.
	time.Sleep(4 * ccSummaryInterval)
	client.Close()

	recs := waitForFinal(t, col)
	var finals, periodic int
	prevSeq := 0
	for _, r := range recs {
		seqS, _ := recAttr(r, "seq")
		seq, _ := strconv.Atoi(seqS)
		if seq != prevSeq+1 {
			t.Errorf("seq not consecutive: got %d after %d", seq, prevSeq)
		}
		prevSeq = seq
		mis, _ := recAttr(r, "mis")
		app, _ := recAttr(r, "appLimited")
		ss, _ := recAttr(r, "slowStart")
		dec, _ := recAttr(r, "decision")
		ra, _ := recAttr(r, "rateAdj")
		m, _ := strconv.Atoi(mis)
		a, _ := strconv.Atoi(app)
		s1, _ := strconv.Atoi(ss)
		s2, _ := strconv.Atoi(dec)
		s3, _ := strconv.Atoi(ra)
		if a > m || s1+s2+s3 != m {
			t.Errorf("invariants: mis=%d appLimited=%d modes=%d+%d+%d", m, a, s1, s2, s3)
		}
		if f, _ := recAttr(r, "final"); f == "true" {
			finals++
			if udid, _ := recAttr(r, "udid"); udid != "good-device" {
				t.Errorf("final udid=%q want good-device", udid)
			}
		} else {
			periodic++
			if m == 0 {
				t.Error("periodic line with mis=0 must be skipped")
			}
		}
	}
	if periodic < 1 || finals != 1 {
		t.Fatalf("periodic=%d final=%d want ≥1 / exactly 1", periodic, finals)
	}
	if f, _ := recAttr(recs[len(recs)-1], "final"); f != "true" {
		t.Error("final line must be the last line for the connection")
	}
}

// Negative control: an authenticated connection that never completes an MI
// (a 5-byte echo is < minMIPackets ACKs) leaves no cc-summary line at all.
func TestCCSummaryLoop_SilentWithoutCompletedMI(t *testing.T) {
	col := withShortCCSummaryInterval(t, 50*time.Millisecond)
	cfg, cleanup := ccSummaryTestServer(t, 5)
	defer cleanup()

	client := NewQUICClient(cfg, nil)
	client.tlsConfig = buildInsecureTLSConfig()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := client.DialTCP(ctx, "example.com:80", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(io.Discard, conn); err != nil {
		t.Fatal(err)
	}
	conn.Close()
	time.Sleep(4 * ccSummaryInterval)
	client.Close()
	time.Sleep(6 * ccSummaryInterval)

	if recs := col.byMessage("DIAG: cc-summary"); len(recs) != 0 {
		t.Fatalf("expected no cc-summary lines for a connection with 0 completed MIs, got %d", len(recs))
	}
}
