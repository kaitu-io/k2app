# Plan: L4 Architecture — k2/k2s Split + Full Stack Integration

## Meta

| Field | Value |
|-------|-------|
| Feature | l4-architecture |
| Spec | docs/features/l4-architecture/spec.md |
| Date | 2026-02-12 |
| Complexity | complex |

## AC Mapping

| AC | Test | Task |
|----|------|------|
| AC1: `go build ./cmd/k2 && go build ./cmd/k2s` | TestBuild_BothBinaries | F1, T6, T7 |
| AC2: `go test ./...` — all existing + new tests pass | all test suites | all tasks |
| AC3: `k2 run -c` connects to `k2s run -s` | TestE2E_ClientServerConnect | T8 |
| AC4: TCP proxy (curl through tunnel) | TestE2E_TCPProxy | T8 |
| AC5: UDP/DNS resolution through tunnel | TestE2E_UDPProxy_DNS | T8 |
| AC6: QUIC primary with TCP-WS fallback | TestQUIC_DialTCP, TestTCPWS_DialTCP, TestTransportManager_Failover | T1, T2 |
| AC7: ECH + TLS fingerprint working | TestE2E_ECH_Fingerprint | T1, T8 |
| AC8: iOS/Android Provider compiles + accepts FD | TestIOSProvider_FromFD, TestAndroidProvider_FromFD | T3 |
| AC9: Proxy mode works without root | TestProxyProvider_SOCKS5Connect, TestProxyProvider_HTTPConnect | T3 |
| AC10: Config `mode: proxy` with defaults | TestConfig_ProxyMode | T3 |

## Dependency Graph

```
F1 (clean L3 dead code)
 │
 ├──► T1 (QUIC Dialer+Listener)  ──┬──► T4 (L4 client tunnel) ──► T6 (daemon wiring) ──► T8 (E2E)
 │                                  │         ▲                         ▲
 ├──► T2 (TCP-WS Dialer+Listener) ─┤         │                         │
 │                                  ├──► T7 (k2s server binary) ────────┘
 └──► T3 (Provider: all platforms) ─┘
           desktop + iOS + Android            │
                                              │
      T5 (DNS middleware L4 adapt) ───────────┘
           depends on: T1
```

Parallel groups:
- **Group A** (after F1): T1, T2, T3 — all independent, can run in parallel
- **Group B** (after Group A): T4, T5, T7 — T4 needs T1+T2+T3; T5 needs T1; T7 needs T1+T2
- **Group C** (after Group B): T6 needs T4+T5; then T8 needs T6+T7

## Foundation Tasks

### F1: Clean Up Dead L3 Code

**Scope**: Remove broken L3 code that prevents compilation. Clear the way for L4 implementation.
**Branch**: `f1-clean-l3-dead-code`
**Files**:
- `core/tunnel.go` — DELETE entirely (old L3 ClientTunnel/ServerTunnel)
- `core/packet.go` — DELETE entirely (WritePacket/ReadPacket L3 framing)
- `core/packet_test.go` — DELETE (tests WritePacket/ReadPacket only)
- `core/direct.go` — DELETE entirely (DirectSender raw socket)
**Depends on**: none
**TDD**:
- RED: `go build ./...` fails with `t.dialer.Dial undefined` (current state)
- GREEN: Delete the 3 files + test file. Verify no other code imports them.
- REFACTOR: Update `core/AGENT.md` to reflect deletions. Update `AGENT.md` status.
**Acceptance**:
- `go build ./...` succeeds (no compilation errors)
- `go test ./...` passes (all 91 wire tests + daemon tests + dns tests still pass)
- No code references deleted types (`ClientTunnel`, `ServerTunnel`, `WritePacket`, `ReadPacket`, `DirectSender`)

---

## Feature Tasks

### T1: QUIC Dialer + Listener Implementation

