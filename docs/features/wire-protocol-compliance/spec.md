# Feature Spec: Wire Protocol Compliance

## Meta

| Field | Value |
|-------|-------|
| Feature | wire-protocol-compliance |
| Status | Draft |
| Created | 2026-02-12 |
| Baseline | k2-baseline (wire/ layer exists, 91 tests) |
| Spec ref | docs/k2v5-protocol-spec.md |

## Problem Statement

The wire/ package implements the k2v5 protocol but has 16 compliance gaps against the protocol spec. Critical issues: server ECH routing is non-functional (all connections go to k2v5), uTLS fingerprinting is defined but never applied, and the WebSocket layer is missing entirely. These gaps make the server trivially detectable by censors.

## Scope

### In Scope

1. Server ECH & Camouflage (gaps #1-4)
2. uTLS Fingerprinting for TCP-WS (gaps #5-7)
3. WebSocket Layer (gap #8)
4. Auth & UDP Housekeeping (gaps #9-13)

### Out of Scope

- QUIC fingerprinting (future: uquic investigation)
- IP packet framing (core layer, not wire)
- PCC algorithm changes
- Mobile platform integration

## Design

### D1: Server ECH & Camouflage

**Current**: `PeekClientHelloSNI` only extracts SNI. `_ = sni` routes everything to k2v5.

**Target**: Full ECH-based routing with reverse proxy camouflage.

#### D1.1: ECH Detection in ClientHello Peek

Enhance `PeekClientHelloSNI` → `PeekClientHello` returning:
```go
type ClientHelloInfo struct {
    SNI    string // outer SNI (plaintext)
    HasECH bool   // true if encrypted_client_hello extension (0xfe0d) present
}

func PeekClientHello(data []byte) (ClientHelloInfo, error)
```

During extension scanning (existing loop in `ech.go:132-141`), also check for `extType == 0xfe0d`.

#### D1.2: Server-Side ECH Decryption

Configure `tls.Config.EncryptedClientHelloKeys` on the server (Go 1.24+ required):
```go
s.TLSConfig = &tls.Config{
    Certificates:            []tls.Certificate{cert},
    EncryptedClientHelloKeys: echKeys, // loaded from key file
    MinVersion:              tls.VersionTLS13,
    NextProtos:              []string{"h3"},
}
```

After TLS handshake, `tlsConn.ConnectionState().ServerName` reveals the **inner** (real) SNI when ECH is used.

#### D1.3: SNI-Based Routing

In `server.go:handleRawTCP`:
```
Peek ClientHello → extract outerSNI + hasECH
  ├─ hasECH=true  → TLS handshake → check inner SNI
  │   ├─ inner SNI matches server hostname → k2v5 handler
  │   └─ inner SNI doesn't match → close (invalid ECH config)
  └─ hasECH=false → ReverseProxy(conn, data, outerSNI+":443")
```

Non-ECH connections are transparently proxied to the real website at the outer SNI, making the server indistinguishable from a legitimate web server.

#### D1.4: ECH Key Management

`ServerConfig` gains ECH section:
```yaml
ech:
  key_path: /etc/k2s/ech-key.pem    # HPKE private key + ECH config
  public_name: www.microsoft.com     # outer SNI for ECH config generation
```

Behavior:
- On first start: if `key_path` doesn't exist, auto-generate HPKE key pair + ECH config, write to file
- On subsequent starts: load from file
- ECH config (public part) is distributed to clients via the `ech=` URL parameter

Implementation: Use Go 1.24 `crypto/tls` ECH key generation APIs (`tls.EncryptedClientHelloKey`).

### D2: uTLS Fingerprinting

**Current**: `BuildClientTLSConfig` returns `utls.ClientHelloID` but both `QUICClient` and `TCPWSClient` discard it with `_`. All connections use vanilla Go `crypto/tls`.

**Target**: TCP-WS uses uTLS for browser-like TLS fingerprints.

#### D2.1: TCP-WS uTLS Integration

In `tcpws.go:connect()`, replace:
```go
tlsConn := tls.Client(conn, tlsCfg)
tlsConn.HandshakeContext(ctx)
```

With:
```go
uconn := utls.UClient(conn, &utls.Config{
    ServerName:                     tlsCfg.ServerName,
    InsecureSkipVerify:             tlsCfg.InsecureSkipVerify,
    EncryptedClientHelloConfigList: tlsCfg.EncryptedClientHelloConfigList,
}, clientHelloID)
uconn.HandshakeContext(ctx)
```

`BuildClientTLSConfig` already returns `clientHelloID` — callers must stop discarding it.

#### D2.2: QUIC Fingerprinting (Deferred)

QUIC stays with apernet/quic-go and standard Go TLS. The QUIC handshake is encrypted after the Initial Packet, so TLS fingerprint is less exposed than TCP.

**Future TODO**: Investigate merging uquic fingerprint capabilities into apernet/quic-go fork. Track in project docs.

### D3: WebSocket Layer

**Current**: TCP-WS does `TCP → TLS → smux` directly. No WebSocket.

**Target**: `TCP → TLS → WebSocket upgrade → smux over WS`.

#### D3.1: Client WebSocket Upgrade

After TLS handshake, client sends HTTP Upgrade request to `/k2v5/tunnel`:
```
GET /k2v5/tunnel HTTP/1.1
Host: <server>
Upgrade: websocket
Connection: Upgrade
```

On success, the WS connection wraps `net.Conn` for smux.

#### D3.2: Server WebSocket Accept

In `server.go:handleK2V5`, after TLS, peek first bytes:
- `POST` → auth handler (existing)
- `GET /k2v5/tunnel` with WS upgrade → accept upgrade → pass WS conn to `tcpwsSrv.HandleConn()`

#### D3.3: Library

Use `nhooyr.io/websocket` or `gorilla/websocket`. Decision deferred to implementation based on API fit. Key requirement: the library must provide a `net.Conn`-compatible wrapper for smux.

### D4: Auth Lifecycle Fixes

#### D4.1: Hybrid Ticket Renewal

In `auth.go:respondTicket`, check remaining TTL:
- **> 50% remaining**: return current ticket info (cheap, no work)
- **< 50% remaining**: call `store.Issue(deviceID, udpEnabled)` to rotate. Old ticket enters grace period automatically.

This gives security benefits of rotation without overhead on every request.

#### D4.2: TicketStore Background Cleanup

Add `StartCleanup(interval time.Duration)` to `TicketStore`:
- Periodically scan `byToken` map, remove expired tickets
- Periodically scan `graceTokens` map, remove expired grace entries
- Called by server on startup with sensible interval (e.g., 5 minutes)

### D5: UDP Resilience

#### D5.1: UDPSessionMap MaxSize Enforcement

In `udp.go:Store()`, check `len(sessions) >= maxSize` before adding. If at capacity, reject the new session (return error or bool).

#### D5.2: UDPSessionMap Background Cleanup

Add `StartCleanup(interval time.Duration)` that periodically calls `Cleanup()` to remove idle sessions. Called by server on startup.

#### D5.3: QUIC UDP Stream Fallback

In `quic.go:DialUDP`, if `SendDatagram` returns error (datagrams not supported), fall back to opening a QUIC stream with smux-framed UDP messages (same format as TCP-WS UDP path).

Server-side: `QUICServer.handleStream` should accept `StreamTypeUDP` (currently only accepts `StreamTypeTCP`).

## Acceptance Criteria

### ECH & Camouflage
- **AC1**: `PeekClientHello` returns `hasECH=true` for ClientHello with `0xfe0d` extension
- **AC2**: `PeekClientHello` returns `hasECH=false` for ClientHello without ECH
- **AC3**: Server auto-generates ECH key pair on first start when key file missing
- **AC4**: Server loads ECH keys from existing file on subsequent starts
- **AC5**: ECH connections with matching inner SNI are handled as k2v5
- **AC6**: Non-ECH connections are reverse-proxied to outer SNI website
- **AC7**: `ServerConfig` YAML supports `ech.key_path` and `ech.public_name`

### uTLS Fingerprinting
- **AC8**: TCP-WS client uses `utls.UClient` with configured `ClientHelloID`
- **AC9**: TLS fingerprint matches configured profile (chrome/firefox/safari/random)
- **AC10**: ECH works correctly through uTLS connection

### WebSocket
- **AC11**: TCP-WS client performs WS upgrade to `/k2v5/tunnel` before smux
- **AC12**: Server accepts WS upgrade and creates smux session over WS conn
- **AC13**: TCP and UDP proxy work correctly over WS+smux transport
- **AC14**: Auth (POST /k2v5/auth) continues to work without WS upgrade

### Auth Lifecycle
- **AC15**: Ticket with > 50% TTL remaining: `respondTicket` returns current info without re-issuing
- **AC16**: Ticket with < 50% TTL remaining: `respondTicket` issues new ticket, old enters grace
- **AC17**: Expired tickets are cleaned up by background goroutine
- **AC18**: Expired grace tokens are cleaned up by background goroutine

### UDP Resilience
- **AC19**: `UDPSessionMap.Store` rejects when at `maxSize` capacity
- **AC20**: Idle UDP sessions are cleaned up by background goroutine
- **AC21**: QUIC UDP falls back to stream-based framing when datagrams not supported
- **AC22**: Server QUIC handler accepts `StreamTypeUDP` streams

## File Impact

| File | Change Type | Description |
|------|-------------|-------------|
| `wire/ech.go` | Modify | `PeekClientHello` with ECH detection, ECH key generation |
| `wire/auth.go` | Modify | Hybrid renewal in `respondTicket`, `StartCleanup` |
| `wire/udp.go` | Modify | `Store` maxSize check, `StartCleanup` |
| `wire/quic.go` | Modify | UDP stream fallback in `DialUDP`, server `StreamTypeUDP` handling |
| `wire/tcpws.go` | Modify | uTLS `UClient`, WebSocket upgrade |
| `server/server.go` | Modify | ECH routing logic, WS upgrade handling, ECH key loading |
| `config/config.go` | Modify | `ServerConfig.ECH` section |
| `wire/ech_test.go` | Modify | Tests for `PeekClientHello`, ECH key generation |
| `wire/auth_test.go` | Modify | Tests for hybrid renewal, cleanup |
| `wire/udp_test.go` | Modify | Tests for maxSize enforcement, cleanup |
| `wire/quic_test.go` | Modify | Tests for UDP stream fallback |
| `wire/tcpws_test.go` | Modify | Tests for uTLS, WS upgrade |
| `server/server_test.go` | Modify | Tests for ECH routing, WS handling |

## Dependencies

- Go 1.24+ (for `tls.Config.EncryptedClientHelloKeys`)
- `github.com/metacubex/utls` (already imported, now actually used)
- WebSocket library: `nhooyr.io/websocket` or `gorilla/websocket` (new dependency)

## Risks

| Risk | Mitigation |
|------|------------|
| Go 1.24 ECH API may have undocumented edge cases | Write focused tests with real ECH key pairs |
| uTLS + ECH combo may have interop issues | Test with actual ECH configs, not just base64 stubs |
| WebSocket library choice affects smux compat | Verify `net.Conn` wrapper before committing to library |
| QUIC UDP stream fallback adds complexity | Reuse existing smux UDP framing code from TCP-WS path |
