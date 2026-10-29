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

**Outcome**: TCP-WS traffic looks like WebSocket to DPI.

> **Superseded (2026-07)**: the `POST /k2v5/auth` ticket endpoint referenced here was removed. Device auth now rides the metadata stream on both transports (see `wire/k2v5-protocol-spec.md` §4).

**Validation**:
- Tests: `wire/tcpws_test.go` (TestTCPWSClient_WSUpgrade), `server/server_test.go` (TestServer_WSUpgrade)
- Library: `coder/websocket` chosen for clean `net.Conn` wrapper

**Trade-offs**:
- Pro: HTTP camouflage, harder to fingerprint
- Con: Slight overhead from WS framing (acceptable for anti-censorship)

---

## Hybrid Ticket Renewal (wire-protocol-compliance, 2026-02-12)

> **SUPERSEDED (2026-07)** — historical record only. This ticket-renewal design (`POST /k2v5/auth`, `AuthHandler`, `TicketStore`, `wire/auth_test.go`) was never wired end-to-end and has been removed. Device auth now rides the metadata stream, validated by the `Validator`/`AuthVerdict` chain (four-state: OK / no opinion / explicit reject 401-403 / cannot determine -1), gated per-connection by `K2_ENFORCE_AUTH`. See `wire/k2v5-protocol-spec.md` §4. The code snippet below no longer exists.

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

---

## ECH Config Forging from DNS Template (zero-config-stealth, 2026-02-12)

**Decision**: Query real Cloudflare ECH config via DNS, extract template fields, forge config with config_id + 1.

**Context**: ECH configs need to look plausible to censors. Random configs may stand out. Cloudflare has production ECH deployment, their configs provide realistic template.

**Implementation**:
```go
// Query DNS HTTPS record (type 65)
provider := &LocalECHProvider{
    DNSServer: "1.1.1.1:53",
    Domain:    "crypto.cloudflare.com",
}
tmpl, _ := provider.FetchTemplate()

// Extract: config_id, kem_id, cipher_suites, public_name, max_name_len
// Forge: config_id = template + 1 (mimics real traffic, avoids collision)
key, configList, _ := GenerateECHKeyFromTemplate(tmpl, privKey)
```

**Outcome**: k2 ECH configs have same cipher_suites, kem_id, public_name as real Cloudflare. config_id incremented to avoid exact duplication while staying plausible.

**Validation**:
- Tests: `wire/ech_provider_test.go` (TestParseECHConfigBytes, TestGenerateECHKeyFromTemplate, TestLocalECHProvider_FetchTemplate)
- Integration: Server auto-generates forged ECH config on first startup

**3-Tier Fallback**:
1. Live DNS query (preferred)
2. Disk cache (`cert_dir/ech_template.json`) if DNS fails
3. Hardcoded defaults (config_id=97, X25519, cloudflare-ech.com) as last resort

**Pattern**: Template-based forgery provides plausible deniability. Censors see traffic matching known legitimate ECH deployments.

---

## ECH Config Rotation with Shared HPKE Key (zero-config-stealth, 2026-02-12)

**Decision**: Server maintains multiple ECH configs (different config_ids) but shares single HPKE private key.

**Context**: Clients cache ECH configs. When server rotates, old clients fail if old config rejected. Multi-config support allows graceful rotation.

**Implementation**:
```go
// Split storage
cert_dir/ech.key              // Single X25519 HPKE private key (persistent)
cert_dir/ech-configs/          // Directory of configs
  ├─ config-98.bin             // Old config
  └─ config-99.bin             // New config (latest)

// Server loads all configs with same key
privKey := EnsureECHPrivateKey("cert_dir/ech.key")
allConfigs := LoadAllECHConfigs("cert_dir/ech-configs/")
tlsCfg.EncryptedClientHelloKeys = BuildECHKeys(privKey, allConfigs)
```

**Outcome**: Old clients with old ECH config continue working. New clients get latest config. Server accepts both.

**Validation**:
- Tests: `wire/ech_test.go` (TestEnsureECHPrivateKey, TestSaveLoadECHConfig, TestBuildECHKeys)
- Integration: Server startup in `server/server.go` (setupAutoProvision)

**Trade-offs**:
- Pro: Graceful rotation, no client breakage
- Con: Multiple configs slightly increase server TLS handshake overhead (negligible)

**Pattern**: Split key (persistent) from config (ephemeral) enables rotation without re-key.

---

## Chinese CA Blocklist via VerifyPeerCertificate (zero-config-stealth, 2026-02-12)

**Decision**: Block Chinese CAs using `tls.Config.VerifyPeerCertificate` callback instead of modifying system cert pool.

**Context**: Go's `x509.SystemCertPool()` returns opaque pool with no Remove API. Chinese CAs (CNNIC, WoSign, CFCA, etc.) have history of mis-issuance and state control.

