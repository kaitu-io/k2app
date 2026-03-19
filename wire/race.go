package wire

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/kaitu-io/k2/safego"
)

const (
	raceTimeout     = 5 * time.Second
	staggerQUICHop  = 300 * time.Millisecond
	staggerTCPWS    = 800 * time.Millisecond
	echoTimeoutRace = 1 * time.Second
)

// RaceResult is the output of a successful transport race.
type RaceResult struct {
	Dialer        Dialer
	TransportType string // "quic-443", "quic-hop", "tcpws"
	TCPWS         Dialer // non-nil when a QUIC candidate won and TCP-WS was also built
}

type raceCandidate struct {
	dialer        Dialer
	tcpws         Dialer // non-nil only for tcpws candidate
	transportType string
	err           error
}

// RaceTransport races three transport candidates with staggered start:
//
//	t=0ms:   QUIC on port 443 (no hop) + echo probe
//	t=300ms: QUIC on hop ports + echo probe
//	t=800ms: TCP-WS on port 443 (no echo needed)
//
// First candidate with verified data flow wins.
// directDialer may be nil (non-desktop platforms).
func RaceTransport(ctx context.Context, cfg Config, dialAddr string, directDialer DirectDialer) (*RaceResult, error) {
	raceStart := time.Now()
	raceCtx, raceCancel := context.WithTimeout(ctx, raceTimeout)

	results := make(chan raceCandidate, 3)
	var wg sync.WaitGroup
	var spawned int
	var winner *raceCandidate

	slog.Info("DIAG: transport-race-start", "candidates", 3)

	launch := func(name string, fn func(context.Context) raceCandidate) {
		spawned++
		wg.Add(1)
		safego.Go(func() {
			defer wg.Done()
			results <- fn(raceCtx)
		})
	}

	// Cleanup: cancel losers, wait for goroutines, close loser dialers.
	defer func() {
		raceCancel()
		go func() {
			wg.Wait()
			close(results)
			for r := range results {
				if r.err == nil && r.dialer != nil && (winner == nil || r.dialer != winner.dialer) {
					r.dialer.Close()
				}
				if r.tcpws != nil && (winner == nil || r.tcpws != winner.tcpws) {
					r.tcpws.Close()
				}
			}
		}()
	}()

	// --- Candidate 1: QUIC on port 443 ---
	launch("quic-443", func(ctx context.Context) raceCandidate {
		return buildAndProbeQUIC(ctx, cfg, dialAddr, directDialer, cfg.Port, 0, 0, "quic-443")
	})

	var consumed int

	// --- Stagger: wait 300ms or early result ---
	if w := waitForWinner(results, staggerQUICHop-time.Since(raceStart), &consumed); w != nil {
		winner = w
		slog.Info("DIAG: transport-race-winner", "winner", w.transportType, "winnerMs", time.Since(raceStart).Milliseconds(), "tried", 1)
		return &RaceResult{Dialer: w.dialer, TransportType: w.transportType}, nil
	}

	// --- Candidate 2: QUIC on hop ports ---
	if cfg.HopStart > 0 && cfg.HopEnd > 0 {
		launch("quic-hop", func(ctx context.Context) raceCandidate {
			return buildAndProbeQUIC(ctx, cfg, dialAddr, directDialer, cfg.Port, cfg.HopStart, cfg.HopEnd, "quic-hop")
		})
	}

	// --- Stagger: wait until 800ms from race start ---
	remaining := staggerTCPWS - time.Since(raceStart)
	if w := waitForWinner(results, remaining, &consumed); w != nil {
		winner = w
		slog.Info("DIAG: transport-race-winner", "winner", w.transportType, "winnerMs", time.Since(raceStart).Milliseconds(), "tried", spawned)
		return &RaceResult{Dialer: w.dialer, TransportType: w.transportType}, nil
	}

	// --- Candidate 3: TCP-WS ---
	launch("tcpws", func(ctx context.Context) raceCandidate {
		return buildTCPWSCandidate(ctx, cfg, dialAddr, directDialer)
	})

	// --- Collect remaining results ---
	var errors [3]error
	for i := consumed; i < spawned; i++ {
		select {
		case r := <-results:
			if r.err == nil {
				winner = &r
				slog.Info("DIAG: transport-race-winner", "winner", r.transportType, "winnerMs", time.Since(raceStart).Milliseconds(), "tried", i+1)
				res := &RaceResult{Dialer: r.dialer, TransportType: r.transportType, TCPWS: r.tcpws}
				return res, nil
			}
			switch r.transportType {
			case "quic-443":
				errors[0] = r.err
			case "quic-hop":
				errors[1] = r.err
			case "tcpws":
				errors[2] = r.err
			}
		case <-raceCtx.Done():
			slog.Warn("DIAG: transport-race-fail",
				"quic443Err", errors[0],
				"quicHopErr", errors[1],
				"tcpwsErr", errors[2],
			)
			return nil, fmt.Errorf("transport race: all candidates failed or timed out")
		}
	}

	slog.Warn("DIAG: transport-race-fail",
		"quic443Err", errors[0],
		"quicHopErr", errors[1],
		"tcpwsErr", errors[2],
	)
	return nil, fmt.Errorf("transport race: quic-443: %v; quic-hop: %v; tcpws: %v", errors[0], errors[1], errors[2])
}

