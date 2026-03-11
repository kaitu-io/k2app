# CLI Redesign Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Rewrite k2/k2s CLI to pure subcommand architecture with `--json` output, `k2 connect` direct mode, and non-interactive `k2s setup`.

**Architecture:** Replace top-level flag dispatch in both binaries with subcommand registry. Add output layer for human/JSON switching. Add `k2 connect` that runs engine directly without daemon.

**Tech Stack:** Go stdlib `flag.NewFlagSet`, `encoding/json`, existing engine/config/daemon packages.

---

### Task 1: Output Layer

Foundation for all commands. Provides human vs JSON output switching.

**Files:**
- Create: `cmd/k2/output.go`
- Create: `cmd/k2s/output.go` (identical copy)
- Test: `cmd/k2/output_test.go`

**Step 1: Write test for output layer**

```go
// cmd/k2/output_test.go
package main

import (
	"bytes"
	"testing"
)

func TestOutputHuman(t *testing.T) {
	var buf bytes.Buffer
	o := &output{w: &buf}
	o.printResult("Connected")
	if got := buf.String(); got != "Connected\n" {
		t.Errorf("got %q", got)
	}
}

func TestOutputJSON(t *testing.T) {
	var buf bytes.Buffer
	o := &output{w: &buf, json: true}
	o.ok(map[string]any{"state": "connected"})
	want := `{"ok":true,"data":{"state":"connected"}}` + "\n"
	if got := buf.String(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestOutputJSONError(t *testing.T) {
	var buf bytes.Buffer
	o := &output{w: &buf, json: true}
	o.fail("DAEMON_NOT_RUNNING", "daemon not running", "run: k2 setup <URL>")
	want := `{"ok":false,"error":"daemon not running","code":"DAEMON_NOT_RUNNING","hint":"run: k2 setup <URL>"}` + "\n"
	if got := buf.String(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./cmd/k2/ -run TestOutput -v`
Expected: FAIL — `output` type not defined

**Step 3: Write output.go**

```go
// cmd/k2/output.go
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// output handles human vs JSON output for all commands.
type output struct {
	w    io.Writer // default os.Stdout
	json bool
}

var out = &output{w: os.Stdout}

// printResult prints a human-readable message (ignored in --json mode).
func (o *output) printResult(msg string) {
	if o.json {
		return
	}
	fmt.Fprintln(o.w, msg)
}

// printf prints a formatted human-readable message (ignored in --json mode).
func (o *output) printf(format string, args ...any) {
	if o.json {
		return
	}
	fmt.Fprintf(o.w, format, args...)
}

// ok outputs a successful JSON result.
func (o *output) ok(data any) {
	if !o.json {
		return
	}
	json.NewEncoder(o.w).Encode(map[string]any{"ok": true, "data": data})
}

// fail outputs a JSON error.
func (o *output) fail(code, message, hint string) {
	if !o.json {
		fmt.Fprintf(os.Stderr, "Error: %s\n", message)
		if hint != "" {
			fmt.Fprintf(os.Stderr, "  %s\n", hint)
		}
		return
	}
	json.NewEncoder(o.w).Encode(map[string]any{
		"ok": false, "error": message, "code": code, "hint": hint,
	})
}

// event outputs a JSON event line (for streaming commands like connect).
func (o *output) event(data map[string]any) {
	json.NewEncoder(o.w).Encode(data)
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./cmd/k2/ -run TestOutput -v`
Expected: PASS

**Step 5: Copy output.go to k2s**

```bash
cp cmd/k2/output.go cmd/k2s/output.go
```

Change package name is not needed — both are `package main`.

**Step 6: Commit**

```bash
git add cmd/k2/output.go cmd/k2/output_test.go cmd/k2s/output.go
git commit -m "feat(cli): add output layer for human/JSON switching"
```

---

### Task 2: k2 Subcommand Dispatch

Replace top-level flag dispatch with pure subcommand model.

**Files:**
- Rewrite: `cmd/k2/main.go`
- Modify: `cmd/k2/ctl.go` — extract `ctlUp`/`ctlDown`/`ctlStatus` as top-level commands
- Modify: `cmd/k2/service.go` — add `--help` support
- Modify: `cmd/k2/process.go` — rename `runDaemon` → `cmdRun` wrapper
- Modify: `cmd/k2/svc_windows.go` — update `startDaemon` to accept args
- Keep: `cmd/k2/svc_other.go`

**Step 1: Write new main.go**

Rewrite `cmd/k2/main.go`:

```go
package main

import (
	_ "embed"
	"fmt"
	"os"
	"strings"
)

//go:embed client.demo.yml
var demoConfig string

var (
	version = "dev"
	commit  = "none"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	// Parse global flags before subcommand.
	args := os.Args[1:]
	for len(args) > 0 && strings.HasPrefix(args[0], "--") {
		switch args[0] {
		case "--json":
			out.json = true
			args = args[1:]
		case "--quiet":
			args = args[1:]
		case "--help", "-h":
			printUsage()
			return
		default:
			fmt.Fprintf(os.Stderr, "Unknown flag: %s\nRun 'k2 help' for usage.\n", args[0])
			os.Exit(1)
		}
	}

	if len(args) == 0 {
		printUsage()
		os.Exit(1)
	}

	cmd := args[0]
	rest := args[1:]

	switch cmd {
	case "connect":
		os.Exit(cmdConnect(rest))
	case "up":
		os.Exit(cmdUp(rest))
	case "down":
		os.Exit(cmdDown(rest))
	case "status":
		os.Exit(cmdStatus(rest))
	case "setup":
		cmdSetup(rest)
	case "run":
		cmdRun(rest)
	case "config":
		os.Exit(cmdConfig(rest))
	case "service":
		cmdService(rest)
	case "upgrade":
		cmdUpgrade(rest)
	case "version":
		fmt.Printf("k2 %s (%s)\n", version, commit)
	case "help", "--help", "-h":
		if len(rest) > 0 {
			printCommandHelp(rest[0])
		} else {
			printUsage()
		}
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\nRun 'k2 help' for usage.\n", cmd)
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `k2 — Kaitu network tunnel

