package provider

import (
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"
)

// TUN-create diagnostics (DIAG: wintun-create-fail).
//
// Why this exists: on Windows, tun.New → WintunCreateAdapter can fail with
// "The device is not ready for use." (ticket #3873). Inside wintun.dll that
// message means: the device node was created, WaitForInterface waited its
// hard-coded 15s for the NDIS interface, gave up, and wintun translated the
// device's DEVPKEY_Device_ProblemStatus into ERROR_NOT_READY. The *reason*
// (problem code + NTSTATUS) goes only to wintun's own logger, which sing-tun
// never registers — and the DLL is memory-loaded inside sing-tun's internal
// package, so we cannot register one either. Worse, on that failure path
// wintun closes the SwDevice, which removes the device: by the time tun.New
// returns, there is nothing left to inspect.
//
// So the Windows sampler (tun_create_diag_windows.go) polls the Wintun
// devices *while* tun.New runs, and on failure this recorder reports the last
// state it saw for our adapter plus every other Wintun adapter on the box
// (another VPN's adapter is a common conflict). Success logs nothing.
//
// This file is untagged so the selection/formatting logic unit-tests on every
// OS; only the device enumeration is Windows-specific.

// tunCreateSampleInterval: wintun's failure path holds the device for 15s, so
// 500ms gives ~30 looks at it; one enumeration costs a few ms.
const tunCreateSampleInterval = 500 * time.Millisecond

// runTUNCreateDiag samples enumerate every interval until the returned stop
// func is called with tun.New's result. On error it takes one last sample
// (covers a failure faster than the first tick) and logs
// DIAG: wintun-create-fail; on success it logs nothing. extra supplies
// platform fields (osBuild) and is only called on failure.
func runTUNCreateDiag(ours string, enumerate func() ([]wintunDevice, error), interval time.Duration, extra func() []any) func(error) {
	start := time.Now()
	rec := newTunCreateRecorder(ours)
	sample := func() {
		devs, err := enumerate()
		rec.record(time.Since(start).Milliseconds(), devs, err)
	}

	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				sample()
			}
		}
	}()

	return func(createErr error) {
		close(done)
		wg.Wait()
		if createErr == nil {
			return
		}
		elapsed := time.Since(start).Milliseconds()
		sample()
		attrs := []any{"elapsedMs", elapsed, "err", createErr.Error()}
		if extra != nil {
			attrs = append(attrs, extra()...)
		}
		attrs = append(attrs, rec.attrs()...)
		slog.Warn("DIAG: wintun-create-fail", attrs...)
	}
}

// wintunDevice is one network device under a Wintun enumerator.
type wintunDevice struct {
	Name          string // DEVPKEY_Wintun_Name: the name passed to WintunCreateAdapter ("k2tun" is ours)
	InstanceID    string
	Present       bool   // CM_Get_DevNode_Status succeeded (false = phantom / already removed)
	Status        uint32 // DN_* flags
	ProblemCode   uint32 // CM_PROB_*; what Device Manager shows as "code N"
	ProblemStatus uint32 // NTSTATUS from DEVPKEY_Device_ProblemStatus; 0 = unset
}

// maxOtherWintun caps the otherWintun field; phantom adapters left behind by
// uninstalled VPNs can number in the dozens.
const maxOtherWintun = 8

type tunCreateRecorder struct {
	ours string

	mu        sync.Mutex
	samples   int
	sampleErr string // first enumeration error, if any
	lastOurs  *wintunDevice
	oursAtMs  int64
	others    map[string]wintunDevice // by InstanceID, latest state
}

func newTunCreateRecorder(ours string) *tunCreateRecorder {
	return &tunCreateRecorder{ours: ours, others: map[string]wintunDevice{}}
}

// record folds one sample in. Our adapter keeps the *last* state seen rather
// than the latest sample's, because wintun deletes the device on failure —
// the final samples normally no longer contain it.
func (r *tunCreateRecorder) record(atMs int64, devs []wintunDevice, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.samples++
	if err != nil && r.sampleErr == "" {
		r.sampleErr = err.Error()
	}
	for _, d := range devs {
		if strings.EqualFold(d.Name, r.ours) {
			r.lastOurs, r.oursAtMs = &d, atMs
		} else {
			r.others[d.InstanceID] = d
		}
	}
}

// recorderSnapshot is a lock-free copy of the recorder for formatting.
type recorderSnapshot struct {
	samples   int
	sampleErr string
	lastOurs  *wintunDevice
	oursAtMs  int64
	others    []wintunDevice // sorted by InstanceID
}

func (r *tunCreateRecorder) snapshot() recorderSnapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := recorderSnapshot{samples: r.samples, sampleErr: r.sampleErr, lastOurs: r.lastOurs, oursAtMs: r.oursAtMs}
	for _, d := range r.others {
		s.others = append(s.others, d)
	}
	sort.Slice(s.others, func(i, j int) bool { return s.others[i].InstanceID < s.others[j].InstanceID })
	return s
}

// attrs returns the slog key/value pairs for DIAG: wintun-create-fail.
func (r *tunCreateRecorder) attrs() []any {
	s := r.snapshot()

	adapter := "not-seen" // device never appeared: failure was before device creation (driver install / SwDeviceCreate)
	if s.lastOurs != nil {
		adapter = fmt.Sprintf("%s seenAtMs=%d", formatWintunDevice(*s.lastOurs), s.oursAtMs)
	}

	var others []string
	for i, d := range s.others {
		if i == maxOtherWintun {
			others = append(others, fmt.Sprintf("+%d more", len(s.others)-maxOtherWintun))
			break
		}
		others = append(others, formatWintunDevice(d))
	}
	otherStr := "none"
	if len(others) > 0 {
		otherStr = strings.Join(others, "; ")
	}

	out := []any{"adapter", adapter, "otherWintun", otherStr, "samples", s.samples}
	if s.sampleErr != "" {
		out = append(out, "sampleErr", s.sampleErr)
	}
	return out
}

func formatWintunDevice(d wintunDevice) string {
	name := d.Name
	if name == "" {
		name = "?"
	}
	if !d.Present {
		return fmt.Sprintf("%s present=false id=%s", name, d.InstanceID)
	}
	return fmt.Sprintf("%s present=true status=0x%X problem=%s problemStatus=0x%08X id=%s",
		name, d.Status, cmProblemString(d.ProblemCode), d.ProblemStatus, d.InstanceID)
}

// cmProblemString renders a CM_PROB_* code as "N(NAME)". The number is what
// the user sees in Device Manager ("This device cannot start. (Code 10)"),
// so it is always printed; the name covers the codes a failing virtual NIC
// driver realistically produces.
func cmProblemString(code uint32) string {
	if code == 0 {
		return "0"
	}
	name, ok := map[uint32]string{
		1:  "NOT_CONFIGURED",
		10: "FAILED_START",
		18: "REINSTALL",
		22: "DISABLED",
		24: "DEVICE_NOT_THERE",
		28: "FAILED_INSTALL",
		31: "FAILED_ADD",
		37: "FAILED_DRIVER_ENTRY",
		39: "DRIVER_FAILED_LOAD",
		43: "FAILED_POST_START",
		48: "DRIVER_BLOCKED",
		52: "UNSIGNED_DRIVER",
	}[code]
	if !ok {
		return fmt.Sprintf("%d", code)
	}
	return fmt.Sprintf("%d(%s)", code, name)
}
