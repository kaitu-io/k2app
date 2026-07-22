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
Lock ordering: `opMu` (serialization) → `mu` (state) → `sseMu` (state).

## Files

- `gateway.go` — `Gateway` struct, `Run`/`Stop`/`doUp`/`doDown`, `EventHandler` impl, auto-reconnect, crash recovery. Wires the k2r-specific `webui.Upgrader` (`newK2rUpgrader`) and `webui.Storage` (rooted at `/etc/k2r/`). `doUp`'s subscription refresh loop wires `sub.SetOnControlKeyHash(g.reconcileControlKey)` plus an immediate reconcile call.
- `api.go` — HTTP API: `/api/core` (up/down/status/set-credential/reset/version) with control-key auth (loopback exempt, TOFU open for `set-credential` only while unbound), `/ping` discovery signature (raw JSON, no auth), `/api/log-level`, router device handlers. `defaultConfigPath()` = `stateDir()/k2r.yml` (production value unchanged: `/etc/k2r/k2r.yml`). `/api/platform` and `/api/upgrade` are registered directly via `webui.New*Handler` in `gateway.go`; `/` serves the embedded webapp when built in, otherwise `handleRootNotice` (see `notice.go`). `/api/storage` retired (headless) — `Gateway.storage` is retained internally, feeding only `RouterDeviceManager` persistence, not exposed over HTTP.
- `auth.go` — control-key bearer auth: `currentControlKeyHash`/`setControlKeyHash` (lazy-load from disk, persist outside `g.mu`), `isLoopback` (127.0.0.1/::1 always authorized — the CLI talks over loopback), `authorized` (deny-by-default, `subtle.ConstantTimeCompare` against the stored SHA-256), `writeUnauthorized` (HTTP 401 + `Response` envelope `{code:401,message:"unauthorized"}` — the companion app keys off the status code, not the envelope), `requireAuth` (wraps the whole mux; exempts exactly `/ping`, `/`, `/api/core` — `/api/core` defers to `handleCore`'s per-action TOFU check), `reconcileControlKey(centerHash)` (Center-authoritative adopt-overwrite: empty hash or hash already matching → no-op, else persist + overwrite; logs `DIAG: control-key-adopt`).
- `controlkey.go` — SHA-256 hash persistence at `stateDir()/control-key.json` (dir 0700 / file 0600); the plaintext control key is never written to disk.
- `notice.go` — brand-neutral, self-contained static page served on `/` (404 elsewhere) when `webui.NewWebappHandler` returns nil, i.e. `nowebapp` headless builds.
- `sse.go` — `/api/events` SSE endpoint, `broadcastSSE` fan-out
- `config.go` — `GatewayConfig` (embeds `ClientConfig`), `LoadGateway`, `resolveExcludeIPs`
- `provider.go` — `gatewayProvider` (implements `provider.Provider`), dual-stack TCP/UDP accept loops
- `listener.go` — TPROXY socket setup (`IP_TRANSPARENT`, `IP_RECVORIGDSTADDR`), `tproxyUDPConn`, `readUDPOrigDst`
- `intercept.go` — `Interceptor` interface, `NewInterceptor` (auto-detect nft vs iptables), IP/subnet family splitting
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
- `tproxyUDPConn` delivers initial packet exactly once via `atomic.Bool.CompareAndSwap`
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

Unit tests cover: nft/ipt rule generation (dual-stack, multi-subnet, exclude sets, TPROXY syntax), IP/subnet family splitting, config loading/defaults, server URL parsing, virtual interface filtering, state JSON roundtrip, provider close idempotency, tproxyUDPConn read-once semantics.
