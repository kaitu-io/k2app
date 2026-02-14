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
| `up` | `wire_url` or `config_path`, `pid` | Connect tunnel |
| `down` | — | Disconnect |
| `reconnect` | — | Reconnect with saved config |
| `status` | — | State + connection info |
| `get_config` | — | Saved config |
| `version` | — | Version info |

### Other

- `GET /ping` — Health check (returns `{"code":0,"message":"pong"}`)
- `GET /metrics` — goroutines, heap, GC stats

Request/Response: `daemon.Request{Action, Params}` / `daemon.Response{Code, Message, Data}`

## Tunnel Lifecycle (doUp/doDown)

**doUp(wireURL or configPath)**:
1. `opMu.Lock()` — serialize with other doUp/doDown
2. `closeTunnel()` — unconditional teardown (idempotent)
3. Set state to `connecting`, save config fields
4. Build config from wire URL or config file
5. Build tunnel via TunnelBuilder
6. Set tunnel refs, state to `connected`
7. Start PID monitor if requested
8. Persist state for auto-reconnect

Error path: set `lastError` + state to `stopped`.

**doDown()**:
1. `opMu.Lock()` — serialize
2. `closeTunnel()` — teardown + transition to `stopped`

## Files

- `daemon.go` — `Daemon` struct, state machine, HTTP server setup
- `tunnel.go` — BuildTunnel helper (wire URL → ClientTunnel)
- `api.go` — `handleCore`/`handlePing`/`handleMetrics` + CORS middleware
- `client.go` — `Client` struct for IPC (used by CLI commands)
- `state.go` — Persisted state: saved config + last connect time (auto-reconnect <1h)
- `process.go` / `process_unix.go` — PID monitoring (auto-disconnect on parent exit)
- `recovery.go` / `recovery_unix.go` — Panic recovery + network restore
- `rlimit.go` / `rlimit_unix.go` — `RLIMIT_NPROC=4000`, `RLIMIT_NOFILE=10240`
- `service.go` — `ServiceManager` interface: Install/Uninstall/IsInstalled
- `service_darwin.go` — macOS launchd plist (`io.kaitu.k2`)
- `service_linux.go` — Linux systemd unit (`k2.service`)

## Testing

```bash
go test ./daemon/...    # Tests for api, state, process, rlimit
```
