# k2 CLI Redesign Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Refactor k2/k2s CLI to nginx-style architecture: foreground-first, config-file-driven, signal control, setup bootstrap.

**Architecture:** Config infrastructure (search, validate, PID, include) in `k2/config/` as foundation. Daemon adds SIGHUP reload handler. Both `k2/cmd/k2/` and `k2/cmd/k2s/` rewrite main dispatch from subcommand-based to flag-based (nginx model). Desktop Tauri and NSIS hooks update command strings.

**Tech Stack:** Go 1.22 (k2 submodule), Rust/Tauri v2 (desktop), NSIS (Windows installer)

**Repo note:** k2/ is a Git submodule. Go changes (F1, F2, T3, T4) happen in the k2 repo. Desktop changes (T5) happen in k2app repo. Plan assumes working directly in k2/ submodule directory.

---

## Dependency Graph

```
F1 (config infra) ── F2 (daemon reload) ──┬── T3 (k2/cmd/k2 rewrite)
                                           └── T4 (k2/cmd/k2s rewrite)
                                           T3 ── T5 (desktop Tauri + NSIS)
```

- F1 and F2 are sequential (F2 depends on F1)
- T3 ‖ T4 — parallel (different directories, no file overlap)
- T5 after T3 (needs `k2 service install` command to exist in binary)

## AC Coverage Map

| AC | Test(s) | Task |
|----|---------|------|
| AC1 (前台运行) | `TestK2_ForegroundRun`, `TestK2s_ForegroundRun` | T3, T4 |
| AC2 (配置搜索) | `TestSearchConfigClient`, `TestSearchConfigServer`, `TestSearchChainPriority` | F1 |
| AC3 (配置验证) | `TestValidateClient_Valid`, `TestValidateClient_BadServerURL`, `TestValidateServer_MissingListen` | F1 |
| AC4 (信号控制) | `TestSignalStop`, `TestSignalReload` | T3, T4 |
| AC5 (PID 文件) | `TestPIDWrite`, `TestPIDRead`, `TestPIDStale`, `TestPIDCleanup` | F1 |
| AC6 (setup 引导) | `TestSetupWithURL`, `TestSetupIdempotent`, `TestK2sSetup` | T3, T4 |
| AC7 (ctl 命令) | `TestCtlUp`, `TestCtlDown`, `TestCtlStatus`, `TestCtlDaemonNotRunning` | T3 |
| AC8 (service 管理) | `TestServiceInstall`, `TestServiceUninstall` | T3, T4 |
| AC9 (热重载) | `TestReloadDynamic`, `TestReloadStaticWarning`, `TestReloadAPINotInterrupted` | F2 |
| AC10 (向后兼容) | `TestDeprecatedRunAlias`, `TestDeprecatedUpAlias` | T3, T4 |
| AC11 (include 指令) | `TestIncludeGlob`, `TestIncludeMergeOrder`, `TestIncludeError` | F1 |
| AC12 (Tauri 适配) | `test_admin_reinstall_uses_service_install` (Rust), NSIS manual verify | T5 |

---

## F1: Config Infrastructure

**Scope:** `k2/config/` — add config search, validation, include, and PID file utilities.

**Files:**
- Create: `k2/config/search.go`
- Create: `k2/config/search_test.go`
- Create: `k2/config/validate.go`
- Create: `k2/config/validate_test.go`
- Create: `k2/config/include.go`
- Create: `k2/config/include_test.go`
- Create: `k2/config/pid.go`
- Create: `k2/config/pid_test.go`
- Modify: `k2/config/config.go` — add `PIDFile` and `Include` fields to structs

**depends_on:** none

### RED

Test function names:

**search_test.go:**
- `TestSearchConfigClient_CurrentDir` — finds `./k2.yaml`
- `TestSearchConfigClient_UserDir` — finds `~/.config/k2/k2.yaml`
- `TestSearchConfigClient_SystemDir` — finds `/etc/k2/k2.yaml`
- `TestSearchConfigClient_Priority` — `./k2.yaml` beats `/etc/k2/k2.yaml`
- `TestSearchConfigClient_NotFound` — returns empty string + nil error
- `TestSearchConfigServer_CurrentDir` — finds `./k2s.yaml`
- `TestSearchConfigServer_SystemDir` — finds `/etc/k2s/k2s.yaml`

