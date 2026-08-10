package wire

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/sasha-s/go-deadlock"
)

// proxyLayerErr is implemented by errors where the wire transport
// (k2 ↔ k2s) was proven working. TCP-WS fallback connects to the
// same server — it won't help with proxy-layer problems.
type proxyLayerErr interface {
	IsProxyLayer() bool
}

// ProxyLayerError wraps any error that occurred after the request
// was successfully sent to the server (stream opened + header written).
// The wire transport worked; TCP-WS fallback to the same server won't help.
type ProxyLayerError struct {
	Err error
}

func (e *ProxyLayerError) Error() string      { return e.Err.Error() }
func (e *ProxyLayerError) Unwrap() error      { return e.Err }
func (e *ProxyLayerError) IsProxyLayer() bool { return true }

// isProxyLayerError returns true when the Dialer has marked the error
// as proxy-layer (server responded, transport worked). Checked via
// interface — no type enumeration needed.
func isProxyLayerError(err error) bool {
	var pe proxyLayerErr
	return errors.As(err, &pe) && pe.IsProxyLayer()
}

// MultiTransportError is returned when both QUIC and TCP-WS transports fail.
// Unwrap returns the TCP-WS error (last-resort transport) for classification.
type MultiTransportError struct {
	QUICErr  error
	TCPWSErr error
}

func (e *MultiTransportError) Error() string {
	return fmt.Sprintf("QUIC: %v; TCP-WS: %v", e.QUICErr, e.TCPWSErr)
}

func (e *MultiTransportError) Unwrap() []error {
	return []error{e.QUICErr, e.TCPWSErr}
}

const (
	// quicProbeInterval is how often the background probe attempts QUIC
	// recovery while TCP-WS fallback is active.
	quicProbeInterval = 30 * time.Second

	// quicConnFailThreshold is how many consecutive QUIC connection (handshake)
	// failures to different hop ports before activating TCP-WS fallback.
	quicConnFailThreshold int32 = 3
)

// legacySelectorEnabled returns true when K2_CHANNEL_SELECTOR_LEGACY=1,
// reverting the channel selector to its pre-scoreboard behavior: switch-away
// by handshake-failure streak alone (no quality path) and switch-back on a
// bare successful handshake alone (no echo verification, no flap-window
// wait). A production rollback valve — remove once the scoreboard is proven
// stable. Read once, at TransportManager construction (NewTransportManager
// caches it into tm.legacy); not re-read per call.
func legacySelectorEnabled() bool {
	return os.Getenv("K2_CHANNEL_SELECTOR_LEGACY") == "1"
}

const (
	switchAwayRatio         = 1.5              // switch QUIC→TCPWS when tcpws > quic × this
	qualityDegradeThreshold = 0.7              // QUIC score below this pre-warns the frequency controller
	qualitySustainWindow    = 10 * time.Second // quality-path condition must hold this long before switching away
	flapWindowBase          = 30 * time.Second // first rollback review window
	flapWindowMax           = 600 * time.Second
	backupFreshSamplesGate  = 2 // backup must have this many fresh samples before the quality path may switch away
)

type channelDecision int

const (
	channelStay channelDecision = iota
	channelSwitchToTCPWS
	channelSwitchToQUIC
)

// selectorState is the persistent state of the hysteresis FSM. Copied by
// value into selectorStep (pure) and written back by the stateful wrapper.
type selectorState struct {
	active       string    // "quic" | "tcpws"
	aboveSince   time.Time // when the quality-path condition first held (zero = not holding)
	flapCount    int       // completed QUIC→TCPWS→QUIC round trips
	nextReviewAt time.Time // rollback review window opens at this time
}

// selectorInput is one evaluation's observations. All scores are ChannelScore.Value.
type selectorInput struct {
	now               time.Time
	scoreQUIC         float64
	scoreTCPWS        float64
	consecConnFails   int32 // QUIC handshake streak (fast path)
	backupFresh       int   // fresh backup samples since degradation pre-warning
	quicEchoRecovered bool  // background probe passed handshake + 3 echo rounds
}

// selectorStep is the pure transition. No time.Now, no I/O, no logging.
func selectorStep(st selectorState, in selectorInput) (selectorState, channelDecision) {
	if st.active != "tcpws" {
		// QUIC branch (default: empty string is treated as QUIC).
		// Fast path: handshake genuinely failing (UDP blocked).
		if in.consecConnFails >= quicConnFailThreshold {
			return switchAway(st, in.now), channelSwitchToTCPWS
		}

		// Quality path: tcpws sustained-better AND backup measured fresh.
		condHolds := in.scoreTCPWS > in.scoreQUIC*switchAwayRatio

		if condHolds {
			// Record when the condition first held
			if st.aboveSince.IsZero() {
				st.aboveSince = in.now
			}

			// Check if backup is fresh and enough time has passed
			held := in.now.Sub(st.aboveSince)

			if in.backupFresh >= backupFreshSamplesGate && held >= qualitySustainWindow {
				return switchAway(st, in.now), channelSwitchToTCPWS
			}
		} else {
			// Condition not holding, reset the timer
			st.aboveSince = time.Time{}
		}

		return st, channelStay
	}

	// active == "tcpws": rollback path. QUIC is the strategic home — return to
	// it as soon as the data path is echo-verified healthy (quicEchoRecovered:
	// a fresh handshake + 3 consecutive echo rounds, set by runProbeOnce),
	// gated only by the growing flap-damping window. Deliberately NO score
	// comparison: while tcpws serves traffic QUIC carries none, so its quality
	// is unmeasurable and scoreQUIC is pinned at neutral — a `scoreQUIC >
	// scoreTCPWS × k` gate is impossible to satisfy against a healthy tcpws and
	// would strand users on the stateful fallback forever. Anti-thrash is the
	// 3-round echo proof + nextReviewAt (30s→600s), not scores.
	if in.now.Before(st.nextReviewAt) {
		return st, channelStay // flap-damped
	}
	if !in.quicEchoRecovered {
		return st, channelStay
	}
	st.active = "quic"
	st.aboveSince = time.Time{}
	return st, channelSwitchToQUIC
}

