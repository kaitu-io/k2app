# Daemon Layer (daemon/)

State machine + HTTP API on `127.0.0.1:1777`. Auto-installed as system service.

## State Machine

3 states: `stopped` → `connecting` → `connected` → `stopped`

`lastError` is orthogonal — present in any state. API returns `{"state":"stopped", "error":"..."}` on failure.

## Concurrency Model

- `opMu sync.Mutex` — serializes doUp/doDown (coarse lifecycle lock)
- `mu sync.RWMutex` — protects field access (fine-grained data lock)
- `closeTunnel()` — idempotent teardown, caller must hold opMu

## API

### POST /api/core

| Action | Params | Description |
|--------|--------|-------------|
| `up` | `config: ClientConfig` or `wire_url`, optional `pid` | Connect tunnel |
| `down` | — | Disconnect |
| `status` | — | State + connection info + active config |
| `version` | — | Version info |

### Other

- `GET /ping` — Health check (returns `{"code":0,"message":"pong"}`)
- `GET /metrics` — goroutines, heap, GC stats

Request/Response: `daemon.Request{Action, Params}` / `daemon.Response{Code, Message, Data}`

## Tunnel Lifecycle (doUp/doDown)

**doUp(cfg *ClientConfig, pid int)**:
1. `opMu.Lock()` — serialize with other doUp/doDown
2. `closeTunnel()` — unconditional teardown (idempotent)
3. `engineConfigFromClientConfig(cfg)` — assemble `engine.Config` with platform fields (DirectDialer, NetworkMonitor, InterfaceMonitor, PreferIPv6)
4. `engine.Start(cfg)` — delegates all lifecycle to engine
5. Start PID monitor if requested
6. Persist state for auto-reconnect

Error path: set `lastError` + state to `stopped`.

**doDown()**:
1. `opMu.Lock()` — serialize
2. `closeTunnel()` — `engine.Stop()` + transition to `stopped`

## Files

- `daemon.go` — `Daemon` struct, state machine, HTTP server setup, `engineConfigFromClientConfig`
- `api.go` — `handleCore`/`handlePing`/`handleMetrics` + CORS middleware
- `client.go` — `Client` struct for IPC (used by CLI commands)
- `state.go` — Persisted state: saved config + last connect time (auto-reconnect <1h)
- `reload.go` — Hot-reload config on SIGHUP (dynamic changes without restart)
- `network_monitor.go` — Desktop network change detection (WiFi/cellular switch)
- `process.go` / `process_unix.go` / `process_windows.go` — PID monitoring (auto-disconnect on parent exit)
- `recovery.go` / `recovery_unix.go` / `recovery_windows.go` — Panic recovery + network restore
- `rlimit.go` / `rlimit_unix.go` / `rlimit_windows.go` — `RLIMIT_NPROC=4000`, `RLIMIT_NOFILE=10240`
- `service.go` — `ServiceManager` interface: Install/Uninstall/IsInstalled
- `service_darwin.go` — macOS launchd plist (`io.kaitu.k2`)
- `service_linux.go` — Linux systemd unit (`k2.service`)
- `service_windows.go` — Windows Service

## Testing

```bash
go test ./daemon/...    # Tests for api, state, process, rlimit
```
