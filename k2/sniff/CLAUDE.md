# Sniff Layer (sniff/)

Protocol detection and domain extraction from first packet/bytes. Used by `core/tunnel.go` for routing overrides.

## Types

- `Result` — Protocol metadata from sniffing: `Protocol` (constant) + `Domain` (extracted hostname)
- Protocol constants: `ProtoTLS`, `ProtoHTTP`, `ProtoQUIC`, `ProtoBitTorrent`, `ProtoSTUN`, `ProtoDTLS`

## Detectors

| Function | Protocol | Input | Returns | Used for |
|----------|----------|-------|---------|----------|
| `SniffTLS([]byte)` | TLS ClientHello | TCP peek | SNI hostname | DIRECT→PROXY upgrade (port 443) |
| `SniffHTTP([]byte)` | HTTP/1.x | TCP peek | Host header value | DIRECT→PROXY upgrade (non-443) |
| `IsQUIC([]byte)` | QUIC long header | UDP packet | bool | Protocol detection (O(1), no decrypt) |
| `SniffQUIC([]byte)` | QUIC Initial | UDP packet | SNI, QUICClient | Domain extraction + client fingerprinting (expensive, decrypts) |
| `IsBitTorrent(io.Reader)` | BT handshake | TCP peek | bool | PROXY→DIRECT |
| `IsUTP([]byte)` | uTP (BT UDP) | UDP packet | bool | PROXY→DIRECT |
| `IsUDPTracker([]byte)` | BT UDP Tracker | UDP packet | bool | PROXY→DIRECT |
| `IsSTUN([]byte)` | STUN (RFC 8489) | UDP packet | bool | Identify WebRTC and **keep it on the tunnel** (ticket #3051) |
| `IsDTLS([]byte)` | DTLS (RFC 6347) | UDP packet | bool | Identify WebRTC and **keep it on the tunnel** (ticket #3051) |

## Files

- `result.go` — `Result` struct and protocol constants
- `tls.go` — TLS ClientHello SNI extraction
- `quic.go` — `IsQUIC` (O(1) protocol check) + `SniffQUIC` (SNI + client fingerprinting)
- `quic_client.go` — QUIC client identification (Chromium/Firefox/Safari/quic-go/unknown)
- `ja3.go` — JA3-style ClientHello parser (`QUICClientHello` struct for fingerprinting)
- `qtls.go` — Shared TLS/QUIC ClientHello parser
- `http.go` — HTTP/1.x Host header extraction (zero-alloc hot path)
- `bittorrent.go` — BT handshake, uTP, UDP Tracker detection
- `stun.go` — STUN magic cookie check (0x2112A442)
- `dtls.go` — DTLS record header validation

## Performance

All detectors are designed for hot-path use (per-connection):
- STUN/DTLS: ~3ns, 0 allocs
- HTTP: ~120ns, 1 alloc (returned string)
- TLS/QUIC: parse-heavy but single-pass

## Testing

```bash
go test ./sniff/...    # Unit tests + benchmarks
go test -bench=. ./sniff/...    # Run benchmarks
```
