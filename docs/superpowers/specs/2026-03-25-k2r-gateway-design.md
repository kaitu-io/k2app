# k2r — Gateway Binary Design

Date: 2026-03-25
Status: Draft
Branch: feat/k2p2p

## Overview

k2r is a standalone binary that turns a Linux machine into a transparent gateway, routing an entire LAN's traffic through a k2 tunnel. It complements k2 (desktop client for local traffic) and k2s (server).

**k2 vs k2r**:
- k2: "I'm a device, route MY traffic through the tunnel" (TUN, local)
- k2r: "I'm a gateway, route my ENTIRE LAN's traffic through the tunnel" (TPROXY, network-wide)

## Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Binary name | `k2r` | Distinct from k2 (client) and k2s (server) |
| Platform scope | Linux only (Phase 1) | 99% of router/soft-router use cases. macOS Phase 3 |
| Relationship to k2 tproxy | Replace and delete | k2's tproxy mode was never used in production |
| Architecture | Independent platform shell (gateway/) | Clean isolation from daemon/, zero risk to k2 |
| Engine integration | Inject Provider via new engine.Config field | 3-line engine change, backward-compatible |
| Management | CLI + HTTP API (Phase 1), Web UI later | Core functionality first |
| k2p Home | Phase 2 addition | Gateway is natural Home host, but deferred |

## Architecture

```
┌──────────────────────────────────────────────────┐
│ Platform Shell: gateway/                          │
│   Gateway struct, HTTP API, Interceptor, LAN scan │
├──────────────────────────────────────────────────┤
│ Engine (engine/) — shared, 3-line change          │
├──────────────────────────────────────────────────┤
│ Core (core/ + core/dns/) — shared, zero change    │
├──────────────────────────────────────────────────┤
│ Wire (wire/) — shared, zero change                │
└──────────────────────────────────────────────────┘
```

Layer model matches CLAUDE.md: gateway/ is a Platform Shell (same level as daemon/ and appext/).

## File Structure

```
cmd/k2r/
  main.go                # CLI: k2r setup / k2r ctl up|down|status / k2r service install|uninstall

gateway/
  gateway.go             # Gateway struct, EventHandler impl, HTTP server (127.0.0.1:1779)
  api.go                 # handleCore (up/down/status), handlePing
  sse.go                 # SSE push (status + stats events)
  config.go              # GatewayConfig, engine.Config assembly, server IP resolution
  provider.go            # gatewayProvider — implements provider.Provider
  listener.go            # TPROXY TCP/UDP listeners (IP_TRANSPARENT, IP_RECVORIGDSTADDR)
  intercept.go           # Interceptor interface definition
  intercept_nft.go       # nftables implementation
  intercept_ipt.go       # iptables fallback implementation
  lan.go                 # LAN subnet auto-discovery
  sysctl.go              # ip_forward management (save/set/restore)
  service_linux.go       # systemd + OpenWRT procd service management
  state.go               # Persisted state (auto-reconnect on reboot)
  recovery.go            # Panic handler + stale rule cleanup on startup
  gateway_test.go        # Unit tests
```

## Components

### 1. Interceptor — Traffic Interception

Intercepts LAN traffic and redirects it to the local TPROXY listener.

```go
// gateway/intercept.go

type Interceptor interface {
    // Install sets up firewall rules to redirect LAN traffic to listenAddr.
    // Must be called after resolving all exclude IPs.
    Install(cfg InterceptConfig) error
    // Remove cleans up all firewall rules (idempotent, errors ignored).
    Remove()
    // Name returns the backend name ("nftables" or "iptables").
    Name() string
}

type InterceptConfig struct {
    ListenPort int      // TPROXY listen port (default 12345)
    LANSubnets []string // LAN subnets to intercept (e.g. "192.168.1.0/24")
    ExcludeIPs []string // IPs to exclude (k2s server, direct DNS upstreams, gateway self)
}
```

