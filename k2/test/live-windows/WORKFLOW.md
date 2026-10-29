# k2 Windows TUN Mode Live Test Workflow

## Environment

| Component | Details |
|-----------|---------|
| Server | 54.46.4.4 (HK, t3.large, Ubuntu 24.04) |
| Client | Windows 11 ARM64 (Parallels VM, 10.211.55.6) |
| Protocol | k2v5 (QUIC+TCP-WS) |
| Mode | TUN (requires admin) |
| Control | All scripts run from Mac via SSH |

## Network Safety Design

k2 TUN modifies routes + DNS, which can break SSH connectivity. Three safety layers:

1. **sing-tun RouteExclude** — `10.211.55.0/24` excluded from TUN routing in client config
2. **Windows persistent route** — `route -p add 10.211.55.0 mask 255.255.255.0 10.211.55.1 metric 1` before TUN start
3. **Watchdog script** — scheduled task pings Mac gateway, kills k2 + restores network if unreachable

## Test Phases

### Phase 0: Build & Deploy (`00-build-deploy.sh`)
- Cross-compile k2 for windows/arm64
- Upload to Windows VM via SCP
- Verify binary runs

### Phase 1: Network Safety (`01-safety-net.sh`)
- Add persistent route for Parallels subnet
- Install watchdog scheduled task
- Verify SSH survives simulated route change

### Phase 2: Generate Config (`02-gen-config.sh`)
- Generate client YAML with RouteExcludeAddrs for Parallels subnet
- Upload config to Windows
- Validate with `k2.exe -t`

### Phase 3: Start Client (`03-start-client.sh`)
- Start k2 daemon (admin)
- Wait for HTTP API ready
- Connect tunnel (`k2 up`)
- Verify TUN device created (sing-tun adapter)
- Verify routing table updated
- Verify SSH still works (safety check)

### Phase 4: DNS Tests (`04-test-dns.sh`)
- DNS hijack working (port 53 interception)
- Chinese domain → direct DNS
- Foreign domain → proxy DNS
- Batch domain routing test

### Phase 5: Routing & Data Plane (`05-test-routing.sh`)
- Exit IP = server IP (proxy path works)
- Chinese site: direct access
- Foreign site: proxied through tunnel
- Latency comparison

### Phase 6: Crash Recovery (`06-test-crash-recovery.sh`)
- Force-kill k2 process (simulated crash)
- Verify network recovers (SSH still works)
- Check route table is clean
- Check DNS is restored
- Restart k2 — should work after crash

### Phase 7: Stop & Cleanup (`07-stop-cleanup.sh`)
- Graceful disconnect (k2 down)
- Stop daemon
- Verify TUN device removed
- Verify routes restored
- Verify DNS restored
- Remove watchdog scheduled task
- Remove persistent route

## Scripts

| Script | Purpose | Runs on |
|--------|---------|---------|
| `env.sh` | Environment variables + helpers | Mac (sourced) |
| `00-build-deploy.sh` | Build k2 for Windows + upload | Mac → Windows |
| `01-safety-net.sh` | Install network safety layers | Mac → Windows |
| `02-gen-config.sh` | Generate + upload client config | Mac → Windows |
| `03-start-client.sh` | Start k2 daemon + connect | Mac → Windows |
| `04-test-dns.sh` | DNS resolution tests | Mac → Windows |
| `05-test-routing.sh` | Smart routing tests | Mac → Windows |
| `06-test-crash-recovery.sh` | Crash recovery verification | Mac → Windows |
| `07-stop-cleanup.sh` | Stop k2 + full cleanup | Mac → Windows |
| `run-all.sh` | Run all phases | Mac |
| `debug-logs.sh` | View client/server logs | Mac → Windows |
| `emergency-restore.sh` | Nuclear option: restore network | Mac → Windows |

## Emergency Recovery

If SSH breaks (all safety layers failed):

1. Open Parallels Desktop window
2. Run as Admin: `C:\Users\david\k2-test\emergency-restore.ps1`
3. Or reboot the VM (clears all transient routes)
