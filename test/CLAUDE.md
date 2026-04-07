# test — End-to-End Integration Tests

Full stack integration tests: k2 client ↔ k2s server.

## Files

- `e2e_test.go` — E2E tests (TCP proxy, UDP proxy, QUIC failover, transport manager)
- `k2p_e2e_test.go` — k2p P2P E2E tests (ICE direct, auth rejection, pin rejection, TCP data flow)
- `helpers_test.go` — test fixtures (env setup, echo servers, mock provider)
- `live/` — Live network diagnostic tests (skip in `-short` mode)
- `k2r-uat/` — Docker-based gateway integration tests (56 test points, 10 phases)

## Test Coverage

1. **TestE2E_QUIC_TCPProxy** — TCP proxy over QUIC transport
2. **TestE2E_QUIC_UDPProxy** — UDP proxy over QUIC transport
3. **TestE2E_TCPWS_TCPProxy** — TCP proxy over TCP-WS transport
4. **TestE2E_TransportManager_Failover** — QUIC blocked, verify TCP-WS fallback
5. **TestE2E_ClientServerConnect** — QUIC handshake + stream open to local echo server
6. **TestK2P_E2E_ICEDirect** — k2p: signal + ICE + QUIC + peer auth + TCP data flow
7. **TestK2P_E2E_AuthRejection** — k2p: wrong PeerSecret rejected at QUIC level
8. **TestK2P_E2E_PinRejection** — k2p: wrong pin rejected at TLS level

## Architecture

Uses in-process server (no external dependencies):

```go
func TestE2E_QUIC_TCPProxy(t *testing.T) {
    env := setupQUICEnv(t)      // QUIC server + client on loopback
    defer env.Close()
    echoAddr := startTCPEchoServer(t)

    prov := &mockProvider{}
    tunnel := core.NewClientTunnel(core.ClientTunnelConfig{
        Provider:  prov,
        Outbounds: []core.Outbound{direct, nil, wireOutbound},
        Router:    core.NewRouter(ruleEngine),
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
make gateway-uat             # k2r Docker UAT (requires Docker)
test/k2r-uat/run.sh --phase rules   # Single phase
test/k2r-uat/run.sh --keep          # Keep containers for debugging
```

## k2r Gateway UAT (k2r-uat/)

Docker-based integration test for the k2r gateway binary as a full soft router.

**Topology**: `lan-client (10.99.0.30) → k2r (10.99.0.20, TPROXY) → k2s (10.99.0.10) → internet`

**10 Phases, 56 test points:**

| Phase | Tests | Covers |
|-------|-------|--------|
| 0 — Build | 1 | Cross-compile k2r, k2s, gen-config |
| 1 — Start | 2 | Docker compose up, API reachable |
| 2 — Firewall rules | 13 | nft/ipt TPROXY, IPv4+IPv6, exclude sets, loopback, sysctl |
| 3 — Tunnel | 4 | Status API, HTTP/HTTPS through tunnel |
| 4 — LAN TCP | 6 | HTTP, HTTPS, 1MB download, 5 concurrent connections |
| 5 — LAN DNS | 4 | DNS over UDP/TCP, log interception, NXDOMAIN |
| 6 — Cleanup | 7 | Rules removed, IPv6 cleanup, sysctl restore, idempotent down |
| 7 — Reconnect | 4 | Re-up, no rule stacking, hot switch |
| 8 — Auto-reconnect | 4 | Persisted state, restart recovery, LAN restored |
| 9 — Crash recovery | 5 | kill -9, stale cleanup, auto-reconnect post-crash |
| 10 — API | 8 | SSE events, log levels, error handling, ping |

**Docker Desktop limitation**: TPROXY data plane requires `nft_tproxy` kernel module. Docker Desktop's LinuxKit may lack it — rule installation tests pass but LAN flow tests skip gracefully. Full data-plane validation requires a native Linux host.
