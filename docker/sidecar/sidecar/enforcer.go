package sidecar

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"time"
)

const cutoffDefaultPollInterval = 5 * time.Second

const (
	// quotaCutoffReserveBytes is the headroom kept below the monthly limit: the
	// node cuts at used >= limit - reserve. MUST match Center's value in
	// api/logic_node_usage.go so node-side and Center-side cutoffs agree.
	quotaCutoffReserveBytes int64 = 500 << 20 // 500 MiB
	// failClosedThreshold is the number of consecutive meter-read errors (with a
	// known limit > 0) after which the enforcer fails closed and pauses traffic.
	failClosedThreshold = 3
)

// statsSource is the shared TrafficMonitor read each cycle by both the enforcer
// and the usage reporter (the single host-NIC metering authority).
type statsSource interface {
	GetTrafficStats() (TrafficStats, error)
}

// enforcer is the sidecar's node-side traffic-cutoff authority. A fast local loop
// reads the shared TrafficMonitor (the single host-NIC metering source) and
// pauses all data-plane containers when used >= limit - reserve. The node is the
// authority — no Center quota verdict is involved. State is persisted so a restart
// re-applies an in-effect cut before the first successful meter read.
type enforcer struct {
	src          statsSource
	docker       dockerController
	statePath    string
	containers   []string
	pollInterval time.Duration

	mu         sync.Mutex
	cut        bool
	meterFails int
	lastLimit  int64 // last successfully-read limit; fail-closed only when >0
}

// newEnforcerFromStats is the testable constructor (inject src/docker/path/
// containers). It loads persisted state so a restart mid-cut re-applies it.
func newEnforcerFromStats(src statsSource, docker dockerController, statePath string, containers []string, interval time.Duration) *enforcer {
	if interval <= 0 {
		interval = cutoffDefaultPollInterval
	}
	st := loadCutoffState(statePath)
	return &enforcer{
		src: src, docker: docker, statePath: statePath,
		containers: containers, pollInterval: interval,
		cut: st.Cut,
	}
}

// NewEnforcer is the production constructor: builds the real docker client + reads
// env. Returns an error if the docker client cannot be created (caller degrades).
func NewEnforcer(tm *TrafficMonitor) (*enforcer, error) {
	docker, err := newRealDocker()
	if err != nil {
		return nil, err
	}
	interval := cutoffDefaultPollInterval
	if v := os.Getenv("K2_CUTOFF_POLL_INTERVAL"); v != "" {
		if d, e := time.ParseDuration(v); e == nil && d > 0 {
			interval = d
		}
	}
	return newEnforcerFromStats(tm, docker, "/etc/kaitu/cutoff.state", []string{"k2s"}, interval), nil
}

// Run drives the fast reconcile loop until ctx is cancelled. The first reconcile
// fires immediately so a restart re-applies a persisted cut at once.
func (e *enforcer) Run(ctx context.Context) {
	slog.Info("DIAG: cutoff-enforcer-start", "component", "cutoff", "interval", e.pollInterval, "containers", e.containers)
	e.reconcileOnce()
	t := time.NewTicker(e.pollInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			slog.Info("DIAG: cutoff-enforcer-stop", "component", "cutoff")
			return
		case <-t.C:
			e.reconcileOnce()
		}
	}
}

// reconcileOnce reads the shared TrafficMonitor, computes the desired cut state,
// and drives every data-plane container toward it. The node is the metering
// authority: cut when used >= limit - reserve; an unlimited node (limit==0) never
// cuts; consecutive meter-read errors fail closed (only when a limit is known).
func (e *enforcer) reconcileOnce() {
	stats, err := e.src.GetTrafficStats()

	e.mu.Lock()
	desired := e.cut
	switch {
	case err != nil:
		e.meterFails++
		// Fail-closed only when there IS a limit to protect; an unlimited node
		// has nothing to cut, so meter errors there change nothing.
		if e.meterFails >= failClosedThreshold && e.lastLimit > 0 {
			desired = true
		}
	default:
		e.meterFails = 0
		if stats.MonthlyTrafficLimitBytes > 0 {
			e.lastLimit = stats.MonthlyTrafficLimitBytes
			desired = stats.UsedTrafficBytes >= stats.MonthlyTrafficLimitBytes-quotaCutoffReserveBytes
		} else {
			desired = false // unlimited → never cut
		}
	}
	changed := desired != e.cut
	e.cut = desired
	e.mu.Unlock()

	e.apply(desired, stats.UsedTrafficBytes)
	if changed {
		if serr := saveCutoffState(e.statePath, cutoffState{Cut: desired}); serr != nil {
			slog.Error("DIAG: cutoff-state-persist-fail", "component", "cutoff", "err", serr)
		}
	}
}

// apply drives every data-plane container toward the desired pause state
// (self-healing — re-applies if Docker resurrected a container).
func (e *enforcer) apply(desired bool, usedBytes int64) {
	ctx := context.Background()
	for _, name := range e.containers {
		paused, exists, derr := e.docker.State(ctx, name)
		if derr != nil {
			slog.Warn("DIAG: cutoff-docker-state-fail", "component", "cutoff", "container", name, "err", derr)
			continue
		}
		if !exists {
			continue
		}
		switch {
		case desired && !paused:
			if perr := e.docker.Pause(ctx, name); perr != nil {
				slog.Error("DIAG: cutoff-pause-fail", "component", "cutoff", "container", name, "err", perr)
			} else {
				slog.Warn("DIAG: cutoff-paused", "component", "cutoff", "container", name, "usedBytes", usedBytes)
			}
		case !desired && paused:
			if uerr := e.docker.Unpause(ctx, name); uerr != nil {
				slog.Error("DIAG: cutoff-unpause-fail", "component", "cutoff", "container", name, "err", uerr)
			} else {
				slog.Info("DIAG: cutoff-unpaused", "component", "cutoff", "container", name)
			}
		}
	}
}