**Scope**: Fill QUICClient with DialTCP/DialUDP/Close (implements wire.Dialer). Fill QUICServer with AcceptTCP/ServeUDP/Close (implements wire.Listener). This is the primary transport.
**Branch**: `t1-quic-dialer-listener`
**Files**:
- `wire/quic.go` — add methods to QUICClient + QUICServer
- `wire/quic_test.go` — unit tests for new methods
- `wire/integration_test.go` — update existing integration tests to use real implementations
**Depends on**: [F1]
**TDD**:
- RED: Write `TestQUICClient_DialTCP` — create QUIC client+server, client dials TCP, expects stream with header. Write `TestQUICClient_DialUDP` — client dials UDP, expects datagram/stream path. Write `TestQUICServer_AcceptTCP` — server accepts, reads stream header, returns addr. Write `TestQUICServer_ServeUDP` — server receives UDP frames, dispatches to handler.
- GREEN: Implement QUICClient: `Dial()` opens QUIC connection (with TLS+ECH), `DialTCP()` opens stream + writes H3 frame type + stream header + reads response, `DialUDP()` returns PacketConn backed by QUIC datagrams (fallback: QUIC stream). Implement QUICServer: `Listen()` accepts QUIC connections, `AcceptTCP()` accepts streams + reads header + returns conn/addr, `ServeUDP()` reads datagrams + dispatches to UDPHandler. Include port hopping in client Dial.
- REFACTOR: Ensure QUICClient satisfies `wire.Dialer` (compile-time check: `var _ Dialer = (*QUICClient)(nil)`). Same for QUICServer + `wire.Listener`. Factor out shared QUIC connection management if needed.
**Acceptance**:
- `var _ Dialer = (*QUICClient)(nil)` compiles
- `var _ Listener = (*QUICServer)(nil)` compiles
- Unit tests pass for DialTCP, DialUDP, AcceptTCP, ServeUDP
- Existing integration tests still pass
- Port hopping tested

### T2: TCP-WS Dialer + Listener Implementation

**Scope**: Fill TCPWSClient with DialTCP/DialUDP/Close (implements wire.Dialer). Fill TCPWSServer with AcceptTCP/ServeUDP/Close (implements wire.Listener). This is the fallback transport.
**Branch**: `t2-tcpws-dialer-listener`
**Files**:
- `wire/tcpws.go` — add methods to TCPWSClient + TCPWSServer
- `wire/tcpws_test.go` — unit tests for new methods
**Depends on**: [F1]
**TDD**:
- RED: Write `TestTCPWSClient_DialTCP` — client opens smux stream, writes stream header, reads response. Write `TestTCPWSClient_DialUDP` — client opens smux stream, writes smux-framed UDP. Write `TestTCPWSServer_AcceptTCP` — server accepts smux stream, reads header. Write `TestTCPWSServer_ServeUDP` — server reads smux UDP frames, dispatches.
- GREEN: Implement TCPWSClient: `Dial()` opens TCP+TLS+WebSocket, creates smux session. `DialTCP()` opens smux stream + writes stream header + reads response. `DialUDP()` opens smux stream, returns PacketConn adapter using smux-framed UDP. Implement TCPWSServer: `Listen()` accepts TCP+WS, creates smux session per client. `AcceptTCP()` accepts smux stream + reads header. `ServeUDP()` reads smux-framed UDP + dispatches.
- REFACTOR: Compile-time interface checks. Factor out smux session management.
**Acceptance**:
- `var _ Dialer = (*TCPWSClient)(nil)` compiles
- `var _ Listener = (*TCPWSServer)(nil)` compiles
- Unit tests pass for all 4 methods
- Smux config matches `DefaultSmuxConfig()`

### T3: Provider Interface + Cross-Platform TUN (sing-tun gVisor Stack)

**Scope**: Redesign Provider from `io.ReadWriteCloser` (L3) to L4 callback model. sing-tun already supports all target platforms with gVisor stack — we wrap its API. TUN creation varies per platform but gVisor stack + Handler interface is universal.

sing-tun platform coverage (confirmed):
```
Platform         TUN source                              Stack    Build tag
─────────────────────────────────────────────────────────────────────────────
macOS            sing-tun creates (tun_darwin.go)         gVisor   darwin
Linux/OpenWRT    sing-tun creates (tun_linux.go)          mixed    linux
                 + auto_redirect + nftables openwrt rules
Windows          sing-tun creates (tun_windows.go)        gVisor   windows
iOS              FD from NEPacketTunnelProvider            gVisor   ios (darwin+arm64)
Android          FD from VpnService.Builder                gVisor   android
```

All platforms use the same `tun.NewGVisor(StackOptions{Handler: ...})` for L3→L4 conversion. Differences are only in TUN device creation + route management.

