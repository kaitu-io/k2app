package engine

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
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
//
// Counted from the EVIDENCE (wire.PathVerdict.EvidenceAt: the last time a
// session actually watched QUIC go bad), not from the Stop that saved it. A
// session that merely inherited the verdict cannot renew it — otherwise a
// user who reconnects every few minutes stays on tcpws for as long as they
// keep reconnecting, on a network nobody has re-measured.
const pathMemoryTTL = 10 * time.Minute

// pathMemoryStore carries the channel selector's verdict on the QUIC path
// from one Engine session to the next (wire.PathVerdict explains why).
// Process-scoped, with a one-file disk copy when the shell supplies a cache
// directory (bind): the iOS Network Extension process exits with the tunnel,
// and Android may kill the app process between two connects, so memory alone
// would lose the verdict exactly on the platforms it is for. The file holds
// three numbers and a timestamp — no addresses, no identifiers.
type pathMemoryStore struct {
	mu         deadlock.Mutex
	verdict    wire.PathVerdict
	evidenceAt time.Time // when QUIC was last observed bad; ages the memory
	valid      bool
	file       string // "" = memory only
}

const pathMemoryFileName = "path-memory.json"

type pathMemoryFile struct {
	ReviewAt     time.Time `json:"review_at"`
	FailedTrials int       `json:"failed_trials"`
	FlapCount    int       `json:"flap_count"`
	EvidenceAt   time.Time `json:"evidence_at"`
}

// bind points the store at dir and, if the process holds no verdict, adopts
// the one on disk. A file from the future (clock change) or one that does not
// parse is ignored; expiry is load's job. dir == "" leaves the store
// memory-only.
func (s *pathMemoryStore) bind(dir string, now time.Time) {
	if dir == "" {
		return
	}
	file := filepath.Join(dir, pathMemoryFileName)
	var disk pathMemoryFile
	haveDisk := false
	if b, err := os.ReadFile(file); err == nil {
		if json.Unmarshal(b, &disk) == nil && !disk.EvidenceAt.IsZero() && !disk.EvidenceAt.After(now) &&
			disk.FailedTrials >= 0 && disk.FlapCount >= 0 {
			haveDisk = true
		}
	}
	s.mu.Lock()
	s.file = file
	if !s.valid && haveDisk {
		s.verdict = wire.PathVerdict{ReviewAt: disk.ReviewAt, FailedTrials: disk.FailedTrials, FlapCount: disk.FlapCount, EvidenceAt: disk.EvidenceAt}
		s.evidenceAt = disk.EvidenceAt
		s.valid = true
	}
	s.mu.Unlock()
}

// persist mirrors the in-memory state to disk. Best effort: a failed write
// costs one session's memory, never a connect. File I/O outside s.mu.
func (s *pathMemoryStore) persist() {
	s.mu.Lock()
	file, valid := s.file, s.valid
	disk := pathMemoryFile{ReviewAt: s.verdict.ReviewAt, FailedTrials: s.verdict.FailedTrials,
		FlapCount: s.verdict.FlapCount, EvidenceAt: s.evidenceAt}
	s.mu.Unlock()
	if file == "" {
		return
	}
	if !valid {
		_ = os.Remove(file)
		return
	}
	b, err := json.Marshal(disk)
	if err != nil {
		return
	}
	tmp := file + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		slog.Debug("engine: path memory not persisted", "err", err)
		return
	}
	if err := os.Rename(tmp, file); err != nil {
		_ = os.Remove(tmp)
	}
}

var pathMemory pathMemoryStore

// record folds one finished session's outcome in. No opinion keeps whatever
// was known — a 45s session that never got as far as a switch says nothing.
func (s *pathMemoryStore) record(tm *wire.TransportManager, now time.Time) {
	v, outcome := tm.PathVerdict(now) // outside s.mu (concurrency rule #1)
	if outcome == wire.PathNoOpinion {
		return
	}
	s.mu.Lock()
	switch outcome {
	case wire.PathQUICBad:
		at := v.EvidenceAt
		if at.IsZero() || at.After(now) {
			at = now
		}
		v.EvidenceAt = at
		s.verdict, s.evidenceAt, s.valid = v, at, true
	case wire.PathQUICProven:
		s.valid = false
	}
	s.mu.Unlock()
	s.persist()
}

// load returns the verdict to seed a new session with, if one is still
// current. ReviewAt is capped at evidenceAt+pathMemoryTTL.
func (s *pathMemoryStore) load(now time.Time) (wire.PathVerdict, bool) {
	s.mu.Lock()
	v, evidenceAt, valid := s.verdict, s.evidenceAt, s.valid
	s.mu.Unlock()
	if !valid || now.Sub(evidenceAt) > pathMemoryTTL {
		return wire.PathVerdict{}, false
	}
	if limit := evidenceAt.Add(pathMemoryTTL); v.ReviewAt.After(limit) {
		v.ReviewAt = limit
	}
	return v, true
}

// clear forgets the verdict: the platform reported a network change.
func (s *pathMemoryStore) clear() {
	s.mu.Lock()
	s.valid = false
	s.mu.Unlock()
	s.persist()
}

// pathMemoryDir is where the disk copy lives: beside the rule cache
// (ClientConfig.CacheDir is the shell's cache dir + "/rules"), not inside it.
func pathMemoryDir(ruleCacheDir string) string {
	if ruleCacheDir == "" {
		return ""
	}
	return filepath.Dir(ruleCacheDir)
}

// applyPathMemory seeds freshly built outbound transports with the remembered
// verdict. Called once per Start, after the transport race. cacheDir is the
// client's cache directory ("" = memory only).
func applyPathMemory(ots []*outboundTransport, cacheDir string, now time.Time) {
	pathMemory.bind(cacheDir, now)
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