**validate_test.go:**
- `TestValidateClient_Valid` — minimal valid config passes
- `TestValidateClient_BadServerURL` — invalid URL → error with field name
- `TestValidateClient_BadListenAddr` — invalid listen → error
- `TestValidateClient_BadMode` — mode not "tun"/"proxy" → error
- `TestValidateClient_BadLogLevel` — invalid log level → error
- `TestValidateServer_Valid` — minimal valid passes
- `TestValidateServer_EmptyListen` — empty listen OK (default applied)

**include_test.go:**
- `TestIncludeGlob` — resolves `/tmp/test-conf.d/*.yaml` to sorted files
- `TestIncludeMerge` — later file overrides earlier keys
- `TestIncludeNested` — include in included file is NOT supported (flat only)
- `TestIncludeBadYAML` — reports file name and error
- `TestIncludeNoMatch` — empty glob → no error, no merge

**pid_test.go:**
- `TestPIDWrite` — writes PID to file, reads back correct value
- `TestPIDRead` — reads existing PID file
- `TestPIDRead_Missing` — missing file → 0, nil error
- `TestPIDStale` — file exists but process dead → stale, cleaned up
- `TestPIDCleanup` — removes file on call

### GREEN

**Step 1: Add fields to config structs**

Modify `k2/config/config.go`:
```go
// Add to ClientConfig struct:
PIDFile string `yaml:"pid_file" json:"pid_file"`
Include string `yaml:"include"  json:"include"`

// Add to ServerConfig struct:
PIDFile string `yaml:"pid_file"`
Include string `yaml:"include"`
```

**Step 2: Implement search.go**

```go
package config

import (
    "os"
    "path/filepath"
    "runtime"
)

// SearchClientConfig finds the first k2.yaml in the search chain.
// Returns empty string if none found.
func SearchClientConfig() string {
    candidates := []string{"k2.yaml"}

    // ~/.config/k2/k2.yaml
    if home, err := os.UserHomeDir(); err == nil {
        candidates = append(candidates, filepath.Join(home, ".config", "k2", "k2.yaml"))
    }

    // /etc/k2/k2.yaml (or ProgramData on Windows)
    if runtime.GOOS == "windows" {
        candidates = append(candidates, filepath.Join(os.Getenv("ProgramData"), "k2", "k2.yaml"))
    } else {
        candidates = append(candidates, "/etc/k2/k2.yaml")
    }

    for _, path := range candidates {
        if _, err := os.Stat(path); err == nil {
            abs, _ := filepath.Abs(path)
            return abs
        }
    }
    return ""
}

// SearchServerConfig finds the first k2s.yaml in the search chain.
func SearchServerConfig() string {
    candidates := []string{"k2s.yaml"}

    if runtime.GOOS == "windows" {
        candidates = append(candidates, filepath.Join(os.Getenv("ProgramData"), "k2s", "k2s.yaml"))
    } else {
        candidates = append(candidates, "/etc/k2s/k2s.yaml")
    }

    for _, path := range candidates {
        if _, err := os.Stat(path); err == nil {
            abs, _ := filepath.Abs(path)
            return abs
        }
    }
    return ""
}
```

**Step 3: Implement validate.go**

```go
package config

import (
    "fmt"
    "net"
    "strings"
)

type ValidationError struct {
    Field   string
    Message string
}

func (e *ValidationError) Error() string {
    return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

func ValidateClient(cfg *ClientConfig) error {
    if cfg.Server != "" && !strings.Contains(cfg.Server, "://") {
        return &ValidationError{Field: "server", Message: "invalid URL format (expected scheme://...)"}
    }
    if cfg.Listen != "" {
        if _, _, err := net.SplitHostPort(cfg.Listen); err != nil {
            return &ValidationError{Field: "listen", Message: fmt.Sprintf("invalid address: %v", err)}
        }
    }
    if cfg.Mode != "" && cfg.Mode != "tun" && cfg.Mode != "proxy" {
        return &ValidationError{Field: "mode", Message: "must be 'tun' or 'proxy'"}
    }
    validLevels := map[string]bool{"debug": true, "info": true, "warn": true, "error": true, "": true}
    if !validLevels[cfg.Log.Level] {
        return &ValidationError{Field: "log.level", Message: "must be debug, info, warn, or error"}
    }
    return nil
}

func ValidateServer(cfg *ServerConfig) error {
    if cfg.Listen != "" {
        if _, _, err := net.SplitHostPort(cfg.Listen); err != nil {
            return &ValidationError{Field: "listen", Message: fmt.Sprintf("invalid address: %v", err)}
        }
    }
    validLevels := map[string]bool{"debug": true, "info": true, "warn": true, "error": true, "": true}
    if !validLevels[cfg.Log.Level] {
        return &ValidationError{Field: "log.level", Message: "must be debug, info, warn, or error"}
    }
    return nil
}
```

