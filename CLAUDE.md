# k2 — Kaitu Network Tunnel

Minimalist VPN tunnel with k2v5 composite wire protocol (QUIC+H3+PCC primary, TCP-WS+smux fallback).
Two binaries: `k2` (client) and `k2s` (server). Client has daemon architecture with HTTP localhost API.

Used as a read-only submodule in k2app. See parent `CLAUDE.md` for integration context.

## Quick Commands

```bash
go build -o k2 ./cmd/k2            # Build client
go build -o k2s ./cmd/k2s          # Build server
go test ./...                      # All tests (wire + core + dns + appext + daemon + server + e2e)
go test -short ./...               # Fast: skip live DNS/network tests
make appext-ios                    # iOS xcframework (gomobile bind)
make appext-android                # Android aar (gomobile bind)
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
sniff/CLAUDE.md                Protocol sniffing (TLS/QUIC/HTTP/BT/STUN/DTLS detectors)
cmd/k2/CLAUDE.md               Client CLI subcommands + dispatch
cmd/k2s/CLAUDE.md              Server CLI + foreground/service mode
config/CLAUDE.md               ClientConfig + ServerConfig + YAML formats
engine/CLAUDE.md               Unified tunnel lifecycle manager (state machine, EventHandler, Config assembly)
core/CLAUDE.md                 L4 ClientTunnel, Router, ConnectionHandler
core/dns/CLAUDE.md             DNS 13-step middleware pipeline
daemon/CLAUDE.md               Desktop platform shell: HTTP API, service management, auto-reconnect
provider/CLAUDE.md             Provider interface (TUN, Proxy, TProxy, iOS, Android)
server/CLAUDE.md               Server listener orchestration, TCP/UDP handlers
wire/CLAUDE.md                 k2v5 wire protocol, Dialer/Listener interfaces
wire/k2v5-protocol-spec.md    k2v5 protocol specification (TLS, ECH, camouflage, framing)
wire/arc/CLAUDE.md             PCC Vivace congestion control
appext/CLAUDE.md               App extension shell: gomobile SDK (macOS sysext, iOS, Android)
test/CLAUDE.md                 E2E integration tests
docs/features/                 Feature specs (cloud-webapp, mobile-sdk, private-ip-guard, etc.)
docs/knowledge/                Distilled patterns (architecture, bugfix, gotchas, testing, tasks)
docs/contracts/                API contracts (webapp-daemon-api)
```

## Architecture

**L4 Proxy Model**: Client uses gVisor stack (sing-tun) to convert TUN packets to L4 TCP/UDP connections. Server is simple userspace proxy (no TUN/NAT).

### Layer Model

```
┌─────────────────────────────────────────────────────────┐
│ Platform Shell (daemon/ appext/)                        │
│   职责: 平台入口 + Config 组装 + 平台特有功能              │
│   规则: 不管隧道状态——状态由 engine 管, shell 只透传        │
├─────────────────────────────────────────────────────────┤
│ Engine (engine/)                                        │
│   职责: 唯一的隧道生命周期管理者 (single source of truth)  │
│   拥有: state machine, EventHandler, error classification │
│   组装: wire → provider → tunnel → DNS middleware         │
├─────────────────────────────────────────────────────────┤
│ Core (core/ + core/dns/)                                │
│   职责: L4 数据面 + DNS pipeline (无状态, 只处理连接)      │
├─────────────────────────────────────────────────────────┤
│ Provider (provider/)                                    │
│   职责: 平台 packet I/O 抽象 (TUN/Proxy → L4 connections)│
├─────────────────────────────────────────────────────────┤
│ Wire (wire/)                                            │
│   职责: k2v5 协议传输 (Dialer/Listener)                  │
└─────────────────────────────────────────────────────────┘
```

### Platform Call Chains

```
Desktop:   CLI → HTTP → daemon.Daemon ──[builds Config]──→ engine.Engine.Start()
App ext:   Native → appext.Engine ──[builds Config]──→ engine.Engine.Start()
                                                           ↓
                                                      engine assembles:
                                                      k2rule → wire → provider → tunnel → DNS
```

Platform Shell (daemon/appext) assembles `engine.Config` with platform-specific fields (DirectDialer, NetworkMonitor for desktop; FileDescriptor for appext), then delegates all lifecycle to engine. Engine doesn't know or care which platform called it.

### Interfaces

- `wire.Dialer`: DialTCP, DialUDP, Close (implemented by QUICClient, TCPWSClient, TransportManager)
- `wire.Listener`: AcceptTCP, ServeUDP, Close (implemented by QUICServer, TCPWSServer)
- `wire.DirectDialer`: DialContext, ListenPacket (implemented by daemon.directDialer, appext.protectedDialer)
- `wire.DialerController`: ControlFunc() (optional, for DNS socket protection — implemented by appext.protectedDialer)
- `wire.StatsProvider`: WireStats() (optional, type-asserted on Dialer — implemented by QUICClient, TCPWSClient)
- `provider.Provider`: Start(ctx, ConnectionHandler), Close, Name (TUN/Proxy/TProxy/iOS/Android)
- `provider.ConnectionHandler`: HandleTCP(conn, dest), HandleUDP(conn, dest) (implemented by ClientTunnel, dnsHandler)
- `engine.EventHandler`: OnStatus(Status), OnStats(tx, rx) (implemented by daemon, appext.eventBridge)
- `engine.NetworkChangeNotifier`: Start(callback func()) error, Close() error (implemented by daemon.singTunMonitor, appext.nePlatform)
- `dns.DNSQuerier`: Query(ctx, *dns.Msg) (*dns.Msg, error) (implemented by DirectDNSClient, ProxyDNSClient)