**Branch**: `t3-provider-gvisor`
**Files**:
- `provider/provider.go` — new interfaces (Provider, ConnectionHandler, ProviderConfig)
- `provider/stack.go` — shared gVisor stack setup (wraps sing-tun Handler → ConnectionHandler)
- `provider/tun_desktop.go` — macOS + Linux + Windows: sing-tun creates TUN (`//go:build darwin || linux || windows`)
- `provider/tun_ios.go` — iOS: accept FD from Swift, wrap with sing-tun (`//go:build ios`)
- `provider/tun_android.go` — Android: accept FD from Java/Kotlin (`//go:build android`)
- `provider/proxy.go` — Proxy mode: SOCKS5/HTTP local server, no TUN, no root (all platforms)
- `provider/proxy_test.go` — Proxy provider tests
- `provider/provider_test.go` — interface tests + TUN mock tests
- DELETE `provider/tun_unix.go` — replaced by platform-specific files
- `config/config.go` — add `Mode` + `ProxyConfig` to ClientConfig
**Depends on**: [F1]
**TDD**:
- RED: Write `TestProvider_Interface` — verify Provider and ConnectionHandler compile. Write `TestStack_HandleTCP` — mock sing-tun Tun, feed TCP packet, verify ConnectionHandler.HandleTCP called. Write `TestStack_HandleUDP` — same for UDP. Write `TestDesktopProvider_Options` — verify correct sing-tun Options per platform (AutoRoute, RouteExclude, etc.). Write `TestProxyProvider_SOCKS5Connect` — SOCKS5 client connects, verify HandleTCP called with correct dest. Write `TestProxyProvider_HTTPConnect` — HTTP CONNECT request, verify HandleTCP called.
- GREEN: Define Provider interface:
  ```go
  type Provider interface {
      Start(ctx context.Context, handler ConnectionHandler) error
      Close() error
      Name() string
  }
  type ConnectionHandler interface {
      HandleTCP(conn net.Conn, dest string)
      HandleUDP(conn net.PacketConn, dest string)
  }
  type ProviderConfig struct {
      Mode           string        // "tun" (default) or "proxy"
      FileDescriptor int           // 0 = create TUN; >0 = use existing FD (iOS/Android)
      DNSServers     []netip.Addr  // excluded from TUN route (anti-loop)
      MTU            int           // default 1400
      ProxyListen    string        // proxy mode: listen address (default "127.0.0.1:1080")
      SetSystemProxy bool          // proxy mode: auto-set OS system proxy
  }
  ```
  Implement `stack.go`: adapter from sing-tun's `tun.Handler` (with `N.TCPConnectionHandlerEx` / `N.UDPConnectionHandlerEx`) to our `ConnectionHandler`. Implement `tun_desktop.go`: `tun.New(Options{AutoRoute: true, ...})` + `tun.NewGVisor(StackOptions{Tun: t, Handler: adapter})`. Implement `tun_ios.go`: `tun.New(Options{FileDescriptor: fd})` + same gVisor stack. Implement `tun_android.go`: same pattern with FD. Set `Inet4RouteExcludeAddress` for DNS server IPs (anti-loop). OpenWRT: rely on sing-tun's built-in nftables openwrt rules.
  Implement `proxy.go`: `ProxyProvider` — starts SOCKS5/HTTP mixed server on `ProxyListen`. On SOCKS5 CONNECT/UDP ASSOCIATE or HTTP CONNECT: extract dest addr, call `handler.HandleTCP(conn, dest)` / `handler.HandleUDP(conn, dest)`. If `SetSystemProxy=true`, configure OS-level system proxy on Start, restore on Close.
  Add to `config/config.go`:
  ```go
  type ClientConfig struct {
      Server string       `yaml:"server"`
      Mode   string       `yaml:"mode"`   // "tun" or "proxy", default "tun"
      Proxy  ProxyConfig  `yaml:"proxy"`
      DNS    DNSConfig    `yaml:"dns"`
      Router RouterConfig `yaml:"router"`
      Log    LogConfig    `yaml:"log"`
  }
  type ProxyConfig struct {
      Listen         string `yaml:"listen"`           // default "127.0.0.1:1080"
      SetSystemProxy bool   `yaml:"set_system_proxy"` // default false
  }
  ```
- REFACTOR: Delete old `tun_unix.go`. Verify build tags are correct per platform. Ensure `ProviderConfig.FileDescriptor` flow works for both create-TUN and use-FD paths. Extract SOCKS5/HTTP parsing into clean functions.
**Acceptance**:
- New Provider interface compiles on all platforms
- `go build -tags ios ./provider/...` compiles
- `go build -tags android ./provider/...` compiles
- Desktop TUN provider creates device and invokes handlers
- FD-based provider (iOS/Android) accepts FD and invokes same handlers
- Proxy provider accepts SOCKS5 + HTTP CONNECT, invokes same handlers
- Proxy provider works without root
- DNS server IPs excluded from TUN routing
- Config `mode: proxy` parsed correctly with defaults
- `go test ./provider/...` passes

### T4: L4 Client Tunnel