**Step 4: Implement include.go**

```go
package config

import (
    "fmt"
    "path/filepath"
    "sort"
    "os"
    "gopkg.in/yaml.v3"
)

// ResolveInclude loads and merges include files into a base map.
// Pattern is a glob (e.g. "/etc/k2/conf.d/*.yaml").
// Files are merged in alphabetical order; later keys override earlier.
func ResolveInclude(pattern string, base map[string]any) error {
    if pattern == "" {
        return nil
    }

    matches, err := filepath.Glob(pattern)
    if err != nil {
        return fmt.Errorf("include glob error: %w", err)
    }
    sort.Strings(matches)

    for _, path := range matches {
        data, err := os.ReadFile(path)
        if err != nil {
            return fmt.Errorf("include %s: %w", path, err)
        }
        var overlay map[string]any
        if err := yaml.Unmarshal(data, &overlay); err != nil {
            return fmt.Errorf("include %s: %w", path, err)
        }
        for k, v := range overlay {
            base[k] = v
        }
    }
    return nil
}
```

**Step 5: Implement pid.go**

```go
package config

import (
    "fmt"
    "os"
    "strconv"
    "strings"
    "syscall"
)

func WritePID(path string) error {
    return os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())), 0644)
}

func ReadPID(path string) (int, error) {
    data, err := os.ReadFile(path)
    if err != nil {
        if os.IsNotExist(err) {
            return 0, nil
        }
        return 0, err
    }
    pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
    if err != nil {
        return 0, fmt.Errorf("invalid PID file: %w", err)
    }
    return pid, nil
}

// IsProcessAlive checks if a process with the given PID exists.
func IsProcessAlive(pid int) bool {
    if pid <= 0 {
        return false
    }
    process, err := os.FindProcess(pid)
    if err != nil {
        return false
    }
    err = process.Signal(syscall.Signal(0))
    return err == nil
}

func RemovePID(path string) error {
    return os.Remove(path)
}

// CheckStale reads PID file. If process is dead, removes the file and returns 0.
func CheckStale(path string) (int, error) {
    pid, err := ReadPID(path)
    if err != nil || pid == 0 {
        return 0, err
    }
    if !IsProcessAlive(pid) {
        _ = RemovePID(path)
        return 0, nil
    }
    return pid, nil
}
```

**Step 6: Run tests**

```bash
cd k2 && go test ./config/... -v -run "TestSearch|TestValidate|TestInclude|TestPID"
```

**Step 7: Commit**

```bash
cd k2 && git add config/
git commit -m "feat(config): add search, validate, include, PID infrastructure

Foundation for nginx-style CLI redesign. Adds:
- Config file search chain (./k2.yaml → ~/.config/ → /etc/)
- Validate() for client and server configs
- Include directive (glob + merge)
- PID file read/write/stale detection

Part of: k2-cli-redesign F1"
```

### REFACTOR

- `[MUST]` Ensure `DefaultPIDPath()` helper returns platform-appropriate default (`/var/run/k2.pid` or `~/.config/k2/k2.pid`)
- `[SHOULD]` Extract validation rules into table-driven approach if > 10 rules
- `[SHOULD]` Add `SearchConfigPaths()` that returns the full candidate list (for error messages)

---

## F2: Daemon Hot Reload

**Scope:** `k2/daemon/` — add SIGHUP handler that reloads dynamic config sections.

**Files:**
- Create: `k2/daemon/reload.go`
- Create: `k2/daemon/reload_test.go`
- Modify: `k2/daemon/daemon.go` — add `ConfigPath` field, `Reload()` method

**depends_on:** F1

### RED

Test function names:

