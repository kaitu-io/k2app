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

- `gateway.go` — `Gateway` struct, `Run`/`Stop`/`doUp`/`doDown`, `EventHandler` impl, auto-reconnect, crash recovery
- `api.go` — HTTP API: `/api/core` (up/down/status), `/ping`, `/api/log-level`
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
- `service_linux.go` — systemd + OpenWRT procd service management
- `client.go` — IPC client for `k2r ctl` commands

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
