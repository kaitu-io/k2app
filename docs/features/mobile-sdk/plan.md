# Execution Plan: Mobile SDK (iOS + Android) — v2

## Meta

| Field | Value |
|-------|-------|
| Feature | mobile-sdk |
| Spec | docs/features/mobile-sdk/spec.md |
| Complexity | Moderate (~8 files, 1 refactoring + 1 new package) |
| Tasks | 3 (sequential) |
| Revision | v2 — architecture review: composition over mutation, no ClientTunnel changes |

## Dependency Graph

```
T1 (DNS HandleQuery) → T2 (Mobile Engine + DNS Handler) → T3 (gomobile Build)
```

Sequential: T2 calls T1's HandleQuery, T3 builds T2's package.

## Architecture Review Changes (v1 → v2)

| v1 (old) | v2 (new) | Reason |
|----------|----------|--------|
| New `DNSEngine` type | Add `HandleQuery` method to existing `Middleware` | Avoids type duplication, Middleware already has all fields |
| T2: modify ClientTunnel to intercept DNS | DNS handler wrapper in `mobile/` package | Composition over mutation. ClientTunnel stays unchanged (SoC) |
| 4 tasks (T1-T4) | 3 tasks (T1-T3) | Merged DNS wiring into T2 since it's mobile-specific |
| Modifies `core/tunnel.go`, `daemon/tunnel.go`, `test/e2e_test.go` | Zero changes to core/tunnel, daemon, or e2e | Backwards compatible, no blast radius |

Key insight: DNS Middleware was never wired into the runtime (dead code). The L3 `HandleDNSQuery` is unused. Adding `HandleQuery` is the first time DNS actually runs. The DNS interception wrapper lives in `mobile/` via ConnectionHandler composition — Provider → dnsHandler(ClientTunnel) — without touching ClientTunnel.

## AC Coverage Map

| AC | Test | Task |
|----|------|------|
| AC1: gomobile produces artifacts | build script verification | T3 |
| AC2: Engine lifecycle | TestEngine_NewEngine, TestEngine_Start_InvalidURL, TestEngine_DoubleStart, TestEngine_Stop_NotStarted | T2 |
| AC3: EventHandler callbacks | TestEngine_EventHandler_StateChanges, TestEngine_EventHandler_OnError | T2 |
| AC4: Traffic flows | TestEngine_TCP_Proxy, TestEngine_UDP_Proxy (mock provider, e2e pattern) | T2 |
| AC5: k2rule routing | TestEngine_Routing_Direct, TestEngine_Routing_Proxy | T2 |
| AC6: DNS middleware on mobile | TestHandleQuery_Direct, TestHandleQuery_Proxy, TestDNSHandler_Intercept | T1 + T2 |
| AC7: Platform compiles | gomobile bind ios + android | T3 |

---

## T1: Add HandleQuery to DNS Middleware

**Goal**: Add L4-friendly `HandleQuery([]byte) ([]byte, error)` method to existing Middleware. Extract `DNSQuerier` interface for testability.

**Files**:
- `core/dns/middleware.go` — MODIFY (add HandleQuery method, extract DNSQuerier interface)
- `core/dns/middleware_test.go` — MODIFY (add HandleQuery tests)

**depends_on**: none

### Design

Add a new method to the existing `Middleware` struct that accepts raw DNS query bytes and returns raw DNS response bytes. This runs steps 2-12 of the pipeline without any L3 packet handling. The existing `HandleDNSQuery` (L3) delegates to `HandleQuery` internally.

Extract `DNSQuerier` interface for testability (both DirectDNSClient and ProxyDNSClient already satisfy it):

