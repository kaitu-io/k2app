package wire

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/kaitu-io/k2/wire/k2cc"
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
