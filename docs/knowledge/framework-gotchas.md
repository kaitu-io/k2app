# Framework Gotchas

Lessons learned from dependency integration.

## uTLS Fingerprint Hardcodes ALPN — Cannot Override (verified, 2026-02-12)

**Issue**: uTLS browser fingerprints (e.g. `HelloChrome_120_PQ`) embed ALPN as part of the TLS fingerprint. Setting `NextProtos` on `utls.Config` does NOT override the fingerprint's ALPN.

**Location**: `u_parrots.go:777` in `github.com/metacubex/utls`:
```go
// Inside HelloChrome_120_PQ spec
&ALPNExtension{AlpnProtocols: []string{"h2", "http/1.1"}}
```

**Impact**: Server MUST accept whatever ALPN the fingerprint sends. For Chrome fingerprints, this means `h2` and `http/1.1`. A server with only `h3` in `NextProtos` will reject TCP-WS connections.

**QUIC exception**: `quic.go` creates its own TLS config with `NextProtos = ["h3"]`. QUIC doesn't use uTLS fingerprinting (QUIC Initial packets are encrypted differently, less exposed to DPI).

**Workaround options**:
1. **Server accepts more protocols** (recommended) — add `h2`, `http/1.1` to server `NextProtos`
2. **Client custom fingerprint** — fork fingerprint to include `h3` (breaks browser mimicry, defeats purpose)
3. **Client removes ALPN extension** — dangerous, makes fingerprint detectable

**Validated in**: TCP-WS ALPN mismatch bugfix (`server/server.go`)

**Pattern**: Treat uTLS fingerprint as immutable black box. Adapt server to fingerprint, not vice versa.

---

## Go crypto/tls: ECH Decryption Independent of ALPN (verified, 2026-02-12)

**Issue**: Concern that changing server `NextProtos` might break ECH encryption.

**Finding**: ECH and ALPN are completely independent in Go's `crypto/tls`. Proven by source code trace:

**Execution order** in `handshake_server.go`:
1. `readClientHello()` line 159: `processECHClientHello()` — ECH decryption happens HERE, replaces outer ClientHello with inner
2. Returns already-decrypted inner ClientHello
3. `processClientHello()` line 299: `negotiateALPN()` — ALPN negotiation uses the inner (decrypted) ClientHello

**Key comment** (`handshake_server.go:147-148`):
```go
// ECH processing has to be done before we do any other negotiation based on
// the contents of the client hello, since we may swap it out completely.
```

**Conclusion**: Any change to `NextProtos` only affects ALPN negotiation, which runs AFTER ECH decryption. ECH uses HPKE keys (`EncryptedClientHelloKeys`), completely separate from `NextProtos`.

**Validated in**: Source code trace of Go 1.25 `crypto/tls`, confirmed by successful ECH+TCP-WS connection after fix.

**Pattern**: ECH is transport-layer encryption of ClientHello extensions. ALPN is application-layer protocol negotiation. They operate at different layers and cannot interfere with each other.

---

## sing-tun gVisor Stack Connection Ownership (l4-architecture, 2026-02-12)

**Issue**: sing-tun's gVisor stack owns TCP/UDP connections. Handler must not close them prematurely.

**Symptom**: Connections die immediately, gVisor panics with "conn already closed".

**Root Cause**: Handler goroutine closes `net.Conn` while gVisor stack still expects to manage it.

**Solution**: Let handler goroutine own connection, defer close in handler:
```go
func (t *ClientTunnel) HandleTCP(conn net.Conn, dest string) {
    defer conn.Close()  // Safe: handler owns connection
    // ... forward data
}
```

**Validated in**: `provider/stack.go`, `core/tunnel.go`

**Source**: l4-architecture feature, T3 (Provider redesign)

---

## smux DefaultConfig vs Custom Config (l4-architecture, 2026-02-12)

**Issue**: smux has conservative defaults (256 streams, 4MB buffer). For VPN use, needs tuning.

**Default**:
```go
config := smux.DefaultConfig()
// MaxReceiveBuffer = 4MB
// MaxStreamBuffer = 64KB
```

**k2 tuning** (wire/tcpws.go):
```go
config := smux.DefaultConfig()
config.MaxReceiveBuffer = 16 * 1024 * 1024  // 16MB
config.MaxStreamBuffer = 1024 * 1024         // 1MB
config.KeepAliveInterval = 30 * time.Second
config.KeepAliveTimeout = 90 * time.Second
```