Quick start:
  k2 connect <URL>          Connect (foreground, no setup needed)
  k2 setup <URL>            Install as system service + connect

Commands:
  connect   Connect to server (foreground)
  up        Connect via background daemon
  down      Disconnect
  status    Connection status

  setup     First-time setup                          [root]
  run       Start daemon (service entry point)        [root for TUN]

  config    Configuration (test, show)
  service   System service (install, uninstall)       [root]
  upgrade   Self-update
  version   Version info

Flags:
  --json    Machine-readable JSON output
  --quiet   Minimal output
  --help    Help for any command
`)
}

func printCommandHelp(cmd string) {
	// Trigger each command's --help by passing it through.
	switch cmd {
	case "connect":
		cmdConnect([]string{"--help"})
	case "up":
		cmdUp([]string{"--help"})
	case "setup":
		cmdSetup([]string{"--help"})
	case "config":
		cmdConfig([]string{"--help"})
	case "service":
		cmdService([]string{"--help"})
	default:
		fmt.Fprintf(os.Stderr, "No help for %q. Run 'k2 help' for command list.\n", cmd)
	}
}
```

**Step 2: Rewrite ctl.go → promote to top-level**

Rename `cmd/k2/ctl.go` content. Keep IPC logic, change function signatures to return exit codes:

```go
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/kaitu-io/k2/config"
	"github.com/kaitu-io/k2/daemon"
)

func cmdUp(args []string) int {
	fs := flag.NewFlagSet("k2 up", flag.ExitOnError)
	pid := fs.Int("pid", 0, "parent PID to monitor (auto-disconnect on exit)")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, `Connect via background daemon.

Usage: k2 up [URL|config.yaml] [flags]

  No args: reconnect with saved config.
  URL:     connect with k2v5:// URL.
  File:    connect with YAML config file.

Flags:
`)
		fs.PrintDefaults()
	}
	fs.Parse(args)

	params := map[string]any{}
	if fs.NArg() > 0 {
		arg := fs.Arg(0)
		var cfg *config.ClientConfig
		if strings.Contains(arg, "://") {
			cfg = config.ClientFromURL(arg)
		} else {
			absPath, err := filepath.Abs(arg)
			if err != nil {
				out.fail("INVALID_ARG", err.Error(), "")
				return 1
			}
			var loadErr error
			cfg, loadErr = config.LoadClient(absPath)
			if loadErr != nil {
				out.fail("INVALID_CONFIG", loadErr.Error(), "")
				return 1
			}
		}
		params["config"] = cfg
	}
	if *pid > 0 {
		params["pid"] = *pid
	}

	client := daemon.NewClient(daemon.DefaultAddr)
	resp, err := client.Do("up", params)
	if err != nil {
		out.fail("DAEMON_NOT_RUNNING", "daemon not running", daemonNotRunningHint())
		return 4
	}
	if resp.Code != 0 {
		out.fail("CONNECT_FAILED", resp.Message, "")
		return 2
	}

	out.printResult("Connecting...")

	for i := 0; i < 30; i++ {
		time.Sleep(1 * time.Second)
		resp, err := client.Do("status", nil)
		if err != nil || resp.Code != 0 {
			continue
		}
		data, _ := json.Marshal(resp.Data)
		var status struct {
			State string `json:"state"`
			Error string `json:"error"`
		}
		json.Unmarshal(data, &status)

		switch status.State {
		case "connected":
			out.printResult("Connected")
			out.ok(resp.Data)
			return 0
		case "stopped":
			msg := "connection stopped"
			if status.Error != "" {
				msg = status.Error
			}
			out.fail("CONNECT_FAILED", msg, "")
			return 2
		}
	}

	out.printResult("Still connecting, check `k2 status`")
	return 0
}

func cmdDown(args []string) int {
	fs := flag.NewFlagSet("k2 down", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Disconnect tunnel.\n\nUsage: k2 down\n")
	}
	fs.Parse(args)

	client := daemon.NewClient(daemon.DefaultAddr)
	resp, err := client.Do("down", nil)
	if err != nil {
		out.fail("DAEMON_NOT_RUNNING", "daemon not running", daemonNotRunningHint())
		return 4
	}
	if resp.Code != 0 {
		out.fail("DISCONNECT_FAILED", resp.Message, "")
		return 2
	}
	out.printResult("Disconnected")
	out.ok(map[string]string{"state": "disconnected"})
	return 0
}

func cmdStatus(args []string) int {
	fs := flag.NewFlagSet("k2 status", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Show connection status.\n\nUsage: k2 status\n")
	}
	fs.Parse(args)

	client := daemon.NewClient(daemon.DefaultAddr)
	resp, err := client.Do("status", nil)
	if err != nil {
		out.fail("DAEMON_NOT_RUNNING", "daemon not running", daemonNotRunningHint())
		return 4
	}
	if resp.Code != 0 {
		out.fail("STATUS_FAILED", resp.Message, "")
		return 2
	}

	if out.json {
		out.ok(resp.Data)
	} else {
		data, _ := json.MarshalIndent(resp.Data, "", "  ")
		fmt.Println(string(data))
	}
	return 0
}
```

