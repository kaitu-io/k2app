package wire

import (
	"context"
	tls "github.com/metacubex/utls"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/kaitu-io/k2/safego"
)

const (
	raceTimeout     = 5 * time.Second
	staggerTCPWS    = 800 * time.Millisecond
	echoTimeoutRace = 1 * time.Second

	// raceCollectorSlack: the collector re-arms its deadline this long past
	// the latest pending candidate's, so the candidate's own expiry (and its
	// error) lands first.
	raceCollectorSlack = 100 * time.Millisecond
)

// RaceResult is the output of a successful transport race.
type RaceResult struct {
	Dialer        Dialer
	TransportType string // "quic-443" or "tcpws"
	TCPWS         Dialer // non-nil when the QUIC candidate won and TCP-WS was also built

	// QUICBackup is a QUIC dialer that connected successfully during the race
	// but lost to TCP-WS. When non-nil, the caller should install it as the
	// primary transport with fallbackActive=true so the background probe can
	// verify QUIC and switch to it (typically within 30s).
	QUICBackup     Dialer
	QUICBackupType string // "quic-443"
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

// RaceTransport races two transport candidates with staggered start:
//
//	t=0ms:   QUIC on port 443 + echo probe
//	t=800ms: TCP-WS on port 443 (no echo needed)
//
// First candidate with verified data flow wins.
// directDialer may be nil (non-desktop platforms).
// jar is the per-outbound CookieJar shared by every candidate built here
// (nil disables cookies).
func RaceTransport(ctx context.Context, cfg Config, dialAddr string, directDialer DirectDialer, jar *CookieJar) (*RaceResult, error) {
	raceStart := time.Now()
	// raceTimeout bounds each candidate's TRANSPORT step only (candDeadline):
	// a candidate whose handshake completed gets metaPhaseBudget more for
	// the metadata verdict + echo (PVE S15). raceCtx itself has no deadline;
	// the caller's ctx still bounds everything.
	raceCtx, raceCancel := context.WithCancel(ctx)
	transportUntil := raceStart.Add(raceTimeout)
	var cands [2]*candDeadline // [0] quic-443, [1] tcpws

	results := make(chan raceCandidate, 2)
	var wg sync.WaitGroup
	var spawned int
	var winner *raceCandidate

	// Track connected QUIC candidates for QUICBackup when tcpws wins.
	var quicBackupMu sync.Mutex
	var quicBackup *connectedQUIC

	slog.Info("DIAG: transport-race-start", "candidates", 3)

	launch := func(idx int, fn func(context.Context) raceCandidate) {
		spawned++
		cd := newCandDeadline(raceCtx, transportUntil)
		cands[idx] = cd
		wg.Add(1)
		safego.Go(func() {
			defer wg.Done()
			r := fn(cd.ctx)
			cd.finish()
			results <- r
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
	// metadata rejection, well under the 800ms tcpws stagger) previously
	// had its error silently discarded by waitForWinner before ever reaching
	// this slice, which is a total loss (worse than the %v-flattening this
	// task set out to fix — that at least preserved a stringified message).
	// Found empirically via Step 9's end-to-end test against a real enforce
	// server: quic-443's ErrMetadataRejected{402} arrived within ~2ms and was
	// swallowed by the first waitForWinner call before this fix.
	var candErrs [2]error

	// --- Candidate 1: QUIC on port 443 ---
	launch(0, func(ctx context.Context) raceCandidate {
		return buildAndProbeQUIC(ctx, cfg, dialAddr, directDialer, jar)
	})

	var consumed int

	// --- Stagger: wait until 800ms from race start ---
	remaining := staggerTCPWS - time.Since(raceStart)
	if w := waitForWinner(results, remaining, &consumed, &candErrs); w != nil {
		winner = w
		slog.Info("DIAG: transport-race-winner", "winner", w.transportType, "winnerMs", time.Since(raceStart).Milliseconds(), "tried", spawned)
		return &RaceResult{Dialer: w.dialer, TransportType: w.transportType}, nil
	}

	// --- Candidate 2: TCP-WS ---
	launch(1, func(ctx context.Context) raceCandidate {
		return buildTCPWSCandidate(ctx, cfg, dialAddr, directDialer, jar)
	})

	// --- Collect remaining results ---
	// The collector's own deadline is the transport deadline, extended for
	// as long as some candidate is past its handshake and waiting on its
	// metadata verdict (candDeadline.pending).
	deadline := time.NewTimer(time.Until(transportUntil))
	defer deadline.Stop()
	for i := consumed; i < spawned; {
		select {
		case r := <-results:
			i++
			if r.err == nil {
				winner = &r
				slog.Info("DIAG: transport-race-winner", "winner", r.transportType, "winnerMs", time.Since(raceStart).Milliseconds(), "tried", i)
				res := &RaceResult{Dialer: r.dialer, TransportType: r.transportType, TCPWS: r.tcpws}
				if testHookRaceWinner != nil {
					testHookRaceWinner(r.transportType)
				}
				// tcpws won — drain remaining results to find connected QUIC candidates.
				// QUIC results may arrive nearly simultaneously (echo-probe completes ~same time).
				if r.transportType == "tcpws" {
					// i already counts this result (incremented above).
					res = drainQUICBackup(res, results, spawned-i, &quicBackupMu, &quicBackup)
				}
				return res, nil
			}
			switch r.transportType {
			case "quic-443":
				candErrs[0] = r.err
			case "tcpws":
				candErrs[1] = r.err
			}
		case <-deadline.C:
			if until, ok := latestPending(cands); ok {
				deadline.Reset(time.Until(until) + raceCollectorSlack)
				continue
			}
			slog.Warn("DIAG: transport-race-fail",
				"quic443Err", candErrs[0],
				"tcpwsErr", candErrs[1],
			)
			return nil, raceExitError(candErrs[0], candErrs[1])
		case <-ctx.Done():
			slog.Warn("DIAG: transport-race-fail",
				"quic443Err", candErrs[0],
				"tcpwsErr", candErrs[1],
			)
			if anyPending(cands) {
				// Reached the node, no verdict before the caller gave up.
				return nil, raceExitError(candErrs[0], errors.Join(candErrs[1], ErrMetadataTimeout))
			}
			return nil, raceExitError(candErrs[0], candErrs[1])
		}
	}

	slog.Warn("DIAG: transport-race-fail",
		"quic443Err", candErrs[0],
		"tcpwsErr", candErrs[1],
	)
	exitErr := raceExitError(candErrs[0], candErrs[1])
	maybeProbeECHBlock(cfg, directDialer, exitErr)
	return nil, exitErr
}

// metaPhaseBudget is how long a candidate may take after its transport
// handshake completed: the metadata verdict (metaReadTimeout, which outlives
// k2s's 5s remote-auth timeout) plus the race echo, plus slack.
func metaPhaseBudget() time.Duration {
	return metaReadTimeout + echoTimeoutRace + 500*time.Millisecond
}

// drainQUICGrace is how long a tcpws win waits for a QUIC candidate still in
// flight (drainQUICBackup). A var only so tests can widen it.
var drainQUICGrace = 50 * time.Millisecond

// testHookRaceWinner, when non-nil (tests only), is called in the collector
// with the winner's transport type, before the QUIC-backup drain.
var testHookRaceWinner func(transportType string)

// RaceWorstCase is the longest RaceTransport can run on its own deadlines: a
// candidate whose transport handshake completes just before raceTimeout,
// then the whole metaPhaseBudget, then the collector's slack. A caller ctx
// shorter than this cuts a slow-but-alive node's verdict (PVE S15) and turns
// it into a bare timeout; the engine derives its race ctx from this value.
func RaceWorstCase() time.Duration {
	return raceTimeout + metaPhaseBudget() + raceCollectorSlack
}

// candDeadline is one race candidate's deadline: transportUntil while its
// transport handshake runs; once connect() reports the handshake done
// (transportUp hook), metaPhaseBudget from then. Expiry cancels only this
// candidate's ctx. Fields under mu; the timer callback takes mu too.
type candDeadline struct {
	ctx    context.Context
	cancel context.CancelFunc

	mu    sync.Mutex
	state int // candTransport → candUp → candDone
	until time.Time
	timer *time.Timer
}

const (
	candTransport = iota // handshake in progress
	candUp               // handshake done, metadata / echo pending
	candDone             // returned or expired
)

func newCandDeadline(parent context.Context, transportUntil time.Time) *candDeadline {
	c := &candDeadline{until: transportUntil}
	ctx, cancel := context.WithCancel(parent)
	c.cancel = cancel
	c.ctx = withTransportUpHook(ctx, c.up)
	c.timer = time.AfterFunc(time.Until(transportUntil), c.expire)
	return c
}

// up: the transport handshake completed — switch to the metadata budget.
func (c *candDeadline) up() {
	c.mu.Lock()
	if c.state == candTransport {
		c.state = candUp
		c.until = time.Now().Add(metaPhaseBudget())
		c.timer.Reset(time.Until(c.until))
	}
	c.mu.Unlock()
}

// expire is the timer callback. A callback that lost a race with up()'s
// Reset sees the new, later deadline and leaves it to the re-armed timer.
func (c *candDeadline) expire() {
	c.mu.Lock()
	fire := c.state == candTransport || (c.state == candUp && !time.Now().Before(c.until))
	if fire {
		c.state = candDone
	}
	c.mu.Unlock()
	if fire {
		c.cancel()
	}
}

// finish: the candidate returned. Its ctx is left to raceCancel (the winner's
// ctx stays alive until RaceTransport returns, as before).
func (c *candDeadline) finish() {
	c.mu.Lock()
	c.state = candDone
	c.mu.Unlock()
	c.timer.Stop()
}

// pending reports whether the candidate is past its handshake and still
// waiting, and until when.
func (c *candDeadline) pending() (time.Time, bool) {
	if c == nil {
		return time.Time{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.until, c.state == candUp
}

// latestPending is the latest deadline among candidates still waiting on
// their metadata verdict.
func latestPending(cands [2]*candDeadline) (time.Time, bool) {
	var latest time.Time
	found := false
	for _, c := range cands {
		if until, ok := c.pending(); ok {
			found = true
			if until.After(latest) {
				latest = until
			}
		}
	}
	return latest, found
}

func anyPending(cands [2]*candDeadline) bool {
	_, ok := latestPending(cands)
	return ok
}

// raceExitError joins the per-candidate errors while PRESERVING their chains:
// a metadata auth rejection (ErrMetadataRejected wrapping StreamError{401..403})
// must survive to engine.ClassifyError, or enforce rollout turns every
// credential problem into an unclassified 570. errors.Join skips nils.
func raceExitError(quic443, tcpws error) error {
	joined := errors.Join(quic443, tcpws)
	if joined == nil {
		return fmt.Errorf("transport race: all candidates failed or timed out")
	}
	return fmt.Errorf("transport race: %w", joined)
}

// buildInsecureTLSConfig builds a minimal InsecureSkipVerify TLS config for
// the dev/test path — `insecure=1` in the URL with no ECH configured. It is
// also what the wire tests use, deliberately: this is the one place that knows
// what an unverified QUIC client config has to look like, and the ServerName
// below is exactly the kind of requirement that rots if every test restates it.
func buildInsecureTLSConfig() *tls.Config {
	return &tls.Config{
		// ServerName must be a real host name. Empty is not an option: quic-go
		// fills an empty ServerName in from the dial address, and since the
		// fingerprinted ClientHello always carries a server_name extension, an
		// IP literal there — forbidden by RFC 6066 §3 — gets the ClientHello
		// dropped unanswered, surfacing as a bare handshake timeout. kuic
		// rejects that config up front (ErrSNIWithoutHostname) instead.
		//
		// The ordinary path never reaches here: BuildClientTLSConfig sets the
		// k2v5 inner SNI, and with ECH the name is encrypted anyway.
		ServerName:         "localhost",
		InsecureSkipVerify: true,
		NextProtos:         []string{"h3"},
	}
}

func buildAndProbeQUIC(ctx context.Context, cfg Config, dialAddr string, dd DirectDialer, jar *CookieJar) raceCandidate {
	const name = "quic-443"

	qcfg := cfg
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
			slog.Warn("DIAG: echo-probe-unsupported", "transport", name, "err", err.Error())
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
	timer := time.NewTimer(drainQUICGrace)
	defer timer.Stop()

	for i := 0; i < remaining; i++ {
		select {
		case r := <-results:
			if r.err == nil && r.transportType != "tcpws" {
				if res.QUICBackup == nil {
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
func waitForWinner(results chan raceCandidate, d time.Duration, consumed *int, candErrs *[2]error) *raceCandidate {
	record := func(r raceCandidate) {
		switch r.transportType {
		case "quic-443":
			candErrs[0] = r.err
		case "tcpws":
			candErrs[1] = r.err
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
