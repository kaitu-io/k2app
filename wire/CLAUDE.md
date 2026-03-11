# Wire Layer (wire/ — k2v5 composite protocol)

~330 test functions. QUIC/H3 primary, TCP-WS/smux fallback.

## Interfaces (wire.go)

```go
// Client transport (L4 proxy model)
type Dialer interface {
    DialTCP(ctx context.Context, addr string) (net.Conn, error)
    DialUDP(ctx context.Context, addr string) (net.PacketConn, error)
    Close() error
}

// Server transport
type Listener interface {
    AcceptTCP(ctx context.Context) (conn net.Conn, addr string, err error)
    ServeUDP(ctx context.Context, handler UDPHandler) error
    Close() error
}

type UDPHandler interface {
    HandleUDP(conn net.PacketConn, addr string)
}

// Optional — type-asserted on Dialer, not part of Dialer interface
type StatsProvider interface {
    WireStats() WireStats
}
```

## Config (wire.go)

`Config` parsed from `k2v5://udid:token@host:port?ech=...&pin=...&fp=...&hop=START-END`

Fields: UDID, Token, Host, Port, ECH, Fingerprint, HopStart, HopEnd, Insecure, Pin.

## Files

- `wire.go` — Dialer/Listener/UDPHandler interfaces, `WireStats`/`StatsProvider`, Config, `ParseURL()`
- `auth.go` — `TicketStore` (1h TTL, 30s grace), `AuthHandler` (JWT full + ticket)
- `auth_info.go` — `AuthInfo` (UDID+Token), `EnsureAuth()` (load or generate credentials)
- `ech.go` — ECH key management (generate, save/load, rotation, migration), `BuildClientTLSConfig`, SNI peek, reverse proxy
- `ech_provider.go` — `ECHProvider` interface, `LocalECHProvider` (Cloudflare DNS HTTPS fetch), `ParseECHConfigBytes`, `GenerateECHKeyFromTemplate`, 3-tier fallback
- `ca.go` — `BuildDefaultCAPool` (system CA + embedded Kaitu CA - Chinese CA blocklist), `IsBlockedCA`
- `kaitu_ca.pem` — Embedded Kaitu CA cert (EC P-256, disguised as DigiCert Global Root CA)
- `certgen.go` — `GenerateCertPair` (RSA-2048 + EC P-256), `EnsureCertPair`, pin hash generation
- `stream.go` — QUIC varint encoding, `StreamHeader` (type 0x4B32 H3 frame), request/response
- `udp.go` — `UDPFrame` wire format, `SessionAllocator`, `SessionMap`, smux framing
- `quic.go` — QUIC config, `StreamConn` adapter, `QUICClient`/`QUICServer`, relay UDP health tracking (`quicUDPMux`: consecutive timeouts + sliding-window loss rate → `ErrDatagramRelayUnhealthy` for TCP-WS fallback)
- `quic_limits_desktop.go` — Desktop/server QUIC flow control windows (8 MB stream / 20 MB conn / 256 streams)
- `quic_limits_mobile.go` — Mobile QUIC flow control windows (2 MB stream / 8 MB conn / 64 streams) — iOS NE ~50 MB jetsam limit
- `tcpws.go` — smux config, `TCPWSClient`/`TCPWSServer`
- `transport.go` — `TransportManager`: QUIC strongly preferred, TCP-WS last-resort fallback. Consecutive handshake failure threshold (3, `handshakeFailStreak`) activates fallback — no exponential backoff. Background QUIC probe loop (30s interval) actively tests recovery during fallback. `DialTCP` tries QUIC first (with context budget 3-5s), falls back to TCP-WS on failure. `FallbackActive` tracks QUIC→TCP-WS state. Direct `*QUICClient`/`*TCPWSClient` type assertions for `ResetConnections()`.
- `ip.go` — `ProbePublicIP`, `FormatSslipHost`, `HasIPv6` (server auto-provisioning helpers)
- `tlspad/` — TLS record padding (probe target cert length, pad server cert to match)
- `arc/` — K2CC (PCC Vivace) congestion control for QUIC
- `VOCABULARY.md` — Three-layer vocabulary convention (Wire/Relay/Tunnel)
- `contract_test.go` — 8 transport behavior contract tests (startup, fallback, recovery, datagram degradation, network switch, slow upstream)
- `testmain_test.go` — goleak `TestMain` with quic-go and smux ignore patterns

