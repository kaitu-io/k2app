//go:build darwin

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/kaitu-io/k2/config"
)

const (
	serviceLabel = "kaitu"
	plistDir     = "/Library/LaunchDaemons"
)

func plistPath() string {
	return filepath.Join(plistDir, serviceLabel+".plist")
}

// plistContent generates the launchd plist XML for the daemon service.
// Primary logging is handled by Go (slog + lumberjack → /var/log/kaitu/k2.log).
// Stderr is redirected into k2.log at startup (config.SetupLogging).
func plistContent(label, exe string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>%s</string>
    <key>ProgramArguments</key>
    <array>
        <string>%s</string>
        <string>run</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>HardResourceLimits</key>
    <dict>
        <key>NumberOfProcesses</key>
        <integer>4000</integer>
        <key>NumberOfFiles</key>
        <integer>10240</integer>
    </dict>
    <key>SoftResourceLimits</key>
    <dict>
        <key>NumberOfProcesses</key>
        <integer>4000</integer>
        <key>NumberOfFiles</key>
        <integer>10240</integer>
    </dict>
    <key>ThrottleInterval</key>
    <integer>2</integer>
</dict>
</plist>
`, label, exe)
}

const (
	watchdogLabel = "kaitu-watchdog"
)

func watchdogPlistPath() string {
	return filepath.Join(plistDir, watchdogLabel+".plist")
}

// watchdogPlistContent generates the launchd plist for the external health check.
// Runs `k2 healthcheck` every 60s. Uses the same binary as the daemon.
// Stderr goes to a separate log file for debugging.
func watchdogPlistContent(label, exe string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>%s</string>
    <key>ProgramArguments</key>
    <array>
        <string>%s</string>
        <string>healthcheck</string>
    </array>
    <key>StartInterval</key>
    <integer>60</integer>
    <key>StandardErrorPath</key>
    <string>/var/log/kaitu/healthcheck.log</string>
</dict>
</plist>
`, label, exe)
}

func installService() error {
	// Diagnostic trail for the desktop shell: every step lands in
	// k2-service-install.log (uploaded with feedback logs). This is the only
	// place launchd/BTM state is observable as root — the daemon itself never
	// runs when launchd refuses the job, so k2.log stays silent (#3911).
	il := &installLog{}
	defer il.flush()
	il.logf("k2 service install starting: version=%s elevated=%v", version, config.IsRoot())

	exe, err := os.Executable()
	if err != nil {
		il.logf("FAIL find executable: %v", err)
		return fmt.Errorf("find executable: %w", err)
	}
	exe, err = filepath.Abs(exe)
	if err != nil {
		il.logf("FAIL resolve executable path: %v", err)
		return fmt.Errorf("resolve executable path: %w", err)
	}
	il.logf("exe=%s", exe)
	logBTMState(il)

	if err := installJob(il, serviceLabel, plistPath(), plistContent(serviceLabel, exe)); err != nil {
		return err
	}

	// Ensure log directory exists for watchdog stderr.
	os.MkdirAll("/var/log/kaitu", 0755)

	// Reset stale healthcheck state from previous install/reboot.
	os.Remove("/var/run/k2-healthcheck-failures")

	// Install companion watchdog — external health check every 60s.
	// Uses the same binary path resolved above. Non-fatal: daemon works without it.
	if err := installJob(il, watchdogLabel, watchdogPlistPath(), watchdogPlistContent(watchdogLabel, exe)); err != nil {
		fmt.Fprintf(os.Stderr, "warning: watchdog install failed: %v\n", err)
	}

	// Ground truth: launchd must actually be running the daemon. A loaded job
	// that keeps dying shows "spawn scheduled" + a non-zero "last exit code".
	if state := waitJobRunning(il, serviceLabel, 3*time.Second); state != "running" {
		il.logf("FAIL daemon not running after bootstrap: state=%q", state)
		return fmt.Errorf("daemon not running after install (launchd state %q, see k2-service-install.log)", state)
	}
	il.logf("daemon running: OK")
	return nil
}

// launchctl runs launchctl and returns its combined output and exit code
// (-1 when it could not be executed at all). Swappable for tests.
var launchctl = func(args ...string) (string, int) {
	out, err := exec.Command("launchctl", args...).CombinedOutput()
	code := 0
	if err != nil {
		code = -1
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		}
	}
	return strings.TrimSpace(string(out)), code
}

