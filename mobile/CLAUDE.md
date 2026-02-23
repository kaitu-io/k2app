# Mobile SDK (mobile/)

gomobile-compatible wrapper around `engine.Engine` for iOS and Android.

## Constraint

gomobile can only export concrete types with simple parameter types (string, int, int64, bool).
Cannot export interfaces from dependency packages — must re-declare `EventHandler` locally.

## API Surface

```go
e := mobile.NewEngine()
e.SetEventHandler(handler)     // EventHandler: OnStateChange, OnError, OnStats
e.Start(configJSON, fd, cfg)   // JSON config + TUN FD + EngineConfig
e.Stop()
e.OnNetworkChanged()           // Called by platform on WiFi/cellular switch
e.StatusJSON() string          // JSON status (matches daemon API format)
```

## Files

- `mobile.go` — `Engine` struct, `Start`/`Stop`/`OnNetworkChanged`/`StatusJSON`
- `event.go` — `EventHandler` interface, `eventBridge` (mobile→engine adapter), state constants
- `config.go` — `EngineConfig` (CacheDir, LogDir, SocketProtector)
- `protector.go` — `SocketProtector` interface (Android VpnService.protect)

## Platform Integration

| Platform | TUN FD Source | Socket Protection | Network Change |
|----------|---------------|-------------------|----------------|
| iOS | NEPacketTunnelProvider | Not needed (kernel self-excludes) | NWPathMonitor → `OnNetworkChanged()` |
| Android | VpnService | `SocketProtector.Protect(fd)` | ConnectivityManager → `OnNetworkChanged()` |

## Build

```bash
make mobile-ios       # iOS xcframework (gomobile bind)
make mobile-android   # Android aar (gomobile bind)
```

Requires `golang.org/x/mobile` in go.mod (gobind uses `go list`).

## Testing

```bash
go test ./mobile/...    # Unit tests
```
