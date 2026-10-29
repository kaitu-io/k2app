package wire

import (
	"time"

	"github.com/sasha-s/go-deadlock"
)

const (
	scoreAlpha      = 0.3 // EWMA weight for a new sample (matches engine ewmaAlpha)
	scoreFailWeight = 2.0 // failure samples move availability this much faster ("fall fast, rise slow")
	scoreNeutral    = 0.5 // absent/stale signal — neither healthy nor faulty (#3463 fail-safe)
	scoreStaleMult  = 3   // a signal older than this × probeInterval reverts to neutral
)

// defaultProbeInterval is used for staleness when no cadence has been set.
const defaultProbeInterval = 16 * time.Second

// freshSampleRing is how many recent availability-sample timestamps a
// ChannelScore remembers for FreshSamples. It only has to cover the
// selector's backupFreshSamplesGate (2) with some slack; older samples are
// by definition not "fresh" and are overwritten.
const freshSampleRing = 4

// ChannelScore is a per-transport quality signal in [0,1]. Two EWMA-smoothed
// components — availability (reachability) and quality (loss/RTT) — are each
// gated by recency: a component with no data, or data older than
// scoreStaleMult × probeInterval, contributes the neutral 0.5 instead of a
// stale value. Value = availability × quality. Pure in-memory; all methods
// are microsecond critical sections (no I/O). See score design in spec §3.1.
type ChannelScore struct {
	mu deadlock.Mutex

	avail     float64
	availInit bool
	availAt   time.Time

	quality     float64
	qualityInit bool
	qualityAt   time.Time

	// recentAt is a ring of the last freshSampleRing availability-sample
	// timestamps (zero = empty slot), read by FreshSamples. Availability only:
	// a quality sample (RecordQuality) is derived from traffic that happened
	// to flow, not proof that the channel was deliberately probed.
	recentAt  [freshSampleRing]time.Time
	recentIdx int

	probeInterval time.Duration
}

func NewChannelScore() *ChannelScore {
	return &ChannelScore{probeInterval: defaultProbeInterval}
}

// RecordAvailability feeds a reachability sample (dial/echo success or failure).
// Failures move the EWMA scoreFailWeight× faster than successes.
func (s *ChannelScore) RecordAvailability(success bool, now time.Time) {
	sample := 0.0
	alpha := scoreAlpha
	if success {
		sample = 1.0
	} else {
		alpha = clamp01(scoreAlpha * scoreFailWeight)
	}
	s.mu.Lock()
	if !s.availInit {
		s.avail = sample
		s.availInit = true
	} else {
		s.avail = alpha*sample + (1-alpha)*s.avail
	}
	s.availAt = now
	s.recentAt[s.recentIdx] = now
	s.recentIdx = (s.recentIdx + 1) % freshSampleRing
	s.mu.Unlock()
}

// FreshSamples returns how many availability samples were recorded within
// the same staleness window Value() applies (scoreStaleMult × probeInterval),
// capped at freshSampleRing. This is the "has this channel actually been
// measured recently" evidence the selector's quality path demands before it
// may switch TO this channel — windowed, so a long-lived process cannot keep
// that gate satisfied with samples from hours ago (the old since-Reset
// counter did exactly that).
func (s *ChannelScore) FreshSamples(now time.Time) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	window := time.Duration(scoreStaleMult) * s.probeInterval
	n := 0
	for _, at := range s.recentAt {
		if !at.IsZero() && now.Sub(at) <= window {
			n++
		}
	}
	return n
}

// RecordQuality feeds a pre-computed instantaneous quality in [0,1].
func (s *ChannelScore) RecordQuality(q float64, now time.Time) {
	q = clamp01(q)
	s.mu.Lock()
	if !s.qualityInit {
		s.quality = q
		s.qualityInit = true
	} else {
		s.quality = scoreAlpha*q + (1-scoreAlpha)*s.quality
	}
	s.qualityAt = now
	s.mu.Unlock()
}

// Reset clears both EWMA components back to "no data yet" — the state a
// freshly constructed ChannelScore is in, where Value() returns
// neutral×neutral. Called by TransportManager.Reset on a network change:
// every sample in here was measured on the OLD network path and says nothing
// about the new one (spec §6, "网络切换 Reset"). probeInterval is deliberately
// preserved: it describes the cadence the probe loops are currently running
// at, which a network change does not alter.
func (s *ChannelScore) Reset() {
	s.mu.Lock()
	s.avail, s.availInit, s.availAt = 0, false, time.Time{}
	s.quality, s.qualityInit, s.qualityAt = 0, false, time.Time{}
	s.recentAt, s.recentIdx = [freshSampleRing]time.Time{}, 0
	s.mu.Unlock()
}

// SetProbeInterval updates the cadence used for staleness gating.
func (s *ChannelScore) SetProbeInterval(d time.Duration) {
	if d <= 0 {
		d = defaultProbeInterval
	}
	s.mu.Lock()
	s.probeInterval = d
	s.mu.Unlock()
}

// Value returns availability × quality, substituting neutral for any
// component that is uninitialized or stale.
func (s *ChannelScore) Value(now time.Time) float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	staleWindow := time.Duration(scoreStaleMult) * s.probeInterval
	a := gate(s.avail, s.availInit, s.availAt, now, staleWindow)
	q := gate(s.quality, s.qualityInit, s.qualityAt, now, staleWindow)
	return a * q
}

func gate(v float64, init bool, at, now time.Time, window time.Duration) float64 {
	if !init || now.Sub(at) > window {
		return scoreNeutral
	}
	return v
}

// QualityFromLossRTT computes QUIC quality from congestion-control stats.
func QualityFromLossRTT(loss float64, rtt, minRTT time.Duration) float64 {
	lossScore := clamp01(1 - loss*2)
	rttScore := 1.0
	if rtt > 0 && minRTT > 0 {
		rttScore = clamp01(float64(minRTT) / float64(rtt))
	}
	return lossScore * rttScore
}

// QualityFromEchoRTT computes TCP-WS quality from echo RTT inflation
// (TCP-WS has no CC loss/RTT — see TCPWSClient.WireStats).
func QualityFromEchoRTT(echoRTT, minEchoRTT time.Duration) float64 {
	if echoRTT <= 0 || minEchoRTT <= 0 {
		return scoreNeutral
	}
	return clamp01(float64(minEchoRTT) / float64(echoRTT))
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