**Scope**: Write the new L4 client tunnel that connects Provider (gVisor connections) to wire.Dialer (QUIC/TCP-WS). Implements ConnectionHandler: routes each TCP/UDP connection through Router, then either direct (net.Dial) or proxy (wire.DialTCP/DialUDP).
**Branch**: `t4-l4-client-tunnel`
**Files**:
- `core/tunnel.go` — new file: L4 ClientTunnel
- `core/tunnel_test.go` — unit tests with mock Dialer + mock Provider
- `core/direct.go` — new simple version: `DirectDialer` using `net.Dial` (no raw socket)
- `core/direct_test.go` — test DirectDialer
**Depends on**: [F1, T1, T2, T3]
**TDD**:
- RED: Write `TestClientTunnel_HandleTCP_Proxy` — mock wire.Dialer, verify DialTCP called with correct addr, data forwarded bidirectionally. Write `TestClientTunnel_HandleTCP_Direct` — mock router returns Direct, verify net.Dial used. Write `TestClientTunnel_HandleUDP_DNS` — dest port 53, verify DNS middleware called. Write `TestClientTunnel_HandleUDP_Proxy` — non-DNS, verify DialUDP called.
- GREEN: Implement `ClientTunnel` struct with: `provider Provider`, `dialer wire.Dialer`, `router *Router`, `dnsMiddleware *dns.Middleware`. Implement `HandleTCP(conn, dest)`: parse dest → router.Route → if proxy: `dialer.DialTCP` + bidirectional copy; if direct: `net.Dial` + bidirectional copy. Implement `HandleUDP(conn, dest)`: if port 53 → DNS middleware; else router.Route → proxy/direct. Implement `Start(ctx)`: `provider.Start(tunnel)`. Implement `Close()`.
- REFACTOR: Extract bidirectional copy helper (TCP pipe pattern from qTunnel). Ensure graceful shutdown with context cancellation.
**Acceptance**:
- ClientTunnel implements ConnectionHandler
- TCP connections routed through wire or direct based on k2rule
- UDP port 53 intercepted by DNS middleware
- Bidirectional forwarding works with proper half-close
- `go test ./core/...` passes

### T5: DNS Middleware L4 Adaptation

**Scope**: Adapt ProxyDNSClient to use `wire.Dialer` instead of a single `net.Conn`. Each DNS query opens a fresh UDP session via `DialUDP`.
**Branch**: `t5-dns-l4-adapt`
**Files**:
- `core/dns/proxy.go` — change `wireConn net.Conn` to use `wire.Dialer`
- `core/dns/proxy_test.go` — update tests
**Depends on**: [T1]
**TDD**:
- RED: Write `TestProxyDNSClient_DialUDP` — mock Dialer, verify DialUDP called with DNS server address, query sent, response received.
- GREEN: Change `ProxyDNSClient` to hold `dialer wire.Dialer` instead of `wireConn net.Conn`. In query method: call `dialer.DialUDP(ctx, dnsServer)`, send DNS query, read response, close. Handle timeout per-query.
- REFACTOR: Ensure existing DNS middleware tests still pass. Remove any L3 packet-level DNS forwarding code.
**Acceptance**:
- ProxyDNSClient uses wire.Dialer.DialUDP
- DNS queries resolve correctly through mock Dialer
- `go test ./core/dns/...` passes (all existing + new tests)

### T6: Client Daemon Wiring

**Scope**: Connect daemon.doUp()/doDown() to the real tunnel stack. Parse config/URL → create wire transport → create provider → create tunnel → start/stop. Remove `k2 run -s` from client binary.
**Branch**: `t6-daemon-wiring`
**Files**:
- `daemon/daemon.go` — wire doUp/doDown to real tunnel lifecycle
- `daemon/daemon_test.go` — test doUp/doDown (with mocks or integration)
- `cmd/k2/cmd_run.go` — remove `-s` server flag, update `-c` to use real tunnel
**Depends on**: [T4, T5]
**TDD**:
- RED: Write `TestDaemon_DoUp_CreatesTransport` — verify doUp with wire URL creates TransportManager, Provider, ClientTunnel. Write `TestDaemon_DoDown_StopsTunnel` — verify tunnel.Close called.
- GREEN: In `doUp()`: parse wireURL with `wire.ParseURL` → `wire.BuildClientTLSConfig` → create `QUICClient` + `TCPWSClient` → `TransportManager` → create `Provider` → create `ClientTunnel` → `tunnel.Start(ctx)`. Store tunnel reference in Daemon struct. In `doDown()`: call `tunnel.Close()`. In `cmd_run.go`: remove `-s` flag, implement `runClientForeground` using same wiring.
- REFACTOR: Extract tunnel creation into a factory function. Ensure error states propagate to daemon state machine.
**Acceptance**:
- `k2 run -c client.yaml` starts real tunnel (needs sudo for TUN)
- `daemon.doUp(wireURL)` creates and starts tunnel
- `daemon.doDown()` cleanly stops tunnel
- State machine transitions correctly (connecting → connected → disconnecting → stopped)
- `go test ./daemon/...` passes