### Binary Split

- `k2`: client (CLI, daemon, provider, core, DNS middleware) — Linux/Windows desktop
- `k2s`: server (listener, auth, TCP/UDP handlers)
- `appext/`: gomobile SDK (Engine + EventHandler bridge — gomobile can't export dependency interfaces) — macOS sysext, iOS, Android
- Shared: wire/, config/, engine/

## Data Flow

- DNS (all TUN): App → TUN → gVisor → dnsHandler (port 53 TCP+UDP) → Middleware.HandleQuery → Cache/Filter/k2rule/dedup → Upstream
- Traffic: TUN → gVisor stack → ConnectionHandler → Router → Wire (QUIC/TCP-WS) → Server
- Server: Wire → Auth → AcceptTCP/ServeUDP → net.Dial → Target
- Control: CLI → HTTP 127.0.0.1:1777 → Daemon → Engine

## Resource Ownership Rules

> **谁创建，谁销毁。Borrower 只用不关。**

Engine creates provider, transport, and tunnel during `Start()`. Engine closes them during `Stop()`. Tunnel borrows provider and dialer references but **never closes them** — it only manages its own resources (context, WaitGroup, connTracker).

```
engine.Stop()
  ├─ healthMonitor.stop()// Engine owns health monitor → stop goroutine first (reads tm/tunnel)
  ├─ prov.Close()       // Engine owns provider → Engine closes it
  ├─ tm.Close()         // Engine owns transport → Engine closes it
  └─ tunnel.Close()     // Tunnel only drains its own goroutines
     ├─ cancel()        // Tunnel owns context
     ├─ conns.closeAll()// Tunnel owns connTracker
     └─ wg.Wait()       // Tunnel owns WaitGroup
```

| Resource | Owner (create + destroy) | Borrower (use only) |
|----------|--------------------------|---------------------|
| provider | Engine | Tunnel, dnsHandler |
| transport/dialer | Engine | Tunnel |
| healthMonitor | Engine | — |
| ctx + wg + connTracker | Tunnel | — |
| handler goroutines | Tunnel | — |
| networkMonitor | Engine | — |

**Why this order matters**: Health monitor stops first (its goroutine reads tm and tunnel). Provider closes next (stops new TUN packets), then transport (drops wire connections), then tunnel (drains in-flight handlers). Tunnel's `connTracker.closeAll()` force-closes any connections stuck in `pipe()`/`pipePacket()` so `wg.Wait()` returns promptly.

**Violation pattern to avoid**: `tunnel.Close()` must never call `provider.Close()` or `dialer.Close()`. This causes double-close panics (e.g. sing-tun `StopFD.Stop()` on macOS).

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
- tproxy provider (Linux only) manages nftables rules — cleanup on Close() is critical
- `tun.Options.DNSServers` must NOT be set — it configures OS DNS settings, not packet interception. sing-tun auto-derives `TUN_IP.Next()`. DNS hijack is application-level (dnsHandler checks port 53)
- `ProviderConfig.RouteExcludeAddrs` (NOT DNSServers) excludes IPs from TUN routing — prevents DirectDNSClient query loops

## Dependencies

- `github.com/kaitu-io/k2rule v0.6.4` — domain/IP routing rules with unified Init API + SetTmpRule (CacheDir required, non-blocking background download, Antiporn opt-in)
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

**Done**: Full L4 architecture, wire protocol (QUIC+TCP-WS), DNS middleware (L3+L4), auth, ECH (DNS-based forging + config rotation), TLS record padding, dual certs (RSA+EC), client CA pool (system + Kaitu CA - Chinese CAs), PCC, client tunnel, server handlers, e2e tests, zero-config server startup, k2/k2s binaries, appext SDK (gomobile, macOS sysext+iOS+Android), tproxy provider (OpenWRT/Linux), OpenWRT service management (init.d/procd), wire observability (K2ARC metrics + TX/RX counting + HealthMonitor FSM + embedded /stats dashboard), protocol sniffing (TLS SNI, QUIC SNI, HTTP Host, BitTorrent, STUN, DTLS), Happy Eyeballs (RFC 8305 IPv6/IPv4 address selection)

## Platform Mapping

| Platform | Entry | Shell | Provider | DirectDialer | NetworkMonitor |
|----------|-------|-------|----------|--------------|----------------|
| macOS sysext | xcframework | appext/ | tun (FD) | nePlatform | nePlatform |
| iOS | xcframework | appext/ | tun (FD) | nePlatform | nePlatform |
| Android | aar | appext/ | tun (FD) | protectedDialer | nil (OS calls OnNetworkChanged) |
| Linux desktop | cmd/k2 | daemon/ | tun (self-create) | daemon.directDialer | singTunMonitor |
| Windows | cmd/k2 | daemon/ | tun (self-create) | daemon.directDialer | singTunMonitor |
| OpenWRT | cmd/k2 | daemon/ | tproxy | nil | nil |
