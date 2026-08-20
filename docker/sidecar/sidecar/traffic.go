package sidecar

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Billing modes — how the per-direction NIC deltas combine into billable usage.
// The mode must match how the hosting provider counts traffic against the plan.
const (
	// BillingModeMax bills the greater of the two directions (legacy default).
	// Correct for outbound-billed providers (e.g. BandwagonHost), where a relay's
	// rx≈tx makes max a safe over-approximation of the billed direction.
	BillingModeMax = "max"
	// BillingModeSum bills inbound+outbound. AWS Lightsail counts BOTH directions
	// against the monthly allowance (verified against Cost Explorer overage line
	// items 2026-08) — max undercounted it by ~half fleet-wide.
	BillingModeSum = "sum"
)

// trafficState persists the per-direction cycle baseline so a restart resumes
// in-cycle usage instead of re-anchoring to the current NIC counters (which
// would zero usage). Baselines are kept per direction so the billing mode
// (sum/max) can combine them at read time — see GetTrafficStats.
type trafficState struct {
	BillingCycleEndAt int64  `json:"billing_cycle_end_at"`
	CycleStartRx      uint64 `json:"cycle_start_rx"`
	CycleStartTx      uint64 `json:"cycle_start_tx"`
	// PriorUsedBytes is usage already consumed THIS cycle before the local NIC
	// anchor (mid-cycle onboarding seed or authoritative correction). Billable
	// usage = PriorUsedBytes + the mode-combined deltas, so a fresh node can
	// declare more than its NIC has ever seen. Absent in legacy files → 0 →
	// identical to the old delta-only math. Zeroed on cycle rollover (never
	// carried into the next month).
	PriorUsedBytes uint64 `json:"prior_used_bytes"`
	// LastUsedBytes is the most recent successfully computed billable usage,
	// persisted (rate-limited) so a VM reboot — which zeroes the kernel NIC
	// counters — can fold the pre-reboot usage back in instead of silently
	// dropping it. Absent in legacy files → 0 (reset detection then re-anchors
	// with nothing to fold, matching the old lossy behavior once).
	LastUsedBytes uint64 `json:"last_used_bytes"`
}

// hasBaseline reports whether the persisted state carries a usable baseline.
// Legacy state files (pre per-direction) leave the counters zero and are treated
// as absent → the node re-anchors once on upgrade. A pure seed (prior>0 with both
// counters 0, e.g. a fresh node whose NIC reads 0) still counts as a baseline.
func (s trafficState) hasBaseline() bool {
	return s.CycleStartRx > 0 || s.CycleStartTx > 0 || s.PriorUsedBytes > 0
}

// loadTrafficState reads the persisted baseline. A missing or corrupt file is
// treated as the zero value — never an error that would block startup.
func loadTrafficState(path string) trafficState {
	var st trafficState
	data, err := os.ReadFile(path)
	if err != nil {
		return trafficState{}
	}
	_ = json.Unmarshal(data, &st)
	return st
}

// saveTrafficState persists the baseline atomically (write temp + rename) so a
// crash mid-write never leaves a half-written file.
func saveTrafficState(path string, st trafficState) error {
	data, err := json.Marshal(st)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// TrafficMonitor traffic monitor
type TrafficMonitor struct {
	mu                sync.RWMutex
	billingStartDate  string    // billing start date (yyyy-MM-dd)
	billingCycleEndAt int64     // current billing cycle end timestamp
	trafficLimitGB    int64     // monthly traffic limit (GB), 0 = unlimited
	billingMode       string    // BillingModeSum / BillingModeMax — how deltas combine
	cycleStartRx      uint64    // RX counter at start of current billing cycle
	cycleStartTx      uint64    // TX counter at start of current billing cycle
	priorUsedBytes    uint64    // usage already consumed this cycle before the NIC anchor (onboarding seed / authoritative correction)
	lastUsedBytes     uint64    // most recent computed billable usage (reboot-reset fold source)
	lastPersistAt     time.Time // last rate-limited state persist from GetTrafficStats
	primaryInterface  string    // primary network interface name
	lastDetectedAt    time.Time // last time interface was detected
	procPath          string    // proc mount for NIC reads ("/host/proc" in prod)
	statePath         string    // persisted cycle baseline path (default /etc/kaitu/traffic.state)
}

const bytesPerGiB = int64(1024 * 1024 * 1024)

// trafficStatePersistInterval rate-limits the LastUsedBytes persist done on each
// successful GetTrafficStats read (the enforcer polls every ~5s; writing the tiny
// state file at most once a minute bounds the reboot-fold loss window to ~1min of
// traffic while keeping disk writes negligible).
const trafficStatePersistInterval = time.Minute

// normalizeBillingMode maps the configured mode string to a valid mode. Empty =
// legacy default (max — non-AWS fleet nodes set no mode). A NON-empty unknown
// value means someone tried to configure a mode and mistyped: fall back to sum,
// the fail-closed direction (over-counts → cuts early → costs availability, never
// money), and log loudly.
func normalizeBillingMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", BillingModeMax:
		return BillingModeMax
	case BillingModeSum:
		return BillingModeSum
	default:
		slog.Error("Unknown traffic billing mode — falling back to fail-closed 'sum'",
			"component", "traffic", "configured", mode)
		return BillingModeSum
	}
}