**reload_test.go:**
- `TestReload_DynamicFields` — changing `server` in config file + Reload() → daemon picks up new server
- `TestReload_StaticFieldWarning` — changing `listen` → Reload() logs warning, field unchanged
- `TestReload_DNSUpdate` — changing DNS servers → Reload() applies new DNS
- `TestReload_LogLevel` — changing log.level → Reload() applies immediately
- `TestReload_InvalidConfig` — broken YAML → Reload() returns error, keeps old config
- `TestReload_NoConfigPath` — Reload() with no config path → error "no config file to reload"

### GREEN

**Step 1: Add ConfigPath and Reload to Daemon**

Modify `k2/daemon/daemon.go`:
```go
// Add to Daemon struct:
ConfigPath string // path to config file for reload

// Add Reload method:
func (d *Daemon) Reload() error {
    if d.ConfigPath == "" {
        return fmt.Errorf("no config file to reload")
    }

    cfg, err := config.LoadClient(d.ConfigPath)
    if err != nil {
        return fmt.Errorf("reload config: %w", err)
    }
    if err := config.ValidateClient(cfg); err != nil {
        return fmt.Errorf("reload validate: %w", err)
    }

    return d.applyDynamicConfig(cfg)
}
```

**Step 2: Implement applyDynamicConfig**

Create `k2/daemon/reload.go`:
```go
package daemon

import (
    "log"
    "github.com/kaitu-io/k2/config"
)

// staticFields are fields that require a restart to change.
var staticFields = []string{"listen", "mode", "tun", "proxy.listen", "pid_file"}

func (d *Daemon) applyDynamicConfig(cfg *config.ClientConfig) error {
    d.mu.Lock()
    defer d.mu.Unlock()

    old := d.lastConfig

    // Warn about static field changes
    if old != nil {
        if cfg.Listen != old.Listen {
            log.Printf("[reload] WARNING: 'listen' changed (%s → %s), requires restart", old.Listen, cfg.Listen)
        }
        if cfg.Mode != old.Mode {
            log.Printf("[reload] WARNING: 'mode' changed (%s → %s), requires restart", old.Mode, cfg.Mode)
        }
    }

    // Apply dynamic fields
    // log.level is always dynamic
    if cfg.Log.Level != "" {
        log.Printf("[reload] log.level: %s", cfg.Log.Level)
    }

    // If server URL changed and we're connected, trigger reconnect
    if old != nil && cfg.Server != old.Server && cfg.Server != "" && d.state == StateConnected {
        log.Printf("[reload] server changed, triggering reconnect")
        d.lastConfig = cfg
        // Reconnect will happen asynchronously
        go func() {
            d.opMu.Lock()
            defer d.opMu.Unlock()
            d.closeTunnel()
            d.doUpLocked(cfg, 0)
        }()
        return nil
    }

    d.lastConfig = cfg
    log.Printf("[reload] config reloaded successfully")
    return nil
}
```

**Step 3: Run tests**

```bash
cd k2 && go test ./daemon/... -v -run "TestReload"
```

**Step 4: Commit**

```bash
cd k2 && git add daemon/reload.go daemon/reload_test.go daemon/daemon.go
git commit -m "feat(daemon): add SIGHUP hot reload for dynamic config

Supports reloading: server, dns, rule, log.level.
Static fields (listen, mode, tun) log warning on change.
Invalid config rejected without affecting running state.

Part of: k2-cli-redesign F2"
```

### REFACTOR

- `[MUST]` Ensure `doUpLocked()` is extracted from existing `doUp()` (no double-lock)
- `[SHOULD]` Add metrics/logging for reload events (count, last-reload-time)

---

## T3: k2 Client CLI Rewrite

**Scope:** `k2/cmd/k2/` — complete rewrite of main dispatch, process lifecycle, signal control, ctl commands, service management, setup, and backward-compat aliases.

