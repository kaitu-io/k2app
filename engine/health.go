package engine

import (
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/sasha-s/go-deadlock"

	"github.com/kaitu-io/k2/core"
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
	Timestamp   time.Time `json:"ts"`
	Transport   string    `json:"transport"`
	LossRate    float64   `json:"loss_rate"`
	Throughput  float64   `json:"throughput"`
	SendingRate float64   `json:"sending_rate"`
	RTTMs       float64   `json:"rtt_ms"`
	MinRTTMs    float64   `json:"min_rtt_ms"`
	Cwnd        int64     `json:"cwnd"`
	Mode        int       `json:"mode"`
	TxBytes     int64     `json:"tx_bytes"`
	RxBytes     int64     `json:"rx_bytes"`
	Health      string    `json:"health"`
	Fallback    bool      `json:"fallback"` // TCP-WS serving due to QUIC failure

	// Datagram health
	DatagramLossRate    float64 `json:"dg_loss_rate"`
	DatagramTimeouts    int32   `json:"dg_timeouts"`
	DatagramHopAttempts int32   `json:"dg_hops"`
}

const (
	ringSize           = 300 // 5 minutes at 1Hz
	actionCooldown     = 10 * time.Second
	ewmaAlpha          = 0.3   // EWMA weight for new sample (matches ARC adaptive alpha weight)
	peakDecayPerSample = 0.999 // per-second decay, half-life ≈ 693 seconds
	ewmaWarmupSamples  = 10   // samples before peakThroughput tracking starts (skip init burst)
)

// healthMonitor samples wire stats at 1Hz, maintains a ring buffer,
// detects health degradation trends, and takes corrective action.
type healthMonitor struct {
	mu     deadlock.Mutex
	tm     *wire.TransportManager
	tunnel *core.ClientTunnel
	done   chan struct{}
	wg     sync.WaitGroup

	health         string
	degradedStreak int
	criticalStreak int
	healthyStreak  int
	lastAction     time.Time
	peakThroughput float64
	hopAttempts    int

	// EWMA-smoothed metrics for health decisions.
	// Raw values are still reported in HealthSnapshot for observability.
	smoothLoss       float64
	smoothThroughput float64
	ewmaInitialized  bool
	ewmaSamples      int // warm-up counter: peakThroughput not updated until stabilized

	ring     [ringSize]HealthSnapshot
	ringIdx  int
	ringFull bool

	onSnapshot func(HealthSnapshot) // push to daemon SSE
	onAction   func()               // triggers OnNetworkChanged for full reconnect
}

func newHealthMonitor(
	tm *wire.TransportManager,
	tunnel *core.ClientTunnel,
	onSnapshot func(HealthSnapshot),
	onAction func(),
) *healthMonitor {
	return &healthMonitor{
		tm:         tm,
		tunnel:     tunnel,
		health:     HealthHealthy,
		onSnapshot: onSnapshot,
		onAction:   onAction,
		done:       make(chan struct{}),
	}
}

func (hm *healthMonitor) start() {
	hm.wg.Add(1)
	go func() {
		defer hm.wg.Done()
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-hm.done:
				return
			case <-ticker.C:
				hm.sample()
			}
		}
	}()
}

func (hm *healthMonitor) stop() {
	close(hm.done)
	hm.wg.Wait()
}

