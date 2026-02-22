# Provider Layer (provider/)

Platform-specific packet I/O. TUN (desktop/mobile) and Proxy (SOCKS5) modes.

## Interface (provider.go)

```go
type Provider interface {
    Start(ctx context.Context, handler ConnectionHandler) error
    Close() error
    Name() string
}

type ConnectionHandler interface {
    HandleTCP(conn net.Conn, dest string)
    HandleUDP(conn net.PacketConn, dest string)
}
```

Provider calls `handler.HandleTCP/HandleUDP` when connections arrive. `ClientTunnel` implements `ConnectionHandler`.

## Files

- `provider.go` — Provider/ConnectionHandler interfaces, ProviderConfig, defaults
- `stack.go` — gVisor sing-tun stack (converts TUN packets → L4 connections → ConnectionHandler)
- `tun_desktop.go` — TUN device for macOS/Linux/Windows (sing-tun, requires root)
- `tun_ios.go` — iOS PacketTunnelProvider (uses existing FD)
- `tun_android.go` — Android VpnService (uses existing FD)
- `proxy.go` — SOCKS5 proxy provider (no root needed, `mode: proxy`)

## Modes

- **TUN** (default): `sing-tun` creates TUN device, gVisor stack handles IP → L4 conversion. Requires root.
- **Proxy**: SOCKS5 server on `127.0.0.1:1080`. No root needed. For Docker/headless deployments.
- **Mobile FD**: iOS/Android pass existing TUN file descriptor via `ProviderConfig.FileDescriptor`.

## Config

```go
type ProviderConfig struct {
    Mode           string       // "tun" or "proxy"
    FileDescriptor int          // 0 = create TUN; >0 = use existing FD (mobile)
    DNSServers     []netip.Addr // excluded from TUN route (anti-loop)
    MTU            int          // default 1400
    ProxyListen    string       // proxy: "127.0.0.1:1080"
}
```