```go
// core/dns/middleware.go additions

// DNSQuerier is satisfied by DirectDNSClient and ProxyDNSClient.
type DNSQuerier interface {
    Query(query *dns.Msg) (*dns.Msg, error)
}

// Change Middleware fields from concrete to interface:
type Middleware struct {
    directDNS DNSQuerier  // was *DirectDNSClient
    proxyDNS  DNSQuerier  // was *ProxyDNSClient
    dnsCache  *DNSCache
    hosts     *HostsFile
    filter    *Filter
    metrics   *Metrics
    writer    io.Writer   // L3 mode only, nil for L4
}

// HandleQuery processes raw DNS query bytes through the full pipeline.
// Returns raw DNS response bytes. This is the L4-friendly entry point.
func (m *Middleware) HandleQuery(query []byte) ([]byte, error) {
    // 1. Parse DNS query
    var msg dns.Msg
    if err := msg.Unpack(query); err != nil {
        return nil, fmt.Errorf("unpack DNS query: %w", err)
    }
    // 2-11. Full pipeline (hosts, filter, cache, k2rule, upstream, SetTmpRule)
    resp, err := m.processQuery(&msg)
    if err != nil {
        return nil, err
    }
    // 12. Pack response
    return resp.Pack()
}

// processQuery runs the DNS pipeline on a parsed message.
// Shared by both HandleQuery (L4) and HandleDNSQuery (L3).
func (m *Middleware) processQuery(query *dns.Msg) (*dns.Msg, error) {
    // Steps 3-12 from current HandleDNSQuery (extract domain, hosts, filter,
    // cache, k2rule, upstream, cache store, SetTmpRule)
}

// HandleDNSQuery (existing L3 method) becomes:
func (m *Middleware) HandleDNSQuery(pkt []byte, info packet.Info) error {
    udpPayload, err := packet.ExtractUDPPayload(pkt)
    if err != nil { return err }
    respBytes, err := m.HandleQuery(udpPayload)
    if err != nil { return err }
    respPacket, err := packet.BuildUDPIP(info.DstIP, info.SrcIP, info.DstPort, info.SrcPort, respBytes)
    if err != nil { return err }
    _, err = m.writer.Write(respPacket)
    return err
}
```

Also update `MiddlewareConfig` to use `DNSQuerier` interface:
```go
type MiddlewareConfig struct {
    DirectDNS DNSQuerier  // was *DirectDNSClient
    ProxyDNS  DNSQuerier  // was *ProxyDNSClient
    DNSCache  *DNSCache
    Hosts     *HostsFile
    Filter    *Filter
    Metrics   *Metrics
    Writer    io.Writer   // nil for L4-only usage
}
```

### TDD Steps

**RED**: Write tests for HandleQuery using mock DNSQuerier
```go
// core/dns/middleware_test.go (additions)

// mockQuerier implements DNSQuerier for testing
type mockQuerier struct {
    resp *dns.Msg
    err  error
}
func (m *mockQuerier) Query(q *dns.Msg) (*dns.Msg, error) { return m.resp, m.err }

func TestHandleQuery_DirectDomain(t *testing.T)
    // k2rule.Init with direct rule for "direct.example.com"
    // mockQuerier returns A record 1.2.3.4
    // HandleQuery → valid response bytes, unpack → has A record

func TestHandleQuery_ProxyDomain(t *testing.T)
    // k2rule.Init with proxy rule
    // proxyDNS mockQuerier returns A record
    // HandleQuery → valid response bytes via proxy path

func TestHandleQuery_RejectDomain(t *testing.T)
    // k2rule.Init with reject rule
    // HandleQuery → NXDOMAIN response bytes

func TestHandleQuery_CacheHit(t *testing.T)
    // Query once → upstream called
    // Query same domain again → upstream NOT called, cache hit

func TestHandleQuery_InvalidBytes(t *testing.T)
    // HandleQuery(garbage) → error

func TestHandleQuery_NilWriter(t *testing.T)
    // Middleware with nil Writer (L4 mode)
    // HandleQuery works fine (doesn't touch writer)
```

**GREEN**:
- Extract `DNSQuerier` interface
- Change Middleware/MiddlewareConfig to use DNSQuerier
- Extract `processQuery` from HandleDNSQuery (steps 3-12)
- Add `HandleQuery` method that unpacks → processQuery → packs
- Refactor `HandleDNSQuery` to: extract UDP → HandleQuery → wrap IP → write

