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
- **Darwin**: live kernel PCB table via the netstat sysctls, per lookup —
  Windows-equivalent semantics including exact-beats-wildcard UDP; the lsof
  snapshot survives only as a self-check-gated fallback. See below.
- **Android**: `OwnerProcessSearcher` (`process_owner.go`, untagged — unit
  tests run everywhere) asks the platform via
  `ConnectivityManager.getConnectionOwnerUid` through the
  `ConnectionOwnerResolver` Go→Java callback, then resolves UID→packages via
  `PackageResolver.PackagesForUID`. The /proc path above is structurally dead
  on Android 10+ (SELinux/hidepid hide `/proc/net/tcp*` and foreign
  `/proc/PID/fd`) — it returned empty for every foreign-app socket, which
  silently no-op'd all package rules until 2026-09; it stays wired as the
  fallback for Android ≤9 and destination-less lookups. Attribution is
  **UID-granular**: `FindProcessConn` (the `ConnProcessSearcher` extension —
  the platform API needs the full 4-tuple, so callers pass dst too) returns
  every package sharing the owning UID (Play Store + GMS share one via
  sharedUserId), and `rule.ConnMeta.PackageNames` carries the full set.

### Darwin: live kernel PCB table, lsof snapshot as fallback

Primary path (`process_darwin_pcb.go`): every lookup reads the kernel's live
socket table via the netstat sysctls `net.inet.{tcp,udp}.pcblist_n`, finds the
local (ip, port) half, and names the owner through libproc `proc_name`
(same pbi_name source as lsof's COMMAND — rule strings stay verbatim-
compatible), with a `proc_pidpath`-basename fallback for cross-uid callers
without root. One fetch+scan measures **~230 µs p50** on a loaded desktop, so
there is no snapshot, TTL, or throttle — and that is the point:

**Name AND path.** Desktop searchers implement `ProcessPathSearcher`
(`FindProcessPath` → name + executable path): macOS `proc_pidpath`, Windows
`QueryFullProcessImageName` (lowercased). The path is what `match.app_paths`
matches — "is this process inside that app's bundle / install dir" — because
basenames collide across apps (`chrome_crashpad_handler`, `java`) while
directories do not. Name and path always describe the SAME pid
(`pcbOwnerIdent`). The lsof fallback backend has no path; rules then match by
name only.

**The owner is `so_e_pid` when set, else `so_last_pid`** (`pcbOwnerName`).
System services open sockets *on an app's behalf* and stamp the app in
`so_e_pid` (SOF_DELEGATED; 0 on ordinary sockets). Measured 2026-09:
`com.apple.WebKit.Networking` (every WKWebView page) and `mDNSResponder`
(every native getaddrinfo) delegate — naming `so_last_pid` alone sent a
forced-direct app's embedded web pages and system-resolver DNS through the
tunnel (工单 #3807, 豆包/剪映). A delegator that already exited falls back to
`so_last_pid`; a socket delegated to the daemon itself is skipped like its own
sockets. `nsurlsessiond` background transfers: delegation not yet observed —
don't assume. The lsof fallback path below has no delegation info at all.

**Why the lsof snapshot design was a bug, not just a cost model** (fixed
2026-09, shipped broken only in v0.4.9): `routeConn` attributes a connection
exactly once, at SYN time. A snapshot built before the socket existed can
never contain it, and the 250 ms miss-throttle returned `""` (fail-open → the
tunnel) for every connection landing inside the window. Browser page loads
arrive in waves — main document first, subresources ~100 ms later — so the
follow-up waves fell inside the window systematically and per-app 直连
silently routed through the tunnel (two-wave differential: wave A 3/3 direct,
wave B at +150 ms 6/6 proxied). The PCB path has neither horn: the kernel
table IS current at decision time.

The `xinpcb_n`/`xsocket_n` structs are **packed** (vflag@44, v4 laddr@76,
so_last_pid@68, so_e_pid@72) — offsets are pinned by a live-kernel dump, enforced by
`TestPCBLayout_OwnSocketFields`, and re-proven at runtime by `pcbSelfCheck()`
(attributes one of our own loopback sockets at first use). A failing
self-check logs `DIAG: process-pcb-fallback` and runs the process on the
legacy lsof snapshot below. Wildcard-laddr UDP sockets are attributed with
Windows semantics (exact beats wildcard) — a strict improvement over the lsof
index, which skipped them.

Fallback path (kept verbatim, tests pin it via `attribBackend =
backendLegacy`): **one `lsof -i -P -n -F pcPn` sweep** indexed by
`(network, ip, port)` — TTL 5 s, singleflight-collapsed, miss-rebuilds
throttled to 250 ms (lsof measured 100–1226 ms per call in production; the
throttle prevents rebuild-per-miss from degenerating into one lsof per
connection). Misses inside the throttle window return `""` — fail-open, the
known blind spot above, acceptable only because this path now serves exotic
environments where the sysctl path failed.

### Windows: who is the system resolver asking for (`dnsclient_etw*.go`)

A native Windows app never sends its own DNS packet — getaddrinfo / DnsQuery
hand the lookup to the Dnscache service, so the query that reaches the tunnel
is svchost.exe's, and the socket table has no "on behalf of" field (macOS's
`so_e_pid` has no Windows counterpart). The requesting process is visible in
exactly one place: the DNS client's ETW provider
(`Microsoft-Windows-DNS-Client`), event **3006**, logged by dnsapi.dll
*inside the calling process* — header pid = the app, payload leads with the
query name. Event 3009 (query on the wire) is logged by svchost and is no use.

