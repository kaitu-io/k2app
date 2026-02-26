---
name: kaitu-node-ops
description: Node infrastructure operations via kaitu-ops-mcp tools. Safety guardrails, dual-architecture identification, standard ops commands, and script execution guidance.
triggers:
  - node ops
  - server ops
  - node management
  - docker ops
  - k2 node
  - exec on node
  - list nodes
  - node health
  - node restart
  - node update
  - node logs
  - docker compose
  - k2v5
  - k2-slave
---

# Kaitu Node Operations

Use this skill when operating on Kaitu VPN nodes via `list_nodes` and `exec_on_node` MCP tools.

## Step 1: Identify Node Architecture

Before ANY operation on a node, determine which architecture it runs:

```bash
# Run this FIRST on every node
exec_on_node(ip, "docker ps --format '{{.Names}}'")
```

**If output contains `k2v5`** → New architecture (k2v5 front door)
**If output contains `k2-slave`** → Old architecture (k2-slave SNI router)

## Step 2: Understand the Architecture

### New Architecture (k2v5 front door)

Deployment path: `/apps/kaitu-slave/`

4 containers with strict dependency chain:

```
k2-sidecar (bridge network)
  ├── healthy ──→ k2v5 (host network, owns :443 TCP+UDP)
  ├── healthy ──→ k2v4-slave (bridge, :K2V4_PORT mapped to container :443)
  └── healthy ──→ k2-oc (bridge, :K2OC_PORT mapped to container :443)
```

| Container | Role | Network | Image |
|-----------|------|---------|-------|
| k2-sidecar | Registration, config generation, RADIUS proxy, health reporting | bridge (k2-internal) | k2-sidecar:latest |
| k2v5 | ECH front door. Owns port 443. ECH traffic → in-process; non-ECH → SNI route to k2v4/k2-oc | host | k2v5:latest |
| k2v4-slave | Legacy TCP-WS tunnel, receives forwarded non-ECH traffic from k2v5 | bridge (k2-internal) | k2-slave:latest |
| k2-oc | OpenConnect tunnel, RADIUS auth via sidecar | bridge (k2-internal) | k2-oc:latest |

Key details:
- k2-sidecar writes `/etc/kaitu/.ready` when config generation is complete
- All other containers wait for sidecar healthcheck before starting
- k2v5 uses host network for direct port 443 access + iptables hop port DNAT
- k2v4-slave and k2-oc use bridge network, exposed via port mapping
- Images from `public.ecr.aws/d6n9t2r2/`

### Old Architecture (k2-slave SNI router)

Deployment path: `/apps/kaitu-slave/` (same)

3 containers:

| Container | Role | Network | Image |
|-----------|------|---------|-------|
| k2-sidecar | Same as k2-sidecar above | bridge | k2-sidecar:latest |
| k2-slave | SNI router (no ECH), host network, port 443 | host | k2-slave:latest |
| k2-oc | Same as above | bridge | k2-oc:latest |

Differences from new architecture:
- Container names: `k2-sidecar` (same name, same image), `k2-slave` instead of `k2v5`
- No k2v4-slave container (k2-slave IS the tunnel, not a front door)
- k2-slave does SNI routing without ECH support
- All operational commands are the same — just substitute container names

## Step 3: Environment Variables

The `.env` file is at `/apps/kaitu-slave/.env`. Core variables:

| Variable | Purpose | Notes |
|----------|---------|-------|
| `K2_NODE_SECRET` | Node authentication key | **NEVER read, display, or modify** |
| `K2_DOMAIN` | Tunnel domain (wildcard `*.example.com`) | Shared by k2v5 + k2v4 |
| `K2V4_PORT` | k2v4 container port (default 8443) | New arch only |
| `K2OC_ENABLED` | Enable OpenConnect tunnel | `true` / `false` |
| `K2OC_DOMAIN` | OpenConnect domain | Separate from K2_DOMAIN |
| `K2OC_PORT` | OpenConnect port (default 10001) | |
| `K2_HOP_PORT_MIN` | Hop port range start (default 10020) | iptables DNAT to 443 |
| `K2_HOP_PORT_MAX` | Hop port range end (default 10119) | Max 100 ports |
| `K2_CENTER_URL` | Center API URL | Default `https://k2.52j.me` |
| `K2_LOG_LEVEL` | Log level | `debug`, `info`, `warn`, `error` |
| `K2_NODE_BILLING_START_DATE` | Traffic billing start | Format: `YYYY-MM-DD` |
| `K2_NODE_TRAFFIC_LIMIT_GB` | Traffic limit in GB | `0` = unlimited |

## Step 4: Standard Operations

Use `exec_on_node(ip, command)` for all operations. Replace `{sidecar}` and `{tunnel}` with the correct container names based on architecture identification:

- **New arch**: `{sidecar}` = `k2-sidecar`, `{tunnel}` = `k2v5`
- **Old arch**: `{sidecar}` = `k2-sidecar`, `{tunnel}` = `k2-slave`

