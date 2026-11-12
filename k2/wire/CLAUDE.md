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

- Implementers: `Dialer` = QUICClient, TCPWSClient, TransportManager, SwappableDialer; `Listener` = QUICServer, TCPWSServer; `StatsProvider` and `EchoProber` (`echo.go`, `RunEchoProbe`) = QUICClient, TCPWSClient. Optional interfaces upper layers may assert on are registered in `k2/CLAUDE.md` § Layer Boundaries.
- `DirectDialer` (`DialContext`, `ListenPacket`) — the shell's bypass-the-tunnel dialer: `daemon.directDialer`, `appext.protectedDialer` (Android), `appext.nePlatform` (Darwin NE), `gateway.gatewayDirectDialer` (SO_MARK). Optional `DialerController.ControlFunc()` supplies the raw-socket control for DNS socket protection — implemented by the same three non-daemon dialers.

## Lock Ordering (wire-internal)

Cross-package chain (`engine.mu → TransportManager.mu → QUICClient.mu / TCPWSClient.mu`) is in `k2/CLAUDE.md`. Inside wire:

```
Serialization locks — never acquired while holding any state lock:
  QUICClient.connectMu → QUICClient.mu
  QUICClient.initUDPMu → QUICClient.mu
  TCPWSClient.initUDPMu → TCPWSClient.mu   (concurrent dials deduped via singleflight.Group — no held mutex)
  smuxUDPMux.writeMu, quicStreamUDPMux.writeMu (standalone)
  udpOverflowStream.initMu → udpOverflowStream.writeMu
  CookieSigner.rotateMu (holds across the rotation's file write) → CookieSigner.mu (state); never under another lock
State locks, standalone (no nesting with any engine/server/transport lock):
  RelayManager.mu   — node pool/ranking/health; client.Do and Transport.CloseIdleConnections run OUTSIDE it
  authCache.mu      — verdict cache; validator I/O (Center call) runs OUTSIDE it, inside a singleflight.Group
  CookieJar.mu      — map ops only; one jar per outbound, shared by every dialer built for it
  authSessions.mu   — connection registry; the sweeper snapshots under it, calls Validator / kill() OUTSIDE
  SlackAlerter.mu   — per-key cooldown map; the webhook POST runs on its own goroutine
  authHealth.mu     — Center-reachability state; SlackAlerter.Fire is called OUTSIDE it (the two never nest)
  ChannelScore.mu   — field-only; read/written by TransportManager under no other lock
  downlinkLossWindow.mu — field-only; QUICClient.WireStats takes it AFTER releasing QUICClient.mu (conn.ConnectionStats() is read outside both)
```

## Config (wire.go)

`Config` parsed from `k2v5://udid:token@host:port?ech=...&pin=...&fp=...`

Fields: UDID, Token, Host, Port, ECH, Fingerprint, Insecure, Pin.

**`Fingerprint` (`fp=`) shapes the TCP-WS leg only.** It picks a uTLS
`ClientHelloID` for `tcpws.go`. The QUIC leg does not read it: it always sends
Chrome-146-over-QUIC — `buildChromeH3Spec()` for the ClientHello and
`chromeH3InitialPacketSpec()` for the Initial *packet* around it
(`quic_fingerprint.go`) — because Chrome's HTTP/3 ClientHello is not the same
shape as its TCP one and there is exactly one QUIC parrot to choose from. Adding
an `fp=` value that only exists on one leg would leave the other leg silently
unshaped — the state this replaced.

Port hopping was removed at the protocol layer: the dial port is whatever the URL
says (443 in production) and never moves. A legacy `hop=` emitted by an
un-upgraded server parses clean and is dropped — never resurrect it as a
dial-port override, a random high UDP port is exactly the anomaly the 443
camouflage exists to avoid.

## Files

