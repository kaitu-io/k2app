package sidecar

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// logRec is one captured slog record.
type logRec struct {
	Level slog.Level
	Msg   string
	Attrs map[string]any
}

type capHandler struct {
	mu   *sync.Mutex
	recs *[]logRec
}

func (h capHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h capHandler) WithAttrs([]slog.Attr) slog.Handler       { return h }
func (h capHandler) WithGroup(string) slog.Handler            { return h }
func (h capHandler) Handle(_ context.Context, r slog.Record) error {
	rec := logRec{Level: r.Level, Msg: r.Message, Attrs: map[string]any{}}
	r.Attrs(func(a slog.Attr) bool { rec.Attrs[a.Key] = a.Value.Any(); return true })
	h.mu.Lock()
	*h.recs = append(*h.recs, rec)
	h.mu.Unlock()
	return nil
}

// captureLogs swaps the default slog handler for the test's duration.
func captureLogs(t *testing.T) func() []logRec {
	t.Helper()
	var mu sync.Mutex
	var recs []logRec
	old := slog.Default()
	slog.SetDefault(slog.New(capHandler{&mu, &recs}))
	t.Cleanup(func() { slog.SetDefault(old) })
	return func() []logRec {
		mu.Lock()
		defer mu.Unlock()
		return append([]logRec(nil), recs...)
	}
}

func countMsg(recs []logRec, lvl slog.Level, sub string) int {
	n := 0
	for _, r := range recs {
		if r.Level == lvl && strings.Contains(r.Msg, sub) {
			n++
		}
	}
	return n
}

const snmpStd = `Ip: Forwarding DefaultTTL
Ip: 1 64
Udp: InDatagrams NoPorts InErrors OutDatagrams RcvbufErrors SndbufErrors InCsumErrors IgnoredMulti MemErrors
Udp: %d 5 7 900 %d 0 0 0 0
UdpLite: InDatagrams NoPorts InErrors OutDatagrams RcvbufErrors SndbufErrors InCsumErrors IgnoredMulti MemErrors
UdpLite: 99 0 0 0 99 0 0 0 0
`

