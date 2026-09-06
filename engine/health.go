package engine

import (
	"context"
	"fmt"
	"log/slog"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/sasha-s/go-deadlock"

	"github.com/kaitu-io/k2/core"
	"github.com/kaitu-io/k2/safego"
	"github.com/kaitu-io/k2/wire"
	"github.com/kaitu-io/k2/wire/k2cc"
)

// Health levels.
const (
	HealthHealthy  = "healthy"
	HealthDegraded = "degraded"
	HealthCritical = "critical"
)

// HealthSnapshot is a 1-second sample of wire and tunnel metrics.
type HealthSnapshot struct {
	Timestamp      time.Time `json:"ts"`
	Transport      string    `json:"transport"`
	PacketLossRate float64   `json:"packet_loss_rate"`
	Throughput     float64   `json:"throughput"`
	SendingRate    float64   `json:"sending_rate"`
	RTTMs          float64   `json:"rtt_ms"`
	MinRTTMs       float64   `json:"min_rtt_ms"`
	Cwnd           int64     `json:"cwnd"`
	Mode           int       `json:"mode"`
	Epsilon        float64   `json:"epsilon"`
	TxBytes        int64     `json:"tx_bytes"`
	RxBytes        int64     `json:"rx_bytes"`
	Health         string    `json:"health"`
	WireFallback   bool      `json:"wire_fallback"` // TCP-WS serving due to QUIC failure

	// Dual-channel quality scoreboard (Task 8 observability). Primary
	// outbound only — HealthSnapshot is per-engine, same scope as every
	// other field above (see hm.primaryTM's doc). daemon/gateway's
	// broadcastSnapshot marshals HealthSnapshot wholesale for SSE, so these
	// reach the UI with no separate plumbing.
	ScoreQUIC     float64 `json:"score_quic"`
	ScoreTCPWS    float64 `json:"score_tcpws"`
	ActiveChannel string  `json:"active_channel"`

	// Relay-layer observability (NOT for wire-layer decisions).
	RelayUDPTimeouts int32 `json:"relay_udp_timeouts"`

	// UDP drop counters are cumulative since the connection was established,
	// not a per-interval delta — unlike RelayUDPTimeouts above (which resets
	// to 0 on every successful receive), nothing ever resets these. A large
	// value may be hours old; to judge current loss rate, diff two heartbeat
	// samples rather than reading the raw value.
	//
	// UDPTxDrops is 0 by construction on stream-backed transports (TCP-WS,
	// QUIC stream-UDP fallback) — they have no lossy send path — while every
	// downlink drop on every transport is counted. See wire.WireStats for the
	// per-transport table; read Transport/WireFallback to know which row you
	// are looking at.
	UDPTxDrops int64 `json:"udp_tx_drops"`
	UDPRxDrops int64 `json:"udp_rx_drops"` // live session, channel full

	// UDPRxDropsNoSession counts downlink packets for a session the client no
	// longer knows. Mostly the benign tail after a session closes, so it is
	// kept out of UDPRxDrops to stop session churn from looking like loss.
	UDPRxDropsNoSession int64 `json:"udp_rx_drops_no_session"`
}

const (
	ringSize       = 300 // 5 minutes at 1Hz
	actionCooldown = 10 * time.Second
	ewmaAlpha      = 0.3 // EWMA weight for new sample (matches ARC adaptive alpha weight)

	// wakeDiagThreshold is the wall-clock gap that triggers a DIAG: wake log.
	// Short gaps (system load, brief doze) are logged for diagnostics.
	wakeDiagThreshold = 5 * time.Second

	// wakeReconnectThreshold is the wall-clock gap that triggers a full reconnect.
	// Aligned with QUIC MaxIdleTimeout (30s): after this gap, the server has
	// certainly closed the connection and NAT mappings may have expired.
	wakeReconnectThreshold = 30 * time.Second
)

// wakeDetector detects sleep/wake cycles via wall-clock gaps and triggers
// reconnect when the gap exceeds the reconnect threshold. Standalone unit
// embedded in healthMonitor — no goroutine, no lock of its own. Called from
// sample() at 1Hz under hm.mu. The onWakeReconnect callback is wired
// post-construction (same pattern as reRaceChan).
type wakeDetector struct {
	lastWall        time.Time
	onWakeReconnect func() // → engine.doNetworkReconnect (wired post-construction)
}

// checkWake compares wall-clock delta to detect sleep/wake. Returns the
// wall-clock gap and whether reconnect is needed. Called under hm.mu, but
// reconnect action MUST be executed outside the lock by the caller.
//
// Uses Round(0) to strip Go's monotonic reading, which freezes during macOS
// sleep. Wall clock advances normally, revealing the true gap.
func (wd *wakeDetector) checkWake(now time.Time, health, transport string, uptimeS int) (
	wallGap time.Duration, reconnect bool,
) {
	if wd.lastWall.IsZero() {
		wd.lastWall = now
		return 0, false
	}

	// Round(0) strips monotonic reading — forces wall-clock-only subtraction.
	wallGap = now.Round(0).Sub(wd.lastWall.Round(0))
	wd.lastWall = now

	if wallGap > wakeDiagThreshold {
		slog.Warn("DIAG: wake",
			"sleepS", int(wallGap.Seconds()),
			"health", health,
			"transport", transport,
			"uptimeS", uptimeS,
		)
	}

	return wallGap, wallGap >= wakeReconnectThreshold
}

