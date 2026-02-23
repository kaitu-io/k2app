# Engine Layer (engine/)

Unified tunnel lifecycle manager — single source of truth for connection state.
Both desktop (daemon/) and mobile (mobile/) delegate all lifecycle here.

## Architecture

```
Platform Shell (daemon/mobile)
    │
    ├── builds engine.Config (platform-specific fields)
    └── calls engine.Start(cfg)
              │
              ├── 1. k2rule.Init (routing rules)
              ├── 2. wire.ParseURL (k2v5:// URL)
              ├── 3. Resolve dial address (ip4/ip6)
              ├── 4. Build transports (QUIC + TCP-WS)
              ├── 5. Router (k2rule)
              ├── 6. Provider (TUN/Proxy/mobile FD)
              ├── 7. ClientTunnel
              ├── 8. DNS middleware (TUN mode only)
              └── 9. Commit → StateConnected
```

## State Machine

3 states: `disconnected` → `connecting` → `connected` → `disconnected`

- `Start()`: disconnected → connecting → connected (or disconnected on error)
- `Stop()`: any → disconnected (idempotent)
- `OnNetworkChanged()`: connected → reconnecting → connected (resets wire transport connections)
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

`engine.Config` unifies desktop and mobile:

| Field | Desktop | Mobile |
|-------|---------|--------|
| `Client` | from YAML/URL | from JSON |
| `FileDescriptor` | -1 (self-create TUN) | >=0 (platform FD) |
| `DirectDialer` | sing-tun interface binding | nil |
| `ProtectFunc` | nil | Android VpnService.protect |
| `NetworkMonitor` | sing-tun DefaultInterfaceMonitor | nil (platform calls directly) |
| `InterfaceMonitor` | same sing-tun instance | nil |

## Files

- `engine.go` — `Engine` struct, `Start`/`Stop`/`OnNetworkChanged`, assembly pipeline
- `config.go` — `Config` struct (platform-unified tunnel parameters)
- `event.go` — `EventHandler` interface, state constants
- `error.go` — `EngineError`, `ClassifyError` (HTTP-aligned error codes)
- `dns_handler.go` — `dnsHandler` (port 53 interception wrapper)
- `network.go` — `NetworkChangeNotifier` interface
- `protect.go` — `protectedDialer` (Android socket protection via syscall.RawConn)

## Testing

```bash
go test ./engine/...    # Engine + error classification tests
```
