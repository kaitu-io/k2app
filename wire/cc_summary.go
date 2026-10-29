package wire

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/kaitu-io/k2/wire/k2cc"
	"github.com/kaitu-io/kuic"
)

// ccSummaryInterval is the server-side cc-summary cadence. A var so tests can
// shorten it; there is deliberately no runtime knob (spec §2). NewQUICServer
// copies it, so the per-connection loops never read the var itself while a
// test restores it.
var ccSummaryInterval = 60 * time.Second

// ccSummaryLoop emits one "DIAG: cc-summary" INFO line per ccSummaryInterval
// for a server connection, plus a final=true line when the connection ends.
// It owns every piece of state (prev, seq, lastEmit, start) so the periodic
// and final lines can never double-count a window. It exits on done (closed
// by handleConn) or on conn.Context().Done() — the latter covers any
// handleConn exit path that never reaches close(state.done).
//
// Skip rule: a periodic line is skipped when the window completed no MI
// (prev/lastEmit are not advanced, so the next windowS spans the idle gap);
// the final line is skipped when the connection never completed an MI at
// all — rejected, scanner and auth-only connections leave no line.
func (s *QUICServer) ccSummaryLoop(log *slog.Logger, ctx *connContext, conn *quic.Conn, done <-chan struct{}) {
	cc := ctx.cc
	if cc == nil {
		return
	}
	start := time.Now()
	lastEmit := start
	var prev k2cc.CCSummary
	seq := 0

	emit := func(final bool) {
		cur := cc.Summary()
		win := cur.Sub(prev)
		if final {
			if cur.MIs == 0 {
				return
			}
		} else if win.MIs == 0 {
			return
		}
		now := time.Now()
		seq++
		log.Info("DIAG: cc-summary", ccSummaryAttrs(win, cur, seq, final,
			int(now.Sub(lastEmit).Seconds()), int(now.Sub(start).Seconds()), ctx.dev.get())...)
		prev = cur
		lastEmit = now
	}

	ticker := time.NewTicker(s.ccInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			emit(false)
		case <-done:
			emit(true)
			return
		case <-conn.Context().Done():
			emit(true)
			return
		}
	}
}

// ccSummaryAttrs renders one cc-summary window as slog key/value pairs. Pure.
// The KEY SET IS A CONTRACT: Phase 2 may append (variant=…) but never renames
// or drops a key; Center-side aggregation parses exactly these names.
//
// Units: *MB are MiB (same as the heartbeat's txMB/rxMB); loss*/bgLoss are
// percent; rate/tput/bwEst are Mbps; percentiles are bucket upper edges
// (">500" = open bucket); lossAgg is clamped to 100 like LossRate().
func ccSummaryAttrs(win, cur k2cc.CCSummary, seq int, final bool, windowS, uptimeS int, udid string) []any {
	lossAgg := 0.0
	if win.BytesSent > 0 {
		lossAgg = min(1.0, float64(win.BytesLost)/float64(win.BytesSent)) * 100
	}
	pct := func(edge float64, of bool) string { return k2cc.FormatBucket(edge, of) }
	variant := cur.Variant
	if variant == "" {
		variant = k2cc.VariantControl
	}
	const mb = 1024 * 1024
	return []any{
		"udid", udid,
		"seq", seq,
		"final", final,
		"windowS", windowS,
		"uptimeS", uptimeS,
		"mis", win.MIs,
		"appLimited", win.AppLimitedMIs,
		"bwAnchored", win.BWAnchoredMIs,
		"floor", win.FloorMIs,
		"slowStart", win.SlowStartMIs,
		"decision", win.DecisionMIs,
		"rateAdj", win.RateAdjMIs,
		"bwDrop", win.BWDrops,
		"bwRise", win.BWRises,
		"ssSat", win.SlowStartSaturations,
		"probes", win.RateProbes,
		"sentMB", fmt.Sprintf("%.1f", float64(win.BytesSent)/mb),
		"ackedMB", fmt.Sprintf("%.1f", float64(win.BytesAcked)/mb),
		"lossAgg", fmt.Sprintf("%.1f", lossAgg),
		"lossP50", pct(win.LossPercentile(0.5)),
		"lossP90", pct(win.LossPercentile(0.9)),
		"lossMax", fmt.Sprintf("%.1f", win.LossMax*100),
		"rttP50", pct(win.RTTPercentile(0.5)),
		"rttP90", pct(win.RTTPercentile(0.9)),
		"minRttMs", int(cur.MinRTT / time.Millisecond),
		"rateP50", pct(win.RatePercentile(0.5)),
		"tputP50", pct(win.TputPercentile(0.5)),
		"tputMax", fmt.Sprintf("%.1f", win.TputMax/1e6),
		"bwEst", fmt.Sprintf("%.1f", cur.BWEst/1e6),
		"bgLoss", fmt.Sprintf("%.1f", cur.BGLoss*100),
		"epsilon", fmt.Sprintf("%.3f", cur.Epsilon),
		"mode", cur.Mode.String(),
		// Appended 2026-10 (loss-attribution A/B). variant is the arm;
		// declLoss is the loss QUIC declared in the window (event-time, the
		// same definition in every arm — compare arms on THIS, not lossAgg,
		// which is what the arm's controller saw); staleLoss is the share of
		// declared loss bytes the stale filter dropped; resolved is the share
		// of sent bytes whose fate was known when their MI was judged;
		// loss{Cruise,Probe,Drain} are own-packet loss of the MIs that RAN at
		// that BW-anchored gain ("-" = no such MI resolved a packet).
		"variant", variant,
		"declLoss", fmt.Sprintf("%.1f", win.DeclaredLoss()*100),
		"staleLoss", fmt.Sprintf("%.0f", ratioPct(win.StaleLostBytes, win.DeclaredLostBytes)),
		"resolved", fmt.Sprintf("%.0f", ratioPct(win.ResolvedBytes, win.BytesSent)),
		"lossCruise", phasePct(win, 1),
		"lossProbe", phasePct(win, 2),
		"lossDrain", phasePct(win, 3),
	}
}

func ratioPct(a, b uint64) float64 {
	if b == 0 {
		return 0
	}
	return min(1.0, float64(a)/float64(b)) * 100
}

func phasePct(win k2cc.CCSummary, phase int) string {
	loss, ok := win.PhaseLoss(phase)
	if !ok {
		return "-"
	}
	return fmt.Sprintf("%.1f", loss*100)
}
