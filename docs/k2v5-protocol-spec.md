# k2v5 Protocol Specification

Version: 1.0
Date: 2025-02-11

## 1. Overview

k2v5 is a composite tunnel protocol that combines QUIC/H3 (primary) and TCP-WebSocket (fallback) transports into a unified interface. It provides TCP and UDP proxying over encrypted connections with censorship resistance features including ECH (Encrypted Client Hello), TLS fingerprint mimicry, and UDP port hopping.

### Design Principles

- **Composite transport**: QUIC preferred, TCP-WS automatic fallback
- **Native multiplexing**: QUIC streams for TCP, QUIC datagrams for UDP (no custom mux layer)
- **ECH mandatory**: Real SNI never exposed in plaintext
- **PCC congestion control**: Rate-based algorithm optimized for lossy/censored networks
- **Single port**: QUIC and TCP-WS share the same server port via protocol detection

## 2. URL Format

```
k2v5://UDID:TOKEN@HOST:PORT?sni=OUTER_SNI&ech=ECH_CONFIG&fp=FINGERPRINT&hop=START-END
```

### Components

| Component | Required | Default | Description |
|-----------|----------|---------|-------------|
| `UDID` | Yes | - | Device unique identifier |
| `TOKEN` | Yes | - | JWT authentication token |
| `HOST` | Yes | - | Server hostname (also used as inner SNI) |
| `PORT` | Yes | - | Server port |
| `sni` | No | `www.microsoft.com` | Outer SNI (camouflage, visible in plaintext) |
| `ech` | Yes* | - | Base64-encoded ECH config list |
| `fp` | No | `chrome` | TLS fingerprint: `chrome`, `firefox`, `safari`, `random` |
| `hop` | No | disabled | UDP port hopping range: `START-END` |

*ECH is mandatory for connection establishment. Without it, the client refuses to connect.

### Example

```
k2v5://device-abc:eyJhbGciOi...@tunnel.example.com:443?sni=www.microsoft.com&ech=AEX...&fp=chrome&hop=20000-30000
```

## 3. TLS Layer

### 3.1 ECH (Encrypted Client Hello)

k2v5 mandates ECH for all connections. The outer SNI (visible to network observers) is a camouflage domain. The inner SNI (encrypted, carries the real server identity) is the `HOST` from the URL.

```
Outer SNI (plaintext):  www.microsoft.com  (camouflage)
Inner SNI (encrypted):  tunnel.example.com (real server)
```

ECH config is provided as a base64-encoded config list in the `ech` URL parameter. Both standard and URL-safe base64 encodings are accepted.

### 3.2 TLS Fingerprinting

Connections use uTLS to mimic real browser TLS fingerprints:

| `fp` value | ClientHello ID |
|------------|----------------|
| `chrome` (default) | `HelloChrome_120_PQ` |
| `firefox` | `HelloFirefox_120` |
| `safari` | `HelloSafari_16_0` |
| `random` | `HelloRandomizedALPN` |

### 3.3 TLS Configuration

- Minimum version: TLS 1.3
- Certificate verification: enabled (not skipped)

### 3.4 Server-Side SNI Routing

The server peeks at the outer SNI from incoming TLS ClientHello messages:

- **Matching ECH inner SNI**: Route to k2v5 tunnel handler
- **Non-matching SNI**: Reverse-proxy transparently to the outer SNI website (camouflage)

This makes the server indistinguishable from a real web server hosting the camouflage domain.

## 4. Authentication

### 4.1 Auth Flow

```
Client                          Server
  |                               |
  |  POST /k2v5/auth              |
  |  Authorization: Bearer <JWT>  |
  |  X-K2-Device: <UDID>          |
  |------------------------------>|
  |                               |  Validate JWT + UDID
  |  200 OK                       |
  |  {"ticket":"...","expires":N} |
  |<------------------------------|
  |                               |
  |  [Use ticket for streams]     |
  |                               |
  |  POST /k2v5/auth              |
  |  X-K2-Ticket: <ticket>        |  Ticket renewal
  |  X-K2-Device: <UDID>          |
  |------------------------------>|
  |                               |
```

### 4.2 Auth Endpoint

**`POST /k2v5/auth`**

Request headers:
- `X-K2-Device` (required): Device UDID
- `Authorization: Bearer <JWT>` (initial auth): JWT token
- `X-K2-Ticket: <ticket>` (renewal): Existing ticket

Response (`200 OK`):
```json
{
  "ticket": "hex-encoded-random-token",
  "expires": 3600,
  "udp": true,
  "version": 1
}
```

Error responses: `401 Unauthorized` with `{"error":"..."}`.

### 4.3 Ticket Lifecycle

- **TTL**: 1 hour (server-configurable)
- **Renewal**: Client presents existing ticket to get a new one
- **Grace period**: 30 seconds — when a new ticket is issued, the old ticket remains valid during this window to prevent race conditions during ticket rotation
- **Device binding**: One active ticket per device. Issuing a new ticket invalidates the previous one (moved to grace period)

### 4.4 Ticket Store

Server-side in-memory store with three maps:

| Map | Key | Value | Purpose |
|-----|-----|-------|---------|
| `byToken` | ticket token | `AuthTicket` | Active ticket lookup |
| `byDevice` | device UDID | current token | Enforce one-ticket-per-device |
| `graceTokens` | old token | grace expiry time | Allow old tickets during transition |

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

### 12.1 Reverse Proxy

Non-ECH connections (or connections with non-matching outer SNI) are transparently forwarded to the real website at the outer SNI address. This provides:

- **Active probing resistance**: Censors connecting to the server see a real website
- **Traffic pattern camouflage**: The server serves real HTTP traffic alongside tunnel traffic

The reverse proxy uses the qTunnel bidirectional pipe pattern with proper half-close:

```
Client <---> Server <---> Real Website (outer SNI)
```

### 12.2 Protocol Detection

The server reads the initial TLS ClientHello to extract the outer SNI:

1. Read first bytes of connection (TLS record header)
2. Parse TLS Handshake → ClientHello → Extensions → SNI
3. **ECH match**: Handle as k2v5 tunnel connection
4. **No match**: Reverse proxy to the outer SNI website

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