**REFACTOR**:
- Verify existing middleware tests still pass (since interface is a superset)
- Remove any duplicate code between HandleQuery and HandleDNSQuery
- Ensure no L3 imports (packet) leak into processQuery

### Verification

```bash
go test ./core/dns/... -v -run TestHandleQuery
go test ./core/dns/... -v   # All existing DNS tests still pass
go build ./...               # Full build passes
```

---

## T2: Mobile Engine Package

**Goal**: Create `mobile/` package with Engine, EventHandler, and DNS-intercepting ConnectionHandler wrapper. Ready for gomobile bind.

**Files**:
- `mobile/mobile.go` — NEW (Engine struct, Start/Stop/Status)
- `mobile/event.go` — NEW (EventHandler interface, state constants)
- `mobile/mobile_test.go` — NEW (lifecycle, events, DNS interception tests)

**depends_on**: T1

### Design

#### Engine (mobile/mobile.go)

Engine.Start() assembles the tunnel stack (simplified BuildTunnel without daemon/service logic):

```go
package mobile

func (e *Engine) Start(url string, fd int) error {
    e.mu.Lock()
    defer e.mu.Unlock()
    if e.state != StateDisconnected {
        return fmt.Errorf("engine: already started")
    }
    e.setState(StateConnecting)

    // 1. Parse k2v5:// URL
    wireCfg, err := wire.ParseURL(url)
    if err != nil { return e.fail(err) }

    // 2. Init k2rule (global mode for mobile v1)
    k2rule.Init(&k2rule.Config{IsGlobal: true, GlobalTarget: k2rule.TargetProxy})

    // 3. Build transports
    quicClient := wire.NewQUICClient(wireCfg, nil)
    tcpwsClient := wire.NewTCPWSClient(wireCfg, nil)
    tm := wire.NewTransportManager()
    tm.SetQUIC(quicClient)
    tm.SetTCPWS(tcpwsClient)

    // 4. Router
    router := core.NewRouter()

    // 5. DNS middleware (L4 mode — no writer needed)
    dnsMW := dns.NewMiddleware(dns.MiddlewareConfig{
        ProxyDNS: dns.NewProxyDNSClient(dns.ProxyDNSClientConfig{Dialer: tm}),
        DNSCache: dns.NewDNSCache(dns.DNSCacheConfig{}),
        Filter:   dns.NewFilter(),
    })

    // 6. TUN provider (from platform FD)
    prov := provider.NewTUNProvider(provider.ProviderConfig{
        FileDescriptor: fd,
    })

    // 7. Client tunnel (does NOT own DNS — composition via dnsHandler)
    ctx, cancel := context.WithCancel(context.Background())
    e.cancel = cancel
    e.tunnel = core.NewClientTunnel(core.ClientTunnelConfig{
        Provider: prov,
        Dialer:   tm,
        Router:   router,
    })

    // 8. Start with DNS-intercepting handler wrapper
    //    Provider → dnsHandler(tunnel, dnsMW) instead of Provider → tunnel
    if err := prov.Start(ctx, &dnsHandler{inner: e.tunnel, dns: dnsMW}); err != nil {
        cancel()
        tm.Close()
        return e.fail(err)
    }

    e.setState(StateConnected)
    return nil
}
```

Note: We call `prov.Start()` directly instead of `tunnel.Start()`, because we need to insert the dnsHandler wrapper between provider and tunnel. The tunnel's internal `provider.Start(ctx, t)` is bypassed — we manually compose the handler chain.

#### DNS Handler Wrapper (inline in mobile/mobile.go)

