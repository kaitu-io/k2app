# k2tunnel - VPN Tunnel

Minimalist VPN tunnel with k2v5 composite wire protocol (QUIC+H3+PCC primary, TCP-WS+smux supplementary).

## Quick Commands

```bash
# Build
go build -o k2client ./cmd/k2client
go build -o k2server ./cmd/k2server

# Test (NOTE: Requires sudo for TUN device)
go test ./core/...          # Packet framing + DNS tests (no sudo needed)
go test ./wire/...          # Wire protocol tests (91 tests, no sudo needed)

# Run (requires sudo)
sudo ./k2client ...
sudo ./k2server ...
```

## Architecture (4 Layers)

```
Provider Layer (platform-specific)
  ├─ provider/tun_unix.go - TUN device (sing-tun library)
  └─ provider/provider.go - Interface

Core Layer (platform-independent)
  ├─ core/tunnel.go  - ClientTunnel/ServerTunnel (bidirectional forwarding)
  ├─ core/router.go  - Routing decisions (uses k2rule.Target)
  ├─ core/direct.go  - Direct connection bypass
  └─ core/packet/    - Packet processing (independent package)
      ├─ packet.go      - IP packet framing [length:2][packet]
      └─ packet_test.go - Packet tests

DNS Layer (comprehensive middleware)
  ├─ core/dns/middleware.go - 13-step DNS pipeline (~92% filter rate)
  ├─ core/dns/cache.go      - Dynamic IP cache (TTL-based)
  ├─ core/dns/dns_cache.go  - DNS message cache
  ├─ core/dns/filter.go     - RFC 6761 + noise filtering
  ├─ core/dns/hosts.go      - /etc/hosts integration
  ├─ core/dns/metrics.go    - DNS statistics
  ├─ core/dns/direct.go     - Direct DNS (114.114.114.114)
  └─ core/dns/proxy.go      - Proxy DNS (8.8.8.8 via tunnel)

Wire Layer (k2v5 composite protocol — 91 tests)
  ├─ wire/wire.go          - Dialer/Listener/UDPHandler interfaces, Config, URL parsing
  ├─ wire/auth.go          - Auth ticket store (1h TTL, 30s grace), HTTP auth handler
  ├─ wire/ech.go           - ECH config, utls fingerprint, SNI peek, reverse proxy
  ├─ wire/stream.go        - QUIC varint, stream header, H3 frame type 0x4B32
  ├─ wire/udp.go           - UDP frame, session allocator, session map, smux framing
  ├─ wire/quic.go          - QUIC config, stream conn, QUICClient/Server, port hopping
  ├─ wire/tcpws.go         - smux config, TCPWSClient/Server
  ├─ wire/transport.go     - TransportManager (QUIC preferred, TCP-WS fallback)
  └─ wire/pcc/pcc.go       - PCC Vivace congestion control (866 lines)

Entry Points
  ├─ cmd/k2client/main.go - Client CLI (-wire URL flag)
  └─ cmd/k2server/main.go - Server CLI (-listen, -protocol, -psk flags)
```

**Data Flow**:
- DNS: App → TUN → DNS Middleware (13 steps) → Cache/Filter/k2rule → Upstream DNS → Response
- Traffic: TUN → Router → Wire (k2v5: QUIC preferred, TCP-WS fallback) → Network

## Key Files

- `core/tunnel.go` - Core logic: ClientTunnel & ServerTunnel (expects wire.Dialer/wire.Listener)
- `core/router.go` - Routing with k2rule integration
- `core/dns/middleware.go` - DNS 13-step pipeline
- `core/packet/packet.go` - IP packet framing utilities
- `provider/tun_unix.go` - TUN device (sing-tun library)
- `config/config.go` - YAML config parsing
- `wire/wire.go` - Dialer/Listener interfaces, k2v5 URL parsing
- `wire/transport.go` - TransportManager (QUIC preferred, TCP-WS fallback)
- `wire/stream.go` - Stream proxy protocol, QUIC varint, H3 frame type
- `wire/auth.go` - Auth ticket store + HTTP handler
- `docs/plans/2026-02-11-k2v5-composite-protocol-design.md` - k2v5 protocol design (1410 lines)

## k2rule Integration

- **Version**: `github.com/kaitu-io/k2rule v0.1.1-go`
- **Purpose**: Domain/IP routing rules (CN → direct, foreign → proxy)
- **Initialization**: `k2rule.InitWithFallback(k2rule.TargetProxy)` in main.go
- **Router usage**: `k2rule.MatchIP(ip)` and `k2rule.MatchDomain(domain)`
- **Types**: Uses `k2rule.Target` (TargetDirect/TargetProxy/TargetReject)
- **TODO**: Load rules from file/CDN (currently using default fallback)

## Dependencies

- `github.com/kaitu-io/k2rule v0.1.1-go` - Domain/IP routing rules
- `github.com/miekg/dns` - DNS message handling
- `github.com/quic-go/webtransport-go` - WebTransport over QUIC
- `github.com/sagernet/sing-tun` - TUN device (cross-platform)
- `golang.org/x/crypto` - Crypto primitives
- `golang.org/x/net` - Network utilities
- `gopkg.in/yaml.v3` - Config parsing
- `github.com/apernet/quic-go` - QUIC with congestion control API (for PCC)
- `github.com/metacubex/utls` - TLS fingerprinting + ECH
- `github.com/xtaci/smux/v2` - TCP-WS stream multiplexing

## Gotchas