// switchAway transitions to tcpws and grows the flap-damping window.
func switchAway(st selectorState, now time.Time) selectorState {
	st.active = "tcpws"
	st.aboveSince = time.Time{}

	var window time.Duration
	const maxSafeShift = 20 // 30s<<20 ≈ 8.7h, far exceeds flapWindowMax; prevent overflow
	if st.flapCount >= maxSafeShift {
		window = flapWindowMax
	} else {
		window = flapWindowBase << uint(st.flapCount)
		if window > flapWindowMax || window <= 0 {
			window = flapWindowMax
		}
	}

	st.nextReviewAt = now.Add(window)
	st.flapCount++
	return st
}

// channelForType maps a primaryType label ("quic-443", "quic-hop", "tcpws")
// to its ChannelScore key. Used to classify which channel a primary dial
// outcome belongs to.
func channelForType(transportType string) string {
	if strings.HasPrefix(transportType, "quic") {
		return "quic"
	}
	return "tcpws"
}

// TransportManager manages primary and TCP-WS transports.
// QUIC is strongly preferred; TCP-WS is used as last-resort fallback only
// when QUIC is genuinely unavailable (UDP blocked). ISPs rarely block UDP,
// so even with 40%+ packet loss, QUIC with port hopping outperforms TCP-WS.
type TransportManager struct {
	mu          deadlock.RWMutex
	primary     Dialer // was: quic — the preferred transport (QUIC-443, QUIC-hop, or tcpws)
	tcpws       Dialer
	primaryType string // "quic-443", "quic-hop", "tcpws"

	// Dual-channel quality scoreboard (Task 1) + hysteresis FSM (Task 2).
	// selState.active is the ONLY source of truth for which channel is
	// serving traffic — FallbackActive() derives from it. Protected by mu.
	scoreQUIC  *ChannelScore
	scoreTCPWS *ChannelScore
	selState   selectorState

	// quicEchoRecovered is set by the background probe (runProbeOnce) only
	// after a freshly (re-)established QUIC handshake also passes 3
	// consecutive echo rounds. evaluateSelector — never the probe itself —
	// decides whether that is enough to roll back (root cause A fix: a
	// probe that only completes the handshake must never clear fallback).
	quicEchoRecovered atomic.Bool

	// backupFresh counts tcpws-channel samples ever recorded (dial or
	// echo). Gates the quality-path switch-away so a single lucky/unlucky
	// tcpws sample right after cold start can't flip the transport.
	backupFresh atomic.Int32

	// tcpwsMinEchoRTT is the running minimum echo RTT observed on the
	// tcpws channel — the baseline for QualityFromEchoRTT (tcpws has no
	// congestion-control stats, unlike QUIC).
	tcpwsMinEchoRTT atomic.Int64 // ns; 0 = unset

	// Post-reset grace: first QUIC dial after Reset gets extended budget.
	freshAfterReset atomic.Bool

	// Background QUIC probe: actively tests QUIC recovery during fallback.
	probeCancel context.CancelFunc

	// channelReviewLoggedWindow / flapDampedLoggedWindow dedupe the
	// per-window DIAG logs emitted from evaluateSelector (Task 8, R2/R1) —
	// keyed on selState.nextReviewAt's value, which only changes when a
	// fresh flap round begins (switchAway). evaluateSelector runs on every
	// Dial*/Handshake call AND every echo tick, so without this gate a
	// naive "log every review" would spam at dial rate while the FSM sits
	// out the flap window. Read/written only from evaluateSelector, under mu.
	channelReviewLoggedWindow time.Time
	flapDampedLoggedWindow    time.Time

	// legacy caches legacySelectorEnabled() at construction time — immutable
	// for the TransportManager's lifetime (tests must t.Setenv BEFORE
	// constructing). When true, evaluateSelector and runProbeOnce fall back
	// to the pre-scoreboard handshake-streak-only semantics.
	// LEGACY (K2_CHANNEL_SELECTOR_LEGACY=1): pre-scoreboard behavior — remove after scoreboard proven stable.
	legacy bool
}

// NewTransportManager creates a new transport manager.
func NewTransportManager() *TransportManager {
	return &TransportManager{
		scoreQUIC:  NewChannelScore(),
		scoreTCPWS: NewChannelScore(),
		selState:   selectorState{active: "quic"},
		legacy:     legacySelectorEnabled(),
	}
}

// ActiveChannel returns which channel ("quic" or "tcpws") is currently
// serving traffic. The single source of truth for channel selection.
func (tm *TransportManager) ActiveChannel() string {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	if tm.selState.active == "" {
		return "quic"
	}
	return tm.selState.active
}

// ScoreQUIC returns the current QUIC channel quality score in [0,1].
func (tm *TransportManager) ScoreQUIC(now time.Time) float64 {
	return tm.scoreQUIC.Value(now)
}

// ScoreTCPWS returns the current TCP-WS channel quality score in [0,1].
func (tm *TransportManager) ScoreTCPWS(now time.Time) float64 {
	return tm.scoreTCPWS.Value(now)
}

// scoreFor returns the ChannelScore for a channel key, or nil for an
// unrecognized channel (defensive — callers pass "quic"/"tcpws" literals).
func (tm *TransportManager) scoreFor(channel string) *ChannelScore {
	switch channel {
	case "quic":
		return tm.scoreQUIC
	case "tcpws":
		return tm.scoreTCPWS
	default:
		return nil
	}
}

