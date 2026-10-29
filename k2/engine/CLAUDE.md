# Engine Layer (engine/)

Unified tunnel lifecycle manager — single source of truth for connection state.
Both desktop (daemon/) and appext (appext/) delegate all lifecycle here.

## Architecture

```
Platform Shell (daemon/appext/gateway)
    │
    ├── builds engine.Config (platform-specific fields)
    └── calls engine.Start(cfg)
              │
              ├── 1.  prefetchRulesForStart — local only: rule.SeedFromEmbedIfEmpty, else whatever is on disk (no network on the connect path)
              ├── 1a. missingConfiguredRegions → DIAG: rule-bundle-missing-region + fail 504
              ├── 1b. buildOutboundMap (via URL → rule.Target; 0=direct, 1=reject, 2+=wire) + buildRuleEngine
              ├── 1c. rule-miss telemetry reporter (only if Telemetry.RuleMiss.Enabled)
              ├── 2.5. Start NetworkMonitor (DirectDialer needs DefaultInterface)
              ├── 5. Router (core.NewRouter(ruleEngine))
              ├── 6. Provider (external from gateway / platform FD / proxy / desktop TUN); Start failure → 412
              ├── 6.5. Per-outbound transport setup:
              │      for each unique k2v5:// → ParseURL → resolve → RaceTransport → TM
              │      (parallel via errgroup; first k2v5 route = primaryTM). Only k2v5:// routes get a
              │      transport — a k2subs:// route must be resolved by the shell first
              ├── 7. ClientTunnel
              ├── 8. DNS middleware (TUN/TProxy mode only)
              │      DNS proxy dialer = primaryTM's TCP-WS (control-plane)
              │      Data traffic dialer = per-outbound TransportManager
              ├── 9. Commit → StateConnected + start reRaceLoop / outboundReplaceLoop
              ├── 10. HealthMonitor start (1Hz wire stats + health FSM + echoLoop)
              └── 11. Rule bundle updater (immediate check, then hourly, hot-reload on change; skipped under NetworkExtension)
              QUIC recovery probing during TCP-WS fallback is wire's (`TransportManager.runProbeOnce`), not engine's.
```

## State Machine

5 engine states: `disconnected`, `connecting`, `connected`, `paused`, `reconnecting`
Note: `reconnecting` and `paused` are transient — engine.state stays `connected`, these are handler-only broadcasts

