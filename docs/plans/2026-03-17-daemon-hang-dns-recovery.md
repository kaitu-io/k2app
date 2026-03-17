# Daemon Hang DNS Recovery — External Watchdog Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** When k2 daemon deadlocks/hangs, automatically restore system DNS and exit cleanly instead of leaving the system with broken DNS pointing at a dead process.

**Architecture:** External watchdog (Unix philosophy: separate monitoring from service). A launchd timer runs `k2 healthcheck` every 60s. If the daemon is alive but unresponsive for 2 consecutive checks (120s), the healthcheck process cleans up DNS and kills the hung daemon. launchd `KeepAlive` auto-restarts it. Additionally, fix the macOS panic recovery TODO (one-line bug fix).

**Tech Stack:** Go stdlib (`net/http`, `os`, `syscall`), `provider.CleanupStaleDNSOverride()` (existing), launchd plist (macOS)

---

## Why External, Not Internal

| Concern | Internal watchdog | External healthcheck |
|---------|-------------------|---------------------|
| Deadlock affects it? | Can't be if atomic-only, but CGo cleanup CAN hang | **No — fresh process, independent CGo context** |
| Industry precedent | Custom | systemd WatchdogSec, launchd, Kubernetes liveness probe |
| Code in daemon | +100 lines, arm/disarm lifecycle | **Zero** |
| DNS cleanup tool | `CleanupStaleDNSOverride()` (CGo) | Same function, but in **fresh process** — no deadlock risk |
| Platform integration | N/A | launchd (macOS), systemd timer (Linux), Scheduled Task (Windows) |

## Root Cause & Defense Layers

```
Root cause (separate plan):  Fix lock ordering in healthMonitor (QUIC deadlock plan)
Layer 1 (this plan):         Panic → restoreNetworkPlatform() → CleanupStaleDNSOverride ✓
Layer 2 (this plan):         Hang → external healthcheck (120s) → cleanup + kill → restart
Layer 3 (existing):          Restart → CleanupStaleDNSOverride() at daemon startup ✓
```

## Scope

- **macOS: full implementation** (primary desktop platform, DNS persistence issue)
- **Linux: not needed** (sing-tun uses resolvectl, DNS auto-restores when TUN disappears)
- **Windows: follow-up** (SCM restart + startup cleanup covers crashes; hang detection deferred)

---

## Task 1: macOS DNS panic recovery (bug fix)

Split `recovery_unix.go` into darwin and linux files. Darwin calls `provider.CleanupStaleDNSOverride()` — matching the Windows behavior that already exists in `recovery_windows.go`.

**Files:**
- Delete: `k2/daemon/recovery_unix.go`
- Create: `k2/daemon/recovery_darwin.go`
- Create: `k2/daemon/recovery_linux.go`

- [ ] **Step 1: Create `recovery_darwin.go`**

```go
//go:build darwin && !ios

package daemon

import (
	"log/slog"

	"github.com/kaitu-io/k2/provider"
)

// restoreNetworkPlatform attempts macOS-specific network restoration after a panic.
// Removes stale Setup:/ DNS override via SCDynamicStore so the system falls back
// to DHCP-assigned DNS. Uses the persisted service ID from /var/run/k2-dns-service.
func restoreNetworkPlatform() {
	slog.Info("recovery: attempting macOS DNS restoration after panic")
	provider.CleanupStaleDNSOverride()
}
```

- [ ] **Step 2: Create `recovery_linux.go`**

```go
//go:build linux && !android

package daemon

import "log/slog"

// restoreNetworkPlatform attempts Linux-specific network restoration after a panic.
// On Linux, sing-tun uses resolvectl which auto-restores when the TUN interface
// is removed by the kernel on process exit.
func restoreNetworkPlatform() {
	slog.Info("recovery: Linux DNS handled by resolvectl on interface removal")
}
```

- [ ] **Step 3: Delete `recovery_unix.go`**

```bash
cd k2 && git rm daemon/recovery_unix.go
```

- [ ] **Step 4: Verify build on all desktop platforms**

