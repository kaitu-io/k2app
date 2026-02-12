# Daemon Layer (daemon/)

State machine + HTTP API on `127.0.0.1:1777`. Auto-installed as system service.

## API

### POST /api/core

| Action | Params | Description |
|--------|--------|-------------|
| `up` | `wire_url` or `config_path`, `pid` | Connect tunnel |
| `down` | — | Disconnect |
| `reconnect` | — | Reconnect with saved config |
| `status` | — | State + connection info |
| `speedtest` | `server_id` | Start async speedtest |
| `get_speedtest_status` | — | Speedtest progress |
| `get_config` | — | Saved config |
| `version` | — | Version info |

### Other

- `GET /ping` — Health check (returns `{"code":0,"message":"pong"}`)
- `GET /metrics` — goroutines, heap, GC stats

Request/Response: `daemon.Request{Action, Params}` / `daemon.Response{Code, Message, Data}`

## Tunnel Lifecycle (doUp/doDown)

**doUp(wireURL or configPath)**:
1. Parse wire URL: `wire.ParseURL("k2v5://...")`
2. Build TLS config: `wire.BuildClientTLSConfig(cfg)` (ECH, fingerprint)
3. Create transports: `QUICClient`, `TCPWSClient` → `TransportManager`
4. Create Provider: `provider.NewProvider(providerCfg)` (TUN or Proxy mode)
5. Create Router: `k2rule.Init(&Config{...})`
6. Create ClientTunnel: `core.NewClientTunnel(provider, dialer, router)`
7. Start tunnel: `tunnel.Start(ctx)` → Provider calls HandleTCP/HandleUDP
8. Save state, transition to "connected"

**doDown()**:
1. Cancel tunnel context
2. `tunnel.Close()` → Provider.Close() + Dialer.Close()
3. Wait for goroutines (WaitGroup)
4. Transition to "stopped"

## Files

- `daemon.go` — `Daemon` struct, state machine, HTTP server setup
- `tunnel.go` — BuildTunnel helper (wire URL → ClientTunnel)
- `api.go` — `handleCore`/`handlePing`/`handleMetrics` + CORS middleware
- `client.go` — `Client` struct for IPC (used by CLI commands)
- `state.go` — Persisted state: saved config + last connect time (auto-reconnect <1h)
- `speedtest.go` — Async speedtest engine with polling
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