Note: `daemonNotRunningHint()` stays as-is in `ctl_hint_*.go`, but now returns just the hint text without "Error:" prefix. Update each hint file to return just the actionable line:

```go
// ctl_hint_linux.go
func daemonNotRunningHint() string {
	return "run 'k2 setup <URL>' or 'sudo systemctl start k2'"
}
```

**Step 3: Add cmdRun wrapper**

Add to `cmd/k2/process.go` — a `cmdRun(args)` function that wraps `startDaemon`:

```go
// cmdRun handles "k2 run [-c config]" — starts daemon in foreground.
func cmdRun(args []string) {
	fs := flag.NewFlagSet("k2 run", flag.ExitOnError)
	configPath := fs.String("c", "", "config file path")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, `Start daemon in foreground (system service entry point).

Usage: k2 run [-c config]

Flags:
`)
		fs.PrintDefaults()
	}
	fs.Parse(args)
	startDaemon(*configPath)
}
```

**Step 4: Add cmdConfig**

Create a small `cmdConfig` function in a new file or in `process.go`:

```go
// cmdConfig handles "k2 config test|show"
func cmdConfig(args []string) int {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: k2 config <test|show>\n")
		return 1
	}
	switch args[0] {
	case "test":
		fs := flag.NewFlagSet("k2 config test", flag.ExitOnError)
		cfgPath := fs.String("c", "", "config file path")
		fs.Parse(args[1:])
		var buf bytes.Buffer
		if err := runTestConfig(*cfgPath, &buf); err != nil {
			out.fail("INVALID_CONFIG", err.Error(), "")
			return 2
		}
		out.printResult(buf.String())
		out.ok(map[string]string{"status": "ok"})
		return 0
	case "show":
		fmt.Print(demoConfig)
		return 0
	case "--help", "-h":
		fmt.Fprintf(os.Stderr, "Usage: k2 config <test|show>\n\n  test  Test configuration\n  show  Print example config\n")
		return 0
	default:
		fmt.Fprintf(os.Stderr, "Unknown config subcommand: %s\n", args[0])
		return 1
	}
}
```

**Step 5: Update cmdService to use flag.NewFlagSet**

```go
func cmdService(args []string) {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprintf(os.Stderr, "Usage: k2 service <install|uninstall>\n")
		if len(args) == 0 {
			os.Exit(1)
		}
		return
	}
	// ... rest unchanged
}
```

**Step 6: Update service templates to use `k2 run`**

In `daemon/service_linux.go`, change `ExecStart`:
```go
// Line ~73-85: Change systemd unit ExecStart
unit := fmt.Sprintf(`[Unit]
Description=k2 network tunnel daemon
After=network.target

[Service]
Type=simple
ExecStart=%s run
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
`, exe)
```

In `daemon/service_darwin.go`, change `ProgramArguments`:
```go
// Line ~25-59: Add "run" to ProgramArguments
plistContent := fmt.Sprintf(`...
    <key>ProgramArguments</key>
    <array>
        <string>%s</string>
        <string>run</string>
    </array>
...`, label, exe)
```

In `daemon/service_windows.go`, change CreateService:
```go
// Line ~50: Add "run" argument
s, err = m.CreateService(serviceName, exe, mgr.Config{
    DisplayName: "Kaitu Network Service",
    StartType:   mgr.StartAutomatic,
}, "run")
```

In OpenWRT section of `daemon/service_linux.go`:
```go
// Line ~131: Add "run" to procd command
script := fmt.Sprintf(`...
start_service() {
    procd_open_instance
    procd_set_param command %s run
    ...
}
`, exe)
```

**Step 7: Run all tests**

Run: `go test ./cmd/k2/... ./daemon/...`
Expected: PASS (may need to fix imports)

**Step 8: Commit**

```bash
git add cmd/k2/ daemon/service_*.go
git commit -m "feat(cli): rewrite k2 to pure subcommand dispatch

- Remove top-level flags (-s, -t, -c, -v)
- Promote ctl up/down/status to k2 up/down/status
- Add k2 run, k2 config test/show
- Update service templates to use 'k2 run'
- --help works on all subcommands via flag.NewFlagSet"
```

---

### Task 3: k2s Subcommand Dispatch

Same pattern as Task 2 for the server binary.

**Files:**
- Rewrite: `cmd/k2s/main.go`
- Modify: `cmd/k2s/setup.go` — keep functionality, adjust entry
- Modify: `cmd/k2s/user.go` — add --help to dispatch
- Modify: `cmd/k2s/service.go` — add --help
- Modify: `cmd/k2s/process.go` — add `cmdRun` with `--foreground` flag
- Modify: `cmd/k2s/wizard.go` — remove biPrint, English only
- Modify: `cmd/k2s/service_linux.go` — ExecStart uses `k2s run --foreground`
- Modify: `cmd/k2s/service_darwin.go` — ProgramArguments uses `run --foreground`
- Modify: `cmd/k2s/service_windows.go` — binPath uses `run --foreground`

