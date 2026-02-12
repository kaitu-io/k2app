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

---

## ECH Server-Side Routing (wire-protocol-compliance, 2026-02-12)

**Decision**: Use `PeekClientHello` to detect ECH extension before TLS handshake. Route based on ECH presence.

**Context**: k2v5 uses ECH to hide real SNI from censors. Non-ECH connections must be reverse-proxied to camouflage server as innocent HTTPS.

**Implementation**:
```go
// Peek ClientHello
info, _ := wire.PeekClientHello(rawBytes)

if !info.HasECH {
    // Non-ECH → reverse proxy to outer SNI
    wire.ReverseProxy(conn, data, info.SNI+":443")
    return
}

// ECH → TLS handshake with ECH decryption → k2v5
tlsConn := tls.Server(conn, s.TLSConfig)
tlsConn.Handshake()
innerSNI := tlsConn.ConnectionState().ServerName

if innerSNI == s.cfg.Host {
    s.handleK2V5(tlsConn) // Authenticated k2v5 client
}
```

**Outcome**: Server is indistinguishable from legitimate website for non-ECH traffic.

**Validation**:
- Tests: `server/server_test.go` (TestServer_ECHRouting_*)
- Pattern: `wire/ech.go:PeekClientHello` returns `ClientHelloInfo{SNI, HasECH}`

**Trade-offs**:
- Pro: Zero detection surface for censors probing without ECH config
- Con: Requires valid TLS cert for camouflage domain

---

## uTLS for Browser-Like TLS Fingerprints (wire-protocol-compliance, 2026-02-12)

**Decision**: Use `github.com/metacubex/utls` for TCP-WS client TLS handshake instead of Go `crypto/tls`.

**Context**: Go's TLS stack has distinctive fingerprint detectable by DPI. Browser-like fingerprints reduce detection risk.

**Implementation**:
```go
// TCP-WS client (production mode)
tlsCfg, clientHelloID, _ := wire.BuildClientTLSConfig(cfg)
uconn := utls.UClient(conn, &utls.Config{
    ServerName:                     tlsCfg.ServerName,
    InsecureSkipVerify:             tlsCfg.InsecureSkipVerify,
    EncryptedClientHelloConfigList: tlsCfg.EncryptedClientHelloConfigList,
}, clientHelloID) // chrome/firefox/safari/random
uconn.HandshakeContext(ctx)
```

**Outcome**: TLS handshake fingerprint matches real browsers. ECH works through uTLS.

**Validation**:
- Tests: `wire/tcpws_test.go` (TestTCPWSClient_uTLS_*)
- Config: `fp=chrome|firefox|safari|random` URL parameter

**Trade-offs**:
- Pro: Evades DPI fingerprinting, ECH compatible
- Con: uTLS may lag behind Go TLS security patches (acceptable for anti-censorship)

**Note**: QUIC stays with standard Go TLS — QUIC handshake encrypted after Initial, less exposed.

---

## WebSocket Layer for HTTP Camouflage (wire-protocol-compliance, 2026-02-12)

**Decision**: Insert WebSocket upgrade between TLS and smux for TCP-WS transport.

Old: `TCP → TLS → smux`
New: `TCP → TLS → WebSocket → smux`

**Context**: Raw smux over TLS has non-HTTP traffic pattern, easily fingerprinted. WebSocket provides HTTP camouflage.

**Implementation**:
```go
// Client: after uTLS handshake
wsConn, _ := websocket.Dial(ctx, "wss://"+host+"/k2v5/tunnel", &websocket.DialOptions{
    HTTPClient: &http.Client{Transport: &http.Transport{
        DialTLSContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
            return uconn, nil // Inject existing TLS connection
        },
    }},
})

// Server: peek first bytes after TLS, check for GET request
if bytes.HasPrefix(data, []byte("GET /k2v5/tunnel")) {
    wsConn, _ := websocket.Accept(tlsConn, nil)
    netConn := websocket.NetConn(ctx, wsConn, websocket.MessageBinary)
    s.tcpwsSrv.HandleConn(netConn) // Feed to smux
}
```

**Outcome**: TCP-WS traffic looks like WebSocket to DPI. Auth endpoint (`POST /k2v5/auth`) continues to work without WS.

**Validation**:
- Tests: `wire/tcpws_test.go` (TestTCPWSClient_WSUpgrade), `server/server_test.go` (TestServer_WSUpgrade)
- Library: `nhooyr.io/websocket` chosen for clean `net.Conn` wrapper

**Trade-offs**:
- Pro: HTTP camouflage, harder to fingerprint
- Con: Slight overhead from WS framing (acceptable for anti-censorship)

---

## Hybrid Ticket Renewal (wire-protocol-compliance, 2026-02-12)

**Decision**: Auto-renew tickets when < 50% TTL remaining. Return existing ticket when > 50% TTL.

**Context**: Security recommends frequent rotation, but re-issuing on every auth request wastes CPU.

**Implementation**:
```go
func (h *AuthHandler) respondTicket(w http.ResponseWriter, token, deviceID string) {
    remaining := h.store.Remaining(token)
    total := int(h.store.ticketTTL.Seconds())

    if remaining < total/2 {
        // Auto-renew: < 50% TTL
        ticket, _ := h.store.Issue(deviceID, true)
        respondJSON(w, ticket) // New token returned
    } else {
        // Return existing: > 50% TTL
        ticket := h.store.Get(token)
        respondJSON(w, ticket) // Same token returned
    }
}
```

**Outcome**: Balanced security (regular rotation) and performance (no excessive re-issuing).

**Validation**:
- Tests: `wire/auth_test.go` (TestAuthHandler_TicketRenewal_*)
- Pattern: Old ticket enters grace period automatically when new issued

---

## QUIC UDP Stream Fallback (wire-protocol-compliance, 2026-02-12)

**Decision**: When QUIC datagrams not supported, fall back to stream-based UDP framing.

**Context**: Not all QUIC implementations support unreliable datagrams. Need reliable fallback.

**Implementation**:
```go
// Client DialUDP
func (c *QUICClient) DialUDP(ctx, addr) (net.PacketConn, error) {
    if c.conn.ConnectionState().SupportsDatagrams {
        return c.newDatagramConn(addr), nil // Preferred: datagrams
    }
    // Fallback: stream with length-prefixed framing
    stream, _ := c.sess.OpenStream()
    wire.WriteStreamHeader(stream, wire.StreamTypeUDP, addr)
    return &streamUDPConn{stream: stream}, nil
}

// Server handleStream
switch streamType {
case wire.StreamTypeTCP:
    s.handleTCPStream(stream, addr)
case wire.StreamTypeUDP:
    s.handleUDPStream(stream, addr) // Reuse TCP-WS UDP framing logic
}
```

**Outcome**: UDP works over QUIC even when datagrams disabled. Reuses existing TCP-WS UDP framing.

**Validation**:
- Tests: `wire/quic_test.go` (TestQUICClient_DialUDP_StreamFallback, TestQUICServer_HandleStream_UDP)
- Pattern: Same UDP framing format across TCP-WS and QUIC stream fallback

**Trade-offs**:
- Pro: Universal UDP support, no datagram dependency
- Con: Stream-based UDP adds latency vs unreliable datagrams
