# server — Server Daemon and Handlers

Server-side listener orchestration and TCP/UDP proxy handlers.

## Files

- `server.go` — Server struct, TLS listener, ECH routing, lifecycle
- `handler.go` — TCP/UDP proxy handlers (AcceptTCP → net.Dial → pipe)
- `*_test.go` — unit tests for TLS peek, handlers

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

## Key Types

```go
type Server struct {
    listener   net.Listener
    ticketStore *wire.TicketStore
    quicServer  *wire.QUICServer
    tcpwsServer *wire.TCPWSServer
    reverseProxy *wire.ReverseProxy
}
```

## Lifecycle

1. `Listen()` — start TLS listener on configured port
2. Accept TCP connections
3. Peek ClientHello SNI:
   - ECH inner SNI match → k2v5 handler
   - No match → reverse proxy (camouflage)
4. k2v5 handler:
   - Auth: POST /k2v5/auth → issue ticket
   - QUIC: handle QUIC connections (AcceptTCP/ServeUDP)
   - TCP-WS: handle WebSocket+smux (AcceptTCP/ServeUDP)
5. TCP handler: read stream header → net.Dial(target) → bidirectional pipe
6. UDP handler: read frames → net.Dial("udp", target) → forward packets
7. `Close()` — graceful shutdown with connection draining

## Error Handling

- TLS handshake failures → log + close (no panic)
- Auth failures → 401 response
- Dial failures → close stream (client sees broken pipe)
- Forward errors → close both ends

## Testing

- `server_test.go` — TLS peek routing, ECH vs non-ECH
- `handler_test.go` — TCP/UDP proxy with mock targets
- `test/e2e_test.go` — full integration with real client
