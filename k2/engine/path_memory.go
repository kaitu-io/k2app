package engine

import (
	"log/slog"
	"time"

	"github.com/kaitu-io/k2/wire"
	"github.com/sasha-s/go-deadlock"
)

// pathMemoryTTL bounds how long a QUIC-path verdict outlives the session
// that produced it. The memory is keyed by nothing: it describes "the network
// this process was last connected on", and the only thing that tells the
// engine the network changed is a platform event delivered WHILE connected
// (reconnect with preserveScoreboard=false clears it). A phone that
// disconnects on Wi-Fi and reconnects on cellular raises no such event, so a
// stale verdict can be applied to the wrong network — the TTL is what bounds
// that mistake: at most this long on tcpws (a working channel, not an
// outage), then the normal echo-verified rollback. Deliberately the
// selector's own first-failed-trial window rather than the 1h sticky one; a
// carried FailedTrials still lets a session that fails its trial escalate.
const pathMemoryTTL = 10 * time.Minute

// pathMemoryStore carries the channel selector's verdict on the QUIC path
// from one Engine session to the next (wire.PathVerdict explains why).
// Process-scoped and in-memory only: an iOS extension process recycled
// between two connects starts with none, which is the pre-existing behavior.
type pathMemoryStore struct {
	mu      deadlock.Mutex
	verdict wire.PathVerdict
	savedAt time.Time
	valid   bool
}

var pathMemory pathMemoryStore

// record folds one finished session's outcome in. No opinion keeps whatever
// was known — a 45s session that never got as far as a switch says nothing.
func (s *pathMemoryStore) record(tm *wire.TransportManager, now time.Time) {
	v, outcome := tm.PathVerdict(now) // outside s.mu (concurrency rule #1)
	s.mu.Lock()
	switch outcome {
	case wire.PathQUICBad:
		s.verdict, s.savedAt, s.valid = v, now, true
	case wire.PathQUICProven:
		s.valid = false
	}
	s.mu.Unlock()
}

// load returns the verdict to seed a new session with, if one is still
// current. ReviewAt is capped at savedAt+pathMemoryTTL.
func (s *pathMemoryStore) load(now time.Time) (wire.PathVerdict, bool) {
	s.mu.Lock()
	v, savedAt, valid := s.verdict, s.savedAt, s.valid
	s.mu.Unlock()
	if !valid || now.Sub(savedAt) > pathMemoryTTL {
		return wire.PathVerdict{}, false
	}
	if limit := savedAt.Add(pathMemoryTTL); v.ReviewAt.After(limit) {
		v.ReviewAt = limit
	}
	return v, true
}

// clear forgets the verdict: the platform reported a network change.
func (s *pathMemoryStore) clear() {
	s.mu.Lock()
	s.valid = false
	s.mu.Unlock()
}

// applyPathMemory seeds freshly built outbound transports with the remembered
// verdict. Called once per Start, after the transport race.
func applyPathMemory(ots []*outboundTransport, now time.Time) {
	v, ok := pathMemory.load(now)
	if !ok {
		return
	}
	for _, ot := range ots {
		if ot == nil {
			continue
		}
		if ot.tm.ApplyPathVerdict(v, now) {
			slog.Info("engine: path memory applied",
				"outbound", ot.name,
				"failedTrials", v.FailedTrials,
				"reviewInS", max(int(v.ReviewAt.Sub(now).Seconds()), 0),
			)
		}
	}
}