**Files:**
- Rewrite: `k2/cmd/k2/main.go` — flag-based dispatch (`-c`, `-t`, `-s`, `-v`, subcommands)
- Create: `k2/cmd/k2/process.go` — foreground daemon lifecycle (PID, signals, HTTP API)
- Create: `k2/cmd/k2/signal.go` — `-s stop|reload` implementation
- Create: `k2/cmd/k2/ctl.go` — `k2 ctl up|down|status` (IPC client)
- Create: `k2/cmd/k2/setup.go` — `k2 setup [URL|-c config]`
- Rewrite: `k2/cmd/k2/service.go` — `k2 service install|uninstall` dispatcher
- Modify: `k2/cmd/k2/service_darwin.go` — adapt to new interface
- Modify: `k2/cmd/k2/service_linux.go` — adapt to new interface
- Modify: `k2/cmd/k2/service_windows.go` — adapt to new interface
- Delete: `k2/cmd/k2/cmd_run.go` — replaced by process.go
- Delete: `k2/cmd/k2/cmd_up.go` — replaced by ctl.go
- Delete: `k2/cmd/k2/cmd_down.go` — replaced by ctl.go
- Delete: `k2/cmd/k2/cmd_status.go` — replaced by ctl.go
- Delete: `k2/cmd/k2/cmd_open.go` — removed or moved to ctl
- Keep: `k2/cmd/k2/cmd_upgrade.go` → rename to `upgrade.go`
- Keep: `k2/cmd/k2/cmd_version.go` → inline into main.go
- Modify: `k2/cmd/k2/client.demo.yml` — add `pid_file` and `include` fields

**depends_on:** F1, F2

### RED

Test function names (integration-style, in `main_test.go` or per-file):

- `TestMainDispatch_Version` — `k2 -v` prints version
- `TestMainDispatch_TestConfig` — `k2 -t` with valid config → success message
- `TestMainDispatch_TestConfig_Invalid` — `k2 -t` with bad config → error
- `TestSignalStop_SendsSIGTERM` — `-s stop` reads PID file, signals process
- `TestSignalReload_SendsSIGHUP` — `-s reload` reads PID file, signals process
- `TestSignalStop_NoPID` — `-s stop` with no PID file → clear error
- `TestCtlUp_URL` — `k2 ctl up k2v5://...` sends IPC up
- `TestCtlDown` — `k2 ctl down` sends IPC down
- `TestCtlStatus` — `k2 ctl status` sends IPC status
- `TestCtlDaemonNotRunning` — `k2 ctl status` when daemon not up → error
- `TestSetupWithURL` — `k2 setup k2v5://...` writes config + installs service
- `TestSetupIdempotent` — repeat setup → prompts about existing config
- `TestServiceInstall` — `k2 service install` creates platform service definition
- `TestServiceUninstall` — `k2 service uninstall` removes service definition
- `TestDeprecatedRunAlias` — `k2 run` prints warning + runs daemon
- `TestDeprecatedUpAlias` — `k2 up url` prints warning + runs ctl up

### GREEN

**Step 1: Rewrite main.go**

```go
package main

import (
    _ "embed"
    "flag"
    "fmt"
    "os"
)

//go:embed client.demo.yml
var demoConfig string

var (
    version = "dev"
    commit  = "none"
)

func main() {
    // Top-level flags (nginx-style)
    configPath := flag.String("c", "", "config file path")
    testConfig := flag.Bool("t", false, "test configuration and exit")
    signal     := flag.String("s", "", "send signal to running process: stop, reload")
    showVersion := flag.Bool("v", false, "show version")

    // Custom usage
    flag.Usage = printUsage
    flag.Parse()

    switch {
    case *showVersion:
        fmt.Printf("k2 %s (%s)\n", version, commit)
    case *testConfig:
        cmdTestConfig(*configPath)
    case *signal != "":
        cmdSignal(*signal, *configPath)
    default:
        // Check for subcommands in remaining args
        args := flag.Args()
        if len(args) > 0 {
            switch args[0] {
            case "ctl":
                cmdCtl(args[1:])
            case "setup":
                cmdSetup(args[1:])
            case "service":
                cmdService(args[1:])
            case "upgrade":
                cmdUpgrade(args[1:])
            case "demo-config":
                fmt.Print(demoConfig)
            // Deprecated aliases
            case "run":
                deprecatedWarn("run", "k2 (no subcommand)")
                runDaemon(*configPath)
            case "up":
                deprecatedWarn("up", "k2 ctl up")
                cmdCtl(append([]string{"up"}, args[1:]...))
            case "down":
                deprecatedWarn("down", "k2 ctl down")
                cmdCtl([]string{"down"})
            case "status":
                deprecatedWarn("status", "k2 ctl status")
                cmdCtl([]string{"status"})
            case "version":
                fmt.Printf("k2 %s (%s)\n", version, commit)
            case "help", "-h", "--help":
                printUsage()
            default:
                fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", args[0])
                printUsage()
                os.Exit(1)
            }
            return
        }
        // No subcommand = run daemon foreground
        runDaemon(*configPath)
    }
}

func deprecatedWarn(old, new string) {
    fmt.Fprintf(os.Stderr, "⚠ 'k2 %s' is deprecated, use '%s' instead.\n\n", old, new)
}

func printUsage() {
    fmt.Fprintf(os.Stderr, `k2 — Kaitu network tunnel