**Reason**: VPN traffic can burst (video, downloads). Small buffers cause backpressure.

**Validated in**: `wire/tcpws_test.go` (throughput tests with large transfers)

**Source**: l4-architecture feature, T2 (TCP-WS implementation)

---

## QUIC Datagram vs Stream for UDP (l4-architecture, 2026-02-12)

**Issue**: QUIC datagrams are unreliable, not all servers support them.

**Decision**: Try datagrams first, fall back to stream-based UDP framing.

```go
func (c *QUICClient) DialUDP(ctx, addr) (net.PacketConn, error) {
    if c.conn.ConnectionState().SupportsDatagrams {
        return c.newDatagramConn(addr), nil
    }
    // Fallback: stream-based UDP
    return c.newStreamUDPConn(addr), nil
}
```

**Validated in**: `wire/quic_test.go` (TestQUICClient_DialUDP_DatagramFallback)

**Trade-off**: Stream-based UDP adds latency but works everywhere.

**Source**: l4-architecture feature, T1 (QUIC implementation)

---

## net.Pipe Synchronous Blocking (l4-architecture, 2026-02-12)

**Issue**: `net.Pipe()` writes block until reads happen on the other end. Easy to deadlock.

**Symptom**: Test hangs, goroutines stuck in `Write()`.

**Example**:
```go
// WRONG: deadlocks if write exceeds buffer
local, remote := net.Pipe()
remote.Write(data)  // Blocks until local.Read() happens
// But local.Read() never called = deadlock
```

**Solution**: Always read/write in separate goroutines:
```go
local, remote := net.Pipe()
go func() {
    remote.Write(data)
}()
buf := make([]byte, len(data))
local.Read(buf)
```

**Validated in**: Multiple tests using pipe for connection mocking.

**Source**: Documented in CLAUDE.md, validated during testing.

---

## bufio.Reader Required for Stream Protocol (l4-architecture, 2026-02-12)

**Issue**: `ReadStreamHeader`/`ReadStreamResponse` use `io.ByteReader.ReadByte()`. `net.Conn` does not implement this.

**Symptom**: Compilation error `net.Conn does not implement io.ByteReader`.

**Root Cause**: HTTP/3 framing uses single-byte reads. `net.Conn` only has `Read([]byte)`.

**Solution**: Wrap connection with `bufio.Reader`:
```go
conn, _ := dialer.DialTCP(ctx, addr)
br := bufio.NewReader(conn)
header, err := wire.ReadStreamHeader(br)
```

**Validated in**: `wire/stream.go`, `wire/quic.go`, `wire/tcpws.go`

**Pattern**: Always wrap wire protocol connections before reading frames.

**Source**: l4-architecture feature, T1/T2 (wire implementations)

---

## k2rule GeoIP Path Requirement (l4-architecture, 2026-02-12)

**Issue**: k2rule v0.1.1-go treated GeoIP as optional but caused nil panics when rules used `GEOIP,CN`.

**Old behavior**:
```go
k2rule.LoadGeoIP(mmdbPath)  // If path empty, skip — but later Match() panics
```

**v0.3.0-go fix**:
```go
k2rule.Init(&k2rule.Config{
    Rules:     rulesYAML,
    GeoIPPath: mmdbPath,  // If empty + rules use GEOIP = error at init
})
```

**Validation**: Init fails fast with clear error if rules need GeoIP but path missing.

**Pattern**: Fail early at init, not at runtime during Match().

**Source**: l4-architecture feature, T6 (k2rule upgrade)

---

## sing-tun AutoRoute vs Manual Route (l4-architecture, 2026-02-12)

**Issue**: sing-tun can auto-configure routes or require manual setup. macOS/Linux differ.

**macOS**:
```go
tun.New(Options{
    AutoRoute: true,  // Uses system routing table
})
```

**Linux**:
```go
tun.New(Options{
    AutoRoute:   true,
    AutoRedirect: true,  // Also configures iptables
})
```

**Windows**:
```go
tun.New(Options{
    StrictRoute: true,  // Prevents DNS leak
})
```

**OpenWRT**:
```go
tun.New(Options{
    AutoRoute: true,
    // sing-tun detects OpenWRT, uses nftables instead of iptables
})
```

**Validated in**: `provider/tun_desktop.go` (platform-specific Options)

**Pattern**: Each platform has different route management. sing-tun abstracts this.