`WindowsProcessSearcher` implements `DNSClientSearcher`: a private real-time
trace session (`k2-dns-client`) feeds a 3-second `qname → pid` table, and
`engine/dns_handler.go metaForQuery` consults it per query — only when the
flow's socket owner is `svchost.exe`. Things that are not obvious:

- **The event arrives AFTER the query.** Measured on Win11 25H2: the wire
  query is ~1 ms behind the event at the source, but ETW delivers to a
  real-time consumer in batches — a steady **26–30 ms** (max seen 42) with
  the millisecond flush timer (`EVENT_TRACE_USE_MS_FLUSH_TIMER`; flush
  periods of 1 ms and 10 ms measure the same, 100 ms measures ~213 ms, and
  without the flag the unit is seconds). So `FindDNSClient` **waits** — up to
  `dnsClientWait` (60 ms) for a record made after the query arrived, falling
  back to an older live record only for retransmissions. That is ~27 ms added
  to every system-resolver lookup that misses Dnscache's cache, paid only
  while a process rule is loaded. Flows owned by anything other than svchost
  (Chromium's own resolver) return at once.
- **Who pays**: anyone on Windows with process criteria loaded — and
  `match.region` counts (it expands to an app-glob entry), so that is every
  smart-mode user, not just App Bypass users. Measured end to end
  (`Resolve-DnsName`, 15 fresh names, watch off → on): p50 193 → 217 ms on
  Win11 25H2, 192 → 236 ms on Win10 22H2. A and AAAA are held back equally
  and before any connection attempt exists, so Happy Eyeballs is unaffected;
  apps that race their own resolution (Chromium) use their own socket and
  never enter this path.
- **`dnsClientFresh` (500 ms) is not a skew allowance.** The handler serves
  one flow's queries one at a time; when the resolver sends A and AAAA down
  one socket, the second is examined only after the first's upstream round
  trip, and the event they share is already that old. Shrink the window and
  that second query sits out the whole 60 ms for nothing.
- **Slow-delivery guard** (`dnsClientTable.slow`): if the latest event took
  longer than `dnsClientWait` to arrive — a Windows that ignores the
  millisecond flag batches per SECOND — lookups stop waiting until delivery
  is fast again (`DIAG: dns-client-watch state=slow|recovered`). Otherwise
  such a machine would pay 60 ms per lookup and still attribute nothing.
- **Needs administrator** (creating a session). The desktop daemon is SYSTEM.
  Refused → `DIAG: dns-client-watch state=unavailable`, attribution stays
  socket-only.
- **Sessions outlive their process.** The name is fixed so the next start
  stops an orphan left by a crashed daemon (`ERROR_ALREADY_EXISTS` → stop →
  retry). Check with `logman query -ets`.
- **Start and stop are serialized** (`dnsLifeMu`), and holds are counted
  (`StartDNSClientWatch` returns a release func; the engine releases through
  its context). Both matter on reconnect: the old engine's release races the
  new engine's start, both address the session by the same name, and the
  kernel recycles session handles — an unserialized late stop can land on
  the new session.
- The callback recovers from panics: it runs on the trace thread, called
  from C, where a panic would kill the daemon.
- **What it cannot fix**: a lookup Dnscache answers from its cache sends no
  wire query, so the app gets whatever view the earlier asker got (same as
  mDNSResponder on macOS); two processes asking for one name inside the
  window are indistinguishable (latest wins); a name the resolver rewrites
  (suffix search list) does not join.
- Struct layouts are hand-mirrored 64-bit SDK layouts handed to the kernel as
  raw memory — `TestETWLayouts` pins them on every OS;
  `TestRealKernelDNSClientWatch` (Windows, elevated; skips otherwise) proves
  pid, payload and delivery delay against the live provider.

### proc_name / lsof COMMAND is not `ps`'s comm

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
