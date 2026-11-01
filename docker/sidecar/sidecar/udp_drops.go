package sidecar

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// udp_drops.go: LOG-ONLY visibility of UDP receive-buffer drops. They exist
// only in the k2s container's netns (/proc/<k2s pid>/net/snmp); pid 1's snmp
// (the host netns used for NIC metrics) shows none. Nothing here feeds Health,
// pressure or metering, and no failure here may affect reporting.

// k2sComm is the kernel comm of the k2s process: entrypoint.sh `exec ./k2s`,
// so comm is the binary basename "k2s" (docker/k2s/Dockerfile).
const k2sComm = "k2s"

type udpCounters struct{ in, rcvbuf uint64 }

// parseUDPSnmp reads the "Udp:" header + value line pair and picks columns by
// NAME (the kernel has appended columns before). UdpLite: lines do not match.
func parseUDPSnmp(data string) (udpCounters, error) {
	var header, values []string
	for _, line := range strings.Split(data, "\n") {
		f := strings.Fields(line)
		if len(f) == 0 || f[0] != "Udp:" {
			continue
		}
		if header == nil {
			header = f
		} else {
			values = f
			break
		}
	}
	if header == nil || values == nil {
		return udpCounters{}, errors.New("no Udp header/value lines")
	}
	if len(values) != len(header) {
		return udpCounters{}, errors.New("Udp header/value column count mismatch")
	}
	col := func(name string) (uint64, error) {
		for i, h := range header {
			if h == name {
				return strconv.ParseUint(values[i], 10, 64)
			}
		}
		return 0, fmt.Errorf("column %s missing", name)
	}
	in, err := col("InDatagrams")
	if err != nil {
		return udpCounters{}, err
	}
	rb, err := col("RcvbufErrors")
	if err != nil {
		return udpCounters{}, err
	}
	return udpCounters{in: in, rcvbuf: rb}, nil
}

// udpWatcher tracks k2s UDP counters across intervals. Not goroutine-safe;
// only the collector loop uses it.
type udpWatcher struct {
	procDir string // dir holding <pid>/ entries (/host/proc); "" disables

	pid     int // cached; rescanned only when reading fails
	base    udpCounters
	basePid int
	hasBase bool
	state   string // last warned failure state; "" = healthy
}

func (w *udpWatcher) fail(state, msg string, err error) {
	w.hasBase = false
	if w.state == state {
		return
	}
	w.state = state
	slog.Warn(msg, "component", "metrics", "err", err)
}

// findK2sPID scans numeric dirs for comm == k2s (exact). 0 = not found.
func (w *udpWatcher) findK2sPID() int {
	entries, err := os.ReadDir(w.procDir)
	if err != nil {
		return 0
	}
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid <= 0 {
			continue
		}
		if commOf(w.procDir, pid) == k2sComm {
			return pid
		}
	}
	return 0
}

func commOf(procDir string, pid int) string {
	b, err := os.ReadFile(filepath.Join(procDir, strconv.Itoa(pid), "comm"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// read verifies the cached pid is still k2s (guards pid reuse) and parses snmp.
func (w *udpWatcher) read(pid int) (udpCounters, error) {
	if commOf(w.procDir, pid) != k2sComm {
		return udpCounters{}, fs.ErrNotExist
	}
	b, err := os.ReadFile(filepath.Join(w.procDir, strconv.Itoa(pid), "net", "snmp"))
	if err != nil {
		return udpCounters{}, err
	}
	return parseUDPSnmp(string(b))
}

// sample returns log attrs for the interval, or nil when unavailable (first
// sample, restart, counter fall, or any failure). It never returns an error.
func (w *udpWatcher) sample() []any {
	if w.procDir == "" {
		return nil
	}
	var cur udpCounters
	var err error
	if w.pid != 0 {
		cur, err = w.read(w.pid)
	} else {
		err = fs.ErrNotExist
	}
	if err != nil { // (re)scan only now that the cached pid failed
		w.pid = w.findK2sPID()
		if w.pid == 0 {
			w.fail("absent", "k2s process not found under host proc; UDP drop stats unavailable", err)
			return nil
		}
		cur, err = w.read(w.pid)
	}
	if err != nil {
		state, msg := "unreadable", "cannot read k2s net/snmp; UDP drop stats unavailable"
		if errors.Is(err, fs.ErrPermission) {
			state, msg = "denied", "no permission to read k2s net/snmp; UDP drop stats unavailable"
		}
		w.pid = 0
		w.fail(state, msg, err)
		return nil
	}
	w.state = ""

	prev, prevPid, had := w.base, w.basePid, w.hasBase
	w.base, w.basePid, w.hasBase = cur, w.pid, true
	if !had || prevPid != w.pid || cur.in < prev.in || cur.rcvbuf < prev.rcvbuf {
		return nil // first sample / k2s restarted / counters fell: re-baselined
	}
	din, drb := cur.in-prev.in, cur.rcvbuf-prev.rcvbuf
	var pct float64
	if din > 0 {
		pct = float64(drb) / float64(din) * 100
	}
	return []any{"udpInDatagrams", din, "udpRcvbufErrors", drb, "udpDropPct", pct}
}
