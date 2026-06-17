package sidecar

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
)

// host_nic.go reads cumulative byte counters from the HOST network interface,
// not the sidecar container's own netns. A bridge-network container reading
// /proc/net/dev sees its veth (≈0 billed traffic); the compose mounts the
// host's /proc at /host/proc so we can read the actually-billed NIC. This is the
// single source of host-NIC truth used by both the billing usage reporter and
// the display traffic monitor.

// hostProcPath returns the proc mount to read NIC counters from.
//
// CRITICAL: /proc/net/* is network-namespace-relative. A bridge-network
// container reading the bind-mounted /host/proc/net/dev still resolves to its
// OWN netns (its veth, ≈0 billed traffic), NOT the host NIC — which silently
// zeroed host-NIC billing and meant the quota cutoff never tripped on real
// traffic. PID 1 always lives in the host net namespace, so /host/proc/1/net/dev
// is the real host NIC counter. Prefer it; fall back to /proc (dev / no mount).
func hostProcPath() string {
	if _, err := os.Stat("/host/proc/1/net/dev"); err == nil {
		return "/host/proc/1"
	}
	return "/proc"
}

// readNICBytes sums cumulative rx+tx bytes across physical interfaces in
// {procPath}/net/dev, skipping lo / veth* / docker*. The value is monotonic
// since boot (kernel counters). procPath is injectable for tests.
func readNICBytes(procPath string) (int64, error) {
	data, err := os.ReadFile(procPath + "/net/dev")
	if err != nil {
		return 0, fmt.Errorf("read %s/net/dev: %w", procPath, err)
	}

	var total int64
	for _, line := range strings.Split(string(data), "\n") {
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
		total += int64(rx + tx)
	}
	return total, nil
}

// hostNICMeter implements nicMeter (usage_reporter.go). It returns cumulative
// host-NIC bytes minus a baseline that resets on each Center epoch, so the value
// POSTed as cumulative_bytes restarts from 0 at every epoch — matching Center's
// per-epoch ledger semantics (api/slave_api_usage.go). A single goroutine (the
// reporter's Run loop and the cutoff enforcer may call concurrently, so
// baseline is guarded by mu.
type hostNICMeter struct {
	mu       sync.Mutex
	procPath string
	baseline int64
}

// NewHostNICMeter constructs a meter rooted at the host proc mount and seeds its
// baseline to the current reading (so cumulative starts near 0 on a fresh boot).
func NewHostNICMeter() *hostNICMeter {
	m := &hostNICMeter{procPath: hostProcPath()}
	if b, err := readNICBytes(m.procPath); err == nil {
		m.baseline = b
	}
	return m
}

// CumulativeBytes returns host-NIC bytes consumed since the last Rebaseline. If
// the raw reading dropped below the baseline (reboot / counter wrap) it
// rebaselines to the current reading and returns 0 — never a negative or
// absurdly large delta.
func (m *hostNICMeter) CumulativeBytes() (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	raw, err := readNICBytes(m.procPath)
	if err != nil {
		return 0, err
	}
	if raw < m.baseline {
		m.baseline = raw
		return 0, nil
	}
	return raw - m.baseline, nil
}

// Rebaseline sets the baseline to the current reading (called on a Center epoch
// change so the next cumulative_bytes restarts from 0).
func (m *hostNICMeter) Rebaseline() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if b, err := readNICBytes(m.procPath); err == nil {
		m.baseline = b
	}
}
