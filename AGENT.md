# k2 — Kaitu Network Tunnel

Minimalist VPN tunnel with k2v5 composite wire protocol (QUIC+H3+PCC primary, TCP-WS+smux fallback).
Two binaries: `k2` (client) and `k2s` (server). Client has daemon architecture with HTTP localhost API.

## Quick Commands

```bash
go build -o k2 ./cmd/k2            # Build client
go build -o k2s ./cmd/k2s          # Build server
go test ./...                      # All tests (242 wire + core + dns + daemon + server + e2e)
go test -short ./...               # Fast: skip live DNS/network tests
sudo k2 run -c client.yaml         # Client foreground (dev)
k2s run                            # Server zero-config (auto-generates certs, ECH, auth)
k2s run -s server.yaml             # Server with explicit config
docker compose up --build           # Docker: server + init container (docker/)
```

## Code Style

- **Minimalist**: Avoid over-engineering, prefer concrete types over interfaces
- **Stdlib first**: `os.Args` + `flag.NewFlagSet` for CLI, `net/http` for HTTP
- **Error wrapping**: Always `fmt.Errorf("context: %w", err)`

## Layer Docs (read on demand)

```
cmd/k2/AGENT.md                Client CLI subcommands + dispatch
cmd/k2s/AGENT.md               Server CLI + foreground/service mode
config/AGENT.md                ClientConfig + ServerConfig + YAML formats
core/AGENT.md                  L4 ClientTunnel, Router, ConnectionHandler
core/dns/AGENT.md              DNS 13-step middleware pipeline
daemon/AGENT.md                Daemon state machine, HTTP API, service management
provider/AGENT.md              Provider interface (TUN, Proxy, iOS, Android)
server/AGENT.md                Server listener orchestration, TCP/UDP handlers
wire/AGENT.md                  k2v5 wire protocol, Dialer/Listener interfaces
wire/k2v5-protocol-spec.md    k2v5 protocol specification (TLS, ECH, camouflage, framing)
wire/tlspad/AGENT.md           TLS record padding for fingerprint mimicry
wire/pcc/AGENT.md              PCC Vivace congestion control
test/AGENT.md                  E2E integration tests
```

## Data Flow

- DNS: App -> TUN -> DNS Middleware (13 steps) -> Cache/Filter/k2rule -> Upstream
- Traffic: TUN -> gVisor stack -> ConnectionHandler -> Router -> Wire (QUIC/TCP-WS) -> Server
- Server: Wire -> Auth -> AcceptTCP/ServeUDP -> net.Dial -> Target
- Control: CLI -> HTTP 127.0.0.1:1777 -> Daemon -> Tunnel

## Architecture

**L4 Proxy Model**: Client uses gVisor stack (sing-tun) to convert TUN packets to L4 TCP/UDP connections. Server is simple userspace proxy (no TUN/NAT).

**Interfaces**:
- `wire.Dialer`: DialTCP, DialUDP, Close (implemented by QUICClient, TCPWSClient, TransportManager)
- `wire.Listener`: AcceptTCP, ServeUDP, Close (implemented by QUICServer, TCPWSServer)
- `provider.Provider`: Start(ctx, ConnectionHandler), Close, Name (TUN/Proxy/iOS/Android)
- `provider.ConnectionHandler`: HandleTCP(conn, dest), HandleUDP(conn, dest) (implemented by ClientTunnel)

**Binary Split**:
- `k2`: client (CLI, daemon, provider, core, DNS middleware)
- `k2s`: server (listener, auth, TCP/UDP handlers)
- Shared: wire/, config/, some daemon/ files

## Critical Gotchas

- sing-tun gVisor stack owns connections — handler must defer close, not close prematurely
- `ReadStreamHeader`/`ReadStreamResponse` require `io.ByteReader` — wrap `net.Conn` with `bufio.NewReader(conn)`
- `net.Pipe()` is synchronous — writes block until reads happen on other end
- TUN device requires root (`sudo k2 run -c`). `k2 up` auto-installs daemon as root via launchd/systemd
- QUIC datagrams may not be supported — wire.DialUDP falls back to stream-based UDP
- smux needs buffer tuning for VPN traffic (default 4MB too small, use 16MB)
- ECH requires X25519 keys (not P-256) — Go's crypto/tls only supports DHKEM(X25519)
- DNS queries need trailing dot (FQDN) — `crypto.cloudflare.com.` not `crypto.cloudflare.com`
- TLS wire formats are big-endian — always use `binary.BigEndian` for ECH/TLS parsing
- Base64 in URLs must use `base64.URLEncoding` — standard `+` decoded to space by `url.Query()`
- uTLS forces outer SNI = ECH config's `public_name` — cannot be overridden independently
- Server generates dual certs (RSA+EC) — both padded to match target site's cert length
- EC CA (`wire/kaitu_ca.pem`, disguised as DigiCert) can sign both RSA and EC server certs

## Dependencies

- `github.com/kaitu-io/k2rule v0.3.0-go` — domain/IP routing rules with unified Init API
- `github.com/miekg/dns` — DNS message handling
- `github.com/sagernet/sing-tun` — TUN device with gVisor stack
- `github.com/apernet/quic-go` — QUIC with congestion control API
- `github.com/metacubex/utls` — TLS fingerprinting + ECH
- `github.com/xtaci/smux/v2` — TCP-WS stream multiplexing

## TODOs

Tracked in `docs/todos/` with priority prefixes:

```
docs/todos/p0-*.md     Critical / next up
docs/todos/p1-*.md     Important
docs/todos/p2-*.md     Nice to have
```

## Status

**Done**: Full L4 architecture, wire protocol (QUIC+TCP-WS), DNS middleware, auth, ECH (DNS-based forging + config rotation), TLS record padding, dual certs (RSA+EC), client CA pool (system + Kaitu CA - Chinese CAs), PCC, client tunnel, server handlers, e2e tests, zero-config server startup, k2/k2s binaries
