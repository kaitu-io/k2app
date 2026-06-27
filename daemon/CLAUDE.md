# Daemon Layer (daemon/)

HTTP API on `127.0.0.1:1777` + SSE push. Auto-installed as system service.
Implements `engine.EventHandler` — receives state from engine, not the other way around.

## State Model

Engine is single source of truth. Daemon stores `lastStatus engine.Status` updated by engine's `OnStatus` callback.
No independent state machine — daemon derives everything from engine events.

State values: `disconnected`, `connecting`, `connected`, `reconnecting`, `paused`

## External Watchdog (macOS)

launchd companion service (`kaitu-watchdog.plist`) runs `k2 healthcheck` every 60s.
Detects daemon hangs by pinging `/ping` with 5s timeout. After 2 consecutive
failures (120s), forces DNS cleanup + SIGKILL. launchd KeepAlive restarts daemon.

Why external (not internal watchdog):
- Fresh process — immune to daemon deadlocks (no shared mutexes or CGo state)
- DNS cleanup via fresh CGo context (SCDynamicStore calls go through configd, not the hung process)
- Follows systemd WatchdogSec / Kubernetes liveness probe pattern

Installed/uninstalled alongside daemon service by `k2 service install/uninstall`.

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
| `relay-fetch` | `RelayRequest`(ip/pin/ech/centerHost/method/path/headers/body)| Relay one control-plane HTTP request to Center through a node (antiblock Phase 2). Stateless — works without an active tunnel. |

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

### Linux-only webui routes (via k2/webui package)

On Linux desktop builds (`cmd/k2`) the daemon ships the React webapp
inline and adds four endpoints shared with `cmd/k2r`. These routes are
a no-op on macOS and Windows (Tauri provides the UI on those platforms).

- `GET /` — Serves the embedded SPA with `window.__K2_GATEWAY__` injected
  (version, commit, arch). Extension-less paths fall through to index.html
  for React Router; asset paths (`.js`/`.css`/`.png`/…) are cache-control
  `immutable, max-age=31536000`.
- `POST /api/storage` — JSON envelope with actions `get`/`set`/`remove`/
  `has`/`keys`/`clear`. Values are AES-256-GCM encrypted with a key
  derived from `/etc/machine-id` via HKDF-SHA256 (same scheme as
  `desktop/src-tauri`, so the storage is drop-in replaceable if the
  binary is ever upgraded atop an existing Tauri install).
- `GET /api/platform` — Returns `{os, platformType, version, commit,
  arch}`; `platformType="desktop"` for cmd/k2 vs `"gateway"` for cmd/k2r.
- `POST /api/upgrade` — Actions `check` (returns `{current, latest}`)
  and `apply` (background download + verify + replace + restart).
  Crash-safe: persists `/etc/kaitu/update-state.json` at each stage and
  rolls back on interrupted replace. `apply` calls
  `systemctl restart kaitu` to pick up the new binary.

See `k2/webui/CLAUDE.md` for the shared implementation. The daemon's
`registerWebUI(mux)` lives in `daemon/webui_linux.go`; `webui_nop.go`
is a stub for macOS/Windows builds so the caller in `Run()` is
unconditional.

Request/Response: `daemon.Request{Action, Params}` / `daemon.Response{Code, Message, Data}`

## Tunnel Lifecycle (doUp/doDown)

**doUp(cfg *ClientConfig, pid int)**:
1. `opMu.Lock()` — serialize with other doUp/doDown
2. `abortUp.Store(false)` — reset on new up
3. **Pre-fire `OnStatus(connecting)` + broadcast** — gives external consumers a monotonic connecting → connected/error view across the whole doUp, even though closeTunnel internally fires disconnected
4. **`inDoUpTeardown.Store(true)` → `closeTunnel()` → `inDoUpTeardown.Store(false)`** — the OnStatus filter (see below) drops the engine.Stop's `OnStatus(disconnected, Error=nil)` so SSE/polling consumers never see the teardown noise. Real failures (Error != nil) still propagate.
5. Network monitor + directdial setup
6. `engineConfigFromClientConfig(cfg)` — assemble `engine.Config` with platform fields
7. `eng := engine.New()` + `eng.SetEventHandler(d)` — daemon receives events from this point
8. **Early abort check**: if `abortUp.Load()` → return error (doDown arrived during setup)
9. `d.StartFunc(eng, cfg)` — engine fires OnStatus(connecting), then OnStatus(connected) or error
10. **Post-start abort check**: if `abortUp.Load()` → `eng.Stop()` + return error (doDown arrived during Start)
11. Install engine, start PID monitor, persist state for auto-reconnect

**OnStatus teardown filter** (added 2026-05-19): `OnStatus(disconnected)` with `Error == nil` is dropped while `inDoUpTeardown > 0`. This narrowly suppresses the engine.Stop noise emitted from closeTunnel during an engine swap; classified failures (Error != nil) are never suppressed. Without this filter, webapp consumers observe `connected → disconnected → connecting → connected` and the vpn-machine state briefly transitions through `idle` and `reconnecting`, causing visible UI flicker ("重连中") on every non-first connect.

`inDoUpTeardown` is an `atomic.Int32` counter, not a bool: under concurrent doUps a single-bool flag would be stomped by one call's deferred `Store(false)` while a peer still needs it set across its own closeTunnel. The counter is incremented before `opMu.Lock` (covers the wait window where an in-flight `doDown`'s closeTunnel might fire `OnStatus(disconnected)`) and decremented immediately after `closeTunnel` returns (so the post-start abort path's `eng.Stop` propagates normally). A deferred decrement guarded by `armed = true` is the safety net for early returns / panics. See `TestDoUp_SuppressesTeardownDisconnectedExternally` and `TestTeardownFilter_CounterSemantics` in `daemon_test.go`.

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
- `recovery.go` / `recovery_darwin.go` / `recovery_linux.go` / `recovery_windows.go` — Panic recovery + network restore (macOS + Windows: calls `provider.CleanupStaleDNSOverride()`)
- `rlimit.go` / `rlimit_unix.go` / `rlimit_windows.go` — `RLIMIT_NPROC=4000`, `RLIMIT_NOFILE=10240`
- `directdial.go` / `directdial_test.go` — `directDialer` (implements `wire.DirectDialer`, sing-tun interface binding)
- `network_monitor_test.go` — Tests for desktop network change detection
- `paths.go` / `paths_test.go` — `cacheDir()`, `logDir()` (platform-specific cache/log directories)
- `slog_migration_test.go` — Guard: no old `log.Printf` calls in daemon package
- `wintun/` — wintun.dll embedding (amd64 + arm64) + runtime arch detection via `IsWow64Process2`
- `helper.go` — `/api/helper` HTTP handler, routes adb-detect/adb-install/adb-status
- `helper_adb.go` — `AndroidHelper`: adb binary management (system discovery + CDN download), gadb device detection, APK download + push + install
- `helper_test.go` — Tests for helper API, install phases, APK URL resolution
- `webui_linux.go` — Linux-only: wires `webui.Storage`, `webui.Upgrader` (k2 desktop config), and registers `/api/storage`, `/api/platform`, `/api/upgrade`, and `/` (SPA) on the daemon mux
- `webui_nop.go` — Non-Linux stub so `Run()` can call `d.registerWebUI(mux)` unconditionally

## Testing

```bash
go test ./daemon/...    # Tests for api, state, process, rlimit, abort
```
