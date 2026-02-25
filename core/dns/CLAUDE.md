# DNS Layer (core/dns/)

13-step middleware pipeline with ~92% filter rate and in-flight query deduplication.

## Pipeline (middleware.go)

Two entry points:
- **L3 (desktop TUN)**: `HandleDNSQuery(pkt []byte, info packet.Info) error` — writes response back via provider
- **L4 (all modes)**: `HandleQuery(query []byte) ([]byte, error)` — pure bytes in/out, used by `engine.dnsHandler`

Steps: parse → validate → hosts lookup → filter (RFC 6761 + noise) → cache check → route (k2rule) → reject check → in-flight dedup → upstream query (direct or proxy) → cache store → SetTmpRule for learned IPs → respond.

**Key**: Direct DNS uses raw UDP sockets (bypasses TUN). Proxy DNS goes through tunnel.

**In-flight dedup**: `sync.Map` keyed by `domain:qtype`. If another goroutine is already querying the same domain+qtype, waiters share the result instead of issuing duplicate upstream queries.

## Files

- `middleware.go` — `Middleware` struct, pipeline + `queryUpstream()` + in-flight dedup
- `dns_cache.go` — `DNSCache`: DNS message cache with negative caching
- `filter.go` — `Filter`: RFC 6761 special-use domains + noise detection
- `hosts.go` — `HostsFile`: /etc/hosts integration
- `metrics.go` — `Metrics`: query/cache/filter/upstream counters
- `direct.go` — `DirectDNSClient`: raw UDP to upstream (bypasses TUN, NXDOMAIN is valid response)
- `proxy.go` — `ProxyDNSClient`: DNS via tunnel

## Gotchas

- `DirectDNSClient` only falls back to next server on transport errors (timeout, connection refused). NXDOMAIN is a valid response and is returned as-is.
- `queryUpstream()` takes `k2rule.Target` (not `int`) — matches `k2rule.Match()` return type.

## Testing

```bash
go test ./core/dns/...    # 35 tests, some need network access
```

Tests cover: cache TTL, negative caching, hosts parsing, filter rules, metrics counters.