**Step 1: Write new main.go**

Same pattern as k2 main.go but with k2s commands:

```go
package main

import (
	_ "embed"
	"fmt"
	"os"
	"strings"
)

//go:embed server.demo.yml
var demoConfig string

var (
	version = "dev"
	commit  = "none"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	args := os.Args[1:]
	for len(args) > 0 && strings.HasPrefix(args[0], "--") {
		switch args[0] {
		case "--json":
			out.json = true
			args = args[1:]
		case "--quiet":
			args = args[1:]
		case "--help", "-h":
			printUsage()
			return
		default:
			fmt.Fprintf(os.Stderr, "Unknown flag: %s\nRun 'k2s help' for usage.\n", args[0])
			os.Exit(1)
		}
	}

	if len(args) == 0 {
		printUsage()
		os.Exit(1)
	}

	cmd := args[0]
	rest := args[1:]

	switch cmd {
	case "run":
		cmdRun(rest)
	case "setup":
		cmdSetup(rest)
	case "user":
		cmdUser(rest)
	case "config":
		os.Exit(cmdConfig(rest))
	case "service":
		cmdService(rest)
	case "upgrade":
		cmdUpgrade(rest)
	case "version":
		fmt.Printf("k2s %s (%s)\n", version, commit)
	case "help", "--help", "-h":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\nRun 'k2s help' for usage.\n", cmd)
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `k2s — Kaitu network tunnel server

Quick start:
  k2s run                   Deploy server (prints connect URL)

Commands:
  run       Start/deploy server
  setup     Setup wizard (or --port/--username for automation)
  user      User management (add, list, del, reset)

  config    Configuration (test, show)
  service   System service (install, uninstall)       [root]
  upgrade   Self-update
  version   Version info

Flags:
  --json    Machine-readable JSON output
  --quiet   Minimal output
  --help    Help for any command
`)
}
```

**Step 2: Add cmdRun with --foreground**

In `cmd/k2s/process.go`, add:

```go
func cmdRun(args []string) {
	fs := flag.NewFlagSet("k2s run", flag.ExitOnError)
	foreground := fs.Bool("foreground", false, "stay in foreground (for service managers)")
	configPath := fs.String("c", "", "config file path")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, `Start or deploy k2s server.

Usage: k2s run [flags]

  Without --foreground: install service + start + print URL + exit.
  With --foreground: run server in foreground (for systemd/launchd).

Flags:
`)
		fs.PrintDefaults()
	}
	fs.Parse(args)

	if *foreground {
		runServer(*configPath)
		return
	}

	// Smart mode: install service if needed, print URL.
	certDir := config.DefaultServerConfig().CertDir
	urlPath := filepath.Join(certDir, "connect-url.txt")

	if isServiceRunning() {
		url := readConnectURL(urlPath)
		if url == "" {
			url = waitForConnectURL(urlPath, 30*time.Second)
		}
		if url != "" {
			out.printResult(url)
			out.ok(map[string]string{"url": url, "status": "running"})
		} else {
			out.fail("TIMEOUT", "timed out waiting for connect URL", "check: sudo journalctl -u k2s -f")
			os.Exit(2)
		}
		return
	}

	// Not running — install and start.
	out.printResult("Installing and starting k2s service...")
	if err := installService(); err != nil {
		out.fail("SERVICE_FAILED", err.Error(), "try: sudo k2s run")
		os.Exit(3)
	}

	url := waitForConnectURL(urlPath, 30*time.Second)
	if url == "" {
		out.fail("TIMEOUT", "timed out waiting for server to start", "check: sudo journalctl -u k2s -f")
		os.Exit(2)
	}
	out.printResult("Server running. Connect URL:")
	out.printResult("  " + url)
	out.ok(map[string]string{"url": url, "status": "started"})
}
```

**Step 3: Remove biPrint from wizard.go — English only**

Replace all `biPrint("中文", "English")` calls with just `fmt.Println("English")`.
Replace all `biPrintf("中文 %s", "English %s", args...)` with `fmt.Printf("English %s\n", args...)`.
Delete `biPrint` and `biPrintf` functions.

**Step 4: Add --help to cmdUser and cmdService**

In `cmd/k2s/user.go`, add case in switch:
```go
case "--help", "-h":
    printUserUsage()
    return
```

In `cmd/k2s/service.go`, add case in switch:
```go
case "--help", "-h":
    fmt.Fprintf(os.Stderr, "Usage: k2s service <install|uninstall>\n")
    return
```

**Step 5: Update k2s service templates**

In `cmd/k2s/service_linux.go`, both `installService()` and `installServiceWithConfig()`:
```go
ExecStart=%s run --foreground
// and
ExecStart=%s run --foreground -c %s
```

In `cmd/k2s/service_darwin.go`, both functions:
```xml
<string>%s</string>
<string>run</string>
<string>--foreground</string>
```
and with config:
```xml
<string>%s</string>
<string>run</string>
<string>--foreground</string>
<string>-c</string>
<string>%s</string>
```

