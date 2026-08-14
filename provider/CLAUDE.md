# Provider Layer (provider/)

Platform-specific packet I/O. TUN (desktop/mobile) and Proxy (SOCKS5) modes.
Gateway TPROXY mode is in `gateway/` package (k2r binary).

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
- `dns_darwin.go` — macOS DNS override via SCDynamicStore (sets TUN IP as DNS for primary service, crash-safe cleanup on daemon restart)
- `dns_windows.go` — Windows DNS crash recovery: marker file (`%ProgramData%\k2\dns-state.json`), `CleanupStaleDNSOverride()` at daemon startup
- `dns_other.go` — no-op stub for Linux/iOS (build tag: `(!darwin && !windows) || ios`)
- `provider_test.go` / `tun_desktop_test.go` — unit tests

## Modes

- **TUN** (default): `sing-tun` creates TUN device, gVisor stack handles IP → L4 conversion. Requires root.
- **Proxy**: SOCKS5/HTTP server on `127.0.0.1:1080`. No root needed. For Docker/headless deployments.
- **Mobile FD**: iOS/Android pass existing TUN file descriptor via `ProviderConfig.FileDescriptor`.

## Config

```go
type ProviderConfig struct {
    Mode              string       // "tun" (default) or "proxy"
    FileDescriptor    int          // 0 = create TUN; >0 = use existing FD (iOS/Android)
    RouteExcludeAddrs []netip.Addr // IPs excluded from TUN routing (direct DNS, wire server)
    MTU               int          // default 1400 (desktop), 4064 (iOS/NE), 9000 (Android)
    NetworkExtension  bool         // Apple NE: cap MTU at 4064
    ProxyListen       string       // proxy: "127.0.0.1:1080"
}
```

**MTU defaults**: `SetDefaults()` picks MTU based on platform build tags (`platformMaxMTU`) and `NetworkExtension` flag. Apple NetworkExtension MTU > 4064 causes kernel perf degradation.

**Important**: `tun.Options.DNSServers` is intentionally NOT set. sing-tun auto-derives `TUN_IP.Next()` as DNS, causing OS to send DNS through TUN where `dnsHandler` intercepts at port 53. `RouteExcludeAddrs` only adds IPs to route exclusion (prevents DirectDNSClient loop).

## Process Attribution (process_*.go)

`ProcessSearcher.FindProcess(network, srcIP, srcPort)` attributes a flow to a
process name for `match.apps` / `process_name` rules. Since 2026-08 it handles
IPv4 **and** IPv6 (plus a v4→v6-table fallback for dual-stack sockets, which
the kernel lists only in the AF_INET6 tables with a `::ffff:` local address).
Platform semantics intentionally differ:

- **Windows**: kernel tables via `GetExtended{Tcp,Udp}Table`. UDP treats
  `0.0.0.0` / `::` as wildcard (kernel registers `sendto()` sockets that way);
  exact local-addr match always beats wildcard. Row structs + decoder live in
  `process_wintable.go` — **untagged on purpose** so the literal-offset layout
  tests run on all platforms; keep it free of Windows API imports.
- **Linux**: `/proc/net/{tcp,udp}{,6}` → inode → PID. **No wildcard matching**
  — unconnected `sendto()` UDP sockets show local `0.0.0.0`/`::` and stay
  unattributed. Known gap, don't "fix" it on Windows's behalf without
  understanding the /proc semantics differ.
- **Darwin**: shells out to lsof (2s cap). IPv6 addrs must be bracketed in the
  `-i` selector (`lsofSpec`).

## Testing

```bash
go test ./provider/...    # Provider unit tests
```