Usage:
  k2                             Start daemon (foreground, reads default config)
  k2 -c <config.yaml>           Start daemon with specific config
  k2 -t [-c <config>]           Test configuration and exit
  k2 -s stop|reload             Signal running daemon
  k2 -v                         Show version

  k2 ctl up [URL|config.yaml]   Connect VPN (IPC to daemon)
  k2 ctl down                   Disconnect VPN
  k2 ctl status                 Show connection status

  k2 setup [URL|-c config]      First-time setup (generate config + install service)
  k2 service install             Install system service
  k2 service uninstall           Uninstall system service
  k2 upgrade [--check]          Download and install latest version
  k2 demo-config                Print example config

`)
}
```

**Step 2: Implement process.go (foreground daemon)**

Key logic: load config (search chain or -c), validate, write PID, register SIGHUP/SIGTERM, start daemon HTTP API, block.

```go
func runDaemon(configPath string) {
    // 1. Load config
    cfg := loadConfigOrDefault(configPath)
    // 2. Validate
    if err := config.ValidateClient(cfg); err != nil {
        log.Fatalf("Config error: %v", err)
    }
    // 3. Check stale PID
    pidPath := resolvePIDPath(cfg)
    if pid, _ := config.CheckStale(pidPath); pid != 0 {
        log.Fatalf("Another k2 is running (PID %d). Stop it first: k2 -s stop", pid)
    }
    // 4. Write PID
    config.WritePID(pidPath)
    defer config.RemovePID(pidPath)
    // 5. Setup wintun (Windows)
    wintun.EnsureExtracted()
    daemon.ApplyResourceLimits()
    // 6. Create daemon
    d := daemon.New()
    d.ConfigPath = configPathUsed
    // 7. Register signals
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()
    sigCh := make(chan os.Signal, 1)
    signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
    go handleSignals(sigCh, d, cancel)
    // 8. Auto-connect if server configured
    if cfg.Server != "" {
        go d.AutoConnect(cfg)
    }
    // 9. Run HTTP API
    d.Run(ctx, cfg.Listen)
}
```

**Step 3: Implement signal.go**

```go
func cmdSignal(sig, configPath string) {
    pidPath := resolvePIDPathForSignal(configPath)
    pid, err := config.ReadPID(pidPath)
    if err != nil || pid == 0 {
        fmt.Fprintf(os.Stderr, "k2 is not running (no PID file at %s)\n", pidPath)
        os.Exit(1)
    }
    if !config.IsProcessAlive(pid) {
        config.RemovePID(pidPath)
        fmt.Fprintf(os.Stderr, "k2 is not running (stale PID %d)\n", pid)
        os.Exit(1)
    }
    process, _ := os.FindProcess(pid)
    switch sig {
    case "stop":
        process.Signal(syscall.SIGTERM)
        fmt.Printf("Stop signal sent to k2 (PID %d)\n", pid)
    case "reload":
        process.Signal(syscall.SIGHUP)
        fmt.Printf("Reload signal sent to k2 (PID %d)\n", pid)
    default:
        fmt.Fprintf(os.Stderr, "Unknown signal: %s (use stop or reload)\n", sig)
        os.Exit(1)
    }
}
```

**Step 4: Implement ctl.go**

Reuse `daemon.NewClient()` IPC. Same logic as current `cmd_up.go`/`cmd_down.go`/`cmd_status.go` but under `k2 ctl` namespace. No `ensureServiceRunning()` magic — if daemon not running, print error.

**Step 5: Implement setup.go**

```go
func cmdSetup(args []string) {
    // Parse: k2 setup [URL] [-c config]
    // 1. Generate config to standard path
    // 2. k2 -t validate
    // 3. k2 service install
    // 4. Start service
    // 5. Print next steps
}
```

**Step 6: Implement service.go**

