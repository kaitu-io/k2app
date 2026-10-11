//go:build darwin

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// When launchd refuses the daemon, k2.log stays empty and the install log is
// the only remote evidence (#3911). Fixtures are real output captured on
// macOS 26: a running job, a job that exits 3 on every spawn, an override
// table with both states, and BTM items incl. one turned off under Login Items.

func readFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestParseDisabledState(t *testing.T) {
	out := readFixture(t, "launchctl-print-disabled.txt")
	for label, want := range map[string]string{
		"kaitu":                    "enabled",
		"homebrew.mxcl.postgresql": "disabled",
		"kaitu-watchdog":           "none",
		"kait":                     "none", // no prefix match
	} {
		if got := parseDisabledState(out, label); got != want {
			t.Errorf("%s: got %q want %q", label, got, want)
		}
	}
	legacy := "\tdisabled services = {\n\t\t\"kaitu\" => true\n\t\t\"other\" => false\n\t}\n"
	if got := parseDisabledState(legacy, "kaitu"); got != "disabled" {
		t.Errorf("legacy true: got %q", got)
	}
	if got := parseDisabledState(legacy, "other"); got != "enabled" {
		t.Errorf("legacy false: got %q", got)
	}
}

func TestParseJobPrint(t *testing.T) {
	run := parseJobPrint(readFixture(t, "launchctl-print-running.txt"))
	if run["state"] != "running" || run["pid"] == "" {
		t.Errorf("running job: state=%q pid=%q", run["state"], run["pid"])
	}
	crash := parseJobPrint(readFixture(t, "launchctl-print-crashing.txt"))
	if crash["state"] != "spawn scheduled" || crash["last exit code"] != "3" {
		t.Errorf("crashing job: state=%q last exit code=%q", crash["state"], crash["last exit code"])
	}
	// Nested dictionaries carry their own "state = active" lines — must not
	// shadow the top-level state.
	if strings.Count(readFixture(t, "launchctl-print-crashing.txt"), "state = ") < 2 {
		t.Fatal("fixture no longer exercises nested state lines")
	}
	nestedFirst := "system/kaitu = {\n\tendpoints = {\n\t\tstate = active\n\t}\n\tstate = not running\n}\n"
	if got := parseJobPrint(nestedFirst)["state"]; got != "not running" {
		t.Errorf("nested state leaked to top level: %q", got)
	}
	if len(parseJobPrint(`Could not find service "kaitu" in domain for system`)) != 0 {
		t.Error("unknown job must parse to empty")
	}
}

func TestParseBTMEntries(t *testing.T) {
	out := readFixture(t, "sfltool-dumpbtm.txt")
	got := parseBTMEntries(out, "/Library/LaunchDaemons/kaitu")
	if len(got) != 2 {
		t.Fatalf("want daemon + watchdog, got %d: %q", len(got), got)
	}
	joined := strings.Join(got, "\n")
	for _, want := range []string{"kaitu.plist", "kaitu-watchdog.plist", "disposition=[enabled, allowed, notified]", "ALL NATION CONNECT"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in %q", want, joined)
		}
	}
	off := parseBTMEntries(out, "/Library/LaunchDaemons/homebrew.mxcl.postgresql")
	if len(off) != 1 || !strings.Contains(off[0], "disallowed") {
		t.Errorf("disallowed item: %q", off)
	}
}

// fakeLaunchctl scripts launchctl replies by subcommand and records calls.
type fakeLaunchctl struct {
	disabled   string // print-disabled output
	bootstraps []int  // exit codes for successive bootstrap calls
	calls      []string
}

func (f *fakeLaunchctl) run(args ...string) (string, int) {
	f.calls = append(f.calls, strings.Join(args, " "))
	switch args[0] {
	case "print-disabled":
		return f.disabled, 0
	case "enable":
		f.disabled = strings.ReplaceAll(f.disabled, "=> disabled", "=> enabled")
		return "", 0
	case "bootstrap":
		code := f.bootstraps[0]
		if len(f.bootstraps) > 1 {
			f.bootstraps = f.bootstraps[1:]
		}
		if code != 0 {
			return "Bootstrap failed: 5: Input/output error", code
		}
		return "", 0
	case "bootout":
		return "Boot-out failed: 3: No such process", 3
	}
	return "", 0
}

func withFakeLaunchctl(t *testing.T, f *fakeLaunchctl) {
	t.Helper()
	orig := launchctl
	launchctl = f.run
	t.Cleanup(func() { launchctl = orig })
}

func indexOf(calls []string, prefix string) int {
	for i, c := range calls {
		if strings.HasPrefix(c, prefix) {
			return i
		}
	}
	return -1
}