// NewTrafficMonitor creates a traffic monitor.
//   - billingStartDate: billing start date (yyyy-MM-dd), e.g. "2025-01-15"
//   - trafficLimitGB:   monthly traffic limit (GB), 0 = unlimited
//   - initialUsedGB:    declared already-used traffic (GB) to SEED the baseline
//     when this node is onboarded mid-cycle. Applied ONLY on the very first boot
//     (no persisted state yet); thereafter the persisted record wins so a restart
//     never re-applies it. 0 = no seed. Adjust later with SetUsage.
//   - billingMode:      BillingModeSum / BillingModeMax (empty = max, the legacy
//     default) — must match how the provider bills, see the mode constants.
func NewTrafficMonitor(billingStartDate string, trafficLimitGB int64, initialUsedGB int64, billingMode string) (*TrafficMonitor, error) {
	if billingStartDate == "" {
		return nil, fmt.Errorf("billingStartDate is required")
	}

	// Validate date format
	if _, err := time.Parse("2006-01-02", billingStartDate); err != nil {
		return nil, fmt.Errorf("invalid billingStartDate format, expected yyyy-MM-dd: %w", err)
	}

	tm := &TrafficMonitor{
		billingStartDate: billingStartDate,
		trafficLimitGB:   trafficLimitGB,
		billingMode:      normalizeBillingMode(billingMode),
		procPath:         hostProcPath(),
		statePath:        "/etc/kaitu/traffic.state",
	}

	// Auto-detect primary network interface
	if err := tm.detectPrimaryInterface(); err != nil {
		return nil, fmt.Errorf("failed to detect primary interface: %w", err)
	}

	// Read current per-direction counters as the starting reference
	rx, tx, err := tm.readInterfaceRxTx()
	if err != nil {
		return nil, fmt.Errorf("failed to read initial traffic: %w", err)
	}

	// Calculate current billing cycle end time
	tm.billingCycleEndAt = tm.calculateNextCycleEnd(time.Now()).Unix()

	// Decide the cycle baseline:
	//  1. valid persisted state for THIS cycle → restore (survives restart, no reset)
	//  2. no persisted state at all + seed configured → seed to initialUsedGB (onboarding)
	//  3. otherwise → anchor to current counters (usage starts at 0)
	st := loadTrafficState(tm.statePath)
	switch {
	case st.BillingCycleEndAt == tm.billingCycleEndAt && st.hasBaseline():
		tm.cycleStartRx = st.CycleStartRx
		tm.cycleStartTx = st.CycleStartTx
		tm.priorUsedBytes = st.PriorUsedBytes
		tm.lastUsedBytes = st.LastUsedBytes
		slog.Info("Traffic cycle baseline restored from state", "component", "traffic",
			"cycleStartRx", tm.cycleStartRx, "cycleStartTx", tm.cycleStartTx, "priorUsedBytes", tm.priorUsedBytes)
		// A VM reboot while the sidecar was down zeroes the kernel NIC counters:
		// the restored baseline would then exceed the live counters and every
		// delta clamps to 0, silently dropping the cycle's usage. Fold the last
		// known usage in and re-anchor instead. (Init is single-threaded — no
		// lock needed yet.)
		tm.foldCounterResetLocked(rx, tx)
	case st.BillingCycleEndAt == 0 && initialUsedGB > 0:
		tm.applyUsageBaseline(rx, tx, initialUsedGB)
		_ = saveTrafficState(tm.statePath, tm.snapshotState())
		slog.Info("Traffic baseline SEEDED from initialUsedGB (first boot)", "component", "traffic",
			"initialUsedGB", initialUsedGB, "cycleStartRx", tm.cycleStartRx, "cycleStartTx", tm.cycleStartTx)
	default:
		tm.cycleStartRx = rx
		tm.cycleStartTx = tx
		_ = saveTrafficState(tm.statePath, tm.snapshotState())
	}

	slog.Info("Traffic monitor initialized",
		"component", "traffic",
		"interface", tm.primaryInterface,
		"billingDate", billingStartDate,
		"limitGB", trafficLimitGB,
		"billingMode", tm.billingMode,
		"rx", rx, "tx", tx)

	return tm, nil
}

