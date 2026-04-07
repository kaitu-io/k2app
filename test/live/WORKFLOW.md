# k2 TUN Mode Live Test Workflow

## Environment

| Component | Details |
|-----------|---------|
| Server | 54.46.4.4 (HK, t3.large, Ubuntu 24.04) |
| Client | macOS arm64, Go 1.25 |
| Protocol | k2v5 (QUIC+TCP-WS) |
| Mode | TUN (requires root) |
| Ports | TCP 443, UDP 443 |

## Test Phases

### Phase 0: Deploy (`00-deploy-server.sh`)
- Cross-compile k2s for linux/amd64
- Upload to server, restart k2s
- Verify connect URL generated

### Phase 1: Server Health (`01-server-health.sh`)
- k2s process running
- TCP/UDP 443 reachable
- TLS handshake OK
- No errors in server log

### Phase 2: Generate Config (`02-gen-config.sh`)
- Generate client YAML from connect URL
- Validate with `k2 -t`

### Phase 3: Start Client (`03-start-client.sh`)
- Start daemon (`k2 run -c config.yaml`)
- Wait for HTTP API ready (127.0.0.1:1777)
- Connect tunnel (`k2 up URL`)
- Verify TUN device created (198.18.x.x)
- Verify routing table updated

### Phase 4: DNS Tests (`04-test-dns.sh`)
- DNS hijack: all port-53 queries intercepted
- Chinese domain → direct DNS (114.114.114.114)
- Foreign domain → proxy DNS (8.8.8.8 via tunnel)
- Batch routing verification

### Phase 5: Routing & Data Plane (`05-test-routing.sh`)
- IP check: exit IP = server IP (proxy path works)
- Chinese site: direct access (no tunnel)
- Foreign site: proxied through tunnel
- YouTube, Google reachable
- Latency comparison (direct vs proxy)

## Debug Cycle

```
for each failure:
  1. Read logs: bash debug-logs.sh errors
  2. Identify root cause
  3. if confidence < 9/10:
     research(sing-tun source, web docs)
     repeat
  4. Fix code
  5. Re-deploy: bash 00-deploy-server.sh  (if server change)
  6. Re-test failed phase
  7. if still fails: next round
```

## Scripts

| Script | Purpose | Needs sudo |
|--------|---------|------------|
| `env.sh` | Environment variables | No |
| `00-deploy-server.sh` | Build + deploy k2s | No |
| `01-server-health.sh` | Server health check | No |
| `02-gen-config.sh` | Generate client config | No |
| `03-start-client.sh` | Start k2 daemon + connect | Yes |
| `04-test-dns.sh` | DNS resolution tests | No |
| `05-test-routing.sh` | Smart routing tests | No |
| `06-stop-client.sh` | Stop k2 + cleanup | Yes |
| `run-all.sh` | Run all phases | Yes |
| `debug-logs.sh` | View client/server logs | No |