In `cmd/k2s/service_windows.go`:
```go
"binPath=", fmt.Sprintf(`"%s" run --foreground`, exe),
// and
"binPath=", fmt.Sprintf(`"%s" run --foreground -c "%s"`, exe, configPath),
```

**Step 6: Add cmdConfig for k2s**

```go
func cmdConfig(args []string) int {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: k2s config <test|show>\n")
		return 1
	}
	switch args[0] {
	case "test":
		fs := flag.NewFlagSet("k2s config test", flag.ExitOnError)
		cfgPath := fs.String("c", "", "config file path")
		fs.Parse(args[1:])
		var buf bytes.Buffer
		if err := runTestConfig(*cfgPath, &buf); err != nil {
			out.fail("INVALID_CONFIG", err.Error(), "")
			return 2
		}
		out.printResult(buf.String())
		return 0
	case "show":
		fmt.Print(demoConfig)
		return 0
	case "--help", "-h":
		fmt.Fprintf(os.Stderr, "Usage: k2s config <test|show>\n")
		return 0
	default:
		fmt.Fprintf(os.Stderr, "Unknown config subcommand: %s\n", args[0])
		return 1
	}
}
```

**Step 7: Run tests**

Run: `go test ./cmd/k2s/...`
Expected: PASS

**Step 8: Commit**

```bash
git add cmd/k2s/
git commit -m "feat(cli): rewrite k2s to pure subcommand dispatch

- Remove top-level flags (-s, -t, -c, -v)
- Add k2s run with --foreground flag
- Add k2s config test/show
- Remove bilingual output, English only
- Update service templates to use 'k2s run --foreground'
- --help works on all subcommands"
```

---

### Task 4: `k2 connect` — Direct Foreground Mode

The core new command. Runs engine directly without daemon.

**Files:**
- Create: `cmd/k2/connect.go`
- Test: `cmd/k2/connect_test.go`

**Step 1: Write connect_test.go**

```go
package main

import (
	"testing"
)

func TestCmdConnectNoArgs(t *testing.T) {
	code := cmdConnect(nil)
	if code != 1 {
		t.Errorf("expected exit 1 for no args, got %d", code)
	}
}

func TestCmdConnectBadURL(t *testing.T) {
	code := cmdConnect([]string{"not-a-url"})
	if code != 1 {
		t.Errorf("expected exit 1 for bad URL, got %d", code)
	}
}

func TestCmdConnectHelp(t *testing.T) {
	// Should not panic
	code := cmdConnect([]string{"--help"})
	_ = code
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./cmd/k2/ -run TestCmdConnect -v`
Expected: FAIL — cmdConnect not defined

**Step 3: Write connect.go**

```go
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/kaitu-io/k2/config"
	"github.com/kaitu-io/k2/engine"
)

// cmdConnect runs a tunnel in foreground without daemon.
// Default mode is proxy (no root needed).
func cmdConnect(args []string) int {
	fs := flag.NewFlagSet("k2 connect", flag.ContinueOnError)
	mode := fs.String("mode", "proxy", "traffic mode: proxy or tun")
	listen := fs.String("listen", "127.0.0.1:1080", "proxy listen address (proxy mode only)")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, `Connect to a k2 server in foreground mode.

Usage: k2 connect <URL> [flags]

  No daemon, no service installation, no configuration files.
  Default mode is proxy (SOCKS5+HTTP on 127.0.0.1:1080). Ctrl+C to stop.

Flags:
`)
		fs.PrintDefaults()
		fmt.Fprintf(os.Stderr, `
Examples:
  k2 connect k2v5://user:token@host:443?ech=...
  k2 connect k2v5://... --mode tun             # VPN mode (needs sudo)
  k2 connect k2v5://... --listen 0.0.0.0:1080  # custom proxy address
`)
	}
	if err := fs.Parse(args); err != nil {
		return 1
	}

	if fs.NArg() == 0 {
		fs.Usage()
		return 1
	}

	wireURL := fs.Arg(0)
	if !strings.Contains(wireURL, "://") {
		out.fail("INVALID_URL", fmt.Sprintf("expected k2v5:// URL, got %q", wireURL), "")
		return 1
	}

	// Build config from URL.
	cfg := config.ClientFromURL(wireURL)
	cfg.Mode = *mode
	if *mode == "proxy" {
		cfg.Proxy.Listen = *listen
	}

	// Validate.
	if err := config.ValidateClient(cfg); err != nil {
		out.fail("INVALID_CONFIG", err.Error(), "")
		return 1
	}

	// Set up logging to stderr.
	cleanup, err := config.SetupLogging(config.LogConfig{}, "k2")
	if err != nil {
		slog.Error("logging setup failed", "err", err)
		return 2
	}
	defer cleanup()

	// Ensure k2rule cache dir exists.
	if cfg.Rule.CacheDir == "" {
		if home, err := os.UserHomeDir(); err == nil {
			cfg.Rule.CacheDir = filepath.Join(home, ".cache", "k2rule")
		}
	}

	// Create engine with event handler.
	eng := engine.New()
	eng.SetEventHandler(&connectHandler{})

	ecfg := engine.Config{
		Client:         cfg,
		FileDescriptor: -1,
		// Proxy mode: no DirectDialer needed (no TUN routing loop).
		// TUN mode: DirectDialer needed but we skip for simplicity —
		// users needing TUN should use k2 setup + k2 up (daemon path).
	}

	// Start engine.
	out.event(map[string]any{"event": "connecting", "server": cfg.Server, "mode": cfg.Mode})

	if err := eng.Start(ecfg); err != nil {
		out.fail("CONNECT_FAILED", err.Error(), "")
		return 2
	}

	// Wait for signal.
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	<-ctx.Done()

	eng.Stop()
	out.event(map[string]any{"event": "disconnected", "reason": "signal"})
	return 0
}

// connectHandler implements engine.EventHandler for foreground mode.
type connectHandler struct{}

func (h *connectHandler) OnStatus(status engine.Status) {
	event := map[string]any{"event": status.State}
	if status.Error != nil {
		event["error"] = status.Error.Message
		event["code"] = status.Error.Code
	}
	out.event(event)
}

func (h *connectHandler) OnStats(txBytes, rxBytes int64) {
	// Stats are not emitted in connect mode (too noisy).
	// Users can check traffic via proxy client.
}
```

