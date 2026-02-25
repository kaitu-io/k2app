# Daemon Layer (daemon/)

HTTP API on `127.0.0.1:1777` + SSE push. Auto-installed as system service.
Implements `engine.EventHandler` — receives state from engine, not the other way around.

## State Model

Engine is single source of truth. Daemon stores `lastStatus engine.Status` updated by engine's `OnStatus` callback.
No independent state machine — daemon derives everything from engine events.

State values: `disconnected`, `connecting`, `connected`, `reconnecting`, `paused`

## Concurrency Model

- `opMu sync.Mutex` — serializes doUp/doDown (coarse lifecycle lock)
- `mu sync.RWMutex` — protects field access (fine-grained data lock)
- `sseMu sync.RWMutex` — protects SSE client map
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

Real-time status push. Each event is a full `engine.Status` snapshot (idempotent).
Heartbeat every 15s. Buffered per client (cap 8), slow clients get dropped.

### Other

- `GET /ping` — Health check (returns `{"code":0,"message":"pong"}`)
- `GET /metrics` — goroutines, heap, GC stats

Request/Response: `daemon.Request{Action, Params}` / `daemon.Response{Code, Message, Data}`

## Tunnel Lifecycle (doUp/doDown)

**doUp(cfg *ClientConfig, pid int)**:
1. `opMu.Lock()` — serialize with other doUp/doDown
2. `closeTunnel()` — unconditional teardown (engine.Stop fires OnStatus(disconnected))
3. `eng := engine.New()` + `eng.SetEventHandler(d)` — daemon receives events from this point
4. `engineConfigFromClientConfig(cfg)` — assemble `engine.Config` with platform fields
5. `d.StartFunc(eng, cfg)` — engine fires OnStatus(connecting), then OnStatus(connected) or error
6. Start PID monitor if requested
7. Persist state for auto-reconnect

Error path: engine's fail() fires OnStatus(disconnected+error) → daemon's lastStatus updated automatically.

**doDown()**:
1. `opMu.Lock()` — serialize
2. `closeTunnel()` — `engine.Stop()` fires OnStatus(disconnected)

## Files

- `daemon.go` — `Daemon` struct, `OnStatus`/`OnStats` (EventHandler impl), HTTP server setup, `engineConfigFromClientConfig`
- `sse.go` — `handleEvents()` SSE endpoint, `broadcastStatus()` fan-out
- `api.go` — `handleCore`/`handlePing`/`handleMetrics`/`handleVersion` + CORS middleware
- `client.go` — `Client` struct for IPC (used by CLI commands)
- `state.go` — Persisted state: saved config + last connect time (auto-reconnect <1h)
- `reload.go` — Hot-reload config on SIGHUP (dynamic changes without restart)
- `network_monitor.go` — Desktop network change detection (WiFi/cellular switch)
- `process.go` / `process_unix.go` / `process_windows.go` — PID monitoring (auto-disconnect on parent exit)
- `recovery.go` / `recovery_unix.go` / `recovery_windows.go` — Panic recovery + network restore
- `rlimit.go` / `rlimit_unix.go` / `rlimit_windows.go` — `RLIMIT_NPROC=4000`, `RLIMIT_NOFILE=10240`
- `service.go` — `ServiceManager` interface: Install/Uninstall/IsInstalled
- `service_darwin.go` — macOS launchd plist (`io.kaitu.k2`)
- `service_linux.go` — Linux systemd unit (`k2.service`) + OpenWRT init.d/procd (auto-detected)
- `service_windows.go` — Windows Service
- `directdial.go` — `directDialer` (implements `wire.DirectDialer`, sing-tun interface binding)
- `paths.go` — `cacheDir()`, `logDir()` (platform-specific cache/log directories)
- `wintun/` — wintun.dll embedding + extraction (Windows TUN driver)

## Testing

```bash
go test ./daemon/...    # Tests for api, state, process, rlimit
```
