# Provider Layer (provider/)

Platform-specific packet I/O. TUN (desktop/mobile), Proxy (SOCKS5), and TProxy (nftables) modes.

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
- `proxy.go` — SOCKS5/HTTP proxy provider (no root needed, `mode: proxy`)
- `tproxy_linux.go` — nftables TPROXY provider (Linux only, `mode: tproxy`)
- `tproxy_other.go` — stub returning error on non-Linux

## Modes

- **TUN** (default): `sing-tun` creates TUN device, gVisor stack handles IP → L4 conversion. Requires root.
- **Proxy**: SOCKS5/HTTP server on `127.0.0.1:1080`. No root needed. For Docker/headless deployments.
- **TProxy** (Linux only): nftables TPROXY transparent proxy. No TUN device. For OpenWRT/headless Linux. Requires root + nft.
- **Mobile FD**: iOS/Android pass existing TUN file descriptor via `ProviderConfig.FileDescriptor`.

## Config

```go
type ProviderConfig struct {
    Mode           string       // "tun", "proxy", or "tproxy"
    FileDescriptor int          // 0 = create TUN; >0 = use existing FD (iOS/Android)
    DNSServers     []netip.Addr // excluded from TUN route (anti-loop)
    MTU            int          // default 1400
    ProxyListen    string       // proxy: "127.0.0.1:1080"
    TProxyPort     int          // tproxy: local redirect port (default 12345)
}
```

## TProxy Details

The tproxy provider uses nftables TPROXY target to redirect traffic:

1. Creates `inet k2-tproxy` table with prerouting chain
2. TCP/UDP traffic redirected to local tproxy listener port
3. IP_TRANSPARENT socket option preserves original destination
4. IP_RECVORIGDSTADDR extracts original UDP destination
5. Cleanup removes all nftables rules and ip route/rules on Close()