**Step 4: Run tests**

Run: `go test ./cmd/k2/ -run TestCmdConnect -v`
Expected: PASS

**Step 5: Manual smoke test (proxy mode)**

If you have a k2s server running:
```bash
go run ./cmd/k2 connect 'k2v5://...' --json
# Expected: {"event":"connecting",...} then {"event":"connected",...}
# Ctrl+C → {"event":"disconnected","reason":"signal"}
```

**Step 6: Commit**

```bash
git add cmd/k2/connect.go cmd/k2/connect_test.go
git commit -m "feat(cli): add k2 connect — direct foreground mode

Runs engine directly without daemon/service. Defaults to proxy mode
(SOCKS5+HTTP on 127.0.0.1:1080), no root needed. Ctrl+C to stop.
Outputs NDJSON events for machine consumption."
```

---

### Task 5: `k2s setup` Non-Interactive Mode

Add flags to skip interactive prompts.

**Files:**
- Modify: `cmd/k2s/setup.go` — add flags, conditional prompting
- Modify: `cmd/k2s/wizard.go` — parameterize prompts
- Test: `cmd/k2s/setup_test.go`

**Step 1: Write test**

```go
// cmd/k2s/setup_test.go
package main

import (
	"testing"
)

func TestBuildMinimalConfig(t *testing.T) {
	cfg := buildMinimalConfig(8443, 50000, 50100, "/tmp/users")
	if cfg["listen"] != ":8443" {
		t.Errorf("unexpected listen: %v", cfg["listen"])
	}
	if cfg["hop_start"] != 50000 {
		t.Errorf("unexpected hop_start: %v", cfg["hop_start"])
	}
}

func TestBuildMinimalConfigNoHop(t *testing.T) {
	cfg := buildMinimalConfig(443, 0, 0, "/tmp/users")
	if _, ok := cfg["hop_start"]; ok {
		t.Error("hop_start should not be set when disabled")
	}
}
```

**Step 2: Run test**

Run: `go test ./cmd/k2s/ -run TestBuildMinimalConfig -v`
Expected: PASS (this tests existing code)

**Step 3: Add flags to cmdSetup**

Modify `cmd/k2s/setup.go` `cmdSetup()`:

```go
func cmdSetup(args []string) {
	fs := flag.NewFlagSet("k2s setup", flag.ExitOnError)
	portFlag := fs.Int("port", 0, "listen port (default 443)")
	usernameFlag := fs.String("username", "", "first user to create (default prince)")
	hopFlag := fs.String("hop", "", "port hop range, e.g. 50000-50100 (default: disabled)")
	noFirewall := fs.Bool("no-firewall", false, "skip firewall configuration")
	reset := fs.Bool("reset", false, "re-run setup (preserves certs and users)")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, `Set up k2s server.

Usage: k2s setup [flags]

  No flags: interactive wizard.
  All flags: fully automated (no prompts).

Flags:
`)
		fs.PrintDefaults()
		fmt.Fprintf(os.Stderr, `
Examples:
  k2s setup                                            # interactive wizard
  k2s setup --port 443 --username alice --no-firewall  # fully automated
  k2s setup --port 8443                                # prompt only for missing
`)
	}
	fs.Parse(args)

	certDir := config.DefaultServerConfig().CertDir
	configPath := filepath.Join(certDir, "config.yml")
	usersPath := filepath.Join(certDir, "users")
	urlPath := filepath.Join(certDir, "connect-url.txt")

	if *reset {
		os.Remove(configPath)
		fmt.Println("Config reset, re-running wizard")
	}

	// If config exists and not reset → start service / print URL.
	if fileExists(configPath) && !*reset {
		if isServiceRunning() {
			url := readConnectURL(urlPath)
			if url == "" {
				url = waitForConnectURL(urlPath, 30*time.Second)
			}
			if url != "" {
				out.printResult(url)
				out.ok(map[string]string{"url": url})
				return
			}
			out.fail("TIMEOUT", "timed out waiting for connect URL", "")
			os.Exit(2)
		}
		fmt.Println("Existing config found, starting service...")
		startServiceWithConfig(configPath)
		waitAndPrintURL(urlPath)
		return
	}

	// Determine which params we have.
	port := *portFlag
	username := *usernameFlag
	hopStart, hopEnd := 0, 0
	if *hopFlag != "" {
		var err error
		hopStart, hopEnd, err = parseRange(*hopFlag)
		if err != nil {
			out.fail("INVALID_HOP", err.Error(), "expected: 50000-50100")
			os.Exit(1)
		}
	}

	// Non-interactive if all required params given.
	nonInteractive := port > 0 && username != "" && *noFirewall

	if nonInteractive {
		runNonInteractiveSetup(certDir, configPath, usersPath, urlPath,
			port, username, hopStart, hopEnd)
		return
	}

	// Interactive: prompt for missing params.
	runInteractiveSetup(certDir, configPath, usersPath, urlPath,
		port, username, hopStart, hopEnd, *noFirewall)
}
```

