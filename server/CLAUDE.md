# server — Server Daemon and Handlers

Server-side listener orchestration, auto-provisioning, and TCP/UDP proxy handlers.

## Files

- `server.go` — Server struct, TLS listener, ECH routing, auto-provisioning, lifecycle
- `handler.go` — TCP/UDP proxy handlers (AcceptTCP → net.Dial → pipe)
- `device_traffic.go` — Per-device traffic accumulator (UDID-keyed delta map, Rx/Tx counts)
- `device_traffic_reporter.go` — Immutable-batch uploader (boot_id + batch_seq idempotency, retry-safe to Center)
- `device_traffic_contract_test.go` — Wire-shape tripwire for `DeviceTrafficRequest` JSON contract
- `*_test.go` — unit tests for TLS peek, handlers, auto-provisioning

## Server DNS Cache

`HandleUDP` transparently caches DNS responses for port-53 traffic using `core/dns.DNSCache` (freelru, 10k entries, 5min–4hr TTL, 30min stale window). Cache hit: respond directly from cache, no upstream query. Cache miss: forward to system resolver (`h.dnsUpstream`, default `127.0.0.11:53` — Docker embedded DNS → host systemd-resolved → cloud provider internal DNS) instead of client-specified upstream. This avoids upstream reachability issues (e.g. `9.9.9.9` unreachable from certain cloud regions). Truncated (TC) responses forwarded but not cached. Non-DNS UDP (port != 53) unchanged. `writeToMu` serializes `conn.WriteTo` calls from cache-hit goroutine and upstream-response goroutine.

## Server UDP NAT Semantics

`HandleUDP`'s egress socket is **unconnected** (`net.ListenUDP("udp4", nil)`, `handler.go`): one session gets one stable egress port, and a reply from *any* source address is accepted back on it — endpoint-independent mapping + filtering, i.e. full-cone NAT. This is what games and ICE/STUN need (Symmetric NAT fails connectivity checks on both). A session tracks the distinct destinations it has addressed in a map + LRU (`udpDestEntry`, `maxUDPDestsPerSession = 64`, evicts oldest on overflow) so it can multiplex a game server + matchmaker + STUN reflector through the one socket.

**Do not change the egress socket back to `net.DialUDP`.** A connected socket pins the session to its first destination and only accepts replies from that peer — that's Symmetric NAT, and it will immediately regress the full-cone behavior this section describes: ICE checks fail Strict, and multi-destination sessions (matchmaker/STUN/game-server on one socket) silently lose replies from every destination but the first.

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
│ Auth  │  metadata stream (0x10) → Validator/AuthVerdict; enforce gate K2_ENFORCE_AUTH
└───┬───┘
    │
QUIC + TCP-WS Listeners
    │
┌───┴────┐  ┌──────────┐
│AcceptTCP│  │ServeUDP  │
└───┬────┘  └────┬─────┘
    │            │
net.Dial   net.ListenUDP
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
    quicSrv  *wire.QUICServer      // SetValidator + SetEnforce (metadata-stream device auth)
    tcpwsSrv *wire.TCPWSServer     // same validator + enforce — both transports gated
    handler  *ProxyHandler         // TCP/UDP proxy (AcceptTCP/ServeUDP)
    TLSConfig   *tls.Config       // cert selection callback + ECH keys
    echConfig   []byte            // ECH config list for client distribution
    done        chan struct{}
}
```

Note: `wire.ReverseProxy` is a function (not a type) — called for non-ECH SNIs to camouflage as the real target site.

**The camouflage relay is the one egress path that never passes device auth**, so its target must never be attacker-chosen. A non-ECH connection is relayed only if its SNI is in `trustedSNIs`, and `learnOuterSNI` admits exactly one name: `s.camouflageName`, the `public_name` of the node's own ECH config. The security here does **not** rest on client behavior: `learnOuterSNI` rejects any outer SNI ≠ camouflageName server-side (`server.go:730`), so even a modified client that decouples its outer SNI (the metacubex server tolerates outer-SNI ≠ public_name for decryption, `ech.go:610`) simply fails to teach the node anything — its ECH tunnel still works, but no arbitrary name enters the whitelist. (Stock clients happen to send `public_name` anyway, `handshake_client.go:310`, but the pin does not depend on that.) Without it, anyone holding the node's ECH config — it ships in the `k2v5://` URL, so every current and *former* user has one — could teach the node an arbitrary string and get a raw TCP relay to `<string>:443`, reaching even reserved IPs that `ProxyHandler.checkAddr` blocks on the authenticated path.

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
- Short request/response only: `reverseProxyHTTP` sets `ReadTimeout 30s` /
  `WriteTimeout 60s`, so streaming, long-poll, and SSE endpoints are out of
  scope and will be cut off — relay is intended for Center API calls only.