// recordDialOutcome feeds a real dial/handshake result into the channel's
// availability signal, gated by isProxyLayerError: a proxy-layer error means
// the wire transport worked and only the proxy/target-side failed (e.g.
// DialTCP's read-response timeout, or connect()'s own metadata-rejection
// path) — that is orthogonal to transport availability (spec §3.1
// correction) and must never trigger a channel switch. Availability never
// otherwise sees "per-dial" noise: every other error class (handshake
// failure, stream-open failure, write failure) IS a legitimate transport
// signal and is recorded.
//
// Called after the dial/connect completes, always outside tm.mu —
// ChannelScore has its own standalone lock (Lock Ordering: tm.mu ->
// ChannelScore.mu is fine elsewhere, but this call site never holds tm.mu).
func (tm *TransportManager) recordDialOutcome(channel string, err error, now time.Time) {
	sc := tm.scoreFor(channel)
	if sc == nil {
		return
	}
	if err != nil && isProxyLayerError(err) {
		return
	}
	sc.RecordAvailability(err == nil, now)
	if channel == "tcpws" {
		tm.backupFresh.Add(1)
	}
}

// RecordQUICStats feeds QUIC quality from congestion-control loss/RTT stats.
func (tm *TransportManager) RecordQUICStats(ws WireStats, now time.Time) {
	tm.scoreQUIC.RecordQuality(QualityFromLossRTT(ws.PacketLossRate, ws.RTT, ws.MinRTT), now)
}

// updateTCPWSMinEchoRTT updates (if lower) and returns the running minimum
// echo RTT observed on the tcpws channel.
func (tm *TransportManager) updateTCPWSMinEchoRTT(rtt time.Duration) time.Duration {
	for {
		cur := tm.tcpwsMinEchoRTT.Load()
		if cur != 0 && time.Duration(cur) <= rtt {
			return time.Duration(cur)
		}
		if tm.tcpwsMinEchoRTT.CompareAndSwap(cur, int64(rtt)) {
			return rtt
		}
	}
}

// RecordEchoResult feeds an echo-probe outcome into a channel's
// availability (and, for tcpws, quality — tcpws has no CC stats, so echo
// RTT inflation is its only quality signal). An IsEchoUnsupported error
// (old server without an echo handler) is benign and recorded as neither
// success nor failure.
func (tm *TransportManager) RecordEchoResult(channel string, rtt time.Duration, err error, now time.Time) {
	sc := tm.scoreFor(channel)
	if sc == nil {
		return
	}
	if err != nil {
		if IsEchoUnsupported(err) {
			return
		}
		sc.RecordAvailability(false, now)
		if channel == "tcpws" {
			tm.backupFresh.Add(1)
		}
		return
	}
	sc.RecordAvailability(true, now)
	if channel == "tcpws" {
		tm.backupFresh.Add(1)
		if rtt > 0 {
			minRTT := tm.updateTCPWSMinEchoRTT(rtt)
			sc.RecordQuality(QualityFromEchoRTT(rtt, minRTT), now)
		}
	}
}

// EvaluateSelector is the exported passthrough to evaluateSelector (Task 7)
// — engine/health.go's echoProbeLoop calls this once per probe cycle so the
// hysteresis FSM is driven on its own cadence rather than only piggy-backing
// on Dial*/Handshake traffic (this is the "later echoLoop" the comment below
// forecast).
func (tm *TransportManager) EvaluateSelector(now time.Time) {
	tm.evaluateSelector(now)
}

// SetChannelProbeInterval is the exported passthrough to the named channel's
// ChannelScore.SetProbeInterval (Task 7) — engine/health.go's echoProbeLoop
// calls this whenever it changes a channel's probe cadence (exponential
// trust schedule for the primary; 60s/5s/off for the backup) so
// ChannelScore's staleness gating tracks the cadence actually in effect. A
// no-op for an unrecognized channel key.
func (tm *TransportManager) SetChannelProbeInterval(channel string, d time.Duration) {
	if sc := tm.scoreFor(channel); sc != nil {
		sc.SetProbeInterval(d)
	}
}

// TCPWS returns the current TCP-WS transport (nil if unset). Exported (Task
// 7) so engine/health.go's echoProbeLoop can opportunistically probe the
// backup channel when active=="quic" and QUIC quality has degraded — see
// wire.TransportManager doc and qualityDegradeThreshold.
func (tm *TransportManager) TCPWS() Dialer {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	return tm.tcpws
}