// snapshotState captures the current baseline for persistence. Caller holds the
// appropriate lock (or is in single-threaded init).
func (tm *TrafficMonitor) snapshotState() trafficState {
	return trafficState{
		BillingCycleEndAt: tm.billingCycleEndAt,
		CycleStartRx:      tm.cycleStartRx,
		CycleStartTx:      tm.cycleStartTx,
		PriorUsedBytes:    tm.priorUsedBytes,
		LastUsedBytes:     tm.lastUsedBytes,
	}
}

// applyUsageBaseline records usedGB as the cycle's prior-used floor and anchors
// the per-direction baseline at the CURRENT NIC counters (so the live delta
// starts at 0). Billable usage then reads priorUsedBytes + the mode-combined
// deltas, which — unlike the old "baseline = NIC − used" math — works even when
// the declared usage exceeds what this node's NIC has ever seen (a fresh
// mid-cycle node).
func (tm *TrafficMonitor) applyUsageBaseline(rx, tx uint64, usedGB int64) {
	tm.priorUsedBytes = uint64(usedGB) * uint64(bytesPerGiB)
	tm.cycleStartRx = rx
	tm.cycleStartTx = tx
	tm.lastUsedBytes = tm.priorUsedBytes
}

// foldCounterResetLocked detects a kernel NIC counter reset (VM reboot: counters
// restart from 0, dropping below the cycle baseline) and recovers: the last known
// billable usage becomes the prior-used floor and the baseline re-anchors at the
// current counters. Caller holds the write lock (or is in single-threaded init).
// Returns true when a reset was detected and folded.
func (tm *TrafficMonitor) foldCounterResetLocked(rx, tx uint64) bool {
	if rx >= tm.cycleStartRx && tx >= tm.cycleStartTx {
		return false
	}
	slog.Warn("NIC counter reset detected (VM reboot?) — folding last known usage into baseline",
		"component", "traffic",
		"rx", rx, "tx", tx, "cycleStartRx", tm.cycleStartRx, "cycleStartTx", tm.cycleStartTx,
		"foldedUsedBytes", tm.lastUsedBytes)
	tm.priorUsedBytes = tm.lastUsedBytes
	tm.cycleStartRx = rx
	tm.cycleStartTx = tx
	_ = saveTrafficState(tm.statePath, tm.snapshotState())
	tm.lastPersistAt = time.Now()
	return true
}

// computeUsedLocked combines the per-direction deltas per the billing mode and
// adds the prior-used floor. Caller holds at least the read lock and has already
// handled counter resets (deltas here clamp at 0 defensively).
func (tm *TrafficMonitor) computeUsedLocked(rx, tx uint64) int64 {
	rxDelta := int64(0)
	if rx > tm.cycleStartRx {
		rxDelta = int64(rx - tm.cycleStartRx)
	}
	txDelta := int64(0)
	if tx > tm.cycleStartTx {
		txDelta = int64(tx - tm.cycleStartTx)
	}
	var used int64
	if tm.billingMode == BillingModeSum {
		used = rxDelta + txDelta
	} else {
		used = rxDelta
		if txDelta > used {
			used = txDelta
		}
	}
	return used + int64(tm.priorUsedBytes)
}

// SetUsage rewrites the persisted baseline so the meter reports usedGB right now,
// for mid-cycle onboarding or manual correction. This is the editable record:
// run `k2-sidecar set-usage <GB>` then restart the sidecar to load it.
func (tm *TrafficMonitor) SetUsage(usedGB int64) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	rx, tx, err := tm.readInterfaceRxTx()
	if err != nil {
		return fmt.Errorf("read NIC for SetUsage: %w", err)
	}
	tm.applyUsageBaseline(rx, tx, usedGB)
	if err := saveTrafficState(tm.statePath, tm.snapshotState()); err != nil {
		return fmt.Errorf("persist SetUsage baseline: %w", err)
	}
	slog.Info("Traffic usage set", "component", "traffic", "usedGB", usedGB,
		"cycleStartRx", tm.cycleStartRx, "cycleStartTx", tm.cycleStartTx)
	return nil
}

