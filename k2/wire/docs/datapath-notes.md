# QUIC data path, fingerprint, UDP relay & datagram budget — full notes

Moved **verbatim** from `wire/CLAUDE.md` (2026-11-14) to keep that always-loaded file short. The invariants stay summarised in `wire/CLAUDE.md`; this file is the full rationale, incident history and the tests that pin each rule. **Read this before changing `quic.go`, `quic_fingerprint.go`, `race.go`, `downlink_loss.go`, `cc_summary.go`, the UDP relay / datagram paths or `Config` URL parsing.**

## Config: `fp=` and port hopping

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

- `quic.go` — QUIC config, `StreamConn` adapter, `QUICClient`/`QUICServer`, relay UDP health tracking (`quicUDPMux`: consecutive timeouts → `ErrDatagramRelayUnhealthy` for TCP-WS fallback, any recv clears the streak, one trial session per 30 s while unhealthy). Per-connection CC observability: `acceptLoop` hands the download-direction k2cc sender to `handleConn(conn, cc)` → `connContext.cc` → `ccSummaryLoop` emits INFO `DIAG: cc-summary` every 60 s (`ccSummaryInterval`, test-shortened) and a `final=true` line on connection end; a periodic line is skipped when the window completed 0 MIs, the final line when the connection never completed one (rejected / scanner / auth-only / sub-200 ms connections leave no line — see `k2cc/CLAUDE.md` Observability). The loop owns prev/seq and exits on `state.done` or `conn.Context().Done()`. Key set is the Phase 2/3 contract — append only (`TestCCSummaryAttrs_FieldSetAndFormatting` pins it). P2P server conns (`NewQUICServerFromConn`) pass `cc=nil` → no line.
- `quic_fingerprint.go` — the Chrome-146-over-QUIC fingerprint handed to kuic on every dial: `buildChromeH3Spec()` (the `ClientHelloSpec`, via `Config.NewClientHelloSpec`) and `chromeH3InitialPacketSpec()` (the Initial *packet* shape, via `Config.InitialPacketSpec`). Ported from refraction-networking/uquic (BSD-3). **`buildChromeH3Spec` is a factory, never a cached value**: kuic writes this connection's transport parameters — including `initial_source_connection_id` — into the returned spec, so a shared one has concurrent dials overwrite each other. Two spec-authoring requirements are load-bearing and fail loudly if dropped: a `QUICTransportParametersExtension` (else `TRANSPORT_PARAMETER_ERROR`) and a GREASE-ECH placeholder (else the server answers `missing_extension` and ECH is silently unused). **Scope**: the packet shape now covers CID lengths, first PN and its encoding lengths `{1,2}`, and CRYPTO fragmentation with PING/PADDING across the two Initial datagrams the ClientHello spans; what remains quic-go's is Initial CRYPTO *retransmissions*/PTO probes, visible only under packet loss. The `quic_fingerprint.go` SCOPE table and `kuic/MAINTAINING.md` carry the details.
- `downlink_loss.go` — `downlinkLossWindow`: turns kuic's cumulative receive-side 1-RTT counters (`ConnectionStats.PacketsReceived1RTT` / `PacketNumberSpan1RTT`) into `WireStats.DownlinkLossRate` (window ≥2s and ≥20 server packet numbers, grows up to 10s, else 0 = no evidence; reset on a new connection). `WireStats.PacketLossRate` stays k2cc's **uplink** MI loss. **Observability only** — heartbeat `downLoss`; it is NOT a health or selector input (pinned by `TestSelector_DownlinkLossDoesNotDriveQualityPath` + `TestHealthMonitor_DownlinkLossDoesNotChangeHealth`). Why it exists: a downloading client's own sender completes no k2cc MI under heavy downlink loss, so uplink loss read 0 while 60% of the server's packets were dropped (PVE netem 2026-11-05). Why it does not decide: production cc-summary the same day showed users at 27-37% sustained downlink loss with only good ratings — k2cc serves that loss fine, and grading it critical would force reconnects + a TCP-WS switch on a working session. A future switch trigger should key on outcomes (unanswered requests, echo failures), judged against production cc-summary × ratings, not netem.
- `race.go` — `RaceTransport`: two-way Happy Eyeballs (QUIC-443 → TCP-WS at +800ms), staggered start, echo verification. **`raceTimeout` (5s) bounds each candidate's transport step only** (`candDeadline`): once `connect()` reports the handshake done (`transportUp` ctx hook, called just before `sendMetadata`) the candidate gets `metaPhaseBudget` (metaReadTimeout + race echo + 0.5s) and the collector waits for it (+`raceCollectorSlack` 100ms) — worst case exported as `RaceWorstCase()` = 14.6s; the engine derives every race ctx from it (`raceCtxTimeout` = worst + 1s, engine `TestRaceCtxOutlivesWireWorstCase`; wire side `TestMetaReadTimeoutOutlivesNodeAuthTimeout`). A node that never completes the handshake still fails at 5s

## Gotchas

