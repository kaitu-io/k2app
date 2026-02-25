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
- `protect.go` — `protectedDialer` (implements `wire.DirectDialer` + `wire.DialerController`) — Android only
- `ne_platform_darwin.go` / `ne_platform_other.go` — `nePlatform`: unified Darwin NE networking (implements `wire.DirectDialer` + `wire.DialerController` + `engine.NetworkChangeNotifier`). Mirrors daemon's 3-type architecture (directDialer + singTunMonitor + ifaceMon) in 1 type with shared `DefaultInterfaceMonitor`
- `platform_darwin.go` / `platform_other.go` — `isNetworkExtension` platform constant

## Platform Integration

| Platform | TUN FD Source | Socket Protection | Network Change |
|----------|---------------|-------------------|----------------|
| macOS sysext | NEPacketTunnelProvider | `nePlatform` (interface-bound dialing via DefaultInterfaceMonitor) | `nePlatform` (sing-tun monitor → engine) |
| iOS | NEPacketTunnelProvider | `nePlatform` (interface-bound dialing via DefaultInterfaceMonitor) | `nePlatform` (sing-tun monitor → engine) |
| Android | VpnService | `SocketProtector.Protect(fd)` → `protectedDialer` | ConnectivityManager → `OnNetworkChanged()` |

## Socket Protection

**Android** (`protect.go`): `protectedDialer` implements `wire.DirectDialer` + `wire.DialerController`. Created when `SocketProtector` is non-nil. Uses `VpnService.protect(fd)` to exclude sockets from VPN routing.

**Darwin** (`ne_platform_darwin.go`): `nePlatform` implements `wire.DirectDialer` + `wire.DialerController` + `engine.NetworkChangeNotifier`. Uses sing-tun `DefaultInterfaceMonitor` with `UnderNetworkExtension: true` for interface-bound dialing (socket probe instead of route table parsing).

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
