package wire

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/apernet/quic-go"
)

// ProbeStats is the aggregated result of a single ProbeURL invocation:
// a fresh QUIC handshake followed by N echo rounds on independent streams.
type ProbeStats struct {
	AvgRttMs      float64
	MinRttMs      float64
	MaxRttMs      float64
	JitterMs      float64 // MaxRttMs - MinRttMs
	LossRate      float64 // timeouts / rounds
	Reachable     bool    // false iff QUIC handshake itself failed
	EchoSupported bool    // false = old k2s without HandleEchoStream
	MeasuredAt    time.Time
}

// Score collapses stats into a single [0,1] quality value, with a sentinel
// -1 when the server doesn't support echo (reachable but unmeasured).
//
//	probeScore    = (1 - lossRate) * rttScore * jitterPenalty
//	rttScore      = 1 / (1 + avgRttMs / 150)
//	jitterPenalty = clamp(1 - (jitterMs - 50)/200, 0.7, 1.0)
func (s ProbeStats) Score() float64 {
	if !s.Reachable {
		return 0
	}
	if !s.EchoSupported {
		return -1
	}
	rttScore := 1.0 / (1.0 + s.AvgRttMs/150.0)
	lossScore := 1.0 - s.LossRate
	if lossScore < 0 {
		lossScore = 0
	}
	jitterPenalty := 1.0 - (s.JitterMs-50.0)/200.0
	if jitterPenalty > 1.0 {
		jitterPenalty = 1.0
	}
	if jitterPenalty < 0.7 {
		jitterPenalty = 0.7
	}
	return lossScore * rttScore * jitterPenalty
}

// probeConnection runs rounds echo probes on an already-established QUIC conn,
// sleeping interval between rounds. Returns aggregated stats. Exposed at
// package scope for tests; production callers use ProbeURL.
//
// Each round has a 1s deadline. Timeouts count as loss. If any round returns
// an isEchoUnsupported error, the probe short-circuits with EchoSupported=false
// (old server won't answer any stream, no point retrying).
func probeConnection(ctx context.Context, conn *quic.Conn, rounds int, interval time.Duration) (ProbeStats, error) {
	if rounds <= 0 {
		return ProbeStats{}, errors.New("probeConnection: rounds must be > 0")
	}

	rtts := make([]float64, 0, rounds)
	losses := 0
	anyUnsupported := false

	for i := 0; i < rounds; i++ {
		if ctx.Err() != nil {
			return ProbeStats{}, ctx.Err()
		}
		roundCtx, cancel := context.WithTimeout(ctx, 1*time.Second)
		rtt, err := EchoProbe(roundCtx, conn)
		cancel()
		if err != nil {
			if IsEchoUnsupported(err) {
				anyUnsupported = true
				break
			}
			losses++
			continue
		}
		// Use sub-millisecond precision: loopback RTTs are < 1ms and
		// rtt.Milliseconds() would truncate to 0.
		rtts = append(rtts, float64(rtt)/float64(time.Millisecond))
		if i < rounds-1 {
			select {
			case <-ctx.Done():
				return ProbeStats{}, ctx.Err()
			case <-time.After(interval):
			}
		}
	}

	stats := ProbeStats{
		Reachable:     true,
		EchoSupported: !anyUnsupported,
		MeasuredAt:    time.Now(),
	}

	if anyUnsupported {
		return stats, nil
	}

	if len(rtts) == 0 {
		stats.LossRate = 1
		return stats, nil
	}

	min, max, sum := rtts[0], rtts[0], 0.0
	for _, r := range rtts {
		sum += r
		if r < min {
			min = r
		}
		if r > max {
			max = r
		}
	}
	stats.AvgRttMs = sum / float64(len(rtts))
	stats.MinRttMs = min
	stats.MaxRttMs = max
	stats.JitterMs = max - min
	stats.LossRate = float64(losses) / float64(rounds)
	return stats, nil
}

// ProbeURL establishes an independent QUIC connection to the server encoded
// in rawURL, runs rounds echo probes, and closes. Does NOT fall back to
// TCP-WS: if QUIC cannot reach this node, the node is not worth selecting.
//
// dd is the DirectDialer for interface binding (pass nil to use OS default
// routing — acceptable for ad-hoc probes when the daemon has no active tunnel).
//
// Budget: ctx deadline caps everything. Internally: 3s handshake + per-round
// 1s timeout. Caller typically passes ctx with 8-10s total deadline.
func ProbeURL(ctx context.Context, rawURL string, dd DirectDialer, rounds int, interval time.Duration) (ProbeStats, error) {
	cfg, err := ParseURL(rawURL)
	if err != nil {
		return ProbeStats{MeasuredAt: time.Now()}, fmt.Errorf("probe: parse URL: %w", err)
	}

	qc := NewQUICClient(cfg)
	defer qc.Close()
	if dd != nil {
		qc.SetDirectDialer(dd)
	}
	// Testing / self-signed servers: skip BuildClientTLSConfig which requires ECH.
	if cfg.Insecure && cfg.ECH == "" {
		qc.SetTLSConfig(buildInsecureTLSConfig())
	}

	handshakeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	conn, err := qc.connect(handshakeCtx)
	cancel()
	if err != nil {
		return ProbeStats{MeasuredAt: time.Now()}, fmt.Errorf("probe: handshake: %w", err)
	}

	return probeConnection(ctx, conn, rounds, interval)
}
