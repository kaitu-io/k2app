//go:build linux

package main

import (
	"errors"
	"os"
	"os/signal"
	"syscall"
	"testing"
	"time"
)

// Go's default action for SIGTERM is to exit on the spot — no deferred
// function, no cleanup. `/etc/init.d/k2r stop` therefore left `table inet
// k2r` behind (OpenWrt 25.12, 2026-09-30).
func TestStopOnSignal(t *testing.T) {
	for _, sig := range []syscall.Signal{syscall.SIGTERM, syscall.SIGINT} {
		t.Run(sig.String(), func(t *testing.T) {
			if signal.Ignored(sig) {
				t.Skipf("%v is ignored in this process (background job)", sig)
			}
			t.Cleanup(func() { signal.Reset(syscall.SIGTERM, syscall.SIGINT) })
			stopped, gaveUp := make(chan struct{}, 1), make(chan struct{}, 1)
			stopOnSignal(func() { stopped <- struct{}{} }, func() { gaveUp <- struct{}{} })

			if err := syscall.Kill(os.Getpid(), sig); err != nil {
				t.Fatal(err)
			}
			select {
			case <-stopped:
			case <-time.After(2 * time.Second):
				t.Fatalf("%v did not stop the gateway", sig)
			}
			select {
			case <-gaveUp:
				t.Fatal("gave up after a single signal")
			case <-time.After(50 * time.Millisecond):
			}

			// The stop is stuck and the operator asks again.
			if err := syscall.Kill(os.Getpid(), sig); err != nil {
				t.Fatal(err)
			}
			select {
			case <-gaveUp:
			case <-time.After(2 * time.Second):
				t.Fatalf("a second %v did not force the exit", sig)
			}
		})
	}
}

// `k2r service uninstall` on OpenWrt 25.12 (2026-09-30) removed the init
// script and left `table inet k2r` plus the fwmark rules: LAN DNS dead with
// no k2r left on the box to clean it up.
func TestUninstallThenClearRules(t *testing.T) {
	var order []string
	err := uninstallThenClearRules(
		func() error { order = append(order, "uninstall"); return nil },
		func() { order = append(order, "clear") },
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(order) != 2 || order[0] != "uninstall" || order[1] != "clear" {
		t.Errorf("order = %v, want the service gone before the rules are cleared", order)
	}
}

func TestUninstallThenClearRules_ClearsEvenWhenUninstallFails(t *testing.T) {
	boom := errors.New("service manager said no")
	cleared := false
	err := uninstallThenClearRules(func() error { return boom }, func() { cleared = true })
	if !errors.Is(err, boom) {
		t.Errorf("err = %v, want the uninstall error", err)
	}
	if !cleared {
		t.Error("rules left in place because the service removal failed")
	}
}