func TestParseUDPSnmp(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    udpCounters
		wantErr bool
	}{
		{"standard, UdpLite ignored", fmt.Sprintf(snmpStd, 1000, 20), udpCounters{1000, 20}, false},
		{"reordered columns parsed by name",
			"Udp: RcvbufErrors InDatagrams NoPorts\nUdp: 3 400 9\n", udpCounters{400, 3}, false},
		{"extra leading column", "Udp: Foo InDatagrams RcvbufErrors\nUdp: 1 50 2\n", udpCounters{50, 2}, false},
		{"malformed: header without RcvbufErrors", "Udp: InDatagrams NoPorts\nUdp: 5 6\n", udpCounters{}, true},
		{"malformed: value count mismatch", "Udp: InDatagrams RcvbufErrors\nUdp: 5\n", udpCounters{}, true},
		{"malformed: non-numeric", "Udp: InDatagrams RcvbufErrors\nUdp: x 1\n", udpCounters{}, true},
		{"missing Udp line", "Ip: Forwarding\nIp: 1\nUdpLite: InDatagrams RcvbufErrors\nUdpLite: 1 1\n", udpCounters{}, true},
		{"header only", "Udp: InDatagrams RcvbufErrors\n", udpCounters{}, true},
		{"empty", "", udpCounters{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseUDPSnmp(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

// fakeProc builds a host-proc dir with pid dirs.
type fakeProc struct{ dir string }

func newFakeProc(t *testing.T) fakeProc { return fakeProc{t.TempDir()} }

func (p fakeProc) proc(t *testing.T, pid int, comm string, snmp string) {
	t.Helper()
	d := filepath.Join(p.dir, fmt.Sprint(pid))
	if err := os.MkdirAll(filepath.Join(d, "net"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "comm"), []byte(comm+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if snmp != "" {
		if err := os.WriteFile(filepath.Join(d, "net", "snmp"), []byte(snmp), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func (p fakeProc) remove(t *testing.T, pid int) {
	t.Helper()
	if err := os.RemoveAll(filepath.Join(p.dir, fmt.Sprint(pid))); err != nil {
		t.Fatal(err)
	}
}

func attrsMap(kv []any) map[string]any {
	m := map[string]any{}
	for i := 0; i+1 < len(kv); i += 2 {
		m[kv[i].(string)] = kv[i+1]
	}
	return m
}

func TestUDPWatcher_Intervals(t *testing.T) {
	t.Run("first sample baselines, second reports interval", func(t *testing.T) {
		p := newFakeProc(t)
		p.proc(t, 1, "systemd", "")
		p.proc(t, 42, "k2s", fmt.Sprintf(snmpStd, 1000, 10))
		w := &udpWatcher{procDir: p.dir}
		if got := w.sample(); got != nil {
			t.Fatalf("first sample must be nil, got %v", got)
		}
		p.proc(t, 42, "k2s", fmt.Sprintf(snmpStd, 2000, 30))
		m := attrsMap(w.sample())
		if m["udpInDatagrams"] != uint64(1000) || m["udpRcvbufErrors"] != uint64(20) || !approx(m["udpDropPct"].(float64), 2) {
			t.Fatalf("got %v", m)
		}
	})
	t.Run("comm must match exactly", func(t *testing.T) {
		p := newFakeProc(t)
		p.proc(t, 7, "k2s-extra", fmt.Sprintf(snmpStd, 1, 1))
		p.proc(t, 8, "k2", fmt.Sprintf(snmpStd, 1, 1))
		w := &udpWatcher{procDir: p.dir}
		if pid := w.findK2sPID(); pid != 0 {
			t.Fatalf("matched pid %d on non-exact comm", pid)
		}
	})
	t.Run("zero InDatagrams delta gives 0 pct, no NaN/Inf", func(t *testing.T) {
		p := newFakeProc(t)
		p.proc(t, 42, "k2s", fmt.Sprintf(snmpStd, 1000, 10))
		w := &udpWatcher{procDir: p.dir}
		w.sample()
		p.proc(t, 42, "k2s", fmt.Sprintf(snmpStd, 1000, 15)) // drops but no deliveries
		m := attrsMap(w.sample())
		if m["udpInDatagrams"] != uint64(0) || m["udpRcvbufErrors"] != uint64(5) || m["udpDropPct"] != float64(0) {
			t.Fatalf("got %v", m)
		}
	})
	t.Run("counter fall skips one interval then re-baselines", func(t *testing.T) {
		p := newFakeProc(t)
		p.proc(t, 42, "k2s", fmt.Sprintf(snmpStd, 5000, 50))
		w := &udpWatcher{procDir: p.dir}
		w.sample()
		p.proc(t, 42, "k2s", fmt.Sprintf(snmpStd, 100, 1)) // in-place restart / reset
		if got := w.sample(); got != nil {
			t.Fatalf("fall must skip, got %v", got)
		}
		p.proc(t, 42, "k2s", fmt.Sprintf(snmpStd, 200, 2))
		m := attrsMap(w.sample())
		if m["udpInDatagrams"] != uint64(100) || m["udpRcvbufErrors"] != uint64(1) {
			t.Fatalf("not re-baselined: %v", m)
		}
	})
	t.Run("rcvbuf fall alone also skips", func(t *testing.T) {
		p := newFakeProc(t)
		p.proc(t, 42, "k2s", fmt.Sprintf(snmpStd, 100, 50))
		w := &udpWatcher{procDir: p.dir}
		w.sample()
		p.proc(t, 42, "k2s", fmt.Sprintf(snmpStd, 200, 5))
		if got := w.sample(); got != nil {
			t.Fatalf("got %v", got)
		}
	})
	t.Run("pid change skips one interval even when counters rose", func(t *testing.T) {
		p := newFakeProc(t)
		p.proc(t, 42, "k2s", fmt.Sprintf(snmpStd, 1000, 10))
		w := &udpWatcher{procDir: p.dir}
		w.sample()
		p.remove(t, 42)
		p.proc(t, 77, "k2s", fmt.Sprintf(snmpStd, 9000, 90)) // new process, higher counters
		if got := w.sample(); got != nil {
			t.Fatalf("pid change must skip, got %v", got)
		}
		if w.pid != 77 {
			t.Fatalf("pid = %d, want 77", w.pid)
		}
		p.proc(t, 77, "k2s", fmt.Sprintf(snmpStd, 9100, 91))
		m := attrsMap(w.sample())
		if m["udpInDatagrams"] != uint64(100) {
			t.Fatalf("not re-baselined on new pid: %v", m)
		}
	})
	t.Run("pid reused by another command is not trusted", func(t *testing.T) {
		p := newFakeProc(t)
		p.proc(t, 42, "k2s", fmt.Sprintf(snmpStd, 1000, 10))
		w := &udpWatcher{procDir: p.dir}
		w.sample()
		p.proc(t, 42, "nginx", fmt.Sprintf(snmpStd, 2000, 20))
		if got := w.sample(); got != nil {
			t.Fatalf("got %v", got)
		}
	})
	t.Run("disabled when procDir empty (/proc fallback)", func(t *testing.T) {
		logs := captureLogs(t)
		w := &udpWatcher{}
		if w.sample() != nil {
			t.Fatal("must be a no-op")
		}
		if n := len(logs()); n != 0 {
			t.Fatalf("fallback must log nothing, got %d records", n)
		}
	})
	t.Run("transient read failure re-baselines instead of spanning the gap", func(t *testing.T) {
		p := newFakeProc(t)
		p.proc(t, 42, "k2s", fmt.Sprintf(snmpStd, 1000, 10))
		w := &udpWatcher{procDir: p.dir}
		w.sample()
		p.proc(t, 42, "k2s", "garbage\n")
		w.sample()
		p.proc(t, 42, "k2s", fmt.Sprintf(snmpStd, 3000, 30))
		if got := w.sample(); got != nil {
			t.Fatalf("first sample after a failure must only re-baseline, got %v", got)
		}
	})
}

// Cached pid: a healthy interval must not rescan the proc dir.
func TestUDPWatcher_NoRescanWhileHealthy(t *testing.T) {
	p := newFakeProc(t)
	p.proc(t, 42, "k2s", fmt.Sprintf(snmpStd, 1000, 10))
	w := &udpWatcher{procDir: p.dir}
	w.sample()
	// A decoy k2s with a lower pid appears; a rescan would pick it up.
	p.proc(t, 10, "k2s", fmt.Sprintf(snmpStd, 1, 1)) // "10" sorts before "42" in ReadDir
	p.proc(t, 42, "k2s", fmt.Sprintf(snmpStd, 1100, 11))
	w.sample()
	if w.pid != 42 {
		t.Fatalf("pid = %d, rescanned while healthy", w.pid)
	}
}

func TestUDPWatcher_WarnDedupe(t *testing.T) {
	t.Run("absent process warns once, never fails", func(t *testing.T) {
		logs := captureLogs(t)
		p := newFakeProc(t)
		p.proc(t, 1, "systemd", "")
		w := &udpWatcher{procDir: p.dir}
		for i := 0; i < 5; i++ {
			if got := w.sample(); got != nil {
				t.Fatalf("absent process must give no fields, got %v", got)
			}
		}
		if n := countMsg(logs(), slog.LevelWarn, "k2s process not found"); n != 1 {
			t.Fatalf("warns = %d, want 1", n)
		}
	})
	t.Run("recovery re-arms the warning", func(t *testing.T) {
		logs := captureLogs(t)
		p := newFakeProc(t)
		w := &udpWatcher{procDir: p.dir}
		w.sample()
		p.proc(t, 42, "k2s", fmt.Sprintf(snmpStd, 10, 1))
		w.sample()
		p.remove(t, 42)
		w.sample()
		w.sample()
		if n := countMsg(logs(), slog.LevelWarn, "k2s process not found"); n != 2 {
			t.Fatalf("warns = %d, want 2 (one per outage)", n)
		}
	})
	t.Run("malformed snmp warns once", func(t *testing.T) {
		logs := captureLogs(t)
		p := newFakeProc(t)
		p.proc(t, 42, "k2s", "Udp: InDatagrams\nUdp: 1\n")
		w := &udpWatcher{procDir: p.dir}
		for i := 0; i < 4; i++ {
			w.sample()
		}
		if n := countMsg(logs(), slog.LevelWarn, "cannot read k2s net/snmp"); n != 1 {
			t.Fatalf("warns = %d, want 1", n)
		}
	})
	t.Run("permission denied is reported as such, once", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("root bypasses file modes")
		}
		logs := captureLogs(t)
		p := newFakeProc(t)
		p.proc(t, 42, "k2s", fmt.Sprintf(snmpStd, 1, 1))
		snmp := filepath.Join(p.dir, "42", "net", "snmp")
		if err := os.Chmod(snmp, 0); err != nil {
			t.Fatal(err)
		}
		w := &udpWatcher{procDir: p.dir}
		for i := 0; i < 3; i++ {
			if w.sample() != nil {
				t.Fatal("denied must give no fields")
			}
		}
		if n := countMsg(logs(), slog.LevelWarn, "no permission"); n != 1 {
			t.Fatalf("warns = %d, want 1", n)
		}
	})
}

func newLogCollector(t *testing.T) (*Collector, string, string) {
	c, root, stat := newTestCollector(t, sampleNetDev)
	return c, root, stat
}

const nextNetDev = `Inter-|   Receive                                                |  Transmit
 face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed
    lo:  100000     500    0    0    0     0          0         0   100000     500    0    0    0     0       0          0
  eth0: 2000000    3000    0   10    0     0          0         0  1000000    2500    0    0    0     0       0          0
`

func advance(t *testing.T, c *Collector, root, stat, netDev, cpu string) Health {
	t.Helper()
	if netDev != "" {
		if err := os.WriteFile(filepath.Join(root, "net", "dev"), []byte(netDev), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if cpu != "" {
		writeStat(t, stat, cpu)
	}
	c.lastAt = c.lastAt.Add(-time.Second)
	return c.collectMetrics()
}

func TestSkippedV1Warn(t *testing.T) {
	const skipMsg = "Metrics v1 sample skipped"
	t.Run("first sample after startup does not warn", func(t *testing.T) {
		logs := captureLogs(t)
		c, _, _ := newLogCollector(t)
		c.collectMetrics()
		if n := countMsg(logs(), slog.LevelWarn, skipMsg); n != 0 {
			t.Fatalf("warns = %d, want 0", n)
		}
	})
	t.Run("counter reset warns once while it persists", func(t *testing.T) {
		logs := captureLogs(t)
		c, root, stat := newLogCollector(t)
		c.collectMetrics()
		advance(t, c, root, stat, nextNetDev, "cpu 200 0 100 1600 20 0 40 40 0 0") // v1
		// cpu counters go backwards every interval
		advance(t, c, root, stat, "", "cpu 1 0 1 1 1 0 0 0 0 0")
		advance(t, c, root, stat, "", "cpu 0 0 0 0 0 0 0 0 0 0")
		if n := countMsg(logs(), slog.LevelWarn, skipMsg); n != 1 {
			t.Fatalf("warns = %d, want 1", n)
		}
		for _, r := range logs() {
			if strings.Contains(r.Msg, skipMsg) && r.Attrs["reason"] != "counter reset" {
				t.Fatalf("reason = %v", r.Attrs["reason"])
			}
		}
	})
	t.Run("persistent read failure from startup warns once", func(t *testing.T) {
		logs := captureLogs(t)
		c, root, _ := newLogCollector(t)
		if err := os.Remove(filepath.Join(root, "net", "dev")); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 5; i++ {
			c.collectMetrics()
		}
		recs := logs()
		if n := countMsg(recs, slog.LevelWarn, skipMsg); n != 1 {
			t.Fatalf("warns = %d, want 1", n)
		}
	})
	t.Run("read failure mid-run warns once, new outage after recovery warns again", func(t *testing.T) {
		logs := captureLogs(t)
		c, root, stat := newLogCollector(t)
		c.collectMetrics()
		advance(t, c, root, stat, nextNetDev, "cpu 200 0 100 1600 20 0 40 40 0 0") // v1 ok
		dev := filepath.Join(root, "net", "dev")
		if err := os.Remove(dev); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 3; i++ {
			c.collectMetrics()
		}
		if n := countMsg(logs(), slog.LevelWarn, skipMsg); n != 1 {
			t.Fatalf("outage 1 warns = %d, want 1", n)
		}
		// recover: restore file, one baseline sample, then a v1 sample
		advance(t, c, root, stat, sampleNetDev, "cpu 300 0 100 1600 20 0 40 40 0 0")
		h := advance(t, c, root, stat, nextNetDev, "cpu 400 0 100 1600 20 0 40 40 0 0")
		if h.MetricsVersion != 1 {
			t.Fatalf("did not recover: version %d", h.MetricsVersion)
		}
		if err := os.Remove(dev); err != nil {
			t.Fatal(err)
		}
		c.collectMetrics()
		if n := countMsg(logs(), slog.LevelWarn, skipMsg); n != 2 {
			t.Fatalf("outage 2 warns = %d, want 2", n)
		}
	})
	t.Run("no host NIC never emits the skip warning", func(t *testing.T) {
		logs := captureLogs(t)
		c, root, stat := newLogCollector(t)
		c.hostNIC = false
		c.collectMetrics()
		advance(t, c, root, stat, nextNetDev, "cpu 0 0 0 0 0 0 0 0 0 0")
		if n := countMsg(logs(), slog.LevelWarn, skipMsg); n != 0 {
			t.Fatalf("warns = %d, want 0", n)
		}
	})
}

func TestNewCollector_NoHostNICWarnsOnce(t *testing.T) {
	if hostProcPath() != "/proc" {
		t.Skip("host proc mounted here")
	}
	logs := captureLogs(t)
	c := NewCollector(nil, time.Hour, "", 0, 0, "")
	c.statPath = filepath.Join(t.TempDir(), "stat")
	writeStat(t, c.statPath, "cpu 100 0 50 800 10 0 20 20 0 0")
	for i := 0; i < 3; i++ {
		c.logCollected(c.collectMetrics())
	}
	recs := logs()
	if n := countMsg(recs, slog.LevelWarn, "Host NIC not visible"); n != 1 {
		t.Fatalf("warns = %d, want 1", n)
	}
	for _, r := range recs {
		if strings.Contains(r.Msg, "Host NIC not visible") {
			s := fmt.Sprint(r.Msg)
			if !strings.Contains(s, "/proc") || !strings.Contains(s, "version 0") {
				t.Fatalf("warn must name fallback and version 0: %q", s)
			}
		}
	}
}

func TestLogCollected_Fields(t *testing.T) {
	logs := captureLogs(t)
	c, root, stat := newLogCollector(t)
	c.collectMetrics()
	h := advance(t, c, root, stat, nextNetDev, "cpu 200 0 100 1600 20 0 40 40 0 0")
	c.logCollected(h)
	var rec *logRec
	for _, r := range logs() {
		if r.Msg == "Collected metrics" {
			r := r
			rec = &r
		}
	}
	if rec == nil {
		t.Fatal("no Collected metrics line")
	}
	for _, k := range []string{"cpu", "memory", "disk", "networkUpMbps", "networkDownMbps", // unchanged
		"metricsVersion", "cpuPercent", "stealPercent", "cpuCores", "hostNIC"} {
		if _, ok := rec.Attrs[k]; !ok {
			t.Errorf("missing field %q", k)
		}
	}
	if rec.Attrs["metricsVersion"] != int64(1) || rec.Attrs["hostNIC"] != true {
		t.Errorf("metricsVersion=%v hostNIC=%v", rec.Attrs["metricsVersion"], rec.Attrs["hostNIC"])
	}
	for _, k := range []string{"udpInDatagrams", "udpRcvbufErrors", "udpDropPct"} {
		if _, ok := rec.Attrs[k]; ok {
			t.Errorf("udp field %q present although k2s is absent / udp disabled", k)
		}
	}
}

// UDP failures must not break the Collected line or its other fields.
func TestLogCollected_UDPFieldsAndAbsentProcess(t *testing.T) {
	logs := captureLogs(t)
	c, _, _ := newLogCollector(t)
	p := newFakeProc(t) // no k2s
	c.udp.procDir = p.dir
	c.logCollected(Health{})
	p.proc(t, 42, "k2s", fmt.Sprintf(snmpStd, 1000, 10))
	c.logCollected(Health{}) // baseline
	p.proc(t, 42, "k2s", fmt.Sprintf(snmpStd, 2000, 30))
	c.logCollected(Health{})
	var lines []logRec
	for _, r := range logs() {
		if r.Msg == "Collected metrics" {
			lines = append(lines, r)
		}
	}
	if len(lines) != 3 {
		t.Fatalf("Collected metrics lines = %d, want 3", len(lines))
	}
	for i := 0; i < 2; i++ {
		if _, ok := lines[i].Attrs["udpDropPct"]; ok {
			t.Errorf("line %d must not carry udp fields", i)
		}
		if _, ok := lines[i].Attrs["metricsVersion"]; !ok {
			t.Errorf("line %d lost base fields", i)
		}
	}
	if lines[2].Attrs["udpInDatagrams"] != uint64(1000) || lines[2].Attrs["udpRcvbufErrors"] != uint64(20) {
		t.Fatalf("last line = %v", lines[2].Attrs)
	}
	if v, _ := lines[2].Attrs["udpDropPct"].(float64); !approx(v, 2) {
		t.Fatalf("udpDropPct = %v", lines[2].Attrs["udpDropPct"])
	}
}

// The Health JSON sent to Center must not gain keys from this change.
func TestHealthJSONKeysUnchanged(t *testing.T) {
	b, err := json.Marshal(Health{})
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(m))
	for k := range m {
		got = append(got, k)
	}
	want := []string{"cpuUsage", "memoryUsage", "diskUsage", "networkIn", "networkOut", "connections",
		"networkSpeedMbps", "bandwidthUpMbps", "bandwidthDownMbps", "networkLatencyMs", "packetLossPercent",
		"billingCycleEndAt", "monthlyTrafficLimitBytes", "usedTrafficBytes",
		"metricsVersion", "cpuPercent", "stealPercent", "cpuCores"}
	if !sameSet(got, want) {
		t.Fatalf("Health JSON keys changed:\n got  %v\n want %v", got, want)
	}
	if reflect.TypeOf(Health{}).NumField() != len(want) {
		t.Fatalf("Health has %d fields, want %d", reflect.TypeOf(Health{}).NumField(), len(want))
	}
}

func sameSet(a, b []string) bool {
	m := map[string]int{}
	for _, s := range a {
		m[s]++
	}
	for _, s := range b {
		m[s]--
	}
	for _, v := range m {
		if v != 0 {
			return false
		}
	}
	return true
}
