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

Use this skill when operating on Kaitu VPN nodes via MCP tools: `list_nodes`, `exec_on_node`, `ping_node`, `delete_node`.

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
  ├── healthy ──→ k2v5 (bridge network, Docker port mapping :443 TCP+UDP + 40000-40019 UDP)
  ├── healthy ──→ k2v4-slave (bridge, :8443 mapped to container :443)
  └── healthy ──→ k2-oc (bridge, :10001 mapped to container :443)
```

| Container | Role | Network | Image |
|-----------|------|---------|-------|
| k2-sidecar | Registration, config generation, RADIUS proxy, health reporting | bridge (k2-internal) | k2-sidecar:latest |
| k2v5 | ECH front door. Owns port 443. ECH traffic → in-process; non-ECH → SNI route to k2v4/k2-oc | bridge (k2-internal) | k2v5:latest |
| k2v4-slave | Legacy TCP-WS tunnel, receives forwarded non-ECH traffic from k2v5 | bridge (k2-internal) | k2-slave:latest |
| k2-oc | OpenConnect tunnel, RADIUS auth via sidecar | bridge (k2-internal) | k2-oc:latest |

Key details:
- k2-sidecar writes `/etc/kaitu/.ready` when config generation is complete
- All other containers wait for sidecar healthcheck before starting
- All 4 containers use bridge network (k2-internal). k2v5 uses Docker port mapping: 443/tcp + 443/udp + 40000-40019/udp → container 443
- k2v5→k2v4-slave/k2-oc communication uses Docker internal DNS (container names), not host ports
- No iptables management, no NET_ADMIN, no wrapper scripts
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
| `K2OC_ENABLED` | Enable OpenConnect tunnel | `true` / `false` |
| `K2OC_DOMAIN` | OpenConnect domain | Separate from K2_DOMAIN |
| `K2_JUMP_PORT_MIN` | Hop port range start (default 40000) | Docker port mapping to container 443 |
| `K2_JUMP_PORT_MAX` | Hop port range end (default 40019) | 20 ports, high range to avoid GFW scan |
| `K2_CENTER_URL` | Center API URL | Default `https://k2.52j.me` |
| `K2_LOG_LEVEL` | Log level | `debug`, `info`, `warn`, `error` |
| `K2_NODE_NAME` | Human-readable node name | Format: `{region}.{provider}.wm{NN}` e.g. `jp-tokyo.aws.wm04` |
| `K2_NODE_REGION` | Node region identifier | e.g. `jp-tokyo.aws`, `hk.aliyun` |
| `K2_NODE_ARCH` | Node architecture identifier | Default `k2v5`. Reported in registration meta. |
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
| Check hop port mapping | `docker port k2v5` |
| BBR status | `sysctl net.ipv4.tcp_congestion_control` |
| Auto-update log | `tail -50 /apps/kaitu-slave/auto-update.log` |
| Cron entries | `crontab -l` |
| Timezone check | `timedatectl \| head -2` |
| Set timezone | `sudo timedatectl set-timezone Asia/Singapore` |
| Container outbound network | `docker exec k2-sidecar wget -qO- --timeout=5 https://api.ipify.org` |
| Deploy compose to single node | `exec_on_node(ip, "sudo tee /apps/kaitu-slave/docker-compose.yml > /dev/null", { scriptPath: "docker/docker-compose.yml" })` |
| Fix cron (single node) | `(sudo crontab -l 2>/dev/null \| grep -v 'auto-update'; echo '0 4 * * * /apps/kaitu-slave/auto-update.sh >> /apps/kaitu-slave/auto-update.log 2>&1') \| sudo crontab -` |

### User Management (k2v5 auth)

k2v5 supports two auth modes (checked in order, first match wins):
1. **users_file**: `/apps/kaitu-slave/users` on host → bind-mounted to `/etc/k2v5/users` in container
2. **remote_url**: Center API at `/slave/device-check-auth` (fallback when file is empty)

