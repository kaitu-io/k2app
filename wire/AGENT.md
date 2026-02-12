# Wire Layer (wire/ — k2v5 composite protocol)

91 tests. QUIC/H3 primary, TCP-WS/smux fallback.

## Interfaces (wire.go)

```go
// Client transport (L4 proxy model)
type Dialer interface {
    DialTCP(ctx context.Context, addr string) (net.Conn, error)
    DialUDP(ctx context.Context, addr string) (net.PacketConn, error)
    Close() error
}

// Server transport
type Listener interface {
    AcceptTCP(ctx context.Context) (conn net.Conn, addr string, err error)
    ServeUDP(ctx context.Context, handler UDPHandler) error
    Close() error
}

type UDPHandler interface {
    HandleUDP(conn net.PacketConn, addr string)
}
```

## Config (wire.go)

`Config` parsed from `k2v5://udid:token@host:port?sni=...&ech=...&fp=...&hop_start=...&hop_end=...`

Fields: UDID, Token, Host, Port, SNI, ECH, Fingerprint, HopStart, HopEnd.

## Files

- `wire.go` — Dialer/Listener/UDPHandler interfaces, Config, `ParseURL()`
- `auth.go` — `TicketStore` (1h TTL, 30s grace), `AuthHandler` (JWT full + ticket)
- `ech.go` — ECH config parsing, utls fingerprint selection, SNI peek, reverse proxy
- `stream.go` — QUIC varint encoding, `StreamHeader` (type 0x4B32 H3 frame), request/response
- `udp.go` — `UDPFrame` wire format, `SessionAllocator`, `SessionMap`, smux framing
- `quic.go` — QUIC config, `StreamConn` adapter, `QUICClient`/`QUICServer`, port hopping
- `tcpws.go` — smux config, `TCPWSClient`/`TCPWSServer`
- `transport.go` — `TransportManager`: sets QUIC/TCP-WS, `DialTCP` prefers QUIC with TCP-WS fallback

## Gotchas

- `ReadStreamHeader`/`ReadStreamResponse` need `io.ByteReader` — wrap conn with `bufio.NewReader(conn)`.
- `net.Pipe()` is synchronous — structure goroutines carefully in tests.

## Testing

```bash
go test ./wire/...    # 91 tests (unit + integration)
```