The single control-plane route is **built in** — Center's address is permanent,
so a node needs no per-node config: `New()` defaults `control_plane_routes` to
`{k2.52j.me: k2.52j.me:443}` when unset. An explicit config replaces (does not
merge with) that default. Sample explicit override:
```yaml
control_plane_routes:
  k2.52j.me: "k2.52j.me:443"
```

## Testing

- `server_test.go` — TLS peek routing, ECH vs non-ECH, auto-provisioning
- `handler_test.go` — TCP/UDP proxy with mock targets
- `control_plane_test.go` — relay routing, reverse-proxy, private-IP rejection
- `test/e2e_test.go` — full integration with real client

## Per-Device Traffic Accounting

Metering points: per-device deltas are counted at the SAME observation points as the node-level `rxBytes`/`txBytes` counters, not at connection/packet start or dial time. In `pipe()` (`handler.go` ~873, the two `devTraffic.Add` calls at ~881 and ~892), `devTraffic.Add` fires per direction when that direction's `io.CopyBuffer` returns, so TCP bytes land when a direction ends — a long-lived connection's bytes land on the day the connection closes, not the day it opened. In `HandleUDP`, bytes accumulate per packet into local `sessRx`/`sessTx` and flush into the device delta map once via `defer` at session end (`handler.go` ~478), so the same late-landing behavior applies. (Line numbers drift — the anchors are the `devTraffic.Add` call sites inside `pipe()` and the `defer` at the top of `HandleUDP`.) Periodically, `DeviceTrafficReporter` batches the deltas as an immutable `DeviceTrafficRequest` (BootID + BatchSeq for idempotency). A pending batch is immutable only in `boot_id`, `batch_seq`, and `devices` — retries resend those three verbatim; `ts` is recomputed fresh on every attempt (`time.Now().Unix()` in `post()`) and is deliberately NOT part of the idempotency key. Center dedups on `(boot_id, batch_seq)` via its per-node cursor, so a re-sent `ts` never causes a false duplicate or a false skip. Legacy `usageReporter` now requires `K2_NODE_CUTOFF_REPORT=1` (default off) because its verdict-handling path is incompatible with Center's current pure-recorder design (reads missing verdict as "stop" instead of "continue") — keep off unless explicitly mitigating legacy verdict consumption.

### Full-cone UDP meters unsolicited inbound — deliberately

`HandleUDP`'s egress socket is `net.ListenUDP` (unconnected), which is what makes the node a full cone: replies from *any* peer reach the session, which is what games and ICE need. The cost is that the kernel no longer filters. Anyone who learns or guesses a live session's ephemeral egress port can spray it, and the downlink loop meters **every byte it reads from the target** into the session's `tx` — so that traffic lands in the user's per-device delta and counts against their quota cutoff. Verified end-to-end: 1000 B from an unrelated host shows up in full in the device delta. Before full-cone, `net.DialUDP`'s connected socket dropped those packets in the kernel and the question never arose.

**We keep metering all of it.** Filtering to "only sources this session addressed" opens a worse hole in the other direction: a client could have a confederate send from an unaddressed port and get unmetered downlink for free. The exposure we accept instead requires an attacker to guess **node IP + the ephemeral egress port of a live session** and only burns quota — no data reaches anyone who wasn't already going to receive it. Do not "fix" this by making the metering source-aware.

What exists instead is visibility. The session's closing `server: HandleUDP done` line carries `srcUnsolicitedPkts` / `srcUnsolicitedBytes` alongside `destsBlocked` / `destsUnresolvable` / `destsEvicted`: how many packets and bytes arrived from a peer this session never addressed. `addressedPeers` (`handler.go`) is the shadow set behind it — refcounted, bounded by the same `maxUDPDestsPerSession` budget as the destination LRU and released in lockstep with it, so it adds no unbounded memory. Two known inaccuracies, both accepted because this is a triage signal and not a security control: a peer whose destination entry was LRU-evicted reads as unaddressed afterwards, and a peer that speaks *before* the session addresses it (legitimate for ICE/STUN) is counted until the session's first uplink packet to it clears the verdict.