// evaluateSelector reads scores + selector state under tm.mu and applies the
// hysteresis FSM's decision (selectorStep, Task 2). Called lazily at the end
// of every Dial*/Handshake call (Phase 1), and also from echoLoop's own
// cadence via the EvaluateSelector passthrough (Task 7).
//
// Task 8 (R1/R2) layers two rate-limited DIAG logs on top of the FSM
// decision, both scoped to selectorStep's tcpws-active (rollback) branch:
//
//   - DIAG: channel-review — a rollback-review status snapshot. Logged at
//     most once per review window while the decision stays "stay" (gated by
//     channelReviewLoggedWindow), plus unconditionally the moment the
//     decision actually IS the rollback (channelSwitchToQUIC) — switches are
//     rare by construction, so that one is never rate-limited.
//   - DIAG: transport-switch reason=flap-damped — fires when a rollback is
//     blocked purely by the flap-damping window (QUIC already
//     echo-verified healthy, but nextReviewAt hasn't arrived). Same
//     once-per-window gate, separate field: this typically fires LATER in
//     the window than the channel-review log above, since echo recovery
//     usually lands after the window opens rather than before it.
//
// Both gates key off newState.nextReviewAt's identity rather than a time
// interval: it only changes when a fresh flap round starts (switchAway), so
// comparing against it naturally yields "once per fallback episode" without
// needing to know the window's (doubling, 30s→600s) duration.
func (tm *TransportManager) evaluateSelector(now time.Time) channelDecision {
	// LEGACY (K2_CHANNEL_SELECTOR_LEGACY=1): pre-scoreboard behavior — remove after scoreboard proven stable.
	if tm.legacy {
		return tm.evaluateSelectorLegacy(now)
	}

	tm.mu.Lock()
	var consecFails int32
	if qc, ok := tm.primary.(*QUICClient); ok {
		consecFails = qc.ConsecConnFails()
	}
	in := selectorInput{
		now:               now,
		scoreQUIC:         tm.scoreQUIC.Value(now),
		scoreTCPWS:        tm.scoreTCPWS.Value(now),
		consecConnFails:   consecFails,
		backupFresh:       int(tm.backupFresh.Load()),
		quicEchoRecovered: tm.quicEchoRecovered.Load(),
	}
	newState, decision := selectorStep(tm.selState, in)
	tm.selState = newState

	logChannelReview := false
	reviewDecision := ""
	switch {
	case decision == channelSwitchToQUIC:
		logChannelReview = true
		reviewDecision = "switch-to-quic"
	case decision == channelStay && newState.active == "tcpws" &&
		tm.channelReviewLoggedWindow != newState.nextReviewAt:
		tm.channelReviewLoggedWindow = newState.nextReviewAt
		logChannelReview = true
		reviewDecision = "stay"
	}

	logFlapDamped := decision == channelStay && newState.active == "tcpws" &&
		in.quicEchoRecovered && in.now.Before(newState.nextReviewAt) &&
		tm.flapDampedLoggedWindow != newState.nextReviewAt
	if logFlapDamped {
		tm.flapDampedLoggedWindow = newState.nextReviewAt
	}
	tm.mu.Unlock()

	if logChannelReview {
		nextReviewInS := int(newState.nextReviewAt.Sub(now).Seconds())
		if nextReviewInS < 0 {
			nextReviewInS = 0
		}
		slog.Info("DIAG: channel-review",
			"activeChannel", newState.active,
			"scoreQuic", in.scoreQUIC,
			"scoreTcpws", in.scoreTCPWS,
			"flapCount", newState.flapCount,
			"nextReviewInS", nextReviewInS,
			"decision", reviewDecision,
		)
	}
	if logFlapDamped {
		// Not an actual transition (from==to=="tcpws") — reused shape so
		// existing `grep "DIAG: transport-switch"` tooling still catches it.
		slog.Warn("DIAG: transport-switch", "from", "tcpws", "to", "tcpws", "reason", "flap-damped")
	}

	switch decision {
	case channelSwitchToTCPWS:
		reason := "quic-quality-degraded"
		if in.consecConnFails >= quicConnFailThreshold {
			reason = "quic-handshake-threshold"
		}
		slog.Warn("wire: switching to TCP-WS", "reason", reason, "scoreQUIC", in.scoreQUIC, "scoreTCPWS", in.scoreTCPWS)
		slog.Warn("DIAG: transport-switch", "from", "quic", "to", "tcpws", "reason", reason)
	case channelSwitchToQUIC:
		// Consume the one-shot verification: the next time QUIC falls back,
		// it must re-earn a fresh handshake + 3 echo rounds before it is
		// eligible to roll back again.
		tm.quicEchoRecovered.Store(false)
		slog.Info("wire: rolling back to QUIC (echo-verified)", "scoreQUIC", in.scoreQUIC, "scoreTCPWS", in.scoreTCPWS)
		slog.Info("DIAG: transport-switch", "from", "tcpws", "to", "quic", "reason", "quic-recovered")
	}
	return decision
}

// evaluateSelectorLegacy is the switch-away half of the
// K2_CHANNEL_SELECTOR_LEGACY=1 rollback valve: only the fast path applies
// (consecutive QUIC handshake failures) — the quality path (score ratio /
// sustain window / backupFresh gate) is disabled entirely. Switch-back is
// NOT decided here: in legacy mode it happens synchronously inside
// runProbeOnce right after a bare handshake success (see its own LEGACY
// fence), so this function never looks at the active=="tcpws" branch at
// all. switchAway's flap-window bookkeeping still runs when this fires
// (harmless — nextReviewAt is simply never consulted by the legacy
// rollback path, so it can never block it).
// LEGACY (K2_CHANNEL_SELECTOR_LEGACY=1): pre-scoreboard behavior — remove after scoreboard proven stable.
func (tm *TransportManager) evaluateSelectorLegacy(now time.Time) channelDecision {
	tm.mu.Lock()
	var consecFails int32
	if qc, ok := tm.primary.(*QUICClient); ok {
		consecFails = qc.ConsecConnFails()
	}
	if tm.selState.active == "tcpws" || consecFails < quicConnFailThreshold {
		tm.mu.Unlock()
		return channelStay
	}
	tm.selState = switchAway(tm.selState, now)
	scoreQUIC := tm.scoreQUIC.Value(now)
	scoreTCPWS := tm.scoreTCPWS.Value(now)
	tm.mu.Unlock()

	slog.Warn("wire: switching to TCP-WS", "reason", "quic-handshake-threshold", "scoreQUIC", scoreQUIC, "scoreTCPWS", scoreTCPWS)
	slog.Warn("DIAG: transport-switch", "from", "quic", "to", "tcpws", "reason", "quic-handshake-threshold")
	return channelSwitchToTCPWS
}