Auto-detection: prefer nftables, fall back to iptables.

```go
// gateway/intercept.go

func NewInterceptor() (Interceptor, error) {
    if _, err := exec.LookPath("nft"); err == nil {
        return &nftInterceptor{}, nil
    }
    if _, err := exec.LookPath("iptables"); err == nil {
        return &iptInterceptor{}, nil
    }
    return nil, errors.New("gateway: neither nft nor iptables found")
}
```

#### nftables Rules (intercept_nft.go)

Table name: `inet k2r` (atomic create/delete, no collision with other nftables users).

```
table inet k2r {
    set exclude_v4 {
        type ipv4_addr
        elements = { 1.2.3.4, 8.8.8.8 }
    }

    chain prerouting {
        type filter hook prerouting priority mangle; policy accept;

        # Skip loopback and excluded destinations
        iifname "lo" return
        ip daddr @exclude_v4 return

        # Intercept LAN traffic
        ip saddr 192.168.1.0/24 meta l4proto tcp tproxy to :12345 meta mark set 1
        ip saddr 192.168.1.0/24 meta l4proto udp tproxy to :12345 meta mark set 1
    }
}
```

Plus IP routing rules (same as existing tproxy):
```
ip rule add fwmark 1 table 100
ip route add local 0.0.0.0/0 dev lo table 100
```

Cleanup: `nft delete table inet k2r` (atomic, cascades all rules).

#### iptables Rules (intercept_ipt.go)

```
# Create chain
iptables -t mangle -N K2R

# Exclude server IPs
iptables -t mangle -A K2R -d 1.2.3.4 -j RETURN
iptables -t mangle -A K2R -d 8.8.8.8 -j RETURN

# TPROXY redirect
iptables -t mangle -A K2R -p tcp -j TPROXY --on-port 12345 --tproxy-mark 1
iptables -t mangle -A K2R -p udp -j TPROXY --on-port 12345 --tproxy-mark 1

# Jump from PREROUTING for LAN subnets
iptables -t mangle -A PREROUTING -s 192.168.1.0/24 -j K2R

# IP routing (same as nftables)
ip rule add fwmark 1 table 100
ip route add local 0.0.0.0/0 dev lo table 100
```

Cleanup: flush chain, delete chain, remove PREROUTING jump, remove IP rules.

### 2. Gateway Provider

Implements `provider.Provider`. Owns both the Interceptor and the TPROXY listeners.

```go
// gateway/provider.go

type gatewayProvider struct {
    interceptor Interceptor
    cfg         InterceptConfig
    listenPort  int
    tcpLn       net.Listener     // TCP TPROXY listener
    udpConn     *net.UDPConn     // UDP TPROXY listener
    done        chan struct{}
}

func (p *gatewayProvider) Start(ctx context.Context, handler provider.ConnectionHandler) error {
    // 1. Install intercept rules
    if err := p.interceptor.Install(p.cfg); err != nil {
        return fmt.Errorf("gateway: install intercept rules: %w", err)
    }
    // 2. Start TCP listener with IP_TRANSPARENT
    // 3. Start UDP listener with IP_TRANSPARENT + IP_RECVORIGDSTADDR
    // 4. Accept loop → handler.HandleTCP(conn, conn.LocalAddr().String())
    // 5. Read loop → handler.HandleUDP(pconn, dst.String())
    return nil
}

func (p *gatewayProvider) Close() error {
    close(p.done)
    // 1. Close listeners
    if p.tcpLn != nil { p.tcpLn.Close() }
    if p.udpConn != nil { p.udpConn.Close() }
    // 2. Remove intercept rules (best-effort, nil-safe)
    if p.interceptor != nil { p.interceptor.Remove() }
    return nil
}

func (p *gatewayProvider) Name() string { return "gateway" }
```

#### TPROXY Listener Details (listener.go)