// healthMonitor samples wire stats at 1Hz, maintains a ring buffer,
// detects health degradation trends, and takes corrective action.
type healthMonitor struct {
	mu        deadlock.Mutex
	primaryTM *outboundTransport   // for health FSM (WireStats, TCPRecentlyHealthy)
	tms       []*outboundTransport // for echoLoop (probe each independently)
	tunnel    *core.ClientTunnel
	done      chan struct{}
	wg        sync.WaitGroup

	health         string
	degradedStreak int
	criticalStreak int
	healthyStreak  int
	lastAction     time.Time

	// EWMA-smoothed loss for health decisions.
	// Raw values are still reported in HealthSnapshot for observability.
	smoothWireLoss  float64
	ewmaInitialized bool

	ring     [ringSize]HealthSnapshot
	ringIdx  int
	ringFull bool

	onSnapshot  func(HealthSnapshot) // push to daemon SSE
	onReconnect func()               // triggers OnNetworkChanged for full reconnect

	lastDial core.DialOutcomes // cumulative dial counts at the previous heartbeat

	// lastCC is the uplink sender's cumulative CC summary at the previous
	// heartbeat; the heartbeat reports ws.CC.Sub(lastCC), i.e. the 30 s
	// window, and a reconnect (new SenderID) restarts the window.
	lastCC k2cc.CCSummary

	diagCounter int          // samples since last DIAG heartbeat (fires at 30)
	connectedAt time.Time    // engine connected timestamp for uptime calc
	wake        wakeDetector // sleep/wake detection + reconnect trigger

	reRaceChan chan *outboundTransport // non-nil: engine's re-race channel (per-TM)

	// mobilePowerSaving disables echoProbeLoop's active backup-channel
	// probing (Task 7): mobile relies on protocol keepalive instead of
	// spending radio wake-ups on an opportunistic tcpws echo while QUIC is
	// active. Desktop leaves this false. Set post-construction from
	// Config.MobilePowerSaving (engine.go) rather than a constructor param,
	// to avoid touching the many existing newHealthMonitor call sites.
	mobilePowerSaving bool
}

func newHealthMonitor(
	primaryTM *outboundTransport,
	tms []*outboundTransport,
	tunnel *core.ClientTunnel,
	onSnapshot func(HealthSnapshot),
	onReconnect func(),
	connectedAt time.Time,
) *healthMonitor {
	return &healthMonitor{
		primaryTM:   primaryTM,
		tms:         tms,
		tunnel:      tunnel,
		health:      HealthHealthy,
		onSnapshot:  onSnapshot,
		onReconnect: onReconnect,
		connectedAt: connectedAt,
		done:        make(chan struct{}),
	}
}

func (hm *healthMonitor) start() {
	hm.wg.Add(2)
	safego.Go(func() {
		defer hm.wg.Done()
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-hm.done:
				return
			case <-ticker.C:
				hm.sampleSafe()
			}
		}
	})
	safego.Go(func() {
		hm.echoLoop()
	})
}

// sampleSafe wraps sample() with panic recovery so the health monitor loop survives.
func (hm *healthMonitor) sampleSafe() {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("health: sample panic recovered", "panic", r,
				"stack", string(debug.Stack()))
		}
	}()
	hm.sample()
}

func (hm *healthMonitor) stop() {
	close(hm.done)
	hm.wg.Wait()
}