// detectPrimaryInterface auto-detects primary network interface
// Selects the interface with the most traffic (excluding lo/veth/docker interfaces)
func (tm *TrafficMonitor) detectPrimaryInterface() error {
	data, err := os.ReadFile(tm.procPath + "/net/dev")
	if err != nil {
		return fmt.Errorf("failed to read /proc/net/dev: %w", err)
	}

	var maxInterface string
	var maxBytes uint64

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if !strings.Contains(line, ":") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 10 {
			continue
		}

		iface := strings.TrimSuffix(fields[0], ":")
		// Skip loopback, virtual and docker interfaces
		if iface == "lo" || strings.HasPrefix(iface, "veth") || strings.HasPrefix(iface, "docker") {
			continue
		}

		// RX + TX bytes — only used to PICK the busiest interface, not to bill.
		rx, _ := strconv.ParseUint(fields[1], 10, 64)
		tx, _ := strconv.ParseUint(fields[9], 10, 64)
		totalBytes := rx + tx

		if totalBytes > maxBytes {
			maxBytes = totalBytes
			maxInterface = iface
		}
	}

	if maxInterface == "" {
		return fmt.Errorf("no valid network interface found")
	}

	tm.primaryInterface = maxInterface
	tm.lastDetectedAt = time.Now()
	slog.Info("Detected primary interface", "component", "traffic", "interface", maxInterface, "totalBytes", maxBytes)
	return nil
}

// readInterfaceRxTx reads the cumulative RX and TX byte counters for the primary
// interface separately (kernel counters, monotonic since boot).
func (tm *TrafficMonitor) readInterfaceRxTx() (rx uint64, tx uint64, err error) {
	// Re-detect primary interface every hour (prevents issues from interface changes)
	if time.Since(tm.lastDetectedAt) > time.Hour {
		if derr := tm.detectPrimaryInterface(); derr != nil {
			slog.Warn("Failed to re-detect interface", "component", "traffic", "err", derr)
		}
	}

	data, rerr := os.ReadFile(tm.procPath + "/net/dev")
	if rerr != nil {
		return 0, 0, fmt.Errorf("failed to read /proc/net/dev: %w", rerr)
	}

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if !strings.Contains(line, ":") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 10 {
			continue
		}

		iface := strings.TrimSuffix(fields[0], ":")
		if iface != tm.primaryInterface {
			continue
		}

		rx, _ = strconv.ParseUint(fields[1], 10, 64)
		tx, _ = strconv.ParseUint(fields[9], 10, 64)
		return rx, tx, nil
	}

	return 0, 0, fmt.Errorf("interface %s not found", tm.primaryInterface)
}

// calculateNextCycleEnd calculates the end of the next billing cycle
func (tm *TrafficMonitor) calculateNextCycleEnd(now time.Time) time.Time {
	startDate, _ := time.Parse("2006-01-02", tm.billingStartDate)
	dayOfMonth := startDate.Day()

	// Calculate this month's cycle end date
	year, month := now.Year(), now.Month()
	cycleEnd := time.Date(year, month, dayOfMonth, 0, 0, 0, 0, time.UTC)

	// If this month's cycle day has passed, next cycle is next month
	if now.After(cycleEnd) {
		cycleEnd = cycleEnd.AddDate(0, 1, 0)
	}

	return cycleEnd
}

