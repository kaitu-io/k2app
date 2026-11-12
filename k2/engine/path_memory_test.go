package engine

import (
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
	t.Cleanup(pathMemory.clear)
	pathMemory.clear()
	now := time.Now()

	// Session 1 ends with a remembered verdict (as if tcpws was serving after
	// a quality switch-away; ApplyPathVerdict produces exactly that state).
	s1 := quicPrimaryTM()
	s1.ApplyPathVerdict(wire.PathVerdict{ReviewAt: now.Add(5 * time.Minute), FailedTrials: 1}, now)
	pathMemory.record(s1, now)

	// Session 2, one minute later, on a different server.
	s2 := &outboundTransport{tm: quicPrimaryTM(), name: "s2"}
	applyPathMemory([]*outboundTransport{s2, nil}, now.Add(time.Minute))
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
	t.Cleanup(pathMemory.clear)
	pathMemory.clear()
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
	t.Cleanup(pathMemory.clear)
	pathMemory.clear()
	now := time.Now()

	s1 := quicPrimaryTM()
	s1.ApplyPathVerdict(wire.PathVerdict{ReviewAt: now.Add(5 * time.Minute)}, now)
	pathMemory.record(s1, now)
	pathMemory.clear()

	s2 := &outboundTransport{tm: quicPrimaryTM(), name: "s2"}
	applyPathMemory([]*outboundTransport{s2}, now.Add(time.Second))
	if got := s2.tm.ActiveChannel(); got != "quic" {
		t.Fatalf("after a network change the session must start on quic, got %s", got)
	}
}