TCP listener uses `IP_TRANSPARENT` socket option. When a LAN device connects to `93.184.216.34:443`, the kernel delivers the connection to our listener, and `conn.LocalAddr()` returns `93.184.216.34:443` (the original destination).

```go
func newTPROXYTCPListener(ctx context.Context, port int) (net.Listener, error) {
    lc := net.ListenConfig{
        Control: func(network, address string, c syscall.RawConn) error {
            return c.Control(func(fd uintptr) {
                syscall.SetsockoptInt(int(fd), syscall.SOL_IP, syscall.IP_TRANSPARENT, 1)
            })
        },
    }
    return lc.Listen(ctx, "tcp", fmt.Sprintf(":%d", port))
}
```

UDP listener uses both `IP_TRANSPARENT` and `IP_RECVORIGDSTADDR`. Original destination is extracted from ancillary data via `ReadMsgUDP()`:

```go
func newTPROXYUDPListener(ctx context.Context, port int) (*net.UDPConn, error) {
    lc := net.ListenConfig{
        Control: func(network, address string, c syscall.RawConn) error {
            return c.Control(func(fd uintptr) {
                syscall.SetsockoptInt(int(fd), syscall.SOL_IP, syscall.IP_TRANSPARENT, 1)
                syscall.SetsockoptInt(int(fd), syscall.SOL_IP, syscall.IP_RECVORIGDSTADDR, 1)
            })
        },
    }
    conn, err := lc.ListenPacket(ctx, "udp", fmt.Sprintf(":%d", port))
    return conn.(*net.UDPConn), err
}

func readUDPOrigDst(conn *net.UDPConn, buf []byte) (n int, src, dst *net.UDPAddr, err error) {
    oob := make([]byte, 64)
    n, oobn, _, srcAddr, err := conn.ReadMsgUDP(buf, oob)
    if err != nil { return 0, nil, nil, err }
    msgs, _ := syscall.ParseSocketControlMessage(oob[:oobn])
    for _, msg := range msgs {
        if msg.Header.Level == syscall.SOL_IP && msg.Header.Type == syscall.IP_RECVORIGDSTADDR {
            port := int(msg.Data[2])<<8 | int(msg.Data[3])
            ip := net.IPv4(msg.Data[4], msg.Data[5], msg.Data[6], msg.Data[7])
            return n, srcAddr, &net.UDPAddr{IP: ip, Port: port}, nil
        }
    }
    return n, srcAddr, srcAddr, nil
}
```

### 3. LAN Subnet Discovery

```go
// gateway/lan.go

func DiscoverLANSubnets() ([]string, error) {
    ifaces, err := net.Interfaces()
    if err != nil { return nil, err }

    var subnets []string
    for _, iface := range ifaces {
        if iface.Flags&net.FlagLoopback != 0 { continue }
        if iface.Flags&net.FlagUp == 0 { continue }
        if isVirtualInterface(iface.Name) { continue }

        addrs, _ := iface.Addrs()
        for _, addr := range addrs {
            ipNet, ok := addr.(*net.IPNet)
            if !ok { continue }
            if !ipNet.IP.IsPrivate() { continue }
            subnets = append(subnets, ipNet.String())
        }
    }
    if len(subnets) == 0 {
        return nil, errors.New("gateway: no private LAN subnets found")
    }
    return subnets, nil
}

// isVirtualInterface filters out non-physical interfaces.
func isVirtualInterface(name string) bool {
    prefixes := []string{"docker", "veth", "br-", "tun", "tap", "virbr", "lo"}
    for _, p := range prefixes {
        if strings.HasPrefix(name, p) { return true }
    }
    return false
}
```

Config override: if `lan_subnets` is set in YAML, skip auto-discovery.

### 4. sysctl Management

