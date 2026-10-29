# k2p ICE Redesign — NAT Traversal via ICE + TURN, Data Plane via QUIC

## Motivation

Current k2p has a custom NAT traversal stack (~800 lines): STUN probing, NAT classification, standard/birthday punch, and a custom WS relay. This stack has three critical issues:

1. **Relay is broken** — Away connects relay on punch failure, but Home never does. Path is dead.
2. **Relay has no authentication** — anyone can connect `GET /k2p/relay?udid=xxx&role=home` and hijack pairing.
3. **STUN socket != punch socket** — NAT classify uses one UDP socket, punch opens another. Under symmetric NAT the mappings differ, making the exchanged reflexive address useless.

ICE (Interactive Connectivity Establishment, RFC 8445) is the standard solution for NAT traversal, battle-tested for 20 years across billions of WebRTC sessions. TURN (RFC 8656) is the standard relay with built-in authentication. Replacing our custom stack with ICE + TURN fixes all three issues and adds capabilities we don't have: same-LAN direct connection, trickle candidate exchange, and IPv6 support.

## Decision Record

**Why ICE, not custom punch?** NAT traversal is a solved problem. Our 800 lines of custom code have 3 critical bugs. pion/ice is production-grade Go, MIT-licensed, used by thousands of applications.

**Why TURN, not custom relay?** TURN has built-in long-term credential auth (RFC 8489), allocation quotas, and permission control. Our custom relay has none.

