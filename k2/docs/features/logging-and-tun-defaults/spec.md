# Feature: Logging Implementation & TUN Device Defaults

## Meta

| Field   | Value                          |
|---------|--------------------------------|
| Feature | logging-and-tun-defaults       |
| Version | v1                             |
| Status  | implemented                    |
| Created | 2026-02-22                     |
| Updated | 2026-02-22                     |

## Version History

| Version | Date       | Summary                                          |
|---------|------------|--------------------------------------------------|
| v1      | 2026-02-22 | Initial: implement LogConfig + TUN name defaults |

## Background

Two issues found from production daemon logs (`/tmp/k2/k2.log`):

1. **LogConfig is dead code** — `LogConfig.Level` and `LogConfig.Output` are parsed and validated in `config/config.go` + `config/validate.go` but never applied. All `log.Printf()` calls go unconditionally to stderr. macOS gets file output via plist `StandardOutPath` redirect to `/tmp/k2/`, Windows and Linux lose all logs.

2. **TUN device name bug** — `provider/tun_desktop.go` passes `Name: ""` to sing-tun. On macOS, sing-tun v0.7.11 (`tun_darwin.go:96`) requires Name matching `"utun%d"` format when `FileDescriptor == 0`. Empty name → `bad tun name: ` error on every connect attempt. The provider's `FileDescriptor` field defaults to Go zero value (0), which sing-tun interprets as "create by name" mode.

## Product Requirements

- PR1: Daemon always produces a log file regardless of how it's launched (service, foreground, config or no config) (v1)
- PR2: Log file location is sensible per platform and privilege level, with no user configuration required (v1)
- PR3: Log files rotate automatically to prevent unbounded disk growth (v1)
- PR4: Config `log.output` overrides auto-detected path when explicitly set (v1)
- PR5: Config `log.level` filters log output at runtime (v1)
- PR6: TUN mode connects successfully on macOS without explicit TUN name configuration (v1)
- PR7: k2s (server) follows the same logging conventions as k2 (daemon) (v1)

## Technical Decisions

### TD1: Auto-detect privilege for log directory (v1)

Priority chain:
1. Config `log.output` (explicit path) → use that
2. `os.Geteuid() == 0` on Unix / admin check on Windows → platform system directory
3. Non-root → platform user directory

| Platform | Root/Service               | User                                    |
|----------|----------------------------|-----------------------------------------|
| macOS    | `/var/log/kaitu/k2.log`    | `~/Library/Logs/kaitu/k2.log`           |
| Linux    | `/var/log/kaitu/k2.log`    | `~/.local/share/kaitu/logs/k2.log`      |
| Windows  | `%ProgramData%\kaitu\k2.log` | `%LOCALAPPDATA%\kaitu\logs\k2.log`   |

k2s uses the same paths but with `k2s.log` filename.

Rationale: The daemon is typically launched as a service (root) by Kaitu app. But developers also run `k2` or `sudo k2` directly. Auto-detection ensures logs always land somewhere writable without requiring a config file.

### TD2: Lumberjack for log rotation (v1)

All file output uses `gopkg.in/natefinanz/lumberjack.v2`:
- MaxSize: 50 MB
- MaxBackups: 3
- MaxAge: 30 days

Rationale: Cross-platform, zero-config rotation. Avoids depending on platform tools (newsyslog, logrotate) that may not be configured.

### TD3: Full migration to Go slog (v1, brainstorm-decided)

Complete replacement of `log.Printf()` with Go 1.21+ `log/slog` across the entire k2 codebase:

- **All** `log.Printf()` / `log.Println()` / `log.Fatalf()` calls replaced with `slog.Info()` / `slog.Debug()` / `slog.Warn()` / `slog.Error()`
- Default level: `info` (matches current `LogConfig` default)
- Config `log.level` maps to `slog.Level`
- Output handler: `slog.NewTextHandler(writer)` — plain text, compatible with existing log format
- `slog.SetDefault()` called at daemon/server startup so all packages use the configured handler
- Migration is full — no `log.Printf()` remains in the codebase after this feature

Scope of migration — k2 repo (126 calls across 22 files):
- `daemon/` (12 files) — daemon lifecycle, state transitions, API, recovery, reload, network monitor, rlimit
- `engine/` — tunnel startup, k2rule init, provider creation
- `core/` — DNS direct/proxy/middleware, tunnel
- `wire/` — QUIC/TCP transport, ECH
- `server/` — cert provisioning, ECH rotation, connection lifecycle, handler
- `cmd/k2/` — process startup, PID management, signal handling, setup, upgrade, ctl
- `cmd/k2s/` — server process startup, setup, signal handling
- `docker/gen-config/` — config generation tool
- `docker/bench/` — benchmark config tool

