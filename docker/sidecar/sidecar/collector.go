package sidecar

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// NetworkStats network statistics
type NetworkStats struct {
	RxBytes   uint64
	TxBytes   uint64
	Timestamp time.Time
}

// Collector metrics collector
type Collector struct {
	node           *Node
	reportInterval time.Duration
	lastNetStats   NetworkStats
	trafficMonitor *TrafficMonitor // traffic monitor

	procRoot string // host proc root for net/dev (injectable in tests)
	// hostNIC: procRoot is the real host netns (/host/proc/1). False when
	// hostProcPath fell back to /proc (container veth → fake ~0 Mbps), in which
	// case metrics are not trustworthy and MetricsVersion stays 0.
	hostNIC  bool
	statPath string // path of the aggregate cpu stat file
	lastCPU  cpuSample
	lastNIC  nicStats
	lastAt   time.Time
	// hasBaseline: lastCPU/lastNIC/lastAt hold a valid previous sample.
	hasBaseline bool
}

// cpuSample is one aggregate "cpu" line from /proc/stat (jiffies).
type cpuSample struct{ busy, steal, total uint64 }

// parseCPUSample parses the aggregate "cpu " line. busy counts user, nice,
// system, irq, softirq and steal (fields 1,2,3,6,7,8).
func parseCPUSample(line string) (cpuSample, bool) {
	f := strings.Fields(line)
	if len(f) < 9 || f[0] != "cpu" {
		return cpuSample{}, false
	}
	var v [10]uint64
	for i := 1; i < len(f) && i <= 10; i++ {
		n, err := strconv.ParseUint(f[i], 10, 64)
		if err != nil {
			return cpuSample{}, false
		}
		v[i-1] = n
	}
	var total uint64
	// guest / guest_nice (fields 9,10) are already counted in user / nice.
	for _, n := range v[:8] {
		total += n
	}
	return cpuSample{
		busy:  v[0] + v[1] + v[2] + v[5] + v[6] + v[7],
		steal: v[7],
		total: total,
	}, true
}

// cpuDelta returns interval CPU and steal percentages; ok=false on counter
// reset or an empty interval.
func cpuDelta(prev, cur cpuSample) (cpuPct, stealPct float64, ok bool) {
	if cur.total <= prev.total || cur.busy < prev.busy || cur.steal < prev.steal {
		return 0, 0, false
	}
	dt := float64(cur.total - prev.total)
	return float64(cur.busy-prev.busy) / dt * 100, float64(cur.steal-prev.steal) / dt * 100, true
}

func readCPUSample(path string) (cpuSample, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return cpuSample{}, false
	}
	line, _, _ := strings.Cut(string(data), "\n")
	return parseCPUSample(line)
}

// NewCollector creates a metrics collector
// billingStartDate: billing start date (yyyy-MM-dd), e.g., "2025-01-15"; if empty, traffic tracking is disabled
// trafficLimitGB: monthly traffic limit (GB), 0 = unlimited
// initialUsedGB: declared already-used traffic (GB) to seed the baseline on first boot (mid-cycle onboarding); 0 = none
// billingMode: BillingModeSum / BillingModeMax (empty = max) — must match how the provider bills
func NewCollector(node *Node, reportInterval time.Duration, billingStartDate string, trafficLimitGB int64, initialUsedGB int64, billingMode string) *Collector {
	procRoot := hostProcPath()
	c := &Collector{
		node:           node,
		reportInterval: reportInterval,
		lastNetStats:   NetworkStats{Timestamp: time.Now()},
		procRoot:       procRoot,
		hostNIC:        procRoot != "/proc",
		statPath:       "/proc/stat",
	}

	// If billing date is provided, initialize traffic monitor
	if billingStartDate != "" {
		monitor, err := NewTrafficMonitor(billingStartDate, trafficLimitGB, initialUsedGB, billingMode)
		if err != nil {
			slog.Warn("Failed to initialize traffic monitor", "component", "collector", "err", err)
		} else {
			c.trafficMonitor = monitor
			slog.Info("Traffic monitoring enabled", "component", "collector")
		}
	} else {
		slog.Info("Traffic monitoring disabled (no billing date configured)", "component", "collector")
	}

	return c
}

// TrafficMonitor returns the collector's monitor (nil if billing date unset) so
// the enforcer + usage reporter share the single NIC-reading authority.
func (c *Collector) TrafficMonitor() *TrafficMonitor { return c.trafficMonitor }

// Run runs the metrics collection loop
func (c *Collector) Run() error {
	// Start periodic reporting
	ticker := time.NewTicker(c.reportInterval)
	defer ticker.Stop()

	slog.Info("Starting metrics collection loop", "component", "metrics")
	for {
		select {
		case <-ticker.C:
			if err := c.collectAndReport(); err != nil {
				slog.Error("Error collecting and reporting", "component", "metrics", "err", err)
			}
		}
	}
}

// collectAndReport collects and reports metrics
func (c *Collector) collectAndReport() error {
	health := c.collectMetrics()

	slog.Info("Collected metrics",
		"component", "metrics",
		"cpu", health.CPUUsage,
		"memory", health.MemoryUsage,
		"disk", health.DiskUsage,
		"networkUpMbps", health.BandwidthUpMbps,
		"networkDownMbps", health.BandwidthDownMbps)

	return c.report(health)
}