// installJob (re)registers one LaunchDaemon in the system domain.
//
// Uses bootout/bootstrap rather than the legacy unload/load: `launchctl load`
// exits 0 even when it fails ("Load failed: 5: Input/output error"), which made
// every install report success while launchd never ran the job. A job that is
// disabled — by `launchctl disable` or by the user turning the app off under
// System Settings → General → Login Items → Allow in the Background — fails
// exactly that way, so the disabled override is logged and cleared first: the
// user just authorised this install with their admin password.
func installJob(il *installLog, label, path, content string) error {
	target := "system/" + label
	out, code := launchctl("bootout", target)
	il.logf("bootout %s: exit=%d %s", target, code, out) // 3 = not loaded (fine)

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		il.logf("FAIL write plist %s: %v", path, err)
		return fmt.Errorf("write plist: %w", err)
	}

	disabledOut, _ := launchctl("print-disabled", "system")
	state := parseDisabledState(disabledOut, label)
	il.logf("disabled override %s: %s", label, state)
	if state == "disabled" {
		out, code := launchctl("enable", target)
		il.logf("enable %s: exit=%d %s", target, code, out)
	}

	// bootout can return before launchd has fully torn the job down; a
	// bootstrap in that window fails with 5 — retry briefly.
	for attempt := 1; ; attempt++ {
		out, code = launchctl("bootstrap", "system", path)
		il.logf("bootstrap %s attempt %d: exit=%d %s", label, attempt, code, out)
		if code == 0 {
			return nil
		}
		if attempt == 3 {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	disabledOut, _ = launchctl("print-disabled", "system")
	if parseDisabledState(disabledOut, label) == "disabled" {
		return fmt.Errorf("launchctl bootstrap %s: exit %d: %s (job is disabled — allow it under System Settings → General → Login Items → Allow in the Background)", label, code, out)
	}
	return fmt.Errorf("launchctl bootstrap %s: exit %d: %s", label, code, out)
}

// waitJobRunning polls `launchctl print` until the job is running or the
// timeout passes, then logs launchd's view of it. Returns the last state
// ("" when launchd does not know the job).
func waitJobRunning(il *installLog, label string, timeout time.Duration) string {
	deadline := time.Now().Add(timeout)
	var out string
	var info map[string]string
	for {
		out, _ = launchctl("print", "system/"+label)
		info = parseJobPrint(out)
		if info["state"] == "running" || time.Now().After(deadline) {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	if len(info) == 0 {
		il.logf("print system/%s: %s", label, out)
	} else {
		il.logf("print system/%s: state=%q pid=%q runs=%q last exit code=%q last terminating signal=%q",
			label, info["state"], info["pid"], info["runs"], info["last exit code"], info["last terminating signal"])
	}
	return info["state"]
}

// logBTMState records Background Task Management's verdict on our daemons
// (macOS 13+; needs root, which install has). Disposition "disallowed" means
// the user turned the app off under Login Items → Allow in the Background.
func logBTMState(il *installLog) {
	out, err := exec.Command("sfltool", "dumpbtm").CombinedOutput()
	if err != nil {
		il.logf("btm: sfltool dumpbtm unavailable: %v", err)
		return
	}
	entries := parseBTMEntries(string(out), plistDir+"/"+serviceLabel)
	if len(entries) == 0 {
		il.logf("btm: no entry for %s*", plistDir+"/"+serviceLabel)
	}
	for _, e := range entries {
		il.logf("btm: %s", e)
	}
}

// parseDisabledState extracts the override for label from `launchctl
// print-disabled` output: "enabled", "disabled", or "none" when there is no
// override. Older macOS prints `=> true` (disabled) / `=> false` instead.
func parseDisabledState(out, label string) string {
	key := `"` + label + `" => `
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, key) {
			continue
		}
		switch v := strings.TrimSpace(strings.TrimPrefix(line, key)); v {
		case "true":
			return "disabled"
		case "false":
			return "enabled"
		default:
			return v
		}
	}
	return "none"
}

// parseJobPrint returns the top-level `key = value` pairs of `launchctl print`
// output (one tab of indentation; nested dictionaries are skipped).
func parseJobPrint(out string) map[string]string {
	info := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		if !strings.HasPrefix(line, "\t") || strings.HasPrefix(line, "\t\t") {
			continue
		}
		k, v, ok := strings.Cut(strings.TrimSpace(line), " = ")
		if !ok || strings.HasSuffix(v, "{") {
			continue
		}
		if _, dup := info[k]; !dup {
			info[k] = v
		}
	}
	return info
}

// parseBTMEntries condenses each `sfltool dumpbtm` item whose URL starts with
// urlPrefix (plist path) to one line of its identifying fields.
func parseBTMEntries(out, urlPrefix string) []string {
	var entries []string
	for _, block := range splitBTMItems(out) {
		fields := map[string]string{}
		for _, line := range strings.Split(block, "\n") {
			k, v, ok := strings.Cut(strings.TrimSpace(line), ": ")
			if ok {
				fields[k] = strings.TrimSpace(v)
			}
		}
		if !strings.HasPrefix(fields["URL"], "file://"+urlPrefix) {
			continue
		}
		entries = append(entries, fmt.Sprintf("url=%s disposition=%s exe=%s developer=%s",
			fields["URL"], fields["Disposition"], fields["Executable Path"], fields["Parent Identifier"]))
	}
	return entries
}

// splitBTMItems splits dumpbtm output on its " #N:" item headers.
func splitBTMItems(out string) []string {
	var items []string
	var cur strings.Builder
	for _, line := range strings.Split(out, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "#") && strings.HasSuffix(t, ":") {
			items = append(items, cur.String())
			cur.Reset()
			continue
		}
		cur.WriteString(line)
		cur.WriteString("\n")
	}
	return append(items, cur.String())
}

func uninstallService() error {
	// Unload watchdog first (depends on daemon being installed).
	exec.Command("launchctl", "unload", watchdogPlistPath()).Run()
	os.Remove(watchdogPlistPath())

	// Unload daemon.
	exec.Command("launchctl", "unload", plistPath()).Run()
	os.Remove(plistPath())

	// Clean up healthcheck state.
	os.Remove("/var/run/k2-healthcheck-failures")

	return nil
}

func isServiceInstalled() bool {
	_, err := os.Stat(plistPath())
	return err == nil
}
