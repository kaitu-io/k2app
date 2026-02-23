# App Extension SDK (appext/)

gomobile-compatible wrapper around `engine.Engine` for OS-managed platforms:
macOS system extension, iOS, Android.

## Constraint

gomobile can only export concrete types with simple parameter types (string, int, int64, bool).
Cannot export interfaces from dependency packages — must re-declare `EventHandler` locally.

## API Surface

```go
e := appext.NewEngine()
e.SetEventHandler(handler)     // EventHandler: OnStateChange, OnError, OnStats
e.Start(configJSON, fd, cfg)   // JSON config + TUN FD + EngineConfig
e.Stop()
e.OnNetworkChanged()           // Called by platform on WiFi/cellular switch
e.StatusJSON() string          // JSON status (matches daemon API format)
```

## Files

- `appext.go` — `Engine` struct, `Start`/`Stop`/`OnNetworkChanged`/`StatusJSON`
- `event.go` — `EventHandler` interface, `eventBridge` (appext→engine adapter), state constants
- `config.go` — `EngineConfig` (CacheDir, LogDir, SocketProtector)
- `protector.go` — `SocketProtector` interface (Android VpnService.protect)
- `protect.go` — `protectedDialer` (implements `wire.DirectDialer` + `wire.DialerController`)

## Platform Integration

| Platform | TUN FD Source | Socket Protection | Network Change |
|----------|---------------|-------------------|----------------|
| macOS sysext | NEPacketTunnelProvider | Not needed (kernel self-excludes) | NWPathMonitor → `OnNetworkChanged()` |
| iOS | NEPacketTunnelProvider | Not needed (kernel self-excludes) | NWPathMonitor → `OnNetworkChanged()` |
| Android | VpnService | `SocketProtector.Protect(fd)` → `protectedDialer` | ConnectivityManager → `OnNetworkChanged()` |

## Socket Protection (protect.go)

`protectedDialer` implements both `wire.DirectDialer` (for transport sockets) and
`wire.DialerController` (for DNS direct query sockets). Created in `appext.Start()`
when `SocketProtector` is non-nil, then passed to engine as `Config.DirectDialer`.

## Build

```bash
make appext-ios       # iOS xcframework (gomobile bind)
make appext-android   # Android aar (gomobile bind)
```

Requires `golang.org/x/mobile` in go.mod (gobind uses `go list`).

## Testing

```bash
go test ./appext/...    # Unit tests
```