```go
// gateway/sysctl.go

type sysctlState struct {
    ipForwardOld string // previous value of net.ipv4.ip_forward
}

func (s *sysctlState) Enable() error {
    old, err := os.ReadFile("/proc/sys/net/ipv4/ip_forward")
    if err != nil { return fmt.Errorf("read ip_forward: %w", err) }
    s.ipForwardOld = strings.TrimSpace(string(old))
    return os.WriteFile("/proc/sys/net/ipv4/ip_forward", []byte("1"), 0644)
}

func (s *sysctlState) Restore() {
    if s.ipForwardOld != "" {
        os.WriteFile("/proc/sys/net/ipv4/ip_forward", []byte(s.ipForwardOld), 0644)
    }
}
```

### 5. Server IP Resolution & Exclude List

```go
// gateway/config.go

func resolveExcludeIPs(serverURL string, directDNS []string, lanSubnets []string) ([]string, error) {
    // 1. Parse k2v5:// URL, extract server hostname
    host, _, err := config.ParseServerURL(serverURL)
    if err != nil { return nil, err }

    // 2. Resolve server hostname
    serverIPs, err := net.LookupHost(host)
    if err != nil { return nil, fmt.Errorf("resolve server %s: %w", host, err) }

    exclude := make([]string, 0, len(serverIPs)+len(directDNS)+8)
    exclude = append(exclude, serverIPs...)

    // 3. Add direct DNS upstream IPs (prevent DNS query loop)
    for _, dns := range directDNS {
        h, _, _ := net.SplitHostPort(dns)
        if h == "" { h = dns }
        if ip := net.ParseIP(h); ip != nil {
            exclude = append(exclude, ip.String())
        }
    }

    // 4. Add gateway's own host IPs (from all non-virtual interfaces).
    //    Must use iface.Addrs() to get actual host IPs, NOT net.ParseCIDR
    //    which returns the network address (e.g. 192.168.1.0, not 192.168.1.1).
    ifaces, _ := net.Interfaces()
    for _, iface := range ifaces {
        if iface.Flags&net.FlagLoopback != 0 { continue }
        addrs, _ := iface.Addrs()
        for _, addr := range addrs {
            ipNet, ok := addr.(*net.IPNet)
            if !ok { continue }
            exclude = append(exclude, ipNet.IP.String())
        }
    }

    return exclude, nil
}
```

Called before `Interceptor.Install()`. If resolution fails, gateway refuses to start (prevents traffic black hole).

### 6. Engine Integration

**Change to engine/config.go** (1 field):

```go
type Config struct {
    // ... existing fields ...

    // Provider is an externally-created provider. If non-nil, engine uses it
    // instead of creating a provider from Client.Mode. Used by gateway/.
    Provider provider.Provider
}
```

**Change to engine/engine.go** (3 lines in Start):

```go
var prov provider.Provider
if cfg.Provider != nil {
    prov = cfg.Provider
} else if cfg.FileDescriptor >= 0 {
    // ... existing mobile path ...
} else if client.Mode == "proxy" {
    // ... existing proxy path ...
} else {
    // ... existing TUN path (desktop default) ...
}
```

The `mode == "tproxy"` branch is deleted entirely.

**Gateway assembles engine.Config**:

```go
func (g *Gateway) engineConfigWith(cfg *GatewayConfig, prov provider.Provider) engine.Config {
    return engine.Config{
        Client:         &cfg.ClientConfig, // Mode already set to "gateway" by doUp
        FileDescriptor: -1,
        Provider:       prov,
        HealthCallback: g.broadcastSnapshot,
        // DirectDialer:   nil  (no interface binding)
        // NetworkMonitor: nil  (always-on, no switch detection)
    }
}
```

### 7. DNS Flow (zero special handling)

DNS works identically to TUN mode. Engine creates `dnsHandler` wrapping `ClientTunnel` when `client.Mode != "proxy"`. Gateway sets `Mode = "gateway"` (see doUp step 6), which passes this gate. The handler intercepts port 53 from the destination string:

