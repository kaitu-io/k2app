# k2 Project Baseline

## Meta

| Field | Value |
|-------|-------|
| Project | k2 — Kaitu Network Tunnel |
| Purpose | Minimalist VPN tunnel with k2v5 composite wire protocol |
| Updated | 2026-02-12 |
| Features | l4-architecture, wire-protocol-compliance, zero-config-stealth |

## cmd/k2 — Client CLI

Client command-line interface with subcommands for tunnel management.

Capabilities:
- Parse subcommands: up, down, status, speedtest, run, version
- `k2 up [URL or config.yaml]` — connect tunnel via daemon API
- `k2 down` — disconnect tunnel via daemon API
- `k2 status` — query daemon status via HTTP API
- `k2 speedtest` — run bandwidth test through tunnel
- `k2 run` — start client daemon (called by service manager)
- `k2 run -c <config.yaml>` — client foreground mode with sudo for TUN
- `k2 version` — display version info
- Flag parsing with stdlib `flag.NewFlagSet`

## cmd/k2s — Server CLI

Server command-line interface for VPS deployment.

Capabilities:
- Parse subcommands: run, status, version
- `k2s run` — start server with zero-config auto-provisioning (no config file required)
- `k2s run -s <server.yaml>` — server foreground mode with explicit config
- `k2s run --install` — install as system service (systemd/launchd)
- `k2s status` — show server status (connections, throughput)
- `k2s version` — display version info
- Zero-config startup: auto-generate TLS certs, ECH configs, auth tokens, log connection URL

## config — Configuration Management

YAML configuration parsing for client and server.

