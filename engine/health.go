package engine

import (
	"context"
	"fmt"
	"log/slog"
	"runtime"
	"runtime/debug"
	"sync"
	"time"

	"github.com/sasha-s/go-deadlock"

	"github.com/kaitu-io/k2/core"
	"github.com/kaitu-io/k2/safego"
	"github.com/kaitu-io/k2/wire"
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

	// Relay-layer observability (NOT for wire-layer decisions).
	RelayUDPTimeouts int32 `json:"relay_udp_timeouts"`
	PortHopAttempts  int32 `json:"port_hop_attempts"`

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

	diagCounter int          // samples since last DIAG heartbeat (fires at 30)
	connectedAt time.Time    // engine connected timestamp for uptime calc
	wake        wakeDetector // sleep/wake detection + reconnect trigger

	reRaceChan chan *outboundTransport // non-nil: engine's re-race channel (per-TM)
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
	tx, rx := hm.tunnel.TrafficBytes()

	now := time.Now()

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

		RelayUDPTimeouts: ws.RelayUDPTimeouts,
		PortHopAttempts:  ws.PortHopAttempts,
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
	emitHeartbeat := hm.diagCounter >= 30
	if emitHeartbeat {
		hm.diagCounter = 0
		hm.mu.Unlock()
		runtime.ReadMemStats(&ms)
		diagTCP, diagUDP = hm.tunnel.ActiveConns()
		hm.mu.Lock()
	}
	if emitHeartbeat {
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
			"udpTxDrops", ws.UDPTxDrops,
			"udpRxDrops", ws.UDPRxDrops,
			"udpRxDropsNoSess", ws.UDPRxDropsNoSession,
			"uptimeS", int(time.Since(hm.connectedAt).Seconds()),
			"fallback", ws.WireFallback,
			"heapMB", fmt.Sprintf("%.1f", float64(ms.HeapInuse)/(1024*1024)),
			"sysMB", fmt.Sprintf("%.1f", float64(ms.Sys)/(1024*1024)),
			"goroutines", runtime.NumGoroutine(),
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

	// In fallback: TCP-WS working but suboptimal. Treat as degraded
	// so HealthMonitor reflects reality without triggering aggressive
	// corrective actions that would disrupt the working TCP-WS path.
	if ws.WireFallback {
		return HealthDegraded
	}

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
// Returns a deferred action (port hop or full reconnect) to execute outside hm.mu.
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
// Only action is full reconnect — wire layer owns port hop decisions.
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

// Echo wire-health probing — exponential-trust schedule.
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

// echoLoop spawns one probe goroutine per outboundTransport, joins on hm.done.
// Per-transport state is local to each child goroutine — no shared map, no lock.
func (hm *healthMonitor) echoLoop() {
	defer hm.wg.Done()
	var wg sync.WaitGroup
	for _, ot := range hm.tms {
		ot := ot
		wg.Add(1)
		safego.Go(func() {
			defer wg.Done()
			hm.echoProbeLoop(ot)
		})
	}
	wg.Wait()
}

// echoProbeLoop runs one transport's exponential-trust echo schedule until
// hm.done. State (trust, strikes, unsupportedLogged) is goroutine-local.
func (hm *healthMonitor) echoProbeLoop(ot *outboundTransport) {
	var trustIdx, strikes int32
	// Rate-limit DIAG: echo-probe-unsupported to once per (transport, state-change)
	// to avoid spam when probing a pre-echo server at high cadence.
	unsupportedLogged := false

	timer := time.NewTimer(echoIntervals[trustIdx])
	defer timer.Stop()

	for {
		select {
		case <-hm.done:
			return
		case <-timer.C:
		}

		primary := ot.tm.Primary()
		if primary == nil {
			trustIdx, strikes, unsupportedLogged = 0, 0, false
			timer.Reset(echoIntervals[0])
			continue
		}
		ep, ok := primary.(wire.EchoProber)
		if !ok {
			// TCP-WS or similar — built-in WS ping/pong covers liveness.
			// Sleep at max interval (cheap no-op pacing).
			trustIdx, strikes, unsupportedLogged = 0, 0, false
			timer.Reset(echoIntervals[len(echoIntervals)-1])
			continue
		}

		transport := ot.tm.PrimaryType()
		echoCtx, cancel := context.WithTimeout(context.Background(), echoProbeTimeout)
		err := ep.RunEchoProbe(echoCtx)
		cancel()

		res := echoStep(trustIdx, strikes, err)

		switch res.Classify {
		case echoOK:
			unsupportedLogged = false
			// echo-probe-ok is logged inside wire.EchoProbe at DEBUG with rttMs;
			// no need to duplicate here.
		case echoUnsupported:
			if !unsupportedLogged {
				slog.Warn("DIAG: echo-probe-unsupported", "transport", transport)
				unsupportedLogged = true
			}
		case echoFail:
			unsupportedLogged = false
			slog.Warn("DIAG: echo-probe-fail",
				"transport", transport,
				"err", err.Error(),
				"trustIdx", trustIdx,
				"strikes", res.NewStrikes,
			)
		}

		trustIdx = res.NewTrust
		strikes = res.NewStrikes

		if res.ShouldRerace && hm.reRaceChan != nil {
			// Cap-1 channel + select-default in engine.outboundReplaceLoop gives
			// natural burst throttling; consumer logs DIAG: transport-rerace.
			select {
			case hm.reRaceChan <- ot:
			default:
			}
			strikes = 0
		}

		timer.Reset(echoIntervals[trustIdx])
	}
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
