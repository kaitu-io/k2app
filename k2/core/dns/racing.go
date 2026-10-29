package dns

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"sync/atomic"
	"time"

	"github.com/miekg/dns"
)

type queryFunc func(ctx context.Context, server string, query *dns.Msg) (*dns.Msg, error)

const (
	degradedThreshold int32 = 3
	baseProbeInterval       = 30 * time.Second
	maxProbeInterval        = 5 * time.Minute
	latencyEWMAAlpha        = 0.3
	defaultRaceN            = 2
)

type serverHealth struct {
	addr            string
	consecutiveFail atomic.Int32
	latencyNs       atomic.Int64 // EWMA nanoseconds, used by pick() for ranking
	lastSuccess     atomic.Int64 // UnixNano, for observability (not used in selection)
	lastProbe       atomic.Int64 // UnixNano, rate-limits degraded server probing
	probeAttempts   atomic.Int32 // incremented on probe schedule, reset to 0 on success; drives exponential backoff
}

func (s *serverHealth) recordSuccess(latencyNs int64) {
	s.consecutiveFail.Store(0)
	s.probeAttempts.Store(0) // reset backoff on success
	s.lastSuccess.Store(time.Now().UnixNano())
	// Benign race: concurrent updates may lose one sample; acceptable for ranking heuristic.
	old := s.latencyNs.Load()
	if old == 0 {
		s.latencyNs.Store(latencyNs)
	} else {
		s.latencyNs.Store(int64(latencyEWMAAlpha*float64(latencyNs) + (1-latencyEWMAAlpha)*float64(old)))
	}
}

// probeBackoffDuration returns the current probe interval with exponential backoff.
// Base 30s doubles per probe attempt: 30s → 60s → 120s → 240s → 300s (cap).
func (s *serverHealth) probeBackoffDuration() time.Duration {
	attempts := s.probeAttempts.Load()
	interval := baseProbeInterval
	for i := int32(0); i < attempts; i++ {
		interval *= 2
		if interval >= maxProbeInterval {
			return maxProbeInterval
		}
	}
	return interval
}

// recordProbeAttempt increments the probe attempt counter for backoff.
func (s *serverHealth) recordProbeAttempt() {
	s.probeAttempts.Add(1)
}

func (s *serverHealth) recordFailure() {
	s.consecutiveFail.Add(1)
}

func (s *serverHealth) healthy() bool {
	return s.consecutiveFail.Load() < degradedThreshold
}

type racingResolver struct {
	servers []*serverHealth
	queryFn queryFunc
	raceN   int
	timeout time.Duration
	label   string // "direct" or "proxy" for error messages
}

func newRacingResolver(addrs []string, fn queryFunc, raceN int, timeout time.Duration, label string) *racingResolver {
	servers := make([]*serverHealth, len(addrs))
	for i, addr := range addrs {
		servers[i] = &serverHealth{addr: addr}
	}
	return &racingResolver{
		servers: servers,
		queryFn: fn,
		raceN:   raceN,
		timeout: timeout,
		label:   label,
	}
}

// pick selects servers to race: top raceN healthy by latency,
// plus optionally one degraded server due for probe.
// If all degraded, returns all (fallback to race-all).
func (r *racingResolver) pick() []*serverHealth {
	var healthy, degraded []*serverHealth
	for _, s := range r.servers {
		if s.healthy() {
			healthy = append(healthy, s)
		} else {
			degraded = append(degraded, s)
		}
	}

	// Sort healthy by latency (0 = untested, sort last among healthy)
	sort.Slice(healthy, func(i, j int) bool {
		li, lj := healthy[i].latencyNs.Load(), healthy[j].latencyNs.Load()
		if li == 0 {
			return false
		}
		if lj == 0 {
			return true
		}
		return li < lj
	})

	n := r.raceN
	if n > len(healthy) {
		n = len(healthy)
	}
	selected := make([]*serverHealth, n)
	copy(selected, healthy[:n])

	// Add one degraded server due for probe (if room or healthy insufficient).
	// CAS on lastProbe prevents concurrent pick() from double-scheduling.
	if len(selected) < r.raceN {
		now := time.Now().UnixNano()
		for _, s := range degraded {
			backoff := s.probeBackoffDuration()
			old := s.lastProbe.Load()
			if now-old >= backoff.Nanoseconds() {
				if !s.lastProbe.CompareAndSwap(old, now) {
					continue // another goroutine already scheduled this probe
				}
				s.recordProbeAttempt()
				selected = append(selected, s)
				break
			}
		}
	}

	// All degraded: race all (same as original behavior)
	if len(selected) == 0 {
		return r.servers
	}

	return selected
}

// Query races selected servers and returns the first success.
func (r *racingResolver) Query(ctx context.Context, query *dns.Msg) (*dns.Msg, error) {
	selected := r.pick()
	if len(selected) == 0 {
		return nil, fmt.Errorf("no %s DNS servers configured", r.label)
	}
	if len(selected) == 1 {
		return r.queryAndTrack(ctx, selected[0], query)
	}

	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	type result struct {
		resp *dns.Msg
		err  error
	}
	ch := make(chan result, len(selected))

	for _, s := range selected {
		go func(srv *serverHealth) {
			resp, err := r.queryAndTrack(ctx, srv, query)
			ch <- result{resp, err}
		}(s)
	}

	var lastErr error
	for range selected {
		select {
		case res := <-ch:
			if res.err == nil {
				return res.resp, nil
			}
			lastErr = res.err
		case <-ctx.Done():
			if lastErr != nil {
				return nil, fmt.Errorf("all %s DNS servers failed: %w", r.label, lastErr)
			}
			return nil, fmt.Errorf("all %s DNS servers timeout", r.label)
		}
	}
	return nil, fmt.Errorf("all %s DNS servers failed: %w", r.label, lastErr)
}

func (r *racingResolver) queryAndTrack(ctx context.Context, s *serverHealth, query *dns.Msg) (*dns.Msg, error) {
	start := time.Now()
	resp, err := r.queryFn(ctx, s.addr, query)
	if err != nil {
		s.recordFailure()
		if !s.healthy() {
			slog.Debug("DNS server degraded", "server", s.addr, "label", r.label,
				"consecutiveFail", s.consecutiveFail.Load(),
				"nextProbeIn", s.probeBackoffDuration().String())
		}
		return nil, err
	}
	s.recordSuccess(time.Since(start).Nanoseconds())
	return resp, nil
}