func (hm *healthMonitor) sample() {
	ws := hm.tm.WireStats()
	tx, rx := hm.tunnel.TrafficBytes()

	hm.mu.Lock()

	// EWMA smoothing: dampens MI boundary artifacts (lossRate spikes,
	// throughput spikes) that cause false health degradation.
	if !hm.ewmaInitialized {
		hm.smoothLoss = ws.LossRate
		hm.smoothThroughput = ws.Throughput
		hm.ewmaInitialized = true
		hm.ewmaSamples = 1
	} else {
		hm.smoothLoss = ewmaAlpha*ws.LossRate + (1-ewmaAlpha)*hm.smoothLoss
		hm.smoothThroughput = ewmaAlpha*ws.Throughput + (1-ewmaAlpha)*hm.smoothThroughput
		hm.ewmaSamples++
	}

	// Decay peak throughput (half-life ~693s) then track from smoothed values.
	// Skip peak tracking during warm-up to avoid inflating peak from init burst
	// (QUIC handshake produces astronomical throughput in the first MI).
	hm.peakThroughput *= peakDecayPerSample
	if hm.ewmaSamples >= ewmaWarmupSamples && hm.smoothThroughput > hm.peakThroughput {
		hm.peakThroughput = hm.smoothThroughput
	}

	// Assess health from smoothed metrics.
	level := hm.assess(ws)
	action := hm.transition(level)

	snap := HealthSnapshot{
		Timestamp:   time.Now(),
		Transport:   ws.Transport,
		LossRate:    ws.LossRate,
		Throughput:  ws.Throughput,
		SendingRate: ws.SendingRate,
		RTTMs:       float64(ws.RTT) / float64(time.Millisecond),
		MinRTTMs:    float64(ws.MinRTT) / float64(time.Millisecond),
		Cwnd:        ws.Cwnd,
		Mode:        ws.Mode,
		TxBytes:     tx,
		RxBytes:     rx,
		Health:      hm.health,
		Fallback:    ws.Fallback,

		DatagramLossRate:    ws.DatagramLossRate,
		DatagramTimeouts:    ws.DatagramTimeouts,
		DatagramHopAttempts: ws.DatagramHopAttempts,
	}

	// Write to ring buffer.
	hm.ring[hm.ringIdx] = snap
	hm.ringIdx = (hm.ringIdx + 1) % ringSize
	if hm.ringIdx == 0 {
		hm.ringFull = true
	}

	onSnapshot := hm.onSnapshot
	hm.mu.Unlock()

	// Side effects outside lock — onAction calls e.OnNetworkChanged() which
	// re-acquires hm.mu via resetCounters(). Calling with hm.mu held would
	// self-deadlock (Go mutexes are non-reentrant).
	if action != nil {
		action()
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
	if ws.Fallback {
		return HealthDegraded
	}

	highLoss := hm.smoothLoss > 0.05
	veryHighLoss := hm.smoothLoss > 0.25
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
func (hm *healthMonitor) transition(level string) (action func()) {
	switch level {
	case HealthHealthy:
		hm.healthyStreak++
		hm.degradedStreak = 0
		hm.criticalStreak = 0
		if hm.healthyStreak >= 10 && hm.health != HealthHealthy {
			slog.Info("health: recovery",
				"from", hm.health,
				"smoothLoss", fmt.Sprintf("%.3f", hm.smoothLoss),
				"smoothThroughput", fmt.Sprintf("%.0f", hm.smoothThroughput),
				"healthyStreak", hm.healthyStreak,
			)
			hm.health = HealthHealthy
			hm.hopAttempts = 0
		}
	case HealthDegraded:
		hm.degradedStreak++
		hm.healthyStreak = 0
		hm.criticalStreak = 0
		if hm.degradedStreak >= 5 && hm.health == HealthHealthy {
			slog.Warn("health: degraded",
				"smoothLoss", fmt.Sprintf("%.3f", hm.smoothLoss),
				"smoothThroughput", fmt.Sprintf("%.0f", hm.smoothThroughput),
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
				"smoothLoss", fmt.Sprintf("%.3f", hm.smoothLoss),
				"smoothThroughput", fmt.Sprintf("%.0f", hm.smoothThroughput),
			)
			hm.health = HealthCritical
		}
		if hm.health == HealthCritical {
			action = hm.maybeAct()
		}
	}
	return
}

// maybeAct decides on corrective action with cooldown.
// Returns the action to execute outside hm.mu, or nil if no action needed.
func (hm *healthMonitor) maybeAct() func() {
	if time.Since(hm.lastAction) < actionCooldown {
		return nil
	}

	// TCP health gate: if TCP streams recently succeeded, the connection is
	// functional despite high datagram/ARC loss. Skip destructive hop to
	// protect active streams (e.g. YouTube video).
	if hm.tm.TCPRecentlyHealthy(30 * time.Second) {
		slog.Debug("health: TCP healthy, skipping hop")
		return nil
	}

	hm.lastAction = time.Now()
	hm.hopAttempts++

	if hm.hopAttempts > 3 {
		hm.hopAttempts = 0
		slog.Warn("health: full reconnect")
		return hm.onAction
	}
	// Port hop: reset cached connections.
	slog.Warn("health: port hop", "attempt", hm.hopAttempts)
	return func() { hm.tm.ResetConnections() }
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
	hm.hopAttempts = 0
	hm.peakThroughput = 0
	hm.smoothLoss = 0
	hm.smoothThroughput = 0
	hm.ewmaInitialized = false
	hm.ewmaSamples = 0
}

// History returns the ring buffer contents oldest-first.
func (hm *healthMonitor) History() []HealthSnapshot {
	hm.mu.Lock()
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