func (hm *healthMonitor) sample() {
	var ws wire.WireStats
	var tcpHealthy bool
	if hm.primaryTM != nil {
		ws = hm.primaryTM.tm.WireStats()
		tcpHealthy = hm.primaryTM.tm.TCPRecentlyHealthy(30 * time.Second)
	}

	// fix-round-2 (N1): feed every OTHER outbound's QUIC quality too.
	// WireStats() has the side effect of calling RecordQUICStats when its
	// TransportManager's active transport is QUIC (wire/transport.go) — that
	// is the ONLY production path that ever primes a ChannelScore's quality
	// component. Before this loop, only hm.primaryTM was ever sampled here,
	// so every non-primary outbound's scoreQUIC.quality stayed permanently
	// uninitialized → ChannelScore's staleness gate substitutes neutral 0.5
	// forever → ScoreQUIC capped at avail×0.5 ≤ 0.5, always below
	// echoBackupProbeThreshold (0.7). Two consequences on any multi-outbound
	// config (multi-slot gateway / rule-based multi-`via`): probeBackupOnce
	// permanently stuck in the degraded 5s cadence, AND wire's
	// selectorStep quality path (scoreTCPWS > scoreQUIC×1.5, sustained,
	// backupFresh≥2) eventually trips a false switch-away on a perfectly
	// healthy QUIC outbound. Health FSM / HealthSnapshot reporting stays
	// primary-only (unchanged) — the return value here is discarded
	// on purpose, only the RecordQUICStats side effect matters.
	for _, ot := range hm.tms {
		if ot == nil || ot == hm.primaryTM {
			continue
		}
		ot.tm.WireStats()
	}

	tx, rx := hm.tunnel.TrafficBytes()

	now := time.Now()

	// Dual-channel quality scoreboard (Task 8): read outside hm.mu, same as
	// WireStats()/TCPRecentlyHealthy() above — TransportManager has its own
	// lock, primary-only (matches HealthSnapshot's existing primary-only scope).
	var scoreQUIC, scoreTCPWS float64
	var activeChannel string
	if hm.primaryTM != nil {
		scoreQUIC = hm.primaryTM.tm.ScoreQUIC(now)
		scoreTCPWS = hm.primaryTM.tm.ScoreTCPWS(now)
		activeChannel = hm.primaryTM.tm.ActiveChannel()
	}

	hm.mu.Lock()

	// Detect sleep/wake via wall-clock gap. Reconnect decision deferred to after Unlock.
	_, wakeReconnect := hm.wake.checkWake(now, hm.health, ws.Transport,
		int(time.Since(hm.connectedAt).Seconds()))

	// EWMA smoothing: dampens MI boundary artifacts (lossRate spikes)
	// that cause false health degradation.
	if !hm.ewmaInitialized {
		hm.smoothWireLoss = ws.PacketLossRate
		hm.ewmaInitialized = true
	} else {
		hm.smoothWireLoss = ewmaAlpha*ws.PacketLossRate + (1-ewmaAlpha)*hm.smoothWireLoss
	}

	// Assess health from smoothed metrics.
	level := hm.assess(ws)
	action := hm.transition(level, tcpHealthy)

	snap := HealthSnapshot{
		Timestamp:      time.Now(),
		Transport:      ws.Transport,
		PacketLossRate: ws.PacketLossRate,
		Throughput:     ws.Throughput,
		SendingRate:    ws.SendingRate,
		RTTMs:          float64(ws.RTT) / float64(time.Millisecond),
		MinRTTMs:       float64(ws.MinRTT) / float64(time.Millisecond),
		Cwnd:           ws.Cwnd,
		Mode:           ws.Mode,
		Epsilon:        ws.Epsilon,
		TxBytes:        tx,
		RxBytes:        rx,
		Health:         hm.health,
		WireFallback:   ws.WireFallback,

		ScoreQUIC:     scoreQUIC,
		ScoreTCPWS:    scoreTCPWS,
		ActiveChannel: activeChannel,

		RelayUDPTimeouts: ws.RelayUDPTimeouts,
		UDPTxDrops:       ws.UDPTxDrops,
		UDPRxDrops:       ws.UDPRxDrops,

		UDPRxDropsNoSession: ws.UDPRxDropsNoSession,
	}

	// Write to ring buffer.
	hm.ring[hm.ringIdx] = snap
	hm.ringIdx = (hm.ringIdx + 1) % ringSize
	if hm.ringIdx == 0 {
		hm.ringFull = true
	}

	// Layer 1 DIAG heartbeat — every 30 seconds.
	// ReadMemStats and ActiveConns are pre-read outside hm.mu to avoid
	// STW and external calls under state lock.
	hm.diagCounter++
	var ms runtime.MemStats
	var diagTCP, diagUDP int64
	var dialTotal core.DialOutcomes
	emitHeartbeat := hm.diagCounter >= 30
	if emitHeartbeat {
		hm.diagCounter = 0
		hm.mu.Unlock()
		runtime.ReadMemStats(&ms)
		diagTCP, diagUDP = hm.tunnel.ActiveConns()
		dialTotal = hm.tunnel.DialOutcomes()
		hm.mu.Lock()
	}
	if emitHeartbeat {
		// Report the window, not the running total: "0 succeeded out of 516
		// attempts in the last 30s" is a diagnosis, whereas a cumulative pair
		// needs subtracting across log lines before it says anything.
		dial := dialTotal.Sub(hm.lastDial)
		hm.lastDial = dialTotal
		// Uplink CC window (client → server). VPN users barely upload, so
		// ccAppLimited ≈ ccMis is the normal shape here — the download
		// direction is the server's "DIAG: cc-summary" line.
		cc := ws.CC.Sub(hm.lastCC)
		hm.lastCC = ws.CC
		ccLossP90 := k2cc.FormatBucket(cc.LossPercentile(0.9))
		ccRttP90 := k2cc.FormatBucket(cc.RTTPercentile(0.9))
		slog.Info("DIAG: heartbeat",
			"health", hm.health,
			"transport", ws.Transport,
			"loss", fmt.Sprintf("%.3f", hm.smoothWireLoss),
			"rttMs", fmt.Sprintf("%.0f", float64(ws.RTT)/float64(time.Millisecond)),
			"minRttMs", fmt.Sprintf("%.0f", float64(ws.MinRTT)/float64(time.Millisecond)),
			"txMB", fmt.Sprintf("%.1f", float64(tx)/(1024*1024)),
			"rxMB", fmt.Sprintf("%.1f", float64(rx)/(1024*1024)),
			"tcpConns", diagTCP,
			"udpConns", diagUDP,
			"dialOk", dial.ProxyOK,
			"dialFail", dial.ProxyFail,
			"directOk", dial.DirectOK,
			"directFail", dial.DirectFail,
			"udpTxDrops", ws.UDPTxDrops,
			"udpRxDrops", ws.UDPRxDrops,
			"udpRxDropsNoSess", ws.UDPRxDropsNoSession,
			"uptimeS", int(time.Since(hm.connectedAt).Seconds()),
			"fallback", ws.WireFallback,
			"activeChannel", activeChannel,
			"scoreQuic", fmt.Sprintf("%.2f", scoreQUIC),
			"scoreTcpws", fmt.Sprintf("%.2f", scoreTCPWS),
			"heapMB", fmt.Sprintf("%.1f", float64(ms.HeapInuse)/(1024*1024)),
			"sysMB", fmt.Sprintf("%.1f", float64(ms.Sys)/(1024*1024)),
			"goroutines", runtime.NumGoroutine(),
			"ccMis", cc.MIs,
			"ccAppLimited", cc.AppLimitedMIs,
			"ccFloor", cc.FloorMIs,
			"ccLossP90", ccLossP90,
			"ccRttP90", ccRttP90,
			"ccBwEst", fmt.Sprintf("%.1f", cc.BWEst/1e6),
			"ccBgLoss", fmt.Sprintf("%.1f", cc.BGLoss*100),
			"ccMode", cc.Mode.String(),
		)
	}

	onSnapshot := hm.onSnapshot
	hm.mu.Unlock()

	// Side effects outside lock — callbacks may re-acquire hm.mu via
	// resetCounters(). Calling with hm.mu held would self-deadlock
	// (Go mutexes are non-reentrant).
	if action != nil {
		action()
	}
	// Wake reconnect: only if health-critical didn't already fire (action == nil)
	// to prevent double reconnect in the same tick.
	if wakeReconnect && action == nil && hm.wake.onWakeReconnect != nil {
		hm.wake.onWakeReconnect()
	}
	if onSnapshot != nil {
		onSnapshot(snap)
	}
}

// assess evaluates current wire stats and returns the appropriate health level.
func (hm *healthMonitor) assess(ws wire.WireStats) string {
	// No transport data yet — retain previous state (default: healthy).
	if ws.Transport == "" {
		return hm.health
	}

	// WireFallback is observability-only here (see HealthSnapshot/heartbeat):
	// being on TCP-WS is a legitimate steady state under the dual-channel
	// selector (it switches back to QUIC only after echo-verified recovery),
	// so channel choice no longer decides the health grade — only quality
	// (loss/RTT below) does. A clean TCP-WS naturally lands in the healthy
	// branch, same as a clean QUIC path.
	//
	// Honest scope note: "only quality decides" is a QUIC-path statement.
	// TCPWSClient.WireStats reports no PacketLossRate and no RTT/MinRTT (there
	// is no congestion controller to read them from — only UDP-mux drop
	// counters), so on a TCP-WS-serving outbound both predicates below are
	// structurally false and this function can only ever return healthy. That
	// is not an oversight to "fix" with a proxy metric: it means degraded /
	// critical — and therefore maybeAct's reconnect — are QUIC-path-only
	// signals today. TCP-WS quality is graded elsewhere, by the echo-RTT
	// channel score (wire.QualityFromEchoRTT) the selector consumes.
	highLoss := hm.smoothWireLoss > 0.05
	veryHighLoss := hm.smoothWireLoss > 0.25
	highRTT := ws.MinRTT > 0 && ws.RTT > 3*ws.MinRTT

	if veryHighLoss {
		return HealthCritical
	}
	if highLoss || highRTT {
		return HealthDegraded
	}
	return HealthHealthy
}