```bash
cd k2 && GOOS=darwin GOARCH=amd64 go build ./daemon/... && GOOS=linux GOARCH=amd64 go build ./daemon/... && GOOS=windows GOARCH=amd64 go build ./daemon/...
```
Expected: all three clean builds.

- [ ] **Step 5: Run existing daemon tests**

```bash
cd k2 && go test ./daemon/... -short -count=1
```
Expected: all pass.

- [ ] **Step 6: Commit**

```bash
cd k2
git add daemon/recovery_darwin.go daemon/recovery_linux.go
git rm daemon/recovery_unix.go
git commit -m "fix(daemon): implement macOS DNS recovery on panic

restoreNetworkPlatform() was a TODO on Unix. Now calls
provider.CleanupStaleDNSOverride() on macOS (matching existing
Windows behavior). Split recovery_unix.go into darwin/linux
platform files with correct build tags."
```

---

## Task 2: `k2 healthcheck` subcommand

Cross-platform health check logic: ping daemon, track consecutive failures, cleanup + kill after threshold. Designed as a separate process invocation — immune to daemon's internal deadlocks.

**Files:**
- Create: `k2/cmd/k2/healthcheck.go`
- Create: `k2/cmd/k2/healthcheck_unix.go`
- Create: `k2/cmd/k2/healthcheck_windows.go`
- Create: `k2/cmd/k2/healthcheck_test.go`
- Modify: `k2/cmd/k2/main.go` — add `healthcheck` case to switch

### Design

```
k2 healthcheck
  ├─ HTTP GET 127.0.0.1:1777/ping (5s timeout)
  ├─ 200 OK → reset failure counter, exit 0
  └─ error →
       ├─ read PID file → process not alive → reset counter, exit 0
       │   (daemon crashed, launchd will restart — nothing for us to do)
       └─ process alive but unresponsive → increment counter
            ├─ counter < 2 → exit 0 (wait for next check)
            └─ counter ≥ 2 → CleanupStaleDNSOverride() + SIGKILL → exit 0
                              (launchd restarts daemon, new daemon runs startup cleanup)
```

- [ ] **Step 1: Write tests**

```go
package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestEvaluateHealth_Healthy(t *testing.T) {
	action := evaluateHealth(true, true, 0, 2)
	if action != actionNone {
		t.Fatalf("expected actionNone, got %d", action)
	}
}

func TestEvaluateHealth_NotRunning(t *testing.T) {
	// Ping failed but process not alive → daemon just isn't running.
	action := evaluateHealth(false, false, 5, 2)
	if action != actionNone {
		t.Fatalf("expected actionNone for dead process, got %d", action)
	}
}

func TestEvaluateHealth_UnresponsiveBelowThreshold(t *testing.T) {
	action := evaluateHealth(false, true, 0, 2)
	if action != actionWait {
		t.Fatalf("expected actionWait, got %d", action)
	}
}

func TestEvaluateHealth_UnresponsiveAtThreshold(t *testing.T) {
	action := evaluateHealth(false, true, 1, 2)
	if action != actionCleanup {
		t.Fatalf("expected actionCleanup at threshold, got %d", action)
	}
}

func TestEvaluateHealth_UnresponsiveAboveThreshold(t *testing.T) {
	action := evaluateHealth(false, true, 5, 2)
	if action != actionCleanup {
		t.Fatalf("expected actionCleanup above threshold, got %d", action)
	}
}

func TestTryPing_HealthyServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"code":0,"message":"pong"}`))
	}))
	defer srv.Close()

	if !tryPing(srv.Listener.Addr().String()) {
		t.Fatal("expected ping to succeed")
	}
}

func TestTryPing_UnresponsiveServer(t *testing.T) {
	// No server on this address → connection refused.
	if tryPing("127.0.0.1:0") {
		t.Fatal("expected ping to fail on closed port")
	}
}

