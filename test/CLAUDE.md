# test — End-to-End Integration Tests

Full stack integration tests: k2 client ↔ k2s server.

## Files

- `e2e_test.go` — E2E tests (TCP proxy, UDP proxy, QUIC failover, transport manager)
- `helpers_test.go` — test fixtures (env setup, echo servers, mock provider)
- `live/` — Live network diagnostic tests (skip in `-short` mode)

## Test Coverage

1. **TestE2E_QUIC_TCPProxy** — TCP proxy over QUIC transport
2. **TestE2E_QUIC_UDPProxy** — UDP proxy over QUIC transport
3. **TestE2E_TCPWS_TCPProxy** — TCP proxy over TCP-WS transport
4. **TestE2E_TransportManager_Failover** — QUIC blocked, verify TCP-WS fallback
5. **TestE2E_ClientServerConnect** — QUIC handshake + stream open to local echo server

## Architecture

Uses in-process server (no external dependencies):

```go
func TestE2E_QUIC_TCPProxy(t *testing.T) {
    env := setupQUICEnv(t)      // QUIC server + client on loopback
    defer env.Close()
    echoAddr := startTCPEchoServer(t)

    prov := &mockProvider{}
    tunnel := core.NewClientTunnel(core.ClientTunnelConfig{
        Provider: prov, Dialer: env.quicClient, Router: core.NewRouter(),
    })
    // ... inject connection, verify echo
}
```

## Test Helpers (helpers_test.go)

- `setupQUICEnv(t)` — QUIC server + client on loopback, returns `testEnv`
- `setupTCPWSEnv(t)` — TCP-WS server + client via net.Pipe, returns `testEnv`
- `startTCPEchoServer(t)` — TCP echo server on random port, returns address
- `startUDPEchoServer(t)` — UDP echo server on random port, returns address
- `mockProvider` — Provider that injects TCP/UDP connections without TUN
- `generateTestCert(t)` — self-signed EC P-256 cert for TLS

## Gotchas

- `AllowPrivateIP = true` must be set on `ProxyHandler` — echo servers run on `127.0.0.1`, which is blocked by default
- `net.Pipe()` is synchronous — writes block until reads happen, structure goroutines carefully

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
