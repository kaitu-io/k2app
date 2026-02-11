# k2 — Kaitu Network Tunnel

Minimalist VPN tunnel with k2v5 composite wire protocol (QUIC+H3+PCC primary, TCP-WS+smux supplementary).
Single binary `k2` with subcommands, daemon architecture, HTTP localhost API.

## Quick Commands

```bash
# Build
go build -o k2 ./cmd/k2

# Test (no sudo needed)
go test ./core/...          # Packet framing + DNS tests
go test ./wire/...          # Wire protocol tests (91 tests)
go test ./daemon/...        # Daemon API tests (14 tests)

# Run
k2 up k2v5://udid:token@host:443?sni=www.microsoft.com   # Connect (auto-installs daemon)
k2 status                                                  # Check status
k2 down                                                    # Disconnect
sudo k2 run -c client.yaml                                 # Client foreground (dev mode)
k2 run -s server.yaml                                      # Server foreground (VPS)
```

## Architecture

```
CLI Layer (cmd/k2/)
  ├─ main.go          Subcommand dispatch
  ├─ cmd_up.go        k2 up (IPC client + auto-install daemon)
  ├─ cmd_down.go      k2 down
  ├─ cmd_status.go    k2 status
  ├─ cmd_speedtest.go k2 speedtest
  ├─ cmd_run.go       k2 run (daemon / -c client / -s server)
  └─ cmd_version.go   k2 version

Daemon Layer (daemon/)
  ├─ daemon.go         State machine, HTTP server on 127.0.0.1:1777
  ├─ api.go            POST /api/core + GET /ping + GET /metrics + CORS
  ├─ client.go         IPC client (used by CLI)
  ├─ speedtest.go      Async speedtest engine
  ├─ state.go          Persisted state (auto-reconnect + saved config)
  ├─ process.go        PID monitoring (auto-disconnect on parent exit)
  ├─ process_unix.go   Unix implementation
  ├─ recovery.go       Panic recovery + network restore
  ├─ recovery_unix.go  Unix implementation
  ├─ rlimit.go         Resource limits (4000 procs, 10240 files)
  ├─ rlimit_unix.go    Unix implementation
  ├─ service.go        ServiceManager (Install/Uninstall/IsInstalled)
  ├─ service_darwin.go macOS launchd plist (io.kaitu.k2)
  └─ service_linux.go  Linux systemd unit (k2.service)

Config Layer (config/)
  └─ config.go         ClientConfig + ServerConfig (separate types)

Provider Layer (provider/)
  ├─ provider.go       Interface
  └─ tun_unix.go       TUN device (sing-tun library)

Core Layer (core/)
  ├─ tunnel.go         ClientTunnel/ServerTunnel (bidirectional forwarding)
  ├─ router.go         Routing decisions (uses k2rule.Target)
  ├─ direct.go         Direct connection bypass
  └─ packet/           Packet processing (independent package)

DNS Layer (core/dns/)
  ├─ middleware.go     13-step DNS pipeline (~92% filter rate)
  ├─ cache.go          Dynamic IP cache (TTL-based)
  ├─ dns_cache.go      DNS message cache
  ├─ filter.go         RFC 6761 + noise filtering
  ├─ hosts.go          /etc/hosts integration
  ├─ metrics.go        DNS statistics
  ├─ direct.go         Direct DNS (114.114.114.114)
  └─ proxy.go          Proxy DNS (8.8.8.8 via tunnel)

Wire Layer (wire/ — k2v5 composite protocol, 91 tests)
  ├─ wire.go           Dialer/Listener/UDPHandler interfaces, Config, URL parsing
  ├─ auth.go           Auth ticket store (1h TTL, 30s grace), HTTP auth handler
  ├─ ech.go            ECH config, utls fingerprint, SNI peek, reverse proxy
  ├─ stream.go         QUIC varint, stream header, H3 frame type 0x4B32
  ├─ udp.go            UDP frame, session allocator, session map, smux framing
  ├─ quic.go           QUIC config, stream conn, QUICClient/Server, port hopping
  ├─ tcpws.go          smux config, TCPWSClient/Server
  ├─ transport.go      TransportManager (QUIC preferred, TCP-WS fallback)
  └─ pcc/pcc.go        PCC Vivace congestion control (866 lines)
```