// collectMetrics collects system metrics
func (c *Collector) collectMetrics() Health {
	health := Health{
		CPUUsage:          getCPUUsage(),
		MemoryUsage:       getMemoryUsage(),
		DiskUsage:         getDiskUsage(),
		Connections:       getConnections(),
		PacketLossPercent: getPacketLoss(),
	}

	// Calculate network bandwidth usage
	netStats := getNetworkStats()
	if !c.lastNetStats.Timestamp.IsZero() {
		duration := netStats.Timestamp.Sub(c.lastNetStats.Timestamp).Seconds()
		if duration > 0 {
			rxDiff := float64(netStats.RxBytes - c.lastNetStats.RxBytes)
			txDiff := float64(netStats.TxBytes - c.lastNetStats.TxBytes)

			// bytes/s -> Mbps
			health.BandwidthDownMbps = (rxDiff * 8) / (duration * 1000000)
			health.BandwidthUpMbps = (txDiff * 8) / (duration * 1000000)
		}
	}

	health.NetworkIn = int64(netStats.RxBytes)
	health.NetworkOut = int64(netStats.TxBytes)
	c.lastNetStats = netStats

	// Trustworthy host metrics: host NIC (not the container veth) + interval CPU.
	health.CPUCores = runtime.NumCPU()
	now := time.Now()
	cpuCur, cpuOK := readCPUSample(c.statPath)
	nicCur, nicErr := readNICStats(c.procRoot)
	if c.hasBaseline && cpuOK && nicErr == nil {
		cpuPct, stealPct, ok1 := cpuDelta(c.lastCPU, cpuCur)
		up, down, loss, ok2 := nicDelta(c.lastNIC, nicCur, now.Sub(c.lastAt).Seconds())
		if ok1 && ok2 {
			health.CPUPercent, health.StealPercent = cpuPct, stealPct
			health.BandwidthUpMbps, health.BandwidthDownMbps = up, down
			health.PacketLossPercent = loss
			if c.hostNIC {
				health.MetricsVersion = 1
			}
		}
	}
	c.hasBaseline = cpuOK && nicErr == nil
	if c.hasBaseline {
		c.lastCPU, c.lastNIC, c.lastAt = cpuCur, nicCur, now
	}

	// Get traffic statistics data
	if c.trafficMonitor != nil {
		trafficStats, err := c.trafficMonitor.GetTrafficStats()
		if err != nil {
			slog.Warn("Failed to get traffic stats", "component", "collector", "err", err)
		} else {
			health.BillingCycleEndAt = trafficStats.BillingCycleEndAt
			health.MonthlyTrafficLimitBytes = trafficStats.MonthlyTrafficLimitBytes
			health.UsedTrafficBytes = trafficStats.UsedTrafficBytes
		}
	}

	return health
}

// report reports metrics to Center
func (c *Collector) report(health Health) error {
	return c.node.ReportStatus(health)
}

// System metrics collection functions

func getCPUUsage() float64 {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0
	}

	lines := strings.Split(string(data), "\n")
	if len(lines) == 0 {
		return 0
	}

	fields := strings.Fields(lines[0])
	if len(fields) < 5 || fields[0] != "cpu" {
		return 0
	}

	var total, idle float64
	for i := 1; i < len(fields); i++ {
		val, _ := strconv.ParseFloat(fields[i], 64)
		total += val
		if i == 4 {
			idle = val
		}
	}

	if total == 0 {
		return 0
	}

	return ((total - idle) / total) * 100
}

func getMemoryUsage() float64 {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0
	}

	var memTotal, memAvailable float64
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}

		value, _ := strconv.ParseFloat(fields[1], 64)
		if strings.HasPrefix(fields[0], "MemTotal:") {
			memTotal = value
		} else if strings.HasPrefix(fields[0], "MemAvailable:") {
			memAvailable = value
		}
	}

	if memTotal == 0 {
		return 0
	}

	return ((memTotal - memAvailable) / memTotal) * 100
}

func getDiskUsage() float64 {
	// Use context with timeout to prevent command hanging
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "df", "-h", "/")
	output, err := cmd.Output()
	if err != nil {
		return 0
	}

	lines := strings.Split(string(output), "\n")
	if len(lines) < 2 {
		return 0
	}

	fields := strings.Fields(lines[1])
	if len(fields) < 5 {
		return 0
	}

	usageStr := strings.TrimSuffix(fields[4], "%")
	usage, _ := strconv.ParseFloat(usageStr, 64)
	return usage
}

func getNetworkStats() NetworkStats {
	data, err := os.ReadFile("/proc/net/dev")
	if err != nil {
		return NetworkStats{Timestamp: time.Now()}
	}

	var rxBytes, txBytes uint64
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
		if iface == "lo" || strings.HasPrefix(iface, "veth") || strings.HasPrefix(iface, "docker") {
			continue
		}

		rx, _ := strconv.ParseUint(fields[1], 10, 64)
		tx, _ := strconv.ParseUint(fields[9], 10, 64)
		rxBytes += rx
		txBytes += tx
	}

	return NetworkStats{
		RxBytes:   rxBytes,
		TxBytes:   txBytes,
		Timestamp: time.Now(),
	}
}

func getConnections() int {
	// Use context with timeout to prevent command hanging
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "ss", "-tan")
	output, err := cmd.Output()
	if err != nil {
		return 0
	}

	lines := strings.Split(string(output), "\n")
	return len(lines) - 1
}

func getPacketLoss() float64 {
	data, err := os.ReadFile("/proc/net/dev")
	if err != nil {
		return 0
	}

	var totalDropped, totalPackets uint64
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if !strings.Contains(line, ":") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}

		iface := strings.TrimSuffix(fields[0], ":")
		if iface == "lo" || strings.HasPrefix(iface, "veth") {
			continue
		}

		packets, _ := strconv.ParseUint(fields[2], 10, 64)
		dropped, _ := strconv.ParseUint(fields[4], 10, 64)
		totalPackets += packets
		totalDropped += dropped
	}

	if totalPackets == 0 {
		return 0
	}

	return (float64(totalDropped) / float64(totalPackets)) * 100
}
