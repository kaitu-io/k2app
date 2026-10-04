package provider

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

// captureSlog swaps the default logger for a text handler into a buffer.
func captureSlog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

func attrMap(kv []any) map[string]any {
	m := map[string]any{}
	for i := 0; i+1 < len(kv); i += 2 {
		m[kv[i].(string)] = kv[i+1]
	}
	return m
}

var failingK2tun = wintunDevice{
	Name: "k2tun", InstanceID: `SWD\Wintun\{A}`, Present: true,
	Status: 0x01802400, ProblemCode: 10, ProblemStatus: 0xC00000A3,
}

// The failure this exists for: wintun removes the device before tun.New
// returns, so the final samples no longer contain it. The report must carry
// the last state seen, not "not-seen".
func TestTunCreateRecorder_KeepsLastSeenAdapterAfterItDisappears(t *testing.T) {
	r := newTunCreateRecorder("k2tun")
	r.record(500, []wintunDevice{failingK2tun}, nil)
	r.record(14500, []wintunDevice{failingK2tun}, nil)
	r.record(15100, nil, nil) // device gone

	got := attrMap(r.attrs())
	adapter := got["adapter"].(string)
	for _, want := range []string{"k2tun present=true", "problem=10(FAILED_START)", "problemStatus=0xC00000A3", "seenAtMs=14500"} {
		if !strings.Contains(adapter, want) {
			t.Errorf("adapter = %q, missing %q", adapter, want)
		}
	}
	if got["samples"] != 3 {
		t.Errorf("samples = %v, want 3", got["samples"])
	}
	if got["otherWintun"] != "none" {
		t.Errorf("otherWintun = %v, want none", got["otherWintun"])
	}
}

func TestTunCreateRecorder_NotSeenAndOthers(t *testing.T) {
	r := newTunCreateRecorder("k2tun")
	r.record(500, []wintunDevice{
		{Name: "wg0", InstanceID: `SWD\Wintun\{B}`, Present: true, Status: 0x0180200A},
		{Name: "", InstanceID: `SWD\Wintun\{C}`, Present: false},
	}, errors.New("ROOT\\Wintun: boom"))
	r.record(1000, nil, errors.New("second error"))

	got := attrMap(r.attrs())
	if got["adapter"] != "not-seen" {
		t.Errorf("adapter = %v, want not-seen", got["adapter"])
	}
	other := got["otherWintun"].(string)
	if !strings.Contains(other, "wg0 present=true status=0x180200A problem=0") ||
		!strings.Contains(other, `? present=false id=SWD\Wintun\{C}`) {
		t.Errorf("otherWintun = %q", other)
	}
	if got["sampleErr"] != "ROOT\\Wintun: boom" {
		t.Errorf("sampleErr = %v, want the first error only", got["sampleErr"])
	}
}

func TestTunCreateRecorder_OurNameIsCaseInsensitiveAndOthersCapped(t *testing.T) {
	r := newTunCreateRecorder("k2tun")
	var devs []wintunDevice
	for i := 0; i < maxOtherWintun+3; i++ {
		devs = append(devs, wintunDevice{Name: fmt.Sprintf("o%02d", i), InstanceID: fmt.Sprintf("id%02d", i)})
	}
	devs = append(devs, wintunDevice{Name: "K2TUN", InstanceID: "ours", Present: true})
	r.record(1, devs, nil)

	got := attrMap(r.attrs())
	if !strings.HasPrefix(got["adapter"].(string), "K2TUN present=true") {
		t.Errorf("adapter = %v", got["adapter"])
	}
	other := got["otherWintun"].(string)
	if strings.Contains(other, "K2TUN") {
		t.Errorf("our adapter leaked into otherWintun: %q", other)
	}
	if !strings.HasSuffix(other, "; +3 more") || strings.Count(other, ";") != maxOtherWintun {
		t.Errorf("otherWintun not capped at %d: %q", maxOtherWintun, other)
	}
}

func TestCMProblemString(t *testing.T) {
	for code, want := range map[uint32]string{0: "0", 10: "10(FAILED_START)", 52: "52(UNSIGNED_DRIVER)", 99: "99"} {
		if got := cmProblemString(code); got != want {
			t.Errorf("cmProblemString(%d) = %q, want %q", code, got, want)
		}
	}
}

// fakeEnumerator returns the failing adapter for the first `visible` calls,
// then nothing — the shape of wintun's 15s-wait-then-delete failure.
type fakeEnumerator struct {
	mu      sync.Mutex
	calls   int
	visible int
}

func (f *fakeEnumerator) enumerate() ([]wintunDevice, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.calls <= f.visible {
		return []wintunDevice{failingK2tun}, nil
	}
	return nil, nil
}

func (f *fakeEnumerator) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func TestRunTUNCreateDiag_FailureLogsLastSeenState(t *testing.T) {
	buf := captureSlog(t)
	f := &fakeEnumerator{visible: 3}
	stop := runTUNCreateDiag("k2tun", f.enumerate, 5*time.Millisecond, func() []any { return []any{"osBuild", "10.0.19045"} })

	deadline := time.Now().Add(5 * time.Second)
	for f.count() < 5 && time.Now().Before(deadline) { // past the point the device vanished
		time.Sleep(2 * time.Millisecond)
	}
	stop(errors.New("The device is not ready for use."))

	out := buf.String()
	for _, want := range []string{
		`msg="DIAG: wintun-create-fail"`,
		`level=WARN`,
		`err="The device is not ready for use."`,
		`osBuild=10.0.19045`,
		`problem=10(FAILED_START)`,
		`problemStatus=0xC00000A3`,
		`otherWintun=none`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("log missing %q:\n%s", want, out)
		}
	}
	if strings.Count(out, "DIAG: wintun-create-fail") != 1 {
		t.Errorf("want exactly one event:\n%s", out)
	}
}

func TestRunTUNCreateDiag_SuccessLogsNothing(t *testing.T) {
	buf := captureSlog(t)
	f := &fakeEnumerator{visible: 100}
	stop := runTUNCreateDiag("k2tun", f.enumerate, time.Millisecond, func() []any {
		t.Error("extra must not be called on success")
		return nil
	})
	time.Sleep(10 * time.Millisecond)
	stop(nil)
	if buf.Len() != 0 {
		t.Errorf("success must log nothing, got:\n%s", buf.String())
	}
	n := f.count()
	time.Sleep(10 * time.Millisecond)
	if f.count() != n {
		t.Error("sampler kept running after stop")
	}
}

// A failure faster than the first tick still gets one sample (the final one).
func TestRunTUNCreateDiag_FastFailureStillSamples(t *testing.T) {
	buf := captureSlog(t)
	f := &fakeEnumerator{visible: 100}
	stop := runTUNCreateDiag("k2tun", f.enumerate, time.Hour, nil)
	stop(errors.New("boom"))
	if f.count() != 1 {
		t.Errorf("enumerate calls = %d, want 1 (final sample)", f.count())
	}
	if !strings.Contains(buf.String(), "samples=1") || !strings.Contains(buf.String(), "problem=10(FAILED_START)") {
		t.Errorf("log:\n%s", buf.String())
	}
}