**Step 4: Add runNonInteractiveSetup**

```go
func runNonInteractiveSetup(certDir, configPath, usersPath, urlPath string,
	port int, username string, hopStart, hopEnd int) {

	if err := os.MkdirAll(certDir, 0755); err != nil {
		out.fail("DIR_FAILED", err.Error(), "")
		os.Exit(2)
	}

	// Create user.
	token, err := generateToken()
	if err != nil {
		out.fail("TOKEN_FAILED", err.Error(), "")
		os.Exit(2)
	}
	hash, err := hashToken(token)
	if err != nil {
		out.fail("HASH_FAILED", err.Error(), "")
		os.Exit(2)
	}
	if err := appendUser(usersPath, username, hash); err != nil {
		out.fail("USER_FAILED", err.Error(), "")
		os.Exit(2)
	}

	// Write config.
	cfg := buildMinimalConfig(port, hopStart, hopEnd, usersPath)
	if err := writeConfigYAML(configPath, cfg); err != nil {
		out.fail("CONFIG_FAILED", err.Error(), "")
		os.Exit(2)
	}

	// Start service.
	startServiceWithConfig(configPath)

	url := waitForConnectURL(urlPath, 30*time.Second)
	if url == "" {
		out.fail("TIMEOUT", "timed out waiting for server", "check: sudo journalctl -u k2s -f")
		os.Exit(2)
	}

	url, err = replaceURLCredentials(url, username, token)
	if err != nil {
		out.fail("URL_FAILED", err.Error(), "")
		os.Exit(2)
	}

	out.printf("Config: %s\n", configPath)
	out.printf("Service: started\n")
	out.printf("URL: %s\n", url)
	out.ok(map[string]any{"url": url, "config": configPath, "username": username})
}
```

**Step 5: Add runInteractiveSetup**

```go
func runInteractiveSetup(certDir, configPath, usersPath, urlPath string,
	portGiven int, usernameGiven string, hopStart, hopEnd int, noFirewall bool) {

	if err := os.MkdirAll(certDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Error creating directory %s: %v\n", certDir, err)
		os.Exit(1)
	}

	printHeader()

	// Step 1: Port (skip if given)
	port := portGiven
	if port == 0 {
		port = promptPort("Listen port", "Listen port", 443)
	} else {
		fmt.Printf("Port: %d\n", port)
	}
	fmt.Println()

	// Step 2: Hop (skip if given via flag)
	if hopStart == 0 && hopEnd == 0 {
		hopStart, hopEnd = promptHopRange()
	}
	fmt.Println()

	// Step 3: Username (skip if given)
	var username, token string
	var err error
	if usernameGiven != "" {
		username = usernameGiven
		token, err = generateToken()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		hash, _ := hashToken(token)
		appendUser(usersPath, username, hash)
		fmt.Printf("User: %s (token auto-generated)\n", username)
	} else {
		username, token, err = addFirstUser(usersPath, "prince")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	}
	fmt.Println()

	// Step 4: Firewall (skip if --no-firewall)
	if !noFirewall {
		runFirewallSetup(port, hopStart, hopEnd)
	}
	fmt.Println()

	// Step 5: Config + start
	fmt.Println("Generating config...")
	cfg := buildMinimalConfig(port, hopStart, hopEnd, usersPath)
	if err := writeConfigYAML(configPath, cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing config: %v\n", err)
		os.Exit(1)
	}

	startServiceWithConfig(configPath)

	url := waitForConnectURL(urlPath, 30*time.Second)
	if url == "" {
		fmt.Fprintln(os.Stderr, "Timed out waiting for server to start.")
		os.Exit(1)
	}

	url, _ = replaceURLCredentials(url, username, token)
	printComplete(configPath, url, hopStart > 0)
}
```

**Step 6: Update wizard.go prompts to English only**

Replace `biPrint` and `biPrintf` throughout:

```go
// Old:
biPrint("启用端口跳跃？", "Enable port hopping?")
// New:
fmt.Print("Enable port hopping?")

// Old:
biPrintf("端口 %d 已被占用", "Port %d in use", port)
// New:
fmt.Printf("Port %d in use\n", port)
```

Update `promptString`, `promptYesNo`, `promptPort` to take single English string instead of two.

Update `printHeader`:
```go
func printHeader() {
	fmt.Println()
	fmt.Println("k2s Setup Wizard")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()
}
```

Update `printComplete` to English only.

**Step 7: Run tests**

Run: `go test ./cmd/k2s/...`
Expected: PASS

**Step 8: Commit**

```bash
git add cmd/k2s/
git commit -m "feat(cli): add non-interactive k2s setup

k2s setup --port 443 --username alice --no-firewall runs fully automated.
Partial flags prompt only for missing params.
Remove bilingual output, English only."
```