## ECH Rotation Flow

1. `EnsureECHPrivateKey(keyPath)` — load or generate X25519 HPKE key (auto-migrates old format)
2. `ECHProvider.FetchTemplate()` → Cloudflare DNS HTTPS record → extract config_id, cipher_suites, public_name
3. `GenerateECHKeyFromTemplate(tmpl, privKey)` → forged config (config_id = Cloudflare's + 1)
4. `SaveECHConfig(dir, config)` / `LoadAllECHConfigs(dir)` → multi-config storage (sorted by config_id desc)
5. `BuildECHKeys(privKey, configs)` → `[]tls.EncryptedClientHelloKey` for TLS config

## Client TLS Verification Priority

1. `pin=sha256:...` → InsecureSkipVerify + pin hash check (most secure)
2. `insecure=1` → InsecureSkipVerify (dev/testing only)
3. Default → system CA + Kaitu CA - Chinese CA blocklist (CNNIC, WoSign, WoTrus, CFCA, GDCA, SHECA)

## Gotchas

- `quicStreamConn.Close()` must call both `CancelRead(0)` and `Stream.Close()`. quic-go's `Stream.Close()` only closes the write direction (FIN). Without `CancelRead`, the receive half never completes and the stream slot leaks — causing `OpenStreamSync` to block at `MAX_STREAMS`. Fixed in the custom `Close()` method.
- `ReadStreamHeader`/`ReadStreamResponse` need `io.ByteReader` — wrap conn with `bufio.NewReader(conn)`.
- `net.Pipe()` is synchronous — structure goroutines carefully in tests.
- ECH base64 in URLs must use `base64.URLEncoding` (standard `+` corrupted by `url.Query()`).
- uTLS forces outer SNI = ECH config's `public_name` — cannot be set independently.
- QUIC connection liveness: quic-go's KeepAlive (10s) + MaxIdleTimeout (30s) detect dead connections at protocol level. `conn.Context().Done()` fires when connection dies. `connect()` checks this before reusing cached conn — no manual `resetConnection()` needed on error paths.
- Stream response timeout ≠ transport failure: A 5s stream response timeout means the server's upstream `net.Dial` to the destination is slow/failing — NOT that QUIC transport is broken (OpenStreamSync succeeded = transport is alive). Do NOT increment `handshakeFailStreak` or trigger TCP-WS fallback on stream timeouts. `handshakeFailStreak` is strictly for QUIC handshake failures (UDP blocked by firewall).
- Relay UDP unhealthy → `ErrDatagramRelayUnhealthy`: When relay datagram path has high loss or consecutive timeouts, `DialUDP` returns `ErrDatagramRelayUnhealthy` for per-request TCP-WS fallback. Relay-layer metrics never trigger wire-layer actions (port hop, reconnect). See `VOCABULARY.md`.
- QUIC flow control is platform-specific (build tags `ios || android` vs desktop) — mobile uses smaller windows (2 MB stream / 8 MB conn vs 8 MB / 20 MB desktop) to stay under iOS NE jetsam limit.
- QUIC stream responses have 5s timeout to prevent indefinite blocking on slow/dead servers.
- `SendDatagram` has 2s timeout (`sendDatagramTimeout`, `ErrDatagramSendTimeout`) — quic-go's 32-frame datagram queue blocks indefinitely under congestion. Timeout converts blocking into dropping (protocol-correct for unreliable DATAGRAM). Applies to both client `quicUDPConn.WriteTo` and server `serverUDPConn.WriteTo`.

## Testing

```bash
go test ./wire/...                          # ~330 tests (unit + integration + contract + rotation + CA)
go test -run TestContract -v ./wire/...     # contract tests only
```