```
LAN Device → DNS query to 8.8.8.8:53
  → nftables TPROXY → gatewayProvider listener
  → handler.HandleUDP(pconn, "8.8.8.8:53")
  → dnsHandler sees port 53 → dns.Middleware.HandleQuery()
  → cache/filter/k2rule/upstream pipeline
  → response back to LAN device
```

No changes to core/dns/ or engine's DNS wiring.

### 8. Gateway Daemon Lifecycle

```go
// gateway/gateway.go

type Gateway struct {
    opMu        deadlock.Mutex   // serializes doUp/doDown (coarse lifecycle lock)
    mu          deadlock.Mutex   // protects field access (fine-grained data lock)
    eng         *engine.Engine
    lastStatus  engine.Status

    interceptor Interceptor
    provider    *gatewayProvider
    sysctl      sysctlState
    httpServer  *http.Server
}

func (g *Gateway) doUp(cfg *GatewayConfig) error {
    // opMu serializes doUp/doDown (same pattern as daemon/).
    g.opMu.Lock()
    defer g.opMu.Unlock()

    // 1. Discover LAN subnets (or use config override)
    subnets, err := discoverOrConfig(cfg)
    if err != nil { return err }

    // 2. Resolve exclude IPs (server + direct DNS + gateway host IPs)
    excludeIPs, err := resolveExcludeIPs(cfg.Server, cfg.DNS.Direct, subnets)
    if err != nil { return err }

    // 3. Enable IP forwarding
    if err := g.sysctl.Enable(); err != nil { return err }

    // 4. Create Interceptor (nft or iptables auto-detect)
    interceptor, err := NewInterceptor()
    if err != nil { return err }

    // 5. Create gateway provider (owns interceptor + TPROXY listeners)
    prov := newGatewayProvider(interceptor, InterceptConfig{
        ListenPort: cfg.ListenPort,
        LANSubnets: subnets,
        ExcludeIPs: excludeIPs,
    })

    // 6. Set Mode to "gateway" so engine wires DNS middleware
    //    (engine gates DNS on Mode != "proxy")
    cfg.ClientConfig.Mode = "gateway"

    // 7. Create engine with injected provider
    eng := engine.New()
    eng.SetEventHandler(g)
    if err := eng.Start(g.engineConfigWith(cfg, prov)); err != nil {
        g.sysctl.Restore()
        return err
    }

    // 8. Store references under lock
    g.mu.Lock()
    g.eng = eng
    g.interceptor = interceptor
    g.provider = prov
    g.mu.Unlock()

    return nil
    // engine.Start calls provider.Start → installs intercept rules + starts listeners
    // engine wires DNS middleware (dnsHandler) automatically
}

func (g *Gateway) doDown() {
    g.opMu.Lock()
    defer g.opMu.Unlock()

    // 1. Extract references under lock, nil fields
    g.mu.Lock()
    eng := g.eng
    g.eng = nil
    interceptor := g.interceptor
    g.interceptor = nil
    g.provider = nil
    g.mu.Unlock()

    // 2. Stop engine (calls provider.Close → removes intercept rules)
    if eng != nil { eng.Stop() }

    // 3. Best-effort interceptor cleanup (in case provider.Close missed it)
    if interceptor != nil { interceptor.Remove() }

    // 4. Restore IP forwarding
    g.sysctl.Restore()
}
```

### 9. Crash Recovery

```go
// gateway/recovery.go

func (g *Gateway) cleanStaleRules() {
    // Called on startup before anything else.
    // Remove any leftover rules from a previous crash.
    if i, err := NewInterceptor(); err == nil {
        i.Remove() // idempotent, ignores errors
    }
}

func (g *Gateway) handlePanic() {
    if r := recover(); r != nil {
        slog.Error("gateway: panic", "err", r, "stack", string(debug.Stack()))
        if g.interceptor != nil {
            g.interceptor.Remove()
        }
        g.sysctl.Restore()
    }
}
```