**Source**: l4-architecture feature, T3 (Provider cross-platform)

---

## Go 1.24+ ECH API: X25519 DHKEM Required (wire-protocol-compliance, 2026-02-12)

**Issue**: Go's `crypto/tls` ECH implementation requires X25519 HPKE keys. Other curves fail at handshake.

**Symptom**: Server accepts non-X25519 keys but ECH handshake fails with cryptic "decryption error".

**Root Cause**: RFC 9180 (HPKE) defines multiple KEMs, but Go TLS only implements X25519 DHKEM (0x0020).

**Solution**: Always generate X25519 keys for ECH:
```go
func GenerateECHKey(publicName string) (tls.EncryptedClientHelloKey, []byte, error) {
    // MUST use X25519, not P-256 or P-384
    privKey, _ := ecdh.X25519().GenerateKey(rand.Reader)
    echKey := tls.EncryptedClientHelloKey{
        Config:     publicName,
        PrivateKey: privKey,
    }
    // Generate config list with X25519 public key
    configList := echKey.GenerateConfig()
    return echKey, configList, nil
}
```

**Validated in**: `wire/ech_test.go` (TestGenerateECHKey), `server/server_test.go` (TestServer_ECHRouting_ECHConnection)

**Pattern**: X25519-only for ECH keys. P-256/P-384 for regular TLS certs is fine.

**Source**: wire-protocol-compliance feature, T1 (ECH key generation)

---

## uTLS + ECH: Config Must Match utls.Config Fields (wire-protocol-compliance, 2026-02-12)

**Issue**: uTLS has its own `utls.Config` type (not `crypto/tls.Config`). Field names almost identical but not interchangeable.

**Symptom**: Compilation error when passing `tls.Config` to `utls.UClient`, or ECH config silently ignored.

**Solution**: Manually copy fields from `tls.Config` to `utls.Config`:
```go
tlsCfg, clientHelloID, _ := wire.BuildClientTLSConfig(cfg)

// WRONG: utls.UClient(conn, tlsCfg, clientHelloID) — type mismatch

// RIGHT: Construct utls.Config manually
uconn := utls.UClient(conn, &utls.Config{
    ServerName:                     tlsCfg.ServerName,
    InsecureSkipVerify:             tlsCfg.InsecureSkipVerify,
    EncryptedClientHelloConfigList: tlsCfg.EncryptedClientHelloConfigList,
}, clientHelloID)
```

**Validated in**: `wire/tcpws.go` (connectTLSWebSocket), `wire/tcpws_test.go` (TestTCPWSClient_uTLS_WithECH)

**Pattern**: Always construct `utls.Config` explicitly. Don't rely on automatic conversion.

**Source**: wire-protocol-compliance feature, T5 (uTLS integration)

---

## coder/websocket: NetConn Requires Context (wire-protocol-compliance, 2026-02-12)

**Issue**: `websocket.NetConn(ctx, wsConn, messageType)` requires long-lived context. If ctx cancelled, NetConn dies.

**Symptom**: WebSocket connection works initially, then dies when ctx cancelled (even if tunnel should stay up).

**Root Cause**: NetConn uses ctx for internal read/write operations, not just for creation.

**Solution**: Pass `context.Background()` or server's long-lived ctx, not per-request ctx:
```go
// Server accepts WebSocket
wsConn, _ := websocket.Accept(tlsConn, nil)

// WRONG: per-request ctx
netConn := websocket.NetConn(ctx, wsConn, websocket.MessageBinary) // Dies when ctx cancelled

// RIGHT: background or server ctx
netConn := websocket.NetConn(context.Background(), wsConn, websocket.MessageBinary)
s.tcpwsSrv.HandleConn(netConn) // Connection lives until explicitly closed
```

**Validated in**: `server/server.go` (handleK2V5), `wire/tcpws.go` (client dial)

**Pattern**: Long-lived connections need long-lived context for websocket.NetConn.

**Source**: wire-protocol-compliance feature, T5/T6 (WebSocket layer)

---

## WebSocket Over Existing TLS: Custom HTTP Transport (wire-protocol-compliance, 2026-02-12)

**Issue**: Standard `websocket.Dial` wants to do its own TLS handshake. But we already have uTLS connection.

**Symptom**: Can't inject existing TLS connection into WebSocket dial, or WS does second handshake (breaks uTLS fingerprint).