Empty file = 0 users = pure remote auth (default). No restart needed — changes take effect on next full auth. Existing tickets (1h TTL) stay valid.

**File format** (one user per line: `udid:token`):
```
a1b2c3d4:0123456789abcdef0123456789abcdef
e5f6g7h8:abcdef0123456789abcdef0123456789
```

| Operation | Command |
|-----------|---------|
| View users | `cat /apps/kaitu-slave/users` |
| Add user | `echo "udid:token" >> /apps/kaitu-slave/users` |
| Remove user | `sed -i "/^UDID:/d" /apps/kaitu-slave/users` |
| Clear all (remote only) | `truncate -s 0 /apps/kaitu-slave/users` |
| Check auth config | `docker exec k2v5 grep -E 'users_file\|remote_url' /etc/kaitu/k2v5-config.yaml` |

## Step 5: Post-Provisioning Checklist

After provisioning a new node (or reinstalling OS), ensure these are all done:

1. **Timezone**: Must be `Asia/Singapore` (UTC+8). All cron schedules assume Beijing time. Verify: `timedatectl | grep 'Time zone'`. Fix: `sudo timedatectl set-timezone Asia/Singapore`. Now automated by provision-node.sh step 1.
2. **SSH port**: Now automated by provision-node.sh step 11 (port 22 → 1022 only). Verify: `ss -tlnp | grep :1022`
3. **provision-node.sh**: Timezone + Docker CE + IPv6 + BBR + SSH port 1022 + docker group + daemon.json + UFW-Docker + cron + unattended-upgrades removal.
4. **docker-compose.yml**: Deploy via `deploy-compose.sh --node=IP` or SCP manually.
5. **.env**: Restore from backup or generate new. **K2_DOMAIN and K2OC_DOMAIN must be globally unique** — run `list_nodes()` first and verify no other node uses the same domains. Domain collision silently breaks the other node.
6. **auto-update.sh + cron**: Deploy via `deploy-auto-update.sh --node=IP`.
7. **Containers up**: `docker compose up -d` and verify sidecar healthy.
8. **BBR active**: `sysctl net.ipv4.tcp_congestion_control` should show `bbr`. Included in provision-node.sh step 8.
9. **Port mapping**: After containers up, verify: `docker port k2v5` should show 443/tcp + 443/udp + 40000-40019/udp (22 mappings total).
10. **Container network**: Verify container outbound: `docker exec k2-sidecar wget -qO- --timeout=5 https://api.ipify.org`. If timeout → check `iptables --version`. If `(nf_tables)` → fix with `update-alternatives --set iptables /usr/sbin/iptables-legacy` + restart Docker.

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

6. **Port mapping is Docker-managed** — k2v5 hop port mapping (40000-40019 UDP) is handled by Docker port mapping in docker-compose.yml. No manual iptables rules needed. Do not add custom iptables DNAT rules.

7. **Update = pull + up, never down** — To update containers: `docker compose pull && docker compose up -d`. Never use `docker compose down` — it removes containers and causes service interruption. The `up -d` command recreates only changed containers.

8. **Tunnel domains must be globally unique** — See "Domain Registry" section below for the authoritative allocation table and assignment procedure. Center silently reassigns domains to the last registrant — breaking the previous owner with no warning (`tunnels: []`).

## Domain Registry

### Naming Convention

All tunnel domains MUST follow: **`www.{city}.people.cn`**

- `{city}` is a Chinese province or city name in pinyin (lowercase, no spaces)
- Examples: `www.beijing.people.cn`, `www.chengdu.people.cn`, `www.dalian.people.cn`
- No random prefixes, no other TLDs (`.aliyun.com` etc.)
- Each domain is globally unique — no two nodes share the same domain, regardless of protocol (k2v5/k2oc)
- Live data source: always `list_nodes()` — never hardcode node-specific data in this skill

### Domain Assignment Procedure (MANDATORY)

