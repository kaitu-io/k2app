package wire

import (
	"bytes"
	"log/slog"
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
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))

	r := NewRolloutStats()
	r.RecordConn()
	r.RecordCold()

	stop := make(chan struct{})
	done := make(chan struct{})
	go func() { r.Run(stop, 20*time.Millisecond, logger); close(done) }()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if bytes.Contains(buf.Bytes(), []byte("DIAG: auth-rollout")) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	close(stop)
	<-done

	out := buf.String()
	for _, want := range []string{"DIAG: auth-rollout", "total=1", "authed=1", "cookie_hit=0", "cold=1", "unauthed=0", "legacy_no_metadata=0"} {
		if !bytes.Contains([]byte(out), []byte(want)) {
			t.Fatalf("emitted line missing %q; got: %s", want, out)
		}
	}
}