// SetPrimary sets the primary transport with its type label.
func (tm *TransportManager) SetPrimary(d Dialer, transportType string) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.primary = d
	tm.primaryType = transportType
}

// Primary returns the current primary transport.
func (tm *TransportManager) Primary() Dialer {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	return tm.primary
}

// PrimaryType returns the type label of the current primary transport.
func (tm *TransportManager) PrimaryType() string {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	return tm.primaryType
}

// ReplacePrimary hot-swaps the primary transport during runtime re-race.
// The new primary is unrelated to whatever the old one was doing, so the
// selector resets to "quic" and any echo-verification from the old primary
// is discarded — it says nothing about the new one.
func (tm *TransportManager) ReplacePrimary(d Dialer, transportType string) {
	tm.mu.Lock()
	old := tm.primary
	tm.primary = d
	tm.primaryType = transportType
	tm.selState = selectorState{active: "quic"}
	tm.channelReviewLoggedWindow = time.Time{}
	tm.flapDampedLoggedWindow = time.Time{}
	tm.mu.Unlock()
	tm.quicEchoRecovered.Store(false)

	// Close old primary outside lock (Rule #4).
	if old != nil {
		tm.mu.RLock()
		isTCPWS := old == tm.tcpws
		tm.mu.RUnlock()
		if !isTCPWS {
			old.Close()
		}
	}

	slog.Info("wire: primary transport replaced",
		"type", transportType,
		"hadOld", old != nil,
	)
}

// SetTCPWS sets the TCP-WS transport.
func (tm *TransportManager) SetTCPWS(d Dialer) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.tcpws = d
}

// SwapTCPWS replaces the TCP-WS transport and returns the old one.
// The caller is responsible for closing the returned dialer (outside any lock).
func (tm *TransportManager) SwapTCPWS(d Dialer) Dialer {
	tm.mu.Lock()
	old := tm.tcpws
	tm.tcpws = d
	tm.mu.Unlock()
	return old
}

// recordQuicSuccess resets the QUIC handshake failure streak after a
// successful dial. It deliberately does NOT clear fallback — recovery from
// tcpws back to quic requires echo verification (evaluateSelector +
// quicEchoRecovered), never a bare handshake/dial success (root cause A).
func (tm *TransportManager) recordQuicSuccess() {
	tm.mu.RLock()
	qc, ok := tm.primary.(*QUICClient)
	tm.mu.RUnlock()
	if ok {
		qc.RecordSuccess()
	}
}

// SetFallbackActive sets or clears the QUIC→TCP-WS fallback flag.
func (tm *TransportManager) SetFallbackActive(active bool) {
	tm.SetFallbackActiveWithReason(active, "quic-handshake-threshold")
}

// SetFallbackActiveWithReason sets or clears fallback with a specific reason
// for diagnostics. Compat shim over selState: still needed for the
// race-lost QUICBackup install path (engine.go installs QUIC as primary
// with fallback=true when tcpws won the transport race) and for manual
// overrides. Idempotent — calling with the same value twice does not
// re-trigger flap bookkeeping or logging.
func (tm *TransportManager) SetFallbackActiveWithReason(active bool, reason string) {
	tm.mu.Lock()
	var didSwitch bool
	if active {
		if tm.selState.active != "tcpws" {
			tm.selState = switchAway(tm.selState, time.Now())
			didSwitch = true
		}
	} else if tm.selState.active != "quic" {
		tm.selState.active = "quic"
		tm.selState.aboveSince = time.Time{}
	}
	tm.mu.Unlock()

	if active && didSwitch {
		slog.Warn("wire: QUIC fallback activated", "reason", reason)
		slog.Warn("DIAG: transport-switch", "from", "quic", "to", "tcpws", "reason", reason)
	}
}

// FallbackActive returns whether QUIC has failed and TCP-WS is serving traffic.
func (tm *TransportManager) FallbackActive() bool {
	return tm.ActiveChannel() == "tcpws"
}

// DialTCP opens a TCP proxy connection.
// Prefers QUIC, falls back to TCP-WS. Tracks fallback state for recovery.
func (tm *TransportManager) DialTCP(ctx context.Context, addr string, earlyData []byte) (net.Conn, error) {
	tm.mu.RLock()
	primary := tm.primary
	primaryType := tm.primaryType
	tcpws := tm.tcpws
	tm.mu.RUnlock()

	active := tm.ActiveChannel()
	primaryChannel := channelForType(primaryType)

	slog.Debug("TransportManager: DialTCP", "addr", addr, "hasPrimary", primary != nil, "hasTCPWS", tcpws != nil)

	var primaryErr error

	// Try primary first (preferred), skip if fallback is active.
	if primary != nil && active != "tcpws" {
		quicCtx, quicCancel := tm.quicContext(ctx, tcpws != nil)
		slog.Debug("TransportManager: trying primary first")
		conn, err := primary.DialTCP(quicCtx, addr, earlyData)
		quicCancel()
		now := time.Now()
		tm.recordDialOutcome(primaryChannel, err, now)
		if err == nil {
			if qc, ok := primary.(*QUICClient); ok {
				qc.RecordSuccess()
			}
			tm.evaluateSelector(now)
			return conn, nil
		}
		// Server responded with error — transport worked, proxy layer failed.
		// TCP-WS hits the same server; fallback won't help.
		if isProxyLayerError(err) {
			slog.Debug("TransportManager: primary proxy error, skipping TCP-WS", "err", err)
			tm.evaluateSelector(now)
			return nil, err
		}

		slog.Debug("TransportManager: primary transport error, trying TCP-WS", "err", err)
		primaryErr = err
	} else if primary != nil {
		slog.Debug("TransportManager: primary skipped, fallback active")
	}

	// Fall back to TCP-WS
	if tcpws != nil {
		slog.Debug("TransportManager: trying TCP-WS fallback")
		conn, err := tcpws.DialTCP(ctx, addr, earlyData)
		now := time.Now()
		tm.recordDialOutcome("tcpws", err, now)
		tm.evaluateSelector(now)
		if err == nil {
			return conn, nil
		}
		slog.Debug("TransportManager: TCP-WS failed", "err", err)
		if primaryErr != nil {
			return nil, &MultiTransportError{QUICErr: primaryErr, TCPWSErr: err}
		}
		return nil, fmt.Errorf("TCP-WS: %w", err)
	}

	tm.evaluateSelector(time.Now())
	if primaryErr != nil {
		return nil, fmt.Errorf("QUIC: %w", primaryErr)
	}
	return nil, ErrNotConnected
}