When assigning domains for a new node or changing an existing node's domains:

1. **Run `list_nodes()`** — get all currently allocated domains across ALL nodes
2. **Collect all `domain` values** from every node's `tunnels` array
3. **Pick a city name** not present in the collected domains. Use `www.{city}.people.cn` format only
4. **Write to `.env`** and apply with `docker compose up -d` (NOT `docker restart` — env vars only reload on container recreation)

### Collision Detection

When diagnosing node issues, check for domain collisions:

1. `list_nodes()` — if a node has fewer tunnels than expected (e.g., only k2oc but no k2v5), it's likely a collision victim
2. Read the victim node's `.env` (`K2_DOMAIN` / `K2OC_DOMAIN`) and search `list_nodes()` output for that domain — it will appear on a different node
3. Fix: change the offending node's `.env` to a unique domain → `docker compose up -d` → then restart victim's sidecar to re-register

### `docker restart` vs `docker compose up -d`

- `docker restart` — reuses existing container with OLD env vars. Does NOT re-read `.env`.
- `docker compose up -d` — recreates containers when config/env changed. **Only way to apply `.env` changes.**

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

Deploy `docker/scripts/auto-update.sh` and configure daily cron (04:00 Beijing time). All nodes MUST have timezone set to `Asia/Singapore` (UTC+8) for cron to execute at the correct time.

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

### MCP Tool Reference

| Tool | Purpose | Key Parameters |
|------|---------|---------------|
| `list_nodes` | List all nodes with tunnels, filterable | `country?`, `name?` |
| `exec_on_node` | Execute command on node via SSH | `ip`, `command`, `timeout?` (default 60s), `scriptPath?` |
| `ping_node` | SSH connectivity check (no command) | `ip` |
| `delete_node` | Remove node from Center DB (API only) | `ip` |
| `query_device_logs` | Query device log uploads by UDID/user/time | `udid?`, `user_id?`, `feedback_id?`, `reason?`, `from?`, `to?` |
| `download_device_log` | Download + decompress a log file from S3 | `s3_key` (from query results) |
| `query_feedback_tickets` | Query user feedback tickets | `udid?`, `email?`, `user_id?`, `status?`, `from?`, `to?` |
| `resolve_feedback_ticket` | Mark a ticket as resolved | `id`, `resolved_by` |

**exec_on_node structured output**: Response includes `status` field:
- `"success"` — command executed (check `exitCode` for pass/fail)
- `"ssh_error"` — SSH connection/auth failed (no stdout/stderr returned)
- `"timeout"` — command timed out (partial output may be available)

stdout capped at 10000 chars, stderr at 2000 chars. Both are redacted for secrets.

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

For larger scripts from `docker/scripts/`, the MCP tool reads the local file and pipes it via SSH stdin. This avoids shell escaping issues.

**MANDATORY**: Always use `scriptPath` + `command` for scripts. The `command` parameter specifies the remote shell. Use `"sudo bash -s"` for scripts requiring root (e.g., `provision-node.sh`):

```
exec_on_node(ip, "sudo bash -s", { scriptPath: "docker/scripts/provision-node.sh", timeout: 300 })
```

For scripts that don't need root:

```
exec_on_node(ip, "bash -s", { scriptPath: "docker/scripts/enable-ipv6.sh" })
```

**NEVER** inline large scripts as command strings or use base64 encoding. Always upload via `scriptPath`.

The MCP implementation handles: read local file → SSH exec channel → pipe to stdin → execute remotely.

### Available Scripts

Scripts in `docker/scripts/` (run ON nodes via SSH stdin pipe):

