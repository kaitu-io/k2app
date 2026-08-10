# Engine Layer (engine/)

Unified tunnel lifecycle manager — single source of truth for connection state.
Both desktop (daemon/) and appext (appext/) delegate all lifecycle here.

## Architecture

```
Platform Shell (daemon/appext)
    │
    ├── builds engine.Config (platform-specific fields)
    └── calls engine.Start(cfg)
              │
              ├── 1. k2rule.Init (routing rules)
              ├── 2.5. Start NetworkMonitor (DirectDialer needs DefaultInterface)
              ├── 5. Router (k2rule)
              ├── 6. Provider (TUN/Proxy/TProxy/appext FD)
              ├── 6.5. Per-outbound transport setup:
              │      for each unique k2v5:// → ParseURL → resolve → RaceTransport → TM
              │      (parallel via errgroup; first k2v5 route = primaryTM)
              ├── 7. ClientTunnel
              ├── 8. DNS middleware (TUN/TProxy mode only)
              │      DNS proxy dialer = primaryTM's TCP-WS (control-plane)
              │      Data traffic dialer = per-outbound TransportManager
              ├── 9. Commit → StateConnected + start reRaceLoop
              ├── 10. HealthMonitor start (1Hz wire stats + health FSM + echoLoop)
              └── 11. Background QUIC probe (active during TCP-WS fallback, 30s interval)
```

## State Machine

5 engine states: `disconnected`, `connecting`, `connected`, `paused`, `reconnecting`
Note: `reconnecting` and `paused` are transient — engine.state stays `connected`, these are handler-only broadcasts

- `Start()`: disconnected → connecting → connected (or disconnected on error)
- `Stop()`: any → disconnected (idempotent, resets `paused`)
- `OnNetworkChanged()`: routes through netCoordinator as SignalChanged → reconnect if network is up
- `OnNetEvent(event)`: routes through netCoordinator — "unavailable" stops probe, "available" triggers doNetworkReconnect, "changed" reconnects if up
- `Pause()`: connected → paused (releases wire transports, provider stays alive — iOS memory pressure)
- `Wake()`: paused → connected (forces fresh wire transport connections)
- `fail()`: any → disconnected + classified error

## Error Classification (error.go)

HTTP-aligned error codes for frontend consumption:

| Code | Category | Meaning | Pattern |
|------|----------|---------|---------|
| 101 | network | Network unavailable | network unreachable, no route to host |
| 108 | network | Timeout | net.Error.Timeout(), context.DeadlineExceeded |
| 400 | client | Bad config | parse URL, missing auth |
| 401 | client | Auth rejected | stream rejected |
| 402 | client | Payment required | subscription expired |
| 403 | client | Forbidden | pin mismatch, blocked CA |
| 502 | server | Protocol error | TLS/QUIC handshake failure |
| 503 | server | Server unreachable | TCP dial, connection refused |
| 504 | server | Rule bundle dependency unreachable | CDN sources all failed (transient, retryable) |
| 570 | server | Unclassified | fallback |

## Config (config.go)

`engine.Config` unifies all platforms via `wire.DirectDialer` interface:

| Field | Desktop (daemon) | App ext (appext) | OpenWRT |
|-------|-----------------|-------------------|---------|
| `Client` | from YAML/URL | from JSON | from YAML/URL |
| `FileDescriptor` | -1 (self-create TUN) | >=0 (platform FD) | -1 |
| `DirectDialer` | daemon.directDialer | protectedDialer (Android) / nil (iOS/macOS) | nil |
| `NetworkMonitor` | sing-tun DefaultInterfaceMonitor | nil (platform calls directly) | nil |
| `InterfaceMonitor` | same sing-tun instance | nil | nil |
| `NetworkExtension` | false | true (Darwin) / false (Android) | false |
| `HealthCallback` | `d.broadcastSnapshot` | nil | nil |

## Files

- `engine.go` — `Engine` struct, `Start`/`Stop`/`OnNetworkChanged`/`OnNetEvent`/`doNetworkReconnect`, assembly pipeline
- `netmon.go` — `NetEvent` (gomobile-compatible network event struct, 8 primitive fields) + `netCoordinator` (fuses sing-tun + platform signals: suppress reconnect when network down, full reconnect on recovery, 500ms debounce)
- `netmon_test.go` — netCoordinator unit tests (6 scenarios)
- `config.go` — `Config` struct (platform-unified tunnel parameters, uses `wire.DirectDialer` interface)
- `status.go` — `Status` struct, custom `MarshalJSON` (computes `uptime_seconds` dynamically)
- `event.go` — `EventHandler` interface (`OnStatus(Status)` + `OnStats`), state constants
- `error.go` — `EngineError`, `ClassifyError` (HTTP-aligned error codes, MultiTransportError handling)
- `health.go` — `healthMonitor` (1Hz sampling, ring buffer 300s, EWMA smoothing α=0.3 on smoothWireLoss, health FSM: healthy→degraded(≥5 streak)→critical(≥5 streak), corrective action: full reconnect with 10s cooldown). `sample()` uses primaryTM for heartbeat stats. `echoLoop`: one goroutine per TM, probing via `wire.EchoProber` on an exponential-trust schedule (`echoIntervals` 1s→16s, `echoProbeTimeout` 2s); 3 consecutive real failures trigger per-TM re-race, so wire death is detected in ~5s. An `init()` guard panics if that worst case ever reaches QUIC `MaxIdleTimeout` — past that point the loop adds nothing over quic-go's own idle timer. **Backward compat**: `echoStep` routes `wire.IsEchoUnsupported` errors (pre-echo servers) to trust-growth rather than strikes, so old servers never storm re-races. That predicate is fail-open, and misreading a real timeout as "unsupported" is what silently disabled re-race entirely in ticket #3463 — see the timeout note in `wire/CLAUDE.md` before touching it. TCP health gate: `tcpRecentlyHealthy()` (30s window) suppresses reconnect when TCP streams are working. Wire-layer metrics only — relay metrics are observability (see `wire/VOCABULARY.md`). `wakeDetector` (embedded in healthMonitor): detects sleep/wake via wall-clock gap, `DIAG: wake` at >5s, triggers `doNetworkReconnect()` at ≥30s (aligned with QUIC MaxIdleTimeout). Heartbeat includes memory metrics (`heapMB`, `sysMB`, `goroutines`) via `runtime.ReadMemStats` every 30s for iOS NE jetsam diagnosis.
- `health_test.go` — Health monitor tests (EWMA smoothing, transitions, reset)
- `engine.go:reRaceLoop` — Background re-race loop triggered by echoLoop (3 consecutive echo failures → channel signal → wire.RaceTransport → ReplacePrimary)
- `contract_test.go` — 4 transport behavior contract tests (TCP health gate, server unreachable stop, auth no-retry, timing)
- `happy_eyeballs.go` — `resolveDialAddr`, `happyEyeballs` (RFC 8305 IPv6/IPv4 race with 250ms head start)
- `dns_handler.go` — `dnsHandler` (port 53 TCP+UDP interception, RFC 1035 TCP framing)
- `network.go` — `NetworkChangeNotifier` interface
- `engine_test.go` — Engine lifecycle tests
- `deadlock_test.go` — Deadlock detection tests (Stop vs health goroutine)
- `slog_migration_test.go` — Guard: no old `log.Printf` calls

## Testing

```bash
go test ./engine/...                          # Engine + error classification tests
go test -run TestContract -v ./engine/...     # contract tests only
```
