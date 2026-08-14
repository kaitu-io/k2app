# Provider Layer (provider/)

Platform-specific packet I/O. TUN (desktop/mobile) and Proxy (SOCKS5) modes.
Gateway TPROXY mode is in `gateway/` package (k2r binary).

## Interface (provider.go)

```go
type Provider interface {
    Start(ctx context.Context, handler ConnectionHandler) error
    Close() error
    Name() string
}

type ConnectionHandler interface {
    HandleTCP(conn net.Conn, dest string)
    HandleUDP(conn net.PacketConn, dest string)
}
```

Provider calls `handler.HandleTCP/HandleUDP` when connections arrive. `ClientTunnel` implements `ConnectionHandler`.

## Files

- `provider.go` — Provider/ConnectionHandler interfaces, ProviderConfig, MTU constants, defaults
- `stack.go` — sing-tun Handler adapter (converts sing-tun connections → ConnectionHandler)
- `tun_desktop.go` — TUN device for macOS/Linux/Windows (sing-tun, requires root)
- `tun_ios.go` — iOS PacketTunnelProvider (uses existing FD)
- `tun_android.go` — Android VpnService (uses existing FD)
- `mtu_ios.go` / `mtu_android.go` / `mtu_desktop.go` — Platform MTU limits (build-tagged)
- `proxy.go` — SOCKS5/HTTP proxy provider (no root needed, `mode: proxy`)
- `dns_darwin.go` — macOS DNS override via SCDynamicStore (sets TUN IP as DNS for primary service, crash-safe cleanup on daemon restart)
- `dns_windows.go` — Windows DNS crash recovery: marker file (`%ProgramData%\k2\dns-state.json`), `CleanupStaleDNSOverride()` at daemon startup
- `dns_other.go` — no-op stub for Linux/iOS (build tag: `(!darwin && !windows) || ios`)
- `provider_test.go` / `tun_desktop_test.go` — unit tests

## Modes

- **TUN** (default): `sing-tun` creates TUN device, gVisor stack handles IP → L4 conversion. Requires root.
- **Proxy**: SOCKS5/HTTP server on `127.0.0.1:1080`. No root needed. For Docker/headless deployments.
- **Mobile FD**: iOS/Android pass existing TUN file descriptor via `ProviderConfig.FileDescriptor`.

## Config

```go
type ProviderConfig struct {
    Mode              string       // "tun" (default) or "proxy"
    FileDescriptor    int          // 0 = create TUN; >0 = use existing FD (iOS/Android)
    RouteExcludeAddrs []netip.Addr // IPs excluded from TUN routing (direct DNS, wire server)
    MTU               int          // default 1400 (desktop), 4064 (iOS/NE), 9000 (Android)
    NetworkExtension  bool         // Apple NE: cap MTU at 4064
    ProxyListen       string       // proxy: "127.0.0.1:1080"
}
```

**MTU defaults**: `SetDefaults()` picks MTU based on platform build tags (`platformMaxMTU`) and `NetworkExtension` flag. Apple NetworkExtension MTU > 4064 causes kernel perf degradation.

**Important**: `tun.Options.DNSServers` is intentionally NOT set. sing-tun auto-derives `TUN_IP.Next()` as DNS, causing OS to send DNS through TUN where `dnsHandler` intercepts at port 53. `RouteExcludeAddrs` only adds IPs to route exclusion (prevents DirectDNSClient loop).

## Process Attribution (process_*.go)

`ProcessSearcher.FindProcess(network, srcIP, srcPort)` attributes a flow to a
process name for `match.apps` / `process_name` rules. Since 2026-08 it handles
IPv4 **and** IPv6 (plus a v4→v6-table fallback for dual-stack sockets, which
the kernel lists only in the AF_INET6 tables with a `::ffff:` local address).