### core/tunnel.go Needs L4 Rewrite
`core/tunnel.go` uses old L3 interfaces (`wire.Dialer.Dial`, `wire.Listener.Accept`) that no longer exist. The k2v5 wire layer uses L4 proxy interfaces (`DialTCP`/`DialUDP`/`AcceptTCP`/`ServeUDP`). The tunnel needs rewriting for the new model.

### Wire Stream Readers Need bufio.NewReader
`ReadStreamHeader` and `ReadStreamResponse` require `io.ByteReader`. Wrap `net.Conn` with `bufio.NewReader(conn)` before calling — `net.Conn` doesn't implement `io.ByteReader`.

### net.Pipe() Is Synchronous
`net.Pipe()` writes block until reads happen on the other end. In tests, structure goroutines carefully to avoid deadlocks — always read before writing when both ends are in the same test.

### TUN Device Auto-Allocation
Device names are automatically allocated by the OS (e.g., `utun0`, `utun1`).
**No configuration needed** - IP addresses (198.18.0.7/15), MTU (1400), and device name are all automatic.

### TUN Device Requires Root
All operations involving TUN devices require `sudo`:
```bash
sudo ./k2client ...  # Not optional
sudo go test ./...   # Only for tests that create TUN devices
```

### IP Packet Framing Format
Wire protocol uses simple length-prefixed framing:
```
[2 bytes length][IP packet data]
```
Big-endian uint16 for length. See `core/packet.go`.

### DNS Middleware Pipeline
DNS queries go through a 13-step pipeline with ~92% filter rate:
1. Hosts file lookup (/etc/hosts)
2. RFC 6761 special-use domains (.local, .invalid)
3. Local domains (.lan, .internal)
4. Noise filtering (PTR, AAAA, WPAD, service discovery)
5. DNS message cache (~32% hit rate)
6. k2rule domain routing (CN → direct, foreign → proxy)
7. Upstream DNS query (direct: 114.114.114.114, proxy: 8.8.8.8)
8. IP cache update for packet routing

**Important**: Direct DNS uses raw UDP sockets (bypasses TUN), Proxy DNS goes through tunnel.

### Server NAT Configuration
Server won't route packets without NAT:
```bash
sudo sysctl -w net.ipv4.ip_forward=1
sudo iptables -t nat -A POSTROUTING -s 198.18.0.0/15 -o eth0 -j MASQUERADE
sudo sysctl -w net.ipv6.conf.all.forwarding=1
```

## Testing

### Unit Tests (No sudo)
```bash
go test ./core/...          # All core tests
go test ./core/dns/...      # DNS middleware tests (45+ tests)
go test ./core/packet/...   # Packet processing tests
```

### Wire Tests (No sudo)
```bash
go test ./wire/...          # All wire protocol tests (91 tests)
go test ./wire/ -run TestQUICLoopback  # QUIC loopback tests
go test ./wire/ -run TestSmuxLoopback  # smux loopback tests
go test ./wire/ -run TestIntegration   # Integration tests
go test ./wire/pcc/...     # PCC congestion control (26 tests)
```

**Test Coverage**:
- DNS middleware: 45+ tests (cache, filter, hosts, metrics)
- Packet processing: 3 tests (UDP/IP framing roundtrip)
- Wire protocol: 65 tests (interfaces, auth, ECH, stream, UDP, QUIC, TCP-WS, transport, integration)
- PCC Vivace: 26 tests (congestion control algorithm)

### Integration Tests (Requires sudo)
Not yet implemented. Manual testing:
1. Run server with test cert
2. Run client pointing to server
3. Verify DNS: `nslookup google.com`
4. Verify traffic: `ping 8.8.8.8`

## Code Style

- **Minimalist philosophy**: Avoid over-engineering, prefer concrete types over interfaces
- **Standard library first**: Use Go stdlib before external deps
- **No unnecessary abstractions**: Router uses `*Router` not interface (after k2rule integration)
- **Concrete types**: Use `k2rule.Target` directly, not custom Action types
- **Error wrapping**: Always use `fmt.Errorf("context: %w", err)`

## Development Workflow

1. **Make changes** to provider/core/wire layers
2. **Run tests**: `go test ./...` (non-TUN tests)
3. **Build binaries**: `go build ./cmd/...`
4. **Manual test** with server + client (requires sudo)

## Implemented Features

- Domain-based routing (k2rule: CN → direct, foreign → proxy)
- DNS middleware with comprehensive caching (~92% filter rate)
- IP-based routing (dynamic cache from DNS)
- TUN device auto-allocation (macOS/Linux)
- Core tunnel logic (ClientTunnel/ServerTunnel bidirectional forwarding)
- Packet framing (length-prefixed IP packets)
- k2v5 wire protocol (QUIC/H3 + TCP-WS/smux composite transport)
- Auth ticket mechanism (JWT full auth + ticket auth, 1h TTL)
- ECH (Encrypted Client Hello) + utls fingerprinting
- PCC Vivace congestion control
- Stream proxy protocol (TCP/UDP framing)
- TransportManager (QUIC preferred, TCP-WS hot standby)

## Not Implemented

- **L4 tunnel rewrite** (core/tunnel.go needs update from L3 VPN to L4 proxy model)
- **Full QUIC client/server** (dial with utls+ECH+PCC, H3 auth, StreamHijacker)
- **Full TCP-WS client/server** (utls+ECH, WebSocket upgrade, smux session)
- GeoIP-based routing (currently domain-based only)
- iOS/Android support (mobile VPN services)
- Windows support (TAP adapter)
- Auto-reconnection on disconnect
- Traffic statistics/monitoring
- Rule file loading from CDN (currently uses fallback)
