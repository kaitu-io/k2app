# server — Server Daemon and Handlers

Server-side listener orchestration, auto-provisioning, and TCP/UDP proxy handlers.

## Files

- `server.go` — Server struct, TLS listener, ECH routing, auto-provisioning, lifecycle
- `handler.go` — TCP/UDP proxy handlers (AcceptTCP → net.Dial → pipe)
- `*_test.go` — unit tests for TLS peek, handlers, auto-provisioning

## Server DNS Cache

`HandleUDP` transparently caches DNS responses for port-53 traffic using `core/dns.DNSCache` (freelru, 10k entries, 5min–4hr TTL, 30min stale window). Cache hit: respond directly from cache, no upstream query. Cache miss: forward to system resolver (`h.dnsUpstream`, default `127.0.0.11:53` — Docker embedded DNS → host systemd-resolved → cloud provider internal DNS) instead of client-specified upstream. This avoids upstream reachability issues (e.g. `9.9.9.9` unreachable from certain cloud regions). Truncated (TC) responses forwarded but not cached. Non-DNS UDP (port != 53) unchanged. `writeToMu` serializes `conn.WriteTo` calls from cache-hit goroutine and upstream-response goroutine.

## Architecture

```
:443 ◄── TLS ClientHello peek
         │
    ┌────┴─────┐
ECH match   No match
    │          │
k2v5 handler  ReverseProxy
    │
┌───┴───┐
│ Auth  │  POST /k2v5/auth, TicketStore
└───┬───┘
    │
QUIC + TCP-WS Listeners
    │
┌───┴────┐  ┌──────────┐
│AcceptTCP│  │ServeUDP  │
└───┬────┘  └────┬─────┘
    │            │
net.Dial     net.Dial
bidirectional  forward
  pipe       packets
```

## Auto-Provisioning (`setupAutoProvision`)

When `CertDir` is set (always true in zero-config mode):

1. `mkdir -p cert_dir`
2. `EnsureCertPair` → RSA-2048 + EC P-256 dual keypair
3. `EnsureECHPrivateKey` → X25519 HPKE key (persistent)
4. `FetchTemplateWithFallback` → Cloudflare ECH template (DNS → cache → defaults)
5. `GenerateECHKeyFromTemplate` → forged ECH config (config_id+1)
6. `SaveECHConfig` + `LoadAllECHConfigs` → multi-config rotation
7. `ProbeTarget(publicName)` → measure target cert length
8. Apply TLS record padding to both RSA and EC certs
9. Configure TLS: cert selection callback + all ECH keys
10. `EnsureAuth` → load or generate UDID+token
11. `logConnectionURL` → print `k2v5://` URL to stderr

## Key Types

```go
type Server struct {
    cfg      *config.ServerConfig
    store    *wire.TicketStore
    quicSrv  *wire.QUICServer
    tcpwsSrv *wire.TCPWSServer
    handler  *ProxyHandler         // TCP/UDP proxy (AcceptTCP/ServeUDP)
    authHandler http.Handler       // POST /k2v5/auth
    TLSConfig   *tls.Config       // cert selection callback + ECH keys
    echConfig   []byte            // ECH config list for client distribution
    done        chan struct{}
}
```

Note: `wire.ReverseProxy` is a function (not a type) — called for non-ECH SNIs to camouflage as the real target site.

## Lifecycle

1. `New(cfg)` → create server, run `setupAutoProvision` if CertDir set
2. `Listen()` → start TLS listener on configured port
3. Accept TCP connections
4. Peek ClientHello SNI:
   - ECH inner SNI match → k2v5 handler
   - No match → reverse proxy (camouflage)
5. k2v5 handler: auth → QUIC/TCP-WS listeners → TCP/UDP proxy
6. `Close()` → graceful shutdown with connection draining

## Control-Plane Relay (antiblock Phase 1)

When an ECH connection's **inner SNI** matches `control_plane_routes`
(`config.ServerConfig`, exact match), `handleK2V5` HTTP-reverse-proxies it to
the configured Center origin over a fresh HTTPS connection (system CA validates
Center's real cert) instead of running the k2v5 tunnel path. This lets clients
reach Center API through any reachable node when the direct path is GFW-blocked.

- Allowlist-only (`matchControlPlaneRoute`, exact match); non-matching inner SNI
  falls through to the existing k2v5 dispatch (backward compatible).
- Reuses `proxySem` for concurrency limiting; emits `DIAG: control-plane-relay`
  audit logs (no body/token).
- 做法一: the node terminates TLS and sees plaintext (accepted by design —
  the node already holds the device credential needed to connect).

Sample node config:
```yaml
control_plane_routes:
  k2.52j.me: "k2.52j.me:443"
```

## Testing

- `server_test.go` — TLS peek routing, ECH vs non-ECH, auto-provisioning
- `handler_test.go` — TCP/UDP proxy with mock targets
- `control_plane_test.go` — relay routing, reverse-proxy, private-IP rejection
- `test/e2e_test.go` — full integration with real client