**Data Flow**:
- DNS: App → TUN → DNS Middleware (13 steps) → Cache/Filter/k2rule → Upstream DNS → Response
- Traffic: TUN → Router → Wire (k2v5: QUIC preferred, TCP-WS fallback) → Network
- Control: CLI/Tauri → HTTP 127.0.0.1:1777 → Daemon → Tunnel

## CLI Design

```
k2 up <URL or config.yaml> [--pid PID]   Connect tunnel (auto-manages daemon)
k2 up                                    Use last saved config
k2 down                                  Disconnect tunnel
k2 status                                Show connection status
k2 speedtest [--server-id ID]            Run speed test

k2 run                                   Daemon mode (service entry point)
k2 run -c <config.yaml>                  Client foreground (dev, needs sudo)
k2 run -s <server.yaml>                  Server foreground (VPS deployment)

k2 version                               Version info
```

## Daemon API

Listen: `127.0.0.1:1777`, Protocol: HTTP JSON

### POST /api/core

| Action | Params | Description |
|--------|--------|-------------|
| `up` | `wire_url` or `config_path`, `pid` | Connect tunnel |
| `down` | — | Disconnect tunnel |
| `reconnect` | — | Reconnect with saved config |
| `status` | — | Get state + connection info |
| `speedtest` | `server_id` | Start async speedtest |
| `get_speedtest_status` | — | Get speedtest progress |
| `get_config` | — | Get saved config |
| `version` | — | Version info |

### Other Endpoints

- `GET /ping` — Health check
- `GET /metrics` — Runtime metrics (goroutines, heap, GC)

## Config

### Client (client.yaml)
```yaml
server: "k2v5://udid:token@host:443?sni=www.microsoft.com&ech=AEX05..."
dns:
  direct: ["114.114.114.114:53", "223.5.5.5:53"]
  proxy: ["8.8.8.8:53", "1.1.1.1:53"]
router:
  default: proxy
log:
  level: info
```

### Server (server.yaml)
```yaml
listen: ":443"
tls:
  cert: /path/to/cert.pem
  key: /path/to/key.pem
pcc: gfw
rate: 100000000
auth:
  remote_url: "https://auth.example.com/check"
  cache_ttl: 5m
log:
  level: info
```

## Key Files

- `cmd/k2/main.go` - CLI entry point + subcommand dispatch
- `daemon/daemon.go` - Daemon state machine + HTTP server
- `daemon/api.go` - API handlers + CORS middleware
- `daemon/client.go` - IPC client for CLI → daemon communication
- `config/config.go` - ClientConfig + ServerConfig types
- `core/tunnel.go` - ClientTunnel & ServerTunnel (old L3, needs rewrite)
- `core/router.go` - Routing with k2rule integration
- `core/dns/middleware.go` - DNS 13-step pipeline
- `wire/wire.go` - Dialer/Listener interfaces, k2v5 URL parsing
- `wire/transport.go` - TransportManager (QUIC preferred, TCP-WS fallback)

## k2rule Integration

- **Version**: `github.com/kaitu-io/k2rule v0.1.1-go`
- **Purpose**: Domain/IP routing rules (CN → direct, foreign → proxy)
- **Initialization**: `k2rule.InitWithFallback(k2rule.TargetProxy)` in main.go
- **Router usage**: `k2rule.MatchIP(ip)` and `k2rule.MatchDomain(domain)`
- **Types**: Uses `k2rule.Target` (TargetDirect/TargetProxy/TargetReject)

## Dependencies

- `github.com/kaitu-io/k2rule v0.1.1-go` - Domain/IP routing rules
- `github.com/miekg/dns` - DNS message handling
- `github.com/sagernet/sing-tun` - TUN device (cross-platform)
- `github.com/apernet/quic-go` - QUIC with congestion control API (for PCC)
- `github.com/metacubex/utls` - TLS fingerprinting + ECH
- `github.com/xtaci/smux/v2` - TCP-WS stream multiplexing
- `gopkg.in/yaml.v3` - Config parsing

