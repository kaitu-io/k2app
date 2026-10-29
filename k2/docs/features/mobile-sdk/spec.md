# Feature Spec: Mobile SDK (iOS + Android)

## Meta

| Field | Value |
|-------|-------|
| Feature | mobile-sdk |
| Status | Draft |
| Created | 2026-02-13 |
| Mode | Evolution (Baseline exists) |

## Summary

Expose the k2 tunnel engine as a mobile SDK via gomobile bind, producing .xcframework (iOS) and .aar (Android). The Go SDK lives in `mobile/` within the k2 repo. Native shells (NEPacketTunnelProvider / VpnService) pass a TUN file descriptor into the Go engine.

## Current State (from Baseline)

- `provider/tun_ios.go` and `provider/tun_android.go` already accept FD via `ProviderConfig.FileDescriptor`
- `daemon/tunnel.go:BuildTunnel()` assembles the full stack (k2rule + wire + router + provider)
- DNS middleware operates at L3 (raw IP packets), tightly coupled to TUN packet writer
- No mobile-facing API exists; `BuildTunnel` is desktop-specific (daemon, service install, etc.)

## Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| FFI | gomobile bind | Official Go tool, auto-generates ObjC/Java bindings, simple API fits type constraints |
| API pattern | Engine + EventHandler interface | gomobile maps Go interfaces to ObjC protocols / Java interfaces |
| DNS | Reuse full 13-step middleware, adapt to L4 | Feature parity with desktop, L4 adapter decouples from TUN packet format |
| Routing | Full k2rule support | Split routing (direct/proxy/reject), same rules as desktop |
| Code location | `mobile/` in k2 repo | Direct import of internal packages, single version |
| Platform priority | iOS + Android in parallel | Go SDK is platform-agnostic; native shells are independent |

## SDK API Surface

### mobile/mobile.go

```go
package mobile

// Engine is the main VPN tunnel instance.
type Engine struct {
    tunnel  *core.ClientTunnel
    handler EventHandler
    mu      sync.Mutex
    state   string // "disconnected", "connecting", "connected"
    cancel  context.CancelFunc
}

// NewEngine creates a new tunnel engine.
func NewEngine() *Engine

// Start connects using a k2v5:// URL and a TUN file descriptor from the platform.
// The fd must be an open TUN device (from NEPacketTunnelProvider or VpnService.Builder).
func (e *Engine) Start(url string, fd int) error

// Stop disconnects the tunnel and releases resources.
func (e *Engine) Stop() error

// Status returns "disconnected", "connecting", or "connected".
func (e *Engine) Status() string

// SetEventHandler registers a callback handler for state changes, errors, and stats.
func (e *Engine) SetEventHandler(handler EventHandler)
```

### mobile/event.go

```go
package mobile

// EventHandler receives events from the engine.
// gomobile maps this to:
//   iOS:     MobileEventHandler protocol (ObjC)
//   Android: mobile.EventHandler interface (Java)
type EventHandler interface {
    OnStateChange(state string)
    OnError(message string)
    OnStats(txBytes, rxBytes int64)
}

// State constants
const (
    StateDisconnected = "disconnected"
    StateConnecting   = "connecting"
    StateConnected    = "connected"
)
```

## Engine.Start() Internal Flow

```
Start(url, fd)
  1. Parse k2v5:// URL → wire.Config
  2. k2rule.Init(config)     — load rules from built-in or URL param
  3. Build transports         — QUICClient + TCPWSClient + TransportManager
  4. core.NewRouter()         — uses k2rule.Match (TmpRule from DNS)
  5. NewTUNProvider(fd)       — platform-specific, gVisor stack
  6. Wire DNS middleware      — L4 adapter: intercept UDP:53 in HandleUDP
  7. core.NewClientTunnel()   — connect provider + router + dialer
  8. tunnel.Start(ctx)
  9. setState("connected"), emit OnStateChange
```

## DNS Middleware Adaptation (L3 → L4)

### Current (L3, desktop only)

```
TUN raw packet → DNS middleware (pkt []byte, info packet.Info)
  → parse IP/UDP headers
  → DNS pipeline (13 steps)
  → build IP/UDP response → write to TUN
```

### New (L4, works everywhere)

```
HandleUDP(conn, dest)
  → if dest port == 53:
      → read DNS query bytes from conn
      → DNS engine (13 steps, input: query bytes, output: response bytes)
      → write response bytes to conn
  → else:
      → normal UDP routing (direct/proxy)
```

Changes needed:
- Extract DNS engine core into `core/dns.Engine` or refactor `Middleware` to accept query bytes instead of raw packets
- `extractUDPPayload` / `buildUDPIP` become unnecessary for the DNS path
- The 13-step pipeline (hosts → cache → filter → k2rule → upstream → SetTmpRule) stays identical
- Desktop TUN mode can also use L4 path (simplification)

## Platform Integration

### iOS — NEPacketTunnelProvider