### T7: Server Binary (k2s)

**Scope**: Create the k2s server binary with its own daemon, CLI, and service management. Server listens on a port, does TLS/ECH peek, routes to auth handler or reverse proxy, then serves QUIC+TCP-WS listeners for proxy requests.
**Branch**: `t7-k2s-server`
**Files**:
- `cmd/k2s/main.go` — new: server CLI entry point
- `cmd/k2s/cmd_run.go` — new: server run/install subcommands
- `cmd/k2s/cmd_status.go` — new: server status
- `cmd/k2s/cmd_version.go` — new: version
- `server/server.go` — new: server daemon (TLS listener, ECH routing, auth)
- `server/server_test.go` — new: server tests
- `server/listener.go` — new: orchestrates QUIC + TCP-WS listeners behind TLS peek
- `server/listener_test.go` — new: listener tests
- `server/handler.go` — new: TCP/UDP proxy handler (AcceptTCP → net.Dial → pipe)
- `server/handler_test.go` — new: handler tests
**Depends on**: [T1, T2]
**TDD**:
- RED: Write `TestServer_TLSPeek_ECHRoute` — connection with ECH inner SNI → k2v5 handler. Write `TestServer_TLSPeek_ReverseProxy` — non-ECH → reverse proxy. Write `TestServer_TCPHandler` — AcceptTCP returns conn+addr, handler dials target, pipes data. Write `TestServer_UDPHandler` — ServeUDP dispatches frames, handler forwards to target.
- GREEN: Implement `server.Server` struct: holds TLS listener, TicketStore, QUICServer, TCPWSServer. `Listen()`: accept TCP, peek ClientHello SNI → ECH match: handle k2v5 (auth then QUIC/TCP-WS accept loop); no match: ReverseProxy. `handleTCP()`: for each AcceptTCP → `net.Dial(addr)` + bidirectional pipe. `handleUDP()`: implement UDPHandler → `net.Dial("udp", addr)` + forward. CLI: `k2s run` starts server, `k2s run --install` installs systemd/launchd service, `k2s status` shows connections.
- REFACTOR: Share service install logic from daemon/ (or extract to shared package). Factor out TCP pipe helper (shared with client tunnel).
**Acceptance**:
- `go build ./cmd/k2s` compiles
- `k2s run -s server.yaml` starts listening
- TLS peek correctly routes ECH vs non-ECH
- Auth handler issues tickets
- TCP proxy: accept stream → dial target → bidirectional forward
- UDP proxy: receive frames → forward to target → return responses
- `go test ./server/...` passes

### T8: End-to-End Integration

**Scope**: Full integration test: k2 client ↔ k2s server. Verify TCP proxy, UDP/DNS proxy, QUIC primary with TCP-WS fallback, ECH.
**Branch**: `t8-e2e-integration`
**Files**:
- `test/e2e_test.go` — new: end-to-end integration tests
- `test/helpers_test.go` — new: test helpers (start server, create client, etc.)
**Depends on**: [T6, T7]
**TDD**:
- RED: Write `TestE2E_ClientServerConnect` — start k2s in-process, connect k2 client, verify handshake. Write `TestE2E_TCPProxy` — client connects, opens TCP to target, sends HTTP request, gets response. Write `TestE2E_UDPProxy_DNS` — client sends DNS query, gets resolution. Write `TestE2E_QUICFallbackTCPWS` — block QUIC, verify TCP-WS fallback works. Write `TestE2E_ECH_Fingerprint` — verify ECH outer SNI is camouflage, inner SNI is real.
- GREEN: Build test harness: in-process server (no TUN needed — mock provider or use loopback). Wire client with mock provider that injects test connections. Verify data flows end-to-end through wire protocol.
- REFACTOR: Extract reusable test fixtures. Ensure tests clean up (no leaked goroutines/listeners).
**Acceptance**:
- All E2E tests pass
- TCP proxy verified with real HTTP request/response
- DNS resolution works through tunnel
- QUIC → TCP-WS fallback verified
- ECH negotiation verified
- No test flakiness (proper timeouts + cleanup)
