package wire

import (
	"log/slog"
	"time"
)

// PathOutcome is what one TransportManager's selector concluded about the
// QUIC path by the time it is asked (PathVerdict).
type PathOutcome int

const (
	// PathNoOpinion: the selector never left QUIC for a quality or data-path
	// reason. Says nothing either way — whatever was known before still holds.
	PathNoOpinion PathOutcome = iota
	// PathQUICBad: tcpws is serving after a quality/data-path switch-away, or
	// QUIC is on a rollback trial that has not carried proven traffic.
	PathQUICBad
	// PathQUICProven: QUIC came back from a quality/data-path switch-away and
	// has since answered real requests across rollbackTrialWindow.
	PathQUICProven
)

// PathVerdict is the selector history worth carrying from one
// TransportManager to the next one built on the same network: until when
// tcpws should keep serving, and how far the rollback-trial escalation has
// got. It exists because the engine builds a fresh TransportManager on every
// Start, and a user on a path that passes the QUIC handshake but drops bulk
// UDP reconnects by hand long before the in-session escalation (30s → 600s →
// 1h) can protect them: ticket #4055 logged six connects in twenty minutes,
// each one starting on QUIC again while the previous session had run on
// tcpws for ninety-one minutes.
type PathVerdict struct {
	ReviewAt     time.Time
	FailedTrials int
	FlapCount    int
	// EvidenceAt is when QUIC was last observed bad (the quality/data-path
	// switch-away), carried unchanged through sessions that only inherit the
	// verdict. The engine ages the memory from it, so reconnecting cannot
	// renew a verdict that no session has re-observed.
	EvidenceAt time.Time
}

// PathVerdict reports what this TransportManager learned about the QUIC path.
// The verdict is meaningful only with PathQUICBad.
//
// Handshake-failure switch-aways are deliberately not evidence: three failed
// handshakes describe one server at least as often as they describe the
// network, and the next server's race measures that directly.
//
// A session that ends while QUIC is on an unproven rollback trial is graded
// as that trial failing (switchAway's own rule: a trial is graded by what
// QUIC carried, not by how long it sat). The user-visible shape is the
// ticket's first session: back on QUIC for two and a half minutes, nothing
// loaded, disconnect.
func (tm *TransportManager) PathVerdict(now time.Time) (PathVerdict, PathOutcome) {
	tm.mu.RLock()
	st := tm.selState
	tm.mu.RUnlock()

	if !st.pathEvidence {
		return PathVerdict{}, PathNoOpinion
	}
	if st.active != "tcpws" {
		if st.trialProven {
			return PathVerdict{}, PathQUICProven
		}
		st = switchAway(st, now)
	}
	return PathVerdict{
		ReviewAt:     st.nextReviewAt,
		FailedTrials: st.failedTrials,
		FlapCount:    st.flapCount,
		EvidenceAt:   st.pathEvidenceAt,
	}, PathQUICBad
}

// ApplyPathVerdict seeds a freshly built TransportManager with a verdict from
// an earlier one on the same network: tcpws serves, QUIC stays installed as
// the primary and comes back through the normal echo-verified rollback once
// v.ReviewAt has passed. Same topology as the race-lost install
// (SetFallbackActiveWithReason), with the review window and trial count
// carried instead of restarted. Reports whether it changed the serving
// channel.
//
// No-op when the primary is not a QUIC channel (tcpws won the race outright:
// there is nothing to hold back) and under K2_CHANNEL_SELECTOR_LEGACY.
func (tm *TransportManager) ApplyPathVerdict(v PathVerdict, now time.Time) bool {
	if tm.legacy {
		return false
	}
	tm.mu.Lock()
	if channelForType(tm.primaryType) != "quic" {
		tm.mu.Unlock()
		return false
	}
	switched := tm.selState.active != "tcpws"
	tm.selState.active = "tcpws"
	tm.selState.aboveSince = time.Time{}
	tm.selState.pathEvidence = true
	tm.selState.pathEvidenceAt = v.EvidenceAt
	if v.ReviewAt.After(tm.selState.nextReviewAt) {
		tm.selState.nextReviewAt = v.ReviewAt
	}
	tm.selState.failedTrials = max(tm.selState.failedTrials, v.FailedTrials)
	tm.selState.flapCount = max(tm.selState.flapCount, v.FlapCount)
	tm.mu.Unlock()

	if switched {
		reviewInS := max(int(v.ReviewAt.Sub(now).Seconds()), 0)
		slog.Warn("wire: starting on TCP-WS (path memory)",
			"failedTrials", v.FailedTrials, "nextReviewInS", reviewInS)
		slog.Warn("DIAG: transport-switch", "from", "quic", "to", "tcpws", "reason", "path-memory")
	}
	return switched
}