func TestInstallJobClearsDisabledOverrideBeforeBootstrap(t *testing.T) {
	f := &fakeLaunchctl{disabled: "\t\t\"kaitu\" => disabled\n", bootstraps: []int{0}}
	withFakeLaunchctl(t, f)
	il := &installLog{}
	if err := installJob(il, "kaitu", filepath.Join(t.TempDir(), "kaitu.plist"), "<plist/>"); err != nil {
		t.Fatal(err)
	}
	en, bs := indexOf(f.calls, "enable system/kaitu"), indexOf(f.calls, "bootstrap system")
	if en < 0 || bs < 0 || en > bs {
		t.Fatalf("want enable before bootstrap, calls=%q", f.calls)
	}
	if !strings.Contains(il.b.String(), "disabled override kaitu: disabled") {
		t.Errorf("override not logged:\n%s", il.b.String())
	}
}

func TestInstallJobLeavesEnabledJobAlone(t *testing.T) {
	f := &fakeLaunchctl{disabled: "\t\t\"kaitu\" => enabled\n", bootstraps: []int{0}}
	withFakeLaunchctl(t, f)
	if err := installJob(&installLog{}, "kaitu", filepath.Join(t.TempDir(), "kaitu.plist"), "<plist/>"); err != nil {
		t.Fatal(err)
	}
	if indexOf(f.calls, "enable") >= 0 {
		t.Errorf("enable called on an enabled job: %q", f.calls)
	}
}

func TestInstallJobRetriesTransientBootstrapFailure(t *testing.T) {
	f := &fakeLaunchctl{bootstraps: []int{5, 0}}
	withFakeLaunchctl(t, f)
	if err := installJob(&installLog{}, "kaitu", filepath.Join(t.TempDir(), "kaitu.plist"), "<plist/>"); err != nil {
		t.Fatalf("second attempt succeeded, want nil: %v", err)
	}
}

// The regression that hid #3911: a bootstrap that keeps failing must fail the
// install (legacy `launchctl load` exited 0 here), and still-disabled jobs
// must name the Login Items switch.
func TestInstallJobReportsPersistentFailure(t *testing.T) {
	f := &fakeLaunchctl{bootstraps: []int{5}}
	withFakeLaunchctl(t, f)
	err := installJob(&installLog{}, "kaitu", filepath.Join(t.TempDir(), "kaitu.plist"), "<plist/>")
	if err == nil || !strings.Contains(err.Error(), "exit 5") {
		t.Fatalf("want exit-5 error, got %v", err)
	}
	if strings.Contains(err.Error(), "Login Items") {
		t.Errorf("job not disabled, hint must not appear: %v", err)
	}

	// enable did not stick (e.g. BTM re-disabled it) → hint.
	f = &fakeLaunchctl{disabled: "\t\t\"kaitu\" => disabled\n", bootstraps: []int{5}}
	withFakeLaunchctl(t, f)
	f.disabled = "\t\t\"kaitu\" => disabled\n"
	stuck := func(args ...string) (string, int) {
		if args[0] == "enable" { // swallow: override stays disabled
			f.calls = append(f.calls, "enable")
			return "", 0
		}
		return f.run(args...)
	}
	launchctl = stuck
	err = installJob(&installLog{}, "kaitu", filepath.Join(t.TempDir(), "kaitu.plist"), "<plist/>")
	if err == nil || !strings.Contains(err.Error(), "Login Items") {
		t.Fatalf("want Login Items hint, got %v", err)
	}
}

func TestWaitJobRunningLogsLaunchdView(t *testing.T) {
	crash := readFixture(t, "launchctl-print-crashing.txt")
	orig := launchctl
	launchctl = func(args ...string) (string, int) { return crash, 0 }
	t.Cleanup(func() { launchctl = orig })

	il := &installLog{}
	if st := waitJobRunning(il, "kaitu", 0); st != "spawn scheduled" {
		t.Fatalf("state=%q", st)
	}
	if !strings.Contains(il.b.String(), `last exit code="3"`) {
		t.Errorf("exit code not logged:\n%s", il.b.String())
	}

	launchctl = func(args ...string) (string, int) {
		return "Bad request.\nCould not find service \"kaitu\" in domain for system", 113
	}
	il = &installLog{}
	if st := waitJobRunning(il, "kaitu", 0); st != "" {
		t.Fatalf("unknown job state=%q", st)
	}
	if !strings.Contains(il.b.String(), "Could not find service") {
		t.Errorf("raw launchctl reply not logged:\n%s", il.b.String())
	}
}
