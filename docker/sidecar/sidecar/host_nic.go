package sidecar

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// host_nic.go reads cumulative byte counters from the HOST network interface,
// not the sidecar container's own netns. A bridge-network container reading
// /proc/net/dev sees its veth (≈0 billed traffic); the compose mounts the
// host's /proc at /host/proc so we can read the actually-billed NIC. These
// helpers (hostProcPath + readNICBytes) are the single source of host-NIC truth
// used by the TrafficMonitor (the metering authority read by both the enforcer
// and the usage reporter).

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

// nicStats holds directional cumulative counters of the physical host NICs.
// rxPackets / rxDropped feed the packet-loss ratio (receive side).
type nicStats struct {
	rx, tx, rxPackets, rxDropped uint64
}

// readNICStats sums per-direction counters across physical interfaces in
// {procPath}/net/dev, with the same skip rules as readNICBytes.
func readNICStats(procPath string) (nicStats, error) {
	var st nicStats
	data, err := os.ReadFile(procPath + "/net/dev")
	if err != nil {
		return st, fmt.Errorf("read %s/net/dev: %w", procPath, err)
	}
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
		pk, _ := strconv.ParseUint(fields[2], 10, 64)
		dr, _ := strconv.ParseUint(fields[4], 10, 64)
		tx, _ := strconv.ParseUint(fields[9], 10, 64)
		st.rx += rx
		st.tx += tx
		st.rxPackets += pk
		st.rxDropped += dr
	}
	return st, nil
}

// nicDelta turns two samples into interval rates. ok=false on a counter reset
// (any counter went backwards) or a non-positive interval.
func nicDelta(prev, cur nicStats, seconds float64) (upMbps, downMbps, lossPct float64, ok bool) {
	if seconds <= 0 || cur.rx < prev.rx || cur.tx < prev.tx ||
		cur.rxPackets < prev.rxPackets || cur.rxDropped < prev.rxDropped {
		return 0, 0, 0, false
	}
	upMbps = float64(cur.tx-prev.tx) * 8 / (seconds * 1e6)
	downMbps = float64(cur.rx-prev.rx) * 8 / (seconds * 1e6)
	if dp := cur.rxPackets - prev.rxPackets; dp > 0 {
		lossPct = float64(cur.rxDropped-prev.rxDropped) / float64(dp) * 100
	}
	return upMbps, downMbps, lossPct, true
}