// DialUDP opens a UDP proxy connection.
// Prefers QUIC (datagram), falls back to TCP-WS (smux stream).
// ErrDatagramRelayUnhealthy is not counted as a QUIC transport failure (F1
// handles it) — it is a relay-layer signal, orthogonal to transport
// availability, so it is excluded from recordDialOutcome entirely (neither
// success nor failure).
func (tm *TransportManager) DialUDP(ctx context.Context, addr string) (net.PacketConn, error) {
	tm.mu.RLock()
	primary := tm.primary
	primaryType := tm.primaryType
	tcpws := tm.tcpws
	tm.mu.RUnlock()

	active := tm.ActiveChannel()
	primaryChannel := channelForType(primaryType)

	slog.Debug("TransportManager: DialUDP", "addr", addr, "hasPrimary", primary != nil, "hasTCPWS", tcpws != nil)

	var primaryErr error

	if primary != nil && active != "tcpws" {
		quicCtx, quicCancel := tm.quicContext(ctx, tcpws != nil)
		slog.Debug("TransportManager: trying primary for UDP")
		conn, err := primary.DialUDP(quicCtx, addr)
		quicCancel()
		now := time.Now()
		if !errors.Is(err, ErrDatagramRelayUnhealthy) {
			tm.recordDialOutcome(primaryChannel, err, now)
		}
		if err == nil {
			slog.Debug("TransportManager: primary UDP success", "addr", addr)
			tm.evaluateSelector(now)
			return conn, nil
		}
		// Server responded with error — transport worked, proxy layer failed.
		if isProxyLayerError(err) {
			slog.Debug("TransportManager: primary UDP proxy error, skipping TCP-WS", "err", err)
			tm.evaluateSelector(now)
			return nil, err
		}

		slog.Debug("TransportManager: primary UDP transport error, trying TCP-WS", "err", err)
		primaryErr = err
	} else if primary != nil {
		slog.Debug("TransportManager: primary UDP skipped, fallback active")
	}

	if tcpws != nil {
		slog.Debug("TransportManager: trying TCP-WS for UDP fallback")
		conn, err := tcpws.DialUDP(ctx, addr)
		now := time.Now()
		tm.recordDialOutcome("tcpws", err, now)
		tm.evaluateSelector(now)
		if err == nil {
			slog.Debug("TransportManager: TCP-WS UDP success", "addr", addr)
			return conn, nil
		}
		slog.Debug("TransportManager: TCP-WS UDP failed", "err", err)
		if primaryErr != nil {
			return nil, &MultiTransportError{QUICErr: primaryErr, TCPWSErr: err}
		}
		return nil, fmt.Errorf("TCP-WS: %w", err)
	}

	tm.evaluateSelector(time.Now())
	if primaryErr != nil {
		return nil, fmt.Errorf("QUIC: %w", primaryErr)
	}
	return nil, ErrNotConnected
}

// DialStreamUDP opens a stream-backed UDP session (reliable).
// Prefers QUIC stream, falls back to TCP-WS (already stream-based).
func (tm *TransportManager) DialStreamUDP(ctx context.Context, addr string) (net.PacketConn, error) {
	tm.mu.RLock()
	primary := tm.primary
	primaryType := tm.primaryType
	tcpws := tm.tcpws
	tm.mu.RUnlock()

	active := tm.ActiveChannel()
	primaryChannel := channelForType(primaryType)

	slog.Debug("TransportManager: DialStreamUDP", "addr", addr, "hasPrimary", primary != nil, "hasTCPWS", tcpws != nil)

	var primaryErr error

	if primary != nil && active != "tcpws" {
		quicCtx, quicCancel := tm.quicContext(ctx, tcpws != nil)
		conn, err := primary.DialStreamUDP(quicCtx, addr)
		quicCancel()
		now := time.Now()
		tm.recordDialOutcome(primaryChannel, err, now)
		if err == nil {
			tm.evaluateSelector(now)
			return conn, nil
		}
		if isProxyLayerError(err) {
			tm.evaluateSelector(now)
			return nil, err
		}
		slog.Debug("TransportManager: primary stream UDP failed, trying TCP-WS", "err", err)
		primaryErr = err
	}

	if tcpws != nil {
		conn, err := tcpws.DialStreamUDP(ctx, addr)
		now := time.Now()
		tm.recordDialOutcome("tcpws", err, now)
		tm.evaluateSelector(now)
		if err == nil {
			return conn, nil
		}
		if primaryErr != nil {
			return nil, &MultiTransportError{QUICErr: primaryErr, TCPWSErr: err}
		}
		return nil, fmt.Errorf("TCP-WS: %w", err)
	}

	tm.evaluateSelector(time.Now())
	if primaryErr != nil {
		return nil, fmt.Errorf("QUIC: %w", primaryErr)
	}
	return nil, ErrNotConnected
}

