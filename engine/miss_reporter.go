package engine

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/net/publicsuffix"

	"github.com/kaitu-io/k2/config"
	"github.com/kaitu-io/k2/rule"
	"github.com/kaitu-io/k2/safego"
)

// ------------------------------------------------------------------
// Phase 1 rule-miss telemetry — concrete MissReporter implementation.
//
// Design (see the Phase 1 brief for full context):
//
//   matchHostLocked ──▶ Engine.missReporter.Report(host, proto)
//                           │ non-blocking send
//                           ▼
//                      ringCh (cap 1024) ──▶ drainLoop goroutine
//                                                │
//                                                ▼
//                                 dedupe by (etld1, protocol)
//                                                │
//                                                ▼
//                                    hash with daily salt (UTC date)
//                                                │
//                                                ▼
//                              batch ≥200 records or every 1 hour
//                                                │
//                                                ▼
//                                      http.Client POST (HTTPS)
//                                                │
//                                                ▼
//                                     success → discard; fail → log
//
// Hot-path constraint: the Engine's rule.Engine only ever sees the
// concrete reporter as a rule.MissReporter interface pointer. When the
// user has telemetry disabled (ClientConfig.Telemetry.RuleMiss.Enabled
// is false, which is the zero-value default) the Engine never constructs
// a missReporter, so the rule.Engine's missReporter field stays nil and
// the Report call site short-circuits with zero allocation. This is
// verified by BenchmarkMatchHostLocked in k2/rule.
//
// Phase 1 deliberately does NOT persist to SQLite — records live only in
// the in-memory ring buffer and are lost on restart. Phase 2 will add
// the SQLite file buffer described in the telemetry design doc.
// ------------------------------------------------------------------

// missEvent is a single rule-miss observation flowing through the
// in-memory ring buffer between the match hot path and the drain loop.
type missEvent struct {
	host     string
	protocol string
}

// missReporter is the concrete engine-side implementation of
// rule.MissReporter. Non-blocking on the hot path (channel send with
// drop-on-full); all hashing, batching, and HTTP I/O happen on the
// background drain goroutine.
type missReporter struct {
	cfg     config.RuleMissConfig
	schemaV int
	client  *http.Client

	ch    chan missEvent
	stop  chan struct{}
	done  chan struct{}
	once  sync.Once
	drops atomic.Uint64 // hot-path drops (buffer full)

	// Test hooks — zero values are fine in production.
	nowFn        func() time.Time     // for fake clock; defaults to time.Now
	flushTrigger chan struct{}        // signals drain goroutine to flush immediately (tests)
	sendFn       func(batch ruleMissBatch) error // overrides HTTP POST in tests
}

// Phase 1 schema version: bumped only on wire-format breaking changes.
const phase1SchemaVersion = 1

// Ring buffer capacity — must hold bursts from the DNS hot path without
// back-pressure. 1024 was chosen so a lookup storm at ~1k qps leaves
// ≥1s of headroom for the drain loop.
const missRingCapacity = 1024

// Hot-path drop log is debounced to avoid log spam. Only every Nth drop
// is logged, and only at WARN level.
const missDropLogEvery = 256

// Batch trigger thresholds. See drainLoop for interaction with flushTrigger.
const (
	missBatchMaxRecords   = 200
	missBatchFlushInterval = 1 * time.Hour
)

// ruleMissRecord is one uploaded record (matches the Center wire format
// documented in api/telemetry.go RuleMissRecord).
type ruleMissRecord struct {
	Hash16     string `json:"hash16"`
	Country    string `json:"country"`
	WeekBucket string `json:"week_bucket"`
	Protocol   string `json:"protocol"`
	Revealed   string `json:"revealed,omitempty"`
}

// ruleMissBatch is the full POST body.
type ruleMissBatch struct {
	SchemaVersion int              `json:"schema_version"`
	ClientVersion string           `json:"client_version"`
	RulesVersion  string           `json:"rules_version"`
	SaltDay       string           `json:"salt_day"`
	Records       []ruleMissRecord `json:"records"`
}

// newMissReporter builds a reporter for the given rule-miss config. The
// caller is responsible for only invoking this when cfg.Enabled is true;
// if cfg.Enabled is false this function still works but the whole point
// of the "dark" Phase 1 is that we never get here on that path.
func newMissReporter(cfg config.RuleMissConfig) *missReporter {
	return &missReporter{
		cfg:     cfg,
		schemaV: phase1SchemaVersion,
		client: &http.Client{
			Timeout: 15 * time.Second,
		},
		ch:    make(chan missEvent, missRingCapacity),
		stop:  make(chan struct{}),
		done:  make(chan struct{}),
		nowFn: time.Now,
	}
}

// start launches the background drain goroutine. Idempotent via sync.Once.
func (r *missReporter) start() {
	r.once.Do(func() {
		safego.Go(func() { r.drainLoop() })
	})
}

// Report is the rule.MissReporter implementation. Non-blocking: drops the
// event if the ring buffer is full. Must stay allocation-free on the hot
// path — only two local variables (host, protocol) are captured into
// the struct, no goroutine spawn, no map access.
func (r *missReporter) Report(host, protocol string) {
	select {
	case r.ch <- missEvent{host: host, protocol: protocol}:
	default:
		n := r.drops.Add(1)
		if n%missDropLogEvery == 0 {
			slog.Warn("DIAG: rule-miss-drop", "totalDrops", n)
		}
	}
}

// Stop signals the drain goroutine to finish and blocks up to 5s for
// its final flush. Safe to call multiple times.
func (r *missReporter) Stop() {
	select {
	case <-r.stop:
		return
	default:
	}
	close(r.stop)
	select {
	case <-r.done:
	case <-time.After(5 * time.Second):
		slog.Warn("DIAG: rule-miss-stop-timeout")
	}
}

