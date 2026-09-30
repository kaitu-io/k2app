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
	// ResponseTimeout marks the one proxy-layer outcome in which the server
	// did NOT answer: the stream opened and the header was written, but the
	// response read hit its deadline. Every other proxy-layer error carries
	// a server response (a status byte, a rejection), which is proof the
	// data path round-trips. The transport-selection layer treats the two
	// differently (see TransportManager.recordQUICDataPath): a run of
	// unanswered dials is the only client-side signal a QUIC path that
	// passes handshakes and echoes but drops bulk (ticket #3747/#3757) is
	// guaranteed to produce — CC loss can sit at 0 for minutes when the
	// sender is app-limited, and the health FSM never leaves healthy.
	ResponseTimeout bool
	// Unanswered is the wider class: the response read failed for ANY
	// reason (deadline, EOF, a connection error surfacing on the stream).
	// The server sent no status. Only ResponseTimeout feeds the distress
	// streak (a connection reset would otherwise turn one event into three
	// instant "timeouts"), but none of these may count as proof that the
	// data path answered (selectorState.trialProven).
	Unanswered bool
}

func (e *ProxyLayerError) Error() string           { return e.Err.Error() }
func (e *ProxyLayerError) Unwrap() error           { return e.Err }
func (e *ProxyLayerError) IsProxyLayer() bool      { return true }
func (e *ProxyLayerError) IsResponseTimeout() bool { return e.ResponseTimeout }
func (e *ProxyLayerError) IsUnanswered() bool      { return e.Unanswered || e.ResponseTimeout } // a timeout is by definition unanswered

// responseTimeoutErr / unansweredErr are the classification interfaces for
// "the request was sent and the server never answered" (deadline
// specifically / any read failure). Implemented by the producer
// (ProxyLayerError via readResponseError); consumers ask, never enumerate.
type responseTimeoutErr interface {
	IsResponseTimeout() bool
}
type unansweredErr interface {
	IsUnanswered() bool
}

// isProxyLayerError returns true when the Dialer has marked the error
// as proxy-layer (server responded, transport worked). Checked via
// interface — no type enumeration needed.
func isProxyLayerError(err error) bool {
	var pe proxyLayerErr
	return errors.As(err, &pe) && pe.IsProxyLayer()
}

// isResponseTimeout reports whether err is a proxy-layer error whose
// producer marked it as an unanswered request (ReadStreamResponse deadline).
func isResponseTimeout(err error) bool {
	var te responseTimeoutErr
	return errors.As(err, &te) && te.IsResponseTimeout()
}

// isUnanswered reports whether err is a proxy-layer error whose producer
// marked it as carrying no server status at all (any ReadStreamResponse
// failure). A StreamError / ErrStreamRejected is a status and is not.
func isUnanswered(err error) bool {
	var ue unansweredErr
	return errors.As(err, &ue) && ue.IsUnanswered()
}

// readResponseError classifies a ReadStreamResponse failure at the point of
// creation — the single producer for both transports' DialTCP. A timeout
// (net.Error.Timeout(), which is how quic-go's stream deadline and
// os.ErrDeadlineExceeded both report) is an unanswered request; anything
// else (EOF, reset, a malformed status) is a proxy-layer error the server at
// least reacted to. Both are proxy-layer: the wire carried the request, so
// per-request TCP-WS fallback to the same node is still pointless.
func readResponseError(err error) error {
	timeout := false
	var nErr net.Error
	if errors.As(err, &nErr) && nErr.Timeout() {
		timeout = true
	}
	return &ProxyLayerError{Err: fmt.Errorf("wire: read response: %w", err), ResponseTimeout: timeout, Unanswered: true}
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
	// failures before activating TCP-WS fallback.
	quicConnFailThreshold int32 = 3

	// Data-path distress (round 3, ticket #3757): the quic channel is in
	// distress when, among the primary-channel DialTCP requests of the last
	// dataPathWindow, at least quicRespTimeoutThreshold went UNANSWERED
	// (ReadStreamResponse deadline, 5s each — see readResponseError) AND
	// those make up at least 1/dataPathTimeoutShare of all requests in the
	// window. Three mirrors the handshake threshold: the handshake fast path
	// says "UDP is blocked", this one says "UDP passes the handshake and the
	// echoes but not the requests" — the carrier-QoS shape of tickets
	// #3747/#3757/#3733, which CC loss under-reports (app-limited senders
	// complete no MI, so the health FSM sits at healthy/degraded while dials
	// time out). A share rather than a consecutive streak because that
	// shape is MIXED, not solid: a fresh per-flow token bucket lets the
	// first requests of every new connection through (#3757's heartbeats:
	// dialOk=2 dialFail=1; the emulated link: 8 ok / 4 timed out per 30s),
	// and each re-race hands out a new one, so "three in a row" is rarely
	// reached even while a third of everything the user does dies. The
	// share protects the other direction: three timeouts on one dead
	// destination among thirty answered requests is a dead destination.
	quicRespTimeoutThreshold = 3
	dataPathTimeoutShare     = 3 // timeouts ≥ 1/3 of the window's quic requests
	dataPathWindow           = 60 * time.Second
	// dataPathRing bounds the window in events, not just time: enough for
	// any 60s of interactive use, and a browser's page-load burst (dozens
	// of concurrent dials) all landing as timeouts still fits.
	dataPathRing = 64
)

