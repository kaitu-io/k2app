//go:build darwin

package main

import (
	"os"
	"strings"
	"testing"
	"time"
)

// TestInstallJobE2E drives the real launchctl as root against a throwaway
// label, starting from the #3911 state: job registered but disabled, where
// the legacy `launchctl load` exited 0 and launchd never ran it.
//
//	go test -c -o /tmp/k2.test ./cmd/k2 && sudo K2_LAUNCHD_E2E=1 /tmp/k2.test -test.run E2E -test.v
func TestInstallJobE2E(t *testing.T) {
	if os.Getenv("K2_LAUNCHD_E2E") == "" || os.Geteuid() != 0 {
		t.Skip("set K2_LAUNCHD_E2E=1 and run as root")
	}
	const label = "io.k2diag.e2e"
	path := plistDir + "/" + label + ".plist"
	script := "/usr/local/bin/k2diag-e2e.sh"
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexec sleep 3600\n"), 0755); err != nil {
		t.Fatal(err)
	}
	plist := strings.Replace(plistContent(label, script), "<string>run</string>", "", 1)
	t.Cleanup(func() {
		launchctl("bootout", "system/"+label)
		launchctl("enable", "system/"+label)
		os.Remove(path)
		os.Remove(script)
	})

	// Reproduce: a disabled job, which the legacy path "installed" silently.
	os.WriteFile(path, []byte(plist), 0644)
	launchctl("bootout", "system/"+label)
	launchctl("disable", "system/"+label)
	out, code := launchctl("load", path)
	t.Logf("legacy load on disabled job: exit=%d %q", code, out)
	if st := waitJobRunning(&installLog{}, label, time.Second); st == "running" {
		t.Fatal("precondition: disabled job should not run")
	}

	il := &installLog{}
	if err := installJob(il, label, path, plist); err != nil {
		t.Fatalf("installJob: %v\n%s", err, il.b.String())
	}
	if st := waitJobRunning(il, label, 3*time.Second); st != "running" {
		t.Fatalf("state=%q\n%s", st, il.b.String())
	}
	// Reinstall over a running job (the normal upgrade path).
	if err := installJob(il, label, path, plist); err != nil {
		t.Fatalf("reinstall: %v\n%s", err, il.b.String())
	}
	if st := waitJobRunning(il, label, 3*time.Second); st != "running" {
		t.Fatalf("after reinstall state=%q\n%s", st, il.b.String())
	}
	t.Logf("install log:\n%s", il.b.String())
}