func TestFailureCount_ReadWriteReset(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "failures")

	// Initially zero.
	if n := readFailureCount(path); n != 0 {
		t.Fatalf("expected 0, got %d", n)
	}

	// Write and read back.
	writeFailureCount(path, 3)
	if n := readFailureCount(path); n != 3 {
		t.Fatalf("expected 3, got %d", n)
	}

	// Reset.
	resetFailureCount(path)
	if n := readFailureCount(path); n != 0 {
		t.Fatalf("expected 0 after reset, got %d", n)
	}
}

func TestFailureCount_StaleFileIgnored(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "failures")

	// Write a failure count, then backdate the file.
	writeFailureCount(path, 5)
	staleTime := time.Now().Add(-10 * time.Minute)
	os.Chtimes(path, staleTime, staleTime)

	// Stale file should be ignored (returns 0) and removed.
	if n := readFailureCount(path); n != 0 {
		t.Fatalf("expected 0 for stale file, got %d", n)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("expected stale file to be removed")
	}
}

func TestReadPIDFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.pid")

	// Missing file → 0.
	if pid := readPIDFile(path); pid != 0 {
		t.Fatalf("expected 0 for missing file, got %d", pid)
	}

	// Valid PID.
	os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())), 0644)
	if pid := readPIDFile(path); pid != os.Getpid() {
		t.Fatalf("expected %d, got %d", os.Getpid(), pid)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd k2 && go test ./cmd/k2/ -run "TestEvaluateHealth|TestTryPing|TestFailureCount|TestReadPID" -v -count=1
```
Expected: FAIL — undefined functions.

- [ ] **Step 3: Write `healthcheck.go`**

```go
package main

import (
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/kaitu-io/k2/daemon"
	"github.com/kaitu-io/k2/provider"
)

const (
	healthcheckPingTimeout     = 5 * time.Second
	healthcheckMaxFailures     = 2 // kill after 2 consecutive failures (120s at 60s interval)
	healthcheckFailureFilePath = "/var/run/k2-healthcheck-failures"
)

// healthAction describes the outcome of health evaluation.
type healthAction int

const (
	actionNone    healthAction = iota // daemon healthy or not running — no action needed
	actionWait                        // unresponsive but below failure threshold
	actionCleanup                     // threshold exceeded — cleanup DNS + kill process
)

// evaluateHealth is a pure function that decides what action to take.
// pingOK: daemon HTTP responded within timeout.
// processAlive: daemon PID exists and is running.
// consecutiveFailures: current failure count (before this check).
// maxFailures: threshold for cleanup action.
func evaluateHealth(pingOK bool, processAlive bool, consecutiveFailures int, maxFailures int) healthAction {
	if pingOK {
		return actionNone
	}
	if !processAlive {
		return actionNone // daemon not running, launchd handles restart
	}
	// Process alive but not responding — potential hang.
	if consecutiveFailures+1 >= maxFailures {
		return actionCleanup
	}
	return actionWait
}

// tryPing sends an HTTP GET to the daemon /ping endpoint.
// Returns true if the daemon responds with 200 OK within timeout.
func tryPing(addr string) bool {
	client := &http.Client{Timeout: healthcheckPingTimeout}
	resp, err := client.Get("http://" + addr + "/ping")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// readPIDFile reads a PID from the given file. Returns 0 if the file
// doesn't exist, is empty, or contains invalid data.
func readPIDFile(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return 0
	}
	return pid
}

// Failure count persistence — simple file with an integer.
// Stale protection: if the file is older than 5 minutes, treat as zero.
// This prevents false positives after macOS reboot (/var/run/ may persist).

func readFailureCount(path string) int {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	// Stale counter protection: if file is older than 5 minutes, ignore it.
	// A legitimate failure sequence has checks every 60s, so the file should
	// always be younger than ~2 minutes.
	if time.Since(info.ModTime()) > 5*time.Minute {
		os.Remove(path)
		return 0
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimSpace(string(data)))
	return n
}

func writeFailureCount(path string, n int) {
	os.WriteFile(path, []byte(strconv.Itoa(n)), 0644)
}

func resetFailureCount(path string) {
	os.Remove(path)
}

// cmdHealthcheck is the entry point for `k2 healthcheck`.
// Designed to be run by launchd every 60s. Silent on success (cron convention).
func cmdHealthcheck() {
	addr := daemon.DefaultAddr
	pidPath := defaultPIDPath()
	failurePath := healthcheckFailureFilePath

	pingOK := tryPing(addr)
	pid := readPIDFile(pidPath)
	processAlive := pid > 0 && isProcessAlive(pid)
	failures := readFailureCount(failurePath)

	action := evaluateHealth(pingOK, processAlive, failures, healthcheckMaxFailures)

	switch action {
	case actionNone:
		resetFailureCount(failurePath)
	case actionWait:
		writeFailureCount(failurePath, failures+1)
		fmt.Fprintf(os.Stderr, "healthcheck: daemon unresponsive (%d/%d)\n",
			failures+1, healthcheckMaxFailures)
	case actionCleanup:
		fmt.Fprintf(os.Stderr, "healthcheck: daemon unresponsive (%d consecutive), forcing cleanup\n",
			failures+1)
		provider.CleanupStaleDNSOverride()
		if pid > 0 {
			killProcess(pid)
		}
		resetFailureCount(failurePath)
	}
}
```

- [ ] **Step 4: Write `healthcheck_unix.go`**

```go
//go:build !windows

package main

import "syscall"

// isProcessAlive checks if a process with the given PID exists.
// Uses kill(pid, 0) which checks without sending a signal.
func isProcessAlive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}

