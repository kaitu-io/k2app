package engine

import (
	"sync"
	"time"

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
}

const (
	ringSize       = 300 // 5 minutes at 1Hz
	actionCooldown = 10 * time.Second
)

// healthMonitor samples wire stats at 1Hz, maintains a ring buffer,
// detects health degradation trends, and takes corrective action.
type healthMonitor struct {
	mu     sync.Mutex
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
	defer hm.mu.Unlock()

	// Track peak throughput for degradation detection.
	if ws.Throughput > hm.peakThroughput {
		hm.peakThroughput = ws.Throughput
	}

	// Assess health from current metrics.
	level := hm.assess(ws)
	hm.transition(level)

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
	}

	// Write to ring buffer.
	hm.ring[hm.ringIdx] = snap
	hm.ringIdx = (hm.ringIdx + 1) % ringSize
	if hm.ringIdx == 0 {
		hm.ringFull = true
	}

	// Push snapshot (non-blocking via caller's buffered channel).
	if hm.onSnapshot != nil {
		hm.onSnapshot(snap)
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

	highLoss := ws.LossRate > 0.05
	veryHighLoss := ws.LossRate > 0.25
	highRTT := ws.MinRTT > 0 && ws.RTT > 3*ws.MinRTT
	throughputDrop := hm.peakThroughput > 0 && ws.Throughput < hm.peakThroughput*0.3

	if veryHighLoss || (hm.health == HealthDegraded && throughputDrop) {
		return HealthCritical
	}
	if highLoss || highRTT {
		return HealthDegraded
	}
	return HealthHealthy
}

// transition updates health state and streaks, taking action when critical.
func (hm *healthMonitor) transition(level string) {
	switch level {
	case HealthHealthy:
		hm.healthyStreak++
		hm.degradedStreak = 0
		hm.criticalStreak = 0
		if hm.healthyStreak >= 10 && hm.health != HealthHealthy {
			hm.health = HealthHealthy
			hm.hopAttempts = 0
		}
	case HealthDegraded:
		hm.degradedStreak++
		hm.healthyStreak = 0
		hm.criticalStreak = 0
		if hm.degradedStreak >= 5 && hm.health == HealthHealthy {
			hm.health = HealthDegraded
		}
	case HealthCritical:
		hm.criticalStreak++
		hm.healthyStreak = 0
		hm.degradedStreak = 0
		if hm.criticalStreak >= 3 && hm.health != HealthCritical {
			hm.health = HealthCritical
		}
		if hm.health == HealthCritical {
			hm.maybeAct()
		}
	}
}

// maybeAct takes corrective action with cooldown.
func (hm *healthMonitor) maybeAct() {
	if time.Since(hm.lastAction) < actionCooldown {
		return
	}
	hm.lastAction = time.Now()
	hm.hopAttempts++

	if hm.hopAttempts > 3 {
		// Escalate: full reconnect.
		hm.hopAttempts = 0
		if hm.onAction != nil {
			hm.onAction()
		}
	} else {
		// Port hop: reset cached connections.
		hm.tm.ResetConnections()
	}
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
