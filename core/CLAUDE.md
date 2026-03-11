# Core Layer (core/)

L4 client tunnel, router, and packet processing. DNS has its own doc at `core/dns/CLAUDE.md`.

## Tunnel (tunnel.go)

L4 client tunnel connecting Provider (TUN/Proxy) to wire.Dialer (QUIC/TCP-WS).

**Type**: `ClientTunnel` implements `provider.ConnectionHandler`

**Architecture**:
```
Provider ──► ClientTunnel.HandleTCP/HandleUDP
                  │
              Router (k2rule)
                  │
          ┌───────┴────────┐
       Direct           Proxy
    net.Dial()      wire.DialTCP/UDP
```

**HandleTCP(conn, dest)**:
1. Parse dest → get domain/IP
2. Router.Route → Direct/Proxy/Reject
3. Sniff (single peek): BT → force DIRECT; TLS SNI (443) / HTTP Host (non-443) → upgrade DIRECT→PROXY; IPv6→hostname rewrite
4. Direct: `net.Dial("tcp", dest)` + bidirectional pipe
5. Proxy: `wire.DialTCP(dest)` + bidirectional pipe
6. Reject: close connection silently

**HandleUDP(conn, dest)**:
1. Router.Route → Direct/Proxy
2. Sniff (single peek): BT (uTP/UDPTracker) / WebRTC (STUN/DTLS) → force DIRECT; QUIC SNI (443) → upgrade DIRECT→PROXY; IPv6→hostname rewrite
3. Direct: `net.ListenPacket` + forward to dest
4. Proxy: `wire.DialUDP(dest)` + forward packets

**Sniff flags** (set by engine, disabled in global mode):
- `sniffBT` — BT/WebRTC detection: PROXY→DIRECT (BitTorrent TCP, uTP, UDPTracker, STUN, DTLS)
- `sniffSNI` — domain extraction: DIRECT→PROXY (TLS SNI on 443, HTTP Host on non-443, QUIC SNI on 443)
- `blockQUIC` — block browser QUIC via ICMP reject (default: false = allow through DATAGRAM path, set via `ClientConfig.BlockQUIC`)
- `rewriteIPv6` — replace IPv6 PROXY dests with hostname via SNI (when server lacks IPv6, works in all modes)

**Traffic counting**: `txBytes`/`rxBytes` `atomic.Int64` fields count all bytes through `pipe()`/`pipePacket()`. `TrafficBytes()` returns cumulative TX/RX.

**Files**: `tunnel.go`, `tunnel_test.go`

## Router (router.go)

Routes traffic using k2rule (domain/IP → Direct/Proxy/Reject).

**Initialization** (engine.go `buildRuleConfig`):
```go
k2rule.Init(&k2rule.Config{
    RuleURL:  rule.RuleURL,
    GeoIPURL: rule.GeoIPURL,
    PornURL:  rule.PornURL,
    CacheDir: rule.CacheDir,
    IsGlobal: rule.Global,
})
```

**Usage**:
```go
router.RouteAddr("8.8.8.8") → k2rule.TargetDirect
router.RouteAddr("example.com") → k2rule.TargetProxy
router.RouteAddr("blocked.com") → k2rule.TargetReject
```

**k2rule v0.6.4**: unified Init API + SetTmpRule, URL-based remote downloads (CacheDir required).

## Testing

```bash
go test ./core/...    # Tunnel + router tests
```

## Packet (packet/)

IP packet parsing for gVisor integration.

- `packet.go` — Parse IPv4/IPv6 headers, extract protocol/IPs/ports
- `packet.Info` struct with `IsDNS()`, `Protocol`, `SrcIP`, `DstIP`, `SrcPort`, `DstPort`
- Used by Provider layer for diagnostics, not by tunnel (gVisor delivers L4 connections)
