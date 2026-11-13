package engine

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kaitu-io/k2/wire"
)

// quicPrimaryTM is a TransportManager with a QUIC primary and a tcpws backup.
func quicPrimaryTM() *wire.TransportManager {
	tm := wire.NewTransportManager()
	tm.SetPrimary(&statsEchoStub{}, "quic-443")
	tm.SetTCPWS(&statsEchoStub{})
	return tm
}

func TestPathMemory_CarriesVerdictToNextSession(t *testing.T) {
	resetPathMemoryForTest(t)
	now := time.Now()

	// Session 1 ends with a remembered verdict (as if tcpws was serving after
	// a quality switch-away; ApplyPathVerdict produces exactly that state).
	s1 := quicPrimaryTM()
	s1.ApplyPathVerdict(wire.PathVerdict{ReviewAt: now.Add(5 * time.Minute), FailedTrials: 1}, now)
	pathMemory.record(s1, now)

	// Session 2, one minute later, on a different server.
	s2 := &outboundTransport{tm: quicPrimaryTM(), name: "s2"}
	applyPathMemory([]*outboundTransport{s2, nil}, "", now.Add(time.Minute))
	if got := s2.tm.ActiveChannel(); got != "tcpws" {
		t.Fatalf("second session started on %s, want tcpws from memory", got)
	}

	// A short session that never switched has no opinion: memory survives it.
	s3 := quicPrimaryTM()
	pathMemory.record(s3, now.Add(2*time.Minute))
	if _, ok := pathMemory.load(now.Add(2 * time.Minute)); !ok {
		t.Fatal("a no-opinion session must not erase the verdict")
	}
}

func TestPathMemory_ExpiresAndCapsReview(t *testing.T) {
	resetPathMemoryForTest(t)
	now := time.Now()

	s1 := quicPrimaryTM()
	s1.ApplyPathVerdict(wire.PathVerdict{ReviewAt: now.Add(time.Hour), FailedTrials: 2}, now)
	pathMemory.record(s1, now)

	v, ok := pathMemory.load(now.Add(time.Minute))
	if !ok {
		t.Fatal("verdict should be current one minute later")
	}
	if limit := now.Add(pathMemoryTTL); v.ReviewAt.After(limit) {
		t.Fatalf("ReviewAt %v exceeds the TTL cap %v — a verdict applied to the wrong network would hold for an hour", v.ReviewAt, limit)
	}
	if _, ok := pathMemory.load(now.Add(pathMemoryTTL + time.Second)); ok {
		t.Fatal("verdict must expire after pathMemoryTTL")
	}
}

func TestPathMemory_ClearedOnNetworkChange(t *testing.T) {
	resetPathMemoryForTest(t)
	now := time.Now()

	s1 := quicPrimaryTM()
	s1.ApplyPathVerdict(wire.PathVerdict{ReviewAt: now.Add(5 * time.Minute)}, now)
	pathMemory.record(s1, now)
	pathMemory.clear()

	s2 := &outboundTransport{tm: quicPrimaryTM(), name: "s2"}
	applyPathMemory([]*outboundTransport{s2}, "", now.Add(time.Second))
	if got := s2.tm.ActiveChannel(); got != "quic" {
		t.Fatalf("after a network change the session must start on quic, got %s", got)
	}
}

// resetPathMemoryForTest returns the package-level store to its zero state
// (including the bound file) and restores that on cleanup.
func resetPathMemoryForTest(t *testing.T) {
	t.Helper()
	zero := func() {
		pathMemory.mu.Lock()
		pathMemory.valid, pathMemory.file = false, ""
		pathMemory.mu.Unlock()
	}
	zero()
	t.Cleanup(zero)
}

// simulateProcessRestart drops everything the process held; the disk copy is
// all that is left (iOS: the extension exits with the tunnel).
func simulateProcessRestart() {
	pathMemory.mu.Lock()
	pathMemory.valid, pathMemory.file = false, ""
	pathMemory.verdict, pathMemory.savedAt = wire.PathVerdict{}, time.Time{}
	pathMemory.mu.Unlock()
}