// dataPathEvent is one primary-channel quic DialTCP outcome that carries
// data-path evidence: answered (a server status arrived) or timed out.
type dataPathEvent struct {
	at       time.Time
	answered bool
}

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

	// minSwitchTargetScore is the positive-evidence floor for the quality
	// path's TARGET: the FSM may switch to tcpws only when tcpws has been
	// measured strictly better than "unknown". Unmeasured is neutral×neutral
	// = 0.25 (scoreNeutral²); a channel whose echoes are failing sits below
	// that after two failures (avail 1→0.4→0.16). The ratio alone cannot
	// express this: once QUIC collapses toward 0 (loss ≥ 0.5 zeroes
	// QualityFromLossRTT) any positive tcpws score satisfies scoreTCPWS >
	// scoreQUIC×1.5, which would move traffic onto a channel nobody has shown
	// to work — the lesser evil is not evidence. Gates the switch only, never
	// the sustain clock (aboveSince), so the first fresh backup sample does not
	// restart the 10s wait.
	minSwitchTargetScore = 0.3

	// rollbackTrialWindow: a rollback to QUIC is a TRIAL. If the selector has
	// to switch away again within this long, the echo proof that justified the
	// rollback did not predict what bulk traffic would see on that path — the
	// exact shape of ticket #3747 (carrier drops bulk UDP, handshake and small
	// echoes pass). The window is sized for the switch-away's own latency after
	// a rollback: fresh CC stats (a few s) + health critical (5 samples) +
	// the backup probe's 5s cadence × 2 samples + the 10s sustain ≈ 30-45s,
	// with slack for a cold tcpws dial. A degradation landing later than this
	// is treated as a new, unrelated event.
	rollbackTrialWindow = 120 * time.Second

	// stickyReviewWindow is the review window after the SECOND consecutive
	// failed trial: the path has now twice looked echo-healthy and twice failed
	// under load, so tcpws is the stable choice and QUIC gets one trial per
	// hour rather than one per flap window. A TTL rather than "until network
	// Reset" so a session that outlives the carrier's QoS episode still finds
	// its way home; an explicit network change (Reset) forgets the history
	// immediately, a same-path reconnect (ResetConnections) keeps it.
	stickyReviewWindow = 1 * time.Hour
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

	// rollbackAt is when traffic last RETURNED to quic — an echo-verified
	// rollback here, or a re-race installing a fresh quic primary while tcpws
	// was serving (ReplacePrimary). Zero until the first return. switchAway
	// reads it to decide whether this switch-away ends a failed trial.
	rollbackAt time.Time
	// failedTrials counts consecutive returns to quic that were followed by a
	// switch-away that grades the trial failed (switchAway): inside
	// rollbackTrialWindow, OR before the trial was ever proven. 1 → the next
	// review waits flapWindowMax; ≥2 → stickyReviewWindow. A switch-away
	// after a PROVEN stay longer than the window resets it to 0: that stay
	// was a successful trial and the new degradation is a fresh event.
	failedTrials int
	// trialProven is set once the quic data path has answered real requests
	// across rollbackTrialWindow of USE since the last return to quic: the
	// first server-answered DialTCP after the return stamps firstAnsweredAt,
	// and a later answered one at least rollbackTrialWindow after it proves
	// the trial (TransportManager.recordQUICDataPath). Cleared, with
	// firstAnsweredAt, by the rollback that starts a trial and by
	// ReplacePrimary from a serving tcpws. switchAway reads it: a trial that
	// never carried answered requests over a window of activity cannot be
	// graded a success by wall clock alone. Two shapes this must not mistake
	// for success (#3757 log: every reconnect answered ~6 dials, then died):
	// a user who rolled back, went idle for ten minutes and failed on the
	// first tap; and one whose first taps after the idle succeeded — a fresh
	// per-flow token bucket lets the first requests through — and died 20s
	// later. Both get the same escalation as a failure 20s after rollback.
	trialProven     bool
	firstAnsweredAt time.Time
}

// selectorInput is one evaluation's observations. All scores are ChannelScore.Value.
type selectorInput struct {
	now               time.Time
	scoreQUIC         float64
	scoreTCPWS        float64
	consecConnFails   int32  // QUIC handshake streak (fast path)
	backupFresh       int    // tcpws availability samples inside its staleness window (ChannelScore.FreshSamples)
	quicEchoRecovered bool   // background probe passed handshake + 3 echo rounds
	primaryChannel    string // channelForType(primaryType): which channel tm.primary actually is
	// dataPathDistressed: unanswered DialTCP requests on the quic channel
	// are ≥ quicRespTimeoutThreshold and ≥ 1/dataPathTimeoutShare of the
	// last dataPathWindow's requests (TransportManager.dataPathDistressedLocked).
	dataPathDistressed bool
}

