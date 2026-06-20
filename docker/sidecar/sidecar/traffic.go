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

// trafficState persists the per-direction cycle baseline so a restart resumes
// in-cycle usage instead of re-anchoring to the current NIC counters (which
// would zero usage). Baselines are kept per direction because billable usage is
// max(inbound, outbound) — see GetTrafficStats.
type trafficState struct {
	BillingCycleEndAt int64  `json:"billing_cycle_end_at"`
	CycleStartRx      uint64 `json:"cycle_start_rx"`
	CycleStartTx      uint64 `json:"cycle_start_tx"`
}

// hasBaseline reports whether the persisted state carries a usable per-direction
// baseline. Legacy state files (pre per-direction) leave both zero and are
// treated as absent → the node re-anchors once on upgrade.
func (s trafficState) hasBaseline() bool { return s.CycleStartRx > 0 || s.CycleStartTx > 0 }

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
	cycleStartRx      uint64    // RX counter at start of current billing cycle
	cycleStartTx      uint64    // TX counter at start of current billing cycle
	primaryInterface  string    // primary network interface name
	lastDetectedAt    time.Time // last time interface was detected
	procPath          string    // proc mount for NIC reads ("/host/proc" in prod)
	statePath         string    // persisted cycle baseline path (default /etc/kaitu/traffic.state)
}

const bytesPerGiB = int64(1024 * 1024 * 1024)

// NewTrafficMonitor creates a traffic monitor.
//   - billingStartDate: billing start date (yyyy-MM-dd), e.g. "2025-01-15"
//   - trafficLimitGB:   monthly traffic limit (GB), 0 = unlimited
//   - initialUsedGB:    declared already-used traffic (GB) to SEED the baseline
//     when this node is onboarded mid-cycle. Applied ONLY on the very first boot
//     (no persisted state yet); thereafter the persisted record wins so a restart
//     never re-applies it. 0 = no seed. Adjust later with SetUsage.
func NewTrafficMonitor(billingStartDate string, trafficLimitGB int64, initialUsedGB int64) (*TrafficMonitor, error) {
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
		slog.Info("Traffic cycle baseline restored from state", "component", "traffic",
			"cycleStartRx", tm.cycleStartRx, "cycleStartTx", tm.cycleStartTx)
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
	}
}

// applyUsageBaseline sets the per-direction baseline so that the current usage
// (max of the two deltas) equals usedGB. A direction whose counter is below the
// declared usage clamps to 0 (its delta then reads the full counter, but the
// dominant direction — from which the declared usage was derived — carries it).
func (tm *TrafficMonitor) applyUsageBaseline(rx, tx uint64, usedGB int64) {
	used := uint64(usedGB) * uint64(bytesPerGiB)
	if rx >= used {
		tm.cycleStartRx = rx - used
	} else {
		tm.cycleStartRx = 0
	}
	if tx >= used {
		tm.cycleStartTx = tx - used
	} else {
		tm.cycleStartTx = 0
	}
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

// GetTrafficStats returns traffic statistics. Billable usage is the GREATER of
// the inbound and outbound deltas this cycle (AWS bills the larger direction),
// NOT their sum.
func (tm *TrafficMonitor) GetTrafficStats() (TrafficStats, error) {
	// Check if billing cycle needs to be reset
	if err := tm.checkAndResetCycle(); err != nil {
		slog.Warn("Failed to check cycle", "component", "traffic", "err", err)
	}

	tm.mu.RLock()
	defer tm.mu.RUnlock()

	rx, tx, err := tm.readInterfaceRxTx()
	if err != nil {
		return TrafficStats{}, fmt.Errorf("failed to read traffic: %w", err)
	}

	rxDelta := int64(0)
	if rx > tm.cycleStartRx {
		rxDelta = int64(rx - tm.cycleStartRx)
	}
	txDelta := int64(0)
	if tx > tm.cycleStartTx {
		txDelta = int64(tx - tm.cycleStartTx)
	}
	usedBytes := rxDelta
	if txDelta > usedBytes {
		usedBytes = txDelta
	}

	return TrafficStats{
		BillingCycleEndAt:        tm.billingCycleEndAt,
		MonthlyTrafficLimitBytes: tm.trafficLimitGB * bytesPerGiB,
		UsedTrafficBytes:         usedBytes,
	}, nil
}

// TrafficStats traffic statistics data
type TrafficStats struct {
	BillingCycleEndAt        int64 // Billing cycle end timestamp (Unix seconds)
	MonthlyTrafficLimitBytes int64 // Monthly traffic limit (bytes), 0 = unlimited
	UsedTrafficBytes         int64 // Traffic used in current cycle (bytes), = max(rx,tx) delta
}