### 10. Configuration

```yaml
# k2r.yml
server: k2v5://example.com:443/path?token=xxx&ech=yyy

# Gateway-specific
listen_port: 12345           # TPROXY port (default 12345)
lan_subnets:                 # Optional, auto-discovered if omitted
  - 192.168.1.0/24
dns_redirect: true           # Intercept DNS port 53 (default true)

# Shared with k2 (same ClientConfig fields)
dns:
  direct:
    - 223.5.5.5              # Direct DNS (for direct-routed domains)
  proxy:
    - https://1.1.1.1/dns-query  # Proxy DNS (through tunnel)
rule:
  global: false              # true = all traffic through tunnel
log:
  level: info
```

GatewayConfig extends ClientConfig with gateway-specific fields:

```go
// gateway/config.go

type GatewayConfig struct {
    config.ClientConfig `yaml:",inline"`
    ListenPort          int      `yaml:"listen_port"`
    LANSubnets          []string `yaml:"lan_subnets"`
    DNSRedirect         bool     `yaml:"dns_redirect"`
}
```

### 11. CLI

```
k2r setup <k2v5://URL>       # Generate config + install service + start
k2r ctl up                    # Connect (install rules + start tunnel)
k2r ctl down                  # Disconnect (remove rules + stop tunnel)
k2r ctl status                # JSON status
k2r service install           # Install systemd/procd service
k2r service uninstall         # Remove service
```

HTTP API on `127.0.0.1:1779` (1777=k2 daemon, 1778=reserved for log-level):
- `POST /api/core` — action: up/down/status (same protocol as daemon/)
- `GET /api/events` — SSE (status + stats)
- `GET /ping` — health check

### 12. Service Management

```go
// gateway/service_linux.go

func installService() error {
    if isOpenWRT() {
        return installOpenWRT() // init.d/procd with START=99
    }
    return installSystemd() // k2r.service, Type=simple, Restart=always
}
```

OpenWRT detection: same `isOpenWRT()` logic (check `/etc/openwrt_release` or `fw4`).

### 13. Build Targets

```makefile
# Makefile additions

build-k2r:
	mkdir -p build
	go build $(PROD_TAGS) -ldflags "$(LDFLAGS)" -o build/k2r ./cmd/k2r

build-k2r-linux-amd64:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build $(PROD_TAGS) -ldflags "$(LDFLAGS)" -o build/k2r-linux-amd64 ./cmd/k2r

build-k2r-linux-arm64:
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build $(PROD_TAGS) -ldflags "$(LDFLAGS)" -o build/k2r-linux-arm64 ./cmd/k2r

build-k2r-armv7:
	CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build $(PROD_TAGS) -ldflags "$(LDFLAGS)" -o build/k2r-linux-armv7 ./cmd/k2r

build-k2r-mipsle:
	CGO_ENABLED=0 GOOS=linux GOARCH=mipsle GOMIPS=softfloat go build $(PROD_TAGS) -ldflags "$(LDFLAGS)" -o build/k2r-linux-mipsle ./cmd/k2r

build-k2r-mips:
	CGO_ENABLED=0 GOOS=linux GOARCH=mips GOMIPS=softfloat go build $(PROD_TAGS) -ldflags "$(LDFLAGS)" -o build/k2r-linux-mips ./cmd/k2r

# All router targets with optional UPX compression
build-k2r-all: build-k2r-linux-amd64 build-k2r-linux-arm64 build-k2r-armv7 build-k2r-mipsle build-k2r-mips
	@if [ "$(UPX)" = "1" ] && command -v upx >/dev/null 2>&1; then \
		for f in build/k2r-linux-*; do upx --best --lzma "$$f"; done; \
	fi
	@echo "k2r builds OK"
```

The earlier `build-router-*` targets (building k2 for router architectures) should be replaced by `build-k2r-*` targets. k2 no longer needs router architecture builds.

