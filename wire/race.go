package wire

import (
	"context"
	"crypto/tls"
	"errors"
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

	// QUICBackup is a QUIC dialer that connected successfully during the race
	// but lost to TCP-WS. When non-nil, the caller should install it as the
	// primary transport with fallbackActive=true so the background probe can
	// verify QUIC and switch to it (typically within 30s).
	QUICBackup     Dialer
	QUICBackupType string // "quic-443" or "quic-hop"
}

type raceCandidate struct {
	dialer        Dialer
	tcpws         Dialer // non-nil only for tcpws candidate
	transportType string
	err           error
}

// connectedQUIC tracks a QUIC candidate that completed dial but may lose the race.
type connectedQUIC struct {
	dialer        Dialer
	transportType string
}

// RaceTransport races three transport candidates with staggered start:
//
//	t=0ms:   QUIC on port 443 (no hop) + echo probe
//	t=300ms: QUIC on hop ports + echo probe
//	t=800ms: TCP-WS on port 443 (no echo needed)
//
// First candidate with verified data flow wins.
// directDialer may be nil (non-desktop platforms).
// jar is the per-outbound CookieJar shared by every candidate built here
// (nil disables cookies).
func RaceTransport(ctx context.Context, cfg Config, dialAddr string, directDialer DirectDialer, jar *CookieJar) (*RaceResult, error) {
	raceStart := time.Now()
	raceCtx, raceCancel := context.WithTimeout(ctx, raceTimeout)

	results := make(chan raceCandidate, 3)
	var wg sync.WaitGroup
	var spawned int
	var winner *raceCandidate

	// Track connected QUIC candidates for QUICBackup when tcpws wins.
	var quicBackupMu sync.Mutex
	var quicBackup *connectedQUIC

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
	// Preserve QUICBackup (connected QUIC that lost to tcpws) — caller owns it.
	defer func() {
		raceCancel()
		go func() {
			wg.Wait()
			close(results)

			quicBackupMu.Lock()
			backup := quicBackup
			quicBackupMu.Unlock()

			for r := range results {
				if r.err == nil && r.dialer != nil && (winner == nil || r.dialer != winner.dialer) {
					// Don't close the QUIC dialer preserved as backup.
					if backup != nil && r.dialer == backup.dialer {
						continue
					}
					r.dialer.Close()
				}
				if r.tcpws != nil && (winner == nil || r.tcpws != winner.tcpws) {
					r.tcpws.Close()
				}
			}
		}()
	}()

	// Named candErrs (not errors) to avoid shadowing the stdlib "errors"
	// package — raceExitError below needs errors.Join to preserve structured
	// causes (e.g. ErrMetadataRejected) through the race exit. Declared here
	// (not just before the final collection loop) because waitForWinner must
	// also record failed candidates it drains during the stagger waits — a
	// candidate that fails FAST (e.g. an enforce server's near-instant
	// metadata rejection, well under the 300ms quic-hop stagger) previously
	// had its error silently discarded by waitForWinner before ever reaching
	// this slice, which is a total loss (worse than the %v-flattening this
	// task set out to fix — that at least preserved a stringified message).
	// Found empirically via Step 9's end-to-end test against a real enforce
	// server: quic-443's ErrMetadataRejected{402} arrived within ~2ms and was
	// swallowed by the first waitForWinner call before this fix.
	var candErrs [3]error

	// --- Candidate 1: QUIC on port 443 ---
	launch("quic-443", func(ctx context.Context) raceCandidate {
		return buildAndProbeQUIC(ctx, cfg, dialAddr, directDialer, cfg.Port, 0, 0, "quic-443", jar)
	})

	var consumed int

	// --- Stagger: wait 300ms or early result ---
	if w := waitForWinner(results, staggerQUICHop-time.Since(raceStart), &consumed, &candErrs); w != nil {
		winner = w
		slog.Info("DIAG: transport-race-winner", "winner", w.transportType, "winnerMs", time.Since(raceStart).Milliseconds(), "tried", 1)
		return &RaceResult{Dialer: w.dialer, TransportType: w.transportType}, nil
	}

	// --- Candidate 2: QUIC on hop ports ---
	if cfg.HopStart > 0 && cfg.HopEnd > 0 {
		launch("quic-hop", func(ctx context.Context) raceCandidate {
			return buildAndProbeQUIC(ctx, cfg, dialAddr, directDialer, cfg.Port, cfg.HopStart, cfg.HopEnd, "quic-hop", jar)
		})
	}

	// --- Stagger: wait until 800ms from race start ---
	remaining := staggerTCPWS - time.Since(raceStart)
	if w := waitForWinner(results, remaining, &consumed, &candErrs); w != nil {
		winner = w
		slog.Info("DIAG: transport-race-winner", "winner", w.transportType, "winnerMs", time.Since(raceStart).Milliseconds(), "tried", spawned)
		return &RaceResult{Dialer: w.dialer, TransportType: w.transportType}, nil
	}

	// --- Candidate 3: TCP-WS ---
	launch("tcpws", func(ctx context.Context) raceCandidate {
		return buildTCPWSCandidate(ctx, cfg, dialAddr, directDialer, jar)
	})

	// --- Collect remaining results ---
	for i := consumed; i < spawned; i++ {
		select {
		case r := <-results:
			if r.err == nil {
				winner = &r
				slog.Info("DIAG: transport-race-winner", "winner", r.transportType, "winnerMs", time.Since(raceStart).Milliseconds(), "tried", i+1)
				res := &RaceResult{Dialer: r.dialer, TransportType: r.transportType, TCPWS: r.tcpws}
				// tcpws won — drain remaining results to find connected QUIC candidates.
				// QUIC results may arrive nearly simultaneously (echo-probe completes ~same time).
				if r.transportType == "tcpws" {
					res = drainQUICBackup(res, results, spawned-i-1, &quicBackupMu, &quicBackup)
				}
				return res, nil
			}
			switch r.transportType {
			case "quic-443":
				candErrs[0] = r.err
			case "quic-hop":
				candErrs[1] = r.err
			case "tcpws":
				candErrs[2] = r.err
			}
		case <-raceCtx.Done():
			slog.Warn("DIAG: transport-race-fail",
				"quic443Err", candErrs[0],
				"quicHopErr", candErrs[1],
				"tcpwsErr", candErrs[2],
			)
			return nil, raceExitError(candErrs[0], candErrs[1], candErrs[2])
		}
	}

	slog.Warn("DIAG: transport-race-fail",
		"quic443Err", candErrs[0],
		"quicHopErr", candErrs[1],
		"tcpwsErr", candErrs[2],
	)
	exitErr := raceExitError(candErrs[0], candErrs[1], candErrs[2])
	maybeProbeECHBlock(cfg, directDialer, exitErr)
	return nil, exitErr
}