| Operation | Command |
|-----------|---------|
| Identify architecture | `docker ps --format '{{.Names}}'` |
| All container status | `cd /apps/kaitu-slave && docker compose ps` |
| Container logs (tail) | `docker logs --tail 100 {container_name}` |
| Pull + restart all | `cd /apps/kaitu-slave && docker compose pull && docker compose up -d` |
| Restart one container | `docker restart {container_name}` |
| View .env config | `cat /apps/kaitu-slave/.env` |
| Sidecar health | `docker inspect --format='{{.State.Health.Status}}' {sidecar}` |
| Disk/memory/CPU | `df -h && free -h && top -bn1 \| head -5` |
| Network connections | `ss -s` |
| IPv6 status | `ip -6 addr show scope global` |
| Check hop port rules | `iptables -t nat -L PREROUTING -n \| grep -E "10020\|REDIRECT"` |
| BBR status | `sysctl net.ipv4.tcp_congestion_control` |
| Auto-update log | `tail -50 /apps/kaitu-slave/auto-update.log` |
| Cron entries | `crontab -l` |

## Step 5: Post-Provisioning Checklist

After provisioning a new node (or reinstalling OS), ensure these are all done:

1. **SSH port**: Changed from 22 to 1022. On Ubuntu 24.04, also sed `/etc/ssh/sshd_config.d/*.conf` drop-in files.
2. **provision-node.sh**: Docker CE + IPv6 + BBR + nftables + daemon.json + UFW-Docker + cron.
3. **docker-compose.yml**: Deploy via `deploy-compose.sh --node=IP` or SCP manually.
4. **.env**: Restore from backup or generate new via Center API.
5. **auto-update.sh + cron**: Deploy via `deploy-auto-update.sh --node=IP`.
6. **Containers up**: `docker compose up -d` and verify sidecar healthy.
7. **BBR active**: `sysctl net.ipv4.tcp_congestion_control` should show `bbr`. Included in provision-node.sh step 7.
8. **Disable unattended-upgrades**: `sudo apt-get remove -y unattended-upgrades` (prevents surprise reboots).

### BBR Congestion Control

All nodes MUST have BBR enabled. BBR significantly improves TCP throughput, especially for high-latency cross-border connections.

```bash
# Check BBR status
sysctl net.ipv4.tcp_congestion_control
# Expected: net.ipv4.tcp_congestion_control = bbr

# Enable BBR (if not already done by provision-node.sh)
sudo bash -c 'sed -i "/net.core.default_qdisc/d" /etc/sysctl.conf; sed -i "/net.ipv4.tcp_congestion_control/d" /etc/sysctl.conf; echo "net.core.default_qdisc = fq" >> /etc/sysctl.conf; echo "net.ipv4.tcp_congestion_control = bbr" >> /etc/sysctl.conf; sysctl -p'
```

Requires Linux kernel 4.9+ (all Ubuntu 20.04+ have it).

## Step 6: Safety Guardrails

These are best-practice guardrails to prevent accidental damage during operations. You have full SSH root access — these rules guide safe usage:

1. **K2_NODE_SECRET is untouchable** — Never read, display, modify, or transmit the node secret. The MCP layer provides technical backstop via stdout redaction, but avoid accessing it in the first place.

