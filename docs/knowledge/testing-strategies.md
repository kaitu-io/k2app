# Testing Strategies

Proven testing patterns from implemented features.

## Interface Mocking for Wire Transport (l4-architecture, 2026-02-12)

**Pattern**: Mock wire.Dialer for testing tunnel logic without real QUIC/TCP connections.

```go
type mockDialer struct {
    dialTCPFunc func(ctx context.Context, addr string) (net.Conn, error)
    dialUDPFunc func(ctx context.Context, addr string) (net.PacketConn, error)
}

func (m *mockDialer) DialTCP(ctx context.Context, addr string) (net.Conn, error) {
    return m.dialTCPFunc(ctx, addr)
}
```

**Used in**:
- `core/tunnel_test.go` — test routing without real server
- `core/dns/proxy_test.go` — test DNS forwarding

**Validation**: All tunnel tests pass without network I/O.

**Benefits**:
- Fast (no real connection overhead)
- Deterministic (inject specific errors)
- Isolated (test tunnel logic independent of wire implementation)

---

## In-Process Server for E2E Tests (l4-architecture, 2026-02-12)

**Pattern**: Start k2s server in same process as test, use loopback networking.

```go
func TestE2E_TCPProxy(t *testing.T) {
    // Start server
    srv := startTestServer(t)
    defer srv.Close()

    // Create client (no TUN needed)
    client := createTestClient(t, srv.Addr())

    // Test TCP proxy through tunnel
    conn, _ := client.DialTCP(ctx, "example.com:80")
    // ... assert behavior
}
```

**Used in**: `test/e2e_test.go` (all E2E tests)

**Validation**: Full integration without external dependencies.

**Benefits**:
- Reproducible (no external server required)
- Fast (loopback, no internet dependency)
- Debuggable (single process, easy breakpoints)

---

## Compile-Time Interface Checks (l4-architecture, 2026-02-12)

**Pattern**: Add `var _ Interface = (*Impl)(nil)` to ensure implementations satisfy interfaces.

```go
// In wire/quic.go
var _ Dialer = (*QUICClient)(nil)
var _ Listener = (*QUICServer)(nil)

// In core/tunnel.go
var _ provider.ConnectionHandler = (*ClientTunnel)(nil)
```

**Used in**: All wire transports, tunnel, provider implementations.

**Validation**: Compilation fails if interface contract broken.

**Benefits**:
- Catch interface violations at compile time
- Self-documenting (shows intent)
- Refactoring safety (interface changes break loudly)

---

## Platform Build Tag Testing (l4-architecture, 2026-02-12)

**Pattern**: Test platform-specific code with build tags, verify all platforms compile.

```bash
# Test iOS build
go build -tags ios ./provider/...

# Test Android build
go build -tags android ./provider/...

# Test desktop builds
GOOS=darwin go build ./provider/...
GOOS=linux go build ./provider/...
```

**Used in**: `provider/` package (TUN implementations per platform).

**Validation**: CI matrix builds all platforms.

**Gotcha**: Can't run iOS/Android tests on CI without simulators — compile-only validation sufficient for interfaces.

---

## Bidirectional Pipe Testing (l4-architecture, 2026-02-12)

**Pattern**: Test TCP/UDP forwarding with echoing servers.

```go
func TestClientTunnel_HandleTCP_Proxy(t *testing.T) {
    // Mock echo server
    ln, _ := net.Listen("tcp", "127.0.0.1:0")
    go func() {
        conn, _ := ln.Accept()
        io.Copy(conn, conn) // Echo back
    }()

    // Inject connection to tunnel
    local, remote := net.Pipe()
    go tunnel.HandleTCP(local, ln.Addr().String())

    // Write to remote end, expect echo
    remote.Write([]byte("hello"))
    buf := make([]byte, 5)
    remote.Read(buf)
    assert.Equal(t, "hello", string(buf))
}
```

**Used in**:
- `core/tunnel_test.go` — TCP/UDP proxy
- `server/handler_test.go` — server-side forwarding

**Validation**: Data flows correctly in both directions.

**Benefits**: Simple, no complex mock needed for full pipe test.

---

## Test Fixtures for Common Configs (l4-architecture, 2026-02-12)

**Pattern**: Extract test config builders to reduce duplication.

```go
// In test/helpers_test.go
func newTestWireConfig() wire.Config {
    return wire.Config{
        UDID:  "test-udid",
        Token: "test-token",
        Host:  "127.0.0.1",
        Port:  8443,
        SNI:   "test.example.com",
    }
}

func startTestServer(t *testing.T) *testServer {
    cfg := newTestServerConfig()
    srv := server.NewServer(cfg)
    // ... start and return
}
```

**Used in**: `test/e2e_test.go`, `test/helpers_test.go`

**Validation**: Consistent configs across all E2E tests.

**Benefits**:
- DRY (don't repeat config setup)
- Easy to update (change one place)
- Readable (test focuses on behavior, not boilerplate)

---

## Table-Driven Routing Tests (l4-architecture, 2026-02-12)

**Pattern**: Test router with multiple scenarios in table format.

```go
func TestRouter_RouteAddr(t *testing.T) {
    tests := []struct {
        addr   string
        want   k2rule.Target
    }{
        {"8.8.8.8", k2rule.TargetDirect},         // IP
        {"example.com", k2rule.TargetProxy},      // Domain
        {"lan.local", k2rule.TargetDirect},       // LAN
        {"blocked.com", k2rule.TargetReject},     // Blocked
    }
    for _, tt := range tests {
        got := router.RouteAddr(tt.addr)
        assert.Equal(t, tt.want, got)
    }
}
```

**Used in**: Router, k2rule integration tests.

**Validation**: Comprehensive coverage with minimal code.

**Benefits**: Easy to add test cases, clear intent.
