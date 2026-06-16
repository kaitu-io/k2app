package sidecar

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"time"
)

const cutoffDefaultPollInterval = 5 * time.Second

// enforcer is the sidecar's node-side traffic-cutoff authority. A fast local loop
// reads host-NIC usage and pauses all data-plane containers when the epoch quota
// is reached; the next epoch (quota reset) unpauses. State is persisted so a
// restart re-applies an in-effect cut before the first Center quota.
type enforcer struct {
	meter        nicMeter
	docker       dockerController
	statePath    string
	containers   []string
	pollInterval time.Duration

	mu            sync.Mutex
	epochID       int64
	quotaTotal    int64
	quotaUsedBase int64 // Center authoritative used at SetQuota
	meterAtReport int64 // local meter snapshot at SetQuota
	haveQuota     bool
	cut           bool
}

// newEnforcer is the testable constructor (inject docker/path/containers). It
// loads persisted state so a restart mid-cut re-applies it.
func newEnforcer(meter nicMeter, docker dockerController, statePath string, containers []string, interval time.Duration) *enforcer {
	if interval <= 0 {
		interval = cutoffDefaultPollInterval
	}
	st := loadCutoffState(statePath)
	return &enforcer{
		meter: meter, docker: docker, statePath: statePath,
		containers: containers, pollInterval: interval,
		epochID: st.EpochID, cut: st.Cut,
	}
}

// NewEnforcer is the production constructor: builds the real docker client + reads
// env. Returns an error if the docker client cannot be created (caller degrades).
func NewEnforcer(meter nicMeter) (*enforcer, error) {
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
	return newEnforcer(meter, docker, "/etc/kaitu/cutoff.state", []string{"k2v5", "k2v4-slave"}, interval), nil
}

// SetQuota is called by the usage reporter each successful cycle: records Center's
// authoritative quota + used, and snapshots the current meter. On a new epoch the
// reporter has already rebaselined the meter (used≈0) → next reconcile unpauses.
func (e *enforcer) SetQuota(epochID, quotaTotal, quotaUsed int64) {
	// Snapshot the meter BEFORE taking e.mu. CumulativeBytes acquires
	// hostNICMeter.mu; reconcile takes hostNICMeter.mu then e.mu, so reading the
	// meter while holding e.mu here would be a lock-order inversion (ABBA deadlock).
	cur, curErr := e.meter.CumulativeBytes()
	e.mu.Lock()
	defer e.mu.Unlock()
	e.epochID = epochID
	e.quotaTotal = quotaTotal
	e.quotaUsedBase = quotaUsed
	if curErr == nil {
		e.meterAtReport = cur
	}
	e.haveQuota = true
}

// Run drives the fast reconcile loop until ctx is cancelled. The first reconcile
// fires immediately so a restart re-applies a persisted cut at once.
func (e *enforcer) Run(ctx context.Context) {
	slog.Info("DIAG: cutoff-enforcer-start", "component", "cutoff", "interval", e.pollInterval, "containers", e.containers)
	e.reconcile(ctx)
	t := time.NewTicker(e.pollInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			slog.Info("DIAG: cutoff-enforcer-stop", "component", "cutoff")
			return
		case <-t.C:
			e.reconcile(ctx)
		}
	}
}

// reconcile reads usage, computes the desired cut state, and drives every
// data-plane container toward it (self-healing — re-applies if Docker resurrected
// a container). Fail-safe: a meter read error changes nothing.
func (e *enforcer) reconcile(ctx context.Context) {
	cur, err := e.meter.CumulativeBytes()
	if err != nil {
		slog.Warn("DIAG: cutoff-meter-fail", "component", "cutoff", "err", err)
		return
	}

	e.mu.Lock()
	desired := e.cut // no quota yet (or anchor stale): hold current state
	var effective int64
	// cur < meterAtReport means the meter was rebaselined (epoch rolled over) but
	// the matching SetQuota hasn't arrived yet — the anchor is stale, so hold the
	// current state rather than computing a verdict on it. Normal intra-epoch reads
	// are monotonic (cur >= meterAtReport).
	if e.haveQuota && cur >= e.meterAtReport {
		effective = e.quotaUsedBase + (cur - e.meterAtReport)
		desired = e.quotaTotal > 0 && effective >= e.quotaTotal
	}
	epoch := e.epochID
	changed := desired != e.cut
	e.cut = desired
	e.mu.Unlock()

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
				slog.Warn("DIAG: cutoff-paused", "component", "cutoff", "container", name, "effectiveUsed", effective)
			}
		case !desired && paused:
			if uerr := e.docker.Unpause(ctx, name); uerr != nil {
				slog.Error("DIAG: cutoff-unpause-fail", "component", "cutoff", "container", name, "err", uerr)
			} else {
				slog.Info("DIAG: cutoff-unpaused", "component", "cutoff", "container", name)
			}
		}
	}

	if changed {
		if serr := saveCutoffState(e.statePath, cutoffState{EpochID: epoch, Cut: desired}); serr != nil {
			slog.Error("DIAG: cutoff-state-persist-fail", "component", "cutoff", "err", serr)
		}
	}
}
