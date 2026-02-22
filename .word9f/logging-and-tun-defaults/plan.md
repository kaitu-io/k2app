# Plan: Logging Implementation & TUN Device Defaults

## Meta

| Field | Value |
|-------|-------|
| Feature | logging-and-tun-defaults |
| Spec | k2/docs/features/logging-and-tun-defaults/spec.md |
| Date | 2026-02-22 |
| Complexity | complex |

## AC Mapping

| AC | Test | Task |
|----|------|------|
| AC1: Auto-detected log file exists | TestDefaultLogPath_Root, TestDefaultLogPath_User, TestSetupLogging_CreatesFile | T1 |
| AC2: Config log.output override | TestSetupLogging_StderrOverride, TestSetupLogging_StdoutOverride, TestSetupLogging_FileOverride | T1 |
| AC3: Log rotation at 50MB | TestSetupLogging_LumberjackConfig | T1 |
| AC4: Log level filtering | TestSetupLogging_LevelDebug, TestSetupLogging_LevelError | T1 |
| AC5: Zero log.Printf in k2 | grep verification | T3, T4, T5, T6, T9 |
| AC5: Zero log.Printf in sidecar | grep verification | T8 |
| AC6: TUN connects on macOS | TestDefaultTunName_Darwin, TestDefaultTunName_Linux, TestDefaultTunName_Windows | T2 |
| AC7: Service plist no log path | TestPlistTemplate_NoLogPath | T7 |
| AC8: k2s same logging pattern | TestSetupLogging_K2sName | T1, T6 |

## Foundation Tasks

### T1: Logging infrastructure (`config/log.go`)

**Scope**: Create `SetupLogging()` and `DefaultLogPath()` in `config/` package. Add lumberjack dependency. Wire slog handler with level filtering and file/stderr/stdout output selection.
**Files**:
- `k2/config/log.go` (new)
- `k2/config/log_unix.go` (new — `os.Geteuid()` privilege detection, build tag `!windows`)
- `k2/config/log_windows.go` (new — Windows admin detection)
- `k2/config/log_test.go` (new)
- `k2/go.mod` (add lumberjack dependency)
- `k2/go.sum` (auto-updated)
**Depends on**: none
**TDD**:
- RED: Write failing tests for path resolution and logging setup
  - `TestDefaultLogPath_Root` — root → `/var/log/kaitu/{name}.log`
  - `TestDefaultLogPath_User` — non-root → `~/Library/Logs/kaitu/{name}.log` (darwin)
  - `TestSetupLogging_Stderr` — `output: stderr` → no file created
  - `TestSetupLogging_Stdout` — `output: stdout` → writes to stdout
  - `TestSetupLogging_FileOverride` — `output: /tmp/test.log` → file created
  - `TestSetupLogging_LevelDebug` — level=debug → debug messages pass
  - `TestSetupLogging_LevelError` — level=error → info messages filtered
  - `TestSetupLogging_LumberjackConfig` — file output uses lumberjack with 50MB/3/30d
  - `TestSetupLogging_K2sName` — name="k2s" → path ends in `k2s.log`
- GREEN: Implement `DefaultLogPath()`, `SetupLogging()`, `parseLevel()`
- REFACTOR:
  - [MUST] Export `SetupLogging(cfg LogConfig, name string)` as the public API
  - [SHOULD] Add godoc comments

**Acceptance**: `go test ./config/...` passes. `SetupLogging()` correctly configures slog global default.

### T2: TUN device name defaults

**Scope**: Fix "bad tun name: " error by setting platform-appropriate default TUN names.
**Files**:
- `k2/provider/tun_desktop.go` (modify — add `defaultTunName()`, use in `tunOpts.Name`)
- `k2/provider/tun_desktop_test.go` (new)
**Depends on**: none
**TDD**:
- RED: Write failing tests for default TUN name
  - `TestDefaultTunName` — returns non-empty string on current platform
- GREEN: Add `defaultTunName()` with platform switch, use in `Start()`
- REFACTOR:
  - [SHOULD] Add comment explaining sing-tun v0.7.11 requirement

**Acceptance**: `go test ./provider/...` passes. `tun.Options.Name` is never empty.

## Feature Tasks

### T3: slog migration — daemon/