**Solution**: Use custom `http.Transport` with `DialTLSContext` that returns existing connection:
```go
// Already have uTLS connection
uconn := utls.UClient(rawConn, &utls.Config{...}, clientHelloID)
uconn.HandshakeContext(ctx)

// Dial WebSocket over existing TLS connection
wsConn, _, err := websocket.Dial(ctx, "wss://"+host+"/k2v5/tunnel", &websocket.DialOptions{
    HTTPClient: &http.Client{
        Transport: &http.Transport{
            DialTLSContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
                return uconn, nil // Inject existing TLS connection
            },
        },
    },
})
```

**Validated in**: `wire/tcpws.go` (connectTLSWebSocket), `wire/tcpws_test.go` (TestTCPWSClient_FullFlow_WS)

**Pattern**: Custom HTTP transport for WebSocket over existing TLS.

**Source**: wire-protocol-compliance feature, T5 (WebSocket + uTLS integration)

---

## smux Over WebSocket: Binary Message Type Required (wire-protocol-compliance, 2026-02-12)

**Issue**: WebSocket has text and binary message types. smux expects raw binary stream.

**Symptom**: smux framing errors, "invalid magic number" panics.

**Root Cause**: WebSocket text messages UTF-8 validate, corrupting binary smux frames.

**Solution**: Always use `websocket.MessageBinary` for NetConn:
```go
// Server
wsConn, _ := websocket.Accept(tlsConn, nil)
netConn := websocket.NetConn(context.Background(), wsConn, websocket.MessageBinary)
sess, _ := smux.Server(netConn, wire.DefaultSmuxConfig())

// Client
wsConn, _, _ := websocket.Dial(ctx, wsURL, nil)
netConn := websocket.NetConn(context.Background(), wsConn, websocket.MessageBinary)
sess, _ := smux.Client(netConn, wire.DefaultSmuxConfig())
```

**Validated in**: `wire/tcpws.go`, `wire/tcpws_test.go` (TestTCPWSClient_FullFlow_WS)

**Pattern**: Binary message type for all non-text protocols over WebSocket.

**Source**: wire-protocol-compliance feature, T5 (WebSocket layer)

---

## miekg/dns: Domain Name Must End with Dot (zero-config-stealth, 2026-02-12)

**Issue**: DNS queries fail silently or return NXDOMAIN if domain doesn't end with `.`

**Symptom**: `crypto.cloudflare.com` query fails, `crypto.cloudflare.com.` succeeds.

**Root Cause**: DNS wire protocol requires fully-qualified domain names (FQDN) to end with root label (empty string = `.`).

**Solution**: Always append trailing dot to domains before DNS query:
```go
domain := "crypto.cloudflare.com"
if domain[len(domain)-1] != '.' {
    domain += "."
}
c := new(dns.Client)
msg := new(dns.Msg)
msg.SetQuestion(domain, dns.TypeHTTPS) // Now valid FQDN
```

**Validated in**: `wire/ech_provider.go` (FetchTemplate), `wire/ech_provider_test.go`

**Pattern**: DNS libraries expect FQDNs, not relative names. Always normalize before query.

**Source**: zero-config-stealth feature, T1 (ECH DNS fetching)

---

## ECH Binary Format: Big-Endian Multi-Byte Fields (zero-config-stealth, 2026-02-12)

**Issue**: ECH config parsing fails with "truncated" errors when using native endianness.

**Symptom**: `ParseECHConfigBytes` reads garbage lengths, overruns buffer.

**Root Cause**: TLS wire formats use big-endian (network byte order) for all multi-byte integers. Go's native endianness is little-endian on most platforms.

**Solution**: Always use `binary.BigEndian` for TLS/ECH parsing:
```go
// Read 2-byte length prefix
listLen := int(binary.BigEndian.Uint16(data[0:2]))

// Read version (2 bytes)
version := binary.BigEndian.Uint16(data[0:2])

// WRONG: binary.LittleEndian or manual bit shifting (unless you're sure of endianness)
```

**Validated in**: `wire/ech_provider.go` (ParseECHConfigBytes, GenerateECHKeyFromTemplate), `wire/ech_provider_test.go`

**Pattern**: TLS/HTTPS/QUIC wire formats = always big-endian. Use `binary.BigEndian` for reads and writes.

**Source**: zero-config-stealth feature, T1 (ECH config parsing)

---

## Go crypto/tls ECH: Must Use X25519, Not P-256 (zero-config-stealth, 2026-02-12)

