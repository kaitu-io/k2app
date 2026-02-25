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

- `provider.go` — Provider/ConnectionHandler interfaces, ProviderConfig, MTU constants, defaults
- `stack.go` — sing-tun Handler adapter (converts sing-tun connections → ConnectionHandler)
- `tun_desktop.go` — TUN device for macOS/Linux/Windows (sing-tun, requires root)
- `tun_ios.go` — iOS PacketTunnelProvider (uses existing FD)
- `tun_android.go` — Android VpnService (uses existing FD)
- `mtu_ios.go` / `mtu_android.go` / `mtu_desktop.go` — Platform MTU limits (build-tagged)
- `proxy.go` — SOCKS5/HTTP proxy provider (no root needed, `mode: proxy`)
- `tproxy_linux.go` — nftables TPROXY provider (Linux only, `mode: tproxy`)
- `tproxy_other.go` — stub returning error on non-Linux
- `dns_darwin.go` — macOS DNS override via SCDynamicStore (sets TUN IP as DNS for primary service, crash-safe cleanup on daemon restart)
- `dns_other.go` — no-op stub for non-macOS
- `provider_test.go` / `tun_desktop_test.go` — unit tests

## Modes

- **TUN** (default): `sing-tun` creates TUN device, gVisor stack handles IP → L4 conversion. Requires root.
- **Proxy**: SOCKS5/HTTP server on `127.0.0.1:1080`. No root needed. For Docker/headless deployments.
- **TProxy** (Linux only): nftables TPROXY transparent proxy. No TUN device. For OpenWRT/headless Linux. Requires root + nft.
- **Mobile FD**: iOS/Android pass existing TUN file descriptor via `ProviderConfig.FileDescriptor`.

## Config

```go
type ProviderConfig struct {
    Mode              string       // "tun", "proxy", or "tproxy"
    FileDescriptor    int          // 0 = create TUN; >0 = use existing FD (iOS/Android)
    RouteExcludeAddrs []netip.Addr // IPs excluded from TUN routing (direct DNS, wire server)
    MTU               int          // default 1400 (desktop), 4064 (iOS/NE), 9000 (Android)
    NetworkExtension  bool         // Apple NE: cap MTU at 4064
    ProxyListen       string       // proxy: "127.0.0.1:1080"
    TProxyPort        int          // tproxy: local redirect port (default 12345)
}
```

**MTU defaults**: `SetDefaults()` picks MTU based on platform build tags (`platformMaxMTU`) and `NetworkExtension` flag. Apple NetworkExtension MTU > 4064 causes kernel perf degradation.

**Important**: `tun.Options.DNSServers` is intentionally NOT set. sing-tun auto-derives `TUN_IP.Next()` as DNS, causing OS to send DNS through TUN where `dnsHandler` intercepts at port 53. `RouteExcludeAddrs` only adds IPs to route exclusion (prevents DirectDNSClient loop).

## TProxy Details

The tproxy provider uses nftables TPROXY target to redirect traffic:

1. Creates `inet k2-tproxy` table with prerouting chain
2. TCP/UDP traffic redirected to local tproxy listener port
3. IP_TRANSPARENT socket option preserves original destination
4. IP_RECVORIGDSTADDR extracts original UDP destination
5. Cleanup removes all nftables rules and ip route/rules on Close()

## Testing

```bash
go test ./provider/...    # Provider unit tests
```