**Scope**: Replace all `log.Printf`/`log.Println`/`log.Fatalf` with `slog` in the daemon package (40+ calls across 9 files). Change `import "log"` to `import "log/slog"`.
**Files**:
- `k2/daemon/daemon.go` (12 calls)
- `k2/daemon/reload.go` (17 calls)
- `k2/daemon/state.go` (3 calls)
- `k2/daemon/process.go` (3 calls)
- `k2/daemon/recovery.go` (3 calls)
- `k2/daemon/recovery_unix.go` (1 call)
- `k2/daemon/recovery_windows.go` (1 call)
- `k2/daemon/rlimit.go` (2 calls)
- `k2/daemon/network_monitor.go` (1 call)
**Depends on**: [T1]
**TDD**:
- RED: `grep -r 'log\.Printf\|log\.Println\|log\.Fatalf' k2/daemon/` must return zero matches after migration
- GREEN: Mechanical replacement:
  - `log.Printf("format", args...)` → `slog.Info("message", "key", value, ...)`
  - `log.Fatalf(...)` → `slog.Error(...); os.Exit(1)` (or preserve fatal semantics)
  - `log.Println(...)` → `slog.Info("message")`
  - Map severity: state transitions → Info, errors → Error, debug detail → Debug
- REFACTOR:
  - [MUST] Ensure no `import "log"` remains in any daemon/ file
  - [SHOULD] Use structured key-value pairs where natural (e.g., `"addr", listenAddr`)

**Acceptance**: `go build ./daemon/...` compiles. `grep 'import "log"' k2/daemon/*.go` returns nothing.

### T4: slog migration — engine/ + core/ + wire/

**Scope**: Replace log calls in tunnel infrastructure packages.
**Files**:
- `k2/engine/engine.go` (3 calls)
- `k2/core/tunnel.go` (5 calls)
- `k2/core/dns/direct.go` (3 calls)
- `k2/core/dns/middleware.go` (10 calls)
- `k2/core/dns/proxy.go` (2 calls)
- `k2/wire/quic.go` (1 call)
- `k2/wire/ech.go` (1 call)
**Depends on**: [T1]
**TDD**:
- RED: `grep -r 'log\.Printf\|log\.Println\|log\.Fatalf' k2/engine/ k2/core/ k2/wire/` must return zero
- GREEN: Mechanical replacement with appropriate slog levels:
  - DNS middleware debug → `slog.Debug()`
  - Engine state → `slog.Info()`
  - Wire transport errors → `slog.Warn()` or `slog.Error()`
- REFACTOR:
  - [MUST] Ensure no `import "log"` remains
  - [SHOULD] DNS middleware: use `slog.Debug()` for per-query logging (high frequency)

**Acceptance**: `go build ./engine/... ./core/... ./wire/...` compiles. Zero `import "log"`.

### T5: slog migration — server/

**Scope**: Replace log calls in server package.
**Files**:
- `k2/server/server.go` (22 calls)
- `k2/server/handler.go` (5 calls)
**Depends on**: [T1]
**TDD**:
- RED: `grep -r 'log\.Printf\|log\.Println\|log\.Fatalf' k2/server/` must return zero
- GREEN: Mechanical replacement:
  - Connection lifecycle → `slog.Info()`
  - Cert provisioning → `slog.Info()`
  - Errors → `slog.Error()`
- REFACTOR:
  - [MUST] Ensure no `import "log"` remains
  - [SHOULD] Add connection context (remote addr) as structured fields

**Acceptance**: `go build ./server/...` compiles.

### T6: slog migration — cmd/k2/ + cmd/k2s/ + integrate SetupLogging

**Scope**: Migrate CLI entry points, call `SetupLogging()` at daemon/server startup. This is the integration point where logging config becomes active.
**Files**:
- `k2/cmd/k2/process.go` (13 calls + add `config.SetupLogging()` call)
- `k2/cmd/k2s/process.go` (11 calls + add `config.SetupLogging()` call)
- `k2/cmd/k2/main_test.go` (has `import "log"`)
**Depends on**: [T1, T3, T4, T5]
**TDD**:
- RED: `grep -r 'log\.Printf\|log\.Println\|log\.Fatalf' k2/cmd/` must return zero
- GREEN:
  - Replace log calls in both process.go files
  - Add `config.SetupLogging(cfg.Log, "k2")` in `runDaemon()` after config load
  - Add `config.SetupLogging(cfg.Log, "k2s")` in `runServer()` after config load
  - For pre-config logging (before config is loaded): `SetupLogging(LogConfig{}, "k2")` (uses defaults)
