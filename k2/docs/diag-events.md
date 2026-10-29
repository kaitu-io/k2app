# k2 DIAG Event Registry

Every `"DIAG: <name>"` log message in k2's non-test Go code has exactly one row here, and every row names an event that exists in code. **`k2/lint/diag_events_test.go` enforces both directions** (`cd k2 && go test ./lint/ -run TestDiagEvents`). Rules for emitting DIAG logs (three layers, threshold gating, INFO vs WARN) live in `k2/CLAUDE.md` § Diagnostic Logging.

Row format the lint parses: a table row whose first cell is the bare event name in backticks — `` | `event-name` | ... ``. Removed events go in the bullet list at the bottom, never in a table.

Level column = the slog level at the call site(s). "Where" = file(s) relative to `k2/`.

## Layer 1 — heartbeat fields

`DIAG: heartbeat` is emitted every 30s from `healthMonitor.sample()` (`engine/health.go`), never at 1Hz.

- Required fields: `health`, `transport`, `loss` (smoothed), `rttMs`, `minRttMs`, `txMB`, `rxMB`, `tcpConns`, `udpConns`, `dialOk`, `dialFail`, `directOk`, `directFail`, `udpTxDrops`, `udpRxDrops`, `uptimeS`, `fallback`, `activeChannel`, `scoreQuic`, `scoreTcpws`, `heapMB`, `sysMB`, `goroutines`; plus the uplink CC window fields (`ccMis ccAppLimited ccFloor ccLossP90 ccRttP90 ccBwEst ccBgLoss ccMode`, see `engine/CLAUDE.md`).
- `udpTxDrops`/`udpRxDrops` are **cumulative since connect**, not per-interval — diff two heartbeats to get a rate.
- `activeChannel`/`scoreQuic`/`scoreTcpws` are the primary outbound's dual-channel scoreboard (`wire.TransportManager`, `wire/CLAUDE.md` transport.go). `transport`/`fallback` say which channel serves traffic *now*; these three add the *why*.
- **The four `dial*` fields are per-window deltas, not cumulative** — the opposite of `udpTxDrops`, deliberately: they answer "is anything working *right now*". `dialOk`/`dialFail` = proxied dials, `directOk`/`directFail` = direct ones; split because a direct-path fault (socket bound to the wrong interface, #3303) and a proxy-path fault (node cannot egress, #3463) hide behind each other when summed. **`dialFail` high with `dialOk` at 0 is the signature no other field shows**: `loss` stays 0.000 and `health` stays `healthy` when every request fails at the proxy layer. All zeros = idle, not healthy.
- Human-readable sizes (MB) and durations (ms/s).

## Engine / health (client)

| Event | Level | Where | Meaning / fields |
|-------|-------|-------|------------------|
| `heartbeat` | INFO | engine/health.go | Layer 1, every 30s — see field list above |
| `app-routes` | INFO | engine/health.go | With each heartbeat, only when a loaded rule asked for process attribution AND ≥1 connection was attributed in the window. `processes` (distinct count), `apps` — per process, busiest first, max 12 then `+N more`: `<name>: direct=N proxy=N [reject=N] fail=N [lastFail=direct/<dest> or proxy/<dest>]`. Window counts. Answers "the app I set to direct still doesn't work" (`core/app_stats.go`) |
| `wake` | WARN | engine/health.go | Wall-clock jump > 5s (`wakeDiagThreshold`); ≥30s also triggers `doWakeReconnect`. sleepS, health, transport, uptimeS |
| `echo-probe-ok` | DEBUG | wire/echo.go, wire/race.go | Echo succeeded. transport, rttMs |
| `echo-probe-fail` | WARN | engine/health.go, wire/race.go | Echo failed. transport, err |
| `echo-probe-unsupported` | WARN | engine/health.go, wire/race.go | Server doesn't support echo (old k2v5); rate-limited once per TM. transport |
| `transport-rerace` | WARN | engine/engine.go | Runtime re-race after 3 consecutive echo fails. reason, previousTransport |
| `connected` | INFO | engine/engine.go | Engine connected. server, mode, disableSniff, dialMs, dnsTransport |
| `session-end` | INFO | engine/engine.go | Engine stop (only if a session was up). uptimeS, txMB, rxMB |
| `wire-error` | WARN | engine/engine.go | Classified wire error reported to engine. code, category, message |
| `rule-bundle-missing-region` | WARN | engine/engine.go | A configured `match.region` route has no usable bundle — `engine.Start` fails loud (504) instead of silently degrading to all-proxy. regions, cacheDir |
| `rule-miss-drop` | WARN | engine/miss_reporter.go | Rule-miss telemetry queue full; logged every `missDropLogEvery` (256) drops. totalDrops |
| `rule-miss-stop-timeout` | WARN | engine/miss_reporter.go | Rule-miss reporter drain did not finish within 5s on Stop |
| `rule-miss-upload-fail` | WARN | engine/miss_reporter.go | Rule-miss batch upload failed. reason, records, err |
| `rule-miss-upload-ok` | DEBUG | engine/miss_reporter.go | Rule-miss batch uploaded. reason, records |
| `dns-handler-panic` | WARN | engine/dns_handler.go | Panic in the port-53 handler recovered (UDP or TCP path); client gets SERVFAIL instead of the NE/VPN process dying. transport, panic, stack |
| `dns-client-watch` | INFO / WARN | engine/dns_handler.go, provider/dnsclient_etw_windows.go | Windows TUN mode, only when a loaded rule asked for process attribution. state: `started` (INFO); `unavailable` + err (WARN — no ETW session, system-resolver DNS stays attributed to svchost); `slow` / `recovered` + lagMs (WARN — events arrive later than a lookup waits) |
| `dns-client-watch-ended` | WARN | provider/dnsclient_etw_windows.go | The ETW `ProcessTrace` loop returned an error — DNS-client attribution stopped. err |

## Wire — client transport

| Event | Level | Where | Meaning / fields |
|-------|-------|-------|------------------|
| `transport-race-start` | INFO | wire/race.go | Race begins. candidates |
| `transport-race-winner` | INFO | wire/race.go | Race complete. winner, winnerMs, tried |
| `transport-race-fail` | WARN | wire/race.go | All candidates failed. quic443Err, tcpwsErr |
| `ech-block-suspected` | WARN | wire/echblock.go | All ECH transports failed AND a no-ECH TLS to the same IP completed (fires off race-fail, background, gated out when a candidate reached auth). host, sni, hint |
| `quic-handshake-fail` | WARN | wire/quic.go | QUIC handshake error or `quicDialHardTimeout` (15s) hit. addr, consecutiveFails, err |
| `quic-dial-orphan-cleanup` | WARN | wire/quic.go | An orphaned `transport.Dial` goroutine (left behind by the hard timeout) resolved. addr, blockedMs |
| `transport-switch` | INFO / WARN | wire/transport.go | QUIC↔TCP-WS transition. from, to, reason. Reasons (see `wire/CLAUDE.md` transport.go): `quic-quality-degraded` / `quic-handshake-threshold` / `quic-datapath-timeout` switch-away, `quic-recovered` echo-verified switch-back, `quic-race-lost` tcpws won the initial race, `flap-damped` — **not a transition** (`from`==`to`=="tcpws"), a rollback blocked purely by the flap-damping window |
| `quic-datapath-timeout` | WARN | wire/transport.go | quic channel enters data-path distress: within `dataPathWindow` (60s) ≥ `quicRespTimeoutThreshold` (3) `DialTCP`s opened a stream, wrote the header and hit the 5s response deadline, AND those are ≥ 1/3 of the window's quic requests. Once per rising edge (evaluated on each quic dial and each selector evaluation). Client signature of a UDP path that passes handshakes and echoes but drops bulk (#3747/#3757); heartbeat `loss` can read 0.000 throughout. Followed by `transport-switch reason=quic-datapath-timeout` once tcpws has two fresh samples ≥ 0.3. timeouts, requests, windowS |
| `channel-review` | INFO | wire/transport.go | Rollback-review evaluation while tcpws is active, at most once per review window (plus one unconditional log on the actual rollback). activeChannel, scoreQuic, scoreTcpws, flapCount, failedTrials (1 = review escalated to 600s, ≥2 = sticky 1h), nextReviewInS, decision (`stay` \| `switch-to-quic` — never `switch-to-tcpws`; switch-away shows as `transport-switch`) |
| `datagram-readloop-exit` | WARN | wire/quic.go | Client `quicUDPMux` datagram read loop terminated. totalReceived, err |
| `udp-mux-rebuild` | WARN | wire/quic.go, wire/tcpws.go | A dead UDP relay mux was evicted from the client cache and will be rebuilt. transport, reason (`write-fail`/`peer-closed`/`closed`), rebuilds (cumulative) |
| `stream-killed-on-write-fail` | WARN | wire/quic.go, wire/tcpws.go | Per-Write deadline (5s) exceeded or write error on a UDP relay mux stream; stream killed. transport, side, sessionID, len, writeMs, err |
| `stream-writer-slow` | WARN | wire/quic.go | quic stream-UDP frame write took > 100ms (but succeeded). transport, sessionID, len, writeMs |
| `stream-mux-no-session` | WARN | wire/quic.go | quic stream-UDP frame for an unknown session — dropped, counted in `UDPRxDropsNoSession`. sessionID, len |
| `stream-mux-drop` | WARN | wire/quic.go | quic stream-UDP session channel full — frame dropped, counted in `UDPRxDrops`. sessionID, len, chanLen |
| `stream-mux-recv` | DEBUG | wire/quic.go | quic stream-UDP frame delivered to its session. sessionID, len |
| `relay-fetch` | INFO | wire/relay.go, wire/relay_manager.go | Antiblock control-plane relay response. centerHost, method, path, status, nodeIP |
| `relay-fetch-fail` | WARN | wire/relay.go, wire/relay_manager.go | Relay dial / round-trip error. centerHost, path, nodeIP, err |

## Wire / server — node side (k2s)

| Event | Level | Where | Meaning / fields |
|-------|-------|-------|------------------|
| `cc-summary` | INFO | wire/cc_summary.go | Server-side K2CC window per QUIC connection every `ccSummaryInterval` (60s) plus a `final=true` line at close; skipped when the window completed no MI. Download-direction CC lives here (client heartbeat carries uplink only) |
| `datagram-recv-exit` | WARN | wire/quic.go | `QUICServer.handleDatagrams` receive loop exited. totalReceived, err |
| `udp-deliver-drop` | DEBUG | wire/quic.go, wire/tcpws.go | Server session readCh full — handler stuck/slow. transport, sessionID, addr, len |
| `tcpws-stream-cap` | WARN | wire/tcpws.go | A TCP-WS session hit `tcpwsMaxStreams`; further streams refused. Logged once per session. max |
| `auth-center-unreachable` | WARN | wire/session.go (also Slack text in wire/alert.go) | Revalidation undecidable (Center down/timeout) — session kept, authAt advanced one window. udid, code, graceS |
| `auth-node-misconfigured` | WARN | wire/auth_remote.go (also Slack text in wire/alert.go) | THIS node cannot authenticate anyone (no `K2_NODE_SECRET`, unresolvable node IPv4) — Center is never asked. reason, udid |
| `auth-session-expired` | INFO | wire/session.go | SvcExp passed on a live session — killed with zero Center calls. udid, svcExp |
| `auth-session-revoked` | WARN | wire/session.go | Center explicit reject (401/402/403) on revalidation — session killed. udid, code |
| `auth-grace-exhausted` | WARN | wire/session.go | Undecidable streak exceeded cookieGraceMax (6h) — session killed. udid, graceS |
| `auth-rollout` | INFO | wire/rollout_stats.go | Every 5 min summary (never per-connection); window deltas, emits even when total=0. total, authed, cookie_hit, cold, unauthed, legacy_no_metadata |
| `control-plane-relay` | INFO | server/control_plane.go | Node relayed a control-plane request to Center. origin, method, path, status, bytes |
| `control-plane-relay-fail` | WARN | server/control_plane.go | Node relay error. origin, path, err |
| `dns-cache-hit` | INFO | server/handler.go | `ProxyHandler.handleSingleDNSQuery` (client proxy-DNS over the tunnel) served from the node cache. domain, qtype |
| `dns-cache-miss` | INFO | server/handler.go | Same path, cache miss → upstream query. domain, qtype |
| `dns-upstream-ok` | INFO | server/handler.go | Upstream answered. domain, qtype, rcode, rttMs |
| `dns-upstream-slow` | WARN | server/handler.go | Upstream answered in > 500ms. domain, qtype, rttMs |
| `dns-upstream-fail` | WARN | server/handler.go | Upstream query failed (client will retry). domain, qtype, rttMs, err |
| `dns-response-pack-fail` | WARN | server/handler.go | Upstream response could not be packed. domain, err |
| `dns-stream-write-fail` | WARN | server/handler.go | Writing the DNS answer back onto the stream failed. err, len |
| `dns-servers-override` | INFO | server/handler.go | `K2_DNS_SERVERS` env replaced the default resolver list. servers |
| `dns-resolve-cache-hit` | INFO | server/resolver.go | Node-side hostname resolve (A records) served from cache. host, ips (count) |
| `dns-resolve-ok` | INFO | server/resolver.go | Node-side hostname resolve succeeded. host, ips |
| `dns-resolve-fail` | WARN | server/resolver.go | Node-side hostname resolve failed. host, err |
| `usage-api-reject-nonloopback` | WARN | server/usage_api.go | Usage API request from a non-loopback address rejected with 403. remote, path |
| `usage-reporter-start` | INFO | server/usage_reporter.go | Legacy node usage reporter loop started (gated on `NodeCutoffReport`). center, ipv4 |
| `usage-reporter-stop` | INFO | server/usage_reporter.go | Usage reporter loop stopped |
| `usage-reporter-cycle-fail` | WARN | server/usage_reporter.go | A report cycle failed; retried after backoff. epoch, total, retryIn, err |
| `usage-reporter-offline-cutoff` | WARN | server/usage_reporter.go | Center unreachable and local total ≥ last-seen hard ceiling — node stops accepting locally. total, ceiling |
| `usage-reporter-epoch-change` | INFO | server/usage_reporter.go | Center returned a new epoch — counters reset. from, to |
| `usage-reporter-verdict-change` | INFO | server/usage_reporter.go | Center verdict changed (`serve` = accepting). from, to, epoch, total |
| `device-traffic-reporter-start` | INFO | server/device_traffic_reporter.go | Per-device traffic reporter loop started. center, ipv4, boot |
| `device-traffic-reporter-stop` | INFO | server/device_traffic_reporter.go | Per-device traffic reporter loop stopped |
| `device-traffic-cycle-fail` | WARN | server/device_traffic_reporter.go | A per-device traffic report cycle failed; next attempt after max backoff. seq, err |

## DNS — client (core/dns)

| Event | Level | Where | Meaning / fields |
|-------|-------|-------|------------------|
| `dns-slow` | INFO | core/dns/middleware.go | Upstream latency > 500ms. domain, upstream, latencyMs |
| `dns-fail` | WARN | core/dns/middleware.go | Upstream error (proxy path never falls back to direct). domain, upstream, err |
| `dns-view-override` | INFO | core/dns/middleware.go | Actual upstream query with a per-process-flipped resolver view (cache hits never log). domain, process, package, view |
| `dns-proxy-timeout` | WARN | core/dns/proxy.go | Proxy DNS 5s timeout. server, domain, connAgeS, isNewConn |
| `dns-proxy-write-fail` | WARN | core/dns/proxy.go | WriteTo failed. server, domain, connAgeS, isNewConn, err |
| `dns-proxy-conn-dead` | WARN | core/dns/proxy.go | Recv loop died while a query waited. server, domain, connAgeS, isNewConn, err |
| `dns-proxy-dial-fail` | WARN | core/dns/proxy.go | DialUDP failed. server, domain, err |
| `dns-proxy-recv-no-callback` | WARN | core/dns/proxy.go | DNS response arrived after its callback expired. server, queryID, domain, numCallbacks |
| `dns-proxy-recv-malformed` | WARN | core/dns/proxy.go | Response failed to unpack — skipped. server, len |
| `dns-proxy-recv-late` | DEBUG | core/dns/proxy.go | Response matched a callback whose waiter already gave up. server, queryID, domain |
| `dns-proxy-recv-ok` | DEBUG | core/dns/proxy.go | Response delivered to its waiter. server, queryID, domain |
| `dns-proxy-recvloop-exit` | DEBUG | core/dns/proxy.go | Pooled proxy-DNS connection's recv loop exited (conn removed). server, err |

## Core / provider (client data plane)

| Event | Level | Where | Meaning / fields |
|-------|-------|-------|------------------|
| `proxy-dial-fail` | WARN | core/outbound_wire.go | Proxied dial failed. dest, transport, outbound, err |
| `proxy-dial-slow` | INFO | core/outbound_wire.go | Proxied dial latency > 3s. dest, transport, outbound, latencyMs |
| `pipe-watchdog` | WARN | core/tunnel.go | pipe() watchdog force-closed a stuck half-closed pipe. firstExitDir, graceMs |
| `process-lookup-slow` | INFO | provider/process_darwin.go, process_linux.go, process_windows.go, process_owner.go | Process attribution lookup > 100ms. platform, network, latencyMs |
| `process-pcb-fallback` | WARN | provider/process_darwin.go | Once at first lookup, only when the Darwin kernel-PCB self-check fails — searcher runs on the legacy lsof snapshot for the process lifetime (expect never; means a macOS ABI change or exotic sandbox). reason |
| `wintun-create-fail` | WARN | provider/tun_create_diag.go | Windows, once per failed `tun.New`. Wintun's own failure reason goes only to its logger (sing-tun never registers one) and wintun deletes the device before `tun.New` returns, so `provider/tun_create_diag*.go` samples devices every 500ms while creating. "The device is not ready for use." = wintun's hard-coded 15s `WaitForInterface` timed out (#3873). `adapter=not-seen` = failed before the device existed (driver install / SwDeviceCreate). elapsedMs, err, osBuild, adapter (`k2tun present= status=DN_* problem=<CM_PROB = Device Manager "Code N"> problemStatus=<NTSTATUS> id= seenAtMs=`), otherWintun (max 8), samples, sampleErr |

## Rule engine (rule/)

Pin/LRU semantics behind the `rule-tmp-*` events: `rule/CLAUDE.md` "tmp Pin Cache".

| Event | Level | Where | Meaning / fields |
|-------|-------|-------|------------------|
| `rule-tmp-pin` | INFO | rule/engine.go | New IP pinned to the tmp rule, or target changed for an already-pinned IP (duplicates suppressed). At capacity each new pin evicts the least-recently-pinned entry, reported as `evictedIP`. ip, target, action (`add`/`change`), prevTarget (change only), tmpSize, evictedIP |
| `rule-tmp-lru-desync` | WARN | rule/engine.go | tmp cache at `tmpRuleCap` but eviction found nothing — tmp/tmpOrder/tmpIndex out of lockstep. Unreachable on a correct write path. tmpSize, cap |
| `rule-tmp-reload` | INFO | rule/engine.go | Hot reload swapped in a changed on-disk rule set (skipped when unchanged). DNS-learned pins are **kept** since #3889 (field was `cleared` before). pinsKept, routes, bundles |
| `rule-bundle-empty` | WARN | rule/ensure.go | Downloaded tarball extracted 0 .krs (stale mirror / format skew) — rejected to avoid poisoning the cache. cacheDir, reason |
| `rule-manifest-unreachable` | WARN | rule/ensure.go | Every manifest mirror failed — falls back to the legacy blind download (24h backstop honored first). err, dir |
| `rule-manifest-bad-version` | WARN | rule/ensure.go | Remote manifest version not RFC3339 — parses to zero time, so OTA rule updates are frozen until the generator is fixed (local bundles keep serving). version, err |
| `rule-pin-fallback` | WARN | rule/ensure.go | The manifest's winning mirror failed the tarball fetch — racing the remaining mirrors. pinned, fallbacks |

## Subscriptions / platform shells (daemon, k2r gateway)

| Event | Level | Where | Meaning / fields |
|-------|-------|-------|------------------|
| `subs-refresh-fail` | WARN | config/subscription.go | Subscription background refresh failed. endpoint, err |
| `subs-creds-rotate` | INFO | config/subscription.go | Subscription fetch adopted rotated credentials. endpoint |
| `subs-stale-cache` | WARN | subscription/resolve.go | Subs fetch failed, serving disk cache. via, cacheAgeS, err |
| `subs-creds-persist` | INFO | daemon/daemon.go, gateway/api.go | Rotated subscription credential persisted. component |
| `subs-creds-persist-miss` | WARN | daemon/daemon.go, gateway/api.go | Rotation callback fired but no route in lastConfig matched the old URL — nothing persisted. component |
| `control-key-adopt` | INFO | gateway/auth.go | k2r adopted the control-key hash from Center subs. source |
| `gw-lan-discover` | INFO | gateway/gateway.go | LAN subnets resolved for interception. subnets, ifaceCount |
| `gw-intercept-install` | INFO | gateway/intercept_nft.go, intercept_nft_netlink.go, intercept_ipt.go | Interception rules installed. backend, subnets, excludeCount |
| `gw-sysctl` | INFO | gateway/sysctl.go | IP forwarding sysctl written. key, oldValue, newValue |
| `multi-slot-mode` | INFO | gateway/gateway.go | Gateway up with per-slot tunnel bindings. slots |
| `slot-binding-invalid` | WARN | gateway/slots.go | A slot binding from the manifest was dropped (slot stays fail-closed via catch-all). reason, slot, idx |
| `slot-bindings-changed` | INFO | gateway/gateway.go | Slot bindings changed while running — tunnel re-applied. slots |
| `slot-reapply-load-failed` | WARN | gateway/gateway.go | Re-apply after a binding change found no persisted config. err |
| `slot-reapply-failed` | WARN | gateway/gateway.go | Re-apply `doUp` failed. err |
| `slot-local-save-failed` | WARN | gateway/api.go | Persisting a slot take-over to local state failed. err |
| `wifi-convergence-failed` | WARN | gateway/gateway.go | Applying per-slot Wi-Fi config failed (tunnel stays up). err |
| `uci-unavailable` | WARN | gateway/uci_linux.go | No `uci` binary — Wi-Fi convergence skipped (message text continues `, skipping wifi convergence`) |
| `uci-cmd-failed` | WARN | gateway/uci_linux.go | A `uci` convergence command failed (best-effort, continues). cmd, err, out |

## Web OTA (webui/, k2r + Linux desktop)

| Event | Level | Where | Meaning / fields |
|-------|-------|-------|------------------|
| `webota-applied` | INFO | webui/webota.go | Web OTA bundle applied (pending→current swap), once per apply. from, to |
| `webota-fail` | WARN | webui/webota.go | Web OTA poll cycle failed (fetch/gate/download/verify/swap), at most once per poll interval (30min). err |
| `webota-boot-ok` | INFO | webui/webota_boot.go | A newly applied web version booted successfully — boot marker cleared. version, serves |
| `webota-boot-fail` | WARN | webui/webota_boot.go | A newly applied web version is judged a failed boot — rollback follows. version, serves, ageS |
| `webota-rolled-back` | WARN | webui/webota_boot.go | Failed web version quarantined and rolled back. version |

## Darwin NE (appext/, iOS)

| Event | Level | Where | Meaning / fields |
|-------|-------|-------|------------------|
| `ne-iface-resolved` | INFO / WARN | appext/ne_platform_darwin.go | NE default-interface probe resolved on a change; WARN with `result=nil` on the no-route path (a `utun` name means direct sockets bind to the TUN — the "国内直连全挂" loop). name, index, isTun, flags (or result=nil) |
| `direct-bind` | INFO / WARN | appext/ne_platform_darwin.go | Interface a Darwin NE direct socket was actually bound to; on identity change, re-emitted at most every 30s while pathological; WARN when `utun` or nil. resolved, index, isTun, net |
| `ne-callback-panic` | WARN | appext/ne_platform_darwin.go | Panic in the NE interface-change callback recovered instead of crashing the extension (sing-tun invokes it on an unrecovered timer goroutine). panic, stack |

## k2p / signal

| Event | Level | Where | Meaning / fields |
|-------|-------|-------|------------------|
| `ice-connected` | INFO | k2p/home.go | Home side: ICE connected for an away session. session, method |
| `ice-failed` | WARN | k2p/home.go | Home side: handling an away session failed. session, err |
| `p2p-connected` | INFO | k2p/away.go | Away side: P2P connection to home established. peer, method, dialMs |
| `ws-upgrade-fail` | WARN | signal/handler.go | Signal server WebSocket upgrade failed. udid, role, err |

## Removed events (grep returns nothing on current builds)

- `rule-bundle-stale` — removed 2026-10 (#3889): `engine.Start` no longer fetches bundles (embedded seed when the cache is empty, else whatever is on disk). A failed refresh surfaces as the updater's `rule: updater initial check failed` / `rule: updater cycle failed` WARN.
- `rule-tmp-evict` — eviction rides along as `rule-tmp-pin`'s `evictedIP`.
- `conn-highwater`, `udp-relay-timeout`, `wire-handshake`, `wire-handshake-fail` — listed in older docs, no longer in code. Relay-timeout state now shows as INFO `datagram relay unhealthy, trial session` / `datagram relay path healthy again` (`wire/CLAUDE.md`).

## grep Playbook

```bash
grep "DIAG: heartbeat" k2.log | tail -20            # is it working?
grep "DIAG:" k2.log | grep -v heartbeat             # what went wrong?
grep "DIAG: dns" k2.log                             # DNS layer (dns-proxy-* = which proxy-DNS step failed)
grep "DIAG: quic\|DIAG: transport" k2.log           # transport layer
grep "DIAG: wake" k2.log                            # sleep/wake
grep "DIAG: connected\|DIAG: session-end" k2.log    # session summary
grep "level=WARN" k2.log | grep "DIAG:"             # all WARN diagnostics
```