```go
// dnsHandler wraps a ConnectionHandler to intercept DNS queries (port 53).
// Non-DNS traffic passes through to the inner handler unchanged.
type dnsHandler struct {
    inner provider.ConnectionHandler
    dns   *dns.Middleware
}

func (h *dnsHandler) HandleTCP(conn net.Conn, dest string) {
    h.inner.HandleTCP(conn, dest)
}

func (h *dnsHandler) HandleUDP(conn net.PacketConn, dest string) {
    _, port, _ := net.SplitHostPort(dest)
    if port == "53" && h.dns != nil {
        h.serveDNS(conn)
        return
    }
    h.inner.HandleUDP(conn, dest)
}

func (h *dnsHandler) serveDNS(conn net.PacketConn) {
    defer conn.Close()
    buf := make([]byte, 4096)
    for {
        n, addr, err := conn.ReadFrom(buf)
        if err != nil { return }
        resp, err := h.dns.HandleQuery(buf[:n])
        if err != nil { continue }
        conn.WriteTo(resp, addr)
    }
}
```

This is the **composition pattern**: `dnsHandler` wraps `ClientTunnel` without modifying it. ClientTunnel remains unchanged — zero blast radius.

#### EventHandler (mobile/event.go)

```go
package mobile

type EventHandler interface {
    OnStateChange(state string)
    OnError(message string)
    OnStats(txBytes, rxBytes int64)
}

const (
    StateDisconnected = "disconnected"
    StateConnecting   = "connecting"
    StateConnected    = "connected"
)
```

### gomobile Compatibility Rules

- All exported types: basic types, string, []byte, error, interfaces with compatible methods
- EventHandler methods use only string and int64 params
- No channels, no func args, no generics in exported API
- Engine is a struct (not interface) for gomobile export
- dnsHandler is unexported (internal composition detail)

### TDD Steps

**RED**: Write tests for Engine lifecycle, events, and DNS interception
```go
// mobile/mobile_test.go

// --- Lifecycle tests ---

func TestEngine_NewEngine(t *testing.T)
    // NewEngine returns non-nil, state == "disconnected"

func TestEngine_Start_InvalidURL(t *testing.T)
    // Start("garbage", 0) → error, state stays "disconnected"

func TestEngine_DoubleStart(t *testing.T)
    // Start → Start again → error "already started"

func TestEngine_Stop_NotStarted(t *testing.T)
    // Stop on fresh engine → no error (idempotent)

func TestEngine_Status(t *testing.T)
    // New → "disconnected"

// --- EventHandler tests ---

func TestEngine_EventHandler_StateChanges(t *testing.T)
    // Set mock EventHandler
    // Start with invalid URL → OnStateChange("connecting"), OnError(msg)
    // Verify callback order

func TestEngine_EventHandler_OnError(t *testing.T)
    // Start with invalid URL → OnError called with message

// --- DNS interception tests ---

func TestDNSHandler_Intercept_Port53(t *testing.T)
    // Create dnsHandler with mock inner handler + mock Middleware
    // Call HandleUDP with port 53 dest
    // Expect: DNS response returned, inner.HandleUDP NOT called

func TestDNSHandler_PassThrough_NonDNS(t *testing.T)
    // Create dnsHandler
    // Call HandleUDP with port 8888 dest
    // Expect: inner.HandleUDP called, DNS not touched

func TestDNSHandler_PassThrough_TCP(t *testing.T)
    // HandleTCP always passes through to inner

func TestDNSHandler_NilDNS_PassThrough(t *testing.T)
    // dnsHandler with nil dns → port 53 passes through to inner
```

Note: Full traffic tests (AC4/AC5) require wire server setup. Use e2e test patterns (mockProvider + pipe connections) for integration-level tests in a separate test file if needed.

**GREEN**:
- Create mobile/event.go with EventHandler + state constants
- Create mobile/mobile.go with Engine (NewEngine, Start, Stop, Status, SetEventHandler, setState, fail) + dnsHandler
- Start() assembles: parse URL → k2rule → transports → router → DNS middleware → tunnel → dnsHandler(tunnel) → prov.Start(ctx, dnsHandler)
- Stop() calls cancel + tunnel.Close()

**REFACTOR**:
- Verify all exported types are gomobile-compatible
- Verify `go build ./mobile/` succeeds on host
- Check fail() properly cleans up partial state (cancel, close transport)
- Ensure dnsHandler is unexported

### Verification

```bash
go test ./mobile/... -v
go build ./mobile/...
go build ./...           # Full build still works
```

---