---

### Task 6: `--json` for k2s User Commands

**Files:**
- Modify: `cmd/k2s/user.go`

**Step 1: Add --json output to cmdUserAdd**

In `cmdUserAdd`, change the output section:

```go
func cmdUserAdd(usersPath, urlPath, username string) {
	// ... existing token generation and user creation ...

	url := buildUserURL(urlPath, username, token)
	if out.json {
		data := map[string]string{"username": username, "token": token}
		if url != "" {
			data["url"] = url
		}
		out.ok(data)
		return
	}
	// Human output
	fmt.Printf("\nUser added: %s\n", username)
	if url != "" {
		fmt.Printf("\nConnect URL:\n  %s\n\n", url)
	} else {
		fmt.Println("Server not started yet, run 'k2s run' first")
	}
}
```

**Step 2: Add --json output to cmdUserList**

```go
func cmdUserList(usersPath string) {
	users, err := loadUsers(usersPath)
	if err != nil {
		out.fail("LOAD_FAILED", err.Error(), "")
		os.Exit(2)
	}

	if out.json {
		names := make([]string, 0, len(users))
		for name := range users {
			names = append(names, name)
		}
		out.ok(map[string]any{"users": names, "count": len(names)})
		return
	}

	if len(users) == 0 {
		fmt.Println("No users found")
		return
	}
	fmt.Println("\nUsers:")
	for name := range users {
		fmt.Printf("  - %s\n", name)
	}
	fmt.Printf("\nTotal: %d\n", len(users))
}
```

**Step 3: Add --json to cmdUserReset**

Same pattern as cmdUserAdd.

**Step 4: Remove remaining biPrint calls from user.go**

Replace all `biPrint`/`biPrintf` with English `fmt.Printf`.

**Step 5: Run tests**

Run: `go test ./cmd/k2s/...`
Expected: PASS

**Step 6: Commit**

```bash
git add cmd/k2s/user.go
git commit -m "feat(cli): add --json output to k2s user commands"
```

---

### Task 7: Final Cleanup

**Files:**
- Delete: `cmd/k2/ctl.go` (logic moved to new cmdUp/cmdDown/cmdStatus in Task 2)
- Modify: `cmd/k2/ctl_hint_linux.go` — simplify hint text
- Modify: `cmd/k2/ctl_hint_darwin.go` — simplify hint text
- Modify: `cmd/k2/ctl_hint_windows.go` — simplify hint text
- Delete: `cmd/k2/signal.go` and `cmd/k2/signal_windows.go` (no more `-s` flag)
- Update: `CLAUDE.md` — new command reference
- Update: `cmd/k2/CLAUDE.md`
- Update: `cmd/k2s/CLAUDE.md`

**Step 1: Remove dead code**

Delete files that are no longer needed:
- `cmd/k2/signal.go` — `-s stop|reload` removed
- `cmd/k2/signal_windows.go` — same
- `cmd/k2s/signal.go` — same
- `cmd/k2s/signal_windows.go` — same

**Step 2: Simplify hint files**

```go
// cmd/k2/ctl_hint_linux.go
func daemonNotRunningHint() string {
	return "run 'k2 setup <URL>' or 'sudo systemctl start k2'"
}
```

Repeat for darwin and windows.

**Step 3: Update CLAUDE.md Quick Commands section**

Replace the old command reference with the new command table:

```markdown
## Quick Commands

```bash
go build -o k2 ./cmd/k2            # Build client
go build -o k2s ./cmd/k2s          # Build server
go test ./...                      # All tests
go test -short ./...               # Fast: skip live DNS/network tests
make quick-check                   # go vet + golangci-lint + go test -short -race

# Client
k2 connect <k2v5://URL>            # Direct connect (foreground, proxy mode)
k2 setup <k2v5://URL>              # First-time: generate config + install service + start
k2 up                              # Connect (via daemon, uses saved config)
k2 down                            # Disconnect
k2 status                          # Connection status
k2 run                             # Start daemon (service entry point)

# Server
k2s run                            # Deploy server (auto-config, prints URL)
k2s setup                          # Interactive setup wizard
k2s setup --port 443 --username alice --no-firewall  # Automated setup
k2s user add <name>                # Add user, print connect URL

# Global flags
--json                             # Machine-readable JSON output
--help                             # Help for any command
```
```

**Step 4: Run full test suite**

Run: `go test ./...`
Expected: PASS

Run: `go vet ./...`
Expected: clean

**Step 5: Commit**

```bash
git add -A
git commit -m "feat(cli): complete CLI redesign — cleanup and docs

- Remove signal.go files (no more -s flag)
- Simplify daemon-not-running hints
- Update CLAUDE.md with new command reference"
```

---

## Summary

| Task | What | Files Changed |
|------|------|---------------|
| 1 | Output layer (human/JSON) | 3 new files |
| 2 | k2 subcommand dispatch | main.go rewrite + ctl.go refactor + service templates |
| 3 | k2s subcommand dispatch | main.go rewrite + wizard English-only + service templates |
| 4 | `k2 connect` direct mode | 2 new files |
| 5 | `k2s setup` non-interactive | setup.go refactor + wizard.go refactor |
| 6 | `--json` for k2s user | user.go modify |
| 7 | Cleanup + docs | delete signal files, update CLAUDE.md |

Total: ~7 commits, each independently testable.
