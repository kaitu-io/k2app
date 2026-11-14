# Wire Layer (wire/ — k2v5 composite protocol)

Long-form notes in `docs/` (not auto-loaded; read the one covering the code before changing it): **`auth-notes.md`** (auth, admission, sessions, alerts, metadata), **`datapath-notes.md`** (QUIC fingerprint, cc-summary, race budget, UDP relay/datagram/addressing), **`selector-notes.md`**, **`echo-probe-notes.md`**, **`ech-camouflage-notes.md`**.

QUIC/H3 primary, TCP-WS/smux fallback.

## Interfaces (wire.go)

`Dialer` (`DialTCP(ctx, addr, earlyData)`, `DialUDP`, `DialStreamUDP` — stream-backed reliable UDP, `Reset()` — clear cached connection state without closing, `Close()`), `Listener` (`AcceptTCP`, `ServeUDP(ctx, UDPHandler)`, `Close()`), `UDPHandler` (`HandleUDP`), optional `StatsProvider` (`WireStats()`, type-asserted on a Dialer). Signatures: `wire.go`.

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

- **`Fingerprint` (`fp=`) shapes the TCP-WS leg only** (uTLS `ClientHelloID` in `tcpws.go`); QUIC always sends Chrome-146-over-QUIC (`quic_fingerprint.go`). Never add an `fp=` value that exists on one leg only — the other leg goes silently unshaped.
- **Port hopping is gone**: dial port = the URL's (443 in production). A legacy `hop=` parses clean and is dropped — never resurrect it as a dial-port override (a random high UDP port defeats the 443 camouflage).

## Files

- `wire.go` — Dialer/Listener/UDPHandler interfaces, `WireStats`/`StatsProvider`, Config, `ParseURL()`. `WireStats.CC` is the client's **uplink** sender's cumulative `k2cc.CCSummary` (zero, `SenderID=0`, on tcpws / before connect) — diff with `Sub` to read a window; the download direction is only in the server's cc-summary line.
- `auth.go` — `AuthVerdict` (OK 通过 / no-opinion 无意见 / explicit reject 明确拒绝 401-403 / cannot-determine 无法判定 -1 / node-fault 节点自身故障 -2) + `Validator`, the single device-auth abstraction for BOTH transports; auth rides the metadata stream (StreamType 0x10, `handleMetadataStream`). Validators: `auth_users.go` (`NewUsersFileValidator`, `ChainValidators`), `auth_remote.go` (`NewRemoteValidator` → Center `POST /slave/device-check-auth`), `auth_cache.go` (`CachingValidator`, OK-only cache, TTL default 15m). Invariants:
  - **-2 is not -1 and not 401**: THIS node cannot authenticate anyone (no `K2_NODE_SECRET`, unresolvable node IPv4) → outranks -1 at the chain tail and surfaces as a **5xx server** code, so one misconfigured node never tells devices their credentials are bad.
  - `ChainValidators`: no-opinion skipped, -1 remembered, explicit 401/402/403 short-circuits; tail = -1 if any "cannot determine" and no reject, else fail-closed 401.
  - **The old ticket flow (spec §4 `POST /k2v5/auth` + `TicketStore`) was removed 2026-07 — do not reintroduce it.**
