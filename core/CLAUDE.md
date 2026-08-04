# Core Layer (core/)

L4 client tunnel, router, outbound implementations, and packet processing. DNS has its own doc at `core/dns/CLAUDE.md`.

## Tunnel (tunnel.go)

L4 client tunnel connecting Provider (TUN/Proxy) to Outbound table.

**Type**: `ClientTunnel` implements `provider.ConnectionHandler`

**Architecture**:
```
Provider ──► ClientTunnel.HandleTCP/HandleUDP
                  │
              Router (rule.Engine)
                  │
              rule.Target (opaque index)
                  │
              outbounds[target]
                  │
    ┌─────────────┼──────────────┐
  direct       wire (k2v5)    k2p
 net.Dial()   wire.DialTCP   k2p.Dial
              + earlyData    (lazy)
```

Outbound table is `[]Outbound` indexed by `rule.Target` value. `nil` entry = reject.

**HandleTCP(conn, dest)**:
1. Parse dest → get domain/IP
2. `routeConn` → `rule.Target` (via Router, or `defaultTarget` when router is nil)
3. `outbound(target)` → lookup Outbound (nil = reject, return)
4. Sniff (single peek): BT → force direct; TLS SNI (443) / HTTP Host (non-443) → upgrade direct→wire; IPv6→hostname rewrite; then **protocol re-route** — if BT was sniffed, `Router.RouteConnProto(sniffed SNI or host, meta+protocol)` lets an explicit `{protocol: [...]}` rule override the force-direct default (no such rule → unchanged)
5. Direct outbound: replay peeked bytes via `prefixConn.Read`, earlyData ignored
6. Wire/k2p outbound: unwrap prefixConn, send peeked bytes as `earlyData` (saves 1 RTT)
7. `pipe(local, remote)` bidirectional copy

**HandleUDP(conn, dest)**:
1. `routeConn` → `rule.Target`
2. `outbound(target)` → lookup Outbound (nil = reject)
3. Sniff (single peek): BT (uTP/UDPTracker) → force direct; WebRTC (STUN/DTLS) → detected and **kept on the tunnel** (ticket #3051 — forcing it direct broke calls behind the GFW); QUIC SNI (443) → upgrade direct→wire; then **protocol re-route** — `Router.RouteConnProto(sniffed SNI or host, meta+protocol)` lets an explicit `{protocol: [...]}` rule override those defaults (no such rule → unchanged). Note `blockQUIC` can drop a proxy-bound QUIC packet before this step ever runs
4. `outbound.DialUDP` returns `(PacketConn, destAddr)` — destAddr non-nil for direct (WriteTo target), nil for wire (pre-connected)
5. `pipePacket(local, remote, destAddr)` bidirectional copy. `pipePacket` treats the session's first observed destination as `primary`: packets to `primary` keep using the outbound's `destAddr` (preserves the domain label from an SNI upgrade / IPv6 rewrite instead of a bare IP), packets to any other destination are forwarded with their real, per-packet address. This is what lets one UDP socket (game server + matchmaker + STUN reflector, say) fan out correctly instead of pinning every packet to the first peer it talked to. See `pipePacket` in `tunnel.go`.

**Sniff flags** (set by engine, disabled in global mode):
- `sniffBT` — BT/WebRTC detection. BitTorrent (TCP handshake, uTP, UDPTracker) → wire→direct. STUN/DTLS are detected but **stay on the wire** (ticket #3051); they set `ConnMeta.Protocol` so a `protocol:` rule can act on them. Only fires on connections headed for the wire — already-direct traffic is never sniffed
- `sniffSNI` — domain extraction: direct→wire (TLS SNI on 443, HTTP Host on non-443, QUIC SNI on 443)
- `blockQUIC` — block browser QUIC via ICMP reject (default: false)
- `rewriteIPv6` — replace IPv6 wire dests with hostname via SNI (when server lacks IPv6)

**Traffic counting**: `txBytes`/`rxBytes` `atomic.Int64` count all bytes through `pipe()`/`pipePacket()`.

**Files**: `tunnel.go`, `tunnel_test.go`

## Outbound (outbound.go)

Interface for dialing remote endpoints, indexed by `rule.Target`:

```go
type Outbound interface {
    DialTCP(ctx, dest, earlyData) (net.Conn, error)
    DialUDP(ctx, dest) (PacketConn, destAddr, error)
    Name() string
    Close() error
}
```

**Implementations**:

| Type | File | Owns resources | Close() |
|------|------|----------------|---------|
| `directOutbound` | `outbound_direct.go` | No (stdlib net.Dial) | no-op |
| `wireOutbound` | `outbound_wire.go` | No (engine owns tm) | no-op |
| `k2pOutbound` | `outbound_k2p.go` | Yes (ICE/QUIC conn) | closes inner dialer |

- `wireOutbound`: wraps `wire.Dialer` + DIAG logging (`proxy-dial-fail`, `proxy-dial-slow`) + WireErrorReporter
- `k2pOutbound`: lazy-connect via `k2p.Dial()` on first use. Dead connection detection via quic-go typed errors triggers reset + reconnect. Concurrency: `connectMu` (serialization) → `mu` (state).

## Router (router.go)

Routes traffic using in-tree `rule.Engine` (domain/IP → `rule.Target` opaque index).

`rule.Target` is a pure `uint8` index — no semantic constants in rule package. Convention: 0=direct, 1=reject, 2+=wire outbounds. Semantics assigned by engine via `buildOutboundMap`.

**Files**: `router.go`

## Testing

```bash
go test ./core/...    # Tunnel + router + outbound tests
```

## Packet (packet/)

IP packet parsing for gVisor integration.

- `packet.go` — Parse IPv4/IPv6 headers, extract protocol/IPs/ports
- `packet.Info` struct with `IsDNS()`, `Protocol`, `SrcIP`, `DstIP`, `SrcPort`, `DstPort`
- Used by Provider layer for diagnostics, not by tunnel (gVisor delivers L4 connections)
