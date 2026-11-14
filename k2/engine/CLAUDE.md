# Engine Layer (engine/)

Unified tunnel lifecycle manager — single source of truth for connection state.
Both desktop (daemon/) and appext (appext/) delegate all lifecycle here.

**Long-form notes (not auto-loaded — read the one you need before changing that code):** `docs/failover-notes.md` (hold / replace loop / exhaustion — every rule with its review ruling) · `docs/health-notes.md` (health FSM, echo + backup probes, strike gating, wake, heartbeat, path memory).

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
              ├── 9. Commit → StateConnected + start outboundReplaceLoop
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

### Hold and replace loop — `hold.go`, `outboundReplaceLoop` (full rules: `docs/failover-notes.md`)

A node that looks dead may only look dead because the user's network is. Invariants:

- **Judge the network before spending candidates**: `judgeNet` → `decideNet` (netCoordinator `isNetworkUp` → passive direct dial ≤10s old via `directTrack` → active `netChecker` on `ClientConfig.Netcheck`), logs `DIAG: netcheck`. Judged at `pre-phaseB` and `pre-exhaust` (always active there; provider exclusions are `Reset()` before holding). Hold / schedule ticks judge **active-only** (`SkipPassive`) — behind a captive portal direct dials "succeed".
- **Empty netcheck list = `netUnknown` = no gating** (k2r gateway / CLI / old webapp); only iface-down (101) can still hold.
- **Hold display**: `buildStatusLocked` reports `reconnecting + holdErr` (101/106/107); **a 401/402 `lastError` outranks the hold**. Backoff `holdBackoff` 5s→10s→30s…, `holdWake` re-judges at once. Only the replace loop writes `holdErr` (plus `Stop`/`fail`); `setHold` is a no-op after ctx cancel or state ≠ connected.
- **No candidates** (`ErrNoCandidates`: manual mode / no fallbacks): never `fail`, never block the loop — the target joins the goroutine-local `retrySet` (Phase A per tick; Phase B with a Reset provider, at most once per `codeLessPhaseBEvery` = 2 min while the node answers code-less). Shown hold: 503. Manual mode and static-only targets retry forever.
- **Exhaustion on a healthy network does not disconnect (R1, 2026-11-05)**: it joins `retrySet` as exhausted (`exhaustedHoldError` 503, `DIAG: outbound-exhausted` / `outbound-exhausted-recovered`). Only `exhaustFailAfter` = **10 min** of continuous online exhaustion fails: `failAtExhaustCap` → `failExhausted` = `OnOutboundFatal` (k2r rebuilds; daemon / appext end disconnected) + `fail(503)`. Offline ticks, a code-less Phase A, or any code-less candidate answer don't count — **a Center outage never disconnects**. A `fromShell` target answering `ErrNoCandidates` (subscription dropped its URL) is an exhaustion, capped.
- **Account verdicts fail at once**: a coded 401/402/403 on the **first** Phase-A (`TestReplaceLoop_FirstPhaseAUserActionFailsImmediately`), a sticky 401/402, or every candidate tried rejecting with a coded 401/402/403. The verdict is `replaceLoopUserAction` (`replace_verdict.go`), **not `ClassifyError`**: the code-less `unauthorized` (Center unreachable from the node) and `wire.ErrMetadataTimeout` are server-side transients — keep rotating.
- **The loop has exactly two session-ending exits**, `failIfUserAction` and `failAtExhaustCap`. `TestReplaceLoopFailSitesAllowList` (AST) allow-lists every reference to `e.fail` / `failExhausted` / `OnOutboundFatal` / `e.Stop` / `e.cancel` / `e.reRaceCancel` — a new call site fails it.
- Every engine race ctx is `raceCtxTimeout` = `wire.RaceWorstCase()` (14.6s) + 1s (`TestRaceCtxOutlivesWireWorstCase`).
- **Pause**: while `e.paused` nothing netchecks or races (a race would reinstall the connections Pause released); a Pause landing mid-round aborts it (`errPausedRound` / `escPaused`); paused time is excluded from the cap. `Wake()` pokes `holdWake`.
- **Providers**: `Start` installs `newCompositeProvider(newStaticProvider(Fallbacks), cfg.OutboundProvider)`; `compositeProvider.Reset()` resets **both** halves.
- **Netcheck DNS**: `client.NetcheckDNS` when set (overleap brand), else `client.DNS.Direct`; `netcheck_dns` is netcheck-only. `directResolve` uses A first, AAAA only when A is empty.
- **iOS shows connected through holds and the whole 10-min exhaustion** — the NE drops `reconnecting`; only `NEVPNStatus.reasserting` shows (spec §5.1 / §6 item 5). Android / desktop show reconnecting + reason.
- Tests: `failover_test.go` — all fakes, injectable clock (`holdTimerFn`, `holdNow`), no real sleeps.