func TestPathMemory_SurvivesProcessRestartViaDisk(t *testing.T) {
	resetPathMemoryForTest(t)
	dir := t.TempDir()
	now := time.Now()

	// Session 1 in process A: bound to the cache dir, ends with a bad verdict.
	applyPathMemory(nil, dir, now)
	s1 := quicPrimaryTM()
	s1.ApplyPathVerdict(wire.PathVerdict{ReviewAt: now.Add(5 * time.Minute), FailedTrials: 1, FlapCount: 3}, now)
	pathMemory.record(s1, now)
	if _, err := os.Stat(filepath.Join(dir, pathMemoryFileName)); err != nil {
		t.Fatalf("verdict not written to disk: %v", err)
	}

	simulateProcessRestart()

	// Session 2 in process B, one minute later.
	s2 := &outboundTransport{tm: quicPrimaryTM(), name: "s2"}
	applyPathMemory([]*outboundTransport{s2}, dir, now.Add(time.Minute))
	if got := s2.tm.ActiveChannel(); got != "tcpws" {
		t.Fatalf("after a process restart the session started on %s, want tcpws from the disk copy", got)
	}
	if v, _ := s2.tm.PathVerdict(now.Add(time.Minute)); v.FailedTrials != 1 || v.FlapCount != 3 {
		t.Fatalf("carried counts lost across the restart: %+v", v)
	}
}

func TestPathMemory_DiskCopyFollowsClearAndProven(t *testing.T) {
	resetPathMemoryForTest(t)
	dir := t.TempDir()
	file := filepath.Join(dir, pathMemoryFileName)
	now := time.Now()

	applyPathMemory(nil, dir, now)
	s1 := quicPrimaryTM()
	s1.ApplyPathVerdict(wire.PathVerdict{ReviewAt: now.Add(5 * time.Minute)}, now)
	pathMemory.record(s1, now)

	pathMemory.clear() // network change
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatalf("a network change must remove the disk copy too (stat err=%v) — a restart would resurrect the verdict", err)
	}
	simulateProcessRestart()
	s2 := &outboundTransport{tm: quicPrimaryTM(), name: "s2"}
	applyPathMemory([]*outboundTransport{s2}, dir, now.Add(time.Second))
	if got := s2.tm.ActiveChannel(); got != "quic" {
		t.Fatalf("cleared memory came back after a restart: active=%s", got)
	}
}

func TestPathMemory_BadDiskCopyIsIgnored(t *testing.T) {
	now := time.Now()
	cases := map[string]string{
		"garbage":     "{not json",
		"empty":       "",
		"from future": `{"review_at":"2999-01-01T00:00:00Z","failed_trials":1,"flap_count":1,"saved_at":"2999-01-01T00:00:00Z"}`,
		"negative":    `{"review_at":"2020-01-01T00:00:00Z","failed_trials":-5,"flap_count":1,"saved_at":"2020-01-01T00:00:00Z"}`,
		"expired":     `{"review_at":"2020-01-01T00:10:00Z","failed_trials":1,"flap_count":1,"saved_at":"2020-01-01T00:00:00Z"}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			resetPathMemoryForTest(t)
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, pathMemoryFileName), []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			s := &outboundTransport{tm: quicPrimaryTM(), name: "s"}
			applyPathMemory([]*outboundTransport{s}, dir, now)
			if got := s.tm.ActiveChannel(); got != "quic" {
				t.Fatalf("a %s disk copy moved the session to %s", name, got)
			}
		})
	}
}

func TestEngine_NetworkChangeClearsPathMemory_HealthReconnectKeepsIt(t *testing.T) {
	resetPathMemoryForTest(t)
	now := time.Now()
	seed := func() {
		s := quicPrimaryTM()
		s.ApplyPathVerdict(wire.PathVerdict{ReviewAt: now.Add(5 * time.Minute)}, now)
		pathMemory.record(s, now)
	}
	connected := func() *Engine {
		e := New()
		e.mu.Lock()
		e.state = StateConnected
		e.primaryTM = &outboundTransport{tm: wire.NewTransportManager()}
		e.outboundTMs = []*outboundTransport{e.primaryTM}
		e.mu.Unlock()
		return e
	}

	seed()
	connected().reconnect(true) // health-critical: same path
	if _, ok := pathMemory.load(now); !ok {
		t.Fatal("a same-path reconnect must keep the verdict")
	}

	connected().doNetworkReconnect() // platform network change
	if _, ok := pathMemory.load(now); ok {
		t.Fatal("a network change must forget the verdict — it described the old network")
	}
}

func TestEngine_StopRecordsPathMemory(t *testing.T) {
	resetPathMemoryForTest(t)
	now := time.Now()
	tm := quicPrimaryTM()
	tm.ApplyPathVerdict(wire.PathVerdict{ReviewAt: now.Add(5 * time.Minute), FailedTrials: 1}, now)

	e := New()
	e.mu.Lock()
	e.state = StateConnected
	e.primaryTM = &outboundTransport{tm: tm}
	e.outboundTMs = []*outboundTransport{e.primaryTM}
	e.mu.Unlock()

	if err := e.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	v, ok := pathMemory.load(time.Now())
	if !ok || v.FailedTrials != 1 {
		t.Fatalf("Stop did not hand the session's verdict to the next one: ok=%v %+v", ok, v)
	}
}
