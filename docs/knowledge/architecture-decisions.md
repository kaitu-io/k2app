# Architecture Decisions

Knowledge extracted from validated implementations across features.

## L4 Proxy Model vs L3 VPN (l4-architecture, 2026-02-12)

**Decision**: Use L4 TCP/UDP proxy model, not L3 IP tunneling.

**Context**: Wire protocol was already L4 (stream/udp.go with tested framing). L3 would require:
- Server-side TUN device, NAT, IP forwarding, root privileges
- Head-of-line blocking (all traffic on single stream)
- Discarding working wire protocol code

**Outcome**: Server is userspace TCP/UDP proxy. Client uses gVisor netstack to convert TUN packets to L4 connections.

**Validation**:
- Tests: `test/e2e_test.go` (TestE2E_TCPProxy, TestE2E_UDPProxy_DNS)
- Production: k2s server runs without root, simple `net.Dial` forwarding

**Trade-offs**:
- Pro: Server simplicity, no NAT complexity, proven by sing-box/clash
- Con: Client needs gVisor stack (but sing-tun already provides this cross-platform)

---

## wire.Dialer/Listener L4 Interfaces (l4-architecture, 2026-02-12)

**Decision**:
```go
type Dialer interface {
    DialTCP(ctx, addr) (net.Conn, error)
    DialUDP(ctx, addr) (net.PacketConn, error)
    Close() error
}

type Listener interface {
    AcceptTCP(ctx) (net.Conn, string, error)
    ServeUDP(ctx, UDPHandler) error
    Close() error
}
```

**Context**: Needed transport abstraction for QUIC primary + TCP-WS fallback.

**Outcome**: TransportManager implements Dialer, delegates to QUICClient/TCPWSClient. Both implement same interface.

**Validation**:
- Tests: `wire/quic_test.go`, `wire/tcpws_test.go`, `wire/transport_test.go`
- Compile-time checks: `var _ Dialer = (*QUICClient)(nil)`

**Pattern**: Interface over transport allows transparent failover without caller changes.

---

## Provider Callback Model (l4-architecture, 2026-02-12)

**Decision**: Provider is callback-driven, not `io.ReadWriteCloser`.

Old:
```go
type Provider interface {
    io.ReadWriteCloser
    Name() string
    MTU() int
}
```

New:
```go
type Provider interface {
    Start(ctx, ConnectionHandler) error
    Close() error
    Name() string
}

type ConnectionHandler interface {
    HandleTCP(conn net.Conn, dest string)
    HandleUDP(conn net.PacketConn, dest string)
}
```

**Context**: sing-tun gVisor stack delivers L4 connections, not raw packets.

**Outcome**: TUN, Proxy, iOS, Android all implement same Provider interface. Handler is identical regardless of connection source.

**Validation**:
- Tests: `provider/provider_test.go` (all platforms), `test/e2e_test.go`
- Cross-platform: `tun_desktop.go`, `tun_ios.go`, `tun_android.go`, `proxy.go`

**Pattern**: Inversion of control separates connection sourcing from routing logic.

---

## Binary Split: k2 (Client) + k2s (Server) (l4-architecture, 2026-02-12)

**Decision**: Two separate binaries instead of `k2 run -s` server mode.

**Context**: Client and server have very different:
- Dependencies (TUN/gVisor vs simple net listeners)
- Runtime environments (end-user OS vs VPS)
- Build requirements (mobile frameworks vs none)

**Outcome**:
- `k2`: CLI, daemon, provider, router, DNS middleware
- `k2s`: server listener, auth, TCP/UDP handlers
- Shared: `wire/`, `config/`, `daemon/client.go`

**Validation**:
- Tests: Both compile independently, e2e test connects them
- Deployment: Smaller server binary, clear separation of concerns

**Trade-offs**:
- Pro: Smaller binaries, clearer code ownership, mobile builds exclude server
- Con: Slight code duplication in CLI scaffolding

---

## TransportManager: QUIC Primary, TCP-WS Fallback (l4-architecture, 2026-02-12)

**Decision**: TransportManager tries QUIC first, falls back to TCP-WS on failure.

**Context**: QUIC blocked in some networks (DPI, corporate firewalls). TCP-WS provides reliable fallback.

**Implementation**:
```go
func (m *TransportManager) DialTCP(ctx, addr) (net.Conn, error) {
    conn, err := m.quic.DialTCP(ctx, addr)
    if err == nil { return conn, nil }
    // fallback
    return m.tcpws.DialTCP(ctx, addr)
}
```

**Validation**:
- Tests: `wire/transport_test.go` (TestTransportManager_QUICFailover)
- Behavior: E2E test blocks QUIC port, verifies TCP-WS used

**Pattern**: Transparent failover at Dialer layer, no application awareness needed.

---

## k2rule.Init(&Config{}) Unified API (l4-architecture, 2026-02-12)

**Decision**: Upgrade k2rule to v0.3.0-go with single `Init(&Config{})` entry point.

Old (v0.1.1-go):
```go
k2rule.InitRules(rulesYAML)
k2rule.LoadGeoIP(mmdbPath)
k2rule.Match(domain)
```

New (v0.3.0-go):
```go
k2rule.Init(&k2rule.Config{
    Rules:      rulesYAML,
    GeoIPPath:  mmdbPath,
})
k2rule.Match(domain)
```

**Context**: Multiple init functions error-prone, GeoIP was optional but treated as required.

**Outcome**: Single init call in `daemon/tunnel.go` BuildTunnel, config passed from ClientConfig.

**Validation**:
- Tests: `core/tunnel_test.go` with mock k2rule config
- Integration: Tunnel creation passes RuleConfig through

**Pattern**: Centralized initialization reduces call-site errors.
