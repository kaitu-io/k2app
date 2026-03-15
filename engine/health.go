package engine

import (
	"fmt"
	"log/slog"
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
	Timestamp   time.Time `json:"ts"`
	Transport   string    `json:"transport"`
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
	RelayUDPTimeouts int32   `json:"relay_udp_timeouts"`
	PortHopAttempts  int32   `json:"port_hop_attempts"`
}

const (
	ringSize           = 300 // 5 minutes at 1Hz
	actionCooldown     = 10 * time.Second
	ewmaAlpha = 0.3 // EWMA weight for new sample (matches ARC adaptive alpha weight)
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

	// EWMA-smoothed loss for health decisions.
	// Raw values are still reported in HealthSnapshot for observability.
	smoothWireLoss  float64
	ewmaInitialized bool

	ring     [ringSize]HealthSnapshot
	ringIdx  int
	ringFull bool

	onSnapshot func(HealthSnapshot) // push to daemon SSE
	onReconnect   func()               // triggers OnNetworkChanged for full reconnect

	diagCounter   int       // samples since last DIAG heartbeat (fires at 30)
	connectedAt   time.Time // engine connected timestamp for uptime calc
	lastSampleWall time.Time // wall clock of previous sample — detects sleep/wake gaps
}

func newHealthMonitor(
	tm *wire.TransportManager,
	tunnel *core.ClientTunnel,
	onSnapshot func(HealthSnapshot),
	onReconnect func(),
	connectedAt time.Time,
) *healthMonitor {
	return &healthMonitor{
		tm:          tm,
		tunnel:      tunnel,
		health:      HealthHealthy,
		onSnapshot:  onSnapshot,
		onReconnect: onReconnect,
		connectedAt: connectedAt,
		done:        make(chan struct{}),
	}
}

func (hm *healthMonitor) start() {
	hm.wg.Add(1)
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
	ws := hm.tm.WireStats()
	tx, rx := hm.tunnel.TrafficBytes()

	now := time.Now()

	hm.mu.Lock()

	// DIAG: wake — detect wall clock jump indicating macOS sleep/wake.
	// Go runtime suspends during sleep, so ticker fires immediately on wake
	// with a large wall clock gap but only 1s of Go-time elapsed.
	if !hm.lastSampleWall.IsZero() {
		wallGap := now.Sub(hm.lastSampleWall)
		if wallGap > 5*time.Second {
			slog.Warn("DIAG: wake",
				"sleepS", int(wallGap.Seconds()),
				"health", hm.health,
				"transport", ws.Transport,
				"uptimeS", int(time.Since(hm.connectedAt).Seconds()),
			)
		}
	}
	hm.lastSampleWall = now

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
	action := hm.transition(level)

	snap := HealthSnapshot{
		Timestamp:   time.Now(),
		Transport:   ws.Transport,
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
	}

	// Write to ring buffer.
	hm.ring[hm.ringIdx] = snap
	hm.ringIdx = (hm.ringIdx + 1) % ringSize
	if hm.ringIdx == 0 {
		hm.ringFull = true
	}

	// Layer 1 DIAG heartbeat — every 30 seconds.
	hm.diagCounter++
	if hm.diagCounter >= 30 {
		hm.diagCounter = 0
		tcp, udp := hm.tunnel.ActiveConns()
		slog.Info("DIAG: heartbeat",
			"health", hm.health,
			"transport", ws.Transport,
			"loss", fmt.Sprintf("%.3f", hm.smoothWireLoss),
			"rttMs", fmt.Sprintf("%.0f", float64(ws.RTT)/float64(time.Millisecond)),
			"minRttMs", fmt.Sprintf("%.0f", float64(ws.MinRTT)/float64(time.Millisecond)),
			"txMB", fmt.Sprintf("%.1f", float64(tx)/(1024*1024)),
			"rxMB", fmt.Sprintf("%.1f", float64(rx)/(1024*1024)),
			"tcpConns", tcp,
			"udpConns", udp,
			"uptimeS", int(time.Since(hm.connectedAt).Seconds()),
			"fallback", ws.WireFallback,
		)
	}

	onSnapshot := hm.onSnapshot
	hm.mu.Unlock()

	// Side effects outside lock — onReconnect calls e.OnNetworkChanged() which
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
func (hm *healthMonitor) transition(level string) (action func()) {
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
			action = hm.maybeAct()
		}
	}
	return
}

// maybeAct decides on corrective action with cooldown.
// Returns the action to execute outside hm.mu, or nil if no action needed.
// Only action is full reconnect — wire layer owns port hop decisions.
func (hm *healthMonitor) maybeAct() func() {
	if time.Since(hm.lastAction) < actionCooldown {
		return nil
	}

	// TCP health gate: if TCP streams recently succeeded, the connection is
	// functional despite high wire loss. Skip reconnect to protect active
	// streams (e.g. YouTube video).
	if hm.tm.TCPRecentlyHealthy(30 * time.Second) {
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
