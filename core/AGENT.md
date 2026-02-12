# Core Layer (core/)

Tunnel, router, and packet processing. DNS has its own doc at `core/dns/AGENT.md`.

## Tunnel — TO BE IMPLEMENTED (T4)

Deleted old L3 tunnel.go (used `Dial(ctx)` / `Accept()` model). New L4 tunnel will implement `ConnectionHandler`:
- `HandleTCP(conn, dest)` — route via Router → wire.DialTCP or net.Dial
- `HandleUDP(conn, dest)` — route via Router → wire.DialUDP or net.Dial; port 53 → DNS middleware

## Router (router.go)

Routes traffic using k2rule: CN domains/IPs -> direct, foreign -> proxy.

- `k2rule.InitWithFallback(k2rule.TargetProxy)` — init in main.go
- `k2rule.MatchIP(ip)` / `k2rule.MatchDomain(domain)` -> `k2rule.Target`
- `k2rule.Target`: `TargetDirect` / `TargetProxy` / `TargetReject`

## Direct — TO BE IMPLEMENTED (T4)

Old raw socket `DirectSender` deleted. New `DirectDialer` will use simple `net.Dial` (no raw socket needed in L4 model).

## Packet (packet/)

`packet.go` — IP packet parsing: extracts protocol, src/dst IP, ports, DNS detection.
`packet.Info` struct with `IsDNS()`, `Protocol`, `SrcIP`, `DstIP`, `SrcPort`, `DstPort`.
Still needed for gVisor integration diagnostics.
