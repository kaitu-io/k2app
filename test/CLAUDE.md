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

## Lossy-link emulation (netem/)

Two drivers share `.build/{k2,k2s,gen-config}` (cross-compile: `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o test/netem/.build/<name> ./cmd/k2 | ./cmd/k2s | ./docker/gen-config`) and the same client shape (proxy mode, socks :1080, daemon API :1777, `K2_MOBILE_POWER_SAVING=1` = appext's mobile probe gate, `docker-compose` passes it through, `nslab.sh` takes `MOBILE=1`).

| Driver | Where it runs | Use it for |
|---|---|---|
| `run.sh` (+ `scenarios.sh`) | Docker (any host) | generic loss/delay/rate/MTU scenarios, health grading, recovery |
| `nslab.sh` (+ `nslab-analyze.sh`) | **native Linux, root** — three netns (`srv` k2s, `cli` k2, root = router/NAT + local HTTP target on 44.44.44.44:8080) | the #3747/#3757 "lossy but reachable QUIC" escape: shaping on the client's UDP egress with `tc prio` + `netem` + `iptables -t mangle` fwmark (silent drops, never `-j DROP`), a traffic loop through the socks port, `/api/stats` polling, per-run results in `.lab/results/<TAG>/` (`driver.log`, `k2.log`, `traffic.log`, `stats.jsonl`) |

`nslab.sh` knobs that matter (all env): `BIG=400` drops UDP:443 packets ≥ 400 B (echo/ACKs pass, requests with `PAD=800` die); `SHORT_ONLY=1` restricts that to QUIC short-header packets so handshake Initials pass — the exact #3757 fingerprint (handshake + echo fine, requests time out, heartbeat `loss` stays low); `BIG_PROB` partial drop; `HASHLIMIT_RATE` per-srcport budget (carrier-QoS shape); `SERVER_BLACKHOLE=1` dead-egress node (#3463 shape); `TRAFFIC_INTERVAL` (0.5 dense / 5 sparse = the chat-app user); `PAUSE_AT/RESUME_AT/REMOVE_AT/REINJECT_AT` seconds after injection; `BIN=` an older client build as the known-bad control. Do **not** use Docker Desktop on macOS for this: its VM fights the host for memory (a chained run was OOM-killed on a load-68 Mac) and `tc police` there kills handshakes with the payload. The Ubuntu run log (2026-09-16, 14 scenarios) is in spec `docs/superpowers/specs/2026-08-10-dual-transport-quality-scoreboard-design.md` §8e.

## OpenWrt install-flow lab (testlab VM 102)

The k2r-uat harness proves the data plane on Linux; it does **not** run `scripts/install-k2r.sh`, procd, apk/opkg or LuCI. Those only break on a real OpenWrt userland, so the install flow is exercised on the testlab OpenWrt VM (PVE `192.168.1.2`, VM 102 = stock OpenWrt 25.12 x86_64 + qemu-ga, snapshot `baseline`; `TESTLAB_OWNER=<you> testlab-up 102 local` rolls back + starts it, `testlab-down 102` releases it). The LAN client is VM 120 with `testlab-up 120 openwrt` (its test NIC lands behind the router's `br-lan` 10.30.0.0/24; the mgmt NIC 10.10.10.20 stays reachable). Recipe (2026-09-16):

1. On VM 120: `gen-config -mode gateway -host 10.10.10.20 -port 443` → run `k2s` on `:443`; serve a fake CDN (`python3 -m http.server 8081` over `cdn/kaitu/k2r/{LATEST,<ver>/checksums.txt,<ver>/k2r-linux-amd64,install-k2r.sh}`); bind `44.44.44.44/32` on a dummy interface with an HTTP server — a LAN client can reach it **only** through the tunnel; move the LAN NIC into a netns (`ip netns add lan`) so VM 120's own default route stays on mgmt, otherwise k2s' egress loops back through the router.
2. On the router: `wget -qO- <fake-cdn>/install-k2r.sh | sed 's#<real CDN>#<fake-cdn>#' | sh -s '<k2v5://… from etc/k2r.yml>'`, then `k2r status`, `nft list table inet k2r`, `/var/log/kaitu/k2r.log`, and from the LAN netns `curl http://44.44.44.44:8080/` + a DNS lookup.
3. Also cover: router `reboot` (state.json auto-reconnect), the script's upgrade path (bump `LATEST`), the up-to-date path, `k2r down/up/reset`, `k2r service uninstall`, LuCI page `admin/services/k2r`.

Things that only this lab catches (all fixed 2026-09-16): busybox `tr` without `[:upper:]` classes ("Linlx"), OpenWrt 25.x uses **apk** and needs `kmod-nft-tproxy` (not Alpine's `iptables`), `k2r -v` does not exist so upgrades looked like fresh installs, modern LuCI has no `/usr/lib/lua/luci` (menu.d JSON + JS view + rpcd ACL instead), IPv6 LAN bind without brackets, and `setup` not connecting.