// selectorStep is the pure transition. No time.Now, no I/O, no logging.
func selectorStep(st selectorState, in selectorInput) (selectorState, channelDecision) {
	if st.active != "tcpws" {
		// QUIC branch (default: empty string is treated as QUIC).
		//
		// Topology gate (final-review C1): this whole branch decides whether to
		// switch AWAY FROM the quic channel — which presupposes that a quic
		// channel exists. When tcpws won the startup race outright,
		// setupOutboundTransport installs the SAME *TCPWSClient as both
		// tm.primary and tm.tcpws while selState.active still defaults to
		// "quic"; the same shape arises after a re-race lands on tcpws
		// (ReplacePrimary resets active to "quic"). In that topology
		// scoreTCPWS is measured (~1.0, fed by the primary's own dials and
		// echoes) while scoreQUIC has no source at all and sits at the neutral
		// 0.25 floor, so `scoreTCPWS > scoreQUIC×1.5` holds permanently: the
		// FSM would "switch away" from a QUIC channel that does not exist,
		// flip FallbackActive, and log a fabricated
		// `transport-switch reason=quic-quality-degraded`. Stay put instead —
		// with no quic dialer there is nothing to switch away from, and the
		// per-request fallback in Dial* is unaffected either way.
		//
		// channelForType only ever yields "quic" or "tcpws", so the production
		// caller (evaluateSelector) always fills this in; the zero value is
		// reserved for selectorStep's own pure unit tests, which pin the FSM's
		// score/time algebra on the normal quic-primary topology.
		if in.primaryChannel == "tcpws" {
			return st, channelStay
		}

		// Fast path: handshake genuinely failing (UDP blocked).
		if in.consecConnFails >= quicConnFailThreshold {
			return switchAway(st, in.now), channelSwitchToTCPWS
		}

		// Data-path fast path: the handshake and the echoes pass, but real
		// requests go unanswered (quicRespTimeoutThreshold in a row). This is
		// the client-side signature of a UDP path that drops bulk while
		// letting small packets through (carrier QoS, tickets #3747/#3757),
		// and it is the ONLY signature such a path is guaranteed to leave:
		// CC loss needs a completed monitor interval to move, which an
		// app-limited sender may never produce, so the quality path below
		// can sit in its blind band (loss 0–0.25, health healthy/degraded)
		// for the whole session while every tap times out. No sustain window
		// — three unanswered 5s dials are already 15s of evidence — but the
		// same positive-evidence gate as the quality path: the target must
		// have been measured recently and measured better than "unknown".
		// The trade this accepts: a node that cannot reach ANY destination
		// (egress fault, #3463) produces the same run of timeouts and moves
		// the user onto tcpws to the same node. That is a channel choice,
		// not an outage — nothing worked before either — and the rollback
		// trial machinery brings QUIC back once requests are answered again.
		if in.dataPathDistressed && in.backupFresh >= backupFreshSamplesGate &&
			in.scoreTCPWS >= minSwitchTargetScore {
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

			if in.backupFresh >= backupFreshSamplesGate && held >= qualitySustainWindow &&
				in.scoreTCPWS >= minSwitchTargetScore {
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
	// comparison: while tcpws serves traffic QUIC carries no user traffic, so
	// its QUALITY component has no source (RecordQUICStats is CC-driven and
	// only fires for the active QUIC path) and stays pinned at neutral,
	// capping scoreQUIC at ≤0.5 — a `scoreQUIC > scoreTCPWS × k` gate is
	// impossible to satisfy against a healthy tcpws and would strand users on
	// the stateful fallback forever. (scoreQUIC is not frozen outright: the
	// AVAILABILITY component keeps moving, because runProbeOnce records its
	// handshake and echo outcomes under the "quic" channel. That is the whole
	// point — but availability alone can never lift Value above 0.5.)
	// Anti-thrash is the 3-round echo proof + nextReviewAt (30s→600s), not
	// scores.
	if in.now.Before(st.nextReviewAt) {
		return st, channelStay // flap-damped
	}
	if !in.quicEchoRecovered {
		return st, channelStay
	}
	st.active = "quic"
	st.aboveSince = time.Time{}
	st.rollbackAt = in.now // the trial starts now; switchAway grades it
	st.trialProven = false // ...and it has carried nothing yet
	st.firstAnsweredAt = time.Time{}
	return st, channelSwitchToQUIC
}

// switchAway transitions to tcpws and sets the rollback review window.
//
// Two schedules feed the window. The base schedule is the flap-damping
// doubling, flapWindowBase << flapCount capped at flapWindowMax. On top of it
// sits the trial escalation: if this switch-away grades the last return to
// quic a failed trial — it comes within rollbackTrialWindow of that return,
// OR the returned-to quic never answered a single request (trialProven
// false) — the first failed trial jumps straight to flapWindowMax, the
// second and later ones to stickyReviewWindow. Without the escalation an
// echo-passing, bulk-dropping path (ticket #3747) oscillates on the base
// schedule alone: every window ends in an echo-verified rollback that fails
// under load ~30s later, so the user eats a ~25s outage per window through the
// whole 30s→600s ramp, and anything that restarts the ramp (a re-race, a
// wake) starts it over. Without the trialProven clause the same user, if
// they merely paused for two minutes after the rollback, re-entered the base
// ramp: the wall clock said "QUIC held", when in truth nobody had asked it
// anything. A trial is graded by what QUIC carried, not by how long it sat.
func switchAway(st selectorState, now time.Time) selectorState {
	st.active = "tcpws"
	st.aboveSince = time.Time{}

	trialFailed := !st.rollbackAt.IsZero() &&
		(now.Sub(st.rollbackAt) <= rollbackTrialWindow || !st.trialProven)
	if trialFailed {
		st.failedTrials++
	} else {
		st.failedTrials = 0
	}

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
	switch {
	case st.failedTrials >= 2:
		window = stickyReviewWindow
	case st.failedTrials == 1:
		window = flapWindowMax
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
// QUIC is strongly preferred; TCP-WS is the fallback channel, chosen by the
// hysteresis FSM below when QUIC is unavailable (UDP blocked — the handshake
// fast path) OR measurably worse than a measured-good tcpws (the quality
// path). The second case is real: a carrier that QoS-drops bulk UDP while
// TCP flows clean lets the QUIC handshake and echoes through and still
// delivers nothing (ticket #3747), so "QUIC beats TCP-WS at any loss rate" is
// not a safe assumption — the scoreboard measures instead of assuming.
type TransportManager struct {
	mu          deadlock.RWMutex
	primary     Dialer // was: quic — the preferred transport (QUIC-443 or tcpws)
	tcpws       Dialer
	primaryType string // "quic-443" or "tcpws"

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

	// dpRing / dpIdx: the last dataPathRing primary-channel quic DialTCP
	// outcomes that carry data-path evidence (answered / timed out), fed by
	// recordQUICDataPath and read over dataPathWindow by
	// dataPathDistressedLocked. PATH-scoped evidence like the scores: a
	// same-path re-race (ReplacePrimary from quic), ResetConnections and a
	// wake all keep it (the window expires it on its own); Reset, the
	// rollback that starts a trial and ReplacePrimary from a serving tcpws
	// clear it — a trial must run on its own evidence, not on the timeouts
	// that sent traffic to tcpws in the first place. Protected by mu; read
	// by evaluateSelector into selectorInput.dataPathDistressed and by
	// DataPathDistressed for engine's backup-probe gate. dpDistressLogged
	// edge-gates the DIAG line (once per rising edge, not per timeout).
	dpRing           [dataPathRing]dataPathEvent
	dpIdx            int
	dpDistressLogged bool

	// The quality path's "backup measured fresh" gate (backupFreshSamplesGate)
	// reads scoreTCPWS.FreshSamples(now): tcpws availability samples (dial or
	// echo) inside the channel's own staleness window. It used to be a
	// monotonic since-Reset counter, which on a long-lived mobile process kept
	// the gate satisfied with samples from hours ago; the window makes "fresh"
	// mean the same thing the score's staleness gate means.

	// tcpwsMinEchoRTT is the running minimum echo RTT observed on the
	// tcpws channel — the baseline for QualityFromEchoRTT (tcpws has no
	// congestion-control stats, unlike QUIC).
	tcpwsMinEchoRTT atomic.Int64 // ns; 0 = unset

	// tcpwsLastTCPOKAt is when a real user TCP stream last succeeded over the
	// tcpws dialer — the tcpws-channel counterpart of QUICClient's
	// lastTCPSuccessAt, and the evidence TCPRecentlyHealthy reports when the
	// primary is a *TCPWSClient rather than a *QUICClient. Written on the
	// success paths of DialTCP only (a stream that carried a request is the
	// signal; UDP sessions and handshakes are not).
	tcpwsLastTCPOKAt atomic.Int64 // UnixNano; 0 = never

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
}

// recordQUICDataPath feeds one primary-channel DialTCP outcome on the quic
// channel into the data-path distress streak and the trial proof — the two
// signals recordDialOutcome deliberately does NOT carry, because it grades
// transport availability and a proxy-layer error says nothing about that.
//
//   - Answered (err == nil, or a proxy-layer error that carries a server
//     status — a StreamError / rejection, never a read failure): the data
//     path round-trips. Recorded as an answered event. The first answer
//     since the last return to quic stamps selState.firstAnsweredAt; an
//     answer rollbackTrialWindow or more after that proves the trial
//     (selectorState.trialProven) — one answered burst is what a fresh
//     per-flow token bucket hands out for free, sustained answering is not.
//   - Unanswered by deadline (isResponseTimeout): recorded as a timeout
//     event.
//   - Unanswered but not a timeout (EOF, a connection error surfacing on
//     the response read), or a transport-layer failure (handshake, stream
//     open, header write): no opinion here — neither proof nor distress
//     (a connection reset would otherwise turn one event into a burst of
//     "timeouts"); recordDialOutcome scores the transport-layer ones.
//
// Called after the dial completes, outside tm.mu; takes mu itself for the
// field writes only. Logs DIAG: quic-datapath-timeout on the rising edge of
// the distress predicate (outside the lock).
func (tm *TransportManager) recordQUICDataPath(err error, now time.Time) {
	var answered bool
	switch {
	case err == nil, isProxyLayerError(err) && !isUnanswered(err):
		answered = true
	case isResponseTimeout(err):
		answered = false
	default:
		return
	}
	tm.mu.Lock()
	tm.dpRing[tm.dpIdx] = dataPathEvent{at: now, answered: answered}
	tm.dpIdx = (tm.dpIdx + 1) % dataPathRing
	if answered {
		switch {
		case tm.selState.firstAnsweredAt.IsZero():
			tm.selState.firstAnsweredAt = now
		case now.Sub(tm.selState.firstAnsweredAt) >= rollbackTrialWindow:
			tm.selState.trialProven = true
		}
	}
	_, logEdge, timeouts, total := tm.dataPathEdgeLocked(now)
	tm.mu.Unlock()
	if logEdge {
		logDataPathEdge(timeouts, total)
	}
}

// dataPathEdgeLocked evaluates the distress predicate at `now` and tracks its
// rising edge for the DIAG line; caller holds tm.mu for writing and emits
// logDataPathEdge outside the lock when logEdge is set. Shared by
// recordQUICDataPath and evaluateSelector: distress can also flip purely by
// window expiry (the pre-outage successes age out while every new dial is
// still waiting on its deadline), and once the switch lands on tcpws no
// further quic dial is recorded — so an edge evaluated only at record time
// is missed for good (netns run s1-big-dense-mobile logged the switch with
// reason=quic-datapath-timeout but never the counts line).
func (tm *TransportManager) dataPathEdgeLocked(now time.Time) (distressed, logEdge bool, timeouts, total int) {
	timeouts, total = tm.dataPathCountsLocked(now)
	distressed = dataPathDistressed(timeouts, total)
	logEdge = distressed && !tm.dpDistressLogged
	tm.dpDistressLogged = distressed
	return distressed, logEdge, timeouts, total
}

func logDataPathEdge(timeouts, total int) {
	slog.Warn("DIAG: quic-datapath-timeout", "timeouts", timeouts, "requests", total, "windowS", int(dataPathWindow.Seconds()))
}

// dataPathCountsLocked counts the ring's events inside dataPathWindow;
// caller holds tm.mu (read or write).
func (tm *TransportManager) dataPathCountsLocked(now time.Time) (timeouts, total int) {
	for _, ev := range tm.dpRing {
		if ev.at.IsZero() || now.Sub(ev.at) > dataPathWindow {
			continue
		}
		total++
		if !ev.answered {
			timeouts++
		}
	}
	return timeouts, total
}

// dataPathDistressed is the pure predicate over one window's counts.
func dataPathDistressed(timeouts, total int) bool {
	return timeouts >= quicRespTimeoutThreshold && timeouts*dataPathTimeoutShare >= total
}

// dataPathDistressedLocked is the distress predicate at `now`; caller holds
// tm.mu (read or write).
func (tm *TransportManager) dataPathDistressedLocked(now time.Time) bool {
	return dataPathDistressed(tm.dataPathCountsLocked(now))
}

// DataPathDistressed reports whether the quic channel is in data-path
// distress at `now`: within the last dataPathWindow, at least
// quicRespTimeoutThreshold DialTCP requests went unanswered and they are at
// least 1/dataPathTimeoutShare of the window's requests. Exported for
// engine/health.go's backup-probe gate: on mobile it opens tcpws probing
// (alongside the loss-critical grade), on desktop it upgrades the probe
// cadence, so the selector's data-path fast path gets the fresh backup
// samples it requires. Meaningful only while quic is the active channel;
// callers gate on ActiveChannel themselves.
func (tm *TransportManager) DataPathDistressed(now time.Time) bool {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	return tm.dataPathDistressedLocked(now)
}

// resetDataPathLocked clears the distress evidence (see dpRing for when);
// caller holds tm.mu for writing.
func (tm *TransportManager) resetDataPathLocked() {
	tm.dpRing = [dataPathRing]dataPathEvent{}
	tm.dpIdx = 0
	tm.dpDistressLogged = false
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
//
// A successful tcpws echo with rtt <= 0 completed inside one tick of the
// caller's clock: on windows/amd64 Go's monotonic clock is
// KUSER_SHARED_DATA.InterruptTime, which only advances on a clock interrupt
// (0.5–15.6 ms), so the two time.Now() calls around a fast echo routinely
// read equal. That sample has no measurable inflation, so it counts as full
// quality; it never touches the min-RTT ratchet, whose zero value means
// "unset" and which a sub-tick reading could not serve as a baseline for.
// Dropping it instead left quality at neutral and scoreTCPWS pinned at 0.5.
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
		return
	}
	sc.RecordAvailability(true, now)
	if channel != "tcpws" {
		return
	}
	if rtt <= 0 {
		sc.RecordQuality(1.0, now)
		return
	}
	minRTT := tm.updateTCPWSMinEchoRTT(rtt)
	sc.RecordQuality(QualityFromEchoRTT(rtt, minRTT), now)
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
	distressed, logDistressEdge, dpTimeouts, dpTotal := tm.dataPathEdgeLocked(now)
	in := selectorInput{
		now:                now,
		scoreQUIC:          tm.scoreQUIC.Value(now),
		scoreTCPWS:         tm.scoreTCPWS.Value(now),
		consecConnFails:    consecFails,
		backupFresh:        tm.scoreTCPWS.FreshSamples(now),
		quicEchoRecovered:  tm.quicEchoRecovered.Load(),
		primaryChannel:     channelForType(tm.primaryType),
		dataPathDistressed: distressed,
	}
	newState, decision := selectorStep(tm.selState, in)
	tm.selState = newState
	if decision == channelSwitchToQUIC {
		// The trial starts with a clean slate: the timeouts that sent us to
		// tcpws were measured on the connection that fallback then dropped.
		// Kept, they would re-fire the data-path fast path on the very next
		// evaluation (tcpws samples are still fresh) before QUIC carried a
		// single request — the trial would be graded without ever running.
		tm.resetDataPathLocked()
	}

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
	if logDistressEdge {
		logDataPathEdge(dpTimeouts, dpTotal)
	}

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
			"failedTrials", newState.failedTrials,
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
		// Same precedence as selectorStep's branches.
		reason := "quic-quality-degraded"
		switch {
		case in.consecConnFails >= quicConnFailThreshold:
			reason = "quic-handshake-threshold"
		case in.dataPathDistressed:
			reason = "quic-datapath-timeout"
		}
		slog.Warn("wire: switching to TCP-WS", "reason", reason, "scoreQUIC", in.scoreQUIC, "scoreTCPWS", in.scoreTCPWS,
			"failedTrials", newState.failedTrials, "nextReviewInS", int(newState.nextReviewAt.Sub(now).Seconds()))
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
// The new primary is a fresh connection, so the selector's CONNECTION-scoped
// state restarts: active returns to "quic" (the race just verified it), the
// sustain clock and any echo-verification from the old primary are dropped.
// The PATH-scoped history survives — flapCount, failedTrials, rollbackAt —
// because a re-race redials the same server over the same network (a server
// replacement builds a whole new TransportManager, see
// engine.replaceOutboundServer). Wiping it here restarted the anti-flap ramp
// at 30s on every re-race, and on a lossy-but-reachable QUIC three echo
// timeouts in a row are routine (ticket #3747 logged 14 re-races), so the
// ramp never got past its first rungs.
//
// If tcpws was serving when the re-race landed, this install IS a return to
// quic — traffic moves to the new primary without the selector's echo gate —
// so it starts a rollback trial like an echo-verified rollback would. That
// also covers the race-lost install (engine re-arms fallback right after
// this call via SetFallbackActiveWithReason): the race itself just preferred
// tcpws over a connected QUIC, which the trial bookkeeping correctly grades
// as a failed trial.
func (tm *TransportManager) ReplacePrimary(d Dialer, transportType string) {
	now := time.Now()
	tm.mu.Lock()
	old := tm.primary
	tm.primary = d
	tm.primaryType = transportType
	prev := tm.selState
	tm.selState = selectorState{
		active:          "quic",
		flapCount:       prev.flapCount,
		failedTrials:    prev.failedTrials,
		rollbackAt:      prev.rollbackAt,
		trialProven:     prev.trialProven,
		firstAnsweredAt: prev.firstAnsweredAt,
	}
	if prev.active == "tcpws" {
		tm.selState.rollbackAt = now
		tm.selState.trialProven = false // a new trial, nothing carried yet
		tm.selState.firstAnsweredAt = time.Time{}
		tm.resetDataPathLocked() // the trial runs on its own evidence
	}
	// From quic the data-path evidence survives: a same-path re-race is
	// routine on exactly the link this evidence describes (echo timeouts
	// strike three times in seconds, faster than three 5s dial deadlines),
	// and each one hands a per-flow-policed connection a fresh burst.
	// Wiping the window here made the distress predicate unreachable.
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
		if primaryChannel == "quic" {
			tm.recordQUICDataPath(err, now)
		}
		if err == nil {
			if qc, ok := primary.(*QUICClient); ok {
				qc.RecordSuccess()
			} else if primaryChannel == "tcpws" {
				// tcpws-as-primary: QUICClient's own lastTCPSuccessAt can't
				// speak for this dialer — record here so TCPRecentlyHealthy
				// has an answer for it too.
				tm.recordTCPWSDialSuccess(now)
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
			tm.recordTCPWSDialSuccess(now)
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
	primaryChannel := channelForType(tm.primaryType)
	tm.mu.RUnlock()
	if p == nil {
		return
	}

	// This is the QUIC RECOVERY probe: everything below records under the
	// hardcoded "quic" channel and, on success, arms quicEchoRecovered — the
	// single gate evaluateSelector uses to roll tcpws→quic back. If the
	// primary is not the quic channel there is nothing to recover, and running
	// anyway would (a) echo-probe the tcpws primary that engine's own
	// echoProbeLoop is already probing, (b) record those tcpws samples as QUIC
	// availability, and (c) arm a rollback to a QUIC dialer that does not
	// exist — the fabricated `transport-switch reason=quic-recovered` half of
	// final-review C1. The pre-rewrite loop had this guard as
	// `qc, ok := p.(*QUICClient); if !ok { continue }`; it is restored here as
	// an explicit early return so the EchoProber section below can never be
	// reached by a non-QUIC primary.
	if primaryChannel != "quic" {
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
	// Freshness re-check (final-review I3): the loop above spends up to ~15s
	// in unlocked I/O, and evaluateSelector can complete a rollback (and
	// consume quicEchoRecovered via Store(false)) at any point during it.
	// Storing unconditionally here would re-arm the flag with THIS episode's
	// evidence after the episode already ended — so the NEXT fallback would
	// find a pre-armed flag and roll back without any fresh verification,
	// which is exactly the invariant (root cause A) this probe exists to
	// establish. If fallback is no longer active the result is stale by
	// definition: drop it and let the next episode's probe earn it again.
	if !tm.FallbackActive() {
		slog.Debug("wire: QUIC probe result discarded, fallback ended mid-probe", "echoOK", echoOK)
		return
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

// ResetConnections drops the cached connections on both transports (the
// next dial re-handshakes, with the post-reset budget) but keeps every
// measurement and selector decision intact: scores, fresh-sample windows,
// selState (active channel, sustain clock, flap and trial history) and the
// tcpws min-RTT ratchet.
//
// This is the reconnect for every "same path, new connection" event: the
// health monitor's wire-quality-critical reconnect, engine Pause/Wake (iOS
// calls those on every screen lock/unlock) and the wall-clock wake detector.
// The network path did not change — nothing said it did — so what the
// scoreboard has learned about that path is still true. Reset (below) is the
// network-CHANGE contract, reserved for an explicit platform network event,
// and wipes all of it. The distinction is load-bearing: with the health
// reconnect using Reset, a lossy-but-reachable QUIC could never be escaped —
// every reconnect zeroed the quality path's evidence faster than the backup
// probe rebuilt it, then redialed the same lossy path (ticket #3747) — and a
// rollback followed by a fresh critical zeroed flapCount, restarting the
// anti-flap window at its 30s base forever. With Pause/Wake using Reset, every
// phone pickup on such a path re-raced onto QUIC and replayed the whole
// escape from scratch.
//
// quicEchoRecovered is the one selector input this DOES clear: it certifies
// a specific QUIC connection's data path, and that connection is exactly what
// is being dropped. A stale certificate surviving an hours-long pause would
// roll traffic back to QUIC on the first evaluation after Wake with no fresh
// proof. runProbeOnce re-earns it within one probe interval while fallback is
// active, so the cost is at most that one interval of delayed rollback. (On
// the health-critical path the flag is already false: that reconnect only
// fires while quic is active, and the flag is consumed on every rollback.)
func (tm *TransportManager) ResetConnections() {
	slog.Debug("wire: connections reset (scoreboard preserved)",
		"activeChannel", tm.ActiveChannel(),
	)
	tm.resetDialers()
	tm.quicEchoRecovered.Store(false)
	tm.freshAfterReset.Store(true)
	// The data-path window (dpRing) is same-path evidence like the scores
	// and stays; dataPathWindow expires it on its own after a long pause.
}

// resetDialers calls the Dialer Reset contract on both transports, outside
// tm.mu. Shared by Reset and ResetConnections.
func (tm *TransportManager) resetDialers() {
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
}

// Reset implements the Dialer Reset contract: resets cached connections on
// the primary and TCP-WS transports and clears fallback state, giving QUIC
// a fresh chance on the next dial. Called by engine on an explicit network
// change (NetEvent / default-interface change) — everything measured so far
// was measured on a path that no longer exists. Sleep/wake, Pause/Wake and
// the health-critical reconnect are same-path events and use
// ResetConnections instead.
func (tm *TransportManager) Reset() {
	slog.Debug("wire: connections reset",
		"wasFallback", tm.FallbackActive(),
	)
	tm.resetDialers()

	tm.mu.Lock()
	tm.selState = selectorState{active: "quic"}
	tm.resetDataPathLocked()
	tm.channelReviewLoggedWindow = time.Time{}
	tm.flapDampedLoggedWindow = time.Time{}
	tm.mu.Unlock()
	tm.quicEchoRecovered.Store(false)
	tm.freshAfterReset.Store(true)

	// Every measurement below was taken on the OLD network path (spec §6,
	// "网络切换 Reset"). The EWMAs would eventually decay to neutral on their
	// own via staleness gating (~3× probeInterval), but tcpwsMinEchoRTT is a
	// monotonic ratchet that NEVER decays: carrying a 5 ms LAN minimum across
	// a move to an 80 ms mobile path pins QualityFromEchoRTT at ~0.06 for the
	// rest of the process lifetime, silently disabling the selector's quality
	// path. Clear both, so the new path is measured from scratch.
	// ChannelScore.Reset also clears the fresh-sample windows the quality
	// path's backupFreshSamplesGate reads.
	tm.scoreQUIC.Reset()
	tm.scoreTCPWS.Reset()
	tm.tcpwsMinEchoRTT.Store(0)
	tm.tcpwsLastTCPOKAt.Store(0)
}

// TCPRecentlyHealthy returns true if a TCP stream over the CURRENT PRIMARY
// succeeded within the given window. Used by engine/health.go to skip
// destructive corrective action (full reconnect, echo-strike re-race) while
// the primary is demonstrably still carrying user traffic.
//
// Both primary kinds answer for themselves. *QUICClient tracks its own
// lastTCPSuccessAt (cleared on its Reset). For a *TCPWSClient primary — the
// topology where tcpws won the startup race outright, or a re-race landed on
// tcpws — the answer comes from tm.tcpwsLastTCPOKAt, recorded on DialTCP's
// tcpws success paths. Before that second branch existed this method returned
// a flat false for every tcpws primary, which made any gate built on it a
// silent no-op in exactly the topology that needed it (final-review I4).
func (tm *TransportManager) TCPRecentlyHealthy(window time.Duration) bool {
	tm.mu.RLock()
	p := tm.primary
	tm.mu.RUnlock()
	if qc, ok := p.(*QUICClient); ok {
		return qc.tcpRecentlyHealthy(window)
	}
	if p == nil {
		return false
	}
	ts := tm.tcpwsLastTCPOKAt.Load()
	if ts == 0 {
		return false
	}
	return time.Since(time.Unix(0, ts)) < window
}

// TCPWSChannelRecentlyHealthy reports whether a TCP stream over the TCP-WS
// CHANNEL succeeded within the given window — regardless of which dialer is
// currently the primary. TCPRecentlyHealthy answers "is the PRIMARY proving
// itself?"; this answers "is the TCP-WS channel proving itself?". The two
// coincide when tcpws IS the primary, and diverge in the fallback topology
// (tcpws serving, QUIC still installed as primary), which is the case that
// needs this one: there, the serving channel is the one nobody else can
// vouch for.
//
// Deliberately fed ONLY by DialTCP (see recordTCPWSDialSuccess): that path
// reads a server response frame, so success is end-to-end proof of life.
func (tm *TransportManager) TCPWSChannelRecentlyHealthy(window time.Duration) bool {
	ts := tm.tcpwsLastTCPOKAt.Load()
	if ts == 0 {
		return false
	}
	return time.Since(time.Unix(0, ts)) < window
}

// recordTCPWSDialSuccess marks now as a successful TCP stream over the tcpws
// dialer. See tcpwsLastTCPOKAt / TCPRecentlyHealthy /
// TCPWSChannelRecentlyHealthy.
//
// ONLY DialTCP calls this, and that is load-bearing rather than an oversight.
// TCPWSClient.DialTCP writes the stream header and then blocks on
// ReadStreamResponse, so `err == nil` means the server answered — real
// proof-of-life, and any failure after the request was sent is classified as
// a ProxyLayerError, so it never reaches this success path. DialUDP/
// DialStreamUDP have no such round trip: they open a smux stream, write a
// header, and return without reading anything (and their fast path returns a
// cached mux with no I/O at all). Against a dead node those keep "succeeding"
// until smux's own keepalive finally tears the session down, so counting them
// would manufacture proof-of-life for exactly the node-death case these
// timestamps are consulted for.
func (tm *TransportManager) recordTCPWSDialSuccess(now time.Time) {
	tm.tcpwsLastTCPOKAt.Store(now.UnixNano())
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
		// Transport=="quic" is the guard: only the QUIC path has
		// congestion-control loss/RTT to convert into a quality sample.
		// PROTECTIVE NOTE — a QUIC WireStats with all-zero CC data (fresh
		// connection, no RTT sample yet) yields QualityFromLossRTT(0,0,0)=1.0,
		// and that optimism is deliberate and load-bearing: it is the ONLY
		// production path that ever initializes scoreQUIC.quality, and until
		// it fires the staleness gate substitutes neutral 0.5, capping
		// ScoreQUIC at avail×0.5 ≤ 0.5 — permanently below
		// qualityDegradeThreshold (0.7), which false-trips the selector's
		// quality path and pins engine's backup probe to its degraded cadence
		// on a perfectly healthy outbound. Do NOT "fix" the zero case to
		// neutral; real measurements overwrite it within one EWMA step.
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