## Gotchas

### core/tunnel.go Needs L4 Rewrite
`core/tunnel.go` uses old L3 interfaces (`wire.Dialer.Dial`, `wire.Listener.Accept`) that no longer exist. The k2v5 wire layer uses L4 proxy interfaces (`DialTCP`/`DialUDP`/`AcceptTCP`/`ServeUDP`). The tunnel needs rewriting for the new model. Until then, `k2 run -c` and `k2 run -s` foreground modes are stubs.

### Wire Stream Readers Need bufio.NewReader
`ReadStreamHeader` and `ReadStreamResponse` require `io.ByteReader`. Wrap `net.Conn` with `bufio.NewReader(conn)` before calling.

### net.Pipe() Is Synchronous
`net.Pipe()` writes block until reads happen on the other end. Structure goroutines carefully in tests.

### TUN Device Auto-Allocation
Device names are automatically allocated by the OS (e.g., `utun0`, `utun1`).
IP addresses (198.18.0.7/15), MTU (1400), and device name are all automatic.

### TUN Device Requires Root
```bash
sudo k2 run -c client.yaml   # Foreground client needs sudo
# k2 up auto-installs daemon as root via launchd/systemd
```

### DNS Middleware Pipeline
DNS queries go through a 13-step pipeline with ~92% filter rate.
**Important**: Direct DNS uses raw UDP sockets (bypasses TUN), Proxy DNS goes through tunnel.

### Server NAT Configuration
```bash
sudo sysctl -w net.ipv4.ip_forward=1
sudo iptables -t nat -A POSTROUTING -s 198.18.0.0/15 -o eth0 -j MASQUERADE
```

## Testing

```bash
go test ./wire/...          # Wire protocol (91 tests)
go test ./wire/pcc/...      # PCC congestion control (26 tests)
go test ./core/packet/...   # Packet processing (3 tests)
go test ./core/dns/...      # DNS middleware (45+ tests, some need network)
go test ./daemon/...        # Daemon API (14 tests)
```

**Total**: 150+ tests (wire: 91, DNS: 45+, daemon: 14, packet: 3)

## Code Style

- **Minimalist philosophy**: Avoid over-engineering, prefer concrete types over interfaces
- **Standard library first**: Use Go stdlib before external deps
- **CLI**: `os.Args` + `flag.NewFlagSet`, no external framework
- **HTTP**: Go stdlib `net/http`, no external framework
- **Error wrapping**: Always use `fmt.Errorf("context: %w", err)`

## Implemented Features

- Single binary `k2` with subcommands (up/down/status/speedtest/run/version)
- Daemon architecture with HTTP API on 127.0.0.1:1777
- System service installation (macOS launchd / Linux systemd)
- CORS support for Tauri desktop app
- PID monitoring (auto-disconnect when parent process exits)
- Auto-reconnect on daemon restart (persisted state, <1h)
- Panic recovery with network restoration
- Resource limits (RLIMIT_NPROC, RLIMIT_NOFILE)
- Async speedtest with polling
- Domain-based routing (k2rule: CN → direct, foreign → proxy)
- DNS middleware with comprehensive caching (~92% filter rate)
- k2v5 wire protocol (QUIC/H3 + TCP-WS/smux composite transport)
- Auth ticket mechanism (JWT full auth + ticket auth, 1h TTL)
- ECH (Encrypted Client Hello) + utls fingerprinting
- PCC Vivace congestion control

## Not Implemented

- **L4 tunnel rewrite** (core/tunnel.go needs update from L3 VPN to L4 proxy model)
- **Full QUIC client/server** (dial with utls+ECH+PCC, H3 auth, StreamHijacker)
- **Full TCP-WS client/server** (utls+ECH, WebSocket upgrade, smux session)
- Actual speedtest implementation (stub only)
- GeoIP-based routing (currently domain-based only)
- iOS/Android support
- Windows support
- Traffic statistics/monitoring
- Rule file loading from CDN