| Script | Purpose | Warning |
|--------|---------|---------|
| `provision-node.sh` | Full node provisioning: Timezone (Asia/Singapore) + Docker CE + IPv6 + BBR + nftables + daemon.json + UFW-Docker + SSH 1022 + auto-update cron | **Destructive**. Stops all containers. For fresh/rebuild nodes only. Requires sudo + explicit user confirmation. |
| `auto-update.sh` | Daily auto-update: pull images, compare, down+up if changed, Slack notify | Safe. Deployed via `deploy-auto-update.sh`. Runs from cron at 04:00 Beijing time (requires Asia/Singapore timezone). |
| `totally-reinstall-docker.sh` | Docker CE reinstall only (no IPv6 kernel params) | **Destructive**. Superseded by `provision-node.sh`. |
| `enable-ipv6.sh` | Enable IPv6 kernel params only | Superseded by `provision-node.sh` step 6. |
| `simple-docker-pull-restart.sh` | Pull latest images and restart | Safe for routine updates. Equivalent to the standard update command. |

Scripts in `.claude/skills/kaitu-node-ops/` (run LOCALLY, orchestrate across nodes):

| Script | Purpose | Warning |
|--------|---------|---------|
| `deploy-compose.sh` | Deploy `docker/docker-compose.yml` to all active nodes via SCP | Safe — MD5 skip, no restart. Use `--all` for inactive nodes. |
| `update-compose.sh` | Pull latest images + restart all active nodes with rolling interval | Safe — uses `pull + up -d` (no `down`). Supports `--sleep`, `--node`, `--dry-run`. |
| `deploy-auto-update.sh` | Deploy `auto-update.sh` + cron to all active nodes | Safe — MD5 skip, idempotent cron. Supports `--node`, `--all`, `--dry-run`. |

## Step 9: Troubleshooting Known Issues

### iptables-nft Incompatibility (Ubuntu 20.04)

**Symptom**: Sidecar loops with `Failed to get IPv4 address: all ipv4 services failed (i/o timeout)`. Host `curl` works fine but `docker exec` network calls fail.

**Root cause**: `iptables` set to `iptables-nft` backend but Docker requires legacy iptables for NAT. Container MASQUERADE rules fail silently → containers cannot reach external network.

**Detection**:
```bash
iptables --version
# BAD: iptables v1.8.4 (nf_tables)
# GOOD: iptables v1.8.4 (legacy)
```

**Fix**:
```bash
sudo update-alternatives --set iptables /usr/sbin/iptables-legacy
sudo update-alternatives --set ip6tables /usr/sbin/ip6tables-legacy
sudo systemctl restart docker
# Then: cd /apps/kaitu-slave && docker compose down && docker compose up -d
```

Note: `provision-node.sh` step 2 handles this for new provisions. This fix is for existing nodes provisioned before the script update.

### Domain Collision (tunnels: [] on a node)

**Symptom**: A previously working node suddenly shows `tunnels: []` in `list_nodes()`.

**Root cause**: Another node registered with the same `K2_DOMAIN` or `K2OC_DOMAIN`. Center reassigns the tunnel to the last registrant.

**Detection**: Run `list_nodes()` and search for the missing domain — it will appear on the wrong node.

**Fix**: Change the conflicting node's `.env` to use unique domains, then restart both nodes.

### Sidecar Restart Loop (no registration)

**Symptom**: `docker compose ps` shows sidecar restarting. Logs show repeated `Detecting missing network info...` → error → restart.

**Common causes**:
1. Container network broken (see iptables-nft above)
2. Missing or invalid `K2_NODE_SECRET` in `.env`
3. `K2_CENTER_URL` unreachable from the node

**Diagnosis**: Check sidecar logs for the specific error, then verify container outbound network with `docker exec k2-sidecar wget -qO- --timeout=5 https://api.ipify.org`.

## Step 10: Post-Deployment Verification

**MANDATORY** after deploying containers to a new or restarted node. Run these checks in order:

