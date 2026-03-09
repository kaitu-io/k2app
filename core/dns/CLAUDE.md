# DNS Layer (core/dns/)

13-step middleware pipeline with ~92% filter rate and in-flight query deduplication.

## Pipeline (middleware.go)

Two entry points:
- **L3 (desktop TUN)**: `HandleDNSQuery(ctx, pkt []byte, info packet.Info) error` — writes response back via provider
- **L4 (all modes)**: `HandleQuery(ctx, query []byte) ([]byte, error)` — pure bytes in/out, used by `engine.dnsHandler`

All entry points require `context.Context`. The engine context propagates through the entire pipeline, enabling cancellation on shutdown/reconnect.

Steps: parse → validate → hosts lookup → filter (RFC 6761 + noise) → cache check → route (k2rule) → reject check → in-flight dedup → upstream query (direct or proxy) → cache store → SetTmpRule for learned IPs → respond.

**Key**: Direct DNS uses raw UDP sockets (bypasses TUN). Proxy DNS goes through tunnel.

**In-flight dedup**: `sync.Map` keyed by `domain:qtype`. If another goroutine is already querying the same domain+qtype, waiters share the result instead of issuing duplicate upstream queries. Owner uses defer-based cleanup (sing-box pattern) — guarantees cleanup even on panic. Waiters respect parent context cancellation: on ctx cancel, return stale data if available or ctx.Err() (sing-box pattern — no independent query, since ctx is already cancelled).

## DNS Cache (dns_cache.go)

Uses freelru (`github.com/sagernet/sing/contrab/freelru`) — sharded LRU with per-entry TTL. No explicit `sync.RWMutex`, no cleanup goroutine.

- Positive cache: stored with lifetime = `ttl + staleFor` (supports stale-while-revalidate RFC 8767)
- Negative cache: stored with TTL only (NXDOMAIN never served stale)
- `GetPacked`: returns fresh entries only (checks `entry.expireAt`)
- `GetStalePacked`: returns expired-but-within-stale-window entries from positive cache only

## Racing Resolver (racing.go)

Shared health-aware server racing used by both DirectDNSClient and ProxyDNSClient. Each client creates a `racingResolver` with its transport-specific `queryServer()` as the query function, then delegates `Query()` to it.

- **Server selection** (`pick()`): Races top-N healthy servers sorted by EWMA latency. Untested servers sort last among healthy. If fewer than N healthy, adds one degraded server due for probe (30s interval). If all degraded, falls back to racing all servers.
- **Health tracking** (`serverHealth`): Lock-free via `sync/atomic`. Records EWMA latency (α=0.3) on success, increments consecutive failure count on failure. Server degrades after 3 consecutive failures, recovers on first success.
- **Default servers**: 6 per category (3 providers × primary + backup). Direct: 114DNS, AliDNS, DNSPod. Proxy: Google, Cloudflare, Quad9.

## Proxy DNS (proxy.go)

Uses persistent UDP connections with message-ID-based multiplexing (sing-box pattern). A single UDP connection per server handles all queries via callback registry, avoiding per-query DialUDP overhead. Delegates `Query()` to `racingResolver`.

## Files

- `middleware.go` — `Middleware` struct, pipeline + `queryUpstream()` + in-flight dedup
- `dns_cache.go` — `DNSCache`: freelru-based DNS cache with negative caching + stale-while-revalidate
- `filter.go` — `Filter`: RFC 6761 special-use domains + noise detection
- `hosts.go` — `HostsFile`: /etc/hosts integration
- `metrics.go` — `Metrics`: query/cache/filter/upstream counters
- `racing.go` — `racingResolver`: health-aware server racing for DNS upstreams (EWMA latency sorting, degradation after 3 consecutive failures, 30s probe interval). Both DirectDNSClient and ProxyDNSClient delegate Query() here.
- `direct.go` — `DirectDNSClient`: raw UDP to upstream (bypasses TUN, NXDOMAIN is valid response), delegates to racingResolver
- `proxy.go` — `ProxyDNSClient`: DNS via tunnel with persistent connection pooling, delegates to racingResolver

## Constitutional Rules (never violate)

- **No proxy→direct DNS fallback**: When k2rule routes a domain to `TargetProxy`, DNS resolution MUST only use proxy DNS servers (via tunnel). If proxy DNS fails, return error — NEVER fall back to direct DNS. Direct DNS in censored networks returns GFW-poisoned IPs, causing certificate errors and security violations. A failed response is better than a poisoned response. (2026-03-08 incident)

## Gotchas

- `DirectDNSClient` only falls back to next server on transport errors (timeout, connection refused). NXDOMAIN is a valid response and is returned as-is.
- `queryUpstream()` takes `k2rule.Target` (not `int`) — matches `k2rule.Match()` return type.
- `DNSQuerier.Query()` requires `context.Context` — all implementations must respect cancellation.
- `ProxyDNSClient` rewrites query IDs for multiplexing — restores original ID before returning.
- freelru `GetWithLifetimeNoExpire` has race issues in sing v0.7.6 — use `Get()` + `entry.expireAt` instead.

## Testing

```bash
go test ./core/dns/...    # ~50 tests, some need network access
go test ./core/dns/... -race  # race detector (critical for connection pooling)
```

Tests cover: cache TTL, negative caching, stale-while-revalidate, hosts parsing, filter rules, metrics counters, inflight dedup (normal, timeout, panic recovery), context cancellation, connection pooling reuse, concurrent cache access.
