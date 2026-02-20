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
| k2-slave-sidecar | Same as k2-sidecar above | bridge | k2-slave-sidecar:latest |
| k2-slave | SNI router (no ECH), host network, port 443 | host | k2-slave:latest |
| k2-oc | Same as above | bridge | k2-oc:latest |

Differences from new architecture:
- Container names: `k2-slave-sidecar` instead of `k2-sidecar`, `k2-slave` instead of `k2v5`
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
- **Old arch**: `{sidecar}` = `k2-slave-sidecar`, `{tunnel}` = `k2-slave`

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

## Step 5: Safety Guardrails

These are best-practice guardrails to prevent accidental damage during operations. You have full SSH root access — these rules guide safe usage:

1. **K2_NODE_SECRET is untouchable** — Never read, display, modify, or transmit the node secret. The MCP layer provides technical backstop via stdout redaction, but avoid accessing it in the first place.

2. **Never delete /apps/kaitu-slave/** — This directory contains the entire node deployment (docker-compose.yml, .env, logs, volumes).

3. **Never modify docker-compose.yml** — Configuration changes go through `.env` file only. The compose file is managed by the deployment system.

4. **Confirm before restart** — Before restarting any container, run `docker compose ps` to check current state. Ensure you understand what will be affected.

5. **Never touch /etc/kaitu/ directly** — This is auto-generated config from sidecar. Manual changes will be overwritten on next sidecar restart.

6. **Never modify iptables rules** — Hop port DNAT rules are managed by k2v5/k2-slave entrypoint scripts. Manual changes break on restart.

7. **Update = pull + up, never down** — To update containers: `docker compose pull && docker compose up -d`. Never use `docker compose down` — it removes containers and causes service interruption. The `up -d` command recreates only changed containers.

## Step 6: Script Execution

Two modes for running scripts on nodes:

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

Scripts are in `docker/scripts/` in the project repository:

| Script | Purpose | Warning |
|--------|---------|---------|
| `prepare-docker-compose.sh` | Initialize node deployment directory + write docker-compose.yml | **Old architecture only**. Requires sudo. Do NOT run on nodes that already have a deployment. |
| `totally-reinstall-docker.sh` | Full Docker CE reinstall (cleanup + nftables + IPv6 + ufw-docker) | **Destructive**. Stops all running containers. Requires explicit user confirmation. |
| `enable-ipv6.sh` | Enable IPv6 kernel params + test connectivity | Requires sudo. Restarts networking service. |
| `simple-docker-pull-restart.sh` | Pull latest images and restart | Safe for routine updates. Equivalent to the standard update command. |
