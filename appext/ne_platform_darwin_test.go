//go:build darwin

package appext

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/sagernet/sing/common/control"
)

// captureLogs redirects slog.Default to an in-memory text handler for the
// duration of the test and returns the buffer. Restored on cleanup.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	prev := slog.Default()
	buf := &bytes.Buffer{}
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return buf
}

// TestOnInterfaceChange_NilInterfaceNoPanic locks the fix for the ≤0.4.7 crash:
// sing-tun emits (nil, 0) on the no-route path, and the callback must not
// dereference it. nil is a valid state (no default route) — the engine is still
// notified so it can react.
func TestOnInterfaceChange_NilInterfaceNoPanic(t *testing.T) {
	buf := captureLogs(t)
	notified := false
	p := &nePlatform{notify: func() { notified = true }}

	p.onInterfaceChange(nil, 0) // must not panic

	if !notified {
		t.Fatal("notify must fire on nil interface (no-route is a valid state)")
	}
	s := buf.String()
	if !strings.Contains(s, "DIAG: ne-iface-resolved") || !strings.Contains(s, "result=nil") {
		t.Fatalf("expected nil-resolved DIAG line, got:\n%s", s)
	}
	if strings.Contains(s, "ne-callback-panic") {
		t.Fatalf("nil path must not reach the recover backstop, got:\n%s", s)
	}
}

// TestOnInterfaceChange_RecoversPanic locks the structural backstop: a panic
// anywhere in the callback (here, in the engine notify) must be recovered, not
// propagated — the sing-tun timer goroutine that invokes this has no recover of
// its own, so an escaping panic kills the whole Network Extension.
func TestOnInterfaceChange_RecoversPanic(t *testing.T) {
	buf := captureLogs(t)
	p := &nePlatform{notify: func() { panic("boom") }}

	// Must return normally rather than propagate the panic. If the recover is
	// removed, "boom" escapes here and fails the test.
	p.onInterfaceChange(&control.Interface{Name: "en0", Index: 1}, 0)

	s := buf.String()
	if !strings.Contains(s, "DIAG: ne-callback-panic") {
		t.Fatalf("expected ne-callback-panic DIAG line, got:\n%s", s)
	}
	if !strings.Contains(s, "boom") {
		t.Fatalf("recover must record the panic value, got:\n%s", s)
	}
}

// TestOnInterfaceChange_PhysicalInterface is the happy path: a real uplink is
// logged with its identity and the engine is notified.
func TestOnInterfaceChange_PhysicalInterface(t *testing.T) {
	buf := captureLogs(t)
	notified := false
	p := &nePlatform{notify: func() { notified = true }}

	p.onInterfaceChange(&control.Interface{Name: "en0", Index: 3}, 0)

	if !notified {
		t.Fatal("notify must fire on a resolved interface")
	}
	s := buf.String()
	if !strings.Contains(s, "DIAG: ne-iface-resolved") || !strings.Contains(s, "name=en0") || !strings.Contains(s, "isTun=false") {
		t.Fatalf("expected resolved en0 DIAG line, got:\n%s", s)
	}
}

// TestOnInterfaceChange_NilNotify guards the other nil: Start may register the
// callback with a nil engine notify; the callback must tolerate that.
func TestOnInterfaceChange_NilNotify(t *testing.T) {
	captureLogs(t)
	p := &nePlatform{notify: nil}
	p.onInterfaceChange(&control.Interface{Name: "en0", Index: 1}, 0) // must not panic
}
