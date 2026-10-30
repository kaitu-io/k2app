# k2s canary node

A k2s that no user ever reaches, for testing a k2s build on a real network
before it touches the fleet. Standalone k2s only: no sidecar, no Center
registration, auth = the self-provisioned udid:token in its cert_dir.

| | |
|---|---|
| Host | AWS Lightsail `canary-restart-test`, ap-southeast-1, nano (0.5 GB + 1 GB swap), Ubuntu 24.04, BBR, TZ Asia/Singapore |
| IP | static `k2s-canary-ip` = **47.131.131.14** (SSH `ubuntu@` with `~/.ssh/id_rsa`, port 22) |
| Dir | `/apps/k2s-canary` (compose project `canary`; volume `canary_k2v5-data` = cert_dir + `k2s.log`) |
| Firewall | Lightsail: 22/tcp, 443/tcp, 443/udp |
| Connect URL | `~/.config/k2s-canary/connect-url` (600, written by `deploy.sh`) — contains the credentials, never print it |

## Before a fleet rollout

```bash
docker/canary/deploy.sh --binary <k2s linux/amd64>     # or: deploy.sh <ECR image ref>
docker/canary/client.sh start <k2 darwin binary>        # private daemon on 127.0.0.1:17777, SOCKS 10805
docker/canary/bench.sh <label>                          # throughput + 400 requests
python3 docker/canary/probe.py ~/.cache/k2s-canary/probe.log &   # availability probe (add --long for a download)
ssh ubuntu@47.131.131.14 'sudo bash /apps/k2s-canary/restart-cycle.sh new recreate 3 30' > ~/.cache/k2s-canary/events.log
python3 docker/canary/analyze.py ~/.cache/k2s-canary/probe.log ~/.cache/k2s-canary/events.log
docker/canary/client.sh stop
```

- **Old vs new**: run the same with the fleet's current image (`deploy.sh public.ecr.aws/d6n9t2r2/k2s:<tag>`) and an old client — the installed app's binary is one (`/Applications/Kaitu.app/Contents/MacOS/k2`, copy it; `k2 version`).
- **TCP-WS / smux path**: the client always prefers QUIC. Close UDP 443 on the Lightsail firewall (`aws lightsail close-instance-public-ports --instance-name canary-restart-test --region ap-southeast-1 --port-info fromPort=443,toPort=443,protocol=udp`), check `transport-race-winner winner=tcpws` in `~/.cache/k2s-canary/k2-daemon.log`, and **open it again afterwards**.
- **Never** use `k2 up` or port 1777 here: that is the installed app's root daemon, and `k2 up` would switch the user's own VPN to the canary. `client.sh` checks the app daemon's state is unchanged on `stop`.
- Upgrade path test: `restart-cycle.sh <label> recreate 1 30 <new image>` with the old image running. Crash path: `restart-cycle.sh <label> kill <n>`.

## Results so far

2026-10-30, Mac client ~55 ms RTT, proxy mode.

Restart (one `up -d -t 2`, user-visible outage via 200 ms probes):

| server | QUIC | TCP-WS |
|---|---|---|
| v0.4.14-918b82f1 (exits only on SIGKILL, says nothing) | 14 / 18 / 26 s; upgrade old→new 22 s | — |
| main @ 4595c1c9a (close 0x04 + reset key) | 1.4 / 4.2 / 4.4 s; SIGKILL 3.2 / 3.2 s | 2.0 / 1.9 / 1.7 s |

TCP-WS single-connection throughput (smux receive windows are receiver-local):

| client \ server | old (v0.4.14) | new (main) |
|---|---|---|
| old 0.4.13 | down 8 Mbps, up 9 Mbps | down 8 Mbps, up 24–57 Mbps |
| new (main) | down 25–55 Mbps, up 9 Mbps | down 44–53 Mbps, up 27–94 Mbps |

400 requests per cell, 0 failures in every combination. QUIC baseline new/new: down 50 Mbps.
