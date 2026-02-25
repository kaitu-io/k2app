# test — End-to-End Integration Tests

Full stack integration tests: k2 client ↔ k2s server.

## Files

- `e2e_test.go` — E2E tests (TCP proxy, UDP/DNS, QUIC failover, ECH)
- `helpers_test.go` — test fixtures (config builders, server start, cleanup)

## Test Coverage

1. **TestE2E_ClientServerConnect** — handshake, auth, connection established
2. **TestE2E_QUIC_TCPProxy** — TCP proxy over QUIC transport
3. **TestE2E_QUIC_UDPProxy** — UDP proxy over QUIC transport
4. **TestE2E_TCPWS_TCPProxy** — TCP proxy over TCP-WS transport
5. **TestE2E_TransportManager_Failover** — QUIC blocked, verify TCP-WS fallback

## Architecture

Uses in-process server (no external dependencies):

```go
func TestE2E_TCPProxy(t *testing.T) {
    // Start test server
    srv := startTestServer(t)
    defer srv.Close()

    // Create test client (no TUN, uses mock provider)
    client := createTestClient(t, srv.Addr())

    // Test TCP proxy
    conn, _ := client.DialTCP(ctx, "example.com:80")
    // ... send HTTP request, verify response
}
```

## Test Helpers

- `newTestWireConfig()` — build wire.Config for tests
- `startTestServer(t)` — in-process k2s server on random port
- `createTestClient(t, addr)` — k2 client connected to test server
- `newMockProvider()` — Provider that injects test connections without TUN

## Validation

All tests pass without:
- External network access
- Root privileges
- TUN device creation
- Real VPS server

Tests use loopback networking, in-process setup, deterministic configs.

## Running

```bash
go test ./test/...           # All E2E tests
go test -v ./test/...        # Verbose output
go test -run TestE2E_TCP ./test/...  # Single test
```