// transition updates health state and streaks.
// Returns a deferred action (full reconnect) to execute outside hm.mu.
func (hm *healthMonitor) transition(level string, tcpHealthy bool) (action func()) {
	switch level {
	case HealthHealthy:
		hm.healthyStreak++
		hm.degradedStreak = 0
		hm.criticalStreak = 0
		if hm.healthyStreak >= 10 && hm.health != HealthHealthy {
			slog.Info("health: recovery",
				"from", hm.health,
				"smoothWireLoss", fmt.Sprintf("%.3f", hm.smoothWireLoss),
				"healthyStreak", hm.healthyStreak,
			)
			hm.health = HealthHealthy
		}
	case HealthDegraded:
		hm.degradedStreak++
		hm.healthyStreak = 0
		hm.criticalStreak = 0
		if hm.degradedStreak >= 5 && hm.health == HealthHealthy {
			slog.Warn("health: degraded",
				"smoothWireLoss", fmt.Sprintf("%.3f", hm.smoothWireLoss),
				"streak", hm.degradedStreak,
			)
			hm.health = HealthDegraded
		}
	case HealthCritical:
		hm.criticalStreak++
		hm.healthyStreak = 0
		hm.degradedStreak = 0
		if hm.criticalStreak >= 5 && hm.health != HealthCritical {
			slog.Warn("health: critical",
				"streak", hm.criticalStreak,
				"smoothWireLoss", fmt.Sprintf("%.3f", hm.smoothWireLoss),
			)
			hm.health = HealthCritical
		}
		if hm.health == HealthCritical {
			action = hm.maybeAct(tcpHealthy)
		}
	}
	return
}

// maybeAct decides on corrective action with cooldown.
// Returns the action to execute outside hm.mu, or nil if no action needed.
// Only action is full reconnect — wire layer owns transport decisions.
func (hm *healthMonitor) maybeAct(tcpHealthy bool) func() {
	if time.Since(hm.lastAction) < actionCooldown {
		return nil
	}

	// TCP health gate: if TCP streams recently succeeded, the connection is
	// functional despite high wire loss. Skip reconnect to protect active
	// streams (e.g. YouTube video).
	if tcpHealthy {
		slog.Debug("health: TCP healthy, skipping reconnect")
		return nil
	}

	hm.lastAction = time.Now()
	slog.Warn("health: full reconnect (wire quality critical)")
	return hm.onReconnect
}

// resetCounters clears all health streaks and corrective action state.
// Called on network change: stale health data from the old network is irrelevant.
func (hm *healthMonitor) resetCounters() {
	hm.mu.Lock()
	defer hm.mu.Unlock()
	hm.health = HealthHealthy
	hm.degradedStreak = 0
	hm.criticalStreak = 0
	hm.healthyStreak = 0
	hm.smoothWireLoss = 0
	hm.ewmaInitialized = false
}

// Echo wire-health probing — exponential-trust schedule for RE-RACE
// DETECTION ONLY (trust/strikes/rerace below). Task 7 gave the same probe
// loop a second, independent job — feeding the dual-channel quality
// scoreboard (wire.TransportManager.RecordEchoResult/EvaluateSelector) and
// opportunistically probing the backup channel — documented separately at
// probeOnce/probeBackupIfNeeded below. The two jobs share one probe per
// tick but are otherwise uncoupled: scoreboard feeding never influences
// trust/strikes, and vice versa.
//
// Principle: "trust" = time we're willing to skip verification. Each successful
// probe halves uncertainty about wire health → next probe interval doubles. Each
// failure resets trust → tighten back to base. Same family as TCP Karn / BGP
// holdtime — proven adaptive verification pattern.
//
// Ceiling MUST sit below QUIC MaxIdleTimeout/2 so echo detects failure before
// QUIC's passive idle timer fires (otherwise this loop adds no value beyond
// quic-go's built-in keepalive). Enforced at init() time below.
//
// Detection timeline (wire dies at t=2s after handshake):
//
//	t=1:  probe ok      → trust=1, interval=2s
//	t=3:  probe FAIL    → trust=0, strikes=1, interval=1s + DIAG: echo-probe-fail
//	t=4:  probe FAIL    → strikes=2
//	t=5:  probe FAIL    → strikes=3 → rerace signal sent
//
// vs old design (single 30s ticker, 3-strike absolute): ~92s nominal, ∞ in
// practice (short-session reconnect loop reset the counter every <30s).
var echoIntervals = []time.Duration{
	1 * time.Second,
	2 * time.Second,
	4 * time.Second,
	8 * time.Second,
	16 * time.Second,
}

const (
	echoProbeTimeout    = 2 * time.Second
	echoReraceThreshold = 3 // consecutive real failures (unsupported and ok don't count)

	// echoBackupProbeThreshold mirrors wire's private qualityDegradeThreshold
	// (wire/transport.go) — the same 0.7 the hysteresis FSM uses to pre-warn
	// before considering a switch away from QUIC. Kept as a separate
	// engine-side constant rather than exporting wire's (R4): this is the
	// only consumer, and widening wire's API for one float is worse than a
	// commented mirror. If wire's threshold ever changes, this must change
	// with it — there is no compile-time link between the two.
	echoBackupProbeThreshold = 0.7

	// backupProbeIntervalHealthy is the low-frequency cadence at which the
	// backup (currently-inactive) channel is opportunistically echo-probed
	// while the primary channel is healthy. Desktop only — see
	// healthMonitor.mobilePowerSaving.
	backupProbeIntervalHealthy = 60 * time.Second

	// backupProbeIntervalDegraded is the upgraded cadence used once the
	// active channel's score drops below echoBackupProbeThreshold — the
	// backup channel needs fresher samples so evaluateSelector's quality
	// path (backupFreshSamplesGate) can act quickly once it's actually
	// ready to serve traffic.
	backupProbeIntervalDegraded = 5 * time.Second
)

func init() {
	// Invariant: worst-case time from wire-death to rerace-signal must stay
	// strictly less than QUIC MaxIdleTimeout, so echo wins the detection race
	// over QUIC's passive idle timer. Otherwise this loop adds no value beyond
	// quic-go's built-in keepalive.
	//
	// Worst case: wire dies right after a successful probe at max trust →
	//   wait `ceiling` for next probe →
	//   first probe times out (strike #1) →
	//   remaining `threshold-1` strikes accumulate at base interval+timeout.
	//
	// This math assumes exactly one RunEchoProbe call per echoProbeLoop
	// iteration (probeOnce). The backup-channel probe (probeBackupOnce) runs
	// on its own independent timer (backupProbeLoop) precisely so its I/O
	// can never be folded into this iteration and silently invalidate this
	// invariant — see backupProbeLoop's doc (fix-round-1, Important 4).
	quicIdle := wire.DefaultQUICConfig().MaxIdleTimeout
	ceiling := echoIntervals[len(echoIntervals)-1]
	base := echoIntervals[0]
	worstCase := ceiling + echoProbeTimeout +
		time.Duration(echoReraceThreshold-1)*(base+echoProbeTimeout)
	if worstCase >= quicIdle {
		panic(fmt.Sprintf(
			"engine: echo worst-case detection %v ≥ QUIC MaxIdleTimeout %v — "+
				"shrink echoIntervals ceiling, base interval, or echoReraceThreshold",
			worstCase, quicIdle,
		))
	}
}