// Handshake verifies wire connectivity by completing the transport handshake
// (QUIC or TCP-WS) without opening a proxy stream or dialing any target.
// The cached connection is reused by subsequent DialTCP/DialUDP calls.
//
// Follows the same QUIC-preferred, TCP-WS-fallback logic as DialTCP:
// - Try QUIC connect first (skip if fallback active)
// - On QUIC failure, track handshake streak + try TCP-WS
// - Return MultiTransportError if both fail
func (tm *TransportManager) Handshake(ctx context.Context) error {
	tm.mu.RLock()
	primary := tm.primary
	tcpws := tm.tcpws
	tm.mu.RUnlock()

	active := tm.ActiveChannel()

	slog.Debug("TransportManager: Handshake", "hasPrimary", primary != nil, "hasTCPWS", tcpws != nil)

	var primaryErr error

	// Try primary first (preferred), skip if fallback is active.
	if primary != nil && active != "tcpws" {
		quicCtx, quicCancel := tm.quicContext(ctx, tcpws != nil)
		slog.Debug("TransportManager: Handshake trying primary")
		if qc, ok := primary.(*QUICClient); ok {
			_, err := qc.connect(quicCtx)
			quicCancel()
			now := time.Now()
			tm.recordDialOutcome("quic", err, now)
			if err == nil {
				qc.RecordSuccess()
				tm.evaluateSelector(now)
				slog.Info("TransportManager: Handshake primary ok")
				return nil
			}
			slog.Debug("TransportManager: Handshake primary failed", "err", err)
			primaryErr = err
			tm.evaluateSelector(now)
		} else {
			quicCancel()
		}
	}

	// Fall back to TCP-WS.
	if tcpws != nil {
		slog.Debug("TransportManager: Handshake trying TCP-WS")
		if tc, ok := tcpws.(*TCPWSClient); ok {
			_, err := tc.connect(ctx)
			now := time.Now()
			tm.recordDialOutcome("tcpws", err, now)
			tm.evaluateSelector(now)
			if err == nil {
				slog.Info("TransportManager: Handshake TCP-WS ok")
				return nil
			}
			slog.Debug("TransportManager: Handshake TCP-WS failed", "err", err)
			if primaryErr != nil {
				return &MultiTransportError{QUICErr: primaryErr, TCPWSErr: err}
			}
			return fmt.Errorf("TCP-WS: %w", err)
		}
	}

	if primaryErr != nil {
		return fmt.Errorf("QUIC: %w", primaryErr)
	}
	return ErrNotConnected
}

// quicContext returns a sub-context for QUIC with a time budget.
// When TCP-WS fallback exists, QUIC gets at most half the remaining time
// (capped at 5s) so TCP-WS still has a chance if QUIC is truly dead.
// Budget is generous because we trust QUIC — even a congested QUIC path
// is better than falling back to TCP-WS.
func (tm *TransportManager) quicContext(parent context.Context, hasFallback bool) (context.Context, context.CancelFunc) {
	if !hasFallback {
		return context.WithCancel(parent)
	}

	const (
		maxQUICBudget   = 5 * time.Second // steady-state: trust QUIC, give it time
		reconnectBudget = 8 * time.Second // post-reset: full handshake needs more time
	)

	// First QUIC dial after Reset gets extended budget for
	// DNS + TLS/ECH + QUIC handshake on a potentially new network path.
	budget := maxQUICBudget
	if tm.freshAfterReset.CompareAndSwap(true, false) {
		budget = reconnectBudget
	}

	if deadline, ok := parent.Deadline(); ok {
		half := time.Until(deadline) / 2
		if half > budget {
			half = budget
		}
		if half < time.Second {
			half = time.Second
		}
		return context.WithTimeout(parent, half)
	}

	return context.WithTimeout(parent, budget)
}

// StartQuicProbe starts a background goroutine that actively probes QUIC
// recovery while TCP-WS fallback is active. The probe resets the cached
// QUIC connection and attempts a fresh handshake every quicProbeInterval.
// Call this after SetPrimary/SetTCPWS. Stopped by Close() or Reset().
func (tm *TransportManager) StartQuicProbe(ctx context.Context) {
	// Cancel any existing probe.
	if tm.probeCancel != nil {
		tm.probeCancel()
	}
	probeCtx, cancel := context.WithCancel(ctx)
	tm.probeCancel = cancel
	go tm.quicProbeLoop(probeCtx)
}

func (tm *TransportManager) quicProbeLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(quicProbeInterval):
		}
		tm.runProbeOnce(ctx)
	}
}

