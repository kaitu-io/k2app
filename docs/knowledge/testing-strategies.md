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

---

## Binary Wire Format Testing with Builder Functions (zero-config-stealth, 2026-02-12)

**Pattern**: Create test builder functions for complex binary formats to enable round-trip testing.

```go
// Test helper constructs valid binary format
func buildTestECHConfigList(configID uint8, kemID uint16, pubKey []byte, ...) []byte {
    // ... construct binary format following spec
    return data
}

func TestParseECHConfigBytes(t *testing.T) {
    // Build known-good binary data
    data := buildTestECHConfigList(42, 0x0020, pubKey, cipherSuites, 128, "example.com")

    // Parse it back
    tmpl, err := ParseECHConfigBytes(data)

    // Assert fields match inputs
    assert.Equal(t, 42, tmpl.ConfigID)
    assert.Equal(t, 0x0020, tmpl.KemID)
}
```

**Used in**:
- `wire/ech_provider_test.go` — ECH config parsing round-trip
- Similar pattern in stream protocol tests

**Validation**: Round-trip parse → build → parse proves format correctness.

**Benefits**:
- Self-documenting (builder shows format structure)
- Reusable across multiple test cases
- Catches off-by-one errors in parsing logic
- Independent of external data sources

---

## DNS Query Testing with Conditional Skips (zero-config-stealth, 2026-02-12)

**Pattern**: Test live DNS queries but skip gracefully when network unavailable.

```go
func TestLocalECHProvider_FetchTemplate_Live(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping live DNS query in short mode")
    }

    provider := &LocalECHProvider{}
    tmpl, err := provider.FetchTemplate()

    if err != nil {
        // Network may be down — skip instead of fail
        t.Skipf("DNS query failed (expected in offline/CI): %v", err)
    }

    // Assert template structure (not exact values — Cloudflare may update)
    if tmpl.KemID != 0x0020 {
        t.Errorf("unexpected kem_id (Cloudflare changed?): 0x%04x", tmpl.KemID)
    }
}
```

**Used in**: `wire/ech_provider_test.go` (TestLocalECHProvider_FetchTemplate)

**Validation**: Test runs in online environments, skips in offline/CI.

**Benefits**:
- CI doesn't flake on network issues
- Still validates live integration when possible
- Detects upstream changes (Cloudflare ECH format)

**Pattern**: Use `t.Skip()` for environmental dependencies, not `t.Error()`.

---

## Fallback Chain Testing (zero-config-stealth, 2026-02-12)

**Pattern**: Test each tier of fallback independently, then test full chain.

```go
// Test tier 1: live fetch
func TestFetchTemplate_Live(t *testing.T) { ... }

// Test tier 2: cache loading
func TestFetchTemplate_Cache(t *testing.T) {
    // Write cache file
    cacheTemplate("cache.json", &ECHTemplate{...})

    // Simulate DNS failure (no network), verify cache used
    tmpl, err := FetchTemplateWithFallback(failingProvider, "cache.json")
    assert.NoError(t, err)
}

// Test tier 3: hardcoded defaults
func TestFetchTemplate_Defaults(t *testing.T) {
    // No cache, no network
    tmpl, err := FetchTemplateWithFallback(failingProvider, "")
    assert.NoError(t, err)
    assert.Equal(t, "cloudflare-ech.com", tmpl.PublicName)
}
```

**Used in**: `wire/ech_provider_test.go` (FetchTemplateWithFallback tests)

**Validation**: Each fallback tier validated independently, full chain tested as integration.

**Benefits**:
- Clear failure isolation
- Each tier testable without environment setup
- Integration test proves chain works end-to-end

**Pattern**: Unit test each fallback tier, integration test full chain.

---

## Auto-Migration Testing (zero-config-stealth, 2026-02-12)

**Pattern**: Test backward-compatible format migrations with before/after file checks.

```go
func TestEnsureECHPrivateKey_Migration_OldFormat(t *testing.T) {
    dir := t.TempDir()
    keyPath := filepath.Join(dir, "ech.key")

    // Write old format (key+config bundled)
    oldData := append(privKeyBytes, configBytes...)
    os.WriteFile(keyPath, oldData, 0600)

    // Ensure key (triggers migration)
    privKey, err := EnsureECHPrivateKey(keyPath)
    assert.NoError(t, err)

    // Verify new format (key only)
    newData, _ := os.ReadFile(keyPath)
    assert.Equal(t, 32, len(newData)) // X25519 key = 32 bytes

    // Verify config moved to separate file
    configs, _ := LoadAllECHConfigs(filepath.Join(dir, "ech-configs"))
    assert.Len(t, configs, 1)
}
```

**Used in**: `wire/ech_test.go` (TestEnsureECHPrivateKey with old format)

**Validation**: Old format auto-migrates to new format, data preserved.

**Benefits**:
- Tests backward compatibility explicitly
- Catches migration bugs before deployment
- Documents migration path for operators

**Pattern**: Simulate old format, trigger migration, assert new format correct.