- **QUIC application close codes**: `0x01` unauthorized at admission, `0x03` revoked by revalidation, `0x04` server shutting down (`CloseConnections`, from `server.Run` on SIGTERM before anything that can block). Clients don't branch on the code. `quic-reset.key` in cert_dir = persisted stateless-reset key (`EnsureStatelessResetKey`).
- `admission.go` — enforce gate (`SetEnforce`; k2s always true — `K2_ENFORCE_AUTH` is gone). **The gate is the only supplier of what egress needs**: `admitTCP` is the sole route to `AcceptTCP`, `admitUDP` the sole reader of the server's `UDPHandler` — a new path that skips it has nothing to proxy with. Pinned by `TestAdmissionGate_EgressSinksAreGated` (AST scan) + `enforce_matrix_test.go` (one test per client-reachable entry); three ungated egress paths shipped when it was a per-site check.
- `cookie.go` — node-issued connection cookie (spec §6): payload codec, `CookieSigner` (HMAC-SHA256/128, `<cert_dir>/cookie.key` 0600, 30d auto-rotation, 30min prev-key overlap). Core invariant: cookies are NEVER renewed locally — only a Center cold auth mints one.
- `cookie_jar.go` — client-side in-memory jar keyed by cert-pin fingerprint (not host:port); one jar per outbound, shared by all RaceTransport candidates so a race never triggers three cold auths. Nil-receiver-safe.
- `metadata_auth.go` — `resolveMetadataAuth`: the single server-side cookie-vs-cold-auth decision consumed verbatim by both transports. Cookie hit bypasses the validator chain entirely; udid comes from the server verdict, never the client frame.
- `session.go` — live-connection registry + 60s revalidation sweeper (spec §6.5); four dispositions by explicit switch on `AuthVerdict.Code` (SvcExp kill, stale revalidate, explicit reject kill, undecidable keep with 6h grace cap). **`!OK` alone is never a kill.** Build sessions ONLY via `newAuthSession` (per-session `revalJitter` de-phases reconnect cohorts; jittering the sweeper ticker would not work) — `TestAuthSession_ConstructedOnlyViaConstructor`.
- `alert.go` — optional Slack paging for node auth faults (`NewSlackAlerter` nil when `K2_SLACK_WEBHOOK_URL` unset; nil-safe). `WithAuthHealthAlerts` wraps the **remote** validator only, observe-only; posts to Slack **directly**, not via Center.
- `auth_info.go` — `AuthInfo` (UDID+Token), `EnsureAuth()` (load or generate credentials)
- `metadata_client.go` — single `sendMetadata` orchestration (`metadataTransport` + QUIC/smux adapters); `ErrMetadataRejected` fails `connect()`. **`ErrMetadataTimeout`**: `metaReadTimeout` (**8s**) must outlive k2s's 5s `remoteAuthClient` timeout (`TestMetaReadTimeoutOutlivesNodeAuthTimeout`); it is a `ProxyLayerError` deliberately **not** wrapping `ErrMetadataRejected` (that maps to 401) — engine reads it as code-less / 503, dial evidence = no opinion.
- `ech.go` — ECH key management (generate, save/load, rotation, migration), `BuildClientTLSConfig`, SNI peek, reverse proxy
- `ech_provider.go` — `ECHProvider` interface, `LocalECHProvider` (Cloudflare DNS HTTPS fetch), `ParseECHConfigBytes`, `GenerateECHKeyFromTemplate`, 3-tier fallback
- `ca.go` — `BuildDefaultCAPool` (system CA + embedded Kaitu CA - Chinese CA blocklist), `IsBlockedCA`
- `kaitu_ca.pem` — Embedded Kaitu CA cert (EC P-256, disguised as DigiCert Global Root CA)
- `certgen.go` — `GenerateCertPair` (RSA-2048 + EC P-256), `EnsureCertPair`, pin hash generation
- `stream.go` — QUIC varint encoding, `Write/ReadStreamHeader` (type 0x4B32 H3 frame), `Write/ReadStreamResponse`
- `udp.go` — `UDPFrame` wire format, `SessionIDAllocator`, smux UDP framing (the server-side session map lives in `tcpws.go`)
- `quic.go` — QUIC config, `StreamConn`, `QUICClient`/`QUICServer`, relay UDP health (`quicUDPMux`). Server emits INFO `DIAG: cc-summary` per connection (`ccSummaryLoop` in `cc_summary.go`, every 60 s + `final=true`; nothing for 0 completed MIs — `k2cc/CLAUDE.md` Observability; `NewQUICServerFromConn` passes `cc=nil`). **Key set is append-only** (Phase 2/3 contract, `TestCCSummaryAttrs_FieldSetAndFormatting`).
- `cc_summary.go` — `ccSummaryAttrs` pure formatter; `cc_summary_test.go` = real client/server positive + negative controls (`syncRecordCollector`).
- `quic_fingerprint.go` — Chrome-146-over-QUIC for kuic: `buildChromeH3Spec()` + `chromeH3InitialPacketSpec()`. **`buildChromeH3Spec` is a factory, never a cached value** (kuic writes per-connection `initial_source_connection_id` into it). Load-bearing spec parts: `QUICTransportParametersExtension` (else `TRANSPORT_PARAMETER_ERROR`) and a GREASE-ECH placeholder (else `missing_extension`, ECH silently unused). Scope table in the file + `kuic/MAINTAINING.md`.
- `quic_limits_desktop.go` / `quic_limits_mobile.go` (build tags `ios || android`) — flow-control windows 8 MB stream / 20 MB conn / 256 streams vs 2 MB / 8 MB / 64 (iOS NE ~50 MB jetsam limit).
- `downlink_loss.go` — `downlinkLossWindow` → `WireStats.DownlinkLossRate` (`WireStats.PacketLossRate` = k2cc **uplink**). **Observability only** (heartbeat `downLoss`), never a health/selector input: `TestSelector_DownlinkLossDoesNotDriveQualityPath`, `TestHealthMonitor_DownlinkLossDoesNotChangeHealth`.
- `echo.go` — `EchoProbe` (client) + `HandleEchoStream` (server), `EchoProber` interface, `StreamTypeEcho=0x04`
- `race.go` — `RaceTransport`: QUIC-443 → TCP-WS at +800ms, echo-verified. **`raceTimeout` (5s) bounds the transport step only**; after `transportUp` the candidate gets `metaPhaseBudget`. Worst case `RaceWorstCase()` = 14.6s; the engine derives every race ctx from it (engine `TestRaceCtxOutlivesWireWorstCase`) — change one timeout and both sides must still hold.
- `tcpws.go` — smux config, `TCPWSClient`/`TCPWSServer`
- `transport.go` — `TransportManager`: QUIC is the strategic home, TCP-WS the fallback channel; channel choice is a pure hysteresis FSM (`selectorStep`, driven by `evaluateSelector`). **`selState.active` is the only source of truth for the serving channel** (`ActiveChannel()`/`FallbackActive()` derive from it). Read **`docs/selector-notes.md`** before touching `transport.go`. Invariants:
  - **Switch away (QUIC→TCP-WS)**, three independent triggers: `consecConnFails >= 3`; quality path (`scoreTCPWS > scoreQUIC × 1.5` held 10s, ≥2 fresh backup samples, `scoreTCPWS ≥ 0.3` positive-evidence floor); data-path fast path (≥3 unanswered primary-quic `DialTCP`s in 60s AND ≥ a third of that window's quic requests). "QUIC beats TCP-WS at any loss" is false — bulk-UDP QoS passes handshake and echo yet delivers nothing.
  - **Topology gate**: `selectorStep` stays when `primaryChannel == "tcpws"` (else it fabricates a switch from a nonexistent QUIC channel).
  - **Switch back is echo-gated ONLY, no score comparison** (QUIC's score is unmeasurable while TCP-WS serves): one-shot `quicEchoRecovered` = fresh handshake + 3 consecutive echo rounds in `runProbeOnce` (primary-is-quic only; re-checks `FallbackActive()` before storing). Anti-thrash: `nextReviewAt` flap window, 30s doubling to 600s, failed trial → 600s then 1h; trials graded by answered traffic, not wall clock.
  - **Per-request fallback** to TCP-WS on one failed *transport-layer* dial, independent of the FSM. `RecordQUICStats` is sampled 1Hz for **every** outbound (else a stale non-primary score false-trips the quality path); also `RecordEchoResult`/`RecordAvailability`.
  - **`Reset` vs `ResetConnections` vs `ReplacePrimary`**: `Reset` (platform network change) wipes selector state, both scores and the min-RTT ratchet (never decays on its own); `ResetConnections` (same path) clears only `quicEchoRecovered`; `ReplacePrimary` (re-race) keeps path-scoped `flapCount`, `failedTrials`, `rollbackAt`.
  - **Path memory** (`path_verdict.go`, #4055): `selState.pathEvidence` marks only a quality/data-path switch-away; `PathVerdict` exported at engine Stop (unproven trial = failed), `PathVerdict.EvidenceAt` (`selState.pathEvidenceAt`) carried unchanged by `ApplyPathVerdict` / `ReplacePrimary`; `ApplyPathVerdict` starts the next session on tcpws. Store + 10 min TTL in `engine/path_memory.go`; cleared by `Reset`.
  - **Rollback valve**: `K2_CHANNEL_SELECTOR_LEGACY=1` = pre-scoreboard semantics; production escape hatch.
- `ip.go` — `ProbePublicIP`, `FormatSslipHost`, `HasIPv6` (server auto-provisioning helpers)
- `tlspad/` — TLS record padding (probe target cert length, pad server cert to match)
- `k2cc/` — K2CC (PCC Vivace) congestion control for QUIC
- `VOCABULARY.md` — Three-layer vocabulary convention (Wire/Relay/Tunnel)
- `relay.go` — `RelayFetch` antiblock control-plane relay: TCP→uTLS(ECH+enforced pin)→one HTTP/1.1 round-trip to Center through a node. `RelayFetchJSON` = gomobile/daemon string boundary. Enforces pin via `VerifyPeerCertificate` (unlike the tcpws dial template).
- `contract_test.go` — 11 transport behavior contract tests (`TestContract*`)
- `testmain_test.go` — goleak `TestMain` with quic-go and smux ignore patterns

## ECH Rotation Flow

`EnsureECHPrivateKey` (X25519) → `ECHProvider.FetchTemplate()` (Cloudflare HTTPS record) → `GenerateECHKeyFromTemplate` (config_id = Cloudflare's + 1) → `SaveECHConfig` / `LoadAllECHConfigs` (sorted by config_id desc) → `BuildECHKeys`. Detail: `docs/ech-camouflage-notes.md`.

## Client TLS Verification Priority

1. `pin=sha256:...` → InsecureSkipVerify + pin hash check (most secure)
2. `insecure=1` → InsecureSkipVerify (dev/testing only)
3. Default → system CA + Kaitu CA - Chinese CA blocklist (CNNIC, WoSign, WoTrus, CFCA, GDCA, SHECA)

## Gotchas

- QUIC datagrams may be unsupported by the path/peer — `DialUDP` falls back to stream-based UDP.
- **connectMu hard timeout**: `QUICClient.connect()` bounds `transport.Dial` by `quicDialHardTimeout` (15s) in a goroutine, orphaning it if quic-go blocks (`DIAG: quic-dial-orphan-cleanup`) so connectMu is released — without it connectMu starvation blocked all dials 30s+ (v0.4.1).
- ECH requires X25519 keys (not P-256) — Go's crypto/tls only supports DHKEM(X25519). TLS wire formats are big-endian — always `binary.BigEndian` for ECH/TLS parsing. DNS queries for ECH config need a trailing dot (FQDN: `crypto.cloudflare.com.`).
- Server certs: dual RSA+EC pair, both padded to match the camouflage target's cert length; the EC CA (`kaitu_ca.pem`, disguised as DigiCert) signs both.
- `EchoProbe` verifies the Short Header data path — a QUIC handshake (Long Header) does NOT prove data flows; always echo-verify QUIC before declaring it the winner.
- **Echo classification is one shared, fail-open predicate** (`IsEchoUnsupported`, consumed by `race.go`, `probe.go`, `engine/health.go` `echoStep`) — **over-match timeouts**: a quic-go stream-deadline expiry is `os.ErrDeadlineExceeded` / `net.Error.Timeout()`, NOT `context.DeadlineExceeded` (#3463). `ErrEchoNoConnection` (`IsEchoNotProbed`) is a third class, not "unsupported". Conn-closed is judged by `isQUICConnClosedErr`. Dead-node evidence = user dials only (`dial_failure.go`, `ConnectFailures()`); the code-less metadata reject is NO opinion. See **`docs/echo-probe-notes.md`**.
- **A duration from two `time.Now()` calls is routinely 0 on Windows** — tests on measured durations must pass with 0 or they fail only on `test-windows`; `RecordEchoResult` treats tcpws echo `rtt <= 0` as full quality.
- `RaceTransport` builds independent QUICClients per candidate; winner returned immediately, losers cleaned up async (WaitGroup + safego.Go).
- Metadata response is `[0x00][MetadataFrame kv]` — `0x02` is taken by coded errors, success-with-payload rides behind `0x00`. Old clients read one status byte and `Close()`; leftover kv bytes are discarded safely on both transports (`TestMetadataResponse_OldClientSafe_*`). Server metadata streams must `CancelRead` on QUIC or the stream slot leaks.
- Device attribution (`ctx.dev.set` / `dev.set`) is gated on `verdict.OK`, independent of enforce/permissive — a self-reported udid is never trusted for accounting. `TestQUICMetadata_AttributionSetOnOK` / `TestTCPWSMetadata_AttributionSetOnOK`; negative `Test{QUIC,TCPWS}Metadata_AttributionRequiresVerdict` (`metadata_server_test.go`).
- `quicStreamConn.Close()` must call both `CancelRead(0)` and `Stream.Close()` (quic-go `Close` is FIN-only) — else the stream slot leaks and `OpenStreamSync` blocks at `MAX_STREAMS`.
- `ReadStreamHeader`/`ReadStreamResponse` need `io.ByteReader` — wrap conn with `bufio.NewReader(conn)`.
- `net.Pipe()` is synchronous — structure goroutines carefully in tests.
- ECH base64 in URLs must use `base64.URLEncoding` (standard `+` corrupted by `url.Query()`).
- **ECH / outer SNI** (**`docs/ech-camouflage-notes.md`**, spec `docs/superpowers/specs/2026-08-10-outer-sni-camouflage-design.md`): outer SNI = `public_name` only because the client sets it (not server-enforced). Forged-ECH probes are stopped by the plaintext `config_id` gate (`echDisposition` / `ownsECHConfigID` → reverse proxy before any TLS state) — never route on ECH *presence*. Residual: a replayed real `config_id` still completes the handshake.
- **Trust QUIC liveness** (KeepAlive 10s + MaxIdleTimeout 30s → `conn.Context().Done()`, checked by `connect()` before reuse): no manual `resetConnection()` on error paths, no app-level staleness check; sleep/wake → `OnNetworkChanged()` → `Reset()`.
- **Stream response timeout (5s) ≠ transport failure** — the server's upstream `net.Dial` is slow. Never bump `handshakeFailStreak` or fall back to TCP-WS on it; that streak is for QUIC handshake failures only.
- **Proxy-layer errors skip TCP-WS fallback**: errors after the request is sent (`StreamError`, `ErrStreamRejected`, `ReadStreamResponse` timeout) carry `IsProxyLayer() bool` (`ProxyLayerError` wrapper); `TransportManager` checks via interface (`proxyLayerErr`), never type enumeration. Only transport-layer errors trigger per-request fallback.
- **Relay UDP unhealthy → `ErrDatagramRelayUnhealthy`** after 3 consecutive relay timeouts (a session that **sent and never received** — `txPackets > 0 && rxPackets == 0` — or a send shed by `maxDatagramInflight`; an answered session hitting its idle deadline never counts). Only a recv clears the streak, so `admitSession` must keep letting **one new session per `relayTrialInterval` (30 s)** through while unhealthy (INFO `datagram relay unhealthy, trial session` / `datagram relay path healthy again`; clock `relayEpoch`). Relay metrics never trigger wire-layer actions — `VOCABULARY.md`.
- `DialTCP` earlyData is written after the stream header, before reading the response (saves 1 RTT; old servers buffer it fine). Pass `nil` for none.
- `SendDatagram` budget: `maxDatagramInflight=8` (`quic.go`, `ErrDatagramSendTimeout`) must stay well below quic-go's 32-frame datagram queue (`datagramQueue.Add()` blocks indefinitely when full); excess is dropped immediately, no goroutine spawned. Applies to client `quicUDPConn.WriteTo` (`quicUDPMux.sendInflight`) and server `serverUDPConn.WriteTo` (`connUDPState.sendInflight`).
- UDP sessions are multi-destination: empty `UDPFrame.Addr` = the session's first destination (old/new interop; `udp.go`, `server/handler.go` `maxUDPDestsPerSession = 64`). That fallback may be a **hostname** (`targetAddr` unparsed; sing-tun → `os.ErrInvalid`) — `core.pipePacket` repairs it (`repairSourceAddr`); never a per-packet `ResolveUDPAddr` here.
- **UDP drop counters — `0` never means "nobody counted"**: `UDPTxDrops` (datagram budget only; `quicStreamUDPMux` / `smuxUDPMux` error instead of dropping), `UDPRxDrops`, `UDPRxDropsNoSession` (separate — benign post-close tail). A stream `writeFrame` error kills only that mux (#3420, `DIAG: udp-mux-rebuild`), so a UDP-only outage under a healthy `DIAG: heartbeat` is real. `WireStats.Transport` + `WireFallback` say which path; diff cumulative `udpRxDrops` / `udpRxDropsNoSess` across heartbeats.
- `InitialPacketSize = 1400` (not 1280): a browser QUIC Initial (1274-byte `UDPFrame`) must fit one DATAGRAM frame, else `DatagramTooLargeError` → silent overflow-stream fallback.

## Testing

```bash
go test ./wire/...                          # unit + integration + contract + rotation + CA (~650 test funcs in wire/ alone)
go test -run TestContract -v ./wire/...     # contract tests only
```

