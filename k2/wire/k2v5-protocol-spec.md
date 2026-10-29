# k2v5 Protocol Specification

Version: 1.2
Date: 2026-07-26

## 1. Overview

k2v5 is a composite tunnel protocol that combines QUIC/H3 (primary) and TCP-WebSocket (fallback) transports into a unified interface. It provides TCP and UDP proxying over encrypted connections with censorship resistance features including ECH (Encrypted Client Hello), TLS fingerprint mimicry, and UDP port hopping.

### Design Principles

- **Zero-config stealth**: Server auto-generates certificates, ECH config, and padding on startup
- **Composite transport**: QUIC preferred, TCP-WS automatic fallback
- **Native multiplexing**: QUIC streams for TCP, QUIC datagrams for UDP (no custom mux layer)
- **ECH mandatory**: Real SNI never exposed in plaintext
- **Self-signed + pin**: No CA dependency, no Certificate Transparency exposure
- **PCC congestion control**: Rate-based algorithm optimized for lossy/censored networks
- **Single port**: QUIC and TCP-WS share the same server port via protocol detection

## 2. URL Format

```
k2v5://UDID:TOKEN@HOST:PORT?ech=ECH_CONFIG&pin=CERT_PIN&fp=FINGERPRINT&hop=START-END
```

### Components

| Component | Required | Default | Description |
|-----------|----------|---------|-------------|
| `UDID` | Yes | - | Device unique identifier |
| `TOKEN` | Yes | - | JWT authentication token |
| `HOST` | Yes | - | Server IP or hostname |
| `PORT` | Yes | - | Server port |
| `ech` | Yes | - | Base64-encoded ECH config list (contains public_name for outer SNI) |
| `pin` | Yes | - | `sha256:` + server certificate SHA-256 fingerprint (base64) |
| `fp` | No | `chrome` | TLS fingerprint: `chrome`, `firefox`, `safari`, `random` |
| `hop` | No | disabled | UDP port hopping range: `START-END` |

The `ech` parameter is mandatory. Without it, the client refuses to connect. The outer SNI (camouflage domain) is the `public_name` inside the ECH config; the current client emits exactly that value and has no separate `sni` parameter. This is a client-side default, **not** a protocol constraint — the outer SNI can be decoupled from `public_name` (see §3.1 and `docs/superpowers/specs/2026-08-10-outer-sni-camouflage-design.md`), but doing so interacts with server-side camouflage and is not a free knob.

The `pin` parameter is mandatory. The client verifies the server certificate by SHA-256 fingerprint, not by domain name or CA chain. This eliminates the need for domain names and avoids Certificate Transparency log exposure.

### Example

```
k2v5://device-abc:eyJhbGci...@203.0.113.1:443?ech=AEX...&pin=sha256:7d2e91...&fp=chrome&hop=20000-30000
```

## 3. TLS Layer

### 3.1 Three-Layer Identity Model

k2v5 uses three distinct layers of identity to achieve stealth:

```
Layer           Value                    Visibility
────────────────────────────────────────────────────────
TCP dest IP     203.0.113.1              Plaintext (network layer)
Outer SNI       cloudflare-ech.com       Plaintext (TLS ClientHello, from ECH config's public_name)
Inner SNI       (arbitrary value)        ECH-encrypted (invisible)
TLS cert        self-signed              TLS 1.3 encrypted (invisible)
Cert verify     pin=sha256:...           Client-local (never on wire)
```

