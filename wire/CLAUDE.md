# Wire Layer (wire/ — k2v5 composite protocol)

~330 test functions. QUIC/H3 primary, TCP-WS/smux fallback.

## Interfaces (wire.go)

```go
// Client transport (L4 proxy model)
type Dialer interface {
    DialTCP(ctx context.Context, addr string, earlyData []byte) (net.Conn, error)
    DialUDP(ctx context.Context, addr string) (net.PacketConn, error)
    // DialStreamUDP opens a stream-backed UDP session (reliable, congestion-controlled).
    DialStreamUDP(ctx context.Context, addr string) (net.PacketConn, error)
    // Reset clears cached connection state without closing the Dialer.
    Reset()
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
- `auth.go` — `JWTValidator` type: the single device-auth abstraction for BOTH transports. Auth rides the metadata stream (StreamType 0x10), validated by `handleMetadataStream` in `quic.go`/`tcpws.go`; the enforce gate (`SetEnforce`, env `K2_ENFORCE_AUTH`, default permissive) rejects unauthenticated proxy streams on both. Validators: `auth_users.go` (`NewUsersFileValidator`, `ChainValidators`), `auth_remote.go` (`NewRemoteValidator` → Center `POST /slave/device-check-auth` with `Basic ipv4:NodeSecret`), `auth_cache.go` (`CachingValidator`, TTL, success-only). **The old ticket flow (spec §4 `POST /k2v5/auth` + `TicketStore`) was never wired end-to-end and was removed 2026-07 — do not reintroduce it.**
- `auth_info.go` — `AuthInfo` (UDID+Token), `EnsureAuth()` (load or generate credentials)
- `ech.go` — ECH key management (generate, save/load, rotation, migration), `BuildClientTLSConfig`, SNI peek, reverse proxy
- `ech_provider.go` — `ECHProvider` interface, `LocalECHProvider` (Cloudflare DNS HTTPS fetch), `ParseECHConfigBytes`, `GenerateECHKeyFromTemplate`, 3-tier fallback
- `ca.go` — `BuildDefaultCAPool` (system CA + embedded Kaitu CA - Chinese CA blocklist), `IsBlockedCA`
- `kaitu_ca.pem` — Embedded Kaitu CA cert (EC P-256, disguised as DigiCert Global Root CA)
- `certgen.go` — `GenerateCertPair` (RSA-2048 + EC P-256), `EnsureCertPair`, pin hash generation
- `stream.go` — QUIC varint encoding, `StreamHeader` (type 0x4B32 H3 frame), request/response
- `udp.go` — `UDPFrame` wire format, `SessionAllocator`, `SessionMap`, smux framing
- `quic.go` — QUIC config, `StreamConn` adapter, `QUICClient`/`QUICServer`, relay UDP health tracking (`quicUDPMux`: consecutive timeouts → `ErrDatagramRelayUnhealthy` for TCP-WS fallback, consecutive-recv counter for hop recovery)
- `quic_limits_desktop.go` — Desktop/server QUIC flow control windows (8 MB stream / 20 MB conn / 256 streams)
- `quic_limits_mobile.go` — Mobile QUIC flow control windows (2 MB stream / 8 MB conn / 64 streams) — iOS NE ~50 MB jetsam limit
- `echo.go` — `EchoProbe` (client) + `HandleEchoStream` (server), `EchoProber` interface, `StreamTypeEcho=0x04`
- `race.go` — `RaceTransport`: three-way Happy Eyeballs (QUIC-443 → QUIC-hop → TCP-WS), staggered start, echo verification
- `tcpws.go` — smux config, `TCPWSClient`/`TCPWSServer`
- `transport.go` — `TransportManager`: QUIC strongly preferred, TCP-WS last-resort fallback. Consecutive handshake failure threshold (3, `handshakeFailStreak`) activates fallback — no exponential backoff. Background QUIC probe loop (30s interval) actively tests recovery during fallback. `DialTCP` tries QUIC first (with context budget 3-5s), falls back to TCP-WS on failure. `FallbackActive` tracks QUIC→TCP-WS state. `Reset` uses the `wire.Dialer` interface contract (no concrete type assertions).
- `ip.go` — `ProbePublicIP`, `FormatSslipHost`, `HasIPv6` (server auto-provisioning helpers)
- `tlspad/` — TLS record padding (probe target cert length, pad server cert to match)
- `k2cc/` — K2CC (PCC Vivace) congestion control for QUIC
- `VOCABULARY.md` — Three-layer vocabulary convention (Wire/Relay/Tunnel)
- `relay.go` — `RelayFetch` antiblock control-plane relay: TCP→uTLS(ECH+enforced pin)→one HTTP/1.1 round-trip to Center through a node. `RelayFetchJSON` = gomobile/daemon string boundary. Enforces pin via `VerifyPeerCertificate` (unlike the tcpws dial template).
- `contract_test.go` — 11 transport behavior contract tests (startup, fallback, recovery, datagram degradation, network switch, slow upstream, proxy error, stream rejection, read-response timeout)
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

- `EchoProbe` verifies Short Header data path — QUIC handshake success (Long Header) does NOT prove data can flow. Middleboxes can allow Long Header but drop Short Header packets. Always echo-verify QUIC before declaring it the winner.
- `echo-probe-unsupported` backward compat: `isEchoUnsupported(err)` returns true for all non-timeout errors (stream reset/EOF = old server). `buildAndProbeQUIC` treats unsupported as success (returns dialer). But `echoLoop` in engine/health.go does NOT check `isEchoUnsupported` — it counts all errors as failures → 3 fails → rerace every 90s against old servers. Server-side fix: deploy k2v5 with `HandleEchoStream` (added in `StreamTypeEcho=0x04` handler). Client-side fix pending: echoLoop should skip unsupported.
- `RaceTransport` creates independent QUICClient instances per candidate. Loser cleanup is async via WaitGroup + safego.Go. Winner is returned immediately.
- `quicStreamConn.Close()` must call both `CancelRead(0)` and `Stream.Close()`. quic-go's `Stream.Close()` only closes the write direction (FIN). Without `CancelRead`, the receive half never completes and the stream slot leaks — causing `OpenStreamSync` to block at `MAX_STREAMS`. Fixed in the custom `Close()` method.
- `ReadStreamHeader`/`ReadStreamResponse` need `io.ByteReader` — wrap conn with `bufio.NewReader(conn)`.
- `net.Pipe()` is synchronous — structure goroutines carefully in tests.
- ECH base64 in URLs must use `base64.URLEncoding` (standard `+` corrupted by `url.Query()`).
- uTLS forces outer SNI = ECH config's `public_name` — cannot be set independently.
- QUIC connection liveness: quic-go's KeepAlive (10s) + MaxIdleTimeout (30s) detect dead connections at protocol level. `conn.Context().Done()` fires when connection dies. `connect()` checks this before reusing cached conn — no manual `resetConnection()` needed on error paths. macOS sleep/wake is handled by singTunMonitor → `OnNetworkChanged()` → `Reset()` (desktop), or by platform-native callbacks (appext). No application-level wall-clock staleness check needed.
- Stream response timeout ≠ transport failure: A 5s stream response timeout means the server's upstream `net.Dial` to the destination is slow/failing — NOT that QUIC transport is broken (OpenStreamSync succeeded = transport is alive). Do NOT increment `handshakeFailStreak` or trigger TCP-WS fallback on stream timeouts. `handshakeFailStreak` is strictly for QUIC handshake failures (UDP blocked by firewall).
- Proxy-layer errors skip TCP-WS fallback: Dialer marks errors as proxy-layer after the request is sent (stream opened + header written). `ProxyLayerError` wrapper and `StreamError` both implement `IsProxyLayer() bool`. `TransportManager` checks via interface (`proxyLayerErr`), not type enumeration. Covers: `StreamError` (server error code), `ErrStreamRejected` (server rejection), `ReadStreamResponse` timeout (server processing). Only transport-layer errors (handshake failure, stream open failure, write failure) trigger per-request fallback.
- Relay UDP unhealthy → `ErrDatagramRelayUnhealthy`: When relay datagram path has consecutive ReadFrom timeouts (threshold: 3), `DialUDP` returns `ErrDatagramRelayUnhealthy` for per-request TCP-WS fallback. Loss rate tracking was removed (DNS racing inflated false positives). Hop counter resets after 10 consecutive recvs without timeout. Relay-layer metrics never trigger wire-layer actions (port hop, reconnect). See `VOCABULARY.md`.
- QUIC flow control is platform-specific (build tags `ios || android` vs desktop) — mobile uses smaller windows (2 MB stream / 8 MB conn vs 8 MB / 20 MB desktop) to stay under iOS NE jetsam limit.
- QUIC stream responses have 5s timeout to prevent indefinite blocking on slow/dead servers.
- `DialTCP(ctx, addr, earlyData)` sends earlyData after stream header, before reading server response. Server's `bufio.Reader` naturally buffers it during header parsing; `pipe()` consumes it after upstream dial. Saves 1 RTT on high-latency links. Backward compatible — old servers handle it without changes. Pass `nil` for no early data.
- `SendDatagram` uses atomic inflight counter (`maxDatagramInflight=32`, `ErrDatagramSendTimeout`) — quic-go's 32-frame datagram queue blocks indefinitely under congestion. When concurrent SendDatagram calls exceed the limit, excess packets are dropped immediately (protocol-correct for unreliable DATAGRAM). No goroutine spawned — avoids leak when SendDatagram blocks. Applies to both client `quicUDPConn.WriteTo` (via `quicUDPMux.sendInflight`) and server `serverUDPConn.WriteTo` (via `connUDPState.sendInflight`).
- `InitialPacketSize = 1400` (not quic-go default 1280): QUIC DATAGRAM frames must fit in a single packet. Browser QUIC Initial packets (1250 bytes payload) create UDPFrame of 1274 bytes. With default 1280, `currentMTUEstimate = 1243 < 1274` → `DatagramTooLargeError` → silent overflow stream fallback, defeating datagram relay's QUIC-over-QUIC design. 1400 gives estimate 1363, safe for all standard Ethernet paths (IPv4 packet = 1428 < 1500). PMTU discovery handles rare sub-1400 paths.

## Testing

```bash
go test ./wire/...                          # ~330 tests (unit + integration + contract + rotation + CA)
go test -run TestContract -v ./wire/...     # contract tests only
```