| # | Check | Command | Expected |
|---|-------|---------|----------|
| 0 | Timezone correct | `timedatectl \| grep 'Time zone'` | `Asia/Singapore (+08, +0800)` |
| 1 | Containers running | `cd /apps/kaitu-slave && docker compose ps` | All 4 containers Up, sidecar (healthy) |
| 2 | Sidecar registered | `docker logs --tail 30 k2-sidecar \| grep "Registration completed"` | `tunnels=2` |
| 3 | Tunnel domains correct | `docker logs --tail 30 k2-sidecar \| grep "Tunnel registered"` | Correct domains, `created=true` |
| 4 | k2v5 started | `docker logs --tail 20 k2v5 \| grep "server ready"` | `k2s server ready listen=:443` |
| 5 | Container network | `docker exec k2-sidecar wget -qO- --timeout=5 https://api.ipify.org` | Returns node's public IP |
| 6 | MCP cross-check | `list_nodes(name=NODE_NAME)` | `tunnels` array has 2 entries with correct domains |
| 7 | No domain conflict | `list_nodes()` — scan ALL nodes | No other node has `tunnels: []` unexpectedly |
| 8 | Port mapping | `docker port k2v5` | 22 mappings: 443/tcp + 443/udp + 40000-40019/udp |

**If any check fails**, investigate before proceeding:
- `tunnels: []` on another node → domain conflict (guardrail #8 violated)
- Container network timeout → iptables-nft issue (Step 9)
- Sidecar restart loop → check `.env` for missing/invalid values (Step 9)

## Step 11: Cloud Provider Notes

- **AWS nodes are Lightsail, not EC2**.查实例/重启优先用 `aws lightsail` 命令（`get-instances`, `reboot-instance`），不要用 `aws ec2`。

## Step 12: Device Log Troubleshooting

When a user reports a problem, use these tools to find and analyze their device logs:

### Workflow

1. **Get identifier** — Ask for the user's UDID, email, or user ID
2. **Find feedback tickets** — `query_feedback_tickets(email="user@example.com")` or `query_feedback_tickets(udid="...")`
3. **Find associated logs** — `query_device_logs(feedback_id="...")` using the feedbackId from the ticket
4. **Download and read logs** — `download_device_log(s3_key="...")` for each log file (service, crash, desktop, system)
5. **Run quick diagnosis** — Execute `bash scripts/k2-quick-diag.sh <downloaded-log-path>` on any k2.log file. The script auto-extracts DIAG events, heartbeats, failures, and gives a verdict (OK/WARN/CRITICAL/PANIC). Use its output to guide deeper investigation.
6. **Deep dive if needed** — Use the DIAG grep patterns in Step 12.1 for targeted analysis based on the script's findings
7. **Resolve** — `resolve_feedback_ticket(id=123, resolved_by="claude")` when troubleshooting is complete

### Log Types

| Type | Content |
|------|---------|
| `service` | Go daemon logs (k2.log) — VPN connection, wire protocol, engine events |
| `crash` | Go panic/crash logs (panic-*.log) — stack traces |
| `desktop` | Tauri desktop app logs (desktop.log) — IPC, updater, tray, log upload |
| `system` | OS-level logs (macOS Console / Windows Event Log) filtered for kaitu |

### Upload Reasons

| Reason | Trigger |
|--------|---------|
| `user_feedback_report` | User submitted a feedback ticket (SubmitTicket page) |
| `beta-auto-upload` | Automatic 24h upload from beta channel users |

### Tips

- Logs are gzip-compressed on S3; `download_device_log` auto-decompresses
- Large logs are truncated to 50k chars; focus on recent entries (end of file)
- Cross-reference `feedback_id` between tickets and logs to find related data
- Time filters (`from`, `to`) use RFC3339 format: `2026-03-08T00:00:00Z`

### DIAG Log Analysis (k2 client logs)

k2 client uses a three-layer diagnostic logging system. All diagnostic logs use the `DIAG:` prefix. When analyzing downloaded `service` logs (k2.log), use this triage workflow:

**Step 1: Quick health scan** — Is the tunnel running and healthy?
```bash
grep "DIAG: heartbeat" <logfile> | tail -20
```
Each heartbeat (every 30s) shows: `health`, `transport`, `loss`, `rttMs`, `txMB`, `rxMB`, `tcpConns`, `udpConns`, `uptimeS`, `fallback`.

**Step 2: Find problems** — What went wrong?
```bash
grep "DIAG:" <logfile> | grep -v heartbeat
```
This shows all event-driven diagnostics (dns-slow, dns-fail, proxy-dial-fail, transport-switch, wire-error, etc.).

**Step 3: Layer-specific drill-down**

| Layer | grep | What it shows |
|-------|------|---------------|
| Connection | `grep "DIAG: connected\|DIAG: session-end"` | Session lifecycle + total traffic |
| DNS | `grep "DIAG: dns"` | Slow (>500ms) or failed DNS queries |
| Transport | `grep "DIAG: quic\|DIAG: transport"` | QUIC handshake failures, QUIC↔TCP-WS switches |
| Proxy | `grep "DIAG: proxy-dial"` | Failed or slow (>3s) proxy dial attempts |
| Wire | `grep "DIAG: wire-error"` | Classified engine errors (auth, timeout, unreachable) |
| Health | `grep "health: degraded\|health: critical"` | Health state transitions (existing logs) |

**DIAG Event Reference:**

| Event | Level | Meaning |
|-------|-------|---------|
| `DIAG: heartbeat` | INFO | 30s periodic health snapshot |
| `DIAG: connected` | INFO | Tunnel established (server, mode, dial time) |
| `DIAG: session-end` | INFO | Tunnel torn down (uptime, total tx/rx) |
| `DIAG: dns-slow` | INFO | DNS query took >500ms |
| `DIAG: dns-fail` | WARN | DNS upstream query failed |
| `DIAG: proxy-dial-fail` | WARN | Wire proxy dial failed (TCP or UDP) |
| `DIAG: proxy-dial-slow` | INFO | Wire proxy dial took >3s |
| `DIAG: quic-handshake-fail` | WARN | QUIC handshake failed (UDP may be blocked) |
| `DIAG: transport-switch` | WARN | Transport changed (QUIC→TCP-WS or back) |
| `DIAG: wire-error` | WARN | Classified engine error with code/category |

**Common diagnosis patterns:**

| Symptom in heartbeat | Likely cause | Next step |
|---------------------|-------------|-----------|
| `health=degraded`, `loss>0.05` | Packet loss on network path | Check `DIAG: quic-handshake-fail` count |
| `health=critical`, `loss>0.25` | Severe packet loss / UDP blocking | Check `DIAG: transport-switch` for fallback |
| `fallback=true` | QUIC blocked, using TCP-WS | Check `DIAG: quic-handshake-fail` for root cause |
| `tcpConns=0`, `udpConns=0` | No traffic flowing | Check `DIAG: proxy-dial-fail` for wire errors |
| `rttMs` very high (>500) | High latency path | May be normal for distant servers |

**Use the quick-diag script** (`scripts/k2-quick-diag.sh`) to automate this analysis — see Step 13.

## Step 13: Client Log Quick Diagnosis Script

**ALWAYS run this script first** when analyzing any k2 client log. It replaces manual grep triage.

```bash
# After download_device_log saves to /tmp/kaitu-device-logs/:
bash scripts/k2-quick-diag.sh /tmp/kaitu-device-logs/k2.log

# For local daemon logs (auto-detects macOS/Linux path):
bash scripts/k2-quick-diag.sh
```

The script outputs:
1. **Session info** — Last connected/disconnected timestamps
2. **Recent heartbeats** — Last 5 DIAG heartbeats with health/transport/loss
3. **Problem events** — All non-heartbeat DIAG events (failures, slow queries, transport switches)
4. **Event summary** — Counts of each DIAG event type
5. **Health transitions** — degraded/critical/recovery state changes
6. **Panics** — Any panic stack traces
7. **Verdict** — Overall assessment (OK / WARN / CRITICAL / PANIC)
