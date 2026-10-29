# Gateway Layer (gateway/)

k2r platform shell — transparent proxy gateway for LAN traffic via TPROXY. Linux only (`//go:build linux`).

## Architecture

```
LAN device → kernel nft/ipt TPROXY → gatewayProvider (TCP/UDP listeners)
                                          │
                                    ConnectionHandler
                                     (dnsHandler + ClientTunnel)
                                          │
                                    engine → wire → k2s → internet
```

Gateway owns the full lifecycle: HTTP API → sysctl → interceptor → provider → engine.
Lock ordering (all standalone — never nested with engine locks):
- `Gateway.opMu` (serialization) → `Gateway.mu` (state) → `Gateway.sseMu` (state)
- `RouterDeviceManager.mu` (state, RWMutex) — acquired only after `Gateway.mu` is released
- `installGate.installMu` (serialization, held across `Interceptor.Install`; taken under `Gateway.opMu` by doUp, alone by shutdown, never under `Gateway.mu`) → `nftNetlinkInterceptor.mu` (serialization, acquired after all state locks are released)

## Files

- `gateway.go` — `Gateway` struct, `Run`/`Stop`/`shutdown`/`doUp`/`doDown`, per-engine `EventHandler` (`engineEvents`), auto-reconnect (boot and after an outbound fatal), crash recovery. Wires the k2r-specific `webui.Upgrader` (`newK2rUpgrader`) and `webui.Storage` (rooted at `/etc/k2r/`). `doUp`'s subscription refresh loop wires `sub.SetOnControlKeyHash(g.reconcileControlKey)` plus an immediate reconcile call.
- `api.go` — HTTP API: `/api/core` (up/down/status/set-credential/reset/version) with control-key auth (loopback exempt, TOFU open for `set-credential` only while unbound), `/ping` discovery signature (raw JSON, no auth), `/api/log-level`, router device handlers. `defaultConfigPath()` = `stateDir()/k2r.yml` (production value unchanged: `/etc/k2r/k2r.yml`). `/api/platform` and `/api/upgrade` are registered directly via `webui.New*Handler` in `gateway.go`; `/` serves the embedded webapp when built in, otherwise `handleRootNotice` (see `notice.go`). `/api/storage` retired (headless) — `Gateway.storage` is retained internally, feeding only `RouterDeviceManager` persistence, not exposed over HTTP.
- `auth.go` — control-key bearer auth: `currentControlKeyHash`/`setControlKeyHash` (lazy-load from disk, persist outside `g.mu`), `isLoopback` (127.0.0.1/::1 always authorized — the CLI talks over loopback), `authorized` (deny-by-default, `subtle.ConstantTimeCompare` against the stored SHA-256), `writeUnauthorized` (HTTP 401 + `Response` envelope `{code:401,message:"unauthorized"}` — the companion app keys off the status code, not the envelope), `requireAuth` (wraps the whole mux; exempts exactly `/ping`, `/`, `/api/core` — `/api/core` defers to `handleCore`'s per-action TOFU check), `reconcileControlKey(centerHash)` (Center-authoritative adopt-overwrite: empty hash or hash already matching → no-op, else persist + overwrite; logs `DIAG: control-key-adopt`).
- `controlkey.go` — SHA-256 hash persistence at `stateDir()/control-key.json` (dir 0700 / file 0600); the plaintext control key is never written to disk.
- `notice.go` — brand-neutral, self-contained static page served on `/` (404 elsewhere) when `webui.NewWebappHandler` returns nil, i.e. `nowebapp` headless builds.
- `sse.go` — `/api/events` SSE endpoint, `broadcastSSE` fan-out
- `config.go` — `GatewayConfig` (embeds `ClientConfig`), `LoadGateway`, `resolveExcludeIPs`
- `provider.go` — `gatewayProvider` (implements `provider.Provider`), dual-stack TCP/UDP accept loops
- `listener.go` — TPROXY socket setup (`IP_TRANSPARENT`, `IP_RECVORIGDSTADDR`), `udpFlowTable` + `tproxyUDPConn` (one flow = one LAN client socket talking to one destination = one tunnel session), `readUDPOrigDst`, `replySocketCache` (one `IP_TRANSPARENT` socket bound to each original destination so UDP replies leave with that destination as source; idle 2 min → swept)
- `intercept.go` — `Interceptor` interface, `NewInterceptor` (auto-detect nft vs iptables), `RemoveInterception` (stateless cleanup for shutdown and `k2r service uninstall`), `installGate`, `linkScopeDests`, IP/subnet family splitting
- `intercept_nft.go` — nftables backend (`inet k2r` table, atomic create/delete)
- `intercept_ipt.go` — iptables/ip6tables backend (`K2R` chain in mangle table)
- `sysctl.go` — IPv4/IPv6 forwarding enable/restore
- `lan.go` — `DiscoverLANSubnets` (private RFC 1918 subnets, virtual interface filtering)
- `state.go` — Persisted state (`/etc/k2r/state.json`) for auto-reconnect
- `client.go` — IPC client for `k2r up/down/status` commands

Shared webui pieces (webapp embed, encrypted storage, platform info, OTA upgrader) live in `k2/webui/`. See `k2/webui/` for the cross-binary code that both `cmd/k2r` (gateway) and `cmd/k2` (daemon, Linux only) consume.

## Interceptor Backends

| Backend | Table/Chain | Atomic cleanup | IPv6 |
|---------|-------------|----------------|------|
| nftables | `inet k2r` (single table) | `nft delete table inet k2r` | Full (ip6 rules in same table) |
| iptables | `K2R` chain in mangle | Flush + delete chain + remove jumps | `ip6tables` parallel chain |

Both backends: fwmark 1 → ip rule → table 100 → local loopback delivery.

All three backends return, ahead of the TPROXY rules and after the MAC allowlist, every destination in `linkScopeDests`: `224.0.0.0/4`, `255.255.255.255`, each LAN subnet's directed broadcast, `ff00::/8`. The policy route is set up by `setupIPRules`, which clears before it adds.

## Exclude IPs

`resolveExcludeIPs` builds the exclusion list to prevent interception loops:
1. Server IPs (resolve server hostname)
2. Direct DNS upstream IPs (prevent DNS query loops)
3. Gateway's own interface IPs

## Control API Auth

`requireAuth` wraps the whole mux; `authorized()` is deny-by-default (no bound key → only loopback passes):

| 请求 | 无绑定 key | 有绑定 key |
|---|---|---|
| `/ping`、`/` | 放行 | 放行 |
| loopback 来源任意端点 | 放行 | 放行 |
| `/api/core` set-credential | 放行（TOFU） | 要求 Bearer |
| `/api/core` 其他 action | 401 | 要求 Bearer |
| 其他端点（LAN 来源） | 401 | 要求 Bearer |

Control-key authority: Center is the single source of truth. It ships `control_key_hash` (snake_case, cross-repo contract) on every `/api/subs` fetch; `reconcileControlKey` adopt-overwrites the local hash whenever Center's differs (covers first-setup TOFU convergence, legacy adoption, and rotation) — a provisional hash set via `set-credential {controlKey}` is only ever a bridge until the first successful subs fetch.

## Config

```yaml
server: "k2v5://udid:token@host:443/?ech=..."
listen_port: 12345        # TPROXY port (default 12345)
lan_subnets:              # Auto-discovered if omitted
  - "192.168.1.0/24"
dns:
  direct: ["223.5.5.5:53"]
  proxy: ["8.8.8.8:53"]
rule:
  global: false
log:
  level: info
```

## Gotchas

- nft `inet` family requires `tproxy ip to` / `tproxy ip6 to` — bare `tproxy to` causes "conflicting protocols"
- Docker containers have read-only `/proc/sys/` — sysctl skips write when value already "1"
- `gatewayProvider.Close()` does NOT remove interceptor rules — `Gateway` owns interceptor lifecycle
- **TPROXY'd UDP is grouped into flows keyed by (source ip:port, destination ip:port)** (`udpFlowTable`); the read loop never calls `HandleUDP` per packet. A session per packet gives every packet of an exchange its own egress port (the server binds one socket per session) and leaves one 60 s session behind per packet: on OpenWrt a modest 20 flows × 50 pps filled all 4096 tunnel slots, stalled TCP and UDP for everyone, and got k2r OOM-killed on a 232 MB router (2026-09-29). The key includes the destination on purpose — routing, sniffing and the reply source stay per destination, as they were per packet. Keying by source alone would give full-cone NAT (what the TUN provider does) but routes a whole socket by its first packet and needs a reply-source policy; not done here.
- `tproxyUDPConn.ReadFrom` returns the packet's **destination** as its address (what `pipePacket` forwards to — same as `singPacketConn` / SOCKS5 `udpSession`), honours the read deadline (that is how an idle flow ends), and `WriteTo` ignores the address it is given: replies always leave from the flow's destination, never from an address the tunnel reports.
- Flow queues are bounded three ways: `udpFlowQueue` packets per flow, `maxQueuedUDPPackets` across all flows, `maxUDPFlows` flows. Over any of them the packet is dropped (first drop per flow / table logged at WARN) — the read loop must never block, every flow shares it.
- **UDP replies must be written from a socket bound to the original destination** (`replySocketCache` / `transparentBind`), never from the shared `0.0.0.0:ListenPort` listener. A reply from `<router>:12345` still reaches an unconnected client socket, but every `connect()`ed one (NTP, QUIC, games) drops it in the kernel — the Docker UAT never sees this because LinuxKit has no UDP TPROXY (`test/k2r-uat/run.sh` "UDP TPROXY may not work"). Verify on the testlab OpenWrt VM with a `connect()`ed UDP client (2026-09-25).
- **Port 53 is exempt from the TPROXY chain whenever DNSRedirect is on** (all three backends emit a `dport 53 return` ahead of the tproxy rules). Without it the query hits both the mangle TPROXY and the nat REDIRECT on a real router: it is rewritten to `<router>:DNSPort`, then tunneled to that private address — LAN `dig @8.8.8.8` times out (UDP) / resets (TCP), and the bogus flows trip `quicUDPMux`'s datagram-relay health probe into smux fallback.
- **The daemon removes its rules when it is stopped** (`cmd/k2r` turns SIGTERM / SIGINT into `Gateway.Stop` → `shutdown`). Go's default for SIGTERM is to exit on the spot, and until 2026-09-30 nothing caught it: after `/etc/init.d/k2r stop` or `k2r service uninstall` the `inet k2r` table stayed, and with it the DNS redirect to a port nobody listened on — no name resolved for the LAN or the router itself until a reboot (OpenWrt 25.12, nft backend). `shutdown` closes `installGate`, runs `RemoveInterception` (by name, so it needs neither `opMu` nor `g.interceptor` and also clears what a crashed predecessor left), and only then waits — at most `shutdownGrace` (3 s, under procd's 5 s SIGKILL) — for the tunnel teardown, which queues behind an in-flight `doUp` (a transport race holds `opMu` ~5 s). `state.json` is untouched, so the next start reconnects. `k2r service uninstall` runs `RemoveInterception` too, for rules from a crash or an older build.
- **Restart is not stop.** A `kill -9` / crash leaves the rules in place on purpose (`startupRecovery`), and the daemon's own OTA restart (`restartService`) sets `restartHandoff` so its SIGTERM does the same: the successor's Install replaces them back-to-back. The flag outlives the restart command by `restartHandoffLinger`: systemd signals the unit's whole cgroup, so `systemctl restart` — a child of the daemon — returns because of the very SIGTERM the daemon is about to handle. A manual `/etc/init.d/k2r restart` is a stop plus a start — for the ~4 s until the new daemon connects the LAN is plainly direct, DNS included. Do not read "rules left behind" as fail-closed on nft: `tproxy` without a listening socket is a no-op there, TCP/UDP pass unproxied and only DNS fails; it is fail-closed on iptables only.
- **`ip rule add` does not dedupe.** Every Install that was not preceded by a Remove (killed daemon) used to stack one more `fwmark 0x1 lookup 100` rule, and Remove deleted one. `setupIPRules` now clears first and `cleanupIPRules` deletes until nothing is left. The nft shell backend had the same gap one level up — its script only ever `add`ed, so after a killed daemon every rule was in the table twice; it now starts with add / delete / add of the table.
- **Link-scope destinations are never TPROXY'd** (`linkScopeDests`): multicast, limited broadcast, directed broadcast. A LAN host's NetBIOS / discovery broadcasts each opened a tunnel session that ended as a relay timeout — on a quiet LAN three of those marked the datagram relay unhealthy within 64 s and new UDP moved to TCP-WS — and the TPROXY socket took the packet away from whatever on the router was meant to get it (measured: dnsmasq received 0 of 3 broadcast DHCPINFORMs from a LAN client while k2r was connected, 3 of 3 with the exemption). Multicast only reaches the hook for groups the router itself joined on the LAN bridge (umdns, miniupnpd); broadcast always does. In the netlink backend these rules carry an explicit `meta nfproto` match: a raw network-header offset in an `inet` table is read from v4 and v6 packets alike, and without it the IPv4 multicast rule would match on bytes of an IPv6 source address.
- **Auto-reconnect yields to the user.** The loop keeps the config it read from `state.json` at daemon start and retries it forever; `doUp` begins by tearing down whatever tunnel is up. So a user who connected by hand while the saved config was failing (`k2r setup` with a new URL does exactly this) lost that tunnel on the next retry and was left disconnected with the rules gone. Each attempt (`autoReconnectUp`) therefore first checks, in the same `opMu` hold as its doUp, that its job still exists: no other doUp has **succeeded** since the loop began (`Gateway.ups`), `state.json` still says connected (`k2r down` / reset end it), and no newer loop exists (`reconnectGen`). A user's up that *fails* deliberately does not stop the loop — the router then has no tunnel and nothing else that would bring one back. The attempt connects with the session `state.json` holds at that moment, not the one the loop saw when it started: set-credential saves the new credential before its own connect, so after that connect fails the loop comes back with the new one.
- **A tunnel whose engine gave up is brought back by the gateway.** When the engine runs out of servers to replace a dead outbound with, it fires `OnOutboundFatal` and shuts itself down (`Engine.fail`) — it does not reconnect, and the rules it served stay behind on a dead listener (nft: data direct, DNS dead). Until 2026-09-30 the gateway only logged that: a node unreachable for ~15 s on an idle router ended the tunnel until someone ran `k2r up`. Now each engine gets its own handler (`engineEvents`), whose fatal starts `reconnectAfterFatal` — the boot-time loop and schedule (3 s, 15 s, 30 s … 300 s), reading `state.json`, so a `k2r down` in between still wins. The handler carries its engine because `Engine.Stop` does not join the replace loop: the first attempt refuses to run over a *different* live engine. **Posture while it retries: not connected = direct.** The first attempt's `closeTunnel` removes the rules, exactly as at boot before the first connect or after a failed `k2r up`; until an attempt succeeds the LAN, DNS included, goes out unproxied, and once the node is back that lasts up to one backoff step (≤ 300 s). k2r has no kill switch; keeping the dead tunnel's rules instead would also redirect the router's own DNS to a listener that is gone. A config that mixes a `k2subs://` route with a plain `k2v5://` one takes this path when the plain outbound dies (the manager is asked and has no session for it).
- **A plain `k2v5://` route gets no `OutboundProvider`** (`outboundProviderFor`). The subscription manager has no session for it and answers `ErrNoSubscription`, which the engine treats as "every server failed" — the fatal above, reported as code 402 because the message contains "subscription". Without a provider the engine re-races the same server until it answers (the mobile behaviour); the rules stay up throughout, so the LAN is fail-closed during the outage and back once a re-race lands after the node answers (a race cycle is 5 s; measured 1 s).
- **No age limit on the saved session.** `state.json`'s timestamp is written when the tunnel comes up, so on a router it measures uptime. The 7-day cutoff inherited from the desktop daemon meant a router that had been connected for more than a week did not reconnect after a power cut. Only `k2r down` (state `disconnected`) stops auto-reconnect.
- IPv6 socket options use raw constants (`IPV6_TRANSPARENT=75`, `IPV6_RECVORIGDSTADDR=74`) — not in Go's syscall package
- Auto-reconnect waits 3s after HTTP server start for network stack readiness

