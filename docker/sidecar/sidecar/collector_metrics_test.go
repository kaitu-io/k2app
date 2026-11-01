package sidecar

import (
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

func TestParseCPUSample(t *testing.T) {
	s, ok := parseCPUSample("cpu 100 0 50 800 10 0 20 20 0 0")
	if !ok {
		t.Fatal("parse failed")
	}
	if s.busy != 190 || s.total != 1000 || s.steal != 20 {
		t.Fatalf("got %+v, want busy=190 total=1000 steal=20", s)
	}
	if _, ok := parseCPUSample("cpu0 1 2 3"); ok {
		t.Fatal("short/non-aggregate line must fail")
	}
}

func TestParseCPUSample_ExcludesGuest(t *testing.T) {
	s, ok := parseCPUSample("cpu 100 0 50 800 10 0 20 20 30 5")
	if !ok {
		t.Fatal("parse failed")
	}
	if s.total != 1000 || s.busy != 190 {
		t.Fatalf("got %+v, want total=1000 busy=190 (guest excluded)", s)
	}
}

func TestReadNICStats_SkipsDockerBridge(t *testing.T) {
	body := sampleNetDev + ` br-abc123: 9000000    9000    0    5    0     0          0         0   8000000    8000    0    0    0     0       0          0
`
	st, err := readNICStats(writeProcNetDev(t, body))
	if err != nil {
		t.Fatal(err)
	}
	if st.rx != 1000000 || st.tx != 500000 || st.rxPackets != 2000 || st.rxDropped != 0 {
		t.Fatalf("br- counted: %+v", st)
	}
}

func TestCPUDelta_Interval(t *testing.T) {
	prev := cpuSample{busy: 190, steal: 20, total: 1000}
	cur := cpuSample{busy: 290, steal: 30, total: 1200}
	cpu, steal, ok := cpuDelta(prev, cur)
	if !ok {
		t.Fatal("expected ok")
	}
	if !approx(cpu, 50) || !approx(steal, 5) {
		t.Fatalf("cpu=%v steal=%v, want 50 / 5", cpu, steal)
	}
}

func TestCPUDelta_CounterReset(t *testing.T) {
	if _, _, ok := cpuDelta(cpuSample{busy: 5, total: 1000}, cpuSample{busy: 1, total: 10}); ok {
		t.Fatal("total regression must be !ok")
	}
	if _, _, ok := cpuDelta(cpuSample{total: 10}, cpuSample{total: 10}); ok {
		t.Fatal("zero total delta must be !ok")
	}
}

func TestReadNICStats_SplitsDirections(t *testing.T) {
	root := writeProcNetDev(t, sampleNetDev)
	st, err := readNICStats(root)
	if err != nil {
		t.Fatal(err)
	}
	if st.rx != 1000000 || st.tx != 500000 || st.rxPackets != 2000 || st.rxDropped != 0 {
		t.Fatalf("got %+v", st)
	}
}

func TestNICDelta_Rates(t *testing.T) {
	prev := nicStats{rx: 0, tx: 0, rxPackets: 1000, rxDropped: 0}
	cur := nicStats{rx: 12_500_000, tx: 2_500_000, rxPackets: 2000, rxDropped: 10}
	up, down, loss, ok := nicDelta(prev, cur, 10)
	if !ok {
		t.Fatal("expected ok")
	}
	// 12.5MB*8/10s = 10 Mbps down; 2.5MB*8/10 = 2 Mbps up; 10/1000 = 1%
	if !approx(down, 10) || !approx(up, 2) || !approx(loss, 1) {
		t.Fatalf("up=%v down=%v loss=%v", up, down, loss)
	}
}

func TestNICDelta_CounterReset(t *testing.T) {
	prev := nicStats{rx: 100, tx: 100, rxPackets: 10}
	cur := nicStats{rx: 50, tx: 200, rxPackets: 20}
	if _, _, _, ok := nicDelta(prev, cur, 10); ok {
		t.Fatal("regression must be !ok")
	}
	if _, _, _, ok := nicDelta(prev, nicStats{rx: 200, tx: 200, rxPackets: 20}, 0); ok {
		t.Fatal("zero seconds must be !ok")
	}
}

func writeStat(t *testing.T, path, line string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(line+"\ncpu0 1 1 1 1 1 1 1 1 0 0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func newTestCollector(t *testing.T, netDev string) (*Collector, string, string) {
	t.Helper()
	root := writeProcNetDev(t, netDev)
	stat := filepath.Join(t.TempDir(), "stat")
	writeStat(t, stat, "cpu 100 0 50 800 10 0 20 20 0 0")
	c := &Collector{procRoot: root, statPath: stat}
	return c, root, stat
}

func TestCollectMetrics_FirstCycleIsVersionZero(t *testing.T) {
	c, _, _ := newTestCollector(t, sampleNetDev)
	h := c.collectMetrics()
	if h.MetricsVersion != 0 {
		t.Fatalf("MetricsVersion = %d, want 0", h.MetricsVersion)
	}
	if h.CPUCores <= 0 {
		t.Fatalf("CPUCores = %d", h.CPUCores)
	}
}

func TestCollectMetrics_SecondCycleIsVersionOne(t *testing.T) {
	c, root, stat := newTestCollector(t, sampleNetDev)
	c.collectMetrics()

	// advance: eth0 rx +1,000,000 tx +500,000; rx packets +1000 with 10 drops.
	next := `Inter-|   Receive                                                |  Transmit
 face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed
    lo:  100000     500    0    0    0     0          0         0   100000     500    0    0    0     0       0          0
  eth0: 2000000    3000    0   10    0     0          0         0  1000000    2500    0    0    0     0       0          0
`
	if err := os.WriteFile(filepath.Join(root, "net", "dev"), []byte(next), 0o644); err != nil {
		t.Fatal(err)
	}
	writeStat(t, stat, "cpu 200 0 100 1600 20 0 40 40 0 0")
	// make the interval deterministic and nonzero
	c.lastAt = c.lastAt.Add(-time.Second)

	h := c.collectMetrics()
	if h.MetricsVersion != 1 {
		t.Fatalf("MetricsVersion = %d, want 1", h.MetricsVersion)
	}
	if h.BandwidthDownMbps <= 0 || h.BandwidthUpMbps <= 0 {
		t.Fatalf("bandwidth not host-based: up=%v down=%v", h.BandwidthUpMbps, h.BandwidthDownMbps)
	}
	if h.BandwidthDownMbps < h.BandwidthUpMbps*1.9 || h.BandwidthDownMbps > h.BandwidthUpMbps*2.1 {
		t.Fatalf("down/up ratio off: up=%v down=%v", h.BandwidthUpMbps, h.BandwidthDownMbps)
	}
	if !approx(h.PacketLossPercent, 1) {
		t.Fatalf("loss = %v, want 1", h.PacketLossPercent)
	}
	if h.CPUPercent <= 0 || h.StealPercent <= 0 {
		t.Fatalf("cpu=%v steal=%v", h.CPUPercent, h.StealPercent)
	}
}