Scope of migration — docker/sidecar (separate Go module, 126 calls across 7 files):
- `main.go` — sidecar lifecycle, config generation, RADIUS proxy
- `sidecar/` — node registration, certificate management, traffic collection, self-cert
- `config/` — config loading

Note: `docker/sidecar/` is a separate Go module (`github.com/kaitu-io/k2-sidecar`), not part of the k2 module. Migration there follows the same pattern but is a separate commit/PR.

Rationale: `log/slog` is stdlib (no dependency), supports leveled logging natively, and the handler pattern makes it easy to swap output targets. Full migration avoids a split codebase where some paths use `log` and others use `slog`.

### TD4: Remove service-layer log redirection (v1)

Since Go now handles file output directly:
- **macOS plist**: Remove `StandardOutPath` / `StandardErrorPath` (Go handles it via lumberjack)
- **Linux systemd**: No `StandardOutput` needed (Go handles it)
- **Windows sc**: No change (never had redirection)

Rationale: Single source of truth for log output. No split between service-layer and Go-layer. Panic recovery (`recovery.go`) also benefits from the same log file.

### TD5: TUN device name platform defaults (v1)

Set platform-appropriate default name in `provider/tun_desktop.go`:

| Platform | Default Name | Rationale                                     |
|----------|-------------|-----------------------------------------------|
| macOS    | `utun0`     | sing-tun parses `"utun%d"` → ifIndex=0 → kernel auto-allocates next available utun |
| Linux    | `tun0`      | Standard TUN device name convention            |
| Windows  | `sing-tun`  | Wintun adapter display name                    |

Rationale: sing-tun v0.7.11 requires a valid name when `FileDescriptor == 0` (desktop create-by-name mode). Empty string causes `fmt.Sscanf("", "utun%d")` to fail. Setting `utun0` with ifIndex=0 makes the kernel allocate the next available utun device (standard macOS behavior — same as WireGuard).

## Acceptance Criteria

### AC1: Auto-detected log file exists after daemon start (v1)
- Start daemon as root → log file created at `/var/log/kaitu/k2.log`
- Start daemon as user → log file created at `~/Library/Logs/kaitu/k2.log` (macOS)
- Log directory created automatically if missing
- All `slog` output appears in the file

### AC2: Config log.output override works (v1)
- Set `log.output: /tmp/test-k2.log` in config → logs go to that path
- Set `log.output: stderr` → logs go to stderr (no file)
- Set `log.output: stdout` → logs go to stdout

### AC3: Log rotation happens at 50MB (v1)
- Log file rotated when exceeding 50MB
- At most 3 backup files retained
- Backups older than 30 days removed

### AC4: Log level filtering works (v1)
- Set `log.level: error` → only error messages appear
- Set `log.level: debug` → all messages appear
- Default (`info`) → debug messages suppressed

### AC5: Zero log.Printf remaining in k2 codebase (v1)
- `grep -r 'log\.Printf\|log\.Println\|log\.Fatalf' --include='*.go' k2/` returns zero matches (excluding vendored/test code)
- All logging uses `slog.Debug()` / `slog.Info()` / `slog.Warn()` / `slog.Error()`
- No import of `"log"` package remains (only `"log/slog"`)
- `docker/sidecar/` also migrated to slog (separate Go module, same pattern)

### AC6: TUN connects on macOS without config (v1)
- `k2 ctl up <url>` succeeds on macOS (no "bad tun name" error)
- TUN device created with auto-assigned utun name (e.g., utun3)
- Provider reports correct device name after start

### AC7: Service plist no longer contains log path (v1)
- `k2 service install` generates plist without `StandardOutPath`/`StandardErrorPath`
- Daemon still produces log file (Go-level output)

### AC8: k2s follows same logging pattern (v1)
- k2s server uses the same `setupLogging()` function
- Log file at `/var/log/kaitu/k2s.log` (root) or user-equivalent

## Testing Strategy

- Unit tests for `DefaultLogPath()`: mock `os.Geteuid()` / `runtime.GOOS`, verify correct paths (v1)
- Unit tests for `setupLogging()`: verify lumberjack writer created for file paths, stderr for default (v1)
- Unit test for `defaultTunName()`: verify per-platform return values (v1)
- Integration: `go build ./...` compiles across platforms (v1)
- Manual: `make dev` → connect → verify log file and no TUN error (v1)

## Deployment & CI/CD

- Changes in k2 submodule — PR to k2 repo (v1)
- After merge: update k2 submodule ref in k2app (v1)
- Existing users: next `k2 service install` picks up new log path. Old `/tmp/k2/k2.log` can be manually cleaned (v1)

## Non-Goals

- Structured JSON logging (plain text is sufficient for daemon logs)
- Remote log shipping (syslog, CloudWatch, etc.)
- Web UI for viewing logs
- Log compression (lumberjack doesn't compress by default, acceptable)
