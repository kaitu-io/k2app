# L4 Architecture: k2/k2s Split + Full Stack Integration

## Meta

| Field | Value |
|-------|-------|
| Feature | l4-architecture |
| Date | 2026-02-12 |
| Status | implemented |

## 1. Problem

k2 has a complete wire protocol layer (91 tests) and a working daemon/CLI shell, but the pieces are not connected:

- `core/tunnel.go` uses old L3 VPN model (`Dial(ctx)` / `Accept()`) — wire layer is L4 (`DialTCP`/`DialUDP`)
- `QUICClient`/`TCPWSClient` are empty shells — no `Dialer` implementation
- `QUICServer`/`TCPWSServer` are empty shells — no `Listener` implementation
- `daemon.doUp()` is a stub — sets state to "connected" without creating any tunnel
- Client and server share one binary but have very different runtime needs

## 2. Architecture Decisions (Confirmed)

### 2.1 L4 Proxy Model (not L3 VPN)

Wire layer is already L4 with tested protocol. Going back to L3 would:
- Discard stream.go/udp.go protocol work
- Require server-side TUN + NAT + IP forwarding + root
- Introduce head-of-line blocking (all traffic on one stream)

L4 keeps the server as a simple userspace TCP/UDP proxy.

### 2.2 gVisor Netstack for Client-Side Connection Tracking

Use sing-tun's gVisor stack mode to convert TUN IP packets into TCP/UDP connections.
This is proven by sing-box/clash. Provider interface changes from `io.ReadWriteCloser` to callback model.

DNS loop avoidance: sing-tun routing exclusions + protect socket (same mechanism needed for both L3 and L4).

### 2.3 Binary Split: k2 (Client) + k2s (Server)

Two separate binaries with shared libraries:

**k2** (client — end user machine):
```
k2 up [URL or config.yaml]     Connect tunnel
k2 down                        Disconnect
k2 status                      Show status
k2 speedtest                   Speed test
k2 run                         Start client daemon (called by service manager)
k2 run -c <config.yaml>        Client foreground (dev/debug, needs sudo)
k2 version                     Version info
```

**k2s** (server — VPS):
```
k2s run                        Start server daemon (called by service manager)
k2s run -s <server.yaml>       Server foreground mode
k2s run --install               Install as system service
k2s status                     Show server status (connections, throughput)
k2s version                    Version info
```

### 2.4 Delete/Replace Decisions

| File | Action | Reason |
|------|--------|--------|
| `core/tunnel.go` | **Delete, rewrite** | L3 model, entire file unusable |
| `core/packet.go` WritePacket/ReadPacket | **Delete** | L3 framing, replaced by wire stream/udp protocol |
| `core/direct.go` DirectSender | **Replace** | L4 "direct" = `net.Dial()`, no raw socket needed |
| `core/dns/proxy.go` ProxyDNSClient | **Adapt** | Change `wireConn net.Conn` to `wire.Dialer` |
| Protocol spec Section 13 | **Remove** | "IP Packet Framing" is L3, no longer applies |

### 2.5 Keep As-Is

| Component | Status |
|-----------|--------|
| `wire/wire.go` interfaces | Done |
| `wire/stream.go` protocol | Done + tested |
| `wire/udp.go` protocol | Done + tested |
| `wire/auth.go` TicketStore + AuthHandler | Done + tested |
| `wire/ech.go` ECH + SNI peek + ReverseProxy | Done + tested |
| `wire/transport.go` TransportManager | Done + tested |
| `wire/pcc/` PCC congestion control | Done + tested |
| `core/router.go` Router | Done |
| `core/dns/` DNS middleware (except proxy.go) | Done + tested |
| `core/packet/` IP packet parser | Done + tested (still needed for gVisor integration) |
| `config/config.go` | Done |
| `daemon/` (client.go, api.go, state.go, process.go, service*.go, recovery.go) | Done + tested |

## 3. New Provider Interface

Current:
```go
type Provider interface {
    io.ReadWriteCloser
    Name() string
    MTU() int
}
```

New (gVisor stack callback model):
```go
type Provider interface {
    Start(ctx context.Context, handler ConnectionHandler) error
    Close() error
    Name() string
}

type ConnectionHandler interface {
    HandleTCP(conn net.Conn, dest string)       // e.g. dest = "93.184.216.34:443"
    HandleUDP(conn net.PacketConn, dest string)  // e.g. dest = "8.8.8.8:53"
}

type ProviderConfig struct {
    FileDescriptor int           // 0 = create TUN; >0 = use existing FD (iOS/Android)
    DNSServers     []netip.Addr  // excluded from TUN route (anti-loop)
    MTU            int           // default 1400
}
```

### Platform TUN Sources

All platforms use the same sing-tun gVisor stack for L3→L4 conversion. Differences are only in how the TUN device is obtained:

| Platform | TUN Source | Build Tag | Notes |
|----------|-----------|-----------|-------|
| macOS | sing-tun creates | `darwin && !ios` | AutoRoute via system routing table |
| Linux | sing-tun creates | `linux` | AutoRoute + auto_redirect |
| OpenWRT | sing-tun creates | `linux` | sing-tun has built-in nftables openwrt rules |
| Windows | sing-tun creates | `windows` | strict_route for DNS leak prevention |
| iOS | FD from NEPacketTunnelProvider | `ios` | `Options.FileDescriptor`, `underNetworkExtension=true` |
| Android | FD from VpnService.Builder | `android` | `Options.FileDescriptor` + protect socket |