// buildInsecureTLSConfig builds a minimal InsecureSkipVerify TLS config
// for testing or dev environments where ECH is not configured.
func buildInsecureTLSConfig() *tls.Config {
	return &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{"h3"},
	}
}

func buildAndProbeQUIC(ctx context.Context, cfg Config, dialAddr string, dd DirectDialer, port, hopStart, hopEnd int, name string) raceCandidate {
	// Clone config with port/hop overrides.
	qcfg := cfg
	qcfg.Port = port
	qcfg.HopStart = hopStart
	qcfg.HopEnd = hopEnd
	if dialAddr != "" {
		qcfg.DialAddr = dialAddr
	}

	qc := NewQUICClient(qcfg, nil)
	if dd != nil {
		qc.SetDirectDialer(dd)
	}

	// For insecure mode without ECH (testing/dev), set TLS config directly
	// to bypass BuildClientTLSConfig which requires ECH.
	if cfg.Insecure && cfg.ECH == "" {
		qc.SetTLSConfig(buildInsecureTLSConfig())
	}

	// Step 1: QUIC handshake.
	conn, err := qc.connect(ctx)
	if err != nil {
		qc.Close()
		slog.Warn("DIAG: echo-probe-fail", "transport", name, "err", fmt.Sprintf("handshake: %v", err))
		return raceCandidate{transportType: name, err: err}
	}

	// Step 2: Echo probe (verify Short Header data path).
	echoCtx, echoCancel := context.WithTimeout(ctx, echoTimeoutRace)
	defer echoCancel()

	rtt, err := EchoProbe(echoCtx, conn)
	if err != nil {
		if isEchoUnsupported(err) {
			slog.Warn("DIAG: echo-probe-unsupported", "transport", name)
			return raceCandidate{dialer: qc, transportType: name}
		}
		slog.Warn("DIAG: echo-probe-fail", "transport", name, "err", err)
		qc.Close()
		return raceCandidate{transportType: name, err: fmt.Errorf("echo probe: %w", err)}
	}

	slog.Debug("DIAG: echo-probe-ok", "transport", name, "rttMs", rtt.Milliseconds())
	return raceCandidate{dialer: qc, transportType: name}
}

func buildTCPWSCandidate(ctx context.Context, cfg Config, dialAddr string, dd DirectDialer) raceCandidate {
	tcfg := cfg
	if dialAddr != "" {
		tcfg.DialAddr = dialAddr
	}

	tc := NewTCPWSClient(tcfg, nil)
	if dd != nil {
		tc.SetDirectDialer(dd)
	}

	// For insecure mode without ECH (testing/dev), set TLS config directly.
	if cfg.Insecure && cfg.ECH == "" {
		tc.SetTLSConfig(&tls.Config{
			InsecureSkipVerify: true,
		})
	}

	_, err := tc.connect(ctx)
	if err != nil {
		tc.Close()
		return raceCandidate{transportType: "tcpws", err: err}
	}

	return raceCandidate{dialer: tc, tcpws: tc, transportType: "tcpws"}
}

// waitForWinner waits up to duration for a successful result on the channel.
// consumed tracks results read (caller uses to avoid double-reading).
func waitForWinner(results chan raceCandidate, d time.Duration, consumed *int) *raceCandidate {
	if d <= 0 {
		select {
		case r := <-results:
			*consumed++
			if r.err == nil {
				return &r
			}
		default:
		}
		return nil
	}

	timer := time.NewTimer(d)
	defer timer.Stop()

	for {
		select {
		case r := <-results:
			*consumed++
			if r.err == nil {
				return &r
			}
			continue
		case <-timer.C:
			return nil
		}
	}
}
