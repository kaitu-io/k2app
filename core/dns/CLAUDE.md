# DNS Layer (core/dns/)

13-step middleware pipeline with ~92% filter rate.

## Pipeline (middleware.go)

Two entry points:
- **L3 (desktop TUN)**: `HandleDNSQuery(pkt []byte, info packet.Info) error` — writes response back via provider
- **L4 (mobile)**: `HandleQuery(query []byte) ([]byte, error)` — pure bytes in/out, used by `engine.dnsHandler`

Steps: parse -> validate -> hosts lookup -> filter (RFC 6761 + noise) -> cache check -> route (k2rule) -> upstream query (direct or proxy) -> cache store -> SetTmpRule for learned IPs -> respond.

**Key**: Direct DNS uses raw UDP sockets (bypasses TUN). Proxy DNS goes through tunnel.

## Files

- `middleware.go` — `Middleware` struct, 13-step `HandleDNSQuery` pipeline
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