// ------------------------------------------------------------------
// Drain loop — consumes ring buffer, dedupes, hashes, batches, posts.
// ------------------------------------------------------------------

func (r *missReporter) drainLoop() {
	defer close(r.done)

	// Dedupe key = etld1 + "|" + protocol. Keeps the upload small while
	// preserving a signal per (domain, l4 protocol) pair.
	seen := make(map[string]ruleMissRecord, missBatchMaxRecords)
	timer := time.NewTimer(missBatchFlushInterval)
	defer timer.Stop()

	flush := func(reason string) {
		if len(seen) == 0 {
			return
		}
		records := make([]ruleMissRecord, 0, len(seen))
		for _, rec := range seen {
			records = append(records, rec)
		}
		seen = make(map[string]ruleMissRecord, missBatchMaxRecords)
		batch := ruleMissBatch{
			SchemaVersion: r.schemaV,
			ClientVersion: r.cfg.ClientVersion,
			SaltDay:       r.saltDay(),
			Records:       records,
		}
		if err := r.send(batch); err != nil {
			slog.Warn("DIAG: rule-miss-upload-fail",
				"reason", reason,
				"records", len(records),
				"err", err,
			)
			return
		}
		slog.Debug("DIAG: rule-miss-upload-ok",
			"reason", reason,
			"records", len(records),
		)
	}
	resetTimer := func() {
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(missBatchFlushInterval)
	}

	for {
		select {
		case ev, ok := <-r.ch:
			if !ok {
				flush("closed")
				return
			}
			r.record(seen, ev)
			if len(seen) >= missBatchMaxRecords {
				flush("size")
				resetTimer()
			}
		case <-timer.C:
			flush("interval")
			resetTimer()
		case <-r.flushTrigger:
			flush("trigger")
			resetTimer()
		case <-r.stop:
			flush("stop")
			return
		}
	}
}

// record hashes the event and stores it in the dedupe map, overwriting
// any existing entry with the same (etld1, protocol) key.
func (r *missReporter) record(seen map[string]ruleMissRecord, ev missEvent) {
	etld1 := extractETLDPlus1(ev.host)
	if etld1 == "" {
		return
	}
	key := etld1 + "|" + ev.protocol
	if _, exists := seen[key]; exists {
		return
	}
	salt := r.saltDay()
	seen[key] = ruleMissRecord{
		Hash16:     hashETLD1(salt, etld1),
		Country:    normalizeCountry(r.cfg.Country),
		WeekBucket: weekBucket(r.nowFn().UTC()),
		Protocol:   ev.protocol,
	}
}

// send issues the HTTP POST. Returns an error on any non-2xx response
// or network failure. Caller logs the error — no retry in Phase 1.
func (r *missReporter) send(batch ruleMissBatch) error {
	if r.sendFn != nil {
		return r.sendFn(batch)
	}
	if r.cfg.Endpoint == "" {
		return fmt.Errorf("rule-miss: empty endpoint")
	}
	body, err := json.Marshal(batch)
	if err != nil {
		return fmt.Errorf("rule-miss: marshal: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.cfg.Endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("rule-miss: new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := r.client.Do(req)
	if err != nil {
		return fmt.Errorf("rule-miss: post: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("rule-miss: http %d", resp.StatusCode)
	}
	return nil
}

// ------------------------------------------------------------------
// Hash / ETLD+1 helpers
// ------------------------------------------------------------------

// extractETLDPlus1 returns the effective top-level domain plus one label
// (e.g. "mail.google.com" → "google.com", "store.example.co.uk" →
// "example.co.uk"). Returns "" for inputs that don't have a registrable
// suffix (bare hosts, IPs, invalid domains).
func extractETLDPlus1(host string) string {
	// publicsuffix.EffectiveTLDPlusOne handles lowercasing internally via
	// the data tables being all lowercase — but we normalize anyway so
	// upper-case input hashes identically to lower-case.
	h := host
	// Strip trailing dot if present (FQDN normalization).
	if n := len(h); n > 0 && h[n-1] == '.' {
		h = h[:n-1]
	}
	etld1, err := publicsuffix.EffectiveTLDPlusOne(h)
	if err != nil {
		return ""
	}
	return etld1
}

// hashETLD1 returns the first 16 hex chars of sha256(salt || "|" || etld1).
// Stable and deterministic: same (salt, etld1) → same output.
func hashETLD1(salt, etld1 string) string {
	var buf bytes.Buffer
	buf.Grow(len(salt) + 1 + len(etld1))
	buf.WriteString(salt)
	buf.WriteByte('|')
	buf.WriteString(etld1)
	sum := sha256.Sum256(buf.Bytes())
	return hex.EncodeToString(sum[:])[:16]
}

// saltDay returns the current daily salt — Phase 1 uses the UTC date as
// the salt. Phase 2 will fetch a rotating salt from Center.
func (r *missReporter) saltDay() string {
	return r.nowFn().UTC().Format("2006-01-02")
}

// weekBucket returns an ISO-8601 week bucket like "2026-W15".
func weekBucket(t time.Time) string {
	year, week := t.ISOWeek()
	return fmt.Sprintf("%04d-W%02d", year, week)
}

// normalizeCountry returns a 2-letter uppercase alpha country code or "XX".
func normalizeCountry(cc string) string {
	if len(cc) != 2 {
		return "XX"
	}
	b := []byte(cc)
	for i, c := range b {
		if c >= 'a' && c <= 'z' {
			b[i] = c - 32
		} else if c < 'A' || c > 'Z' {
			return "XX"
		}
	}
	return string(b)
}

// Assert the concrete type satisfies the rule.MissReporter interface.
var _ rule.MissReporter = (*missReporter)(nil)