The outer SNI equals the `public_name` inside the ECH config **because the current client emit path sets it so** (`handshake_client.go:310` in metacubex/utls overwrites `hello.serverName` with the ECH config's `PublicName` when ECH is active; k2 does not override it). This is a client-side behavior, not a hard protocol rule — the metacubex server does **not** enforce that the sent outer `server_name` matches `PublicName` (`ech.go:610`), so a modified client could decouple the two. Decoupling is not free: see §12 and the design doc, because the server's active-probe camouflage (`learnOuterSNI` / `handleRawTCP`) only trusts this node's own `public_name`.

The `public_name` must be a domain that genuinely deploys ECH in production (e.g., `cloudflare-ech.com`). **Selection criterion:** pick the domain that is *currently un-blocked* ∩ *shares the largest real ECH population*. Cloudflare uses a single `public_name` for all ECH-enabled sites, so all legitimate Cloudflare ECH traffic shares this outer SNI value. **This deterrent only bites an outer-SNI-*value*-only filter, not an `SNI + IP` filter**: that legitimate crowd lives on Cloudflare's IPs, while our traffic is on plain VPS IPs, so a censor conditioning on `SNI==cloudflare-ech.com AND IP∉Cloudflare` separates us cleanly with zero collateral. `cloudflare-ech.com` is therefore the least-bad default, **not** a shield against IP-conditioned blocking — see the design doc §4–§5. **Do not pick an obscure domain**: a niche `public_name` has near-zero legitimate ECH population, which makes `SNI==<niche> AND IP∉CDN` a zero-collateral, cleanly-blockable signature — the opposite of blending in.

The inner SNI can be any value since certificate verification uses fingerprint pinning, not domain name matching.

### 3.2 ECH (Encrypted Client Hello)

k2v5 mandates ECH for all connections. The ECH config contains:

- **HPKE public key**: For encrypting the inner ClientHello
- **public_name**: Becomes the outer SNI (camouflage domain, e.g. `cloudflare-ech.com`)

ECH config is provided as a base64-encoded config list in the `ech` URL parameter. Both standard and URL-safe base64 encodings are accepted.

The server holds the corresponding HPKE private key (`tls.Config.EncryptedClientHelloKeys` in Go 1.24+) and decrypts the inner ClientHello during the TLS handshake.

#### ECH Config Forging

The server forges its ECH config to blend in with real Cloudflare ECH traffic:

1. **Query Cloudflare DNS**: Fetch the real HTTPS DNS record for `cloudflare-ech.com` to obtain the genuine ECH config
2. **Copy template fields**: Use the same `cipher_suites`, `kem_id`, and `public_name` from the real Cloudflare config
3. **Increment config_id**: Set `config_id = Cloudflare's config_id + 1` to avoid collision while staying close to the real value
4. **Substitute HPKE key**: Replace the public key with the server's own HPKE public key
5. **Multiple configs**: The server supports multiple ECH config entries for key rotation; older configs remain active during transition

This forging approach ensures that the ECH config structure and metadata (cipher suites, KEM, public_name) are indistinguishable from genuine Cloudflare ECH configs. Only the HPKE public key and config_id differ, which are not observable by passive censors since the ECH config is distributed out-of-band via the k2v5:// URL.

### 3.3 Certificate Pinning

k2v5 uses self-signed certificates verified by public key SHA-256 pinning:

- Server auto-generates dual certificates on first startup: RSA-2048 + ECDSA P-256 (10-year validity)
- The public key SHA-256 hashes are embedded in the `pin` URL parameter as a dual pin: `sha256:<rsa_urlsafe_b64>,sha256:<ec_urlsafe_b64>`
- Client verification: `InsecureSkipVerify: true` + custom `VerifyPeerCertificate` that checks `sha256(publicKey) in pins`
- Either pin matching is sufficient (server selects RSA or EC cert per-connection based on target site algorithm)
- No dependency on system CA store, no domain name matching, no Certificate Transparency exposure

**Why not Let's Encrypt / public CA:**
- Certificate Transparency logs are public — any cert issuance creates a permanent record linking the domain to the server, discoverable via crt.sh
- Self-signed certs have zero external footprint
- TLS 1.3 encrypts the certificate in transit — passive observers cannot see it
- Active probers are handled by ReverseProxy (Section 12) and never see the self-signed cert

**Key algorithm matching**: The server generates its self-signed cert using the same key algorithm (ECDSA-P256 / RSA-2048) as the `public_name` domain's real certificate. This ensures the encrypted CertificateVerify message has the same length as a real connection to that domain.

### 3.4 TLS Record Padding

TLS 1.3 encrypts all handshake messages after ServerHello. While content is hidden, encrypted record **lengths** are observable. A single self-signed cert (~500B) produces shorter records than a real multi-level CA chain (~3.8KB).

k2v5 uses TLS 1.3 record padding (RFC 8446 Section 5.4) to match the encrypted record lengths of the `public_name` domain:

1. On startup, server connects to `public_name:443` and measures the real Certificate message length
2. Server calculates `padding = real_cert_length - self_cert_length`
3. During TLS handshake, server pads the Certificate record to match
4. Padding is refreshed every 24 hours to track cert rotations on the target site

Client-side requires no awareness of padding — TLS 1.3 record padding removal is automatic per the standard.

### 3.5 TLS Fingerprinting

Connections use uTLS to mimic real browser TLS fingerprints:

| `fp` value | ClientHello ID |
|------------|----------------|
| `chrome` (default) | `HelloChrome_120_PQ` |
| `firefox` | `HelloFirefox_120` |
| `safari` | `HelloSafari_16_0` |
| `random` | `HelloRandomizedALPN` |

### 3.6 TLS Configuration

- Minimum version: TLS 1.3
- Certificate verification: fingerprint pinning (not CA chain)
- ECH: mandatory on client, decrypted on server

### 3.7 Server-Side ECH Routing

The server inspects the raw TLS ClientHello **before** performing the TLS handshake:

1. Peek the full ClientHello from the TCP stream (without consuming it)
2. Check for ECH extension (type `0xfe0d`) and, when present, read the plaintext outer `config_id` (offset 5 of the extension body: `[type:1][kdf:2][aead:2][config_id:1]`)
3. **ECH present, `config_id` owned by this node** → TLS handshake with ECH decryption → k2v5 handler
4. **No ECH** → ReverseProxy the raw TCP bytes to the `public_name` website
5. **ECH present, `config_id` NOT owned** (GREASE, forged, or foreign) → ReverseProxy to `public_name` (same as no ECH)

The routing decision happens at the TCP level, before any TLS state is established, so these connections never see the server's self-signed certificate.

> **Implementation note (2026-08-10).** Steps 3/5 were originally specified as "route on ECH *presence*, reverse-proxy on decryption failure." The presence-only routing shipped, but the decryption-failure branch did **not** — a `HasECH` connection went straight to a bare `Handshake()`, which completes with the self-signed cert on an undecryptable ECH (utls falls back to the outer hello), exposing it to a config-free active prober (or Chrome's GREASE ECH). It is now gated on the plaintext `config_id` (`server.echDisposition` / `ownsECHConfigID`), which catches GREASE and random-config probes before any TLS state. **Residual**: `config_id` is observable on the wire, so a prober replaying a real one still reaches the handler and completes the handshake; fully closing that requires an out-of-band HPKE decrypt (metacubex/utls keeps HPKE in `internal/`). See `docs/superpowers/specs/2026-08-10-outer-sni-camouflage-design.md` §9.

## 4. Authentication

Device auth rides the **connection metadata stream** (StreamType `0x10`, §5.x), not a
separate HTTP handshake. There is no ticket, no `/k2v5/auth` endpoint, and no
per-connection round-trip beyond the metadata stream itself. The same model and the
same `Validator`/`AuthVerdict` chain govern **both transports** (QUIC and TCP-WS) —
auth that only covered QUIC would let a client force the TCP-WS fallback to bypass it.

> **Historical note.** Spec v1.1 (2025-02) described a ticket flow (`POST /k2v5/auth`
> → `AuthTicket` → present ticket on streams). That flow was **never implemented
> end-to-end** — no client ever obtained a ticket and no stream ever required one —
> so it was removed in 2026-07 along with `AuthHandler`/`TicketStore`. Do not
> reintroduce it.

### 4.1 Auth Flow

```
Client                                Server
  |  Open metadata stream (0x10)        |
  |  MetadataFrame{udid, token, mode,   |
  |                version}             |
  |------------------------------------>|
  |                                     |  Validator(token, udid, mode) → AuthVerdict
  |  Stream response: OK / unauthorized |
  |<------------------------------------|
  |                                     |
  |  Open proxy stream(s) (TCP/UDP)     |  Admission gated by the metadata
  |------------------------------------>|  verdict (see 4.3 Enforcement)
```

The client sends the metadata stream first, before any proxy stream. Because a proxy
stream may still race ahead of the metadata stream on the server, the enforce gate
waits briefly (`authWaitDefault`, 500 ms) for the verdict before rejecting.

### 4.2 Metadata Frame

`MetadataFrame` (wire framing in §5.x), kv-extensible so old servers ignore unknown keys:

| Field | Required | Description |
|-------|----------|-------------|
| `udid` | yes | Device UDID (also the traffic-attribution key once a passing verdict lands — self-reported, not trusted otherwise) |
| `token` | yes | JWT authentication token (from the `k2v5://UDID:TOKEN@…` URL) |
| `mode` | no | `gateway` for router clients, `""` for normal clients |
| `version` | no | Client app version — **observation only, never an auth signal** (self-reported, spoofable) |

### 4.3 Enforcement

The server validates the metadata via a `Validator` (returning a four-state
`AuthVerdict` — OK / no opinion / explicit reject 401-403 / cannot determine -1) and
records the verdict per connection. **Admission is a property of the connection, not
of a stream**: one
metadata stream decides, and every proxy path on that connection lives or dies by
that verdict. Both transports share the implementation (`wire/admission.go`), and
the two things egress needs — a route to `AcceptTCP` and the server's `UDPHandler`
— are handed out only by the gate, so a path that does not ask cannot proxy.
Admission is governed by a single flag, `K2_ENFORCE_AUTH` (config
`auth.enforce_auth`, default **false**):

- **Permissive (default)** — an unauthenticated or unvalidated connection is still
  served, but attribution follows the server-side verdict, never the self-reported
  udid: only a passing verdict attributes the connection to its udid, otherwise it
  lands in the empty-udid bucket. The stream itself is never blocked. This is the
  rollout-safe default: legacy clients that send no/invalid metadata keep working
  until the flag is flipped.
- **Enforce** — proxy streams on a connection whose metadata failed (or never sent)
  validation are rejected; QUIC tears the connection, TCP-WS tears the smux session.

Flip to enforce only after fixed clients have propagated fleet-wide, else unupgraded
clients are cut off.

Every client-reachable egress entry is gated: QUIC stream TCP (`0x01`), stream UDP
(`0x02`), the client overflow stream (`0x03`), QUIC datagrams, and the TCP-WS
stream types. The echo stream (`0x04`) is deliberately **not** gated — it reflects
bytes inside the connection and never reaches the network, so it grants an
unauthenticated peer no egress, while gating it would add the admission wait to the
transport race's data-path probe.

### 4.4 Validator Chain

`Validator` (`wire/auth.go`) is resolved server-side as an ordered chain
(`ChainValidators`), wrapped in a success-only TTL cache (`CachingValidator`) so
opening auth to every connection does not hammer Center. The chain's semantics
follow the four-state `AuthVerdict`: the first `OK` wins immediately; a "no
opinion" verdict (`Code == 0`, the validator doesn't know this udid) is skipped;
an explicit rejection (`Code` 401/402/403 — only Center issues these) short-circuits
the chain right there; if every validator ran out with no explicit reject but at
least one returned "cannot determine" (`Code == -1`, e.g. Center unreachable), the
chain tail returns `Code: -1` rather than fail-closing; only when every validator
had no opinion at all does the tail fail-closed to `Code: 401`.

| Validator | Source |
|-----------|--------|
| `NewUsersFileValidator` | local `users_file` (`username:token_or_bcrypt`) |
| `NewRemoteValidator` | Center `POST /slave/device-check-auth`, authenticated with `Basic <nodeIPv4>:<NodeSecret>`; empty secret fails closed |
| accept-all fallback | only when no validator configured: `token != "" && udid != ""` |

## 5. Wire Framing

### 5.1 Varint Encoding (RFC 9000 Section 16)

All variable-length integers use QUIC varint encoding:

| Prefix (2 bits) | Length | Value Range |
|-----------------|--------|-------------|
| `00` | 1 byte | 0 - 63 |
| `01` | 2 bytes | 0 - 16,383 |
| `10` | 4 bytes | 0 - 1,073,741,823 |
| `11` | 8 bytes | 0 - 4,611,686,018,427,387,903 |

### 5.2 TCP Stream Header

When opening a proxy stream (QUIC stream or smux stream), the client writes:

```
[type:1][addr_len:varint][addr:addr_len bytes]
```

| Field | Size | Description |
|-------|------|-------------|
| `type` | 1 byte | `0x01` = TCP, `0x02` = UDP |
| `addr_len` | varint | Length of destination address |
| `addr` | variable | Destination address (e.g. `example.com:443`) |

### 5.3 TCP Stream Response

Server responds after processing the stream header:

**Success**:
```
[0x00]
```

**Error**:
```
[0x01][err_len:varint][err_msg:err_len bytes]
```

After a successful response, the stream becomes a bidirectional TCP pipe.

### 5.4 UDP Message Frame

Used on all UDP paths (QUIC datagram, QUIC stream, smux stream):

```
[session_id:4 BE][addr_len:varint][addr:addr_len bytes][payload:remaining]
```

| Field | Size | Description |
|-------|------|-------------|
| `session_id` | 4 bytes, big-endian | Session identifier (fixed offset for fast parsing) |
| `addr_len` | varint | Length of target address (0 for subsequent packets) |
| `addr` | variable | Target address (omitted when addr_len=0) |
| `payload` | remaining bytes | UDP payload |

**Subsequent packet optimization**: After the first packet establishes the session-to-address mapping, subsequent packets can set `addr_len=0` to omit the address entirely.

### 5.5 Smux-Framed UDP

For UDP over byte streams (smux), each UDP frame is length-prefixed:

```
[frame_len:varint][UDP message frame]
```

This enables multiplexing multiple UDP frames on a single stream.

### 5.6 H3 Custom Frame Type

QUIC proxy streams are tagged with a custom H3 frame type:

```
H3FrameTypeK2Proxy = 0x4B32  ("K2" in ASCII)
```

Written as a varint at the start of proxy streams to identify k2v5 traffic.

## 6. Transport: QUIC/H3 (Primary)

### 6.1 QUIC Configuration

| Parameter | Value |
|-----------|-------|
| Datagrams | Enabled |
| Max idle timeout | 30 seconds |
| Keep-alive period | 10 seconds |
| Max incoming streams | 256 |

### 6.2 TCP Proxying over QUIC

```
Client                            Server
  |  Open QUIC stream               |
  |  [H3 frame type: 0x4B32]        |
  |  [Stream header: TCP, addr]      |
  |--------------------------------->|
  |                                  | Connect to target
  |  [Stream response: 0x00 OK]      |
  |<---------------------------------|
  |                                  |
  |  <--- bidirectional data --->    |
  |                                  |
```

Each TCP proxy connection uses one QUIC stream. QUIC's native multiplexing eliminates the need for a custom mux layer.

### 6.3 UDP Proxying over QUIC

**Preferred: QUIC Datagrams** (unreliable, low-latency)

```
Client                            Server
  |  QUIC Datagram:                  |
  |  [UDP frame: sid, addr, payload] |
  |--------------------------------->|
  |                                  | Forward to target
  |  QUIC Datagram:                  |
  |  [UDP frame: sid, "", response]  |
  |<---------------------------------|
```

**Fallback: QUIC Stream** (reliable, for servers without datagram support)

```
Client                            Server
  |  Open QUIC stream                |
  |  [Stream header: UDP, addr]      |
  |  [Smux-framed UDP messages]      |
  |<---------- bidirectional ------->|
```

### 6.4 Port Hopping

When `hop=START-END` is configured, the client randomly selects a UDP port from `[START, END)` for each QUIC connection. All ports in the range must be forwarded to the server.

This provides resilience against per-port blocking.

## 7. Transport: TCP-WebSocket (Fallback)

### 7.1 When Used

TCP-WS is used when:
- QUIC is blocked (UDP firewall)
- QUIC connection fails

The `TransportManager` automatically falls back from QUIC to TCP-WS.

### 7.2 Smux Configuration

TCP-WS uses smux for stream multiplexing:

| Parameter | Value |
|-----------|-------|
| Keep-alive interval | 10 seconds |
| Keep-alive timeout | 30 seconds |
| Max frame size | 32 KB |
| Max receive buffer | 256 KB |
| Max stream buffer | 64 KB per stream |

### 7.3 TCP Proxying over TCP-WS

```
Client                            Server
  |  WebSocket connection            |
  |  [smux session]                  |
  |=================================>|
  |                                  |
  |  Open smux stream                |
  |  [Stream header: TCP, addr]      |
  |--------------------------------->|
  |  [Stream response: 0x00 OK]      |
  |<---------------------------------|
  |  <--- bidirectional data --->    |
```

### 7.4 UDP Proxying over TCP-WS

UDP over TCP-WS uses a dedicated smux stream with length-prefixed framing:

```
Client                            Server
  |  Open smux stream                |
  |  [Smux-framed UDP messages]      |
  |<---------- bidirectional ------->|
```

Each UDP message is prefixed with `[frame_len:varint]` to delineate messages on the byte stream.

## 8. Transport Manager

The `TransportManager` provides a unified `Dialer` interface over both transports:

```
         DialTCP / DialUDP
                |
        TransportManager
           /         \
     QUIC (try)    TCP-WS (fallback)
```

**Selection logic**:
1. Try QUIC transport
2. If QUIC fails or unavailable, try TCP-WS
3. If both fail, return `ErrNotConnected`

Both transports implement the same `Dialer` interface:

```go
type Dialer interface {
    DialTCP(ctx context.Context, addr string) (net.Conn, error)
    DialUDP(ctx context.Context, addr string) (net.PacketConn, error)
    Close() error
}
```

## 9. Server Interface

```go
type Listener interface {
    AcceptTCP(ctx context.Context) (conn net.Conn, addr string, err error)
    ServeUDP(ctx context.Context, handler UDPHandler) error
    Close() error
}

type UDPHandler interface {
    HandleUDP(conn net.PacketConn, addr string)
}
```

The server listener accepts TCP proxy requests and dispatches UDP sessions to handlers. A single listener handles both QUIC and TCP-WS clients.

## 10. UDP Session Management

### 10.1 Client-Side Session IDs

Session IDs are sequential uint32 values allocated per connection, starting at 1. The allocator is lock-free (atomic increment).

### 10.2 Server-Side Session Map

The server maintains a map of `session_id -> (addr, last_seen)`:

- **Max size**: Configurable (prevents memory exhaustion)
- **Idle TTL**: Configurable (default varies by deployment)
- **Cleanup**: Periodic sweep removes sessions idle longer than TTL

**Session lifecycle**:
1. First packet: `Store(session_id, addr)` — creates mapping
2. Subsequent packets: `Load(session_id)` to resolve addr, `Touch(session_id)` to update last-seen
3. Idle timeout: `Cleanup()` removes stale sessions

## 11. Congestion Control: PCC Vivace

### 11.1 Overview

k2v5 uses PCC (Performance-oriented Congestion Control) Vivace instead of standard QUIC congestion control (Cubic/NewReno). PCC uses gradient ascent on a utility function to find the optimal sending rate.

### 11.2 Utility Function

```
U = Throughput - alpha * LossRate * Throughput - beta * DeltaRTT
```

| Parameter | Default | GFW Mode | Description |
|-----------|---------|----------|-------------|
| `alpha` | 10.0 | 0.5 | Loss penalty coefficient |
| `beta` | 0.1 | 0.1 | Latency penalty coefficient |
| `epsilon` | 0.15 | 0.15 | Probing step size (15%) |

**GFW mode** (`AggressiveMode`): Low alpha tolerates non-congestion packet loss typical of censorship infrastructure.

**Adaptive alpha mode**: Alpha is automatically adjusted based on observed loss rate:
```
alpha = BASE_ALPHA / (1 + SENSITIVITY * avg_loss_rate)
```
With EWMA smoothing (30% weight to new value), clamped to `[0.1, 10.0]`.

### 11.3 Modes

| Mode | Behavior |
|------|----------|
| **Slow Start** | Multiply rate by 2.5x each MI until utility drops (>5% decrease exits) |
| **Decision** | Probe with +/- epsilon to determine rate direction |
| **Rate Adjusting** | Apply gradient ascent based on utility changes |

### 11.4 Monitor Intervals (MI)

MIs are the observation windows for rate decisions:

| Condition | Threshold |
|-----------|-----------|
| Standard completion | >= 10 ACKed packets + RTT samples |
| Relaxed completion | >= 3 ACKed packets + >= 30 seconds |
| Forced completion | >= 60 seconds |
| App-limited detection | Actual sending < 50% of target rate |

App-limited MIs are skipped for utility calculations.

### 11.5 Rate Boundaries

| Scenario | Minimum Rate |
|----------|-------------|
| Upload (maxRate < 30 Mbps) | 10 Mbps |
| Download (maxRate >= 30 Mbps) | 30 Mbps |

Maximum rate: Configurable per-connection (`maxRate` parameter).

Initial rate: 30% of maxRate (fast start).

### 11.6 Rate Probing

Every 10 MIs, the rate is probed upward by 20% to recover from previous reductions. This occurs before app-limited checks and resets utility counters.

### 11.7 Pacing

PCC uses rate-based pacing:
- Pacing budget accrues based on `currentRate / 8 * elapsed_time`
- Budget capped at congestion window to prevent bursts
- Packets sent only when budget > 0

## 12. Server Camouflage

### 12.1 Threat Model

| Adversary | Capability | Mitigation |
|-----------|-----------|------------|
| Passive observer | See TCP dest IP + outer SNI + encrypted record sizes | ECH hides inner SNI; TLS record padding matches real site; cert encrypted by TLS 1.3 |
| Active prober (no ECH) | Connect to server, examine response | ReverseProxy serves the real `public_name` website |
| Active prober (forged ECH) | Send fabricated ECH blob | ECH decryption fails → ReverseProxy (same as no-ECH) |
| IP-SNI cross-checker | Verify outer SNI resolves to dest IP | **Partial.** ReverseProxy answers the *active-probe* form ("is there really a `public_name` site here?") — it does **not** make the dest IP belong to `public_name`'s ASN. A censor conditioning on `outer SNI == cloudflare-ech.com AND dest IP ∉ Cloudflare's announced ranges` is a zero-collateral signature this layer cannot defeat; it is only survivable via the collateral-damage deterrent and/or a non-ECH transport. See `docs/superpowers/specs/2026-08-10-outer-sni-camouflage-design.md` §5. |
| CT log scanner | Search crt.sh for certificates | Self-signed cert has no CT entry — zero footprint |
| ECH-aware censor | Block all connections with ECH extension | Out of scope for protocol-level mitigation (requires transport obfuscation) |

### 12.2 Reverse Proxy

Non-ECH connections are transparently forwarded to the real `public_name` website at the TCP level. The server never performs a TLS handshake for these connections — it forwards the raw bytes including the original ClientHello:

```
Prober → TCP:443 → K2 Server → TCP → Real public_name Website
         (raw bytes forwarded, including TLS ClientHello)
```

The relay target is **not** taken from the prober's ClientHello. The server keeps a
whitelist of outer SNIs seen on successful ECH connections, and only its own ECH
`public_name` may enter it — a client picks its own outer SNI, so trusting it would
turn the node into an open TCP relay to an arbitrary host, on the one path that
never passes device auth. Legitimate clients are unaffected: uTLS forces their outer
SNI to `public_name` (§3.1).

This provides:

- **Active probing resistance**: Censors see a real website with a valid certificate
- **Certificate concealment**: The self-signed cert is never exposed to non-ECH clients
- **Behavioral consistency**: The server appears to be a reverse proxy / CDN for the `public_name` domain

The reverse proxy uses the qTunnel bidirectional pipe pattern with proper half-close.

### 12.3 ECH Routing Flow

```
TCP connection to :443
  │
  ├── Peek full ClientHello (before TLS)
  │     ├── Parse extensions list
  │     └── Check for ECH extension (0xfe0d)
  │
  ├── ECH extension present
  │     └── TLS handshake with ECH decryption
  │           ├── ECH decrypted OK → inner SNI revealed
  │           │     └── k2v5 handler
  │           │           ├── GET /k2v5/tunnel → WebSocket upgrade → smux tunnel
  │           │           └── other methods → connection closed
  │           └── ECH decryption failed (forged)
  │                 └── ReverseProxy to public_name
  │
  └── No ECH extension
        └── ReverseProxy to public_name (raw TCP)
```

### 12.4 Server Auto-Provisioning

On first startup (no certificate files found), the server automatically:

1. **Auto-generate dual certificates**: RSA-2048 + ECDSA P-256 self-signed certs (10-year validity)
2. **Query Cloudflare DNS**: Fetch the real HTTPS DNS record for `cloudflare-ech.com` to obtain the ECH config template
3. **Forge ECH config**: Copy cipher_suites, kem_id, public_name from Cloudflare; set config_id = Cloudflare's + 1; substitute own HPKE public key
4. **Probe public_name for TLS record padding**: Connect to `public_name:443`, measure certificate chain length and leaf key algorithm, calculate padding amounts
5. **Generate auth token**: Random token for initial client authentication
6. **Log the complete `k2v5://` URL**: Including dual pin (`sha256:<rsa>,sha256:<ec>`), ECH config, and auth token — admin copies this to distribute to users
7. **Persist to disk**: Save certs, HPKE private key, ECH configs, and auth token; reload on restart

On subsequent startups:

1. Load existing certs + HPKE key + ECH configs from disk
2. Re-probe `public_name` — refresh padding value
3. Start serving

At runtime:

- Every 24 hours, re-probe `public_name` to update padding if the target cert chain changes

## 13. IP Packet Framing (Core Layer)

Between the TUN device and the wire layer, IP packets use simple length-prefixed framing:

```
[length:2 BE][IP packet:length bytes]
```

- Length: uint16 big-endian
- Maximum packet size: 65,535 bytes (uint16 max)
- Used on the tunnel's bidirectional packet stream

## 14. Error Codes

| Error | Description |
|-------|-------------|
| `ErrNotConnected` | No transport available (both QUIC and TCP-WS down) |
| `ErrUDPNotSupported` | Server does not support UDP for this session |
| `ErrStreamRejected` | Server rejected the proxy stream request |
