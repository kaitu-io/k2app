# Provider Layer (provider/)

Platform-specific packet I/O. Currently TUN only.

## Interface (provider.go)

```go
type Provider interface {
    io.ReadWriteCloser
    Name() string    // e.g. "utun0"
    MTU() int        // 1400
}
```

## TUN (tun_unix.go)

Uses `github.com/sagernet/sing-tun`. Device name auto-allocated by OS.
IP: 198.18.0.7/15, MTU: 1400. Requires root.

## Future Providers

- `PacketTunnelProvider` (iOS)
- `VpnService` (Android)
- `iptables` redirect (OpenWRT)