```go
func cmdService(args []string) {
    if len(args) == 0 {
        fmt.Fprintln(os.Stderr, "Usage: k2 service install|uninstall")
        os.Exit(1)
    }
    switch args[0] {
    case "install":
        installService()
    case "uninstall":
        uninstallService()
    default:
        fmt.Fprintf(os.Stderr, "Unknown service command: %s\n", args[0])
        os.Exit(1)
    }
}
```

**Step 7: Migrate service_*.go files**

Rename functions from `installK2Service()` to `installService()` / `uninstallService()`. Update plist/unit templates to call `k2` (no `run --foreground`). Add `uninstallService()` to each platform file.

**Step 8: Delete old files, run tests**

```bash
cd k2 && rm cmd/k2/cmd_run.go cmd/k2/cmd_up.go cmd/k2/cmd_down.go cmd/k2/cmd_status.go cmd/k2/cmd_open.go cmd/k2/cmd_version.go
cd k2 && go test ./cmd/k2/... -v
```

**Step 9: Commit**

```bash
cd k2 && git add cmd/k2/
git commit -m "feat(cmd/k2): nginx-style CLI rewrite

- k2 = foreground daemon (no 'run' subcommand)
- k2 -t = config validation
- k2 -s stop|reload = signal control via PID file
- k2 ctl up|down|status = IPC client (replaces k2 up/down/status)
- k2 setup = first-time bootstrap
- k2 service install|uninstall = explicit service management
- Deprecated aliases: k2 run/up/down/status print warning + forward

Part of: k2-cli-redesign T3"
```

### REFACTOR

- `[MUST]` Ensure `ensureServiceRunning()` magic is completely removed from ctl.go
- `[MUST]` Service templates call `k2` not `k2 run --foreground`
- `[SHOULD]` Extract `loadConfigOrDefault()` as shared utility for process.go and test config

---

## T4: k2s Server CLI Rewrite

**Scope:** `k2/cmd/k2s/` — same pattern as T3 but for server binary.

**Files:**
- Rewrite: `k2/cmd/k2s/main.go` — flag-based dispatch
- Create: `k2/cmd/k2s/process.go` — foreground server lifecycle
- Create: `k2/cmd/k2s/signal.go` — `-s stop|reload`
- Create: `k2/cmd/k2s/setup.go` — `k2s setup` (auto-provision + service install)
- Create: `k2/cmd/k2s/service.go` — `k2s service install|uninstall` dispatcher
- Modify: `k2/cmd/k2s/service_darwin.go` — adapt, add uninstall
- Modify: `k2/cmd/k2s/service_linux.go` — adapt, add uninstall
- Modify: `k2/cmd/k2s/service_windows.go` — adapt, add uninstall
- Delete: `k2/cmd/k2s/cmd_run.go` — replaced by process.go + setup.go
- Delete: `k2/cmd/k2s/cmd_version.go` — inline into main.go
- Modify: `k2/cmd/k2s/server.demo.yml` — add `pid_file` and `include` fields

**depends_on:** F1, F2

### RED

Test function names:

- `TestK2sMainDispatch_Version` — `k2s -v` prints version
- `TestK2sMainDispatch_TestConfig` — `k2s -t` with valid config → success
- `TestK2sSignalStop` — `-s stop` signals running k2s
- `TestK2sSignalReload` — `-s reload` signals running k2s
- `TestK2sSetup` — `k2s setup` creates config + installs service + prints URL
- `TestK2sServiceInstall` — creates platform service definition
- `TestK2sServiceUninstall` — removes service definition
- `TestK2sDeprecatedRunAlias` — `k2s run` prints warning + runs server
- `TestK2sDeprecatedRunForeground` — `k2s run --foreground` prints warning + runs

### GREEN

**Step 1: Rewrite main.go** — same flag pattern as T3 (`-c`, `-t`, `-s`, `-v`), subcommands: `setup`, `service`, `demo-config`. Deprecated aliases: `run`.

**Step 2: Implement process.go** — load server config, validate, write PID, register signals, `server.New(cfg).Run(ctx)`.

**Step 3: Implement signal.go** — same pattern as T3.

**Step 4: Implement setup.go** — move `runSmart()` logic here, but as explicit `k2s setup`: auto-provision certs/ECH + write config + install service + print URL.

**Step 5: Implement service.go + platform files** — same pattern as T3, service name `k2s`.