**Who turns it on**: `engine/engine.go gateProcessSearcher` installs a searcher
only when the compiled ruleset has process criteria
(`rule.Engine.HasProcessCriteria`). No App-Bypass-style rule → no searcher →
zero attribution cost. See `rule/CLAUDE.md` for which `match:` keys count —
note that **`match.region` does**, via the app-glob meta entry it expands to.

### The cost model that bit us

Two facts multiply, and neither is visible from this package alone:

1. `core/tunnel.go routeConn` is the **only** caller and it invokes
   `FindProcess` **exactly once per connection** (the later SNI re-routes reuse
   the `ConnMeta` already built).
2. `processCache` is keyed by `cacheKey(network, srcIP, srcPort)` — and every
   new connection carries a **fresh source port**.

So the cache is structurally incapable of helping the path that matters: a new
connection is always a new key, always a miss. It only absorbs repeated
lookups for the *same* 4-tuple, which the call pattern above never produces.
Treat `processCacheCap` / `processCacheTTL` as a negative-result damper, not as
a hit-rate optimization.

### Platform semantics intentionally differ

- **Windows**: kernel tables via `GetExtended{Tcp,Udp}Table`. UDP treats
  `0.0.0.0` / `::` as wildcard (kernel registers `sendto()` sockets that way);
  exact local-addr match always beats wildcard. Row structs + decoder live in
  `process_wintable.go` — **untagged on purpose** so the literal-offset layout
  tests run on all platforms; keep it free of Windows API imports.
- **Linux**: `/proc/net/{tcp,udp}{,6}` → inode → PID. **No wildcard matching**
  — unconnected `sendto()` UDP sockets show local `0.0.0.0`/`::` and stay
  unattributed. Known gap, don't "fix" it on Windows's behalf without
  understanding the /proc semantics differ.
- **Darwin**: one periodic `lsof` sweep, indexed — see below. There is no
  per-connection lsof call any more, so there is no address selector to get
  wrong (the old `lsofSpec` bracketing quirk is gone with it).

### Darwin: snapshot, not per-connection query

macOS has no /proc; attribution means shelling out to `lsof`, which walks the
whole process/fd table. Measured in production: **100–1226 ms per call**, and
under browser-level concurrency the calls queue behind each other (one 9 s
window accumulated 9.8 s of lookup latency, surfacing as
`DIAG: process-lookup-slow` floods and multi-second TLS handshakes).

`DarwinProcessSearcher` therefore runs **one `lsof -i -P -n -F pcPn` sweep**
and indexes every local socket by `(network, ip, port)` — TTL 5 s,
singleflight-collapsed, async rebuild when stale. One sweep is ~87 entries in
~90 ms on a normal desktop.

**Do not "simplify" this into Linux's rebuild-on-miss policy.** Linux can
afford it (reading /proc is cheap); Darwin cannot. A brand-new connection is by
definition absent from a snapshot taken before it existed, so rebuild-on-miss
degenerates straight back to one lsof per connection. `minRebuildInterval`
(250 ms) is what prevents that, and a test
(`TestDarwinProcessSearcher_MissesAreThrottled`) pins it.

Within the throttle window a miss returns `""` — **fail-open**: the connection
takes the region/default route (the tunnel). Same outcome the old code produced
on lsof timeout, and the safe direction: a missed attribution costs an
App-Bypass *direct* route, never a leak.

### lsof's COMMAND is not `ps`'s comm

Verified on production macOS: Claude Code reports as `claude` under
`ps -o comm=` but as **`2.1.232`** (its version string) in `lsof`. Attribution
matches against lsof's value, while UI app lists may source names elsewhere —
a rule written from what `ps` (or the UI) shows can silently never match. When
adding an app to a bypass list, confirm the name with
`lsof -i -P -n -F pc` rather than `ps`.

## Testing

```bash
go test ./provider/...    # Provider unit tests

# Darwin: exercise the real lsof sweep + parser (opt-in, shells out).
# Discriminator: our own socket MUST be absent (selfPid filter).
K2_REAL_LSOF=1 go test ./provider/ -run TestRealWorld_BuildConnIndex -v
```