- `Start()`: disconnected → connecting → connected (or disconnected on error)
- `Stop()`: any → disconnected (idempotent, resets `paused`)
- `OnNetworkChanged()`: routes through netCoordinator as SignalChanged → reconnect if network is up
- `OnNetEvent(event)`: routes through netCoordinator — "unavailable" stops probe, "available" triggers doNetworkReconnect, "changed" reconnects if up (appext's `NotifyNetEvent` lands here)
- `Pause()`: connected → paused (releases wire transports via the scoreboard-preserving `ResetConnections`, provider stays alive — iOS memory pressure / every device sleep)
- `Wake()`: paused → connected (forces fresh wire transport connections, scoreboard and channel choice kept)
- `fail()`: any → disconnected + classified error

## Error Classification (error.go)

HTTP-aligned error codes for frontend consumption (ranges are load-bearing: 1xx network, 4xx client, 5xx server — never mix):

| Code | Category | Meaning | Pattern |
|------|----------|---------|---------|
| 101 | network | Network unavailable | network unreachable, no route to host |
| 108 | network | Timeout | net.Error.Timeout(), context.DeadlineExceeded |
| 400 | client | Bad config | parse URL, missing auth |
| 401 | client | Auth rejected | stream rejected |
| 402 | client | Payment required | subscription expired |
| 403 | client | Forbidden | pin mismatch, blocked CA |
| 412 | client | Environment setup failed (operator must fix the host) | any `provider.Start` failure (`providerStartError`, classified at the call site — sing-tun/wintun text is OS-localized); gateway `install intercept` / `iptables` / `ip6tables` messages |
| 502 | server | Protocol error | TLS/QUIC handshake failure |
| 503 | server | Server unreachable | TCP dial, connection refused |
| 504 | server | Rule bundles unavailable (retryable) | no bundle on disk after embedded seed, or a configured `match.region` bundle missing |
| 570 | server | Unclassified | fallback |

## Config (config.go)

`engine.Config` unifies all platforms via `wire.DirectDialer` interface:

| Field | Desktop (daemon) | App ext (appext) | Gateway (k2r) |
|-------|-----------------|-------------------|---------------|
| `Client` | from YAML/URL | from JSON | from YAML/URL |
| `FileDescriptor` | -1 (self-create TUN) | >=0 (platform FD) | -1 |
| `Provider` | nil (engine builds) | nil (engine builds) | gateway TPROXY provider |
| `DirectDialer` | daemon.directDialer | protectedDialer (Android) / nePlatform (Darwin NE) | gatewayDirectDialer (SO_MARK) |
| `NetworkMonitor` | sing-tun DefaultInterfaceMonitor (`singTunMonitor`) | nePlatform (Darwin) / nil (Android: platform calls `NotifyNetEvent`) | nil |
| `InterfaceMonitor` | same sing-tun instance | `nePlatform.InterfaceMonitor()` (Darwin) / nil | nil |
| `NetworkExtension` | false | true (Darwin) / false (Android) | false |
| `HealthCallback` | `d.broadcastSnapshot` | nil | gateway health callback |
| `MobilePowerSaving` | false (`K2_MOBILE_POWER_SAVING=1` test-only override) | true | false |

## Resource Ownership

> **谁创建，谁销毁。Borrower 只用不关。**

Engine creates provider, transports and tunnel in `Start()` and closes them in `Stop()`. Tunnel borrows provider and dialer references but **never closes them** — it only manages its own context, WaitGroup and connTracker. `tunnel.Close()` calling `provider.Close()`/`dialer.Close()` causes double-close panics (e.g. sing-tun `StopFD.Stop()` on macOS). Exception: an external provider (gateway) keeps its own pre-commit lifecycle — its creator destroys it.

`Stop()` order (everything closed OUTSIDE `e.mu` — `prov.Close()` can block on dnsOverride CGo, tunnel handlers call `ReportWireError()` which needs `e.mu`):

```
netCoord.close() → healthMonitor.stop() → reRaceCancel()   // goroutines that read tm/tunnel stop first
[extract + nil fields under e.mu, broadcast disconnected status]
networkMonitor.Close()   // fires callbacks
cancel()                 // engine context
prov.Close()             // stops new TUN packets + DNS override
closeOutboundTMs()       // drops wire connections; then each outbound's Close() (k2p closes ICE/QUIC)
tunnel.Close()           // cancel → conns.closeAll() → wg.Wait(); handlers fail fast because prov/tm are closed
ruleEngine.Close()
missReporter.Stop()      // last — may flush a final batch, blocks ≤5s
```

| Resource | Owner (create + destroy) | Borrower (use only) |
|----------|--------------------------|---------------------|
| provider | Engine (gateway: its own) | Tunnel, dnsHandler |
| transport/dialer (per-outbound TMs) | Engine | Tunnel |
| healthMonitor, networkMonitor | Engine | — |
| ctx + wg + connTracker, handler goroutines | Tunnel | — |

`connTracker.closeAll()` force-closes connections stuck in `pipe()`/`pipePacket()` so `wg.Wait()` returns promptly. A `Stop()` during `Start()` cleans up in the same order at step 9.

## Files

- `engine.go` — `Engine` struct, `Start`/`Stop`/`OnNetworkChanged`/`OnNetEvent`/`doNetworkReconnect`, assembly pipeline, `buildOutboundMap`/`buildRouteEntries`
- `netmon.go` — `NetEvent` (gomobile-compatible network event struct, 8 primitive fields) + `netCoordinator` (fuses sing-tun + platform signals: suppress reconnect when network down, full reconnect on recovery, 500ms debounce)
- `netmon_test.go` — netCoordinator unit tests (6 scenarios)
- `config.go` — `Config` struct (platform-unified tunnel parameters, uses `wire.DirectDialer` interface)
- `status.go` — `Status` struct, custom `MarshalJSON` (computes `uptime_seconds` dynamically)
- `event.go` — `EventHandler` interface (`OnStatus(Status)` + `OnStats`), state constants
- `error.go` — `EngineError`, `ClassifyError` (HTTP-aligned error codes, MultiTransportError handling)
- `health.go` — `healthMonitor` (1Hz sampling, ring buffer 300s, EWMA smoothing α=0.3 on smoothWireLoss, health FSM: healthy→degraded(≥5 streak)→critical(≥5 streak), corrective action: wire reconnect). **The reconnect preserves the wire scoreboard and backs off per repeat**: `onHealthCritical` → `e.reconnect(preserveScoreboard=true)` → `wire.TransportManager.ResetConnections` (connections only; scores / selector state / flap history survive — same path, same evidence). The first reconnect of a critical episode fires immediately ("the connection went stale" is worth one redial); if the redialed connection is critical again the path itself is lossy, and repeats wait `actionCooldownBase` (30s) doubling to `actionCooldownMax` (120s) — long enough for the channel selector's quality path (2 fresh backup samples at the 5s cadence + 10s sustain) to move traffic to tcpws in between. Ten consecutive healthy samples end the episode; a network reconnect (`doNetworkReconnect` → `resetActionBackoff` + `reconnect(false)`, i.e. the wiping `Reset`) forgets it. `resetCounters` (called by every reconnect) deliberately does NOT touch the backoff. Before this (ticket #3747, Android) a lossy-but-reachable QUIC produced a 10s reconnect loop that redialed the same path and wiped the selector's evidence each time, so the user could never reach tcpws. Pinned by `TestHealthMonitor_ReconnectBackoffGrowsAndResets` / `TestEngine_HealthReconnectPreservesScoreboard_NetworkReconnectWipes`. `sample()` uses primaryTM for heartbeat stats and for `HealthSnapshot`'s `ScoreQUIC`/`ScoreTCPWS`/`ActiveChannel` fields (`json:"score_quic"/"score_tcpws"/"active_channel"`, primary outbound only — same scope as everything else in the snapshot). It also calls `WireStats()` on every OTHER outbound in `hm.tms` (side-effect only, return discarded) — `WireStats()` is the one production path that primes a `ChannelScore`'s QUIC-quality component (`wire.TransportManager.RecordQUICStats`), so without this loop every non-primary outbound's `scoreQUIC.quality` would stay permanently uninitialized and its `ScoreQUIC` would be capped at `avail×0.5`, eventually false-tripping wire's quality-path switch-away on a perfectly healthy QUIC outbound. `echoLoop`: one primary-probe goroutine (`echoProbeLoop`) AND one backup-probe goroutine (`backupProbeLoop`), independently timed, per TM.
  - `echoProbeLoop` probes via `wire.EchoProber` on an exponential-trust schedule (`echoIntervals` 1s→16s, `echoProbeTimeout` 2s); 3 consecutive real failures trigger per-TM re-race, so wire death is detected in ~5s. An `init()` guard panics if that worst case ever reaches QUIC `MaxIdleTimeout` — past that point the loop adds nothing over quic-go's own idle timer. **Backward compat**: `echoStep` routes `wire.IsEchoUnsupported` errors (pre-echo servers) to trust-growth rather than strikes, so old servers never storm re-races. That predicate is fail-open, and misreading a real timeout as "unsupported" is what silently disabled re-race entirely in ticket #3463 — see the timeout note in `wire/CLAUDE.md` before touching it. Each probe also feeds the dual-channel quality scoreboard: `RecordEchoResult(primaryChannel, ...)` + `EvaluateSelector(now)` (see `wire/CLAUDE.md` transport.go for the FSM this drives) — a second, uncoupled job on the same probe, never influencing trust/strikes.
    - **Strike gating (`suppressStrike`)**: scoring is never gated, but two topologies keep an echo failure OUT of the strike counter **while the TCP-WS channel has live proof of life**, because there a failing echo is expected rather than informative. (1) Fallback active (`ActiveChannel()=="tcpws"`) while the primary is still the QUIC dialer, AND `TCPWSChannelRecentlyHealthy(30s)`: that QUIC path is known dead — leaving it ungated, its timeouts strike 3× in seconds and fire a re-race that closes the QUIC dialer with no backup and `ReplacePrimary(tcpws)`es over the selector's whole fallback state, so the user never returns to QUIC; recovery of that path is owned by wire's `runProbeOnce`. (2) The primary IS the tcpws channel and `TCPRecentlyHealthy(30s)` is true: `TCPWSClient` became an `EchoProber` in this branch, so tcpws primaries now run the 1-16s schedule with a 2s timeout, and TCP head-of-line blocking under bulk transfer can blow that 2s three times in a row on a session that is working — the re-race would close the very session serving the transfer. With no recent TCP success, strikes accumulate normally in **both** topologies.
    - **Why case (1)'s proof condition is mandatory, and the trade it accepts**: `probeOnce` is the ONLY producer on `reRaceChan`, Phase B server replacement (`replaceOutboundServer`) is reachable only through it, and `assess()` cannot reach critical on TCP-WS stats — so there is no other automated escalation. Suppressing on topology alone would sever it exactly when the NODE dies while a user sits in fallback (crash, quota cutoff — routine fleet events), leaving both channels dead and the user hung until a network change or manual reconnect. Accepted trade: an **idle** fallback user produces no dials, so has no live proof, so strikes resume and a re-race may land tcpws as primary — which is now a stable, honest terminal state (the C1 topology gates stop the FSM and the recovery probe from fabricating a QUIC channel there). The idle case is thus the pre-fix status quo minus the phantom loop. Note the proof clock is fed by **`DialTCP` only** (wire's `recordTCPWSDialSuccess`): that path reads a server response, so success is end-to-end; UDP dials over tcpws open a smux stream with no round trip and keep "succeeding" against a dead node, which would manufacture exactly the proof this gate must not have.
    - **Honest scope of root cause C**: the spec claimed channel switching would leave re-race entirely. What is delivered is narrower — re-race no longer fires *from the dead primary during fallback* or *from a loaded-but-working tcpws primary*; it remains the escalation for every other echo-death, which is its intended "this outbound is beyond channel choice" role.
  - `backupProbeLoop` opportunistically echo-probes the currently-inactive channel on its own timer (never piggy-backed on `echoProbeLoop`'s tick, which would distort its re-race timing invariant). Scope is asymmetric: active=="quic" → probes tcpws at `backupProbeIntervalHealthy` (60s), upgraded to `backupProbeIntervalDegraded` (5s) once `ScoreQUIC` drops below `echoBackupProbeThreshold` (0.7, mirrors wire's private `qualityDegradeThreshold`) so the quality path's `backupFresh` gate has fresh samples by the time it needs them; active=="tcpws" → no-op, because QUIC recovery probing during fallback is already owned by wire's `runProbeOnce` (see `wire/CLAUDE.md`). `hm.mobilePowerSaving` (desktop `false`, set from `Config.MobilePowerSaving`) narrows this to **distressed only** (`primaryDistressed`: the health FSM in `critical`, which only smoothed loss > 0.25 can reach, **or** `wire.TransportManager.DataPathDistressed` — three consecutive quic `DialTCP`s sent and never answered): while QUIC is healthy or merely degraded *and answering requests*, mobile probes nothing and relies on protocol keepalive; once distressed it probes at the 5s cadence until the selector switches or QUIC recovers. The second key (round 3, #3757) closes the loss gate's blind band: CC loss only moves when a monitor interval completes, so an app-limited sender reports `loss=0.000` for minutes while every request dies at the 5s deadline, and the loss-only gate never opened (#3757: 205/260 QUIC heartbeats at loss 0; the emulated per-flow-policed link took 32s of dead dials to cross 0.25). Desktop is not gated but its cadence upgrade was score-only; distress now upgrades it to 5s as well, since the selector's data-path fast path needs two fresh tcpws samples and the 60s cadence would take a minute to supply them. Health grading itself is untouched — distress opens probing, `wire`'s selector decides. Pinned by `TestBackupProbe_MobilePowerSavingProbesWhenDataPathDistressed` / `TestBackupProbe_DesktopCadenceUpgradesOnDataPathDistress`. **The gate's early returns are a WAKE cadence, not a probe cadence**: all three (`mobilePowerSaving && !critical`, `active != quic`, tcpws-primary) return `backupProbeIntervalDegraded` (5s) — pure in-memory checks, no radio — because that return value is `backupProbeLoop`'s only wake source; the original #3747 fix returned the 60s healthy interval there, so the gate opening (and a rollback to quic) was noticed up to 60s late, and since the first critical's reconnect closes the gate again for ~7s via `resetCounters`, a wake landing in that gap slept another 60s. The backup echo runs under its own `backupEchoProbeTimeout` (5s, not the primary's 2s `echoProbeTimeout`): the backup tcpws is usually cold (every health-critical `ResetConnections` drops its session too) and a cold TCP+TLS+WS+smux+metadata dial from cellular does not fit in 2s — a spurious timeout here is not a strike but a false availability sample that drags `scoreTCPWS` under `minSwitchTargetScore` exactly when the switch needs it. It is keyed on the health grade, **not** on `ScoreQUIC < 0.7`, on purpose: the score carries an RTT term, and cellular bufferbloat under a healthy download drives it to ~0.5 while a tcpws first measured under that same load gets a loaded min-RTT baseline (ratio ≈ 1.0) — score-gating would switch a healthy download onto tcpws. Loss separates "the UDP path drops bulk" (ticket #3747: carrier UDP QoS, handshake and echoes pass, nothing loads, tcpws — which the DNS control plane was already using — never got a sample) from "the shared queue is full". Pinned by `TestBackupProbe_MobilePowerSavingProbesWhenLossCritical`.
  - TCP health gate: `TCPRecentlyHealthy()` (30s window) suppresses reconnect when TCP streams are working — and, as of the final review, the echo strike above. It answers for whichever primary is installed: `*QUICClient`'s own `lastTCPSuccessAt`, or (tcpws-as-primary) the TCP-success clock `wire.TransportManager` records on `DialTCP`'s tcpws success paths. It returned a flat `false` for every tcpws primary before that second branch existed, which would have made the strike gate a silent no-op in exactly the topology it targets. Wire-layer metrics only — relay metrics are observability (see `wire/VOCABULARY.md`). `wakeDetector` (embedded in healthMonitor): detects sleep/wake via wall-clock gap, `DIAG: wake` at >5s, triggers `doWakeReconnect()` at ≥30s (aligned with QUIC MaxIdleTimeout) — the **scoreboard-preserving** reconnect (`reconnect(true)` → `ResetConnections`, backoff kept), because a wake says the connections are stale, not that the path changed; a path change arrives separately as a NetEvent / sing-tun interface change and goes through `doNetworkReconnect()` (`Reset`, backoff forgotten). `Pause()`/`Wake()` preserve the scoreboard the same way (iOS calls them on every screen lock/unlock). Before this every phone pickup on a lossy-but-reachable QUIC put the user back on it and replayed the whole escape. Pinned by `TestEngine_PauseWakeAndWakeReconnectPreserveScoreboard`. Heartbeat includes memory metrics (`heapMB`, `sysMB`, `goroutines`) via `runtime.ReadMemStats` every 30s for iOS NE jetsam diagnosis, plus `activeChannel`/`scoreQuic`/`scoreTcpws` (Task 8) so the dual-channel scoreboard is visible without DEBUG — see the heartbeat field list in `k2/docs/diag-events.md`. It also carries the **uplink** CC window since the previous heartbeat (`ccMis ccAppLimited ccFloor ccLossP90 ccRttP90 ccBwEst ccBgLoss ccMode`, via `ws.CC.Sub(hm.lastCC)`; the `SenderID` epoch restarts the window on reconnect, and tcpws fallback reads as all-zero with `fallback=true`) — `ccAppLimited ≈ ccMis` is the normal shape for a VPN client that barely uploads, not a fault; the server's `DIAG: cc-summary` is where download-direction CC lives.
- `health_test.go` — Health monitor tests (EWMA smoothing, transitions, reset)
- `engine.go:reRaceLoop` — Background re-race loop triggered by echoLoop (3 consecutive echo failures → channel signal → wire.RaceTransport → ReplacePrimary)
- `contract_test.go` — 4 transport behavior contract tests (TCP health gate, server unreachable stop, auth no-retry, timing)
- `happy_eyeballs.go` — `resolveDialAddr`, `happyEyeballs` (RFC 8305 IPv6/IPv4 race with 250ms head start)
- `dns_handler.go` — `dnsHandler` (port 53 TCP+UDP interception, RFC 1035 TCP framing)
- `network.go` — `NetworkChangeNotifier` interface
- `engine_test.go` — Engine lifecycle tests
- `deadlock_test.go` — Deadlock detection tests (Stop vs health goroutine)
- `slog_migration_test.go` — Guard: no old `log.Printf` calls

## Testing

```bash
go test ./engine/...                          # Engine + error classification tests
go test -run TestContract -v ./engine/...     # contract tests only
```