// runProbeOnce executes one recovery-probe cycle: while tcpws is serving
// traffic, attempt a fresh QUIC handshake, then echo-verify the data path
// with 3 consecutive rounds. Only echo-verified recovery is eligible for
// rollback — evaluateSelector (not this probe) owns the actual switch
// decision. This is the root cause A fix: the old probe cleared fallback
// right after a bare handshake success; this one requires the data path to
// also prove itself before quicEchoRecovered is ever set.
//
// All connect + echo I/O happens without tm.mu held (check-release-act
// pattern): tm.primary is read once under RLock, then every blocking call
// runs unlocked.
func (tm *TransportManager) runProbeOnce(ctx context.Context) {
	if !tm.FallbackActive() {
		return
	}

	tm.mu.RLock()
	p := tm.primary
	tm.mu.RUnlock()
	if p == nil {
		return
	}

	// Force a fresh handshake attempt for the real QUICClient — any cached
	// connection is presumed dead (that's why we're in fallback).
	if qc, ok := p.(*QUICClient); ok {
		qc.resetConnection()
		probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		_, err := qc.connect(probeCtx)
		cancel()
		now := time.Now()
		tm.recordDialOutcome("quic", err, now)
		if err != nil {
			slog.Debug("wire: QUIC probe handshake failed", "err", err)
			tm.quicEchoRecovered.Store(false)
			return
		}

		// LEGACY (K2_CHANNEL_SELECTOR_LEGACY=1): pre-scoreboard behavior — remove after scoreboard proven stable.
		// Old root-cause-A rollback: a bare handshake success switches back
		// to QUIC immediately — no echo verification, no quicEchoRecovered
		// gate, no nextReviewAt wait. This is deliberately what a rollback
		// valve is for. Equivalent to the pre-Task-3
		// SetFallbackActiveWithReason(false, "quic-probe-success") path,
		// inlined here because that shim's false branch does not itself log
		// (only its active=true branch does).
		if tm.legacy {
			tm.recordQuicSuccess()
			tm.mu.Lock()
			wasTCPWS := tm.selState.active == "tcpws"
			if wasTCPWS {
				tm.selState.active = "quic"
				tm.selState.aboveSince = time.Time{}
			}
			tm.mu.Unlock()
			if wasTCPWS {
				slog.Info("wire: rolling back to QUIC (legacy handshake-only)")
				slog.Info("DIAG: transport-switch", "from", "tcpws", "to", "quic", "reason", "quic-probe-success")
			}
			return
		}
	}

	prober, ok := p.(EchoProber)
	if !ok {
		// No data-path verification available for this dialer — cannot
		// echo-verify recovery, so it can never be eligible for rollback.
		tm.quicEchoRecovered.Store(false)
		return
	}

	echoOK := true
	for i := 0; i < 3; i++ {
		echoCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		err := prober.RunEchoProbe(echoCtx)
		cancel()
		tm.RecordEchoResult("quic", 0, err, time.Now())
		if err != nil && !IsEchoUnsupported(err) {
			echoOK = false
			break
		}
	}
	tm.quicEchoRecovered.Store(echoOK)
	if echoOK {
		slog.Info("wire: QUIC probe echo-verified, eligible for rollback")
	} else {
		slog.Debug("wire: QUIC probe echo verification failed")
	}
	// NOTE: do NOT clear fallback here — evaluateSelector owns the switch
	// decision (reads quicEchoRecovered + scores + the flap-damping window).
}

// forceActiveForTest sets the selector's active channel directly, bypassing
// the hysteresis FSM. Test-only.
func (tm *TransportManager) forceActiveForTest(channel string) {
	tm.mu.Lock()
	tm.selState = selectorState{active: channel}
	tm.mu.Unlock()
}

// runProbeOnceForTest runs exactly one probe cycle synchronously, bypassing
// the quicProbeInterval ticker. Test-only.
func (tm *TransportManager) runProbeOnceForTest(ctx context.Context) {
	tm.runProbeOnce(ctx)
}

// Reset implements the Dialer Reset contract: resets cached connections on
// the primary and TCP-WS transports and clears fallback state, giving QUIC
// a fresh chance on the next dial. Called by engine on network change.
func (tm *TransportManager) Reset() {
	slog.Debug("wire: connections reset",
		"wasFallback", tm.FallbackActive(),
	)
	tm.mu.RLock()
	p := tm.primary
	t := tm.tcpws
	tm.mu.RUnlock()

	if p != nil {
		p.Reset()
	}
	if t != nil {
		t.Reset()
	}

	tm.mu.Lock()
	tm.selState = selectorState{active: "quic"}
	tm.channelReviewLoggedWindow = time.Time{}
	tm.flapDampedLoggedWindow = time.Time{}
	tm.mu.Unlock()
	tm.quicEchoRecovered.Store(false)
	tm.backupFresh.Store(0)
	tm.freshAfterReset.Store(true)
}

// TCPRecentlyHealthy returns true if a TCP stream succeeded within the given window.
// Used by health monitor to skip destructive port hops when TCP is working fine.
func (tm *TransportManager) TCPRecentlyHealthy(window time.Duration) bool {
	tm.mu.RLock()
	p := tm.primary
	tm.mu.RUnlock()
	if qc, ok := p.(*QUICClient); ok {
		return qc.tcpRecentlyHealthy(window)
	}
	return false
}

// WireStats returns stats from the active transport.
// When in fallback mode, reports TCP-WS stats with WireFallback flag set.
func (tm *TransportManager) WireStats() WireStats {
	tm.mu.RLock()
	p, t := tm.primary, tm.tcpws
	tm.mu.RUnlock()

	if tm.ActiveChannel() == "tcpws" {
		if sp, ok := t.(StatsProvider); ok {
			ws := sp.WireStats()
			ws.WireFallback = true
			return ws
		}
	}

	if sp, ok := p.(StatsProvider); ok {
		ws := sp.WireStats()
		if ws.Transport == "quic" {
			tm.RecordQUICStats(ws, time.Now())
		}
		return ws
	}
	if sp, ok := t.(StatsProvider); ok {
		return sp.WireStats()
	}
	return WireStats{}
}

// Close closes both transports and stops the probe goroutine.
// probeCancel is accessed without tm.mu — safe because engine state machine
// guarantees Start/Close serialization (no concurrent callers).
func (tm *TransportManager) Close() error {
	if tm.probeCancel != nil {
		tm.probeCancel()
		tm.probeCancel = nil
	}

	// Extract refs under lock, nil them, close outside (Rule #4).
	tm.mu.Lock()
	primary := tm.primary
	tm.primary = nil
	tcpws := tm.tcpws
	tm.tcpws = nil
	tm.mu.Unlock()

	var firstErr error
	// Avoid double-close when primary == tcpws (tcpws won the race).
	if primary != nil && primary != tcpws {
		if err := primary.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if tcpws != nil {
		if err := tcpws.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
