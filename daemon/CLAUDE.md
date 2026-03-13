# Daemon Layer (daemon/)

HTTP API on `127.0.0.1:1777` + SSE push. Auto-installed as system service.
Implements `engine.EventHandler` — receives state from engine, not the other way around.

## State Model

Engine is single source of truth. Daemon stores `lastStatus engine.Status` updated by engine's `OnStatus` callback.
No independent state machine — daemon derives everything from engine events.

State values: `disconnected`, `connecting`, `connected`, `reconnecting`, `paused`

## Concurrency Model

- `opMu deadlock.Mutex` — serializes doUp/doDown (coarse lifecycle lock)
- `abortUp atomic.Bool` — doDown sets before opMu.Lock; doUp checks after Start
- `mu deadlock.RWMutex` — protects field access (fine-grained data lock)
- `sseMu deadlock.RWMutex` — protects SSE client map
- `closeTunnel()` — idempotent teardown, caller must hold opMu

## API

### POST /api/core

| Action | Params | Description |
|--------|--------|-------------|
| `up` | `config: ClientConfig`, optional `pid` | Connect tunnel |
| `down` | — | Disconnect |
| `status` | — | State + structured error + active config |
| `version` | — | Version info |

### GET /api/events — SSE

Real-time push. Two event types:
- `event: status` — full `engine.Status` snapshot (idempotent)
- `event: stats` — 1Hz `engine.HealthSnapshot` (wire metrics + health level)

Heartbeat every 15s. Buffered per client (cap 8), slow clients get dropped.

### POST /api/log-level

Hot-switch global slog log level at runtime. Body: `{"level": "debug"}`.
Valid levels: `debug`, `info`, `warn`, `error`. Uses `config.SetLogLevel()` (LevelVar).

### POST /api/helper

Android ADB install helper. Separate endpoint from `/api/core`.

| Action | Params | Description |
|--------|--------|-------------|
| `adb-detect` | — | Prepare adb + list connected Android devices (serial, model, state) |
| `adb-install` | `serial`, `url` (optional) | Push + install APK to device. Async — poll `adb-status` |
| `adb-status` | — | Current install state: phase/progress/version/error |

Phases: `idle` → `prepare_adb` → `downloading` → `pushing` → `installing` → `done` | `error`

**adb discovery** (`findAdbCandidates`): When daemon runs as root, `$PATH` differs from user. Searches: `exec.LookPath`, Android SDK in all `/Users/*/`, Homebrew paths. Falls back to CDN download from `tools/tools.json`.

**Webapp routing**: Tauri bridge routes `adb-*` actions to Rust IPC `daemon_helper_exec`. Standalone bridge routes to `/api/helper` (not `/api/core`).

### Other

- `GET /ping` — Health check (returns `{"code":0,"message":"pong"}`)
- `GET /metrics` — goroutines, heap, GC stats
- `GET /api/stats` — Ring buffer of last 300 health snapshots (5 min, oldest-first)
- `GET /stats` — Embedded HTML dashboard (dark theme, 4 Canvas charts, SSE live updates)

Request/Response: `daemon.Request{Action, Params}` / `daemon.Response{Code, Message, Data}`

## Tunnel Lifecycle (doUp/doDown)

**doUp(cfg *ClientConfig, pid int)**:
1. `opMu.Lock()` — serialize with other doUp/doDown
2. `abortUp.Store(false)` — reset on new up
3. `closeTunnel()` — unconditional teardown (engine.Stop fires OnStatus(disconnected))
4. Network monitor + directdial setup
5. `engineConfigFromClientConfig(cfg)` — assemble `engine.Config` with platform fields
6. `eng := engine.New()` + `eng.SetEventHandler(d)` — daemon receives events from this point
7. **Early abort check**: if `abortUp.Load()` → return error (doDown arrived during setup)
8. `d.StartFunc(eng, cfg)` — engine fires OnStatus(connecting), then OnStatus(connected) or error
9. **Post-start abort check**: if `abortUp.Load()` → `eng.Stop()` + return error (doDown arrived during Start)
10. Install engine, start PID monitor, persist state for auto-reconnect

Error path: engine's fail() fires OnStatus(disconnected+error) → daemon's lastStatus updated automatically.
Abort path: both Start and Stop happen within the same opMu critical section — route atomicity guaranteed.

**doDown()**:
1. `abortUp.Store(true)` — signal before acquiring lock (doUp can detect pending down)
2. `opMu.Lock()` — serialize (blocks until doUp releases)
3. `abortUp.Store(false)` — reset after acquiring
4. `closeTunnel()` — `engine.Stop()` fires OnStatus(disconnected)

## Files

- `daemon.go` — `Daemon` struct, `OnStatus`/`OnStats` (EventHandler impl), HTTP server setup, `engineConfigFromClientConfig`
- `sse.go` — `handleEvents()` SSE endpoint, `broadcastStatus()` + `broadcastSnapshot()` fan-out
- `api.go` — `handleCore`/`handlePing`/`handleMetrics`/`handleVersion`/`handleStats` + CORS middleware
- `stats_embed.go` — `go:embed stats.html`, serves `/stats` dashboard
- `stats.html` — Embedded dark-themed stats dashboard (vanilla JS, Canvas charts, SSE live)
- `client.go` — `Client` struct for IPC (used by CLI commands)
- `state.go` — Persisted state: saved config + last connect time (auto-reconnect <1h)
- `reload.go` — Hot-reload config on SIGHUP (dynamic changes without restart)
- `network_monitor.go` — Desktop network change detection (WiFi/cellular switch)
- `process.go` / `process_unix.go` / `process_windows.go` — PID monitoring (auto-disconnect on parent exit)
- `recovery.go` / `recovery_unix.go` / `recovery_windows.go` — Panic recovery + network restore (Windows: calls `provider.CleanupStaleDNSOverride()`)
- `rlimit.go` / `rlimit_unix.go` / `rlimit_windows.go` — `RLIMIT_NPROC=4000`, `RLIMIT_NOFILE=10240`
- `service.go` — `ServiceManager` interface: Install/Uninstall/IsInstalled
- `service_darwin.go` — macOS launchd plist (`io.kaitu.k2`)
- `service_linux.go` — Linux systemd unit (`k2.service`) + OpenWRT init.d/procd (auto-detected)
- `service_windows.go` — Windows Service
- `directdial.go` / `directdial_test.go` — `directDialer` (implements `wire.DirectDialer`, sing-tun interface binding)
- `network_monitor.go` / `network_monitor_test.go` — Desktop network change detection (WiFi/cellular switch)
- `paths.go` / `paths_test.go` — `cacheDir()`, `logDir()` (platform-specific cache/log directories)
- `slog_migration_test.go` — Guard: no old `log.Printf` calls in daemon package
- `wintun/` — wintun.dll embedding (amd64 + arm64) + runtime arch detection via `IsWow64Process2`
- `helper.go` — `/api/helper` HTTP handler, routes adb-detect/adb-install/adb-status
- `helper_adb.go` — `AndroidHelper`: adb binary management (system discovery + CDN download), gadb device detection, APK download + push + install
- `helper_test.go` — Tests for helper API, install phases, APK URL resolution

## Testing

```bash
go test ./daemon/...    # Tests for api, state, process, rlimit, abort
```