**Issue**: ECH handshake fails with "decryption error" when using P-256 ECDH keys.

**Symptom**: Server accepts key at generation, but client handshake fails silently.

**Root Cause**: Go's `crypto/tls` ECH implementation only supports X25519 DHKEM (kem_id=0x0020). Other curves defined in RFC 9180 but not implemented.

**Solution**: Always generate X25519 keys for ECH:
```go
// RIGHT: X25519 for ECH
privKey, _ := ecdh.X25519().GenerateKey(rand.Reader)
echKey := tls.EncryptedClientHelloKey{
    Config:     echConfig,
    PrivateKey: privKey.Bytes(),
}

// WRONG: P-256 for ECH (will fail at handshake)
privKey, _ := ecdh.P256().GenerateKey(rand.Reader)
```

**Note**: P-256/P-384 ECDSA certs for TLS certificates are fine. X25519 requirement is ECH-specific.

**Validated in**: `wire/ech.go` (EnsureECHPrivateKey), `wire/ech_provider.go` (GenerateECHKeyFromTemplate), all ECH tests

**Pattern**: X25519-only for ECH keys. Check kem_id in template must be 0x0020.

**Source**: zero-config-stealth feature, T1/T2 (ECH key generation)

---

## uTLS Outer SNI Binding to ECH public_name (adoption-scan, unverified)

**Issue**: Cannot independently set outer SNI when ECH is enabled. uTLS forces `ClientHello.server_name = ECH config's public_name`.

**Symptom**: `sni=custom.example.com` URL parameter ignored. Outer SNI always equals ECH public_name.

**Root Cause**: uTLS strictly binds outer SNI to ECH config's public_name field. This is correct per ECH spec — outer SNI must match for camouflage.

**Decision**: Removed `sni` URL parameter entirely. Outer SNI = `cloudflare-ech.com` (from ECH config), matching billions of real Cloudflare connections.

**Validated in**: `wire/ech.go` (BuildClientTLSConfig), protocol spec section 3.1

**Pattern**: ECH config structure dictates outer SNI. Don't fight it — use same public_name as real traffic.

**Source**: zero-config-stealth feature, T5 (SNI cleanup)

---

## RSA + EC Dual Certificates for Algorithm Camouflage (adoption-scan, unverified)

**Issue**: DPI can distinguish tunnels from real TLS by encrypted Certificate record length. Self-signed cert (500B) vs real cert chain (3-4KB) is obvious.

**Solution**: Generate both RSA-2048 and EC P-256 certs. Server selects based on ClientHello preferences. Both padded to match target site's cert length.

```go
type CertPair struct {
    RSA, EC      tls.Certificate
    RSAPinHash   [32]byte  // sha256(PKIX RSA pubkey)
    ECPinHash    [32]byte  // sha256(PKIX EC pubkey)
}
```

**Client pin format**: `pin=sha256:<rsa_b64>,sha256:<ec_b64>` — both hashes in URL.

**EC CA can sign both**: An ECDSA CA (like our DigiCert-mimicking Kaitu CA) signs both RSA and EC server certs. CA key type only determines signature algorithm, not subject key type.

**Validated in**: `wire/certgen.go` (GenerateCertPair), `server/server.go` (SetCertPair, GetCertificate callback), `wire/tlspad/`

**Pattern**: Maintain algorithm diversity. Server picks best match per connection. Pin both hashes.

**Source**: tls-record-padding feature, T2 (Dual keypair generation)

---

## TLS Record Padding: Probe Target to Match Cert Length (adoption-scan, unverified)

**Issue**: TLS 1.3 hides cert content but not record length. Short encrypted record = obvious tunnel.

**Solution**: Probe target site (public_name), measure its Certificate message length, pad server cert to match via X.509 extensions.

**Flow**:
1. `ProbeTarget("cloudflare-ech.com")` → observe cert chain length (e.g., 3800B)
2. `MeasureCertDERLen(&cert)` → server cert length (e.g., 500B)
3. `GeneratePaddedCert(privKey, targetLen)` → padded cert ≈ 3800B

**Fallback**: If probe fails (restricted network), server logs warning, continues with unpadded cert. Less stealth but functional.

**Validated in**: `wire/tlspad/` (padding package), `server/server.go` (setupAutoProvision step 8-9)

**Pattern**: Measure real-world observable metrics, match them. Padding disabled gracefully on failure.

**Source**: tls-record-padding feature, T1 (TLS record padding)