## Error Classification (error.go)

HTTP-aligned error codes for frontend consumption (ranges are load-bearing: 1xx network, 4xx client, 5xx server — never mix):

| Code | Category | Meaning | Pattern |
|------|----------|---------|---------|
| 101 | network | Network unavailable | network unreachable, no route to host |
| 106 | network | No internet (interface up, nothing answers) | not pattern-matched — netcheck verdict `netNoInternet.holdError()` (`netcheck.go`). **Not** shown when no netcheck targets are configured (`netUnknown`, no gating) |
| 107 | network | Captive portal / interception | not pattern-matched — netcheck verdict `netCaptive.holdError()`: a target answered with the wrong status/body (redirects are not followed) |
| 108 | network | Timeout | net.Error.Timeout(), context.DeadlineExceeded |
| 400 | client | Bad config | parse URL, missing auth |
| 401 | client | Auth rejected | stream rejected |
| 402 | client | Payment required | by code, producer-typed: `wire.ErrMetadataRejected` wrapping `StreamError{Code:402}` (node metadata auth) and `config.FetchStatusError{Status:402}` (Center k2subs fetch; 400/401/403 map to same code, 5xx→503, else 570). Text fallback `payment required` only; bare word `subscription` deliberately NOT matched. A sticky 401/402 is not overwritten by outbound exhaustion, and a 401/402 re-race skips replacement candidates |
| 403 | client | Forbidden | pin mismatch, blocked CA |
| 412 | client | Environment setup failed (operator must fix the host) | any `provider.Start` failure (`providerStartError`, classified at the call site — sing-tun/wintun text is OS-localized); gateway `install intercept` / `iptables` / `ip6tables` messages |
| 502 | server | Protocol error | TLS/QUIC handshake failure |
| 503 | server | Server unreachable | TCP dial, connection refused; outbound replacement candidates exhausted on a healthy network (`outboundExhaustedError`, set by the producer); also the `reconnecting` hold reason when the outbound has no replacement candidates (no disconnect) |
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

## Health and probing — `health.go` (full rules: `docs/health-notes.md`)

