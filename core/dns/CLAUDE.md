# DNS Layer (core/dns/)

13-step middleware pipeline with ~92% filter rate.

## Pipeline (middleware.go)

`Middleware.HandleDNSQuery(pkt []byte, info packet.Info) error`

Steps: parse -> validate -> hosts lookup -> filter (RFC 6761 + noise) -> cache check -> route (k2rule) -> upstream query (direct or proxy) -> cache store -> IP cache store -> respond via provider.

**Key**: Direct DNS uses raw UDP sockets (bypasses TUN). Proxy DNS goes through tunnel.

## Files

- `middleware.go` — `Middleware` struct, 13-step `HandleDNSQuery` pipeline
- `cache.go` — `DynamicIPCache`: IP -> k2rule.Target mapping (TTL-based), used by router
- `dns_cache.go` — `DNSCache`: DNS message cache with negative caching
- `filter.go` — `Filter`: RFC 6761 special-use domains + noise detection
- `hosts.go` — `HostsFile`: /etc/hosts integration
- `metrics.go` — `Metrics`: query/cache/filter/upstream counters
- `direct.go` — `DirectDNSClient`: raw UDP to 114.114.114.114 (bypasses TUN)
- `proxy.go` — `ProxyDNSClient`: DNS via tunnel (8.8.8.8)

## Testing

```bash
go test ./core/dns/...    # 45+ tests, some need network access
```

Tests cover: cache TTL, negative caching, hosts parsing, filter rules, metrics counters.
