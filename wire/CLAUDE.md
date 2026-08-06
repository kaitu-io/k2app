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
- `auth.go` — `AuthVerdict` (OK 通过 / no-opinion 无意见 / explicit reject 明确拒绝 401-403 / cannot-determine 无法判定 -1 / node-fault 节点自身故障 -2) + `Validator` — the single device-auth abstraction for BOTH transports. **-2 is not -1 and not 401**: it means THIS node cannot authenticate anyone (no `K2_NODE_SECRET`, unresolvable node IPv4). It outranks -1 at the chain tail (permanent, not transient) and surfaces to the client as a **5xx server** code, so one misconfigured node never tells every device that ITS credentials are bad — and the client keeps racing to a healthy node instead of giving up on a 4xx. Auth rides the metadata stream (StreamType 0x10), validated by `handleMetadataStream` in `quic.go`/`tcpws.go`. Validators: `auth_users.go` (`NewUsersFileValidator`, `ChainValidators` — four-state chain semantics: no-opinion is skipped and the chain keeps going, -1 is remembered but the chain keeps going, an explicit 401/402/403 short-circuits immediately; the tail returns -1 if any validator saw "cannot determine" and none explicitly rejected, else fail-closes to 401), `auth_remote.go` (`NewRemoteValidator` → Center `POST /slave/device-check-auth` with `Basic ipv4:NodeSecret`), `auth_cache.go` (`CachingValidator` — caches OK verdicts only, singleflight-deduped concurrent lookups, TTL default 15m). **The old ticket flow (spec §4 `POST /k2v5/auth` + `TicketStore`) was never wired end-to-end and was removed 2026-07 — do not reintroduce it.**
- `admission.go` — the enforce gate (`SetEnforce`, env `K2_ENFORCE_AUTH`, default permissive). `connAuth` = per-connection verdict; `admissionPolicy.admit` = the decision, shared by both transports. **The gate is not a check you remember to call — it is the only supplier of what egress needs**: `admitTCP` is the sole route from a proxy stream to `AcceptTCP`, `admitUDP` the sole reader of the server's `UDPHandler`, so a new path that skips it has nothing to proxy with. Three ungated egress paths shipped when the gate was a per-dispatch-site check (TCP-WS streams, QUIC datagrams, QUIC overflow); `TestAdmissionGate_EgressSinksAreGated` (AST scan) fails if anyone reaches an egress sink outside the gate, and `enforce_matrix_test.go` holds one behaviour test per client-reachable entry.
- `cookie.go` — node-issued connection cookie (spec §6): payload codec, `CookieSigner` (HMAC-SHA256/128, `<cert_dir>/cookie.key` 0600, 30d auto-rotation, 30min prev-key overlap). Core invariant: cookies are NEVER renewed locally — only a Center cold auth mints one.
- `cookie_jar.go` — client-side in-memory jar keyed by cert-pin fingerprint (not host:port); one jar per outbound, shared by all RaceTransport candidates so a race never triggers three cold auths. Nil-receiver-safe.
- `metadata_auth.go` — `resolveMetadataAuth`: the single server-side cookie-vs-cold-auth decision consumed verbatim by both transports. Cookie hit bypasses the validator chain entirely; udid comes from the server verdict, never the client frame.
- `session.go` — live-connection registry + 60s revalidation sweeper (spec §6.5). Four dispositions dispatched by an explicit switch on `AuthVerdict.Code`: SvcExp expiry (kill, zero Center), stale-endorsement revalidate, explicit reject (kill), undecidable (keep + one-window advance, 6h grace cap). `!OK` alone is never a kill. Each session carries its own `revalJitter` (0-2min, subtracted from the window so it can only fire early) to de-phase reconnect cohorts — build sessions ONLY via `newAuthSession`, pinned by `TestAuthSession_ConstructedOnlyViaConstructor`. Jittering the sweeper's ticker instead would NOT work: the trigger is per-session `authAt`, so a cohort moves together no matter when the pass runs.
- `alert.go` — optional Slack alerting for node-level auth faults (spec §6.4's required alert). `NewSlackAlerter` returns nil when `K2_SLACK_WEBHOOK_URL` is unset and every method is nil-safe, so an unconfigured node pays nothing. `WithAuthHealthAlerts` decorates the **remote** validator (never the cached chain — cache hits never asked Center) and only observes, never alters a verdict. Pages on sustained `auth-center-unreachable` (5min) and immediately on `auth-node-misconfigured`; 30min per-key cooldown; recovery uses its own key so it is never swallowed by the outage cooldown. The node posts to Slack **directly** — the one path in question is the path to Center. One node alerting alone is the page-worthy case; fleet-wide is just a Center outage.
- `auth_info.go` — `AuthInfo` (UDID+Token), `EnsureAuth()` (load or generate credentials)
- `metadata_client.go` — single `sendMetadata` orchestration (`metadataTransport` interface + QUIC/smux adapters), `ErrMetadataRejected` sentinel (rejection fails `connect()`, engine classifies structurally)
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
- Metadata response is `[0x00][MetadataFrame kv]` — `0x02` is taken by coded errors, success-with-payload rides behind `0x00`. Old clients read one status byte and `Close()`; leftover kv bytes are discarded safely on both transports (`TestMetadataResponse_OldClientSafe_*`). Server metadata streams must `CancelRead` on QUIC or the stream slot leaks.
- Device attribution (`ctx.dev.set` in QUIC's `handleMetadataStream`, bare `dev.set` in TCP-WS's) is gated on `verdict.OK`, independent of the enforce/permissive admission gate — a self-reported udid without a passing verdict is never trusted for accounting and lands in the empty-udid bucket even in permissive mode. Positive-attribution coverage: `TestQUICMetadata_AttributionSetOnOK` / `TestTCPWSMetadata_AttributionSetOnOK`; negative: `Test{QUIC,TCPWS}Metadata_AttributionRequiresVerdict` (`metadata_server_test.go`).
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
- `SendDatagram` uses atomic inflight counter (`maxDatagramInflight=8`, `wire/quic.go:55`, `ErrDatagramSendTimeout`) — quic-go's own datagram queue holds up to 32 frames and blocks indefinitely (`datagramQueue.Add()`) once full under congestion. Our cap must stay well below that 32: with N sessions sharing one queue, only `maxDatagramInflight` goroutines are allowed into the blocking `Add()` path at once, so a value close to 32 would let most `HandleUDP` goroutines pile up there together instead of continuing to read from their targets. When concurrent SendDatagram calls exceed the limit, excess packets are dropped immediately (protocol-correct for unreliable DATAGRAM). No goroutine spawned — avoids leak when SendDatagram blocks. Applies to both client `quicUDPConn.WriteTo` (via `quicUDPMux.sendInflight`) and server `serverUDPConn.WriteTo` (via `connUDPState.sendInflight`).
- UDP sessions are multi-destination: one session (= one client source port) can address many destinations over its lifetime. Uplink `UDPFrame.Addr` is **that packet's** destination; empty means "same as the session's first destination". Downlink `UDPFrame.Addr` is the **actual source** the reply came from; empty means "from the session's first destination" (old server, pre-multi-dest). Both ends fall back to the session's first destination on an empty addr, so the *frame encoding* interoperates in any old/new combination. See `udp.go` (`UDPFrame`) and `server/handler.go` (`HandleUDP`'s per-session destination LRU, `maxUDPDestsPerSession = 64`).
- **The empty-addr fallback returns the session target VERBATIM, which may be a hostname** — `DialUDP(ctx, dest)` stores `dest` unparsed in `targetAddr`, and `core/tunnel.go handleUDP` hands it a *domain* whenever the QUIC SNI upgrade or the IPv6→hostname rewrite fires. So a new client talking to a not-yet-upgraded node gets a hostname `net.Addr` out of every `ReadFrom`. Callers must not assume it is an IP: sing-tun rejects a non-IP write destination with `os.ErrInvalid`, which used to kill the whole session on the first downlink packet. `core.pipePacket` now repairs it (`repairSourceAddr` → the session's primary destination) rather than the wire layer re-resolving per packet. Do not "simplify" that back into a `ResolveUDPAddr` here: it puts DNS on the per-packet hot path and can hand the app an IP it never addressed, which its connected socket then discards. So the honest interop claim is: **wire-level frames interoperate in any combination; the hostname-shaped fallback address is a real hazard for anything that writes it to a socket.**
- **UDP drop instrumentation — what a `0` means, per transport.** Every path that silently discards a UDP packet increments a counter, so `0` never means "nobody counted":

  | path | `UDPTxDrops` | `UDPRxDrops` (live session, channel full) | `UDPRxDropsNoSession` |
  |---|---|---|---|
  | QUIC datagram (`quicUDPMux`) | send budget saturated | ✅ | ✅ |
  | QUIC stream-UDP (`quicStreamUDPMux`) | n/a — always 0 | ✅ | ✅ |
  | TCP-WS smux (`smuxUDPMux`) | n/a — always 0 | ✅ | ✅ |

  The two `n/a` cells are **not gaps**: a stream-backed transport has no lossy send path — `writeFrame` either writes the frame or returns an error to the caller, so a lost packet is always an explicit error, never a silent discard. But note what that error does **not** do: it kills only the *mux* (that one relay stream), never the smux session or QUIC connection carrying it. The session stays up, `DIAG: heartbeat` keeps reporting `healthy`, and only UDP is affected. Until #3420 that was an outright trap — the dead mux stayed cached and every later UDP send failed instantly; now the next dial evicts and rebuilds it, which you see as `DIAG: udp-mux-rebuild`. So a UDP-only outage with a healthy heartbeat is a real shape: read `udp-mux-rebuild`, not the drop counters. Only the datagram path can accept a packet from the app and then silently discard it. `WireStats.Transport` + `WireFallback` tell you which row you are reading. `UDPRxDropsNoSession` is deliberately **not** folded into `UDPRxDrops`: it is dominated by the benign tail of packets arriving just after a session closed or was idle-reaped, so it scales with session churn, not with loss — a rising `udpRxDrops` is a stuttering user, a rising `udpRxDropsNoSess` usually is not. All counters are cumulative for the life of the connection; diff two heartbeat samples to read a rate.
- `InitialPacketSize = 1400` (not quic-go default 1280): QUIC DATAGRAM frames must fit in a single packet. Browser QUIC Initial packets (1250 bytes payload) create UDPFrame of 1274 bytes. With default 1280, `currentMTUEstimate = 1243 < 1274` → `DatagramTooLargeError` → silent overflow stream fallback, defeating datagram relay's QUIC-over-QUIC design. 1400 gives estimate 1363, safe for all standard Ethernet paths (IPv4 packet = 1428 < 1500). PMTU discovery handles rare sub-1400 paths.

## Testing

```bash
go test ./wire/...                          # ~330 tests (unit + integration + contract + rotation + CA)
go test -run TestContract -v ./wire/...     # contract tests only
```
