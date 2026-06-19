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

// trafficState persists the cycle baseline so a restart resumes in-cycle usage
// instead of re-anchoring to the current NIC counter (which would zero usage).
type trafficState struct {
	BillingCycleEndAt int64  `json:"billing_cycle_end_at"`
	CycleStartBytes   uint64 `json:"cycle_start_bytes"`
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
	mu               sync.RWMutex
	billingStartDate string    // billing start date (yyyy-MM-dd)
	billingCycleEndAt int64    // current billing cycle end timestamp
	trafficLimitGB   int64    // monthly traffic limit (GB), 0 = unlimited
	startBytes       uint64   // cumulative traffic at start of monitoring
	cycleStartBytes  uint64   // cumulative traffic at start of current billing cycle
	primaryInterface string   // primary network interface name
	lastDetectedAt   time.Time // last time interface was detected
	procPath         string    // proc mount for NIC reads ("/host/proc" in prod)
	statePath        string    // persisted cycle baseline path (default /etc/kaitu/traffic.state)
}

// NewTrafficMonitor creates a traffic monitor
// billingStartDate: billing start date (yyyy-MM-dd), e.g., "2025-01-15"
// trafficLimitGB: monthly traffic limit (GB), 0 = unlimited
func NewTrafficMonitor(billingStartDate string, trafficLimitGB int64) (*TrafficMonitor, error) {
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
	}

	// Auto-detect primary network interface
	if err := tm.detectPrimaryInterface(); err != nil {
		return nil, fmt.Errorf("failed to detect primary interface: %w", err)
	}

	// Read current traffic as starting value
	currentBytes, err := tm.readInterfaceBytes()
	if err != nil {
		return nil, fmt.Errorf("failed to read initial traffic: %w", err)
	}
	tm.startBytes = currentBytes
	tm.cycleStartBytes = currentBytes

	// Calculate current billing cycle end time
	tm.billingCycleEndAt = tm.calculateNextCycleEnd(time.Now()).Unix()

	// Restore the cycle baseline across restarts IF the persisted cycle still
	// matches the current one — otherwise a restart re-anchors the baseline to
	// the current NIC counter and silently resets in-cycle usage to ~0.
	tm.statePath = "/etc/kaitu/traffic.state"
	if st := loadTrafficState(tm.statePath); st.BillingCycleEndAt == tm.billingCycleEndAt && st.CycleStartBytes > 0 {
		tm.cycleStartBytes = st.CycleStartBytes // resume in-cycle usage across restart
		slog.Info("Traffic cycle baseline restored from state", "component", "traffic", "cycleStartBytes", tm.cycleStartBytes)
	} else {
		_ = saveTrafficState(tm.statePath, trafficState{BillingCycleEndAt: tm.billingCycleEndAt, CycleStartBytes: tm.cycleStartBytes})
	}

	slog.Info("Traffic monitor initialized",
		"component", "traffic",
		"interface", tm.primaryInterface,
		"billingDate", billingStartDate,
		"limitGB", trafficLimitGB,
		"startBytes", currentBytes)

	return tm, nil
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

		// RX + TX bytes
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

// readInterfaceBytes reads cumulative byte count (RX + TX) for primary interface
func (tm *TrafficMonitor) readInterfaceBytes() (uint64, error) {
	// Re-detect primary interface every hour (prevents issues from interface changes)
	if time.Since(tm.lastDetectedAt) > time.Hour {
		if err := tm.detectPrimaryInterface(); err != nil {
			slog.Warn("Failed to re-detect interface", "component", "traffic", "err", err)
		}
	}

	data, err := os.ReadFile(tm.procPath + "/net/dev")
	if err != nil {
		return 0, fmt.Errorf("failed to read /proc/net/dev: %w", err)
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

		rx, _ := strconv.ParseUint(fields[1], 10, 64)
		tx, _ := strconv.ParseUint(fields[9], 10, 64)
		return rx + tx, nil
	}

	return 0, fmt.Errorf("interface %s not found", tm.primaryInterface)
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

	// Read current traffic as new cycle starting point
	currentBytes, err := tm.readInterfaceBytes()
	if err != nil {
		return fmt.Errorf("failed to reset cycle: %w", err)
	}

	oldCycleEnd := time.Unix(tm.billingCycleEndAt, 0)
	tm.cycleStartBytes = currentBytes
	tm.billingCycleEndAt = tm.calculateNextCycleEnd(now).Unix()

	// Persist the new baseline so a restart in the new cycle resumes from here.
	_ = saveTrafficState(tm.statePath, trafficState{BillingCycleEndAt: tm.billingCycleEndAt, CycleStartBytes: tm.cycleStartBytes})

	slog.Info("Billing cycle reset",
		"component", "traffic",
		"oldEnd", oldCycleEnd.Format("2006-01-02"),
		"newEnd", time.Unix(tm.billingCycleEndAt, 0).Format("2006-01-02"),
		"cycleStartBytes", currentBytes)

	return nil
}

// GetTrafficStats returns traffic statistics
func (tm *TrafficMonitor) GetTrafficStats() (TrafficStats, error) {
	// Check if billing cycle needs to be reset
	if err := tm.checkAndResetCycle(); err != nil {
		slog.Warn("Failed to check cycle", "component", "traffic", "err", err)
	}

	tm.mu.RLock()
	defer tm.mu.RUnlock()

	currentBytes, err := tm.readInterfaceBytes()
	if err != nil {
		return TrafficStats{}, fmt.Errorf("failed to read traffic: %w", err)
	}

	// Calculate traffic used in current cycle
	usedBytes := int64(0)
	if currentBytes > tm.cycleStartBytes {
		usedBytes = int64(currentBytes - tm.cycleStartBytes)
	}

	return TrafficStats{
		BillingCycleEndAt:        tm.billingCycleEndAt,
		MonthlyTrafficLimitBytes: tm.trafficLimitGB * 1024 * 1024 * 1024,
		UsedTrafficBytes:         usedBytes,
	}, nil
}

// TrafficStats traffic statistics data
type TrafficStats struct {
	BillingCycleEndAt        int64 // Billing cycle end timestamp (Unix seconds)
	MonthlyTrafficLimitBytes int64 // Monthly traffic limit (bytes), 0 = unlimited
	UsedTrafficBytes         int64 // Traffic used in current cycle (bytes)
}