// raceExitError joins the per-candidate errors while PRESERVING their chains:
// a metadata auth rejection (ErrMetadataRejected wrapping StreamError{401..403})
// must survive to engine.ClassifyError, or enforce rollout turns every
// credential problem into an unclassified 570. errors.Join skips nils.
func raceExitError(quic443, quicHop, tcpws error) error {
	joined := errors.Join(quic443, quicHop, tcpws)
	if joined == nil {
		return fmt.Errorf("transport race: all candidates failed or timed out")
	}
	return fmt.Errorf("transport race: %w", joined)
}

// buildInsecureTLSConfig builds a minimal InsecureSkipVerify TLS config
// for testing or dev environments where ECH is not configured.
func buildInsecureTLSConfig() *tls.Config {
	return &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{"h3"},
	}
}

func buildAndProbeQUIC(ctx context.Context, cfg Config, dialAddr string, dd DirectDialer, port, hopStart, hopEnd int, name string, jar *CookieJar) raceCandidate {
	// Clone config with port/hop overrides.
	qcfg := cfg
	qcfg.Port = port
	qcfg.HopStart = hopStart
	qcfg.HopEnd = hopEnd
	if dialAddr != "" {
		qcfg.DialAddr = dialAddr
	}

	qc := NewQUICClient(qcfg, jar)
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
		if IsEchoUnsupported(err) {
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

func buildTCPWSCandidate(ctx context.Context, cfg Config, dialAddr string, dd DirectDialer, jar *CookieJar) raceCandidate {
	tcfg := cfg
	if dialAddr != "" {
		tcfg.DialAddr = dialAddr
	}

	tc := NewTCPWSClient(tcfg, jar)
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

// drainQUICBackup reads remaining race results (with a short grace period) to
// find a connected QUIC candidate. If found, it's preserved as QUICBackup on
// the result so the engine can install QUIC as primary with fallback=true.
// The quicBackup pointer is set so the defer cleanup skips closing it.
func drainQUICBackup(
	res *RaceResult,
	results chan raceCandidate,
	remaining int,
	mu *sync.Mutex,
	backup **connectedQUIC,
) *RaceResult {
	if remaining <= 0 {
		return res
	}

	// Brief grace period for nearly-simultaneous QUIC results. Echo-probe
	// finishes within milliseconds of tcpws in practice (observed: 2ms).
	// Keep short to avoid blocking the return path and delaying cleanup.
	timer := time.NewTimer(50 * time.Millisecond)
	defer timer.Stop()

	for i := 0; i < remaining; i++ {
		select {
		case r := <-results:
			if r.err == nil && r.transportType != "tcpws" {
				// Prefer quic-443 over quic-hop.
				if res.QUICBackup == nil || r.transportType == "quic-443" {
					// Close previously found backup if replacing with quic-443.
					if res.QUICBackup != nil {
						res.QUICBackup.Close()
					}
					res.QUICBackup = r.dialer
					res.QUICBackupType = r.transportType
					found := connectedQUIC{dialer: r.dialer, transportType: r.transportType}
					mu.Lock()
					*backup = &found
					mu.Unlock()
				} else {
					r.dialer.Close()
				}
			}
		case <-timer.C:
			slog.Debug("wire: drain grace expired", "collected", i, "of", remaining)
			return res
		}
	}

	if res.QUICBackup != nil {
		slog.Info("wire: QUIC connected but lost race, preserved as backup",
			"backup", res.QUICBackupType,
		)
	}
	return res
}

// waitForWinner drains `results` for up to d, returning the first successful
// candidate. Failed candidates it drains along the way are recorded into
// candErrs (indexed by transport, same convention as the final collection
// loop in RaceTransport) instead of being discarded — a candidate that fails
// fast (within the stagger window) must not lose its error just because it
// didn't win the race.
func waitForWinner(results chan raceCandidate, d time.Duration, consumed *int, candErrs *[3]error) *raceCandidate {
	record := func(r raceCandidate) {
		switch r.transportType {
		case "quic-443":
			candErrs[0] = r.err
		case "quic-hop":
			candErrs[1] = r.err
		case "tcpws":
			candErrs[2] = r.err
		}
	}

	if d <= 0 {
		select {
		case r := <-results:
			*consumed++
			if r.err == nil {
				return &r
			}
			record(r)
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
			record(r)
			continue
		case <-timer.C:
			return nil
		}
	}
}