## Testing

```bash
GOOS=linux go vet ./gateway/...     # Static analysis (any OS)
GOOS=linux go build ./gateway/...   # Cross-compile check (any OS)
go test ./gateway/...               # Unit tests (Linux only — //go:build linux)
make gateway-check                  # All above (unit tests on Linux, cross-compile on macOS)
make gateway-uat                    # Docker integration: 10 phases, 56 test points
```

Unit tests cover: nft/ipt rule generation (dual-stack, multi-subnet, exclude sets, TPROXY syntax, link-scope exemption), IP/subnet family splitting, config loading/defaults, server URL parsing, virtual interface filtering, state JSON roundtrip, provider close idempotency, UDP flow table (one session per flow, close/dispatch races, queue budget), reply source, shutdown (rules first, gate, grace, restart handoff), auto-reconnect standing down, reconnect after an outbound fatal (dead engine replaced, late event ignored, newest loop wins), provider choice, saved-session age.

Two tests load a real ruleset into the kernel inside a throwaway network namespace (`intercept_nft_netlink_kernel_test.go`: chain order of the netlink backend, the shell backend over a stale table). They need root and **skip without it** — a non-root run is green without having run them.

What only a real router shows — procd's stop/restart/respawn, the nft backend's behaviour with no listener, busybox `ip` — is checked on the testlab OpenWrt VM: see `test/CLAUDE.md` "k2r lifecycle checks".

`go test ./...` on macOS skips this whole package and `lint/` is a separate package — run both `GOOS=linux go vet ./gateway/` and `go test ./lint/` before committing gateway code (a `Close()` under `mu` shipped to main on 2026-09-25 because neither was run). UDP through real TPROXY is covered by `test/k2r-udp-lab/run.sh` (native Linux, root), not by the Docker UAT.