// killProcess sends SIGKILL to the given PID.
func killProcess(pid int) {
	syscall.Kill(pid, syscall.SIGKILL)
}
```

- [ ] **Step 5: Write `healthcheck_windows.go`**

```go
//go:build windows

package main

// isProcessAlive is a stub on Windows. External health check not yet implemented.
// Windows follow-up: use OpenProcess + CloseHandle.
func isProcessAlive(pid int) bool { return false }

// killProcess is a stub on Windows.
// Windows follow-up: use TerminateProcess.
func killProcess(pid int) {}
```

- [ ] **Step 6: Add `healthcheck` case to `main.go`**

In `main.go`, add to the switch (after `android-install` case, before `demo-config`):

```go
	case "healthcheck":
		cmdHealthcheck()
```

Note: do NOT add to `printUsage()` — this is an internal command for the service manager.

- [ ] **Step 7: Run tests**

```bash
cd k2 && go test ./cmd/k2/ -run "TestEvaluateHealth|TestTryPing|TestFailureCount|TestReadPID" -v -count=1 -count=1
```
Expected: all tests PASS.

- [ ] **Step 8: Run with race detector**

```bash
cd k2 && go test -race ./cmd/k2/ -run "TestEvaluateHealth|TestTryPing|TestFailureCount|TestReadPID" -v -count=1
```
Expected: PASS, no race conditions.

- [ ] **Step 9: Verify build**

```bash
cd k2 && GOOS=darwin go build ./cmd/k2/ && GOOS=linux go build ./cmd/k2/ && GOOS=windows go build ./cmd/k2/
```
Expected: clean builds on all platforms.

- [ ] **Step 10: Commit**

```bash
cd k2
git add cmd/k2/healthcheck.go cmd/k2/healthcheck_unix.go cmd/k2/healthcheck_windows.go cmd/k2/healthcheck_test.go cmd/k2/main.go
git commit -m "feat(cli): add k2 healthcheck subcommand for external watchdog

