# App Extension SDK (appext/)

gomobile-compatible wrapper around `engine.Engine` for OS-managed platforms:
macOS system extension, iOS, Android.

## Constraint

gomobile can only export concrete types with simple parameter types (string, int, int64, bool).
Cannot export interfaces from dependency packages — must re-declare `EventHandler` locally.

## No subscription resolver here

Unlike `daemon/` (`resolveSubscriptions` in daemon.go), `appext/` does **not**
rewrite `k2subs://` routes to `k2v5://` before calling `engine.Start`. Reason:
NE / Android service processes have tight memory budgets (iOS NE is 50MB) and
adding a Go HTTPS client + JSON cache + background refresher inflates binary
size and complexity. It also keeps the platform shell stateless — engine
receives an already-resolved configuration.

Subscription URL resolution on mobile happens **in the main App webapp**
(`webapp/src/services/subs-resolver.ts`) before `_k2.run('up', config)`.
Engine receives `routes:[{via:'k2v5://...'}]` like any other connection.

If a `k2subs://` route reaches engine here, `engine.buildOutboundMap`
(engine.go:1052) treats it as reserved → drops the route → 0 wire outbounds →
fail with code 570 "no k2v5 outbound configured". This is the symptom of a
webapp bug (smart mode missing the resolver), not an engine bug.

## API Surface

```go
e := appext.NewEngine()
e.SetEventHandler(handler)     // EventHandler: OnStatus(statusJSON), OnStats(tx, rx)
e.Start(configJSON, fd, cfg)   // JSON config + TUN FD + EngineConfig
e.Stop()
e.NotifyNetEvent(ev)           // Network state change from platform (WiFi/cell, available/unavailable)
e.StatusJSON() string          // JSON status (matches daemon API format)
```

## Files

- `appext.go` — `Engine` struct, `Start`/`Stop`/`OnNetworkChanged`/`StatusJSON`
- `event.go` — `EventHandler` interface, `eventBridge` (appext→engine adapter), state constants
- `net_event.go` — `NetEvent` struct (mirrors `engine.NetEvent` for gomobile export), `NewNetEvent` constructor
- `config.go` — `EngineConfig` (CacheDir, LogDir, SocketProtector)
- `protector.go` — `SocketProtector` interface (Android VpnService.protect)
- `resolver.go` — `PackageResolver` (UID→package(s), installer) + `ConnectionOwnerResolver` (4-tuple→UID via ConnectivityManager.getConnectionOwnerUid, API 29+) — both implemented by K2VpnService (Kotlin); `EngineConfig.ConnectionOwner` wires the latter into `provider.OwnerProcessSearcher`, without it Android falls back to the /proc searcher that Android 10+ SELinux blinds
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

## iOS Memory Optimization

iOS NE has ~50 MB jetsam limit. Two-layer GC strategy (sing-box pattern):
- `memlimit_ios.go`: `SetGCPercent(10)` (aggressive — GC at 10% heap growth) + `SetMemoryLimit(35MB)` (hard ceiling, 15MB headroom for C/ObjC)
- `appext.go`: `FreeOSMemory()` after `Start()` succeeds — reclaims init-time allocations (config parsing, k2rule loading)
- `memprofile.go`: `MemorySnapshot()` — per-component heap breakdown (exported as `AppextMemorySnapshot` for iOS/Android diagnostics)

Additional platform-specific controls in `core/`:
- `limits_ios.go`: 512 max connections (non-blocking drop), 8KB TCP buffers, 15s UDP idle timeout
- `gc_ios.go`: sampled GC every 8 connection releases if HeapInuse > 20MB

## Testing

```bash
go test ./appext/...    # Unit tests
```