```swift
// Network Extension target
class K2PacketTunnel: NEPacketTunnelProvider {
    let engine = MobileNewEngine()

    override func startTunnel(options: [String: NSObject]?) async throws {
        guard let url = (protocolConfiguration as? NETunnelProviderProtocol)?
            .providerConfiguration?["url"] as? String else {
            throw NSError(domain: "K2", code: 1)
        }

        let settings = NEPacketTunnelNetworkSettings(tunnelRemoteAddress: "10.0.0.1")
        settings.ipv4Settings = NEIPv4Settings(
            addresses: ["10.0.0.1"], subnetMasks: ["255.255.255.0"])
        settings.ipv4Settings?.includedRoutes = [NEIPv4Route.default()]
        settings.dnsSettings = NEDNSSettings(servers: ["10.0.0.2"])
        settings.mtu = 1400

        try await setTunnelNetworkSettings(settings)

        let fd = packetFlow.value(forKeyPath: "socket.fileDescriptor") as! Int32
        try engine.start(url, fd: Int(fd))
    }

    override func stopTunnel(with reason: NEProviderStopReason) async {
        try? engine.stop()
    }
}
```

Xcode project structure:
```
K2App/
  K2App/           — main app (VPN config UI, on/off toggle)
  K2Tunnel/        — Network Extension target (K2PacketTunnel)
  K2Mobile.xcframework  — gomobile output
```

### Android — VpnService

```kotlin
class K2VpnService : VpnService() {
    private val engine = Mobile.newEngine()

    fun connect(url: String) {
        val iface = Builder()
            .addAddress("10.0.0.1", 24)
            .addRoute("0.0.0.0", 0)
            .addDnsServer("10.0.0.2")
            .setMtu(1400)
            .setBlocking(true)
            .establish() ?: return

        val fd = iface.detachFd()
        engine.start(url, fd)
    }

    fun disconnect() {
        engine.stop()
    }
}
```

Android project structure:
```
k2-android/
  app/              — main app (VPN config UI)
  k2mobile.aar      — gomobile output
```

## Build Commands

```bash
# Prerequisites
go install golang.org/x/mobile/cmd/gomobile@latest
gomobile init

# iOS — xcframework (arm64 device + arm64/amd64 simulator)
gomobile bind -target=ios -o build/K2Mobile.xcframework ./mobile/

# Android — aar (arm64 + arm + x86_64 + x86)
gomobile bind -target=android -o build/k2mobile.aar -androidapi 21 ./mobile/
```

## File Structure (new/modified)

```
mobile/                    # NEW — gomobile-exported SDK package
  mobile.go               # Engine, NewEngine, Start, Stop, Status
  event.go                # EventHandler interface, state constants
  dns.go                  # L4 DNS adapter (wraps core/dns middleware)

core/dns/
  engine.go               # NEW — L4-friendly DNS engine (query bytes → response bytes)
  middleware.go            # MODIFIED — refactor to use engine.go internally
```

## Acceptance Criteria

### AC1: gomobile bind produces valid artifacts
- `gomobile bind -target=ios ./mobile/` produces `K2Mobile.xcframework` without errors
- `gomobile bind -target=android ./mobile/` produces `k2mobile.aar` without errors

### AC2: Engine lifecycle works
- `NewEngine()` + `Start(url, fd)` + `Stop()` cycle completes without crash
- `Status()` returns correct state at each stage
- `Start()` on already-started engine returns error
- `Stop()` on already-stopped engine is idempotent

### AC3: EventHandler callbacks fire
- `OnStateChange("connecting")` fires during Start
- `OnStateChange("connected")` fires after successful Start
- `OnStateChange("disconnected")` fires after Stop
- `OnError(msg)` fires on connection failure

### AC4: Traffic flows through tunnel
- TCP connection through the tunnel reaches the server and echoes back
- UDP connection through the tunnel reaches the server and echoes back
- DNS query through the tunnel resolves correctly

### AC5: k2rule routing works on mobile
- Direct-routed domains bypass the tunnel
- Proxy-routed domains go through the tunnel
- Rejected domains get blocked

### AC6: DNS middleware runs on mobile
- DNS queries intercepted at L4 (port 53 in HandleUDP)
- Full 13-step pipeline executes (cache, filter, k2rule, upstream, SetTmpRule)
- DNS-learned IPs get correct routing via SetTmpRule

### AC7: Platform integration compiles
- iOS Network Extension target links K2Mobile.xcframework and compiles
- Android app includes k2mobile.aar and compiles
- Both platforms can create TUN FD and pass to Engine.Start

## Out of Scope (v1)

- Stats collection (OnStats callback) — stub for now, implement in v2
- Auto-reconnect on network change — native shell responsibility
- CocoaPods / Maven distribution — binary artifacts first
- Background keep-alive / push notifications
- Split tunnel UI (k2rule config is internal, not user-facing in v1)
- Certificate pinning UI
