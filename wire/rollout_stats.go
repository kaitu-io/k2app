package wire

import (
	"log/slog"
	"sync/atomic"
	"time"
)

// RolloutStats — Phase 3 enforce-rollout observability (spec §7.2).
//
// Per-connection auth outcomes are counted at the admission decision points on
// BOTH transports and summarised as one INFO line every RolloutReportInterval:
//
//	DIAG: auth-rollout  total=N authed=N cookie_hit=N cold=N unauthed=N legacy_no_metadata=N
//
// This is a periodic heartbeat-style summary (never per-connection). Counters
// are window deltas: each emission snapshots-and-resets, so aggregating over a
// day is a plain sum over lines. All methods are nil-receiver safe so tests
// and callers without stats never crash.
//
// Field semantics:
//	total               connections accepted (QUIC handleConn + TCP-WS handleSession)
//	cookie_hit          metadata verdict OK via local cookie verification (zero Center)
//	cold                metadata verdict OK via validator chain (users_file/Center)
//	authed              cookie_hit + cold (derived at emit time)
//	unauthed            metadata presented but verdict failed (permissive: still served; enforce: rejected) — the core rollback signal during enforce
//	legacy_no_metadata  connection closed without ever presenting a metadata stream
type RolloutStats struct {
	total, cookieHit, cold, unauthed, legacy atomic.Int64
}

// RolloutReportInterval is the DIAG: auth-rollout emission period.
const RolloutReportInterval = 5 * time.Minute

// NewRolloutStats creates an empty counter set.
func NewRolloutStats() *RolloutStats { return &RolloutStats{} }

// RecordConn counts an accepted wire connection (either transport).
func (r *RolloutStats) RecordConn() {
	if r == nil {
		return
	}
	r.total.Add(1)
}

// RecordCookieHit counts a metadata verdict satisfied by local cookie verification.
func (r *RolloutStats) RecordCookieHit() {
	if r == nil {
		return
	}
	r.cookieHit.Add(1)
}

// RecordCold counts a metadata verdict satisfied by the validator chain.
func (r *RolloutStats) RecordCold() {
	if r == nil {
		return
	}
	r.cold.Add(1)
}

// RecordUnauthed counts a presented-but-failed metadata verdict.
func (r *RolloutStats) RecordUnauthed() {
	if r == nil {
		return
	}
	r.unauthed.Add(1)
}

// RecordLegacyNoMetadata counts a connection that closed without ever sending
// a metadata stream — the pre-metadata legacy client population.
func (r *RolloutStats) RecordLegacyNoMetadata() {
	if r == nil {
		return
	}
	r.legacy.Add(1)
}

// recordVerdict is the ONLY mapping from an admission verdict to counters.
// Both transports' handleMetadataStream call it so the two cannot drift.
func recordVerdict(r *RolloutStats, viaCookie, ok bool) {
	switch {
	case ok && viaCookie:
		r.RecordCookieHit()
	case ok:
		r.RecordCold()
	default:
		r.RecordUnauthed()
	}
}

type rolloutSnapshot struct {
	total, cookieHit, cold, unauthed, legacy int64
}

// snapshotAndReset atomically drains the window counters.
func (r *RolloutStats) snapshotAndReset() rolloutSnapshot {
	return rolloutSnapshot{
		total:     r.total.Swap(0),
		cookieHit: r.cookieHit.Swap(0),
		cold:      r.cold.Swap(0),
		unauthed:  r.unauthed.Swap(0),
		legacy:    r.legacy.Swap(0),
	}
}

// Run emits one DIAG: auth-rollout line per interval until stop closes.
// Emits even when total==0 — a silent reporter is indistinguishable from a
// dead one, and ops aggregation needs proof of life. log==nil → slog.Default().
func (r *RolloutStats) Run(stop <-chan struct{}, interval time.Duration, log *slog.Logger) {
	if r == nil {
		return
	}
	if log == nil {
		log = slog.Default()
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			s := r.snapshotAndReset()
			log.Info("DIAG: auth-rollout",
				"total", s.total,
				"authed", s.cookieHit+s.cold,
				"cookie_hit", s.cookieHit,
				"cold", s.cold,
				"unauthed", s.unauthed,
				"legacy_no_metadata", s.legacy,
			)
		}
	}
}