// echoStepResult is the pure-function output of one probe iteration.
// Extracted from echoProbeLoop for unit testability.
type echoStepResult struct {
	NewTrust     int32
	NewStrikes   int32
	ShouldRerace bool
	Classify     echoClass
}

type echoClass int

const (
	echoOK echoClass = iota
	echoUnsupported
	echoFail
)

// echoStep is the state-machine transition for one probe outcome.
// Pure function — no time, no I/O, no logging. Tests cover all 4 cases.
func echoStep(trust, strikes int32, err error) echoStepResult {
	maxTrust := int32(len(echoIntervals)) - 1
	switch {
	case err == nil:
		return echoStepResult{NewTrust: min(trust+1, maxTrust), NewStrikes: 0, Classify: echoOK}
	case wire.IsEchoUnsupported(err):
		// Pre-echo server: treat as success-equivalent for trust accounting.
		// Server can't validate but isn't broken; other failure modes (QUIC idle,
		// stream-open failures elsewhere) will surface real wire death.
		// CRITICAL: without this branch, 1s probe tick would emit 18× rerace
		// storm against pre-echo servers (vs the old 30s tick's tolerable noise).
		return echoStepResult{NewTrust: min(trust+1, maxTrust), NewStrikes: 0, Classify: echoUnsupported}
	default:
		newStrikes := strikes + 1
		return echoStepResult{
			NewTrust:     0,
			NewStrikes:   newStrikes,
			ShouldRerace: newStrikes >= echoReraceThreshold,
			Classify:     echoFail,
		}
	}
}

// echoStrikeTCPHealthyWindow is how far back a successful TCP stream still
// counts as proof that the primary is carrying traffic. Same window (and same
// reasoning) as sample()'s TCPRecentlyHealthy call feeding maybeAct: it must
// comfortably span several echo intervals (ceiling 16s) so a single slow
// window doesn't look like silence.
const echoStrikeTCPHealthyWindow = 30 * time.Second

// suppressStrike decides whether an echo FAILURE on the primary must be kept
// out of the re-race strike counter. Scoring is never affected — the sample
// still feeds the channel's ChannelScore; only the destructive escalation
// (3 strikes → reRaceChan → full RaceTransport + ReplacePrimary) is gated.
// Returns the reason string for the DIAG trail.
//
// Two topologies need it, both of them ones where a failing echo is expected
// rather than informative (final review I1 + I4):
//
//   - Fallback is active (tcpws serving) while the primary is still the QUIC
//     dialer, AND the tcpws channel is provably still carrying traffic. That
//     QUIC path is known-dead — being dead is precisely why the selector moved
//     to tcpws — and its recovery is already owned by wire's runProbeOnce
//     (30s, handshake + 3 echo rounds, quicEchoRecovered). Left ungated, its
//     echo timeouts strike 3× within seconds and fire a re-race that closes
//     the QUIC dialer without preserving a backup and calls
//     ReplacePrimary(tcpws), destroying the FSM's entire fallback state (flap
//     history, quicEchoRecovered, the rollback machinery) — the re-race
//     almost always beats the 10s-sustain quality path, so this is what the
//     half-dead-QUIC scenario actually did before the gate. This is the part
//     of root cause C ("two uncoordinated switching mechanisms") that the
//     scoreboard alone did not resolve.
//
//     The tcpws-proof condition is NOT optional (final review NEW-1). probeOnce
//     is the ONLY producer on reRaceChan, and Phase B server replacement
//     (replaceOutboundServer) is reachable only through it; assess() cannot
//     reach critical on tcpws stats, so healthMonitor's reconnect path is no
//     backstop here. An unconditional suppression therefore severs the only
//     automated escalation exactly when the NODE dies while a user sits in
//     fallback — a routine fleet event (crash, quota cutoff) — leaving both
//     channels dead and the user hung until a network change or a manual
//     reconnect. Requiring live proof keeps the escalation for the
//     both-channels-dead case while still protecting a working tcpws session.
//
//     Accepted trade: an IDLE fallback user generates no dials, so there is no
//     live proof and strikes resume. That re-race may land tcpws as primary —
//     which is now a stable, honest terminal state (the C1 topology gates stop
//     the FSM and the recovery probe from fabricating a QUIC channel there).
//     I.e. the idle case is the pre-fix status quo minus the phantom loop,
//     which is strictly better than a user who cannot escalate at all.
//
//   - The primary IS the tcpws channel and TCP streams over it succeeded
//     recently. TCPWSClient became an EchoProber in this branch, which put
//     tcpws primaries on the 1-16s trust schedule with a 2s timeout for the
//     first time. An 8-byte echo shares one TCP connection with bulk
//     transfer, so under load TCP head-of-line blocking can blow that 2s
//     three times in a row on a connection that is working fine — and the
//     re-race would then close the very session serving the transfer.
//     Recent TCP success is the same "it's actually working, don't touch it"
//     evidence maybeAct already uses to skip reconnects.
//
// BOTH cases require live proof, and that symmetry is the invariant to
// preserve: when no TCP stream has recently succeeded over the TCP-WS channel,
// strikes accumulate normally in either topology. The gate withholds the
// escalation only while there is live proof it would be destructive — never
// on topology alone.
//
// "Live proof" is deliberately DialTCP-only (wire's recordTCPWSDialSuccess):
// that path reads a server response, so it is end-to-end. UDP dials over
// tcpws open a smux stream without any round trip — they keep "succeeding"
// against a dead node — so counting them would manufacture exactly the proof
// this gate must not have.
func suppressStrike(tm *wire.TransportManager, primaryChannel, activeChannel string, class echoClass) (bool, string) {
	if class != echoFail {
		return false, ""
	}
	if activeChannel == "tcpws" && primaryChannel == "quic" &&
		tm.TCPWSChannelRecentlyHealthy(echoStrikeTCPHealthyWindow) {
		return true, "fallback-active-quic-primary-owned-by-wire-probe"
	}
	if primaryChannel == "tcpws" && tm.TCPRecentlyHealthy(echoStrikeTCPHealthyWindow) {
		return true, "tcpws-primary-tcp-recently-healthy"
	}
	return false, ""
}