## Code to Delete

| File/Code | Action |
|-----------|--------|
| `provider/tproxy_linux.go` | Delete entirely |
| `engine/engine.go` `mode == "tproxy"` branch | Delete |
| `config/config.go` Mode "tproxy" option | Remove from docs/comments |
| `daemon/service_linux.go` `isOpenWRT()` + `installOpenWRT()` + `uninstallOpenWRT()` | Delete OpenWRT parts, keep systemd |
| `Makefile` `build-router-*` targets | Replace with `build-k2r-*` |

## Testing Strategy

**Unit tests** (gateway/):
- Interceptor rule generation: verify nftables/iptables command strings without executing
- LAN subnet discovery: mock net.Interfaces
- Server IP resolution: mock net.LookupHost
- sysctl save/restore: mock file reads/writes

**Integration tests** (gateway/):
- gatewayProvider with mock ConnectionHandler: verify HandleTCP/HandleUDP receive correct original destinations
- Engine integration: gateway provider injected into engine, verify DNS middleware wiring

**E2E tests** (test/):
- Docker container with nftables: full LAN simulation (two containers, one as gateway, one as LAN client)
- Verify DNS interception, TCP proxy, UDP proxy through gateway

## Data Flow

```
LAN Device (192.168.1.100)
  │
  │ TCP SYN to 93.184.216.34:443
  ▼
nftables/iptables TPROXY (inet k2r)
  │ mark=1 → ip rule → loopback → local listener
  ▼
gatewayProvider TCP listener (:12345)
  │ conn.LocalAddr() = "93.184.216.34:443" (original dest)
  ▼
dnsHandler.HandleTCP(conn, "93.184.216.34:443")
  │ port != 53 → pass through
  ▼
ClientTunnel.HandleTCP
  │ Router.Match("93.184.216.34") → Proxy
  ▼
wire.Dialer.DialTCP → QUIC/TCP-WS → k2s → 93.184.216.34:443
```

DNS flow:
```
LAN Device → DNS query to 8.8.8.8:53
  → TPROXY → gatewayProvider listener
  → dnsHandler.HandleUDP(pconn, "8.8.8.8:53")
  → port == 53 → dns.Middleware.HandleQuery()
  → k2rule route: direct → DirectDNSClient
                   proxy  → ProxyDNSClient (through tunnel)
  → response → LAN device
```

## Platform Mapping Update

| Platform | Binary | Shell | Provider | DirectDialer | NetworkMonitor |
|----------|--------|-------|----------|--------------|----------------|
| macOS sysext | xcframework | appext/ | tun (FD) | nePlatform | nePlatform |
| iOS | xcframework | appext/ | tun (FD) | nePlatform | nePlatform |
| Android | aar | appext/ | tun (FD) | protectedDialer | nil |
| Linux desktop | k2 | daemon/ | tun (self-create) | directDialer | singTunMonitor |
| Windows | k2 | daemon/ | tun (self-create) | directDialer | singTunMonitor |
| **Linux gateway** | **k2r** | **gateway/** | **gateway (TPROXY)** | **nil** | **nil** |

## Future Phases

- **Phase 2**: k2p Home — gateway/ registers with signal server, accepts Away peer connections via P2P/relay
- **Phase 3**: macOS gateway — pf + TUN/routing table approach
- **Phase 4**: Web UI — shared with k2app or standalone embedded dashboard

## DIAG Events

| Event | Level | Threshold | Fields |
|-------|-------|-----------|--------|
| `DIAG: gw-intercept-install` | INFO | on install | backend, subnets, excludeCount |
| `DIAG: gw-intercept-fail` | WARN | on error | backend, err |
| `DIAG: gw-lan-discover` | INFO | on discovery | subnets, ifaceCount |
| `DIAG: gw-sysctl` | INFO | on change | key, oldValue, newValue |