**Step 6: Delete old files, run tests**

```bash
cd k2 && rm cmd/k2s/cmd_run.go cmd/k2s/cmd_version.go
cd k2 && go test ./cmd/k2s/... -v
```

**Step 7: Commit**

```bash
cd k2 && git add cmd/k2s/
git commit -m "feat(cmd/k2s): nginx-style CLI rewrite

- k2s = foreground server (no 'run' subcommand)
- k2s -t = config validation
- k2s -s stop|reload = signal control
- k2s setup = first-time bootstrap (replaces smart mode)
- k2s service install|uninstall = explicit service management
- Deprecated alias: k2s run prints warning + forwards

Part of: k2-cli-redesign T4"
```

### REFACTOR

- `[MUST]` Ensure smart mode auto-install is completely removed from default dispatch
- `[MUST]` `k2s setup` prints connect URL (same as old smart mode output)
- `[SHOULD]` Share signal.go logic between k2 and k2s (extract to internal package if >50% identical)

---

## T5: Desktop Tauri + NSIS Adaptation

**Scope:** `desktop/` and `webapp/` — update k2 command invocations to match new CLI interface.

**Files:**
- Modify: `desktop/src-tauri/src/service.rs:207-263` — change `run --install` → `service install`
- Modify: `desktop/src-tauri/installer-hooks.nsh` — change `svc up/down` → `service install/uninstall` + update comments
- Modify: `webapp/src/stores/vpn.store.ts` — update comment referencing `svc up`

**depends_on:** T3

### RED

**service.rs:**
- `test_admin_reinstall_uses_service_install` — verify command string contains "service install" not "run --install"

**NSIS:** Manual verification (NSIS tests are not automated).

### GREEN

**Step 1: Update service.rs macOS path**

Change `admin_reinstall_service_macos()`:
```rust
// Line ~213-214
let script = format!(
    r#"do shell script "{} service install" with administrator privileges"#,
    service_path
);
```

**Step 2: Update service.rs Windows path**

Change `admin_reinstall_service_windows()`:
```rust
// Line ~247-249
let ps_script = format!(
    r#"Start-Process -FilePath '{}' -ArgumentList 'service','install' -Verb RunAs -Wait -WindowStyle Hidden"#,
    service_path.display()
);
```

**Step 3: Update installer-hooks.nsh**

Replace all 3 occurrences:
- Line 80: `svc down` → `service uninstall`
- Line 131: `svc up` → `service install`
- Line 213: `svc down` → `service uninstall`

Update header comments (lines 4-13) to reference `service install/uninstall`.

**Step 4: Update vpn.store.ts comment**

Find the comment referencing `'svc up'` and update to `'service install'`.

**Step 5: Run Rust tests**

```bash
cd desktop/src-tauri && cargo test
```

**Step 6: Commit**

```bash
git add desktop/src-tauri/src/service.rs desktop/src-tauri/installer-hooks.nsh webapp/src/stores/vpn.store.ts
git commit -m "fix(desktop): update k2 command to new CLI interface

- service.rs: 'run --install' → 'service install'
- installer-hooks.nsh: 'svc up/down' → 'service install/uninstall'
- vpn.store.ts: update comment

Part of: k2-cli-redesign T5"
```

### REFACTOR

- `[SHOULD]` Verify NSIS hooks work on Windows test machine after k2 binary is updated
- `[SHOULD]` Consider extracting the k2 command path (`"/Applications/Kaitu.app/Contents/MacOS/k2"`) as a constant

---

## Execution Summary

| Task | Files | Parallel? | Estimated Scope |
|------|-------|-----------|-----------------|
| F1 | 9 new (config/) | — | search + validate + include + PID |
| F2 | 3 new/mod (daemon/) | after F1 | SIGHUP reload handler |
| T3 | ~12 new/mod/del (cmd/k2/) | after F1+F2, ‖ T4 | Full k2 CLI rewrite |
| T4 | ~10 new/mod/del (cmd/k2s/) | after F1+F2, ‖ T3 | Full k2s CLI rewrite |
| T5 | 3 mod (desktop/) | after T3 | Command string updates |

**Critical path:** F1 → F2 → T3 → T5 (longest chain)
**Parallel opportunity:** T3 ‖ T4 (different directories)

**Merge order:** F1 → F2 → (T3 ‖ T4) → T5