**Why keep QUIC, not switch to WebRTC DataChannel?** k2p is a proxy tunnel. QUIC streams give us per-stream independent ordering (no HOL blocking), multiplexed TCP proxy, and UDP datagrams. SCTP (DataChannel's transport) shares a single congestion window across all streams — a high-throughput download on one channel starves DNS queries on another. k2v5's existing `wire.Dialer`/`wire.Listener` interface gives us full code reuse with `server.ProxyHandler`.

**Why not worry about GFW?** P2P hole-punched traffic between residential IPs is indistinguishable from WebRTC video calls (WeChat, Tencent Meeting, DingTalk). GFW does not block residential-to-residential encrypted UDP — doing so would break all video calling and gaming. GFW blocks based on: IP blacklists (Google/YouTube IPs), SNI blacklists, known VPN protocol fingerprints (OpenVPN/WireGuard). None of these apply to residential-to-residential QUIC.

## Architecture

### Layer Model

```
┌─────────────────────────────────────────────────┐
│ CLI / App (cmd/k2/peer_*.go)                    │
│   k2 peer setup / k2 peer run                   │
├─────────────────────────────────────────────────┤
│ k2p Orchestration (k2p/home.go, k2p/away.go)   │
│   Listen() / Dial() — creates ICE agent per     │
│   session, exchanges candidates, waits for ICE  │
│   connection, layers QUIC on top                │
├─────────────────────────────────────────────────┤
│ ICE (pion/ice)                                  │
│   Candidate gathering (host + srflx + relay)    │
│   Connectivity checks, keepalive, ICE restart   │
│   Returns *ice.Conn (net.Conn)                  │
├─────────────────────────────────────────────────┤
│ QUIC (apernet/quic-go)                          │
│   Stream multiplexing, datagrams, CC            │
│   Runs over ice.Conn wrapped as PacketConn      │
├─────────────────────────────────────────────────┤
│ Peer Auth (k2p/auth.go) — unchanged             │
│   First QUIC stream: [ver][len][secret]          │
├─────────────────────────────────────────────────┤
│ wire.Dialer / wire.Listener (wire/quic_p2p.go)  │
│   k2v5 framing for proxy stream semantics       │
│   Reused unchanged                               │
├─────────────────────────────────────────────────┤
│ server.ProxyHandler — unchanged                  │
│   HandleTCP / HandleUDP → net.Dial(LAN target)  │
└─────────────────────────────────────────────────┘
```

### Signal Server (k2s)

```
┌─────────────────────────────────────────────────┐
│ k2s                                              │
│                                                  │
│  HTTP endpoints:                                 │
│    POST /k2p/claim          (unchanged)          │
│    POST /k2p/register       (simplified)         │
│    GET  /k2p/lookup/{udid}  (add TURN creds)     │
│                                                  │
│  WS endpoints:                                   │
│    GET /k2p/ws?udid=X&token=T&role=home          │
│      Home long-lived WS (presence + candidate    │
│      relay). Unchanged purpose, new message fmt. │
│                                                  │
│    GET /k2p/ws?udid=X&role=away                  │
│      Away ephemeral WS (candidate exchange for   │
│      one ICE session). New endpoint.             │
│                                                  │
│  TURN server (embedded pion/turn):               │
│    UDP :3478 (or configurable port)              │
│    Long-term credentials via shared secret       │
│    Allocation quota + permission control         │
│                                                  │
│  Deleted:                                        │
│    POST /k2p/connect        (replaced by WS)     │
│    GET  /k2p/relay          (replaced by TURN)   │
└─────────────────────────────────────────────────┘
```

## Protocol Flows

### Setup (unchanged)

```
User ── k2 peer setup <k2v5://URL> ──→
  1. Parse URL → extract signal server
  2. Generate/input UDID
  3. Prompt password (PeerSecret)
  4. Generate TLS cert pair (EC P-256) → ~/.k2p/
  5. Detect LAN CIDRs + DNS
  6. POST /k2p/claim → RegToken
  7. bcrypt(password) → SecretHash (local)
  8. Build k2p:// URL (UDID + password + pin + signal)
  9. Save ~/.k2p/config.json
```

### Home Online

```
Home                                    k2s
 │                                       │
 ├─ POST /k2p/register ────────────────→ │  Bearer: RegToken
 │  {udid, networks, dns}                │  (no addr/nat_type — ICE handles)
 │  ← 200 OK                            │
 │                                       │
 ├─ GET /k2p/ws?udid=X&token=T          │
 │       &role=home ────────────────────→│  validate token, upgrade WS
 │  ← 101 WebSocket                     │  store.SetWS(udid, conn)
 │                                       │
 │  [keepalive: 25s re-register]         │
 │  [WS: wait for session-start msgs]   │
```

### Away Connects

```
Away                          k2s Signal                    Home
 │                               │                           │
 │  GET /k2p/lookup/{udid} ─────→│                           │
 │  ← {networks, dns, online,   │                           │
 │     turn_username, turn_pwd,  │                           │
 │     turn_urls, turn_ttl}      │                           │
 │                               │                           │
 │  GET /k2p/ws?udid=X           │                           │
 │       &role=away ────────────→│                           │
 │  ← 101 WebSocket             │                           │
 │                               │                           │
 │  (create ICE agent with       │                           │
 │   STUN + TURN URLs)           │                           │
 │  agent.GatherCandidates()     │                           │
 │                               │                           │
 │── {"type":"credentials",      │                           │
 │    "ufrag":"aB1c",            │── forward to Home WS ────→│
 │    "pwd":"xY2z"} ───────────→│   with session ID          │
 │                               │                           │
 │                               │                     (create ICE agent
 │                               │                      for this session,
 │                               │                      GatherCandidates)
 │                               │                           │
 │── {"type":"candidate",        │                           │
 │    "candidate":"..."} ──────→ │── forward ──────────────→ │
 │                               │                           │
 │                               │   ←── forward ────────── │── {"type":"credentials",...}
 │                               │                           │
 │                               │   ←── forward ────────── │── {"type":"candidate",...}
 │                               │                           │
 │  ... trickle candidates exchanged in parallel ...        │
 │                               │                           │
 │══ ICE connectivity checks (direct STUN binding) ═════════│
 │                               │                           │
 │  ICE selects best pair:       │                           │
 │   host (LAN) / srflx (punch) / relay (TURN)              │
 │                               │                           │
 │  Close Away WS (signal done)  │                           │
```

### QUIC + Auth (over ICE connection)

```
Away (QUIC Client)                       Home (QUIC Server)
 │                                         │
 │  ice.Conn wrapped as PacketConn         │  ice.Conn wrapped as PacketConn
 │                                         │
 ├─ QUIC Dial(syntheticAddr, TLS{h3})     │
 │  TLS: InsecureSkipVerify + pin check    │  present EC cert
 │  ← QUIC connection established          │  ← QUIC Accept
 │                                         │
 ├─ Auth on first stream (unchanged):      │
 │  [0x01][len BE u16][PeerSecret UTF-8]   │  ReadAuthFrame → bcrypt verify
 │  ← [0x00 = OK]                         │  → Write response
 │                                         │
 │  ← wire.Dialer (QUICClient)            │  ← wire.Listener (QUICServer)
 │                                         │
 │══ k2v5 streams: proxy traffic ══════════│
 │  TCP: StreamHeader → ProxyHandler.TCP   │
 │  UDP: Datagram → ProxyHandler.UDP       │
```

### Transport Type Detection

```go
pair, _ := agent.GetSelectedCandidatePair()
switch pair.Local.Type() {
case ice.CandidateTypeHost:           → "p2p-lan"     // same LAN
case ice.CandidateTypeServerReflexive: → "p2p-direct"  // NAT punched
case ice.CandidateTypeRelay:           → "p2p-relay"   // TURN
}
```

## Signal API Changes

### POST /k2p/register (simplified)

```
Before: {udid, addr, nat_type, networks, dns}
After:  {udid, networks, dns}
```

`addr` and `nat_type` removed — ICE handles NAT discovery internally.

### GET /k2p/lookup/{udid} (extended)

```json
{
  "udid": "home-abc",
  "networks": ["192.168.1.0/24"],
  "dns": "192.168.1.1",
  "online": true,
  "turn": {
    "urls": ["turn:k2s.example.com:3478?transport=udp"],
    "username": "1680000000:home-abc",
    "password": "hmac-sha1-derived",
    "ttl": 3600
  }
}
```

TURN credentials are time-limited (1h TTL), generated from k2s's shared secret using TURN REST API format (RFC-compliant, same as Twilio/Xirsys).

### GET /k2p/ws (extended)

Home role (unchanged purpose, new message format):
```
GET /k2p/ws?udid=X&token=T&role=home
```

Away role (new):
```
GET /k2p/ws?udid=X&role=away
```

Signal generates a session ID for each Away connection. All messages include session ID for routing.

### WS Message Format

```json
// Signal → Home: new Away wants to connect
{"type": "session-start", "session": "s1"}

// Bidirectional: ICE credentials
{"type": "credentials", "session": "s1", "ufrag": "aB1c", "pwd": "xY2z"}

// Bidirectional: ICE candidate (trickle)
{"type": "candidate", "session": "s1", "candidate": "candidate:842163049 1 udp ..."}

// Bidirectional: gathering complete
{"type": "gathering-done", "session": "s1"}

// Signal → Away/Home: peer disconnected from signal
{"type": "session-end", "session": "s1"}
```

### Deleted Endpoints

- `POST /k2p/connect` — replaced by WS session-start
- `GET /k2p/relay` — replaced by TURN

## TURN Server Design

### Embedding in k2s

k2s starts a pion/turn server on a dedicated UDP port (default 3478, configurable).

```go
turnServer, _ := turn.NewServer(turn.ServerConfig{
    Realm: "k2p",
    AuthHandler: turnRESTAuthHandler(sharedSecret),
    PacketConnConfigs: []turn.PacketConnConfig{{
        PacketConn: udpListener,
        RelayAddressGenerator: &turn.RelayAddressGeneratorStatic{
            RelayAddress: publicIP,
            Address:      "0.0.0.0",
        },
    }},
    QuotaHandler: func(username, realm string, srcAddr net.Addr) bool {
        return getAllocationCount(username) < maxAllocationsPerUser
    },
})
```

### TURN Credential Flow

```
1. k2s has a server_shared_secret (generated at setup, stored in config)
2. Client requests credentials via GET /k2p/lookup (or dedicated endpoint)
3. k2s generates:
     username = "expiry_timestamp:udid"
     password = base64(HMAC-SHA1(username, server_shared_secret))
     ttl = 3600 (1 hour)
4. Client uses these credentials in ICE agent TURN URL config
5. When TURN allocation request arrives, k2s's AuthHandler:
     - Parses timestamp from username
     - Checks not expired
     - Recomputes HMAC, compares with provided key
     - No database lookup needed
```

### Abuse Prevention

| Mechanism | Implementation |
|-----------|---------------|
| **TTL** | Credentials expire after 1 hour |
| **Quota** | QuotaHandler: max 5 allocations per username |
| **Rate limit** | Credential endpoint rate-limited per IP |
| **Permission** | PermissionHandler: relay only to public IPs (no internal network relay) |

## ice.Conn → QUIC Adapter

QUIC's `Transport.Conn` requires `net.PacketConn`. ICE's `Conn` implements `net.Conn`. Adapter:

```go
// icePacketConn adapts ice.Conn to net.PacketConn for QUIC transport.
// Same pattern as k2p.RelayConn (already tested and working).
type icePacketConn struct {
    conn      *ice.Conn
    localAddr net.Addr
    peerAddr  net.Addr
}

func (c *icePacketConn) ReadFrom(p []byte) (int, net.Addr, error) {
    n, err := c.conn.Read(p)
    return n, c.peerAddr, err
}

func (c *icePacketConn) WriteTo(p []byte, _ net.Addr) (int, error) {
    return c.conn.Write(p)
}

func (c *icePacketConn) Close() error              { return c.conn.Close() }
func (c *icePacketConn) LocalAddr() net.Addr        { return c.localAddr }
func (c *icePacketConn) SetDeadline(t time.Time) error     { return c.conn.SetDeadline(t) }
func (c *icePacketConn) SetReadDeadline(t time.Time) error { return c.conn.SetReadDeadline(t) }
func (c *icePacketConn) SetWriteDeadline(t time.Time) error { return c.conn.SetWriteDeadline(t) }
```

**Why this works**: ice.Conn is backed by UDP — each Read() returns one datagram (packet boundaries preserved). QUIC reads one packet per ReadFrom. STUN keepalive packets are consumed by ICE internally and never surface in Read(). QUIC and STUN have non-overlapping first-byte ranges (STUN: 0x00-0x01, QUIC Long: 0xC0+, QUIC Short: 0x40+).

**MTU**: TURN ChannelData adds 4 bytes overhead. Standard path MTU ~1472 (Ethernet 1500 - IP 20 - UDP 8). QUIC minimum initial packet 1200 bytes. 1472 - 4 = 1468 >> 1200. No MTU issues.

## Security Model

### Unchanged from current k2p

| Secret | Holder | Purpose | Sent to k2s? |
|--------|--------|---------|:---:|
| PeerSecret | Home + Away | Peer auth (QUIC stream) | Never |
| Pin (cert hash) | Away | Verify Home's TLS cert | Never |
| TLS private key | Home only | QUIC TLS handshake | Never |
| RegToken | Home + k2s | Signal auth (register, WS) | Yes (bcrypt hash stored) |

### New: TURN credentials

| Secret | Holder | Purpose | Risk if leaked |
|--------|--------|---------|---------------|
| TURN username/password | Home, Away, k2s | TURN relay allocation | Attacker can use TURN bandwidth for 1 hour. Cannot access Home (needs PeerSecret). Low value. |
| server_shared_secret | k2s only | Derive TURN credentials | Must protect. Leaked = unlimited TURN credential generation. |

### Zero-trust preserved

k2s (signal + TURN) still cannot:
- Decrypt traffic (QUIC TLS between Home and Away, k2s has no private key)
- Impersonate Home (needs TLS private key)
- Impersonate Away (needs PeerSecret)
- Inject traffic (QUIC integrity check)
- Access Home's LAN (needs PeerSecret to pass auth)

TURN relay forwards encrypted QUIC packets. k2s sees ciphertext only.

## Configuration Changes

### Config struct

```go
type Config struct {
    HomeUDID     string            // unchanged
    SignalServer string            // unchanged
    Token        string            // unchanged
    WireCfg      wire.Config       // unchanged
    // TicketStore *wire.TicketStore — REMOVED 2026-07 (ticket auth deleted; device auth rides the metadata stream)
    TLSCert      *tls.Certificate  // unchanged
    PeerSecret   string            // unchanged
    Pin          string            // unchanged
    Networks     []string          // unchanged
    DNS          string            // unchanged
    RegToken     string            // unchanged
    SecretHash   string            // unchanged

    // Removed:
    // DirectDialer  — ICE manages its own sockets
    // STUNServers   — configured in ICE agent URLs
    // RelayServer   — replaced by TURN

    // New:
    STUNURLs     []string          // STUN server URLs (default: Google/Cloudflare)
    TURNURLs     []string          // TURN server URLs (from lookup response)
    TURNUsername string             // TURN credential (from lookup response)
    TURNPassword string             // TURN credential (from lookup response)
}
```

### URL format (unchanged)

```
k2p://udid:password@udid.signal-domain:port?pin=sha256:xxx
```

TURN URLs are not in the k2p:// URL — they come from k2s at runtime via /k2p/lookup.

### k2s config (new fields)

```yaml
turn:
  enabled: true
  port: 3478                    # UDP port for TURN
  public_ip: "203.0.113.1"     # k2s public IP for relay address
  shared_secret: "auto"        # auto-generated at setup, or explicit
  max_allocations_per_user: 5
  credential_ttl: 3600         # seconds
```

## File Changes

### Delete (~800 lines)

| File | Lines | Replaced by |
|------|-------|-------------|
| k2p/nat.go | 103 | pion/ice internal NAT handling |
| k2p/stun.go | 76 | pion/ice internal STUN |
| k2p/punch.go | 229 | pion/ice connectivity checks |
| k2p/relay.go | 80 | pion/turn |
| signal/relay.go | 153 | pion/turn |
| k2p/nat_test.go | ~50 | ICE tests |
| k2p/stun_test.go | ~50 | ICE tests |
| k2p/punch_test.go | ~100 | ICE tests |
| k2p/relay_test.go | ~50 | TURN tests |

### Create (~300 lines)

| File | Purpose |
|------|---------|
| k2p/ice.go | ICE agent creation, icePacketConn adapter, candidate exchange helpers |
| signal/turn.go | Embedded TURN server, credential generation, REST API auth handler |

### Modify (~400 lines)

| File | Changes |
|------|---------|
| k2p/home.go | Replace punch with ICE Accept, remove NAT classify, add session-based candidate exchange |
| k2p/away.go | Replace punch+relay with ICE Dial, remove relay fallback (TURN handles it), add candidate exchange |
| k2p/signal.go | WS messages become candidate exchange. Remove RequestConnect. Add TURNCreds to PeerInfo. |
| k2p/config.go | Remove STUNServers/DirectDialer/RelayServer, add TURN fields |
| signal/handler.go | Remove /k2p/connect and /k2p/relay. WS handler becomes session-based candidate router. Add TURN cred endpoint. |
| signal/store.go | Remove Addr/NATType fields from PeerEntry/PeerSnapshot. |
| signal/types.go | Simplify RegisterRequest (remove addr/nat_type). Add TURNCredentials type. |
| cmd/k2s | Embed pion/turn server startup |

### Unchanged

| File | Why |
|------|-----|
| k2p/auth.go | Peer auth is at QUIC stream level, transport-independent |
| k2p/url.go | URL format unchanged |
| k2p/networks.go | LAN detection unchanged |
| wire/quic_p2p.go | QUIC server/client from conn, transport-independent |

## New Dependencies

```
github.com/pion/ice/v4         — ICE agent (MIT)
github.com/pion/turn/v4        — TURN server (MIT)
github.com/pion/stun/v3        — already in go.mod
github.com/pion/logging        — pion log framework
github.com/pion/transport/v3   — network utilities
github.com/pion/dtls/v3        — DTLS (ICE internal, STUN message integrity)
```

Note: pion/webrtc is NOT needed. Only ICE + TURN layers.

## Gained Capabilities

| Capability | Before | After |
|-----------|--------|-------|
| Same-LAN direct | No | Yes (ICE host candidate) |
| Relay fallback | Broken | Working (TURN, authenticated) |
| Relay auth | None | TURN long-term credentials |
| IPv6 | No | Yes (ICE handles) |
| Trickle candidates | No (wait for STUN, then punch) | Yes (incremental exchange) |
| Network change | No | ICE restart (v2) |
| NAT traversal reliability | Custom (socket reuse bug) | Standard ICE (20 years, production-grade) |
| Transport detection | Manual | Automatic (host/srflx/relay) |

## Lost Capabilities

| Capability | Impact |
|-----------|--------|
| Birthday punch (Hard-Hard NAT) | Low — success rate ~8%, TURN fallback is more reliable |
| Custom punch strategies | Low — ICE standard checks cover Easy-Easy and Easy-Hard |

## DIAG Events

### Remove

| Event | Replaced by |
|-------|-------------|
| `DIAG: nat-classify` | ICE internal (logged by pion at debug level) |
| `DIAG: punch-start` | `DIAG: ice-connecting` |
| `DIAG: punch-ok` | `DIAG: ice-connected` |
| `DIAG: punch-fail` | `DIAG: ice-failed` |
| `DIAG: p2p-relay-fallback` | Implicit in transport type = "p2p-relay" |

### New

| Event | Level | Fields |
|-------|-------|--------|
| `DIAG: ice-connecting` | INFO | session, candidateTypes |
| `DIAG: ice-connected` | INFO | session, localType, remoteType, localAddr, remoteAddr |
| `DIAG: ice-failed` | WARN | session, err |
| `DIAG: ice-disconnected` | WARN | session |
| `DIAG: turn-allocation` | INFO | username, relayAddr |
| `DIAG: turn-allocation-expired` | INFO | username |

## Lock Ordering Graph Update

Remove from CLAUDE.md:
```
signal.Relay.mu (state)
RelayConn.mu (serialization, standalone — protects ws.WriteMessage)
```

No new locks — ICE agent manages its own concurrency internally.

## Testing Strategy

### Unit Tests

| Test | Verifies |
|------|----------|
| icePacketConn adapter | ReadFrom/WriteTo preserve packet boundaries |
| TURN credential generation | HMAC matches, expiry works, rotation works |
| WS candidate exchange | Session routing, multi-Away, message ordering |
| Signal store (simplified) | Register without addr/nat_type |

### Integration Tests

| Test | Verifies |
|------|----------|
| ICE loopback direct | Two ICE agents on loopback → host candidate selected → QUIC + auth |
| ICE with TURN | Block direct path → TURN relay → QUIC + auth |
| Auth rejection over ICE | Wrong PeerSecret rejected (same as current, new transport) |
| Pin rejection over ICE | Wrong cert pin rejected (same as current, new transport) |
| Multi-Away concurrent | Two Aways connect simultaneously, independent sessions |

### E2E Tests

| Test | Verifies |
|------|----------|
| Full flow (signal + ICE + QUIC + proxy) | Replaces TestK2P_E2E_DirectPunch with ICE-based flow |
| TURN fallback e2e | ICE with only relay candidate → full proxy flow |

## Migration

### Breaking Changes

- `POST /k2p/connect` removed — Away no longer calls this
- `GET /k2p/relay` removed — replaced by TURN
- `RegisterRequest` no longer has `addr` and `nat_type` fields
- `LookupResponse` gains `turn` field, loses `addr` and `nat_type`
- WS message format changed (PunchNotify → candidate exchange)

### Non-Breaking

- k2p:// URL format unchanged
- ~/.k2p/config.json format unchanged (new fields optional)
- PeerSecret / Pin / RegToken semantics unchanged
- Peer auth protocol unchanged
- k2v5 data plane unchanged

### Rollout

Since k2p is not yet released (all on feat/k2p2p branch), this is a clean replacement with no backwards compatibility concerns. The old custom NAT/punch/relay code is deleted entirely.