**Blocked Organizations**:
```go
blockedCAOrgs = []string{
    "CNNIC", "WoSign", "WoTrus", "CFCA", "GDCA", "SHECA",
    "China Financial Certification Authority",
    "Shanghai Electronic Certification Authority",
    "Guang Dong Certificate Authority",
}
```

**Implementation**:
```go
tlsCfg.RootCAs = BuildDefaultCAPool() // system + Kaitu CA
tlsCfg.VerifyPeerCertificate = func(rawCerts [][]byte, chains [][]*x509.Certificate) error {
    // Runs AFTER standard CA chain validation
    for _, chain := range chains {
        for _, cert := range chain {
            if IsBlockedCA(cert) {
                return fmt.Errorf("blocked CA: %s", cert.Issuer.Organization)
            }
        }
    }
    return nil
}
```

**Outcome**: Certificates issued by blocked CAs fail handshake even if chain validates against system pool.

**Validation**:
- Tests: `wire/ca_test.go` (TestIsBlockedCA, TestBuildDefaultCAPool)
- Pattern: Post-validation rejection preserves standard TLS chain validation

**Trade-offs**:
- Pro: Clean blocklist implementation, no system pool mutation
- Con: Blocked CAs still in system pool (cert validates before rejection callback)

**Security Note**: VerifyPeerCertificate runs AFTER standard validation. Blocklist is defense-in-depth, not primary trust mechanism.

---

## Server Zero-Config with Auto-Provisioning Flow (zero-config-stealth, 2026-02-12)

**Decision**: `k2s run` with no config file auto-generates all necessary credentials and logs connection URL.

**Context**: Traditional VPN setup requires: TLS certs, ECH keys, auth tokens, URL construction. Manual setup error-prone and time-consuming.

**6-Step Auto-Provision Flow** (`server/server.go:setupAutoProvision`):
```go
1. Ensure cert_dir exists (os.MkdirAll)
2. EnsureDualCertPair(cert_dir) — generate RSA+EC TLS certs
3. SetupECHRotation(certDir, provider) — fetch template, forge configs
4. ProbeAndApplyPadding(certPair, echConfig) — TLS fingerprint mimicry
5. EnsureAuth(cert_dir/auth.json) — generate UDID+token
6. Log k2v5://UDID:TOKEN@HOST:PORT?ech=<b64>&pin=<dual_pins>
```

**Default Configuration**:
```go
cert_dir: ~/.k2s/ (Linux/macOS), %APPDATA%\k2s\ (Windows)
listen:   :443
ech.provider: local (Cloudflare DNS)
```

**Outcome**: User runs `k2s run`, waits 2 seconds, copies URL, pastes to client. Zero manual cert/key generation.

**Validation**:
- Tests: `server/server_test.go` (integration test with zero config)
- Files: `wire/auth_info.go` (EnsureAuth), `config/config.go` (DefaultServerConfig)

**Trade-offs**:
- Pro: Instant setup, no expertise required
- Con: Self-signed certs require `insecure=1` URL parameter (acceptable for personal VPN)

**Pattern**: Auto-provisioning unifies cert, ECH, padding, and auth generation into single startup flow.

---

## SNI Cleanup: Outer SNI = ECH Public Name (zero-config-stealth, 2026-02-12)

**Decision**: Remove `sni` URL parameter, `SNI` field from Config. Outer SNI always equals ECH config's `public_name`.

**Context**: The current uTLS client emit sets outer SNI = ECH config's public_name (`handshake_client.go:310`), and a configurable `sni` parameter that silently had no effect was misleading — so it was removed. **The original premise that "independent SNI control is impossible" is inaccurate**: the metacubex server does not enforce outer-SNI == public_name (`ech.go:610`), so decoupling is feasible by overriding the emit. Whether to re-expose it (as a server-allowlisted decoy pool, not a free client knob) is re-evaluated in `docs/superpowers/specs/2026-08-10-outer-sni-camouflage-design.md`. [Correction added 2026-08-10.]

**Old Behavior**:
```go
cfg.SNI = "www.microsoft.com" // Default fake SNI
// URL parameter: k2v5://...?sni=custom.example.com
```

**New Behavior**:
```go
// No SNI field, no URL parameter
// Outer SNI = echConfig.public_name (from template = "cloudflare-ech.com")
```

**Outcome**: Simplified configuration, removed misleading parameter. ECH handshake dictates outer SNI.

**Validation**:
- Tests: All existing tests updated to remove SNI assertions
- Protocol spec: Updated sections 3.1, 3.2, 3.3, 12.4 to document new behavior

**Trade-offs**:
- Pro: No user confusion, matches uTLS reality
- Con: Less flexibility (but flexibility was illusory — uTLS ignored custom SNI anyway)

**Pattern**: Remove config knobs that don't actually control behavior.