// checkAndResetCycle checks and resets billing cycle if needed
func (tm *TrafficMonitor) checkAndResetCycle() error {
	now := time.Now()
	if now.Unix() <= tm.billingCycleEndAt {
		return nil // Cycle has not ended
	}

	tm.mu.Lock()
	defer tm.mu.Unlock()

	// Re-check (prevents concurrent resets)
	if now.Unix() <= tm.billingCycleEndAt {
		return nil
	}

	// Read current counters as the new cycle starting point
	rx, tx, err := tm.readInterfaceRxTx()
	if err != nil {
		return fmt.Errorf("failed to reset cycle: %w", err)
	}

	oldCycleEnd := time.Unix(tm.billingCycleEndAt, 0)
	tm.cycleStartRx = rx
	tm.cycleStartTx = tx
	tm.priorUsedBytes = 0 // new cycle starts fresh; the onboarding seed never carries forward
	tm.lastUsedBytes = 0  // ditto — a reboot right after rollover must not fold last cycle's usage in
	tm.billingCycleEndAt = tm.calculateNextCycleEnd(now).Unix()

	// Persist the new baseline so a restart in the new cycle resumes from here.
	// A new cycle resets usage to 0 — the onboarding seed is NEVER re-applied.
	_ = saveTrafficState(tm.statePath, tm.snapshotState())

	slog.Info("Billing cycle reset",
		"component", "traffic",
		"oldEnd", oldCycleEnd.Format("2006-01-02"),
		"newEnd", time.Unix(tm.billingCycleEndAt, 0).Format("2006-01-02"),
		"cycleStartRx", rx, "cycleStartTx", tx)

	return nil
}

// GetTrafficStats returns traffic statistics. Billable usage combines the
// per-direction deltas per the billing mode (sum for AWS Lightsail, max for
// outbound-billed providers) plus the prior-used floor. Takes the write lock:
// each read also refreshes lastUsedBytes, folds NIC counter resets, and persists
// the state at most once per trafficStatePersistInterval.
func (tm *TrafficMonitor) GetTrafficStats() (TrafficStats, error) {
	// Check if billing cycle needs to be reset
	if err := tm.checkAndResetCycle(); err != nil {
		slog.Warn("Failed to check cycle", "component", "traffic", "err", err)
	}

	tm.mu.Lock()
	defer tm.mu.Unlock()

	rx, tx, err := tm.readInterfaceRxTx()
	if err != nil {
		return TrafficStats{}, fmt.Errorf("failed to read traffic: %w", err)
	}

	tm.foldCounterResetLocked(rx, tx)
	usedBytes := tm.computeUsedLocked(rx, tx)
	tm.lastUsedBytes = uint64(usedBytes)
	if time.Since(tm.lastPersistAt) >= trafficStatePersistInterval {
		_ = saveTrafficState(tm.statePath, tm.snapshotState())
		tm.lastPersistAt = time.Now()
	}

	return TrafficStats{
		BillingCycleEndAt:        tm.billingCycleEndAt,
		MonthlyTrafficLimitBytes: tm.trafficLimitGB * bytesPerGiB,
		UsedTrafficBytes:         usedBytes,
	}, nil
}

// AdoptAuthoritativeUsed raises the local meter to a provider-reported figure
// (one-way ratchet: it never lowers usage, so it can only make the enforcer cut
// earlier — the node keeps cutoff authority). epochID must match the cycle the
// figure was computed against (the BillingCycleEndAt the reporter just sent);
// a mismatch (cycle rolled over in between) is ignored. Returns whether the
// figure was adopted.
func (tm *TrafficMonitor) AdoptAuthoritativeUsed(authBytes int64, epochID int64) (bool, error) {
	if authBytes <= 0 {
		return false, nil
	}
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if epochID != tm.billingCycleEndAt {
		return false, nil // stale correction from a previous cycle
	}
	rx, tx, err := tm.readInterfaceRxTx()
	if err != nil {
		return false, fmt.Errorf("read NIC for authoritative adopt: %w", err)
	}
	tm.foldCounterResetLocked(rx, tx)
	if authBytes <= tm.computeUsedLocked(rx, tx) {
		return false, nil // local meter is already at or above the provider figure
	}
	tm.priorUsedBytes = uint64(authBytes)
	tm.cycleStartRx = rx
	tm.cycleStartTx = tx
	tm.lastUsedBytes = uint64(authBytes)
	if err := saveTrafficState(tm.statePath, tm.snapshotState()); err != nil {
		slog.Warn("Failed to persist authoritative baseline", "component", "traffic", "err", err)
	}
	tm.lastPersistAt = time.Now()
	slog.Warn("Adopted provider-authoritative usage (local meter was lower)",
		"component", "traffic", "authoritativeBytes", authBytes)
	return true, nil
}

// TrafficStats traffic statistics data
type TrafficStats struct {
	BillingCycleEndAt        int64 // Billing cycle end timestamp (Unix seconds)
	MonthlyTrafficLimitBytes int64 // Monthly traffic limit (bytes), 0 = unlimited
	UsedTrafficBytes         int64 // Traffic used in current cycle (bytes), mode-combined deltas + prior floor
}