- **connectMu hard timeout**: `QUICClient.connect()` runs `transport.Dial` in a goroutine bounded by `quicDialHardTimeout` (15s). If quic-go's `Conn.destroy()` blocks (waiting for `conn.run()` to exit on context cancel), the goroutine is orphaned (`DIAG: quic-dial-orphan-cleanup` when it finally resolves) and connectMu is released. Without it, connectMu starvation blocked all DialTCP/DialUDP for 30s+ (v0.4.1, 4/22 devices). `TCPWSClient` relies on `net.Dialer{Timeout}`; `daemon.directDialer` derives Timeout from the context deadline.
- Relay UDP unhealthy → `ErrDatagramRelayUnhealthy`: When relay datagram path has consecutive relay timeouts (threshold: 3), `DialUDP` returns `ErrDatagramRelayUnhealthy` for per-request TCP-WS fallback. A relay timeout is a `quicUDPConn` read deadline on a session that **sent and never received** (`txPackets > 0 && rxPackets == 0`), or a send the datagram budget shed (`maxDatagramInflight`). A session that was answered and then hit its idle deadline is not one: every UDP session ends on that deadline, and whenever the client had the last word (QUIC close, final ACK) the downlink deadline fires first — counting those marked a healthy path unhealthy after three quiet sessions. Loss rate tracking was removed (DNS racing inflated false positives). Any successful recv clears the timeout streak, and nothing else does — so an unhealthy relay keeps one way open for a datagram to arrive: `admitSession` lets **one new session per `relayTrialInterval` (30 s)** use the datagram path anyway (INFO `datagram relay unhealthy, trial session`). Its reply heals the relay (`datagram relay path healthy again`); if it goes unanswered, that one flow is black-holed until its idle deadline (up to 60 s, so two unanswered trials can be open at once) and the next trial is an interval later — healing therefore waits for a trial that lands on a destination which answers. DNS is never the trial (it uses `DialStreamUDP`), and no trial happens while the active channel is TCP-WS (`TransportManager` does not ask QUIC then). Without the trial, once the last datagram session had ended nothing could arrive any more and the relay stayed unhealthy until the QUIC connection was replaced — on a router, as long as it ran (three LAN broadcasts to nobody were enough to get there). The schedule is set when the relay is marked unhealthy and is not pushed out by later timeouts (shed bursts, the trial's own). The clock is monotonic (`relayEpoch`). Relay-layer metrics never trigger wire-layer actions (reconnect, transport switch). See `VOCABULARY.md`.
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

## Gotchas — liveness, stream timeout, proxy-layer errors

- QUIC connection liveness: quic-go's KeepAlive (10s) + MaxIdleTimeout (30s) detect dead connections at protocol level. `conn.Context().Done()` fires when connection dies. `connect()` checks this before reusing cached conn — no manual `resetConnection()` needed on error paths. macOS sleep/wake is handled by singTunMonitor → `OnNetworkChanged()` → `Reset()` (desktop), or by platform-native callbacks (appext). No application-level wall-clock staleness check needed.
- Stream response timeout ≠ transport failure: A 5s stream response timeout means the server's upstream `net.Dial` to the destination is slow/failing — NOT that QUIC transport is broken (OpenStreamSync succeeded = transport is alive). Do NOT increment `handshakeFailStreak` or trigger TCP-WS fallback on stream timeouts. `handshakeFailStreak` is strictly for QUIC handshake failures (UDP blocked by firewall).
- Proxy-layer errors skip TCP-WS fallback: Dialer marks errors as proxy-layer after the request is sent (stream opened + header written). `ProxyLayerError` wrapper and `StreamError` both implement `IsProxyLayer() bool`. `TransportManager` checks via interface (`proxyLayerErr`), not type enumeration. Covers: `StreamError` (server error code), `ErrStreamRejected` (server rejection), `ReadStreamResponse` timeout (server processing). Only transport-layer errors (handshake failure, stream open failure, write failure) trigger per-request fallback.

## Interfaces block, misc Files/Gotchas narration (moved 2026-11-14)

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

- `cc_summary.go` — `ccSummaryLoop` + `ccSummaryAttrs` (pure formatter); `cc_summary_test.go` has the real client/server positive control (paced 4 MiB download → periodic + final lines, seq/udid/mode-sum invariants) and negative control (5-byte echo → 0 lines) plus the goroutine-safe `syncRecordCollector`.
- `quic_limits_desktop.go` — Desktop/server QUIC flow control windows (8 MB stream / 20 MB conn / 256 streams)
- `quic_limits_mobile.go` — Mobile QUIC flow control windows (2 MB stream / 8 MB conn / 64 streams) — iOS NE ~50 MB jetsam limit
- `contract_test.go` — 11 transport behavior contract tests (startup, fallback, recovery, datagram degradation, network switch, slow upstream, proxy error, stream rejection, read-response timeout)
- `RaceTransport` creates independent QUICClient instances per candidate. Loser cleanup is async via WaitGroup + safego.Go. Winner is returned immediately.
- `quicStreamConn.Close()` must call both `CancelRead(0)` and `Stream.Close()`. quic-go's `Stream.Close()` only closes the write direction (FIN). Without `CancelRead`, the receive half never completes and the stream slot leaks — causing `OpenStreamSync` to block at `MAX_STREAMS`. Fixed in the custom `Close()` method.
- QUIC flow control is platform-specific (build tags `ios || android` vs desktop) — mobile uses smaller windows (2 MB stream / 8 MB conn vs 8 MB / 20 MB desktop) to stay under iOS NE jetsam limit.
- QUIC stream responses have 5s timeout to prevent indefinite blocking on slow/dead servers.
- `DialTCP(ctx, addr, earlyData)` sends earlyData after stream header, before reading server response. Server's `bufio.Reader` naturally buffers it during header parsing; `pipe()` consumes it after upstream dial. Saves 1 RTT on high-latency links. Backward compatible — old servers handle it without changes. Pass `nil` for no early data.