2. **Never delete /apps/kaitu-slave/** — This directory contains the entire node deployment (docker-compose.yml, .env, logs, volumes).

3. **Never modify docker-compose.yml** — Configuration changes go through `.env` file only. The compose file is managed by the deployment system.

4. **Confirm before restart** — Before restarting any container, run `docker compose ps` to check current state. Ensure you understand what will be affected.

5. **Never touch /etc/kaitu/ directly** — This is auto-generated config from sidecar. Manual changes will be overwritten on next sidecar restart.

6. **Never modify iptables rules** — Hop port DNAT rules are managed by k2v5/k2-slave entrypoint scripts. Manual changes break on restart.

7. **Update = pull + up, never down** — To update containers: `docker compose pull && docker compose up -d`. Never use `docker compose down` — it removes containers and causes service interruption. The `up -d` command recreates only changed containers.

## Step 7: Batch Operations

All batch scripts live in this skill directory (`.claude/skills/kaitu-node-ops/`).

**Requires**: `KAITU_CENTER_URL` + `KAITU_ACCESS_KEY` env vars. SSH via `KAITU_SSH_USER` (default: `ubuntu`) port `KAITU_SSH_PORT` (default: `1022`).

### Deploy docker-compose.yml to All Nodes

The canonical k2v5 compose file lives at `docker/docker-compose.yml` in the repo.

```bash
# Active nodes only (tunnelCount > 0) — recommended
.claude/skills/kaitu-node-ops/deploy-compose.sh

# All nodes including inactive (tunnelCount = 0)
.claude/skills/kaitu-node-ops/deploy-compose.sh --all

# Preview what would be deployed
.claude/skills/kaitu-node-ops/deploy-compose.sh --dry-run
```

The script fetches the live node list from Center API, filters by `tunnelCount > 0`, compares MD5 checksums (skips up-to-date), creates `/apps/kaitu-slave/` if missing, and uploads via SCP.

### Update Docker Compose on All Nodes

Pull latest images and restart containers on all active nodes with a rolling interval.

```bash
# Update all active nodes (60s between each)
.claude/skills/kaitu-node-ops/update-compose.sh

# Custom interval between nodes
.claude/skills/kaitu-node-ops/update-compose.sh --sleep=30

# Update a single node only
.claude/skills/kaitu-node-ops/update-compose.sh --node=8.218.55.0

# Preview what would be updated
.claude/skills/kaitu-node-ops/update-compose.sh --dry-run
```

Per-node steps: `docker compose pull` → `docker compose up -d` → wait 10s → verify sidecar healthy → sleep before next node.

### Deploy Auto-Update Cron to All Nodes

Deploy `docker/scripts/auto-update.sh` and configure daily cron (20:00 UTC = 04:00 Beijing).

```bash
# Active nodes only — recommended
.claude/skills/kaitu-node-ops/deploy-auto-update.sh

# All nodes including inactive
.claude/skills/kaitu-node-ops/deploy-auto-update.sh --all

# Single node
.claude/skills/kaitu-node-ops/deploy-auto-update.sh --node=8.218.55.0

# Preview
.claude/skills/kaitu-node-ops/deploy-auto-update.sh --dry-run
```

Per-node steps: SCP `auto-update.sh` → `chmod +x` → ensure cron installed → add cron entry (idempotent).

**Auto-update script behavior** (`docker/scripts/auto-update.sh`):
1. Random 0-10 min stagger delay (skip with `K2_NO_STAGGER=1`)
2. `docker compose pull` with retry (5 attempts, exponential backoff for ECR rate limits)
3. Compare running container image IDs vs pulled `:latest` — skip if identical
4. `docker compose down` (remove containers + networks, keep volumes) + `docker compose up -d`
5. Wait 30s, verify sidecar healthy
6. Slack notification on update/error (silent when no changes)
7. Log to `/apps/kaitu-slave/auto-update.log` (auto-rotate at 1MB)

### Node Activity Heuristic

The Center API has no explicit `status` field. Use these signals to identify active vs inactive nodes:

| Signal | Active | Inactive |
|--------|--------|----------|
| `tunnelCount` | > 0 | 0 |
| `name` | Named (`hk.aliyun.wm01`) | IP-as-name (`13.114.150.53`) |
| SSH on :1022 | Reachable | Often unreachable |

Nodes with `tunnelCount == 0` and IP-as-name are typically decommissioned AWS instances. The batch scripts skip them by default.

## Step 8: Script Execution

Two modes for running scripts on individual nodes:

### Mode 1: Direct Command (small operations)

For one-liners or short commands, use the `command` parameter directly:

```
exec_on_node(ip, "cd /apps/kaitu-slave && docker compose ps")
```

### Mode 2: Stdin Pipe (script files)

For larger scripts from `docker/scripts/`, the MCP tool reads the local file and pipes it via SSH stdin. This avoids shell escaping issues:

```
exec_on_node(ip, "bash -s", { scriptPath: "docker/scripts/enable-ipv6.sh" })
```

The MCP implementation handles: read local file → SSH exec channel → pipe to stdin → execute remotely.

### Available Scripts

Scripts in `docker/scripts/` (run ON nodes via SSH stdin pipe):

| Script | Purpose | Warning |
|--------|---------|---------|
| `provision-node.sh` | Full node provisioning: Docker CE + IPv6 + BBR + nftables + daemon.json + UFW-Docker + auto-update cron | **Destructive**. Stops all containers. For fresh/rebuild nodes only. Requires sudo + explicit user confirmation. |
| `auto-update.sh` | Daily auto-update: pull images, compare, down+up if changed, Slack notify | Safe. Deployed via `deploy-auto-update.sh`. Runs from cron at 20:00 UTC. |
| `totally-reinstall-docker.sh` | Docker CE reinstall only (no IPv6 kernel params) | **Destructive**. Superseded by `provision-node.sh`. |
| `enable-ipv6.sh` | Enable IPv6 kernel params only | Superseded by `provision-node.sh` step 6. |
| `simple-docker-pull-restart.sh` | Pull latest images and restart | Safe for routine updates. Equivalent to the standard update command. |

Scripts in `.claude/skills/kaitu-node-ops/` (run LOCALLY, orchestrate across nodes):

| Script | Purpose | Warning |
|--------|---------|---------|
| `deploy-compose.sh` | Deploy `docker/docker-compose.yml` to all active nodes via SCP | Safe — MD5 skip, no restart. Use `--all` for inactive nodes. |
| `update-compose.sh` | Pull latest images + restart all active nodes with rolling interval | Safe — uses `pull + up -d` (no `down`). Supports `--sleep`, `--node`, `--dry-run`. |
| `deploy-auto-update.sh` | Deploy `auto-update.sh` + cron to all active nodes | Safe — MD5 skip, idempotent cron. Supports `--node`, `--all`, `--dry-run`. |