// echoLoop spawns one primary-probe goroutine AND one backup-probe goroutine
// per outboundTransport, joins on hm.done. Per-transport state is local to
// each child goroutine — no shared map, no lock.
//
// The two loops run on independent timers (fix-round-1, Important 1 + 4):
// echoProbeLoop's re-race detection cadence (exponential trust, ≤16s
// ceiling) must never be distorted or extended by the backup-channel probe's
// I/O, and the backup probe's advertised cadence (60s/5s) must be the REAL
// cadence it runs at, not bounded by whatever the primary's trust timer
// happens to be doing.
func (hm *healthMonitor) echoLoop() {
	defer hm.wg.Done()
	var wg sync.WaitGroup
	for _, ot := range hm.tms {
		ot := ot
		wg.Add(2)
		safego.Go(func() {
			defer wg.Done()
			hm.echoProbeLoop(ot)
		})
		safego.Go(func() {
			defer wg.Done()
			hm.backupProbeLoop(ot)
		})
	}
	wg.Wait()
}

// echoLoopState is one echoProbeLoop goroutine's mutable state, factored out
// of the loop body (into probeOnce) so probeOnceForTest can drive a single
// iteration deterministically without a goroutine or timer. Real-loop state
// is goroutine-local (one echoLoopState per outboundTransport, never shared);
// test-hook state is a fresh zero value per call — see probeOnceForTest.
type echoLoopState struct {
	trustIdx, strikes int32
	// unsupportedLogged rate-limits DIAG: echo-probe-unsupported to once per
	// (transport, state-change), avoiding spam when probing a pre-echo
	// server at high cadence.
	unsupportedLogged bool
}

// echoProbeLoop runs one transport's exponential-trust echo schedule until
// hm.done. State is goroutine-local (echoLoopState), never shared.
func (hm *healthMonitor) echoProbeLoop(ot *outboundTransport) {
	st := &echoLoopState{}

	timer := time.NewTimer(echoIntervals[st.trustIdx])
	defer timer.Stop()

	for {
		select {
		case <-hm.done:
			return
		case <-timer.C:
		}

		hm.probeOnce(ot, st, time.Now())
		timer.Reset(echoIntervals[st.trustIdx])
	}
}

// probeOnceForTest drives exactly one echoProbeLoop (primary-channel-only,
// as of fix-round-1 — see probeBackupOnce for the backup-channel test hook)
// iteration synchronously, bypassing the timer/goroutine. Uses fresh
// loop-local state each call — the tests that use this hook drive a single
// probe cycle and don't depend on trust/strikes carrying over between
// calls. Test-only.
func (hm *healthMonitor) probeOnceForTest(ot *outboundTransport, now time.Time) {
	hm.probeOnce(ot, &echoLoopState{}, now)
}

// probeOnce runs one primary-channel echo probe: re-race trust/strikes
// bookkeeping (unchanged from before Task 7), plus feeding the result into
// the dual-channel quality scoreboard and evaluating the hysteresis FSM on
// the probe's own cadence (Task 7 R1/R2). Backup-channel probing is NOT done
// here as of fix-round-1 — see backupProbeLoop/probeBackupOnce, which run on
// their own independent timer so backup I/O can never distort this probe's
// re-race detection timing (the init() worst-case invariant below assumes
// exactly one RunEchoProbe call per iteration; adding more would invalidate
// it). Mutates st in place; the caller re-arms its timer from st.trustIdx
// afterward.
func (hm *healthMonitor) probeOnce(ot *outboundTransport, st *echoLoopState, now time.Time) {
	primary := ot.tm.Primary()
	if primary == nil {
		st.trustIdx, st.strikes, st.unsupportedLogged = 0, 0, false
		return
	}
	ep, ok := primary.(wire.EchoProber)
	if !ok {
		// No data-path verification available for this dialer — nothing to
		// probe, so pace at the max interval (cheap no-op). NOTE: TCP-WS is
		// NOT in this branch anymore; *TCPWSClient implements EchoProber as of
		// this branch, which is why suppressStrike has a tcpws-primary case.
		// Only test doubles and future dialers land here.
		st.trustIdx = int32(len(echoIntervals) - 1)
		st.strikes, st.unsupportedLogged = 0, false
		return
	}

	transport := ot.tm.PrimaryType()
	// primaryChannel is whatever channel `primary` actually belongs to — NOT
	// necessarily ot.tm.ActiveChannel(); see the scoreboard block at the end
	// of this function for the full topology note.
	primaryChannel := channelForType(transport)
	activeChannel := ot.tm.ActiveChannel()

	echoCtx, cancel := context.WithTimeout(context.Background(), echoProbeTimeout)
	start := time.Now()
	err := ep.RunEchoProbe(echoCtx)
	cancel()
	probeNow := time.Now()

	res := echoStep(st.trustIdx, st.strikes, err)

	// Strike gating (final-review I1 + I4). echoStep stays pure and always
	// classifies the outcome — scoring below is fed unconditionally either
	// way. What is gated here is only the STRIKE, i.e. whether this failure
	// counts toward the 3-strike re-race that tears the whole outbound down
	// and re-runs RaceTransport.
	if suppress, why := suppressStrike(ot.tm, primaryChannel, activeChannel, res.Classify); suppress {
		res.NewStrikes = st.strikes
		res.ShouldRerace = false
		slog.Debug("engine: echo strike suppressed", "transport", transport, "reason", why)
	}

	switch res.Classify {
	case echoOK:
		st.unsupportedLogged = false
		// echo-probe-ok is logged inside wire.EchoProbe at DEBUG with rttMs;
		// no need to duplicate here.
	case echoUnsupported:
		if !st.unsupportedLogged {
			slog.Warn("DIAG: echo-probe-unsupported", "transport", transport)
			st.unsupportedLogged = true
		}
	case echoFail:
		st.unsupportedLogged = false
		slog.Warn("DIAG: echo-probe-fail",
			"transport", transport,
			"err", err.Error(),
			"trustIdx", st.trustIdx,
			"strikes", res.NewStrikes,
		)
	}

	st.trustIdx = res.NewTrust
	st.strikes = res.NewStrikes

	if res.ShouldRerace && hm.reRaceChan != nil {
		// Cap-1 channel + select-default in engine.outboundReplaceLoop gives
		// natural burst throttling; consumer logs DIAG: transport-rerace.
		select {
		case hm.reRaceChan <- ot:
		default:
		}
		st.strikes = 0
	}

	// Task 7: feed the dual-channel scoreboard with this probe's outcome and
	// evaluate the hysteresis FSM on the probe's own cadence (R1/R2) — rather
	// than only piggy-backing on Dial*/Handshake traffic. primaryChannel
	// (computed above) is whatever channel `primary` actually belongs to — NOT
	// necessarily ot.tm.ActiveChannel(): when tcpws won the initial race but
	// QUIC also connected, tm.primary is installed as the QUIC dialer with
	// fallback active (see setupOutboundTransport), so probing `primary` here
	// probes QUIC even while tcpws is the active/serving channel. Recording
	// under primaryChannel (not activeChannel) keeps each channel's score fed
	// only by probes that actually exercised it. This recording is deliberately
	// NOT gated by suppressStrike above: a suppressed strike still means the
	// echo genuinely failed, and the score is how that fact reaches the
	// selector.
	var rtt time.Duration
	if primaryChannel == "tcpws" {
		// R1: rtt is measured at the call site (includes stream-open
		// overhead — acceptable, QualityFromEchoRTT is self-relative).
		// QUIC's quality signal comes from RecordQUICStats (CC stats), so
		// rtt stays 0 for the quic channel — RecordEchoResult's quality
		// branch is tcpws-only anyway.
		rtt = probeNow.Sub(start)
	}
	ot.tm.RecordEchoResult(primaryChannel, rtt, err, probeNow)
	ot.tm.EvaluateSelector(probeNow)
	ot.tm.SetChannelProbeInterval(primaryChannel, echoIntervals[st.trustIdx])
}