Capabilities:
- Parse ClientConfig from YAML: server URL, mode (tun/proxy), DNS, router, log settings
- Parse ServerConfig from YAML: listen address, auth, TLS, ECH (provider), reverse proxy
- Validate wire URL format (k2v5://udid:token@host:port?ech=...&fp=...&hop=...&pin=...&insecure=...)
- Support mode switching: "tun" (default) or "proxy" (SOCKS5/HTTP)
- Proxy config: listen address, set_system_proxy flag
- DNS config: servers, DoH, cache settings
- Router config: k2rule rules YAML, GeoIP path
- Log config: level, file output
- ECH config: provider type (local for DNS-based forging)
- Default server config: cert_dir (~/.k2s/), listen (:443), ECH provider (local)

## core — Client Tunnel Logic

L4 tunnel connecting Provider connections to wire.Dialer.

Capabilities:
- ClientTunnel implements ConnectionHandler for Provider callbacks
- Route TCP/UDP connections through Router (k2rule)
- Direct routing: use net.Dial for local/excluded destinations
- Proxy routing: use wire.DialTCP/DialUDP for tunneled destinations
- Reject routing: silently drop blocked connections
- Bidirectional pipe for TCP forwarding (local ↔ remote)
- Packet forwarding for UDP (with dest address handling)
- Router integration with k2rule: domain/IP → Direct/Proxy/Reject
- Graceful shutdown with context cancellation and WaitGroup

## core/dns — DNS Middleware Pipeline

13-step DNS processing middleware with caching and filtering.

Capabilities:
- Intercept DNS queries from TUN (UDP port 53)
- Middleware pipeline: Hosts → Cache → Filter → Direct/DoH/Proxy
- Hosts file resolution (/etc/hosts + custom)
- In-memory cache with TTL and negative caching
- Domain filtering (block ads, malware)
- DNS metrics collection (query count, latency)
- Direct DNS: query system resolvers
- DoH (DNS-over-HTTPS): query encrypted DNS servers
- ProxyDNSClient: forward queries through wire.Dialer (L4 DialUDP)
- Dynamic IP cache for reverse lookups (IP → domain mapping)

## core/packet — IP Packet Parsing

Parse IPv4/IPv6 packets from TUN device (used by gVisor integration).

Capabilities:
- Parse IP headers (version, protocol, src, dest)
- Extract TCP/UDP port numbers
- Validate packet structure
- Support both IPv4 and IPv6

## daemon — Client Daemon State Machine

Manages tunnel lifecycle, HTTP API, and service installation.

Capabilities:
- State machine: stopped → connecting → connected → disconnecting → stopped
- HTTP API on 127.0.0.1:1777: /up, /down, /status, /speedtest
- IPC client for CLI commands (connect to daemon HTTP API)
- Tunnel lifecycle: doUp creates Provider + wire.Dialer + ClientTunnel, doDown stops all
- BuildTunnel: parse wire URL → TLS config → TransportManager → Provider → ClientTunnel
- Service installation: launchd (macOS), systemd (Linux)
- Service management: install, uninstall, start, stop
- Process management: PID file, daemonize, signal handling
- Recovery: restart on crash, exponential backoff
- Resource limits: increase RLIMIT_NOFILE for high connection count
- Speedtest: bandwidth measurement through tunnel

## provider — Connection Source Abstraction

Platform-agnostic interface for TCP/UDP connection sourcing.

Capabilities:
- Provider interface: Start(ctx, ConnectionHandler), Close(), Name()
- ConnectionHandler interface: HandleTCP(conn, dest), HandleUDP(conn, dest)
- TUN provider (desktop): create TUN device via sing-tun, gVisor stack for L3→L4
- TUN provider (iOS): accept file descriptor from NEPacketTunnelProvider
- TUN provider (Android): accept file descriptor from VpnService.Builder
- Proxy provider: SOCKS5/HTTP mixed proxy server, no root required
- Auto-route configuration (platform-specific)
- DNS server exclusion from TUN routes (anti-loop)
- System proxy configuration (proxy mode)
- MTU configuration (default 1400 for QUIC overhead)
- File descriptor passthrough for mobile platforms

## server — Server Daemon and Handlers

Server-side listener orchestration and TCP/UDP proxy handlers.

Capabilities:
- TLS listener with ClientHello peek (SNI + ECH presence detection)
- ECH-based routing: HasECH=true → decrypt inner SNI → k2v5 handler, HasECH=false → reverse proxy to outer SNI
- ECH key management: auto-generate X25519 HPKE keys on first start, load from key_path on subsequent starts
- Zero-config auto-provisioning: 6-step flow (certs, ECH, padding, auth, URL logging)
- ECH config forging: query Cloudflare DNS for real ECH template, forge config with config_id + 1
- ECH config rotation: maintain multiple configs (shared HPKE key, different config_ids) for graceful rotation
- TLS record padding: probe target, apply padding distribution to match fingerprint
- Device auth: metadata stream (StreamType 0x10) on QUIC + TCP-WS, validated by the Validator/AuthVerdict chain (four-state: OK / no opinion / explicit reject 401-403 / cannot determine -1; users_file → remote /slave/device-check-auth; neither → only the cert_dir/auth.json credential), success cached by CachingValidator; always enforced per-connection (K2_ENFORCE_AUTH removed 2026-10-30). (Ticket auth POST /k2v5/auth removed 2026-07.)
- Auth auto-generation: create UDID + token on first start, persist to cert_dir/auth.json
- QUIC listener: AcceptTCP streams, ServeUDP datagrams/streams (with stream fallback)
- TCP-WS listener: WebSocket upgrade to /k2v5/tunnel, smux multiplexing over binary WS frames
- TCP handler: AcceptTCP → read stream header → net.Dial(target) → bidirectional pipe
- UDP handler: ServeUDP → read frames → net.Dial("udp", target) → forward packets
- UDP session management: maxSize enforcement, background cleanup of idle sessions
- Connection URL persisted to `connect-url.txt` (0600) for `cmd/k2s` to read and print — never routed through slog (spec §9 credential-log guard): k2v5://UDID:TOKEN@HOST:PORT?ech=<b64>&pin=<dual_pins>
- Connection tracking and metrics
- Graceful shutdown with connection draining

## wire — k2v5 Wire Protocol

L4 transport protocol with QUIC primary and TCP-WS fallback.

Capabilities:
- Dialer interface: DialTCP, DialUDP, Close
- Listener interface: AcceptTCP, ServeUDP, Close
- QUICClient: QUIC/H3 client with DialTCP (stream), DialUDP (datagram preferred, stream fallback when datagrams unsupported)
- QUICServer: QUIC/H3 server with AcceptTCP (stream), ServeUDP (datagram/stream with StreamTypeUDP handling)
- TCPWSClient: TCP+uTLS+WebSocket+smux client with DialTCP/DialUDP
- TCPWSServer: TCP+TLS+WebSocket+smux server with AcceptTCP/ServeUDP
- TransportManager: QUIC primary, TCP-WS automatic fallback
- Stream protocol: HTTP/3 frame type + header (dest addr, flags) + response (status) + data
- UDP protocol: datagrams (preferred) or stream-based frames with length prefix (QUIC/TCP-WS)
- Auth: device authentication via the metadata stream + Validator/AuthVerdict chain (four-state: OK / no opinion / explicit reject 401-403 / cannot determine -1; users_file/remote/provisioned auth.json) with success-only CachingValidator; always enforced per-connection on both transports (ticket-based TicketStore/AuthHandler removed 2026-07)
- Auth info management: EnsureAuth (load or generate UDID + token), persist to JSON
- ECH core: X25519 HPKE key generation, PeekClientHello with HasECH detection, EnsureECHPrivateKey (auto-generate/load)
- ECH client: BuildClientTLSConfig with ECH config list, mandates ECH (no fallback to plaintext SNI)
- ECH provider: interface for fetching ECH templates, LocalECHProvider queries Cloudflare DNS HTTPS records
- ECH template parsing: extract config_id, kem_id, cipher_suites, public_name from binary ECHConfig format
- ECH config forging: GenerateECHKeyFromTemplate (copy template fields, increment config_id, use own HPKE key)
- ECH config rotation: split storage (ech.key for private key, ech-configs/*.bin for configs), support multiple configs
- ECH fallback chain: live DNS query, disk cache, hardcoded defaults (never errors)
- CA pool: system CAs + embedded Kaitu CA - Chinese CA blocklist (CNNIC, WoSign, WoTrus, CFCA, etc.)
- CA blocklist: VerifyPeerCertificate callback rejects Chinese CAs post-validation
- TLS fingerprinting: uTLS for TCP-WS with browser-like handshakes (chrome, firefox, safari, random)
- WebSocket layer: client upgrade to /k2v5/tunnel after uTLS, server accept with binary message type
- Port hopping: randomize client port within configured range
- Parse k2v5:// URLs: udid:token@host:port?ech=&fp=&hop=&pin=&insecure=
- ReverseProxy: camouflage server as innocent HTTPS site (for non-ECH connections)
- UDPSessionMap: maxSize enforcement, background cleanup with configurable interval
- SessionIDAllocator: reusable session IDs for UDP multiplexing

## wire/tlspad — TLS Record Padding

TLS fingerprint mimicry via record padding distribution matching.

Capabilities:
- Probe target server TLS handshake, extract padding distribution
- Parse ClientHello + ServerHello records, measure padding lengths
- Generate padding bytes for server certificates to match target distribution
- Apply padding to TLS config at server startup
- Support dual certificate pairs (RSA + EC) with separate padding profiles
- Fallback to zero padding if probe fails

## wire/pcc — PCC Vivace Congestion Control

Congestion control algorithm for QUIC transport.

Capabilities:
- Implement quic-go congestion.CongestionControl interface
- PCC Vivace algorithm: monitor intervals, utility function, rate adjustment
- RTT estimation and tracking
- Packet loss detection
- Adaptive rate control based on network conditions
- Helper functions for congestion window calculations

## test — Integration Testing

End-to-end tests for full client-server stack.

Capabilities:
- In-process server startup for reproducible testing
- Test TCP proxy: HTTP request/response through tunnel
- Test UDP proxy: DNS queries through tunnel
- Test QUIC → TCP-WS failover when QUIC blocked
- Test ECH negotiation and fingerprinting
- Test fixtures: common config builders, mock servers
- Helper functions: start server, create client, cleanup
