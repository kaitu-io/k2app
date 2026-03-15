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
              ├── 2. wire.ParseURL (k2v5:// URL)
              ├── 2.5. Start NetworkMonitor (DirectDialer needs DefaultInterface)
              ├── 3. Happy Eyeballs address resolution (RFC 8305)
              ├── 4. Build transports (QUIC + TCP-WS)
              ├── 4.5. Wire handshake probe (QUIC/TCP-WS handshake only, no target dial)
              ├── 5. Router (k2rule)
              ├── 6. Provider (TUN/Proxy/TProxy/appext FD)
              ├── 7. ClientTunnel
              ├── 8. DNS middleware (TUN/TProxy mode only)
              ├── 9. Commit → StateConnected
              ├── 10. HealthMonitor start (1Hz wire stats + health FSM)
              └── 11. Background QUIC probe (active during TCP-WS fallback, 30s interval)
```

## State Machine

5 engine states: `disconnected`, `connecting`, `connected`, `paused`, `reconnecting`
Note: `reconnecting` and `paused` are transient — engine.state stays `connected`, these are handler-only broadcasts

- `Start()`: disconnected → connecting → connected (or disconnected on error)
- `Stop()`: any → disconnected (idempotent, resets `paused`)
- `OnNetworkChanged()`: connected (not paused) → reconnecting → connected (resets wire transports)
- `Pause()`: connected → paused (releases wire transports, provider stays alive — iOS memory pressure)
- `Wake()`: paused → connected (forces fresh wire transport connections)
- `fail()`: any → disconnected + classified error

## Error Classification (error.go)

HTTP-aligned error codes for frontend consumption:

| Code | Meaning | Pattern |
|------|---------|---------|
| 400 | Bad config | parse URL, missing auth |
| 401 | Auth rejected | stream rejected |
| 403 | Forbidden | pin mismatch, blocked CA |
| 408 | Timeout | net.Error.Timeout() |
| 502 | Protocol error | TLS/QUIC handshake failure |
| 503 | Server unreachable | TCP dial, connection refused |
| 570 | Unclassified | fallback |

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

- `engine.go` — `Engine` struct, `Start`/`Stop`/`OnNetworkChanged`, assembly pipeline
- `config.go` — `Config` struct (platform-unified tunnel parameters, uses `wire.DirectDialer` interface)
- `status.go` — `Status` struct, custom `MarshalJSON` (computes `uptime_seconds` dynamically)
- `event.go` — `EventHandler` interface (`OnStatus(Status)` + `OnStats`), state constants
- `error.go` — `EngineError`, `ClassifyError` (HTTP-aligned error codes, MultiTransportError handling)
- `health.go` — `healthMonitor` (1Hz sampling, ring buffer 300s, EWMA smoothing α=0.3 on smoothWireLoss, health FSM: healthy→degraded(≥5 streak)→critical(≥5 streak), corrective action: full reconnect with 10s cooldown). TCP health gate: `tcpRecentlyHealthy()` (30s window) suppresses reconnect when TCP streams are working. Wire-layer metrics only — relay metrics are observability (see `wire/VOCABULARY.md`). `DIAG: wake` event emitted when wall-clock gap > 5s between samples (uses `Round(0)` to strip monotonic reading for accurate sleep detection — monotonic clock freezes during macOS sleep but wall clock advances).
- `health_test.go` — Health monitor tests (EWMA smoothing, transitions, reset)
- `recovery_probe.go` — Background QUIC probe during TCP-WS fallback (3-attempt limit, 3s initial delay, stops on client/auth errors)
- `recovery_probe_test.go` — Recovery probe tests
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
