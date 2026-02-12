# Core Layer (core/)

L4 client tunnel, router, and packet processing. DNS has its own doc at `core/dns/AGENT.md`.

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
3. Direct: `net.Dial("tcp", dest)` + bidirectional pipe
4. Proxy: `wire.DialTCP(dest)` + bidirectional pipe
5. Reject: close connection silently

**HandleUDP(conn, dest)**:
1. Port 53 → DNS middleware (intercepted)
2. Non-DNS → Router.Route → Direct/Proxy
3. Direct: `net.ListenPacket` + forward to dest
4. Proxy: `wire.DialUDP(dest)` + forward packets

**Files**: `tunnel.go`, `tunnel_test.go`

## Router (router.go)

Routes traffic using k2rule (domain/IP → Direct/Proxy/Reject).

**Initialization** (daemon/tunnel.go):
```go
k2rule.Init(&k2rule.Config{
    Rules:     rulesYAML,
    GeoIPPath: mmdbPath,
})
```

**Usage**:
```go
router.RouteAddr("8.8.8.8") → k2rule.TargetDirect
router.RouteAddr("example.com") → k2rule.TargetProxy
router.RouteAddr("blocked.com") → k2rule.TargetReject
```

**k2rule v0.3.0-go**: unified Init API, GeoIP validation at init (not runtime).

## Packet (packet/)

IP packet parsing for gVisor integration.

- `packet.go` — Parse IPv4/IPv6 headers, extract protocol/IPs/ports
- `packet.Info` struct with `IsDNS()`, `Protocol`, `SrcIP`, `DstIP`, `SrcPort`, `DstPort`
- Used by Provider layer for diagnostics, not by tunnel (gVisor delivers L4 connections)
