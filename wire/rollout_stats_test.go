package wire

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"
)

func TestRolloutStats_RecordAndSnapshotReset(t *testing.T) {
	r := NewRolloutStats()
	r.RecordConn()
	r.RecordConn()
	r.RecordCookieHit()
	r.RecordCold()
	r.RecordUnauthed()
	r.RecordLegacyNoMetadata()

	s := r.snapshotAndReset()
	if s.total != 2 || s.cookieHit != 1 || s.cold != 1 || s.unauthed != 1 || s.legacy != 1 {
		t.Fatalf("snapshot = %+v, want total=2 cookieHit=1 cold=1 unauthed=1 legacy=1", s)
	}
	// reset semantics: second snapshot is all-zero
	s2 := r.snapshotAndReset()
	if s2.total != 0 || s2.cookieHit != 0 || s2.cold != 0 || s2.unauthed != 0 || s2.legacy != 0 {
		t.Fatalf("second snapshot = %+v, want all zero", s2)
	}
}

func TestRolloutStats_NilReceiverSafe(t *testing.T) {
	var r *RolloutStats
	// must not panic
	r.RecordConn()
	r.RecordCookieHit()
	r.RecordCold()
	r.RecordUnauthed()
	r.RecordLegacyNoMetadata()

	stop := make(chan struct{})
	close(stop)
	r.Run(stop, time.Millisecond, nil) // must return immediately, not panic
}

func TestRecordVerdict_Mapping(t *testing.T) {
	cases := []struct {
		name              string
		viaCookie, ok     bool
		cookieHit, cold, unauthed int64
	}{
		{"cookie-ok", true, true, 1, 0, 0},
		{"cold-ok", false, true, 0, 1, 0},
		{"cookie-presented-but-verdict-fail", true, false, 0, 0, 1},
		{"cold-fail", false, false, 0, 0, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := NewRolloutStats()
			recordVerdict(r, tc.viaCookie, tc.ok)
			s := r.snapshotAndReset()
			if s.cookieHit != tc.cookieHit || s.cold != tc.cold || s.unauthed != tc.unauthed {
				t.Fatalf("got cookieHit=%d cold=%d unauthed=%d, want %d/%d/%d",
					s.cookieHit, s.cold, s.unauthed, tc.cookieHit, tc.cold, tc.unauthed)
			}
		})
	}
}

func TestRolloutStats_RunEmitsDIAGLine(t *testing.T) {
	// recordChan synchronizes between Run() goroutine and test goroutine.
	// The custom handler sends each log record down the channel.
	recordChan := make(chan *slog.Record, 1)
	customHandler := &testLogHandler{recordChan: recordChan}
	logger := slog.New(customHandler)

	r := NewRolloutStats()
	r.RecordConn()
	r.RecordCold()

	stop := make(chan struct{})
	done := make(chan struct{})
	go func() { r.Run(stop, 20*time.Millisecond, logger); close(done) }()

	// Wait for the DIAG: auth-rollout record to arrive on the channel.
	deadline := time.Now().Add(2 * time.Second)
	var record *slog.Record
	for {
		select {
		case rec := <-recordChan:
			if rec.Message == "DIAG: auth-rollout" {
				record = rec
				goto foundRecord
			}
		case <-time.After(time.Until(deadline)):
			t.Fatal("timeout waiting for DIAG: auth-rollout record")
		}
	}

foundRecord:
	close(stop)
	<-done

	// Verify all required fields are present in the record.
	attrs := make(map[string]int64)
	record.Attrs(func(a slog.Attr) bool {
		if v, ok := a.Value.Any().(int64); ok {
			attrs[a.Key] = v
		}
		return true
	})

	expectedFields := map[string]int64{
		"total":                  1,
		"authed":                 1,
		"cookie_hit":             0,
		"cold":                   1,
		"unauthed":               0,
		"legacy_no_metadata":     0,
	}

	for key, expectedVal := range expectedFields {
		if actualVal, ok := attrs[key]; !ok {
			t.Fatalf("missing field %q in log record", key)
		} else if actualVal != expectedVal {
			t.Fatalf("field %q = %d, want %d", key, actualVal, expectedVal)
		}
	}
}

func TestConnAuth_Settled(t *testing.T) {
	a := newConnAuth()
	if a.settled() {
		t.Fatal("fresh connAuth must not be settled")
	}
	a.settle(true)
	if !a.settled() {
		t.Fatal("connAuth must be settled after settle()")
	}
}

// TestRolloutStats_CallSitesPresent is a source-scan tripwire in the spirit of
// TestAdmissionGate_EgressSinksAreGated: the counters only mean anything if
// BOTH transports feed them at the same three points. Deleting any hook goes
// red here (mutation-verified).
func TestRolloutStats_CallSitesPresent(t *testing.T) {
	cases := []struct {
		file  string
		funcs map[string][]string // function name → required call tokens inside its body
	}{
		{"quic.go", map[string][]string{
			"func (s *QUICServer) handleConn":           {".RecordConn()", ".RecordLegacyNoMetadata()", ".settled()", ".claimRolloutCount()"},
			"func (s *QUICServer) handleMetadataStream": {"recordVerdict(", ".claimRolloutCount()"},
		}},
		{"tcpws.go", map[string][]string{
			"func (s *TCPWSServer) handleSession":        {".RecordConn()", ".RecordLegacyNoMetadata()", ".settled()", ".claimRolloutCount()"},
			"func (s *TCPWSServer) handleMetadataStream": {"recordVerdict(", ".claimRolloutCount()"},
		}},
	}
	for _, tc := range cases {
		src, err := os.ReadFile(tc.file)
		if err != nil {
			t.Fatalf("read %s: %v", tc.file, err)
		}
		text := string(src)
		for fn, tokens := range tc.funcs {
			start := strings.Index(text, fn)
			if start < 0 {
				t.Fatalf("%s: function %q not found", tc.file, fn)
			}
			// body ends at the next top-level func declaration
			end := strings.Index(text[start+len(fn):], "\nfunc ")
			body := text[start:]
			if end >= 0 {
				body = text[start : start+len(fn)+end]
			}
			for _, tok := range tokens {
				if !strings.Contains(body, tok) {
					t.Errorf("%s %s: missing required call %q", tc.file, fn, tok)
				}
			}
		}
	}
}

// testLogHandler is a custom slog.Handler that sends records down a channel.
// Used to synchronize test goroutines without concurrent buffer access.
type testLogHandler struct {
	recordChan chan<- *slog.Record
}

func (h *testLogHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return level >= slog.LevelInfo
}

func (h *testLogHandler) Handle(ctx context.Context, record slog.Record) error {
	// Send a copy of the record down the channel for the test to read.
	// Non-blocking send to avoid deadlock if channel is full.
	select {
	case h.recordChan <- &record:
	default:
	}
	return nil
}

func (h *testLogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return h
}

func (h *testLogHandler) WithGroup(name string) slog.Handler {
	return h
}