- `wire.go` — Dialer/Listener/UDPHandler interfaces, `WireStats`/`StatsProvider`, Config, `ParseURL()`. `WireStats.CC` is the client's **uplink** sender's cumulative `k2cc.CCSummary` (zero, `SenderID=0`, on tcpws / before connect) — diff with `Sub` to read a window; the download direction is only in the server's cc-summary line.
- `auth.go` — `AuthVerdict` (OK 通过 / no-opinion 无意见 / explicit reject 明确拒绝 401-403 / cannot-determine 无法判定 -1 / node-fault 节点自身故障 -2) + `Validator` — the single device-auth abstraction for BOTH transports. **-2 is not -1 and not 401**: it means THIS node cannot authenticate anyone (no `K2_NODE_SECRET`, unresolvable node IPv4). It outranks -1 at the chain tail (permanent, not transient) and surfaces to the client as a **5xx server** code, so one misconfigured node never tells every device that ITS credentials are bad — and the client keeps racing to a healthy node instead of giving up on a 4xx. Auth rides the metadata stream (StreamType 0x10), validated by `handleMetadataStream` in `quic.go`/`tcpws.go`. Validators: `auth_users.go` (`NewUsersFileValidator`, `ChainValidators` — four-state chain semantics: no-opinion is skipped and the chain keeps going, -1 is remembered but the chain keeps going, an explicit 401/402/403 short-circuits immediately; the tail returns -1 if any validator saw "cannot determine" and none explicitly rejected, else fail-closes to 401), `auth_remote.go` (`NewRemoteValidator` → Center `POST /slave/device-check-auth` with `Basic ipv4:NodeSecret`), `auth_cache.go` (`CachingValidator` — caches OK verdicts only, singleflight-deduped concurrent lookups, TTL default 15m). **The old ticket flow (spec §4 `POST /k2v5/auth` + `TicketStore`) was never wired end-to-end and was removed 2026-07 — do not reintroduce it.**
- **QUIC application close codes**: `0x01` unauthorized at admission, `0x03` revoked by revalidation, `0x04` server shutting down (`CloseConnections`, called from `server.Run` on SIGTERM before anything that can block). Clients don't branch on the code today — any close = dead conn, lazy redial on the next stream. `quic-reset.key` in cert_dir = persisted stateless-reset key (`EnsureStatelessResetKey`) so a restarted k2s resets its predecessor's connections.
- `admission.go` — the enforce gate (`SetEnforce`; k2s always sets true — the `K2_ENFORCE_AUTH` knob is gone; permissive survives only for wire tests/k2p). `connAuth` = per-connection verdict; `admissionPolicy.admit` = the decision, shared by both transports. **The gate is not a check you remember to call — it is the only supplier of what egress needs**: `admitTCP` is the sole route from a proxy stream to `AcceptTCP`, `admitUDP` the sole reader of the server's `UDPHandler`, so a new path that skips it has nothing to proxy with. Three ungated egress paths shipped when the gate was a per-dispatch-site check (TCP-WS streams, QUIC datagrams, QUIC overflow); `TestAdmissionGate_EgressSinksAreGated` (AST scan) fails if anyone reaches an egress sink outside the gate, and `enforce_matrix_test.go` holds one behaviour test per client-reachable entry.
- `cookie.go` — node-issued connection cookie (spec §6): payload codec, `CookieSigner` (HMAC-SHA256/128, `<cert_dir>/cookie.key` 0600, 30d auto-rotation, 30min prev-key overlap). Core invariant: cookies are NEVER renewed locally — only a Center cold auth mints one.
- `cookie_jar.go` — client-side in-memory jar keyed by cert-pin fingerprint (not host:port); one jar per outbound, shared by all RaceTransport candidates so a race never triggers three cold auths. Nil-receiver-safe.
- `metadata_auth.go` — `resolveMetadataAuth`: the single server-side cookie-vs-cold-auth decision consumed verbatim by both transports. Cookie hit bypasses the validator chain entirely; udid comes from the server verdict, never the client frame.
- `session.go` — live-connection registry + 60s revalidation sweeper (spec §6.5). Four dispositions dispatched by an explicit switch on `AuthVerdict.Code`: SvcExp expiry (kill, zero Center), stale-endorsement revalidate, explicit reject (kill), undecidable (keep + one-window advance, 6h grace cap). `!OK` alone is never a kill. Each session carries its own `revalJitter` (0-2min, subtracted from the window so it can only fire early) to de-phase reconnect cohorts — build sessions ONLY via `newAuthSession`, pinned by `TestAuthSession_ConstructedOnlyViaConstructor`. Jittering the sweeper's ticker instead would NOT work: the trigger is per-session `authAt`, so a cohort moves together no matter when the pass runs.
- `alert.go` — optional Slack alerting for node-level auth faults (spec §6.4's required alert). `NewSlackAlerter` returns nil when `K2_SLACK_WEBHOOK_URL` is unset and every method is nil-safe, so an unconfigured node pays nothing. `WithAuthHealthAlerts` decorates the **remote** validator (never the cached chain — cache hits never asked Center) and only observes, never alters a verdict. Pages on sustained `auth-center-unreachable` (5min) and immediately on `auth-node-misconfigured`; 30min per-key cooldown; recovery uses its own key so it is never swallowed by the outage cooldown. The node posts to Slack **directly** — the one path in question is the path to Center. One node alerting alone is the page-worthy case; fleet-wide is just a Center outage.
- `auth_info.go` — `AuthInfo` (UDID+Token), `EnsureAuth()` (load or generate credentials)
- `metadata_client.go` — single `sendMetadata` orchestration (`metadataTransport` interface + QUIC/smux adapters), `ErrMetadataRejected` sentinel (rejection fails `connect()`, engine classifies structurally). **`ErrMetadataTimeout`** (PVE S15, Center hanging): the response read hit `metaReadTimeout` (**8s** — must outlive k2s's 5s `remoteAuthClient` timeout, or the client stops listening just before the node's code-less reject) after the request went out → `connect()` fails with it as a `ProxyLayerError`. Deliberately **not** wrapping `ErrMetadataRejected` (that maps to 401 at initial connect); engine reads it as code-less in the replace loop and 503 in `ClassifyError`; dial evidence = no opinion. Other metadata transport errors stay best-effort
- `ech.go` — ECH key management (generate, save/load, rotation, migration), `BuildClientTLSConfig`, SNI peek, reverse proxy
- `ech_provider.go` — `ECHProvider` interface, `LocalECHProvider` (Cloudflare DNS HTTPS fetch), `ParseECHConfigBytes`, `GenerateECHKeyFromTemplate`, 3-tier fallback
- `ca.go` — `BuildDefaultCAPool` (system CA + embedded Kaitu CA - Chinese CA blocklist), `IsBlockedCA`
- `kaitu_ca.pem` — Embedded Kaitu CA cert (EC P-256, disguised as DigiCert Global Root CA)
- `certgen.go` — `GenerateCertPair` (RSA-2048 + EC P-256), `EnsureCertPair`, pin hash generation
- `stream.go` — QUIC varint encoding, `StreamHeader` (type 0x4B32 H3 frame), request/response
- `udp.go` — `UDPFrame` wire format, `SessionAllocator`, `SessionMap`, smux framing
- `quic.go` — QUIC config, `StreamConn` adapter, `QUICClient`/`QUICServer`, relay UDP health tracking (`quicUDPMux`: consecutive timeouts → `ErrDatagramRelayUnhealthy` for TCP-WS fallback, any recv clears the streak, one trial session per 30 s while unhealthy). Per-connection CC observability: `acceptLoop` hands the download-direction k2cc sender to `handleConn(conn, cc)` → `connContext.cc` → `ccSummaryLoop` emits INFO `DIAG: cc-summary` every 60 s (`ccSummaryInterval`, test-shortened) and a `final=true` line on connection end; a periodic line is skipped when the window completed 0 MIs, the final line when the connection never completed one (rejected / scanner / auth-only / sub-200 ms connections leave no line — see `k2cc/CLAUDE.md` Observability). The loop owns prev/seq and exits on `state.done` or `conn.Context().Done()`. Key set is the Phase 2/3 contract — append only (`TestCCSummaryAttrs_FieldSetAndFormatting` pins it). P2P server conns (`NewQUICServerFromConn`) pass `cc=nil` → no line.
- `cc_summary.go` — `ccSummaryLoop` + `ccSummaryAttrs` (pure formatter); `cc_summary_test.go` has the real client/server positive control (paced 4 MiB download → periodic + final lines, seq/udid/mode-sum invariants) and negative control (5-byte echo → 0 lines) plus the goroutine-safe `syncRecordCollector`.
- `quic_fingerprint.go` — the Chrome-146-over-QUIC fingerprint handed to kuic on every dial: `buildChromeH3Spec()` (the `ClientHelloSpec`, via `Config.NewClientHelloSpec`) and `chromeH3InitialPacketSpec()` (the Initial *packet* shape, via `Config.InitialPacketSpec`). Ported from refraction-networking/uquic (BSD-3). **`buildChromeH3Spec` is a factory, never a cached value**: kuic writes this connection's transport parameters — including `initial_source_connection_id` — into the returned spec, so a shared one has concurrent dials overwrite each other. Two spec-authoring requirements are load-bearing and fail loudly if dropped: a `QUICTransportParametersExtension` (else `TRANSPORT_PARAMETER_ERROR`) and a GREASE-ECH placeholder (else the server answers `missing_extension` and ECH is silently unused). **Scope**: the packet shape now covers CID lengths, first PN and its encoding lengths `{1,2}`, and CRYPTO fragmentation with PING/PADDING across the two Initial datagrams the ClientHello spans; what remains quic-go's is Initial CRYPTO *retransmissions*/PTO probes, visible only under packet loss. The `quic_fingerprint.go` SCOPE table and `kuic/MAINTAINING.md` carry the details.
- `quic_limits_desktop.go` — Desktop/server QUIC flow control windows (8 MB stream / 20 MB conn / 256 streams)
- `quic_limits_mobile.go` — Mobile QUIC flow control windows (2 MB stream / 8 MB conn / 64 streams) — iOS NE ~50 MB jetsam limit
- `downlink_loss.go` — `downlinkLossWindow`: turns kuic's cumulative receive-side 1-RTT counters (`ConnectionStats.PacketsReceived1RTT` / `PacketNumberSpan1RTT`) into `WireStats.DownlinkLossRate` (window ≥2s and ≥20 server packet numbers, grows up to 10s, else 0 = no evidence; reset on a new connection). `WireStats.PacketLossRate` stays k2cc's **uplink** MI loss. **Observability only** — heartbeat `downLoss`; it is NOT a health or selector input (pinned by `TestSelector_DownlinkLossDoesNotDriveQualityPath` + `TestHealthMonitor_DownlinkLossDoesNotChangeHealth`). Why it exists: a downloading client's own sender completes no k2cc MI under heavy downlink loss, so uplink loss read 0 while 60% of the server's packets were dropped (PVE netem 2026-11-05). Why it does not decide: production cc-summary the same day showed users at 27-37% sustained downlink loss with only good ratings — k2cc serves that loss fine, and grading it critical would force reconnects + a TCP-WS switch on a working session. A future switch trigger should key on outcomes (unanswered requests, echo failures), judged against production cc-summary × ratings, not netem.
- `echo.go` — `EchoProbe` (client) + `HandleEchoStream` (server), `EchoProber` interface, `StreamTypeEcho=0x04`
- `race.go` — `RaceTransport`: two-way Happy Eyeballs (QUIC-443 → TCP-WS at +800ms), staggered start, echo verification. **`raceTimeout` (5s) bounds each candidate's transport step only** (`candDeadline`): once `connect()` reports the handshake done (`transportUp` ctx hook, called just before `sendMetadata`) the candidate gets `metaPhaseBudget` (metaReadTimeout + race echo + 0.5s) and the collector waits for it (+`raceCollectorSlack` 100ms) — worst case exported as `RaceWorstCase()` = 14.6s; the engine derives every race ctx from it (`raceCtxTimeout` = worst + 1s, engine `TestRaceCtxOutlivesWireWorstCase`; wire side `TestMetaReadTimeoutOutlivesNodeAuthTimeout`). A node that never completes the handshake still fails at 5s
- `tcpws.go` — smux config, `TCPWSClient`/`TCPWSServer`
- `transport.go` — `TransportManager`: QUIC is the strategic home, TCP-WS the fallback channel. Channel choice is a pure hysteresis FSM (`selectorStep`, driven by `evaluateSelector` after every dial/handshake and once per `engine/health.go` echo-probe tick). **`selState.active` is the only source of truth for the serving channel** — `ActiveChannel()`/`FallbackActive()` derive from it. Full rationale, incident history and pinning tests: **`docs/selector-notes.md`** (read before touching `transport.go`). Invariants:
  - **Switch away (QUIC→TCP-WS)** has three independent triggers: `consecConnFails >= 3`; the quality path (`scoreTCPWS > scoreQUIC × 1.5` held 10s, with ≥2 fresh backup samples and `scoreTCPWS ≥ 0.3` — the floor is the positive-evidence rule); the data-path fast path (≥3 unanswered primary-quic `DialTCP`s in 60s AND ≥ a third of that window's quic requests — a share, not a streak). A carrier that QoS-drops bulk UDP passes handshake and echo yet delivers nothing: "QUIC beats TCP-WS at any loss" is false, the scoreboard measures.
  - **Topology gate**: `selectorStep` stays outright when `primaryChannel == "tcpws"` — otherwise it fabricates a switch away from a QUIC channel that does not exist.
  - **Switch back is echo-gated ONLY, deliberately no score comparison** (QUIC carries no traffic while TCP-WS serves, so its score is unmeasurable). Gate = `quicEchoRecovered`: a fresh QUIC handshake AND 3 consecutive echo rounds in `runProbeOnce`; one-shot, consumed on use. `runProbeOnce` runs only when the primary IS the quic channel and re-checks `FallbackActive()` before storing its result. Anti-thrash = that proof + `nextReviewAt` flap window (30s doubling to 600s; a failed rollback trial escalates to 600s, then 1h; trials are graded by answered traffic, not wall clock).
  - **Per-request fallback**: `DialTCP/DialUDP/DialStreamUDP` try the primary first and fall back to TCP-WS on one failed *transport-layer* dial, independent of the FSM. Both `ChannelScore`s are fed by `RecordQUICStats` (sampled 1Hz for **every** outbound, else a non-primary score goes stale and false-trips the quality path) and `RecordEchoResult`/`RecordAvailability`.
  - **`Reset` vs `ResetConnections` vs `ReplacePrimary`** — pick by event: `Reset` (explicit platform network change) wipes selector state, both scores and the min-RTT ratchet (the ratchet never decays on its own — a stale LAN baseline silently disables the quality path); `ResetConnections` (same path, new connection: health-critical reconnect, Pause/Wake) keeps scores and selector history and clears only `quicEchoRecovered`; `ReplacePrimary` (re-race) restarts connection-scoped state but keeps path-scoped history (`flapCount`, `failedTrials`, `rollbackAt`).
  - **Path memory** (`path_verdict.go`, ticket #4055): `selState.pathEvidence` marks a quality/data-path switch-away (never a handshake streak or lost race). `PathVerdict` exports it at engine Stop — an unproven rollback trial at Stop is graded failed — and `ApplyPathVerdict` seeds the next session's manager to start on tcpws with the review window and `failedTrials` carried. The store, its 10 min TTL and the clear-on-network-change live in `engine/path_memory.go`. Path-scoped: survives `ReplacePrimary`, cleared by `Reset`.
  - **Rollback valve**: `K2_CHANNEL_SELECTOR_LEGACY=1` reverts to pre-scoreboard semantics (no quality path, bare-handshake switch back). Production escape hatch; remove once the scoreboard is proven stable.
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

- **connectMu hard timeout**: `QUICClient.connect()` runs `transport.Dial` in a goroutine bounded by `quicDialHardTimeout` (15s). If quic-go's `Conn.destroy()` blocks (waiting for `conn.run()` to exit on context cancel), the goroutine is orphaned (`DIAG: quic-dial-orphan-cleanup` when it finally resolves) and connectMu is released. Without it, connectMu starvation blocked all DialTCP/DialUDP for 30s+ (v0.4.1, 4/22 devices). `TCPWSClient` relies on `net.Dialer{Timeout}`; `daemon.directDialer` derives Timeout from the context deadline.
- QUIC datagrams may be unsupported by the path/peer — `DialUDP` falls back to stream-based UDP.
- ECH requires X25519 keys (not P-256) — Go's crypto/tls only supports DHKEM(X25519). TLS wire formats are big-endian — always `binary.BigEndian` for ECH/TLS parsing. DNS queries for ECH config need a trailing dot (FQDN: `crypto.cloudflare.com.`).
- Server certs: dual RSA+EC pair, both padded to match the camouflage target's cert length; the EC CA (`kaitu_ca.pem`, disguised as DigiCert) signs both.
- `EchoProbe` verifies Short Header data path — QUIC handshake success (Long Header) does NOT prove data can flow. Middleboxes can allow Long Header but drop Short Header packets. Always echo-verify QUIC before declaring it the winner.
- **Echo classification is one shared predicate, and it fails open.** `IsEchoUnsupported(err)` is true for all non-timeout errors (old server: stream reset/EOF) and is consumed by `race.go`, `probe.go` and `engine/health.go` `echoStep` — a misclassification blinds transport selection, node scoring and re-race at once, so **over-match timeouts**. A quic-go stream-deadline expiry is NOT `context.DeadlineExceeded` (it unwraps to `os.ErrDeadlineExceeded` / `net.Error.Timeout()`); testing only the former read a black-holed data path as "old server" (ticket #3463). A probe that never reached a live connection is a third class: `ErrEchoNoConnection` (`IsEchoNotProbed`) — not "unsupported", same trust/strike arithmetic, `runProbeOnce` treats it as not OK. "Conn already closed" is judged by quic error type (`isQUICConnClosedErr`), not only `conn.Context()`. A dead node with no cached connection is closed by user-dial evidence (`dial_failure.go`, `ConnectFailures()`; user dials only, never probes; the code-less metadata reject is NO opinion — it would turn a Center outage into fleet-wide disconnects). Full rationale and pinning tests: **`docs/echo-probe-notes.md`**.
- **A duration from two `time.Now()` calls is routinely 0 on Windows** (monotonic clock ticks at 0.5–15.6 ms). A test asserting on a measured duration must pass with a zero reading or it only fails on `test-windows`; `RecordEchoResult` treats a successful tcpws echo with `rtt <= 0` as full quality. Detail: `docs/echo-probe-notes.md`.
- `RaceTransport` creates independent QUICClient instances per candidate. Loser cleanup is async via WaitGroup + safego.Go. Winner is returned immediately.
- Metadata response is `[0x00][MetadataFrame kv]` — `0x02` is taken by coded errors, success-with-payload rides behind `0x00`. Old clients read one status byte and `Close()`; leftover kv bytes are discarded safely on both transports (`TestMetadataResponse_OldClientSafe_*`). Server metadata streams must `CancelRead` on QUIC or the stream slot leaks.
- Device attribution (`ctx.dev.set` in QUIC's `handleMetadataStream`, bare `dev.set` in TCP-WS's) is gated on `verdict.OK`, independent of the enforce/permissive admission gate — a self-reported udid without a passing verdict is never trusted for accounting and lands in the empty-udid bucket even in permissive mode. Positive-attribution coverage: `TestQUICMetadata_AttributionSetOnOK` / `TestTCPWSMetadata_AttributionSetOnOK`; negative: `Test{QUIC,TCPWS}Metadata_AttributionRequiresVerdict` (`metadata_server_test.go`).
- `quicStreamConn.Close()` must call both `CancelRead(0)` and `Stream.Close()`. quic-go's `Stream.Close()` only closes the write direction (FIN). Without `CancelRead`, the receive half never completes and the stream slot leaks — causing `OpenStreamSync` to block at `MAX_STREAMS`. Fixed in the custom `Close()` method.
- `ReadStreamHeader`/`ReadStreamResponse` need `io.ByteReader` — wrap conn with `bufio.NewReader(conn)`.
- `net.Pipe()` is synchronous — structure goroutines carefully in tests.
- ECH base64 in URLs must use `base64.URLEncoding` (standard `+` corrupted by `url.Query()`).
- **ECH / outer SNI / active-probe surface** (full analysis: **`docs/ech-camouflage-notes.md`**, spec `docs/superpowers/specs/2026-08-10-outer-sni-camouflage-design.md`): outer SNI = the ECH config's `public_name` only because the client emit sets it — not a server-enforced rule; the inner SNI is readable before the server presents its cert (k2 currently reads it late, after `Handshake()`); and routing on ECH *presence* once exposed the self-signed cert to a forged ECH extension — fixed by the plaintext `config_id` gate (`echDisposition` / `ownsECHConfigID` → reverse proxy before any TLS state). Residual: a prober replaying an observed real `config_id` still completes the handshake.
- QUIC connection liveness: quic-go's KeepAlive (10s) + MaxIdleTimeout (30s) detect dead connections at protocol level. `conn.Context().Done()` fires when connection dies. `connect()` checks this before reusing cached conn — no manual `resetConnection()` needed on error paths. macOS sleep/wake is handled by singTunMonitor → `OnNetworkChanged()` → `Reset()` (desktop), or by platform-native callbacks (appext). No application-level wall-clock staleness check needed.
- Stream response timeout ≠ transport failure: A 5s stream response timeout means the server's upstream `net.Dial` to the destination is slow/failing — NOT that QUIC transport is broken (OpenStreamSync succeeded = transport is alive). Do NOT increment `handshakeFailStreak` or trigger TCP-WS fallback on stream timeouts. `handshakeFailStreak` is strictly for QUIC handshake failures (UDP blocked by firewall).
- Proxy-layer errors skip TCP-WS fallback: Dialer marks errors as proxy-layer after the request is sent (stream opened + header written). `ProxyLayerError` wrapper and `StreamError` both implement `IsProxyLayer() bool`. `TransportManager` checks via interface (`proxyLayerErr`), not type enumeration. Covers: `StreamError` (server error code), `ErrStreamRejected` (server rejection), `ReadStreamResponse` timeout (server processing). Only transport-layer errors (handshake failure, stream open failure, write failure) trigger per-request fallback.
- Relay UDP unhealthy → `ErrDatagramRelayUnhealthy`: When relay datagram path has consecutive relay timeouts (threshold: 3), `DialUDP` returns `ErrDatagramRelayUnhealthy` for per-request TCP-WS fallback. A relay timeout is a `quicUDPConn` read deadline on a session that **sent and never received** (`txPackets > 0 && rxPackets == 0`), or a send the datagram budget shed (`maxDatagramInflight`). A session that was answered and then hit its idle deadline is not one: every UDP session ends on that deadline, and whenever the client had the last word (QUIC close, final ACK) the downlink deadline fires first — counting those marked a healthy path unhealthy after three quiet sessions. Loss rate tracking was removed (DNS racing inflated false positives). Any successful recv clears the timeout streak, and nothing else does — so an unhealthy relay keeps one way open for a datagram to arrive: `admitSession` lets **one new session per `relayTrialInterval` (30 s)** use the datagram path anyway (INFO `datagram relay unhealthy, trial session`). Its reply heals the relay (`datagram relay path healthy again`); if it goes unanswered, that one flow is black-holed until its idle deadline (up to 60 s, so two unanswered trials can be open at once) and the next trial is an interval later — healing therefore waits for a trial that lands on a destination which answers. DNS is never the trial (it uses `DialStreamUDP`), and no trial happens while the active channel is TCP-WS (`TransportManager` does not ask QUIC then). Without the trial, once the last datagram session had ended nothing could arrive any more and the relay stayed unhealthy until the QUIC connection was replaced — on a router, as long as it ran (three LAN broadcasts to nobody were enough to get there). The schedule is set when the relay is marked unhealthy and is not pushed out by later timeouts (shed bursts, the trial's own). The clock is monotonic (`relayEpoch`). Relay-layer metrics never trigger wire-layer actions (reconnect, transport switch). See `VOCABULARY.md`.
- QUIC flow control is platform-specific (build tags `ios || android` vs desktop) — mobile uses smaller windows (2 MB stream / 8 MB conn vs 8 MB / 20 MB desktop) to stay under iOS NE jetsam limit.
- QUIC stream responses have 5s timeout to prevent indefinite blocking on slow/dead servers.
- `DialTCP(ctx, addr, earlyData)` sends earlyData after stream header, before reading server response. Server's `bufio.Reader` naturally buffers it during header parsing; `pipe()` consumes it after upstream dial. Saves 1 RTT on high-latency links. Backward compatible — old servers handle it without changes. Pass `nil` for no early data.
- `SendDatagram` uses atomic inflight counter (`maxDatagramInflight=8` in `quic.go`, `ErrDatagramSendTimeout`) — quic-go's own datagram queue holds up to 32 frames and blocks indefinitely (`datagramQueue.Add()`) once full under congestion. Our cap must stay well below that 32: with N sessions sharing one queue, only `maxDatagramInflight` goroutines are allowed into the blocking `Add()` path at once, so a value close to 32 would let most `HandleUDP` goroutines pile up there together instead of continuing to read from their targets. When concurrent SendDatagram calls exceed the limit, excess packets are dropped immediately (protocol-correct for unreliable DATAGRAM). No goroutine spawned — avoids leak when SendDatagram blocks. Applies to both client `quicUDPConn.WriteTo` (via `quicUDPMux.sendInflight`) and server `serverUDPConn.WriteTo` (via `connUDPState.sendInflight`).
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