// channelForType classifies a primaryType label the same way
// wire.TransportManager does internally ("quic*" → "quic", else "tcpws").
// Duplicated here (rather than exported from wire) because it's a one-line
// string-prefix check — not worth widening wire's API for.
func channelForType(transportType string) string {
	if strings.HasPrefix(transportType, "quic") {
		return "quic"
	}
	return "tcpws"
}

// backupLoopState is backupProbeLoop's per-transport state. Separate from
// echoLoopState (fix-round-1): the backup probe now runs on its own
// independent timer/goroutine rather than being piggy-backed inside
// echoProbeLoop's tick — see backupProbeLoop's doc for why.
type backupLoopState struct {
	// nextProbeAt paces the backup-channel probe. Zero = eligible immediately.
	nextProbeAt time.Time
}

// backupProbeLoop opportunistically echo-probes the backup (currently
// inactive) channel on its own independent timer — decoupled from
// echoProbeLoop's exponential-trust timer (fix-round-1, review findings
// Important 1 + 4).
//
// Piggy-backing the backup probe inside echoProbeLoop's tick (the original
// Task 7 implementation) had two bugs: (1) the backup probe could only ever
// run as often as the primary's *current* trust-scaled interval — up to the
// 16s ceiling — so the advertised 5s degraded cadence was frequently
// unachievable, while SetChannelProbeInterval kept telling ChannelScore
// "5s" regardless, understating the real staleness window; (2) it injected
// extra blocking RunEchoProbe I/O into the very iteration init()'s
// worst-case detection-time invariant assumes contains exactly one probe,
// silently invalidating that guard under a degraded-QUIC run. A dedicated
// timer fixes both at once: this goroutine's actual firing cadence IS what
// gets reported to SetChannelProbeInterval, and echoProbeLoop's timing is
// never touched by backup I/O.
func (hm *healthMonitor) backupProbeLoop(ot *outboundTransport) {
	st := &backupLoopState{}

	// Fire (almost) immediately for the first check — probeBackupOnce's own
	// gating (mobilePowerSaving / active-channel / primary-is-tcpws) decides
	// whether that first check actually probes anything.
	timer := time.NewTimer(time.Millisecond)
	defer timer.Stop()

	for {
		select {
		case <-hm.done:
			return
		case <-timer.C:
		}

		next := hm.probeBackupOnce(ot, st, time.Now())
		timer.Reset(next)
	}
}