## T3: gomobile Build Verification

**Goal**: Verify gomobile bind produces valid .xcframework and .aar artifacts. Add Makefile targets.

**Files**:
- `Makefile` — MODIFY (add mobile build targets)

**depends_on**: T2

### Design

Add build targets:

```makefile
.PHONY: mobile-ios mobile-android mobile

mobile-ios:
	gomobile bind -target=ios -o build/K2Mobile.xcframework ./mobile/

mobile-android:
	gomobile bind -target=android -o build/k2mobile.aar -androidapi 21 ./mobile/

mobile: mobile-ios mobile-android
```

### TDD Steps

**RED**:
- Run `gomobile bind -target=ios ./mobile/` → expect success
- Run `gomobile bind -target=android ./mobile/` → expect success
- Verify output files exist and have non-zero size

**GREEN**:
- Install gomobile: `go install golang.org/x/mobile/cmd/gomobile@latest && gomobile init`
- Create build/ directory
- Run gomobile bind for both platforms
- Fix any type compatibility issues

**REFACTOR**:
- Add `build/` to .gitignore
- Verify xcframework contains arm64 + simulator slices
- Verify aar contains arm64 + x86_64

### Verification

```bash
make mobile-ios      # K2Mobile.xcframework created
make mobile-android  # k2mobile.aar created
ls -la build/        # Both artifacts present
```

### Known Risks

- sing-tun CGo dependencies may cause cross-compilation issues
- Android NDK required for android target
- iOS build requires macOS with Xcode command line tools
- If CGo issues arise, may need build tag adjustments on mobile/mobile.go

---

## Execution Notes

### Why Not Modify ClientTunnel (Architecture Decision)

The original plan (v1) added DNS interception directly to ClientTunnel. The v2 plan uses ConnectionHandler composition instead:

1. **SoC**: ClientTunnel routes TCP/UDP. DNS is a separate concern.
2. **Zero blast radius**: No changes to core/tunnel.go, daemon/tunnel.go, or test/e2e_test.go.
3. **Testability**: dnsHandler is independently testable with mock inner handler.
4. **Desktop unaffected**: Desktop path continues to not use DNS middleware (dead code). When desktop DNS is needed, the same pattern can be applied there.

### Provider.Start() vs Tunnel.Start()

In mobile, we call `prov.Start(ctx, dnsHandler)` directly instead of `tunnel.Start()`. This is because `tunnel.Start()` passes itself as the handler to provider:

```go
func (t *ClientTunnel) Start(ctx context.Context) error {
    return t.provider.Start(t.ctx, t)  // t = ClientTunnel as handler
}
```

We need to insert the dnsHandler between provider and tunnel, so we compose manually. The tunnel's internal context management still works — we just don't use its `Start()` method. Instead, Engine manages the ctx/cancel lifecycle.

### DNS Middleware on Mobile vs Desktop

- **Mobile (this plan)**: DNS queries arrive as L4 UDP connections (port 53) from gVisor stack. Platform (iOS/Android) configures DNS servers to route through TUN. dnsHandler intercepts in HandleUDP.
- **Desktop (current)**: DNS middleware exists but is dead code — `HandleDNSQuery` is never called. Desktop can adopt the same L4 pattern later.
- **DNS hijacking**: On mobile, sing-tun does NOT perform DNS hijacking. The platform VPN API (NEPacketTunnelNetworkSettings / VpnService.Builder) sets DNS servers. This is independent and stable.

### Build Tags

Provider has build tags:
- `tun_desktop.go`: `(darwin && !ios) || linux || windows`
- `tun_ios.go`: `ios`
- `tun_android.go`: `android`

gomobile compiles for the target platform, selecting the correct provider. Host tests (go test) use `tun_desktop.go` — this is fine since tests don't create real TUN providers.

### What's NOT in This Plan

Per spec "Out of Scope":
- OnStats callback (stub only)
- Auto-reconnect
- CocoaPods/Maven distribution
- Native app shells (K2App, k2-android)
- Split tunnel UI
- Desktop DNS wiring (separate PR)