- `healthMonitor`: 1Hz sampling, EWMA α=0.3 on k2cc's **uplink** `PacketLossRate` only — `DownlinkLossRate` is heartbeat-only by decision (`TestHealthMonitor_DownlinkLossDoesNotChangeHealth`). FSM healthy→degraded(≥5 streak)→critical(≥5 streak).
- **Critical → `reconnect(preserveScoreboard=true)`** (`ResetConnections`: connections only, wire scores survive). First reconnect of an episode is immediate; repeats wait `actionCooldownBase` 30s doubling to `actionCooldownMax` 120s; ten healthy samples end the episode. `resetCounters` must NOT touch that backoff (ticket #3747: a 10s loop that wiped the selector's evidence so the user never reached tcpws). Pins: `TestHealthMonitor_ReconnectBackoffGrowsAndResets`, `TestEngine_HealthReconnectPreservesScoreboard_NetworkReconnectWipes`.
- **Preserve vs wipe the scoreboard**: health critical, wake (`wakeDetector` → `doWakeReconnect` at a ≥30s wall-clock gap) and `Pause`/`Wake` preserve; only a path change (`doNetworkReconnect` → `reconnect(false)` + `resetActionBackoff`) wipes. Pin: `TestEngine_PauseWakeAndWakeReconnectPreserveScoreboard`.
- `sample()` must keep calling `WireStats()` on every **non-primary** outbound — it is the only path that primes `ScoreQUIC`'s quality component; without it healthy non-primary QUIC outbounds false-switch to tcpws.
- `noteActiveChannelLocked`: a change of serving channel restarts the health grade (ticket #4055).
- **`echoProbeLoop`** (`echoIntervals` 1s→16s, `echoProbeTimeout` 2s): 3 strikes → `reRaceChan` → `outboundReplaceLoop` Phase A (`phaseA`: `wire.RaceTransport` → `ReplacePrimary`). It is the **only producer on `reRaceChan`**, hence the only road to Phase B. Strikes decay on recovery; not-probed (`wire.ErrEchoNoConnection`) strikes only on user-dial evidence while the network is up (dead node → Phase B 29–31s after the first failing dial; `echo_dialfail_timeline_test.go`); `wire.IsEchoUnsupported` routes to trust growth, never strikes — it is fail-open and once silently disabled re-race (ticket #3463, read `wire/CLAUDE.md` first). `suppressStrike` keeps strikes out **only while TCP-WS has live proof of life** (`TCPWSChannelRecentlyHealthy(30s)` / `TCPRecentlyHealthy(30s)`, fed by `DialTCP` only) — never gate on topology alone, or a node dying under a fallback user is never escalated.
- **`backupProbeLoop`** probes the inactive channel only when active=="quic": `backupProbeIntervalHealthy` 60s, `backupProbeIntervalDegraded` 5s once `ScoreQUIC` < `echoBackupProbeThreshold` (0.7) or the primary is distressed. Mobile (`MobilePowerSaving`) probes **only when distressed** (`critical`, or `wire.TransportManager.DataPathDistressed`). The gate's early returns are a 5s *wake* cadence, not a probe cadence; the backup uses `backupEchoProbeTimeout` 5s (cold tcpws). Gate on the health grade, not the score — cellular bufferbloat drags the score to ~0.5 on a healthy download. Pins: `TestBackupProbe_*`.
- Heartbeat (every 30s): memory (`heapMB`, `sysMB`, `goroutines` — iOS NE jetsam), `activeChannel` / `scoreQuic` / `scoreTcpws`, uplink CC window (`ccAppLimited ≈ ccMis` is normal for a client). Field list: `k2/docs/diag-events.md`.
- **`path_memory.go`**: the selector's QUIC-path verdict (`wire.PathVerdict`) survives `Stop` → next `Start` (disk copy `path-memory.json`), 10-min TTL counted from `PathVerdict.EvidenceAt`, cleared by a network-change reconnect. Pins: `TestEngine_StopRecordsPathMemory`, `TestEngine_NetworkChangeClearsPathMemory_HealthReconnectKeepsIt`; the Start hook only by `test/netem/nslab.sh RECONNECT_AT`.

## Files

- `engine.go` — `Engine` struct, `Start`/`Stop`/`OnNetworkChanged`/`OnNetEvent`/`doNetworkReconnect`, assembly pipeline, `buildOutboundMap`/`buildRouteEntries`, `outboundReplaceLoop` + `phaseA` (the old separate `reRaceLoop` is folded in)
- `hold.go` — failover network gate (`judgeNet`, `holdUntilOnline`, `setHold`, `wakeHold`, `directResolve`) · `replace_verdict.go` — `replaceLoopUserAction`
- `health.go` — `healthMonitor`, `echoProbeLoop`, `backupProbeLoop`, `wakeDetector` · `path_memory.go` — cross-session QUIC-path verdict
- `netmon.go` — `NetEvent` (gomobile-compatible, 8 primitive fields) + `netCoordinator` (fuses sing-tun + platform signals; suppresses reconnect while down, full reconnect on recovery, 500ms debounce)
- `config.go` — `Config` · `status.go` — `Status`, custom `MarshalJSON` (computes `uptime_seconds`) · `event.go` — `EventHandler` (`OnStatus(Status)` + `OnStats`), state constants · `error.go` — `EngineError`, `ClassifyError` (MultiTransportError handling) · `network.go` — `NetworkChangeNotifier`
- `happy_eyeballs.go` — `resolveDialAddr`, `happyEyeballs` (RFC 8305, 250ms IPv6 head start) · `dns_handler.go` — `dnsHandler` (port 53 TCP+UDP, RFC 1035 TCP framing)
- Tests: `failover_test.go` (`TestReplaceLoop_*`), `health_test.go`, `netmon_test.go`, `contract_test.go` (4 transport contracts: TCP health gate, server unreachable stop, auth no-retry, timing), `engine_test.go`, `deadlock_test.go` (Stop vs health goroutine), `slog_migration_test.go` (no `log.Printf`)

## Testing

```bash
go test ./engine/...                          # Engine + error classification tests
go test -run TestContract -v ./engine/...     # contract tests only
```