// probeBackupOnce evaluates whether the backup channel is due for a probe
// at `now` and, if so, probes it and records the outcome. Returns the
// duration until the next CHECK is due — backupProbeLoop re-arms its timer
// from this; a test can call this directly with synthetic `now` values and
// a state that persists across calls, without any real waiting (fix-round-1
// Important 3: this replaces probeOnceForTest as the way to exercise
// backup-cadence behavior — echoProbeLoop's probeOnceForTest no longer
// touches backup logic at all).
//
// The returned duration is a WAKE cadence, not necessarily the PROBE
// cadence (N2, actually reachable from backupProbeLoop as of fix-round-3 —
// round 2 wrote this same intent into the WAITING branch only, which
// production never actually reaches: backupProbeLoop's sole wake source is
// this function's own previous return value, so a probe-path return of the
// full healthy interval means the next wake always lands exactly at (or
// after) the freshly-set st.nextProbeAt, and the waiting branch's
// now.Before(st.nextProbeAt) is therefore never true in production — only a
// test driving a synthetic mid-window `now` could reach it. Fix-round-3
// caps the PROBE path's return the same way, making the waiting branch's
// pull-in logic reachable): while healthy-and-waiting for the next probe,
// this still returns at most backupProbeIntervalDegraded so the caller
// re-evaluates whether the mode has changed — a healthy desktop outbound
// still only actually PROBES every 60s (st.nextProbeAt / SetChannelProbeInterval
// are the real probe gate/cadence, untouched by this capping), but a
// degradation partway through that 60s window is now actually noticed
// within ≤2 mode-check ticks (~10s) instead of stranded behind the stale
// 60s-away deadline — one tick to WAKE and see the new mode, a second for
// the pulled-in nextProbeAt to actually come due. That 2× bound is what
// TestBackupProbe_ModeChangeDuringHealthyWaitNoticedQuickly asserts; the
// single-tick "~5s" this comment used to claim was never reachable.
//
// R5's scope is deliberately asymmetric:
//
//   - active=="quic": backup = tm.TCPWS(). Cadence is 60s while ScoreQUIC is
//     healthy, upgraded to 5s once it drops below echoBackupProbeThreshold
//     (mirrors wire's private qualityDegradeThreshold — R4) so
//     evaluateSelector's quality path has fresh backupFresh samples by the
//     time it needs them.
//   - active=="tcpws": no-op. QUIC recovery probing during fallback is
//     ALREADY owned by wire's runProbeOnce (30s, echo-verified,
//     quicEchoRecovered) — duplicating it here would be a second,
//     uncoordinated prober racing the same QUIC connection.
//
// Fix-round-1 Important 2: when tcpws won the transport race outright (no
// separate QUIC dialer — see setupOutboundTransport), tm.primary AND
// tm.TCPWS() are the literal same *TCPWSClient. echoProbeLoop already probes
// it as the primary; probing "the backup" here would double-probe the
// identical connection (two echo streams per tick, a doubled EWMA
// availability sample, a doubled backupFresh count, and a clobbered
// probeInterval). Guarded by channelForType(PrimaryType())=="tcpws".
//
// hm.mobilePowerSaving disables this entirely: mobile relies on protocol
// keepalive instead of spending radio wake-ups on opportunistic backup
// echoes. Per R3, that branch does NOT touch the backup channel's
// ChannelScore.probeInterval — left at whatever it last was
// (defaultProbeInterval if never set), so a channel with no active probing
// goes stale → neutral 0.5 on ChannelScore's own staleness gate. That decay
// is a designed fail-safe, not a bug.
func (hm *healthMonitor) probeBackupOnce(ot *outboundTransport, st *backupLoopState, now time.Time) time.Duration {
	if hm.mobilePowerSaving {
		return backupProbeIntervalHealthy
	}
	if ot.tm.ActiveChannel() != "quic" {
		return backupProbeIntervalHealthy
	}
	if channelForType(ot.tm.PrimaryType()) == "tcpws" {
		return backupProbeIntervalHealthy
	}

	interval := backupProbeIntervalHealthy
	if ot.tm.ScoreQUIC(now) < echoBackupProbeThreshold {
		interval = backupProbeIntervalDegraded
	}

	if !st.nextProbeAt.IsZero() && now.Before(st.nextProbeAt) {
		// fix-round-2 (N2): don't just sleep out the full remaining wait —
		// mode (healthy/degraded) can change while we're waiting (e.g. QUIC
		// degrades 10s into a 60s healthy window), and the pre-N2 code
		// wouldn't notice until the stale nextProbeAt finally arrived, up to
		// backupProbeIntervalHealthy (60s) late. Pull nextProbeAt IN — never
		// push it out — whenever the interval just computed from the
		// CURRENT score would land sooner than the already-scheduled probe.
		// A later flip back to healthy does not cancel an already-pulled-in
		// probe (worst case: one harmless extra probe, not a missed one).
		if candidate := now.Add(interval); candidate.Before(st.nextProbeAt) {
			st.nextProbeAt = candidate
		}
		remaining := st.nextProbeAt.Sub(now)
		if remaining > backupProbeIntervalDegraded {
			// Still keep WAKING at least this often to re-check mode, even
			// though the actual PROBE stays gated by st.nextProbeAt — a
			// healthy desktop outbound still only probes every 60s, it just
			// re-evaluates whether that's still the right cadence every
			// ≤backupProbeIntervalDegraded.
			return backupProbeIntervalDegraded
		}
		return remaining
	}
	// SetChannelProbeInterval and st.nextProbeAt (the actual PROBE gate)
	// still use the real, achievable `interval` (60s/5s) unchanged — the
	// declared-vs-real cadence equality established in fix-round-1 is
	// unaffected by fix-round-3 below (fix-round-1 Important 1).
	ot.tm.SetChannelProbeInterval("tcpws", interval)

	if backup := ot.tm.TCPWS(); backup != nil {
		if prober, ok := backup.(wire.EchoProber); ok {
			ctx, cancel := context.WithTimeout(context.Background(), echoProbeTimeout)
			start := time.Now()
			err := prober.RunEchoProbe(ctx)
			cancel()
			probeNow := time.Now()

			if err != nil && !wire.IsEchoUnsupported(err) {
				slog.Warn("DIAG: echo-probe-fail", "transport", "tcpws-backup", "err", err.Error())
			}

			ot.tm.RecordEchoResult("tcpws", probeNow.Sub(start), err, probeNow)
			ot.tm.EvaluateSelector(probeNow)
		}
	}

	st.nextProbeAt = now.Add(interval)

	// fix-round-3 (N2 still open after round 2): backupProbeLoop's ONLY
	// wake source is the duration returned here — timer.Reset(next) is
	// called right after this function returns (backupProbeLoop below), so
	// returning the full `interval` (60s in the healthy branch) means the
	// loop's next wake lands exactly AT (or just after) the freshly-set
	// st.nextProbeAt, every single time. now.Before(st.nextProbeAt) is then
	// never true on the following wake, so the waiting branch above —
	// including its pull-in logic — is never entered in production; only a
	// test calling probeBackupOnce with a synthetic mid-window `now` could
	// ever reach it. Capping the wake here the same way the waiting branch
	// already caps it fixes that: a healthy outbound still only PROBES
	// every 60s (st.nextProbeAt / SetChannelProbeInterval are untouched
	// above), but the loop now WAKES at most every backupProbeIntervalDegraded
	// to re-evaluate mode — making the waiting branch's pull-in reachable,
	// so a degradation partway through the 60s window is now actually
	// noticed within ≤2 wake ticks (~10s, one to see the mode change and one
	// for the pulled-in nextProbeAt to come due) instead of at the stale 60s
	// boundary.
	return min(interval, backupProbeIntervalDegraded)
}

// History returns the ring buffer contents oldest-first.
func (hm *healthMonitor) History() []HealthSnapshot {
	hm.mu.Lock()
	// concurrency:allow — pure field read returning a copy, no I/O or external calls
	defer hm.mu.Unlock()

	var n int
	if hm.ringFull {
		n = ringSize
	} else {
		n = hm.ringIdx
	}
	if n == 0 {
		return nil
	}

	out := make([]HealthSnapshot, n)
	if hm.ringFull {
		// Oldest is at ringIdx, wrap around.
		copy(out, hm.ring[hm.ringIdx:])
		copy(out[ringSize-hm.ringIdx:], hm.ring[:hm.ringIdx])
	} else {
		copy(out, hm.ring[:n])
	}
	return out
}
