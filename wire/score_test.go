package wire

import (
	"testing"
	"time"
)

func TestChannelScore_NeutralWhenNoData(t *testing.T) {
	s := NewChannelScore()
	now := time.Unix(1000, 0)
	if got := s.Value(now); got != scoreNeutral*scoreNeutral {
		t.Fatalf("no-data score = %v, want %v", got, scoreNeutral*scoreNeutral)
	}
}

func TestChannelScore_FailFallsFasterThanRise(t *testing.T) {
	s := NewChannelScore()
	base := time.Unix(1000, 0)
	// Prime to full availability.
	for i := 0; i < 20; i++ {
		s.RecordAvailability(true, base.Add(time.Duration(i)*time.Second))
		s.RecordQuality(1.0, base.Add(time.Duration(i)*time.Second))
	}
	full := s.Value(base.Add(20 * time.Second))
	// One failure.
	s.RecordAvailability(false, base.Add(21*time.Second))
	afterFail := s.Value(base.Add(21 * time.Second))
	// One success after the failure.
	s.RecordAvailability(true, base.Add(22*time.Second))
	afterRecover := s.Value(base.Add(22 * time.Second))
	dropFromFail := full - afterFail
	riseFromRecover := afterRecover - afterFail
	if dropFromFail <= riseFromRecover {
		t.Fatalf("fail should move more than recover: drop=%v rise=%v", dropFromFail, riseFromRecover)
	}
}

func TestChannelScore_StaleRevertsToNeutral(t *testing.T) {
	s := NewChannelScore()
	s.SetProbeInterval(2 * time.Second)
	base := time.Unix(1000, 0)
	s.RecordAvailability(true, base)
	s.RecordQuality(1.0, base)
	fresh := s.Value(base)
	// 3×interval + 1s later → stale → neutral.
	stale := s.Value(base.Add(scoreStaleMult*2*time.Second + time.Second))
	if fresh <= stale {
		t.Fatalf("expected fresh(%v) > stale(%v)", fresh, stale)
	}
	if stale != scoreNeutral*scoreNeutral {
		t.Fatalf("stale score = %v, want neutral² = %v", stale, scoreNeutral*scoreNeutral)
	}
}

func TestQualityFromLossRTT_HalfDeadDrops(t *testing.T) {
	healthy := QualityFromLossRTT(0.0, 20*time.Millisecond, 20*time.Millisecond)
	halfDead := QualityFromLossRTT(0.3, 200*time.Millisecond, 20*time.Millisecond)
	if healthy <= halfDead {
		t.Fatalf("healthy(%v) must exceed half-dead(%v)", healthy, halfDead)
	}
	if healthy < 0.99 {
		t.Fatalf("healthy quality should be ~1.0, got %v", healthy)
	}
}

func TestQualityFromEchoRTT_InflationDrops(t *testing.T) {
	good := QualityFromEchoRTT(30*time.Millisecond, 30*time.Millisecond)
	inflated := QualityFromEchoRTT(300*time.Millisecond, 30*time.Millisecond)
	if good <= inflated {
		t.Fatalf("good(%v) must exceed inflated(%v)", good, inflated)
	}
}
