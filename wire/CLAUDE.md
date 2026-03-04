# Wire Layer (wire/ — k2v5 composite protocol)

~190 test functions. QUIC/H3 primary, TCP-WS/smux fallback.

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
- `quic.go` — QUIC config, `StreamConn` adapter, `QUICClient`/`QUICServer`, port hopping, datagram health tracking (`quicUDPMux`: consecutive timeouts + sliding-window loss rate → hop → recovery probe cycle)
- `quic_limits_desktop.go` — Desktop/server QUIC flow control windows (8 MB stream / 20 MB conn / 256 streams)
- `quic_limits_mobile.go` — Mobile QUIC flow control windows (2 MB stream / 4 MB conn / 64 streams) — iOS NE ~50 MB jetsam limit
- `tcpws.go` — smux config, `TCPWSClient`/`TCPWSServer`
- `transport.go` — `TransportManager`: QUIC preferred, TCP-WS fallback. `DialTCP` with QUIC backoff (30s→60s→120s). `DialUDP` with `ErrDatagramUnhealthy` fallback (not counted as transport failure). `FallbackActive` tracks QUIC→TCP-WS state. Direct `*QUICClient`/`*TCPWSClient` type assertions for `ResetConnections()`.
- `ip.go` — `ProbePublicIP`, `FormatSslipHost`, `HasIPv6` (server auto-provisioning helpers)
- `tlspad/` — TLS record padding (probe target cert length, pad server cert to match)
- `arc/` — PCC Vivace congestion control for QUIC

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

- `ReadStreamHeader`/`ReadStreamResponse` need `io.ByteReader` — wrap conn with `bufio.NewReader(conn)`.
- `net.Pipe()` is synchronous — structure goroutines carefully in tests.
- ECH base64 in URLs must use `base64.URLEncoding` (standard `+` corrupted by `url.Query()`).
- uTLS forces outer SNI = ECH config's `public_name` — cannot be set independently.
- Dead QUIC conn: `OpenStreamSync` failure caches stale connection — both `DialTCP` and `DialUDP` must call `resetConnection()` for non-context errors so next retry builds fresh conn.
- Datagram recovery cycle: 3 port hops exhaust → fallback to TCP-WS streams for 3 min → reset hop counter → fresh QUIC round. Cycle repeats until network change or datagram recovery.
- QUIC flow control is platform-specific (build tags `ios || android` vs desktop) — mobile uses 4× smaller windows to stay under iOS NE jetsam limit.

## Testing

```bash
go test ./wire/...    # ~190 tests (unit + integration + rotation + CA)
```