- REFACTOR:
  - [MUST] Two-phase init: call `SetupLogging` with defaults first, then again with loaded config
  - [MUST] Ensure no `import "log"` remains in cmd/
  - [SHOULD] Update demo configs to document `log.output` / `log.level`

**Acceptance**: `go build ./cmd/...` compiles. Full `go test ./...` passes.

### T7: Service file cleanup

**Scope**: Remove plist `StandardOutPath`/`StandardErrorPath` and `logDir` from macOS service files. Go-level logging now handles file output.
**Files**:
- `k2/daemon/service_darwin.go` (remove logDir, StandardOutPath, StandardErrorPath from plist)
- `k2/cmd/k2s/service_darwin.go` (same cleanup)
**Depends on**: [T6]
**TDD**:
- RED: Verify plist template no longer contains `StandardOutPath` or `StandardErrorPath`
  - `TestInstallService_PlistNoLogRedirect` (or manual grep of generated plist)
- GREEN: Remove `logDir` variable, `os.MkdirAll(logDir, ...)`, and the two plist keys from template
- REFACTOR:
  - [SHOULD] Clean up unused `logDir` references

**Acceptance**: `go build ./daemon/... ./cmd/k2s/...` compiles. Plist template has no log path entries.

### T8: slog migration — docker/sidecar (separate module)

**Scope**: Migrate docker/sidecar from `log` to `slog`. Separate Go module, separate commit. No lumberjack needed (Docker captures stdout). Just slog for structured + leveled output.
**Files**:
- `docker/sidecar/main.go` (42 calls)
- `docker/sidecar/sidecar/node.go` (38 calls)
- `docker/sidecar/config/config.go` (27 calls)
- `docker/sidecar/sidecar/selfcert.go` (6 calls)
- `docker/sidecar/sidecar/traffic.go` (5 calls)
- `docker/sidecar/sidecar/collector.go` (7 calls)
- `docker/sidecar/sidecar/connect_url.go` (1 call)
**Depends on**: none (separate Go module, no code dependency on T1)
**TDD**:
- RED: `grep -r 'log\.Printf\|log\.Println\|log\.Fatalf' docker/sidecar/` must return zero
- GREEN: Mechanical replacement. Add `slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))` in `main()`.
  - `[Sidecar]` prefix → structured field `"component", "sidecar"` or just drop prefix
  - `[RADIUS]` prefix → `"component", "radius"`
  - `log.Fatalf` → `slog.Error(...)` + `os.Exit(1)`
- REFACTOR:
  - [MUST] Ensure no `import "log"` remains
  - [SHOULD] Use component-scoped loggers: `slog.With("component", "sidecar")`

**Acceptance**: `cd docker/sidecar && go build ./...` compiles.

### T9: slog migration — docker tools

**Scope**: Migrate small Docker utility tools.
**Files**:
- `k2/docker/gen-config/main.go` (3 calls)
- `k2/docker/bench/gen-bench-config/main.go` (4 calls)
**Depends on**: none
**TDD**:
- RED: Zero `log.Printf` remaining in these files
- GREEN: Mechanical replacement
- REFACTOR:
  - [SHOULD] Minimal — these are small tools

**Acceptance**: `go build` compiles for both tools.

## Execution Graph

```
T1 (logging infra) ──┬──→ T3 (daemon/) ──┐
                     ├──→ T4 (engine/)    ├──→ T6 (cmd/ + integrate) ──→ T7 (service cleanup)
                     └──→ T5 (server/)  ──┘
T2 (TUN defaults) ── (independent)
T8 (sidecar)       ── (independent, separate module)
T9 (docker tools)  ── (independent)
```

Parallel lanes:
- **Lane A**: T1 → T3 + T4 + T5 (parallel) → T6 → T7
- **Lane B**: T2 (independent)
- **Lane C**: T8 (independent)
- **Lane D**: T9 (independent)

T2, T8, T9 can execute in parallel with Lane A from the start.