Cross-platform health check: pings daemon HTTP API, tracks consecutive
failures, and after 2 failures (120s) forces DNS cleanup + SIGKILL.
Designed to run as a separate process via launchd/systemd timer — immune
to daemon deadlocks. Pure logic in evaluateHealth() is fully unit tested."
```

---

## Task 3: macOS launchd watchdog integration

Install a companion launchd plist that runs `k2 healthcheck` every 60s. Integrated into existing `installService()` / `uninstallService()` lifecycle.

**Files:**
- Modify: `k2/daemon/service_darwin.go`

- [ ] **Step 1: Add watchdog plist template and paths**

Add after `plistContent()`:

```go
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
```

- [ ] **Step 2: Install watchdog in `installService()`**

Add after the existing `launchctl load` call (after line 78):

```go
	// Ensure log directory exists for watchdog stderr.
	os.MkdirAll("/var/log/kaitu", 0755)

	// Reset stale healthcheck state from previous install/reboot.
	os.Remove("/var/run/k2-healthcheck-failures")

	// Install companion watchdog — external health check every 60s.
	// Uses the same binary path resolved above.
	exec.Command("launchctl", "unload", watchdogPlistPath()).Run()
	if err := os.WriteFile(watchdogPlistPath(), []byte(watchdogPlistContent(watchdogLabel, exe)), 0644); err != nil {
		return fmt.Errorf("write watchdog plist: %w", err)
	}
	if err := exec.Command("launchctl", "load", watchdogPlistPath()).Run(); err != nil {
		// Non-fatal: daemon works without watchdog.
		fmt.Fprintf(os.Stderr, "warning: watchdog install failed: %v\n", err)
	}
```

- [ ] **Step 3: Uninstall watchdog in `uninstallService()`**

Replace the function:

```go
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
```

- [ ] **Step 4: Verify build**

```bash
cd k2 && GOOS=darwin go build ./daemon/...
```
Expected: clean build.

- [ ] **Step 5: Verify existing daemon tests**

```bash
cd k2 && go test ./daemon/... -short -count=1
```
Expected: all pass.

- [ ] **Step 6: Commit**

```bash
cd k2
git add daemon/service_darwin.go
git commit -m "feat(daemon): install launchd watchdog alongside daemon service

installService() now also installs kaitu-watchdog.plist which runs
'k2 healthcheck' every 60s. If daemon is unresponsive for 120s
(2 consecutive failures), healthcheck cleans up DNS and kills the
hung process. launchd KeepAlive auto-restarts the daemon.

uninstallService() removes both plists and healthcheck state file."
```

---

## Task 4: Update documentation

**Files:**
- Modify: `k2/daemon/CLAUDE.md`
- Modify: `k2/cmd/k2/CLAUDE.md`

- [ ] **Step 1: Add to daemon/CLAUDE.md Files section**

After the existing `recovery.go` entries:
```
- `service_darwin.go` now also installs/uninstalls `kaitu-watchdog.plist` (external health check timer)
```

Add new section after "Concurrency Model":
```
## External Watchdog (macOS)

launchd companion service (`kaitu-watchdog.plist`) runs `k2 healthcheck` every 60s.
Detects daemon hangs by pinging `/ping` with 5s timeout. After 2 consecutive
failures (120s), forces DNS cleanup + SIGKILL. launchd KeepAlive restarts daemon.

Why external (not internal watchdog):
- Fresh process — immune to daemon deadlocks (no shared mutexes or CGo state)
- DNS cleanup via fresh CGo context (SCDynamicStore calls go through configd, not the hung process)
- Follows systemd WatchdogSec / Kubernetes liveness probe pattern

Installed/uninstalled alongside daemon service by `k2 service install/uninstall`.

Linux: not needed (resolvectl auto-restores DNS when TUN disappears).
Windows: follow-up (SCM restart + startup cleanup covers crashes).
```

- [ ] **Step 2: Add to cmd/k2/CLAUDE.md Subcommands section**

After `android-install` entry:
```
  healthcheck               External health check (internal, used by launchd)
```

Add to Files section:
```
- `healthcheck.go` — `k2 healthcheck`: external watchdog for launchd (ping daemon, failure counting, DNS cleanup + kill)
- `healthcheck_unix.go` / `healthcheck_windows.go` — Platform process management (isProcessAlive, killProcess)
- `healthcheck_test.go` — Unit tests for health evaluation logic
```

- [ ] **Step 3: Commit**

```bash
cd k2
git add daemon/CLAUDE.md cmd/k2/CLAUDE.md
git commit -m "docs: document external watchdog in daemon and CLI CLAUDE.md"
```

---

## Verification Checklist

After all tasks:

```bash
# Full build (all desktop platforms)
cd k2
GOOS=darwin GOARCH=amd64 go build ./... 2>&1 | head -5
GOOS=linux GOARCH=amd64 go build ./... 2>&1 | head -5
GOOS=windows GOARCH=amd64 go build ./... 2>&1 | head -5