### Client Run Modes

Besides TUN mode, support **proxy mode** (local SOCKS5/HTTP proxy, no root needed):

```yaml
# client.yaml
server: "k2v5://..."
mode: tun          # "tun" (default) or "proxy"
proxy:
  listen: "127.0.0.1:1080"    # SOCKS5 + HTTP mixed proxy
  set_system_proxy: true       # auto-set OS system proxy settings
```

| Mode | Root | Capture | Implementation |
|------|------|---------|----------------|
| `tun` | Yes | All system traffic | TUN + gVisor → ConnectionHandler |
| `proxy` | **No** | Apps respecting proxy settings | SOCKS5/HTTP server → ConnectionHandler |

Both modes use the same `ConnectionHandler` interface — the Provider is the only difference.
Proxy mode is implemented as `ProxyProvider` in `provider/proxy.go` (pure Go, no platform dependencies).

## 4. New Client Tunnel Architecture

```
┌──────────────────────────────────────────────────┐
│                  Client (k2)                     │
│                                                  │
│  TUN Device ──► gVisor Netstack ──► Handler      │
│                                      │           │
│                    ┌─────────────────┤           │
│                    │                 │           │
│              HandleTCP          HandleUDP        │
│                    │                 │           │
│              ┌─────▼──────┐   ┌─────▼──────┐    │
│              │   Router   │   │   Router   │    │
│              │  (k2rule)  │   │  (k2rule)  │    │
│              └──┬──────┬──┘   └──┬──────┬──┘    │
│           direct│ proxy│   direct│ proxy│        │
│                 │      │        │      │        │
│           net.Dial  wire.     net.  wire.       │
│                  DialTCP     Dial  DialUDP      │
│                    │                 │           │
│              TransportManager                   │
│              QUIC ──► TCP-WS fallback           │
└──────────────────────────────────────────────────┘
```

DNS interception: When HandleUDP sees dest port 53, route to DNS middleware instead of wire.

## 5. New Server Architecture

```
┌──────────────────────────────────────────────────┐
│                  Server (k2s)                    │
│                                                  │
│  :443 ◄── TLS ClientHello peek                  │
│              │                                   │
│        ┌─────┴──────┐                           │
│    ECH match     No match                       │
│        │             │                           │
│   k2v5 handler   ReverseProxy                   │
│        │         (camouflage)                    │
│   ┌────┴────┐                                   │
│   │  Auth   │  POST /k2v5/auth                  │
│   │ Handler │  TicketStore                      │
│   └────┬────┘                                   │
│        │                                        │
│   QUIC Listener + TCP-WS Listener               │
│        │                                        │
│   ┌────┴────┐  ┌────────────┐                   │
│   │AcceptTCP│  │ ServeUDP   │                   │
│   │ stream  │  │ datagram/  │                   │
│   │ header  │  │ stream     │                   │
│   └────┬────┘  └─────┬──────┘                   │
│        │             │                          │
│   net.Dial(addr)  net.Dial(addr)               │
│   bidirectional   forward UDP                   │
│     forward       packets                       │
└──────────────────────────────────────────────────┘
```

## 6. Shared vs Split Code

```
Shared (used by both k2 and k2s):
  wire/          — protocol, interfaces, auth, ech, stream, udp
  config/        — ClientConfig, ServerConfig, YAML
  daemon/        — only: client.go (IPC), rlimit, recovery

Client only (k2):
  cmd/k2/        — client CLI
  provider/      — TUN device (gVisor stack)
  core/          — tunnel, router, dns middleware, direct
  daemon/        — daemon.go, api.go, state.go, process.go, service*.go, speedtest.go

Server only (k2s):
  cmd/k2s/       — server CLI
  server/        — server daemon, listener orchestration
```

## 7. Implementation Priority

Phase 1 — Wire Implementation (no integration, just fill empty shells):
  1. QUICClient implements Dialer (DialTCP, DialUDP, Close)
  2. TCPWSClient implements Dialer (DialTCP, DialUDP, Close)
  3. QUICServer implements Listener (AcceptTCP, ServeUDP, Close)
  4. TCPWSServer implements Listener (AcceptTCP, ServeUDP, Close)

Phase 2 — Provider Rewrite:
  5. New Provider interface with gVisor netstack + ConnectionHandler
  6. tun_unix.go rewrite using sing-tun stack mode

Phase 3 — Client L4 Tunnel:
  7. Delete old tunnel.go, write new L4 client tunnel
  8. Adapt DNS middleware (ProxyDNSClient uses wire.Dialer)
  9. Replace DirectSender with net.Dial
  10. daemon.doUp()/doDown() wiring

Phase 4 — Server:
  11. cmd/k2s/ binary + server daemon
  12. Server listener orchestration (TLS peek → auth → QUIC/TCP-WS listener)

Phase 5 — Binary Split:
  13. Move cmd/k2 to client-only, create cmd/k2s
  14. Refactor daemon/ into shared + client-only parts

## 8. Acceptance Criteria

- `go build ./cmd/k2 && go build ./cmd/k2s` — both compile
- `go test ./...` — all existing tests pass + new tests
- `k2 run -c client.yaml` connects to `k2s run -s server.yaml`
- TCP proxy: curl through tunnel works
- UDP proxy: DNS resolution through tunnel works
- QUIC primary with TCP-WS fallback verified
- ECH + TLS fingerprint working
