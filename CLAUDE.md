# k2 — Kaitu Network Tunnel

Minimalist VPN tunnel with k2v5 composite wire protocol (QUIC+H3+PCC primary, TCP-WS+smux fallback).
Two binaries: `k2` (client) and `k2s` (server). Client has daemon architecture with HTTP localhost API.

Used as a read-only submodule in k2app. See parent `CLAUDE.md` for integration context.

## Quick Commands

```bash
go build -o k2 ./cmd/k2            # Build client
go build -o k2s ./cmd/k2s          # Build server
go test ./...                      # All tests (wire + core + dns + mobile + daemon + server + e2e)
go test -short ./...               # Fast: skip live DNS/network tests
make mobile-ios                    # iOS xcframework (gomobile bind)
make mobile-android                # Android aar (gomobile bind)
k2 setup <k2v5://URL>              # First-time: generate config + install service + start
k2 ctl up                          # Connect (uses saved config)
k2 ctl status                      # Connection status JSON
k2 ctl down                        # Disconnect
k2 service install                 # Install as system service
k2 service uninstall               # Remove system service
k2s setup                          # Server: install service + print connect URL
k2s -c server.yaml                 # Server with explicit config (foreground)
docker compose up --build           # Docker: server + init container (docker/)
```

## Code Style

- **Minimalist**: Avoid over-engineering, prefer concrete types over interfaces
- **Stdlib first**: `os.Args` + `flag.NewFlagSet` for CLI, `net/http` for HTTP
- **Error wrapping**: Always `fmt.Errorf("context: %w", err)`

## Layer Docs (read on demand)

```
cmd/k2/CLAUDE.md               Client CLI subcommands + dispatch
cmd/k2s/CLAUDE.md              Server CLI + foreground/service mode
config/CLAUDE.md               ClientConfig + ServerConfig + YAML formats
core/CLAUDE.md                 L4 ClientTunnel, Router, ConnectionHandler
core/dns/CLAUDE.md             DNS 13-step middleware pipeline
daemon/CLAUDE.md               Daemon state machine, HTTP API, service management
provider/CLAUDE.md             Provider interface (TUN, Proxy, iOS, Android)
server/CLAUDE.md               Server listener orchestration, TCP/UDP handlers
wire/CLAUDE.md                 k2v5 wire protocol, Dialer/Listener interfaces
wire/k2v5-protocol-spec.md    k2v5 protocol specification (TLS, ECH, camouflage, framing)
wire/pcc/CLAUDE.md             PCC Vivace congestion control
mobile/                        Mobile SDK (Engine, EventHandler, dnsHandler)
test/CLAUDE.md                 E2E integration tests
docs/features/                 Feature specs (cloud-webapp, mobile-sdk, private-ip-guard, etc.)
docs/knowledge/                Distilled patterns (architecture, bugfix, gotchas, testing, tasks)
docs/contracts/                API contracts (webapp-daemon-api)
```

## Data Flow

- DNS (L4/mobile): App -> TUN -> gVisor -> dnsHandler (port 53) -> Middleware.HandleQuery -> Cache/Filter/k2rule/TmpRule -> Upstream
- DNS (L3/desktop): App -> TUN -> DNS Middleware.HandleDNSQuery (raw IP) -> HandleQuery -> same pipeline
- Traffic: TUN -> gVisor stack -> ConnectionHandler -> Router -> Wire (QUIC/TCP-WS) -> Server
- Server: Wire -> Auth -> AcceptTCP/ServeUDP -> net.Dial -> Target
- Control: CLI -> HTTP 127.0.0.1:1777 -> Daemon -> Tunnel

## Architecture

**L4 Proxy Model**: Client uses gVisor stack (sing-tun) to convert TUN packets to L4 TCP/UDP connections. Server is simple userspace proxy (no TUN/NAT).

**Interfaces**:
- `wire.Dialer`: DialTCP, DialUDP, Close (implemented by QUICClient, TCPWSClient, TransportManager)
- `wire.Listener`: AcceptTCP, ServeUDP, Close (implemented by QUICServer, TCPWSServer)
- `provider.Provider`: Start(ctx, ConnectionHandler), Close, Name (TUN/Proxy/iOS/Android)
- `provider.ConnectionHandler`: HandleTCP(conn, dest), HandleUDP(conn, dest) (implemented by ClientTunnel, mobile.dnsHandler)
- `dns.DNSQuerier`: Query(*dns.Msg) (*dns.Msg, error) (implemented by DirectDNSClient, ProxyDNSClient)

**Binary Split**:
- `k2`: client (CLI, daemon, provider, core, DNS middleware)
- `k2s`: server (listener, auth, TCP/UDP handlers)
- `mobile/`: gomobile SDK (Engine + EventHandler + dnsHandler composition)
- Shared: wire/, config/, some daemon/ files

## Critical Gotchas

- sing-tun gVisor stack owns connections — handler must defer close, not close prematurely
- `ReadStreamHeader`/`ReadStreamResponse` require `io.ByteReader` — wrap `net.Conn` with `bufio.NewReader(conn)`
- `net.Pipe()` is synchronous — writes block until reads happen on other end
- TUN device requires root. `k2 setup` or `k2 service install` installs daemon as root via launchd/systemd/Windows Service
- QUIC datagrams may not be supported — wire.DialUDP falls back to stream-based UDP
- smux needs buffer tuning for VPN traffic (default 4MB too small, use 16MB)
- ECH requires X25519 keys (not P-256) — Go's crypto/tls only supports DHKEM(X25519)
- DNS queries need trailing dot (FQDN) — `crypto.cloudflare.com.` not `crypto.cloudflare.com`
- TLS wire formats are big-endian — always use `binary.BigEndian` for ECH/TLS parsing
- Base64 in URLs must use `base64.URLEncoding` — standard `+` decoded to space by `url.Query()`
- uTLS forces outer SNI = ECH config's `public_name` — cannot be overridden independently
- Server generates dual certs (RSA+EC) — both padded to match target site's cert length
- EC CA (`wire/kaitu_ca.pem`, disguised as DigiCert) can sign both RSA and EC server certs
- `tun_desktop.go` build tag: `(darwin && !ios) || (linux && !android) || windows` — must exclude android/ios

## Dependencies

- `github.com/kaitu-io/k2rule v0.5.0-go` — domain/IP routing rules with unified Init API + SetTmpRule (CacheDir required)
- `github.com/miekg/dns` — DNS message handling
- `github.com/sagernet/sing-tun` — TUN device with gVisor stack
- `github.com/apernet/quic-go` — QUIC with congestion control API
- `github.com/metacubex/utls` — TLS fingerprinting + ECH
- `github.com/xtaci/smux/v2` — TCP-WS stream multiplexing
- `golang.org/x/mobile` — gomobile bind (iOS xcframework + Android aar)

## TODOs

Tracked in `docs/todos/` with priority prefixes:

```
docs/todos/p0-*.md     Critical / next up
docs/todos/p1-*.md     Important
docs/todos/p2-*.md     Nice to have
```

## Status

**Done**: Full L4 architecture, wire protocol (QUIC+TCP-WS), DNS middleware (L3+L4), auth, ECH (DNS-based forging + config rotation), TLS record padding, dual certs (RSA+EC), client CA pool (system + Kaitu CA - Chinese CAs), PCC, client tunnel, server handlers, e2e tests, zero-config server startup, k2/k2s binaries, mobile SDK (gomobile, iOS+Android)