# All tests
go test ./daemon/... -v -count=1
go test ./cmd/k2/ -v -count=1

# Race detector
go test -race ./daemon/... -count=1
go test -race ./cmd/k2/ -run "TestEvaluateHealth|TestTryPing|TestFailureCount|TestReadPID" -count=1

# Full project short tests
go test -short ./...

# Vet all platforms
GOOS=darwin go vet ./...
GOOS=linux go vet ./...
GOOS=windows go vet ./...
```

## Manual Verification (macOS)

```bash
# 1. Install service (creates both plists)
sudo k2 service install

# 2. Verify both plists exist
ls -la /Library/LaunchDaemons/kaitu*.plist
# Expected: kaitu.plist + kaitu-watchdog.plist

# 3. Verify watchdog is running
launchctl list | grep kaitu-watchdog
# Expected: shows PID and label

# 4. Test healthcheck manually (daemon running)
k2 healthcheck
echo $?
# Expected: exit 0, no output (daemon healthy)

# 5. Test healthcheck manually (daemon stopped)
sudo launchctl unload /Library/LaunchDaemons/kaitu.plist
k2 healthcheck
echo $?
# Expected: exit 0, no output (not running = no action)

# 6. Simulate hang: create a long-running process and fake the PID file
sleep 9999 &
FAKE_PID=$!
echo $FAKE_PID > /var/run/k2.pid   # PID of a real running process
k2 healthcheck                      # first failure
cat /var/run/k2-healthcheck-failures
# Expected: "1"
k2 healthcheck                      # second failure → cleanup + kill
# Expected: stderr message about cleanup, failure file removed, sleep process killed
kill $FAKE_PID 2>/dev/null          # cleanup if not already killed

# 7. Uninstall service
sudo k2 service uninstall
ls /Library/LaunchDaemons/kaitu*.plist
# Expected: both gone
```

## Edge Cases

| Scenario | Behavior | Why correct |
|----------|----------|-------------|
| Daemon healthy | Ping 200 → reset counter | No action needed |
| Daemon not running (crashed) | Ping fails, PID file gone → `actionNone` | launchd KeepAlive restarts; startup runs CleanupStaleDNSOverride |
| Daemon hung (deadlocked) | Ping timeout, process alive → count up | After 2 checks (120s) → cleanup + SIGKILL |
| Daemon slow startup | Ping fails briefly, 120s threshold absorbs it | No false positive |
| Binary being upgraded | Old process gone, PID file stale → `actionNone` | New process starts, writes new PID |
| Rapid restart loop | CleanupStaleDNSOverride idempotent | No harm from repeated cleanup |
| Healthcheck itself hangs | curl 5s timeout + scutil <1s + kill <1s | Max runtime ~6s per check |
| Watchdog without daemon plist | healthcheck exits early (no process) | No side effects |
| Multiple k2 processes | PID file identifies the daemon specifically | Only the daemon PID is killed |
| CGo hang in hung daemon | Fresh healthcheck process, independent CGo | SCDynamicStore calls go through configd |
| Reboot with stale counter file | readFailureCount ignores files >5min old | Prevents false positive on first post-reboot check |
| installService resets counter | os.Remove in installService | Clean slate after service reinstall |

## Platform Follow-ups

**Linux (systemd):** Add `WatchdogSec=120` + `Type=notify` to unit template. Send `sd_notify("WATCHDOG=1")` in broadcastSnapshot, `sd_notify("READY=1")` after HTTP listen. Low effort but changes startup semantics — separate PR.

**Windows:** Add Scheduled Task running `k2 healthcheck` every 60s. `healthcheck_windows.go` needs real `isProcessAlive` (OpenProcess) and `killProcess` (TerminateProcess). DNS cleanup already works at startup. Lower priority since root cause (deadlock) is fixed separately.
